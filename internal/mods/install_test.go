// Copyright (c) 2026 Aristarh Ucolov.
package mods

import (
	"os"
	"path/filepath"
	"testing"
)

func mkFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallStagesAndReinstallDropsStaleFiles(t *testing.T) {
	src, srv := t.TempDir(), t.TempDir()
	mkFile(t, filepath.Join(src, "@Test", "addons", "a.pbo"), "v1")
	mkFile(t, filepath.Join(src, "@Test", "addons", "old.pbo"), "v1-only")
	if err := Install(srv, src, "@Test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srv, "@Test", "addons", "old.pbo")); err != nil {
		t.Fatalf("first install incomplete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srv, "@Test.installing")); !os.IsNotExist(err) {
		t.Fatal("staging folder left behind")
	}
	// New version drops old.pbo; reinstalling must not keep it.
	_ = os.Remove(filepath.Join(src, "@Test", "addons", "old.pbo"))
	mkFile(t, filepath.Join(src, "@Test", "addons", "a.pbo"), "v2")
	if err := Install(srv, src, "@Test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srv, "@Test", "addons", "old.pbo")); !os.IsNotExist(err) {
		t.Fatal("stale file from the previous version survived a reinstall")
	}
	if b, _ := os.ReadFile(filepath.Join(srv, "@Test", "addons", "a.pbo")); string(b) != "v2" {
		t.Fatalf("not updated: %q", b)
	}
	// Leftover staging folders (manager killed mid-copy) are not mods.
	for _, n := range []string{"@Test.updating", "@Test.old", "@Other.installing"} {
		if err := os.MkdirAll(filepath.Join(srv, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	list, err := List(srv, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if isStagingDir(m.Name) {
			t.Fatalf("staging folder listed as a mod: %s", m.Name)
		}
	}
}
