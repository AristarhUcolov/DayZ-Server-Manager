// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/auth"
	"dayzmanager/internal/config"
	"dayzmanager/internal/i18n"
	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/updater"
	"dayzmanager/internal/util"
	"dayzmanager/internal/validator"
)

type handlers struct {
	app *app.App
}

func (h *handlers) register(mux *http.ServeMux) {
	// Meta.
	mux.HandleFunc("/api/info", methods(h.info, http.MethodGet))
	mux.HandleFunc("/api/i18n", methods(h.i18nBundle, http.MethodGet))

	// Auth.
	mux.HandleFunc("/api/auth/status", methods(h.authStatus, http.MethodGet))
	mux.HandleFunc("/api/auth/login", methods(h.authLogin, http.MethodPost))
	mux.HandleFunc("/api/auth/logout", methods(h.authLogout, http.MethodPost))

	// Manager config (language, vanilla path, launch params).
	mux.HandleFunc("/api/config", methods(h.config, http.MethodGet, http.MethodPost, http.MethodPut))
	mux.HandleFunc("/api/config/finish-first-run", methods(h.finishFirstRun, http.MethodPost))

	// Server control.
	mux.HandleFunc("/api/server/status", methods(h.serverStatus, http.MethodGet))
	mux.HandleFunc("/api/server/start", methods(h.serverStart, http.MethodPost))
	mux.HandleFunc("/api/server/stop", methods(h.serverStop, http.MethodPost))
	mux.HandleFunc("/api/server/restart", methods(h.serverRestart, http.MethodPost))

	// server.cfg.
	mux.HandleFunc("/api/servercfg", methods(h.serverCfg, http.MethodGet, http.MethodPost, http.MethodPut))
	mux.HandleFunc("/api/servercfg/mission", methods(h.serverCfgMission, http.MethodPost))

	// Missions.
	mux.HandleFunc("/api/missions", methods(h.missionsList, http.MethodGet))
	mux.HandleFunc("/api/missions/duplicate", methods(h.missionsDuplicate, http.MethodPost))

	// Mods.
	mux.HandleFunc("/api/mods", methods(h.modsList, http.MethodGet))
	mux.HandleFunc("/api/mods/install", methods(h.modsInstall, http.MethodPost))
	mux.HandleFunc("/api/mods/uninstall", methods(h.modsUninstall, http.MethodPost))
	mux.HandleFunc("/api/mods/update", methods(h.modsUpdate, http.MethodPost))
	mux.HandleFunc("/api/mods/update-all", methods(h.modsUpdateAll, http.MethodPost))
	mux.HandleFunc("/api/mods/sync-keys", methods(h.modsSyncKeys, http.MethodPost))
	mux.HandleFunc("/api/mods/enable", methods(h.modsEnable, http.MethodPost))
	mux.HandleFunc("/api/mods/order", methods(h.modsOrder, http.MethodPost))

	// Types.
	mux.HandleFunc("/api/types", methods(h.typesList, http.MethodGet))
	mux.HandleFunc("/api/types/item", methods(h.typesItem, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete))
	mux.HandleFunc("/api/types/presets", methods(h.typesPresets, http.MethodGet))
	mux.HandleFunc("/api/types/apply-preset", methods(h.typesApplyPreset, http.MethodPost))

	// Moded types files.
	mux.HandleFunc("/api/moded", methods(h.modedList, http.MethodGet))
	mux.HandleFunc("/api/moded/create", methods(h.modedCreate, http.MethodPost))
	mux.HandleFunc("/api/moded/delete", methods(h.modedDelete, http.MethodPost))

	// Raw file browser/editor.
	mux.HandleFunc("/api/files/tree", methods(h.filesTree, http.MethodGet))
	mux.HandleFunc("/api/files/read", methods(h.filesRead, http.MethodGet))
	mux.HandleFunc("/api/files/write", methods(h.filesWrite, http.MethodPost))

	// Validator.
	mux.HandleFunc("/api/validate", methods(h.validate, http.MethodGet, http.MethodPost))

	// Self-update check (GitHub Releases).
	mux.HandleFunc("/api/update/check", methods(h.updateCheck, http.MethodGet))

	// Events (events.xml zombie/vehicle/helicrash spawn tables).
	mux.HandleFunc("/api/events", methods(h.eventsList, http.MethodGet))
	mux.HandleFunc("/api/events/item", methods(h.eventsItem, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete))

	// Logs.
	mux.HandleFunc("/api/logs/list", methods(h.logsList, http.MethodGet))
	mux.HandleFunc("/api/logs/read", methods(h.logsRead, http.MethodGet))
	mux.HandleFunc("/api/logs/stream", methods(h.logsStream, http.MethodGet))

	// RCon.
	mux.HandleFunc("/api/rcon/players", methods(h.rconPlayers, http.MethodGet))
	mux.HandleFunc("/api/rcon/say", methods(h.rconSay, http.MethodPost))
	mux.HandleFunc("/api/rcon/kick", methods(h.rconKick, http.MethodPost))
	mux.HandleFunc("/api/rcon/ban", methods(h.rconBan, http.MethodPost))
	mux.HandleFunc("/api/rcon/command", methods(h.rconCommand, http.MethodPost))
}

