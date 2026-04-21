// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

import "errors"

// ProcessStats is a no-op on non-Windows. The manager ships for Windows;
// this stub keeps `go build` green on dev machines.
func ProcessStats(pid uint32) (float64, uint64, error) {
	return 0, 0, errors.New("process stats: unsupported on this OS")
}
