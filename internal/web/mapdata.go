// Copyright (c) 2026 Aristarh Ucolov.
//
// Map data endpoints: which world the active mission runs, and aggregated
// death/kill positions for the heatmap. Positions come from the .ADM log the
// player database already ingests, so nothing new needs enabling on the server.
package web

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dayzmanager/internal/players"
)

type mapInfo struct {
	Key      string  `json:"key"`                // "chernarus" | "livonia" | "sakhal" | "unknown"
	Name     string  `json:"name"`               // display name
	Size     float64 `json:"size"`               // world edge length in metres (maps are square)
	HasImage bool    `json:"hasImage"`           // an admin-supplied background image exists
	ImageVer int64   `json:"imageVer,omitempty"` // its mtime, used to bust the browser cache
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
	mi := worldFromTemplate(tpl)
	if p := h.mapImageFile(mi.Key); p != "" {
		mi.HasImage = true
		if st, err := os.Stat(p); err == nil {
			mi.ImageVer = st.ModTime().Unix()
		}
	}
	return mi
}

// mapImageExts are the picture formats accepted for a custom map background.
var mapImageExts = []string{".webp", ".png", ".jpg", ".jpeg"}

func (h *handlers) mapImageDir() string {
	return filepath.Join(h.app.ManagerDir, "maps")
}

// mapImageFile returns the stored background image for a map key, or "" when
// none has been uploaded. Base() guards against a key with path separators.
func (h *handlers) mapImageFile(key string) string {
	key = filepath.Base(key)
	if key == "" || key == "." {
		return ""
	}
	for _, ext := range mapImageExts {
		p := filepath.Join(h.mapImageDir(), key+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (h *handlers) mapImageKey(r *http.Request) string {
	if k := strings.TrimSpace(r.URL.Query().Get("map")); k != "" {
		return filepath.Base(k)
	}
	return h.currentMap().Key
}

// mapImage serves (GET) or stores (POST) the admin-supplied background image for
// a map. The image is a top-down picture covering the whole world square; the
// interactive map stretches it to the world bounds so positions line up.
func (h *handlers) mapImage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.mapImageUpload(w, r)
		return
	}
	p := h.mapImageFile(h.mapImageKey(r))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	// Long cache — the URL carries an mtime version, so a new upload busts it.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, p)
}

const maxMapImageBytes = 25 << 20 // 25 MB — a full-map export is a few MB

func (h *handlers) mapImageUpload(w http.ResponseWriter, r *http.Request) {
	key := h.mapImageKey(r)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", http.StatusBadRequest)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !contains(mapImageExts, ext) {
		http.Error(w, "image must be .webp, .png, .jpg or .jpeg", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMapImageBytes+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) > maxMapImageBytes {
		http.Error(w, "image too large (max 25 MB)", http.StatusRequestEntityTooLarge)
		return
	}
	if err := os.MkdirAll(h.mapImageDir(), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Replace any existing image for this key (possibly a different extension).
	for _, e := range mapImageExts {
		_ = os.Remove(filepath.Join(h.mapImageDir(), key+e))
	}
	if err := os.WriteFile(filepath.Join(h.mapImageDir(), key+ext), data, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "map": key})
}

// mapImageDelete removes a map's custom background image.
func (h *handlers) mapImageDelete(w http.ResponseWriter, r *http.Request) {
	key := h.mapImageKey(r)
	for _, e := range mapImageExts {
		_ = os.Remove(filepath.Join(h.mapImageDir(), key+e))
	}
	writeJSON(w, map[string]string{"status": "deleted"})
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
