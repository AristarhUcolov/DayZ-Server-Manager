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
	"runtime"
	"strings"
)

type DayZCandidate struct {
	Path       string `json:"path"`
	HasWorkshop bool  `json:"hasWorkshop"`
}

func FindDayZInstalls() []DayZCandidate {
	if runtime.GOOS != "windows" {
		return nil
	}
	roots := steamRoots()
	seen := map[string]bool{}
	var out []DayZCandidate
	for _, r := range roots {
		for _, lib := range libraryFolders(r) {
			p := filepath.Join(lib, "steamapps", "common", "DayZ")
			if seen[strings.ToLower(p)] {
				continue
			}
			seen[strings.ToLower(p)] = true
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				continue
			}
			ws, _ := os.Stat(filepath.Join(p, "!Workshop"))
			out = append(out, DayZCandidate{
				Path:       p,
				HasWorkshop: ws != nil && ws.IsDir(),
			})
		}
	}
	return out
}

func steamRoots() []string {
	var roots []string
	candidates := []string{
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
		os.Getenv("ProgramFiles(x86)") + `\Steam`,
		os.Getenv("ProgramFiles") + `\Steam`,
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "steam.exe")); err == nil {
			roots = append(roots, c)
		}
	}
	return roots
}

// reLibPath extracts every "path" entry from libraryfolders.vdf. The vdf
// format is braces+quotes so a regex on "path" lines is tolerant across
// minor schema changes (Steam has revised this file shape several times).
var reLibPath = regexp.MustCompile(`"path"\s*"([^"]+)"`)

func libraryFolders(steamRoot string) []string {
	libs := []string{steamRoot}
	data, err := os.ReadFile(filepath.Join(steamRoot, "config", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, m := range reLibPath.FindAllStringSubmatch(string(data), -1) {
		p := strings.ReplaceAll(m[1], `\\`, `\`)
		libs = append(libs, p)
	}
	return libs
}
