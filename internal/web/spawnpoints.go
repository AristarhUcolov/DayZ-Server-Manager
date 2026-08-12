// Copyright (c) 2026 Aristarh Ucolov.
//
// Player spawn-point editor backend. Reads/writes cfgplayerspawnpoints.xml —
// where fresh, server-hop and travel players appear — as a whole document so
// the map editor can move points, rename groups and tune the spawn/generator/
// group params, all round-tripped. Requires the server stopped to save.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

func (h *handlers) spawnPointsPath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgplayerspawnpoints.xml"), nil
}

// spawnPoints dispatches GET (load) / POST (save) on the same path.
func (h *handlers) spawnPoints(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.spawnPointsSave(w, r)
		return
	}
	path, err := h.spawnPointsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, lerr := dztypes.LoadPlayerSpawns(path)
	exists := lerr == nil
	if !exists {
		doc = &dztypes.PlayerSpawnDoc{}
	}
	writeJSON(w, map[string]interface{}{
		"map":    h.currentMap(),
		"path":   path,
		"exists": exists,
		"doc":    doc,
	})
}

func (h *handlers) spawnPointsSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var doc dztypes.PlayerSpawnDoc
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path, err := h.spawnPointsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := writeFileAtomic(path, dztypes.MarshalPlayerSpawns(&doc)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	groups := len(doc.Fresh.Groups) + len(doc.Hop.Groups) + len(doc.Travel.Groups)
	writeJSON(w, map[string]interface{}{"status": "saved", "groups": groups})
}
