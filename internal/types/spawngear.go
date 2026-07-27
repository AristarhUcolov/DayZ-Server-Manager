// Copyright (c) 2026 Aristarh Ucolov.
//
// Player fresh-spawn loadout — DayZ's spawn-gear system.
//
// What a player spawns WEARING and CARRYING is not in cfggameplay.json (that is
// stamina, building, environment) and not in cfgeconomycore.xml. In vanilla it
// is hardcoded in the mission's init.c, which the panel must not touch. DayZ
// 1.20+ added a data-driven alternative: one or more preset files, each a plain
// JSON object, registered in cfggameplay.json under
// PlayerData.spawnGearPresetFiles. This models that file.
//
// Schema (verified against the Bohemia wiki and the DayZ modding wiki):
//
//	{
//	  "spawnWeight": 1,               // relative chance vs other presets
//	  "name": "Basic Survivor",
//	  "characterTypes": ["SurvivorM_Mirek", ...],  // one picked at random
//	  "attachmentSlotItemSets": [     // what is worn, per body slot
//	    { "slotName": "Body",
//	      "discreteItemSets": [ { "itemType": "TShirt_Beige", "spawnWeight": 1,
//	          "attributes": {healthMin,healthMax,quantityMin,quantityMax},
//	          "quickBarSlot": -1,
//	          "simpleChildrenTypes": [...], "complexChildrenTypes": [...] } ] } ],
//	  "discreteUnsortedItemSets": [   // what is carried loose in inventory
//	    { "name": "Cargo1", "spawnWeight": 1, "attributes": {...},
//	      "simpleChildrenTypes": ["Rag","Apple"] } ]
//	}
//
// Fields the schema does not name are preserved verbatim (json.RawMessage
// passthrough), so a future DayZ addition survives an edit — the same rule the
// XML writers follow.
package types

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GearAttributes is the health/quantity range an item spawns with. Pointers so
// an omitted attribute round-trips as absent rather than as 0.
type GearAttributes struct {
	HealthMin   *float64 `json:"healthMin,omitempty"`
	HealthMax   *float64 `json:"healthMax,omitempty"`
	QuantityMin *float64 `json:"quantityMin,omitempty"`
	QuantityMax *float64 `json:"quantityMax,omitempty"`
}

// GearItem is one item variant, in a slot or in cargo.
type GearItem struct {
	ItemType                 string          `json:"itemType,omitempty"`
	Name                     string          `json:"name,omitempty"` // cargo sets carry a name instead
	SpawnWeight              int             `json:"spawnWeight"`
	Attributes               *GearAttributes `json:"attributes,omitempty"`
	QuickBarSlot             *int            `json:"quickBarSlot,omitempty"`
	SimpleChildrenTypes      []string        `json:"simpleChildrenTypes,omitempty"`
	ComplexChildrenTypes     []GearItem      `json:"complexChildrenTypes,omitempty"`
	SimpleChildrenUseDefault *bool           `json:"simpleChildrenUseDefaultAttributes,omitempty"`
}

// GearSlot is one attachment slot (Body, Legs, Feet, …) and its variants.
type GearSlot struct {
	SlotName         string     `json:"slotName"`
	DiscreteItemSets []GearItem `json:"discreteItemSets"`
}

// GearPreset is one loadout preset — the whole file.
type GearPreset struct {
	SpawnWeight              int        `json:"spawnWeight"`
	Name                     string     `json:"name"`
	CharacterTypes           []string   `json:"characterTypes"`
	AttachmentSlotItemSets   []GearSlot `json:"attachmentSlotItemSets"`
	DiscreteUnsortedItemSets []GearItem `json:"discreteUnsortedItemSets"`

	// extra keeps any top-level field the schema above does not model, so a
	// future DayZ addition is not silently dropped on save.
	extra map[string]json.RawMessage `json:"-"`
}

// LoadGearPreset reads and parses one preset file, keeping unknown top-level
// keys for round-tripping.
func LoadGearPreset(path string) (*GearPreset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p GearPreset
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	// Second pass: capture every key, then delete the modelled ones, leaving
	// only the extras.
	raw := map[string]json.RawMessage{}
	_ = json.Unmarshal(data, &raw)
	for _, known := range []string{"spawnWeight", "name", "characterTypes",
		"attachmentSlotItemSets", "discreteUnsortedItemSets"} {
		delete(raw, known)
	}
	if len(raw) > 0 {
		p.extra = raw
	}
	return &p, nil
}

