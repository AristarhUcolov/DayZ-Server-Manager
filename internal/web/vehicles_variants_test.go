// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Vehicles page's parts editor reads each colour variant's entry and
// writes it back into the file that defines it — the mod's own spawnabletypes
// file for a modded car — keeping what it does not edit.
func TestVehicleVariantsReadAndSave(t *testing.T) {
	h, _, m := vehicleFixture(t)

	get := func(event string) map[string]interface{} {
		rec := httptest.NewRecorder()
		h.vehiclesVariants(rec, httptest.NewRequest("GET", "/api/vehicles/variants?event="+event, nil))
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d %s", event, rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	out := get("ModPickup")
	vs, _ := out["variants"].([]interface{})
	if len(vs) != 1 {
		t.Fatalf("variants: %v", out)
	}
	v := vs[0].(map[string]interface{})
	if v["type"] != "Pickup_Red" || v["file"] != "carmod_ce/carmod_spawnabletypes.xml" || v["entry"] == nil {
		t.Fatalf("variant: %v", v)
	}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.vehiclesVariants(rec, httptest.NewRequest("POST", "/api/vehicles/variants", strings.NewReader(body)))
		return rec
	}
	rec := post(`{"event":"ModPickup","variants":[{"type":"Pickup_Red","attachments":[
		{"chance":"1.00","items":[{"name":"PickupWheel","chance":"1.00"}]},
		{"chance":"0.40","items":[{"name":"PickupDoor_Red","chance":"3"},{"name":"PickupDoor_Rust","chance":"1"},{"name":"  ","chance":"1"}]},
		{"chance":"0.50","items":[]}]}]}`)
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(m, "carmod_ce", "carmod_spawnabletypes.xml"))
	s := string(data)
	if !strings.Contains(s, `<item name="PickupDoor_Rust" chance="1"/>`) || strings.Contains(s, `CarBattery`) {
		t.Fatalf("the mod's file was not rewritten as edited:\n%s", s)
	}
	if strings.Count(s, "<attachments") != 2 {
		t.Fatalf("an empty group or a nameless part was written:\n%s", s)
	}
	if main, _ := os.ReadFile(filepath.Join(m, "cfgspawnabletypes.xml")); strings.Contains(string(main), "Pickup_Red") {
		t.Fatal("the variant was written into cfgspawnabletypes.xml as well")
	}

	// What is in the trunk is written when sent, and kept when not.
	if rec := post(`{"event":"ModPickup","variants":[{"type":"Pickup_Red","attachments":[{"chance":"1.00","items":[{"name":"PickupWheel","chance":"1.00"}]}],
		"cargo":[{"chance":"0.30","items":[{"name":"Canteen","chance":"1.00"}]}]}]}`); rec.Code != 200 {
		t.Fatalf("save with cargo: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"event":"ModPickup","variants":[{"type":"Pickup_Red","attachments":[{"chance":"0.50","items":[{"name":"PickupWheel","chance":"1.00"}]}]}]}`); rec.Code != 200 {
		t.Fatalf("save without cargo: %d", rec.Code)
	}
	data, _ = os.ReadFile(filepath.Join(m, "carmod_ce", "carmod_spawnabletypes.xml"))
	if s := string(data); !strings.Contains(s, `<cargo chance="0.30">`) || !strings.Contains(s, `Canteen`) || !strings.Contains(s, `<attachments chance="0.50">`) {
		t.Fatalf("cargo not written or not kept:\n%s", s)
	}

	// A vanilla car keeps its cargo-free entry in the main file; a bad chance
	// and a type of another vehicle are refused.
	if rec := post(`{"event":"VehicleCivilianSedan","variants":[{"type":"CivilianSedan","attachments":[{"chance":"x","items":[{"name":"CivSedanWheel"}]}]}]}`); rec.Code != 400 {
		t.Fatalf("bad chance accepted: %d", rec.Code)
	}
	if rec := post(`{"event":"VehicleCivilianSedan","variants":[{"type":"Wreck_UH1Y","attachments":[]}]}`); rec.Code != 400 {
		t.Fatalf("another vehicle's entry accepted: %d", rec.Code)
	}
}
