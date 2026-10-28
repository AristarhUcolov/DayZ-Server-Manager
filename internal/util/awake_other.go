// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

import (
	"os/exec"
	"sync"
)

var awake struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// SetKeepAwake stops (true) or allows again (false) the computer from going
// to sleep while idle. On Linux it holds a systemd sleep inhibitor (a
// `systemd-inhibit` child) when systemd is there; elsewhere it does nothing.
func SetKeepAwake(on bool) {
	awake.mu.Lock()
	defer awake.mu.Unlock()
	if on == (awake.cmd != nil) {
		return
	}
	if !on {
		_ = awake.cmd.Process.Kill()
		_ = awake.cmd.Wait()
		awake.cmd = nil
		return
	}
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return
	}
	cmd := exec.Command(path, "--what=sleep:idle", "--who=DayZ Server Manager",
		"--why=A DayZ server is running", "--mode=block", "sleep", "infinity")
	if cmd.Start() == nil {
		awake.cmd = cmd
	}
}
