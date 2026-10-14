// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import "testing"

func TestParseServerEvents(t *testing.T) {
	cases := []struct {
		line string
		want ServerEvent
	}{
		{"Player #3 Survivor (203.0.113.42:2304) connected", ServerEvent{Kind: "connect", No: 3, Name: "Survivor", IP: "203.0.113.42"}},
		{"Player #3 Mr. Smith (Lobby) - BE GUID: 4A9F0C1E2D3B4A5968778695A4B3C2D1", ServerEvent{Kind: "guid", No: 3, Name: "Mr. Smith (Lobby)", GUID: "4a9f0c1e2d3b4a5968778695a4b3c2d1"}},
		{"Verified GUID (4a9f0c1e2d3b4a5968778695a4b3c2d1) of player #12 Some Guy", ServerEvent{Kind: "guid", No: 12, Name: "Some Guy", GUID: "4a9f0c1e2d3b4a5968778695a4b3c2d1"}},
		{"Player #12 Some Guy disconnected", ServerEvent{Kind: "disconnect", No: 12, Name: "Some Guy"}},
	}
	for _, c := range cases {
		got, ok := parseServerEvent(c.line)
		if !ok || got != c.want {
			t.Errorf("%q: got %+v (ok=%v), want %+v", c.line, got, ok, c.want)
		}
	}
	for _, line := range []string{"(Global) Survivor: Player #1 x disconnected", "RCon admin #0 (127.0.0.1:5000) logged in"} {
		if ev, ok := parseServerEvent(line); ok {
			t.Errorf("%q parsed as %+v", line, ev)
		}
	}
}

func TestEventSinkReceivesPlayerLines(t *testing.T) {
	m := NewManager()
	var got []ServerEvent
	m.SetEventSink(func(ev ServerEvent) { got = append(got, ev) })
	m.onServerMessage("Player #1 Alice (10.0.0.2:2304) connected\nPlayer #2 Bob disconnected")
	if len(got) != 2 || got[0].Kind != "connect" || got[1].Kind != "disconnect" || got[1].Name != "Bob" {
		t.Fatalf("events: %+v", got)
	}
}
