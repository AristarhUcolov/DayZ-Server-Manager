// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleSpawnable = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<spawnabletypes>
    <!-- keep this comment -->
    <type name="AKM">
        <attachments chance="1.00">
            <item name="Mag_AKM_30Rnd" chance="1.00"/>
        </attachments>
        <attachments chance="0.35">
            <item name="KobraOptic" chance="0.60"/>
            <item name="PSO1Optic" chance="0.40"/>
        </attachments>
    </type>
    <type name="Barrel_Green">
        <cargo preset="foodVillage"/>
    </type>
    <type name="FutureThing" somethingUnmodelled="1">
        <weirdFutureElement value="42"/>
    </type>
</spawnabletypes>
`

func writeSample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfgspawnabletypes.xml")
	if err := os.WriteFile(p, []byte(sampleSpawnable), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSpawnable(t *testing.T) {
	list, err := LoadSpawnable(writeSample(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d types, want 3", len(list))
	}
	akm := list[0]
	if akm.Name != "AKM" {
		t.Errorf("name = %q", akm.Name)
	}
	if len(akm.Attachments) != 2 {
		t.Fatalf("AKM attachment groups = %d, want 2", len(akm.Attachments))
	}
	if akm.Attachments[0].Chance != "1.00" || akm.Attachments[0].Items[0].Name != "Mag_AKM_30Rnd" {
		t.Errorf("first group wrong: %+v", akm.Attachments[0])
	}
	if len(akm.Attachments[1].Items) != 2 || akm.Attachments[1].Items[1].Name != "PSO1Optic" {
		t.Errorf("second group wrong: %+v", akm.Attachments[1])
	}
	// Chance strings must round-trip verbatim, not be reformatted to "0.6".
	if akm.Attachments[1].Items[0].Chance != "0.60" {
		t.Errorf("chance = %q, want %q", akm.Attachments[1].Items[0].Chance, "0.60")
	}
	if list[1].Cargo == nil || list[1].Cargo[0].Preset != "foodVillage" {
		t.Errorf("cargo preset not parsed: %+v", list[1])
	}
}

// The whole point of the surgical writer: editing one type must not touch
// comments or elements this package does not model.
func TestSaveSpawnableTypePreservesEverythingElse(t *testing.T) {
	p := writeSample(t)
	st := &SpawnableType{
		Name: "AKM",
		Attachments: []SpawnGroup{
			{Chance: "1.00", Items: []SpawnItem{{Name: "Mag_AKM_Drum75Rnd", Chance: "1.00"}}},
		},
	}
	if err := SaveSpawnableType(p, st); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	s := string(out)

	if !strings.Contains(s, "<!-- keep this comment -->") {
		t.Error("comment was lost")
	}
	if !strings.Contains(s, `somethingUnmodelled="1"`) || !strings.Contains(s, "<weirdFutureElement value=\"42\"/>") {
		t.Error("unmodelled future element was lost")
	}
	if !strings.Contains(s, `preset="foodVillage"`) {
		t.Error("other type's cargo preset was lost")
	}
	if !strings.Contains(s, "Mag_AKM_Drum75Rnd") {
		t.Error("new attachment not written")
	}
	if strings.Contains(s, "KobraOptic") {
		t.Error("old AKM optic group should have been replaced")
	}
	// Exactly one AKM block must remain.
	if n := strings.Count(s, `<type name="AKM"`); n != 1 {
		t.Errorf("AKM blocks = %d, want 1", n)
	}
	// Re-parsing must still work and reflect the edit.
	list, err := LoadSpawnable(p)
	if err != nil {
		t.Fatalf("file no longer parses: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("types after save = %d, want 3", len(list))
	}
}

func TestSaveSpawnableTypeAppendsNew(t *testing.T) {
	p := writeSample(t)
	st := &SpawnableType{
		Name:        "M4A1",
		Attachments: []SpawnGroup{{Chance: "1.00", Items: []SpawnItem{{Name: "Mag_STANAG_30Rnd"}}}},
	}
	if err := SaveSpawnableType(p, st); err != nil {
		t.Fatal(err)
	}
	list, err := LoadSpawnable(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("types = %d, want 4", len(list))
	}
	var found *SpawnableType
	for i := range list {
		if list[i].Name == "M4A1" {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatal("M4A1 not appended")
	}
	// An item with no explicit chance defaults to 1.00 on render.
	if found.Attachments[0].Items[0].Chance != "1.00" {
		t.Errorf("default chance = %q", found.Attachments[0].Items[0].Chance)
	}
}

func TestDeleteSpawnableType(t *testing.T) {
	p := writeSample(t)
	n, err := DeleteSpawnableType(p, "Barrel_Green")
	if err != nil || n != 1 {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
	list, _ := LoadSpawnable(p)
	if len(list) != 2 {
		t.Errorf("types = %d, want 2", len(list))
	}
	if n, _ := DeleteSpawnableType(p, "NotThere"); n != 0 {
		t.Error("deleting a missing type reported success")
	}
}

// A <type> an admin commented out to disable it is not a live entry. Editing
// the same name must rewrite the LIVE block, not splice a copy inside the
// comment — which is what v0.15.0 did, producing a save that appeared to work
// while the weapon spawned with nothing.
func TestCommentedOutBlocksAreNotTouched(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.xml")
	src := `<spawnabletypes>
    <!-- disabled for now
    <type name="AKM">
        <attachments chance="1.00"><item name="OLD" chance="1.00"/></attachments>
    </type>
    -->
    <type name="AKM">
        <attachments chance="1.00"><item name="LIVE" chance="1.00"/></attachments>
    </type>
