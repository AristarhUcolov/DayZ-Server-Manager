// Copyright (c) 2026 Aristarh Ucolov.
package logs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Earlier runs' RPT / ADM files are offered next to the newest one — after a
// crash and a watchdog restart the crashed run is no longer the newest — and
// an id can only name a log file in the profiles folder.
func TestWithPreviousRuns(t *testing.T) {
	server := t.TempDir()
	prof := filepath.Join(server, "profiles")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 13, 3, 0, 0, 0, time.Local)
	names := []string{}
	for i := 0; i < 8; i++ {
		stamp := base.Add(time.Duration(i) * time.Hour)
		n := "DayZServer_x64_" + stamp.Format("2006-01-02_15-04-05") + ".RPT"
		p := filepath.Join(prof, n)
		if err := os.WriteFile(p, []byte("run\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := stamp.Add(50 * time.Minute) // last write: near the run's end
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := os.WriteFile(filepath.Join(prof, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcs := WithPrevious(server, "profiles")
	var cur string
	var prev []Source
	for _, s := range srcs {
		switch {
		case s.ID == "rpt":
			cur = filepath.Base(s.Path)
		case s.Prev && s.Kind == "rpt":
			prev = append(prev, s)
		case s.Prev:
			t.Errorf("unexpected earlier run %+v", s)
		}
	}
	if cur != names[7] {
		t.Fatalf("newest RPT = %q, want %q", cur, names[7])
	}
	if len(prev) != PrevRuns {
		t.Fatalf("%d earlier runs, want %d", len(prev), PrevRuns)
	}
	for i, s := range prev {
		want := names[6-i]
		if s.ID != "rpt:"+want || !s.Exists {
			t.Errorf("earlier run %d = %+v, want %s", i, s, want)
		}
		if got := time.Unix(s.Started, 0); !got.Equal(base.Add(time.Duration(6-i) * time.Hour)) {
			t.Errorf("%s started %v", want, got)
		}
		if p := Resolve(server, "profiles", s.ID); p != filepath.Join(prof, want) {
			t.Errorf("Resolve(%s) = %q", s.ID, p)
		}
	}
	for _, id := range []string{
		"rpt:..\\..\\secret.RPT", "rpt:../secret.RPT", "rpt:notes.txt", "adm:" + names[0],
		"rpt:", "rpt:missing.RPT", "stdout:x",
	} {
		if p := Resolve(server, "profiles", id); p != "" {
			t.Errorf("Resolve(%q) = %q, want nothing", id, p)
		}
	}
	// The current sources are unchanged.
	if p := Resolve(server, "profiles", "rpt"); p != filepath.Join(prof, names[7]) {
		t.Errorf("Resolve(rpt) = %q", p)
	}
}
