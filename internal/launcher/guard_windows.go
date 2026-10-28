// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
	createNoWindow         = 0x08000000
)

// detach: a hidden console of its own (closing the manager's console window
// does not reach it), its own process group, and — when allowed — out of the
// manager's job, so ending the manager's process tree spares it.
func detach(cmd *exec.Cmd, breakaway bool) {
	flags := uint32(createNewProcessGroup | createNoWindow)
	if breakaway {
		flags |= createBreakawayFromJob
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
}
