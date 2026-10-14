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
	"dayzmanager/internal/util"
)

// Broadcaster is implemented by rcon.Manager — kept as an interface here so
// the server package doesn't depend on rcon (which would cycle: rcon may
// later want config, and everyone wants server).
type Broadcaster interface {
	Say(msg string) error
}

// runningProc is the DayZ server process the controller supervises. cmd is nil
// when the process was adopted (started by an earlier manager session that has
// since exited) — it is then watched by PID instead of cmd.Wait.
type runningProc struct {
	pid       int
	cmd       *exec.Cmd
	startedAt time.Time
	adopted   bool
}

// Controller supervises the DayZServer process.
type Controller struct {
	serverDir string
	cfg       *config.Manager
	log       *log.Logger

	mu   sync.Mutex
	proc *runningProc

	running  atomic.Bool
	stopping atomic.Bool
	// runGen increments on every start/adopt. A countdown remembers the
	// generation it was armed for and aborts if it changes (the admin
	// restarted manually) — or if the server stops — instead of blindly
	// restarting a server someone just shut down.
	runGen atomic.Int64

	// Supervisor loops.
	restartStop  chan struct{}
	tickOverride time.Duration // tests only; set before StartAutoRestartLoop

	// Optional RCon broadcaster — set by app wiring. Used to announce
	// scheduled/auto restarts with a countdown.
	Broadcast Broadcaster

	// Optional RCon hooks — set by app wiring. GracefulShutdown asks the server
	// to save and exit (#shutdown); LockServer locks/unlocks joining (#lock);
	// KickAll disconnects everyone online and returns how many were kicked.
	GracefulShutdown func() error
	LockServer       func(lock bool) error
	KickAll          func(reason string) (int, error)

	// Optional config-profile apply hook — set by web wiring. Restores a saved
	// profile's files; called during the restart down-window (server stopped) so
	// the config write is safe. Nil = scheduled profile swaps are unavailable.
	ApplyProfile func(slug string) error
	pendingMu    sync.Mutex
	pendingProf  string // profile to apply on the next restart, set by the schedule

	// Optional write lock — set by app wiring to app.WriteMu. Held around every
	// start the manager makes on its own (watchdog, restart, retry) together with
	// the mod-update/profile step before it, so DayZ never boots against files a
	// web request is still writing and those steps never overlap such a write.
	// Never held across Stop's graceful wait. Nil = no exclusion (tests).
	WriteLock sync.Locker

	// Optional online-player-count hook — set by app wiring (queries RCon).
	// Returns the number online, or -1 when it cannot be determined (server
	// down / RCon not connected). Nil = the smart "restart only when empty"
	// feature is unavailable and restarts proceed normally.
	PlayerCount func() int

	// Optional pre-start check — set by the multi-server wiring: refuses a
	// start whose ports another running server of the panel already uses.
	// Runs before every start the manager makes (panel, schedule, watchdog).
	PreStart func() error

	// Guards against overlapping restart flows: a countdown (or a deferred
	// wait-for-empty) must not run alongside a second trigger.
	restarting atomic.Bool
	// restartMu serializes Restart() itself. A manual restart can arrive while
	// a scheduled one is already cycling the server (two tabs, a double click);
	// without it both reached Start() and the loser reported a false
	// "RESTART FAILED". restartErr is the outcome a waiting caller shares.
	restartMu  sync.Mutex
	restartErr error
	// plan is the restart currently counting down (nil when none) — the exact
	// moment the dashboard shows, so it can never disagree with what players
	// were told.
	plan atomic.Pointer[RestartPlan]

	// Optional lifecycle-event sink — set by app wiring (Discord webhooks).
	// Called with a short event key ("started", "stopped", "restart",
	// "crashed", "crashloop", "restartfailed", "modupdate", …) and a detail
	// (a reason, mod names, an error). Implementations must not block.
	Notify func(event, detail string)

	// cycling is set while Restart() cycles the server: its stop is part of
	// the restart and is not reported as "stopped". restartReason is what the
	// next Restart() reports (set by the countdown; empty = a manual restart).
	cycling       atomic.Bool
	restartReason atomic.Value // string

	// stopRequested distinguishes a manager-initiated Stop/Restart from a
	// crash in onExit(): only unrequested exits count as crashes for the
	// watchdog and notifications.
	stopRequested atomic.Bool

	// Mod auto-update hooks — set by app wiring so the server package doesn't
	// import mods. Both take the resolved mod source directory. Nil = the
	// feature is unavailable (no-op). UpdateMods copies newer mod files into the
	// server dir (must run while stopped — file locks); ModUpdatesAvailable
	// reports whether any active mod is newer in the source than on the server.
	UpdateMods          func(sourceDir string) ([]string, error)
	ModUpdatesAvailable func(sourceDir string) (bool, error)

	// One-shot: forces a mod update on the next restart even when
	// AutoUpdateModsOnRestart is off (set by the update-check loop, which
	// restarts precisely because it found a pending update).
	forceModUpdate atomic.Bool

	// Crash-loop protection — track recent exits to detect a broken mod set.
	recentExits []time.Time
	loopPaused  atomic.Bool

	// Last process-tuning (affinity/priority) outcome, for the Settings page.
	tuneMu  sync.Mutex
	tuneErr string

	// Schedule snapshot. The supervisor loops run for the whole process
	// lifetime and read these every tick, while the web layer replaces the
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
	restartWarnMessage string
	scheduledRestarts  []string
	announcements      []config.ScheduledAnnouncement
	intervalAnnounces  []config.IntervalAnnouncement
	scheduledProfiles  []config.ScheduledProfile

	// Kick-before-restart window.
	restartKickSeconds int
	restartLock        bool
	restartKickMessage string
	gracefulStopSec    int

	// Mod auto-update.
	autoUpdateOnRestart    bool
	autoUpdateCheckMinutes int
	modSourceDir           string

	// Crash watchdog.
	restartOnCrash bool

	// Smart restarts.
	restartOnlyWhenEmpty bool
	restartEmptyMaxWait  int
	restartOnHighMem     bool
	restartMemThreshMB   int

	// Process tuning.
	cpuAffinity     []int
	processPriority string

	// Launch parameters — buildArgs reads these instead of the live shared
	// *config.Manager. Start() runs from scheduler goroutines concurrently
	// with web config POSTs (App.MutateConfig replaces *Config wholesale), so
	// reading c.cfg there was the same class of data race the snapshot was
	// introduced to kill; a restart mid-save could even launch DayZ with
	// half-old/half-new args.
	serverCfg    string
	serverPort   int
	cpuCount     int
	doLogs       bool
	adminLog     bool
	netLog       bool
	freezeCheck  bool
	filePatching bool
	bePath       string
	profilesDir  string
	mods         []string
	serverMods   []string
	limitFPS     int
	extraParams  string
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
		restartWarnMessage:     cfg.RestartWarnMessage,
		scheduledRestarts:      append([]string(nil), cfg.ScheduledRestarts...),
		announcements:          append([]config.ScheduledAnnouncement(nil), cfg.ScheduledAnnouncements...),
		intervalAnnounces:      append([]config.IntervalAnnouncement(nil), cfg.IntervalAnnouncements...),
		scheduledProfiles:      append([]config.ScheduledProfile(nil), cfg.ScheduledProfiles...),
		restartKickSeconds:     cfg.RestartKickSeconds,
		restartLock:            cfg.RestartLock,
		restartKickMessage:     cfg.RestartKickMessage,
		gracefulStopSec:        cfg.GracefulStopSeconds,
		autoUpdateOnRestart:    cfg.AutoUpdateModsOnRestart,
		autoUpdateCheckMinutes: cfg.AutoUpdateCheckMinutes,
		modSourceDir:           cfg.ModSourceDir(),
		restartOnCrash:         cfg.RestartOnCrash,

		restartOnlyWhenEmpty: cfg.RestartOnlyWhenEmpty,
		restartEmptyMaxWait:  cfg.RestartEmptyMaxWaitMinutes,
		restartOnHighMem:     cfg.RestartOnHighMem,
		restartMemThreshMB:   cfg.RestartMemThresholdMB,

		cpuAffinity:     append([]int(nil), cfg.CPUAffinity...),
		processPriority: cfg.ProcessPriority,

		serverCfg:    cfg.ServerCfg,
		serverPort:   cfg.ServerPort,
		cpuCount:     cfg.CPUCount,
		doLogs:       cfg.DoLogs,
		adminLog:     cfg.AdminLog,
		netLog:       cfg.NetLog,
		freezeCheck:  cfg.FreezeCheck,
		filePatching: cfg.FilePatching,
		bePath:       cfg.BEPath,
		profilesDir:  cfg.ProfilesDir,
		mods:         append([]string(nil), cfg.Mods...),
		serverMods:   append([]string(nil), cfg.ServerMods...),
		limitFPS:     cfg.LimitFPS,
		extraParams:  cfg.ExtraLaunchParams,
	}
}

