// Copyright (c) 2026 Aristarh Ucolov.
//
// Vehicles that live only in a mod's PBOs. Many car mods ship no server files
// (types / events / spawn points): the server then never spawns them and the
// admin has to write everything by hand. The manager reads the mods' configs
// (internal/pbo), recognises vehicle classes — public classes whose class
// chain reaches a vanilla vehicle base — so they show up on the Vehicles page
// as soon as the mod is installed, and can set them up in one step: an event
// modelled on a vanilla car the admin picks (its settings and spawn spots),
// <type> entries, and parts found by matching the car's attachment slots to
// the parts the mods define.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/pbo"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// vehicleBases are vanilla classes every land vehicle, boat or helicopter
// descends from. A mod class whose chain (across all installed mods) reaches
// one of them is a vehicle.
var vehicleBases = map[string]bool{
	"car": true, "carscript": true, "boat": true, "boatscript": true, "helicopter": true,
	"helicopterscript": true, "offroadhatchback": true, "civiliansedan": true, "hatchback_02": true,
	"sedan_02": true, "truck_01_base": true, "truck_01_covered": true, "truck_01_chassis": true,
	"offroad_02": true, "boat_01_base": true, "boat_01_colorbase": true,
}

// cfgClass is a CfgVehicles class of an installed mod.
type cfgClass struct {
	c   *pbo.Class
	mod string
}

type pboCacheEntry struct {
	size    int64
	mod     time.Time
	classes []*pbo.Class
}

var pboCache struct {
	sync.Mutex
	m map[string]pboCacheEntry
}

// pboVehicleClasses returns the CfgVehicles classes of every config in one
// PBO (cached until the file changes).
func pboVehicleClasses(path string) []*pbo.Class {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	pboCache.Lock()
	if pboCache.m == nil {
		pboCache.m = map[string]pboCacheEntry{}
	}
	if e, ok := pboCache.m[path]; ok && e.size == st.Size() && e.mod.Equal(st.ModTime()) {
		pboCache.Unlock()
		return e.classes
	}
	pboCache.Unlock()
	var out []*pbo.Class
	if f, err := pbo.Open(path); err == nil {
		entries := append(f.Find("config.bin"), f.Find("config.cpp")...)
		for _, e := range entries {
			data, err := f.Read(e)
			if err != nil {
				continue
			}
			tree, err := pbo.ParseConfig(data)
			if err != nil {
				continue
			}
			if v := tree.Child("CfgVehicles"); v != nil {
				out = append(out, v.Classes...)
			}
		}
	}
	pboCache.Lock()
	pboCache.m[path] = pboCacheEntry{size: st.Size(), mod: st.ModTime(), classes: out}
	pboCache.Unlock()
	return out
}

