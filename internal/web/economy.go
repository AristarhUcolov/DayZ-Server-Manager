// Copyright (c) 2026 Aristarh Ucolov.
//
// Loot economy dashboard + CE tuning presets. The dashboard aggregates a
// types.xml (vanilla or a moded file) into category/usage/tier breakdowns and a
// top-items list, so an admin can see the shape of their loot economy at a
// glance. The tuning endpoint scales <nominal>/<min> across the file by a
// factor — the "more loot / less loot" one-click presets.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	dztypes "dayzmanager/internal/types"
)

type nameCount struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Nominal int    `json:"nominal,omitempty"`
}

// economyStats aggregates the selected types file for the dashboard.
func (h *handlers) economyStats(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	doc, path, err := h.loadTypesFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	catMap := map[string]*nameCount{}
	usageMap := map[string]int{}
	valueMap := map[string]int{}
	totalNominal, totalMin, withNominal := 0, 0, 0

	type topItem struct {
		Name     string `json:"name"`
		Nominal  int    `json:"nominal"`
		Category string `json:"category,omitempty"`
	}
	tops := make([]topItem, 0, len(doc.Types))

	for i := range doc.Types {
		t := &doc.Types[i]
		// "" = no <category>: the page names that bucket itself and does not
		// count it as one of the file's categories.
		cat := ""
		if t.Category != nil && t.Category.Name != "" {
			cat = t.Category.Name
		}
		cs := catMap[cat]
		if cs == nil {
			cs = &nameCount{Name: cat}
			catMap[cat] = cs
		}
		cs.Count++
		nom := 0
		if t.Nominal != nil {
			nom = *t.Nominal
			totalNominal += nom
			withNominal++
		}
		cs.Nominal += nom
		if t.Min != nil {
			totalMin += *t.Min
		}
		for _, u := range t.Usages {
			usageMap[u.Name]++
		}
		for _, v := range t.Values {
			valueMap[v.Name]++
		}
		tops = append(tops, topItem{Name: t.Name, Nominal: nom, Category: cat})
	}

	categories := mapToSortedByNominal(catMap)
	usages := countMapToSorted(usageMap, 12)
	values := countMapToSorted(valueMap, 0) // tiers are few — keep all
	sort.Slice(tops, func(i, j int) bool {
		if tops[i].Nominal != tops[j].Nominal {
			return tops[i].Nominal > tops[j].Nominal
		}
		return tops[i].Name < tops[j].Name
	})
	if len(tops) > 20 {
		tops = tops[:20]
	}

	// When the ×1 snapshot was taken (0 = none yet), so the page can say what a
	// reset would bring back — or that there is nothing to restore.
	var baselineAt int64
	if mission, merr := h.missionTemplate(); merr == nil {
		if bp, ok := h.lootBaseline(mission, file, false); ok {
			if st, serr := os.Stat(bp); serr == nil {
				baselineAt = st.ModTime().Unix()
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"file":         path,
		"total":        len(doc.Types),
		"totalNominal": totalNominal,
		"totalMin":     totalMin,
		"withNominal":  withNominal,
		"categories":   categories,
		"usages":       usages,
		"values":       values,
		"top":          tops,
		"baselineAt":   baselineAt,
	})
}

func mapToSortedByNominal(m map[string]*nameCount) []nameCount {
	out := make([]nameCount, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Nominal != out[j].Nominal {
			return out[i].Nominal > out[j].Nominal
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func countMapToSorted(m map[string]int, limit int) []nameCount {
	out := make([]nameCount, 0, len(m))
	for k, v := range m {
		out = append(out, nameCount{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// lootBaselinePath is where the untouched copy of a types file is kept the
// first time it is scaled, so the ×1 "reset" can restore the original amounts.
// Lives under the manager's own data dir, never inside the mission. The data
// dir is one per server, not per mission, so the snapshot is keyed by mission
// AND file: Chernarus' types.xml must never be restored into Livonia's.
func (h *handlers) lootBaselinePath(mission, file string) string {
	return filepath.Join(h.app.ManagerDir, "loot-baseline", sanitizeKey(mission), lootBaselineName(file))
}

// lootBaselineName — sanitizeKey caps length; distinct short types-file names
// don't collide.
func lootBaselineName(file string) string {
	if file == "" {
		file = "types.xml"
	}
	return sanitizeKey(filepath.Base(file)) + ".xml"
}

// lootBaseline returns the snapshot path for a mission's types file and whether
// a snapshot exists. Snapshots taken before they were keyed by mission sit
// directly in loot-baseline/; one is claimed (moved under the mission, when
// adopt is set) only when the server has a single mission — with several it
// may belong to another map, and restoring it there would be wrong.
func (h *handlers) lootBaseline(mission, file string, adopt bool) (string, bool) {
	p := h.lootBaselinePath(mission, file)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	legacy := filepath.Join(h.app.ManagerDir, "loot-baseline", lootBaselineName(file))
	if _, err := os.Stat(legacy); err != nil || len(findMissions(h.mpmissionsDir(), 0)) != 1 {
		return p, false
	}
	if !adopt {
		return legacy, true
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, false
	}
	if err := os.Rename(legacy, p); err != nil {
		return p, false
	}
	return p, true
}

// economyTune scales <nominal>/<min> across a types file by a factor, or (when
// reset is set) restores the pre-tuning amounts from the baseline snapshot.
// Writes the file only when something changed. Requires the server stopped.
func (h *handlers) economyTune(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File     string  `json:"file"`
		Factor   float64 `json:"factor"`
		Nominal  bool    `json:"nominal"`
		Min      bool    `json:"min"`
		Category string  `json:"category"`
		Reset    bool    `json:"reset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	basePath, hasBase := h.lootBaseline(mission, req.File, true)

	// ×1 — restore the amounts captured before the first scaling. Only
	// <nominal>/<min> come back, matched by type name: copying the whole file
	// over would also undo every Types / validator edit made since.
	if req.Reset {
		if !hasBase {
			// Never scaled through here, so there's nothing to restore.
			writeJSON(w, map[string]interface{}{"reset": true, "hadBaseline": false})
			return
		}
		base, berr := dztypes.Load(basePath)
		if berr != nil {
			http.Error(w, berr.Error(), http.StatusInternalServerError)
			return
		}
		restored := doc.RestoreAmounts(base)
		// SaveTypes backs the current file up before writing.
		if err := dztypes.SaveTypes(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// The file holds the snapshot's amounts again. The next scaling takes a
		// fresh snapshot, so edits made after this reset survive a later ×1.
		_ = os.Remove(basePath)
		writeJSON(w, map[string]interface{}{"reset": true, "hadBaseline": true, "restored": restored})
		return
	}

	if req.Factor < 0.05 || req.Factor > 20 {
		http.Error(w, "factor out of range (0.05–20)", http.StatusBadRequest)
		return
	}
	// Default to scaling nominal when the caller specifies neither field.
	if !req.Nominal && !req.Min {
		req.Nominal = true
	}
	// Snapshot the untouched file the first time it's scaled, so ×1 can undo
	// every later scaling back to these amounts.
	if !hasBase {
		if orig, oerr := os.ReadFile(path); oerr == nil {
			_ = os.MkdirAll(filepath.Dir(basePath), 0o755)
			_ = writeFileAtomic(basePath, orig)
		}
	}
	touched, floored := doc.Scale(req.Factor, dztypes.ScaleOpts{Nominal: req.Nominal, Min: req.Min, Category: req.Category})
	if touched > 0 {
		if err := dztypes.SaveTypes(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if floored == nil {
		floored = []string{}
	}
	writeJSON(w, map[string]interface{}{"touched": touched, "factor": req.Factor, "floored": floored})
}
