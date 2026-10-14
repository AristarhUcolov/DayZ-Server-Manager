// Copyright (c) 2026 Aristarh Ucolov.

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess stands in for a browser: it fails at once, exits cleanly
// at once (a hand-off to a running browser) or keeps running (a window).
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("DZM_HELPER") {
	case "":
		return
	case "fail":
		fmt.Fprintln(os.Stderr, "Running as root without --no-sandbox is not supported.")
		os.Exit(3)
	case "handoff":
		os.Exit(0)
	case "window":
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
}

func helper(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(), "DZM_HELPER="+mode)
	return cmd
}

// A browser that refuses to start is reported as a failure with its reason
// (before, any quick exit counted as success and the user saw no window and
// no explanation); a clean quick exit is a hand-off; a running one is a window.
func TestLaunchTellsFailureHandOffAndWindowApart(t *testing.T) {
	if _, err := Launch(helper("fail"), 2*time.Second); err == nil || !strings.Contains(err.Error(), "--no-sandbox") {
		t.Fatalf("a failing browser must be an error carrying its message, got %v", err)
	}
	l, err := Launch(helper("handoff"), 2*time.Second)
	if err != nil || !l.HandedOff {
		t.Fatalf("a clean quick exit is a hand-off: %+v %v", l, err)
	}
	l, err = Launch(helper("window"), 500*time.Millisecond)
	if err != nil || l.HandedOff || l.Done == nil {
		t.Fatalf("a running browser is a window: %+v %v", l, err)
	}
	select {
	case <-l.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("Done never reported the exit")
	}
}

func TestLaunchMissingProgram(t *testing.T) {
	if err := TryStart(exec.Command("dzm-no-such-browser-xyz"), time.Second); err == nil {
		t.Fatal("a missing program must fail")
	}
}
