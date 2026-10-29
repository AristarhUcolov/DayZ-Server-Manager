// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// spawnableEntry is one class's own cfgspawnabletypes entry for the editors
// outside the Attachments page — a weapon in a car's trunk, an optic's
// battery. DayZ applies an item's own entry wherever the economy spawns it.
// GET ?name= answers the entry and the file that holds it: the main file or a
// mod's registered spawnabletypes file (the entry is null when there is none
// yet). POST {name, attachments, cargo} writes those groups into that file —
// cfgspawnabletypes.xml for a class without an entry — keeping the rest of
// the entry (hoarder, damage, tags). Backup first; server stopped.
func (h *handlers) spawnableEntry(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	mainPath := filepath.Join(missionDir, "cfgspawnabletypes.xml")
	rel := func(p string) string {
		if r, err := filepath.Rel(missionDir, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	if r.Method == http.MethodGet {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		out := map[string]interface{}{"name": name, "file": rel(mainPath), "entry": nil}
		if e, ok := loadMissionSpawnable(missionDir)[strings.ToLower(name)]; ok {
			out["entry"], out["file"] = e.st, rel(e.path)
		}
		writeJSON(w, out)
		return
	}

	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name        string               `json:"name"`
		Attachments []dztypes.SpawnGroup `json:"attachments"`
		Cargo       []dztypes.SpawnGroup `json:"cargo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || strings.ContainsAny(name, "<>\"&\r\n") {
		http.Error(w, "a class name is required", http.StatusBadRequest)
		return
	}
	att, err := cleanSpawnGroups(req.Attachments)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cargo, err := cleanSpawnGroups(req.Cargo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, path := &dztypes.SpawnableType{Name: name}, mainPath
	if e, ok := loadMissionSpawnable(missionDir)[strings.ToLower(name)]; ok {
		cp := *e.st
		st, path = &cp, e.path
	}
	st.Attachments, st.Cargo = att, cargo
	if _, err := os.Stat(path); err != nil {
		http.Error(w, rel(path)+" not found in the active mission", http.StatusBadRequest)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := dztypes.SaveSpawnableType(path, st); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "file": rel(path)})
}
