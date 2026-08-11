// Copyright (c) 2026 Aristarh Ucolov.
//
// Per-player profile — a small admin panel for one player: identity and stats
// from the player database, their recent combat events from the killfeed, ban
// status, and the live session id when they're online (for kick / message).
// Ban and unban are persistent (a GUID line in bans.txt), applied live with
// RCon loadBans when the server is running.
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/players"
	"dayzmanager/internal/util"
)

func (h *handlers) playerProfile(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		http.Error(w, "player key required", http.StatusBadRequest)
		return
	}
	db := h.playersDB()
	db.Ingest(h.profilesAbs())
	list, kills := db.Snapshot(0)

	var pl *players.Player
	for i := range list {
		if list[i].Key == key {
			pl = &list[i]
			break
		}
	}
	if pl == nil {
		http.Error(w, "unknown player", http.StatusNotFound)
		return
	}

	// Combat events involving this player, matched by current name and aliases.
	names := map[string]bool{strings.ToLower(pl.Name): true}
	for _, a := range pl.Aliases {
		if a != "" {
			names[strings.ToLower(a)] = true
		}
	}
	events := make([]players.KillEvent, 0, 80)
	for _, k := range kills { // Snapshot returns newest-first
		if names[strings.ToLower(k.Killer)] || names[strings.ToLower(k.Victim)] {
			events = append(events, k)
			if len(events) >= 80 {
				break
			}
		}
	}

	banned := false
	banMinutes := ""
	if pl.ID != "" {
		if data, err := os.ReadFile(h.bansPath()); err == nil {
			bans, _ := parseBans(string(data))
			for _, b := range bans {
				if strings.EqualFold(strings.TrimSpace(b.ID), pl.ID) {
					banned = true
					banMinutes = strings.TrimSpace(b.Minutes)
					break
				}
			}
		}
	}

	online, onlineID := false, -1
	if h.app.ServerIsRunning() {
		for _, p := range h.app.RCon.PlayersFresh(10 * time.Second) {
			if (pl.ID != "" && strings.EqualFold(p.GUID, pl.ID)) || strings.EqualFold(p.Name, pl.Name) {
				online, onlineID = true, p.ID
				break
			}
		}
	}
	pl.Online = online

	writeJSON(w, map[string]interface{}{
		"player":     pl,
		"events":     events,
		"banned":     banned,
		"banMinutes": banMinutes,
		"onlineId":   onlineID,
	})
}

// playerBan adds a permanent GUID ban to bans.txt (idempotent) and, when the
// server is running, reloads bans and kicks the player if they're online.
func (h *handlers) playerBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GUID   string `json:"guid"`
		Reason string `json:"reason"`
		Days   int    `json:"days"` // 0 = permanent; otherwise the ban lifts after N days
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	guid := strings.TrimSpace(req.GUID)
	if guid == "" || strings.ContainsAny(guid, " \t") {
		http.Error(w, "valid GUID required", http.StatusBadRequest)
		return
	}
	// BattlEye's second column is minutes remaining ("0" = permanent). A day is
	// 1440 minutes; BE counts it down and lifts the ban when it hits zero.
	mins := "0"
	if req.Days > 0 {
		if req.Days > 3650 {
			req.Days = 3650
		}
		mins = strconv.Itoa(req.Days * 1440)
	}
	path := h.bansPath()
	data, _ := os.ReadFile(path)
	bans, comments := parseBans(string(data))
	for _, b := range bans {
		if strings.EqualFold(strings.TrimSpace(b.ID), guid) {
			writeJSON(w, map[string]interface{}{"status": "already-banned"})
			return
		}
	}
	bans = append(bans, banEntry{ID: guid, Minutes: mins, Reason: strings.TrimSpace(req.Reason)})
	if err := os.MkdirAll(h.beDir(), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := writeFileAtomic(path, banFileBytes(bans, comments)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded := false
	if h.app.ServerIsRunning() {
		if _, err := h.app.RCon.Command("loadBans"); err == nil {
			reloaded = true
		}
		for _, p := range h.app.RCon.PlayersFresh(10 * time.Second) {
			if strings.EqualFold(p.GUID, guid) {
				_ = h.app.RCon.Kick(p.ID, req.Reason)
				h.app.RCon.InvalidatePlayers()
				break
			}
		}
	}
	writeJSON(w, map[string]interface{}{"status": "banned", "reloaded": reloaded})
}

// playerUnban removes a GUID from bans.txt, applying it live when possible.
func (h *handlers) playerUnban(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GUID string `json:"guid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	guid := strings.TrimSpace(req.GUID)
	if guid == "" {
		http.Error(w, "GUID required", http.StatusBadRequest)
		return
	}
	path := h.bansPath()
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, map[string]interface{}{"status": "not-banned"})
		return
	}
	bans, comments := parseBans(string(data))
	kept := bans[:0]
	removed := false
	for _, b := range bans {
		if strings.EqualFold(strings.TrimSpace(b.ID), guid) {
			removed = true
			continue
		}
		kept = append(kept, b)
	}
	if !removed {
		writeJSON(w, map[string]interface{}{"status": "not-banned"})
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := writeFileAtomic(path, banFileBytes(kept, comments)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded := false
	if h.app.ServerIsRunning() {
		if _, err := h.app.RCon.Command("loadBans"); err == nil {
			reloaded = true
		}
	}
	writeJSON(w, map[string]interface{}{"status": "unbanned", "reloaded": reloaded})
}
