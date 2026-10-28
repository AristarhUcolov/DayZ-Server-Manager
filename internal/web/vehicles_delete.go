// Copyright (c) 2026 Aristarh Ucolov.
//
// Deleting a modded vehicle. Switching a vehicle off keeps its tuning; delete
// is for a vehicle the admin does not want at all. It removes the event, its
// spawn points, and the part lists and <type> entries of its variants that the
// panel itself added (dzm_vehicles) and no other vehicle uses — a mod's own
// files and the vanilla files are never edited beyond the event. The deletion
// is remembered, so the automatic import does not bring the vehicle back with
// the next mod update; adding it again from the mods list clears that.
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

func (h *handlers) vehiclesDelete(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Event string `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Event) == "" {
		http.Error(w, "event required", http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	res, err := deleteModVehicle(missionDir, req.Event)
	if err != nil {
		if msg := err.Error(); strings.HasPrefix(msg, "vehicles.") {
			authError(w, res.status, msg, nil) // an i18n key the panel translates
			return
		}
		http.Error(w, err.Error(), res.status)
		return
	}
	h.app.Log.Printf("vehicle %s deleted from %s (%d spawn point(s), %d part list(s), %d type(s))",
		res.Event, res.File, res.Positions, res.Parts, res.Types)
	writeJSON(w, res)
}

// vehicleDeleted is what deleteModVehicle removed.
type vehicleDeleted struct {
	Event     string `json:"event"`
	File      string `json:"file"`
	Positions int    `json:"positions"`
	Parts     int    `json:"parts"`
	Types     int    `json:"types"`
	status    int
}

type httpError string

func (e httpError) Error() string { return string(e) }

func deleteModVehicle(missionDir, name string) (vehicleDeleted, error) {
	out := vehicleDeleted{status: http.StatusInternalServerError}
	files := missionEcoFiles(missionDir, "events")
	var at *ecoFile
	var ev *dztypes.Event
	for i := range files {
		doc, err := dztypes.LoadEvents(files[i].Path)
		if err != nil {
			continue
		}
		if e := doc.Find(name); e != nil {
			at, ev = &files[i], e
			break
		}
	}
	if ev == nil {
		out.status = http.StatusNotFound
		return out, httpError("no event named " + name)
	}
	if at.Rel == files[0].Rel {
		// db/events.xml: the game's own vehicles are switched off, not erased.
		out.status = http.StatusBadRequest
		return out, httpError("vehicles.delete.vanilla")
	}
	out.Event, out.File = ev.Name, at.Rel
	if _, err := dztypes.DeleteEventBlock(at.Path, ev.Name); err != nil {
		return out, err
	}

	// Its spawn points.
	spPath := filepath.Join(missionDir, "cfgeventspawns.xml")
	if doc, err := dztypes.LoadEventSpawns(spPath); err == nil {
		kept := doc.Events[:0]
		for _, e := range doc.Events {
			if strings.EqualFold(e.Name, ev.Name) {
				out.Positions += len(e.Pos)
				continue
			}
			kept = append(kept, e)
		}
		if out.Positions > 0 || len(kept) != len(doc.Events) {
			doc.Events = kept
			_ = util.BackupBeforeWrite(spPath)
			if err := dztypes.SaveEventSpawns(spPath, doc); err != nil {
				return out, err
			}
		}
	}

	// Variants no other vehicle uses: their part lists and types, from the
	// files the panel wrote.
	inUse := map[string]bool{}
	for _, f := range files {
		doc, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			continue
		}
		for _, e := range doc.Events {
			if e.Children != nil {
				for _, c := range e.Children.Child {
					inUse[strings.ToLower(c.Type)] = true
				}
			}
		}
	}
	drop := map[string]bool{}
	if ev.Children != nil {
		for _, c := range ev.Children.Child {
			if k := strings.ToLower(c.Type); !inUse[k] {
				drop[k] = true
			}
		}
	}
	if len(drop) > 0 {
		dzm := filepath.Join(missionDir, dzmFolder)
		if sp := filepath.Join(dzm, "spawnabletypes.xml"); fileExists(sp) {
			for k := range drop {
				if n, err := dztypes.DeleteSpawnableType(sp, matchCase(sp, k)); err == nil {
					out.Parts += n
				}
			}
		}
		if tp := filepath.Join(dzm, "types.xml"); fileExists(tp) {
			if removed, err := dztypes.RemoveTypesInSet(tp, drop); err == nil {
				out.Types = len(removed)
			}
		}
	}

	// Remember it, so the next mod update does not add it again.
	src := loadVehicleSources(missionDir)
	mod := src.Events[ev.Name]
	for k, m := range src.Events {
		if strings.EqualFold(k, ev.Name) {
			mod = m
			delete(src.Events, k)
		}
	}
	if src.Removed == nil {
		src.Removed = map[string]string{}
	}
	src.Removed[ev.Name] = mod
	saveVehicleSources(missionDir, src)
	out.status = http.StatusOK
	return out, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// matchCase returns the class name as the file spells it (DeleteSpawnableType
// matches the exact name), falling back to the lower-cased key.
func matchCase(path, lower string) string {
	list, err := dztypes.LoadSpawnable(path)
	if err != nil {
		return lower
	}
	for _, t := range list {
		if strings.EqualFold(t.Name, lower) {
			return t.Name
		}
	}
	return lower
}
