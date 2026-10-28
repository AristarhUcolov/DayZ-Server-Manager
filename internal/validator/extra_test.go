// Copyright (c) 2026 Aristarh Ucolov.
package validator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codes(is []Issue) map[string]int {
	out := map[string]int{}
	for _, i := range is {
		out[i.Code]++
	}
	return out
}

func TestLenientJSON(t *testing.T) {
	src := `{
  // comment
  "a": [1, 2, 3,],
  "url": "http://x//y, z", /* block */
  "b": {"c": true,},
}`
	fixed := lenientJSON([]byte(src))
	var v map[string]interface{}
	if err := json.Unmarshal(fixed, &v); err != nil {
		t.Fatalf("still invalid: %v\n%s", err, fixed)
	}
	if v["url"] != "http://x//y, z" {
		t.Errorf("string content altered: %q", v["url"])
	}
}

func TestCfgSemicolons(t *testing.T) {
	dir := t.TempDir()
	cfg := `hostname = "My // server";  // name
password = ""
maxPlayers = 60;
motd[] = {
  "Welcome",
  "Have fun"
};
motdInterval = 1
class Missions
{
    class DayZ
    {
        template="dayzOffline.chernarusplus"
    };
};
maxPlayers = 50;
`
	write(t, filepath.Join(dir, "serverDZ.cfg"), cfg)
	bad := cfgMissingSemicolons(cfg)
	want := []int{2, 8, 13}
	if len(bad) != len(want) {
		t.Fatalf("missing-semicolon lines = %v, want %v", bad, want)
	}
	for i := range want {
		if bad[i] != want[i] {
			t.Fatalf("missing-semicolon lines = %v, want %v", bad, want)
		}
	}
	o := Options{ServerDir: dir, ServerCfg: "serverDZ.cfg", ServerPort: 2302}
	is := checkServerCfg(o)
	c := codes(is)
	if c["cfg.semicolon"] != 3 || c["cfg.duplicateKey"] != 1 || c["cfg.missionMissing"] != 1 {
		t.Fatalf("codes = %v", c)
	}
	if _, err := fixServerCfgSemicolons(o); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "serverDZ.cfg"))
	if left := cfgMissingSemicolons(string(after)); len(left) != 0 {
		t.Fatalf("lines still missing ';' after fix: %v\n%s", left, after)
	}
	if !strings.Contains(string(after), `hostname = "My // server";  // name`) {
		t.Errorf("a correct line was altered:\n%s", after)
	}
	if !strings.Contains(string(after), `password = "";`) {
		t.Errorf("fix not applied:\n%s", after)
	}
}

func TestPortsClash(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "serverDZ.cfg"), "steamQueryPort = 2303;\n")
	is := checkServerCfg(Options{ServerDir: dir, ServerCfg: "serverDZ.cfg", ServerPort: 2302, RConPort: 2304})
	if codes(is)["ports.clash"] != 2 {
		t.Fatalf("expected query + rcon clashes, got %+v", is)
	}
}

func TestEscapeBareAmps(t *testing.T) {
	got, n := escapeBareAmps(`a & b &amp; &#38; &#x26; &lt; &x <c/>`)
	want := `a &amp; b &amp; &#38; &#x26; &lt; &amp;x <c/>`
	if got != want || n != 2 {
		t.Fatalf("got %q (%d), want %q (2)", got, n, want)
	}
}

