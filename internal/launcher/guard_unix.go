// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

// detach: a session of its own, so the terminal closing (SIGHUP) or Ctrl+C
// in it does not reach the guardian.
func detach(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
