// Copyright (c) 2026 Aristarh Ucolov.
//
// "Where does this item spawn?" — a reverse loot lookup. Given part of a class
// name, it searches every types file DayZ loads for the mission (types.xml and
// each file cfgeconomycore.xml registers) plus moded_types files nobody
// registered, and returns, for each match, the economy fields that decide where
// and how often it spawns: category, usage (locations), value (tiers), tags,
// nominal, min and lifetime. It also lists the other ways the item reaches the
// world — mounted on or packed in another item (cfgspawnabletypes), through a
// random preset (cfgrandompresets) or as part of an event (events.xml) — so an
// item with no usage is not simply reported as "never spawns".
package web

import (
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	dztypes "dayzmanager/internal/types"
)

// lootRef is one other way an item spawns.
type lootRef struct {
	Kind       string  `json:"kind"`                 // attachment | cargo | preset | event
	Parent     string  `json:"parent"`               // the item, preset or event it comes with
	Chance     float64 `json:"chance,omitempty"`     // real chance 0..1 (slot chance × weight share)
	PresetKind string  `json:"presetKind,omitempty"` // cargo | attachments, for a preset
}

// chanceVal reads a chance/weight the way DayZ does: blank means 1.
func chanceVal(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

// lootRefs indexes, for the lower-cased names in want, every attachment / cargo
// slot, random preset and event child that can spawn them.
func (h *handlers) lootRefs(missionDir string, want map[string]bool) map[string][]lootRef {
	out := map[string][]lootRef{}
	addGroups := func(kind, parent string, groups []dztypes.SpawnGroup) {
		for _, g := range groups {
			if g.Preset != "" || len(g.Items) == 0 {
				continue // a preset's items are indexed from cfgrandompresets.xml
			}
			total := 0.0
			for _, it := range g.Items {
				total += chanceVal(it.Chance)
			}
			for _, it := range g.Items {
				k := strings.ToLower(it.Name)
				if !want[k] {
					continue
				}
				c := 0.0
				if total > 0 {
					c = chanceVal(g.Chance) * chanceVal(it.Chance) / total
				}
				out[k] = append(out[k], lootRef{Kind: kind, Parent: parent, Chance: c})
			}
		}
	}
	for _, f := range missionEcoFiles(missionDir, "spawnabletypes") {
		list, err := dztypes.LoadSpawnable(f.Path)
		if err != nil {
			continue
		}
		for i := range list {
			addGroups("attachment", list[i].Name, list[i].Attachments)
			addGroups("cargo", list[i].Name, list[i].Cargo)
		}
	}
	if doc, err := dztypes.LoadRandomPresets(filepath.Join(missionDir, "cfgrandompresets.xml")); err == nil {
		addPresets := func(kind string, ps []dztypes.RandomPreset) {
			for _, p := range ps {
				total := 0.0
				for _, it := range p.Items {
					total += it.Chance
				}
				for _, it := range p.Items {
					k := strings.ToLower(it.Name)
					if !want[k] {
						continue
					}
					c := 0.0
					if total > 0 {
						c = p.Chance * it.Chance / total
					}
					out[k] = append(out[k], lootRef{Kind: "preset", Parent: p.Name, Chance: c, PresetKind: kind})
				}
			}
		}
		addPresets("cargo", doc.Cargo)
		addPresets("attachments", doc.Attachments)
	}
	for _, f := range missionEcoFiles(missionDir, "events") {
		doc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			continue
		}
		for _, e := range doc.Events {
			if e.Children == nil {
				continue
			}
			for _, c := range e.Children.Child {
				k := strings.ToLower(c.Type)
				if want[k] {
					out[k] = append(out[k], lootRef{Kind: "event", Parent: e.Name})
				}
			}
		}
	}
	return out
}

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

	type lootHit struct {
		File       string `json:"file"`
		Registered bool   `json:"registered"` // DayZ loads the file this entry is in
		// What the Types page's file picker calls the file ("" = types.xml);
		// set only when Types can open it.
		TypesFile string    `json:"typesFile"`
		Editable  bool      `json:"editable"`
		Name      string    `json:"name"`
		Nominal   *int      `json:"nominal,omitempty"`
		Min       *int      `json:"min,omitempty"`
		Lifetime  *int      `json:"lifetime,omitempty"`
		Category  string    `json:"category,omitempty"`
		Usages    []string  `json:"usages,omitempty"`
		Values    []string  `json:"values,omitempty"`
		Tags      []string  `json:"tags,omitempty"`
		AlsoIn    []lootRef `json:"alsoIn,omitempty"`
		AlsoMore  int       `json:"alsoMore,omitempty"` // further references not listed
	}
	const maxHits = 100
	const maxRefs = 12
	hits := []lootHit{}
	truncated := false

	for _, f := range h.missionTypeFiles() {
		if len(hits) >= maxHits {
			truncated = true
			break
		}
		doc, err := dztypes.Load(f.Path)
		if err != nil {
			continue
		}
		typesFile, editable := "", false
		if f.Label == "types.xml" {
			editable = true
		} else if f.Moded {
			typesFile, editable = filepath.Base(f.Path), true
		}
		for i := range doc.Types {
			t := &doc.Types[i]
			if !strings.Contains(strings.ToLower(t.Name), needle) {
				continue
			}
			res := lootHit{File: f.Label, Registered: f.Registered, TypesFile: typesFile, Editable: editable,
				Name: t.Name, Nominal: t.Nominal, Min: t.Min, Lifetime: t.Lifetime}
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

	if len(hits) > 0 {
		want := map[string]bool{}
		for _, hit := range hits {
			want[strings.ToLower(hit.Name)] = true
		}
		refs := h.lootRefs(dztypes.MissionDir(h.app.ServerDir, mission), want)
		for i := range hits {
			list := append([]lootRef(nil), refs[strings.ToLower(hits[i].Name)]...)
			// Likeliest first; events (no chance) last.
			sort.SliceStable(list, func(a, b int) bool { return list[a].Chance > list[b].Chance })
			if len(list) > maxRefs {
				hits[i].AlsoMore = len(list) - maxRefs
				list = list[:maxRefs]
			}
			hits[i].AlsoIn = list
		}
	}

	writeJSON(w, map[string]interface{}{
		"query":     q,
		"hits":      hits,
		"total":     len(hits),
		"truncated": truncated,
	})
}
