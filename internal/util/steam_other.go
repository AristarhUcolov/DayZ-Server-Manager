// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

import (
	"os"
	"path/filepath"
)

// steamRoots finds Steam on Linux: the native install, the Flatpak and Snap
// packages, and SteamCMD's usual home folders.
func steamRoots() []string {
	home, _ := os.UserHomeDir()
	var roots []string
	for _, p := range []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "root"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
		filepath.Join(home, "Steam"),
		filepath.Join(home, "steamcmd"),
		"/opt/steamcmd",
		"/usr/games/steamcmd",
	} {
		if st, err := os.Stat(filepath.Join(p, "steamapps")); err == nil && st.IsDir() {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				p = r
			}
			roots = append(roots, p)
		}
	}
	return roots
}