// methods rejects any verb not in allowed with 405 before calling handler.
func methods(h http.HandlerFunc, allowed ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, m := range allowed {
			if r.Method == m {
				h(w, r)
				return
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------

func (h *handlers) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"name":      h.app.Name,
		"version":   h.app.Version,
		"author":    h.app.Author,
		"serverDir": h.app.ServerDir,
	})
}

func (h *handlers) i18nBundle(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("lang")
	if code == "" {
		code = h.app.Config.Language
	}
	writeJSON(w, map[string]interface{}{
		"locale":    code,
		"supported": i18n.Supported(),
		"messages":  i18n.Get(code),
	})
}

func (h *handlers) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, h.app.Config)
		return
	}
	var patch config.Manager
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	*h.app.Config = patch
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.app.Config)
}

func (h *handlers) finishFirstRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Language        string `json:"language"`
		VanillaDayZPath string `json:"vanillaDayZPath"`
		Exposure        string `json:"exposure"`
		AdminUsername   string `json:"adminUsername"`
		AdminPassword   string `json:"adminPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Language != "" {
		h.app.Config.Language = req.Language
	}
	// Validate the vanilla DayZ path — the rest of the manager is useless
	// without a working !Workshop folder to source mods from. Allowing
	// the wizard to finish with a bogus path just pushes the confusing
	// error into every mod list refresh.
	if req.VanillaDayZPath != "" {
		st, err := os.Stat(req.VanillaDayZPath)
		if err != nil || !st.IsDir() {
			http.Error(w, "vanilla DayZ path does not exist: "+req.VanillaDayZPath, http.StatusBadRequest)
			return
		}
		ws := filepath.Join(req.VanillaDayZPath, "!Workshop")
		if st, err := os.Stat(ws); err != nil || !st.IsDir() {
			http.Error(w, "!Workshop folder not found inside "+req.VanillaDayZPath+" — is this the DayZ client install?", http.StatusBadRequest)
			return
		}
	}
	h.app.Config.VanillaDayZPath = req.VanillaDayZPath
	if req.Exposure != "" {
		h.app.Config.Exposure = req.Exposure
	}
	// Auth is required when binding outside localhost. On "internet" exposure
	// we refuse to finish the wizard without a password — that's the whole
	// point of the gate.
	if req.Exposure == "internet" && strings.TrimSpace(req.AdminPassword) == "" {
		http.Error(w, "password required for LAN/Internet exposure", http.StatusBadRequest)
		return
	}
	if req.AdminPassword != "" {
		hash, salt, err := auth.HashPassword(req.AdminPassword)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.app.Config.AdminUsername = strings.TrimSpace(req.AdminUsername)
		if h.app.Config.AdminUsername == "" {
			h.app.Config.AdminUsername = "admin"
		}
		h.app.Config.AdminPasswordHash = hash
		h.app.Config.AdminPasswordSalt = salt
		h.app.Config.RequireAuth = true
	}
	h.app.Config.FirstRunDone = true
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.app.Config)
}

// ---------------------------------------------------------------------------
// Auth.

func (h *handlers) authStatus(w http.ResponseWriter, r *http.Request) {
	authed := false
	if !h.app.Config.RequireAuth {
		authed = true
	} else if c, err := r.Cookie(auth.SessionCookieName); err == nil {
		authed = h.app.Auth.Valid(c.Value)
	}
	writeJSON(w, map[string]interface{}{
		"requireAuth":   h.app.Config.RequireAuth,
		"authenticated": authed,
		"username":      h.app.Config.AdminUsername,
	})
}

func (h *handlers) authLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.app.Config.RequireAuth {
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}
	if req.Username != h.app.Config.AdminUsername ||
		!auth.VerifyPassword(req.Password, h.app.Config.AdminPasswordHash, h.app.Config.AdminPasswordSalt) {
		// Constant-ish delay discourages online brute force. The cost of
		// PBKDF2 verification is already ~50-100ms which helps too.
		time.Sleep(300 * time.Millisecond)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	tok, err := h.app.Auth.Create()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, auth.CookieFor(tok))
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil {
		h.app.Auth.Destroy(c.Value)
	}
	http.SetCookie(w, auth.ClearCookie())
	writeJSON(w, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Server control.

func (h *handlers) serverStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"running": h.app.ServerIsRunning(),
		"pid":     h.app.Server.PID(),
		"uptime":  h.app.Server.Uptime().Round(time.Second).String(),
		"port":    h.app.Config.ServerPort,
	})
}

func (h *handlers) serverStart(w http.ResponseWriter, r *http.Request) {
	// Take WriteMu while starting so we can't launch DayZServer mid-write.
	// Holding it only across the exec.Start call keeps the window tiny.
	h.app.WriteMu.Lock()
	err := h.app.Server.Start()
	h.app.WriteMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) serverStop(w http.ResponseWriter, r *http.Request) {
	if err := h.app.Server.Stop(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped"})
}

func (h *handlers) serverRestart(w http.ResponseWriter, r *http.Request) {
	if err := h.app.Server.Restart(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "restarted"})
}

// ---------------------------------------------------------------------------
// server.cfg editor.

func (h *handlers) serverCfg(w http.ResponseWriter, r *http.Request) {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)
	if r.Method == http.MethodGet {
		cfg, err := config.LoadServerCfg(cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{
			"path":    cfgPath,
			"values":  cfg.AsMap(),
			"mission": cfg.MissionTemplate(),
		})
		return
	}
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, v := range patch {
		// Skip empty-string values so we never overwrite a numeric field
		// with a blank quoted string when the user leaves the input empty.
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		cfg.Set(k, v)
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

func (h *handlers) serverCfgMission(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Template == "" {
		http.Error(w, "template required", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !cfg.SetMissionTemplate(req.Template) {
		http.Error(w, "no class Missions block found in server.cfg", http.StatusUnprocessableEntity)
		return
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"template": req.Template})
}

// ---------------------------------------------------------------------------
// Missions.

func (h *handlers) missionsList(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(h.app.ServerDir, "mpmissions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	active, _ := h.missionTemplate()
	type row struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	out := []row{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, row{Name: e.Name(), Active: strings.EqualFold(e.Name(), active)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, map[string]interface{}{"missions": out, "active": active})
}

func (h *handlers) missionsDuplicate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src := strings.TrimSpace(req.Source)
	dst := strings.TrimSpace(req.Target)
	if src == "" || dst == "" {
		http.Error(w, "source and target required", http.StatusBadRequest)
		return
	}
	// Base() guards against `../foo` slipping into paths.
	src = filepath.Base(src)
	dst = filepath.Base(dst)
	base := filepath.Join(h.app.ServerDir, "mpmissions")
	srcDir := filepath.Join(base, src)
	dstDir := filepath.Join(base, dst)
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		http.Error(w, "source mission does not exist", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(dstDir); err == nil {
		http.Error(w, "target mission already exists", http.StatusConflict)
		return
	}
	if err := copyDirTree(srcDir, dstDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "duplicated", "target": dst})
}

// copyDirTree recursively copies src into dst, creating dst and any
// intermediate dirs. Existing files are overwritten. Used for mission
// duplication — the caller has already verified dst doesn't exist.
func copyDirTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// ---------------------------------------------------------------------------
// Mods.

func (h *handlers) modsList(w http.ResponseWriter, r *http.Request) {
	list, err := mods.List(h.app.ServerDir, h.app.Config.VanillaDayZPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"mods":        list,
		"activeMods":  h.app.Config.Mods,
		"serverMods":  h.app.Config.ServerMods,
		"vanillaPath": h.app.Config.VanillaDayZPath,
	})
}

func (h *handlers) modsInstall(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Install(h.app.ServerDir, h.app.Config.VanillaDayZPath, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "installed", "mod": name})
}

func (h *handlers) modsUninstall(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Uninstall(h.app.ServerDir, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Also drop from active mod list.
	h.app.Config.Mods = removeOnce(h.app.Config.Mods, name)
	h.app.Config.ServerMods = removeOnce(h.app.Config.ServerMods, name)
	_ = h.app.SaveConfig()
	writeJSON(w, map[string]string{"status": "uninstalled", "mod": name})
}

func (h *handlers) modsUpdate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Update(h.app.ServerDir, h.app.Config.VanillaDayZPath, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "updated", "mod": name})
}

func (h *handlers) modsUpdateAll(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	updated, err := mods.UpdateAll(h.app.ServerDir, h.app.Config.VanillaDayZPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"updated": updated, "count": len(updated)})
}

func (h *handlers) modsSyncKeys(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if err := mods.SyncKeys(h.app.ServerDir, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) modsEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mod        string `json:"mod"`
		Enabled    bool   `json:"enabled"`
		ServerSide bool   `json:"serverSide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mod == "" {
		http.Error(w, "mod required", http.StatusBadRequest)
		return
	}
	target := &h.app.Config.Mods
	if req.ServerSide {
		target = &h.app.Config.ServerMods
	}
	if req.Enabled {
		if !contains(*target, req.Mod) {
			*target = append(*target, req.Mod)
		}
	} else {
		*target = removeOnce(*target, req.Mod)
	}
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"active": h.app.Config.Mods, "server": h.app.Config.ServerMods})
}

