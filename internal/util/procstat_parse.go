// Copyright (c) 2026 Aristarh Ucolov.

package util

import (
	"errors"
	"strconv"
	"strings"
)

// parseProcStatTicks pulls utime+stime (CPU time in clock ticks) out of the
// contents of /proc/<pid>/stat. It lives in an un-tagged file so the parsing —
// the fiddly part, because field 2 (comm) is parenthesised and may contain
// spaces or ')' — can be unit-tested on any OS, not only when built for Linux.
//
// Field layout (1-indexed): 1=pid, 2=(comm), 3=state, …, 14=utime, 15=stime.
// After the LAST ')', index 0 is field 3 (state); utime is index 11, stime 12.
func parseProcStatTicks(stat string) (uint64, error) {
	rp := strings.LastIndexByte(stat, ')')
	if rp < 0 {
		return 0, errors.New("unparseable /proc stat: no ')'")
	}
	fields := strings.Fields(stat[rp+1:])
	if len(fields) < 13 {
		return 0, errors.New("short /proc stat")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return utime + stime, nil
}

// parseStatmRSSBytes returns the resident-set size in bytes from the contents
// of /proc/<pid>/statm (field 2 is RSS in pages). Returns 0 on any problem.
func parseStatmRSSBytes(statm string, pageSize int) uint64 {
	f := strings.Fields(statm)
	if len(f) < 2 || pageSize <= 0 {
		return 0
	}
	pages, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(pageSize)
}
