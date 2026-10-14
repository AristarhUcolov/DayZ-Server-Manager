// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureBEConfigCreatesFile(t *testing.T) {
	dir := t.TempDir()
	changed, err := EnsureBEConfig(dir, "secret123", 2306, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected file to be created (changed=true)")
	}
	be := FindBEConfig(dir)
	if be == nil {
		t.Fatal("FindBEConfig returned nil after write")
	}
	if be.RConPassword != "secret123" {
		t.Errorf("RConPassword = %q, want secret123", be.RConPassword)
	}
	if be.RConPort != 2306 {
		t.Errorf("RConPort = %d, want 2306", be.RConPort)
	}
}

func TestEnsureBEConfigIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureBEConfig(dir, "pw", 2306, false); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureBEConfig(dir, "pw", 2306, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("second identical write should report no change")
	}
}

func TestEnsureBEConfigUpdatesPasswordPreservesPortAndExtras(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beserver_x64.cfg")
	// Pre-existing file with a custom port and an unrelated setting.
	if err := os.WriteFile(path, []byte("RConPassword old\nRConPort 2399\nMaxPing 250\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureBEConfig(dir, "newpass", 2306, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected change when password differs")
	}
	be := FindBEConfig(dir)
	if be.RConPassword != "newpass" {
		t.Errorf("password = %q, want newpass", be.RConPassword)
	}
	if be.RConPort != 2399 {
		t.Errorf("port = %d, want 2399 preserved (must not clobber existing)", be.RConPort)
	}
	data, _ := os.ReadFile(path)
	if want := "MaxPing 250"; !contains(string(data), want) {
		t.Errorf("unrelated setting %q was lost: %s", want, data)
	}
}

// An RCon port set explicitly in the panel must reach BattlEye even when the
// file already has one: the panel connects to the new port, so leaving the old
// one made every RCon command time out.
func TestEnsureBEConfigForcesExplicitPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beserver_x64.cfg")
	if err := os.WriteFile(path, []byte("RConPassword pw\nRConPort 2306\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureBEConfig(dir, "pw", 2310, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected a change when the explicit port differs")
	}
	if be := FindBEConfig(dir); be.RConPort != 2310 {
		t.Errorf("port = %d, want 2310", be.RConPort)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(strings.ToLower(string(data)), "rconport"); n != 1 {
		t.Errorf("RConPort appears %d times, want 1:\n%s", n, data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// BattlEye writes `Key<TAB>Value` at least as often as with a space. Splitting
// on a literal space made a tab-separated file read as "not configured", and
// saving then APPENDED a second RConPassword line — two conflicting
// credentials, while the panel reported success.
func TestBEConfigAcceptsTabs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "beserver_x64.cfg")
	os.WriteFile(p, []byte("RConPassword\tmysecret\r\nRConPort\t2306\r\nMaxPing\t350\r\n"), 0o644)

	cfg := parseBEConfig(p)
	if cfg == nil || cfg.RConPassword != "mysecret" {
		t.Fatalf("password = %+v, want mysecret", cfg)
	}
	if cfg.RConPort != 2306 {
		t.Errorf("port = %d, want 2306", cfg.RConPort)
	}

	if _, err := EnsureBEConfig(dir, "newpass", 2306, false); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	body := string(out)
	if n := strings.Count(strings.ToLower(body), "rconpassword"); n != 1 {
		t.Errorf("RConPassword appears %d times, want 1:\n%s", n, body)
	}
	if cfg2 := parseBEConfig(p); cfg2 == nil || cfg2.RConPassword != "newpass" {
		t.Errorf("password after write = %+v, want newpass\n%s", cfg2, body)
	}
}

// BattlEye renames beserver_x64.cfg to beserver_x64_active_<id>.cfg when the
// server starts; the newest such file holds the live RCon password.
func TestFindActiveBEConfig(t *testing.T) {
	dir := t.TempDir()
	if FindActiveBEConfig(dir) != nil {
		t.Fatal("found an active config in an empty folder")
	}
	write := func(name, body string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("beserver_x64.cfg", "RConPassword next\r\nRConPort 2306\r\n", 0)
	write("beserver_x64_active_1a2b3c.cfg", "RConPassword old\r\nRConPort 2306\r\n", 2*time.Hour)
	write("BEServer_x64_active_4d5e6f.cfg", "RConPassword live\r\nRConPort 2310\r\nRConIP 127.0.0.1\r\n", time.Hour)
	be := FindActiveBEConfig(dir)
	if be == nil || be.RConPassword != "live" || be.RConPort != 2310 {
		t.Fatalf("active = %+v, want the newest active file (live, 2310)", be)
	}
	if got := FindBEConfig(dir); got == nil || got.RConPassword != "next" {
		t.Fatalf("FindBEConfig = %+v, want beserver_x64.cfg", got)
	}
}
