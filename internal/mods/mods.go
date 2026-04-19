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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
}

// List returns the union of mods found in the client Workshop folder and the
// mods already installed in the server directory.
func List(serverDir, vanillaDayZPath string) ([]Mod, error) {
	byName := map[string]*Mod{}

	// Server-side @Mod directories.
	srvEntries, _ := os.ReadDir(serverDir)
	for _, e := range srvEntries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "@") {
			continue
		}
		p := filepath.Join(serverDir, e.Name())
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
	if vanillaDayZPath != "" {
		ws := filepath.Join(vanillaDayZPath, "!Workshop")
		wsEntries, _ := os.ReadDir(ws)
		for _, e := range wsEntries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "@") {
				continue
			}
			p := filepath.Join(ws, e.Name())
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
	dst := filepath.Join(serverDir, modName)
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("copy mod: %w", err)
	}
	if err := SyncKeys(serverDir, []string{modName}); err != nil {
		return fmt.Errorf("sync keys: %w", err)
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

func dirSize(dir string) int64 {
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
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
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

// ErrNoVanillaPath is returned when install/list is called without a
// configured vanilla DayZ path.
var ErrNoVanillaPath = errors.New("vanilla DayZ path is not configured")
