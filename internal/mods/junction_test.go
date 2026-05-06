package mods

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestList_FollowsSymlinkedWorkshop reproduces the bug where DayZ Launcher
// creates @Mod entries under !Workshop as Windows junctions / symlinks.
// Before the fix, os.DirEntry.IsDir() returned false for the symlink and
// every mod was silently skipped, leaving the Sync-all picker empty.
//
// On non-Windows we use os.Symlink to stand in for the junction; the same
// is-dir-or-link logic applies in both cases.
func TestList_FollowsSymlinkedWorkshop(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Symlink on Windows requires SeCreateSymbolicLinkPrivilege which
		// most test runners don't have. The Linux/macOS path exercises the
		// same code we care about (DirEntry.Type() == ModeSymlink).
		t.Skip("requires symlink privileges on Windows; covered on POSIX runners")
	}

	root := t.TempDir()
	server := filepath.Join(root, "server")
	vanilla := filepath.Join(root, "DayZ")
	workshop := filepath.Join(vanilla, "!Workshop")
	realMod := filepath.Join(root, "real", "@CF")
	for _, d := range []string{server, workshop, realMod} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Plant a key so countKeys / dirSize have something to find inside the
	// linked target.
	keysDir := filepath.Join(realMod, "keys")
	if err := os.MkdirAll(keysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keysDir, "cf.bikey"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}

	// !Workshop/@CF → real/@CF (the junction-equivalent).
	if err := os.Symlink(realMod, filepath.Join(workshop, "@CF")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := List(server, vanilla)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 mod, got %d: %+v", len(got), got)
	}
	m := got[0]
	if m.Name != "@CF" {
		t.Errorf("name = %q, want @CF", m.Name)
	}
	if !m.AvailableInWorkshop {
		t.Errorf("AvailableInWorkshop = false, want true")
	}
	if m.KeyCount != 1 {
		t.Errorf("KeyCount = %d, want 1 (dirSize/keys must follow the link)", m.KeyCount)
	}
}
