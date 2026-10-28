// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/players"
)

// The server-wide actions need a running server; stopped, they say so
// instead of failing inside RCon.
func TestPlayersAdminNeedsRunningServer(t *testing.T) {
	h, _ := profileApp(t)
	for _, c := range []struct {
		path string
		fn   func(*httptest.ResponseRecorder, string)
	}{
		{"/api/players/kickall", func(rec *httptest.ResponseRecorder, body string) {
			h.playersKickAll(rec, httptest.NewRequest("POST", "/api/players/kickall", strings.NewReader(body)))
		}},
		{"/api/players/lock", func(rec *httptest.ResponseRecorder, body string) {
			h.playersLock(rec, httptest.NewRequest("POST", "/api/players/lock", strings.NewReader(body)))
		}},
	} {
		rec := httptest.NewRecorder()
		c.fn(rec, `{"lock":true,"reason":"maintenance"}`)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), "players.kickAll.notRunning") {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body.String())
		}
	}
	if locked, _ := h.joinLocked(); locked {
		t.Error("locked without a running server")
	}
}

// The profile lists other players last seen from the same address, and what
// the player built.
func TestProfileSameIPAndBases(t *testing.T) {
	h, a := profileApp(t)
	prof := filepath.Join(a.ServerDir, "profiles")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	adm := "AdminLog started on 2026-10-20 at 18:00:00\n" +
		"18:00:05 | Player \"Anna\" (id=AAA= pos=<1000.0, 2000.0, 50.0>) is connected\n" +
		"18:00:06 | Player \"Anna2\" (id=BBB= pos=<1100.0, 2100.0, 50.0>) is connected\n" +
		"18:00:07 | Player \"Carl\" (id=CCC= pos=<1200.0, 2200.0, 50.0>) is connected\n" +
		"18:01:00 | Player \"Anna\" (id=AAA= pos=<1010.0, 2010.0, 50.0>) placed Fence Kit<FenceKit>\n"
	if err := os.WriteFile(filepath.Join(prof, "DayZServer_x64.ADM"), []byte(adm), 0o644); err != nil {
		t.Fatal(err)
	}
	db := h.playersDB()
	db.Ingest(h.profilesAbs())
	db.LinkRCon([]players.OnlineRef{
		{Name: "Anna", GUID: "11111111111111111111111111111111", IP: "203.0.113.7"},
		{Name: "Anna2", GUID: "22222222222222222222222222222222", IP: "203.0.113.7"},
		{Name: "Carl", GUID: "33333333333333333333333333333333", IP: "198.51.100.9"},
	})
	rec := httptest.NewRecorder()
	h.playerProfile(rec, httptest.NewRequest("GET", "/api/players/profile?key=AAA%3D", nil))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		SameIP []struct{ Key, Name string } `json:"sameIp"`
		Bases  []players.BaseEvent          `json:"bases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.SameIP) != 1 || out.SameIP[0].Name != "Anna2" {
		t.Errorf("sameIp = %+v, want Anna2 only", out.SameIP)
	}
	if len(out.Bases) != 1 || out.Bases[0].Action != "placed" {
		t.Errorf("bases = %+v", out.Bases)
	}
}
