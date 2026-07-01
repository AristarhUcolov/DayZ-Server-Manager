// Copyright (c) 2026 Aristarh Ucolov.
package server

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dayzmanager/internal/config"
)

// Broadcaster is implemented by rcon.Manager — kept as an interface here so
// the server package doesn't depend on rcon (which would cycle: rcon may
// later want config, and everyone wants server).
type Broadcaster interface {
	Say(msg string) error
}

// Controller supervises the DayZServer_x64 process.
type Controller struct {
	serverDir string
	cfg       *config.Manager
	log       *log.Logger

	mu        sync.Mutex
	cmd       *exec.Cmd
	startedAt time.Time

	running atomic.Bool

	// Auto-restart loop.
	restartStop chan struct{}

	// Optional RCon broadcaster — set by app wiring. Used to announce
	// scheduled/auto restarts with a countdown.
	Broadcast Broadcaster

	// Mod auto-update hooks — set by app wiring so the server package doesn't
	// import mods. Both take the client !Workshop's owning DayZ path. Nil = the
	// feature is unavailable (no-op). UpdateMods copies newer mod files into the
	// server dir (must run while stopped — file locks); ModUpdatesAvailable
	// reports whether any active mod is newer in !Workshop than on the server.
	UpdateMods          func(vanillaPath string) ([]string, error)
	ModUpdatesAvailable func(vanillaPath string) (bool, error)

	// One-shot: forces a mod update on the next restart even when
	// AutoUpdateModsOnRestart is off (set by the update-check loop, which
	// restarts precisely because it found a pending update).
	forceModUpdate atomic.Bool

	// Crash-loop protection — track recent exits to detect a broken mod set.
	recentExits []time.Time
	loopPaused  atomic.Bool

	// Schedule snapshot. The supervisor loops run for the whole process
	// lifetime and read these every minute, while the web layer replaces the
	// shared *config.Manager wholesale on every POST. Reading the live config
	// from the loops would be a data race (a torn slice header could panic the
	// goroutine, which runs outside the HTTP recoverer and would take the whole
	// manager down). So the loops read this immutable copy instead, refreshed
	// via SetScheduleConfig whenever the config changes.
	schedMu sync.Mutex
	sched   scheduleConfig
}

// scheduleConfig is the subset of manager config the supervisor loops need.
// Slices are always replaced wholesale (never mutated in place) so a snapshot
// returned by schedSnapshot can be read without further locking.
type scheduleConfig struct {
	autoRestartEnabled bool
	autoRestartSeconds int
	restartWarnMinutes []int
	scheduledRestarts  []string
	announcements      []config.ScheduledAnnouncement
	intervalAnnounces  []config.IntervalAnnouncement

	// Mod auto-update (item 2).
	autoUpdateOnRestart    bool
	autoUpdateCheckMinutes int
	vanillaPath            string
}

func NewController(serverDir string, cfg *config.Manager, logger *log.Logger) *Controller {
	c := &Controller{serverDir: serverDir, cfg: cfg, log: logger}
	c.SetScheduleConfig(cfg)
	return c
}

// SetScheduleConfig refreshes the snapshot the supervisor loops read. Call it
// from every path that mutates the manager config (web config POST,
// announcements POST, ReloadConfig) so scheduled restarts/announcements pick up
// changes without a restart — and without racing the loops.
func (c *Controller) SetScheduleConfig(cfg *config.Manager) {
	c.schedMu.Lock()
	defer c.schedMu.Unlock()
	c.sched = scheduleConfig{
		autoRestartEnabled:     cfg.AutoRestartEnabled,
		autoRestartSeconds:     cfg.AutoRestartSeconds,
		restartWarnMinutes:     append([]int(nil), cfg.RestartWarnMinutes...),
		scheduledRestarts:      append([]string(nil), cfg.ScheduledRestarts...),
		announcements:          append([]config.ScheduledAnnouncement(nil), cfg.ScheduledAnnouncements...),
		intervalAnnounces:      append([]config.IntervalAnnouncement(nil), cfg.IntervalAnnouncements...),
		autoUpdateOnRestart:    cfg.AutoUpdateModsOnRestart,
		autoUpdateCheckMinutes: cfg.AutoUpdateCheckMinutes,
		vanillaPath:            cfg.VanillaDayZPath,
	}
}

func (c *Controller) schedSnapshot() scheduleConfig {
	c.schedMu.Lock()
	defer c.schedMu.Unlock()
	return c.sched
}

func (c *Controller) IsRunning() bool { return c.running.Load() }

func (c *Controller) Uptime() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil {
		return 0
	}
	return time.Since(c.startedAt)
}

