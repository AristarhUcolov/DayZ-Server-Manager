// Copyright (c) 2026 Aristarh Ucolov.
//
// Discord webhook client: plain messages and embeds, an optional ping, the
// webhook's name/avatar override, editing a message the webhook posted (the
// live status message), and Discord's rate limits (HTTP 429 → wait the time
// Discord asks for, then retry). Notifications are best-effort and must never
// block or crash server management — callers run them off the main path.
package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var client = &http.Client{Timeout: 10 * time.Second}

// Message is one webhook post.
type Message struct {
	Content         string           `json:"content,omitempty"`
	Username        string           `json:"username,omitempty"`
	AvatarURL       string           `json:"avatar_url,omitempty"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// Embed is a Discord embed (the coloured card).
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

type EmbedFooter struct {
	Text string `json:"text"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// AllowedMentions limits who a message may ping. Without it a player name like
// "@everyone" in a relayed chat line would ping the whole server.
type AllowedMentions struct {
	Parse []string `json:"parse"`
	Roles []string `json:"roles,omitempty"`
	Users []string `json:"users,omitempty"`
}

// Colours of the embeds.
const (
	ColorOK    = 0x3ecf8e
	ColorInfo  = 0x5fa8fb
	ColorWarn  = 0xf5b740
	ColorError = 0xfb5d63
	ColorMuted = 0x8a94ab
	ColorAdmin = 0xff7a2b
)

// ValidWebhook checks that s looks like a Discord webhook URL, so a typo is
// reported when it is saved instead of failing silently at every event.
func ValidWebhook(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("discord: webhook URL is empty")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" {
		return errors.New("discord: the webhook URL must start with https://")
	}
	host := strings.ToLower(u.Hostname())
	okHost := host == "discord.com" || host == "discordapp.com" || strings.HasSuffix(host, ".discord.com") || strings.HasSuffix(host, ".discordapp.com")
	if !okHost || !strings.HasPrefix(u.Path, "/api/webhooks/") {
		return errors.New("discord: this is not a webhook URL (Discord → channel settings → Integrations → Webhooks → Copy Webhook URL)")
	}
	if len(strings.Split(strings.Trim(strings.TrimPrefix(u.Path, "/api/webhooks/"), "/"), "/")) < 2 {
		return errors.New("discord: the webhook URL is incomplete (it ends with /<id>/<token>)")
	}
	return nil
}

// Discord posts a plain-content message (kept for simple callers).
func Discord(webhookURL, content string) error {
	return Post(webhookURL, Message{Content: content})
}

// Post sends a message to the webhook.
func Post(webhookURL string, m Message) error {
	_, err := send(http.MethodPost, webhookURL, m)
	return err
}

// PostWait sends a message and returns its id (for later edits).
func PostWait(webhookURL string, m Message) (string, error) {
	u := webhookURL
	if strings.Contains(u, "?") {
		u += "&wait=true"
	} else {
		u += "?wait=true"
	}
	body, err := send(http.MethodPost, u, m)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.ID == "" {
		return "", errors.New("discord: no message id in the reply")
	}
	return resp.ID, nil
}

// ErrUnknownMessage means an edited message no longer exists (deleted in
// Discord): post a new one.
var ErrUnknownMessage = errors.New("discord: the message no longer exists")

// Edit replaces a message the webhook posted.
func Edit(webhookURL, messageID string, m Message) error {
	base := webhookURL
	query := ""
	if i := strings.Index(base, "?"); i >= 0 {
		base, query = base[:i], base[i:]
	}
	_, err := send(http.MethodPatch, strings.TrimRight(base, "/")+"/messages/"+url.PathEscape(messageID)+query, m)
	return err
}

func send(method, webhookURL string, m Message) ([]byte, error) {
	if err := ValidWebhook(stripQuery(webhookURL)); err != nil {
		return nil, err
	}
	m = clamp(m)
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(method, webhookURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "DayZServerManager (https://github.com/AristarhUcolov, 1.0)")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("discord: %w", err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests && attempt < 3:
			// Rate limited: wait as long as Discord asks (capped), then retry.
			time.Sleep(retryAfter(resp, data))
			continue
		case resp.StatusCode == http.StatusNotFound && method == http.MethodPatch:
			return nil, ErrUnknownMessage
		case resp.StatusCode >= 300:
			return nil, fmt.Errorf("discord: webhook returned HTTP %d%s", resp.StatusCode, discordReason(data))
		}
		return data, nil
	}
}

func stripQuery(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		return u[:i]
	}
	return u
}

// retryAfter is how long a 429 asks to wait (JSON retry_after in seconds, or
// the Retry-After header), between 0.5 and 30 seconds.
func retryAfter(resp *http.Response, body []byte) time.Duration {
	var r struct {
		RetryAfter float64 `json:"retry_after"`
	}
	d := time.Duration(0)
	if json.Unmarshal(body, &r) == nil && r.RetryAfter > 0 {
		d = time.Duration(r.RetryAfter * float64(time.Second))
	} else if s := resp.Header.Get("Retry-After"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			d = time.Duration(f * float64(time.Second))
		}
	}
	if d < 500*time.Millisecond {
		d = 500 * time.Millisecond
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// discordReason extracts Discord's error message ({"message": "..."}).
func discordReason(body []byte) string {
	var r struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &r) == nil && r.Message != "" {
		return " (" + r.Message + ")"
	}
	return ""
}

// clamp keeps a message inside Discord's limits (content 2000, embed title
// 256, description 4096, field 1024) — cutting on a character boundary, never
// inside a multi-byte one.
func clamp(m Message) Message {
	m.Content = cut(m.Content, 1990)
	m.Username = cut(m.Username, 80)
	if len(m.Embeds) > 10 {
		m.Embeds = m.Embeds[:10]
	}
	for i := range m.Embeds {
		e := &m.Embeds[i]
		e.Title = cut(e.Title, 250)
		e.Description = cut(e.Description, 4000)
		if e.Footer != nil {
			e.Footer.Text = cut(e.Footer.Text, 2000)
		}
		if len(e.Fields) > 25 {
			e.Fields = e.Fields[:25]
		}
		for j := range e.Fields {
			e.Fields[j].Name = cut(e.Fields[j].Name, 250)
			e.Fields[j].Value = cut(e.Fields[j].Value, 1020)
		}
	}
	return m
}

// cut shortens s to at most max characters (runes), adding "…".
func cut(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// Escape makes text safe to show in Discord: markdown and mentions are
// neutralised (a player called "@everyone" or "**x**" is shown as written).
func Escape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`, `*`, `\*`, `_`, `\_`, "`", "\\`", `~`, `\~`, `|`, `\|`, `>`, `\>`,
		"@", "@​", "#", "#​",
	)
	return r.Replace(s)
}
