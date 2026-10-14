// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
)

func fakeDayZ(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DayZServer_x64.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DayZServer"), []byte("\x7fELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A request reaches the server named in its X-DZM-Server header (or ?srv=);
// the panel-wide API and static files always reach the primary.
func TestRequestsReachTheirServer(t *testing.T) {
	primary, err := app.New(fakeDayZ(t), "DayZ Server Manager", "test", "t")
	if err != nil {
		t.Fatal(err)
	}
	f := app.OpenFleet(primary)
	t.Cleanup(func() {
		for _, a := range f.Apps() {
			a.Detach()
			a.CloseLog()
		}
	})
	second, err := f.Add(fakeDayZ(t), "Second")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.MutateConfig(func(c *config.Manager) error { c.ServerPort = 2402; return nil }); err != nil {
		t.Fatal(err)
	}
	s := New(f, "127.0.0.1", 8787)
	hd := s.http.Handler
	do := func(path, server string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil)
		r.RemoteAddr = "127.0.0.1:50000"
		if server != "" {
			r.Header.Set(ServerHeader, server)
		}
		w := httptest.NewRecorder()
		hd.ServeHTTP(w, r)
		return w
	}
	port := func(w *httptest.ResponseRecorder) int {
		var c config.Manager
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatalf("config: %v (%s)", err, w.Body)
		}
		return c.ServerPort
	}
	if p := port(do("/api/config", "")); p != 2302 {
		t.Fatalf("no header: port %d, want the primary's 2302", p)
	}
	if p := port(do("/api/config", second.ID)); p != 2402 {
		t.Fatalf("header %s: port %d, want 2402", second.ID, p)
	}
	if p := port(do("/api/config?srv="+second.ID, "")); p != 2402 {
		t.Fatalf("?srv=: port %d, want 2402", p)
	}
	if w := do("/api/config", "nope"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "servers.unknown") {
		t.Fatalf("unknown server: %d %s", w.Code, w.Body)
	}
	// Panel-wide: works whatever server the tab is on.
	if w := do("/api/i18n?lang=en", "nope"); w.Code != http.StatusOK {
		t.Fatalf("i18n with an unknown server header: %d", w.Code)
	}
	if w := do("/api/servers", second.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Second"`) {
		t.Fatalf("server list: %d %s", w.Code, w.Body)
	}
	// /api/info names the server of the request, and lists every managed
	// folder for a manager starting on this PC (loopback).
	var info map[string]interface{}
	_ = json.Unmarshal(do("/api/info", second.ID).Body.Bytes(), &info)
	if info["serverId"] != second.ID || info["serverDir"] != second.ServerDir {
		t.Fatalf("info: %v", info)
	}
	if dirs, _ := info["managedDirs"].([]interface{}); len(dirs) != 2 {
		t.Fatalf("managedDirs: %v", info["managedDirs"])
	}
	// Removing the server stops serving it.
	if err := f.Remove(second.ID); err != nil {
		t.Fatal(err)
	}
	s.detach(second.ID)
	if w := do("/api/config", second.ID); w.Code != http.StatusNotFound {
		t.Fatalf("removed server still answers: %d", w.Code)
	}
}
