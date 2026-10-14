// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package server

import (
	"os/exec"
	"strconv"
)

// setProcAttr: nothing to set on Windows — DayZServer_x64 runs in its own
// window exactly as the reference .bat starts it.
func setProcAttr(cmd *exec.Cmd) {}

// ensureExecutable is a no-op on Windows (no exec bit).
func ensureExecutable(path string) error { return nil }

// forceKill terminates the server and its child tree (BattlEye), the same way
// the reference .bat does with taskkill /T /F. Works for adopted processes too.
func forceKill(p *runningProc) {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(p.pid), "/T", "/F").Run()
}
