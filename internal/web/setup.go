// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/config"
	dzlogs "dayzmanager/internal/logs"
)

// setupItem is one line of the dashboard's "Getting started" checklist.
type setupItem struct {
	ID    string `json:"id"`
	Done  bool   `json:"done"`
	Route string `json:"route"`
}

// setupChecklist reports what a new admin still has to set up, derived from
// the real configuration — not from which pages were visited — so the card
// disappears by itself once the server is properly configured.
func (h *handlers) setupChecklist(w http.ResponseWriter, r *http.Request) {
	c := h.app.Cfg()
	var hostname, adminPass string
	if sc, err := config.LoadServerCfg(filepath.Join(h.app.ServerDir, c.ServerCfg)); err == nil {
		hostname, _ = sc.Get("hostname")
		adminPass, _ = sc.Get("passwordAdmin")
	}
	unq := func(s string) string { return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `";`)) }
	hostname, adminPass = unq(hostname), unq(adminPass)

	// Server-only needs no source; SteamCMD installs itself on first use.
	srcOK := c.EffectiveModSource() == config.ModSourceServer || c.EffectiveModSource() == config.ModSourceSteamCMD
	if dir := c.ModSourceDir(); dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			srcOK = true
		}
	}
	started := h.app.ServerIsRunning()
	if !started {
		for _, s := range dzlogs.Discover(h.app.ServerDir, c.ProfilesDir) {
			if s.ID == "rpt" && s.Exists {
				started = true
			}
		}
	}

	items := []setupItem{
		{"hostname", hostname != "" && !strings.EqualFold(hostname, "EXAMPLE NAME"), "server"},
		{"adminPassword", adminPass != "", "server"},
		{"modSource", srcOK, "settings"},
		{"rcon", strings.TrimSpace(c.RConPassword) != "", "rcon"},
		{"restarts", (c.AutoRestartEnabled && c.AutoRestartSeconds > 0) || len(c.ScheduledRestarts) > 0, "settings/restarts"},
		{"watchdog", c.RestartOnCrash, "settings/restarts"},
		{"backups", c.BackupIntervalHours > 0, "settings/backups"},
		{"started", started, "dashboard"},
	}
	// Reachable from other devices: a login is part of a sane setup.
	if c.Exposure == "lan" || c.Exposure == "internet" {
		items = append(items[:1:1], append([]setupItem{{"login", h.auth.enabled(), "settings/access"}}, items[1:]...)...)
	}
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	writeJSON(w, map[string]interface{}{"items": items, "done": done, "total": len(items)})
}
