// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The starter preset must be valid JSON and match the fields DayZ reads —
// generating a file the game silently ignores is the whole failure mode this
// feature exists to prevent.
func TestStarterGearPresetIsValid(t *testing.T) {
	p := StarterGearPreset()
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.json")
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadGearPreset(path)
	if err != nil {
		t.Fatalf("starter preset does not parse back: %v", err)
	}
	if back.SpawnWeight < 1 {
		t.Error("spawnWeight must be >= 1 or DayZ never picks the preset")
	}
	if len(back.AttachmentSlotItemSets) == 0 {
		t.Error("starter has no clothing — a naked spawn")
	}
	// The Body slot must offer at least one real item.
	var bodyItems int
	for _, s := range back.AttachmentSlotItemSets {
		if s.SlotName == "Body" {
			bodyItems = len(s.DiscreteItemSets)
		}
	}
	if bodyItems == 0 {
		t.Error("Body slot is empty")
	}
	// Fresh spawns must be PRISTINE, not worn: healthMin/healthMax are health
	// fractions where 1.0 = pristine. The first default shipped 0.45-0.7.
	for _, sl := range back.AttachmentSlotItemSets {
		for _, it := range sl.DiscreteItemSets {
			if it.Attributes == nil || it.Attributes.HealthMin == nil || *it.Attributes.HealthMin < 0.99 {
				t.Errorf("%s spawns worn (healthMin=%v), want pristine 1.0", it.ItemType, it.Attributes)
			}
		}
	}
	// Round-trip must be byte-stable (no drift on a no-op edit).
	raw1, _ := os.ReadFile(path)
	if err := back.Save(path); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(path)
	if string(raw1) != string(raw2) {
		t.Error("re-saving an unchanged preset changed the bytes")
	}
}

