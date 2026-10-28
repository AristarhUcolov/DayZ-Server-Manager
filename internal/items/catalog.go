// Copyright (c) 2026 Aristarh Ucolov.
//
// Package items builds the item catalog the panel's pickers show: every
// spawnable DayZ item (config scope 2) with its name in the panel's language,
// the player slots it fits and a category. It is read at run time from the
// server's own game files — addons/*.pbo and dta/*.pbo, the string table in
// dta/languagecore.pbo — and from the PBOs of the server's mods. Nothing of
// the game is bundled with the manager: it reads the admin's installation the
// way the game itself does.
package items

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"dayzmanager/internal/pbo"
)

// PlayerSlots are the survivor's attachment slots a loadout can fill, in the
// order the game's config lists them (Hands is special: any item fits).
var PlayerSlots = []string{"Headgear", "Mask", "Eyewear", "Body", "Vest", "Gloves", "Armband",
	"Back", "Shoulder", "Melee", "Hips", "Legs", "Feet"}

// Item is one catalog entry, in the requested language.
type Item struct {
	Class string   `json:"c"`
	Name  string   `json:"n"`
	Cat   string   `json:"k"`           // clothing, weapons, magazines, ammo, attachments, medical, food, tools, containers, explosives, other
	Slots []string `json:"s,omitempty"` // player slots it fits (PlayerSlots names)
	Qty   bool     `json:"q,omitempty"` // has a quantity: a stack, a liquid, rounds
	Mod   string   `json:"m,omitempty"` // the @mod it comes from ("" = the game)
	// Inside: other items go on or into it (attachment slots, magazines,
	// cargo space) — the loadout editor offers "attachments & inside".
	Inside bool `json:"i,omitempty"`
}

// Survivor is a character model a loadout can be limited to.
type Survivor struct {
	Class  string `json:"c"`
	Name   string `json:"n"`
	Female bool   `json:"f,omitempty"`
}

// Catalog is the localized result.
type Catalog struct {
	Items     []Item     `json:"items"`
	Survivors []Survivor `json:"survivors"`
	// Vehicles: cars, trucks, boats (and modded ones) — not items, so never
	// offered by a picker; named here for the Vehicles and Attachments pages.
	Vehicles []Item   `json:"vehicles,omitempty"`
	PBOs     int      `json:"pbos"`
	Mods     []string `json:"mods,omitempty"`
	Built    int64    `json:"built"`
}

// Data is the language-independent catalog of one installation.
type Data struct {
	items     []rawItem
	survivors []rawItem
	vehicles  []rawItem           // config class Transport: cars, trucks, boats
	strs      map[string][]string // lower-case $STR key → column values
	cols      map[string]int      // column name → index in strs values
	known     map[string]bool     // every config class (lower case): items, vehicles, buildings…
	pbos      int
	mods      []string
	built     time.Time
	// Indexes for Detail: lower-case class → index in items, lower-case
	// inventory slot → the items that fit it.
	byClass    map[string]int
	bySlot     map[string][]int
	vehByClass map[string]int // lower-case class → index in vehicles
}

type rawItem struct {
	class   string
	nameKey string // lower-case string-table key ("" = literal name or none)
	literal string // a displayName that is not a string-table key
	suffix  string // colour/variant the shared parent name lacks ("Black")
	cat     string
	slots   []string
	qty     bool
	mod     string
	female  bool
	islots  []string // every inventory slot it fits (lower case)
	attach  []string // its own attachment slots (config attachments[])
	mags    []string // a weapon's magazines (config magazines[])
	ammo    []string // rounds of a weapon without magazines (config chamberableFrom[])
	cargo   bool     // it has cargo space (config itemsCargoSize)
}

// langColumn maps the panel's languages to the string table's columns; the
// others fall back to English.
var langColumn = map[string]string{
	"en": "english", "ru": "russian", "de": "german", "cs": "czech", "pl": "polish",
	"hu": "hungarian", "it": "italian", "es": "spanish", "fr": "french",
	"zh-tw": "chinese", "ja": "japanese", "pt": "portuguese", "zh": "chinesesimp",
}

// Localize returns the catalog with names in lang.
func (d *Data) Localize(lang string) Catalog {
	out := Catalog{Items: make([]Item, 0, len(d.items)), PBOs: d.pbos, Mods: d.mods, Built: d.built.Unix()}
	for _, it := range d.items {
		out.Items = append(out.Items, Item{Class: it.class, Name: d.name(it, lang), Cat: it.cat, Slots: it.slots, Qty: it.qty, Mod: it.mod,
			Inside: it.cargo || len(it.mags) > 0 || len(it.ammo) > 0 || d.fits(it)})
	}
	for _, s := range d.survivors {
		out.Survivors = append(out.Survivors, Survivor{Class: s.class, Name: d.name(s, lang), Female: s.female})
	}
	for _, v := range d.vehicles {
		out.Vehicles = append(out.Vehicles, Item{Class: v.class, Name: d.name(v, lang), Cat: v.cat, Mod: v.mod, Inside: d.fits(v)})
	}
	return out
}

