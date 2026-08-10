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
		cat := "uncategorized"
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

// economyTune scales <nominal>/<min> across a types file by a factor. Writes
// the file only when something changed. Requires the server stopped.
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	touched := doc.Scale(req.Factor, dztypes.ScaleOpts{Nominal: req.Nominal, Min: req.Min, Category: req.Category})
	if touched > 0 {
		if err := dztypes.SaveTypes(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"touched": touched, "factor": req.Factor})
}
