// Copyright (c) 2026 Aristarh Ucolov.
//
// Discord: the settings' test button and event list, and the live sources the
// notifications come from — the RCon message stream (chat, connects) and the
// live tail of the admin log (kills, watched players).
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"dayzmanager/internal/app"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/notify"
	"dayzmanager/internal/players"
	"dayzmanager/internal/rcon"
	"dayzmanager/internal/util"
)

func (h *handlers) registerDiscord(mux *http.ServeMux) {
	mux.HandleFunc("/api/discord/test", methods(h.discordTest, http.MethodPost))
	mux.HandleFunc("/api/discord/catalog", methods(h.discordCatalog, http.MethodGet))
}

// discordTest posts a test message with the settings from the form (not the
// saved ones), so a webhook can be checked before it is turned on.
func (h *handlers) discordTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		Mention  string `json:"mention"`
		Username string `json:"username"`
		Avatar   string `json:"avatar"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := notify.ValidWebhook(req.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !app.ValidDiscordMention(req.Mention) {
		http.Error(w, "mention: use here, everyone, or a role ID (digits)", http.StatusBadRequest)
		return
	}
	cfg := h.app.Cfg()
	cfg.DiscordMention, cfg.DiscordUsername, cfg.DiscordAvatarURL = req.Mention, req.Username, req.Avatar
	if req.Language != "" {
		cfg.DiscordLanguage = req.Language
	}
	if err := h.app.DiscordTest(strings.TrimSpace(req.URL), cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "sent"})
}

func (h *handlers) discordCatalog(w http.ResponseWriter, r *http.Request) {
	type item struct {
		app.DiscordEvent
		Icon  string `json:"icon"`
		Color string `json:"color"`
	}
	items := make([]item, 0, len(app.DiscordCatalog))
	for _, e := range app.DiscordCatalog {
		icon, color := app.DiscordStyle(e.Key)
		items = append(items, item{e, icon, fmt.Sprintf("#%06x", color)})
	}
	writeJSON(w, map[string]interface{}{"events": items, "languages": i18n.Languages()})
}

// onChat forwards in-game chat to Discord: players' lines on public channels
// (not private messages, not the panel's own announcements).
func (h *handlers) onChat(m rcon.ChatMsg) {
	if m.Source != rcon.ChatFromPlayer || strings.EqualFold(m.Channel, "Direct") || m.To != "" {
		return
	}
	h.app.Notify("chat", m.Channel+"\x1f"+m.Name+"\x1f"+m.Text)
}

// onRConEvent turns BattlEye's connect/disconnect lines into join/leave
// notifications — real time, while RCon is connected.
func (h *handlers) onRConEvent(ev rcon.ServerEvent) {
	switch ev.Kind {
	case "connect":
		h.app.Notify("join", ev.Name)
	case "disconnect":
		h.app.Notify("leave", ev.Name)
	case "guid":
		// Link the GUID early when the admin log already has the session.
		go util.SafeGo(h.app.Log, "link GUID", func() {
			h.playersDB().LinkRCon([]players.OnlineRef{{Name: ev.Name, GUID: ev.GUID}})
		})
	}
}

// onAdmEvent handles the live tail of the admin log: PvP kills, watched
// players connecting, and — without RCon — joins and leaves.
func (h *handlers) onAdmEvent(le players.LiveEvent) {
	ev := le.Event
	switch ev.Type {
	case "kill":
		if ev.Target != "" && ev.Target != ev.Player {
			h.app.Notify("kill", ev.Target+"\x1f"+ev.Player+"\x1f"+ev.Weapon+"\x1f"+ev.Distance)
		}
	case "connect":
		if le.Player.Watch {
			h.app.Notify("watch", ev.Player)
		}
		if !h.app.RCon.Configured() {
			h.app.Notify("join", ev.Player)
		}
	case "disconnect":
		if !h.app.RCon.Configured() {
			h.app.Notify("leave", ev.Player)
		}
	}
}

// notifyModeration reports a moderation action (ban, unban, expired, kick) to
// Discord when the server's notifications ask for it.
func (h *handlers) notifyModeration(action, name, reason, by string, until int64) {
	if h.app == nil {
		return
	}
	h.app.NotifyModeration(action, name, reason, by, until)
}
