// Copyright (c) 2026 Aristarh Ucolov.
package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withRPT(t *testing.T, body string) (serverDir, profilesDir string) {
	t.Helper()
	serverDir = t.TempDir()
	profilesDir = filepath.Join(serverDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(profilesDir, "DayZServer_x64_2026-07-22_04-00-00.RPT")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return serverDir, "profiles"
}

func codes(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestDiagnoseFindsMissingMod(t *testing.T) {
	sd, pd := withRPT(t, `
11:00:01 Starting mission:
11:00:02 Warning Message: Cannot open file 'P:\DayZServer\@CF\addons\cf.pbo'
11:00:02 Application terminated intentionally
`)
	got := Diagnose(sd, pd)
	if !has(codes(got), "mod_missing") {
		t.Fatalf("missing mod not detected, got %v", codes(got))
	}
	f := got[0]
	if f.Severity != SevFatal {
		t.Errorf("severity = %s, want fatal", f.Severity)
	}
	if !strings.Contains(f.Detail, "@CF") {
		t.Errorf("detail should name the mod, got %q", f.Detail)
	}
	if !strings.Contains(f.Line, "Cannot open file") {
		t.Errorf("the raw evidence line is missing: %q", f.Line)
	}
}

func TestDiagnoseFindsPortAndSignature(t *testing.T) {
	sd, pd := withRPT(t, `
10:00:00 Bad version wrong signature for file mymod.pbo
10:00:01 Network: bind failed: address already in use
`)
	got := codes(Diagnose(sd, pd))
	for _, want := range []string{"bad_signature", "port_busy"} {
		if !has(got, want) {
			t.Errorf("%s not detected, got %v", want, got)
		}
	}
}

// A rule that fires on a healthy server is worse than no rule: the page would
// cry wolf and admins would stop reading it.
func TestDiagnoseIsQuietOnAHealthyLog(t *testing.T) {
	sd, pd := withRPT(t, `
11:00:00 DayZ Server: 1.27
11:00:01 Loading mission
11:00:02 Warning Message: No entry 'bin\config.bin/CfgVehicles.SomeProp'
11:00:03 Mission read.
11:00:04 Connecting to BattlEye
11:00:05 Player Survivor connected
`)
	if got := Diagnose(sd, pd); len(got) != 0 {
		t.Errorf("false positives on a healthy log: %v", got)
	}
}

func TestDiagnoseDeduplicates(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString(`Warning Message: Cannot open file 'X:\srv\@Expansion\addons\core.pbo'` + "\n")
	}
	sd, pd := withRPT(t, b.String())
	got := Diagnose(sd, pd)
	n := 0
	for _, f := range got {
		if f.Code == "mod_missing" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("mod_missing reported %d times, want 1", n)
	}
}

func TestDiagnoseHandlesNoLogs(t *testing.T) {
	dir := t.TempDir()
	if got := Diagnose(dir, "profiles"); len(got) != 0 {
		t.Errorf("expected no findings with no logs, got %v", got)
	}
}
