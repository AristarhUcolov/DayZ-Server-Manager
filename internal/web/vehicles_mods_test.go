// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

// vehicleFixture is a mission with one vanilla car, a modded car registered in
// its own events file, and an installed mod that ships server files for a
// third car the mission does not know yet.
func vehicleFixture(t *testing.T) (*handlers, string, string) {
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(m, "cfgeconomycore.xml"), `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<economycore>
    <classes></classes>
    <ce folder="carmod_ce">
        <file name="carmod_events.xml" type="events" />
        <file name="carmod_spawnabletypes.xml" type="spawnabletypes" />
    </ce>
</economycore>
`)
	put(t, filepath.Join(m, "db", "events.xml"), `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<events>
    <event name="VehicleCivilianSedan">
        <nominal>8</nominal>
        <min>5</min>
        <max>11</max>
        <lifetime>300</lifetime>
        <restock>0</restock>
        <saveable>0</saveable>
        <active>1</active>
        <flags deletable="0" init_random="0" remove_damaged="1"/>
        <position>fixed</position>
        <limit>mixed</limit>
        <children>
            <child lootmax="0" lootmin="0" max="5" min="3" type="CivilianSedan"/>
        </children>
    </event>
    <event name="StaticHeliCrash">
        <nominal>3</nominal>
        <children>
            <child lootmax="15" lootmin="10" max="3" min="1" type="Wreck_UH1Y"/>
        </children>
    </event>
</events>
`)
	// A modded car whose event name does not start with "Vehicle": recognised
	// by the parts in its own registered spawnabletypes file.
	put(t, filepath.Join(m, "carmod_ce", "carmod_events.xml"), `<events>
    <event name="ModPickup">
        <nominal>4</nominal>
        <min>2</min>
        <max>6</max>
        <lifetime>300</lifetime>
        <active>1</active>
        <flags deletable="0" init_random="0" remove_damaged="1"/>
        <position>fixed</position>
        <children>
            <child lootmax="0" lootmin="0" max="4" min="2" type="Pickup_Red"/>
        </children>
    </event>
</events>
`)
	put(t, filepath.Join(m, "carmod_ce", "carmod_spawnabletypes.xml"), `<spawnabletypes>
    <type name="Pickup_Red">
        <attachments chance="0.80">
            <item name="PickupWheel" chance="1.00"/>
        </attachments>
        <attachments chance="0.50">
            <item name="CarBattery" chance="1.00"/>
        </attachments>
    </type>
</spawnabletypes>
`)
	put(t, filepath.Join(m, "cfgspawnabletypes.xml"), `<spawnabletypes>
    <type name="CivilianSedan">
        <attachments chance="0.80">
            <item name="CivSedanWheel" chance="1.00"/>
        </attachments>
    </type>
    <type name="Wreck_UH1Y">
        <cargo chance="1.00">
            <item name="M4A1" chance="1.00"/>
        </cargo>
    </type>
</spawnabletypes>
`)
	put(t, filepath.Join(m, "cfgeventspawns.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<eventposdef>
	<event name="VehicleCivilianSedan">
		<pos x="100" z="200" a="0"/>
	</event>
</eventposdef>
`)
	put(t, filepath.Join(m, "db", "types.xml"), `<types>
    <type name="CivilianSedan"><nominal>0</nominal><lifetime>3888000</lifetime></type>
</types>
`)
	mod := filepath.Join(dir, "@SuperCars")
	put(t, filepath.Join(mod, "ServerFiles", "events.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<events>
    <!-- copy this into your events.xml -->
    <event name="VehicleSuperCar">
        <nominal>3</nominal>
        <min>1</min>
        <max>4</max>
        <lifetime>300</lifetime>
        <restock>0</restock>
        <saveable>0</saveable>
        <active>1</active>
        <flags deletable="0" init_random="0" remove_damaged="1"/>
        <position>fixed</position>
        <limit>mixed</limit>
        <children>
            <child lootmax="0" lootmin="0" max="2" min="1" type="SuperCar_Blue"/>
        </children>
    </event>
</events>
`)
	put(t, filepath.Join(mod, "ServerFiles", "cfgeventspawns.xml"), `<eventposdef>
    <event name="VehicleSuperCar">
        <pos x="5000" z="6000" a="90"/>
        <pos x="5100" z="6100" a="180"/>
    </event>
</eventposdef>
`)
	put(t, filepath.Join(mod, "ServerFiles", "cfgspawnabletypes.xml"), `<spawnabletypes>
    <type name="SuperCar_Blue">
        <attachments chance="1.00">
            <item name="SuperCarWheel" chance="1.00"/>
        </attachments>
    </type>
</spawnabletypes>
`)
	put(t, filepath.Join(mod, "ServerFiles", "types.xml"), `<types>
    <type name="SuperCar_Blue">
        <nominal>0</nominal>
        <lifetime>3888000</lifetime>
        <flags count_in_cargo="0" count_in_hoarder="0" count_in_map="1" count_in_player="0" crafted="0" deloot="0"/>
    </type>
    <type name="SuperCarWheel">
        <nominal>10</nominal>
        <lifetime>14400</lifetime>
        <category name="vehiclesparts"/>
    </type>
    <type name="SuperCarKeychain">
        <nominal>5</nominal>
    </type>
</types>
`)
	return h, dir, m
}

