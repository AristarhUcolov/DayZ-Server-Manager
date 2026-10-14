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

// A deleted mission goes to the trash and comes back under its own name; a
// restore never replaces a mission of the same name, and the trash refuses
// names that would reach outside it.
func TestMissionTrashRestoreRoundTrip(t *testing.T) {
	h, dir := testHandlers(t)
	put(t, filepath.Join(dir, "serverDZ.cfg"), "class Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	put(t, filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus", "init.c"), "void main(){}")
	put(t, filepath.Join(dir, "mpmissions", "old.enoch", "cfgeconomycore.xml"), "<economycore/>")
	post := func(fn func(w *httptest.ResponseRecorder), want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		fn(rec)
		if rec.Code != want {
			t.Fatalf("status %d, want %d: %s", rec.Code, want, rec.Body.String())
		}
		return rec
	}

	rec := post(func(w *httptest.ResponseRecorder) {
		h.missionsDelete(w, httptest.NewRequest("POST", "/api/missions/delete", strings.NewReader(`{"name":"old.enoch"}`)))
	}, 200)
	var del struct{ Entry string }
	_ = json.Unmarshal(rec.Body.Bytes(), &del)
	if !strings.HasPrefix(del.Entry, "old.enoch-") {
		t.Fatalf("trash entry %q", del.Entry)
	}

	rec = post(func(w *httptest.ResponseRecorder) {
		h.missionsTrash(w, httptest.NewRequest("GET", "/api/missions/trash", nil))
	}, 200)
	var list struct {
		Missions []struct {
			Entry, Name, At string
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Missions) != 1 || list.Missions[0].Name != "old.enoch" || list.Missions[0].At == "" {
		t.Fatalf("trash list = %+v", list.Missions)
	}

	// A new mission of the same name blocks the restore.
	put(t, filepath.Join(dir, "mpmissions", "old.enoch", "init.c"), "new")
	post(func(w *httptest.ResponseRecorder) {
		h.missionsRestore(w, httptest.NewRequest("POST", "/api/missions/restore", strings.NewReader(`{"entry":"`+del.Entry+`"}`)))
	}, 409)
	if got, _ := os.ReadFile(filepath.Join(dir, "mpmissions", "old.enoch", "init.c")); string(got) != "new" {
		t.Fatal("a restore replaced the mission that was there")
	}
	_ = os.RemoveAll(filepath.Join(dir, "mpmissions", "old.enoch"))

	post(func(w *httptest.ResponseRecorder) {
		h.missionsRestore(w, httptest.NewRequest("POST", "/api/missions/restore", strings.NewReader(`{"entry":"`+del.Entry+`"}`)))
	}, 200)
	if _, err := os.Stat(filepath.Join(dir, "mpmissions", "old.enoch", "cfgeconomycore.xml")); err != nil {
		t.Fatalf("mission not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.missionTrashDir(), del.Entry)); !os.IsNotExist(err) {
		t.Fatal("the trash still holds the restored mission")
	}

	for _, bad := range []string{`..`, `../mpmissions`, `x/../..`, ``} {
		post(func(w *httptest.ResponseRecorder) {
			h.missionsTrashDelete(w, httptest.NewRequest("POST", "/api/missions/trash/delete", strings.NewReader(`{"entry":"`+bad+`"}`)))
		}, 404)
	}
	if _, err := os.Stat(filepath.Join(dir, "mpmissions", "dayzOffline.chernarusplus")); err != nil {
		t.Fatal("a bad trash entry reached mpmissions")
	}
}

// "..." used to pass and, on Windows, resolve to mpmissions itself — a delete
// or overwrite-import then moved every mission to the trash.
func TestSafeMissionNameRejectsTrailingDots(t *testing.T) {
	for _, bad := range []string{"", " ", ".", "..", "...", "foo.", "foo. ", "foo..", "a/b", `a\b`, "c:x"} {
		if got, err := safeMissionName(bad); err == nil {
			t.Errorf("safeMissionName(%q) = %q, want error", bad, got)
		}
	}
	for _, ok := range []string{"dayzOffline.chernarusplus", "regular.enoch", " my.mission ", ".hidden"} {
		if _, err := safeMissionName(ok); err != nil {
			t.Errorf("safeMissionName(%q) = %v, want ok", ok, err)
		}
	}
}
