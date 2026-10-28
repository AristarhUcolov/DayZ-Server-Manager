// Copyright (c) 2026 Aristarh Ucolov.
package items

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ------------------------------------------------------------ item detail

// Ref is an item named in the panel's language.
type Ref struct {
	Class string `json:"c"`
	Name  string `json:"n"`
	Cat   string `json:"k"`
	Mod   string `json:"m,omitempty"`
}

// AttachSlot is one attachment slot of an item and what fits it.
type AttachSlot struct {
	Slot  string `json:"slot"`
	Items []Ref  `json:"items"`
}

// Detail is what can go on and into one item: its attachment slots with the
// items that fit each, a weapon's magazines, and whether it has cargo space.
type Detail struct {
	Class  string       `json:"c"`
	Name   string       `json:"n"`
	Cat    string       `json:"k"`
	Attach []AttachSlot `json:"attach"`
	Mags   []Ref        `json:"mags,omitempty"`
	Ammo   []Ref        `json:"ammo,omitempty"` // a weapon without magazines: the rounds it is loaded with
	Cargo  bool         `json:"cargo,omitempty"`
}

// Detail describes one item in lang; false when the catalog has no such item.
func (d *Data) Detail(class, lang string) (Detail, bool) {
	var it rawItem
	if i, ok := d.byClass[strings.ToLower(class)]; ok {
		it = d.items[i]
	} else if i, ok := d.vehByClass[strings.ToLower(class)]; ok {
		it = d.vehicles[i]
	} else {
		return Detail{}, false
	}
	out := Detail{Class: it.class, Name: d.name(it, lang), Cat: it.cat, Cargo: it.cargo, Attach: []AttachSlot{}}
	ref := func(j int) Ref {
		x := d.items[j]
		return Ref{Class: x.class, Name: d.name(x, lang), Cat: x.cat, Mod: x.mod}
	}
	sortRefs := func(r []Ref) {
		sort.Slice(r, func(a, b int) bool { return strings.ToLower(r[a].Name) < strings.ToLower(r[b].Name) })
	}
	for _, s := range it.attach {
		slot := AttachSlot{Slot: s, Items: []Ref{}}
		for _, j := range d.bySlot[strings.ToLower(s)] {
			slot.Items = append(slot.Items, ref(j))
		}
		sortRefs(slot.Items)
		out.Attach = append(out.Attach, slot)
	}
	refs := func(classes []string) []Ref {
		var r []Ref
		seen := map[string]bool{}
		for _, c := range classes {
			k := strings.ToLower(c)
			if seen[k] {
				continue
			}
			seen[k] = true
			if j, ok := d.byClass[k]; ok {
				r = append(r, ref(j))
			}
		}
		sortRefs(r)
		return r
	}
	out.Mags = refs(it.mags)
	out.Ammo = refs(it.ammo)
	return out, true
}

// fits reports whether anything in the catalog fits one of its attachment
// slots.
func (d *Data) fits(it rawItem) bool {
	for _, s := range it.attach {
		if len(d.bySlot[strings.ToLower(s)]) > 0 {
			return true
		}
	}
	return false
}

// index builds the lookup tables Detail uses.
func (d *Data) index() {
	d.byClass = make(map[string]int, len(d.items))
	d.bySlot = map[string][]int{}
	for i, it := range d.items {
		d.byClass[strings.ToLower(it.class)] = i
		for _, s := range it.islots {
			d.bySlot[s] = append(d.bySlot[s], i)
		}
	}
	d.vehByClass = make(map[string]int, len(d.vehicles))
	for i, v := range d.vehicles {
		d.vehByClass[strings.ToLower(v.class)] = i
	}
}

// survivorName is the character a survivor class stands for:
// SurvivorM_Mirek → Mirek, SurvivorF_Eva → Eva.
func survivorName(class string) string {
	if i := strings.IndexByte(class, '_'); i >= 0 && i < len(class)-1 {
		return Humanize(class[i+1:])
	}
	return Humanize(class)
}

// ------------------------------------------------------------- disk cache

var cacheDir struct {
	sync.Mutex
	dir string
}

// SetCacheDir sets the folder the catalog is kept in between runs ("" = no
// copy on disk).
func SetCacheDir(dir string) {
	cacheDir.Lock()
	cacheDir.dir = dir
	cacheDir.Unlock()
}

