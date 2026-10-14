// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two IDs a Steam account has in DayZ: the admin log's player ID (what
// ban.txt takes) and BattlEye's GUID.
func TestIDsFromSteam64(t *testing.T) {
	// base64url(sha256("76561198120341761")), checked independently.
	if got := dayzIDFromSteam64("76561198120341761"); got != "z_cHEWnwT2Q_BiyRzxvTQzDoOTnQFrRZmgMp4zGKsnw=" {
		t.Errorf("dayz id = %q", got)
	}
	if got := dayzIDFromSteam64("76561198120341761"); !reDayZID.MatchString(got) {
		t.Errorf("derived id %q does not look like an admin-log id", got)
	}
	g := beGUIDFromSteam64("76561198120341761")
	if !reBEGUID.MatchString(g) {
		t.Errorf("guid %q", g)
	}
}

const dayzBanHeader = "//Players added to the ban.txt won't be able to connect to this server.\r\n" +
	"//For comments use the // prefix. It can be used after an inserted ID, to easily mark it.\r\n\r\n"

// ban.txt keeps its header, comments and other bans; the panel's lines match
// the file's line endings.
func TestDayZBanFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ban.txt")
	other := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ="
	if err := os.WriteFile(p, []byte(dayzBanHeader+other+"\t// old one\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := dayzIDFromSteam64("76561198120341761")
	if added, err := addDayZBan(p, id, "Vasya | cheating | DZM"); err != nil || !added {
		t.Fatalf("add: %v %v", added, err)
	}
	if added, _ := addDayZBan(p, id, "again"); added {
		t.Fatal("a second add of the same id must be a no-op")
	}
	data, _ := os.ReadFile(p)
	got := parseDayZBans(string(data))
	if len(got) != 2 || got[0].ID != other || got[1].ID != id || got[1].Comment != "Vasya | cheating | DZM" {
		t.Fatalf("entries: %+v", got)
	}
	if !strings.HasSuffix(string(data), "\r\n") || strings.Count(string(data), "\n") != strings.Count(string(data), "\r\n") {
		t.Fatalf("line endings mixed: %q", data)
	}
	if removed, err := removeDayZBan(p, id); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != dayzBanHeader+other+"\t// old one\r\n" {
		t.Fatalf("file not restored exactly:\n%q", data)
	}
	// Non-ASCII never reaches the game's file.
	if n := asciiNote("Вася | читы"); n != "|" {
		t.Errorf("asciiNote = %q", n)
	}
	if n := panelNote("", "cheating", "Вася"); n != "cheating | DZM" {
		t.Errorf("panelNote = %q", n)
	}
	if n := shownNote("Vasya | cheating | DZM"); n != "Vasya · cheating" {
		t.Errorf("shownNote = %q", n)
	}
}

// BattlEye's bans.txt: -1 / 0 permanent, a Unix time is the end; small numbers
// left by older panel versions are minutes from the file's last write.
func TestBEBanExpiry(t *testing.T) {
	mod := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tok := range []string{"-1", "0", "perm", ""} {
		if perm, _ := beBanExpiry(tok, mod); !perm {
			t.Errorf("%q should be permanent", tok)
		}
	}
	if perm, until := beBanExpiry("1415673088", mod); perm || until != 1415673088 {
		t.Errorf("timestamp: %v %d", perm, until)
	}
	if perm, until := beBanExpiry("1440", mod); perm || until != mod.Add(24*time.Hour).Unix() {
		t.Errorf("legacy minutes: %v %d", perm, until)
	}
	if beBanToken(0) != "-1" || beBanToken(1700000000) != "1700000000" {
		t.Error("tokens")
	}
}

// A ban by Steam64 goes into DayZ's ban.txt (the admin-log ID) and is
// recorded; a timed one is lifted by the expiry pass.
func TestBanByTargetAndExpiry(t *testing.T) {
	h, dir := testHandlers(t)
	post := func(path string, body interface{}) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
		w := httptest.NewRecorder()
		switch path {
		case "/api/bans/add":
			h.bansAdd(w, r)
		case "/api/bans/remove":
			h.bansRemove(w, r)
		}
		return w
	}
	if w := post("/api/bans/add", map[string]interface{}{"target": "not an id"}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad target: %d", w.Code)
	}
	w := post("/api/bans/add", map[string]interface{}{"target": "76561198120341761", "minutes": 60, "reason": "cheating"})
	if w.Code != 200 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ban.txt"))
	if !strings.Contains(string(data), dayzIDFromSteam64("76561198120341761")) {
		t.Fatalf("ban.txt: %q", data)
	}
	// Not due yet.
	h.expireBans()
	if f := h.loadBanFile(); len(f.Records) != 1 || !f.Records[0].active() {
		t.Fatalf("records: %+v", f.Records)
	}
	// Make it due and expire.
	f := h.loadBanFile()
	f.Records[0].Until = time.Now().Add(-time.Minute).Unix()
	if err := h.saveBanFile(f); err != nil {
		t.Fatal(err)
	}
	h.expireBans()
	data, _ = os.ReadFile(filepath.Join(dir, "ban.txt"))
	if strings.Contains(string(data), dayzIDFromSteam64("76561198120341761")) {
		t.Fatalf("expired ban still in ban.txt: %q", data)
	}
	if f := h.loadBanFile(); f.Records[0].active() || f.Records[0].LiftedBy != "expired" {
		t.Fatalf("record not lifted: %+v", f.Records[0])
	}
	// An entry added by hand is listed and can be lifted.
	if err := os.WriteFile(filepath.Join(dir, "ban.txt"), []byte("76561198956764064\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := post("/api/bans/remove", map[string]string{"id": "dayz:76561198956764064"}); w.Code != 200 {
		t.Fatalf("remove external: %d %s", w.Code, w.Body)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "ban.txt"))
	if strings.TrimSpace(string(data)) != "" {
		t.Fatalf("external entry not removed: %q", data)
	}
}

