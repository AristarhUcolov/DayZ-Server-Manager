// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"dayzmanager/internal/config"
	"dayzmanager/internal/server"
)

// A failed save must leave the live config untouched: committing first made
// the panel show unsaved values while the scheduler kept the old snapshot, and
// the change vanished on the next manager start.
func TestMutateConfigFailedSaveKeepsOldConfig(t *testing.T) {
	dir := t.TempDir()
	// The parent of manager.json is a file, so SaveManager cannot write.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Manager{Language: "en", ServerPort: 2302}
	a := &App{
		Config:     cfg,
		configPath: filepath.Join(blocker, "manager.json"),
		Server:     server.NewController(dir, cfg, log.New(io.Discard, "", 0)),
	}
	err := a.MutateConfig(func(m *config.Manager) error {
		m.Language = "ru"
		return nil
	})
	if !errors.Is(err, ErrConfigSave) {
		t.Fatalf("want ErrConfigSave, got %v", err)
	}
	if got := a.Cfg().Language; got != "en" {
		t.Fatalf("unsaved change is live: language = %q, want en", got)
	}
}
