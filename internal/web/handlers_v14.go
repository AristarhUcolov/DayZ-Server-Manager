// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.14.0 handlers: player database + killfeed (from .ADM), cfggameplay.json
// editor, and the CPU/RAM/players metrics history that powers the dashboard
// performance charts.
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/players"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// ---------------------------------------------------------------------------
// Player database + killfeed.

var playersStore struct {
	once sync.Once
	s    *players.Store
}

func (h *handlers) playersDB() *players.Store {
	playersStore.once.Do(func() {
		playersStore.s = players.Open(h.app.ManagerDir)
	})
	return playersStore.s
}

func (h *handlers) profilesAbs() string {
	dir := h.app.Cfg().ProfilesDir
	if dir == "" {
		dir = "profiles"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(h.app.ServerDir, dir)
	}
	return dir
}

// playersList ingests any new ADM lines (throttled inside the store), marks
// currently connected players via the RCon cache, and returns the database
// plus the newest killfeed entries.
func (h *handlers) playersList(w http.ResponseWriter, r *http.Request) {
	db := h.playersDB()
	db.Ingest(h.profilesAbs())
	list, kills := db.Snapshot(100)

	online := map[string]bool{}
	if h.app.ServerIsRunning() {
		for _, p := range h.app.RCon.PlayersFresh(10 * time.Second) {
			online[strings.ToLower(p.Name)] = true
		}
	}
	onlineCount := 0
	for i := range list {
		if online[strings.ToLower(list[i].Name)] {
			list[i].Online = true
			onlineCount++
		}
	}
	writeJSON(w, map[string]interface{}{
		"players":  list,
		"killfeed": kills,
		"total":    len(list),
		"online":   onlineCount,
	})
}

// playersIngestLoop keeps the database current even when nobody has the page
// open, so session/playtime math stays accurate.
func (h *handlers) playersIngestLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		h.playersDB().Ingest(h.profilesAbs())
	}
}

// ---------------------------------------------------------------------------
// cfggameplay.json editor.

func (h *handlers) gameplayPath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfggameplay.json"), nil
}

func (h *handlers) gameplay(w http.ResponseWriter, r *http.Request) {
	path, err := h.gameplayPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, map[string]interface{}{"path": path, "exists": false, "content": ""})
			return
		}
		writeJSON(w, map[string]interface{}{"path": path, "exists": true, "content": string(data)})
		return
	}

	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Refuse malformed JSON outright — DayZ silently ignores the whole file
	// on a single syntax error, which is exactly the failure mode this editor
	// exists to prevent.
	var probe interface{}
	if err := json.Unmarshal([]byte(req.Content), &probe); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = util.BackupBeforeWrite(path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The file only takes effect with enableCfgGameplayFile=1 — set it the
	// same way cfgeconomycore auto-registration works, so "I edited the file
	// but nothing changed" can't happen.
	flagSet := false
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	if cfg, err := config.LoadServerCfg(cfgPath); err == nil {
		if v, _ := cfg.Get("enableCfgGameplayFile"); v != "1" {
			cfg.Set("enableCfgGameplayFile", 1)
			if cfg.Save(cfgPath) == nil {
				flagSet = true
			}
		}
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "enabledFlag": flagSet})
}

// ---------------------------------------------------------------------------
// Metrics history — 30s samples of CPU / RAM / player count kept for 24h in
// memory. Powers the dashboard performance charts.

type histSample struct {
	T       int64   `json:"t"` // unix seconds
	CPU     float64 `json:"cpu"`
	Mem     uint64  `json:"mem"`
	Players int     `json:"players"`
	Running bool    `json:"running"`
}

var metricsHist struct {
	mu      sync.Mutex
	samples []histSample
}

const (
	histStep = 30 * time.Second
	histKeep = 2880 // 24h at 30s
)

func (h *handlers) metricsSampler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(histStep):
		}
		s := histSample{T: time.Now().Unix(), Running: h.app.ServerIsRunning()}
		if s.Running {
			if pid := h.app.Server.PID(); pid > 0 {
				if cpu, mem, err := util.ProcessStats(uint32(pid)); err == nil {
					if cores := runtime.NumCPU(); cores > 0 {
						cpu /= float64(cores)
					}
					s.CPU = cpu
					s.Mem = mem
				}
			}
			s.Players = len(h.app.RCon.PlayersFresh(45 * time.Second))
		}
		metricsHist.mu.Lock()
		metricsHist.samples = append(metricsHist.samples, s)
		if len(metricsHist.samples) > histKeep {
			metricsHist.samples = metricsHist.samples[len(metricsHist.samples)-histKeep:]
		}
		metricsHist.mu.Unlock()
	}
}

// metricsHistory returns samples for the requested window (?seconds=3600).
func (h *handlers) metricsHistory(w http.ResponseWriter, r *http.Request) {
	seconds := int64(3600)
	if v := r.URL.Query().Get("seconds"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 && n <= 86400 {
			seconds = n
		}
	}
	cutoff := time.Now().Unix() - seconds
	metricsHist.mu.Lock()
	out := make([]histSample, 0, len(metricsHist.samples))
	for _, s := range metricsHist.samples {
		if s.T >= cutoff {
			out = append(out, s)
		}
	}
	metricsHist.mu.Unlock()
	writeJSON(w, map[string]interface{}{"samples": out, "stepSeconds": int(histStep.Seconds())})
}
