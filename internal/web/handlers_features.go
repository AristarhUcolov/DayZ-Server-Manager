// Copyright (c) 2026 Aristarh Ucolov.
//
// Release-3 feature handlers: weather + in-game time (item 16), server wipe
// (item 17), and LAN network-address discovery (item 9). Kept in their own
// file so the core handlers stay readable.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dayzmanager/internal/config"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
	"dayzmanager/internal/weather"
)

// ---------------------------------------------------------------------------
// Weather + time (item 16).
//
// Weather lives in the mission's cfgweather.xml; in-game time scale lives in
// serverDZ.cfg (serverTimeAcceleration / serverNightTimeAcceleration /
// serverTime / serverTimePersistent). Both are read by DayZ at mission load,
// so writes require the server to be stopped (acquireWrite enforces it).
//
// Note for the UI: DayZ has no vanilla way to schedule "weather X at time Y" —
// that needs a mod/script driving RCon. We expose presets, a manual editor,
// and the time scale instead.

func (h *handlers) weatherGet(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgweather.xml")
	params, perr := weather.Parse(path)

	resp := map[string]interface{}{
		"params":  params,
		"presets": weather.Presets(),
		"matched": weather.MatchPreset(params),
		"exists":  perr == nil,
		"mission": mission,
	}

	if cfg, err := config.LoadServerCfg(filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)); err == nil {
		get := func(k, def string) string {
			if v, ok := cfg.Get(k); ok {
				return v
			}
			return def
		}
		resp["time"] = map[string]interface{}{
			"serverTimeAcceleration":      get("serverTimeAcceleration", "1"),
			"serverNightTimeAcceleration": get("serverNightTimeAcceleration", "1"),
			"serverTime":                  get("serverTime", "SystemTime"),
			"serverTimePersistent":        get("serverTimePersistent", "0"),
		}
	}
	writeJSON(w, resp)
}

func (h *handlers) weatherPreset(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	var req struct {
		Name string `json:"name"`
		// Optional: keep the smoothness the user picked instead of resetting it
		// to the preset's default on every preset click.
		Transition string `json:"transition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var p weather.Params
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if name == "off" {
		// Hand control back to vanilla: disable the file (enable="0").
		p = weather.Params{Enable: false}
	} else {
		var found bool
		if p, found = weather.Preset(name); !found {
			http.Error(w, "unknown preset: "+req.Name, http.StatusBadRequest)
			return
		}
	}
	if strings.TrimSpace(req.Transition) != "" {
		p.Transition = req.Transition
	}
	if err := h.writeWeather(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "params": p, "matched": name})
}

func (h *handlers) weatherCustom(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	var p weather.Params
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Applying custom weather implies enabling the file — otherwise DayZ would
	// silently ignore it and the user would think nothing happened.
	p.Enable = true
	if err := h.writeWeather(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "params": p, "matched": weather.MatchPreset(p)})
}

func (h *handlers) writeWeather(p weather.Params) error {
	mission, err := h.missionTemplate()
	if err != nil {
		return err
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgweather.xml")
	return weather.Write(path, p)
}

func (h *handlers) weatherTime(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	var req struct {
		ServerTimeAcceleration      *float64 `json:"serverTimeAcceleration"`
		ServerNightTimeAcceleration *float64 `json:"serverNightTimeAcceleration"`
		ServerTime                  *string  `json:"serverTime"`
		ServerTimePersistent        *int     `json:"serverTimePersistent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfgPath := filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clampF := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	if req.ServerTimeAcceleration != nil {
		cfg.Set("serverTimeAcceleration", clampF(*req.ServerTimeAcceleration, 0.1, 24))
	}
	if req.ServerNightTimeAcceleration != nil {
		cfg.Set("serverNightTimeAcceleration", clampF(*req.ServerNightTimeAcceleration, 0.1, 64))
	}
	if req.ServerTime != nil {
		cfg.Set("serverTime", strings.TrimSpace(*req.ServerTime))
	}
	if req.ServerTimePersistent != nil {
		v := 0
		if *req.ServerTimePersistent != 0 {
			v = 1
		}
		cfg.Set("serverTimePersistent", v)
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Server wipe (item 17).
//
// A "wipe" clears the central-economy persistence: the mission's storage_<N>
// folder(s), where DayZ saves players, vehicles, base building and territory
// state. The map, loot tables (types.xml), economy and other config are NOT
// touched. We move the storage folders into .dayz-manager/wipes/<timestamp>/
// (instant, same-volume rename = a free backup) rather than deleting, so a
// mistaken wipe is fully recoverable. Server must be stopped.

type wipeFolder struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
}

func (h *handlers) wipeStorageFolders(missionDir string) []string {
	var names []string
	entries, _ := os.ReadDir(missionDir)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "storage_") {
			names = append(names, e.Name())
		}
	}
	return names
}

func (h *handlers) wipePreview(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)

	folders := []wipeFolder{}
	var total int64
	for _, name := range h.wipeStorageFolders(missionDir) {
		sz := dirSizeWalk(filepath.Join(missionDir, name))
		folders = append(folders, wipeFolder{Name: name, SizeBytes: sz})
		total += sz
	}

	instanceID := ""
	if cfg, err := config.LoadServerCfg(filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)); err == nil {
		if v, ok := cfg.Get("instanceId"); ok {
			instanceID = v
		}
	}
	writeJSON(w, map[string]interface{}{
		"mission":       mission,
		"folders":       folders,
		"totalBytes":    total,
		"instanceId":    instanceID,
		"serverRunning": h.app.ServerIsRunning(),
	})
}

func (h *handlers) wipeApply(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	targets := h.wipeStorageFolders(missionDir)
	if len(targets) == 0 {
		writeJSON(w, map[string]interface{}{"wiped": 0, "message": "no persistence to wipe"})
		return
	}

	ts := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(h.app.ManagerDir, "wipes", ts)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	moved := []string{}
	for _, name := range targets {
		src := filepath.Join(missionDir, name)
		dst := filepath.Join(backupDir, name)
		if err := os.Rename(src, dst); err != nil {
			http.Error(w, fmt.Sprintf("move %s: %v", name, err), http.StatusInternalServerError)
			return
		}
		moved = append(moved, name)
	}
	h.app.Log.Printf("server wipe: moved %d storage folder(s) to %s", len(moved), backupDir)
	writeJSON(w, map[string]interface{}{
		"wiped":     len(moved),
		"folders":   moved,
		"backupDir": backupDir,
	})
}

// ---------------------------------------------------------------------------
// Network addresses (item 9).
//
// When the panel is exposed on the LAN (Exposure "lan"/"internet" → bound to
// 0.0.0.0), this tells the operator which URL to open on a phone or other
// device on the same network. The port is taken from the request host so it
// always reflects the port actually serving this page.

func (h *handlers) networkAddresses(w http.ResponseWriter, r *http.Request) {
	port := "8787"
	if _, p, err := net.SplitHostPort(r.Host); err == nil && p != "" {
		port = p
	}
	ips := util.LANAddresses()
	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%s/", ip, port))
	}
	exposure := h.app.Config.Exposure
	writeJSON(w, map[string]interface{}{
		"exposure":   exposure,
		"port":       port,
		"addresses":  ips,
		"urls":       urls,
		"lanEnabled": exposure == "lan" || exposure == "internet",
	})
}

// ---------------------------------------------------------------------------

// dirSizeWalk sums the byte size of all regular files under root.
func dirSizeWalk(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
