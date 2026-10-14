// Copyright (c) 2026 Aristarh Ucolov.
package app

// Discord notifications: which events exist, how each becomes a message, and
// the queue that posts them in order without ever making the caller wait.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/notify"
)

// DiscordEvent describes one notification the panel can post.
type DiscordEvent struct {
	Key     string `json:"key"`
	Group   string `json:"group"`             // "server" | "players" | "admin"
	Default bool   `json:"default"`           // on unless switched off
	Alert   bool   `json:"alert,omitempty"`   // pings DiscordMention
	Players bool   `json:"players,omitempty"` // goes to the player-activity webhook when one is set
}

// DiscordCatalog is every event, in the order the settings show them.
var DiscordCatalog = []DiscordEvent{
	{Key: "online", Group: "server", Default: true},
	{Key: "started", Group: "server"},
	{Key: "stopped", Group: "server", Default: true},
	{Key: "restart", Group: "server", Default: true},
	{Key: "restartplanned", Group: "server"},
	{Key: "crashed", Group: "server", Default: true, Alert: true},
	{Key: "restartfailed", Group: "server", Default: true, Alert: true},
	{Key: "crashloop", Group: "server", Default: true, Alert: true},
	{Key: "modupdate", Group: "server", Default: true},
	{Key: "backup", Group: "server", Default: true},
	{Key: "wipe", Group: "server", Default: true},
	{Key: "join", Group: "players", Players: true},
	{Key: "leave", Group: "players", Players: true},
	{Key: "watch", Group: "players", Default: true, Players: true},
	{Key: "kill", Group: "players", Players: true},
	{Key: "chat", Group: "players", Players: true},
	{Key: "ban", Group: "admin", Default: true},
	{Key: "unban", Group: "admin", Default: true},
	{Key: "kick", Group: "admin"},
}

// DiscordStyle is the emoji and colour an event's messages carry (the emoji
// leads every message title) — the settings page shows the same.
func DiscordStyle(key string) (icon string, color int) {
	title := i18n.Text("en", "discord."+key)
	if i := strings.IndexByte(title, ' '); i > 0 {
		icon = title[:i]
	}
	return icon, eventColor[key]
}

func discordEvent(key string) (DiscordEvent, bool) {
	for _, e := range DiscordCatalog {
		if e.Key == key {
			return e, true
		}
	}
	return DiscordEvent{}, false
}

// discordOn reports whether event is switched on in cfg.
func discordOn(cfg *config.Manager, ev DiscordEvent) bool {
	if v, ok := cfg.DiscordEvents[ev.Key]; ok {
		return v
	}
	return ev.Default
}

// ------------------------------------------------------------------ queue

type discordJob struct {
	webhook string
	msg     notify.Message
	// Lines of a batchable event (chat, joins, kills): consecutive jobs of the
	// same batch and webhook are merged into one message, so a busy server
	// does not run into Discord's rate limit.
	batch string
	line  string
}

type discordQueue struct {
	ch      chan discordJob
	lastErr time.Time
}

func (a *App) discordQ() *discordQueue {
	a.dqOnce.Do(func() {
		a.dq = &discordQueue{ch: make(chan discordJob, 300)}
		go a.discordWorker(a.dq)
	})
	return a.dq
}

func (a *App) enqueue(j discordJob) {
	q := a.discordQ()
	select {
	case q.ch <- j:
	default:
		if time.Since(q.lastErr) > time.Minute {
			q.lastErr = time.Now()
			a.Log.Printf("discord: queue full — notifications dropped")
		}
	}
}

func (a *App) discordWorker(q *discordQueue) {
	var held *discordJob
	for {
		var j discordJob
		if held != nil {
			j, held = *held, nil
		} else {
			select {
			case <-a.stop:
				return
			case j = <-q.ch:
			}
		}
		if j.batch != "" {
			lines := []string{j.line}
			deadline := time.After(1500 * time.Millisecond)
		collect:
			for len(lines) < 20 {
				select {
				case n := <-q.ch:
					if n.batch == j.batch && n.webhook == j.webhook {
						lines = append(lines, n.line)
						continue
					}
					held = &n
					break collect
				case <-deadline:
					break collect
				}
			}
			if len(j.msg.Embeds) > 0 {
				j.msg.Embeds[0].Description = strings.Join(lines, "\n")
			} else {
				j.msg.Content = strings.Join(lines, "\n")
			}
		}
		if err := notify.Post(j.webhook, j.msg); err != nil && time.Since(q.lastErr) > time.Minute {
			q.lastErr = time.Now()
			a.Log.Printf("discord: %v", err)
		}
		// Gentle pacing: webhooks allow about 5 posts per 2 seconds.
		time.Sleep(450 * time.Millisecond)
	}
}

