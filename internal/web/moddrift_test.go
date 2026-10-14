// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

// Register lists an existing moded_types file; Delete unregisters it and
// keeps a copy in the manager's trash instead of removing it for good.
func TestModedRegisterAndDeleteToTrash(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	eco := filepath.Join(m, "cfgeconomycore.xml")
	put(t, eco, "<economycore>\n</economycore>\n")
	body := typesXML("HandMade")
	put(t, filepath.Join(m, "moded_types", "mine.xml"), body)

	rec := httptest.NewRecorder()
	h.modedRegister(rec, httptest.NewRequest("POST", "/api/moded/register", strings.NewReader(`{"fileName":"mine.xml"}`)))
	if rec.Code != 200 {
		t.Fatalf("modedRegister = %d %s", rec.Code, rec.Body.String())
	}
	if reg, _ := dztypes.RegisteredInModed(eco); !reg["mine.xml"] {
		t.Fatal("mine.xml is not registered after modedRegister")
	}
	rec = httptest.NewRecorder()
	h.modedRegister(rec, httptest.NewRequest("POST", "/api/moded/register", strings.NewReader(`{"fileName":"nope.xml"}`)))
	if rec.Code == 200 {
		t.Error("registering a missing file succeeded")
	}

	rec = httptest.NewRecorder()
	h.modedDelete(rec, httptest.NewRequest("POST", "/api/moded/delete", strings.NewReader(`{"fileName":"mine.xml"}`)))
	if rec.Code != 200 {
		t.Fatalf("modedDelete = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(m, "moded_types", "mine.xml")); !os.IsNotExist(err) {
		t.Error("mine.xml is still in moded_types")
	}
	if reg, _ := dztypes.RegisteredInModed(eco); reg["mine.xml"] {
		t.Error("mine.xml is still registered")
	}
	kept, _ := filepath.Glob(filepath.Join(h.app.ManagerDir, "trash", "moded", "*", "*-mine.xml"))
	if len(kept) != 1 {
		t.Fatalf("trash holds %v, want one copy of mine.xml", kept)
	}
	if b, _ := os.ReadFile(kept[0]); string(b) != body {
		t.Error("the trashed copy differs from the deleted file")
	}
}

// An unreadable cfgrandompresets.xml is reported as such, and the editor may
// not save over it: its list is empty, not the file's content.
func TestRandomPresetsBrokenFileIsNotOverwritten(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	p := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "cfgrandompresets.xml")
	// A comma decimal — an easy slip on a Russian keyboard — fails to parse.
	broken := "<randompresets>\n\t<cargo chance=\"0,3\" name=\"a\">\n\t\t<item name=\"X\" chance=\"1\"/>\n\t</cargo>\n</randompresets>\n"
	put(t, p, broken)

	rec := httptest.NewRecorder()
	h.randomPresetsList(rec, httptest.NewRequest("GET", "/api/randompresets", nil))
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("list of a broken file = %s, want an error", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.randomPresetsSave(rec, httptest.NewRequest("POST", "/api/randompresets/save",
		strings.NewReader(`{"presets":[{"kind":"cargo","name":"new","chance":1,"items":[{"name":"Y","chance":1}]}]}`)))
	if rec.Code == 200 {
		t.Error("saving over an unreadable file succeeded")
	}
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Error("the unreadable file was overwritten")
	}
}

