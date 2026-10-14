// Copyright (c) 2026 Aristarh Ucolov.
//
// Best-effort scan for the DayZ client install across Steam libraries. We
// read libraryfolders.vdf from every plausible Steam location, then look for
// a DayZ folder with a !Workshop subfolder (the marker we actually need).
// Users can still type a path manually if detection misses.
package util

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DayZCandidate struct {
	Path        string `json:"path"`
	HasWorkshop bool   `json:"hasWorkshop"`
}

// FindDayZInstalls lists DayZ client installs across every Steam library.
func FindDayZInstalls() []DayZCandidate {
	seen := map[string]bool{}
	var out []DayZCandidate
	for _, lib := range allLibraries() {
		p := filepath.Join(lib, "steamapps", "common", "DayZ")
		if seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		ws, _ := os.Stat(filepath.Join(p, "!Workshop"))
		out = append(out, DayZCandidate{Path: p, HasWorkshop: ws != nil && ws.IsDir()})
	}
	return out
}

// FindWorkshopContent lists DayZ Workshop download folders
// (steamapps/workshop/content/221100) — what SteamCMD fills on a host without
// the game, usable as a "mods folder" source.
func FindWorkshopContent() []string {
	seen := map[string]bool{}
	var out []string
	for _, lib := range allLibraries() {
		p := filepath.Join(lib, "steamapps", "workshop", "content", "221100")
		if seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func allLibraries() []string {
	var libs []string
	seen := map[string]bool{}
	for _, r := range steamRoots() {
		for _, lib := range libraryFolders(r) {
			k := strings.ToLower(filepath.Clean(lib))
			if !seen[k] {
				seen[k] = true
				libs = append(libs, lib)
			}
		}
	}
	return libs
}

// reLibPath extracts every "path" entry from libraryfolders.vdf. The vdf
// format is braces+quotes so a regex on "path" lines is tolerant across
// minor schema changes (Steam has revised this file shape several times).
var reLibPath = regexp.MustCompile(`"path"\s*"([^"]+)"`)

func libraryFolders(steamRoot string) []string {
	libs := []string{steamRoot}
	for _, rel := range []string{filepath.Join("config", "libraryfolders.vdf"), filepath.Join("steamapps", "libraryfolders.vdf")} {
		data, err := os.ReadFile(filepath.Join(steamRoot, rel))
		if err != nil {
			continue
		}
		for _, m := range reLibPath.FindAllStringSubmatch(string(data), -1) {
			libs = append(libs, strings.ReplaceAll(m[1], `\`, `\`))
		}
	}
	return libs
}
