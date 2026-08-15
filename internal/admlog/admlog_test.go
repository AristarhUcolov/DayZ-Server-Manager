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
		// Real vanilla-format lines (from a live server): the "[HP: nn]" health
		// annotation and the "is connected" wording used to break parsing.
		{`01:05:19 | Player "Aristarh Ucolov" (id=M9= pos=<7669.5, 5186.9, 214.9>)[HP: 98.1937] hit by Заражённый into LeftLeg(8) for 7.225 damage (MeleeInfectedLong)`, "hit", "Aristarh Ucolov", ""},
		{`01:00:00 | Player "A" (id=a= pos=<0,0,0>)[HP: 50] hit by Player "B" (id=b= pos=<1,1,1>) into Torso(1) for 10 damage (FirearmClose)`, "hit", "A", "B"},
		{`01:07:42 | Player "Aristarh Ucolov" (DEAD) (id=M9= pos=<7566.7, 5135.5, 214.0>) died. Stats> Water: 565.357 Energy: 566.556 Bleed sources: 2`, "death", "Aristarh Ucolov", ""},
		{`01:07:47 | Player "Aristarh Ucolov" (id=M9= pos=<7940.9, 3432.2, 8.5>) is connected`, "connect", "Aristarh Ucolov", ""},
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

// A real hit line must still yield the player's position — that's what feeds the
// live map. Before the [HP: nn] fix this returned no position at all.
func TestParseLineHitCarriesPosition(t *testing.T) {
	ev, ok := ParseLine(`01:05:19 | Player "Aristarh Ucolov" (id=M9= pos=<7669.5, 5186.9, 214.9>)[HP: 98.1937] hit by Заражённый into LeftLeg(8) for 7.225 damage (MeleeInfectedLong)`)
	if !ok || ev.Type != "hit" {
		t.Fatalf("type = %q ok=%v, want hit", ev.Type, ok)
	}
	if len(ev.Pos) < 2 || ev.Pos[0] != 7669.5 || ev.Pos[1] != 5186.9 {
		t.Errorf("pos = %v, want [7669.5 5186.9 …]", ev.Pos)
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
