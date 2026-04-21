// Copyright (c) 2026 Aristarh Ucolov.
//
// types.xml parser/editor for DayZ central economy.
//
// Schema (simplified):
//   <types>
//     <type name="AKM">
//       <nominal>15</nominal>
//       <lifetime>14400</lifetime>
//       <restock>1800</restock>
//       <min>8</min>
//       <quantmin>-1</quantmin>
//       <quantmax>-1</quantmax>
//       <cost>100</cost>
//       <flags count_in_cargo="0" count_in_hoarder="0" count_in_map="1" count_in_player="0" crafted="0" deloot="0"/>
//       <category name="weapons"/>
//       <usage name="Military"/>
//       <value name="Tier4"/>
//       <tag name="..."/>
//     </type>
//     ...
//   </types>
package types

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dayzmanager/internal/util"
)

type Flags struct {
	CountInCargo   int `xml:"count_in_cargo,attr"    json:"countInCargo"`
	CountInHoarder int `xml:"count_in_hoarder,attr"  json:"countInHoarder"`
	CountInMap     int `xml:"count_in_map,attr"      json:"countInMap"`
	CountInPlayer  int `xml:"count_in_player,attr"   json:"countInPlayer"`
	Crafted        int `xml:"crafted,attr"           json:"crafted"`
	Deloot         int `xml:"deloot,attr"            json:"deloot"`
}

type NamedRef struct {
	Name string `xml:"name,attr" json:"name"`
}

type Type struct {
	XMLName  xml.Name   `xml:"type"             json:"-"`
	Name     string     `xml:"name,attr"        json:"name"`
	Nominal  *int       `xml:"nominal,omitempty"  json:"nominal,omitempty"`
	Lifetime *int       `xml:"lifetime,omitempty" json:"lifetime,omitempty"`
	Restock  *int       `xml:"restock,omitempty"  json:"restock,omitempty"`
	Min      *int       `xml:"min,omitempty"      json:"min,omitempty"`
	QuantMin *int       `xml:"quantmin,omitempty" json:"quantmin,omitempty"`
	QuantMax *int       `xml:"quantmax,omitempty" json:"quantmax,omitempty"`
	Cost     *int       `xml:"cost,omitempty"     json:"cost,omitempty"`
	Flags    *Flags     `xml:"flags,omitempty"    json:"flags,omitempty"`
	Category *NamedRef  `xml:"category,omitempty" json:"category,omitempty"`
	Usages   []NamedRef `xml:"usage"              json:"usages,omitempty"`
	Values   []NamedRef `xml:"value"              json:"values,omitempty"`
	Tags     []NamedRef `xml:"tag"                json:"tags,omitempty"`
}

type TypesDoc struct {
	XMLName xml.Name `xml:"types"`
	Types   []Type   `xml:"type"`
}

func Load(path string) (*TypesDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc TypesDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return &doc, nil
}

