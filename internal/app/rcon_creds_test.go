// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dayzmanager/internal/config"
)

// While the server runs, RCon logs in with what BattlEye loaded at start
// (beserver_x64_active_*.cfg) — a password saved in the panel since then is
// only tried after it. Stopped, the panel's and the file's passwords are all
// there is.
func TestRConCredsPreferTheLiveBattlEyeFile(t *testing.T) {
	a := newPrimary(t)
	if err := a.MutateConfig(func(c *config.Manager) error { c.RConPassword = "panel"; return nil }); err != nil {
		t.Fatal(err)
	}
	be := filepath.Join(a.ServerDir, "battleye")
	if err := os.MkdirAll(be, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// SyncBEConfig may have written beserver_x64.cfg at boot: set it explicitly.
	write(filepath.Join(be, "beserver_x64.cfg"), "RConPassword next\r\nRConPort 2306\r\n")
	active := filepath.Join(be, "beserver_x64_active_5f3a.cfg")
	write(active, "RConPassword live\r\nRConPort 2310\r\nRConIP 0.0.0.0\r\n")

	c := a.rconCredsFor(true)
	if c.Host != "127.0.0.1" || c.Port != 2310 {
		t.Fatalf("running: %s:%d, want 127.0.0.1:2310 (the live file's port)", c.Host, c.Port)
	}
	if want := []string{"live", "panel", "next"}; !reflect.DeepEqual(c.Passwords, want) {
		t.Fatalf("running: passwords %q, want %q", c.Passwords, want)
	}

	c = a.rconCredsFor(false)
	if c.Port != 2306 || !reflect.DeepEqual(c.Passwords, []string{"panel", "next"}) {
		t.Fatalf("stopped: %d %q, want 2306 [panel next]", c.Port, c.Passwords)
	}

	// Started without -BEpath, BattlEye keeps its files under the profiles
	// folder.
	if err := os.Remove(active); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(a.ServerDir, "profiles", "BattlEye", "BEServer_x64_active_77.cfg"), "RConPassword prof\r\nRConPort 2320\r\n")
	c = a.rconCredsFor(true)
	if c.Port != 2320 || len(c.Passwords) == 0 || c.Passwords[0] != "prof" {
		t.Fatalf("profiles/BattlEye: %d %q", c.Port, c.Passwords)
	}

	// A config save re-reads them at once (status checks re-read every few
	// seconds, every dial re-reads).
	a.ApplyRConConfig()
	if !a.RCon.Configured() {
		t.Fatal("RCon not configured although passwords are known")
	}
}
