// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
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
