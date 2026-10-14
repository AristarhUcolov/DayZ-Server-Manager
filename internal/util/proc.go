// Copyright (c) 2026 Aristarh Ucolov.
package util

import "time"

// ProcInfo describes a running process found by FindProcessesByExe.
type ProcInfo struct {
	PID     int
	Exe     string
	Started time.Time // zero when the OS would not say
}

// PriorityLevels are the process priority names the panel offers, lowest
// first. "realtime" is deliberately absent — it can starve the OS itself.
var PriorityLevels = []string{"idle", "belownormal", "normal", "abovenormal", "high"}
