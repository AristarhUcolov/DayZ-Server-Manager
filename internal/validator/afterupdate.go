// Copyright (c) 2026 Aristarh Ucolov.
//
// After a DayZ server update. Updating the server replaces its stock files —
// serverDZ.cfg, cfggameplay.json, cfgeconomycore.xml — and with them what the
// manager had added: the registration of the panel's mod vehicles
// (dzm_vehicles), the fresh-spawn loadouts listed in cfggameplay.json and the
// enableCfgGameplayFile switch. Nothing fails loudly: the loadout falls back
// to init.c, the mod cars stop spawning. The checks here say so, and
// HealAfterUpdate puts back what the manager itself had set up — the server
// calls it before every start.
package validator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/config"
	dztypes "dayzmanager/internal/types"
)

// dzmFolder is where the panel keeps the mod vehicles it imported.
const dzmFolder = "dzm_vehicles"

// unregisteredDzm lists the panel's mod-vehicle files (as file, CE type) that
// cfgeconomycore.xml does not register, so DayZ never reads them.
func unregisteredDzm(missionDir string) [][2]string {
	refs, err := dztypes.ListEconomyCE(filepath.Join(missionDir, "cfgeconomycore.xml"))
	if err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, r := range refs {
		if strings.EqualFold(strings.Trim(r.Folder, `/\`), dzmFolder) {
			have[strings.ToLower(r.Name)] = true
		}
	}
	var out [][2]string
	for _, f := range [][2]string{{"types.xml", "types"}, {"spawnabletypes.xml", "spawnabletypes"}, {"events.xml", "events"}} {
		if _, err := os.Stat(filepath.Join(missionDir, dzmFolder, f[0])); err == nil && !have[f[0]] {
			out = append(out, f)
		}
	}
	return out
}

// gearState is the fresh-spawn loadout as cfggameplay.json and the mission
// folder have it.
type gearState struct {
	gameplay   string   // cfggameplay.json
	listed     bool     // PlayerData.spawnGearPresetFiles is in the file at all
	registered []string // what it lists
	unlinked   []string // preset files on disk it does not list
	broken     []string // listed but missing or unreadable
	parseErr   error
}

func readGear(missionDir string) gearState {
	g := gearState{gameplay: filepath.Join(missionDir, "cfggameplay.json")}
	data, err := os.ReadFile(g.gameplay)
	if err != nil {
		return g
	}
	var doc struct {
		PlayerData map[string]json.RawMessage `json:"PlayerData"`
	}
	if g.parseErr = json.Unmarshal(data, &doc); g.parseErr != nil {
		return g
	}
	_, g.listed = doc.PlayerData["spawnGearPresetFiles"]
	g.registered, _ = dztypes.RegisteredGearFiles(g.gameplay)
	for _, rel := range g.registered {
		if _, err := dztypes.LoadGearPreset(filepath.Join(missionDir, filepath.FromSlash(rel))); err != nil {
			g.broken = append(g.broken, rel)
		}
	}
	for _, rel := range dztypes.FindGearPresetFiles(missionDir) {
		known := false
		for _, r := range g.registered {
			if dztypes.SameGearPath(r, rel) {
				known = true
				break
			}
		}
		if !known {
			if _, err := dztypes.LoadGearPreset(filepath.Join(missionDir, filepath.FromSlash(rel))); err == nil {
				g.unlinked = append(g.unlinked, rel)
			}
		}
	}
	return g
}

// gameplayOn reports whether serverDZ.cfg has enableCfgGameplayFile = 1 (a
// missing line counts as off, as in DayZ).
func (o Options) gameplayOn() (bool, string) {
	if o.ServerCfg == "" {
		return true, "" // unknown: no claim either way
	}
	path := filepath.Join(o.ServerDir, o.ServerCfg)
	cfg, err := config.LoadServerCfg(path)
	if err != nil {
		return true, path
	}
	v, _ := cfg.Get("enableCfgGameplayFile")
	return strings.TrimSpace(v) == "1", path
}

// checkAfterUpdate reports what a server update undid.
func checkAfterUpdate(o Options) []Issue {
	md := o.missionDir()
	if md == "" {
		return nil
	}
	var out []Issue
	if miss := unregisteredDzm(md); len(miss) > 0 {
		names := make([]string, len(miss))
		for i, f := range miss {
			names[i] = dzmFolder + "/" + f[0]
		}
		out = append(out, Issue{File: filepath.Join(md, "cfgeconomycore.xml"), Severity: SevError, Code: "dzm.unregistered", Fix: "dzm.unregistered",
			Message: fmt.Sprintf("the mod vehicles the panel added are not registered in cfgeconomycore.xml (%s) — they do not spawn", strings.Join(names, ", ")),
			Args:    A("files", strings.Join(names, ", "))})
	}
	g := readGear(md)
	if g.parseErr != nil {
		return out // checkJSON reports the syntax error itself
	}
	if len(g.broken) > 0 {
		out = append(out, Issue{File: g.gameplay, Severity: SevError, Code: "gear.broken", Fix: "gear.broken",
			Message: fmt.Sprintf("cfggameplay.json lists loadout files DayZ cannot read (%s) — DayZ then ignores every loadout", strings.Join(g.broken, ", ")),
			Args:    A("files", strings.Join(g.broken, ", "))})
	}
	if len(g.registered) == 0 && len(g.unlinked) > 0 {
		out = append(out, Issue{File: g.gameplay, Severity: SevWarning, Code: "gear.unlinked", Fix: "gear.unlinked",
			Message: fmt.Sprintf("loadout files are in the mission folder but cfggameplay.json does not list them (%s) — players spawn from init.c", strings.Join(g.unlinked, ", ")),
			Args:    A("files", strings.Join(g.unlinked, ", "))})
	}
	if len(g.registered) > len(g.broken) {
		if on, cfgPath := o.gameplayOn(); !on {
			out = append(out, Issue{File: cfgPath, Severity: SevWarning, Code: "gear.flagOff", Fix: "gear.flagOff",
				Message: "loadouts are set up but enableCfgGameplayFile is off in the server config — DayZ ignores cfggameplay.json and them",
				Args:    A()})
		}
	}
	return out
}

// fixAfterUpdate repairs the given kinds (dzm.unregistered, gear.broken,
// gear.unlinked, gear.flagOff) and returns what it did.
func fixAfterUpdate(o Options, do func(string) bool) ([]Fixed, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	var out []Fixed
	if do("dzm.unregistered") {
		if miss := unregisteredDzm(md); len(miss) > 0 {
			if err := dztypes.RegisterCEFiles(filepath.Join(md, "cfgeconomycore.xml"), dzmFolder, miss); err != nil {
				return out, err
			}
			for _, f := range miss {
				out = append(out, F("ceRegistered", fmt.Sprintf("registered %s/%s in cfgeconomycore.xml", dzmFolder, f[0]), "file", dzmFolder+"/"+f[0]))
			}
		}
	}
	g := readGear(md)
	if g.parseErr != nil {
		return out, nil
	}
	list := g.registered
	changed := false
	if do("gear.broken") && len(g.broken) > 0 {
		kept := list[:0:0]
		for _, r := range list {
			bad := false
			for _, b := range g.broken {
				if r == b {
					bad = true
				}
			}
			if !bad {
				kept = append(kept, r)
			}
		}
		list, changed = kept, true
		out = append(out, F("gearRemoved", "removed the unreadable loadout entries "+strings.Join(g.broken, ", ")+" from cfggameplay.json", "files", strings.Join(g.broken, ", ")))
	}
	if do("gear.unlinked") && len(g.registered) == 0 && len(g.unlinked) > 0 {
		list, changed = append(list, g.unlinked...), true
		out = append(out, F("gearListed", "listed the loadout files "+strings.Join(g.unlinked, ", ")+" in cfggameplay.json", "files", strings.Join(g.unlinked, ", ")))
	}
	if changed {
		if err := dztypes.SetRegisteredGearFiles(g.gameplay, list); err != nil {
			return out, err
		}
	}
	if (do("gear.flagOff") || (changed && len(list) > 0)) && len(list) > 0 {
		if on, cfgPath := o.gameplayOn(); !on && cfgPath != "" {
			cfg, err := config.LoadServerCfg(cfgPath)
			if err != nil {
				return out, err
			}
			cfg.Set("enableCfgGameplayFile", 1)
			if err := cfg.Save(cfgPath); err != nil {
				return out, err
			}
			out = append(out, F("gameplayOn", "set enableCfgGameplayFile = 1 in "+filepath.Base(cfgPath), "file", filepath.Base(cfgPath)))
		}
	}
	return out, nil
}

// HealAfterUpdate puts back what a server update replaced and the manager had
// set up itself, before the server starts: the registration of the panel's
// mod vehicles, and — when cfggameplay.json came back as the stock file, with
// no loadout list at all — the loadout files that are still in the mission
// folder (plus the enableCfgGameplayFile switch they need). A list the admin
// emptied on purpose is left alone: only the validator mentions it.
func HealAfterUpdate(o Options) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	codes := map[string]bool{"dzm.unregistered": true}
	if g := readGear(md); g.parseErr == nil && !g.listed && len(g.unlinked) > 0 {
		codes["gear.unlinked"] = true
	}
	done, err := fixAfterUpdate(o, func(c string) bool { return codes[c] })
	return Texts(done), err
}
