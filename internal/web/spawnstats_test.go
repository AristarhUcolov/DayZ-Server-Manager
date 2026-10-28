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
