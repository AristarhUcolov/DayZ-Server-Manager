// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package mods

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mklinkJ makes a directory junction, the way DayZ Launcher links
// !Workshop\@Mod to the Workshop download (no privilege needed, unlike a
// symlink).
func mklinkJ(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

// A mod reached through a junction is listed with its real size and keys and
// installs whole — before, Go 1.23 read the junction as a file: 0 B, no keys,
// and "copy mod: read …: Incorrect function". A folder linked inside the mod
// is copied too.
func TestJunctionedWorkshopModInstalls(t *testing.T) {
	root := t.TempDir()
	server := filepath.Join(root, "server")
	vanilla := filepath.Join(root, "DayZ")
	workshop := filepath.Join(vanilla, "!Workshop")
	real := filepath.Join(root, "content", "2670506982")
	extra := filepath.Join(root, "elsewhere", "data")
	for _, d := range []string{server, workshop, filepath.Join(real, "addons"), filepath.Join(real, "keys"), extra} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(real, "addons", "fort.pbo"): "pbo-data",
		filepath.Join(real, "keys", "fort.bikey"): "key",
		filepath.Join(real, "meta.cpp"):           "publishedid = 2670506982;\nname = \"Building Fortifications\";\n",
		filepath.Join(extra, "readme.txt"):        "linked",
	}
	for p, data := range files {
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mklinkJ(t, filepath.Join(workshop, "@Building Fortifications"), real)
	mklinkJ(t, filepath.Join(real, "docs"), extra)

	list, err := List(server, workshop)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].KeyCount != 1 || list[0].SizeBytes == 0 {
		t.Fatalf("listed %+v", list)
	}
	if err := Install(server, workshop, "@Building Fortifications"); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, rel := range []string{"addons/fort.pbo", "meta.cpp", "docs/readme.txt"} {
		if _, err := os.Stat(filepath.Join(server, "@Building Fortifications", filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s not installed: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(server, "keys", "fort.bikey")); err != nil {
		t.Errorf("key not synced: %v", err)
	}
}
