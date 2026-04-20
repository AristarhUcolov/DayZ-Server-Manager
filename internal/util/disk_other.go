// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

// DiskFree is a stub on non-Windows platforms — the DayZ server is Windows-only
// so the disk-space gate is only meaningful there. Return (0, nil) to signal
// "unknown"; callers treat an unknown result as "do not block".
func DiskFree(path string) (uint64, error) { return 0, nil }
