// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

type eventsListRow struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Points   int    `json:"points"`
	Position string `json:"position"`
	Dup      bool   `json:"dup"`
	Children int    `json:"children"`
}

func getEventsList(t *testing.T, h *handlers) map[string][]eventsListRow {
	t.Helper()
	rec := httptest.NewRecorder()
	h.eventsList(rec, httptest.NewRequest("GET", "/api/events", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/events: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Events []eventsListRow `json:"events"`
		Files  []string        `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	by := map[string][]eventsListRow{}
	for _, r := range out.Events {
		by[r.Name] = append(by[r.Name], r)
	}
	return by
}

// The Events page lists the events of every events file the mission uses —
// a mod's registered file too, not only db/events.xml — with the file each
// comes from, how it spawns and its spawn points; a second definition of the
// same event is flagged (DayZ uses the first).
func TestEventsListCoversEveryEventsFile(t *testing.T) {
	h, _, m := vehicleFixture(t)

	by := getEventsList(t, h)
	sedan := by["VehicleCivilianSedan"]
	if len(sedan) != 1 || sedan[0].File != "db/events.xml" || sedan[0].Points != 1 || sedan[0].Position != "fixed" || sedan[0].Dup {
		t.Fatalf("VehicleCivilianSedan = %+v", sedan)
	}
	mod := by["ModPickup"]
	if len(mod) != 1 || mod[0].File != "carmod_ce/carmod_events.xml" || mod[0].Children != 1 {
		t.Fatalf("ModPickup from the registered mod file = %+v", mod)
	}
	if heli := by["StaticHeliCrash"]; len(heli) != 1 || heli[0].Points != 0 {
		t.Fatalf("StaticHeliCrash = %+v", heli)
	}

	// The mod file defines the sedan again: listed twice, the second flagged.
	put(t, filepath.Join(m, "carmod_ce", "carmod_events.xml"), `<events>
    <event name="ModPickup"><nominal>4</nominal><position>fixed</position></event>
    <event name="VehicleCivilianSedan"><nominal>2</nominal><position>fixed</position></event>
</events>
`)
	by = getEventsList(t, h)
	sedan = by["VehicleCivilianSedan"]
	if len(sedan) != 2 || sedan[0].Dup || !sedan[1].Dup || sedan[1].File != "carmod_ce/carmod_events.xml" {
		t.Fatalf("duplicate sedan = %+v", sedan)
	}
}

// A vehicle the panel imported from a mod says which mod, and whether that
// mod is loaded (its vehicles are switched off with it); the parts chance all
// variants share is reported so the page can mark the current setting.
func TestVehiclesListReportsModAndPartsChance(t *testing.T) {
	h, _, m := vehicleFixture(t)
	put(t, vehicleSourcesPath(m), `{"events": {"ModPickup": "@CarMod"}}`)

	type row struct {
		Name        string   `json:"name"`
		Mod         string   `json:"mod"`
		ModActive   *bool    `json:"modActive"`
		PartsChance *float64 `json:"partsChance"`
	}
	list := func() map[string]row {
		rec := httptest.NewRecorder()
		h.vehiclesList(rec, httptest.NewRequest("GET", "/api/vehicles", nil))
		var out struct {
			Vehicles []row `json:"vehicles"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		by := map[string]row{}
		for _, v := range out.Vehicles {
			by[v.Name] = v
		}
		return by
	}

	by := list()
	pickup, ok := by["ModPickup"]
	if !ok || pickup.Mod != "@CarMod" || pickup.ModActive == nil || *pickup.ModActive {
		t.Fatalf("ModPickup with its mod off = %+v", pickup)
	}
	if pickup.PartsChance != nil {
		t.Fatalf("ModPickup parts differ (0.80 / 0.50) but partsChance = %v", *pickup.PartsChance)
	}
	sedan := by["VehicleCivilianSedan"]
	if sedan.Mod != "" || sedan.ModActive != nil {
		t.Fatalf("vanilla sedan reports a mod: %+v", sedan)
	}
	if sedan.PartsChance == nil || *sedan.PartsChance != 0.8 {
		t.Fatalf("sedan partsChance = %v, want 0.8", sedan.PartsChance)
	}

	h.app.Config.Mods = []string{"@CarMod"}
	if p := list()["ModPickup"]; p.ModActive == nil || !*p.ModActive {
		t.Fatalf("ModPickup with its mod on = %+v", p)
	}
}
