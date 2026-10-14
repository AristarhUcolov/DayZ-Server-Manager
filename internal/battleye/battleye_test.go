// Copyright (c) 2026 Aristarh Ucolov.
package battleye

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The list offers what DayZ's BattlEye uses — bans.txt and its settings file —
// plus .txt files that exist, never Arma-era placeholders; the settings come
// back with the RCon password masked and cannot be written.
func TestListReadWrite(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("BEServer_x64.cfg", "RConPassword hunter2\r\nRConPort 2306\r\n")
	write("beserver_x64_active_1a2b.cfg", "RConPassword live\n")
	write("custom.txt", "x\n")
	write("BEServer_x64.dll", "MZ")

	kinds := map[string]string{}
	for _, f := range List(dir) {
		kinds[f.Name] = f.Kind
	}
	want := map[string]string{"bans.txt": "bans", "BEServer_x64.cfg": "settings", "beserver_x64_active_1a2b.cfg": "active", "custom.txt": "file"}
	if len(kinds) != len(want) {
		t.Fatalf("listed %v, want %v", kinds, want)
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("%s: kind %q, want %q", name, kinds[name], kind)
		}
	}

	got, err := Read(dir, "BEServer_x64.cfg")
	if err != nil || strings.Contains(got, "hunter2") || !strings.Contains(got, "RConPort 2306") {
		t.Fatalf("settings read: %q, %v", got, err)
	}
	if err := Write(dir, "BEServer_x64.cfg", "RConPassword x"); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("writing the settings must be refused, got %v", err)
	}
	for _, bad := range []string{"../bans.txt", `..\bans.txt`, "BEServer_x64.dll", ".hidden.txt"} {
		if _, err := Read(dir, bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if err := Write(dir, "custom.txt", "y\n"); err != nil {
		t.Fatal(err)
	}
}
