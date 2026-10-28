// Copyright (c) 2026 Aristarh Ucolov.
//
// Modded vehicles. A vehicle mod only adds car classes; the server spawns them
// once the mission's Central Economy knows about them: an <event> (how many,
// which variants), positions in cfgeventspawns.xml, parts in
// cfgspawnabletypes.xml and <type> entries. Mod authors ship these as "server
// files" inside the mod, and admins usually paste them into extra files
// registered in cfgeconomycore.xml.
//
// So the Vehicles page reads every events/spawnabletypes file the mission
// registers (not just db/events.xml), recognises vehicles by their parts as
// well as by the Vehicle* naming, and can copy the server files a mod ships
// into the mission (folder dzm_vehicles) in one click.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// ecoFile is one Central Economy file of the mission.
type ecoFile struct {
	Rel  string // relative to the mission folder, slash-separated
	Path string
}

// vanillaEcoFiles are the files DayZ reads without any registration.
var vanillaEcoFiles = map[string]string{
	"events":         "db/events.xml",
	"types":          "db/types.xml",
	"spawnabletypes": "cfgspawnabletypes.xml",
}

// missionEcoFiles lists the mission's files of one kind ("events", "types",
// "spawnabletypes"): the vanilla one first, then every file registered for
// that kind in cfgeconomycore.xml. Paths never leave the mission folder.
func missionEcoFiles(missionDir, kind string) []ecoFile {
	var out []ecoFile
	seen := map[string]bool{}
	add := func(rel string) {
		rel = path.Clean(strings.ReplaceAll(rel, `\`, "/"))
		if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." || path.IsAbs(rel) || strings.Contains(rel, ":") {
			return
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ecoFile{Rel: rel, Path: filepath.Join(missionDir, filepath.FromSlash(rel))})
	}
	if v, ok := vanillaEcoFiles[kind]; ok {
		add(v)
	}
	if refs, err := dztypes.ListEconomyCE(filepath.Join(missionDir, "cfgeconomycore.xml")); err == nil {
		for _, r := range refs {
			if strings.EqualFold(r.Type, kind) {
				add(r.Folder + "/" + r.Name)
			}
		}
	}
	return out
}

// spawnableEntry is a cfgspawnabletypes entry and the file it lives in.
type spawnableEntry struct {
	st   *dztypes.SpawnableType
	path string
}

// loadMissionSpawnable reads every spawnabletypes file of the mission; the
// first definition of a class wins.
func loadMissionSpawnable(missionDir string) map[string]spawnableEntry {
	out := map[string]spawnableEntry{}
	for _, f := range missionEcoFiles(missionDir, "spawnabletypes") {
		list, err := dztypes.LoadSpawnable(f.Path)
		if err != nil {
			continue
		}
		for i := range list {
			k := strings.ToLower(list[i].Name)
			if _, ok := out[k]; !ok {
				out[k] = spawnableEntry{st: &list[i], path: f.Path}
			}
		}
	}
	return out
}

// partWords mark vehicle parts in attachment lists (vanilla and the usual mod
// naming): a class spawning with these is a vehicle.
var partWords = []string{"wheel", "battery", "sparkplug", "glowplug", "radiator", "headlight", "door", "hood", "trunk", "rotor"}

func isVehiclePart(item string) bool {
	l := strings.ToLower(item)
	for _, w := range partWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// spawnsAsVehicle reports whether a class's attachments are vehicle parts.
func spawnsAsVehicle(st *dztypes.SpawnableType) bool {
	if st == nil {
		return false
	}
	for _, g := range st.Attachments {
		for _, it := range g.Items {
			if isVehiclePart(it.Name) {
				return true
			}
		}
	}
	return false
}

// looksLikeVehicle: named like a vanilla vehicle event, or its variants spawn
// with vehicle parts.
func looksLikeVehicle(e *dztypes.Event, spawnable func(string) *dztypes.SpawnableType) bool {
	if isVehicleEvent(e.Name) {
		return true
	}
	if e.Children == nil {
		return false
	}
	for _, c := range e.Children.Child {
		if spawnsAsVehicle(spawnable(c.Type)) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- mod scan

// modServerFiles are the Central Economy files found inside one mod.
type modServerFiles struct {
	events    map[string]string // event name (lower) → file path
	eventDefs map[string]dztypes.Event
	positions map[string][]posSource // event name (lower) → every position list shipped for it
	spawnable map[string]spawnableEntry
	typesSrc  map[string]typeSrc // class (lower) → where its <type> is
}

// posSource is one event's positions from one cfgeventspawns file of a mod —
// mods for several maps ship one file per map.
type posSource struct {
	path string
	pos  []dztypes.SpawnPos
}

type typeSrc struct {
	path string
	name string // as written in that file (case may differ from the event's)
}

// pickPositions chooses the position list meant for the current map: lists
// with points off this map are dropped; among several, the one whose path
// names the map wins; when that stays ambiguous, none is taken (the admin
// places the points rather than getting another map's).
func pickPositions(cands []posSource, aliases []string, worldSize float64) []dztypes.SpawnPos {
	var fit []posSource
	for _, c := range cands {
		ok := len(c.pos) > 0
		for _, p := range c.pos {
			if worldSize > 0 && (p.X < 0 || p.Z < 0 || p.X > worldSize || p.Z > worldSize) {
				ok = false
				break
			}
		}
		if ok {
			fit = append(fit, c)
		}
	}
	if len(fit) == 1 {
		return fit[0].pos
	}
	var named []posSource
	for _, c := range fit {
		norm := normWorld(c.path)
		for _, a := range aliases {
			if a != "" && strings.Contains(norm, a) {
				named = append(named, c)
				break
			}
		}
	}
	if len(named) == 1 {
		return named[0].pos
	}
	return nil
}

// mapAliases are the normalised names the current map goes by.
func (h *handlers) mapAliases() ([]string, float64) {
	mi := h.currentMap()
	out := []string{normWorld(mi.Key), normWorld(mi.Name)}
	for _, wd := range worldRegistry {
		if wd.Key == mi.Key {
			out = append(out, wd.Aliases...)
		}
	}
	return out, mi.Size
}

// xmlRoot returns the root element name of an XML file (cheap sniff).
func xmlRoot(data []byte) string {
	s := string(data)
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 || i+1 >= len(s) {
			return ""
		}
		s = s[i+1:]
		switch {
		case strings.HasPrefix(s, "?"), strings.HasPrefix(s, "!"):
			if j := strings.IndexByte(s, '>'); j >= 0 {
				if strings.HasPrefix(s, "!--") {
					if k := strings.Index(s, "-->"); k >= 0 {
						s = s[k+3:]
						continue
					}
				}
				s = s[j+1:]
				continue
			}
			return ""
		default:
			end := strings.IndexAny(s, " \t\r\n/>")
			if end < 0 {
				return ""
			}
			return strings.ToLower(s[:end])
		}
	}
}

func scanModServerFiles(dir string) modServerFiles {
	f := modServerFiles{events: map[string]string{}, eventDefs: map[string]dztypes.Event{},
		positions: map[string][]posSource{}, spawnable: map[string]spawnableEntry{}, typesSrc: map[string]typeSrc{}}
	if r, err := filepath.EvalSymlinks(dir); err == nil && r != "" {
		dir = r
	}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.EqualFold(filepath.Ext(p), ".xml") || info.Size() > 8<<20 {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		switch xmlRoot(data) {
		case "events":
			if doc, err := dztypes.LoadEvents(p); err == nil {
				for _, e := range doc.Events {
					k := strings.ToLower(e.Name)
					if _, ok := f.events[k]; !ok {
						f.events[k] = p
						f.eventDefs[k] = e
					}
				}
			}
		case "eventposdef":
			if doc, err := dztypes.LoadEventSpawns(p); err == nil {
				for _, e := range doc.Events {
					if len(e.Pos) > 0 {
						k := strings.ToLower(e.Name)
						f.positions[k] = append(f.positions[k], posSource{path: p, pos: e.Pos})
					}
				}
			}
		case "spawnabletypes":
			if list, err := dztypes.LoadSpawnable(p); err == nil {
				for i := range list {
					if k := strings.ToLower(list[i].Name); f.spawnable[k].st == nil {
						f.spawnable[k] = spawnableEntry{st: &list[i], path: p}
					}
				}
			}
		case "types":
			if doc, err := dztypes.Load(p); err == nil {
				for _, t := range doc.Types {
					if k := strings.ToLower(t.Name); f.typesSrc[k].path == "" {
						f.typesSrc[k] = typeSrc{path: p, name: t.Name}
					}
				}
			}
		}
		return nil
	})
	return f
}

type modVehicle struct {
	Event      string   `json:"event"`
	Variants   []string `json:"variants"`
	Positions  int      `json:"positions"`         // spawn points the mod ships
	HasParts   bool     `json:"hasParts"`          // part lists the mod ships (or the mission already has)
	InMission  bool     `json:"inMission"`         // already defined in the mission
	MissionPos int      `json:"missionPos"`        // positions the mission already has for it
	Removed    bool     `json:"removed,omitempty"` // the admin deleted it (not added automatically)
}

type modVehicles struct {
	Mod      string       `json:"mod"`
	Vehicles []modVehicle `json:"vehicles"`
	// Detected: vehicles the mod's configs define that its server files (if
	// any) do not cover — they need a setup (vehicles_detect.go).
	Detected []vehicleFamily `json:"detected,omitempty"`
	Active   bool            `json:"active"` // the mod is switched on
}

// installedModDirs lists the @mods in the server folder.
func (h *handlers) installedModDirs() map[string]string {
	out := map[string]string{}
	entries, err := os.ReadDir(h.app.ServerDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "@") && (e.IsDir() || e.Type()&os.ModeSymlink != 0) && !mods.IsStagingDir(e.Name()) {
			out[e.Name()] = filepath.Join(h.app.ServerDir, e.Name())
		}
	}
	return out
}

// missionEventNames collects every event name the mission defines (any file).
func missionEventNames(missionDir string) map[string]bool {
	out := map[string]bool{}
	for _, f := range missionEcoFiles(missionDir, "events") {
		if doc, err := dztypes.LoadEvents(f.Path); err == nil {
			for _, e := range doc.Events {
				out[strings.ToLower(e.Name)] = true
			}
		}
	}
	return out
}

func (h *handlers) vehiclesMods(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	known := missionEventNames(missionDir)
	missionSpawn := loadMissionSpawnable(missionDir)
	missionPos := map[string]int{}
	if sp, err := dztypes.LoadEventSpawns(filepath.Join(missionDir, "cfgeventspawns.xml")); err == nil {
		for _, e := range sp.Events {
			missionPos[strings.ToLower(e.Name)] = len(e.Pos)
		}
	}
	aliases, worldSize := h.mapAliases()
	out := []modVehicles{}
	dirs := h.installedModDirs()
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)
	byName, byMod := modClassIndex(dirs)
	spawned := missionSpawnedTypes(missionDir)
	sources := loadVehicleSources(missionDir)
	cfg := h.app.Cfg()
	for _, mod := range names {
		files := scanModServerFiles(dirs[mod])
		spawnable := func(class string) *dztypes.SpawnableType {
			if e, ok := files.spawnable[strings.ToLower(class)]; ok {
				return e.st
			}
			if e, ok := missionSpawn[strings.ToLower(class)]; ok {
				return e.st
			}
			return nil
		}
		mv := modVehicles{Mod: mod}
		keys := make([]string, 0, len(files.eventDefs))
		for k := range files.eventDefs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e := files.eventDefs[k]
			if !looksLikeVehicle(&e, spawnable) {
				continue
			}
			v := modVehicle{Event: e.Name, Positions: len(pickPositions(files.positions[k], aliases, worldSize)),
				InMission: known[k], MissionPos: missionPos[k], Removed: !known[k] && sources.removed(e.Name)}
			if e.Children != nil {
				for _, c := range e.Children.Child {
					v.Variants = append(v.Variants, c.Type)
					if spawnable(c.Type) != nil {
						v.HasParts = true
					}
				}
			}
			mv.Vehicles = append(mv.Vehicles, v)
		}
		// Vehicles the configs define beyond what the server files cover.
		covered := map[string]bool{}
		for _, v := range mv.Vehicles {
			for _, c := range v.Variants {
				covered[strings.ToLower(c)] = true
			}
		}
		for _, f := range modVehicleFamilies(mod, byName, byMod, spawned) {
			all := true
			for _, v := range f.Variants {
				if !covered[strings.ToLower(v)] {
					all = false
				}
			}
			if !all {
				mv.Detected = append(mv.Detected, f)
			}
		}
		if len(mv.Vehicles) > 0 || len(mv.Detected) > 0 {
			mv.Active = modActive(cfg.Mods, cfg.ServerMods, mod)
			out = append(out, mv)
		}
	}
	writeJSON(w, map[string]interface{}{"mods": out, "auto": !cfg.AutoModVehiclesOff})
}

// dzmFolder is where imported vehicle definitions live inside the mission.
const dzmFolder = "dzm_vehicles"

// vehiclesImport copies the server files a mod ships for the chosen vehicle
// events into the mission (see importModVehicles).
func (h *handlers) vehiclesImport(w http.ResponseWriter, r *http.Request) {
	if !h.requireStopped(w) {
		return
	}
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mod    string   `json:"mod"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Events) == 0 {
		http.Error(w, "mod and events required", http.StatusBadRequest)
		return
	}
	if _, ok := h.installedModDirs()[req.Mod]; !ok {
		http.Error(w, "mod not installed: "+req.Mod, http.StatusNotFound)
		return
	}
	res, err := h.importModVehicles(req.Mod, req.Events)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// vehicleImport is what importModVehicles did.
type vehicleImport struct {
	Events    int      `json:"events"`
	Positions int      `json:"positions"`
	Parts     int      `json:"parts"`
	Types     int      `json:"types"`
	Imported  []string `json:"imported"`
	Skipped   []string `json:"skipped"`
	Folder    string   `json:"folder"`
}

// importModVehicles copies the server files a mod ships for the given vehicle
// events into the mission: event blocks, their spawn points (only where the
// mission has none), the variants' part lists and the <type> entries of the
// variants and their parts that the mission lacks. Nothing that exists is
// overwritten; every touched file is backed up first. Caller holds the write
// lock with the server stopped.
func (h *handlers) importModVehicles(mod string, events []string) (vehicleImport, error) {
	out := vehicleImport{Folder: dzmFolder}
	modDir, ok := h.installedModDirs()[mod]
	if !ok {
		return out, fmt.Errorf("mod not installed: %s", mod)
	}
	mission, err := h.missionTemplate()
	if err != nil {
		return out, err
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	files := scanModServerFiles(modDir)
	known := missionEventNames(missionDir)
	missionSpawn := loadMissionSpawnable(missionDir)
	missionTypes := map[string]bool{}
	for _, f := range missionEcoFiles(missionDir, "types") {
		if doc, err := dztypes.Load(f.Path); err == nil {
			for _, t := range doc.Types {
				missionTypes[strings.ToLower(t.Name)] = true
			}
		}
	}

	readCache := map[string]string{}
	read := func(p string) string {
		if s, ok := readCache[p]; ok {
			return s
		}
		b, _ := os.ReadFile(p)
		readCache[p] = string(b)
		return readCache[p]
	}

	aliases, worldSize := h.mapAliases()
	var eventBlocks, partBlocks, typeBlocks []string
	newPositions := map[string][]dztypes.SpawnPos{}
	addedParts, addedTypes := map[string]bool{}, map[string]bool{}
	addType := func(class string) {
		k := strings.ToLower(class)
		if missionTypes[k] || addedTypes[k] {
			return
		}
		if src := files.typesSrc[k]; src.path != "" {
			if b, ok := dztypes.TypeBlock(read(src.path), src.name); ok {
				typeBlocks = append(typeBlocks, b)
				addedTypes[k] = true
			}
		}
	}
	for _, name := range events {
		k := strings.ToLower(name)
		src, ok := files.events[k]
		if !ok || known[k] {
			out.Skipped = append(out.Skipped, name)
			continue
		}
		block, ok := dztypes.EventBlock(read(src), files.eventDefs[k].Name)
		if !ok {
			out.Skipped = append(out.Skipped, name)
			continue
		}
		eventBlocks = append(eventBlocks, block)
		out.Imported = append(out.Imported, files.eventDefs[k].Name)
		if pos := pickPositions(files.positions[k], aliases, worldSize); len(pos) > 0 {
			newPositions[files.eventDefs[k].Name] = pos
		}
		if e := files.eventDefs[k]; e.Children != nil {
			for _, c := range e.Children.Child {
				ck := strings.ToLower(c.Type)
				addType(c.Type)
				if _, have := missionSpawn[ck]; have || addedParts[ck] {
					continue
				}
				if ent, ok := files.spawnable[ck]; ok {
					if b, ok := dztypes.TypeBlock(read(ent.path), ent.st.Name); ok {
						partBlocks = append(partBlocks, b)
						addedParts[ck] = true
					}
					for _, g := range ent.st.Attachments {
						for _, it := range g.Items {
							addType(it.Name)
						}
					}
				}
			}
		}
	}
	if len(eventBlocks) == 0 {
		return out, nil
	}

	// The writers back up what they change (an existing dzm_vehicles file);
	// a new file is written whole, and cfgeconomycore.xml gets every
	// registration in one write — so its single backup is the original.
	eco := filepath.Join(missionDir, "cfgeconomycore.xml")
	var register [][2]string
	write := func(kind, root, file string, blocks []string) error {
		if len(blocks) == 0 {
			return nil
		}
		p := filepath.Join(missionDir, dzmFolder, file)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := dztypes.CreateXMLFile(p, root, blocks); err != nil {
				return err
			}
		} else if err := dztypes.AppendBlocks(p, root, blocks); err != nil {
			return err
		}
		register = append(register, [2]string{file, kind})
		return nil
	}
	for _, step := range []struct {
		kind, root, file string
		blocks           []string
	}{
		{"types", "types", "types.xml", typeBlocks},
		{"spawnabletypes", "spawnabletypes", "spawnabletypes.xml", partBlocks},
		{"events", "events", "events.xml", eventBlocks},
	} {
		if err := write(step.kind, step.root, step.file, step.blocks); err != nil {
			return out, fmt.Errorf("%s: %v", step.file, err)
		}
	}
	if err := dztypes.RegisterCEFiles(eco, dzmFolder, register); err != nil {
		return out, fmt.Errorf("cfgeconomycore.xml: %v", err)
	}

	posAdded := 0
	if len(newPositions) > 0 {
		spPath := filepath.Join(missionDir, "cfgeventspawns.xml")
		doc, err := dztypes.LoadEventSpawns(spPath)
		if os.IsNotExist(err) {
			doc, err = &dztypes.EventSpawnsDoc{}, nil
		}
		if err == nil {
			changed := false
			for name, pos := range newPositions {
				if e := doc.Find(name); e != nil && len(e.Pos) > 0 {
					continue // the mission already places it — keep the admin's points
				}
				doc.SetPositions(name, pos)
				posAdded++
				changed = true
			}
			if changed {
				_ = util.BackupBeforeWrite(spPath)
				if err := dztypes.SaveEventSpawns(spPath, doc); err != nil {
					return out, fmt.Errorf("cfgeventspawns.xml: %v", err)
				}
			}
		}
	}
	// Remember which mod each event came from: switching the mod off later
	// switches its vehicles off too (vehicles_auto.go).
	src := loadVehicleSources(missionDir)
	for _, name := range out.Imported {
		src.Events[name] = mod
		// Added again by hand: no longer a deleted vehicle.
		for k := range src.Removed {
			if strings.EqualFold(k, name) {
				delete(src.Removed, k)
			}
		}
	}
	saveVehicleSources(missionDir, src)

	h.app.Log.Printf("vehicles from %s: %d event(s), %d spawn list(s), %d part list(s), %d type(s) → %s/",
		mod, len(eventBlocks), posAdded, len(partBlocks), len(typeBlocks), dzmFolder)
	out.Events, out.Positions, out.Parts, out.Types = len(eventBlocks), posAdded, len(partBlocks), len(typeBlocks)
	return out, nil
}
