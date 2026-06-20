// Copyright (c) 2026 Aristarh Ucolov.
//
// Opt-in RCon protocol tracing. Off by default; enable by starting the manager
// with the environment variable DAYZ_RCON_DEBUG=1. Traces go to manager.log
// (Logger is wired by the app). Used to diagnose "rcon: timeout" — it shows
// exactly what the read loop receives after a command is sent.
package rcon

import (
	"log"
	"os"
)

var (
	// Logger receives the traces (wired by app to manager.log).
	Logger *log.Logger
	// Debug gates the verbose output.
	Debug = os.Getenv("DAYZ_RCON_DEBUG") == "1"
)

func dbg(format string, args ...interface{}) {
	if Debug && Logger != nil {
		Logger.Printf("[rcon] "+format, args...)
	}
}
