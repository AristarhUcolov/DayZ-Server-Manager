// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/config"
)

// fakeServer makes a folder that looks like a DayZ server install.
func fakeServer(t *testing.T, cfg string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DayZServer_x64.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DayZServer"), []byte("\x7fELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(dir, "serverDZ.cfg"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newPrimary(t *testing.T) *App {
	t.Helper()
	a, err := New(fakeServer(t, "hostname = \"Main\";\nsteamQueryPort = 27016;\n"), "DayZ Server Manager", "test", "t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Detach(); a.CloseLog() })
	return a
}

// closeAll ends every server's loops and log (Windows cannot delete a temp
// folder with an open manager.log).
func closeAll(t *testing.T, f *Fleet) {
	t.Cleanup(func() {
		for _, a := range f.Apps() {
			a.Detach()
			a.CloseLog()
		}
	})
}

func TestFleetAddRenameMoveRemove(t *testing.T) {
	primary := newPrimary(t)
	f := OpenFleet(primary)
	closeAll(t, f)
	if f.Multi() || len(f.Members()) != 1 {
		t.Fatalf("fresh fleet: %+v", f.Members())
	}
	second := fakeServer(t, "hostname = \"Second\";\n")
	a, err := f.Add(second, "  Livonia   PvE ")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Detach() })
	if a.Label() != "Livonia PvE" || a.Primary != primary || f.Get(a.ID) != a {
		t.Fatalf("added: label %q primary %v", a.Label(), a.Primary == primary)
	}
	// Each server keeps its own manager folder.
	if _, err := os.Stat(filepath.Join(second, ".dayz-manager", "manager.json")); err != nil {
		t.Fatalf("no manager.json for the added server: %v", err)
	}
	// The same folder, a folder inside another server, or around one: refused.
	if _, err := f.Add(second, "again"); err == nil {
		t.Fatal("the same folder twice must be refused")
	}
	inner := filepath.Join(second, "sub")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(inner, "DayZServer_x64.exe"), []byte("MZ"), 0o755)
	if _, err := f.Add(inner, "nested"); err == nil || !strings.Contains(err.Error(), "inside") {
		t.Fatalf("a folder inside another server must be refused, got %v", err)
	}
	if _, err := f.Add(t.TempDir(), "empty"); err == nil {
		t.Fatal("a folder without a DayZ server must be refused")
	}
	if _, err := f.Add("relative/path", "x"); err == nil {
		t.Fatal("a relative path must be refused")
	}

	if err := f.Rename(a.ID, "Livonia"); err != nil || a.Label() != "Livonia" {
		t.Fatalf("rename: %v %q", err, a.Label())
	}
	// Persisted, and reopened on the next start.
	var ff fleetFile
	data, _ := os.ReadFile(filepath.Join(primary.ManagerDir, "servers.json"))
	if err := json.Unmarshal(data, &ff); err != nil || len(ff.Servers) != 2 || ff.Servers[1].Name != "Livonia" || ff.Servers[1].Dir != second {
		t.Fatalf("servers.json: %s (%v)", data, err)
	}
	a.Detach()
	f2 := OpenFleet(primary)
	if m := f2.Members(); len(m) != 2 || m[1].App == nil || m[1].Name != "Livonia" {
		t.Fatalf("reopened: %+v", m)
	}
	f2.Apps()[1].Detach()
	f2.Apps()[1].CloseLog()

	if err := f.Remove(PrimaryID); err == nil {
		t.Fatal("the primary cannot be removed")
	}
	if err := f.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if f.Multi() || f.Get(a.ID) != nil {
		t.Fatal("removed server still listed")
	}
	// Nothing was deleted.
	if _, err := os.Stat(filepath.Join(second, ".dayz-manager", "manager.json")); err != nil {
		t.Fatalf("remove must keep the files: %v", err)
	}
}

// A missing folder stays in the list with its error instead of creating an
// empty tree where a drive used to be.
func TestFleetMissingFolderIsKeptNotCreated(t *testing.T) {
	primary := newPrimary(t)
	gone := filepath.Join(t.TempDir(), "unplugged")
	data, _ := json.Marshal(fleetFile{Servers: []fleetEntry{{ID: PrimaryID}, {ID: "s1", Name: "Old", Dir: gone}}})
	if err := os.WriteFile(filepath.Join(primary.ManagerDir, "servers.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	f := OpenFleet(primary)
	closeAll(t, f)
	m := f.Members()
	if len(m) != 2 || m[1].App != nil || m[1].Error == "" {
		t.Fatalf("members: %+v", m)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatal("opening a missing server must not create its folder")
	}
}

// Language, exposure and the other panel-wide settings belong to the primary:
// an added server reads them from there and a change made through it lands
// there.
func TestPanelWideSettingsLiveWithThePrimary(t *testing.T) {
	primary := newPrimary(t)
	f := OpenFleet(primary)
	closeAll(t, f)
	a, err := f.Add(fakeServer(t, ""), "Second")
	if err != nil {
		t.Fatal(err)
	}
	if err := primary.MutateConfig(func(c *config.Manager) error {
		c.Language = "de"
		c.Exposure = "lan"
		c.FirstRunDone = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if c := a.Cfg(); c.Language != "de" || c.Exposure != "lan" || !c.FirstRunDone {
		t.Fatalf("added server sees language %q exposure %q firstRun %v", c.Language, c.Exposure, c.FirstRunDone)
	}
	if err := a.MutateConfig(func(c *config.Manager) error { c.Language = "fr"; c.ServerPort = 2402; return nil }); err != nil {
		t.Fatal(err)
	}
	if primary.Cfg().Language != "fr" {
		t.Fatal("a language change made through an added server must reach the primary")
	}
	if primary.Cfg().ServerPort == 2402 || a.Cfg().ServerPort != 2402 {
		t.Fatal("the game port belongs to each server")
	}
}

func TestPortClashesAndFreePorts(t *testing.T) {
	primary := newPrimary(t)
	f := OpenFleet(primary)
	closeAll(t, f)
	a, err := f.Add(fakeServer(t, "hostname = \"Second\";\nsteamQueryPort = 27016;\n"), "Second")
	if err != nil {
		t.Fatal(err)
	}
	c := f.PortClashes(a)
	if len(c) != 1 || !strings.Contains(c[0], "2302–2306") || !strings.Contains(c[0], "27016") {
		t.Fatalf("clashes: %v", c)
	}
	p, err := f.AssignFreePorts(a)
	if err != nil {
		t.Fatal(err)
	}
	if p.Game != 2402 || p.RCon != 2406 || p.Query != 27017 {
		t.Fatalf("free ports: %+v", p)
	}
	if got := a.Ports(); got != p {
		t.Fatalf("ports after assign: %+v, want %+v", got, p)
	}
	if c := f.PortClashes(a); len(c) != 0 {
		t.Fatalf("still clashing: %v", c)
	}
	if got := portRanges([]int{27016, 2302, 2303, 2304, 2305, 2306}); got != "2302–2306, 27016" {
		t.Errorf("portRanges = %q", got)
	}
	if got := portRanges([]int{5, 7, 8}); got != "5, 7, 8" {
		t.Errorf("portRanges = %q", got)
	}
}