// modsOrder replaces the active mod list with a caller-supplied ordering.
// Load order matters for DayZ mods (e.g. frameworks like @CF must precede
// their consumers), so the UI exposes drag & drop that sends the new order
// here. Unknown names are refused — otherwise a rename race could silently
// reintroduce a uninstalled mod into the launch args.
func (h *handlers) modsOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mods       []string `json:"mods"`
		ServerSide bool     `json:"serverSide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	known := map[string]bool{}
	current := h.app.Config.Mods
	if req.ServerSide {
		current = h.app.Config.ServerMods
	}
	for _, m := range current {
		known[m] = true
	}
	clean := make([]string, 0, len(req.Mods))
	seen := map[string]bool{}
	for _, m := range req.Mods {
		if !known[m] || seen[m] {
			continue
		}
		seen[m] = true
		clean = append(clean, m)
	}
	if req.ServerSide {
		h.app.Config.ServerMods = clean
	} else {
		h.app.Config.Mods = clean
	}
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"active": h.app.Config.Mods, "server": h.app.Config.ServerMods})
}

func (h *handlers) modName(r *http.Request) (string, error) {
	var req struct {
		Mod string `json:"mod"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", err
	}
	if req.Mod == "" || !strings.HasPrefix(req.Mod, "@") {
		return "", fmt.Errorf("mod name (starting with '@') required")
	}
	return req.Mod, nil
}

