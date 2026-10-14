// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"sync"
	"testing"
	"time"
)

func TestRecordSentAndEchoDedupe(t *testing.T) {
	m := NewManager()
	var mu sync.Mutex
	var sunk []ChatMsg
	m.SetChatSink(func(c ChatMsg) { mu.Lock(); sunk = append(sunk, c); mu.Unlock() })

	m.expectEcho("Restart in 5 minutes")
	// BattlEye relays our own say back to RCon clients — must not show twice,
	// even when the relay is parsed before the send has been recorded.
	m.onServerMessage("RCon admin #0: (Global) Restart in 5 minutes")
	m.recordSent("Global", "", "Restart in 5 minutes")
	// Another admin tool's say is shown as an admin message.
	m.onServerMessage("RCon admin #1: (Global) Event at the airfield")
	// Regular player chat still parses; system lines do not.
	m.onServerMessage("(Side) Survivor: hi: all\nPlayer #3 Bob disconnected")

	got := m.RecentChat(10)
	if len(got) != 3 {
		t.Fatalf("got %d messages: %+v", len(got), got)
	}
	if got[0].Source != ChatFromManager || got[0].Text != "Restart in 5 minutes" || got[0].Channel != "Global" {
		t.Errorf("sent message: %+v", got[0])
	}
	if got[1].Source != ChatFromAdmin || got[1].Name != "RCon admin #1" || got[1].Text != "Event at the airfield" {
		t.Errorf("admin message: %+v", got[1])
	}
	if got[2].Source != ChatFromPlayer || got[2].Name != "Survivor" || got[2].Text != "hi: all" {
		t.Errorf("player message: %+v", got[2])
	}
	mu.Lock()
	n := len(sunk)
	mu.Unlock()
	if n != 3 {
		t.Fatalf("sink got %d messages, want 3", n)
	}
	// The echo is consumed once: the same text sent by someone else later is shown.
	m.onServerMessage("RCon admin #0: (Global) Restart in 5 minutes")
	if n := len(m.RecentChat(10)); n != 4 {
		t.Fatalf("second identical admin line dropped (have %d)", n)
	}
}

// A say that fails to send must not swallow the same line from another admin.
func TestFailedSayForgetsEcho(t *testing.T) {
	m := NewManager() // unconfigured: Command fails
	if err := m.Say("Event at the airfield"); err == nil {
		t.Fatal("Say succeeded without a connection")
	}
	m.onServerMessage("RCon admin #1: (Global) Event at the airfield")
	if got := m.RecentChat(10); len(got) != 1 || got[0].Source != ChatFromAdmin {
		t.Fatalf("admin line after a failed say: %+v", got)
	}
}

// A say that queued behind slow commands for longer than echoTTL must still
// have its relay deduped; once delivered, the entry expires echoTTL later.
func TestInFlightEchoDoesNotExpire(t *testing.T) {
	m := NewManager()
	m.expectEcho("Restart in 1 minute")
	m.chatMu.Lock()
	m.sentEcho[0].at = time.Now().Add(-2 * echoTTL) // registered long ago, still in flight
	m.chatMu.Unlock()
	m.expectEcho("other") // prunes: must keep the in-flight entry
	m.onServerMessage("RCon admin #0: (Global) Restart in 1 minute")
	m.recordSent("Global", "", "Restart in 1 minute")
	if got := m.RecentChat(10); len(got) != 1 || got[0].Source != ChatFromManager {
		t.Fatalf("in-flight echo not deduped: %+v", got)
	}
	// Delivered but relay not seen yet: re-stamped, so a late relay is deduped.
	m.forgetEcho("other")
	m.expectEcho("Late relay")
	m.chatMu.Lock()
	m.sentEcho[len(m.sentEcho)-1].at = time.Now().Add(-2 * echoTTL)
	m.chatMu.Unlock()
	m.recordSent("Global", "", "Late relay")
	m.onServerMessage("RCon admin #0: (Global) Late relay")
	if got := m.RecentChat(10); len(got) != 2 {
		t.Fatalf("late relay after delivery shown twice: %+v", got)
	}
	m.chatMu.Lock()
	n := len(m.sentEcho)
	m.chatMu.Unlock()
	if n != 0 {
		t.Fatalf("%d echo entries leaked", n)
	}
}

func TestSeedChatKeepsNewest(t *testing.T) {
	m := NewManager()
	var old []ChatMsg
	for i := 0; i < chatBufferMax+50; i++ {
		old = append(old, ChatMsg{At: int64(i), Text: "x"})
	}
	m.SeedChat(old)
	got := m.RecentChat(0)
	if len(got) != chatBufferMax || got[len(got)-1].At != int64(chatBufferMax+49) {
		t.Fatalf("seed kept %d, last %d", len(got), got[len(got)-1].At)
	}
}

func TestSanitizeSay(t *testing.T) {
	if got := sanitizeSay("  line one\r\nline two\n"); got != "line one line two" {
		t.Fatalf("got %q", got)
	}
}