func (c *Controller) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Start launches DayZServer_x64.exe with the current manager config.
func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil {
		return errors.New("server already running")
	}

	exe := filepath.Join(c.serverDir, "DayZServer_x64.exe")
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("DayZServer_x64.exe not found in %s", c.serverDir)
	}

	args := c.buildArgs()
	cmd := exec.Command(exe, args...)
	cmd.Dir = c.serverDir

	logPath := filepath.Join(c.serverDir, ".dayz-manager", "server.stdout.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	out, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}

	c.log.Printf("starting: %s %s", exe, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start DayZServer: %w", err)
	}
	c.cmd = cmd
	c.startedAt = time.Now()
	c.running.Store(true)

	go c.wait(cmd, out)
	return nil
}

func (c *Controller) wait(cmd *exec.Cmd, out *os.File) {
	err := cmd.Wait()
	c.mu.Lock()
	c.cmd = nil
	c.running.Store(false)
	// Rolling 5-minute window. Three exits inside that window = crash loop.
	now := time.Now()
	c.recentExits = append(c.recentExits, now)
	cutoff := now.Add(-5 * time.Minute)
	keep := c.recentExits[:0]
	for _, t := range c.recentExits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	c.recentExits = keep
	if len(c.recentExits) >= 3 {
		c.loopPaused.Store(true)
		c.log.Printf("crash-loop detected: %d exits in 5m — auto-restart paused", len(c.recentExits))
	}
	c.mu.Unlock()
	if out != nil {
		_ = out.Close()
	}
	if err != nil {
		c.log.Printf("server exited: %v", err)
	} else {
		c.log.Printf("server exited cleanly")
	}
}

// LoopPaused reports whether auto-restart is suspended because of a crash loop.
func (c *Controller) LoopPaused() bool { return c.loopPaused.Load() }

// ClearLoopPause resets the crash-loop counter so auto-restart resumes.
func (c *Controller) ClearLoopPause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loopPaused.Store(false)
	c.recentExits = nil
}

// Stop terminates the process (forcefully on Windows via taskkill /F, the same
// way the reference .bat does).
func (c *Controller) Stop() error {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		pid := strconv.Itoa(cmd.Process.Pid)
		_ = exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
	} else {
		_ = cmd.Process.Kill()
	}
	return nil
}