// Registering a preset must not disturb the rest of cfggameplay.json — the
// events.xml lesson, applied to JSON.
func TestSetRegisteredGearFilesPreservesOtherConfig(t *testing.T) {
	dir := t.TempDir()
	gp := filepath.Join(dir, "cfggameplay.json")
	os.WriteFile(gp, []byte(`{
    "version": 123,
    "GeneralData": { "disableBaseDamage": true },
    "PlayerData": {
        "disableRespawnDialog": false,
        "spawnGearPresetFiles": []
    },
    "WorldsData": { "environmentMinTemps": [-2, -1, 0] }
}`), 0o644)

	if err := SetRegisteredGearFiles(gp, []string{"fresh.json", "military.json"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(gp)
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("registration produced invalid JSON: %v", err)
	}
	// The unrelated sections must survive untouched.
	if doc["version"].(float64) != 123 {
		t.Error("version was lost or changed")
	}
	if doc["GeneralData"] == nil || doc["WorldsData"] == nil {
		t.Error("an unrelated top-level section was dropped")
	}
	pd := doc["PlayerData"].(map[string]interface{})
	if pd["disableRespawnDialog"] != false {
		t.Error("a sibling field inside PlayerData was dropped")
	}
	got := pd["spawnGearPresetFiles"].([]interface{})
	if len(got) != 2 || got[0] != "fresh.json" {
		t.Errorf("preset list wrong: %v", got)
	}

	// Reading it back through the typed helper agrees.
	files, err := RegisteredGearFiles(gp)
	if err != nil || len(files) != 2 {
		t.Fatalf("RegisteredGearFiles = %v, %v", files, err)
	}
}

// A missing cfggameplay.json is created rather than erroring.
func TestSetRegisteredGearFilesCreatesFile(t *testing.T) {
	dir := t.TempDir()
	gp := filepath.Join(dir, "cfggameplay.json")
	if err := SetRegisteredGearFiles(gp, []string{"fresh.json"}); err != nil {
		t.Fatal(err)
	}
	files, err := RegisteredGearFiles(gp)
	if err != nil || len(files) != 1 || files[0] != "fresh.json" {
		t.Fatalf("files=%v err=%v", files, err)
	}
	data, _ := os.ReadFile(gp)
	var doc map[string]interface{}
	if json.Unmarshal(data, &doc) != nil || doc["version"] == nil {
		t.Error("created cfggameplay.json is missing a version")
	}
}

// Unknown top-level fields (a future DayZ addition, or a mod's) must survive an
// edit rather than being dropped.
func TestGearPresetPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	os.WriteFile(path, []byte(`{
    "spawnWeight": 2,
    "name": "X",
    "characterTypes": [],
    "attachmentSlotItemSets": [],
    "discreteUnsortedItemSets": [],
    "futureFieldFromDayZ_1_30": { "keep": [1, 2, 3] }
}`), 0o644)
	p, err := LoadGearPreset(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "Y"
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !contains2(string(data), "futureFieldFromDayZ_1_30") {
		t.Errorf("unknown field was dropped on save:\n%s", data)
	}
}

// An item or cargo set the editor shows as "new" but that carries no explicit
// health must be written as pristine, not left to the engine's default — the
// exact way fresh spawns were arriving in worn gear despite a pristine setting.
func TestNormalizeFillsMissingConditionWithPristine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	// A worn item with no attributes, and a cargo set with loose children but
	// no inheritance flag — both the gaps that produced random condition.
	os.WriteFile(path, []byte(`{
    "spawnWeight": 1,
    "name": "X",
    "characterTypes": [],
    "attachmentSlotItemSets": [
        { "slotName": "Body", "discreteItemSets": [ { "itemType": "TShirt_Grey", "spawnWeight": 1 } ] }
    ],
    "discreteUnsortedItemSets": [
        { "name": "Cargo", "spawnWeight": 1, "simpleChildrenTypes": ["Rag", "Apple"] }
    ]
}`), 0o644)
	p, err := LoadGearPreset(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Save(path); err != nil { // Save normalizes
		t.Fatal(err)
	}
	back, err := LoadGearPreset(path)
	if err != nil {
		t.Fatal(err)
	}
	it := back.AttachmentSlotItemSets[0].DiscreteItemSets[0]
	if it.Attributes == nil || it.Attributes.HealthMin == nil || *it.Attributes.HealthMin != 1.0 {
		t.Errorf("worn item without attributes was not made pristine: %+v", it.Attributes)
	}
	set := back.DiscreteUnsortedItemSets[0]
	if set.Attributes == nil || set.Attributes.HealthMin == nil || *set.Attributes.HealthMin != 1.0 {
		t.Errorf("cargo set without attributes was not made pristine: %+v", set.Attributes)
	}
	if set.SimpleChildrenUseDefault == nil || !*set.SimpleChildrenUseDefault {
		t.Error("cargo children were not pinned to inherit the set's condition")
	}
}

// Normalize must never overwrite a condition the user chose on purpose.
func TestNormalizeLeavesExplicitConditionAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	os.WriteFile(path, []byte(`{
    "spawnWeight": 1, "name": "X", "characterTypes": [],
    "attachmentSlotItemSets": [
        { "slotName": "Body", "discreteItemSets": [
            { "itemType": "TShirt_Grey", "spawnWeight": 1, "attributes": { "healthMin": 0.45, "healthMax": 0.65 } } ] }
    ],
    "discreteUnsortedItemSets": []
}`), 0o644)
	p, _ := LoadGearPreset(path)
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	back, _ := LoadGearPreset(path)
	it := back.AttachmentSlotItemSets[0].DiscreteItemSets[0]
	if *it.Attributes.HealthMin != 0.45 || *it.Attributes.HealthMax != 0.65 {
		t.Errorf("explicit condition was overwritten: %v-%v", *it.Attributes.HealthMin, *it.Attributes.HealthMax)
	}
}

func contains2(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestGearFileName(t *testing.T) {
	cases := map[string]string{
		"Fresh spawn": "fresh_spawn.json", "Military!!!": "military.json",
		"":  "loadout.json", "  PvP Kit ": "pvp_kit.json",
	}
	for in, want := range cases {
		if got := GearFileName(in); got != want {
			t.Errorf("GearFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
