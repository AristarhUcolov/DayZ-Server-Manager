// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/config"
	dztypes "dayzmanager/internal/types"
)

// gearFixture is a server whose serverDZ.cfg is the DayZ 1.29 vanilla one —
// no enableCfgGameplayFile line at all — and whose cfggameplay.json lists no
// presets, the state a server update leaves behind.
func gearFixture(t *testing.T) (*handlers, string, string) {
	t.Helper()
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n    class DayZ\n    {\n        template=\"dayzOffline.chernarusplus\";\n    };\n};\n")
	mission := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(mission, "cfggameplay.json"), `{"version":130,"PlayerData":{"disablePersonalLight":true,"spawnGearPresetFiles":[]}}`)
	put(t, filepath.Join(mission, "cfgeffectarea.json"), `{"Areas":[],"SafePositions":[]}`)
	return h, dir, mission
}

func gearState(t *testing.T, h *handlers) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h.spawnGearList(rec, httptest.NewRequest("GET", "/api/spawngear", nil))
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func gearEnable(t *testing.T, h *handlers) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h.spawnGearEnable(rec, httptest.NewRequest("POST", "/api/spawngear/enable", strings.NewReader("{}")))
	if rec.Code != 200 {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func registeredGear(t *testing.T, mission string) []string {
	t.Helper()
	files, err := dztypes.RegisteredGearFiles(filepath.Join(mission, "cfggameplay.json"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// The reported bug: fresh_spawn.json exists from an earlier "Enable", a
// server update reset cfggameplay.json, the page said "init.c" and Enable
// failed with "a loadout named fresh_spawn.json already exists". Enable must
// reconnect the existing file (keeping the admin's edits), switch the server
// flag on, and be safe to press again.
func TestSpawnGearEnableReusesUnlinkedPreset(t *testing.T) {
	h, dir, mission := gearFixture(t)
	custom := dztypes.StarterGearPreset()
	custom.Name = "Fresh spawn"
	custom.CharacterTypes = []string{"SurvivorF_Eva"} // the admin's edit
	if err := custom.Save(filepath.Join(mission, "fresh_spawn.json")); err != nil {
		t.Fatal(err)
	}

	st := gearState(t, h)
	if st["active"] != false {
		t.Fatal("an unregistered preset must not count as active")
	}
	if un, _ := st["unlinked"].([]interface{}); len(un) != 1 {
		t.Fatalf("the unregistered fresh_spawn.json was not reported: %v", st["unlinked"])
	}

	out := gearEnable(t, h)
	if linked, _ := out["linked"].([]interface{}); len(linked) != 1 || linked[0] != "fresh_spawn.json" {
		t.Fatalf("enable did not reconnect the existing file: %v", out)
	}
	if out["created"] != "" {
		t.Errorf("enable created %v although a preset was there", out["created"])
	}
	back, err := dztypes.LoadGearPreset(filepath.Join(mission, "fresh_spawn.json"))
	if err != nil || len(back.CharacterTypes) != 1 {
		t.Fatalf("the admin's preset was replaced: %+v %v", back, err)
	}
	cfg, _ := config.LoadServerCfg(filepath.Join(dir, "serverDZ.cfg"))
	if v, _ := cfg.Get("enableCfgGameplayFile"); v != "1" {
		t.Errorf("enableCfgGameplayFile = %q, want 1 (the line is missing from the 1.29 vanilla file)", v)
	}
	if cfg.MissionTemplate() != "dayzOffline.chernarusplus" {
		t.Error("adding the flag broke the Missions block")
	}
	if st := gearState(t, h); st["active"] != true {
		t.Fatalf("after enable the loadout is not active: %v", st)
	}

	// Pressing it again changes nothing.
	gearEnable(t, h)
	if files := registeredGear(t, mission); len(files) != 1 {
		t.Errorf("second enable registered %v", files)
	}
}

// With nothing to reuse, Enable creates the starter loadout; a file of that
// name that DayZ cannot read is left alone and the starter gets a free name.
func TestSpawnGearEnableNeverOverwrites(t *testing.T) {
	h, _, mission := gearFixture(t)
	put(t, filepath.Join(mission, "fresh_spawn.json"), `{"spawnWeight": 1, "name": "broken",`)
	out := gearEnable(t, h)
	if out["created"] != "fresh_spawn_2.json" {
		t.Fatalf("created %v, want fresh_spawn_2.json", out["created"])
	}
	if data, _ := os.ReadFile(filepath.Join(mission, "fresh_spawn.json")); !strings.Contains(string(data), "broken") {
		t.Error("the unreadable file was overwritten")
	}
	if files := registeredGear(t, mission); len(files) != 1 || files[0] != "fresh_spawn_2.json" {
		t.Errorf("registered %v", files)
	}
}

// DayZ stops at the first registered file it cannot read and switches every
// loadout off, so one missing file must make the page say "not active".
func TestSpawnGearMissingFileBlocksAll(t *testing.T) {
	h, dir, mission := gearFixture(t)
	gearEnable(t, h)
	_ = dztypes.SetRegisteredGearFiles(filepath.Join(mission, "cfggameplay.json"), []string{"fresh_spawn.json", "gone.json"})
	st := gearState(t, h)
	if st["active"] != false || st["blocked"] != true {
		t.Fatalf("a missing registered file must block the loadout: %v", st)
	}
	// Deleting the broken entry clears it.
	rec := httptest.NewRecorder()
	h.spawnGearDelete(rec, httptest.NewRequest("DELETE", "/api/spawngear/delete?file=gone.json", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	if st := gearState(t, h); st["active"] != true {
		t.Fatalf("still blocked after removing the broken entry: %v", st)
	}
	_ = dir
}

// A new or imported preset whose name matches an existing file gets its own
// file instead of overwriting the other one.
func TestSpawnGearSaveNewKeepsExisting(t *testing.T) {
	h, _, mission := gearFixture(t)
	gearEnable(t, h)
	body := `{"file":"","preset":{"name":"Fresh spawn","spawnWeight":1,"characterTypes":[],"attachmentSlotItemSets":[],"discreteUnsortedItemSets":[]}}`
	rec := httptest.NewRecorder()
	h.spawnGearSave(rec, httptest.NewRequest("POST", "/api/spawngear/save", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["file"] != "fresh_spawn_2.json" {
		t.Fatalf("saved as %v", out["file"])
	}
	first, _ := dztypes.LoadGearPreset(filepath.Join(mission, "fresh_spawn.json"))
	if first == nil || len(first.AttachmentSlotItemSets) == 0 {
		t.Error("the existing loadout was overwritten by the new one")
	}
}

// Linking an unregistered file works, and a file DayZ cannot read is refused
// (it would switch every loadout off).
func TestSpawnGearRegister(t *testing.T) {
	h, _, mission := gearFixture(t)
	_ = os.MkdirAll(filepath.Join(mission, "custom"), 0o755)
	if err := dztypes.StarterGearPreset().Save(filepath.Join(mission, "custom", "pvp.json")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(mission, "bad.json"), `{"attachmentSlotItemSets": [`)
	post := func(file string) int {
		rec := httptest.NewRecorder()
		h.spawnGearRegister(rec, httptest.NewRequest("POST", "/api/spawngear/register", strings.NewReader(`{"file":"`+file+`"}`)))
		return rec.Code
	}
	if code := post("custom/pvp.json"); code != 200 {
		t.Fatalf("register: %d", code)
	}
	if code := post("bad.json"); code == 200 {
		t.Error("an unreadable preset was registered")
	}
	if code := post("../serverDZ.cfg"); code == 200 {
		t.Error("a path outside the mission was accepted")
	}
	if files := registeredGear(t, mission); len(files) != 1 || files[0] != "custom/pvp.json" {
		t.Errorf("registered %v", files)
	}
}

// A mission without cfggameplay.json gets the full vanilla file, not one that
// holds nothing but the preset list.
func TestSpawnGearCreatesFullGameplayFile(t *testing.T) {
	h, _, mission := gearFixture(t)
	_ = os.Remove(filepath.Join(mission, "cfggameplay.json"))
	gearEnable(t, h)
	data, err := os.ReadFile(filepath.Join(mission, "cfggameplay.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"StaminaData", "BaseBuildingData", "spawnGearPresetFiles"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("created cfggameplay.json lacks %s", key)
		}
	}
}
