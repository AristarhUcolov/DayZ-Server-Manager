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

	// Row is a list entry enriched with what the UI needs to describe it
	// without re-deriving anything client-side.
	type Row struct {
		dztypes.SpawnableType
		Category   string `json:"category,omitempty"` // from types.xml: weapons, clothes, containers…
		Kind       string `json:"kind"`               // weapon | clothing | container | vehicle | other
		AttSlots   int    `json:"attSlots"`
		CargoSlots int    `json:"cargoSlots"`
	}
	cats := h.typeCategories()
	rows := make([]Row, 0, len(list))
	counts := map[string]int{}
	for i := range list {
		st := list[i]
		r := Row{
			SpawnableType: st,
			Category:      cats[strings.ToLower(st.Name)],
			AttSlots:      len(st.Attachments),
			CargoSlots:    len(st.Cargo),
		}
		r.Kind = spawnKind(&st, r.Category)
		counts[r.Kind]++
		rows = append(rows, r)
	}

	writeJSON(w, map[string]interface{}{
		"path":   path,
		"exists": true,
		"types":  rows,
		"count":  len(rows),
		"kinds":  counts,
	})
}

// typeCategories maps a lower-cased class name to its types.xml <category>.
// Vanilla + every registered moded_types file, so modded gear is labelled too.
func (h *handlers) typeCategories() map[string]string {
	out := map[string]string{}
	add := func(file string) {
		doc, _, err := h.loadTypesFile(file)
		if err != nil {
			return
		}
		for i := range doc.Types {
			t := &doc.Types[i]
			if t.Name == "" || t.Category == nil || t.Category.Name == "" {
				continue
			}
			out[strings.ToLower(t.Name)] = t.Category.Name
		}
	}
	add("")
	if mission, err := h.missionTemplate(); err == nil {
		if entries, err := os.ReadDir(dztypes.ModedDir(h.app.ServerDir, mission)); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
					add(e.Name())
				}
			}
		}
	}
	return out
}

// spawnKind labels an entry for the UI. types.xml's category is authoritative
// where it exists; roughly a third of the entries (vehicles, wrecks, tents)
// have none, so the shape of the entry itself decides those.
func spawnKind(st *dztypes.SpawnableType, category string) string {
	switch strings.ToLower(category) {
	case "weapons":
		return "weapon"
	case "clothes":
		return "clothing"
	case "containers":
		return "container"
	case "tools", "food", "materials", "explosives":
		return "other"
	}
	name := strings.ToLower(st.Name)
	switch {
	case strings.Contains(name, "wreck"), strings.Contains(name, "civiliansedan"),
		strings.Contains(name, "hatchback"), strings.Contains(name, "offroad"),
		strings.Contains(name, "truck"), strings.Contains(name, "van_"),
		strings.Contains(name, "sedan"), strings.Contains(name, "boat"):
		return "vehicle"
	case st.Hoarder:
		// <hoarder> marks persistent storage: barrels, tents, stashes.
		return "container"
	case len(st.Cargo) > 0 && len(st.Attachments) == 0:
		return "container"
	case len(st.Attachments) > 0:
		return "weapon"
	}
	return "other"
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
	Kind   string                `json:"kind"` // weapon | clothing | container | vehicle
	Type   dztypes.SpawnableType `json:"type"`
}

