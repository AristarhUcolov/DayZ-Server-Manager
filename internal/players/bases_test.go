// Copyright (c) 2026 Aristarh Ucolov.
package players

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const admBaseSample = `AdminLog started on 2026-10-20 at 18:00:00
18:00:05 | Player "Anna" (id=AAA= pos=<1000.0, 2000.0, 50.0>) is connected
18:01:00 | Player "Anna" (id=AAA= pos=<1010.0, 2010.0, 50.0>) placed Fence Kit<FenceKit>
18:02:00 | Player "Anna" (id=AAA= pos=<1011.0, 2011.0, 50.0>)Built wall_base_down on Fence with Hammer
18:30:00 | Player "Eve" (id=EEE= pos=<1012.0, 2012.0, 50.0>) destroyed Fence with Sledgehammer
18:31:00 | Player "Eve" (id=EEE= pos=<1013.0, 2013.0, 50.0>) performed EmoteGreeting
`

// Base-building lines land in the store with who, what and where.
func TestBasesFromTheLog(t *testing.T) {
	st := ingestADM(t, admBaseSample)
	b := st.Bases()
	if len(b) != 3 {
		t.Fatalf("bases = %+v, want 3", b)
	}
	if b[0].Action != "placed" || b[0].Player != "Anna" || b[0].Key != "AAA=" || b[0].Pos[0] != 1010 {
		t.Errorf("first = %+v", b[0])
	}
	if b[2].Kind != "raid" || b[2].Player != "Eve" || b[2].Tool != "Sledgehammer" {
		t.Errorf("raid = %+v", b[2])
	}
	if b[1].At >= b[2].At {
		t.Errorf("times out of order: %s, %s", b[1].At, b[2].At)
	}
}

// After an upgrade the logs were already read (offsets stored) before base
// events were kept: the first pass searches them once.
func TestBasesSeededFromLogsReadBefore(t *testing.T) {
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	mgr := filepath.Join(dir, ".dayz-manager")
	for _, d := range []string{profiles, mgr} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	adm := filepath.Join(profiles, "DayZServer_x64.ADM")
	if err := os.WriteFile(adm, []byte(admBaseSample), 0o644); err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(map[string]interface{}{
		"players":  map[string]interface{}{},
		"killfeed": []interface{}{},
		"offsets":  map[string]int64{adm: int64(len(admBaseSample))},
	})
	if err := os.WriteFile(filepath.Join(mgr, "players.json"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	st := Open(mgr)
	st.Ingest(profiles)
	if n := len(st.Bases()); n != 3 {
		t.Fatalf("seeded %d base events, want 3", n)
	}
	// Seeding runs once: a reopened store keeps the same three.
	st2 := Open(mgr)
	st2.Ingest(profiles)
	if n := len(st2.Bases()); n != 3 {
		t.Fatalf("after reopening: %d, want 3", n)
	}
}

// The live map's trail: earlier positions in order, and none after a death.
func TestLiveTrail(t *testing.T) {
	st := ingestADM(t, `AdminLog started on 2026-10-20 at 18:00:00
18:00:05 | Player "Anna" (id=AAA= pos=<1000.0, 2000.0, 50.0>) is connected
18:05:00 | Player "Anna" (id=AAA= pos=<1100.0, 2000.0, 50.0>)
18:05:30 | Player "Anna" (id=AAA= pos=<1101.0, 2001.0, 50.0>) performed EmoteGreeting
18:10:00 | Player "Anna" (id=AAA= pos=<1300.0, 2100.0, 50.0>)
`)
	anna := livePos(st, "Anna")
	if anna == nil {
		t.Fatal("no position")
	}
	// 1101,2001 is within 5 m of 1100,2000: one point, not two.
	if len(anna.Trail) != 2 || anna.Trail[0][0] != 1000 || anna.Trail[1][0] != 1101 {
		t.Errorf("trail = %v, want [[1000 2000] [1101 2001]]", anna.Trail)
	}
}
