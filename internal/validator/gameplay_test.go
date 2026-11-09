// Copyright (c) 2026 Aristarh Ucolov.

package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/pbo"
)

const gpScript = `class CfgGameplayJson
{
	int version = -1;
	ref ITEM_GeneralData GeneralData			= new ITEM_GeneralData;
	ref ITEM_BaseBuildingData BaseBuildingData 	= new ITEM_BaseBuildingData;
}

class ITEM_DataBase
{
	bool ValidateServer()
	{
		int local = 3;
		return true;
	}
}

class ITEM_GeneralData : ITEM_DataBase
{
	override bool ValidateServer()
	{
		return true;
	}
	bool disableBaseDamage;
	bool disableRespawnDialog; // the death screen
}

class ITEM_BaseBuildingData : ITEM_DataBase
{
	ref ITEM_HologramData HologramData = new ITEM_HologramData;
}

class ITEM_HologramData : ITEM_DataBase
{
	bool disableIsCollidingBBoxCheck;
	bool disableColdAreaPlacementCheck;
	ref TStringSet disallowedTypesInUnderground;
}
`

func TestParseGameplaySchema(t *testing.T) {
	s := parseGameplaySchema(gpScript)
	if s == nil {
		t.Fatal("no schema")
	}
	if s["CfgGameplayJson"]["GeneralData"] != "ITEM_GeneralData" || s["CfgGameplayJson"]["BaseBuildingData"] != "ITEM_BaseBuildingData" {
		t.Fatalf("sections: %v", s["CfgGameplayJson"])
	}
	if _, ok := s["ITEM_GeneralData"]["disableRespawnDialog"]; !ok {
		t.Fatalf("fields: %v", s["ITEM_GeneralData"])
	}
	for _, bad := range []string{"local", "true", "ValidateServer"} {
		for c, f := range s {
			if _, ok := f[bad]; ok {
				t.Fatalf("%s.%s is a method's, not a setting", c, bad)
			}
		}
	}
	if _, ok := s["ITEM_HologramData"]["disallowedTypesInUnderground"]; !ok {
		t.Fatal("arrays and sets are settings too")
	}
}

// A misspelt key, Bohemia's own retired one and a key in the wrong section
// are reported, the right ones are not.
func TestCheckGameplayKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pbo.Write(filepath.Join(dir, "dta", "scripts.pbo"), [][2]string{{"prefix", "scripts"}},
		[]pbo.WriteFile{{Name: `3_Game\CfgGameplayDataJson.c`, Data: []byte(gpScript)}}); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	if err := os.MkdirAll(md, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"version": 130,
	 "GeneralData": {"disableBaseDamage": false, "disableRespawnDialg": false, "disableIsCollidingBBoxCheck": true},
	 "BaseBuildingData": {"HologramData": {"disableIsCollidingBBoxCheck": false, "disableColdAreaBuildingCheck": false}}}`
	if err := os.WriteFile(filepath.Join(md, "cfggameplay.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkGameplayKeys(Options{ServerDir: dir, Mission: "dayzOffline.chernarusplus"})
	want := map[string]string{
		"GeneralData.disableRespawnDialg":                            "disableRespawnDialog",
		"GeneralData.disableIsCollidingBBoxCheck":                    "",
		"BaseBuildingData.HologramData.disableColdAreaBuildingCheck": "disableColdAreaPlacementCheck",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d issues: %+v", len(got), got)
	}
	for _, is := range got {
		meant, ok := want[is.Args["key"]]
		if !ok || is.Args["meant"] != meant {
			t.Fatalf("unexpected: %+v", is)
		}
	}
}

// TestGameplayKeysOnRealServer reads a real DayZ server's scripts when
// DZM_SERVER names one.
func TestGameplayKeysOnRealServer(t *testing.T) {
	dir := os.Getenv("DZM_SERVER")
	if dir == "" {
		t.Skip("set DZM_SERVER to a DayZ server folder")
	}
	s := gameplaySchema(dir)
	if s == nil {
		t.Fatal("no schema read")
	}
	n := 0
	for _, f := range s {
		n += len(f)
	}
	t.Logf("%d classes, %d fields", len(s), n)
	for _, is := range checkGameplayKeys(Options{ServerDir: dir, Mission: "dayzOffline.chernarusplus"}) {
		t.Logf("%s", is.Message)
		if !strings.HasPrefix(is.Message, "cfggameplay.json") {
			t.Fatal(is.Message)
		}
	}
}
