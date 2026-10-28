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
	"dayzmanager/internal/guide"
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

// cfgGameplayEnabled reports whether cfggameplay.json is switched on in
// server.cfg (a missing line counts as off, as in DayZ).
func (h *handlers) cfgGameplayEnabled() bool {
	return h.serverCfgFlag("enableCfgGameplayFile")
}

// serverCfgFlag reports whether a 0/1 setting of serverDZ.cfg is 1.
func (h *handlers) serverCfgFlag(key string) bool {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	c, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return false
	}
	v, _ := c.Get(key)
	return strings.TrimSpace(v) == "1"
}

// setServerCfgFlag sets a 0/1 setting of serverDZ.cfg to 1, adding the line
// when the file has none. It takes effect at the server's next start.
func (h *handlers) setServerCfgFlag(key string) error {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	c, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return err
	}
	if v, _ := c.Get(key); strings.TrimSpace(v) == "1" {
		return nil
	}
	c.Set(key, 1)
	return c.Save(cfgPath)
}

// gearPresetInfo is one row of the loadout list.
type gearPresetInfo struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Weight  int    `json:"weight"`
	Slots   int    `json:"slots"`
	Cargo   int    `json:"cargo"`
	Missing bool   `json:"missing"`           // listed in cfggameplay.json, no file on disk
	Invalid string `json:"invalid,omitempty"` // the file is there but DayZ cannot read it
}

func loadGearInfo(missionDir, rel string) gearPresetInfo {
	info := gearPresetInfo{File: rel}
	p, err := dztypes.LoadGearPreset(filepath.Join(missionDir, filepath.FromSlash(rel)))
	switch {
	case os.IsNotExist(err):
		info.Missing = true
	case err != nil:
		info.Invalid = err.Error()
	default:
		info.Name = p.Name
		info.Weight = p.SpawnWeight
		info.Slots = len(p.AttachmentSlotItemSets)
		info.Cargo = len(p.DiscreteUnsortedItemSets)
	}
	return info
}

