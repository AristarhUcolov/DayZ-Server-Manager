// Copyright (c) 2026 Aristarh Ucolov.
//
// v1.0.0 "October Update" endpoints: mod-source inspection, process tuning,
// and the other additions registered from registerV100.
package web

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

func (h *handlers) registerV100(mux *http.ServeMux) {
	mux.HandleFunc("/api/mods/source/inspect", methods(h.modsSourceInspect, http.MethodGet))
	mux.HandleFunc("/api/server/tuning", methods(h.serverTuning, http.MethodGet))
	mux.HandleFunc("/api/fs/reveal", methods(h.fsReveal, http.MethodPost))
	mux.HandleFunc("/api/vehicles", methods(h.vehiclesList, http.MethodGet))
	mux.HandleFunc("/api/vehicles/parts", methods(h.vehiclesParts, http.MethodPost))
	mux.HandleFunc("/api/vehicles/variants", methods(h.vehiclesVariants, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/vehicles/mods", methods(h.vehiclesMods, http.MethodGet))
	mux.HandleFunc("/api/vehicles/import", methods(h.vehiclesImport, http.MethodPost))
	mux.HandleFunc("/api/vehicles/setup", methods(h.vehiclesSetup, http.MethodPost))
	mux.HandleFunc("/api/vehicles/delete", methods(h.vehiclesDelete, http.MethodPost))
	mux.HandleFunc("/api/vehicles/tweak", methods(h.vehiclesTweak, http.MethodPost))
	mux.HandleFunc("/api/missions/full", methods(h.missionsFull, http.MethodGet))
	mux.HandleFunc("/api/missions/available", methods(h.missionsAvailable, http.MethodGet))
	mux.HandleFunc("/api/missions/install", methods(h.missionsInstall, http.MethodPost))
	mux.HandleFunc("/api/missions/import", methods(h.missionsImport, http.MethodPost))
	mux.HandleFunc("/api/missions/import-folder", methods(h.missionsImportFolder, http.MethodPost))
	mux.HandleFunc("/api/missions/delete", methods(h.missionsDelete, http.MethodPost))
	mux.HandleFunc("/api/missions/trash", methods(h.missionsTrash, http.MethodGet))
	mux.HandleFunc("/api/missions/restore", methods(h.missionsRestore, http.MethodPost))
	mux.HandleFunc("/api/missions/trash/delete", methods(h.missionsTrashDelete, http.MethodPost))
	mux.HandleFunc("/api/missions/activate", methods(h.missionsActivate, http.MethodPost))
	mux.HandleFunc("/api/missions/linked", methods(h.missionsLinked, http.MethodGet))
	mux.HandleFunc("/api/map/layers", methods(h.mapLayersInfo, http.MethodGet))
	mux.HandleFunc("/api/map/layer.png", methods(h.mapLayerImage, http.MethodGet))
	mux.HandleFunc("/api/map/buildings", methods(h.mapBuildings, http.MethodGet))
	mux.HandleFunc("/api/map/zones", methods(h.mapZones, http.MethodGet))
	mux.HandleFunc("/api/app/exit", methods(h.appExit, http.MethodPost))
	mux.HandleFunc("/api/app/show", methods(h.appShow, http.MethodPost))
	mux.HandleFunc("/api/app/shortcuts", methods(h.appShortcuts, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/app/autostart", methods(h.appAutostart, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/setup/checklist", methods(h.setupChecklist, http.MethodGet))
}

// appExit shuts the manager down from the UI (the desktop app has no console
// to Ctrl+C). {"stopServer": false} leaves the DayZ server running; the next
// manager start picks it up again. Only honoured from this machine.
func (h *handlers) appExit(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		http.Error(w, "the manager can only be closed from the machine it runs on", http.StatusForbidden)
		return
	}
	var req struct {
		StopServer bool `json:"stopServer"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if h.app.RequestExit == nil {
		http.Error(w, "exit is not available in this mode", http.StatusNotImplemented)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "exiting"})
	go func() {
		time.Sleep(300 * time.Millisecond) // let the response reach the browser
		h.app.RequestExit(req.StopServer)
	}()
}

// appShow brings the desktop window forward — a second launch of the panel
// calls this on the running one instead of opening a duplicate.
func (h *handlers) appShow(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) || h.app.ShowWindow == nil || !h.app.ShowWindow() {
		writeJSON(w, map[string]interface{}{"shown": false})
		return
	}
	writeJSON(w, map[string]interface{}{"shown": true})
}

// modsSourceInspect reports what a candidate mod source would provide, so the
// Settings page can say "found 23 mods (12 from SteamCMD)" before saving.
func (h *handlers) modsSourceInspect(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	dir := path
	if mode == config.ModSourceClient || mode == "" {
		dir = filepath.Join(path, "!Workshop")
	}
	writeJSON(w, mods.InspectSource(dir))
}

// serverTuning reports the CPU count and the running server's actual affinity
// and priority (plus the last error applying them) for the Settings page.
func (h *handlers) serverTuning(w http.ResponseWriter, r *http.Request) {
	cores, prio, lastErr := h.app.Server.Tuning()
	writeJSON(w, map[string]interface{}{
		"numCPU":   runtime.NumCPU(),
		"running":  h.app.ServerIsRunning(),
		"cores":    cores,
		"priority": prio,
		"error":    lastErr,
	})
}

// ---------------------------------------------------------------------------
// Open file location. Editors got an "Open folder" button because admins spend
// real time hopping between DayZServer / profiles / mission / db folders. The
// manager opens Explorer (or the Linux file manager) ON THE SERVER MACHINE, so
// it only does that for requests from that machine; a remote viewer gets the
// path back to copy instead. Paths are confined to the server's own folders.

func (h *handlers) revealRoots() []string {
	cfg := h.app.Cfg()
	roots := []string{h.app.ServerDir, h.profilesAbs(), h.beDir(), h.app.ManagerDir}
	if src := cfg.ModSourceDir(); src != "" {
		roots = append(roots, src)
	}
	if cfg.VanillaDayZPath != "" {
		roots = append(roots, cfg.VanillaDayZPath)
	}
	return roots
}

func withinAny(p string, roots []string) bool {
	for _, r := range roots {
		if r == "" {
			continue
		}
		rel, err := filepath.Rel(r, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// revealTarget resolves a kind shortcut, a mod name, or a (server-relative or
// absolute) path to an absolute path inside the allowed roots.
func (h *handlers) revealTarget(kind, mod, path string) (string, error) {
	mission, _ := h.missionTemplate()
	missionDir := ""
	if mission != "" {
		missionDir = dztypes.MissionDir(h.app.ServerDir, mission)
	}
	switch kind {
	case "server":
		return h.app.ServerDir, nil
	case "mission":
		return missionDir, nil
	case "db":
		return filepath.Join(missionDir, "db"), nil
	case "missionfile": // a file relative to the active mission folder
		if missionDir == "" || path == "" {
			return "", errors.New("no mission")
		}
		return filepath.Join(missionDir, filepath.FromSlash(path)), nil
	case "moded":
		return dztypes.ModedDir(h.app.ServerDir, mission), nil
	case "profiles", "logs":
		return h.profilesAbs(), nil
	case "profilesfile": // a file relative to the profiles folder (mod settings)
		if path == "" {
			return h.profilesAbs(), nil
		}
		return filepath.Join(h.profilesAbs(), filepath.FromSlash(path)), nil
	case "keys":
		return filepath.Join(h.app.ServerDir, "keys"), nil
	case "battleye":
		return h.beDir(), nil
	case "mpmissions":
		return filepath.Join(h.app.ServerDir, "mpmissions"), nil
	case "modsource":
		return h.app.Cfg().ModSourceDir(), nil
	case "manager":
		return h.app.ManagerDir, nil
	case "backups":
		return filepath.Join(h.app.ManagerDir, "backups"), nil
	case "mod":
		if mod == "" || strings.ContainsAny(mod, `/\`) {
			return "", errors.New("invalid mod name")
		}
		p := filepath.Join(h.app.ServerDir, mod)
		if _, err := os.Stat(p); err != nil {
			if alt, err2 := mods.SourcePath(h.app.Cfg().ModSourceDir(), mod); err2 == nil {
				return alt, nil
			}
		}
		return p, nil
	}
	if path == "" {
		return "", errors.New("nothing to open")
	}
	p := filepath.FromSlash(path)
	if !filepath.IsAbs(p) {
		p = filepath.Join(h.app.ServerDir, p)
	}
	return filepath.Clean(p), nil
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (h *handlers) fsReveal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		Mod  string `json:"mod"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target, err := h.revealTarget(req.Kind, req.Mod, req.Path)
	if err != nil || target == "" {
		http.Error(w, "nothing to open", http.StatusBadRequest)
		return
	}
	if !withinAny(target, h.revealRoots()) {
		http.Error(w, "path is outside the server folders", http.StatusForbidden)
		return
	}
	// A file that doesn't exist yet: open its folder instead.
	if _, err := os.Stat(target); err != nil {
		parent := filepath.Dir(target)
		if _, err := os.Stat(parent); err != nil {
			http.Error(w, "not found: "+target, http.StatusNotFound)
			return
		}
		target = parent
	}
	out := map[string]interface{}{"path": target, "opened": false}
	switch {
	case !isLoopback(r):
		out["reason"] = "remote"
	case !util.CanReveal():
		out["reason"] = "headless"
	default:
		if err := util.Reveal(target); err != nil {
			out["reason"] = err.Error()
		} else {
			out["opened"] = true
		}
	}
	writeJSON(w, out)
}
