// Copyright (c) 2026 Aristarh Ucolov.
//
// Mod sources. Mods are installed/updated FROM a source directory into the
// server directory. Two layouts are understood:
//
//	<src>/@ModName/...        the client's !Workshop (DayZ Launcher junctions),
//	                          or any hand-made folder of @mods
//	<src>/<publishedid>/...   SteamCMD's steamapps/workshop/content/221100 —
//	                          the only way to get Workshop mods on a host
//	                          without the game client; the server folder name
//	                          comes from the mod's meta.cpp ("@" + name)
//
// With no source at all (server-only hosting) the manager still manages the
// @mods already in the server directory; only install/update is unavailable.
package mods

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNoSource is returned when install/update/list needs a mod source but
// none is configured (Settings → Mod source).
var ErrNoSource = errors.New("no mod source configured — choose the DayZ client folder or a mods folder in Settings")

// SourceInfo summarises a candidate source directory for the Settings page.
type SourceInfo struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Mods     int    `json:"mods"`     // mods found in either layout
	SteamCMD int    `json:"steamcmd"` // of which in the <publishedid> layout
}

// InspectSource reports what a directory would provide as a mod source.
func InspectSource(dir string) SourceInfo {
	info := SourceInfo{Path: dir}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return info
	}
	info.Exists = true
	for _, sm := range scanSourceList(dir) {
		info.Mods++
		if sm.steamcmd {
			info.SteamCMD++
		}
	}
	return info
}

type sourceMod struct {
	name     string // server folder name, "@Foo"
	path     string
	steamcmd bool
}

// scanSource maps server folder name → source path for every mod in dir.
func scanSource(dir string) map[string]string {
	out := map[string]string{}
	for _, sm := range scanSourceList(dir) {
		out[sm.name] = sm.path
	}
	return out
}

func scanSourceList(dir string) []sourceMod {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sourceMod
	taken := map[string]bool{}
	var numeric []os.DirEntry
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if !isDirOrJunction(e, full) {
			continue
		}
		switch {
		case strings.HasPrefix(e.Name(), "@"):
			out = append(out, sourceMod{name: e.Name(), path: full})
			taken[strings.ToLower(e.Name())] = true
		case isNumeric(e.Name()):
			numeric = append(numeric, e)
		}
	}
	// SteamCMD folders: named after meta.cpp, falling back to the id. Resolved
	// after the @ folders so an explicit @Name always wins a name clash.
	for _, e := range numeric {
		full := filepath.Join(dir, e.Name())
		if !looksLikeMod(full) {
			continue
		}
		name := "@" + e.Name()
		if meta := readMeta(full); meta.Name != "" {
			if n := safeFolderName(meta.Name); n != "" {
				name = "@" + n
			}
		}
		if taken[strings.ToLower(name)] {
			name = "@" + e.Name()
		}
		taken[strings.ToLower(name)] = true
		out = append(out, sourceMod{name: name, path: full, steamcmd: true})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// looksLikeMod: a SteamCMD content folder is a mod when it has an addons
// folder (any case) or a meta.cpp.
func looksLikeMod(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "meta.cpp")); err == nil {
		return true
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "addons") {
			return true
		}
	}
	return false
}

// safeFolderName makes a meta.cpp display name usable as a folder name on both
// Windows and Linux.
func safeFolderName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 32, strings.ContainsRune(`<>:"/\|?*;`, r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(strings.TrimSpace(b.String()), ".")
}

// SourcePath resolves one mod in the source, or an error naming the source.
func SourcePath(srcDir, modName string) (string, error) {
	if srcDir == "" {
		return "", ErrNoSource
	}
	if p, ok := scanSource(srcDir)[modName]; ok {
		return p, nil
	}
	return "", fmt.Errorf("mod %s not found in the mod source (%s)", modName, srcDir)
}

// ---------------------------------------------------------------------------
// Scan cache. List() used to walk every file of every mod (GBs on a big pack)
// on every call — the Mods page and the dashboard both call it. Results are
// cached per folder, keyed by a cheap signature (folder, addons and meta.cpp
// mtimes) that changes whenever the manager or Steam replaces the mod.

type treeInfo struct {
	sig    string
	size   int64
	keys   int
	newest time.Time
}