// Save writes the preset as pretty JSON, folding the preserved extras back in.
func (p *GearPreset) Save(path string) error {
	// Marshal the modelled fields, then merge extras at the top level.
	base, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(p.extra) > 0 {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(base, &m)
		for k, v := range p.extra {
			m[k] = v
		}
		base, err = json.Marshal(m)
		if err != nil {
			return err
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(base, &out); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(out, "", "    ")
	if err != nil {
		return err
	}
	return writeAtomic(path, pretty)
}

// StarterGearPreset returns a sensible, valid starting loadout: the vanilla
// fresh-spawn clothes plus a rag and an apple, so enabling the system produces
// something that visibly works rather than a naked spawn.
func StarterGearPreset() *GearPreset {
	one := 1.0
	hMin, hMax := 0.45, 0.7
	noQuick := -1
	item := func(cls string) GearItem {
		return GearItem{
			ItemType: cls, SpawnWeight: 1, QuickBarSlot: &noQuick,
			Attributes: &GearAttributes{HealthMin: &hMin, HealthMax: &hMax},
		}
	}
	return &GearPreset{
		SpawnWeight:    1,
		Name:           "Fresh spawn",
		CharacterTypes: []string{}, // empty = all survivor types
		AttachmentSlotItemSets: []GearSlot{
			{SlotName: "Body", DiscreteItemSets: []GearItem{item("TShirt_Grey"), item("TShirt_Blue")}},
			{SlotName: "Legs", DiscreteItemSets: []GearItem{item("Jeans_Blue"), item("CanvasPants_Beige")}},
			{SlotName: "Feet", DiscreteItemSets: []GearItem{item("JoggingShoes_Black"), item("AthleticShoes_Grey")}},
		},
		DiscreteUnsortedItemSets: []GearItem{
			{
				Name: "Starter", SpawnWeight: 1,
				Attributes:          &GearAttributes{QuantityMin: &one, QuantityMax: &one},
				SimpleChildrenTypes: []string{"Rag", "Apple"},
			},
		},
	}
}

// ---- cfggameplay.json registration ---------------------------------------

// GameplayRegistration edits cfggameplay.json's PlayerData.spawnGearPresetFiles
// list without disturbing the rest of the file (stamina, building, etc.).
// cfggameplay.json is strict JSON with no comments, so a parse + re-marshal is
// content-safe — unlike the XML files, key order is not meaningful here.

// RegisteredGearFiles returns the preset paths listed in cfggameplay.json,
// relative to the mission folder. Missing file or field yields an empty list,
// not an error.
func RegisteredGearFiles(gameplayPath string) ([]string, error) {
	data, err := os.ReadFile(gameplayPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		PlayerData struct {
			SpawnGearPresetFiles []string `json:"spawnGearPresetFiles"`
		} `json:"PlayerData"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse cfggameplay.json: %w", err)
	}
	return doc.PlayerData.SpawnGearPresetFiles, nil
}

// SetRegisteredGearFiles rewrites PlayerData.spawnGearPresetFiles to exactly
// the given list, creating cfggameplay.json (with a version) if absent and
// leaving every other field untouched.
func SetRegisteredGearFiles(gameplayPath string, files []string) error {
	root := map[string]interface{}{}
	if data, err := os.ReadFile(gameplayPath); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse cfggameplay.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, ok := root["version"]; !ok {
		root["version"] = 1
	}
	pd, _ := root["PlayerData"].(map[string]interface{})
	if pd == nil {
		pd = map[string]interface{}{}
	}
	// json.MarshalIndent renders []string cleanly; store as []interface{} to
	// keep the surrounding map homogeneous.
	arr := make([]interface{}, 0, len(files))
	for _, f := range files {
		arr = append(arr, f)
	}
	pd["spawnGearPresetFiles"] = arr
	root["PlayerData"] = pd

	pretty, err := json.MarshalIndent(root, "", "    ")
	if err != nil {
		return err
	}
	return writeAtomic(gameplayPath, pretty)
}

// GearFileName sanitises a preset name into a mission-relative json filename.
func GearFileName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "loadout"
	}
	return out + ".json"
}

// SortGearFiles returns the list in a stable, human order.
func SortGearFiles(files []string) []string {
	out := append([]string(nil), files...)
	sort.Strings(out)
	return out
}
