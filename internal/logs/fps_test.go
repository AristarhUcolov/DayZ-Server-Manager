// Copyright (c) 2026 Aristarh Ucolov.

package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastServerFPS(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "DayZServer_x64_2026-10-10_12-00-00.RPT")

	if _, _, ok := LastServerFPS(p); ok {
		t.Fatal("missing file must not report an FPS")
	}

	log := strings.Join([]string{
		"12:00:01.001 Mission read.",
		"12:01:01.002 Average server FPS: 41.25 (measured interval: 60 s)",
		"12:01:30.000 Player \"Survivor\" is connected (id=abc)",
		"12:02:01.003 Average server FPS: 59.97 (measured interval: 60 s)",
		"12:02:05.000 something else",
	}, "\r\n")
	if err := os.WriteFile(p, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	fps, _, ok := LastServerFPS(p)
	if !ok || fps != 59.97 {
		t.Fatalf("got %v %v, want the newest line 59.97", fps, ok)
	}

	// The cache must notice the file growing.
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\r\n12:03:01.004 Average server FPS: 12.5 (measured interval: 60 s)\r\n")
	f.Close()
	if fps, _, ok := LastServerFPS(p); !ok || fps != 12.5 {
		t.Fatalf("after append got %v %v, want 12.5", fps, ok)
	}

	// No FPS lines at all (logAverageFps off).
	q := filepath.Join(dir, "other.RPT")
	os.WriteFile(q, []byte("12:00:00 nothing here\n"), 0o644)
	if _, _, ok := LastServerFPS(q); ok {
		t.Fatal("a log without FPS lines must report ok=false")
	}
}
