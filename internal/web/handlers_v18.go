// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.18.0 handlers, all three closing gaps the code was one step away from:
//
//   - /api/diagnose      — read the RPT the admin never opens and say why the
//     server did not start.
//   - /api/backups/diff  — the panel already keeps a .bak beside every file it
//     overwrites, but restoring one was blind.
//   - /api/wipe/list|restore — wipeApply already MOVES the storage folders
//     aside precisely so a wipe is reversible; nothing
//     could move them back.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	dzlogs "dayzmanager/internal/logs"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// backupStampRe matches the `.bak.YYYYMMDD-HHMMSS` suffix the manager appends
// to every snapshot, anchored at end so a file merely containing ".bak." is not
// mistaken for one.
var backupStampRe = regexp.MustCompile(`\.bak\.(\d{8}-\d{6})$`)

// backupsAll scans the server tree for every `<file>.bak.<ts>` snapshot the
// manager has taken and groups them by the original file, so the History page
// can show "what changed and when" across all editors in one place — instead of
// the admin having to remember which file they edited and open it in Files.
//
// Big, irrelevant subtrees (installed mods, DB storage shards, keys) are skipped
// so the walk stays cheap: snapshots only ever live next to the config files the
// manager itself rewrites.
func (h *handlers) backupsAll(w http.ResponseWriter, r *http.Request) {
	root := h.app.ServerDir
	type ver struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Time int64  `json:"time"`
	}
	type group struct {
		Path     string `json:"path"`     // original file, relative to server dir
		Exists   bool   `json:"exists"`   // the live file is still present
		Versions []ver  `json:"versions"` // newest first
	}
	groups := map[string]*group{}

	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry — skip, never abort the whole walk
		}
		if d.IsDir() {
			name := d.Name()
			// Skip the heavy, snapshot-free subtrees.
			if p != root && (strings.HasPrefix(name, "@") || strings.HasPrefix(name, "storage_") ||
				name == "keys" || name == "BattlEyeLogs" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		m := backupStampRe.FindStringSubmatchIndex(d.Name())
		if m == nil {
			return nil
		}
		origBase := d.Name()[:m[0]] // strip ".bak.<ts>"
		relDir, e := filepath.Rel(root, filepath.Dir(p))
		if e != nil {
			return nil
		}
		origRel := filepath.ToSlash(filepath.Join(relDir, origBase))
		g := groups[origRel]
		if g == nil {
			_, statErr := os.Stat(filepath.Join(filepath.Dir(p), origBase))
			g = &group{Path: origRel, Exists: statErr == nil}
			groups[origRel] = g
		}
		info, _ := d.Info()
		g.Versions = append(g.Versions, ver{Name: d.Name(), Size: sizeOrZero(info), Time: info.ModTime().Unix()})
		return nil
	})

	out := make([]*group, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.Versions, func(i, j int) bool { return g.Versions[i].Time > g.Versions[j].Time })
		out = append(out, g)
	}
	// Most-recently-touched file first — that is what an admin is looking for.
	sort.Slice(out, func(i, j int) bool {
		var ti, tj int64
		if len(out[i].Versions) > 0 {
			ti = out[i].Versions[0].Time
		}
		if len(out[j].Versions) > 0 {
			tj = out[j].Versions[0].Time
		}
		return ti > tj
	})
	writeJSON(w, map[string]interface{}{"files": out})
}

// ---------------------------------------------------------------------------
// Why did the server not start?

func (h *handlers) diagnose(w http.ResponseWriter, r *http.Request) {
	cfg := h.app.Cfg()
	findings := dzlogs.Diagnose(h.app.ServerDir, cfg.ProfilesDir)
	if findings == nil {
		findings = []dzlogs.Finding{}
	}
	writeJSON(w, map[string]interface{}{
		"findings": findings,
		"running":  h.app.ServerIsRunning(),
	})
}

// ---------------------------------------------------------------------------
// Diff a file against one of its .bak siblings.