type diskItem struct {
	Class   string   `json:"c"`
	NameKey string   `json:"nk,omitempty"`
	Literal string   `json:"l,omitempty"`
	Suffix  string   `json:"x,omitempty"`
	Cat     string   `json:"k,omitempty"`
	Slots   []string `json:"s,omitempty"`
	Qty     bool     `json:"q,omitempty"`
	Mod     string   `json:"m,omitempty"`
	Female  bool     `json:"f,omitempty"`
	ISlots  []string `json:"is,omitempty"`
	Attach  []string `json:"a,omitempty"`
	Mags    []string `json:"mg,omitempty"`
	Ammo    []string `json:"am,omitempty"`
	Cargo   bool     `json:"cg,omitempty"`
}

type diskData struct {
	Version     int                 `json:"version"`
	Fingerprint string              `json:"fingerprint"`
	Items       []diskItem          `json:"items"`
	Survivors   []diskItem          `json:"survivors"`
	Vehicles    []diskItem          `json:"vehicles,omitempty"`
	Strs        map[string][]string `json:"strs"`
	Cols        map[string]int      `json:"cols"`
	Known       []string            `json:"known"`
	PBOs        int                 `json:"pbos"`
	Mods        []string            `json:"mods,omitempty"`
	Built       int64               `json:"built"`
}

// diskVersion changes when the cached shape changes, so an older copy is
// rebuilt instead of read with missing fields.
const diskVersion = 4

func diskPath(fp string) string {
	cacheDir.Lock()
	dir := cacheDir.dir
	cacheDir.Unlock()
	if dir == "" {
		return ""
	}
	sum := sha1.Sum([]byte(fp))
	return filepath.Join(dir, "items-"+hex.EncodeToString(sum[:6])+".json")
}

func toDisk(it rawItem) diskItem {
	return diskItem{Class: it.class, NameKey: it.nameKey, Literal: it.literal, Suffix: it.suffix, Cat: it.cat, Slots: it.slots,
		Qty: it.qty, Mod: it.mod, Female: it.female, ISlots: it.islots, Attach: it.attach, Mags: it.mags, Ammo: it.ammo, Cargo: it.cargo}
}

func fromDisk(it diskItem) rawItem {
	return rawItem{class: it.Class, nameKey: it.NameKey, literal: it.Literal, suffix: it.Suffix, cat: it.Cat, slots: it.Slots,
		qty: it.Qty, mod: it.Mod, female: it.Female, islots: it.ISlots, attach: it.Attach, mags: it.Mags, ammo: it.Ammo, cargo: it.Cargo}
}

func saveDisk(fp string, d *Data) {
	path := diskPath(fp)
	if path == "" {
		return
	}
	out := diskData{Version: diskVersion, Fingerprint: fp, Strs: d.strs, Cols: d.cols, PBOs: d.pbos, Mods: d.mods, Built: d.built.Unix()}
	for _, it := range d.items {
		out.Items = append(out.Items, toDisk(it))
	}
	for _, it := range d.survivors {
		out.Survivors = append(out.Survivors, toDisk(it))
	}
	for _, it := range d.vehicles {
		out.Vehicles = append(out.Vehicles, toDisk(it))
	}
	for k := range d.known {
		out.Known = append(out.Known, k)
	}
	sort.Strings(out.Known)
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// One copy: the older ones belong to game files that are gone.
	if old, err := filepath.Glob(filepath.Join(filepath.Dir(path), "items-*.json")); err == nil {
		for _, p := range old {
			if p != path {
				_ = os.Remove(p)
			}
		}
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

func loadDisk(fp string) *Data {
	path := diskPath(fp)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var in diskData
	if json.Unmarshal(data, &in) != nil || in.Version != diskVersion || in.Fingerprint != fp {
		return nil
	}
	d := &Data{strs: in.Strs, cols: in.Cols, known: make(map[string]bool, len(in.Known)), pbos: in.PBOs, mods: in.Mods, built: time.Unix(in.Built, 0)}
	if d.strs == nil {
		d.strs = map[string][]string{}
	}
	if d.cols == nil {
		d.cols = map[string]int{}
	}
	for _, k := range in.Known {
		d.known[k] = true
	}
	for _, it := range in.Items {
		d.items = append(d.items, fromDisk(it))
	}
	for _, it := range in.Survivors {
		d.survivors = append(d.survivors, fromDisk(it))
	}
	for _, it := range in.Vehicles {
		d.vehicles = append(d.vehicles, fromDisk(it))
	}
	d.index()
	return d
}
