// Copyright (c) 2026 Aristarh Ucolov.
//go:build linux

package util

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Nice values standing in for the Windows priority classes.
var niceLevels = map[string]int{
	"idle": 19, "belownormal": 10, "normal": 0, "abovenormal": -5, "high": -10,
}

// bootTime reads the system boot time (btime in /proc/stat).
func bootTime() time.Time {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "btime ") {
			if n, err := strconv.ParseInt(strings.TrimSpace(sc.Text()[6:]), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
	}
	return time.Time{}
}

// procStartTime converts /proc/<pid>/stat starttime (field 22, clock ticks
// since boot) to wall time.
func procStartTime(pid int) time.Time {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return time.Time{}
	}
	f := strings.Fields(s[i+1:])
	// f[0] is field 3 (state); starttime is field 22 → f[19].
	if len(f) < 20 {
		return time.Time{}
	}
	ticks, err := strconv.ParseInt(f[19], 10, 64)
	if err != nil {
		return time.Time{}
	}
	bt := bootTime()
	if bt.IsZero() {
		return time.Time{}
	}
	return bt.Add(time.Duration(ticks) * time.Second / linuxUserHZ)
}

// procState returns the one-letter state from /proc/<pid>/stat ("" if gone).
func procState(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(data)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		if f := strings.Fields(s[i+1:]); len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

// FindProcessesByExe lists running processes whose /proc/<pid>/exe resolves to
// exePath. A binary replaced on disk reads as "<path> (deleted)" and still counts.
func FindProcessesByExe(exePath string) []ProcInfo {
	want := filepath.Clean(exePath)
	if r, err := filepath.EvalSymlinks(want); err == nil {
		want = r
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []ProcInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		exe, err := os.Readlink("/proc/" + e.Name() + "/exe")
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		if filepath.Clean(exe) != want {
			continue
		}
		if st := procState(pid); st == "Z" || st == "" {
			continue
		}
		out = append(out, ProcInfo{PID: pid, Exe: exe, Started: procStartTime(pid)})
	}
	return out
}

// ProcessAlive reports whether pid is a live (non-zombie) process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	st := procState(pid)
	return st != "" && st != "Z"
}

// WaitPID polls until a non-child process exits. The exit code of a process we
// did not start is not observable on Linux, so it is reported as -1.
func WaitPID(pid int) (int, error) {
	for ProcessAlive(pid) {
		time.Sleep(time.Second)
	}
	return -1, nil
}

// cpuSet mirrors the kernel's cpu_set_t (1024 CPUs).
type cpuSet [16]uint64

// tasks returns every thread id of pid, falling back to pid itself.
func tasks(pid int) []int {
	entries, err := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		return []int{pid}
	}
	var out []int
	for _, e := range entries {
		if n, err := strconv.Atoi(e.Name()); err == nil {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return []int{pid}
	}
	return out
}

// SetAffinity pins every thread of pid to the given CPU indexes. Affinity is
// per-thread on Linux, so threads already running must each be set; new threads
// inherit it. An empty list allows all CPUs.
func SetAffinity(pid int, cores []int) error {
	var set cpuSet
	if len(cores) == 0 {
		for i := range set {
			set[i] = ^uint64(0)
		}
	} else {
		any := false
		for _, c := range cores {
			if c >= 0 && c < len(set)*64 {
				set[c/64] |= 1 << uint(c%64)
				any = true
			}
		}
		if !any {
			return errors.New("no valid CPU cores selected")
		}
	}
	var firstErr error
	for _, tid := range tasks(pid) {
		_, _, e := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY, uintptr(tid),
			unsafe.Sizeof(set), uintptr(unsafe.Pointer(&set)))
		if e != 0 && firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}

// ProcessAffinity returns the CPU indexes pid's main thread may run on.
func ProcessAffinity(pid int) ([]int, error) {
	var set cpuSet
	_, _, e := syscall.RawSyscall(syscall.SYS_SCHED_GETAFFINITY, uintptr(pid),
		unsafe.Sizeof(set), uintptr(unsafe.Pointer(&set)))
	if e != 0 {
		return nil, e
	}
	var out []int
	for i := 0; i < len(set)*64; i++ {
		if set[i/64]&(1<<uint(i%64)) != 0 {
			out = append(out, i)
		}
	}
	return out, nil
}

// SetPriority maps the panel's priority name to a nice value on every thread.
// Raising priority (negative nice) needs root or CAP_SYS_NICE.
func SetPriority(pid int, level string) error {
	nice, ok := niceLevels[strings.ToLower(level)]
	if !ok {
		return fmt.Errorf("unknown priority %q", level)
	}
	var firstErr error
	for _, tid := range tasks(pid) {
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, tid, nice); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil && nice < 0 {
		return fmt.Errorf("%w (raising priority on Linux needs root or CAP_SYS_NICE)", firstErr)
	}
	return firstErr
}

// ProcessPriority maps pid's nice value back to the closest panel name.
func ProcessPriority(pid int) string {
	n, err := syscall.Getpriority(syscall.PRIO_PROCESS, pid)
	if err != nil {
		return ""
	}
	// The raw syscall returns 20-nice.
	nice := 20 - n
	best, bestD := "", 1<<30
	for name, v := range niceLevels {
		d := v - nice
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = name, d
		}
	}
	return best
}
