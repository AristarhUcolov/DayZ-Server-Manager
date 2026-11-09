// Copyright (c) 2026 Aristarh Ucolov.
//
// Contaminated zones. Three things make them, in three files:
//
//   - the static gas zones: cfgeffectarea.json, "Areas" — a centre, a radius,
//     how high and how deep the gas reaches, its particles and the screen
//     tint — and "SafePositions", where a player who logs in inside a zone is
//     moved to;
//   - the dynamic zones: an events.xml event whose child is
//     ContaminatedArea_Dynamic (vanilla calls it StaticContaminatedArea): the
//     economy drops a gas zone at one of its cfgeventspawns.xml points, for
//     the event's lifetime;
//   - the zones' loot: types with the ContaminatedArea usage. Which buildings
//     count as inside a zone is in the map's own files.
//
// cfgeffectarea.json goes to the page as it is and comes back the same way,
// so key order and any field the page does not know survive an edit.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

const zoneUsage = "ContaminatedArea"

type zoneDynamic struct {
	Event     string             `json:"event"`
	File      string             `json:"file"`
	Nominal   *int               `json:"nominal,omitempty"`
	Min       *int               `json:"min,omitempty"`
	Max       *int               `json:"max,omitempty"`
	Lifetime  *int               `json:"lifetime,omitempty"`
	Active    *int               `json:"active,omitempty"`
	Radius    int                `json:"radius,omitempty"` // of each spot, cfgeventspawns <zone r>
	Positions []dztypes.SpawnPos `json:"positions"`
}

type zoneLoot struct {
	Name    string   `json:"name"`
	Nominal *int     `json:"nominal,omitempty"`
	Min     *int     `json:"min,omitempty"`
	File    string   `json:"file"`
	Usages  []string `json:"usages,omitempty"`
	Values  []string `json:"values,omitempty"`
}

func (h *handlers) zonesGet(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	md := dztypes.MissionDir(h.app.ServerDir, mission)
	out := map[string]interface{}{
		"file":    "cfgeffectarea.json",
		"map":     h.currentMap(),
		"running": h.app.ServerIsRunning(),
	}
	if data, err := os.ReadFile(filepath.Join(md, "cfgeffectarea.json")); err == nil {
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		if json.Valid(data) {
			out["doc"] = json.RawMessage(data)
		} else {
			out["invalid"] = true
		}
	}

	// Dynamic zones and their spots.
	spots := map[string]dztypes.SpawnEvent{}
	if sp, err := dztypes.LoadEventSpawns(filepath.Join(md, "cfgeventspawns.xml")); err == nil {
		for _, e := range sp.Events {
			spots[strings.ToLower(e.Name)] = e
		}
	}
	dyn := []zoneDynamic{}
	seen := map[string]bool{}
	for _, f := range missionEcoFiles(md, "events") {
		doc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			continue
		}
		for _, e := range doc.Events {
			if seen[strings.ToLower(e.Name)] || !isZoneEvent(e) {
				continue
			}
			seen[strings.ToLower(e.Name)] = true
			z := zoneDynamic{Event: e.Name, File: f.Rel, Nominal: e.Nominal, Min: e.Min, Max: e.Max,
				Lifetime: e.Lifetime, Active: e.Active, Positions: []dztypes.SpawnPos{}}
			if s, ok := spots[strings.ToLower(e.Name)]; ok {
				z.Positions = s.Pos
				if s.Zone != nil && s.Zone.R != nil {
					z.Radius = *s.Zone.R
				}
			}
			dyn = append(dyn, z)
		}
	}
	out["dynamic"] = dyn

	// The zones' loot, from every types file of the mission.
	loot := []zoneLoot{}
	for _, f := range missionEcoFiles(md, "types") {
		doc, err := dztypes.Load(f.Path)
		if err != nil {
			continue
		}
		for _, t := range doc.Types {
			if !hasRef(t.Usages, zoneUsage) {
				continue
			}
			loot = append(loot, zoneLoot{Name: t.Name, Nominal: t.Nominal, Min: t.Min, File: f.Rel,
				Usages: refNames(t.Usages), Values: refNames(t.Values)})
		}
	}
	out["loot"] = loot
	if data, err := os.ReadFile(filepath.Join(md, "cfglimitsdefinition.xml")); err == nil {
		out["usage"] = bytes.Contains(data, []byte(`"`+zoneUsage+`"`))
	}
	writeJSON(w, out)
}

// isZoneEvent: the event spawns ContaminatedArea_Dynamic (or is named so).
func isZoneEvent(e dztypes.Event) bool {
	if strings.Contains(strings.ToLower(e.Name), "contaminatedarea") {
		return true
	}
	if e.Children != nil {
		for _, c := range e.Children.Child {
			if strings.HasPrefix(strings.ToLower(c.Type), "contaminatedarea") {
				return true
			}
		}
	}
	return false
}

