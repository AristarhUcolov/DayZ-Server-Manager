// Copyright (c) 2026 Aristarh Ucolov.
//
// Map data endpoints: which world the active mission runs, and aggregated
// death/kill positions for the heatmap. Positions come from the .ADM log the
// player database already ingests, so nothing new needs enabling on the server.
package web

import (
	"net/http"
	"strings"
	"time"

	"dayzmanager/internal/players"
)

type mapInfo struct {
	Key  string  `json:"key"`  // "chernarus" | "livonia" | "sakhal" | "unknown"
	Name string  `json:"name"` // display name
	Size float64 `json:"size"` // world edge length in metres (maps are square)
}

// worldFromTemplate resolves the DayZ world of the active mission from its
// template folder name (e.g. "dayzOffline.chernarusplus" → Chernarus). Custom
// missions keep the world name after the dot, so a substring match is enough.
//
// Sizes: Chernarus 15360 and Livonia 12800 are the long-known engine values;
// Sakhal (Frostline) uses 16384 here — adjust in one place if BI's figure
// differs, the heatmap plots positions as a fraction of Size either way.
func worldFromTemplate(template string) mapInfo {
	t := strings.ToLower(template)
	switch {
	case strings.Contains(t, "enoch"), strings.Contains(t, "livonia"):
		return mapInfo{Key: "livonia", Name: "Livonia", Size: 12800}
	case strings.Contains(t, "sakhal"):
		return mapInfo{Key: "sakhal", Name: "Sakhal", Size: 16384}
	case strings.Contains(t, "chernarus"):
		return mapInfo{Key: "chernarus", Name: "Chernarus", Size: 15360}
	default:
		// Unknown/modded world: still plot on a generic square. 15360 is the
		// most common edge length, so points land in a sensible range.
		return mapInfo{Key: "unknown", Name: template, Size: 15360}
	}
}

func (h *handlers) currentMap() mapInfo {
	tpl, _ := h.missionTemplate()
	return worldFromTemplate(tpl)
}

type heatPoint struct {
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	Kind string  `json:"kind"` // pvp | env | suicide
}

// mapHeat returns the map metadata and every death/kill position the killfeed
// still holds. The heatmap is a picture of recent activity (the killfeed is
// capped), which is exactly what "where do fights happen" wants.
func (h *handlers) mapHeat(w http.ResponseWriter, r *http.Request) {
	db := h.playersDB()
	db.Ingest(h.profilesAbs())
	_, kills := db.Snapshot(0)

	points := make([]heatPoint, 0, len(kills))
	var pvp, env, suicide int
	for _, k := range kills {
		if len(k.Pos) < 2 {
			continue
		}
		kind := k.Kind
		if kind == "" {
			if k.Suicide {
				kind = "suicide"
			} else if k.Killer != "" {
				kind = "pvp"
			} else {
				kind = "env"
			}
		}
		switch kind {
		case "pvp":
			pvp++
		case "suicide":
			suicide++
		default:
			env++
		}
		points = append(points, heatPoint{X: k.Pos[0], Z: k.Pos[1], Kind: kind})
	}

	writeJSON(w, map[string]interface{}{
		"map":    h.currentMap(),
		"points": points,
		"total":  len(points),
		"counts": map[string]int{"pvp": pvp, "env": env, "suicide": suicide},
	})
}

// mapLive returns the last-known position of every currently-connected player.
// Positions come from the .ADM log, which vanilla DayZ writes only on connect,
// combat and chat — so a position can be stale (surfaced as its age). Players
// who are online but haven't produced a positioned line yet simply aren't
// plotted, which is why onlineCount can exceed the number of points.
func (h *handlers) mapLive(w http.ResponseWriter, r *http.Request) {
	db := h.playersDB()
	db.Ingest(h.profilesAbs())

	running := h.app.ServerIsRunning()
	online := map[string]bool{}
	if running {
		for _, p := range h.app.RCon.PlayersFresh(10 * time.Second) {
			online[strings.ToLower(p.Name)] = true
		}
	}

	plotted := []players.LivePos{}
	for _, lp := range db.LivePositions() {
		if online[strings.ToLower(lp.Name)] {
			plotted = append(plotted, lp)
		}
	}

	writeJSON(w, map[string]interface{}{
		"map":         h.currentMap(),
		"players":     plotted,
		"running":     running,
		"onlineCount": len(online),
	})
}