var (
	treeMu    sync.Mutex
	treeCache = map[string]treeInfo{}
)

func treeSig(dir string) string {
	dir = resolveSymlink(dir)
	var b strings.Builder
	for _, p := range []string{dir, filepath.Join(dir, "meta.cpp"), filepath.Join(dir, "addons"), filepath.Join(dir, "Addons")} {
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d;", st.ModTime().UnixNano(), st.Size())
		} else {
			b.WriteString("-;")
		}
	}
	return b.String()
}

// scanTreeCached is scanTree with the per-folder cache above.
func scanTreeCached(dir string) (int64, int, time.Time) {
	sig := treeSig(dir)
	treeMu.Lock()
	if ti, ok := treeCache[dir]; ok && ti.sig == sig {
		treeMu.Unlock()
		return ti.size, ti.keys, ti.newest
	}
	treeMu.Unlock()
	size, keys, newest := scanTree(dir)
	treeMu.Lock()
	treeCache[dir] = treeInfo{sig: sig, size: size, keys: keys, newest: newest}
	treeMu.Unlock()
	return size, keys, newest
}

// forgetTree drops a folder from the scan cache (after install/update/remove).
func forgetTree(dir string) {
	treeMu.Lock()
	delete(treeCache, dir)
	treeMu.Unlock()
}

// ---------------------------------------------------------------------------
// Linux. The Linux server binary only loads mods whose addons/keys folders and
// the files inside addons are lower-case — a mod packed with "Addons/" works on
// Windows and silently does nothing (or crashes the server) on Linux.

// CaseProblems lists the paths inside modDir that a Linux server would not load
// because of upper-case letters (relative paths, for display).
func CaseProblems(modDir string) []string {
	var out []string
	root := resolveSymlink(modDir)
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		low := strings.ToLower(e.Name())
		if low != "addons" && low != "keys" {
			continue
		}
		if e.Name() != low {
			out = append(out, e.Name()+"/")
		}
		if low != "addons" || !e.IsDir() {
			continue
		}
		_ = filepath.Walk(filepath.Join(root, e.Name()), func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if name := info.Name(); name != strings.ToLower(name) && p != filepath.Join(root, e.Name()) {
				rel, _ := filepath.Rel(root, p)
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	return out
}

// NormalizeForLinux renames the addons/keys folders and everything inside
// addons to lower-case. Returns how many entries were renamed. A no-op on a
// mod that is already lower-case.
func NormalizeForLinux(modDir string) (int, error) {
	root := resolveSymlink(modDir)
	renamed := 0
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		low := strings.ToLower(e.Name())
		if low != "addons" && low != "keys" {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if e.Name() != low {
			target := filepath.Join(root, low)
			if _, err := os.Stat(target); err == nil && !sameFile(dir, target) {
				return renamed, fmt.Errorf("both %s and %s exist in %s", e.Name(), low, root)
			}
			if err := os.Rename(dir, target); err != nil {
				return renamed, err
			}
			renamed++
			dir = target
		}
		if low != "addons" {
			continue
		}
		n, err := lowerTree(dir)
		renamed += n
		if err != nil {
			return renamed, err
		}
	}
	return renamed, nil
}

// lowerTree lower-cases every name under dir, deepest first so renaming a
// folder never invalidates a path still to be visited.
func lowerTree(dir string) (int, error) {
	var paths []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	n := 0
	for _, p := range paths {
		base := filepath.Base(p)
		low := strings.ToLower(base)
		if base == low {
			continue
		}
		target := filepath.Join(filepath.Dir(p), low)
		if _, err := os.Stat(target); err == nil && !sameFile(p, target) {
			return n, fmt.Errorf("both %s and %s exist", p, target)
		}
		if err := os.Rename(p, target); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// afterCopy runs the platform fix-ups on a freshly installed/updated mod.
func afterCopy(modDir string) {
	forgetTree(modDir)
	if runtime.GOOS != "linux" {
		return
	}
	if n, err := NormalizeForLinux(modDir); err != nil {
		if Logger != nil {
			Logger.Printf("linux lower-case fix for %s: %v", filepath.Base(modDir), err)
		}
	} else if n > 0 && Logger != nil {
		Logger.Printf("linux: lower-cased %d name(s) in %s so the Linux server loads it", n, filepath.Base(modDir))
	}
}
