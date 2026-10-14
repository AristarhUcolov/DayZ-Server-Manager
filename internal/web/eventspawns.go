// Copyright (c) 2026 Aristarh Ucolov.
//
// Event spawn-point editor backend. Reads cfgeventspawns.xml (where events
// spawn) and cross-checks it against events.xml (what events exist), so the map
// editor can show, per event, whether it actually has any spawn points — the
// single most common "my event never spawns" mistake.
package web

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"os"
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
	info := map[string]eventSpawnMeta{}
	mission, _ := h.missionTemplate()
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	territories := envTerritoryNames(missionDir)
	// Every events file of the mission — modded events live in files registered
	// in cfgeconomycore.xml and must not show up as orphans.
	for _, f := range missionEcoFiles(missionDir, "events") {
		evDoc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			continue
		}
		for _, e := range evDoc.Events {
			if !definedSet[strings.ToLower(e.Name)] {
				defined = append(defined, e.Name)
				definedSet[strings.ToLower(e.Name)] = true
				info[e.Name] = eventMetaOf(e, territories)
			}
		}
	}
	sort.Strings(defined)

	noPositions := []string{} // spawns from points here, but has none
	for _, name := range defined {
		if !info[name].needsPoints() {
			continue
		}
		e := doc.Find(name)
		if e == nil || len(e.Pos) == 0 {
			noPositions = append(noPositions, name)
		}
	}
	orphans := []string{} // positioned here but not defined in events.xml
	if len(definedSet) > 0 {
		for _, e := range events {
			if len(e.Pos) > 0 && !definedSet[strings.ToLower(e.Name)] {
				orphans = append(orphans, e.Name)
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"map":         h.currentMap(),
		"path":        path,
		"events":      events,
		"defined":     defined,
		"info":        info,
		"noPositions": noPositions,
		"orphans":     orphans,
	})
}

// eventSpawnMeta is what the spawn-point editor needs to know about an event
// from events.xml: whether it spawns at cfgeventspawns.xml points at all.
type eventSpawnMeta struct {
	// Mode: "points" (fixed — the points on this page), "player" (around
	// players), "uniform" (anywhere) or "territory" (animals: env/ files).
	Mode    string `json:"mode"`
	Active  bool   `json:"active"`
	Nominal *int   `json:"nominal,omitempty"`
}

// needsPoints: only an active "points" event that keeps something on the map
// stops spawning without points; nominal 0 spawns nothing either way.
func (m eventSpawnMeta) needsPoints() bool {
	return m.Mode == "points" && m.Active && (m.Nominal == nil || *m.Nominal > 0)
}

func eventMetaOf(e dztypes.Event, territories map[string]bool) eventSpawnMeta {
	m := eventSpawnMeta{Mode: "points", Active: e.Active == nil || *e.Active != 0, Nominal: e.Nominal}
	switch strings.ToLower(strings.TrimSpace(e.Position)) {
	case "player":
		m.Mode = "player"
	case "uniform":
		m.Mode = "uniform"
	default:
		if isTerritoryEvent(e.Name, territories) {
			m.Mode = "territory"
		}
	}
	return m
}

// isTerritoryEvent: animals spawn in the territories of cfgenvironment.xml
// (AnimalCow → territory "Cow", AmbientHen → "AmbientHen"), not at points.
// Animal*/Ambient* count even without the file, as on the Events page.
func isTerritoryEvent(name string, territories map[string]bool) bool {
	n := strings.ToLower(name)
	if territories[n] {
		return true
	}
	for _, p := range []string{"animal", "ambient"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// envTerritoryNames lists the territory names cfgenvironment.xml declares,
// lowercased. A missing or unreadable file gives an empty set.
func envTerritoryNames(missionDir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(missionDir, "cfgenvironment.xml"))
	if err != nil {
		return out
	}
	var env struct {
		Territories struct {
			Territory []struct {
				Name string `xml:"name,attr"`
			} `xml:"territory"`
		} `xml:"territories"`
	}
	if xml.Unmarshal(data, &env) != nil {
		return out
	}
	for _, t := range env.Territories.Territory {
		if n := strings.ToLower(strings.TrimSpace(t.Name)); n != "" {
			out[n] = true
		}
	}
	return out
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
	if os.IsNotExist(err) {
		doc, err = &dztypes.EventSpawnsDoc{}, nil
	}
	if err != nil {
		// Saving into an empty document would replace every other event's
		// points with this one event — refuse until the file is fixed.
		http.Error(w, "cfgeventspawns.xml cannot be read ("+err.Error()+") — fix it first (Validator or Mission DB)", http.StatusConflict)
		return
	}
	count := doc.SetPositions(req.Name, req.Pos)
	_ = util.BackupBeforeWrite(path)
	if err := dztypes.SaveEventSpawns(path, doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"name": req.Name, "count": count})
}
