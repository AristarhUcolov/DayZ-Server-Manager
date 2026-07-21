package admlog

import "testing"

// The killfeed used to call every one of these a suicide, because Target is
// only filled for a `killed by Player "X"` line.
func TestNonPlayerKillerIsCaptured(t *testing.T) {
	cases := []struct{ line, source, target string }{
		{`17:45:10 | Player "Bob" (DEAD) (id=xyz= pos=<1, 2, 3>) killed by ZmbM_HermitSkinny_Beard with Melee`, "ZmbM_HermitSkinny_Beard", ""},
		{`17:45:10 | Player "Bob" (DEAD) (id=xyz= pos=<1, 2, 3>) killed by Animal_UrsusArctos`, "Animal_UrsusArctos", ""},
		{`17:45:10 | Player "Bob" (DEAD) (id=xyz= pos=<1, 2, 3>) killed by FallDamage`, "FallDamage", ""},
		// A real PvP kill must still be attributed to the player, not a source.
		{`17:45:10 | Player "Bob" (DEAD) (id=xyz= pos=<1, 2, 3>) killed by Player "Alice" (id=k= pos=<1, 2, 3>) with AK74 from 12.5 meters`, "", "Alice"},
	}
	for _, c := range cases {
		ev, ok := ParseLine(c.line)
		if !ok || ev.Type != "kill" {
			t.Fatalf("not parsed as a kill: %s", c.line)
		}
		if ev.Source != c.source {
			t.Errorf("source = %q, want %q\n  %s", ev.Source, c.source, c.line)
		}
		if ev.Target != c.target {
			t.Errorf("target = %q, want %q\n  %s", ev.Target, c.target, c.line)
		}
	}
}
