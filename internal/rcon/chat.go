// Copyright (c) 2026 Aristarh Ucolov.
//
// Live in-game chat, captured from the BattlEye RCon message stream.
//
// BattlEye pushes unsolicited "server message" packets (type 0x02) to every
// logged-in RCon client — this is the ONLY real-time feed the manager gets, and
// it carries chat lines like `(Global) Survivor: hello`. Vanilla DayZ never
// writes chat to the .ADM admin log, so this stream is the only source. The
// stream is wired via Conn.OnMessage → Manager.onServerMessage (see manager.go
// Connect), parsed here, and kept in a bounded ring buffer the Chat page polls.
package rcon

import (
	"regexp"
	"strings"
	"time"
)

const chatBufferMax = 300

// ChatMsg is one parsed in-game chat line.
type ChatMsg struct {
	At      int64  `json:"at"` // unix milliseconds
	Channel string `json:"channel"`
	Name    string `json:"name"`
	Text    string `json:"text"`
}

// reBEChat matches a BattlEye chat line `(Channel) Name: text`. System lines
// (connect/disconnect, "RCon admin ... logged in", kick notices) do NOT start
// with a parenthesised channel, so this prefix reliably identifies chat. Name is
// non-greedy up to the first ": " so message text may itself contain colons.
var reBEChat = regexp.MustCompile(`^\(([A-Za-z]+)\)\s+(.*?):\s(.*)$`)

func parseChatLine(line string) (ChatMsg, bool) {
	m := reBEChat.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return ChatMsg{}, false
	}
	name := strings.TrimSpace(m[2])
	if name == "" {
		return ChatMsg{}, false
	}
	return ChatMsg{At: time.Now().UnixMilli(), Channel: m[1], Name: name, Text: m[3]}, true
}

// onServerMessage is the Conn.OnMessage callback. Runs on the reader goroutine,
// so it must not block — a mutexed append to the ring buffer is fine. A single
// packet may carry more than one line.
func (m *Manager) onServerMessage(raw string) {
	var parsed []ChatMsg
	for _, line := range strings.Split(raw, "\n") {
		if msg, ok := parseChatLine(strings.TrimRight(line, "\r")); ok {
			parsed = append(parsed, msg)
		}
	}
	if len(parsed) == 0 {
		return
	}
	m.chatMu.Lock()
	m.chat = append(m.chat, parsed...)
	if len(m.chat) > chatBufferMax {
		m.chat = m.chat[len(m.chat)-chatBufferMax:]
	}
	m.chatMu.Unlock()
}

// RecentChat returns the newest up-to-limit chat messages, oldest first.
func (m *Manager) RecentChat(limit int) []ChatMsg {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	if limit <= 0 || limit > len(m.chat) {
		limit = len(m.chat)
	}
	out := make([]ChatMsg, limit)
	copy(out, m.chat[len(m.chat)-limit:])
	return out
}
