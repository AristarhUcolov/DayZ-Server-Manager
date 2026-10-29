// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
)

// spawnStats is the Player loadout page's "fresh character" card: how fed,
// watered and warm a new character starts (config.SpawnStats, each value a
// share of the stat's maximum, null = the game's own). The server tweaks mod
// applies it, like the vehicles' fuel: written at once when the server is
// stopped, otherwise before its next start.
func (h *handlers) spawnStats(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		st := h.app.Cfg().SpawnStats
		if st == nil {
			st = &config.SpawnStats{}
		}
		writeJSON(w, map[string]interface{}{"stats": st, "tweaksMod": app.TweaksMod})
		return
	}
	var req config.SpawnStats
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, v := range []*float64{req.Food, req.Water, req.Warmth} {
		if v != nil && (*v < 0 || *v > 1) {
			http.Error(w, "a value runs from 0 to 1", http.StatusBadRequest)
			return
		}
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		c.SpawnStats = nil
		if req.Active() {
			st := req
			c.SpawnStats = &st
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	applied, err := h.applyTweaks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "applied": applied})
}

// applyTweaks rewrites the server tweaks mod now when the server is stopped
// (it holds the archive open while it runs; the next start writes it then).
func (h *handlers) applyTweaks() (bool, error) {
	if h.app.ServerIsRunning() {
		return false, nil
	}
	return true, h.app.SyncServerTweaks()
}

// spawnChamber is the loadout's "round in the chamber" switch: the weapon
// classes a fresh character's spawn gear loads without one
// (config.Unchambered). GET lists them; POST {"class", "chambered"} sets one
// class. Like the stats, the server tweaks mod applies it.
func (h *handlers) spawnChamber(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]interface{}{"unchambered": orEmpty(app.ChamberClasses(h.app.Cfg().Unchambered))})
		return
	}
	var req struct {
		Class     string `json:"class"`
		Chambered bool   `json:"chambered"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cls := strings.ToLower(strings.TrimSpace(req.Class))
	if cls == "" || len(cls) > 128 || strings.ContainsAny(cls, "\"\\\r\n<>&") {
		http.Error(w, "invalid weapon class", http.StatusBadRequest)
		return
	}
	var list []string
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		kept := []string{}
		for _, x := range app.ChamberClasses(c.Unchambered) {
			if x != cls {
				kept = append(kept, x)
			}
		}
		if !req.Chambered {
			kept = append(kept, cls)
		}
		c.Unchambered = app.ChamberClasses(kept)
		list = c.Unchambered
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	applied, err := h.applyTweaks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "applied": applied, "unchambered": orEmpty(list)})
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
