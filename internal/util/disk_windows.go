// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"syscall"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetDFSEx        = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// DiskFree reports the number of free bytes available on the volume that
// contains path. Returns (0, err) on failure so callers can warn but still
// proceed if they choose.
func DiskFree(path string) (uint64, error) {
	utf, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, totalFree uint64
	r, _, callErr := procGetDFSEx.Call(
		uintptr(unsafe.Pointer(utf)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, callErr
	}
	return freeToCaller, nil
}
