// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"

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
	applied := false
	if !h.app.ServerIsRunning() {
		if err := h.app.SyncServerTweaks(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		applied = true
	}
	writeJSON(w, map[string]interface{}{"ok": true, "applied": applied})
}