// ---------------------------------------------------------------------------
// Types.

func (h *handlers) typesList(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	doc, path, err := h.loadTypesFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		Name     string `json:"name"`
		Nominal  *int   `json:"nominal,omitempty"`
		Min      *int   `json:"min,omitempty"`
		Lifetime *int   `json:"lifetime,omitempty"`
		Category string `json:"category,omitempty"`
	}
	rows := make([]row, 0, len(doc.Types))
	for _, t := range doc.Types {
		r := row{Name: t.Name, Nominal: t.Nominal, Min: t.Min, Lifetime: t.Lifetime}
		if t.Category != nil {
			r.Category = t.Category.Name
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	writeJSON(w, map[string]interface{}{"file": path, "types": rows, "count": len(rows)})
}

func (h *handlers) typesItem(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	name := r.URL.Query().Get("name")
	doc, path, err := h.loadTypesFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t := doc.Find(name)
		if t == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, t)
	case http.MethodPut, http.MethodPost:
		unlock, ok := h.acquireWrite(w)
		if !ok {
			return
		}
		defer unlock()
		var t dztypes.Type
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if t.Name == "" {
			t.Name = name
		}
		doc.Upsert(t)
		if err := doc.Save(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "saved", "name": t.Name})
	case http.MethodDelete:
		unlock, ok := h.acquireWrite(w)
		if !ok {
			return
		}
		defer unlock()
		n := doc.Remove(name)
		if n == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := doc.Save(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"removed": n})
	}
}