func (c *Controller) schedSnapshot() scheduleConfig {
	c.schedMu.Lock()
	defer c.schedMu.Unlock()
	return c.sched
}

func (c *Controller) IsRunning() bool { return c.running.Load() }

// IsStopping reports whether a stop is in progress (graceful shutdown pending).
func (c *Controller) IsStopping() bool { return c.stopping.Load() }

func (c *Controller) current() *runningProc {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.proc
}

// StartedAt reports when the current server process started, or the zero time
// when it is not running. The auto-restart interval is measured from here so
// it agrees with the dashboard countdown, which is uptime-based.
func (c *Controller) StartedAt() time.Time {
	if p := c.current(); p != nil {
		return p.startedAt
	}
	return time.Time{}
}

func (c *Controller) Uptime() time.Duration {
	if p := c.current(); p != nil {
		return time.Since(p.startedAt)
	}
	return 0
}

func (c *Controller) PID() int {
	if p := c.current(); p != nil {
		return p.pid
	}
	return 0
}

// Adopted reports whether the running server was picked up from an earlier
// manager session rather than started by this one.
func (c *Controller) Adopted() bool {
	p := c.current()
	return p != nil && p.adopted
}

// serverExeCandidates are the DayZ dedicated-server binary names to look for,
// in order. Windows ships DayZServer_x64.exe; the Linux server binary is
// DayZServer (an ELF). On non-Windows we try both so an oddly-named install
// still starts.
func serverExeCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{"DayZServer_x64.exe"}
	}
	return []string{"DayZServer", "DayZServer_x64"}
}

