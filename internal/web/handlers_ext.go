// Copyright (c) 2026 Aristarh Ucolov.
//
// Extra handlers: Workshop collection imports, mod sync-all, BattlEye editor,
// Types bulk-edit, mission central-economy DB editor, scheduled RCon
// announcements. Kept in a separate file to avoid growing handlers.go past
// what the Read tool tolerates in one go.
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

	"dayzmanager/internal/battleye"
	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// ---------------------------------------------------------------------------
// Mods: sync-all + collection importer.

func (h *handlers) modsSyncAll(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Only []string `json:"only"` // optional filter by @Mod names
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	res, err := mods.SyncAll(h.app.ServerDir, h.app.Config.VanillaDayZPath, req.Only)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (h *handlers) modsCollectionResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Save bool   `json:"save"` // persist to config.WorkshopCollections
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	id, err := mods.ParseCollectionURL(req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	children, err := mods.FetchCollectionChildren(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	res, err := mods.ResolveCollection(h.app.Config.VanillaDayZPath, children)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res.CollectionID = id
	if req.Save {
		if !contains(h.app.Config.WorkshopCollections, req.URL) {
			h.app.Config.WorkshopCollections = append(h.app.Config.WorkshopCollections, req.URL)
			_ = h.app.SaveConfig()
		}
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------------------------
// BattlEye editor.

func (h *handlers) beDir() string {
	return battleye.Dir(h.app.ServerDir, h.app.Config.BEPath)
}

func (h *handlers) battleyeList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"dir":   h.beDir(),
		"files": battleye.List(h.beDir()),
	})
}

func (h *handlers) battleyeRead(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	content, err := battleye.Read(h.beDir(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{"name": name, "content": content})
}

func (h *handlers) battleyeWrite(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := battleye.Write(h.beDir(), req.Name, req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// Types bulk-edit.

func (h *handlers) typesBulkPatch(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File  string                 `json:"file"`
		Names []string               `json:"names"`
		Patch dztypes.BulkFieldPatch `json:"patch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Names) == 0 {
		http.Error(w, "names required", http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	touched := doc.BulkPatch(req.Names, req.Patch)
	if err := doc.Save(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"touched": touched})
}

// ---------------------------------------------------------------------------
// Mission central-economy DB editor.
//
// Covers the XML files sitting in `<mission>/db/` and the top-level
// `cfgeconomycore.xml` / `cfgspawnabletypes.xml` etc. — the ones most server
// admins hand-tune. Everything is raw text at this layer; the typed editors
// (Types, Events) stay as the higher-level alternative.

var allowedMissionDBFiles = map[string]bool{
	"types.xml":              true,
	"events.xml":             true,
	"economy.xml":            true,
	"globals.xml":            true,
	"messages.xml":           true,
	"cfgeventspawns.xml":     true,
	"cfgplayerspawnpoints.xml": true,
	"cfgweather.xml":         true,
	"cfgeffectarea.json":     true,
	"init.c":                 true,
	"cfgeconomycore.xml":     true,
	"cfgenvironment.xml":     true,
	"cfggameplay.json":       true,
	"cfgrandompresets.xml":   true,
	"cfgspawnabletypes.xml":  true,
	"cfglimitsdefinition.xml": true,
	"cfglimitsdefinitionuser.xml": true,
	"cfgundergroundtriggers.json": true,
}

func (h *handlers) missionDir() (string, error) {
	tmpl, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(h.app.ServerDir, "mpmissions", tmpl), nil
}

func (h *handlers) missionDBList(w http.ResponseWriter, r *http.Request) {
	dir, err := h.missionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	type entry struct {
		Name     string `json:"name"`
		Path     string `json:"path"` // relative to mission dir
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
		Exists   bool   `json:"exists"`
	}
	want := []struct{ path string }{
		{"init.c"},
		{"cfgeconomycore.xml"},
		{"cfgenvironment.xml"},
		{"cfggameplay.json"},
		{"cfgrandompresets.xml"},
		{"cfgspawnabletypes.xml"},
		{"cfglimitsdefinition.xml"},
		{"cfglimitsdefinitionuser.xml"},
		{"cfgundergroundtriggers.json"},
		{"cfgeffectarea.json"},
		{"db/types.xml"},
		{"db/events.xml"},
		{"db/economy.xml"},
		{"db/globals.xml"},
		{"db/messages.xml"},
		{"db/cfgeventspawns.xml"},
		{"db/cfgplayerspawnpoints.xml"},
		{"db/cfgweather.xml"},
	}
	out := make([]entry, 0, len(want))
	for _, w := range want {
		full := filepath.Join(dir, filepath.FromSlash(w.path))
		st, err := os.Stat(full)
		e := entry{Name: filepath.Base(w.path), Path: w.path}
		if err == nil && !st.IsDir() {
			e.Exists = true
			e.Size = st.Size()
			e.Modified = st.ModTime().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	writeJSON(w, map[string]interface{}{"dir": dir, "files": out})
}

func (h *handlers) missionDBResolve(rel string) (string, error) {
	dir, err := h.missionDir()
	if err != nil {
		return "", err
	}
	// Strict: rel must be either a bare allowed name or "db/<allowed>".
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid path")
	}
	base := strings.ToLower(filepath.Base(cleaned))
	if !allowedMissionDBFiles[base] {
		return "", fmt.Errorf("file not on mission-db allowlist: %s", base)
	}
	return filepath.Join(dir, filepath.FromSlash(cleaned)), nil
}

func (h *handlers) missionDBRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.missionDBResolve(rel)
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
	// 4 MB cap — types.xml is routinely ~1 MB; anything larger is likely
	// not a hand-editable file.
	body, err := io.ReadAll(io.LimitReader(f, 4*1024*1024))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"path": rel, "content": string(body)})
}

func (h *handlers) missionDBWrite(w http.ResponseWriter, r *http.Request) {
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
	full, err := h.missionDBResolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
// Scheduled announcements.

func (h *handlers) announcementsList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]interface{}{
			"announcements": h.app.Config.ScheduledAnnouncements,
		})
		return
	}
	var req struct {
		Announcements []config.ScheduledAnnouncement `json:"announcements"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Light validation: HH:MM sanity.
	for i, a := range req.Announcements {
		t := strings.TrimSpace(a.Time)
		if len(t) != 5 || t[2] != ':' {
			http.Error(w, fmt.Sprintf("announcement %d: bad time %q (want HH:MM)", i, t), http.StatusBadRequest)
			return
		}
	}
	h.app.Config.ScheduledAnnouncements = req.Announcements
	if err := h.app.SaveConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"announcements": h.app.Config.ScheduledAnnouncements})
}
