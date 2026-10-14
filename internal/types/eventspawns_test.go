// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleEventSpawns = `<?xml version="1.0" encoding="UTF-8"?>
<eventposdef>
	<event name="StaticContaminatedArea">
		<zone smin="0" smax="0" dmin="0" dmax="0" r="100"/>
		<pos x="3663.71" z="8339.06" a="0.0"/>
	</event>
	<event name="VehicleCivilianSedan">
		<pos x="7886.0" z="8987.6" a="176.0" group="sedan_group"/>
		<pos x="4521.2" z="10234.9" a="90.0"/>
	</event>
</eventposdef>
`

func TestEventSpawnsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfgeventspawns.xml")
	if err := os.WriteFile(path, []byte(sampleEventSpawns), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadEventSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(doc.Events))
	}
	// Zone must be preserved.
	ca := doc.Find("StaticContaminatedArea")
	if ca == nil || ca.Zone == nil || ca.Zone.R == nil || *ca.Zone.R != 100 {
		t.Fatalf("contaminated-area zone not parsed: %+v", ca)
	}
	// group on a pos must be preserved.
	sedan := doc.Find("VehicleCivilianSedan")
	if sedan == nil || len(sedan.Pos) != 2 || sedan.Pos[0].Group != "sedan_group" {
		t.Fatalf("sedan positions/group not parsed: %+v", sedan)
	}

	// Save, reload, and confirm the modeled fields survived.
	if err := SaveEventSpawns(path, doc); err != nil {
		t.Fatal(err)
	}
	doc2, err := LoadEventSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	sedan2 := doc2.Find("VehicleCivilianSedan")
	if sedan2 == nil || len(sedan2.Pos) != 2 {
		t.Fatalf("after save: sedan positions = %+v", sedan2)
	}
	if sedan2.Pos[0].Group != "sedan_group" || sedan2.Pos[0].X != 7886.0 {
		t.Errorf("after save: pos0 = %+v", sedan2.Pos[0])
	}
	ca2 := doc2.Find("StaticContaminatedArea")
	if ca2 == nil || ca2.Zone == nil || ca2.Zone.R == nil || *ca2.Zone.R != 100 {
		t.Errorf("after save: zone lost: %+v", ca2)
	}
}

func TestEventSpawnsSetPositions(t *testing.T) {
	doc := &EventSpawnsDoc{}
	doc.SetPositions("NewEvent", []SpawnPos{{X: 100, Z: 200, A: 0}})
	if e := doc.Find("NewEvent"); e == nil || len(e.Pos) != 1 {
		t.Fatalf("SetPositions did not create the event")
	}
	doc.SetPositions("NewEvent", []SpawnPos{{X: 1, Z: 2}, {X: 3, Z: 4}})
	if e := doc.Find("NewEvent"); e == nil || len(e.Pos) != 2 {
		t.Fatalf("SetPositions did not replace positions")
	}
}

// TestEventSpawnsKeepHeight guards the v1.0.0 fix: saving through the map
// editor used to strip every y= attribute from the whole file (the model had no
// Y field), silently re-grounding static events and boats. Both an explicit
// height and an explicit 0.0 must survive, an absent y must stay absent, and an
// unknown attribute must round-trip untouched.
func TestEventSpawnsKeepHeight(t *testing.T) {
	const src = `<?xml version="1.0" encoding="UTF-8"?>
<eventposdef>
	<event name="StaticHeliCrash">
		<pos x="2593.47" z="5094.11" a="0" y="201.66"/>
		<pos x="2020.90" z="9813.24" a="0"/>
	</event>
	<event name="StaticBoatFishing">
		<pos x="1107.817993" z="7084.618652" y="0.0" a="0" custom="keep"/>
	</event>
</eventposdef>
`
	dir := t.TempDir()
	path := filepath.Join(dir, "cfgeventspawns.xml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadEventSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	// Edit an unrelated event, as the editor does, then save the whole file.
	doc.SetPositions("VehicleCivilianSedan", []SpawnPos{{X: 1, Z: 2, A: 3}})
	if err := SaveEventSpawns(path, doc); err != nil {
		t.Fatal(err)
	}
	back, err := LoadEventSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	heli := back.Find("StaticHeliCrash")
	if heli == nil || len(heli.Pos) != 2 {
		t.Fatalf("heli crash positions lost: %+v", heli)
	}
	if heli.Pos[0].Y == nil || *heli.Pos[0].Y != 201.66 {
		t.Errorf("explicit height lost: %+v", heli.Pos[0])
	}
	if heli.Pos[1].Y != nil {
		t.Errorf("absent height became %v", *heli.Pos[1].Y)
	}
	boat := back.Find("StaticBoatFishing")
	if boat == nil || boat.Pos[0].Y == nil || *boat.Pos[0].Y != 0 {
		t.Fatalf("explicit y=0.0 lost: %+v", boat)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `custom="keep"`) {
		t.Errorf("unknown attribute dropped:\n%s", raw)
	}
}