</spawnabletypes>
`
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only the live entry is visible.
	list, err := LoadSpawnable(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Attachments[0].Items[0].Name != "LIVE" {
		t.Fatalf("commented-out block leaked into the parse: %+v", list)
	}

	st := &SpawnableType{Name: "AKM", Attachments: []SpawnGroup{
		{Chance: "1.00", Items: []SpawnItem{{Name: "NEW", Chance: "1.00"}}},
	}}
	if err := SaveSpawnableType(p, st); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	if !strings.Contains(got, `name="OLD"`) {
		t.Error("the commented-out block was modified")
	}
	if strings.Contains(got, `name="LIVE"`) {
		t.Error("the live block was not replaced")
	}
	if !strings.Contains(got, `name="NEW"`) {
		t.Error("the new block was not written")
	}
	// Exactly one live entry must remain — not an appended duplicate.
	after, err := LoadSpawnable(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("live types = %d, want 1:\n%s", len(after), got)
	}
}

// Vanilla Livonia writes `-------` inside a comment, which is illegal XML.
// DayZ ignores comments; so must we, or the whole editor is dead on that map.
func TestLoadSpawnableSurvivesIllegalComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.xml")
	src := `<spawnabletypes>
    <!--------- weapons ---------->
    <type name="AKM">
        <attachments chance="1.00"><item name="Mag_AKM_30Rnd" chance="1.00"/></attachments>
    </type>
</spawnabletypes>
`
	os.WriteFile(p, []byte(src), 0o644)
	list, err := LoadSpawnable(p)
	if err != nil {
		t.Fatalf("illegal comment broke the parse: %v", err)
	}
	if len(list) != 1 || list[0].Name != "AKM" {
		t.Fatalf("got %+v", list)
	}
}

// A name that is a prefix of another must not match the wrong block.
func TestTypeBlockRegexIsExact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.xml")
	src := `<spawnabletypes>
    <type name="AK">
        <attachments chance="1.00"><item name="A" chance="1.00"/></attachments>
    </type>
    <type name="AK74">
        <attachments chance="1.00"><item name="B" chance="1.00"/></attachments>
    </type>
</spawnabletypes>
`
	os.WriteFile(p, []byte(src), 0o644)
	st := &SpawnableType{Name: "AK", Attachments: []SpawnGroup{{Chance: "1.00", Items: []SpawnItem{{Name: "CHANGED"}}}}}
	if err := SaveSpawnableType(p, st); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	s := string(out)
	if !strings.Contains(s, `name="B"`) {
		t.Error("AK74 block was clobbered when editing AK")
	}
	if !strings.Contains(s, "CHANGED") {
		t.Error("AK block was not updated")
	}
	list, _ := LoadSpawnable(p)
	if len(list) != 2 {
		t.Errorf("types = %d, want 2", len(list))
	}
}

func TestValidChance(t *testing.T) {
	for _, ok := range []string{"", "1", "1.00", "0.35", "0"} {
		if !ValidChance(ok) {
			t.Errorf("ValidChance(%q) = false", ok)
		}
	}
	for _, bad := range []string{"abc", "-1", "1,5"} {
		if ValidChance(bad) {
			t.Errorf("ValidChance(%q) = true", bad)
		}
	}
}