func (d *TypesDoc) Save(path string) error {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "    ")
	if err := enc.Encode(d); err != nil {
		return err
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_ = util.BackupBeforeWrite(path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the first matching type by name (case-insensitive).
func (d *TypesDoc) Find(name string) *Type {
	for i := range d.Types {
		if strings.EqualFold(d.Types[i].Name, name) {
			return &d.Types[i]
		}
	}
	return nil
}

// Upsert inserts or replaces a type by name.
func (d *TypesDoc) Upsert(t Type) {
	for i := range d.Types {
		if strings.EqualFold(d.Types[i].Name, t.Name) {
			d.Types[i] = t
			return
		}
	}
	d.Types = append(d.Types, t)
}

// Remove deletes types by name. Returns how many were removed.
func (d *TypesDoc) Remove(names ...string) int {
	set := map[string]struct{}{}
	for _, n := range names {
		set[strings.ToLower(n)] = struct{}{}
	}
	out := d.Types[:0]
	removed := 0
	for _, t := range d.Types {
		if _, drop := set[strings.ToLower(t.Name)]; drop {
			removed++
			continue
		}
		out = append(out, t)
	}
	d.Types = out
	return removed
}

// Sort orders types alphabetically by name (nice for diffs).
func (d *TypesDoc) Sort() {
	sort.Slice(d.Types, func(i, j int) bool {
		return strings.ToLower(d.Types[i].Name) < strings.ToLower(d.Types[j].Name)
	})
}

// BulkPatch applies the non-nil numeric fields of patch to every type named
// in names (case-insensitive). Scalar fields only — Usages/Values/Tags are
// intentionally left out because the meaning of "merge" vs "replace" for
// those is ambiguous at bulk scale and the per-type editor already handles
// them. Returns the count of types actually touched.
type BulkFieldPatch struct {
	Nominal  *int `json:"nominal,omitempty"`
	Lifetime *int `json:"lifetime,omitempty"`
	Restock  *int `json:"restock,omitempty"`
	Min      *int `json:"min,omitempty"`
	QuantMin *int `json:"quantmin,omitempty"`
	QuantMax *int `json:"quantmax,omitempty"`
	Cost     *int `json:"cost,omitempty"`
	Category *string `json:"category,omitempty"`
}

func (d *TypesDoc) BulkPatch(names []string, patch BulkFieldPatch) int {
	want := map[string]struct{}{}
	for _, n := range names {
		want[strings.ToLower(n)] = struct{}{}
	}
	touched := 0
	for i := range d.Types {
		if _, ok := want[strings.ToLower(d.Types[i].Name)]; !ok {
			continue
		}
		t := &d.Types[i]
		if patch.Nominal != nil {
			v := *patch.Nominal
			t.Nominal = &v
		}
		if patch.Lifetime != nil {
			v := *patch.Lifetime
			t.Lifetime = &v
		}
		if patch.Restock != nil {
			v := *patch.Restock
			t.Restock = &v
		}
		if patch.Min != nil {
			v := *patch.Min
			t.Min = &v
		}
		if patch.QuantMin != nil {
			v := *patch.QuantMin
			t.QuantMin = &v
		}
		if patch.QuantMax != nil {
			v := *patch.QuantMax
			t.QuantMax = &v
		}
		if patch.Cost != nil {
			v := *patch.Cost
			t.Cost = &v
		}
		if patch.Category != nil {
			if *patch.Category == "" {
				t.Category = nil
			} else {
				t.Category = &NamedRef{Name: *patch.Category}
			}
		}
		touched++
	}
	return touched
}

// ---------------------------------------------------------------------------
// Spawn presets — quick bulk tag/value/usage assignments the user can apply
// from the UI. Kept as a simple built-in list; the UI can extend it later.

type Preset struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	LabelRU     string     `json:"labelRu"`
	Usages      []NamedRef `json:"usages,omitempty"`
	Values      []NamedRef `json:"values,omitempty"`
	Tags        []NamedRef `json:"tags,omitempty"`
	Category    string     `json:"category,omitempty"`
	Nominal     *int       `json:"nominal,omitempty"`
	Min         *int       `json:"min,omitempty"`
	Lifetime    *int       `json:"lifetime,omitempty"`
	Restock     *int       `json:"restock,omitempty"`
}

func ip(v int) *int { return &v }

// BuiltinPresets ships with sensible starters covering the most common tiers
// and locations. Applying a preset merges its fields into the selected type(s).
func BuiltinPresets() []Preset {
	return []Preset{
		{ID: "military-tier4", Label: "Military — Tier 4", LabelRU: "Военные — Tier 4",
			Usages: []NamedRef{{Name: "Military"}}, Values: []NamedRef{{Name: "Tier4"}}},
		{ID: "military-tier3", Label: "Military — Tier 3", LabelRU: "Военные — Tier 3",
			Usages: []NamedRef{{Name: "Military"}}, Values: []NamedRef{{Name: "Tier3"}}},
		{ID: "civilian", Label: "Civilian", LabelRU: "Гражданские",
			Usages: []NamedRef{{Name: "Village"}, {Name: "Town"}}, Values: []NamedRef{{Name: "Tier1"}, {Name: "Tier2"}}},
		{ID: "industrial", Label: "Industrial", LabelRU: "Промышленные",
			Usages: []NamedRef{{Name: "Industrial"}}, Values: []NamedRef{{Name: "Tier2"}, {Name: "Tier3"}}},
		{ID: "hunting", Label: "Hunting", LabelRU: "Охотничьи",
			Usages: []NamedRef{{Name: "Hunting"}, {Name: "Farm"}}, Values: []NamedRef{{Name: "Tier1"}, {Name: "Tier2"}}},
		{ID: "rare",
			Label: "Rare (low nominal, long lifetime)", LabelRU: "Редкие (низкий nominal, большой lifetime)",
			Nominal: ip(2), Min: ip(1), Lifetime: ip(28800), Restock: ip(3600)},
	}
}
