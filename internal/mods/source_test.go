// Copyright (c) 2026 Aristarh Ucolov.
package mods

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSteamCMDSourceLayout: a SteamCMD workshop/content/221100 folder (numeric
// ids) is a valid mod source — hosts without the game client get their mods
// this way. Names come from meta.cpp; non-mod folders are ignored.
func TestSteamCMDSourceLayout(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "1559212036", "meta.cpp"), "protocol = 1;\npublishedid = 1559212036;\nname = \"CF\";\ntimestamp = 100;\n")
	writeFile(t, filepath.Join(src, "1559212036", "addons", "cf.pbo"), "x")
	writeFile(t, filepath.Join(src, "1559212036", "keys", "cf.bikey"), "k")
	writeFile(t, filepath.Join(src, "@Other", "addons", "o.pbo"), "x")
	writeFile(t, filepath.Join(src, "777", "readme.txt"), "not a mod")

	got := scanSource(src)
	if len(got) != 2 || got["@CF"] == "" || got["@Other"] == "" {
		t.Fatalf("scanSource = %v, want @CF and @Other", got)
	}
	info := InspectSource(src)
	if !info.Exists || info.Mods != 2 || info.SteamCMD != 1 {
		t.Errorf("InspectSource = %+v", info)
	}

	server := t.TempDir()
	if err := Install(server, src, "@CF"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(server, "@CF", "addons", "cf.pbo")); err != nil {
		t.Fatalf("mod not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(server, "keys", "cf.bikey")); err != nil {
		t.Fatalf("key not synced: %v", err)
	}

	// Same timestamp → up to date; a newer Workshop publish → update.
	list, _ := List(server, src)
	for _, m := range list {
		if m.Name == "@CF" && m.UpdateAvailable {
			t.Fatalf("fresh install reported an update")
		}
	}
	writeFile(t, filepath.Join(src, "1559212036", "meta.cpp"), "publishedid = 1559212036;\nname = \"CF\";\ntimestamp = 200;\n")
	list, _ = List(server, src)
	found := false
	for _, m := range list {
		if m.Name == "@CF" {
			found = true
			if !m.UpdateAvailable {
				t.Fatalf("newer meta.cpp timestamp not detected as an update")
			}
		}
	}
	if !found {
		t.Fatal("@CF missing from List")
	}
	if err := Install(server, "", "@CF"); err != ErrNoSource {
		t.Errorf("install without a source: %v, want ErrNoSource", err)
	}
}

// TestNormalizeForLinux: upper-case addons/keys folders and pbo names are
// lower-cased — the Linux server ignores (or crashes on) anything else.
func TestNormalizeForLinux(t *testing.T) {
	mod := filepath.Join(t.TempDir(), "@Mod")
	writeFile(t, filepath.Join(mod, "Addons", "My_Mod.PBO"), "x")
	writeFile(t, filepath.Join(mod, "Addons", "My_Mod.PBO.mod.bisign"), "x")
	writeFile(t, filepath.Join(mod, "Keys", "Mod.bikey"), "x")
	writeFile(t, filepath.Join(mod, "meta.cpp"), "name = \"Mod\";")

	if probs := CaseProblems(mod); len(probs) < 3 {
		t.Fatalf("CaseProblems = %v, want the Addons, Keys folders and pbo files", probs)
	}
	n, err := NormalizeForLinux(mod)
	if err != nil {
		t.Fatal(err)
	}
	if n < 4 {
		t.Errorf("renamed %d entries, want at least 4", n)
	}
	names := func(dir string) map[string]bool {
		out := map[string]bool{}
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			out[e.Name()] = true
		}
		return out
	}
	top := names(mod)
	if !top["addons"] || !top["keys"] || top["Addons"] || top["Keys"] {
		t.Errorf("top level after normalize: %v", top)
	}
	add := names(filepath.Join(mod, "addons"))
	if !add["my_mod.pbo"] || !add["my_mod.pbo.mod.bisign"] {
		t.Errorf("addons after normalize: %v", add)
	}
	if probs := CaseProblems(mod); len(probs) != 0 {
		t.Errorf("problems left after normalize: %v", probs)
	}
}
