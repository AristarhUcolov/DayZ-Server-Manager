// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

// writeTestPBO makes a PBO with plain (uncompressed) files.
func writeTestPBO(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var hdr, data bytes.Buffer
	u32 := func(v uint32) { _ = binary.Write(&hdr, binary.LittleEndian, v) }
	for name, content := range files {
		hdr.WriteString(name)
		hdr.WriteByte(0)
		u32(0)
		u32(uint32(len(content)))
		u32(0)
		u32(0)
		u32(uint32(len(content)))
		data.WriteString(content)
	}
	hdr.WriteByte(0)
	for i := 0; i < 5; i++ {
		u32(0)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append(hdr.Bytes(), data.Bytes()...), make([]byte, 21)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

const myCarsConfig = `class CfgPatches { class MyCars { units[] = {}; }; };
class CfgVehicles
{
	class CarScript;
	class CarWheel;
	class CarDoor;
	class MyCar_Base: CarScript
	{
		scope = 0;
		displayName = "My Car";
		attachments[] = {"MyCarWheel_1_1", "MyCar_Door_1_1", "CarBattery", "SparkPlug"};
	};
	class MyCar: MyCar_Base { scope = 2; };
	class MyCar_Black: MyCar { scope = 2; };
	class MyCarWheel: CarWheel { scope = 2; inventorySlot[] = {"MyCarWheel_1_1", "MyCarWheel_1_2"}; };
	class MyCarWheel_Ruined: MyCarWheel { scope = 2; };
	class MyCar_Door_1_1: CarDoor { scope = 2; inventorySlot = "MyCar_Door_1_1"; };
	class MyCar_Door_1_1_Black: MyCar_Door_1_1 { scope = 2; };
};`

// A car mod with no server files at all shows up on the Vehicles page, and
// one step sets it up: an event modelled on a vanilla car, its spawn spots,
// types and parts matched to the car's slots (black doors for the black car).
func TestDetectAndSetUpModVehicles(t *testing.T) {
	h, dir, m := vehicleFixture(t)
	writeTestPBO(t, filepath.Join(dir, "@MyCars", "addons", "mycars.pbo"), map[string]string{"config.cpp": myCarsConfig})

	rec := httptest.NewRecorder()
	h.vehiclesMods(rec, httptest.NewRequest("GET", "/api/vehicles/mods", nil))
	var scan struct {
		Mods []modVehicles `json:"mods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &scan); err != nil {
		t.Fatal(err)
	}
	var mine *modVehicles
	for i := range scan.Mods {
		if scan.Mods[i].Mod == "@MyCars" {
			mine = &scan.Mods[i]
		}
	}
	if mine == nil || len(mine.Detected) != 1 {
		t.Fatalf("scan = %+v", scan.Mods)
	}
	f := mine.Detected[0]
	if f.Family != "MyCar_Base" || f.Event != "VehicleMyCar" || strings.Join(f.Variants, ",") != "MyCar,MyCar_Black" || f.Spawning || f.Display != "My Car" {
		t.Fatalf("family = %+v", f)
	}

	body, _ := json.Marshal(map[string]interface{}{"mod": "@MyCars", "family": "MyCar_Base", "nominal": 3, "copyFrom": "VehicleCivilianSedan", "parts": true})
	rec = httptest.NewRecorder()
	h.vehiclesSetup(rec, httptest.NewRequest("POST", "/api/vehicles/setup", bytes.NewReader(body)))
	if rec.Code != 400 {
		t.Fatalf("a switched-off mod must be refused: %d %s", rec.Code, rec.Body)
	}
	h.app.Config.Mods = []string{"@MyCars"}
	rec = httptest.NewRecorder()
	h.vehiclesSetup(rec, httptest.NewRequest("POST", "/api/vehicles/setup", bytes.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("setup %d: %s", rec.Code, rec.Body)
	}
	evs, err := dztypes.LoadEvents(filepath.Join(m, dzmFolder, "events.xml"))
	if err != nil {
		t.Fatal(err)
	}
	e := evs.Find("VehicleMyCar")
	if e == nil || e.Children == nil || len(e.Children.Child) != 2 || *e.Nominal != 3 {
		t.Fatalf("event: %+v", e)
	}
	ev, _ := os.ReadFile(filepath.Join(m, dzmFolder, "events.xml"))
	if !strings.Contains(string(ev), "<lifetime>300</lifetime>") || !strings.Contains(string(ev), `remove_damaged="1"`) {
		t.Fatalf("the model's settings were not kept:\n%s", ev)
	}
	sp, _ := dztypes.LoadEventSpawns(filepath.Join(m, "cfgeventspawns.xml"))
	if p := sp.Find("VehicleMyCar"); p == nil || len(p.Pos) != 1 || p.Pos[0].X != 100 {
		t.Fatalf("spots: %+v", p)
	}
	ty, _ := os.ReadFile(filepath.Join(m, dzmFolder, "types.xml"))
	if !strings.Contains(string(ty), `name="MyCar"`) || !strings.Contains(string(ty), `name="MyCar_Black"`) || !strings.Contains(string(ty), "<lifetime>3888000</lifetime>") {
		t.Fatalf("types:\n%s", ty)
	}
	parts, _ := os.ReadFile(filepath.Join(m, dzmFolder, "spawnabletypes.xml"))
	ps := string(parts)
	blackAt := strings.Index(ps, `name="MyCar_Black"`)
	if blackAt < 0 || !strings.Contains(ps[blackAt:], "MyCar_Door_1_1_Black") || !strings.Contains(ps, `"MyCarWheel"`) ||
		strings.Contains(ps, "Ruined") || !strings.Contains(ps, `"CarBattery"`) || !strings.Contains(ps, `"SparkPlug"`) {
		t.Fatalf("parts:\n%s", ps)
	}
	plainAt := strings.Index(ps, `name="MyCar"`)
	if plainAt < 0 || blackAt < plainAt || strings.Contains(ps[plainAt:blackAt], "Door_1_1_Black") {
		t.Fatalf("the plain car must get the plain door:\n%s", ps)
	}
	// Now listed as spawning, and a second setup is refused by name clash
	// handling (a new, numbered event) — not by overwriting.
	rec = httptest.NewRecorder()
	h.vehiclesMods(rec, httptest.NewRequest("GET", "/api/vehicles/mods", nil))
	if !strings.Contains(rec.Body.String(), `"spawning":true`) {
		t.Fatalf("after setup: %s", rec.Body)
	}
}

// Switching a vehicle mod on adds its cars; off switches them off; on again
// brings them back — without losing the event.
func TestModVehiclesFollowTheMod(t *testing.T) {
	h, _, m := vehicleFixture(t)
	setMods := func(mods ...string) { h.app.Config.Mods = mods }
	setMods("@SuperCars")
	res := h.syncModVehicles([]string{"@SuperCars"})
	if len(res) != 1 || strings.Join(res[0].Added, ",") != "VehicleSuperCar" {
		t.Fatalf("on: %+v", res)
	}
	path := filepath.Join(m, dzmFolder, "events.xml")
	active := func() int {
		doc, err := dztypes.LoadEvents(path)
		if err != nil || doc.Find("VehicleSuperCar") == nil {
			t.Fatalf("event gone: %v", err)
		}
		if a := doc.Find("VehicleSuperCar").Active; a != nil {
			return *a
		}
		return 1
	}
	if active() != 1 {
		t.Fatal("added switched off")
	}
	setMods()
	res = h.syncModVehicles([]string{"@SuperCars"})
	if len(res) != 1 || strings.Join(res[0].Deactivated, ",") != "VehicleSuperCar" || active() != 0 {
		t.Fatalf("off: %+v", res)
	}
	setMods("@SuperCars")
	res = h.syncModVehicles([]string{"@SuperCars"})
	if len(res) != 1 || strings.Join(res[0].Reactivated, ",") != "VehicleSuperCar" || len(res[0].Added) != 0 || active() != 1 {
		t.Fatalf("on again: %+v", res)
	}
	// Turned off in Settings: nothing happens.
	h.app.Config.AutoModVehiclesOff, h.app.Config.Mods = true, nil
	if res := h.syncModVehicles([]string{"@SuperCars"}); res != nil || active() != 1 {
		t.Fatalf("auto off: %+v", res)
	}
}