func (h *handlers) typesPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, dztypes.BuiltinPresets())
}

func (h *handlers) typesApplyPreset(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File     string   `json:"file"`
		Names    []string `json:"names"`
		PresetID string   `json:"presetId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var preset *dztypes.Preset
	for _, p := range dztypes.BuiltinPresets() {
		if p.ID == req.PresetID {
			p := p
			preset = &p
			break
		}
	}
	if preset == nil {
		http.Error(w, "unknown preset", http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	applied := 0
	for _, name := range req.Names {
		t := doc.Find(name)
		if t == nil {
			continue
		}
		t.Usages = mergeNamed(t.Usages, preset.Usages)
		t.Values = mergeNamed(t.Values, preset.Values)
		t.Tags = mergeNamed(t.Tags, preset.Tags)
		if preset.Nominal != nil {
			t.Nominal = preset.Nominal
		}
		if preset.Min != nil {
			t.Min = preset.Min
		}
		if preset.Lifetime != nil {
			t.Lifetime = preset.Lifetime
		}
		if preset.Restock != nil {
			t.Restock = preset.Restock
		}
		if preset.Category != "" {
			t.Category = &dztypes.NamedRef{Name: preset.Category}
		}
		applied++
	}
	if err := doc.Save(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"applied": applied})
}

// loadTypesFile picks the file:
// - "" or "types.xml"    → mpmissions/<mission>/db/types.xml
// - otherwise            → mpmissions/<mission>/moded_types/<file>
func (h *handlers) loadTypesFile(file string) (*dztypes.TypesDoc, string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return nil, "", err
	}
	var path string
	if file == "" || file == "types.xml" {
		path = filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "types.xml")
	} else {
		clean := filepath.Base(file) // prevent traversal
		path = filepath.Join(dztypes.ModedDir(h.app.ServerDir, mission), clean)
	}
	doc, err := dztypes.Load(path)
	if err != nil {
		return nil, path, err
	}
	return doc, path, nil
}

// ---------------------------------------------------------------------------
// Moded types files.

func (h *handlers) modedList(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := dztypes.ModedDir(h.app.ServerDir, mission)
	entries, _ := os.ReadDir(dir)

	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	registered, _ := dztypes.RegisteredInModed(ecoPath) // ignore not-exist

	type entry struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		Modified   string `json:"modified"`
		Registered bool   `json:"registered"`
	}
	out := []entry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
			continue
		}
		info, _ := e.Info()
		out = append(out, entry{
			Name:       e.Name(),
			Size:       info.Size(),
			Modified:   info.ModTime().Format(time.RFC3339),
			Registered: registered[strings.ToLower(e.Name())],
		})
	}
	writeJSON(w, map[string]interface{}{"folder": dir, "files": out})
}

func (h *handlers) modedCreate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		FileName     string `json:"fileName"`
		AutoRegister bool   `json:"autoRegister"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !strings.HasSuffix(strings.ToLower(req.FileName), ".xml") {
		http.Error(w, "file name must end with .xml", http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := dztypes.ModedDir(h.app.ServerDir, mission)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clean := filepath.Base(req.FileName)
	fp := filepath.Join(dir, clean)
	if _, err := os.Stat(fp); err == nil {
		http.Error(w, "file already exists", http.StatusConflict)
		return
	}
	stub := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<types>\n</types>\n")
	if err := os.WriteFile(fp, stub, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.AutoRegister {
		ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
		if err := dztypes.RegisterModedFile(ecoPath, clean, "types"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]string{"file": clean})
}

func (h *handlers) modedDelete(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		FileName string `json:"fileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clean := filepath.Base(req.FileName)
	fp := filepath.Join(dztypes.ModedDir(h.app.ServerDir, mission), clean)
	if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	_, _ = dztypes.UnregisterModedFile(ecoPath, clean)
	writeJSON(w, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Raw files tree/editor. Confined to the server directory.

func (h *handlers) filesTree(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type node struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	out := []node{}
	for _, e := range entries {
		info, _ := e.Info()
		child := filepath.ToSlash(filepath.Join(rel, e.Name()))
		out = append(out, node{Name: e.Name(), Path: child, IsDir: e.IsDir(), Size: sizeOrZero(info)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, map[string]interface{}{"path": rel, "entries": out})
}

func (h *handlers) filesRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"path": rel, "content": string(body)})
}

func (h *handlers) filesWrite(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := h.resolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = util.BackupBeforeWrite(full)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// Validator.

// ---------------------------------------------------------------------------
// Logs.

func (h *handlers) logsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, dzlogs.Discover(h.app.ServerDir, h.app.Config.ProfilesDir))
}

// logsRead returns a static snapshot — the last N bytes of a log file.
// Handy for the initial scrollback on the Logs tab.
func (h *handlers) logsRead(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	path := dzlogs.Resolve(h.app.ServerDir, h.app.Config.ProfilesDir, id)
	if path == "" {
		http.Error(w, "unknown log id", http.StatusBadRequest)
		return
	}
	const maxBytes = 256 * 1024
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, map[string]interface{}{"path": path, "content": ""})
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	offset := int64(0)
	if st != nil && st.Size() > maxBytes {
		offset = st.Size() - maxBytes
	}
	_, _ = f.Seek(offset, 0)
	body, _ := io.ReadAll(f)
	writeJSON(w, map[string]interface{}{"path": path, "content": string(body)})
}

// logsStream is a Server-Sent Events endpoint. EventSource on the client
// keeps the socket open; the server writes "data: <chunk>\n\n" as it reads.
func (h *handlers) logsStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	path := dzlogs.Resolve(h.app.ServerDir, h.app.Config.ProfilesDir, id)
	if path == "" {
		http.Error(w, "unknown log id", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "log file not found (yet)", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	stop := make(chan struct{})
	go func() {
		<-r.Context().Done()
		close(stop)
	}()
	_ = dzlogs.Tail(stop, path, 64*1024, func(chunk []byte) error {
		// SSE escapes newlines into separate "data:" lines so we just
		// send each line; a terminal line is still a valid event.
		for _, line := range strings.Split(string(chunk), "\n") {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
				return err
			}
		}
		flusher.Flush()
		return nil
	})
}

// ---------------------------------------------------------------------------
// RCon.

func (h *handlers) rconPlayers(w http.ResponseWriter, r *http.Request) {
	h.app.ApplyRConConfig()
	players, err := h.app.RCon.Players()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]interface{}{"players": players, "count": len(players)})
}

func (h *handlers) rconSay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message  string `json:"message"`
		PlayerID *int   `json:"playerId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	h.app.ApplyRConConfig()
	var err error
	if req.PlayerID != nil {
		err = h.app.RCon.SayTo(*req.PlayerID, req.Message)
	} else {
		err = h.app.RCon.Say(req.Message)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID int    `json:"playerId"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.app.ApplyRConConfig()
	if err := h.app.RCon.Kick(req.PlayerID, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID int    `json:"playerId"`
		Minutes  int    `json:"minutes"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.app.ApplyRConConfig()
	if err := h.app.RCon.Ban(req.PlayerID, req.Minutes, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		http.Error(w, "command required", http.StatusBadRequest)
		return
	}
	h.app.ApplyRConConfig()
	out, err := h.app.RCon.Command(req.Command)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"output": out})
}

// ---------------------------------------------------------------------------
// events.xml editor.

func (h *handlers) eventsList(w http.ResponseWriter, r *http.Request) {
	doc, path, err := h.loadEventsFile()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		Name     string `json:"name"`
		Nominal  *int   `json:"nominal,omitempty"`
		Min      *int   `json:"min,omitempty"`
		Max      *int   `json:"max,omitempty"`
		Lifetime *int   `json:"lifetime,omitempty"`
		Active   *int   `json:"active,omitempty"`
		Children int    `json:"children"`
	}
	out := make([]row, 0, len(doc.Events))
	for _, e := range doc.Events {
		r := row{Name: e.Name, Nominal: e.Nominal, Min: e.Min, Max: e.Max, Lifetime: e.Lifetime, Active: e.Active}
		if e.Children != nil {
			r.Children = len(e.Children.Child)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, map[string]interface{}{"file": path, "events": out, "count": len(out)})
}

func (h *handlers) eventsItem(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	doc, path, err := h.loadEventsFile()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		e := doc.Find(name)
		if e == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, e)
	case http.MethodPut, http.MethodPost:
		if !h.requireStopped(w) {
			return
		}
		var e dztypes.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if e.Name == "" {
			e.Name = name
		}
		if e.Name == "" {
			http.Error(w, "event name required", http.StatusBadRequest)
			return
		}
		doc.Upsert(e)
		if err := doc.Save(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "saved", "name": e.Name})
	case http.MethodDelete:
		if !h.requireStopped(w) {
			return
		}
		n := doc.Remove(name)
		if n == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := doc.Save(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"removed": n})
	}
}