// ------------------------------------------------------------------ texts

// discordLang is the language of the messages.
func discordLang(cfg *config.Manager) string {
	if cfg.DiscordLanguage != "" {
		return cfg.DiscordLanguage
	}
	return cfg.Language
}

// dtext is one message string in language lang.
func dtext(lang, key string) string {
	if s := i18n.Text(lang, key); s != "" {
		return s
	}
	return key
}

func fill(s string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

var eventColor = map[string]int{
	"online": notify.ColorOK, "started": notify.ColorInfo, "stopped": notify.ColorMuted,
	"restart": notify.ColorInfo, "restartplanned": notify.ColorWarn, "crashed": notify.ColorError,
	"restartfailed": notify.ColorError, "crashloop": notify.ColorError, "modupdate": notify.ColorInfo,
	"backup": notify.ColorMuted, "wipe": notify.ColorWarn, "join": notify.ColorOK, "leave": notify.ColorMuted,
	"watch": notify.ColorWarn, "kill": notify.ColorError, "chat": notify.ColorInfo,
	"ban": notify.ColorAdmin, "unban": notify.ColorOK, "kick": notify.ColorAdmin, "test": notify.ColorOK,
}

// discordBase is the frame of every message: name/avatar override, footer
// with the server's name, timestamp, colour, and no pings unless asked.
func (a *App) discordBase(cfg *config.Manager, event, title string) notify.Message {
	m := notify.Message{
		Username:        strings.TrimSpace(cfg.DiscordUsername),
		AvatarURL:       strings.TrimSpace(cfg.DiscordAvatarURL),
		AllowedMentions: &notify.AllowedMentions{Parse: []string{}},
		Embeds: []notify.Embed{{
			Title:     title,
			Color:     eventColor[event],
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Footer:    &notify.EmbedFooter{Text: a.Label() + " · DayZ Server Manager"},
		}},
	}
	return m
}

var reDigits = regexp.MustCompile(`^\d{5,25}$`)

// mention turns DiscordMention into the content and allowed mentions of an
// alert.
func mention(spec string) (string, *notify.AllowedMentions) {
	spec = strings.TrimSpace(spec)
	switch {
	case spec == "":
		return "", nil
	case strings.EqualFold(spec, "here") || spec == "@here":
		return "@here", &notify.AllowedMentions{Parse: []string{"everyone"}}
	case strings.EqualFold(spec, "everyone") || spec == "@everyone":
		return "@everyone", &notify.AllowedMentions{Parse: []string{"everyone"}}
	}
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(spec, "<@&"), "<@!"), "<@"), ">")
	if !reDigits.MatchString(id) {
		return "", nil
	}
	if strings.HasPrefix(spec, "<@") && !strings.HasPrefix(spec, "<@&") {
		return "<@" + id + ">", &notify.AllowedMentions{Parse: []string{}, Users: []string{id}}
	}
	return "<@&" + id + ">", &notify.AllowedMentions{Parse: []string{}, Roles: []string{id}}
}

// ValidDiscordMention checks the DiscordMention setting.
func ValidDiscordMention(spec string) bool {
	if strings.TrimSpace(spec) == "" {
		return true
	}
	s, _ := mention(spec)
	return s != ""
}

// Notify posts a server event (see DiscordCatalog) with an optional detail.
// Never blocks: the message is queued.
func (a *App) Notify(event, detail string) {
	cfg := a.Cfg()
	ev, ok := discordEvent(event)
	if !ok || !cfg.DiscordEnabled || !discordOn(&cfg, ev) {
		return
	}
	hook := strings.TrimSpace(cfg.DiscordWebhookURL)
	if ev.Players && strings.TrimSpace(cfg.DiscordPlayersWebhookURL) != "" {
		hook = strings.TrimSpace(cfg.DiscordPlayersWebhookURL)
	}
	if hook == "" {
		return
	}
	b := discordLang(&cfg)
	title, desc := a.discordTexts(b, event, detail)
	m := a.discordBase(&cfg, event, title)
	m.Embeds[0].Description = desc
	if ev.Alert {
		if content, am := mention(cfg.DiscordMention); content != "" {
			m.Content, m.AllowedMentions = content, am
		}
	}
	j := discordJob{webhook: hook, msg: m}
	switch event {
	case "chat", "join", "leave", "kill":
		// One line per event; the worker merges a burst into one message.
		j.batch, j.line = event, desc
		m.Embeds[0].Description = ""
		j.msg = m
	}
	a.enqueue(j)
}