// The Vehicles page lists modded cars from registered events files, by their
// parts, and says which file each one lives in; heli crashes stay out.
func TestVehiclesListIncludesRegisteredModFiles(t *testing.T) {
	h, _, _ := vehicleFixture(t)
	rec := httptest.NewRecorder()
	h.vehiclesList(rec, httptest.NewRequest("GET", "/api/vehicles", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Vehicles []vehicleRow `json:"vehicles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	got := map[string]vehicleRow{}
	for _, v := range resp.Vehicles {
		got[v.Name] = v
	}
	if len(got) != 2 {
		t.Fatalf("vehicles = %v, want VehicleCivilianSedan and ModPickup", got)
	}
	if got["ModPickup"].File != "carmod_ce/carmod_events.xml" || !got["ModPickup"].Children[0].Known {
		t.Fatalf("ModPickup = %+v", got["ModPickup"])
	}
	if got["VehicleCivilianSedan"].File != "db/events.xml" || got["VehicleCivilianSedan"].Positions != 1 {
		t.Fatalf("sedan = %+v", got["VehicleCivilianSedan"])
	}
}

// Importing a mod's car copies its event (flags and all), spawn points, parts
// and the missing types into dzm_vehicles, registers the files, and changes
// nothing that already existed.
func TestVehiclesImportFromMod(t *testing.T) {
	h, _, m := vehicleFixture(t)

	rec := httptest.NewRecorder()
	h.vehiclesMods(rec, httptest.NewRequest("GET", "/api/vehicles/mods", nil))
	var scan struct {
		Mods []modVehicles `json:"mods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &scan); err != nil {
		t.Fatal(err)
	}
	if len(scan.Mods) != 1 || scan.Mods[0].Mod != "@SuperCars" || len(scan.Mods[0].Vehicles) != 1 {
		t.Fatalf("scan = %+v", scan)
	}
	if v := scan.Mods[0].Vehicles[0]; v.Event != "VehicleSuperCar" || v.Positions != 2 || v.InMission || !v.HasParts {
		t.Fatalf("vehicle = %+v", v)
	}

	before, _ := os.ReadFile(filepath.Join(m, "db", "events.xml"))
	body, _ := json.Marshal(map[string]interface{}{"mod": "@SuperCars", "events": []string{"VehicleSuperCar"}})
	rec = httptest.NewRecorder()
	h.vehiclesImport(rec, httptest.NewRequest("POST", "/api/vehicles/import", bytes.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("import status %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["events"].(float64) != 1 || res["positions"].(float64) != 1 || res["parts"].(float64) != 1 || res["types"].(float64) != 2 {
		t.Fatalf("result = %v", res)
	}

	ev, _ := os.ReadFile(filepath.Join(m, dzmFolder, "events.xml"))
	for _, want := range []string{`<event name="VehicleSuperCar">`, `<flags deletable="0" init_random="0" remove_damaged="1"/>`, "<position>fixed</position>", "<limit>mixed</limit>"} {
		if !strings.Contains(string(ev), want) {
			t.Fatalf("imported events.xml lacks %q:\n%s", want, ev)
		}
	}
	if _, err := dztypes.LoadEvents(filepath.Join(m, dzmFolder, "events.xml")); err != nil {
		t.Fatalf("imported events.xml does not parse: %v", err)
	}
	ty, _ := os.ReadFile(filepath.Join(m, dzmFolder, "types.xml"))
	if !strings.Contains(string(ty), "SuperCar_Blue") || !strings.Contains(string(ty), "SuperCarWheel") || strings.Contains(string(ty), "SuperCarKeychain") {
		t.Fatalf("imported types.xml:\n%s", ty)
	}
	eco, _ := os.ReadFile(filepath.Join(m, "cfgeconomycore.xml"))
	for _, want := range []string{`<ce folder="dzm_vehicles">`, `name="events.xml" type="events"`, `name="spawnabletypes.xml" type="spawnabletypes"`, `name="types.xml" type="types"`, `carmod_events.xml`} {
		if !strings.Contains(string(eco), want) {
			t.Fatalf("cfgeconomycore.xml lacks %q:\n%s", want, eco)
		}
	}
	sp, err := dztypes.LoadEventSpawns(filepath.Join(m, "cfgeventspawns.xml"))
	if err != nil || sp.Find("VehicleSuperCar") == nil || len(sp.Find("VehicleSuperCar").Pos) != 2 || len(sp.Find("VehicleCivilianSedan").Pos) != 1 {
		t.Fatalf("cfgeventspawns: %+v %v", sp, err)
	}
	after, _ := os.ReadFile(filepath.Join(m, "db", "events.xml"))
	if !bytes.Equal(before, after) {
		t.Fatal("db/events.xml must not be touched")
	}

	// A second import of the same car changes nothing.
	rec = httptest.NewRecorder()
	h.vehiclesImport(rec, httptest.NewRequest("POST", "/api/vehicles/import", bytes.NewReader(body)))
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["events"].(float64) != 0 {
		t.Fatalf("re-import = %v", res)
	}
	// And the page now lists it, from dzm_vehicles/events.xml.
	rec = httptest.NewRecorder()
	h.vehiclesList(rec, httptest.NewRequest("GET", "/api/vehicles", nil))
	if !strings.Contains(rec.Body.String(), `"file":"dzm_vehicles/events.xml"`) {
		t.Fatalf("list after import: %s", rec.Body.String())
	}
}

// Saving a modded vehicle edits the file it lives in, nothing else.
func TestEventsItemWritesToTheEventsFile(t *testing.T) {
	h, _, m := vehicleFixture(t)
	body := `{"Name":"ModPickup","Nominal":9}`
	rec := httptest.NewRecorder()
	h.eventsItem(rec, httptest.NewRequest("POST", "/api/events/item?name=ModPickup&file=carmod_ce/carmod_events.xml", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := os.ReadFile(filepath.Join(m, "carmod_ce", "carmod_events.xml"))
	if !strings.Contains(string(got), "<nominal>9</nominal>") {
		t.Fatalf("carmod_events.xml not patched:\n%s", got)
	}
	vanilla, _ := os.ReadFile(filepath.Join(m, "db", "events.xml"))
	if strings.Contains(string(vanilla), "ModPickup") {
		t.Fatal("the modded event must not be appended to db/events.xml")
	}
	// A path outside the mission's registered files is refused.
	rec = httptest.NewRecorder()
	h.eventsItem(rec, httptest.NewRequest("POST", "/api/events/item?name=X&file=../../serverDZ.cfg", strings.NewReader(`{"Name":"X"}`)))
	if rec.Code == 200 {
		t.Fatal("an unregistered file must be refused")
	}
}
