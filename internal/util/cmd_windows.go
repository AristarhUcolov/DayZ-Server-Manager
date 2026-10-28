// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"os/exec"
	"syscall"
)

// HiddenCommand is exec.Command for helper tools (netsh, powershell) that
// must not flash a console window over the desktop panel.
func HiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// HideWindow keeps a command (made with exec.CommandContext, say) from
// opening a console window.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