func (h *handlers) loadEventsFile() (*dztypes.EventsDoc, string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "events.xml")
	doc, err := dztypes.LoadEvents(path)
	if err != nil {
		return nil, path, err
	}
	return doc, path, nil
}

// ---------------------------------------------------------------------------
// Validator.

func (h *handlers) updateCheck(w http.ResponseWriter, r *http.Request) {
	res := updater.Check(r.Context(), h.app.Version)
	writeJSON(w, res)
}

func (h *handlers) validate(w http.ResponseWriter, r *http.Request) {
	mission, _ := h.missionTemplate()
	issues, err := validator.ValidateAll(h.app.ServerDir, mission)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"issues": issues, "count": len(issues)})
}

// ---------------------------------------------------------------------------

func (h *handlers) missionTemplate() (string, error) {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Config.ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return "", fmt.Errorf("read server.cfg: %w", err)
	}
	t := cfg.MissionTemplate()
	if t == "" {
		return "", dztypes.ErrNoMission
	}
	return t, nil
}

// resolve cleans rel and ensures it stays within serverDir.
func (h *handlers) resolve(rel string) (string, error) {
	clean := filepath.Clean("/" + rel) // absolute-style clean
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	full := filepath.Join(h.app.ServerDir, clean)
	absBase, _ := filepath.Abs(h.app.ServerDir)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absBase) {
		return "", fmt.Errorf("path escapes server dir")
	}
	return full, nil
}

