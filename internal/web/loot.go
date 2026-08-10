// Copyright (c) 2026 Aristarh Ucolov.
//
// "Where does this item spawn?" — a reverse loot lookup. Given part of a class
// name, it searches types.xml and every custom (moded) types file and returns,
// for each match, the economy fields that decide where and how often it spawns:
// category, usage (locations), value (tiers), tags, nominal and min. Answers the
// single most common player question without opening any file.
package web

import (
	"net/http"
	"os"
	"strings"

	dztypes "dayzmanager/internal/types"
)

func (h *handlers) lootWhere(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, map[string]interface{}{"query": q, "hits": []interface{}{}, "total": 0, "tooShort": true})
		return
	}
	needle := strings.ToLower(q)

	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// types.xml first, then every custom types file.
	files := []string{""}
	if entries, err := os.ReadDir(dztypes.ModedDir(h.app.ServerDir, mission)); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				files = append(files, e.Name())
			}
		}
	}

	type lootHit struct {
		File     string   `json:"file"`
		Name     string   `json:"name"`
		Nominal  *int     `json:"nominal,omitempty"`
		Min      *int     `json:"min,omitempty"`
		Lifetime *int     `json:"lifetime,omitempty"`
		Category string   `json:"category,omitempty"`
		Usages   []string `json:"usages,omitempty"`
		Values   []string `json:"values,omitempty"`
		Tags     []string `json:"tags,omitempty"`
	}
	const maxHits = 100
	hits := []lootHit{}
	truncated := false

	for _, f := range files {
		if len(hits) >= maxHits {
			truncated = true
			break
		}
		doc, _, err := h.loadTypesFile(f)
		if err != nil {
			continue
		}
		label := f
		if f == "" {
			label = "types.xml"
		}
		for i := range doc.Types {
			t := &doc.Types[i]
			if !strings.Contains(strings.ToLower(t.Name), needle) {
				continue
			}
			res := lootHit{File: label, Name: t.Name, Nominal: t.Nominal, Min: t.Min, Lifetime: t.Lifetime}
			if t.Category != nil {
				res.Category = t.Category.Name
			}
			for _, u := range t.Usages {
				res.Usages = append(res.Usages, u.Name)
			}
			for _, v := range t.Values {
				res.Values = append(res.Values, v.Name)
			}
			for _, tg := range t.Tags {
				res.Tags = append(res.Tags, tg.Name)
			}
			hits = append(hits, res)
			if len(hits) >= maxHits {
				truncated = true
				break
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"query":     q,
		"hits":      hits,
		"total":     len(hits),
		"truncated": truncated,
	})
}
