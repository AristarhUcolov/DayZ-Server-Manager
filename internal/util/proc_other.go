// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows && !linux

package util

import "errors"

var errProcUnsupported = errors.New("process control: unsupported on this OS")

func FindProcessesByExe(exePath string) []ProcInfo { return nil }
func ProcessAlive(pid int) bool                    { return false }
func WaitPID(pid int) (int, error)                 { return -1, errProcUnsupported }
func SetAffinity(pid int, cores []int) error       { return errProcUnsupported }
func SetPriority(pid int, level string) error      { return errProcUnsupported }
func ProcessAffinity(pid int) ([]int, error)       { return nil, errProcUnsupported }
func ProcessPriority(pid int) string               { return "" }
