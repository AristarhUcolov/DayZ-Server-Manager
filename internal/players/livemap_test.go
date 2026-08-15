// Copyright (c) 2026 Aristarh Ucolov.
package players

import (
	"math"
	"os"
	"path/filepath"
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

func TestLiveMapAndHeatmapFromRealADM(t *testing.T) {
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "DayZServer_x64.ADM"), []byte(admRealSample), 0o644); err != nil {
		t.Fatal(err)
	}

	st := Open(filepath.Join(dir, ".dayz-manager"))
	st.Ingest(profiles)

	// Live map: a hit line must produce a last-known position for the player.
	var found *LivePos
	pos := st.LivePositions()
	for i := range pos {
		if pos[i].Name == "Aristarh Ucolov" {
			found = &pos[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no live position — hit lines are not feeding the live map")
	}
	if math.Abs(found.X-7707.9) > 0.05 || math.Abs(found.Z-5166.1) > 0.05 {
		t.Errorf("last position = (%v, %v), want (7707.9, 5166.1)", found.X, found.Z)
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
