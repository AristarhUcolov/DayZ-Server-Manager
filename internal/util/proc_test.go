// Copyright (c) 2026 Aristarh Ucolov.
package util

import (
	"os"
	"runtime"
	"testing"
)

// TestFindOwnProcess: the test binary must find itself by its own exe path —
// the same lookup the controller uses to adopt a still-running DayZ server.
func TestFindOwnProcess(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("process lookup is implemented for windows and linux")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range FindProcessesByExe(exe) {
		if p.PID == os.Getpid() {
			found = true
			if p.Started.IsZero() {
				t.Errorf("start time not resolved for own process")
			}
		}
	}
	if !found {
		t.Fatalf("own pid %d not found for %s", os.Getpid(), exe)
	}
	if !ProcessAlive(os.Getpid()) {
		t.Errorf("own process reported dead")
	}
	if ProcessPriority(os.Getpid()) == "" {
		t.Errorf("priority not readable")
	}
	if cores, err := ProcessAffinity(os.Getpid()); err != nil || len(cores) == 0 {
		t.Errorf("affinity not readable: %v %v", cores, err)
	}
}
