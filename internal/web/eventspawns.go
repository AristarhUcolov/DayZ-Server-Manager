// Copyright (c) 2026 Aristarh Ucolov.
//
// Event spawn-point editor backend. Reads cfgeventspawns.xml (where events
// spawn) and cross-checks it against events.xml (what events exist), so the map
// editor can show, per event, whether it actually has any spawn points — the
// single most common "my event never spawns" mistake.
package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

func (h *handlers) eventSpawnsPath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeventspawns.xml"), nil
}

// eventSpawnsList returns every event's spawn positions plus a cross-check
// against events.xml (which events are defined, which have no positions here,
// and which positions belong to no defined event).
func (h *handlers) eventSpawnsList(w http.ResponseWriter, r *http.Request) {
	path, err := h.eventSpawnsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := dztypes.LoadEventSpawns(path)
	if err != nil {
		// Missing/broken file — treat as empty so the editor still opens.
		doc = &dztypes.EventSpawnsDoc{}
	}

	type outEvent struct {
		Name    string             `json:"name"`
		HasZone bool               `json:"hasZone"`
		Pos     []dztypes.SpawnPos `json:"pos"`
	}
	spawnNames := map[string]bool{}
	events := make([]outEvent, 0, len(doc.Events))
	for i := range doc.Events {
		e := &doc.Events[i]
		spawnNames[strings.ToLower(e.Name)] = true
		events = append(events, outEvent{Name: e.Name, HasZone: e.Zone != nil, Pos: e.Pos})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Name < events[j].Name })

	// Cross-check against events.xml.
	defined := []string{}
	definedSet := map[string]bool{}
	mission, _ := h.missionTemplate()
	evPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "events.xml")
	if evDoc, err := dztypes.LoadEvents(evPath); err == nil {
		for _, e := range evDoc.Events {
			defined = append(defined, e.Name)
			definedSet[strings.ToLower(e.Name)] = true
		}
		sort.Strings(defined)
	}

	noPositions := []string{} // defined in events.xml but absent/empty here
	for _, name := range defined {
		e := doc.Find(name)
		if e == nil || len(e.Pos) == 0 {
			noPositions = append(noPositions, name)
		}
	}
	orphans := []string{} // positioned here but not defined in events.xml
	if len(definedSet) > 0 {
		for _, e := range events {
			if !definedSet[strings.ToLower(e.Name)] {
				orphans = append(orphans, e.Name)
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"map":         h.currentMap(),
		"path":        path,
		"events":      events,
		"defined":     defined,
		"noPositions": noPositions,
		"orphans":     orphans,
	})
}

// eventSpawnsSave replaces one event's position list and writes the file.
// Requires the server stopped, and backs up the current file first.
func (h *handlers) eventSpawnsSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name string             `json:"name"`
		Pos  []dztypes.SpawnPos `json:"pos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "event name required", http.StatusBadRequest)
		return
	}
	path, err := h.eventSpawnsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := dztypes.LoadEventSpawns(path)
	if err != nil {
		doc = &dztypes.EventSpawnsDoc{}
	}
	count := doc.SetPositions(req.Name, req.Pos)
	_ = util.BackupBeforeWrite(path)
	if err := dztypes.SaveEventSpawns(path, doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"name": req.Name, "count": count})
}
