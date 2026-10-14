// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows && !linux

package desktop

import (
	"errors"

	"dayzmanager/internal/launcher"
)

var errNoWebKit = errors.New("no built-in app window on this system")

// runWebKit: only the Linux build carries a WebKitGTK window; elsewhere the
// panel uses an app-mode browser window.
func runWebKit(inst *launcher.Instance, url string) error { return errNoWebKit }
