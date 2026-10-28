// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

import "os/exec"

// HiddenCommand is exec.Command (console windows are a Windows concern).
func HiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// HideWindow does nothing here (console windows are a Windows concern).
func HideWindow(cmd *exec.Cmd) {}
