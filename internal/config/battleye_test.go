// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureBEConfigCreatesFile(t *testing.T) {
	dir := t.TempDir()
	changed, err := EnsureBEConfig(dir, "secret123", 2306)
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
	if _, err := EnsureBEConfig(dir, "pw", 2306); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureBEConfig(dir, "pw", 2306)
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
	changed, err := EnsureBEConfig(dir, "newpass", 2306)
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

	if _, err := EnsureBEConfig(dir, "newpass", 2306); err != nil {
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