// Known reports whether any config of the game or the mods defines class —
// an item or not (a vehicle, a building, a zombie).
func (d *Data) Known(class string) bool { return d.known[strings.ToLower(class)] }

func (d *Data) name(it rawItem, lang string) string {
	name := ""
	if it.nameKey != "" {
		if row, ok := d.strs[it.nameKey]; ok {
			for _, col := range []string{langColumn[lang], "english", "original"} {
				if i, ok := d.cols[col]; ok && col != "" && i < len(row) && strings.TrimSpace(row[i]) != "" {
					name = strings.TrimSpace(row[i])
					break
				}
			}
		}
	}
	if name == "" {
		name = it.literal
	}
	// Names still to be translated read "$UNT$Name" in the string table.
	if len(name) > 5 && strings.EqualFold(name[:5], "$UNT$") {
		name = strings.TrimSpace(name[5:])
	}
	if name == "" {
		return Humanize(it.class)
	}
	if it.suffix != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(it.suffix)) {
		name += " (" + it.suffix + ")"
	}
	return name
}

// ------------------------------------------------------------------ loading

var cache struct {
	sync.Mutex
	m map[string]*Data // fingerprint → data
}

// Load returns the catalog of a server folder and its mod folders, read once
// and kept until a PBO changes.
func Load(serverDir string, modDirs []string) (*Data, error) {
	files, mods := pboFiles(serverDir, modDirs)
	if len(files) == 0 {
		return nil, fmt.Errorf("no game data found in %s (addons/*.pbo)", serverDir)
	}
	fp := fingerprint(files)
	cache.Lock()
	defer cache.Unlock()
	if d, ok := cache.m[fp]; ok {
		return d, nil
	}
	// Reading every archive takes seconds: a copy on disk, kept until a PBO
	// changes, makes the first open after a restart instant too.
	d := loadDisk(fp)
	if d == nil {
		d = build(files, mods)
		saveDisk(fp, d)
	}
	if cache.m == nil || len(cache.m) > 6 {
		cache.m = map[string]*Data{}
	}
	cache.m[fp] = d
	return d, nil
}

type pboFile struct {
	path string
	mod  string // "@Name", "" for the game
	size int64
	mod2 time.Time
}

// pboFiles lists the game's PBOs, then each mod's (addons/ or Addons/), so
// later mods override earlier definitions as they do in the game.
func pboFiles(serverDir string, modDirs []string) ([]pboFile, []string) {
	var out []pboFile
	add := func(dir, mod string) int {
		n := 0
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pbo") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, pboFile{path: filepath.Join(dir, e.Name()), mod: mod, size: info.Size(), mod2: info.ModTime()})
			n++
		}
		return n
	}
	add(filepath.Join(serverDir, "dta"), "")
	add(filepath.Join(serverDir, "addons"), "")
	// DLC content sits in its own folder with an addons/ inside (sakhal/,
	// nasdara/): the game's items too, not a mod.
	if entries, err := os.ReadDir(serverDir); err == nil {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if !e.IsDir() || strings.HasPrefix(name, "@") || strings.HasPrefix(name, ".") || name == "addons" || name == "dta" {
				continue
			}
			add(filepath.Join(serverDir, e.Name(), "addons"), "")
		}
	}
	var mods []string
	for _, md := range modDirs {
		entries, err := os.ReadDir(md)
		if err != nil {
			continue
		}
		n := 0
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), "addons") {
				n += add(filepath.Join(md, e.Name()), filepath.Base(md))
			}
		}
		if n > 0 {
			mods = append(mods, filepath.Base(md))
		}
	}
	return out, mods
}

func fingerprint(files []pboFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s|%d|%d\n", f.path, f.size, f.mod2.UnixNano())
	}
	return b.String()
}

// cls is one class of the merged config tree.
type cls struct {
	name, parent string
	values       map[string]interface{}
	pbo          string // the archive that defined it last (base name, lower-case)
	mod          string
}

