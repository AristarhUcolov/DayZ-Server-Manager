// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	processVMRead                  = 0x0010
)

var (
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procGetProcessTimes      = kernel32.NewProc("GetProcessTimes")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	psapi                    = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters maps to PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type cpuSample struct {
	kernel uint64
	user   uint64
	wall   time.Time
}

var (
	cpuSampleMu sync.Mutex
	cpuSamples  = map[uint32]cpuSample{}
)

// ProcessStats samples CPU% and working-set memory for the given PID.
// cpuPercent is normalized across all logical cores (0..100 is one core fully
// busy on an N-core box); multiply by core count to get per-process total.
// First call for a PID returns cpuPercent=0 (no baseline yet).
func ProcessStats(pid uint32) (cpuPercent float64, memBytes uint64, err error) {
	h, _, callErr := procOpenProcess.Call(
		uintptr(processQueryLimitedInformation|processVMRead),
		0,
		uintptr(pid),
	)
	if h == 0 {
		return 0, 0, callErr
	}
	handle := syscall.Handle(h)
	defer procCloseHandle.Call(uintptr(handle))

	var creation, exit, kernel, user syscall.Filetime
	r, _, e := procGetProcessTimes.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return 0, 0, e
	}
	var mem processMemoryCounters
	mem.CB = uint32(unsafe.Sizeof(mem))
	r2, _, e2 := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.CB),
	)
	if r2 == 0 {
		return 0, 0, e2
	}
	memBytes = uint64(mem.WorkingSetSize)

	k := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	u := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	now := time.Now()

	cpuSampleMu.Lock()
	prev, ok := cpuSamples[pid]
	cpuSamples[pid] = cpuSample{kernel: k, user: u, wall: now}
	cpuSampleMu.Unlock()

	if ok && now.After(prev.wall) {
		// FILETIME is in 100-nanosecond intervals.
		cpuNs := float64((k+u)-(prev.kernel+prev.user)) * 100.0
		wallNs := float64(now.Sub(prev.wall).Nanoseconds())
		if wallNs > 0 {
			// Raw percent of one CPU core (can exceed 100% on multi-core).
			cpuPercent = (cpuNs / wallNs) * 100.0
		}
	}
	return cpuPercent, memBytes, nil
}
