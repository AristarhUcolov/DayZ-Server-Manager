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

	go func() {
		for {
			if !c.cfg.AutoRestartEnabled || c.cfg.AutoRestartSeconds <= 0 {
				select {
				case <-stop:
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(c.cfg.AutoRestartSeconds) * time.Second):
				if c.IsRunning() {
					c.log.Printf("auto-restart: cycling server")
					_ = c.Restart()
				}
			}
		}
	}()
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