// spawnGearList reports the system state, every registered preset, and the
// preset files sitting in the mission folder without being registered.
//
// How DayZ decides (missionServer.c / CfgPlayerSpawnHandler.c, 1.29): the
// presets load only when enableCfgGameplayFile = 1 and the mission has a
// cfggameplay.json listing them, and the FIRST registered file that is
// missing or unreadable stops the whole list — every loadout is then off and
// players spawn from init.c. "active" mirrors exactly that.
func (h *handlers) spawnGearList(w http.ResponseWriter, r *http.Request) {
	missionDir, err := h.spawnGearMissionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	registered, regErr := dztypes.RegisteredGearFiles(gameplayPath)

	presets := []gearPresetInfo{}
	usable, broken := 0, 0
	for _, rel := range registered {
		info := loadGearInfo(missionDir, rel)
		switch {
		case info.Missing || info.Invalid != "":
			broken++
		case info.Weight >= 1: // DayZ skips a preset with spawnWeight < 1
			usable++
		}
		presets = append(presets, info)
	}

	// Presets on disk that cfggameplay.json does not list.
	unlinked := []gearPresetInfo{}
	for _, rel := range dztypes.FindGearPresetFiles(missionDir) {
		known := false
		for _, reg := range registered {
			if dztypes.SameGearPath(reg, rel) {
				known = true
				break
			}
		}
		if !known {
			unlinked = append(unlinked, loadGearInfo(missionDir, rel))
		}
	}

	_, gpErr := os.Stat(gameplayPath)
	flag := h.cfgGameplayEnabled()
	out := map[string]interface{}{
		"presets":     presets,
		"unlinked":    unlinked,
		"cfgGameplay": gpErr == nil, // the file exists
		"flagEnabled": flag,         // enableCfgGameplayFile = 1
		"blocked":     broken > 0,   // a broken entry switches every loadout off
		// active = the loadout actually takes effect; anything less means
		// players still spawn from init.c and the UI must say so.
		"active": gpErr == nil && regErr == nil && flag && usable > 0 && broken == 0,
	}
	if regErr != nil {
		out["gameplayError"] = regErr.Error()
	}
	writeJSON(w, out)
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
		// A new or imported preset never overwrites another one that
		// happens to have the same name.
		rel = dztypes.FreeGearFileName(missionDir, dztypes.GearFileName(req.Preset.Name))
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
	if err := ensureGameplayFile(gameplayPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
		if !dztypes.SameGearPath(f, rel) {
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

// spawnGearEnable switches the loadout system on, whatever state it is in:
// it reuses every preset already in the mission folder (a server update that
// replaced cfggameplay.json unregisters them but leaves the files), creates
// the starter loadout only when there is nothing to reuse, and turns on
// enableCfgGameplayFile. Running it twice changes nothing.
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
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	if err := ensureGameplayFile(gameplayPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	registered, err := dztypes.RegisteredGearFiles(gameplayPath)
	if err != nil {
		// A syntax error in cfggameplay.json: rewriting it would lose the
		// admin's settings, so say what is wrong instead.
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	usable := 0
	for _, rel := range registered {
		if info := loadGearInfo(missionDir, rel); !info.Missing && info.Invalid == "" {
			usable++
		}
	}

	var linked []string
	created := ""
	if usable == 0 {
		for _, rel := range dztypes.FindGearPresetFiles(missionDir) {
			info := loadGearInfo(missionDir, rel)
			if info.Invalid != "" || isRegisteredGear(registered, rel) {
				continue
			}
			linked = append(linked, rel)
		}
		if len(linked) == 0 {
			starter := dztypes.StarterGearPreset()
			// Never overwrite a file that is there but unreadable.
			created = dztypes.FreeGearFileName(missionDir, dztypes.GearFileName(starter.Name))
			full, err := safeGearPath(missionDir, created)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := starter.Save(full); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			linked = append(linked, created)
		}
		for _, rel := range linked {
			if err := h.ensureGearRegistered(gameplayPath, rel); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	flagSet := h.ensureCfgGameplayFlag()
	if linked == nil {
		linked = []string{}
	}
	writeJSON(w, map[string]interface{}{"status": "enabled", "linked": linked,
		"created": created, "flagSet": flagSet})
}

// spawnGearRegister links a preset file that is in the mission folder but
// missing from cfggameplay.json.
func (h *handlers) spawnGearRegister(w http.ResponseWriter, r *http.Request) {
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
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := safeGearPath(missionDir, req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Registering a file DayZ cannot read would switch every loadout off.
	if _, err := dztypes.LoadGearPreset(full); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	gameplayPath := filepath.Join(missionDir, "cfggameplay.json")
	if err := ensureGameplayFile(gameplayPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.ensureGearRegistered(gameplayPath, filepath.ToSlash(filepath.Clean(filepath.FromSlash(req.File)))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flagSet := h.ensureCfgGameplayFlag()
	writeJSON(w, map[string]interface{}{"status": "linked", "file": req.File, "flagSet": flagSet})
}

// ensureGameplayFile gives a mission without cfggameplay.json the vanilla one,
// so registering a loadout never leaves a file holding nothing but the list
// (DayZ would then read every other setting as 0).
func ensureGameplayFile(gameplayPath string) error {
	if _, err := os.Stat(gameplayPath); !os.IsNotExist(err) {
		return nil
	}
	return os.WriteFile(gameplayPath, []byte(guide.GameplayDefaults), 0o644)
}

func isRegisteredGear(registered []string, rel string) bool {
	for _, f := range registered {
		if dztypes.SameGearPath(f, rel) {
			return true
		}
	}
	return false
}

// ensureGearRegistered adds rel to cfggameplay.json's list if not already there.
func (h *handlers) ensureGearRegistered(gameplayPath, rel string) error {
	registered, err := dztypes.RegisteredGearFiles(gameplayPath)
	if err != nil {
		return err
	}
	if isRegisteredGear(registered, rel) {
		return nil
	}
	return dztypes.SetRegisteredGearFiles(gameplayPath, append(registered, rel))
}

// ensureCfgGameplayFlag turns on enableCfgGameplayFile, the server.cfg switch
// without which cfggameplay.json — and therefore the whole loadout — is ignored.
func (h *handlers) ensureCfgGameplayFlag() bool {
	return h.setServerCfgFlag("enableCfgGameplayFile") == nil
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
