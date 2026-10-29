// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/app"
)

// Saving a fresh character's food, water and warmth writes the tweaks mod at
// once (server stopped); a value outside 0..1 is refused; clearing every
// value takes the mod away again.
func TestSpawnStatsWritesMod(t *testing.T) {
	h, a := profileApp(t)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.spawnStats(rec, httptest.NewRequest("POST", "/api/spawngear/stats", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"food":1.5}`); rec.Code != 400 {
		t.Fatalf("food above 1 accepted: %d", rec.Code)
	}
	if rec := post(`{"food":1,"water":0.75}`); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	st := h.app.Cfg().SpawnStats
	if st == nil || st.Food == nil || *st.Food != 1 || st.Water == nil || *st.Water != 0.75 || st.Warmth != nil {
		t.Fatalf("stats %+v", st)
	}
	if mods := h.app.Cfg().ServerMods; len(mods) != 1 || mods[0] != app.TweaksMod {
		t.Fatalf("server mods %v", mods)
	}
	if _, err := os.Stat(filepath.Join(a.ServerDir, app.TweaksMod, "addons", "dzm_servertweaks.pbo")); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.spawnStats(rec, httptest.NewRequest("GET", "/api/spawngear/stats", nil))
	var got struct {
		Stats map[string]float64 `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Stats["food"] != 1 || got.Stats["water"] != 0.75 {
		t.Fatalf("GET: %s", rec.Body.String())
	}

	if rec := post(`{}`); rec.Code != 200 {
		t.Fatalf("clear: %d", rec.Code)
	}
	if cfg := h.app.Cfg(); cfg.SpawnStats != nil || len(cfg.ServerMods) != 0 {
		t.Fatalf("after clearing: stats %+v, mods %v", cfg.SpawnStats, cfg.ServerMods)
	}
}

// The "round in the chamber" switch: turning it off lists the weapon class
// (lower case, once) and writes the tweaks mod; turning it back on removes
// it, and the mod with it; a class that could break the script is refused.
func TestSpawnChamberSwitch(t *testing.T) {
	h, a := profileApp(t)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.spawnChamber(rec, httptest.NewRequest("POST", "/api/spawngear/chamber", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"class":"M4\"A1","chambered":false}`); rec.Code != 400 {
		t.Fatalf("unsafe class accepted: %d", rec.Code)
	}
	for _, body := range []string{`{"class":"M4A1","chambered":false}`, `{"class":"m4a1","chambered":false}`, `{"class":"Mosin9130","chambered":false}`} {
		if rec := post(body); rec.Code != 200 {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if got := h.app.Cfg().Unchambered; len(got) != 2 || got[0] != "m4a1" || got[1] != "mosin9130" {
		t.Fatalf("unchambered %v", got)
	}
	if _, err := os.Stat(filepath.Join(a.ServerDir, app.TweaksMod, "addons", "dzm_servertweaks.pbo")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.spawnChamber(rec, httptest.NewRequest("GET", "/api/spawngear/chamber", nil))
	var got struct {
		Unchambered []string `json:"unchambered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Unchambered) != 2 {
		t.Fatalf("GET: %s", rec.Body.String())
	}
	post(`{"class":"M4A1","chambered":true}`)
	post(`{"class":"mosin9130","chambered":true}`)
	if cfg := h.app.Cfg(); len(cfg.Unchambered) != 0 || len(cfg.ServerMods) != 0 {
		t.Fatalf("after turning both back on: %v, mods %v", cfg.Unchambered, cfg.ServerMods)
	}
	if _, err := os.Stat(filepath.Join(a.ServerDir, app.TweaksMod)); !os.IsNotExist(err) {
		t.Error("the mod is still there with nothing to do")
	}
}
