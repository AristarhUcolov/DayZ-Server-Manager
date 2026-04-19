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
	"dayzmanager/internal/config"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/validator"
)

type handlers struct {
	app *app.App
}

func (h *handlers) register(mux *http.ServeMux) {
	// Meta.
	mux.HandleFunc("/api/info", methods(h.info, http.MethodGet))
	mux.HandleFunc("/api/i18n", methods(h.i18nBundle, http.MethodGet))

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

	// Mods.
	mux.HandleFunc("/api/mods", methods(h.modsList, http.MethodGet))
	mux.HandleFunc("/api/mods/install", methods(h.modsInstall, http.MethodPost))
	mux.HandleFunc("/api/mods/uninstall", methods(h.modsUninstall, http.MethodPost))
	mux.HandleFunc("/api/mods/update", methods(h.modsUpdate, http.MethodPost))
	mux.HandleFunc("/api/mods/update-all", methods(h.modsUpdateAll, http.MethodPost))
	mux.HandleFunc("/api/mods/sync-keys", methods(h.modsSyncKeys, http.MethodPost))
	mux.HandleFunc("/api/mods/enable", methods(h.modsEnable, http.MethodPost))

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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Language != "" {
		h.app.Config.Language = req.Language
	}
	h.app.Config.VanillaDayZPath = req.VanillaDayZPath
	if req.Exposure != "" {
		h.app.Config.Exposure = req.Exposure
	}
	h.app.Config.FirstRunDone = true
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.app.Config)
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
	if err := h.app.Server.Start(); err != nil {
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
		if !h.requireStopped(w) {
			return
		}
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

func (h *handlers) typesPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, dztypes.BuiltinPresets())
}

func (h *handlers) typesApplyPreset(w http.ResponseWriter, r *http.Request) {
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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
	if !h.requireStopped(w) {
		return
	}
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

// requireStopped writes a 409 and returns false if the DayZ server is running.
// All write endpoints must call this — matches the user's rule that file edits
// only happen while the server is off (DayZ locks its working set).
func (h *handlers) requireStopped(w http.ResponseWriter) bool {
	if h.app.ServerIsRunning() {
		http.Error(w, "server is running — stop it before editing files", http.StatusConflict)
		return false
	}
	return true
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
