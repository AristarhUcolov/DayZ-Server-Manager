// Copyright (c) 2026 Aristarh Ucolov.
package admlog

import "testing"

func TestParseLine(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		wantPlr  string
		wantTgt  string
	}{
		{`17:42:01 | Player "Survivor" (id=abc123= pos=<1234.5, 6789.0, 12.3>) connected`, "connect", "Survivor", ""},
		{`17:42:05 | Player "Survivor" (id=abc123= pos=<1234.5, 6789.0, 12.3>) disconnected`, "disconnect", "Survivor", ""},
		{`17:45:10 | Player "Victim" (DEAD) (id=xyz= pos=<100, 200, 5>) killed by Player "Killer" (id=k= pos=<105, 205, 5>) with AK74 from 12.5 meters`, "kill", "Victim", "Killer"},
		{`17:50:00 | Player "A" (id=a= pos=<0,0,0>) hit by Player "B" (id=b= pos=<0,0,0>) into LeftLeg with Mosin9130`, "hit", "A", "B"},
		{`17:55:00 | Player "Chatter" (id=c= pos=<0,0,0>) Chat("GLOBAL"): hello world`, "chat", "Chatter", ""},
	}
	for _, c := range cases {
		got, ok := ParseLine(c.in)
		if !ok {
			t.Errorf("ParseLine(%q) = !ok", c.in)
			continue
		}
		if got.Type != c.wantType {
			t.Errorf("type = %q, want %q (%q)", got.Type, c.wantType, c.in)
		}
		if got.Player != c.wantPlr {
			t.Errorf("player = %q, want %q (%q)", got.Player, c.wantPlr, c.in)
		}
		if got.Target != c.wantTgt {
			t.Errorf("target = %q, want %q (%q)", got.Target, c.wantTgt, c.in)
		}
	}
}

func TestParseLineSkipsHeader(t *testing.T) {
	if _, ok := ParseLine("AdminLog started on 2026-04-21 at 12:00:00"); ok {
		t.Errorf("header line should not parse")
	}
	if _, ok := ParseLine(""); ok {
		t.Errorf("blank line should not parse")
	}
}