func build(files []pboFile, mods []string) *Data {
	d := &Data{strs: map[string][]string{}, cols: map[string]int{}, known: map[string]bool{}, pbos: len(files), mods: mods, built: time.Now()}
	trees := map[string]map[string]*cls{"cfgvehicles": {}, "cfgweapons": {}, "cfgmagazines": {}}
	var tables [][]byte
	for _, f := range files {
		pf, err := pbo.Open(f.path)
		if err != nil {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path)))
		for _, e := range pf.Entries {
			low := strings.ToLower(strings.ReplaceAll(e.Name, "\\", "/"))
			switch {
			case strings.HasSuffix(low, "stringtable.csv"):
				if data, err := pf.Read(e); err == nil {
					tables = append(tables, data)
				}
			case strings.HasSuffix(low, "config.bin") || strings.HasSuffix(low, "config.cpp"):
				data, err := pf.Read(e)
				if err != nil {
					continue
				}
				root, err := pbo.ParseConfig(data)
				if err != nil {
					continue
				}
				for top, m := range trees {
					tc := root.Child(top)
					if tc == nil {
						continue
					}
					for _, c := range tc.Classes {
						k := strings.ToLower(c.Name)
						old, ok := m[k]
						if !ok {
							old = &cls{name: c.Name, values: map[string]interface{}{}}
							m[k] = old
						}
						if c.Parent != "" {
							old.parent = c.Parent
						}
						for kk, vv := range c.Values {
							old.values[kk] = vv
						}
						old.pbo, old.mod = base, f.mod
					}
				}
			}
		}
	}
	for _, t := range tables {
		d.addStrings(t)
	}

	used := map[string]bool{}
	for top, m := range trees {
		for k := range m {
			d.known[k] = true
		}
		for _, c := range m {
			if scope, _ := inheritedNum(m, c, "scope"); scope != 2 {
				continue
			}
			chain := ancestry(m, c)
			var it rawItem
			switch top {
			case "cfgweapons":
				if !chain["weapon_base"] {
					continue
				}
				it.cat = "weapons"
			case "cfgmagazines":
				if !chain["magazine_base"] && !chain["defaultmagazine"] {
					continue
				}
				it.cat = "magazines"
				if chain["ammunition_base"] {
					it.cat = "ammo"
				}
				it.qty = true
			case "cfgvehicles":
				if strings.HasPrefix(strings.ToLower(c.name), "survivor") && chain["survivorbase"] {
					// Every survivor's displayName is just "Survivor": the
					// class names the character (SurvivorM_Mirek → Mirek).
					s := rawItem{class: c.name, female: chain["survivorfemale_base"], literal: survivorName(c.name)}
					d.survivors = append(d.survivors, s)
					continue
				}
				if chain["transport"] {
					// A car, truck or boat: its attachments[] are the parts
					// it is built from (wheels, doors, battery…).
					v := rawItem{class: c.name, mod: c.mod, cat: "vehicles", attach: inheritedStrings(m, c, "attachments")}
					v.nameKey, v.literal = nameOf(m, c)
					if v.nameKey != "" {
						used[v.nameKey] = true
					}
					if _, own := c.values["displayname"]; !own {
						if i := strings.LastIndexByte(c.name, '_'); i > 0 && i < len(c.name)-1 {
							v.suffix = Humanize(c.name[i+1:])
						}
					}
					d.vehicles = append(d.vehicles, v)
					continue
				}
				if !chain["inventory_base"] {
					continue
				}
				it.cat = category(chain, c.pbo)
			}
			it.class, it.mod = c.name, c.mod
			it.nameKey, it.literal = nameOf(m, c)
			if it.nameKey != "" {
				used[it.nameKey] = true
			}
			// A colour variant shares its parent's name ("Hoodie"): keep the
			// variant from the class name ("Hoodie (Black)").
			if _, own := c.values["displayname"]; !own {
				if i := strings.LastIndexByte(c.name, '_'); i > 0 && i < len(c.name)-1 {
					it.suffix = Humanize(c.name[i+1:])
				}
			}
			for _, s := range inheritedStrings(m, c, "inventoryslot") {
				for _, ps := range PlayerSlots {
					if strings.EqualFold(s, ps) && !contains(it.slots, ps) {
						it.slots = append(it.slots, ps)
					}
				}
			}
			if v, ok := inheritedNum(m, c, "varquantitymax"); ok && v > 0 {
				it.qty = true
			}
			for _, s := range inheritedStrings(m, c, "inventoryslot") {
				if s = strings.ToLower(strings.TrimSpace(s)); s != "" && !contains(it.islots, s) {
					it.islots = append(it.islots, s)
				}
			}
			it.attach = inheritedStrings(m, c, "attachments")
			if top == "cfgweapons" {
				it.mags = inheritedStrings(m, c, "magazines")
				// A weapon without magazines (an internal one: Mosin, SKS,
				// shotguns) is loaded with the rounds it chambers.
				if len(it.mags) == 0 {
					it.ammo = inheritedStrings(m, c, "chamberablefrom")
				}
			}
			if v, ok := inherited(m, c, "itemscargosize"); ok {
				if a, ok := v.([]interface{}); ok && len(a) == 2 {
					w, _ := a[0].(float64)
					hh, _ := a[1].(float64)
					it.cargo = w > 0 && hh > 0
				}
			}
			d.items = append(d.items, it)
		}
	}
	// Only the rows the catalog names are kept in memory.
	for k := range d.strs {
		if !used[k] {
			delete(d.strs, k)
		}
	}
	sort.Slice(d.items, func(i, j int) bool { return strings.ToLower(d.items[i].class) < strings.ToLower(d.items[j].class) })
	sort.Slice(d.vehicles, func(i, j int) bool {
		return strings.ToLower(d.vehicles[i].class) < strings.ToLower(d.vehicles[j].class)
	})
	d.index()
	sort.Slice(d.survivors, func(i, j int) bool {
		if d.survivors[i].female != d.survivors[j].female {
			return !d.survivors[i].female
		}
		return d.survivors[i].class < d.survivors[j].class
	})
	return d
}

