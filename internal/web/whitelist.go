// Copyright (c) 2026 Aristarh Ucolov.
//
// Whitelist: DayZ's own whitelist.txt in the server folder. Same format as
// ban.txt — the 44-character player ID from the admin log, one per line, "//"
// comments — and, per its header, "players can be added/removed while the
// server is running and change will come in effect immediately, kicking the
// removed player". It is only enforced with enableWhitelist = 1 in
// serverDZ.cfg, which the server reads at start.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/config"
)

func (h *handlers) registerWhitelist(mux *http.ServeMux) {
	mux.HandleFunc("/api/whitelist", methods(h.whitelistList, http.MethodGet))
	mux.HandleFunc("/api/whitelist/add", methods(h.whitelistAdd, http.MethodPost))
	mux.HandleFunc("/api/whitelist/remove", methods(h.whitelistRemove, http.MethodPost))
	mux.HandleFunc("/api/whitelist/enable", methods(h.whitelistEnable, http.MethodPost))
}

func (h *handlers) whitelistPath() string { return filepath.Join(h.app.ServerDir, "whitelist.txt") }

func (h *handlers) serverCfgPath() string {
	return filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
}

// whitelistEnabled reads enableWhitelist from serverDZ.cfg.
func (h *handlers) whitelistEnabled() bool {
	sc, err := config.LoadServerCfg(h.serverCfgPath())
	if err != nil {
		return false
	}
	v, ok := sc.Get("enableWhitelist")
	return ok && strings.Trim(strings.TrimSpace(v), `";`) == "1"
}

func (h *handlers) whitelistList(w http.ResponseWriter, r *http.Request) {
	data, _ := os.ReadFile(h.whitelistPath())
	names := h.namesByID()
	banned := h.bannedAccounts()
	type entry struct {
		ID      string `json:"id"`
		Comment string `json:"comment,omitempty"`
		Name    string `json:"name,omitempty"`
		Key     string `json:"key,omitempty"`
		Example bool   `json:"example,omitempty"` // the placeholder line of the template
		// Banned: the same account is in ban.txt too — whitelisted or not,
		// it cannot join.
		Banned bool `json:"banned,omitempty"`
	}
	out := []entry{}
	for _, b := range parseDayZBans(string(data)) {
		// A Steam64 line is known by the player ID it derives.
		e := entry{ID: b.ID, Comment: shownNote(b.Comment), Banned: banned[accountID(b.ID)]}
		if n, ok := names[accountID(b.ID)]; ok {
			e.Name, e.Key = n[0], n[1]
		}
		// Bohemia's template ships "1111111111112222222222222333333333XXXXXXAAAA // Example".
		e.Example = strings.Contains(strings.ToLower(b.Comment), "example") || strings.HasPrefix(b.ID, "111111111111")
		out = append(out, e)
	}
	writeJSON(w, map[string]interface{}{"enabled": h.whitelistEnabled(), "entries": out, "running": h.app.ServerIsRunning()})
}

func (h *handlers) whitelistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key    string `json:"key"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, name := "", ""
	if req.Key != "" {
		p, ok := h.playersDB().Get(req.Key)
		if !ok {
			http.Error(w, "unknown player", http.StatusNotFound)
			return
		}
		if !reDayZID.MatchString(p.ID) {
			http.Error(w, "this player has no ID in the admin log yet", http.StatusBadRequest)
			return
		}
		id, name = p.ID, p.Name
	} else {
		t := strings.TrimSpace(req.Target)
		switch {
		case reSteam64.MatchString(t):
			id = dayzIDFromSteam64(t)
		case reDayZID.MatchString(t):
			id = t
		default:
			http.Error(w, "enter a Steam64 ID (17 digits) or a player ID from the admin log (44 characters)", http.StatusBadRequest)
			return
		}
		if n, ok := h.namesByID()[id]; ok {
			name = n[0]
		}
	}
	h.bansMu.Lock()
	added, err := addDayZBan(h.whitelistPath(), id, panelNote(name))
	h.bansMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if added {
		h.app.Log.Printf("whitelist: added %s (%s)", nonEmpty(name, id), nonEmpty(h.actor(r), "panel"))
	}
	writeJSON(w, map[string]interface{}{"ok": true, "added": added, "id": id})
}

func (h *handlers) whitelistRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	// The line and any other one for the same account (its Steam64 ID or the
	// player ID derived from it), so "Remove" really takes the player off.
	id := strings.TrimSpace(req.ID)
	h.bansMu.Lock()
	removed, err := removeDayZLines(h.whitelistPath(), func(v string) bool { return sameDayZID(v, id) })
	h.bansMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if removed {
		h.app.Log.Printf("whitelist: removed %s (%s)", req.ID, nonEmpty(h.actor(r), "panel"))
	}
	writeJSON(w, map[string]interface{}{"ok": true, "removed": removed})
}

// whitelistEnable switches enableWhitelist in serverDZ.cfg (read at the next
// server start).
func (h *handlers) whitelistEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// serverDZ.cfg is read at start only, so it may change under a running
	// server (the switch then applies on the next start) — but never while
	// another panel write is in progress.
	h.app.WriteMu.Lock()
	defer h.app.WriteMu.Unlock()
	path := h.serverCfgPath()
	sc, err := config.LoadServerCfg(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := 0
	if req.On {
		v = 1
	}
	sc.Set("enableWhitelist", v)
	if err := sc.Save(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "enabled": req.On, "restartNeeded": h.app.ServerIsRunning()})
}
