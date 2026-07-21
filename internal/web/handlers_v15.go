// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.15.0 handlers: the attachments ("обвесы") editor over cfgspawnabletypes.xml
// — the file that decides which magazine / optic / buttstock a spawned weapon
// comes with, and with what probability.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
)

// spawnablePath resolves cfgspawnabletypes.xml for the active mission.
func (h *handlers) spawnablePath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return dztypes.SpawnablePath(h.app.ServerDir, mission), nil
}

// spawnableList returns every <type> plus the class names found in types.xml
// (used by the UI for autocomplete, so modded item names work too).
func (h *handlers) spawnableList(w http.ResponseWriter, r *http.Request) {
	path, err := h.spawnablePath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err != nil {
		writeJSON(w, map[string]interface{}{"path": path, "exists": false, "types": []interface{}{}})
		return
	}
	list, err := dztypes.LoadSpawnable(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	writeJSON(w, map[string]interface{}{
		"path":   path,
		"exists": true,
		"types":  list,
		"count":  len(list),
	})
}

// spawnableClassNames lists item class names from the mission's types.xml so
// the attachment inputs can autocomplete real (including modded) classes
// instead of relying on the user typing them correctly.
func (h *handlers) spawnableClassNames(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	seen := map[string]bool{}
	var names []string
	// Reuse loadTypesFile so path resolution stays in one place ("" = the
	// mission's db/types.xml, anything else = moded_types/<file>).
	add := func(file string) {
		doc, _, err := h.loadTypesFile(file)
		if err != nil {
			return
		}
		for _, t := range doc.Types {
			if t.Name != "" && !seen[t.Name] {
				seen[t.Name] = true
				names = append(names, t.Name)
			}
		}
	}
	add("")
	// Custom types files count too — modded magazines/optics live there.
	if entries, err := os.ReadDir(dztypes.ModedDir(h.app.ServerDir, mission)); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				add(e.Name())
			}
		}
	}
	sort.Strings(names)
	writeJSON(w, map[string]interface{}{"names": names, "count": len(names)})
}

// spawnableItem is GET (one type) / POST (save) / DELETE (remove).
func (h *handlers) spawnableItem(w http.ResponseWriter, r *http.Request) {
	path, err := h.spawnablePath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		list, err := dztypes.LoadSpawnable(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, t := range list {
			if strings.EqualFold(t.Name, name) {
				writeJSON(w, t)
				return
			}
		}
		http.Error(w, "type not found: "+name, http.StatusNotFound)
		return
	}

	// Mutating from here on — DayZ holds locks on mission files while running.
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	if r.Method == http.MethodDelete {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		removed, err := dztypes.DeleteSpawnableType(path, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if removed == 0 {
			http.Error(w, "type not found: "+name, http.StatusNotFound)
			return
		}
		// removed > 1 means the file carried duplicate entries — worth saying,
		// because DayZ was silently using only one of them.
		writeJSON(w, map[string]interface{}{"status": "deleted", "name": name, "removed": removed})
		return
	}

	var st dztypes.SpawnableType
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st.Name = strings.TrimSpace(st.Name)
	if st.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	// Validate every chance up front: a bad value would make DayZ drop the
	// whole entry silently, which is exactly what this editor prevents.
	for _, grp := range append(append([]dztypes.SpawnGroup{}, st.Attachments...), st.Cargo...) {
		if !dztypes.ValidChance(grp.Chance) {
			http.Error(w, "invalid group chance: "+grp.Chance, http.StatusBadRequest)
			return
		}
		for _, it := range grp.Items {
			if !dztypes.ValidChance(it.Chance) {
				http.Error(w, "invalid item chance for "+it.Name+": "+it.Chance, http.StatusBadRequest)
				return
			}
		}
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "cfgspawnabletypes.xml not found in the active mission", http.StatusBadRequest)
		return
	}
	if err := dztypes.SaveSpawnableType(path, &st); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved", "name": st.Name})
}

// ---------------------------------------------------------------------------
// Built-in weapon templates.
//
// These are starting points, not gospel: the UI drops them into the editor so
// the admin can adjust chances and swap classes before saving. Class names are
// vanilla DayZ; the autocomplete list from types.xml is what makes modded
// weapons workable.

type spawnPreset struct {
	ID     string                `json:"id"`
	Label  string                `json:"label"`
	Weapon string                `json:"weapon"`
	Type   dztypes.SpawnableType `json:"type"`
}

