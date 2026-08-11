// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.22.0: the Server health page. One place that answers "is anything wrong?"
// by folding together the four signals the manager already computes but that
// were scattered across pages: is the process up (or stuck in a crash loop),
// is there disk room, does the config validate, and — if it won't start — why.
package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/util"
)

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	running := h.app.ServerIsRunning()

	// Disk free on the volume the server lives on. Best-effort: 0 on error.
	var diskFree uint64
	if f, err := util.DiskFree(h.app.ServerDir); err == nil {
		diskFree = f
	}

	// Start-failure diagnosis — only meaningful counts here; the detailed
	// findings are rendered separately by the existing diagnosis card. Reading
	// the RPT is cheap, so it stays inline; config validation does NOT (it
	// parses every mission file, ~seconds) and the page fetches it separately
	// so the fast tiles paint immediately.
	var dFatal, dWarn int
	for _, f := range dzlogs.Diagnose(h.app.ServerDir, h.app.Cfg().ProfilesDir) {
		switch f.Severity {
		case dzlogs.SevFatal:
			dFatal++
		case dzlogs.SevWarn:
			dWarn++
		}
	}

	writeJSON(w, map[string]interface{}{
		"server": map[string]interface{}{
			"running":    running,
			"loopPaused": h.app.Server.LoopPaused(),
			"pid":        h.app.Server.PID(),
			"uptime":     h.app.Server.Uptime().Round(time.Second).String(),
		},
		"disk":    map[string]interface{}{"free": diskFree},
		"startup": map[string]interface{}{"fatal": dFatal, "warnings": dWarn},
	})
}

// ---------------------------------------------------------------------------
// Disk cleanup — the server dumps RPT/ADM/crash logs and the manager keeps
// .bak snapshots; on a long-lived server these quietly eat gigabytes. These
// endpoints report how much each occupies and delete them on request.

// cleanupRoots is where logs and snapshots can live: the server dir, plus the
// profiles dir when it is configured outside the server dir.
func (h *handlers) cleanupRoots() []string {
	roots := []string{h.app.ServerDir}
	pr := h.profilesRoot()
	if rel, err := filepath.Rel(h.app.ServerDir, pr); err != nil || strings.HasPrefix(rel, "..") {
		roots = append(roots, pr)
	}
	return roots
}

// walkCleanup visits every file under the cleanup roots, skipping the heavy
// subtrees (installed mods, DB storage shards, keys) that never hold either.
func (h *handlers) walkCleanup(fn func(path string, info fs.FileInfo)) {
	seen := map[string]bool{}
	for _, root := range h.cleanupRoots() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if p != root && (strings.HasPrefix(name, "@") || strings.HasPrefix(name, "storage_") ||
					name == "keys" || name == "BattlEyeLogs" || strings.HasPrefix(name, ".")) {
					return fs.SkipDir
				}
				return nil
			}
			if seen[p] {
				return nil
			}
			seen[p] = true
			if info, e := d.Info(); e == nil {
				fn(p, info)
			}
			return nil
		})
	}
}

// isAdmArtifact reports whether a filename is a DayZ admin log (.ADM). Split
// out so the cleanup can offer "ADM logs" as its own target — an admin often
// wants to clear chat/kill history without touching RPT crash diagnostics.
func isAdmArtifact(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".adm")
}

// cleanupSelect computes which files a target would remove and their total
// size. "logs" keeps the newest .RPT (so start-failure diagnosis survives) and
// excludes .ADM; "adm" keeps the newest .ADM. dryRun stops it deleting.
func (h *handlers) cleanupSelect(target string, dryRun bool) (count int, freed int64) {
	keep := map[string]bool{}
	if target == "logs" || target == "adm" {
		var newest string
		var newestT time.Time
		wantExt := ".rpt"
		if target == "adm" {
			wantExt = ".adm"
		}
		h.walkCleanup(func(p string, info fs.FileInfo) {
			if strings.EqualFold(filepath.Ext(p), wantExt) && info.ModTime().After(newestT) {
				newestT, newest = info.ModTime(), p
			}
		})
		if newest != "" {
			keep[newest] = true
		}
	}
	h.walkCleanup(func(p string, info fs.FileInfo) {
		base := filepath.Base(p)
		match := false
		switch target {
		case "logs":
			match = isServerLogArtifact(base) && !isAdmArtifact(base)
		case "adm":
			match = isAdmArtifact(base)
		case "backups":
			match = backupStampRe.MatchString(base)
		}
		if !match || keep[p] {
			return
		}
		if dryRun {
			count++
			freed += info.Size()
			return
		}
		if os.Remove(p) == nil {
			count++
			freed += info.Size()
		}
	})
	return count, freed
}

// wipesCleanup reports or removes old wipe snapshots under .dayz-manager/wipes/.
// The newest snapshot is always kept so the most recent wipe stays undoable;
// older ones are stale (the server long since rebuilt that state) and just eat
// disk. Counts whole snapshots, not files.
func (h *handlers) wipesCleanup(dryRun bool) (count int, freed int64) {
	dir := filepath.Join(h.app.ManagerDir, "wipes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // timestamp names → newest first
	for i, name := range names {
		if i == 0 {
			continue // keep the newest for undo
		}
		full := filepath.Join(dir, name)
		sz := dirSize(full)
		if dryRun {
			count++
			freed += sz
			continue
		}
		if os.RemoveAll(full) == nil {
			count++
			freed += sz
		}
	}
	return count, freed
}

// dirSize sums the size of every file under root (best-effort).
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// cleanupScan reports the count and size of removable logs, ADM logs, backups
// and old wipe snapshots.
func (h *handlers) cleanupScan(w http.ResponseWriter, r *http.Request) {
	lc, lb := h.cleanupSelect("logs", true)
	ac, ab := h.cleanupSelect("adm", true)
	bc, bb := h.cleanupSelect("backups", true)
	wc, wb := h.wipesCleanup(true)
	writeJSON(w, map[string]interface{}{
		"logs":    map[string]interface{}{"count": lc, "bytes": lb},
		"adm":     map[string]interface{}{"count": ac, "bytes": ab},
		"backups": map[string]interface{}{"count": bc, "bytes": bb},
		"wipes":   map[string]interface{}{"count": wc, "bytes": wb},
	})
}

// cleanupRun deletes the chosen category. Best-effort: files the running server
// has locked (the current RPT/ADM) are simply skipped.
func (h *handlers) cleanupRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Target {
	case "logs", "adm", "backups", "wipes":
	default:
		http.Error(w, "target must be logs, adm, backups or wipes", http.StatusBadRequest)
		return
	}
	var count int
	var freed int64
	if req.Target == "wipes" {
		count, freed = h.wipesCleanup(false)
	} else {
		count, freed = h.cleanupSelect(req.Target, false)
	}
	h.app.Log.Printf("cleanup %s: removed %d file(s), freed %d bytes", req.Target, count, freed)
	writeJSON(w, map[string]interface{}{"deleted": count, "freed": freed})
}