func hasRef(refs []dztypes.NamedRef, name string) bool {
	for _, r := range refs {
		if strings.EqualFold(r.Name, name) {
			return true
		}
	}
	return false
}

func refNames(refs []dztypes.NamedRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}

// zonesSave writes cfgeffectarea.json as the page sends it — checked first:
// every area named, a centre of three finite numbers and a radius above
// zero, every safe position a pair.
func (h *handlers) zonesSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Doc json.RawMessage `json:"doc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Doc) == 0 {
		http.Error(w, "doc required", http.StatusBadRequest)
		return
	}
	if err := checkEffectAreas(req.Doc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeffectarea.json")
	var buf bytes.Buffer
	if err := json.Indent(&buf, req.Doc, "", "    "); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	buf.WriteByte('\n')
	if err := util.BackupBeforeWrite(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved"})
}

func checkEffectAreas(doc json.RawMessage) error {
	var d struct {
		Areas []struct {
			AreaName string `json:"AreaName"`
			Type     string `json:"Type"`
			Data     struct {
				Pos    []float64 `json:"Pos"`
				Radius float64   `json:"Radius"`
			} `json:"Data"`
		} `json:"Areas"`
		SafePositions [][]float64 `json:"SafePositions"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return fmt.Errorf("cfgeffectarea.json: %w", err)
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	names := map[string]bool{}
	for i, a := range d.Areas {
		name := strings.TrimSpace(a.AreaName)
		if name == "" {
			return fmt.Errorf("zone %d has no name", i+1)
		}
		if names[strings.ToLower(name)] {
			return fmt.Errorf("two zones are called %q", name)
		}
		names[strings.ToLower(name)] = true
		if len(a.Data.Pos) != 3 || !finite(a.Data.Pos[0]) || !finite(a.Data.Pos[1]) || !finite(a.Data.Pos[2]) {
			return fmt.Errorf("zone %q needs a position of three numbers", name)
		}
		if !(a.Data.Radius > 0) || !finite(a.Data.Radius) {
			return fmt.Errorf("zone %q needs a radius above 0", name)
		}
	}
	for i, p := range d.SafePositions {
		if len(p) != 2 || !finite(p[0]) || !finite(p[1]) {
			return fmt.Errorf("safe position %d needs two numbers (x, z)", i+1)
		}
	}
	return nil
}

// zonesLoot edits the zones' loot: {"rows": [{name, nominal, min}]} sets how
// many, {"add": [names]} / {"remove": [names]} give or take the
// ContaminatedArea usage — in whichever types file holds each item.
func (h *handlers) zonesLoot(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Rows []struct {
			Name    string `json:"name"`
			Nominal *int   `json:"nominal"`
			Min     *int   `json:"min"`
		} `json:"rows"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, row := range req.Rows {
		if (row.Nominal != nil && *row.Nominal < 0) || (row.Min != nil && *row.Min < 0) {
			http.Error(w, "counts cannot be negative", http.StatusBadRequest)
			return
		}
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	md := dztypes.MissionDir(h.app.ServerDir, mission)
	pending := map[string]bool{}
	for _, n := range req.Add {
		pending[strings.ToLower(strings.TrimSpace(n))] = true
	}
	var notFound []string
	touched := 0
	for _, f := range missionEcoFiles(md, "types") {
		doc, err := dztypes.Load(f.Path)
		if err != nil {
			continue
		}
		changed := 0
		for _, row := range req.Rows {
			changed += doc.BulkPatch([]string{row.Name}, dztypes.BulkFieldPatch{Nominal: row.Nominal, Min: row.Min})
		}
		for _, n := range req.Add {
			if pending[strings.ToLower(strings.TrimSpace(n))] && doc.SetUsage(strings.TrimSpace(n), zoneUsage, true) {
				delete(pending, strings.ToLower(strings.TrimSpace(n)))
				changed++
			}
		}
		for _, n := range req.Remove {
			if doc.SetUsage(strings.TrimSpace(n), zoneUsage, false) {
				changed++
			}
		}
		if changed == 0 {
			continue
		}
		if err := util.BackupBeforeWrite(f.Path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := dztypes.SaveTypes(f.Path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		touched += changed
	}
	for _, n := range req.Add {
		if pending[strings.ToLower(strings.TrimSpace(n))] {
			notFound = append(notFound, strings.TrimSpace(n))
		}
	}
	writeJSON(w, map[string]interface{}{"touched": touched, "notFound": notFound})
}
