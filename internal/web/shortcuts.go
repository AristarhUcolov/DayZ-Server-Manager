// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http"
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
