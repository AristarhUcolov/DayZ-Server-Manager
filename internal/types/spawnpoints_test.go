// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const spawnPointsSample = `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<playerspawnpoints>
	<fresh>
		<spawn_params>
			<min_dist_infected>30</min_dist_infected>
			<max_dist_player>150</max_dist_player>
		</spawn_params>
		<generator_params>
			<grid_density>4</grid_density>
			<max_steepness>45</max_steepness>
		</generator_params>
		<group_params>
			<enablegroups>true</enablegroups>
			<lifetime>120</lifetime>
		</group_params>
		<generator_posbubbles>
			<group name="WestCherno">
				<pos x="6063.018555" z="1931.907227" />
				<pos x="5933.964844" z="2171.072998" />
			</group>
		</generator_posbubbles>
		<generator_posbubbles>
			<group name="EastCherno">
				<pos x="8040.858398" z="3332.236328" />
			</group>
		</generator_posbubbles>
	</fresh>
	<hop>
		<spawn_params>
			<min_dist_player>500</min_dist_player>
		</spawn_params>
		<generator_params>
			<grid_density>4</grid_density>
		</generator_params>
		<group_params>
			<lifetime>120</lifetime>
		</group_params>
		<generator_posbubbles>
			<group name="HopA">
				<pos x="7500" z="7500" />
			</group>
		</generator_posbubbles>
	</hop>
</playerspawnpoints>
`

func TestPlayerSpawnsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfgplayerspawnpoints.xml")
	if err := os.WriteFile(p, []byte(spawnPointsSample), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadPlayerSpawns(p)
	if err != nil {
		t.Fatal(err)
	}
	// Structure preserved.
	if !doc.Fresh.Present || !doc.Hop.Present || doc.Travel.Present {
		t.Fatalf("section presence wrong: fresh=%v hop=%v travel=%v", doc.Fresh.Present, doc.Hop.Present, doc.Travel.Present)
	}
	if len(doc.Fresh.Groups) != 2 {
		t.Fatalf("fresh groups=%d want 2", len(doc.Fresh.Groups))
	}
	// The two fresh groups came from two separate posbubbles blocks.
	if doc.Fresh.Groups[0].Block != 0 || doc.Fresh.Groups[1].Block != 1 {
		t.Errorf("block indices lost: %d,%d", doc.Fresh.Groups[0].Block, doc.Fresh.Groups[1].Block)
	}
	if doc.Fresh.Groups[0].Name != "WestCherno" || len(doc.Fresh.Groups[0].Pos) != 2 {
		t.Errorf("first group wrong: %+v", doc.Fresh.Groups[0])
	}
	// Param leaves preserved in order and value.
	if len(doc.Fresh.SpawnParams) != 2 || doc.Fresh.SpawnParams[0].Key != "min_dist_infected" || doc.Fresh.SpawnParams[0].Value != "30" {
		t.Errorf("spawn params wrong: %+v", doc.Fresh.SpawnParams)
	}

	// Round-trip: re-marshal, re-parse, compare the model.
	out := MarshalPlayerSpawns(doc)
	if !strings.Contains(string(out), `<pos x="6063.018555" z="1931.907227" />`) {
		t.Errorf("coordinate precision lost:\n%s", out)
	}
	if strings.Count(string(out), "<generator_posbubbles>") != 3 { // fresh(2) + hop(1)
		t.Errorf("posbubbles block count changed:\n%s", out)
	}
	p2 := filepath.Join(t.TempDir(), "out.xml")
	os.WriteFile(p2, out, 0o644)
	doc2, err := LoadPlayerSpawns(p2)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc2.Fresh.Groups) != 2 || len(doc2.Hop.Groups) != 1 {
		t.Errorf("re-parse lost groups: fresh=%d hop=%d", len(doc2.Fresh.Groups), len(doc2.Hop.Groups))
	}
	if doc2.Fresh.Groups[1].Block != 1 {
		t.Errorf("re-parse lost block split: %d", doc2.Fresh.Groups[1].Block)
	}
}
