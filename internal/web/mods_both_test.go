// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Switching a mod on for one list moves it off the other — unless the admin
// asks to load it both ways (-mod and -serverMod), as some mods require.
func TestModsEnableMoveOrBoth(t *testing.T) {
	h, a := profileApp(t)
	if err := os.MkdirAll(filepath.Join(a.ServerDir, "@Trader", "addons"), 0o755); err != nil {
		t.Fatal(err)
	}
	enable := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.modsEnable(rec, httptest.NewRequest("POST", "/api/mods/enable", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	lists := func() (client, server bool) {
		c := a.Cfg()
		return contains(c.Mods, "@Trader"), contains(c.ServerMods, "@Trader")
	}
	enable(`{"mod":"@Trader","enabled":true}`)
	if c, s := lists(); !c || s {
		t.Fatalf("client mod: %v %v", c, s)
	}
	enable(`{"mod":"@Trader","enabled":true,"serverSide":true,"keepOther":true}`)
	if c, s := lists(); !c || !s {
		t.Fatalf("both ways: %v %v", c, s)
	}
	enable(`{"mod":"@Trader","enabled":false}`)
	if c, s := lists(); c || !s {
		t.Fatalf("players' copy off, server's kept: %v %v", c, s)
	}
	enable(`{"mod":"@Trader","enabled":true}`)
	if c, s := lists(); !c || s {
		t.Fatalf("switched for players without keepOther moves it: %v %v", c, s)
	}
}
