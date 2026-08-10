// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows && !linux

package util

import "errors"

// ProcessStats is a no-op on platforms other than Windows and Linux (e.g. a
// macOS dev box). Windows and Linux have real implementations.
func ProcessStats(pid uint32) (float64, uint64, error) {
	return 0, 0, errors.New("process stats: unsupported on this OS")
}
