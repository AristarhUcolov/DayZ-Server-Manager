// Copyright (c) 2026 Aristarh Ucolov.
package logs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readFirst(t *testing.T, path string, offset int64) string {
	t.Helper()
	stop := make(chan struct{})
	got := make(chan string, 1)
	go func() {
		_ = TailFrom(stop, path, offset, func(b []byte) error {
			select {
			case got <- string(b):
			default:
			}
			return nil
		})
	}()
	defer close(stop)
	select {
	case s := <-got:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("no data")
		return ""
	}
}

func TestTailFromOffset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.RPT")
	if err := os.WriteFile(p, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := readFirst(t, p, 6); s != "second\n" {
		t.Fatalf("from 6: %q", s)
	}
	// An offset past the end: the file was replaced by a shorter one since
	// the snapshot — start over instead of waiting forever.
	if s := readFirst(t, p, 999); s != "first\nsecond\n" {
		t.Fatalf("past end: %q", s)
	}
}