func TestClosestCEType(t *testing.T) {
	for in, want := range map[string]string{
		"type": "types", "spawnabletype": "spawnabletypes", "Event": "events", "globalz": "globals", "zzz": "",
	} {
		if got := closestCEType(in); got != want {
			t.Errorf("closestCEType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckModsAndFixKeys(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "@A", "keys", "a.bikey"), "k")
	write(t, filepath.Join(dir, "@A", "addons", "a.pbo"), "x")
	o := Options{ServerDir: dir, Mods: []string{"@A", "@B", "@a"}}
	c := codes(checkMods(o))
	if c["mods.missing"] != 1 || c["mods.duplicate"] != 1 || c["mods.keys"] != 1 {
		t.Fatalf("codes = %v", c)
	}
	if _, err := fixModKeys(o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "keys", "a.bikey")); err != nil {
		t.Fatalf("key not synced: %v", err)
	}
	if codes(checkMods(o))["mods.keys"] != 0 {
		t.Errorf("mods.keys still reported after the fix")
	}
}

func TestSpawnableAndGlobalsFixes(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "mpmissions", "m.world")
	write(t, filepath.Join(md, "cfgspawnabletypes.xml"), `<spawnabletypes>
	<type name="AKM">
		<attachments chance="25">
			<item name="Mag_AKM_30Rnd" chance="-1"/>
		</attachments>
		<attachments preset="ghostPreset"/>
	</type>
</spawnabletypes>
`)
	write(t, filepath.Join(md, "cfgrandompresets.xml"), `<randompresets><cargo name="realPreset" chance="1"><item name="Apple" chance="1"/></cargo></randompresets>`)
	write(t, filepath.Join(md, "db", "globals.xml"), `<variables>
    <var name="A" type="0" value="1"/>
    <var name="B" type="0" value="2"/>
    <var name="A" type="0" value="3"/>
</variables>
`)
	o := Options{ServerDir: dir, Mission: "m.world"}
	if codes(checkSpawnableChances(md))["spawnable.chance"] != 1 || codes(checkGlobals(md))["globals.duplicate"] != 1 {
		t.Fatalf("checks did not fire")
	}
	if _, err := fixSpawnable(o, true, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(md, "cfgspawnabletypes.xml"))
	s := string(raw)
	if !strings.Contains(s, `chance="0.25"`) || !strings.Contains(s, `chance="0.00"`) || strings.Contains(s, "ghostPreset") {
		t.Fatalf("spawnable not fixed:\n%s", s)
	}
	// Item weights above 1 (BI uses 40.00 / 30.00) are valid and untouched.
	write(t, filepath.Join(md, "cfgspawnabletypes.xml"), `<spawnabletypes><type name="AKM"><attachments chance="1.00"><item name="A" chance="40.00"/><item name="B" chance="30.00"/></attachments></type></spawnabletypes>`)
	if n := len(checkSpawnableChances(md)); n != 0 {
		t.Fatalf("relative item weights flagged: %d issue(s)", n)
	}
	if _, err := fixGlobals(o); err != nil {
		t.Fatal(err)
	}
	g, _ := os.ReadFile(filepath.Join(md, "db", "globals.xml"))
	if strings.Count(string(g), `name="A"`) != 1 || !strings.Contains(string(g), `value="1"`) {
		t.Fatalf("globals not deduped (first kept):\n%s", g)
	}
}

// A repeated cfgspawnabletypes.xml entry is reported with a fix, and the fix
// keeps the first entry, the comments and the other entries.
func TestSpawnableDuplicateFix(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "mpmissions", "m.world")
	write(t, filepath.Join(md, "cfgspawnabletypes.xml"), `<spawnabletypes>
	<!-- hunting gear -->
	<type name="HuntingVest">
		<cargo chance="0.30"><item name="Apple" chance="1"/></cargo>
	</type>
	<type name="AKM">
		<attachments chance="1.00"><item name="Mag_AKM_30Rnd" chance="1.00"/></attachments>
	</type>
	<type name="huntingvest">
		<cargo chance="0.90"><item name="Pear" chance="1"/></cargo>
	</type>
</spawnabletypes>
`)
	is := checkSpawnable(md)
	if codes(is)["spawnable.duplicate"] != 1 || is[0].Fix != "spawnable.duplicate" {
		t.Fatalf("duplicate not reported with a fix: %+v", is)
	}
	done, err := AutoFixWith(Options{ServerDir: dir, Mission: "m.world"}, []string{"spawnable.duplicate"})
	if err != nil || len(done) != 1 || done[0].Key != "spawnableDedup" || done[0].Args["n"] != "1" {
		t.Fatalf("fix: %+v, %v", done, err)
	}
	raw, _ := os.ReadFile(filepath.Join(md, "cfgspawnabletypes.xml"))
	s := string(raw)
	if strings.Contains(s, "Pear") || !strings.Contains(s, "Apple") || !strings.Contains(s, "AKM") || !strings.Contains(s, "<!-- hunting gear -->") {
		t.Fatalf("wrong entry removed:\n%s", s)
	}
	if n := codes(checkSpawnable(md))["spawnable.duplicate"]; n != 0 {
		t.Errorf("still %d duplicate(s) after the fix", n)
	}
}

func TestInitCBraces(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "mpmissions", "m.w")
	write(t, filepath.Join(md, "init.c"), "void main() {\n  string s = \"}\"; // }\n  /* { */\n")
	if codes(checkInitC(Options{ServerDir: dir, Mission: "m.w"}))["initc.braces"] != 1 {
		t.Fatal("unbalanced init.c not reported")
	}
	write(t, filepath.Join(md, "init.c"), "void main() {\n  string s = \"}\"; // }\n}\n")
	if n := len(checkInitC(Options{ServerDir: dir, Mission: "m.w"})); n != 0 {
		t.Fatalf("balanced init.c reported %d issues", n)
	}
}

func TestEncodingFix(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "mpmissions", "m.w")
	// "<a/>" as UTF-16LE with BOM.
	write(t, filepath.Join(md, "x.xml"), string([]byte{0xFF, 0xFE, '<', 0, 'a', 0, '/', 0, '>', 0}))
	o := Options{ServerDir: dir, Mission: "m.w"}
	if codes(checkEncoding(o))["encoding.utf16"] != 1 {
		t.Fatal("UTF-16 not detected")
	}
	if _, err := fixEncoding(o); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(md, "x.xml"))
	if string(b) != "<a/>" {
		t.Fatalf("converted = %q", b)
	}
}

// TestStockMissionsStayClean: the new checks must not fire on the shipped
// vanilla missions (a check that cries wolf teaches admins to ignore the
// page). Runs against the local reference server when it is present.
func TestStockMissionsStayClean(t *testing.T) {
	server := filepath.Join("..", "..", "DayZServer")
	if _, err := os.Stat(filepath.Join(server, "mpmissions")); err != nil {
		t.Skip("reference DayZServer fixture not present")
	}
	newCodes := map[string]bool{"json.syntax": true, "encoding.utf16": true, "initc.braces": true,
		"spawnable.chance": true, "globals.duplicate": true, "ce.missing": true, "ce.type": true}
	for _, m := range []string{"dayzOffline.chernarusplus", "dayzOffline.enoch", "dayzOffline.sakhal"} {
		is, err := ValidateWith(Options{ServerDir: server, Mission: m, WorldSize: 15360})
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range is {
			if newCodes[i.Code] {
				t.Errorf("%s: false positive %s: %s (%s:%d)", m, i.Code, i.Message, filepath.Base(i.File), i.Line)
			}
		}
	}
}