// category sorts an item by its base classes, and for the game's own items by
// the archive it comes from (medicine and tools share no base class).
func category(chain map[string]bool, pboName string) string {
	switch {
	case chain["clothing_base"]:
		return "clothing"
	case chain["explosivesbase"] || chain["grenade_base"]:
		return "explosives"
	case chain["itemoptics"] || chain["itemsuppressor"] || strings.HasPrefix(pboName, "weapon_") && !strings.HasPrefix(pboName, "weapon_magazines"):
		return "attachments"
	case pboName == "gear_medical":
		return "medical"
	case chain["edible_base"]:
		return "food"
	case chain["container_base"] || pboName == "gear_containers":
		return "containers"
	case pboName == "weapons_melee":
		return "weapons"
	case pboName == "gear_tools":
		return "tools"
	}
	return "other"
}

// ancestry is the set of lower-case class names from c up to the root.
func ancestry(m map[string]*cls, c *cls) map[string]bool {
	out := map[string]bool{}
	for n := 0; c != nil && n < 40; n++ {
		k := strings.ToLower(c.name)
		if out[k] {
			break
		}
		out[k] = true
		c = m[strings.ToLower(c.parent)]
	}
	return out
}

func inherited(m map[string]*cls, c *cls, key string) (interface{}, bool) {
	for n := 0; c != nil && n < 40; n++ {
		if v, ok := c.values[key]; ok {
			return v, true
		}
		c = m[strings.ToLower(c.parent)]
	}
	return nil, false
}

func inheritedNum(m map[string]*cls, c *cls, key string) (float64, bool) {
	v, ok := inherited(m, c, key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		var f float64
		_, err := fmt.Sscan(strings.TrimSpace(n), &f)
		return f, err == nil
	}
	return 0, false
}

func inheritedStrings(m map[string]*cls, c *cls, key string) []string {
	v, ok := inherited(m, c, key)
	if !ok {
		return nil
	}
	switch a := v.(type) {
	case []interface{}:
		var out []string
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{a}
	}
	return nil
}

// nameOf returns the class's displayName as a string-table key (lower case,
// without "$") or as a literal.
func nameOf(m map[string]*cls, c *cls) (key, literal string) {
	v, ok := inherited(m, c, "displayname")
	if !ok {
		return "", ""
	}
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "$") {
		return strings.ToLower(s[1:]), ""
	}
	return "", s
}

// addStrings reads a string table (CSV: "Language","original","english",…).
func (d *Data) addStrings(data []byte) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	head, err := r.Read()
	if err != nil {
		return
	}
	// Map this file's columns onto the shared column order.
	idx := make([]int, len(head))
	for i, h := range head {
		name := strings.ToLower(strings.TrimSpace(h))
		if i == 0 || name == "" {
			idx[i] = -1
			continue
		}
		j, ok := d.cols[name]
		if !ok {
			j = len(d.cols)
			d.cols[name] = j
		}
		idx[i] = j
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(rec) < 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(rec[0]))
		if key == "" {
			continue
		}
		row := d.strs[key]
		for i, v := range rec {
			if i >= len(idx) || idx[i] < 0 || v == "" {
				continue
			}
			for len(row) <= idx[i] {
				row = append(row, "")
			}
			row[idx[i]] = v
		}
		d.strs[key] = row
	}
}

// Humanize turns a class name into words: "BallisticHelmet_Green" →
// "Ballistic Helmet Green", "Mag_STANAG_30Rnd" → "Mag STANAG 30Rnd".
func Humanize(class string) string {
	var b strings.Builder
	rs := []rune(class)
	for i, r := range rs {
		if r == '_' {
			b.WriteByte(' ')
			continue
		}
		if i > 0 && unicode.IsUpper(r) && rs[i-1] != '_' {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
