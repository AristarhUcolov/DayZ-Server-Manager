// Copyright (c) 2026 Aristarh Ucolov.
//
// Mod types drift — checks, for every installed @mod that ships loot, whether
// that loot is actually present in the mission economy. A mod update can add
// (or a first install forget to merge) types that then silently never spawn;
// this surfaces exactly which mods have loot missing from types.xml and the
// custom types files, the most common "the mod's items don't spawn" cause.
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
)

// missionTypeFile is one types file the mission holds: db/types.xml, a file
// cfgeconomycore.xml registers, or a moded_types file nobody registered.
type missionTypeFile struct {
	Label      string // shown to the admin: "types.xml", "moded_types/x.xml", "expansion_ce/x.xml"
	Path       string
	Registered bool // DayZ loads it
	Moded      bool // lives in moded_types (the Custom types page can register it)
}

// missionTypeFiles lists the types files DayZ loads for the active mission —
// db/types.xml plus every <file type="types"> that cfgeconomycore.xml registers,
// in whichever <ce folder> — followed by the moded_types files that are NOT
// registered. DayZ ignores those, so they must never count as active loot.
func (h *handlers) missionTypeFiles() []missionTypeFile {
	mission, err := h.missionTemplate()
	if err != nil {
		return nil
	}
	mdir := dztypes.MissionDir(h.app.ServerDir, mission)
	var out []missionTypeFile
	seen := map[string]bool{}
	modedPrefix := strings.ToLower(dztypes.ModedFolder) + "/"
	for _, f := range missionEcoFiles(mdir, "types") {
		rel := strings.ToLower(f.Rel)
		seen[rel] = true
		label := f.Rel
		if rel == "db/types.xml" {
			label = "types.xml"
		}
		out = append(out, missionTypeFile{Label: label, Path: f.Path, Registered: true,
			Moded: strings.HasPrefix(rel, modedPrefix) && !strings.Contains(rel[len(modedPrefix):], "/")})
	}
	if entries, err := os.ReadDir(dztypes.ModedDir(h.app.ServerDir, mission)); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			rel := dztypes.ModedFolder + "/" + e.Name()
			if seen[strings.ToLower(rel)] {
				continue
			}
			out = append(out, missionTypeFile{Label: rel, Path: filepath.Join(mdir, dztypes.ModedFolder, e.Name()), Moded: true})
		}
	}
	return out
}

// missionTypeSets returns the lowercased names DayZ actually loads, and the
// names found only in unregistered moded_types files (name → file name), so
// the drift check can say "the file is there but not registered" instead of
// calling that loot active.
func (h *handlers) missionTypeSets() (active map[string]bool, parked map[string]string) {
	active, parked = map[string]bool{}, map[string]string{}
	files := h.missionTypeFiles()
	for _, f := range files {
		if !f.Registered {
			continue
		}
		if doc, err := dztypes.Load(f.Path); err == nil {
			for i := range doc.Types {
				active[strings.ToLower(doc.Types[i].Name)] = true
			}
		}
	}
	for _, f := range files {
		if f.Registered {
			continue
		}
		if doc, err := dztypes.Load(f.Path); err == nil {
			for i := range doc.Types {
				k := strings.ToLower(doc.Types[i].Name)
				if !active[k] {
					if _, ok := parked[k]; !ok {
						parked[k] = filepath.Base(f.Path)
					}
				}
			}
		}
	}
	return active, parked
}

// collectModTypeNames returns the set of type names a mod ships across every
// bundled types doc, resolving the server-dir copy (or the !Workshop junction).
func (h *handlers) collectModTypeNames(modName string) map[string]string {
	dir := filepath.Join(h.app.ServerDir, modName)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if alt, err := mods.SourcePath(h.app.Cfg().ModSourceDir(), modName); err == nil {
			if st, err := os.Stat(alt); err == nil && st.IsDir() {
				dir = alt
			}
		}
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != "" {
		dir = resolved
	}
	names := map[string]string{} // lower -> original casing
	const maxTypes = 5000
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info == nil || info.IsDir() {
			return nil
		}
		if len(names) >= maxTypes {
			return filepath.SkipDir
		}
		if strings.ToLower(filepath.Ext(info.Name())) != ".xml" || info.Size() > 10*1024*1024 {
			return nil
		}
		if doc, err := dztypes.Load(p); err == nil && len(doc.Types) > 0 {
			for i := range doc.Types {
				n := doc.Types[i].Name
				if n != "" {
					names[strings.ToLower(n)] = n
				}
			}
		}
		return nil
	})
	return names
}

func (h *handlers) modsDrift(w http.ResponseWriter, r *http.Request) {
	active, parked := h.missionTypeSets()
	list, err := mods.List(h.app.ServerDir, h.app.Cfg().ModSourceDir())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type report struct {
		Mod     string   `json:"mod"`
		Ships   int      `json:"ships"`
		Active  int      `json:"active"`
		Missing int      `json:"missing"`
		Sample  []string `json:"sample,omitempty"`
		Status  string   `json:"status"` // ok | partial | notmerged
		// Missing names that sit in a moded_types file cfgeconomycore.xml does
		// not register: merged, but DayZ never loads them.
		Unregistered      int      `json:"unregistered,omitempty"`
		UnregisteredFiles []string `json:"unregisteredFiles,omitempty"`
	}
	reports := []report{}
	// installed counts every mod on the server, checked only those shipping
	// loot: "none of your 3 mods ships a types file" beats an unexplained
	// empty list.
	checked, installed := 0, 0
	for _, m := range list {
		if !m.InstalledInServer {
			continue
		}
		installed++
		names := h.collectModTypeNames(m.Name)
		if len(names) == 0 {
			continue // ships no loot — nothing to check
		}
		checked++
		missing := []string{}
		unreg := 0
		unregFiles := map[string]bool{}
		for lower, orig := range names {
			if !active[lower] {
				missing = append(missing, orig)
				if f, ok := parked[lower]; ok {
					unreg++
					unregFiles[f] = true
				}
			}
		}
		sort.Strings(missing)
		sample := missing
		if len(sample) > 12 {
			sample = sample[:12]
		}
		status := "ok"
		if len(missing) == len(names) {
			status = "notmerged"
		} else if len(missing) > 0 {
			status = "partial"
		}
		files := make([]string, 0, len(unregFiles))
		for f := range unregFiles {
			files = append(files, f)
		}
		sort.Strings(files)
		reports = append(reports, report{
			Mod: m.Name, Ships: len(names), Active: len(names) - len(missing),
			Missing: len(missing), Sample: sample, Status: status,
			Unregistered: unreg, UnregisteredFiles: files,
		})
	}
	// Worst first: not-merged, then partial, then ok; alpha within a group.
	rank := map[string]int{"notmerged": 0, "partial": 1, "ok": 2}
	sort.SliceStable(reports, func(i, j int) bool {
		if rank[reports[i].Status] != rank[reports[j].Status] {
			return rank[reports[i].Status] < rank[reports[j].Status]
		}
		return reports[i].Mod < reports[j].Mod
	})

	writeJSON(w, map[string]interface{}{"mods": reports, "checked": checked, "installed": installed})
}
