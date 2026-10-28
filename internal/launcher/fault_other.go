// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import "time"

// windowsFault: only Windows keeps a crash record of its own.
func windowsFault(int, time.Time) string { return "" }