func (c *Controller) Restart() error {
	_ = c.Stop()
	// wait for wait() to clear the cmd.
	for i := 0; i < 50; i++ {
		if !c.IsRunning() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The server is now stopped and its file locks on the @mod folders are
	// clear — the only safe window to refresh mods (item 2).
	c.beforeRestart()
	return c.Start()
}

// beforeRestart updates mods during the restart down-window when the user opted
// in (AutoUpdateModsOnRestart) or the update-check loop forced it once. Safe to
// call on every restart — it no-ops without the hook, a vanilla path, or a
// reason to update.
func (c *Controller) beforeRestart() {
	forced := c.forceModUpdate.Swap(false)
	s := c.schedSnapshot()
	if !forced && !s.autoUpdateOnRestart {
		return
	}
	if c.UpdateMods == nil || s.vanillaPath == "" {
		return
	}
	updated, err := c.UpdateMods(s.vanillaPath)
	if err != nil {
		c.log.Printf("auto-update mods: %v", err)
		return
	}
	if len(updated) > 0 {
		c.log.Printf("auto-update: refreshed %d mod(s): %s", len(updated), strings.Join(updated, ", "))
	}
}

// restartWithCountdown broadcasts "restart in Nm" via RCon for each entry in
// warnMinutes (largest first), sleeping between them, then restarts.
// If Broadcast is nil or any announce fails, it proceeds silently.
func (c *Controller) restartWithCountdown(warnMinutes []int) error {
	if c.Broadcast != nil && len(warnMinutes) > 0 {
		// sort descending
		m := append([]int(nil), warnMinutes...)
		for i := range m {
			for j := i + 1; j < len(m); j++ {
				if m[j] > m[i] {
					m[i], m[j] = m[j], m[i]
				}
			}
		}
		prev := 0
		// wait long enough BEFORE the first announcement so timing lines up
		if m[0] > 0 {
			announce := fmt.Sprintf("Server restart in %d minutes", m[0])
			_ = c.Broadcast.Say(announce)
			c.log.Printf("rcon broadcast: %s", announce)
		}
		for i, mins := range m {
			if i == 0 {
				prev = mins
				continue
			}
			gap := prev - mins
			if gap > 0 {
				time.Sleep(time.Duration(gap) * time.Minute)
			}
			announce := fmt.Sprintf("Server restart in %d minute(s)", mins)
			if c.Broadcast != nil {
				_ = c.Broadcast.Say(announce)
			}
			c.log.Printf("rcon broadcast: %s", announce)
			prev = mins
		}
		if prev > 0 {
			time.Sleep(time.Duration(prev) * time.Minute)
		}
	}
	return c.Restart()
}

// StartAutoRestartLoop starts a goroutine that restarts the server on the
// configured interval. Mirrors the `timeout 14390 && taskkill && goto start`
// behavior of the reference .bat.
func (c *Controller) StartAutoRestartLoop() {
	c.mu.Lock()
	if c.restartStop != nil {
		c.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	c.restartStop = stop
	c.mu.Unlock()

	// Scheduled cron-style restarts: check every minute whether any of the
	// cfg.ScheduledRestarts ("HH:MM") entries matches local time.
	go c.scheduledRestartLoop(stop)
	// Scheduled RCon announcements (rules reminders, discord links, events).
	go c.scheduledAnnounceLoop(stop)
	// Interval RCon announcements (every N minutes while running).
	go c.intervalAnnounceLoop(stop)
	// Periodic mod-update check → update + restart when a new version appears.
	go c.modUpdateCheckLoop(stop)

	// Interval-based auto-restart (the classic .bat behavior).
	go func() {
		lastStart := time.Now()
		for {
			s := c.schedSnapshot()
			if !s.autoRestartEnabled || s.autoRestartSeconds <= 0 || c.loopPaused.Load() {
				select {
				case <-stop:
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			elapsed := time.Since(lastStart)
			remaining := time.Duration(s.autoRestartSeconds)*time.Second - elapsed
			if remaining <= 0 {
				if c.IsRunning() {
					c.log.Printf("auto-restart: cycling server")
					_ = c.restartWithCountdown(s.restartWarnMinutes)
				}
				lastStart = time.Now()
				continue
			}
			// Subtract the countdown warning — restartWithCountdown sleeps
			// for it internally, so we shouldn't double-wait.
			warnTotal := maxWarnMinutes(s.restartWarnMinutes)
			wait := remaining - time.Duration(warnTotal)*time.Minute
			if wait < 100*time.Millisecond {
				wait = 100 * time.Millisecond
			}
			select {
			case <-stop:
				return
			case <-time.After(wait):
				if c.IsRunning() {
					c.log.Printf("auto-restart: cycling server")
					_ = c.restartWithCountdown(s.restartWarnMinutes)
				}
				lastStart = time.Now()
			}
		}
	}()
}

// scheduledAnnounceLoop fires once per minute and broadcasts any enabled
// announcement whose Time matches current HH:MM. A dedup map prevents
// firing the same announcement twice within its minute window in case the
// loop gets delayed and wakes up twice inside the same minute.
func (c *Controller) scheduledAnnounceLoop(stop <-chan struct{}) {
	fired := map[string]string{} // key → last yyyy-mm-ddTHH:MM we fired
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}
		if c.Broadcast == nil || !c.IsRunning() {
			continue
		}
		hhmm := time.Now().Format("15:04")
		stamp := time.Now().Format("2006-01-02T15:04")
		for i, a := range c.schedSnapshot().announcements {
			if !a.Enabled || strings.TrimSpace(a.Time) != hhmm {
				continue
			}
			key := fmt.Sprintf("%d:%s", i, a.Time)
			if fired[key] == stamp {
				continue
			}
			fired[key] = stamp
			msg := a.Message
			go func() {
				if err := c.Broadcast.Say(msg); err != nil {
					c.log.Printf("announce %q: %v", msg, err)
				}
			}()
		}
	}
}

// intervalAnnounceLoop broadcasts each enabled interval announcement every
// IntervalMinutes while the server is running. The per-item timer resets while
// the server is down, so "every 30 min" counts uptime, not wall-clock through a
// restart/outage.
func (c *Controller) intervalAnnounceLoop(stop <-chan struct{}) {
	lastFired := map[int]time.Time{} // snapshot index → last broadcast time
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}
		if c.Broadcast == nil || !c.IsRunning() {
			lastFired = map[int]time.Time{} // reset timers while down
			continue
		}
		now = time.Now()
		for i, a := range c.schedSnapshot().intervalAnnounces {
			if !a.Enabled || a.IntervalMinutes <= 0 || strings.TrimSpace(a.Message) == "" {
				continue
			}
			lf, seen := lastFired[i]
			if !seen {
				lastFired[i] = now // baseline: first fire one interval from now
				continue
			}
			if now.Sub(lf) >= time.Duration(a.IntervalMinutes)*time.Minute {
				lastFired[i] = now
				msg := a.Message
				go func() {
					if err := c.Broadcast.Say(msg); err != nil {
						c.log.Printf("interval announce %q: %v", msg, err)
					}
				}()
			}
		}
	}
}

