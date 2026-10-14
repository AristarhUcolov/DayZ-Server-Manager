// Copyright (c) 2026 Aristarh Ucolov.
//
// Vehicles. DayZ has no "spawn a car" command: vehicles are Central Economy
// events. events.xml says how many of each (nominal/min/max and the colour
// variants as <child> entries with their own counts), cfgeventspawns.xml says
// where, and cfgspawnabletypes.xml says which parts each variant spawns with. Those three
// files are edited by hand today; this page brings them together per vehicle.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

type vehicleChild struct {
	Type     string `json:"type"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	LootMin  int    `json:"lootmin"`
	LootMax  int    `json:"lootmax"`
	Parts    int    `json:"parts"`    // attachment slots in cfgspawnabletypes
	Complete bool   `json:"complete"` // every slot and item at chance 1.00
	Known    bool   `json:"known"`    // has a cfgspawnabletypes entry
}

type vehicleRow struct {
	Name      string         `json:"name"`
	Nominal   *int           `json:"nominal,omitempty"`
	Min       *int           `json:"min,omitempty"`
	Max       *int           `json:"max,omitempty"`
	Lifetime  *int           `json:"lifetime,omitempty"`
	Restock   *int           `json:"restock,omitempty"`
	Saveable  *int           `json:"saveable,omitempty"`
	Active    *int           `json:"active,omitempty"`
	Children  []vehicleChild `json:"children"`
	Positions int            `json:"positions"`
	File      string         `json:"file"` // events file it is defined in, relative to the mission
	// Mod is the mod the panel imported this vehicle from (dzm_vehicles), and
	// ModActive whether that mod is loaded now — a vehicle of a switched-off
	// mod is switched off automatically and must stay off.
	Mod       string `json:"mod,omitempty"`
	ModActive *bool  `json:"modActive,omitempty"`
	// PartsChance is the slot chance every variant's parts share in
	// cfgspawnabletypes (1 = complete cars); absent when they differ.
	PartsChance *float64 `json:"partsChance,omitempty"`
}

// isVehicleEvent recognises vehicle events: vanilla names them Vehicle*, and
// mods follow the convention (VehicleHeli…, VehicleBike…).
func isVehicleEvent(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "vehicle")
}

func (h *handlers) vehiclesList(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	files := missionEcoFiles(missionDir, "events")
	if _, err := os.Stat(files[0].Path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	posCount := map[string]int{}
	if sp, err := dztypes.LoadEventSpawns(filepath.Join(missionDir, "cfgeventspawns.xml")); err == nil {
		for _, e := range sp.Events {
			posCount[strings.ToLower(e.Name)] = len(e.Pos)
		}
	}
	// Every spawnabletypes file the mission registers — modded vehicles keep
	// their part lists in their own files.
	spawnable := loadMissionSpawnable(missionDir)
	lookup := func(class string) *dztypes.SpawnableType {
		if e, ok := spawnable[strings.ToLower(class)]; ok {
			return e.st
		}
		return nil
	}

	cfg := h.app.Cfg()
	sources := loadVehicleSources(missionDir)
	modOf := func(event string) string {
		if m, ok := sources.Events[event]; ok {
			return m
		}
		for k, m := range sources.Events {
			if strings.EqualFold(k, event) {
				return m
			}
		}
		return ""
	}

	out := []vehicleRow{}
	var problems []string
	seen := map[string]bool{}
	for _, f := range files {
		doc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, f.Rel+": "+err.Error())
			}
			continue
		}
		for i := range doc.Events {
			e := &doc.Events[i]
			// db/events.xml first: a later file defining the same event again
			// is a duplicate the validator reports, not a second vehicle.
			if seen[strings.ToLower(e.Name)] || !looksLikeVehicle(e, lookup) {
				continue
			}
			seen[strings.ToLower(e.Name)] = true
			row := vehicleRow{Name: e.Name, Nominal: e.Nominal, Min: e.Min, Max: e.Max,
				Lifetime: e.Lifetime, Restock: e.Restock, Saveable: e.Saveable, Active: e.Active,
				Positions: posCount[strings.ToLower(e.Name)], Children: []vehicleChild{}, File: f.Rel}
			var chances []float64
			mixed := false
			if e.Children != nil {
				for _, c := range e.Children.Child {
					vc := vehicleChild{Type: c.Type, Min: c.Min, Max: c.Max, LootMin: c.LootMin, LootMax: c.LootMax}
					if st := lookup(c.Type); st != nil {
						vc.Known = true
						vc.Parts = len(st.Attachments)
						vc.Complete = partsComplete(st)
						for _, g := range st.Attachments {
							if g.Preset != "" {
								mixed = true
								continue
							}
							chances = append(chances, chanceOf(g.Chance))
						}
					}
					row.Children = append(row.Children, vc)
				}
			}
			if !mixed && len(chances) > 0 {
				same := true
				for _, c := range chances[1:] {
					if c-chances[0] > 0.005 || chances[0]-c > 0.005 {
						same = false
						break
					}
				}
				if same {
					v := chances[0]
					row.PartsChance = &v
				}
			}
			if m := modOf(e.Name); m != "" {
				on := modActive(cfg.Mods, cfg.ServerMods, m)
				row.Mod, row.ModActive = m, &on
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, map[string]interface{}{"file": files[0].Path, "vehicles": out, "map": h.currentMap(), "problems": problems})
}

// partsComplete: every attachment slot is certain to spawn (slot chance 1,
// and a lone item not down-weighted).
func partsComplete(st *dztypes.SpawnableType) bool {
	if len(st.Attachments) == 0 {
		return false
	}
	for _, g := range st.Attachments {
		if g.Preset != "" || !isOne(g.Chance) {
			return false
		}
		if len(g.Items) == 1 && !isOne(g.Items[0].Chance) {
			return false
		}
	}
	return true
}

// chanceOf reads a cfgspawnabletypes chance; a missing one counts as 1.
func chanceOf(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1
	}
	var f float64
	if _, err := fmt.Sscan(s, &f); err != nil {
		return 1
	}
	return f
}

func isOne(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true // DayZ reads a missing chance as 1
	}
	var f float64
	_, err := fmt.Sscan(s, &f)
	return err == nil && f >= 0.999
}

// vehiclesParts sets how complete a vehicle class spawns: every attachment
// slot (and each single-item slot's item) of each variant gets the given
// chance in cfgspawnabletypes.xml — where vanilla defines vehicle parts (its
// event children carry lootmin/lootmax 0). chance 1 = cars spawn with every
// part. Surgical write, backup first; server must be stopped.
func (h *handlers) vehiclesParts(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Event  string  `json:"event"`
		Type   string  `json:"type"` // one variant, or "" for every variant of the event
		Chance float64 `json:"chance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Event == "" {
		http.Error(w, "event required", http.StatusBadRequest)
		return
	}
	if req.Chance < 0 || req.Chance > 1 {
		http.Error(w, "chance must be 0..1", http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	var ev *dztypes.Event
	for _, f := range missionEcoFiles(missionDir, "events") {
		if doc, err := dztypes.LoadEvents(f.Path); err == nil {
			if ev = doc.Find(req.Event); ev != nil {
				break
			}
		}
	}
	if ev == nil || ev.Children == nil {
		http.Error(w, "event has no vehicle variants", http.StatusNotFound)
		return
	}
	// Each variant's parts are written in the file that defines them —
	// cfgspawnabletypes.xml or a mod's registered spawnabletypes file.
	byName := loadMissionSpawnable(missionDir)
	chance := fmt.Sprintf("%.2f", req.Chance)
	kids := ev.Children.Child
	touched := 0
	backedUp := map[string]bool{}
	for i := range kids {
		c := &kids[i]
		if req.Type != "" && !strings.EqualFold(c.Type, req.Type) {
			continue
		}
		ent, ok := byName[strings.ToLower(c.Type)]
		st, spPath := ent.st, ent.path
		if !ok || st == nil || len(st.Attachments) == 0 {
			continue
		}
		for gi := range st.Attachments {
			g := &st.Attachments[gi]
			if g.Preset != "" {
				continue // a preset reference carries its own chances
			}
			// The slot chance is the part's chance. A single-item slot's item
			// is set to 1.00 so the result is the same whether DayZ reads item
			// chances as relative weights or as a second roll; a multi-item
			// slot keeps its weights (which variant of the part appears).
			g.Chance = chance
			if len(g.Items) == 1 {
				g.Items[0].Chance = "1.00"
			}
		}
		if !backedUp[spPath] {
			_ = util.BackupBeforeWrite(spPath)
			backedUp[spPath] = true
		}
		if err := dztypes.SaveSpawnableType(spPath, st); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		touched++
	}
	if touched == 0 {
		http.Error(w, "no variant of this vehicle has parts in cfgspawnabletypes.xml", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "variants": touched})
}
