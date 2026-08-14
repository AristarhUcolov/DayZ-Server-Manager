// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import "testing"

func TestParseChatLine(t *testing.T) {
	chat := []struct {
		line, channel, name, text string
	}{
		{"(Global) Survivor: hello everyone", "Global", "Survivor", "hello everyone"},
		{"(Direct) Bob: where is the heli?", "Direct", "Bob", "where is the heli?"},
		{"(Vehicle) Big Boss: drive: left now", "Vehicle", "Big Boss", "drive: left now"}, // colon in text
		{"(Global) [ADMIN] Zed_Killer: воду нашли?", "Global", "[ADMIN] Zed_Killer", "воду нашли?"}, // unicode + brackets in name
	}
	for _, c := range chat {
		m, ok := parseChatLine(c.line)
		if !ok {
			t.Errorf("%q: not parsed as chat", c.line)
			continue
		}
		if m.Channel != c.channel || m.Name != c.name || m.Text != c.text {
			t.Errorf("%q → %+v, want channel=%q name=%q text=%q", c.line, m, c.channel, c.name, c.text)
		}
		if m.At == 0 {
			t.Errorf("%q: no timestamp", c.line)
		}
	}

	// System / non-chat lines must NOT be mistaken for chat.
	notChat := []string{
		"RCon admin #0 (127.0.0.1:2306) logged in",
		"Player #0 Survivor (76561198000000000) connected",
		"Verified GUID (abcd) of player #0 Survivor",
		"Player #1 Bob disconnected",
		"",
		"just some text",
	}
	for _, l := range notChat {
		if _, ok := parseChatLine(l); ok {
			t.Errorf("%q: wrongly parsed as chat", l)
		}
	}
}

func TestRecentChatRingBuffer(t *testing.T) {
	m := NewManager()
	for i := 0; i < chatBufferMax+50; i++ {
		m.onServerMessage("(Global) P: msg")
	}
	if got := len(m.RecentChat(0)); got != chatBufferMax {
		t.Errorf("ring buffer len = %d, want cap %d", got, chatBufferMax)
	}
	if got := len(m.RecentChat(10)); got != 10 {
		t.Errorf("RecentChat(10) = %d, want 10", got)
	}
	// A multi-line packet is split into separate messages.
	m2 := NewManager()
	m2.onServerMessage("(Global) A: one\n(Direct) B: two\nnot chat\n(Side) C: three")
	if got := len(m2.RecentChat(0)); got != 3 {
		t.Errorf("multi-line packet → %d messages, want 3", got)
	}
}