// scheduledRestartLoop wakes at the start of every minute and begins a restart
// countdown so that the actual restart lands exactly on one of the configured
// HH:MM times. restartWithCountdown sleeps for the warning window internally,
// so we start it `maxWarn` minutes early — a "03:00" restart with warnings
// [5,3,1] announces at 02:55/02:57/02:59 and cycles the server at 03:00.
func (c *Controller) scheduledRestartLoop(stop <-chan struct{}) {
	fired := map[string]string{} // scheduled time → last yyyy-mm-ddTHH:MM fired
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}
		if !c.IsRunning() || c.loopPaused.Load() {
			continue
		}
		now = time.Now()
		hhmm := now.Format("15:04")
		stamp := now.Format("2006-01-02T15:04")
		s := c.schedSnapshot()
		warn := maxWarnMinutes(s.restartWarnMinutes)
		for _, t := range s.scheduledRestarts {
			sched := strings.TrimSpace(t)
			if sched == "" {
				continue
			}
			if shiftHHMM(sched, -warn) != hhmm {
				continue
			}
			if fired[sched] == stamp {
				continue // already fired this minute
			}
			fired[sched] = stamp
			warnMins := s.restartWarnMinutes
			c.log.Printf("scheduled restart %s — countdown starting", sched)
			go func() { _ = c.restartWithCountdown(warnMins) }()
			break
		}
	}
}

// modUpdateCheckLoop polls the client !Workshop on the configured cadence and,
// when an active mod is newer there than on the server, triggers an
// update+restart (item 2). The mods are actually copied during the restart
// down-window via beforeRestart (forced once here), so file locks are clear.
// Disabled when AutoUpdateCheckMinutes <= 0.
func (c *Controller) modUpdateCheckLoop(stop <-chan struct{}) {
	var lastCheck time.Time
	for {
		// Wake at each minute boundary; the cadence gate below decides whether
		// this tick actually performs a check.
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}

		s := c.schedSnapshot()
		if s.autoUpdateCheckMinutes <= 0 || s.vanillaPath == "" || c.ModUpdatesAvailable == nil {
			continue
		}
		if !c.IsRunning() || c.loopPaused.Load() {
			continue
		}
		if time.Since(lastCheck) < time.Duration(s.autoUpdateCheckMinutes)*time.Minute {
			continue
		}
		lastCheck = time.Now()

		avail, err := c.ModUpdatesAvailable(s.vanillaPath)
		if err != nil {
			c.log.Printf("mod update check: %v", err)
			continue
		}
		if !avail {
			continue
		}
		c.log.Printf("mod update available — updating and restarting")
		c.forceModUpdate.Store(true)
		go func() { _ = c.restartWithCountdown(s.restartWarnMinutes) }()
	}
}

// maxWarnMinutes returns the largest entry in the warning list (0 if empty).
func maxWarnMinutes(warn []int) int {
	m := 0
	for _, w := range warn {
		if w > m {
			m = w
		}
	}
	return m
}

// shiftHHMM parses an "HH:MM" string, shifts it by deltaMinutes (may be
// negative), and re-formats as "HH:MM", wrapping across midnight. Returns the
// input unchanged if it isn't a valid time.
func shiftHHMM(hhmm string, deltaMinutes int) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return t.Add(time.Duration(deltaMinutes) * time.Minute).Format("15:04")
}

func (c *Controller) StopAutoRestartLoop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restartStop != nil {
		close(c.restartStop)
		c.restartStop = nil
	}
}

// buildArgs mirrors the reference .bat launch line.
func (c *Controller) buildArgs() []string {
	args := []string{
		"-config=" + c.cfg.ServerCfg,
		"-port=" + strconv.Itoa(c.cfg.ServerPort),
		"-cpuCount=" + strconv.Itoa(c.cfg.CPUCount),
	}
	if c.cfg.DoLogs {
		args = append(args, "-dologs")
	}
	if c.cfg.AdminLog {
		args = append(args, "-adminlog")
	}
	if c.cfg.NetLog {
		args = append(args, "-netlog")
	}
	if c.cfg.FreezeCheck {
		args = append(args, "-freezecheck")
	}
	if c.cfg.FilePatching {
		args = append(args, "-filePatching")
	}
	if p := c.cfg.BEPath; p != "" {
		args = append(args, "-BEpath="+absOrRel(c.serverDir, p))
	}
	if p := c.cfg.ProfilesDir; p != "" {
		args = append(args, "-profiles="+absOrRel(c.serverDir, p))
	}
	if len(c.cfg.Mods) > 0 {
		args = append(args, "-mod="+strings.Join(c.cfg.Mods, ";"))
	}
	if len(c.cfg.ServerMods) > 0 {
		args = append(args, "-serverMod="+strings.Join(c.cfg.ServerMods, ";"))
	}
	return args
}

func absOrRel(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}
