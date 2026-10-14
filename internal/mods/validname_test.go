// Copyright (c) 2026 Aristarh Ucolov.

package mods

import (
	"os"
	"path/filepath"
	"testing"
)

// A mod name is one folder name: "@/../.." used to make Uninstall remove the
// folder ABOVE the server directory.
func TestModNameCannotLeaveTheServerFolder(t *testing.T) {
	for _, bad := range []string{"@/../..", `@\..\..`, "@..\\x", "@a/b", "@", "Mod", "@x:y", "@trailing.", "@nul\x00"} {
		if ValidName(bad) == nil {
			t.Errorf("ValidName(%q) accepted", bad)
		}
	}
	for _, good := range []string{"@CF", "@DayZ-Expansion-Core", "@Mod.With.Dots", "@Name With Spaces"} {
		if err := ValidName(good); err != nil {
			t.Errorf("ValidName(%q) = %v", good, err)
		}
	}

	root := t.TempDir()
	server := filepath.Join(root, "server")
	sibling := filepath.Join(root, "keep-me")
	for _, d := range []string{server, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := Uninstall(server, "@/../.."); err == nil {
		t.Fatal("Uninstall accepted a traversal name")
	}
	for _, d := range []string{server, sibling} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s was deleted: %v", d, err)
		}
	}
}
