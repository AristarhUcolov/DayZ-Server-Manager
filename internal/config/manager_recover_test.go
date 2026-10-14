// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManagerRecoversFromCorruptFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manager.json")
	if err := os.WriteFile(p, []byte(`{"language": "ru", broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadManager(p)
	var rec *RecoveredError
	if !errors.As(err, &rec) || c == nil {
		t.Fatalf("want RecoveredError with defaults, got %v / %v", c, err)
	}
	if b, _ := os.ReadFile(rec.Backup); string(b) != `{"language": "ru", broken` {
		t.Fatal("broken file not kept for recovery")
	}
	// The fresh file is valid now.
	if _, err := LoadManager(p); err != nil {
		t.Fatalf("reload after recovery: %v", err)
	}
}

// Notepad's "UTF-8 with BOM" is valid JSON to a human; it must load, not be set
// aside as corrupt with every setting reset to defaults.
func TestLoadManagerAcceptsBOM(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manager.json")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"firstRunDone": true, "language": "ru"}`)...)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadManager(p)
	if err != nil {
		t.Fatalf("BOM file treated as broken: %v", err)
	}
	if !c.FirstRunDone || c.Language != "ru" {
		t.Fatalf("settings not loaded: firstRunDone=%v language=%q", c.FirstRunDone, c.Language)
	}
}
