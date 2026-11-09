// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zonesFixture(t *testing.T) (*handlers, string) {
	t.Helper()
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	m := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")
	put(t, filepath.Join(m, "cfgeffectarea.json"), `{
    "Areas": [
        {
            "AreaName": "Ship-Bow",
            "Type": "ContaminatedArea_Static",
            "TriggerType": "ContaminatedTrigger",
            "Data": { "Pos": [13917, 0, 11178], "Radius": 75, "PosHeight": 20, "NegHeight": 3, "ParticleName": "graphics/particles/contaminated_area_gas_bigass" },
            "PlayerData": { "PPERequesterType": "PPERequester_ContaminatedAreaTint" }
        }
    ],
    "SafePositions": [[434, 13624]]
}`)
	put(t, filepath.Join(m, "db", "events.xml"), `<events>
    <event name="StaticContaminatedArea">
        <nominal>0</nominal>
        <min>2</min>
        <max>4</max>
        <lifetime>2100</lifetime>
        <active>1</active>
        <children>
            <child lootmax="0" lootmin="0" max="4" min="2" type="ContaminatedArea_Dynamic"/>
        </children>
    </event>
</events>
`)
	put(t, filepath.Join(m, "cfgeventspawns.xml"), `<eventposdef>
    <event name="StaticContaminatedArea">
        <zone smin="0" smax="0" dmin="5" dmax="8" r="75" />
        <pos x="341.0" z="9401.0" />
        <pos x="509.0" z="11089.0" />
    </event>
</eventposdef>
`)
	put(t, filepath.Join(m, "db", "types.xml"), `<types>
    <type name="NBCJacketGray">
        <nominal>3</nominal>
        <min>1</min>
        <usage name="Military"/>
    </type>
    <type name="AK_Suppressor">
        <nominal>2</nominal>
        <min>1</min>
        <usage name="ContaminatedArea"/>
    </type>
</types>
`)
	return h, m
}

func TestZonesGet(t *testing.T) {
	h, _ := zonesFixture(t)
	rec := httptest.NewRecorder()
	h.zonesGet(rec, httptest.NewRequest("GET", "/api/zones", nil))
	var got struct {
		Doc struct {
			Areas []struct {
				AreaName string `json:"AreaName"`
			} `json:"Areas"`
		} `json:"doc"`
		Dynamic []zoneDynamic `json:"dynamic"`
		Loot    []zoneLoot    `json:"loot"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	if len(got.Doc.Areas) != 1 || got.Doc.Areas[0].AreaName != "Ship-Bow" {
		t.Errorf("areas = %+v", got.Doc.Areas)
	}
	if len(got.Dynamic) != 1 || got.Dynamic[0].Event != "StaticContaminatedArea" || len(got.Dynamic[0].Positions) != 2 || got.Dynamic[0].Radius != 75 {
		t.Errorf("dynamic = %+v", got.Dynamic)
	}
	if len(got.Loot) != 1 || got.Loot[0].Name != "AK_Suppressor" {
		t.Errorf("loot = %+v", got.Loot)
	}
}

// The file comes back as the page sends it: key order and fields the page
// does not know survive; a zone without a radius is refused.
func TestZonesSave(t *testing.T) {
	h, m := zonesFixture(t)
	save := func(doc string) int {
		rec := httptest.NewRecorder()
		h.zonesSave(rec, httptest.NewRequest("POST", "/api/zones/save", strings.NewReader(`{"doc":`+doc+`}`)))
		return rec.Code
	}
	good := `{"Areas":[{"AreaName":"Ship-Bow","Type":"ContaminatedArea_Static","TriggerType":"ContaminatedTrigger","Data":{"Pos":[13900,0,11100],"Radius":90,"Custom":7}},` +
		`{"AreaName":"New zone","Type":"ContaminatedArea_Static","Data":{"Pos":[2000,0,3000],"Radius":100}}],"SafePositions":[[1,2]],"Extra":true}`
	if code := save(good); code != 200 {
		t.Fatalf("save: %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(m, "cfgeffectarea.json"))
	s := string(data)
	if !strings.Contains(s, `"Custom": 7`) || !strings.Contains(s, `"Extra": true`) || strings.Index(s, `"AreaName"`) > strings.Index(s, `"Type"`) {
		t.Fatalf("written:\n%s", s)
	}
	for _, bad := range []string{
		`{"Areas":[{"AreaName":"A","Data":{"Pos":[1,0,1],"Radius":0}}]}`,
		`{"Areas":[{"AreaName":"","Data":{"Pos":[1,0,1],"Radius":5}}]}`,
		`{"Areas":[{"AreaName":"A","Data":{"Pos":[1,1],"Radius":5}}]}`,
		`{"Areas":[{"AreaName":"A","Data":{"Pos":[1,0,1],"Radius":5}},{"AreaName":"a","Data":{"Pos":[1,0,1],"Radius":5}}]}`,
		`{"Areas":[],"SafePositions":[[1]]}`,
	} {
		if code := save(bad); code != 400 {
			t.Errorf("accepted %s: %d", bad, code)
		}
	}
}

// Zone loot: items gain or lose the ContaminatedArea usage and get their
// counts, in the types file that holds them; an unknown name is reported.
func TestZonesLoot(t *testing.T) {
	h, m := zonesFixture(t)
	rec := httptest.NewRecorder()
	h.zonesLoot(rec, httptest.NewRequest("POST", "/api/zones/loot", strings.NewReader(
		`{"add":["NBCJacketGray","NoSuchThing"],"remove":["AK_Suppressor"],"rows":[{"name":"NBCJacketGray","nominal":6,"min":2}]}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "NoSuchThing") {
		t.Fatalf("loot: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(m, "db", "types.xml"))
	s := string(data)
	jacket := s[strings.Index(s, "NBCJacketGray"):strings.Index(s, "AK_Suppressor")]
	if !strings.Contains(jacket, `<usage name="ContaminatedArea"/>`) || !strings.Contains(jacket, "<nominal>6</nominal>") || !strings.Contains(jacket, `<usage name="Military"/>`) {
		t.Errorf("jacket:\n%s", jacket)
	}
	if strings.Contains(s[strings.Index(s, "AK_Suppressor"):], "ContaminatedArea") {
		t.Errorf("the suppressor kept the usage:\n%s", s)
	}
}
