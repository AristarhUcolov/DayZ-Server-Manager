// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

func setMission(t *testing.T, dir, tpl string) {
	t.Helper()
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\""+tpl+"\";\n\t};\n};\n")
}

const tuneTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<types>
    <!-- keep me -->
    <type name="AKM">
        <nominal>10</nominal>
        <lifetime>3600</lifetime>
        <restock>0</restock>
        <min>5</min>
        <category name="weapons"/>
    </type>
    <type name="M4A1">
        <nominal>1</nominal>
        <lifetime>3600</lifetime>
        <restock>0</restock>
        <min>1</min>
        <category name="weapons"/>
    </type>
</types>
`

func tune(t *testing.T, h *handlers, body string) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h.economyTune(rec, httptest.NewRequest("POST", "/api/economy/tune", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("economyTune(%s) = %d %s", body, rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func loadType(t *testing.T, path, name string) *dztypes.Type {
	t.Helper()
	doc, err := dztypes.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ty := doc.Find(name)
	if ty == nil {
		t.Fatalf("%s missing from %s", name, path)
	}
	return ty
}

// ×1 must bring back only the amounts: edits made after the first scaling
// (another field, a new type, a comment) survive, and a backup is taken.
func TestEconomyResetRestoresOnlyAmounts(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	path := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "db", "types.xml")
	put(t, path, tuneTypes)

	tune(t, h, `{"factor":2,"nominal":true,"min":true}`)
	if n := *loadType(t, path, "AKM").Nominal; n != 20 {
		t.Fatalf("after ×2 AKM nominal = %d, want 20", n)
	}
	// A later edit made on the Types page, plus a brand-new type.
	doc, _ := dztypes.Load(path)
	lt := 7200
	doc.Find("AKM").Lifetime = &lt
	doc.MarkDirty("AKM")
	nom := 3
	doc.Upsert(dztypes.Type{Name: "NewGun", Nominal: &nom})
	if err := dztypes.SaveTypes(path, doc); err != nil {
		t.Fatal(err)
	}

	out := tune(t, h, `{"reset":true}`)
	if out["hadBaseline"] != true {
		t.Fatalf("reset reported no baseline: %v", out)
	}
	akm := loadType(t, path, "AKM")
	if *akm.Nominal != 10 || *akm.Min != 5 {
		t.Errorf("AKM after reset = %d/%d, want 10/5", *akm.Nominal, *akm.Min)
	}
	if *akm.Lifetime != 7200 {
		t.Errorf("reset reverted the lifetime edit: %d", *akm.Lifetime)
	}
	if n := *loadType(t, path, "NewGun").Nominal; n != 3 {
		t.Errorf("NewGun nominal = %d, want it kept at 3", n)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "<!-- keep me -->") {
		t.Error("reset dropped the file's comments")
	}
	baks, _ := filepath.Glob(path + ".bak.*")
	if len(baks) == 0 {
		t.Error("reset wrote types.xml without a backup")
	}
	// The snapshot is used up: the next scaling takes a fresh one.
	if out := tune(t, h, `{"reset":true}`); out["hadBaseline"] != false {
		t.Errorf("second reset found a baseline: %v", out)
	}
}

// The data dir is shared by every mission of the server: a snapshot taken on
// one map must never be restored into another.
func TestEconomyResetIsPerMission(t *testing.T) {
	h, dir := testHandlers(t)
	cherno := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "db", "types.xml")
	enoch := filepath.Join(dir, "mpmissions", "dayzOffline.enoch", "db", "types.xml")
	put(t, cherno, tuneTypes)
	livonia := strings.Replace(tuneTypes, "<nominal>10</nominal>", "<nominal>40</nominal>", 1)
	put(t, enoch, livonia)

	setMission(t, dir, "dayzOffline.chernarusplus")
	tune(t, h, `{"factor":2,"nominal":true,"min":true}`)

	setMission(t, dir, "dayzOffline.enoch")
	if out := tune(t, h, `{"reset":true}`); out["hadBaseline"] != false {
		t.Fatalf("Livonia restored Chernarus' snapshot: %v", out)
	}
	if b, _ := os.ReadFile(enoch); string(b) != livonia {
		t.Error("Livonia's types.xml was changed by a reset meant for Chernarus")
	}
}

// Saving many inline edits is one write: the backup taken before it holds the
// file exactly as it was before the save.
func TestTypesPatchManyWritesOnce(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	path := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "db", "types.xml")
	put(t, path, tuneTypes)

	rec := httptest.NewRecorder()
	h.typesPatchMany(rec, httptest.NewRequest("POST", "/api/types/patch-many", strings.NewReader(
		`{"file":"","rows":[{"name":"AKM","patch":{"nominal":12}},{"name":"m4a1","patch":{"min":0,"category":"tools"}}]}`)))
	if rec.Code != 200 {
		t.Fatalf("typesPatchMany = %d %s", rec.Code, rec.Body.String())
	}
	if n := *loadType(t, path, "AKM").Nominal; n != 12 {
		t.Errorf("AKM nominal = %d, want 12", n)
	}
	m4 := loadType(t, path, "M4A1")
	if *m4.Min != 0 || m4.Category == nil || m4.Category.Name != "tools" {
		t.Errorf("M4A1 = min %d cat %v, want 0 / tools", *m4.Min, m4.Category)
	}
	baks, _ := filepath.Glob(path + ".bak.*")
	if len(baks) != 1 {
		t.Fatalf("%d backups, want exactly 1", len(baks))
	}
	if b, _ := os.ReadFile(baks[0]); string(b) != tuneTypes {
		t.Error("the backup is not the file as it was before the save")
	}
}

// ×0.25 on nominal 1 must not make a rare item stop spawning.
func TestEconomyScaleKeepsRareItems(t *testing.T) {
	h, dir := testHandlers(t)
	setMission(t, dir, "dayzOffline.chernarusplus")
	path := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "db", "types.xml")
	put(t, path, tuneTypes)

	out := tune(t, h, `{"factor":0.25,"nominal":true,"min":true}`)
	m4 := loadType(t, path, "M4A1")
	if *m4.Nominal != 1 || *m4.Min != 1 {
		t.Errorf("M4A1 after ×0.25 = %d/%d, want 1/1", *m4.Nominal, *m4.Min)
	}
	fl, _ := out["floored"].([]interface{})
	if len(fl) != 1 || fl[0] != "M4A1" {
		t.Errorf("floored = %v, want [M4A1]", out["floored"])
	}
	if n := *loadType(t, path, "AKM").Nominal; n != 3 {
		t.Errorf("AKM after ×0.25 = %d, want 3", n)
	}
}
