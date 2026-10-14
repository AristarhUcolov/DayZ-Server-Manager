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
//
// Messages the manager itself sends (the Chat/RCon pages, per-player messages,
// scheduled announcements and restart warnings) never come back on the stream
// as player chat, so they are recorded at send time (expectEcho + recordSent) — otherwise
// the feed and the archive would show only half of the conversation. Lines
// another RCon tool sends ("RCon admin #1: (Global) …") are recorded as admin
// messages; our own echo of such a line is dropped.
package rcon

import (
	"regexp"
	"strings"
	"time"
)

const chatBufferMax = 300

// Chat message sources.
const (
	ChatFromPlayer  = ""        // a player in game
	ChatFromManager = "manager" // sent from this panel (or its scheduler)
	ChatFromAdmin   = "admin"   // another RCon client
)

// ChatMsg is one chat line.
type ChatMsg struct {
	At      int64  `json:"at"` // unix milliseconds
	Channel string `json:"channel"`
	Name    string `json:"name"`
	Text    string `json:"text"`
	Source  string `json:"source,omitempty"`
	To      string `json:"to,omitempty"` // direct message recipient
}

// reBEChat matches a BattlEye chat line `(Channel) Name: text`. System lines
// (connect/disconnect, "RCon admin ... logged in", kick notices) do NOT start
// with a parenthesised channel, so this prefix reliably identifies chat. Name is
// non-greedy up to the first ": " so message text may itself contain colons.
var reBEChat = regexp.MustCompile(`^\(([A-Za-z]+)\)\s+(.*?):\s(.*)$`)

// reAdminSay matches what BattlEye relays to RCon clients when an admin uses
// `say`: `RCon admin #0: (Global) text`.
var reAdminSay = regexp.MustCompile(`^RCon admin #(\d+):\s+\(([A-Za-z]+)\)\s(.*)$`)

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

func parseAdminLine(line string) (ChatMsg, bool) {
	m := reAdminSay.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return ChatMsg{}, false
	}
	return ChatMsg{At: time.Now().UnixMilli(), Channel: m[2], Name: "RCon admin #" + m[1],
		Text: m[3], Source: ChatFromAdmin}, true
}

// SetChatSink registers a callback that receives every new chat message
// (incoming and sent) — the persistent archive. It runs on the RCon reader
// goroutine and must not block.
func (m *Manager) SetChatSink(fn func(ChatMsg)) {
	m.chatMu.Lock()
	m.chatSink = fn
	m.chatMu.Unlock()
}

// SeedChat preloads the ring buffer (history from the archive at start-up) so
// the Chat page does not come up empty after a manager restart.
func (m *Manager) SeedChat(msgs []ChatMsg) {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	merged := append(append([]ChatMsg(nil), msgs...), m.chat...)
	if len(merged) > chatBufferMax {
		merged = merged[len(merged)-chatBufferMax:]
	}
	m.chat = merged
}

// onServerMessage is the Conn.OnMessage callback. Runs on the reader goroutine,
// so it must not block — a mutexed append to the ring buffer is fine. A single
// packet may carry more than one line.
func (m *Manager) onServerMessage(raw string) {
	var parsed []ChatMsg
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if msg, ok := parseAdminLine(line); ok {
			if m.isOwnEcho(msg.Text) {
				continue
			}
			parsed = append(parsed, msg)
			continue
		}
		if msg, ok := parseChatLine(line); ok {
			parsed = append(parsed, msg)
		}
	}
	m.appendChat(parsed...)
	m.emitEvents(raw)
}

// appendChat adds messages to the ring buffer and hands them to the sink.
func (m *Manager) appendChat(msgs ...ChatMsg) {
	if len(msgs) == 0 {
		return
	}
	m.chatMu.Lock()
	m.chat = append(m.chat, msgs...)
	if len(m.chat) > chatBufferMax {
		m.chat = append([]ChatMsg(nil), m.chat[len(m.chat)-chatBufferMax:]...)
	}
	sink := m.chatSink
	m.chatMu.Unlock()
	if sink != nil {
		for _, msg := range msgs {
			sink(msg)
		}
	}
}

// echoTTL is how long a delivered say's relay is still treated as our own echo.
const echoTTL = 20 * time.Second

// expectEcho remembers a message the manager is about to send so BattlEye's
// relay of it is not shown twice. It must run BEFORE the say goes out: the
// relay is parsed on the reader goroutine and can arrive before (or right
// behind) the command reply, i.e. before Command has returned. The entry stays
// valid while the send is in flight (Command may first queue behind slower
// commands on cmdMu) and for echoTTL after recordSent / until forgetEcho.
func (m *Manager) expectEcho(text string) {
	now := time.Now()
	m.chatMu.Lock()
	keep := m.sentEcho[:0]
	for _, e := range m.sentEcho {
		if e.pending || now.Sub(e.at) < echoTTL {
			keep = append(keep, e)
		}
	}
	m.sentEcho = append(keep, sentEcho{text: strings.TrimSpace(text), at: now, pending: true})
	m.chatMu.Unlock()
}

// forgetEcho drops one in-flight expectEcho entry for a say that failed to
// send, so the same line from another admin is not swallowed.
func (m *Manager) forgetEcho(text string) {
	text = strings.TrimSpace(text)
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	for i := len(m.sentEcho) - 1; i >= 0; i-- {
		if e := m.sentEcho[i]; e.pending && e.text == text {
			m.sentEcho = append(m.sentEcho[:i], m.sentEcho[i+1:]...)
			return
		}
	}
}

// recordSent records a delivered message in the feed/archive as sent by the
// manager. Its echo must already be registered with expectEcho; if the relay
// has not been seen yet, the entry now expires echoTTL after delivery.
func (m *Manager) recordSent(channel, to, text string) {
	now := time.Now()
	t := strings.TrimSpace(text)
	m.chatMu.Lock()
	for i := range m.sentEcho {
		if e := &m.sentEcho[i]; e.pending && e.text == t {
			e.pending, e.at = false, now
			break
		}
	}
	m.chatMu.Unlock()
	m.appendChat(ChatMsg{At: now.UnixMilli(), Channel: channel, Text: text, Source: ChatFromManager, To: to})
}

func (m *Manager) isOwnEcho(text string) bool {
	text = strings.TrimSpace(text)
	now := time.Now()
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	for i, e := range m.sentEcho {
		if e.text == text && (e.pending || now.Sub(e.at) < echoTTL) {
			m.sentEcho = append(m.sentEcho[:i], m.sentEcho[i+1:]...)
			return true
		}
	}
	return false
}

type sentEcho struct {
	text    string
	at      time.Time // registration, then delivery time
	pending bool      // say still in flight (never expires while set)
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

// sanitizeSay makes a message safe for one RCon `say` command: BattlEye takes
// the rest of the line as the text, so line breaks (which DayZ chat cannot
// show anyway) become spaces.
func sanitizeSay(msg string) string {
	msg = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg)
	return strings.TrimSpace(msg)
}
