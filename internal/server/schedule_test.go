// Copyright (c) 2026 Aristarh Ucolov.
package server

import (
	"io"
	"log"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/util"
)

// fakeServer starts a harmless long-running child to stand in for DayZServer
// and hands it to the controller as the supervised process.
func fakeServer(t *testing.T, c *Controller) *runningProc {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "120", "127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "120")
	}
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a stand-in process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	p := &runningProc{pid: cmd.Process.Pid, cmd: cmd, startedAt: time.Now()}
	c.mu.Lock()
	c.proc = p
	c.mu.Unlock()
	c.runGen.Add(1)
	c.running.Store(true)
	go c.waitCmd(p, nil)
	return p
}

func quietController(t *testing.T, cfg *config.Manager) *Controller {
	return NewController(t.TempDir(), cfg, log.New(io.Discard, "", 0))
}

func TestFormatWarn(t *testing.T) {
	if got := formatWarn("", 5); got != "Server restart in 5 min" {
		t.Errorf("default: %q", got)
	}
	if got := formatWarn("Рестарт через {m} мин.", 3); got != "Рестарт через 3 мин." {
		t.Errorf("template: %q", got)
	}
	if got := formatWarn("Restarting soon", 1); got != "Restarting soon (1)" {
		t.Errorf("no placeholder: %q", got)
	}
}

func TestNextOccurrence(t *testing.T) {
	base := time.Date(2026, 10, 1, 13, 30, 0, 0, time.Local)
	at, ok := nextOccurrence("14:00", base)
	if !ok || !at.Equal(time.Date(2026, 10, 1, 14, 0, 0, 0, time.Local)) {
		t.Errorf("later today: %v %v", at, ok)
	}
	at, _ = nextOccurrence("03:00", base)
	if !at.Equal(time.Date(2026, 10, 2, 3, 0, 0, 0, time.Local)) {
		t.Errorf("tomorrow: %v", at)
	}
	at, _ = nextOccurrence("13:30", base)
	if !at.Equal(base) {
		t.Errorf("exact now: %v", at)
	}
	if _, ok := nextOccurrence("25:99", base); ok {
		t.Errorf("invalid time accepted")
	}
}

// TestManualRestartMovesIntervalSchedule is the regression test for the
// v0.31 report "players were told 3 minutes while the panel said 8": the old
// loop slept on a deadline computed from the PREVIOUS run, so a manual restart
// left a stale timer that fired on the old schedule. The deadline must follow
// the current run.
func TestManualRestartMovesIntervalSchedule(t *testing.T) {
	c := quietController(t, &config.Manager{AutoRestartEnabled: true, AutoRestartSeconds: 3})
	c.tickOverride = 20 * time.Millisecond
	first := fakeServer(t, c)
	c.StartAutoRestartLoop()
	defer c.StopAutoRestartLoop()

	// Manual restart after 1.5s: a new run, a new deadline (t≈4.5s).
	time.Sleep(1500 * time.Millisecond)
	c.mu.Lock()
	c.proc = nil
	c.mu.Unlock()
	second := fakeServer(t, c)
	_ = first

	plan := c.NextRestart()
	if plan == nil || plan.Reason != "interval" {
		t.Fatalf("no interval plan after manual restart: %+v", plan)
	}
	if d := time.Until(plan.At); d < 2500*time.Millisecond || d > 3500*time.Millisecond {
		t.Fatalf("next restart in %v, want ~3s from the manual restart", d)
	}
	// The old deadline (t=3s) must NOT kill the new run.
	time.Sleep(2 * time.Second)
	if !util.ProcessAlive(second.pid) {
		t.Fatalf("server restarted on the OLD schedule")
	}
	// The new deadline (~t=4.5s) must.
	deadline := time.Now().Add(4 * time.Second)
	for util.ProcessAlive(second.pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if util.ProcessAlive(second.pid) {
		t.Fatalf("server was not restarted on the new schedule")
	}
}

// TestCountdownAbortsOnManualStop: stopping the server during a countdown must
// cancel it — the old code restarted (i.e. started) a server the admin had
// just shut down.
func TestCountdownAbortsOnManualStop(t *testing.T) {
	c := quietController(t, &config.Manager{})
	p := fakeServer(t, c)
	done := make(chan error, 1)
	go func() { done <- c.restartAt(time.Now().Add(1500*time.Millisecond), "scheduled") }()
	time.Sleep(300 * time.Millisecond)
	c.runGen.Add(1) // a manual restart happened
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("aborted countdown returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("countdown did not abort")
	}
	if !util.ProcessAlive(p.pid) {
		t.Fatalf("aborted countdown still restarted the server")
	}
	if c.NextRestart() != nil && c.NextRestart().Active {
		t.Fatalf("plan left active after abort")
	}
}

// TestKickWindowLocksAndKicks: inside the kick window the server is locked and
// players are kicked; the dashboard plan reports the exact target.
func TestKickWindowLocksAndKicks(t *testing.T) {
	c := quietController(t, &config.Manager{RestartKickSeconds: 2, RestartLock: true, RestartKickMessage: "bye"})
	fakeServer(t, c)
	var locks, kicks atomic.Int32
	var mu sync.Mutex
	var reason string
	c.LockServer = func(lock bool) error {
		if lock {
			locks.Add(1)
		}
		return nil
	}
	c.KickAll = func(r string) (int, error) {
		mu.Lock()
		reason = r
		mu.Unlock()
		kicks.Add(1)
		return 1, nil
	}
	target := time.Now().Add(2500 * time.Millisecond)
	go func() { _ = c.restartAt(target, "scheduled") }()
	time.Sleep(1200 * time.Millisecond)
	plan := c.NextRestart()
	if plan == nil || !plan.Active || !plan.Kicking || !plan.At.Equal(target) {
		t.Fatalf("plan during kick window = %+v, want active+kicking at %v", plan, target)
	}
	time.Sleep(2 * time.Second)
	if locks.Load() != 1 {
		t.Errorf("lock called %d times, want 1", locks.Load())
	}
	if kicks.Load() < 1 {
		t.Errorf("nobody was kicked")
	}
	mu.Lock()
	defer mu.Unlock()
	if reason != "bye" {
		t.Errorf("kick reason %q", reason)
	}
}
