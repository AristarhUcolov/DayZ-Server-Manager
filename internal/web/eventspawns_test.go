// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// The checks used to warn that ~40 vanilla events "will not spawn": infected
// and trajectories spawn around players, animals in their env territories, and
// switched-off or nominal-0 events spawn nothing anyway. Only an active
// "fixed" event with something to keep on the map needs points, and an empty
// <event> block in cfgeventspawns.xml is no orphan.
func TestEventSpawnsChecksOnlyFlagEventsThatNeedPoints(t *testing.T) {
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	ev := func(name, pos string, nominal, active int) string {
		return "<event name=\"" + name + "\"><nominal>" + strconv.Itoa(nominal) + "</nominal><active>" + strconv.Itoa(active) +
			"</active><position>" + pos + "</position></event>\n"
	}
	put(t, filepath.Join(m, "db", "events.xml"), "<events>\n"+
		ev("InfectedCity", "player", 50, 1)+
		ev("TrajectoryApple", "player", 140, 1)+
		ev("StaticChristmasTree", "uniform", 13, 1)+
		ev("AnimalCow", "fixed", 7, 1)+
		ev("AmbientHen", "fixed", 3, 1)+
		ev("Mammoth", "fixed", 2, 1)+
		ev("StaticFridgeTest", "fixed", 3, 0)+
		ev("Loot", "fixed", 0, 1)+
		ev("StaticHeliCrash", "fixed", 3, 1)+
		ev("VehicleCivilianSedan", "fixed", 8, 1)+
		"</events>\n")
	put(t, filepath.Join(m, "cfgenvironment.xml"), `<env><territories>
		<territory type="Herd" name="Cow"><file usable="cattle_territories"/></territory>
		<territory type="Herd" name="Mammoth"><file usable="mammoth_territories"/></territory>
	</territories></env>`)
	put(t, filepath.Join(m, "cfgeventspawns.xml"), `<eventposdef>
	<event name="VehicleCivilianSedan"><pos x="100" z="200" a="0"/></event>
	<event name="VehicleTransitBus"></event>
	<event name="OldEvent"><pos x="1" z="2" a="0"/></event>
</eventposdef>`)

	rec := httptest.NewRecorder()
	h.eventSpawnsList(rec, httptest.NewRequest("GET", "/api/eventspawns", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		NoPositions []string `json:"noPositions"`
		Orphans     []string `json:"orphans"`
		Info        map[string]struct {
			Mode   string `json:"mode"`
			Active bool   `json:"active"`
		} `json:"info"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if want := []string{"StaticHeliCrash"}; !reflect.DeepEqual(out.NoPositions, want) {
		t.Errorf("noPositions = %v, want %v", out.NoPositions, want)
	}
	if want := []string{"OldEvent"}; !reflect.DeepEqual(out.Orphans, want) {
		t.Errorf("orphans = %v, want %v", out.Orphans, want)
	}
	for name, mode := range map[string]string{
		"InfectedCity": "player", "StaticChristmasTree": "uniform", "AnimalCow": "territory",
		"AmbientHen": "territory", "Mammoth": "territory", "StaticHeliCrash": "points",
	} {
		if got := out.Info[name].Mode; got != mode {
			t.Errorf("%s: mode %q, want %q", name, got, mode)
		}
	}
	if out.Info["StaticFridgeTest"].Active || !out.Info["StaticHeliCrash"].Active {
		t.Errorf("active flags wrong: %+v", out.Info)
	}
}
