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

// A class's own entry is read from the file that defines it — a mod's
// spawnabletypes file for a modded car part — and written back there; a class
// without an entry gets one in cfgspawnabletypes.xml.
func TestSpawnableEntryReadAndSave(t *testing.T) {
	h, _, m := vehicleFixture(t)
	get := func(name string) map[string]interface{} {
		rec := httptest.NewRecorder()
		h.spawnableEntry(rec, httptest.NewRequest("GET", "/api/spawnable/entry?name="+name, nil))
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d %s", name, rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if out := get("Pickup_Red"); out["entry"] == nil || out["file"] != "carmod_ce/carmod_spawnabletypes.xml" {
		t.Fatalf("mod entry: %v", out)
	}
	if out := get("AKM"); out["entry"] != nil || out["file"] != "cfgspawnabletypes.xml" {
		t.Fatalf("missing entry: %v", out)
	}
	post := func(body string) int {
		rec := httptest.NewRecorder()
		h.spawnableEntry(rec, httptest.NewRequest("POST", "/api/spawnable/entry", strings.NewReader(body)))
		return rec.Code
	}
	if code := post(`{"name":"AKM","attachments":[{"chance":"0.30","items":[{"name":"AK_Suppressor","chance":"1.00"}]}],"cargo":[]}`); code != 200 {
		t.Fatalf("new entry: %d", code)
	}
	main, _ := os.ReadFile(filepath.Join(m, "cfgspawnabletypes.xml"))
	if !strings.Contains(string(main), `<type name="AKM">`) || !strings.Contains(string(main), "AK_Suppressor") {
		t.Fatalf("AKM not written:\n%s", main)
	}
	if code := post(`{"name":"Pickup_Red","attachments":[],"cargo":[{"chance":"0.50","items":[{"name":"Canteen"}]}]}`); code != 200 {
		t.Fatalf("mod entry save: %d", code)
	}
	mod, _ := os.ReadFile(filepath.Join(m, "carmod_ce", "carmod_spawnabletypes.xml"))
	if !strings.Contains(string(mod), "Canteen") {
		t.Fatalf("mod file not written:\n%s", mod)
	}
	if code := post(`{"name":"Bad<Name>","attachments":[]}`); code != 400 {
		t.Fatalf("bad name accepted: %d", code)
	}
	if code := post(`{"name":"AKM","attachments":[{"chance":"x","items":[{"name":"A"}]}]}`); code != 400 {
		t.Fatalf("bad chance accepted: %d", code)
	}
}
