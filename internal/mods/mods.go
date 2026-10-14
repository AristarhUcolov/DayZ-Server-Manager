// Copyright (c) 2026 Aristarh Ucolov.
//
// Steam Workshop mod sync for DayZ.
//
// On disk the *client* DayZ install contains:
//
//	<DayZ>/!Workshop/@ModName/...            ← here mods are downloaded by Steam
//	<DayZ>/!Workshop/@ModName/keys/*.bikey   ← BattlEye signing keys
//
// The dedicated server expects mods sitting at:
//
//	<DayZServer>/@ModName/...                ← loaded via -mod=@ModName;@Other
//	<DayZServer>/keys/*.bikey                ← combined keys folder
//
// This package implements the install / uninstall / update / sync-keys flow
// so users don't have to copy things manually.
package mods

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
	MetaTimestamp       int64     `json:"metaTimestamp,omitempty"` // Workshop publish time from meta.cpp (source copy preferred)
	CaseProblems        []string  `json:"caseProblems,omitempty"`  // Linux: names the server would not load
}

// List returns the union of mods found in the mod source (client !Workshop,
// a custom folder, or SteamCMD content) and the mods already installed in the
// server directory. srcDir may be "" (server-only): then only installed mods
// are listed and none reports an update.
func List(serverDir, srcDir string) ([]Mod, error) {
	byName := map[string]*Mod{}

	// Server-side @Mod directories.
	srvEntries, _ := os.ReadDir(serverDir)
	for _, e := range srvEntries {
		if !strings.HasPrefix(e.Name(), "@") || isStagingDir(e.Name()) {
			continue
		}
		p := filepath.Join(serverDir, e.Name())
		if !isDirOrJunction(e, p) {
			continue
		}
		size, keys, newest := scanTreeCached(p)
		byName[e.Name()] = &Mod{
			Name:              e.Name(),
			InstalledInServer: true,
			ServerPath:        p,
			SizeBytes:         size,
			KeyCount:          keys,
			ServerModifiedAt:  newest,
		}
	}

	// Source-side mods. Steam / DayZ Launcher creates !Workshop @Mod entries
	// as Windows junctions pointing at steamapps/workshop/content/221100/<id>/,
	// so scanSource follows them — DirEntry.IsDir() is false on a junction.
	for name, p := range scanSource(srcDir) {
		m := byName[name]
		if m == nil {
			m = &Mod{Name: name}
			byName[name] = m
		}
		m.AvailableInWorkshop = true
		m.WorkshopPath = p
		if !m.InstalledInServer {
			// Not on the server yet — one walk fills size, keys and mtime.
			size, keys, newest := scanTreeCached(p)
			m.SizeBytes = size
			m.KeyCount = keys
			m.WorkshopModifiedAt = newest
		}
	}

	out := make([]Mod, 0, len(byName))
	for _, m := range byName {
		srcMeta := readMeta(m.WorkshopPath)
		srvMeta := readMeta(m.ServerPath)
		if m.InstalledInServer && m.AvailableInWorkshop {
			m.UpdateAvailable = updateAvailable(m, srcMeta, srvMeta)
		}
		// meta.cpp lives in the mod root; prefer the source copy so the
		// server-side mod folder can be stripped without losing metadata.
		meta := srcMeta
		if meta.Name == "" && meta.PublishedID == "" {
			meta = srvMeta
		}
		m.DisplayName = meta.Name
		m.PublishedID = meta.PublishedID
		m.MetaTimestamp = meta.Timestamp
		if runtime.GOOS == "linux" && m.InstalledInServer {
			m.CaseProblems = CaseProblems(m.ServerPath)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// updateAvailable decides whether the source copy is newer than the server
// copy. Steam rewrites meta.cpp's timestamp on every Workshop publish, so when
// both copies carry one it is exact and costs nothing. Without it we fall back
// to comparing the newest file times (a full walk of the source copy).
func updateAvailable(m *Mod, srcMeta, srvMeta modMeta) bool {
	if srcMeta.Timestamp != 0 && srvMeta.Timestamp != 0 {
		// Both copies use the same encoding, so the raw values compare
		// directly. (Never time.Unix() them: they are not Unix seconds, and
		// the resulting year-160-billion date made /api/mods fail to encode —
		// an empty response and "Unexpected end of JSON input" on the page.)
		if t := MetaTime(srcMeta.Timestamp); !t.IsZero() {
			m.WorkshopModifiedAt = t
		}
		return srcMeta.Timestamp > srvMeta.Timestamp
	}
	_, _, newest := scanTreeCached(m.WorkshopPath)
	m.WorkshopModifiedAt = newest
	return newest.After(m.ServerModifiedAt.Add(2 * time.Second))
}

// Install copies @ModName from the mod source into the server directory and
// merges its .bikey files into <serverDir>/keys.
func Install(serverDir, srcDir, modName string) error {
	if err := ValidName(modName); err != nil {
		return err
	}
	if !strings.HasPrefix(modName, "@") {
		return fmt.Errorf("mod name must start with '@'")
	}
	src, err := SourcePath(srcDir, modName)
	if err != nil {
		return err
	}
	dst := filepath.Join(serverDir, modName)
	if _, err := os.Stat(dst); err == nil {
		// Reinstalling over an existing copy: swap it like an update, so files
		// the new version dropped do not linger next to the new ones.
		return Update(serverDir, srcDir, modName)
	}
	if err := checkDiskSpace(serverDir, src); err != nil {
		return err
	}
	// Copy into a staging folder and rename at the end: a copy that fails
	// half-way (disk full, a locked file) must not leave a half mod that looks
	// installed and then stops the server from starting.
	staging := dst + ".installing"
	_ = os.RemoveAll(staging)
	if err := copyTree(src, staging); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("copy mod: %w", err)
	}
	if err := os.Rename(staging, dst); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("install mod: %w", err)
	}
	afterCopy(dst)
	if err := SyncKeys(serverDir, []string{modName}); err != nil {
		return fmt.Errorf("sync keys: %w", err)
	}
	return nil
}

// checkDiskSpace fails fast if the target volume looks too small for a mod
// copy. We require free space >= 1.2x the source size — a 20% headroom covers
// filesystem overhead. Unknown disk info is permissive.
func checkDiskSpace(serverDir, src string) error {
	size := dirSize(src)
	if size <= 0 {
		return nil
	}
	free, _ := util.DiskFree(serverDir)
	if free == 0 {
		return nil // unknown
	}
	required := uint64(float64(size) * 1.2)
	if free < required {
		return fmt.Errorf("not enough free space on target disk: need ~%d MB, have %d MB",
			required/1024/1024, free/1024/1024)
	}
	return nil
}

// isStagingDir reports the temporary folders Install/Update use (left behind
// only if the manager died mid-copy) — never a real mod.
// IsStagingDir reports whether name is an install in progress (a temporary
// folder that must never be taken for a mod).
func IsStagingDir(name string) bool { return isStagingDir(name) }

func isStagingDir(name string) bool {
	for _, suf := range []string{".installing", ".updating", ".old"} {
		if strings.HasSuffix(strings.ToLower(name), suf) {
			return true
		}
	}
	return false
}

// Update refreshes an already-installed mod from the source copy.
// It copies into a staging directory first, then atomically swaps, so a
// half-broken update never leaves the server in a corrupt state.
func Update(serverDir, srcDir, modName string) error {
	if err := ValidName(modName); err != nil {
		return err
	}
	if !strings.HasPrefix(modName, "@") {
		return fmt.Errorf("mod name must start with '@'")
	}
	src, err := SourcePath(srcDir, modName)
	if err != nil {
		return err
	}
	dst := filepath.Join(serverDir, modName)
	staging := dst + ".updating"
	_ = os.RemoveAll(staging)
	// The staging copy exists next to the old one until the swap.
	if err := checkDiskSpace(serverDir, src); err != nil {
		return err
	}

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
		_ = os.RemoveAll(staging)
		return fmt.Errorf("swap in updated mod: %w", err)
	}
	_ = os.RemoveAll(backup)
	afterCopy(dst)

	// Idempotent — this re-pushes all installed mods' keys into /keys.
	return SyncKeys(serverDir, nil)
}