// NotifyDiscord is the old name of Notify without a detail.
func (a *App) NotifyDiscord(event string) { a.Notify(event, "") }

// NotifyModeration posts a ban, unban, expired ban or kick.
func (a *App) NotifyModeration(action, name, reason, by string, until int64) {
	event := action
	if action == "expired" {
		event = "unban"
	}
	parts := []string{action, name, reason, by, strconv.FormatInt(until, 10)}
	a.Notify(event, strings.Join(parts, "\x1f"))
}

// discordTexts builds the title and description of an event.
func (a *App) discordTexts(b, event, detail string) (string, string) {
	title := dtext(b, "discord."+event)
	f := strings.Split(detail, "\x1f")
	at := func(i int) string {
		if i < len(f) {
			return strings.TrimSpace(f[i])
		}
		return ""
	}
	esc := notify.Escape
	switch event {
	case "online":
		// players|max|map
		var parts []string
		if at(1) != "" {
			parts = append(parts, fill(dtext(b, "discord.f.players"), "n", at(0), "max", at(1)))
		}
		if at(2) != "" {
			parts = append(parts, fill(dtext(b, "discord.f.map"), "map", esc(at(2))))
		}
		return title, strings.Join(parts, " · ")
	case "restart", "restartplanned":
		// reason|minutes
		reason := at(0)
		rtext := dtext(b, "restart.reason."+reason)
		if reason == "" || rtext == "restart.reason."+reason {
			rtext = reason
		}
		desc := ""
		if rtext != "" {
			desc = fill(dtext(b, "discord.f.reason"), "reason", rtext)
		}
		if event == "restartplanned" && at(1) != "" {
			title = fill(dtext(b, "discord.restartplanned.in"), "m", at(1))
		}
		return title, desc
	case "crashed":
		desc := esc(at(0))
		if at(1) == "watchdog" {
			desc = strings.TrimSpace(desc + "\n" + dtext(b, "discord.f.watchdog"))
		}
		return title, desc
	case "restartfailed", "modupdate", "backup", "wipe":
		return title, esc(at(0))
	case "join", "leave", "watch":
		return title, fill(dtext(b, "discord.line."+event), "name", "**"+esc(at(0))+"**")
	case "kill":
		// killer|victim|weapon|distance
		line := fill(dtext(b, "discord.line.kill"), "killer", "**"+esc(at(0))+"**", "victim", "**"+esc(at(1))+"**")
		var extra []string
		if at(2) != "" {
			extra = append(extra, esc(at(2)))
		}
		if at(3) != "" {
			extra = append(extra, at(3)+" m")
		}
		if len(extra) > 0 {
			line += " (" + strings.Join(extra, ", ") + ")"
		}
		return title, line
	case "chat":
		// channel|name|text
		return title, fmt.Sprintf("`%s` **%s**: %s", esc(at(0)), esc(at(1)), esc(at(2)))
	case "ban", "unban", "kick":
		// action|name|reason|by|until
		action, name, reason, by := at(0), at(1), at(2), at(3)
		if action == "expired" {
			title = dtext(b, "discord.expired")
		}
		if name == "" {
			name = "—"
		}
		lines := []string{"**" + esc(name) + "**"}
		if event == "ban" {
			until, _ := strconv.ParseInt(at(4), 10, 64)
			if until > 0 {
				lines = append(lines, fill(dtext(b, "discord.f.until"), "until", "<t:"+strconv.FormatInt(until, 10)+":f>"))
			} else {
				lines = append(lines, dtext(b, "discord.f.permanent"))
			}
		}
		if reason != "" {
			lines = append(lines, fill(dtext(b, "discord.f.reason"), "reason", esc(reason)))
		}
		if by != "" {
			lines = append(lines, fill(dtext(b, "discord.f.by"), "by", esc(by)))
		}
		return title, strings.Join(lines, "\n")
	}
	return title, esc(detail)
}

// DiscordTest posts a test message to url with the given settings (from the
// settings form, before they are saved).
func (a *App) DiscordTest(url string, cfg config.Manager) error {
	b := discordLang(&cfg)
	m := a.discordBase(&cfg, "test", dtext(b, "discord.test"))
	m.Embeds[0].Description = dtext(b, "discord.test.body")
	if content, am := mention(cfg.DiscordMention); content != "" {
		m.Content, m.AllowedMentions = content, am
	}
	return notify.Post(url, m)
}

// ------------------------------------------------------- live status message

