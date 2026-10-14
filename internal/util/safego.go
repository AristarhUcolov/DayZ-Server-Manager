// Copyright (c) 2026 Aristarh Ucolov.
package util

import (
	"log"
	"runtime/debug"
	"time"
)

// SafeLoop runs a long-lived background loop and keeps the manager alive if
// it panics: the panic and its stack go to the log, and the loop is started
// again after a growing pause (5 s … 5 min). A panic in a goroutine is not
// caught by anything else — it would take down the whole manager, with every
// scheduled restart, backup and the crash watchdog. SafeLoop returns when fn
// returns normally (the loop's own stop condition).
func SafeLoop(logger *log.Logger, name string, fn func()) {
	backoff := 5 * time.Second
	for {
		if !runRecovered(logger, name, fn) {
			return
		}
		time.Sleep(backoff)
		if backoff < 5*time.Minute {
			backoff *= 2
		}
		if logger != nil {
			logger.Printf("%s: restarted after a crash", name)
		}
	}
}

// SafeGo runs fn once in the current goroutine, logging instead of crashing
// on a panic. For one-shot background work (a broadcast, a notification).
func SafeGo(logger *log.Logger, name string, fn func()) {
	runRecovered(logger, name, fn)
}

// runRecovered reports whether fn panicked.
func runRecovered(logger *log.Logger, name string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			if logger != nil {
				logger.Printf("BUG: %s panicked: %v\n%s", name, r, debug.Stack())
			} else {
				log.Printf("BUG: %s panicked: %v\n%s", name, r, debug.Stack())
			}
		}
	}()
	fn()
	return false
}
