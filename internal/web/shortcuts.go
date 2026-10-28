// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"

	"dayzmanager/internal/app"
)

// appShortcuts reports (GET) or creates (POST) the manager's Start menu /
// applications-menu and desktop shortcuts — what -install does, from the
// Settings page. Only for a browser on the computer the manager runs on: a
// shortcut on a remote server's desktop helps nobody.
func (h *handlers) appShortcuts(w http.ResponseWriter, r *http.Request) {
	a := h.app
	local := isLoopback(r)
	supported := local && a.InstallShortcuts != nil
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]interface{}{
			"supported": supported,
			"installed": a.ShortcutsInstalled != nil && a.ShortcutsInstalled(),
			"local":     local,
		})
		return
	}
	if !supported {
		http.Error(w, "shortcuts can only be created from the computer the manager runs on", http.StatusForbidden)
		return
	}
	paths, err := a.InstallShortcuts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "paths": paths})
}

// appAutostart reports (GET) or switches (POST {"enabled": bool}) starting
// the manager together with the computer — a Windows sign-in entry or a Linux
// desktop-login entry; a Linux server without a desktop gets a systemd unit
// to install instead.
func (h *handlers) appAutostart(w http.ResponseWriter, r *http.Request) {
	a := h.app
	if a.Autostart == nil || a.SetAutostart == nil {
		writeJSON(w, app.AutostartInfo{})
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, a.Autostart())
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.SetAutostart(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a.Autostart())
}
