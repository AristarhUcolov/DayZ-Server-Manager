// Copyright (c) 2026 Aristarh Ucolov.
//go:build linux

package util

import "syscall"

// DiskFree reports the free bytes available to an unprivileged user on the
// filesystem that contains path — the Linux counterpart of the Windows
// GetDiskFreeSpaceExW call, so the Server-health disk gate works there too.
func DiskFree(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	// Bavail = blocks free for a non-root user; Bsize = fundamental block size.
	return st.Bavail * uint64(st.Bsize), nil
}
