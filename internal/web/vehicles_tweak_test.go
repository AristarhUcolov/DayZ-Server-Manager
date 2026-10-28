// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/app"
)

// Saving a vehicle's fuel writes the tweaks mod at once (server stopped) and
// lists it as a server-side mod; clearing it takes both away again; a range
// out of order or out of 0..1 is refused.
func TestVehiclesTweakWritesMod(t *testing.T) {
	h, a := profileApp(t)
	dir := a.ServerDir
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.vehiclesTweak(rec, httptest.NewRequest("POST", "/api/vehicles/tweak", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"event":"VehicleCivilianSedan","classes":["CivilianSedan"],"fuel":{"min":0.8,"max":0.2}}`); rec.Code != 400 {
		t.Fatalf("min above max accepted: %d", rec.Code)
	}
	if rec := post(`{"event":"VehicleCivilianSedan","classes":["CivilianSedan","Bad\"Name"],"fuel":{"min":0.5,"max":1}}`); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	cfg := h.app.Cfg()
	if len(cfg.VehicleTweaks) != 1 || len(cfg.VehicleTweaks[0].Classes) != 1 {
		t.Fatalf("tweaks %+v", cfg.VehicleTweaks)
	}
	if len(cfg.ServerMods) != 1 || cfg.ServerMods[0] != app.TweaksMod {
		t.Fatalf("server mods %v", cfg.ServerMods)
	}
	if _, err := os.Stat(filepath.Join(dir, app.TweaksMod, "addons", "dzm_servertweaks.pbo")); err != nil {
		t.Fatal(err)
	}
	if rec := post(`{"event":"VehicleCivilianSedan","classes":["CivilianSedan"]}`); rec.Code != 200 {
		t.Fatalf("clear: %d", rec.Code)
	}
	if cfg := h.app.Cfg(); len(cfg.VehicleTweaks) != 0 || len(cfg.ServerMods) != 0 {
		t.Fatalf("after clearing: tweaks %+v, mods %v", cfg.VehicleTweaks, cfg.ServerMods)
	}
}
