// Copyright (c) 2026 Aristarh Ucolov.
//
// Steam Workshop mod sync for DayZ.
//
// On disk the *client* DayZ install contains:
//   <DayZ>/!Workshop/@ModName/...            ← here mods are downloaded by Steam
//   <DayZ>/!Workshop/@ModName/keys/*.bikey   ← BattlEye signing keys
//
// The dedicated server expects mods sitting at:
//   <DayZServer>/@ModName/...                ← loaded via -mod=@ModName;@Other
//   <DayZServer>/keys/*.bikey                ← combined keys folder
//
// This package implements the install / uninstall / update / sync-keys flow
// so users don't have to copy things manually.
package mods

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/util"
)

// Logger is set by the app wiring so copy failures end up in manager.log
// instead of silently disappearing. Nil is safe — callers fall back to
// discard when it is unset.
var Logger *log.Logger

type Mod struct {
	Name                string    `json:"name"` // "@CF"
	InstalledInServer   bool      `json:"installedInServer"`
	AvailableInWorkshop bool      `json:"availableInWorkshop"`
	WorkshopPath        string    `json:"workshopPath,omitempty"`
	ServerPath          string    `json:"serverPath,omitempty"`
	SizeBytes           int64     `json:"sizeBytes"`
	KeyCount            int       `json:"keyCount"`
	ServerModifiedAt    time.Time `json:"serverModifiedAt,omitempty"`
	WorkshopModifiedAt  time.Time `json:"workshopModifiedAt,omitempty"`
	UpdateAvailable     bool      `json:"updateAvailable"`
	DisplayName         string    `json:"displayName,omitempty"`
	PublishedID         string    `json:"publishedId,omitempty"`
}

// List returns the union of mods found in the client Workshop folder and the
// mods already installed in the server directory.
func List(serverDir, vanillaDayZPath string) ([]Mod, error) {
	byName := map[string]*Mod{}

	// Server-side @Mod directories.
	srvEntries, _ := os.ReadDir(serverDir)
	for _, e := range srvEntries {
		if !strings.HasPrefix(e.Name(), "@") {
			continue
		}
		p := filepath.Join(serverDir, e.Name())
		if !isDirOrJunction(e, p) {
			continue
		}
		byName[e.Name()] = &Mod{
			Name:              e.Name(),
			InstalledInServer: true,
			ServerPath:        p,
			SizeBytes:         dirSize(p),
			KeyCount:          countKeys(p),
			ServerModifiedAt:  newestMTime(p),
		}
	}

	// Workshop-side mods (from the user's main DayZ install).
	// Steam / DayZ Launcher creates @Mod entries as Windows junctions
	// pointing at steamapps/workshop/content/221100/<id>/, so we have to
	// follow them — DirEntry.IsDir() returns false on a symlink.
	if vanillaDayZPath != "" {
		ws := filepath.Join(vanillaDayZPath, "!Workshop")
		wsEntries, _ := os.ReadDir(ws)
		for _, e := range wsEntries {
			if !strings.HasPrefix(e.Name(), "@") {
				continue
			}
			p := filepath.Join(ws, e.Name())
			if !isDirOrJunction(e, p) {
				continue
			}
			m := byName[e.Name()]
			if m == nil {
				m = &Mod{Name: e.Name()}
				byName[e.Name()] = m
			}
			m.AvailableInWorkshop = true
			m.WorkshopPath = p
			m.WorkshopModifiedAt = newestMTime(p)
			if !m.InstalledInServer {
				m.SizeBytes = dirSize(p)
				m.KeyCount = countKeys(p)
			}
		}
	}

	out := make([]Mod, 0, len(byName))
	for _, m := range byName {
		if m.InstalledInServer && m.AvailableInWorkshop {
			m.UpdateAvailable = m.WorkshopModifiedAt.After(m.ServerModifiedAt.Add(2 * time.Second))
		}
		// meta.cpp lives in the mod root; prefer the workshop copy so the
		// server-side mod folder can be stripped without losing metadata.
		meta := readMeta(m.WorkshopPath)
		if meta.Name == "" && meta.PublishedID == "" {
			meta = readMeta(m.ServerPath)
		}
		m.DisplayName = meta.Name
		m.PublishedID = meta.PublishedID
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Install copies @ModName from the client Workshop folder into the server
// directory and merges its .bikey files into <serverDir>/keys.
func Install(serverDir, vanillaDayZPath, modName string) error {
	if !strings.HasPrefix(modName, "@") {
		return fmt.Errorf("mod name must start with '@'")
	}
	src := filepath.Join(vanillaDayZPath, "!Workshop", modName)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return fmt.Errorf("mod not found in Workshop: %s", src)
	}
	if err := checkDiskSpace(serverDir, src); err != nil {
		return err
	}
	dst := filepath.Join(serverDir, modName)
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("copy mod: %w", err)
	}
	if err := SyncKeys(serverDir, []string{modName}); err != nil {
		return fmt.Errorf("sync keys: %w", err)
	}
	return nil
}

