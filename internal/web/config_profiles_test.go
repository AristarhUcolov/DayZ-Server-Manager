// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
)

func profileApp(t *testing.T) (*handlers, *app.App) {
	t.Helper()
	a, err := app.New(fakeDayZ(t), "DayZ Server Manager", "test", "t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Detach(); a.CloseLog() })
	return &handlers{app: a}, a
}

func saveProfile(t *testing.T, h *handlers, body string) cfgProfile {
	t.Helper()
	rec := httptest.NewRecorder()
	h.cfgProfileSave(rec, httptest.NewRequest("POST", "/api/config-profiles/save", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("save %s: %d %s", body, rec.Code, rec.Body.String())
	}
	var p cfgProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Applying a profile restores the game files and the mod list, but never the
// panel's own settings: the schedule that fired the swap, Discord, RCon.
func TestProfileApplyKeepsPanelSettings(t *testing.T) {
	h, a := profileApp(t)
	cfgPath := filepath.Join(a.ServerDir, "serverDZ.cfg")
	put(t, cfgPath, "hostname = \"weekend\";\n")
	if err := a.MutateConfig(func(c *config.Manager) error { c.Mods = []string{"@Weekend"}; return nil }); err != nil {
		t.Fatal(err)
	}
	p := saveProfile(t, h, `{"name":"Weekend"}`)

	put(t, cfgPath, "hostname = \"weekday\";\n")
	if err := a.MutateConfig(func(c *config.Manager) error {
		c.Mods = []string{"@Weekday"}
		c.ScheduledProfiles = []config.ScheduledProfile{{Time: "18:00", Profile: p.Slug, Enabled: true}}
		c.DiscordWebhookURL = "https://discord.example/hook"
		c.RConPassword = "secret"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := h.applyProfileBySlug(p.Slug); err != nil {
		t.Fatal(err)
	}
	c := a.Cfg()
	if len(c.Mods) != 1 || c.Mods[0] != "@Weekend" {
		t.Fatalf("mods = %v, want the profile's", c.Mods)
	}
	if len(c.ScheduledProfiles) != 1 || c.DiscordWebhookURL == "" || c.RConPassword != "secret" {
		t.Fatalf("panel settings were rolled back: %+v %q %q", c.ScheduledProfiles, c.DiscordWebhookURL, c.RConPassword)
	}
	if got, _ := os.ReadFile(cfgPath); !strings.Contains(string(got), "weekend") {
		t.Fatalf("serverDZ.cfg not restored: %s", got)
	}
}

// Import copies the picked mission into this server before switching the
// template to it, and never points the server at a mission it lacks.
func TestImportCopiesTheMission(t *testing.T) {
	h, a := profileApp(t)
	cfgPath := filepath.Join(a.ServerDir, "serverDZ.cfg")
	put(t, cfgPath, "hostname = \"x\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"dayzOffline.chernarusplus\";\n\t};\n};\n")
	src := t.TempDir()
	put(t, filepath.Join(src, "mpmissions", "custom.deerisle", "init.c"), "void main() {}")
	apply := func(mission string) map[string]interface{} {
		body, _ := json.Marshal(map[string]string{"sourceDir": src, "mission": mission})
		rec := httptest.NewRecorder()
		h.importApply(rec, httptest.NewRequest("POST", "/api/import/apply", strings.NewReader(string(body))))
		if rec.Code != 200 {
			t.Fatalf("import %s: %d %s", mission, rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	template := func() string {
		c, err := config.LoadServerCfg(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		return c.MissionTemplate()
	}

	if out := apply("custom.deerisle"); out["copiedMission"] != "custom.deerisle" {
		t.Fatalf("mission not reported as copied: %v", out)
	}
	if _, err := os.Stat(filepath.Join(a.ServerDir, "mpmissions", "custom.deerisle", "init.c")); err != nil {
		t.Fatalf("mission folder not copied: %v", err)
	}
	if got := template(); got != "custom.deerisle" {
		t.Fatalf("template = %q", got)
	}
	if out := apply("nowhere.map"); out["missionError"] == "" {
		t.Fatalf("a mission neither server has must be refused: %v", out)
	}
	if got := template(); got != "custom.deerisle" {
		t.Fatalf("template switched to a missing mission: %q", got)
	}
}

// Names that slugify to nothing or to the same slug must not collide, and
// saving a name again (any case) updates that profile.
func TestProfileNamesDoNotCollide(t *testing.T) {
	h, _ := profileApp(t)
	a := saveProfile(t, h, `{"name":"PvP выходные"}`)
	b := saveProfile(t, h, `{"name":"PvP будни"}`)
	if a.Slug == b.Slug {
		t.Fatalf("two names share slug %q", a.Slug)
	}
	c := saveProfile(t, h, `{"name":"Выходные"}`)
	d := saveProfile(t, h, `{"name":"выходные","note":"v2"}`)
	if c.Slug != d.Slug {
		t.Fatalf("saving the same name again made a new profile: %q vs %q", c.Slug, d.Slug)
	}
	entries, _ := os.ReadDir(h.cfgProfilesDir())
	if len(entries) != 3 {
		t.Fatalf("want 3 profiles, have %d", len(entries))
	}
	if e := saveProfile(t, h, `{"name":"Renamed","slug":"`+a.Slug+`"}`); e.Slug != a.Slug {
		t.Fatalf("update by slug wrote %q", e.Slug)
	}
}
