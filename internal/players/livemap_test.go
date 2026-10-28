// Copyright (c) 2026 Aristarh Ucolov.
package players

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real vanilla lines from a live server: hit lines carry an "[HP: nn]" annotation
// between the position and "hit by", and the connect line reads "is connected".
// Both used to break parsing, so a fighting player never appeared on the live map
// and their death never reached the heatmap.
const admRealSample = `AdminLog started on 2026-08-14 at 01:00:00
01:05:19 | Player "Aristarh Ucolov" (id=M9= pos=<7669.5, 5186.9, 214.9>)[HP: 98.1937] hit by Заражённый into LeftLeg(8) for 7.225 damage (MeleeInfectedLong)
01:06:50 | Player "Aristarh Ucolov" (id=M9= pos=<7707.9, 5166.1, 215.0>)[HP: 63.6581] hit by Заражённый into Torso(1) for 6.175 damage (MeleeInfected)
01:07:42 | Player "Aristarh Ucolov" (DEAD) (id=M9= pos=<7566.7, 5135.5, 214.0>) died. Stats> Water: 565.357 Energy: 566.556 Bleed sources: 2
`

// ingestADM writes one .ADM file and reads it into a fresh store.
func ingestADM(t *testing.T, content string) *Store {
	t.Helper()
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "DayZServer_x64.ADM"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	st := Open(filepath.Join(dir, ".dayz-manager"))
	st.Ingest(profiles)
	return st
}

func livePos(st *Store, name string) *LivePos {
	for _, p := range st.LivePositions() {
		if p.Name == name {
			p := p
			return &p
		}
	}
	return nil
}

func TestLiveMapAndHeatmapFromRealADM(t *testing.T) {
	// Live map: a hit line must produce a last-known position for the player.
	alive := admRealSample[:strings.Index(admRealSample, "01:07:42")]
	found := livePos(ingestADM(t, alive), "Aristarh Ucolov")
	if found == nil {
		t.Fatal("no live position — hit lines are not feeding the live map")
	}
	if math.Abs(found.X-7707.9) > 0.05 || math.Abs(found.Z-5166.1) > 0.05 {
		t.Errorf("last position = (%v, %v), want (7707.9, 5166.1)", found.X, found.Z)
	}

	st := ingestADM(t, admRealSample)
	// After the death the player respawns somewhere else: the body's spot
	// must not stay on the live map as "the player".
	if p := livePos(st, "Aristarh Ucolov"); p != nil {
		t.Errorf("a dead player is still plotted at (%v, %v)", p.X, p.Z)
	}

	// Heatmap: the death must reach the killfeed as an environment death.
	_, kills := st.Snapshot(0)
	if len(kills) != 1 {
		t.Fatalf("killfeed = %d, want 1", len(kills))
	}
	if kills[0].Kind != "env" {
		t.Errorf("death kind = %q, want env", kills[0].Kind)
	}
	if len(kills[0].Pos) < 2 || math.Abs(kills[0].Pos[0]-7566.7) > 0.05 {
		t.Errorf("death pos = %v, want x≈7566.7", kills[0].Pos)
	}
}

// adminLogPlayerList = 1 makes DayZ list every player with a position every
// five minutes, as a bare `Player "X" (id=… pos=<…>)` line. Those lines — and
// any other line about a player that carries a position — must move the dot;
// a disconnect must remove it.
func TestLiveMapPlayerListAndDisconnect(t *testing.T) {
	st := ingestADM(t, `AdminLog started on 2026-10-20 at 18:00:00
18:00:05 | Player "Anna" (id=AAA= pos=<1000.0, 2000.0, 50.0>) is connected
18:00:06 | Player "Bob" (id=BBB= pos=<3000.0, 4000.0, 60.0>) is connected
18:05:00 | ##### PlayerList log: 2 players
18:05:00 | Player "Anna" (id=AAA= pos=<1500.5, 2500.5, 51.0>)
18:05:00 | Player "Bob" (id=BBB= pos=<3100.0, 4100.0, 61.0>)
18:05:00 | #####
18:07:10 | Player "Anna" (id=AAA= pos=<1600.0, 2600.0, 52.0>) placed Fireplace
18:08:00 | Player "Bob"(id=BBB=) has been disconnected
`)
	anna := livePos(st, "Anna")
	if anna == nil || math.Abs(anna.X-1600) > 0.05 || math.Abs(anna.Z-2600) > 0.05 {
		t.Fatalf("Anna = %+v, want the placing line's position (1600, 2600)", anna)
	}
	if bob := livePos(st, "Bob"); bob != nil {
		t.Errorf("Bob disconnected but is still plotted at (%v, %v)", bob.X, bob.Z)
	}
}
