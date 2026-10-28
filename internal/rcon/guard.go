// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"log"
	"runtime/debug"
	"sync/atomic"
)

var logf atomic.Value // func(string, ...interface{})

// SetLogf sets where the RCon client reports a recovered panic (the manager's
// log); the standard logger until then.
func SetLogf(f func(string, ...interface{})) { logf.Store(f) }

// guard recovers a panic on one of the client's background goroutines — a
// malformed packet must cost the connection, not the whole manager — and then
// runs onPanic.
func guard(name string, onPanic func()) {
	r := recover()
	if r == nil {
		return
	}
	msg := "BUG: rcon %s panicked: %v\n%s"
	if f, ok := logf.Load().(func(string, ...interface{})); ok && f != nil {
		f(msg, name, r, debug.Stack())
	} else {
		log.Printf(msg, name, r, debug.Stack())
	}
	if onPanic != nil {
		onPanic()
	}
}
