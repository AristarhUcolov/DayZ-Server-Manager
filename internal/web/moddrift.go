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

// missionActiveTypeNames collects the lowercased names of every type active in
// the mission: types.xml plus every file in the custom types folder.
func (h *handlers) missionActiveTypeNames() map[string]bool {
	set := map[string]bool{}
	if doc, _, err := h.loadTypesFile(""); err == nil {
		for i := range doc.Types {
			set[strings.ToLower(doc.Types[i].Name)] = true
		}
	}
	if mission, err := h.missionTemplate(); err == nil {
		modedDir := dztypes.ModedDir(h.app.ServerDir, mission)
		if entries, err := os.ReadDir(modedDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
					continue
				}
				if doc, err := dztypes.Load(filepath.Join(modedDir, e.Name())); err == nil {
					for i := range doc.Types {
						set[strings.ToLower(doc.Types[i].Name)] = true
					}
				}
			}
		}
	}
	return set
}

// collectModTypeNames returns the set of type names a mod ships across every
// bundled types doc, resolving the server-dir copy (or the !Workshop junction).
func (h *handlers) collectModTypeNames(modName string) map[string]string {
	dir := filepath.Join(h.app.ServerDir, modName)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if vp := h.app.Cfg().VanillaDayZPath; vp != "" {
			alt := filepath.Join(vp, "!Workshop", modName)
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
	active := h.missionActiveTypeNames()
	list, err := mods.List(h.app.ServerDir, h.app.Cfg().VanillaDayZPath)
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
	}
	reports := []report{}
	checked := 0
	for _, m := range list {
		if !m.InstalledInServer {
			continue
		}
		names := h.collectModTypeNames(m.Name)
		if len(names) == 0 {
			continue // ships no loot — nothing to check
		}
		checked++
		missing := []string{}
		for lower, orig := range names {
			if !active[lower] {
				missing = append(missing, orig)
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
		reports = append(reports, report{
			Mod: m.Name, Ships: len(names), Active: len(names) - len(missing),
			Missing: len(missing), Sample: sample, Status: status,
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

	writeJSON(w, map[string]interface{}{"mods": reports, "checked": checked})
}
