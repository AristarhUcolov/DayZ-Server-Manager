// Copyright (c) 2026 Aristarh Ucolov.
package util

import (
	"errors"
	"os"
	"runtime"
)

// ErrNoDesktop means there is no graphical session to open a file manager in
// (a headless Linux host, a service) — the caller shows the path instead.
var ErrNoDesktop = errors.New("no desktop session to open a file manager in")

// CanReveal reports whether this machine has a desktop the file manager can
// open on.
func CanReveal() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
