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

// A world-only wipe takes storage_1/data — bases, vehicles, the economy's
// state — and leaves players.db, the characters. Undoing it puts the data
// back; while the server has made new data since, it is refused.
func TestWipeWorldOnly(t *testing.T) {
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	st := filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "storage_1")
	put(t, filepath.Join(st, "players.db"), "characters")
	put(t, filepath.Join(st, "data", "vehicles.bin"), "cars")

	rec := httptest.NewRecorder()
	h.wipeApply(rec, httptest.NewRequest("POST", "/api/wipe", strings.NewReader(`{"mode":"world"}`)))
	if rec.Code != 200 {
		t.Fatalf("wipe: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(st, "players.db")); err != nil {
		t.Fatal("the characters were wiped too")
	}
	if _, err := os.Stat(filepath.Join(st, "data")); !os.IsNotExist(err) {
		t.Fatal("the world is still there")
	}

	list := func() wipeSet {
		t.Helper()
		rec := httptest.NewRecorder()
		h.wipeList(rec, httptest.NewRequest("GET", "/api/wipe/list", nil))
		var out struct {
			Wipes []wipeSet `json:"wipes"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Wipes) != 1 {
			t.Fatalf("list: %s", rec.Body.String())
		}
		return out.Wipes[0]
	}
	set := list()
	if !set.World || len(set.Folders) != 1 || set.Folders[0] != "storage_1/data" || len(set.Blocked) != 0 {
		t.Fatalf("set = %+v", set)
	}

	// The server ran since and made a new world: restoring would bury it.
	put(t, filepath.Join(st, "data", "vehicles.bin"), "new cars")
	if s := list(); len(s.Blocked) != 1 {
		t.Fatalf("not blocked: %+v", s)
	}
	restore := func() int {
		rec := httptest.NewRecorder()
		h.wipeRestore(rec, httptest.NewRequest("POST", "/api/wipe/restore", strings.NewReader(`{"id":"`+set.ID+`"}`)))
		return rec.Code
	}
	if code := restore(); code != 409 {
		t.Fatalf("restore over a new world: %d", code)
	}
	os.RemoveAll(filepath.Join(st, "data"))
	if code := restore(); code != 200 {
		t.Fatalf("restore: %d", code)
	}
	if data, _ := os.ReadFile(filepath.Join(st, "data", "vehicles.bin")); string(data) != "cars" {
		t.Fatalf("the old world is not back: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(st, "players.db")); string(data) != "characters" {
		t.Fatal("the characters changed")
	}
	if _, err := os.Stat(filepath.Join(h.wipesDir(), set.ID)); !os.IsNotExist(err) {
		t.Fatal("the emptied backup stays in the list")
	}
}
