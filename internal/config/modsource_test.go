// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"path/filepath"
	"testing"
)

// Every mod source mode installs from the right folder; the Steam Workshop
// source reads the panel's SteamCMD download folder.
func TestModSourceDir(t *testing.T) {
	cases := []struct {
		m    Manager
		mode string
		dir  string
	}{
		{Manager{VanillaDayZPath: `D:\DayZ`}, ModSourceClient, filepath.Join(`D:\DayZ`, "!Workshop")},
		{Manager{ModSource: ModSourceFolder, ModsFolderPath: `E:\mods`}, ModSourceFolder, `E:\mods`},
		{Manager{ModSource: ModSourceServer, ModsFolderPath: `E:\mods`}, ModSourceServer, ""},
		{Manager{ModSource: ModSourceSteamCMD, SteamCMDContent: `C:\srv\.dayz-manager\steamcmd\steamapps\workshop\content\221100`},
			ModSourceSteamCMD, `C:\srv\.dayz-manager\steamcmd\steamapps\workshop\content\221100`},
		{Manager{ModSource: "nonsense"}, ModSourceClient, ""},
	}
	for _, c := range cases {
		if got := c.m.EffectiveModSource(); got != c.mode {
			t.Errorf("%+v: mode %q, want %q", c.m.ModSource, got, c.mode)
		}
		if got := c.m.ModSourceDir(); got != c.dir {
			t.Errorf("%+v: dir %q, want %q", c.m.ModSource, got, c.dir)
		}
	}
}
