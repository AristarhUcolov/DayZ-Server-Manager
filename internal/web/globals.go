// Copyright (c) 2026 Aristarh Ucolov.
//
// globals.xml editor backend. A friendly form over the central-economy global
// variables, with a plain-language tooltip per known variable (from the guide,
// under the "gl." prefix). Schema-free: whatever variables the file holds are
// returned, so it works on any DayZ version or modded globals set.
package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

func (h *handlers) globalsPath() (string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "globals.xml"), nil
}

func (h *handlers) globalsGet(w http.ResponseWriter, r *http.Request) {
	path, err := h.globalsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := dztypes.LoadGlobals(path)
	if err != nil {
		writeJSON(w, map[string]interface{}{"path": path, "exists": false, "vars": []dztypes.GlobalVar{}})
		return
	}
	writeJSON(w, map[string]interface{}{"path": path, "exists": true, "vars": doc.Vars})
}

func (h *handlers) globalsSave(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Vars []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path, err := h.globalsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := dztypes.LoadGlobals(path)
	if err != nil {
		http.Error(w, "globals.xml not found", http.StatusNotFound)
		return
	}
	applied := 0
	for _, v := range req.Vars {
		if doc.SetValue(v.Name, v.Value) {
			applied++
		}
	}
	if applied > 0 {
		_ = util.BackupBeforeWrite(path)
		if err := dztypes.SaveGlobals(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"applied": applied})
}