// checkDiskSpace fails fast if the target volume looks too small for a mod
// copy. We require free space ≥ 1.2× the source size — a 20% headroom covers
// filesystem overhead. Unknown disk info (non-Windows) is permissive.
func checkDiskSpace(serverDir, src string) error {
	size := dirSize(src)
	if size <= 0 {
		return nil
	}
	free, _ := util.DiskFree(serverDir)
	if free == 0 {
		return nil // unknown / non-Windows
	}
	required := uint64(float64(size) * 1.2)
	if free < required {
		return fmt.Errorf("not enough free space on target disk: need ~%d MB, have %d MB",
			required/1024/1024, free/1024/1024)
	}
	return nil
}

// Update refreshes an already-installed mod from the Workshop copy.
// It copies into a staging directory first, then atomically swaps, so a
// half-broken update never leaves the server in a corrupt state.
func Update(serverDir, vanillaDayZPath, modName string) error {
	if !strings.HasPrefix(modName, "@") {
		return fmt.Errorf("mod name must start with '@'")
	}
	src := filepath.Join(vanillaDayZPath, "!Workshop", modName)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return fmt.Errorf("mod not found in Workshop: %s", src)
	}
	dst := filepath.Join(serverDir, modName)
	staging := dst + ".updating"
	_ = os.RemoveAll(staging)

	if err := copyTree(src, staging); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("copy mod into staging: %w", err)
	}
	backup := dst + ".old"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backup); err != nil {
			_ = os.RemoveAll(staging)
			return fmt.Errorf("move old mod aside: %w", err)
		}
	}
	if err := os.Rename(staging, dst); err != nil {
		// try to restore old
		_ = os.Rename(backup, dst)
		return fmt.Errorf("swap in updated mod: %w", err)
	}
	_ = os.RemoveAll(backup)

	// Idempotent — this re-pushes all installed mods' keys into /keys.
	return SyncKeys(serverDir, nil)
}

// UpdateAll updates every installed mod that has a fresher Workshop copy.
// Returns the list of mods it actually touched.
func UpdateAll(serverDir, vanillaDayZPath string) ([]string, error) {
	list, err := List(serverDir, vanillaDayZPath)
	if err != nil {
		return nil, err
	}
	var updated []string
	for _, m := range list {
		if !m.UpdateAvailable {
			continue
		}
		if err := Update(serverDir, vanillaDayZPath, m.Name); err != nil {
			return updated, fmt.Errorf("update %s: %w", m.Name, err)
		}
		updated = append(updated, m.Name)
	}
	return updated, nil
}

// SyncAllResult describes what SyncAll did so the UI can show a detailed toast
// instead of a silent "done".
type SyncAllResult struct {
	Installed   []string `json:"installed"`
	Updated     []string `json:"updated"`
	Skipped     []string `json:"skipped"` // already up to date
	WorkshopDir string   `json:"workshopDir"`
	Total       int      `json:"total"`         // mods seen in !Workshop
	NotInstalled int     `json:"notInstalled"`  // (before sync) waiting for install
	OutOfDate    int     `json:"outOfDate"`     // (before sync) update-available count
}

// SyncAll brings the server directory in line with the client !Workshop:
// every Workshop mod missing on the server is installed, every outdated one
// is updated. Nothing is removed — if a mod is only present server-side
// (user copied manually, or Workshop version was unsubscribed) it stays.
// Keys are synced once at the end. This is the one-click "match Workshop"
// the users asked for instead of tracking each mod manually.
func SyncAll(serverDir, vanillaDayZPath string, only []string) (*SyncAllResult, error) {
	if vanillaDayZPath == "" {
		return nil, ErrNoVanillaPath
	}
	list, err := List(serverDir, vanillaDayZPath)
	if err != nil {
		return nil, err
	}
	filter := map[string]bool{}
	for _, n := range only {
		filter[n] = true
	}
	res := &SyncAllResult{
		WorkshopDir: filepath.Join(vanillaDayZPath, "!Workshop"),
	}
	for _, m := range list {
		if m.AvailableInWorkshop {
			res.Total++
			if !m.InstalledInServer {
				res.NotInstalled++
			} else if m.UpdateAvailable {
				res.OutOfDate++
			}
		}
		if len(filter) > 0 && !filter[m.Name] {
			continue
		}
		if !m.AvailableInWorkshop {
			continue
		}
		switch {
		case !m.InstalledInServer:
			if err := Install(serverDir, vanillaDayZPath, m.Name); err != nil {
				return res, fmt.Errorf("install %s: %w", m.Name, err)
			}
			res.Installed = append(res.Installed, m.Name)
		case m.UpdateAvailable:
			if err := Update(serverDir, vanillaDayZPath, m.Name); err != nil {
				return res, fmt.Errorf("update %s: %w", m.Name, err)
			}
			res.Updated = append(res.Updated, m.Name)
		default:
			res.Skipped = append(res.Skipped, m.Name)
		}
	}
	// One final key sync catches any bikeys that may have moved between
	// subfolders during the update.
	_ = SyncKeys(serverDir, nil)
	return res, nil
}

