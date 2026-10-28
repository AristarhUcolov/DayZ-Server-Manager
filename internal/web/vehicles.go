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

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
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
	// Fuel / Coolant: how full the tank and the radiator are at spawn, set by
	// the panel's tweaks mod (absent = the game's default).
	Fuel    *config.FluidRange `json:"fuel,omitempty"`
	Coolant *config.FluidRange `json:"coolant,omitempty"`
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
			for _, vt := range cfg.VehicleTweaks {
				if strings.EqualFold(vt.Event, e.Name) {
					row.Fuel, row.Coolant = vt.Fuel, vt.Coolant
				}
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, map[string]interface{}{"file": files[0].Path, "vehicles": out, "map": h.currentMap(), "problems": problems,
		"tweaksMod": app.TweaksMod})
}

// vehiclesTweak saves one vehicle event's spawn condition: how full the tank
// and the radiator are (nil = the game's default). The tweaks mod is written
// at once when the server is stopped, otherwise before its next start.
func (h *handlers) vehiclesTweak(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Event   string             `json:"event"`
		Classes []string           `json:"classes"`
		Fuel    *config.FluidRange `json:"fuel"`
		Coolant *config.FluidRange `json:"coolant"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Event = strings.TrimSpace(req.Event)
	if req.Event == "" {
		http.Error(w, "event required", http.StatusBadRequest)
		return
	}
	for _, fr := range []*config.FluidRange{req.Fuel, req.Coolant} {
		if fr != nil && (fr.Min < 0 || fr.Max > 1 || fr.Min > fr.Max) {
			http.Error(w, "a fill level runs from 0 to 1, minimum first", http.StatusBadRequest)
			return
		}
	}
	var classes []string
	for _, c := range req.Classes {
		// A class name goes into the generated script: no quotes, no breaks.
		if c = strings.TrimSpace(c); c != "" && !strings.ContainsAny(c, "\"\\\r\n") {
			classes = append(classes, c)
		}
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		out := c.VehicleTweaks[:0:0]
		for _, vt := range c.VehicleTweaks {
			if !strings.EqualFold(vt.Event, req.Event) {
				out = append(out, vt)
			}
		}
		if req.Fuel != nil || req.Coolant != nil {
			out = append(out, config.VehicleTweak{Event: req.Event, Classes: classes, Fuel: req.Fuel, Coolant: req.Coolant})
		}
		c.VehicleTweaks = out
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	applied := false
	if !h.app.ServerIsRunning() {
		if err := h.app.SyncServerTweaks(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		applied = true
	}
	writeJSON(w, map[string]interface{}{"ok": true, "applied": applied})
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

// vehicleVariant is one colour variant's parts as cfgspawnabletypes has them.
type vehicleVariant struct {
	Type  string                 `json:"type"`
	File  string                 `json:"file"`            // where its entry is (or would go), relative to the mission
	Entry *dztypes.SpawnableType `json:"entry,omitempty"` // absent: no entry yet
}

// findVehicleEvent returns the event (from any registered events file).
func findVehicleEvent(missionDir, name string) *dztypes.Event {
	for _, f := range missionEcoFiles(missionDir, "events") {
		if doc, err := dztypes.LoadEvents(f.Path); err == nil {
			if ev := doc.Find(name); ev != nil {
				return ev
			}
		}
	}
	return nil
}

// vehiclesVariants is the Vehicles page's parts editor. GET ?event= answers
// each colour variant's cfgspawnabletypes entry; POST {event, variants: [{type,
// attachments, cargo}]} writes their attachment groups — and what is in the
// trunk, when cargo is sent — each into the file that defines the variant (a
// mod's registered spawnabletypes file, or cfgspawnabletypes.xml for a variant
// without an entry yet). Everything else in an entry (damage, tags) is kept.
// Backups first; server stopped.
func (h *handlers) vehiclesVariants(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	mainPath := filepath.Join(missionDir, "cfgspawnabletypes.xml")
	rel := func(p string) string {
		if r, err := filepath.Rel(missionDir, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	if r.Method == http.MethodGet {
		ev := findVehicleEvent(missionDir, strings.TrimSpace(r.URL.Query().Get("event")))
		if ev == nil {
			http.Error(w, "vehicle not found", http.StatusNotFound)
			return
		}
		byName := loadMissionSpawnable(missionDir)
		out := []vehicleVariant{}
		if ev.Children != nil {
			for _, c := range ev.Children.Child {
				vv := vehicleVariant{Type: c.Type, File: rel(mainPath)}
				if e, ok := byName[strings.ToLower(c.Type)]; ok {
					vv.Entry, vv.File = e.st, rel(e.path)
				}
				out = append(out, vv)
			}
		}
		writeJSON(w, map[string]interface{}{"event": ev.Name, "variants": out})
		return
	}

	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Event    string `json:"event"`
		Variants []struct {
			Type        string                `json:"type"`
			Attachments []dztypes.SpawnGroup  `json:"attachments"`
			Cargo       *[]dztypes.SpawnGroup `json:"cargo"` // nil: left as it is
		} `json:"variants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ev := findVehicleEvent(missionDir, strings.TrimSpace(req.Event))
	if ev == nil || ev.Children == nil {
		http.Error(w, "vehicle not found", http.StatusNotFound)
		return
	}
	// Only the event's own variants: the editor must not write other entries.
	variant := map[string]string{}
	for _, c := range ev.Children.Child {
		variant[strings.ToLower(c.Type)] = c.Type
	}
	byName := loadMissionSpawnable(missionDir)
	backedUp := map[string]bool{}
	saved := 0
	for _, v := range req.Variants {
		name, ok := variant[strings.ToLower(strings.TrimSpace(v.Type))]
		if !ok {
			http.Error(w, v.Type+" is not a variant of "+ev.Name, http.StatusBadRequest)
			return
		}
		groups, err := cleanSpawnGroups(v.Attachments)
		if err != nil {
			http.Error(w, name+": "+err.Error(), http.StatusBadRequest)
			return
		}
		var cargo []dztypes.SpawnGroup
		if v.Cargo != nil {
			if cargo, err = cleanSpawnGroups(*v.Cargo); err != nil {
				http.Error(w, name+": "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		st, path := &dztypes.SpawnableType{Name: name}, mainPath
		if e, ok := byName[strings.ToLower(name)]; ok {
			cp := *e.st
			st, path = &cp, e.path
		}
		st.Attachments = groups
		if v.Cargo != nil {
			st.Cargo = cargo
		}
		if _, err := os.Stat(path); err != nil {
			http.Error(w, rel(path)+" not found in the active mission", http.StatusBadRequest)
			return
		}
		if !backedUp[path] {
			_ = util.BackupBeforeWrite(path)
			backedUp[path] = true
		}
		if err := dztypes.SaveSpawnableType(path, st); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		saved++
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "variants": saved})
}

// cleanSpawnGroups drops parts without a name and groups left without parts,
// and checks every chance the way DayZ reads them.
func cleanSpawnGroups(in []dztypes.SpawnGroup) ([]dztypes.SpawnGroup, error) {
	out := []dztypes.SpawnGroup{}
	for _, g := range in {
		if !dztypes.ValidChance(g.Chance) {
			return nil, fmt.Errorf("invalid chance %q", g.Chance)
		}
		g.Preset = strings.TrimSpace(g.Preset)
		if g.Preset != "" {
			g.Items = nil
			out = append(out, g)
			continue
		}
		var items []dztypes.SpawnItem
		for _, it := range g.Items {
			it.Name = strings.TrimSpace(it.Name)
			if it.Name == "" {
				continue
			}
			if !dztypes.ValidChance(it.Chance) {
				return nil, fmt.Errorf("invalid chance %q for %s", it.Chance, it.Name)
			}
			items = append(items, it)
		}
		if len(items) > 0 {
			g.Items = items
			out = append(out, g)
		}
	}
	return out, nil
}
