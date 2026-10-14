// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	processSetInformation = 0x0200
	processSynchronize    = 0x00100000
	infiniteWait          = 0xFFFFFFFF
)

var (
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procWaitForSingleObject        = kernel32.NewProc("WaitForSingleObject")
	procSetProcessAffinityMask     = kernel32.NewProc("SetProcessAffinityMask")
	procGetProcessAffinityMask     = kernel32.NewProc("GetProcessAffinityMask")
	procSetPriorityClass           = kernel32.NewProc("SetPriorityClass")
	procGetPriorityClass           = kernel32.NewProc("GetPriorityClass")
)

// Windows priority classes, keyed by the names the panel stores.
var priorityClasses = map[string]uintptr{
	"idle":        0x00000040,
	"belownormal": 0x00004000,
	"normal":      0x00000020,
	"abovenormal": 0x00008000,
	"high":        0x00000080,
}

func openProcess(access uint32, pid int) (syscall.Handle, error) {
	h, _, err := procOpenProcess.Call(uintptr(access), 0, uintptr(pid))
	if h == 0 {
		return 0, err
	}
	return syscall.Handle(h), nil
}

// processImageAndStart returns a process's full image path and creation time.
func processImageAndStart(pid int) (string, time.Time, error) {
	h, err := openProcess(processQueryLimitedInformation, pid)
	if err != nil {
		return "", time.Time{}, err
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, syscall.MAX_LONG_PATH)
	size := uint32(len(buf))
	r, _, e := procQueryFullProcessImageNameW.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", time.Time{}, e
	}
	var creation, exit, kernel, user syscall.Filetime
	started := time.Time{}
	if r2, _, _ := procGetProcessTimes.Call(uintptr(h),
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r2 != 0 {
		started = time.Unix(0, creation.Nanoseconds())
	}
	return syscall.UTF16ToString(buf[:size]), started, nil
}

// FindProcessesByExe lists running processes whose image is exactly exePath
// (case-insensitive, cleaned). Used to adopt a DayZ server that is still
// running from an earlier manager session.
func FindProcessesByExe(exePath string) []ProcInfo {
	want := strings.ToLower(filepath.Clean(exePath))
	base := strings.ToLower(filepath.Base(exePath))
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	var out []ProcInfo
	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		if strings.ToLower(syscall.UTF16ToString(pe.ExeFile[:])) != base {
			continue
		}
		full, started, e := processImageAndStart(int(pe.ProcessID))
		if e != nil || strings.ToLower(filepath.Clean(full)) != want {
			continue
		}
		out = append(out, ProcInfo{PID: int(pe.ProcessID), Exe: full, Started: started})
	}
	return out
}

// WaitPID blocks until the process exits and returns its exit code (-1 when
// it cannot be read). Works for processes this program did not start.
func WaitPID(pid int) (int, error) {
	h, err := openProcess(processSynchronize|processQueryLimitedInformation, pid)
	if err != nil {
		return -1, err
	}
	defer syscall.CloseHandle(h)
	if r, _, e := procWaitForSingleObject.Call(uintptr(h), infiniteWait); r != 0 {
		return -1, e
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return -1, nil
	}
	return int(code), nil
}

// ProcessAlive reports whether pid names a live process.
func ProcessAlive(pid int) bool {
	h, err := openProcess(processQueryLimitedInformation, pid)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// SetAffinity pins the process to the given logical CPU indexes (0-based).
// An empty list restores all CPUs the system allows.
func SetAffinity(pid int, cores []int) error {
	h, err := openProcess(processSetInformation|processQueryLimitedInformation, pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	var procMask, sysMask uintptr
	if r, _, e := procGetProcessAffinityMask.Call(uintptr(h),
		uintptr(unsafe.Pointer(&procMask)), uintptr(unsafe.Pointer(&sysMask))); r == 0 {
		return e
	}
	mask := sysMask
	if len(cores) > 0 {
		mask = 0
		for _, c := range cores {
			if c < 0 || c >= int(unsafe.Sizeof(mask))*8 {
				continue
			}
			mask |= 1 << uint(c)
		}
		mask &= sysMask
		if mask == 0 {
			return errors.New("none of the selected CPU cores exist on this machine")
		}
	}
	if r, _, e := procSetProcessAffinityMask.Call(uintptr(h), mask); r == 0 {
		return e
	}
	return nil
}

// SetPriority sets the process priority class by its panel name.
func SetPriority(pid int, level string) error {
	class, ok := priorityClasses[strings.ToLower(level)]
	if !ok {
		return fmt.Errorf("unknown priority %q", level)
	}
	h, err := openProcess(processSetInformation|processQueryLimitedInformation, pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	if r, _, e := procSetPriorityClass.Call(uintptr(h), class); r == 0 {
		return e
	}
	return nil
}

// ProcessAffinity returns the CPU indexes the process may run on.
func ProcessAffinity(pid int) ([]int, error) {
	h, err := openProcess(processQueryLimitedInformation, pid)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)
	var procMask, sysMask uintptr
	if r, _, e := procGetProcessAffinityMask.Call(uintptr(h),
		uintptr(unsafe.Pointer(&procMask)), uintptr(unsafe.Pointer(&sysMask))); r == 0 {
		return nil, e
	}
	var out []int
	for i := 0; i < int(unsafe.Sizeof(procMask))*8; i++ {
		if procMask&(1<<uint(i)) != 0 {
			out = append(out, i)
		}
	}
	return out, nil
}

// ProcessPriority returns the process priority class name ("" if unknown).
func ProcessPriority(pid int) string {
	h, err := openProcess(processQueryLimitedInformation, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	r, _, _ := procGetPriorityClass.Call(uintptr(h))
	for name, c := range priorityClasses {
		if c == r {
			return name
		}
	}
	return ""
}