// A Steam64 line in ban.txt (written by hand or another tool) is the same
// account as the admin-log player ID derived from it: the Bans tab names the
// player, the profile and the whitelist see the ban, a panel ban of the same
// player is not listed twice, and lifting it clears both lines.
func TestSteam64BanResolvesToPlayer(t *testing.T) {
	h, dir := testHandlers(t)
	const steam = "76561198120341761"
	id := dayzIDFromSteam64(steam)
	db := `{"players": {"` + id + `": {"key": "` + id + `", "id": "` + id + `", "name": "Rook",
		"firstSeen": "2026-10-01T10:00:00Z", "lastSeen": "2026-10-13T10:00:00Z"}}, "offsets": {}}`
	put(t, filepath.Join(dir, ".dayz-manager", "players.json"), db)
	put(t, filepath.Join(dir, "ban.txt"), steam+"\t// Cheating\r\n")
	put(t, filepath.Join(dir, "whitelist.txt"), id+"\r\n")

	type listed struct {
		Active []banView `json:"active"`
	}
	list := func() []banView {
		w := httptest.NewRecorder()
		h.bansList(w, httptest.NewRequest(http.MethodGet, "/api/bans", nil))
		var out listed
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Active
	}
	act := list()
	if len(act) != 1 || act[0].Name != "Rook" || act[0].Key != id || act[0].DayZID != id || act[0].Steam64 != steam || act[0].ID != "dayz:"+steam {
		t.Fatalf("hand-made Steam64 ban: %+v", act)
	}
	if b := h.banFor(id, ""); b == nil || b.ID != "dayz:"+steam {
		t.Fatalf("profile does not see the Steam64 ban: %+v", b)
	}
	w := httptest.NewRecorder()
	h.whitelistList(w, httptest.NewRequest(http.MethodGet, "/api/whitelist", nil))
	if !strings.Contains(w.Body.String(), `"banned":true`) || !strings.Contains(w.Body.String(), `"name":"Rook"`) {
		t.Fatalf("whitelist entry not flagged as banned: %s", w.Body)
	}

	// The panel bans the same player: one ban in the list, not two.
	rec := httptest.NewRecorder()
	h.bansAdd(rec, httptest.NewRequest(http.MethodPost, "/api/bans/add", strings.NewReader(`{"key":"`+id+`","minutes":0,"reason":"cheating"}`)))
	if rec.Code != 200 {
		t.Fatalf("ban by key: %d %s", rec.Code, rec.Body)
	}
	act = list()
	if len(act) != 1 || act[0].Source != "panel" {
		t.Fatalf("same account listed twice: %+v", act)
	}
	// Lifting it takes the player off ban.txt entirely.
	rm := httptest.NewRecorder()
	h.bansRemove(rm, httptest.NewRequest(http.MethodPost, "/api/bans/remove", strings.NewReader(`{"id":"`+act[0].ID+`"}`)))
	if rm.Code != 200 {
		t.Fatalf("unban: %d %s", rm.Code, rm.Body)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ban.txt"))
	if strings.Contains(string(data), steam) || strings.Contains(string(data), id) {
		t.Fatalf("ban.txt still bans the account: %q", data)
	}
	if act = list(); len(act) != 0 {
		t.Fatalf("still listed: %+v", act)
	}
	// Whitelist removal by the player ID also drops a Steam64 line of the account.
	put(t, filepath.Join(dir, "whitelist.txt"), steam+"\r\nother\r\n")
	wr := httptest.NewRecorder()
	h.whitelistRemove(wr, httptest.NewRequest(http.MethodPost, "/api/whitelist/remove", strings.NewReader(`{"id":"`+id+`"}`)))
	if data, _ := os.ReadFile(filepath.Join(dir, "whitelist.txt")); wr.Code != 200 || string(data) != "other\r\n" {
		t.Fatalf("whitelist remove: %d %q", wr.Code, data)
	}
}
