package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The exact block the user pasted in their bug report.
const userMissionsBlock = `hostname = "DayZ Server";

class Missions
{
    class DayZ
    {
        template="dayzOffline.chernarusplus"; // Mission to load on server startup. <MissionName>.<TerrainName>
					      // Vanilla mission: dayzOffline.chernarusplus
					      // DLC mission: dayzOffline.enoch
    };
};

password = "";
`

func TestSetMissionTemplate_ReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serverDZ.cfg")
	if err := os.WriteFile(path, []byte(userMissionsBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServerCfg(path)
	if err != nil {
		t.Fatalf("LoadServerCfg: %v", err)
	}
	if got := cfg.MissionTemplate(); got != "dayzOffline.chernarusplus" {
		t.Fatalf("MissionTemplate before set: got %q, want chernarusplus", got)
	}
	if !cfg.SetMissionTemplate("dayzOffline.enoch") {
		t.Fatal("SetMissionTemplate returned false")
	}
	if got := cfg.MissionTemplate(); got != "dayzOffline.enoch" {
		t.Fatalf("MissionTemplate after set: got %q, want enoch", got)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	count := strings.Count(string(saved), "class Missions")
	if count != 1 {
		t.Fatalf("class Missions appears %d times after save; want 1\n%s", count, string(saved))
	}
	if !strings.Contains(string(saved), `template="dayzOffline.enoch"`) {
		t.Fatalf("saved file missing new template:\n%s", string(saved))
	}
}
