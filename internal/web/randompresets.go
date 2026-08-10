// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgrandompresets.xml library backend. Returns every cargo/attachment preset
// flattened into one list; the UI shows each item's real spawn chance (group
// chance × item weight ÷ sum of weights), the same math the Attachments editor
// uses, so a preset shared across many weapons is finally readable at a glance.
package web

import (
	"net/http"
	"path/filepath"
	"sort"

	dztypes "dayzmanager/internal/types"
)

func (h *handlers) randomPresetsList(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgrandompresets.xml")
	doc, err := dztypes.LoadRandomPresets(path)
	if err != nil {
		writeJSON(w, map[string]interface{}{"path": path, "exists": false, "presets": []interface{}{}})
		return
	}

	type outPreset struct {
		Kind   string               `json:"kind"`
		Name   string               `json:"name"`
		Chance float64              `json:"chance"`
		Items  []dztypes.PresetItem `json:"items"`
	}
	presets := make([]outPreset, 0, len(doc.Cargo)+len(doc.Attachments))
	for _, p := range doc.Cargo {
		presets = append(presets, outPreset{Kind: "cargo", Name: p.Name, Chance: p.Chance, Items: p.Items})
	}
	for _, p := range doc.Attachments {
		presets = append(presets, outPreset{Kind: "attachments", Name: p.Name, Chance: p.Chance, Items: p.Items})
	}
	sort.SliceStable(presets, func(i, j int) bool {
		if presets[i].Kind != presets[j].Kind {
			return presets[i].Kind < presets[j].Kind
		}
		return presets[i].Name < presets[j].Name
	})

	writeJSON(w, map[string]interface{}{"path": path, "exists": true, "presets": presets})
}