// modPBOs lists a mod's addon archives (addons/ or Addons/, one level deep).
func modPBOs(modDir string) []string {
	var out []string
	entries, _ := os.ReadDir(modDir)
	for _, e := range entries {
		if !e.IsDir() || !strings.EqualFold(e.Name(), "addons") {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(modDir, e.Name()))
		for _, f := range files {
			if !f.IsDir() && strings.EqualFold(filepath.Ext(f.Name()), ".pbo") {
				out = append(out, filepath.Join(modDir, e.Name(), f.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

// modClassIndex reads the CfgVehicles classes of all installed mods: by name
// (lower-case; a later mod overrides an earlier one, like the game's load
// order would) and per mod.
func modClassIndex(dirs map[string]string) (map[string]cfgClass, map[string][]cfgClass) {
	byName := map[string]cfgClass{}
	byMod := map[string][]cfgClass{}
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, mod := range names {
		for _, p := range modPBOs(dirs[mod]) {
			for _, c := range pboVehicleClasses(p) {
				cc := cfgClass{c: c, mod: mod}
				byName[strings.ToLower(c.Name)] = cc
				byMod[mod] = append(byMod[mod], cc)
			}
		}
	}
	return byName, byMod
}

// chain walks a class and its parents through the installed mods' classes;
// it stops at the first class no mod defines (a vanilla one), returned last.
func chain(name string, byName map[string]cfgClass) (mods []cfgClass, vanilla string) {
	seen := map[string]bool{}
	cur := name
	for i := 0; i < 64 && cur != ""; i++ {
		k := strings.ToLower(cur)
		if seen[k] {
			break
		}
		seen[k] = true
		cc, ok := byName[k]
		if !ok {
			return mods, cur
		}
		mods = append(mods, cc)
		cur = cc.c.Parent
	}
	return mods, ""
}

// inherited reads a value through the class chain.
func inherited(name, key string, byName map[string]cfgClass) (*pbo.Class, bool) {
	ch, _ := chain(name, byName)
	for _, cc := range ch {
		if _, ok := cc.c.Values[strings.ToLower(key)]; ok {
			return cc.c, true
		}
	}
	return nil, false
}

func isVehicleClass(name string, byName map[string]cfgClass) bool {
	ch, vanilla := chain(name, byName)
	if vehicleBases[strings.ToLower(vanilla)] {
		return true
	}
	for _, cc := range ch {
		if vehicleBases[strings.ToLower(cc.c.Name)] {
			return true // a mod re-declares a vanilla base
		}
	}
	return false
}

func publicClass(name string, byName map[string]cfgClass) bool {
	if c, ok := inherited(name, "scope", byName); ok {
		s, _ := c.Number("scope")
		return s >= 2
	}
	return false
}

// vehicleFamily is a modded car and its variants (colours).
type vehicleFamily struct {
	Family   string   `json:"family"`   // root class of the car in the mod
	Event    string   `json:"event"`    // suggested event name
	Variants []string `json:"variants"` // public classes to spawn
	Display  string   `json:"display,omitempty"`
	Spawning bool     `json:"spawning"` // a mission event already spawns it
}

var reBaseSuffix = regexp.MustCompile(`(?i)(_?colou?rbase|_?base)$`)

// modVehicleFamilies lists the vehicles one mod defines, grouped by car.
func modVehicleFamilies(mod string, byName map[string]cfgClass, byMod map[string][]cfgClass, spawned map[string]bool) []vehicleFamily {
	fams := map[string]*vehicleFamily{}
	var order []string
	for _, cc := range byMod[mod] {
		name := cc.c.Name
		if !publicClass(name, byName) || !isVehicleClass(name, byName) {
			continue
		}
		// The family root: the topmost class of this mod in the chain.
		ch, _ := chain(name, byName)
		root := name
		for _, x := range ch {
			if x.mod != mod || vehicleBases[strings.ToLower(x.c.Name)] {
				break
			}
			root = x.c.Name
		}
		k := strings.ToLower(root)
		f := fams[k]
		if f == nil {
			ev := "Vehicle" + reBaseSuffix.ReplaceAllString(root, "")
			f = &vehicleFamily{Family: root, Event: ev}
			fams[k] = f
			order = append(order, k)
		}
		f.Variants = append(f.Variants, name)
		if spawned[strings.ToLower(name)] {
			f.Spawning = true
		}
		if f.Display == "" {
			if c, ok := inherited(name, "displayName", byName); ok {
				if d, _ := c.String("displayName"); d != "" && !strings.HasPrefix(d, "$") {
					f.Display = d
				}
			}
		}
	}
	out := make([]vehicleFamily, 0, len(order))
	for _, k := range order {
		sort.Strings(fams[k].Variants)
		out = append(out, *fams[k])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Family < out[j].Family })
	return out
}

// missionSpawnedTypes collects every class any mission event spawns.
func missionSpawnedTypes(missionDir string) map[string]bool {
	out := map[string]bool{}
	for _, f := range missionEcoFiles(missionDir, "events") {
		if doc, err := dztypes.LoadEvents(f.Path); err == nil {
			for _, e := range doc.Events {
				if e.Children == nil {
					continue
				}
				for _, c := range e.Children.Child {
					out[strings.ToLower(c.Type)] = true
				}
			}
		}
	}
	return out
}

// ------------------------------------------------------------------ parts

// vanillaSlotItems fills common slots with the vanilla part that fits them.
var vanillaSlotItems = map[string]string{
	"carbattery": "CarBattery", "truckbattery": "TruckBattery", "carradiator": "CarRadiator",
	"sparkplug": "SparkPlug", "glowplug": "GlowPlug",
	"reflector_1_1": "HeadlightH7", "reflector_2_1": "HeadlightH7",
}

// guessParts picks, for each attachment slot of a vehicle class, a part that
// fits it: a public class of the installed mods whose inventorySlot names the
// slot (preferring one that shares the variant's colour suffix, e.g. a black
// door for a black car), else the vanilla part for common slots.
func guessParts(variant, family string, byName map[string]cfgClass) []string {
	c, ok := inherited(variant, "attachments", byName)
	if !ok {
		return nil
	}
	slots := c.Strings("attachments")
	if len(slots) == 0 {
		return nil
	}
	// slot → candidate parts
	bySlot := map[string][]string{}
	for _, cc := range byName {
		name := cc.c.Name
		var fits []string
		// inventorySlot is often inherited (a colour variant of a door).
		if owner, ok := inherited(name, "inventorySlot", byName); ok {
			switch s := owner.Values["inventoryslot"].(type) {
			case string:
				fits = []string{s}
			case []interface{}:
				fits = owner.Strings("inventoryslot")
			}
		}
		if len(fits) == 0 || !publicClass(name, byName) || strings.Contains(strings.ToLower(name), "ruined") {
			continue
		}
		for _, s := range fits {
			k := strings.ToLower(s)
			bySlot[k] = append(bySlot[k], name)
		}
	}
	suffix := strings.ToLower(strings.TrimPrefix(strings.ToLower(variant), strings.ToLower(reBaseSuffix.ReplaceAllString(family, ""))))
	var out []string
	for _, slot := range slots {
		k := strings.ToLower(slot)
		cands := bySlot[k]
		sort.Strings(cands)
		pick := ""
		for _, cnd := range cands {
			if suffix != "" && strings.HasSuffix(strings.ToLower(cnd), suffix) {
				pick = cnd
				break
			}
		}
		if pick == "" && len(cands) > 0 {
			pick = cands[0]
		}
		if pick == "" {
			pick = vanillaSlotItems[k]
		}
		if pick != "" {
			out = append(out, pick)
		}
	}
	return out
}

// ------------------------------------------------------------------ setup

// vehicleTypeBlock is the <type> entry of a vehicle, modelled on a vanilla
// car's (lifetime, flags) when the mission has one.
func vehicleTypeBlock(name, model string) string {
	if model != "" {
		re := regexp.MustCompile(`(<type\s+name=")[^"]*(")`)
		return re.ReplaceAllString(model, "${1}"+name+"${2}")
	}
	// Indented as a top-level block of the file (indentBlocks keeps it).
	return fmt.Sprintf(`    <type name="%s">
        <nominal>0</nominal>
        <lifetime>3888000</lifetime>
        <restock>0</restock>
        <min>0</min>
        <quantmin>-1</quantmin>
        <quantmax>-1</quantmax>
        <cost>100</cost>
        <flags count_in_cargo="0" count_in_hoarder="0" count_in_map="1" count_in_player="0" crafted="0" deloot="0"/>
    </type>`, name)
}

// vehiclesSetup sets up a vehicle a mod defines without server files: an
// event modelled on a vanilla car event (its settings and spawn spots), the
// <type> entries and the parts — all in dzm_vehicles.
func (h *handlers) vehiclesSetup(w http.ResponseWriter, r *http.Request) {
	if !h.requireStopped(w) {
		return
	}
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mod      string `json:"mod"`
		Family   string `json:"family"`
		Nominal  int    `json:"nominal"`
		CopyFrom string `json:"copyFrom"` // a mission vehicle event to model it on
		Parts    bool   `json:"parts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mod == "" || req.Family == "" || req.CopyFrom == "" {
		http.Error(w, "mod, family and copyFrom required", http.StatusBadRequest)
		return
	}
	if req.Nominal < 1 || req.Nominal > 50 {
		http.Error(w, "the number on the map must be 1-50", http.StatusBadRequest)
		return
	}
	dirs := h.installedModDirs()
	if _, ok := dirs[req.Mod]; !ok {
		http.Error(w, "mod not installed: "+req.Mod, http.StatusNotFound)
		return
	}
	if cfg := h.app.Cfg(); !modActive(cfg.Mods, cfg.ServerMods, req.Mod) {
		// Without the mod loaded the server has no such classes to spawn.
		http.Error(w, "switch the mod on first (Mods page): "+req.Mod, http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	byName, byMod := modClassIndex(dirs)
	var fam *vehicleFamily
	for _, f := range modVehicleFamilies(req.Mod, byName, byMod, missionSpawnedTypes(missionDir)) {
		if strings.EqualFold(f.Family, req.Family) {
			f := f
			fam = &f
		}
	}
	if fam == nil {
		http.Error(w, "no such vehicle in "+req.Mod, http.StatusNotFound)
		return
	}

	// The model event: its block (settings), spawn spots, first child's type.
	var modelSrc, modelBlock string
	var modelEv dztypes.Event
	for _, f := range missionEcoFiles(missionDir, "events") {
		doc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			continue
		}
		if e := doc.Find(req.CopyFrom); e != nil {
			data, _ := os.ReadFile(f.Path)
			if b, ok := dztypes.EventBlock(string(data), e.Name); ok {
				modelSrc, modelBlock, modelEv = f.Path, b, *e
			}
			break
		}
	}
	if modelBlock == "" {
		http.Error(w, "event not found in the mission: "+req.CopyFrom, http.StatusBadRequest)
		return
	}
	_ = modelSrc

	// Event name: unique in the mission.
	known := missionEventNames(missionDir)
	evName := fam.Event
	for i := 2; known[strings.ToLower(evName)]; i++ {
		evName = fmt.Sprintf("%s_%d", fam.Event, i)
	}
	block := buildVehicleEvent(modelBlock, modelEv.Name, evName, req.Nominal, fam.Variants)

	// Types: modelled on the model's first child type entry.
	missionTypes := map[string]bool{}
	typeModel := ""
	firstChild := ""
	if modelEv.Children != nil && len(modelEv.Children.Child) > 0 {
		firstChild = modelEv.Children.Child[0].Type
	}
	for _, f := range missionEcoFiles(missionDir, "types") {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		if doc, err := dztypes.Load(f.Path); err == nil {
			for _, t := range doc.Types {
				missionTypes[strings.ToLower(t.Name)] = true
				if typeModel == "" && firstChild != "" && strings.EqualFold(t.Name, firstChild) {
					if b, ok := dztypes.TypeBlock(string(data), t.Name); ok {
						typeModel = b
					}
				}
			}
		}
	}
	var typeBlocks, partBlocks []string
	partTypes := map[string]bool{}
	missionSpawn := loadMissionSpawnable(missionDir)
	for _, v := range fam.Variants {
		if !missionTypes[strings.ToLower(v)] {
			typeBlocks = append(typeBlocks, vehicleTypeBlock(v, typeModel))
		}
		if !req.Parts {
			continue
		}
		if _, have := missionSpawn[strings.ToLower(v)]; have {
			continue
		}
		parts := guessParts(v, fam.Family, byName)
		if len(parts) == 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "    <type name=\"%s\">\n", v)
		for _, p := range parts {
			fmt.Fprintf(&b, "        <attachments chance=\"1.00\">\n            <item name=\"%s\" chance=\"1.00\"/>\n        </attachments>\n", p)
			partTypes[strings.ToLower(p)] = true
		}
		b.WriteString("    </type>")
		partBlocks = append(partBlocks, b.String())
	}

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
		{"events", "events", "events.xml", []string{block}},
	} {
		if err := write(step.kind, step.root, step.file, step.blocks); err != nil {
			http.Error(w, fmt.Sprintf("%s: %v", step.file, err), http.StatusInternalServerError)
			return
		}
	}
	if err := dztypes.RegisterCEFiles(eco, dzmFolder, register); err != nil {
		http.Error(w, "cfgeconomycore.xml: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Spawn spots: the model event's.
	spots := 0
	spPath := filepath.Join(missionDir, "cfgeventspawns.xml")
	if doc, err := dztypes.LoadEventSpawns(spPath); err == nil {
		if m := doc.Find(modelEv.Name); m != nil && len(m.Pos) > 0 {
			pos := append([]dztypes.SpawnPos(nil), m.Pos...)
			doc.SetPositions(evName, pos)
			_ = util.BackupBeforeWrite(spPath)
			if err := dztypes.SaveEventSpawns(spPath, doc); err != nil {
				http.Error(w, "cfgeventspawns.xml: "+err.Error(), http.StatusInternalServerError)
				return
			}
			spots = len(pos)
		}
	}
	src := loadVehicleSources(missionDir)
	src.Events[evName] = req.Mod
	saveVehicleSources(missionDir, src)
	h.app.Log.Printf("vehicle %s from %s set up as %s (model %s): %d variant(s), %d spot(s), %d part list(s)",
		fam.Family, req.Mod, evName, modelEv.Name, len(fam.Variants), spots, len(partBlocks))
	writeJSON(w, map[string]interface{}{"event": evName, "variants": len(fam.Variants), "spots": spots,
		"types": len(typeBlocks), "parts": len(partBlocks)})
}

// buildVehicleEvent turns the model event's block into one for the new car:
// renamed, nominal/min/max set for the count wanted, children replaced.
func buildVehicleEvent(model, modelName, name string, nominal int, variants []string) string {
	b := strings.Replace(model, `name="`+modelName+`"`, `name="`+name+`"`, 1)
	set := func(tag string, v int) {
		re := regexp.MustCompile(`<` + tag + `>\s*-?\d+\s*</` + tag + `>`)
		b = re.ReplaceAllString(b, fmt.Sprintf("<%s>%d</%s>", tag, v, tag))
	}
	min := nominal / 2
	if min < 1 {
		min = 1
	}
	set("nominal", nominal)
	set("min", min)
	set("max", nominal)
	set("active", 1)
	indent := "        "
	if m := regexp.MustCompile(`\n([ \t]+)<child\b`).FindStringSubmatch(b); m != nil {
		indent = m[1]
	}
	var kids strings.Builder
	for _, v := range variants {
		fmt.Fprintf(&kids, "%s<child lootmax=\"0\" lootmin=\"0\" max=\"%d\" min=\"0\" type=\"%s\"/>\n", indent, nominal, v)
	}
	reKids := regexp.MustCompile(`(?s)(<children>\s*\n).*?([ \t]*</children>)`)
	if reKids.MatchString(b) {
		b = reKids.ReplaceAllString(b, "${1}"+strings.ReplaceAll(kids.String(), "$", "$$")+"${2}")
	} else {
		b = strings.Replace(b, "</event>", "    <children>\n"+kids.String()+"    </children>\n</event>", 1)
	}
	return b
}
