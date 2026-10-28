// Copyright (c) 2026 Aristarh Ucolov.
package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/config"
	dztypes "dayzmanager/internal/types"
)

// updatedServer is a server whose stock files were just replaced by a DayZ
// update: serverDZ.cfg without enableCfgGameplayFile, a cfggameplay.json with
// no loadout list, a cfgeconomycore.xml without the panel's dzm_vehicles —
// while the panel's own files are still in the mission folder.
func updatedServer(t *testing.T) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	write(filepath.Join(m, "cfgeconomycore.xml"), "<economycore>\n    <classes></classes>\n</economycore>\n")
	write(filepath.Join(m, "cfggameplay.json"), `{"version": 130, "PlayerData": {"disablePersonalLight": true}}`)
	write(filepath.Join(m, dzmFolder, "events.xml"), "<events>\n</events>\n")
	write(filepath.Join(m, dzmFolder, "types.xml"), "<types>\n</types>\n")
	if err := dztypes.StarterGearPreset().Save(filepath.Join(m, "fresh_spawn.json")); err != nil {
		t.Fatal(err)
	}
	return Options{ServerDir: dir, Mission: "dayzOffline.chernarusplus", ServerCfg: "serverDZ.cfg"}, m
}

// Before the server starts, the manager puts back what the update undid.
func TestHealAfterUpdate(t *testing.T) {
	o, m := updatedServer(t)
	got := codes(checkAfterUpdate(o))
	if got["dzm.unregistered"] == 0 || got["gear.unlinked"] == 0 {
		t.Fatalf("issues = %v, want dzm.unregistered and gear.unlinked", got)
	}
	done, err := HealAfterUpdate(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) == 0 {
		t.Fatal("nothing was repaired")
	}
	if refs, _ := dztypes.ListEconomyCE(filepath.Join(m, "cfgeconomycore.xml")); len(refs) != 2 {
		t.Errorf("dzm_vehicles registrations = %v, want events and types", refs)
	}
	if files, _ := dztypes.RegisteredGearFiles(filepath.Join(m, "cfggameplay.json")); len(files) != 1 || files[0] != "fresh_spawn.json" {
		t.Errorf("loadouts = %v", files)
	}
	cfg, _ := config.LoadServerCfg(filepath.Join(o.ServerDir, "serverDZ.cfg"))
	if v, _ := cfg.Get("enableCfgGameplayFile"); v != "1" {
		t.Errorf("enableCfgGameplayFile = %q", v)
	}
	if left := checkAfterUpdate(o); len(left) != 0 {
		t.Errorf("still reported after the repair: %+v", left)
	}
	// Running it again changes nothing.
	if again, _ := HealAfterUpdate(o); len(again) != 0 {
		t.Errorf("second run did %v", again)
	}
}

// An empty list the admin left on purpose is not filled at start — only the
// validator mentions the unlinked files.
func TestHealKeepsAnEmptiedList(t *testing.T) {
	o, m := updatedServer(t)
	os.WriteFile(filepath.Join(m, "cfggameplay.json"), []byte(`{"version": 130, "PlayerData": {"spawnGearPresetFiles": []}}`), 0o644)
	if _, err := HealAfterUpdate(o); err != nil {
		t.Fatal(err)
	}
	if files, _ := dztypes.RegisteredGearFiles(filepath.Join(m, "cfggameplay.json")); len(files) != 0 {
		t.Errorf("an emptied list was refilled: %v", files)
	}
	if codes(checkAfterUpdate(o))["gear.unlinked"] == 0 {
		t.Error("the unlinked loadout is not reported")
	}
}

// A listed loadout file that is gone switches every loadout off; the fix
// drops the entry, and with the rest still listed turns the switch on.
func TestBrokenLoadoutEntry(t *testing.T) {
	o, m := updatedServer(t)
	gp := filepath.Join(m, "cfggameplay.json")
	if err := dztypes.SetRegisteredGearFiles(gp, []string{"fresh_spawn.json", "gone.json"}); err != nil {
		t.Fatal(err)
	}
	got := codes(checkAfterUpdate(o))
	if got["gear.broken"] == 0 || got["gear.flagOff"] == 0 {
		t.Fatalf("issues = %v, want gear.broken and gear.flagOff", got)
	}
	done, err := AutoFixWith(o, []string{"gear.broken", "gear.flagOff"})
	if err != nil {
		t.Fatal(err)
	}
	if files, _ := dztypes.RegisteredGearFiles(gp); len(files) != 1 || files[0] != "fresh_spawn.json" {
		t.Errorf("after the fix: %v (did %v)", files, done)
	}
	data, _ := os.ReadFile(filepath.Join(o.ServerDir, "serverDZ.cfg"))
	if !strings.Contains(string(data), "enableCfgGameplayFile = 1;") {
		t.Errorf("the switch was not turned on:\n%s", data)
	}
}
