// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.20.0 handlers: the player fresh-spawn loadout editor over DayZ's
// spawn-gear system (preset JSON files registered in cfggameplay.json).
//
// The point that makes this safe to ship: the panel is honest about the
// system's state. A loadout only takes effect when three things line up — the
// preset file exists, it is listed in cfggameplay.json, and enableCfgGameplayFile
// is 1 — so /api/spawngear reports exactly which of those are true, and the UI
// refuses to imply "done" when the mission is still spawning players from
// init.c.
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/config"
	dztypes "dayzmanager/internal/types"
)

var errBadGearPath = errors.New("invalid loadout path — must be a .json inside the mission")

func (h *handlers) spawnGearMissionDir() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return dztypes.MissionDir(h.app.ServerDir, mission), nil
}

// gearActive reports whether cfggameplay.json is switched on in server.cfg.
func (h *handlers) cfgGameplayEnabled() bool {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	c, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return false
	}
	v, _ := c.Get("enableCfgGameplayFile")
	return v == "1"
}

// spawnGearList reports the system state and every registered preset.
func (h *handlers) spawnGearList(w http.ResponseWriter, r *http.Request) {
	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	registered, _ := dztypes.RegisteredGearFiles(gameplayPath)

	type presetInfo struct {
		File    string `json:"file"`
		Name    string `json:"name"`
		Weight  int    `json:"weight"`
		Slots   int    `json:"slots"`
		Cargo   int    `json:"cargo"`
		Missing bool   `json:"missing"` // listed in cfggameplay but no file on disk
	}
	var presets []presetInfo
	for _, rel := range registered {
		info := presetInfo{File: rel}
		p, err := dztypes.LoadGearPreset(filepath.Join(missionDir, filepath.FromSlash(rel)))
		if err != nil {
			info.Missing = true
		} else {
			info.Name = p.Name
			info.Weight = p.SpawnWeight
			info.Slots = len(p.AttachmentSlotItemSets)
			info.Cargo = len(p.DiscreteUnsortedItemSets)
		}
		presets = append(presets, info)
	}
	if presets == nil {
		presets = []presetInfo{}
	}

	_, gpErr := os.Stat(gameplayPath)
	writeJSON(w, map[string]interface{}{
		"presets":     presets,
		"cfgGameplay": gpErr == nil,           // the file exists
		"flagEnabled": h.cfgGameplayEnabled(), // enableCfgGameplayFile = 1
		// active = the loadout actually takes effect; anything less means the
		// mission is still using init.c and the UI must say so.
		"active": gpErr == nil && h.cfgGameplayEnabled() && len(presets) > 0,
	})
}

// spawnGearGet returns one preset for editing.
func (h *handlers) spawnGearGet(w http.ResponseWriter, r *http.Request) {
	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rel := r.URL.Query().Get("file")
	full, err := safeGearPath(missionDir, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := dztypes.LoadGearPreset(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, p)
}

// spawnGearSave writes one preset. The file is registered in cfggameplay.json
// on first save, and enableCfgGameplayFile is set, so a saved loadout can never
// sit inert because a second switch was left off.
func (h *handlers) spawnGearSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		File   string             `json:"file"` // empty on create
		Preset dztypes.GearPreset `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Preset.Name) == "" {
		http.Error(w, "preset needs a name", http.StatusBadRequest)
		return
	}
	if req.Preset.SpawnWeight < 1 {
		req.Preset.SpawnWeight = 1 // 0 would mean DayZ never picks it
	}
	rel := req.File
	if strings.TrimSpace(rel) == "" {
		rel = dztypes.GearFileName(req.Preset.Name)
	}
	full, err := safeGearPath(missionDir, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := req.Preset.Save(full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Make sure it is registered and the whole system is switched on.
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	if err := h.ensureGearRegistered(gameplayPath, rel); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flagSet := h.ensureCfgGameplayFlag()
	writeJSON(w, map[string]interface{}{"status": "saved", "file": rel, "flagSet": flagSet})
}

// spawnGearDelete removes a preset file and unregisters it.
func (h *handlers) spawnGearDelete(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rel := r.URL.Query().Get("file")
	full, err := safeGearPath(missionDir, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	registered, _ := dztypes.RegisteredGearFiles(gameplayPath)
	kept := registered[:0]
	for _, f := range registered {
		if f != rel {
			kept = append(kept, f)
		}
	}
	if err := dztypes.SetRegisteredGearFiles(gameplayPath, kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = os.Remove(full) // the file may already be gone; unregistering is what matters
	writeJSON(w, map[string]interface{}{"status": "deleted", "file": rel})
}

// spawnGearEnable creates the starter loadout for a mission that is still on
// init.c, so "enable" produces something that visibly works rather than a
// naked spawn.
func (h *handlers) spawnGearEnable(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	starter := dztypes.StarterGearPreset()
	rel := dztypes.GearFileName(starter.Name)
	full, err := safeGearPath(missionDir, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Do not clobber an existing file of the same name.
	if _, err := os.Stat(full); err == nil {
		http.Error(w, "a loadout named "+rel+" already exists", http.StatusConflict)
		return
	}
	if err := starter.Save(full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	if err := h.ensureGearRegistered(gameplayPath, rel); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.ensureCfgGameplayFlag()
	writeJSON(w, map[string]interface{}{"status": "enabled", "file": rel})
}

// ensureGearRegistered adds rel to cfggameplay.json's list if not already there.
func (h *handlers) ensureGearRegistered(gameplayPath, rel string) error {
	registered, _ := dztypes.RegisteredGearFiles(gameplayPath)
	for _, f := range registered {
		if f == rel {
			return nil
		}
	}
	return dztypes.SetRegisteredGearFiles(gameplayPath, append(registered, rel))
}

// ensureCfgGameplayFlag turns on enableCfgGameplayFile, the server.cfg switch
// without which cfggameplay.json — and therefore the whole loadout — is ignored.
func (h *handlers) ensureCfgGameplayFlag() bool {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	c, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return false
	}
	if v, _ := c.Get("enableCfgGameplayFile"); v == "1" {
		return true
	}
	c.Set("enableCfgGameplayFile", 1)
	return c.Save(cfgPath) == nil
}

// safeGearPath resolves a mission-relative preset path, refusing anything that
// escapes the mission folder or is not a .json.
func safeGearPath(missionDir, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errBadGearPath
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.Contains(clean, ".."+string(filepath.Separator)) {
		return "", errBadGearPath
	}
	if strings.ToLower(filepath.Ext(clean)) != ".json" {
		return "", errBadGearPath
	}
	full := filepath.Join(missionDir, clean)
	// Final belt-and-braces: the resolved path must stay inside the mission.
	if relCheck, err := filepath.Rel(missionDir, full); err != nil ||
		strings.HasPrefix(relCheck, "..") {
		return "", errBadGearPath
	}
	return full, nil
}
