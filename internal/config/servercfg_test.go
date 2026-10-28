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

// A numeric password used to be written bare — `password = 1234;` — which
// BattlEye reads as no password at all. The panel reported "saved" while
// locking every player out.
func TestSetStringAlwaysQuotes(t *testing.T) {
	cfg := &ServerCfg{}
	cfg.SetString("password", "1234")
	cfg.SetString("hostname", "12345")
	cfg.SetString("passwordAdmin", "0000")

	for _, k := range []string{"password", "hostname", "passwordAdmin"} {
		v := ""
		for _, e := range cfg.Entries {
			if e.Key == k {
				v = e.Value
			}
		}
		if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
			t.Errorf("%s was written as %s — must be a quoted string", k, v)
		}
	}
	// And it must read back as the original text.
	if got, _ := cfg.Get("password"); got != "1234" {
		t.Errorf("password round-tripped as %q", got)
	}
}

// Enfusion escapes an embedded quote by doubling it, not with a backslash.
func TestSetStringDoublesEmbeddedQuotes(t *testing.T) {
	cfg := &ServerCfg{}
	cfg.SetString("hostname", `My "Best" Server`)
	v, _ := cfg.Get("hostname")
	if v != `My ""Best"" Server` {
		// Get only strips the outer quotes; what matters is the escaping form.
		raw := ""
		for _, e := range cfg.Entries {
			if e.Key == "hostname" {
				raw = e.Value
			}
		}
		if !strings.Contains(raw, `""Best""`) {
			t.Errorf("embedded quotes were not doubled: %s", raw)
		}
	}
}

// A file that spells a key with different casing must be updated, not
// duplicated — two definitions of the same key is a broken server.cfg.
func TestSetIsCaseInsensitiveAndDoesNotDuplicate(t *testing.T) {
	cfg := &ServerCfg{Entries: []ServerCfgEntry{
		{Kind: EntryKV, Key: "hostName", Value: `"old"`},
	}}
	cfg.SetString("hostname", "new")
	n := 0
	for _, e := range cfg.Entries {
		if e.Kind == EntryKV && strings.EqualFold(e.Key, "hostname") {
			n++
			if e.Value != `"new"` {
				t.Errorf("value = %s, want \"new\"", e.Value)
			}
		}
	}
	if n != 1 {
		t.Errorf("key defined %d times after Set — must stay 1", n)
	}
}

// A save must not reformat the admin's file: untouched lines come back byte
// for byte (spacing, tab-aligned comments, whitespace-only lines), and a
// changed value is swapped inside its own line.
func TestSaveKeepsLinesAsWritten(t *testing.T) {
	const src = "hostname = \"EXAMPLE NAME\";  // Server name\n" +
		"password = \"\";              // Password to connect to the server\n" +
		"description = \"PvE; no raids\";\t\t\t// Description\n" +
		" \n" +
		"thirdPersonView=1;          // 3rd person view\n" +
		"maxPlayers = 60;            // Maximum amount of players\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "serverDZ.cfg")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServerCfg(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Get("description"); got != "PvE; no raids" {
		t.Errorf("description = %q — a ';' inside quotes cut the value", got)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != src {
		t.Fatalf("an unchanged file was rewritten:\n%s", data)
	}

	// A file whose last line has no line ending keeps it that way.
	trimmed := strings.TrimSuffix(src, "\n")
	os.WriteFile(path, []byte(trimmed), 0o644)
	if c2, err := LoadServerCfg(path); err != nil || c2.Save(path) != nil {
		t.Fatalf("round trip without a final newline: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != trimmed {
		t.Fatalf("a final line ending was added: %q", data)
	}
	os.WriteFile(path, []byte(src), 0o644)
	if cfg, err = LoadServerCfg(path); err != nil {
		t.Fatal(err)
	}

	cfg.Set("maxPlayers", 80)
	cfg.Set("thirdPersonView", 1) // same value: the line stays as it was
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "maxPlayers = 60;", "maxPlayers = 80;", 1)
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Fatalf("saving one change touched other lines:\n%s", data)
	}
}

// A Windows file keeps its CRLF line endings, class block included.
func TestSaveKeepsCRLF(t *testing.T) {
	src := "hostname = \"x\";\r\nmaxPlayers = 60;\r\nclass Missions\r\n{\r\n\tclass DayZ\r\n\t{\r\n\t\ttemplate=\"dayzOffline.chernarusplus\";\r\n\t};\r\n};\r\n"
	path := filepath.Join(t.TempDir(), "serverDZ.cfg")
	os.WriteFile(path, []byte(src), 0o644)
	cfg, err := LoadServerCfg(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Set("maxPlayers", 70)
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if want := strings.Replace(src, "60", "70", 1); string(data) != want {
		t.Fatalf("got %q\nwant %q", data, want)
	}
}
