// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// steamRoots finds Steam installs and bare SteamLibrary folders: the path
// Steam records in the registry (it is often on D:), the default folders, and
// a SteamLibrary / Steam folder at the root of every drive.
func steamRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if st, err := os.Stat(filepath.Join(p, "steamapps")); err == nil && st.IsDir() {
			roots = append(roots, p)
		}
	}
	for _, k := range []struct {
		root registry.Key
		path string
		name string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		if key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE); err == nil {
			if v, _, err := key.GetStringValue(k.name); err == nil {
				add(v)
			}
			key.Close()
		}
	}
	add(os.Getenv("ProgramFiles(x86)") + `\Steam`)
	add(os.Getenv("ProgramFiles") + `\Steam`)
	for d := 'C'; d <= 'Z'; d++ {
		drive := string(d) + `:\`
		for _, name := range []string{"SteamLibrary", "Steam", `Games\Steam`, `Games\SteamLibrary`} {
			add(drive + name)
		}
	}
	return roots
}