// Uninstall removes @ModName from the server directory. Keys are removed from
// <serverDir>/keys ONLY if no other still-installed mod provides the same key
// file — so uninstalling one component of a mod suite (e.g. @DayZ-Expansion-AI)
// does not strip the shared signing key required by the rest of the suite.
func Uninstall(serverDir, modName string) error {
	if !strings.HasPrefix(modName, "@") {
		return fmt.Errorf("mod name must start with '@'")
	}
	modDir := filepath.Join(serverDir, modName)
	keyNames := collectKeyNames(modDir)

	if err := os.RemoveAll(modDir); err != nil {
		return err
	}

	stillProvided := map[string]bool{}
	entries, _ := os.ReadDir(serverDir)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "@") || strings.EqualFold(e.Name(), modName) {
			continue
		}
		for _, k := range collectKeyNames(filepath.Join(serverDir, e.Name())) {
			stillProvided[strings.ToLower(k)] = true
		}
	}

	keysDir := filepath.Join(serverDir, "keys")
	for _, k := range keyNames {
		if stillProvided[strings.ToLower(k)] {
			continue // another mod still signs with this key
		}
		_ = os.Remove(filepath.Join(keysDir, k))
	}
	return nil
}

// SyncKeys copies every .bikey from each named mod into <serverDir>/keys.
// If names is empty it scans every installed @Mod in the server dir.
func SyncKeys(serverDir string, names []string) error {
	keysDir := filepath.Join(serverDir, "keys")
	if err := os.MkdirAll(keysDir, 0o755); err != nil {
		return err
	}
	if len(names) == 0 {
		entries, _ := os.ReadDir(serverDir)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "@") {
				names = append(names, e.Name())
			}
		}
	}
	for _, name := range names {
		for _, sub := range []string{"keys", "Keys"} {
			src := filepath.Join(serverDir, name, sub)
			entries, err := os.ReadDir(src)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".bikey") {
					continue
				}
				if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(keysDir, e.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------

func collectKeyNames(modDir string) []string {
	var names []string
	for _, sub := range []string{"keys", "Keys"} {
		entries, err := os.ReadDir(filepath.Join(modDir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".bikey") {
				names = append(names, e.Name())
			}
		}
	}
	return names
}

func countKeys(modDir string) int {
	return len(collectKeyNames(modDir))
}

// resolveSymlink returns the link target (recursively) if dir is a
// junction/symlink, otherwise dir itself. filepath.Walk will not descend
// into a symlink at its root, so we have to resolve before walking — DayZ
// Launcher mounts !Workshop/@Mod entries as junctions.
func resolveSymlink(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil && r != "" {
		return r
	}
	return dir
}

func dirSize(dir string) int64 {
	dir = resolveSymlink(dir)
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func newestMTime(dir string) time.Time {
	dir = resolveSymlink(dir)
	var newest time.Time
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

func copyTree(src, dst string) error {
	src = resolveSymlink(src)
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if Logger != nil {
				Logger.Printf("copyTree: walk %s: %v", path, err)
			}
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := copyFile(path, target); err != nil {
			if Logger != nil {
				Logger.Printf("copyTree: copy %s → %s: %v", path, target, err)
			}
			return err
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// readMeta parses a DayZ mod's meta.cpp for the human-readable name and
// Workshop publishedid. The file is a tiny CPP-like KV file, e.g.:
//
//	protocol = 1;
//	publishedid = 1559212036;
//	name = "Community Framework";
//	timestamp = 1695822543;
//
// Returns a zero ModMeta if meta.cpp is missing or unreadable — callers
// treat that as "no metadata available".
func readMeta(modDir string) modMeta {
	if modDir == "" {
		return modMeta{}
	}
	data, err := os.ReadFile(filepath.Join(modDir, "meta.cpp"))
	if err != nil {
		return modMeta{}
	}
	var m modMeta
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ";")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		switch strings.ToLower(k) {
		case "name":
			m.Name = v
		case "publishedid":
			m.PublishedID = v
		}
	}
	return m
}

type modMeta struct {
	Name        string
	PublishedID string
}

// ErrNoVanillaPath is returned when install/list is called without a
// configured vanilla DayZ path.
var ErrNoVanillaPath = errors.New("vanilla DayZ path is not configured")

// isDirOrJunction reports whether the entry should be treated as a directory.
// On Windows, DayZ Launcher / Steam mount mods under !Workshop as NTFS
// junctions to steamapps/workshop/content/221100/<id>/, so DirEntry.IsDir()
// returns false (the entry type is reported as a symlink / reparse point)
// even though the link resolves to a real folder. Anything that's not a
// plain regular file is worth a follow-up os.Stat to dereference.
func isDirOrJunction(e os.DirEntry, fullPath string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type().IsRegular() {
		return false
	}
	st, err := os.Stat(fullPath)
	return err == nil && st.IsDir()
}
