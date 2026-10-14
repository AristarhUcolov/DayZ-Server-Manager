// Copyright (c) 2026 Aristarh Ucolov.
package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dayzmanager/internal/config"
)

// modCountingController wires an UpdateMods hook that counts calls and lingers
// so concurrent restarts overlap. There is no DayZServer binary in the test
// dir, so Start fails fast after the update step.
func modCountingController(t *testing.T, calls *atomic.Int32) *Controller {
	c := quietController(t, &config.Manager{
		AutoUpdateModsOnRestart: true,
		ModSource:               config.ModSourceFolder,
		ModsFolderPath:          t.TempDir(),
	})
	c.loopPaused.Store(true) // keeps the failed-start retry goroutine idle
	c.UpdateMods = func(string) ([]string, error) {
		calls.Add(1)
		time.Sleep(300 * time.Millisecond)
		return nil, nil
	}
	return c
}

// Two overlapping restarts (double click, two tabs, manual during scheduled)
// must cycle the server once: the old code let both reach the mod update and
// Start, and the loser raised a false "RESTART FAILED".
func TestConcurrentRestartsRunOnce(t *testing.T) {
	var calls atomic.Int32
	c := modCountingController(t, &calls)
	if c.schedSnapshot().modSourceDir == "" {
		t.Fatal("test config has no mod source dir")
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = c.Restart()
		}(i)
		time.Sleep(50 * time.Millisecond)
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("mod update ran %d times, want 1", n)
	}
	// Both callers report the one restart's outcome (here: no binary).
	if errs[0] == nil || errs[1] == nil || errs[0].Error() != errs[1].Error() {
		t.Fatalf("callers disagree on the outcome: %v / %v", errs[0], errs[1])
	}
}

// The manager's own start step (mod update + Start) must wait for WriteLock so
// it never runs while a web request is mid-write.
func TestRestartWaitsForWriteLock(t *testing.T) {
	var calls atomic.Int32
	c := modCountingController(t, &calls)
	var mu sync.Mutex
	c.WriteLock = &mu

	mu.Lock()
	done := make(chan struct{})
	go func() {
		_ = c.Restart()
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		mu.Unlock()
		t.Fatalf("mod update ran while a write held the lock")
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not proceed after the lock was released")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("mod update ran %d times after the lock was released, want 1", n)
	}
}

// A scheduled profile whose countdown is cancelled must not stay staged for
// some unrelated later restart to apply.
func TestCancelledProfileCountdownDropsProfile(t *testing.T) {
	c := quietController(t, &config.Manager{})
	c.ApplyProfile = func(string) error { return nil }
	fakeServer(t, c)
	done := make(chan error, 1)
	go func() {
		done <- c.restartWithProfile(time.Now().Add(1500*time.Millisecond), "profile", "weekend")
	}()
	time.Sleep(300 * time.Millisecond)
	c.pendingMu.Lock()
	staged := c.pendingProf
	c.pendingMu.Unlock()
	if staged != "weekend" {
		t.Fatalf("profile not staged during the countdown: %q", staged)
	}
	c.runGen.Add(1) // the admin stopped/started by hand
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("countdown did not abort")
	}
	c.pendingMu.Lock()
	left := c.pendingProf
	c.pendingMu.Unlock()
	if left != "" {
		t.Fatalf("cancelled countdown left profile %q staged", left)
	}
}

// The admin pressing Start while a restart waits (graceful stop, the 30s wait,
// or a web write holding the lock) already gave the restart what it wanted: it
// must neither update mods under the new run nor report a failed start.
func TestRestartYieldsToManualStart(t *testing.T) {
	var calls atomic.Int32
	c := modCountingController(t, &calls)
	var mu sync.Mutex
	c.WriteLock = &mu

	mu.Lock()
	done := make(chan error, 1)
	go func() { done <- c.Restart() }()
	time.Sleep(300 * time.Millisecond)
	fakeServer(t, c) // the admin's Start, a new run
	mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restart reported %v although the server is up", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not return")
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("mod update ran %d times under a running server", n)
	}
}
