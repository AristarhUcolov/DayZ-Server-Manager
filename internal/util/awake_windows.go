// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"runtime"
	"sync"
)

// Execution-state flags for SetThreadExecutionState.
const (
	esSystemRequired = 0x00000001
	esContinuous     = 0x80000000
)

var procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")

var awake struct {
	once sync.Once
	ch   chan bool
}

// SetKeepAwake stops (true) or allows again (false) Windows from putting an
// idle computer to sleep — the screen may still turn off. A sleeping PC takes
// the DayZ server and the panel with it, which looks like the manager
// "closing by itself" after the power plan's 15-30 minutes.
//
// The execution state belongs to the thread that sets it, so one goroutine,
// locked to its own OS thread, owns it for the life of the process.
func SetKeepAwake(on bool) {
	awake.once.Do(func() {
		awake.ch = make(chan bool, 1)
		go func() {
			runtime.LockOSThread()
			for want := range awake.ch {
				flags := uintptr(esContinuous)
				if want {
					flags |= esSystemRequired
				}
				procSetThreadExecutionState.Call(flags)
			}
		}()
	})
	// The newest wish wins: drop a queued older one.
	select {
	case <-awake.ch:
	default:
	}
	awake.ch <- on
}
