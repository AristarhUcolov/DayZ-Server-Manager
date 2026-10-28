// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server-wide player actions on the Players page: kick everyone and lock or
// unlock joining (BattlEye #lock / #unlock). BattlEye does not report whether
// the server is locked, so the panel remembers its own lock — until the
// server starts again, since every run starts unlocked.

type joinLock struct {
	mu sync.Mutex
	at time.Time // when the panel locked joining; zero = not locked by the panel
}

// joinLocked reports whether the panel locked joining in the current run.
func (h *handlers) joinLocked() (bool, time.Time) {
	h.lock.mu.Lock()
	defer h.lock.mu.Unlock()
	if h.lock.at.IsZero() {
		return false, time.Time{}
	}
	if !h.app.ServerIsRunning() || h.app.Server.StartedAt().After(h.lock.at) {
		h.lock.at = time.Time{}
		return false, time.Time{}
	}
	return true, h.lock.at
}

func (h *handlers) registerPlayersAdmin(mux *http.ServeMux) {
	mux.HandleFunc("/api/players/kickall", methods(h.playersKickAll, http.MethodPost))
	mux.HandleFunc("/api/players/lock", methods(h.playersLock, http.MethodPost))
}

// playersKickAll kicks every player online with one reason — before
// maintenance, or to make everyone reconnect. Each kick lands in the player's
// moderation history.
func (h *handlers) playersKickAll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.app.ServerIsRunning() {
		authError(w, http.StatusConflict, "players.kickAll.notRunning", nil)
		return
	}
	ps, err := h.app.RCon.Players()
	if err != nil {
		rconFail(w, err)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	keys := h.keysByGUID()
	by := h.actor(r)
	n := 0
	var firstErr error
	for _, p := range ps {
		if err := h.app.RCon.Kick(p.ID, reason); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
		h.logModeration(modEvent{At: time.Now().Unix(), Action: "kick", Key: keys[strings.ToLower(p.GUID)], Name: p.Name, Reason: reason, By: by})
	}
	h.app.RCon.InvalidatePlayers()
	if n == 0 && firstErr != nil {
		rconFail(w, firstErr)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "kicked": n, "online": len(ps)})
}

// playersLock locks ({"lock": true}) or unlocks joining.
func (h *handlers) playersLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lock bool `json:"lock"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.app.ServerIsRunning() {
		authError(w, http.StatusConflict, "players.kickAll.notRunning", nil)
		return
	}
	if err := h.app.RCon.Lock(req.Lock); err != nil {
		rconFail(w, err)
		return
	}
	h.lock.mu.Lock()
	if req.Lock {
		h.lock.at = time.Now()
	} else {
		h.lock.at = time.Time{}
	}
	h.lock.mu.Unlock()
	writeJSON(w, map[string]interface{}{"ok": true, "locked": req.Lock})
}

// keysByGUID maps the BattlEye GUIDs the player database knows (lower case)
// to the players' keys.
func (h *handlers) keysByGUID() map[string]string {
	list, _ := h.playersDB().Snapshot(0)
	out := make(map[string]string, len(list))
	for _, p := range list {
		if p.BEGUID != "" {
			out[strings.ToLower(p.BEGUID)] = p.Key
		}
	}
	return out
}
