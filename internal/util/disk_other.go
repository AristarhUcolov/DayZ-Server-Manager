// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows && !linux

package util

// DiskFree is a stub on platforms other than Windows and Linux (e.g. a macOS
// dev box) — the DayZ dedicated server only runs on those two. Return (0, nil)
// to signal "unknown"; callers treat an unknown result as "do not block".
func DiskFree(path string) (uint64, error) { return 0, nil }