// FindServerExe returns the first server-binary candidate that exists in dir.
func FindServerExe(dir string) (string, bool) {
	for _, name := range serverExeCandidates() {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// Start launches the DayZ dedicated server with the current manager config.
func (c *Controller) Start() error {
	// Before c.mu: the check reads other servers' settings, and nothing may
	// wait on another lock while holding this one.
	if c.PreStart != nil && !c.running.Load() {
		if err := c.PreStart(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil {
		return errors.New("server already running")
	}

	exe, ok := FindServerExe(c.serverDir)
	if !ok {
		return fmt.Errorf("DayZ server binary (%s) not found in %s",
			strings.Join(serverExeCandidates(), " or "), c.serverDir)
	}
	// A still-running server from an earlier session (or started by hand)
	// would make this second copy fail on the port — pick it up instead.
	if others := util.FindProcessesByExe(exe); len(others) > 0 {
		c.adoptLocked(others[0])
		return nil
	}
	if err := ensureExecutable(exe); err != nil {
		return err
	}

	args := c.buildArgs()
	cmd := exec.Command(exe, args...)
	cmd.Dir = c.serverDir
	setProcAttr(cmd)

	logPath := filepath.Join(c.serverDir, ".dayz-manager", "server.stdout.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	out, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}

	c.log.Printf("starting: %s %s", exe, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		if out != nil {
			_ = out.Close()
		}
		return fmt.Errorf("start DayZServer: %w", err)
	}
	p := &runningProc{pid: cmd.Process.Pid, cmd: cmd, startedAt: time.Now()}
	c.proc = p
	c.runGen.Add(1)
	c.running.Store(true)

	go util.SafeGo(c.log, "process watcher", func() { c.waitCmd(p, out) })
	go util.SafeGo(c.log, "process tuning", func() { _ = c.applyTuning(p.pid) })
	c.notify("started", "")
	return nil
}

// AdoptRunning looks for a DayZ server already running from this server
// directory — left behind when the manager was closed, crashed, or was updated
// — and supervises it as if this session had started it. Without this the
// panel showed "stopped" for a live server and Start then failed on the port.
func (c *Controller) AdoptRunning() bool {
	exe, ok := FindServerExe(c.serverDir)
	if !ok {
		return false
	}
	procs := util.FindProcessesByExe(exe)
	if len(procs) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil {
		return false
	}
	c.adoptLocked(procs[0])
	return true
}

func (c *Controller) adoptLocked(pi util.ProcInfo) {
	started := pi.Started
	if started.IsZero() {
		started = time.Now()
	}
	p := &runningProc{pid: pi.PID, startedAt: started, adopted: true}
	c.proc = p
	c.runGen.Add(1)
	c.running.Store(true)
	c.log.Printf("adopted running DayZ server (pid %d, started %s)", pi.PID, started.Format(time.RFC3339))
	go util.SafeGo(c.log, "adopted process watcher", func() { c.waitAdopted(p) })
}

// notify fires the lifecycle sink if wired. Never blocks the caller.
func (c *Controller) notify(event, detail string) {
	if c.Notify != nil {
		c.Notify(event, detail)
	}
}

func (c *Controller) waitCmd(p *runningProc, out *os.File) {
	err := p.cmd.Wait()
	if out != nil {
		_ = out.Close()
	}
	c.onExit(p, err)
}

func (c *Controller) waitAdopted(p *runningProc) {
	code, err := util.WaitPID(p.pid)
	if err == nil && code > 0 {
		err = fmt.Errorf("exit status %d", code)
	}
	c.onExit(p, err)
}

// onExit is the shared bookkeeping for a supervised process ending, whether it
// was our child or adopted: crash-loop accounting, notifications, watchdog.
func (c *Controller) onExit(p *runningProc, err error) {
	c.mu.Lock()
	if c.proc != p {
		c.mu.Unlock()
		return // a stale watcher for a process we already let go of
	}
	c.proc = nil
	c.running.Store(false)
	requested := c.stopRequested.Swap(false)
	// Rolling 5-minute window. Three crashes inside that window = crash loop.
	// Only unrequested exits count — an admin restarting three times in five
	// minutes is not a broken mod set.
	now := time.Now()
	if !requested {
		c.recentExits = append(c.recentExits, now)
	}
	cutoff := now.Add(-5 * time.Minute)
	keep := c.recentExits[:0]
	for _, t := range c.recentExits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	c.recentExits = keep
	loopJustPaused := false
	if !requested && len(c.recentExits) >= 3 && !c.loopPaused.Load() {
		c.loopPaused.Store(true)
		loopJustPaused = true
		c.log.Printf("crash-loop detected: %d exits in 5m — auto-restart paused", len(c.recentExits))
	}
	c.mu.Unlock()
	if err != nil {
		c.log.Printf("server exited: %v", err)
	} else {
		c.log.Printf("server exited cleanly")
	}
	switch {
	case loopJustPaused:
		c.notify("crashloop", "")
	case requested:
		// A stop inside a restart is reported as the restart itself.
		if !c.cycling.Load() {
			c.notify("stopped", "")
		}
	default:
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		if c.schedSnapshot().restartOnCrash {
			detail += "\x1fwatchdog"
		}
		c.notify("crashed", detail)
	}
	// Crash watchdog: bring an unrequested-exit server back up after a short
	// grace period (lets BattlEye/locks settle). Crash-loop pause wins.
	if !requested && !c.loopPaused.Load() && c.schedSnapshot().restartOnCrash {
		c.log.Printf("watchdog: server exited unexpectedly — restarting in 10s")
		go func() {
			time.Sleep(10 * time.Second)
			unlock := c.lockWrites()
			defer unlock()
			// Re-checked under the lock: a write may have held it for minutes,
			// and the admin may have started the server meanwhile.
			if c.IsRunning() || c.loopPaused.Load() || !c.schedSnapshot().restartOnCrash {
				return
			}
			c.beforeRestart()
			if err := c.Start(); err != nil {
				c.log.Printf("watchdog restart failed: %v", err)
			}
		}()
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

// waitStopped polls until the server is down or d elapses; true = it stopped.
func (c *Controller) waitStopped(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !c.IsRunning() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !c.IsRunning()
}

// Stop shuts the server down. When RCon is available and GracefulStopSeconds
// > 0 it first asks the server to exit cleanly (#shutdown saves the world and
// every player), and force-kills only if that doesn't happen in time — the old
// immediate taskkill /F threw away up to a save interval of progress.
func (c *Controller) Stop() error {
	p := c.current()
	if p == nil {
		return nil
	}
	if !c.stopping.CompareAndSwap(false, true) {
		// A stop is already in flight; just wait for it.
		c.waitStopped(time.Duration(c.schedSnapshot().gracefulStopSec+15) * time.Second)
		return nil
	}
	defer c.stopping.Store(false)
	c.stopRequested.Store(true)

	s := c.schedSnapshot()
	if s.gracefulStopSec > 0 && c.GracefulShutdown != nil {
		if err := c.GracefulShutdown(); err == nil {
			c.log.Printf("graceful shutdown requested — waiting up to %ds for the server to save and exit", s.gracefulStopSec)
			if c.waitStopped(time.Duration(s.gracefulStopSec) * time.Second) {
				return nil
			}
			c.log.Printf("graceful shutdown timed out — forcing the server down")
		} else {
			c.log.Printf("graceful shutdown unavailable (%v) — forcing the server down", err)
		}
	}
	forceKill(p)
	c.waitStopped(15 * time.Second)
	return nil
}

// lockWrites takes WriteLock when wired and returns its release.
func (c *Controller) lockWrites() func() {
	if c.WriteLock == nil {
		return func() {}
	}
	c.WriteLock.Lock()
	return c.WriteLock.Unlock
}

func (c *Controller) Restart() error {
	if !c.restartMu.TryLock() {
		// Another restart is already cycling the server: wait for it and share
		// its outcome instead of stopping the freshly started server again.
		// (Its reason was taken when it began; one staged now is not ours.)
		c.restartReason.Store("")
		c.restartMu.Lock()
		err := c.restartErr
		c.restartMu.Unlock()
		return err
	}
	defer c.restartMu.Unlock()
	reason, _ := c.restartReason.Swap("").(string)
	if reason == "" {
		reason = "manual"
	}
	if c.IsRunning() {
		c.notify("restart", reason)
	}
	c.cycling.Store(true)
	defer c.cycling.Store(false)
	c.restartErr = c.restart()
	return c.restartErr
}

func (c *Controller) restart() error {
	// Remembered BEFORE Stop: a run that starts any time after this point (the
	// admin pressed Start during the graceful wait, the 30s wait below, or while
	// we waited for a web write) is a new run, i.e. what this restart wanted.
	gen := c.runGen.Load()
	_ = c.Stop()
	// Wait for onExit() to clear the process. 30s: BattlEye and the Windows
	// file cache can hold the process tree well past five seconds, and giving
	// up early makes Start() fail with "server already running".
	c.waitStopped(30 * time.Second)

	unlock := c.lockWrites()
	defer unlock()
	if c.IsRunning() && c.runGen.Load() != gen {
		return nil
	}
	// The server is now stopped and its file locks on the @mod folders are
	// clear — the only safe window to refresh mods or swap a profile. If the
	// old process refused to die, skip that step: copying mods or restoring
	// config files under a live DayZServer is exactly what it must never do
	// (Start below then reports the failure).
	if !c.IsRunning() {
		c.beforeRestart()
	}

	if err := c.Start(); err != nil {
		// The server is DOWN and the watchdog will not help: Stop() marked this
		// exit as requested. Say so loudly and keep trying, because nothing
		// else will.
		c.log.Printf("RESTART FAILED — the server is down and will not come back on its own: %v", err)
		c.notify("restartfailed", err.Error())
		go util.SafeGo(c.log, "start retry", c.retryStart)
		return err
	}
	return nil
}

// retryStart re-attempts a start after a failed restart. Bounded: five tries
// over ~75 seconds. A transient lock (antivirus scan, Steam update finishing)
// clears in that window; anything longer needs a human, and hammering the exe
// forever would only bury the log line that says so.
func (c *Controller) retryStart() {
	for i := 0; i < 5; i++ {
		time.Sleep(15 * time.Second)
		unlock := c.lockWrites()
		if c.IsRunning() || c.loopPaused.Load() {
			unlock()
			return
		}
		err := c.Start()
		unlock()
		if err == nil {
			c.log.Printf("server recovered on retry %d after a failed restart", i+1)
			return
		}
	}
	c.log.Printf("server did not recover after a failed restart — manual action needed")
}

// beforeRestart updates mods during the restart down-window when the user opted
// in (AutoUpdateModsOnRestart) or the update-check loop forced it once. Safe to
// call on every restart — it no-ops without the hook, a mod source, or a
// reason to update.
func (c *Controller) beforeRestart() {
	// Apply a scheduled config-profile swap while the server is down (the only
	// safe time to overwrite mission/config files).
	c.pendingMu.Lock()
	prof := c.pendingProf
	c.pendingProf = ""
	c.pendingMu.Unlock()
	if prof != "" && c.ApplyProfile != nil {
		if err := c.ApplyProfile(prof); err != nil {
			c.log.Printf("scheduled profile %q: %v", prof, err)
		} else {
			c.log.Printf("scheduled profile applied: %s", prof)
		}
	}

	forced := c.forceModUpdate.Swap(false)
	s := c.schedSnapshot()
	if !forced && !s.autoUpdateOnRestart {
		return
	}
	if c.UpdateMods == nil || s.modSourceDir == "" {
		return
	}
	updated, err := c.UpdateMods(s.modSourceDir)
	if err != nil {
		c.log.Printf("auto-update mods: %v", err)
		return
	}
	if len(updated) > 0 {
		c.log.Printf("auto-update: refreshed %d mod(s): %s", len(updated), strings.Join(updated, ", "))
		c.notify("modupdate", strings.Join(updated, ", "))
	}
}

// ApplyTuning re-applies the configured CPU affinity and priority to the
// running server (Settings save). Returns the error, if any, for the UI.
func (c *Controller) ApplyTuning() error {
	pid := c.PID()
	if pid <= 0 {
		return nil
	}
	return c.applyTuning(pid)
}

// applyTuning pins the server to the configured cores and sets its priority.
// Runs right after start; failures are logged (and surfaced on Settings) but
// never stop the server — a mis-set affinity must not take a server down.
func (c *Controller) applyTuning(pid int) error {
	s := c.schedSnapshot()
	var errs []string
	if len(s.cpuAffinity) > 0 {
		if err := util.SetAffinity(pid, s.cpuAffinity); err != nil {
			errs = append(errs, "affinity: "+err.Error())
		} else {
			c.log.Printf("CPU affinity set to cores %v", s.cpuAffinity)
		}
	}
	if s.processPriority != "" {
		if err := util.SetPriority(pid, s.processPriority); err != nil {
			errs = append(errs, "priority: "+err.Error())
		} else {
			c.log.Printf("process priority set to %s", s.processPriority)
		}
	}
	c.tuneMu.Lock()
	c.tuneErr = strings.Join(errs, "; ")
	c.tuneMu.Unlock()
	if len(errs) > 0 {
		c.log.Printf("process tuning: %s", strings.Join(errs, "; "))
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Tuning reports the running server's actual affinity and priority, plus the
// last tuning error. Empty values when the server is down.
func (c *Controller) Tuning() (cores []int, priority string, lastErr string) {
	c.tuneMu.Lock()
	lastErr = c.tuneErr
	c.tuneMu.Unlock()
	pid := c.PID()
	if pid <= 0 {
		return nil, "", lastErr
	}
	cores, _ = util.ProcessAffinity(pid)
	return cores, util.ProcessPriority(pid), lastErr
}

// buildArgs mirrors the reference .bat launch line. It reads the race-safe
// snapshot, never the live shared config (see scheduleConfig's launch fields).
func (c *Controller) buildArgs() []string {
	s := c.schedSnapshot()
	args := []string{
		"-config=" + s.serverCfg,
		"-port=" + strconv.Itoa(s.serverPort),
		"-cpuCount=" + strconv.Itoa(s.cpuCount),
	}
	if s.doLogs {
		args = append(args, "-dologs")
	}
	if s.adminLog {
		args = append(args, "-adminlog")
	}
	if s.netLog {
		args = append(args, "-netlog")
	}
	if s.freezeCheck {
		args = append(args, "-freezecheck")
	}
	if s.filePatching {
		args = append(args, "-filePatching")
	}
	if p := s.bePath; p != "" {
		args = append(args, "-BEpath="+absOrRel(c.serverDir, p))
	}
	if p := s.profilesDir; p != "" {
		args = append(args, "-profiles="+absOrRel(c.serverDir, p))
	}
	if len(s.mods) > 0 {
		args = append(args, "-mod="+strings.Join(s.mods, ";"))
	}
	if len(s.serverMods) > 0 {
		args = append(args, "-serverMod="+strings.Join(s.serverMods, ";"))
	}
	if s.limitFPS > 0 {
		args = append(args, "-limitFPS="+strconv.Itoa(s.limitFPS))
	}
	args = append(args, SplitParams(s.extraParams)...)
	return args
}

// SplitParams splits a user-typed parameter line into arguments: whitespace
// separates, double quotes group (`-profiles="C:\My Profiles"` stays one
// argument, quotes removed). No other escaping — Windows paths keep their
// backslashes.
func SplitParams(line string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote bool
		has   bool
	)
	for _, r := range line {
		switch {
		case r == '"':
			quote = !quote
			has = true
		case !quote && (r == ' ' || r == '\t' || r == '\r' || r == '\n'):
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// absOrRel resolves a configured path against the server dir. Separators are
// normalised first so a config written on Windows ("profiles\\x") still works
// when the manager moves to Linux.
func absOrRel(base, p string) string {
	p = filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}
