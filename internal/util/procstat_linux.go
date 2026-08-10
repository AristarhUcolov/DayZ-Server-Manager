// Copyright (c) 2026 Aristarh Ucolov.
//go:build linux

package util

import (
	"os"
	"strconv"
	"sync"
	"time"
)

// linuxUserHZ is the kernel's clock-tick rate (_SC_CLK_TCK). It is effectively
// always 100 on Linux, so utime/stime in /proc/<pid>/stat are hundredths of a
// second — good enough for a CPU-usage gauge.
const linuxUserHZ = 100

type cpuSampleLinux struct {
	ticks uint64 // utime + stime, in clock ticks
	wall  time.Time
}

var (
	cpuSampleMuLinux sync.Mutex
	cpuSamplesLinux  = map[uint32]cpuSampleLinux{}
)

// ProcessStats samples CPU% and resident memory for pid on Linux, matching the
// Windows implementation's contract: cpuPercent is raw percent of one core (can
// exceed 100% on a multi-core box), and the first call for a pid returns 0
// because there is no baseline to diff against yet.
func ProcessStats(pid uint32) (cpuPercent float64, memBytes uint64, err error) {
	p := strconv.FormatUint(uint64(pid), 10)

	data, err := os.ReadFile("/proc/" + p + "/stat")
	if err != nil {
		return 0, 0, err
	}
	ticks, err := parseProcStatTicks(string(data))
	if err != nil {
		return 0, 0, err
	}

	// Resident memory: /proc/<pid>/statm field 2 is RSS in pages.
	if sm, e := os.ReadFile("/proc/" + p + "/statm"); e == nil {
		memBytes = parseStatmRSSBytes(string(sm), os.Getpagesize())
	}

	now := time.Now()
	cpuSampleMuLinux.Lock()
	prev, ok := cpuSamplesLinux[pid]
	cpuSamplesLinux[pid] = cpuSampleLinux{ticks: ticks, wall: now}
	cpuSampleMuLinux.Unlock()

	if ok && now.After(prev.wall) {
		cpuSec := float64(ticks-prev.ticks) / float64(linuxUserHZ)
		wallSec := now.Sub(prev.wall).Seconds()
		if wallSec > 0 {
			cpuPercent = (cpuSec / wallSec) * 100.0
		}
	}
	return cpuPercent, memBytes, nil
}