// UpdateAll updates every installed mod that has a fresher source copy.
// Returns the list of mods it actually touched.
func UpdateAll(serverDir, srcDir string) ([]string, error) {
	if srcDir == "" {
		return nil, ErrNoSource
	}
	list, err := List(serverDir, srcDir)
	if err != nil {
		return nil, err
	}
	var updated []string
	for _, m := range list {
		if !m.UpdateAvailable {
			continue
		}
		if err := Update(serverDir, srcDir, m.Name); err != nil {
			return updated, fmt.Errorf("update %s: %w", m.Name, err)
		}
		updated = append(updated, m.Name)
	}
	return updated, nil
}

// SyncAllResult describes what SyncAll did so the UI can show a detailed toast
// instead of a silent "done".
type SyncAllResult struct {
	Installed    []string `json:"installed"`
	Updated      []string `json:"updated"`
	Skipped      []string `json:"skipped"` // already up to date
	WorkshopDir  string   `json:"workshopDir"`
	Total        int      `json:"total"`        // mods seen in the source
	NotInstalled int      `json:"notInstalled"` // (before sync) waiting for install
	OutOfDate    int      `json:"outOfDate"`    // (before sync) update-available count
}

// SyncAll brings the server directory in line with the mod source: every
// source mod missing on the server is installed, every outdated one is
// updated. Nothing is removed — if a mod is only present server-side (user
// copied manually, or the Workshop version was unsubscribed) it stays. Keys
// are synced once at the end.
func SyncAll(serverDir, srcDir string, only []string) (*SyncAllResult, error) {
	if srcDir == "" {
		return nil, ErrNoSource
	}
	list, err := List(serverDir, srcDir)
	if err != nil {
		return nil, err
	}
	filter := map[string]bool{}
	for _, n := range only {
		filter[n] = true
	}
	res := &SyncAllResult{WorkshopDir: srcDir}
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
			if err := Install(serverDir, srcDir, m.Name); err != nil {
				return res, fmt.Errorf("install %s: %w", m.Name, err)
			}
			res.Installed = append(res.Installed, m.Name)
		case m.UpdateAvailable:
			if err := Update(serverDir, srcDir, m.Name); err != nil {
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

// ValidName checks a mod folder name: "@" plus a single path element. A name
// with a separator ("@/../..") would otherwise lead Install, Update or
// Uninstall — which ends in os.RemoveAll — outside the server folder.
func ValidName(name string) error {
	if !strings.HasPrefix(name, "@") || len(name) < 2 || len(name) > 200 {
		return fmt.Errorf("mod name must start with '@'")
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) || strings.ContainsRune(name, 0) ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || filepath.Base(name) != name {
		return fmt.Errorf("invalid mod name %q", name)
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("invalid mod name %q", name)
		}
	}
	return nil
}

// Uninstall removes @ModName from the server directory. Keys are removed from
// <serverDir>/keys ONLY if no other still-installed mod provides the same key
// file — so uninstalling one component of a mod suite (e.g. @DayZ-Expansion-AI)
// does not strip the shared signing key required by the rest of the suite.
func Uninstall(serverDir, modName string) error {
	if err := ValidName(modName); err != nil {
		return err
	}
	modDir := filepath.Join(serverDir, modName)
	if filepath.Dir(modDir) != filepath.Clean(serverDir) {
		return fmt.Errorf("invalid mod name %q", modName)
	}
	keyNames := collectKeyNames(modDir)

	if err := os.RemoveAll(modDir); err != nil {
		return err
	}
	forgetTree(modDir)

	stillProvided := map[string]bool{}
	entries, _ := os.ReadDir(serverDir)
	for _, e := range entries {
		if !isDirOrJunction(e, filepath.Join(serverDir, e.Name())) ||
			!strings.HasPrefix(e.Name(), "@") || strings.EqualFold(e.Name(), modName) {
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
			if isDirOrJunction(e, filepath.Join(serverDir, e.Name())) && strings.HasPrefix(e.Name(), "@") {
				names = append(names, e.Name())
			}
		}
	}
	for _, name := range names {
		for _, src := range collectKeyFiles(filepath.Join(serverDir, name)) {
			dst := filepath.Join(keysDir, filepath.Base(src))
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------

// collectKeyFiles returns the absolute paths of every .bikey under modDir,
// scanned RECURSIVELY. Mods usually put signing keys in keys/ or Keys/, but
// some use "key" (singular), other casings, or drop the .bikey in the mod root
// — so we look everywhere rather than trusting one fixed folder name.
func collectKeyFiles(modDir string) []string {
	root := resolveSymlink(modDir) // !Workshop entries are junctions
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(info.Name()), ".bikey") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// collectKeyNames returns just the base file names of every .bikey under modDir.
func collectKeyNames(modDir string) []string {
	files := collectKeyFiles(modDir)
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	return names
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

// scanTree walks dir once and returns the total file size, the number of
// .bikey files, and the newest modification time. List needs all three per mod;
// computing them with three separate filepath.Walk calls (dirSize + countKeys +
// newestMTime) tripled the directory-scan cost on large modpacks, which is what
// made the Mods page slow to open.
func scanTree(dir string) (size int64, keyCount int, newest time.Time) {
	dir = resolveSymlink(dir)
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		if strings.EqualFold(filepath.Ext(info.Name()), ".bikey") {
			keyCount++
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return
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
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	// Close reports delayed write errors (a full disk often surfaces only
	// here on Windows) — ignoring it would leave a silently truncated .pbo.
	return out.Close()
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
		case "timestamp":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				m.Timestamp = n
			} else if u, err := strconv.ParseUint(v, 10, 64); err == nil {
				m.Timestamp = int64(u) // top bit set (a "local" DateTime kind)
			}
		}
	}
	return m
}

type modMeta struct {
	Name        string
	PublishedID string
	Timestamp   int64 // raw meta.cpp timestamp (see MetaTime); 0 when absent
}

// ticksAtUnixEpoch is 1970-01-01 in .NET ticks (100 ns since 0001-01-01).
const ticksAtUnixEpoch = 621355968000000000

// MetaTime decodes the timestamp DayZ Tools write into a mod's meta.cpp. It is
// a .NET DateTime.ToBinary() value — 100 ns ticks since 0001-01-01 in the low
// 62 bits, the DateTime kind in the top two — e.g. 5249743004478005813 is
// 2022-12-03. Small values are taken as plain Unix seconds (hand-made mods,
// tests). Returns the zero time for anything that does not decode to a
// plausible date, so callers can never produce an unencodable time.Time.
func MetaTime(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	var t time.Time
	if ts > 0 && ts < 1<<40 {
		t = time.Unix(ts, 0).UTC()
	} else {
		ticks := int64(uint64(ts) & (1<<62 - 1))
		if ticks < ticksAtUnixEpoch {
			return time.Time{}
		}
		d := ticks - ticksAtUnixEpoch
		t = time.Unix(d/10_000_000, (d%10_000_000)*100).UTC()
	}
	if y := t.Year(); y < 2000 || y > 2200 {
		return time.Time{}
	}
	return t
}

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