func grp(chance string, items ...dztypes.SpawnItem) dztypes.SpawnGroup {
	return dztypes.SpawnGroup{Chance: chance, Items: items}
}
func itm(name, chance string) dztypes.SpawnItem {
	return dztypes.SpawnItem{Name: name, Chance: chance}
}

func builtinSpawnPresets() []spawnPreset {
	return []spawnPreset{
		{
			ID: "akm", Label: "AKM", Weapon: "AKM",
			Type: dztypes.SpawnableType{
				Name: "AKM",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_AKM_30Rnd", "0.85"), itm("Mag_AKM_Drum75Rnd", "0.15")),
					grp("0.35", itm("KobraOptic", "0.50"), itm("PSO1Optic", "0.30"), itm("KashtanOptic", "0.20")),
					grp("0.80", itm("AK_WoodBttstck", "0.50"), itm("AK_PlasticBttstck", "0.35"), itm("AK_FoldingBttstck", "0.15")),
					grp("0.80", itm("AK_WoodHndgrd", "0.50"), itm("AK_PlasticHndgrd", "0.35"), itm("AK_RailHndgrd", "0.15")),
					grp("0.10", itm("AK_Suppressor", "1.00")),
				},
			},
		},
		{
			ID: "ak74", Label: "AK-74", Weapon: "AK74",
			Type: dztypes.SpawnableType{
				Name: "AK74",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_AK74_30Rnd", "0.80"), itm("Mag_AK74_45Rnd", "0.20")),
					grp("0.35", itm("KobraOptic", "0.55"), itm("PSO1Optic", "0.30"), itm("KashtanOptic", "0.15")),
					grp("0.80", itm("AK74_WoodBttstck", "0.55"), itm("AK74_PlasticBttstck", "0.45")),
					grp("0.80", itm("AK74_WoodHndgrd", "0.50"), itm("AK74_PlasticHndgrd", "0.35"), itm("AK74_RailHndgrd", "0.15")),
					grp("0.10", itm("AK_Suppressor", "1.00")),
				},
			},
		},
		{
			ID: "aks74u", Label: "AKS-74U", Weapon: "AKS74U",
			Type: dztypes.SpawnableType{
				Name: "AKS74U",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_AK74_30Rnd", "1.00")),
					grp("0.60", itm("AKS74U_Bttstck", "1.00")),
					grp("0.10", itm("AK_Suppressor", "1.00")),
				},
			},
		},
		{
			ID: "m4a1", Label: "M4-A1", Weapon: "M4A1",
			Type: dztypes.SpawnableType{
				Name: "M4A1",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_STANAG_30Rnd", "0.80"), itm("Mag_STANAG_60Rnd", "0.20")),
					grp("0.35", itm("M4_T3NRDSOptic", "0.40"), itm("ACOGOptic", "0.35"), itm("M68Optic", "0.25")),
					grp("0.80", itm("M4_OEBttstck", "0.40"), itm("M4_MPBttstck", "0.35"), itm("M4_CQBBttstck", "0.25")),
					grp("0.80", itm("M4_RISHndgrd", "0.50"), itm("M4_PlasticHndgrd", "0.50")),
					grp("0.10", itm("M4_Suppressor", "1.00")),
				},
			},
		},
		{
			ID: "mosin", Label: "Mosin 91/30", Weapon: "Mosin9130",
			Type: dztypes.SpawnableType{
				Name: "Mosin9130",
				Attachments: []dztypes.SpawnGroup{
					grp("0.30", itm("PUScopeOptic", "1.00")),
					grp("0.25", itm("Mosin_Bayonet", "1.00")),
					grp("0.15", itm("Mosin_Compensator", "1.00")),
				},
			},
		},
		{
			ID: "svd", Label: "SVD", Weapon: "SVD",
			Type: dztypes.SpawnableType{
				Name: "SVD",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_SVD_10Rnd", "1.00")),
					grp("0.70", itm("PSO1Optic", "0.60"), itm("PSO11Optic", "0.40")),
				},
			},
		},
		{
			ID: "empty", Label: "Blank template", Weapon: "",
			Type: dztypes.SpawnableType{
				Attachments: []dztypes.SpawnGroup{grp("1.00", itm("", "1.00"))},
			},
		},
	}
}

func (h *handlers) spawnablePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"presets": builtinSpawnPresets()})
}
