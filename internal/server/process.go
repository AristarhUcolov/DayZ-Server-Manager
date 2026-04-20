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

	// Crash-loop protection — track recent exits to detect a broken mod set.
	recentExits []time.Time
	loopPaused  atomic.Bool
}

func NewController(serverDir string, cfg *config.Manager, logger *log.Logger) *Controller {
	return &Controller{serverDir: serverDir, cfg: cfg, log: logger}
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
	return c.Start()
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

	// Interval-based auto-restart (the classic .bat behavior).
	go func() {
		lastStart := time.Now()
		for {
			if !c.cfg.AutoRestartEnabled || c.cfg.AutoRestartSeconds <= 0 || c.loopPaused.Load() {
				select {
				case <-stop:
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			elapsed := time.Since(lastStart)
			remaining := time.Duration(c.cfg.AutoRestartSeconds)*time.Second - elapsed
			if remaining <= 0 {
				if c.IsRunning() {
					c.log.Printf("auto-restart: cycling server")
					_ = c.restartWithCountdown(c.cfg.RestartWarnMinutes)
				}
				lastStart = time.Now()
				continue
			}
			// Subtract the countdown warning — restartWithCountdown sleeps
			// for it internally, so we shouldn't double-wait.
			warnTotal := 0
			for _, m := range c.cfg.RestartWarnMinutes {
				if m > warnTotal {
					warnTotal = m
				}
			}
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
					_ = c.restartWithCountdown(c.cfg.RestartWarnMinutes)
				}
				lastStart = time.Now()
			}
		}
	}()
}

// scheduledRestartLoop wakes at the start of every minute and fires a
// restart when the current HH:MM matches one of cfg.ScheduledRestarts.
func (c *Controller) scheduledRestartLoop(stop <-chan struct{}) {
	// Align roughly to the minute boundary.
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
		hhmm := time.Now().Format("15:04")
		for _, t := range c.cfg.ScheduledRestarts {
			if strings.TrimSpace(t) == hhmm {
				c.log.Printf("scheduled restart at %s", hhmm)
				go func() { _ = c.restartWithCountdown(c.cfg.RestartWarnMinutes) }()
				break
			}
		}
	}
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
