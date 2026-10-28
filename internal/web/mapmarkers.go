// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Admin markers: notes pinned on the map — a clan's base, a trader, a spot a
// player reported — shared by every admin of this server and drawn on every
// map page. Kept per world in map-markers.json in the manager's folder.

type mapMarker struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Z     float64 `json:"z"`
	Label string  `json:"label"`
	Note  string  `json:"note,omitempty"`
	Color string  `json:"color"`
	At    string  `json:"at"`           // RFC3339, the last change
	By    string  `json:"by,omitempty"` // the panel user who made it, with panel login on
}

var markerColors = map[string]bool{"red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true}

const (
	maxMarkers     = 500
	maxMarkerLabel = 60
	maxMarkerNote  = 500
)

func (h *handlers) markersFile() string { return filepath.Join(h.app.ManagerDir, "map-markers.json") }

// loadMarkers reads every world's markers. A missing or broken file is an
// empty set.
func (h *handlers) loadMarkers() map[string][]mapMarker {
	out := map[string][]mapMarker{}
	if data, err := os.ReadFile(h.markersFile()); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

// mapMarkers returns the current world's markers.
func (h *handlers) mapMarkers(w http.ResponseWriter, r *http.Request) {
	key := h.currentMap().Key
	h.markersMu.Lock()
	list := h.loadMarkers()[key]
	h.markersMu.Unlock()
	if list == nil {
		list = []mapMarker{}
	}
	writeJSON(w, map[string]interface{}{"map": key, "markers": list})
}

// mapMarkersEdit adds, changes or deletes one marker of the current world:
// {"op": "add" | "update" | "delete", "marker": {…}}. It answers with the
// world's markers after the change.
func (h *handlers) mapMarkersEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op     string    `json:"op"`
		Marker mapMarker `json:"marker"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m := req.Marker
	if req.Op != "delete" {
		m.Label = strings.TrimSpace(m.Label)
		m.Note = strings.TrimSpace(m.Note)
		if m.Label == "" {
			authError(w, http.StatusBadRequest, "map.markers.needLabel", nil)
			return
		}
		m.Label = clipRunes(m.Label, maxMarkerLabel)
		m.Note = clipRunes(m.Note, maxMarkerNote)
		if !markerColors[m.Color] {
			m.Color = "red"
		}
		if math.IsNaN(m.X) || math.IsNaN(m.Z) || math.IsInf(m.X, 0) || math.IsInf(m.Z, 0) {
			http.Error(w, "bad position", http.StatusBadRequest)
			return
		}
		m.X = math.Round(m.X*100) / 100
		m.Z = math.Round(m.Z*100) / 100
		m.At = time.Now().UTC().Format(time.RFC3339)
		m.By = ""
		if h.auth != nil {
			if ss := h.auth.sessionFor(r); ss != nil {
				m.By = ss.User
			}
		}
	}

	key := h.currentMap().Key
	h.markersMu.Lock()
	defer h.markersMu.Unlock()
	all := h.loadMarkers()
	list := all[key]
	idx := -1
	for i := range list {
		if list[i].ID == m.ID {
			idx = i
			break
		}
	}
	switch req.Op {
	case "add":
		if len(list) >= maxMarkers {
			authError(w, http.StatusBadRequest, "map.markers.tooMany", nil)
			return
		}
		m.ID = newMarkerID()
		list = append(list, m)
	case "update":
		if idx < 0 {
			authError(w, http.StatusNotFound, "map.markers.gone", nil)
			return
		}
		list[idx] = m
	case "delete":
		if idx >= 0 {
			list = append(list[:idx], list[idx+1:]...)
		}
	default:
		http.Error(w, "unknown op", http.StatusBadRequest)
		return
	}
	if len(list) == 0 {
		delete(all, key)
	} else {
		all[key] = list
	}
	data, err := json.MarshalIndent(all, "", " ")
	if err == nil {
		err = writeFileAtomic(h.markersFile(), data)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []mapMarker{}
	}
	writeJSON(w, map[string]interface{}{"map": key, "markers": list, "id": m.ID})
}

func newMarkerID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// clipRunes cuts s to at most n characters (not bytes).
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
