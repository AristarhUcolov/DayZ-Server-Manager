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
	"regexp"
	"strconv"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

var (
	globalIntRe   = regexp.MustCompile(`^-?\d+$`)
	globalFloatRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// globalValueOK checks a value against its variable's type (0 = integer,
// 1 = float). An empty or malformed number — "", "abc", "1,5", "0.5" for an
// integer — would otherwise be written into globals.xml as typed.
func globalValueOK(typ, v string) bool {
	v = strings.TrimSpace(v)
	switch typ {
	case "0":
		return globalIntRe.MatchString(v)
	case "1":
		return globalFloatRe.MatchString(v)
	}
	return v != ""
}

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
	// Check every value against its variable's type before writing any.
	types := map[string]string{}
	for _, v := range doc.Vars {
		types[strings.ToLower(v.Name)] = v.Type
	}
	var bad []string
	for _, v := range req.Vars {
		if typ, ok := types[strings.ToLower(v.Name)]; ok && !globalValueOK(typ, v.Value) {
			bad = append(bad, v.Name+"="+strconv.Quote(v.Value))
		}
	}
	if len(bad) > 0 {
		http.Error(w, "not a valid number: "+strings.Join(bad, ", "), http.StatusBadRequest)
		return
	}
	applied := 0
	for _, v := range req.Vars {
		if doc.SetValue(v.Name, strings.TrimSpace(v.Value)) {
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