// requireStopped is retained as the simple-path guard for write handlers.
// Most handlers now call acquireWrite (which takes the cross-subsystem lock
// before checking running), so this is kept for read paths that only need
// the running check.
func (h *handlers) requireStopped(w http.ResponseWriter) bool {
	if unlock, ok := h.acquireWrite(w); ok {
		unlock()
		return true
	}
	return false
}

// acquireWrite serializes all state-mutating operations across subsystems
// AND verifies the DayZ server is stopped — in a single atomic step. Without
// this, a POST /api/server/start could race with a concurrent POST
// /api/types/item that had already passed its requireStopped check, leaving
// DayZ to boot against a half-written file.
//
// Caller pattern:
//
//	unlock, ok := h.acquireWrite(w)
//	if !ok { return }
//	defer unlock()
func (h *handlers) acquireWrite(w http.ResponseWriter) (func(), bool) {
	h.app.WriteMu.Lock()
	if h.app.ServerIsRunning() {
		h.app.WriteMu.Unlock()
		http.Error(w, "server is running — stop it before editing files", http.StatusConflict)
		return nil, false
	}
	return h.app.WriteMu.Unlock, true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func sizeOrZero(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeOnce(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func mergeNamed(a, b []dztypes.NamedRef) []dztypes.NamedRef {
	seen := map[string]struct{}{}
	for _, x := range a {
		seen[strings.ToLower(x.Name)] = struct{}{}
	}
	for _, x := range b {
		if _, ok := seen[strings.ToLower(x.Name)]; ok {
			continue
		}
		a = append(a, x)
		seen[strings.ToLower(x.Name)] = struct{}{}
	}
	return a
}
