// Copyright (c) 2026 Aristarh Ucolov.
package admlog

import "testing"

// The base-building lines as DayZ 1.29 writes them (PluginAdminLog and the
// actions' GetAdminLogMessage).
func TestBaseAction(t *testing.T) {
	cases := []struct {
		line                     string
		kind, action, obj, extra string
	}{
		{`12:00:01 | Player "Bob" (id=B= pos=<4512.3, 10234.5, 235.1>) placed Fence Kit<FenceKit>`, "build", "placed", "Fence Kit", "FenceKit"},
		{`12:00:02 | Player "Bob" (id=B= pos=<4512.3, 10234.5, 235.1>)Built wall_base_down on Fence with Hammer`, "build", "built", "Fence", "wall_base_down"},
		{`12:00:03 | Player "Bob" (id=B= pos=<4512.3, 10234.5, 235.1>)Built wall_base_down on Fence with hands`, "build", "built", "Fence", "wall_base_down"},
		{`12:00:04 | Player "Eve" (id=E= pos=<4513.0, 10233.0, 235.0>) destroyed Fence with Sledgehammer`, "raid", "destroyed", "Fence", ""},
		{`12:00:05 | Player "Eve" (id=E= pos=<4513.0, 10233.0, 235.0>) destroyed combination lock with Hacksaw`, "raid", "destroyed", "combination lock", ""},
		{`12:00:06 | Player "Bob" (id=B= pos=<4512.3, 10234.5, 235.1>) has raised Flag_Chernarus on TerritoryFlag at <4512.1, 235.2, 10234.4>`, "build", "flag-up", "TerritoryFlag", "Flag_Chernarus"},
		{`12:00:07 | Player "Bob" (id=B= pos=<4512.3, 10234.5, 235.1>)Player SurvivorM_Boris<5a2c1f80> Dug in UndergroundStash<3b1e0c40> at position <4512.0, 235.0, 10234.0>`, "stash", "dug-in", "UndergroundStash", ""},
	}
	for _, c := range cases {
		ev, ok := ParseLine(c.line)
		if !ok {
			t.Fatalf("not parsed: %s", c.line)
		}
		if len(ev.Pos) < 2 {
			t.Errorf("no position: %s", c.line)
		}
		if ev.Base != c.kind {
			t.Errorf("ParseLine set Base %q, want %q: %s", ev.Base, c.kind, c.line)
		}
		b, ok := BaseAction(ev)
		if !ok {
			t.Errorf("not a base action (type %q, message %q): %s", ev.Type, ev.Message, c.line)
			continue
		}
		if b.Kind != c.kind || b.Action != c.action || b.Object != c.obj {
			t.Errorf("got %+v for %s", b, c.line)
		}
		if c.extra != "" && b.Class != c.extra && b.Part != c.extra {
			t.Errorf("class/part %+v, want %q", b, c.extra)
		}
	}
	for _, line := range []string{
		`12:00:08 | Player "Bob" (id=B= pos=<1, 2, 3>) performed EmoteGreeting`,
		`12:00:09 | Player "Bob" (id=B= pos=<1, 2, 3>) is unconscious`,
		`12:00:10 | Player "Bob" (id=B= pos=<1, 2, 3>)`,
	} {
		ev, _ := ParseLine(line)
		if b, ok := BaseAction(ev); ok {
			t.Errorf("%s parsed as %+v", line, b)
		}
	}
}
