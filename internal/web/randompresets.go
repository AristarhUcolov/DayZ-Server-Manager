// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgrandompresets.xml library backend. Returns every cargo/attachment preset
// flattened into one list; the UI shows each item's real spawn chance (group
// chance × item weight ÷ sum of weights), the same math the Attachments editor
// uses, so a preset shared across many weapons is finally readable at a glance.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

func (h *handlers) randomPresetsPath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgrandompresets.xml"), nil
}

func (h *handlers) randomPresetsList(w http.ResponseWriter, r *http.Request) {
	path, err := h.randomPresetsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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

// randomPresetsSave rewrites cfgrandompresets.xml from the editor's full list.
// Requires the server stopped (it's a mission config file). Empty groups and
// blank item rows are dropped so the file stays clean.
func (h *handlers) randomPresetsSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Presets []struct {
			Kind   string  `json:"kind"`
			Name   string  `json:"name"`
			Chance float64 `json:"chance"`
			Items  []struct {
				Name   string  `json:"name"`
				Chance float64 `json:"chance"`
			} `json:"items"`
		} `json:"presets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var doc dztypes.RandomPresetsDoc
	seen := map[string]bool{} // kind|name — the game keys presets by name
	for _, p := range req.Presets {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			http.Error(w, "every preset needs a name", http.StatusBadRequest)
			return
		}
		key := p.Kind + "|" + strings.ToLower(name)
		if seen[key] {
			http.Error(w, "duplicate preset name: "+name, http.StatusBadRequest)
			return
		}
		seen[key] = true
		rp := dztypes.RandomPreset{Name: name, Chance: p.Chance}
		for _, it := range p.Items {
			if strings.TrimSpace(it.Name) == "" {
				continue
			}
			rp.Items = append(rp.Items, dztypes.PresetItem{Name: strings.TrimSpace(it.Name), Chance: it.Chance})
		}
		if p.Kind == "cargo" {
			doc.Cargo = append(doc.Cargo, rp)
		} else {
			doc.Attachments = append(doc.Attachments, rp)
		}
	}
	path, err := h.randomPresetsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := writeFileAtomic(path, dztypes.MarshalRandomPresets(&doc)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "cargo": len(doc.Cargo), "attachments": len(doc.Attachments)})
}