// Where to find: hits say whether DayZ loads their file, and list the other
// ways the item spawns (on/in another item, a random preset, an event).
func TestLootWhereRegistrationAndOtherPaths(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(m, "db", "types.xml"), typesXML("Mag_AKM_30Rnd"))
	put(t, filepath.Join(m, "moded_types", "parked.xml"), typesXML("Mag_AKM_Drum"))
	put(t, filepath.Join(m, "cfgspawnabletypes.xml"), `<spawnabletypes>
    <type name="AKM">
        <attachments chance="0.5">
            <item name="Mag_AKM_30Rnd" chance="3"/>
            <item name="Mag_AKM_Drum" chance="1"/>
        </attachments>
    </type>
</spawnabletypes>
`)
	put(t, filepath.Join(m, "cfgrandompresets.xml"), `<randompresets>
	<cargo chance="0.2" name="mixArmy">
		<item name="Mag_AKM_30Rnd" chance="1"/>
	</cargo>
</randompresets>
`)
	put(t, filepath.Join(m, "db", "events.xml"), `<events>
    <event name="StaticHeliCrash">
        <children>
            <child lootmax="0" lootmin="0" max="1" min="1" type="Mag_AKM_30Rnd"/>
        </children>
    </event>
</events>
`)
	rec := httptest.NewRecorder()
	h.lootWhere(rec, httptest.NewRequest("GET", "/api/loot/where?q=mag_akm", nil))
	var out struct {
		Hits []struct {
			Name       string `json:"name"`
			File       string `json:"file"`
			Registered bool   `json:"registered"`
			AlsoIn     []struct {
				Kind   string  `json:"kind"`
				Parent string  `json:"parent"`
				Chance float64 `json:"chance"`
			} `json:"alsoIn"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	if len(out.Hits) != 2 {
		t.Fatalf("hits = %+v, want 2", out.Hits)
	}
	for _, hit := range out.Hits {
		switch hit.Name {
		case "Mag_AKM_30Rnd":
			if !hit.Registered || hit.File != "types.xml" {
				t.Errorf("30Rnd = %+v, want registered in types.xml", hit)
			}
			kinds := map[string]bool{}
			for _, r := range hit.AlsoIn {
				kinds[r.Kind] = true
				if r.Kind == "attachment" && (r.Parent != "AKM" || r.Chance < 0.374 || r.Chance > 0.376) {
					t.Errorf("attachment ref = %+v, want AKM at 0.375", r)
				}
			}
			if !kinds["attachment"] || !kinds["preset"] || !kinds["event"] {
				t.Errorf("30Rnd alsoIn = %+v, want attachment, preset and event", hit.AlsoIn)
			}
		case "Mag_AKM_Drum":
			if hit.Registered {
				t.Error("a hit from an unregistered moded_types file reads as registered")
			}
		}
	}
}

// globals.xml values are checked against their variable's type before any of
// them is written.
func TestGlobalsSaveRejectsBadNumbers(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	p := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "db", "globals.xml")
	orig := "<variables>\n\t<var name=\"ZombieMaxCount\" type=\"0\" value=\"1000\"/>\n\t<var name=\"LootDamageMax\" type=\"1\" value=\"0.82\"/>\n</variables>\n"
	put(t, p, orig)
	for _, body := range []string{
		`{"vars":[{"name":"ZombieMaxCount","value":"0.5"}]}`,
		`{"vars":[{"name":"ZombieMaxCount","value":""}]}`,
		`{"vars":[{"name":"LootDamageMax","value":"1,5"}]}`,
		`{"vars":[{"name":"LootDamageMax","value":"0.9"},{"name":"ZombieMaxCount","value":"abc"}]}`,
	} {
		rec := httptest.NewRecorder()
		h.globalsSave(rec, httptest.NewRequest("POST", "/api/globals/save", strings.NewReader(body)))
		if rec.Code == 200 {
			t.Errorf("globalsSave(%s) = 200, want a refusal", body)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatal("a refused save changed globals.xml")
	}
	rec := httptest.NewRecorder()
	h.globalsSave(rec, httptest.NewRequest("POST", "/api/globals/save", strings.NewReader(`{"vars":[{"name":"ZombieMaxCount","value":" 800 "},{"name":"LootDamageMax","value":"0.9"}]}`)))
	if rec.Code != 200 {
		t.Fatalf("valid save = %d %s", rec.Code, rec.Body.String())
	}
	doc, _ := dztypes.LoadGlobals(p)
	if doc.Vars[0].Value != "800" || doc.Vars[1].Value != "0.9" {
		t.Errorf("saved values = %+v", doc.Vars)
	}
}

func typesXML(names ...string) string {
	s := "<types>\n"
	for _, n := range names {
		s += "    <type name=\"" + n + "\"><nominal>1</nominal></type>\n"
	}
	return s + "</types>\n"
}

// Loot counts as active only when DayZ loads its file: db/types.xml or a file
// cfgeconomycore.xml registers — in moded_types or any other CE folder. A
// moded_types file nobody registered is reported separately.
func TestMissionTypeSetsFollowRegistration(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(m, "db", "types.xml"), typesXML("AKM"))
	put(t, filepath.Join(m, "moded_types", "reg.xml"), typesXML("RegGun"))
	put(t, filepath.Join(m, "moded_types", "unreg.xml"), typesXML("ParkedGun", "AKM"))
	put(t, filepath.Join(m, "expansion_ce", "exp_types.xml"), typesXML("ExpGun"))
	put(t, filepath.Join(m, "cfgeconomycore.xml"), `<economycore>
    <ce folder="moded_types">
        <file name="reg.xml" type="types" />
    </ce>
    <ce folder="expansion_ce">
        <file type="types" name="exp_types.xml"/>
        <file name="exp_spawnable.xml" type="spawnabletypes"/>
    </ce>
    <ce folder="../outside">
        <file name="evil.xml" type="types"/>
    </ce>
</economycore>
`)
	put(t, filepath.Join(dir, "mpmissions", "outside", "evil.xml"), typesXML("EvilGun"))

	active, parked := h.missionTypeSets()
	for _, n := range []string{"akm", "reggun", "expgun"} {
		if !active[n] {
			t.Errorf("%s should be active", n)
		}
	}
	for _, n := range []string{"parkedgun", "evilgun"} {
		if active[n] {
			t.Errorf("%s must not count as active", n)
		}
	}
	if parked["parkedgun"] != "unreg.xml" {
		t.Errorf("parked = %v, want parkedgun → unreg.xml", parked)
	}
	if _, ok := parked["akm"]; ok {
		t.Error("a name that is also loaded from types.xml is not parked")
	}
}

// The drift report counts every installed mod, not only those shipping loot
// (the page's empty state says how many were checked), and names the
// unregistered moded_types file that holds a mod's missing loot.
func TestModsDriftCountsInstalledAndUnregistered(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(m, "db", "types.xml"), typesXML("AKM"))
	put(t, filepath.Join(m, "cfgeconomycore.xml"), "<economycore>\n</economycore>\n")
	put(t, filepath.Join(m, "moded_types", "parked.xml"), typesXML("ModGun"))
	put(t, filepath.Join(dir, "@LootMod", "types.xml"), typesXML("ModGun", "ModAmmo"))
	put(t, filepath.Join(dir, "@NoLoot", "readme.txt"), "no loot here")

	rec := httptest.NewRecorder()
	h.modsDrift(rec, httptest.NewRequest("GET", "/api/mods/drift", nil))
	if rec.Code != 200 {
		t.Fatalf("modsDrift = %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Checked   int `json:"checked"`
		Installed int `json:"installed"`
		Mods      []struct {
			Mod               string   `json:"mod"`
			Missing           int      `json:"missing"`
			Unregistered      int      `json:"unregistered"`
			UnregisteredFiles []string `json:"unregisteredFiles"`
		} `json:"mods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Installed != 2 || out.Checked != 1 {
		t.Errorf("installed=%d checked=%d, want 2 and 1", out.Installed, out.Checked)
	}
	if len(out.Mods) != 1 {
		t.Fatalf("mods = %+v, want only @LootMod", out.Mods)
	}
	got := out.Mods[0]
	if got.Mod != "@LootMod" || got.Missing != 2 || got.Unregistered != 1 ||
		len(got.UnregisteredFiles) != 1 || got.UnregisteredFiles[0] != "parked.xml" {
		t.Errorf("report = %+v, want 2 missing, 1 of them in parked.xml", got)
	}
}