// cargoGrp builds a <cargo> slot — what spawns INSIDE a container, backpack
// or vehicle, as opposed to what is mounted on it.
func cargoGrp(chance string, items ...dztypes.SpawnItem) dztypes.SpawnGroup {
	return dztypes.SpawnGroup{Chance: chance, Items: items}
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
			ID: "akm", Label: "AKM", Weapon: "AKM", Kind: "weapon",
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
			ID: "ak74", Label: "AK-74", Weapon: "AK74", Kind: "weapon",
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
			ID: "aks74u", Label: "AKS-74U", Weapon: "AKS74U", Kind: "weapon",
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
			ID: "m4a1", Label: "M4-A1", Weapon: "M4A1", Kind: "weapon",
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
			ID: "mosin", Label: "Mosin 91/30", Weapon: "Mosin9130", Kind: "weapon",
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
			ID: "svd", Label: "SVD", Weapon: "SVD", Kind: "weapon",
			Type: dztypes.SpawnableType{
				Name: "SVD",
				Attachments: []dztypes.SpawnGroup{
					grp("1.00", itm("Mag_SVD_10Rnd", "1.00")),
					grp("0.70", itm("PSO1Optic", "0.60"), itm("PSO11Optic", "0.40")),
				},
			},
		},
		// --- clothing: vests and jackets carry attachments AND pocket cargo ---
		{
			ID: "vest", Label: "Plate carrier (vest)", Weapon: "PlateCarrierVest", Kind: "clothing",
			Type: dztypes.SpawnableType{
				Name: "PlateCarrierVest",
				Attachments: []dztypes.SpawnGroup{
					grp("0.40", itm("PlateCarrierHolster", "0.50"), itm("PlateCarrierPouches", "0.50")),
				},
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("0.30", itm("BandageDressing", "0.60"), itm("Rag", "0.40")),
				},
			},
		},
		{
			ID: "jacket", Label: "Jacket (pocket loot)", Weapon: "M65Jacket", Kind: "clothing",
			Type: dztypes.SpawnableType{
				Name: "M65Jacket",
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("0.25", itm("Rag", "0.40"), itm("BandageDressing", "0.30"), itm("Matchbox", "0.30")),
				},
			},
		},
		{
			ID: "backpack", Label: "Backpack", Weapon: "MountainBag_Blue", Kind: "clothing",
			Type: dztypes.SpawnableType{
				Name: "MountainBag_Blue",
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("0.35", itm("Canteen", "0.35"), itm("TunaCan", "0.35"), itm("Rope", "0.30")),
				},
			},
		},
		// --- containers: crates, barrels, tents ---
		{
			ID: "crate", Label: "Wooden crate", Weapon: "WoodenCrate", Kind: "container",
			Type: dztypes.SpawnableType{
				Name: "WoodenCrate",
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("1.00", itm("Nail", "0.40"), itm("WoodenPlank", "0.35"), itm("MetalWire", "0.25")),
					cargoGrp("0.30", itm("Hammer", "0.50"), itm("Screwdriver", "0.50")),
				},
			},
		},
		{
			ID: "seachest", Label: "Sea chest", Weapon: "SeaChest", Kind: "container",
			Type: dztypes.SpawnableType{
				Name: "SeaChest",
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("0.60", itm("BandageDressing", "0.50"), itm("TetracyclineAntibiotics", "0.50")),
				},
			},
		},
		{
			ID: "barrel", Label: "Barrel (persistent storage)", Weapon: "Barrel_Green", Kind: "container",
			Type: dztypes.SpawnableType{
				Name:    "Barrel_Green",
				Hoarder: true,
			},
		},
		// --- vehicles: parts are attachments, boot loot is cargo ---
		{
			ID: "car", Label: "Car (parts + boot)", Weapon: "Hatchback_02", Kind: "vehicle",
			Type: dztypes.SpawnableType{
				Name: "Hatchback_02",
				Attachments: []dztypes.SpawnGroup{
					grp("0.50", itm("HatchbackWheel", "1.00")),
					grp("0.30", itm("CarBattery", "1.00")),
					grp("0.30", itm("SparkPlug", "1.00")),
				},
				Cargo: []dztypes.SpawnGroup{
					cargoGrp("0.40", itm("Wrench", "0.50"), itm("LugWrench", "0.50")),
				},
			},
		},
		{
			ID: "empty", Label: "Blank template", Weapon: "", Kind: "weapon",
			Type: dztypes.SpawnableType{
				Attachments: []dztypes.SpawnGroup{grp("1.00", itm("", "1.00"))},
			},
		},
	}
}

func (h *handlers) spawnablePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"presets": builtinSpawnPresets()})
}
