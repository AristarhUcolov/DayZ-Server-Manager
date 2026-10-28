// Copyright (c) 2026 Aristarh Ucolov.
//
// Per-player profile — a small admin panel for one player: identity (the
// admin log's player ID, the BattlEye GUID and last IP learnt through RCon),
// stats from the player database, their recent combat events, the current ban
// (bans.go) and their moderation history, and the live session number when
// they're online (for kick / message).
package web

import (
	"net/http"
	"os"
	"strings"
	"time"

	"dayzmanager/internal/players"
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

	dayzID := ""
	if reDayZID.MatchString(pl.ID) {
		dayzID = pl.ID
	}
	ban := h.banFor(dayzID, pl.BEGUID)

	// Online: matched by BattlEye GUID first (exact), else by name.
	online, onlineNo, ping, ip := false, -1, 0, ""
	if h.app.ServerIsRunning() && h.app.RCon.Configured() {
		for _, p := range h.app.RCon.PlayersFresh(10 * time.Second) {
			byGUID := pl.BEGUID != "" && strings.EqualFold(p.GUID, pl.BEGUID)
			if byGUID || (pl.BEGUID == "" && p.Name == pl.Name) {
				online, onlineNo, ping, ip = true, p.ID, p.Ping, p.IP
				if byGUID {
					break
				}
			}
		}
	}
	pl.Online = online

	out := map[string]interface{}{
		"player":   pl,
		"events":   events,
		"banned":   ban != nil,
		"onlineId": onlineNo,
		"history":  h.modHistory(pl.Key, pl.Name),
		"canBan":   dayzID != "" || pl.BEGUID != "",
	}
	if ban != nil {
		out["ban"] = ban
	}
	if dayzID != "" {
		if data, err := os.ReadFile(h.whitelistPath()); err == nil {
			for _, e := range parseDayZBans(string(data)) {
				if sameDayZID(e.ID, dayzID) { // a Steam64 line counts too
					out["whitelisted"] = true
				}
			}
		}
	}
	if online {
		out["ping"], out["onlineIp"] = ping, ip
	}
	// Other players last seen from the same address: a second account, a
	// family, a LAN party — for the admin to judge, e.g. after a ban.
	if addr := firstNonEmpty(ip, pl.LastIP); addr != "" {
		var same []map[string]interface{}
		for _, o := range list {
			if o.Key != pl.Key && o.LastIP == addr {
				same = append(same, map[string]interface{}{"key": o.Key, "name": o.Name, "lastSeen": o.LastSeen})
			}
		}
		if len(same) > 0 {
			out["sameIp"] = same
		}
	}
	// What they built, placed, destroyed or buried, newest first.
	var built []players.BaseEvent
	all := db.Bases()
	for i := len(all) - 1; i >= 0 && len(built) < 40; i-- {
		if all[i].Key == pl.Key {
			built = append(built, all[i])
		}
	}
	if len(built) > 0 {
		out["bases"] = built
	}
	writeJSON(w, out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
