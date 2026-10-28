// Copyright (c) 2026 Aristarh Ucolov.
package launcher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A process that ends at once: the test binary running no tests.
func shortProcess(t *testing.T) int {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

func guardFixture(t *testing.T) (serverDir, managerDir string) {
	t.Helper()
	serverDir = t.TempDir()
	managerDir = filepath.Join(serverDir, ".dayz-manager")
	if err := os.MkdirAll(managerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	SetServerDir(serverDir)
	t.Cleanup(func() { SetServerDir("") })
	old, oldPause := relaunch, guardPause
	guardPause = 0
	t.Cleanup(func() { relaunch, guardPause = old, oldPause })
	return serverDir, managerDir
}

func writeMarker(t *testing.T, managerDir string, st runState) {
	t.Helper()
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(managerDir, runMarkerFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGuardianRestartsAfterAnUnexpectedEnd(t *testing.T) {
	if !guardianSupported() {
		t.Skip("no guardian on this system")
	}
	serverDir, managerDir := guardFixture(t)
	pid := shortProcess(t)
	writeMarker(t, managerDir, runState{PID: pid, Started: time.Now().Add(-time.Hour), LastSeen: time.Now()})
	var got []string
	relaunch = func(exe, dir string, args []string) error {
		if dir != serverDir {
			t.Errorf("relaunched in %s, want %s", dir, serverDir)
		}
		got = args
		return nil
	}
	RunGuardian(pid, 8790, "")
	if strings.Join(got, " ") != "-autostart -dir "+serverDir+" -port 8790" {
		t.Fatalf("relaunch args = %q", got)
	}
	if !restartedByGuardian(managerDir, time.Now()) {
		t.Fatal("the new start is not recognised as the guardian's restart")
	}
	log, _ := os.ReadFile(filepath.Join(managerDir, "manager.log"))
	if !strings.Contains(string(log), "the guardian started it again") {
		t.Fatalf("no log line: %q", log)
	}
}

func TestGuardianLeavesANormalExitAlone(t *testing.T) {
	if !guardianSupported() {
		t.Skip("no guardian on this system")
	}
	_, managerDir := guardFixture(t)
	called := false
	relaunch = func(string, string, []string) error { called = true; return nil }

	// No marker: the manager exited normally.
	RunGuardian(shortProcess(t), 8790, "")
	// A sign-out or shutdown.
	pid := shortProcess(t)
	writeMarker(t, managerDir, runState{PID: pid, Started: time.Now(), LastSeen: time.Now(), SessionEnd: true})
	RunGuardian(pid, 8790, "")
	// The marker of a newer run.
	writeMarker(t, managerDir, runState{PID: pid + 1, Started: time.Now(), LastSeen: time.Now()})
	RunGuardian(shortProcess(t), 8790, "")
	// Turned off in the settings.
	pid = shortProcess(t)
	writeMarker(t, managerDir, runState{PID: pid, Started: time.Now(), LastSeen: time.Now()})
	if err := os.WriteFile(filepath.Join(managerDir, "manager.json"), []byte(`{"noGuardian": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	RunGuardian(pid, 8790, "")
	if called {
		t.Fatal("the guardian restarted a manager it should have left alone")
	}
}

func TestGuardianStopsAfterRepeatedCrashes(t *testing.T) {
	if !guardianSupported() {
		t.Skip("no guardian on this system")
	}
	_, managerDir := guardFixture(t)
	now := time.Now()
	writeGuardState(managerDir, guardState{Restarts: []time.Time{now.Add(-10 * time.Minute), now.Add(-5 * time.Minute), now.Add(-time.Minute)}})
	called := false
	relaunch = func(string, string, []string) error { called = true; return nil }
	pid := shortProcess(t)
	writeMarker(t, managerDir, runState{PID: pid, Started: now, LastSeen: now})
	RunGuardian(pid, 8790, "")
	if called {
		t.Fatal("restarted a fourth time within the window")
	}
	// Restarts older than the window no longer count.
	writeGuardState(managerDir, guardState{Restarts: []time.Time{now.Add(-50 * time.Minute), now.Add(-40 * time.Minute), now.Add(-30 * time.Minute)}})
	RunGuardian(pid, 8790, "")
	if !called {
		t.Fatal("old restarts kept the guardian from restarting")
	}
}

func TestExitRequestRemovesTheRunMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), runMarkerFile)
	lc := &lifecycle{path: path, st: runState{PID: 1, Started: time.Now(), LastSeen: time.Now()}}
	lc.write()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("marker not written")
	}
	i := &Instance{life: lc}
	i.markExiting()
	lc.write() // a heartbeat after the exit request
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the marker is still there after an exit request")
	}
}
