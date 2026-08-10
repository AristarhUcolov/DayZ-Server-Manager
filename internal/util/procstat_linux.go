// Copyright (c) 2026 Aristarh Ucolov.
//go:build linux

package util

import (
	"errors"
	"os"
	"strconv"
	"strings"
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
	s := string(data)
	// Field 2 (comm) is parenthesised and may itself contain spaces or ')', so
	// split on the part after the LAST ')'. After it, index 0 is field 3 (state);
	// utime is field 14 → index 11, stime is field 15 → index 12.
	rp := strings.LastIndexByte(s, ')')
	if rp < 0 {
		return 0, 0, errors.New("unparseable /proc stat")
	}
	fields := strings.Fields(s[rp+1:])
	if len(fields) < 13 {
		return 0, 0, errors.New("short /proc stat")
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	ticks := utime + stime

	// Resident memory: /proc/<pid>/statm field 2 is RSS in pages.
	if sm, e := os.ReadFile("/proc/" + p + "/statm"); e == nil {
		f := strings.Fields(string(sm))
		if len(f) >= 2 {
			if pages, e2 := strconv.ParseUint(f[1], 10, 64); e2 == nil {
				memBytes = pages * uint64(os.Getpagesize())
			}
		}
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