type discordStatusFile struct {
	Hook      string `json:"hook"` // sha256 of the webhook URL it belongs to
	MessageID string `json:"messageId"`
}

type statusState struct {
	mu   sync.Mutex
	last string // fingerprint of the last posted state
	at   time.Time
}

func (a *App) statusPath() string { return filepath.Join(a.ManagerDir, "discord-status.json") }

func hookHash(u string) string {
	s := sha256.Sum256([]byte(u))
	return hex.EncodeToString(s[:8])
}

// discordStatusLoop keeps the live status message up to date: on a change of
// state at once (checked every 30 s), else every 5 minutes.
func (a *App) discordStatusLoop() {
	for {
		select {
		case <-a.stop:
			return
		case <-time.After(30 * time.Second):
		}
		a.updateDiscordStatus(false)
	}
}

// UpdateDiscordStatus refreshes the status message now (after a settings save).
func (a *App) UpdateDiscordStatus() { go a.updateDiscordStatus(true) }

func (a *App) updateDiscordStatus(force bool) {
	cfg := a.Cfg()
	hook := strings.TrimSpace(cfg.DiscordWebhookURL)
	if !cfg.DiscordEnabled || !cfg.DiscordStatus || hook == "" {
		return
	}
	a.status.mu.Lock()
	defer a.status.mu.Unlock()
	b := discordLang(&cfg)
	m, fp := a.statusMessage(&cfg, b)
	if !force && fp == a.status.last && time.Since(a.status.at) < 5*time.Minute {
		return
	}
	var st discordStatusFile
	if data, err := os.ReadFile(a.statusPath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Hook != hookHash(hook) {
		st = discordStatusFile{Hook: hookHash(hook)}
	}
	var err error
	if st.MessageID != "" {
		err = notify.Edit(hook, st.MessageID, m)
		if err == notify.ErrUnknownMessage {
			st.MessageID = ""
		}
	}
	if st.MessageID == "" {
		var id string
		if id, err = notify.PostWait(hook, m); err == nil {
			st.MessageID = id
			if data, e := json.Marshal(st); e == nil {
				_ = os.WriteFile(a.statusPath(), data, 0o644)
			}
		}
	}
	if err != nil {
		a.Log.Printf("discord status message: %v", err)
		return
	}
	a.status.last, a.status.at = fp, time.Now()
}

// statusMessage builds the live status embed and a fingerprint of what it
// shows (minus the clock), to post only on a change.
func (a *App) statusMessage(cfg *config.Manager, b string) (notify.Message, string) {
	running := a.ServerIsRunning()
	q := a.Query()
	state, color := "offline", notify.ColorMuted
	switch {
	case running && q.OK:
		state, color = "online", notify.ColorOK
	case running:
		state, color = "starting", notify.ColorWarn
	}
	m := a.discordBase(cfg, "online", fill(dtext(b, "discord.status."+state), "name", a.Label()))
	e := &m.Embeds[0]
	e.Color = color
	fp := state
	if running && q.OK && q.Info != nil {
		e.Fields = append(e.Fields,
			notify.EmbedField{Name: dtext(b, "discord.status.players"), Value: fmt.Sprintf("%d / %d", q.Info.Players, q.Info.MaxPlayers), Inline: true},
			notify.EmbedField{Name: dtext(b, "discord.status.map"), Value: notify.Escape(q.Info.Map), Inline: true},
		)
		fp += fmt.Sprintf("|%d|%d|%s", q.Info.Players, q.Info.MaxPlayers, q.Info.Map)
		if q.Info.Name != "" {
			e.Description = notify.Escape(q.Info.Name)
		}
	}
	if running {
		if up := a.Server.Uptime(); up > 0 {
			e.Fields = append(e.Fields, notify.EmbedField{Name: dtext(b, "discord.status.since"),
				Value: "<t:" + strconv.FormatInt(time.Now().Add(-up).Unix(), 10) + ":R>", Inline: true})
		}
		if plan := a.Server.NextRestart(); plan != nil {
			e.Fields = append(e.Fields, notify.EmbedField{Name: dtext(b, "discord.status.restart"),
				Value: "<t:" + strconv.FormatInt(plan.At.Unix(), 10) + ":R>", Inline: true})
			fp += "|r" + strconv.FormatInt(plan.At.Unix(), 10)
		}
	}
	e.Fields = append(e.Fields, notify.EmbedField{Name: dtext(b, "discord.status.updated"),
		Value: "<t:" + strconv.FormatInt(time.Now().Unix(), 10) + ":R>", Inline: true})
	return m, fp
}