func (h *handlers) backupsDiff(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	name := r.URL.Query().Get("backup")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The backup must be a sibling of the file and carry its .bak prefix —
	// nothing else may be read through this endpoint.
	base := filepath.Base(full)
	if name == "" || !strings.HasPrefix(name, base+".bak.") || strings.ContainsAny(name, `/\`) {
		http.Error(w, "not a backup of this file", http.StatusBadRequest)
		return
	}
	bakPath := filepath.Join(filepath.Dir(full), name)

	const maxDiff = 8 << 20
	oldData, err := readCapped(bakPath, maxDiff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	newData, err := readCapped(full, maxDiff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Old = the backup, new = what is on disk now, so "-" reads as "this is
	// what restoring would bring back".
	res := util.DiffLines(string(oldData), string(newData), 3)
	writeJSON(w, map[string]interface{}{
		"path":   rel,
		"backup": name,
		"diff":   res,
	})
}

func readCapped(path string, max int64) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > max {
		return nil, fmt.Errorf("%s is %d bytes — too large to diff", filepath.Base(path), st.Size())
	}
	return os.ReadFile(path)
}

// ---------------------------------------------------------------------------
// Undo a wipe.

type wipeSet struct {
	ID      string   `json:"id"`   // the timestamp folder name
	When    string   `json:"when"` // RFC3339, parsed from the folder name
	Folders []string `json:"folders"`
	Size    int64    `json:"size"`
	// Blocked names folders that already exist in the mission again, which is
	// what makes a restore unsafe: it would overwrite a world players have
	// been building on since the wipe.
	Blocked []string `json:"blocked,omitempty"`
}

func (h *handlers) wipesDir() string { return filepath.Join(h.app.ManagerDir, "wipes") }

func (h *handlers) wipeList(w http.ResponseWriter, r *http.Request) {
	missionDir := ""
	if mission, err := h.missionTemplate(); err == nil {
		missionDir = dztypes.MissionDir(h.app.ServerDir, mission)
	}

	entries, _ := os.ReadDir(h.wipesDir())
	out := []wipeSet{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		set := wipeSet{ID: e.Name()}
		if ts, err := time.ParseInLocation("20060102-150405", e.Name(), time.Local); err == nil {
			set.When = ts.Format(time.RFC3339)
		}
		sub, _ := os.ReadDir(filepath.Join(h.wipesDir(), e.Name()))
		for _, f := range sub {
			set.Folders = append(set.Folders, f.Name())
			if missionDir != "" {
				if _, err := os.Stat(filepath.Join(missionDir, f.Name())); err == nil {
					set.Blocked = append(set.Blocked, f.Name())
				}
			}
		}
		set.Size = dirSizeWalk(filepath.Join(h.wipesDir(), e.Name()))
		out = append(out, set)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	writeJSON(w, map[string]interface{}{"wipes": out, "dir": h.wipesDir()})
}

func (h *handlers) wipeRestore(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	// The id is a folder name we generated; refuse anything that could escape.
	if strings.ContainsAny(req.ID, `/\`) || strings.Contains(req.ID, "..") {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	src := filepath.Join(h.wipesDir(), req.ID)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		http.Error(w, "no such wipe", http.StatusNotFound)
		return
	}

	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)

	entries, err := os.ReadDir(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Check every destination BEFORE moving anything: a half-restored world is
	// worse than either state on its own.
	var blocked []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(missionDir, e.Name())); err == nil {
			blocked = append(blocked, e.Name())
		}
	}
	if len(blocked) > 0 {
		http.Error(w, fmt.Sprintf("the mission already has %s — the server has built new state since this wipe. "+
			"Wipe again first if you really want the old world back.", strings.Join(blocked, ", ")),
			http.StatusConflict)
		return
	}

	var restored []string
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(missionDir, e.Name())
		if err := os.Rename(from, to); err != nil {
			http.Error(w, fmt.Sprintf("restore %s: %v", e.Name(), err), http.StatusInternalServerError)
			return
		}
		restored = append(restored, e.Name())
	}
	// The folder is now empty; leaving it behind would clutter the list.
	_ = os.Remove(src)

	h.app.Log.Printf("wipe %s undone: restored %d storage folder(s)", req.ID, len(restored))
	writeJSON(w, map[string]interface{}{"restored": restored, "count": len(restored)})
}
