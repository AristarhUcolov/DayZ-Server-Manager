// Copyright (c) 2026 Aristarh Ucolov.
//
// Modded vehicles appear by themselves. When a mod becomes active — switched
// on, or installed / updated / synced while on — the vehicles it ships server
// files for are added to the mission (dzm_vehicles, see vehicles_mods.go),
// and the Vehicles page shows them at once. Switching the mod off (or
// uninstalling it) switches its vehicles off (<active>0</active>) instead of
// deleting them, so an admin's tuning survives and comes back with the mod.
// Settings → General → "Add vehicles from mods automatically" turns it off.
package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
)

// vehicleSources remembers which mod each imported vehicle event came from,
// and the vehicles the admin deleted (the automatic import skips those).
type vehicleSources struct {
	Events  map[string]string `json:"events"`            // event name → @Mod
	Removed map[string]string `json:"removed,omitempty"` // deleted event → @Mod
}

// removed reports whether the admin deleted this vehicle event.
func (v vehicleSources) removed(event string) bool {
	for k := range v.Removed {
		if strings.EqualFold(k, event) {
			return true
		}
	}
	return false
}

func vehicleSourcesPath(missionDir string) string {
	return filepath.Join(missionDir, dzmFolder, "sources.json")
}

func loadVehicleSources(missionDir string) vehicleSources {
	var v vehicleSources
	if data, err := os.ReadFile(vehicleSourcesPath(missionDir)); err == nil {
		_ = json.Unmarshal(data, &v)
	}
	if v.Events == nil {
		v.Events = map[string]string{}
	}
	return v
}

func saveVehicleSources(missionDir string, v vehicleSources) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	p := vehicleSourcesPath(missionDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		_ = writeFileAtomic(p, data)
	}
}

// autoVehicles reports, per mod, what the automatic step did.
type autoVehicles struct {
	Mod         string   `json:"mod"`
	Added       []string `json:"added,omitempty"`       // vehicle events copied into the mission
	Reactivated []string `json:"reactivated,omitempty"` // switched back on with the mod
	Deactivated []string `json:"deactivated,omitempty"` // switched off with the mod
	Error       string   `json:"error,omitempty"`
}

func (a autoVehicles) empty() bool {
	return len(a.Added) == 0 && len(a.Reactivated) == 0 && len(a.Deactivated) == 0 && a.Error == ""
}

// modActive reports whether a mod is in the launch lists.
func modActive(mods, serverMods []string, mod string) bool {
	return contains(mods, mod) || contains(serverMods, mod)
}

// syncModVehicles brings the vehicles of the given mods in line with whether
// each mod is active: active → its vehicle events the mission lacks are
// imported and its switched-off ones switched back on; inactive or gone → its
// imported vehicles are switched off. Caller holds the write lock with the
// server stopped (mod changes require that already).
func (h *handlers) syncModVehicles(modNames []string) []autoVehicles {
	cfg := h.app.Cfg()
	if cfg.AutoModVehiclesOff || len(modNames) == 0 {
		return nil
	}
	mission, err := h.missionTemplate()
	if err != nil {
		return nil
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	dirs := h.installedModDirs()
	var out []autoVehicles
	seen := map[string]bool{}
	for _, mod := range modNames {
		if mod == "" || seen[mod] || strings.ContainsAny(mod, `/\`) {
			continue
		}
		seen[mod] = true
		res := autoVehicles{Mod: mod}
		_, installed := dirs[mod]
		if installed && modActive(cfg.Mods, cfg.ServerMods, mod) {
			res.Reactivated = h.setModVehiclesActive(missionDir, mod, true)
			if events := h.missingModVehicles(dirs[mod], missionDir); len(events) > 0 {
				imp, err := h.importModVehicles(mod, events)
				if err != nil {
					res.Error = err.Error()
				}
				res.Added = imp.Imported
			}
		} else {
			res.Deactivated = h.setModVehiclesActive(missionDir, mod, false)
		}
		if !res.empty() {
			out = append(out, res)
		}
	}
	return out
}

// missingModVehicles lists the vehicle events a mod ships that the mission
// does not define yet.
func (h *handlers) missingModVehicles(modDir, missionDir string) []string {
	files := scanModServerFiles(modDir)
	if len(files.eventDefs) == 0 {
		return nil
	}
	known := missionEventNames(missionDir)
	missionSpawn := loadMissionSpawnable(missionDir)
	sources := loadVehicleSources(missionDir)
	spawnable := func(class string) *dztypes.SpawnableType {
		if e, ok := files.spawnable[strings.ToLower(class)]; ok {
			return e.st
		}
		if e, ok := missionSpawn[strings.ToLower(class)]; ok {
			return e.st
		}
		return nil
	}
	var out []string
	for k, e := range files.eventDefs {
		e := e
		// A vehicle the admin deleted stays deleted.
		if !known[k] && !sources.removed(e.Name) && looksLikeVehicle(&e, spawnable) {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// setModVehiclesActive switches the imported vehicle events of a mod on or
// off in dzm_vehicles/events.xml. Only events that are not already in the
// wanted state are touched; returns their names.
func (h *handlers) setModVehiclesActive(missionDir, mod string, on bool) []string {
	src := loadVehicleSources(missionDir)
	path := filepath.Join(missionDir, dzmFolder, "events.xml")
	doc, err := dztypes.LoadEvents(path)
	if err != nil {
		return nil
	}
	want := 0
	if on {
		want = 1
	}
	var changed []string
	for name, from := range src.Events {
		if from != mod {
			continue
		}
		e := doc.Find(name)
		if e == nil {
			continue
		}
		cur := 1
		if e.Active != nil {
			cur = *e.Active
		}
		if cur == want {
			continue
		}
		if ok, err := dztypes.PatchEvent(path, name, map[string]int{"active": want}); err == nil && ok {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	if len(changed) > 0 {
		state := "off"
		if on {
			state = "on"
		}
		h.app.Log.Printf("vehicles of %s switched %s: %s", mod, state, strings.Join(changed, ", "))
	}
	return changed
}
