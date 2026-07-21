// Copyright (c) 2026 Aristarh Ucolov.
package validator

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

func TestWhitelistLimitsInsertsAndStaysValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfglimitsdefinition.xml")
	original := `<?xml version="1.0" encoding="UTF-8"?>
<lists>
	<categories>
		<category name="weapons"/>
	</categories>
	<tags>
		<tag name="floor"/>
	</tags>
	<usageflags>
		<usage name="Military"/>
	</usageflags>
	<valueflags>
		<value name="Tier1"/>
	</valueflags>
</lists>
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	miss := map[string]map[string]string{
		"category": {"food": "Food"},
		"usage":    {"custom": "Custom"},
		"value":    {},
		"tag":      {"shelf": "Shelf"},
	}
	written, err := whitelistLimits(path, miss)
	if err != nil {
		t.Fatal(err)
	}
	// AutoFix reports from this map, so it must describe what really landed.
	if len(written["category"]) != 1 || len(written["usage"]) != 1 || len(written["tag"]) != 1 {
		t.Errorf("written set does not match the insertions: %#v", written)
	}
	if len(written["value"]) != 0 {
		t.Errorf("reported a value insertion that was never requested: %#v", written["value"])
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	for _, want := range []string{`name="Food"`, `name="Custom"`, `name="Shelf"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing inserted %s\n%s", want, s)
		}
	}
	// Must still be well-formed XML.
	if err := xml.Unmarshal(data, new(struct {
		XMLName xml.Name `xml:"lists"`
	})); err != nil {
		t.Fatalf("result is not valid XML: %v", err)
	}
	// A .bak of the original must exist.
	if _, err := os.Stat(path + ".bak"); err != nil {
		// util.BackupBeforeWrite may name it differently; just ensure the
		// original content is recoverable somewhere in the dir.
		entries, _ := os.ReadDir(dir)
		found := false
		for _, e := range entries {
			if strings.Contains(e.Name(), "cfglimitsdefinition") && strings.Contains(e.Name(), "bak") {
				found = true
			}
		}
		if !found {
			t.Error("no backup file written before edit")
		}
	}
}

// The shape BI actually ships: <user> nested inside <usageflags>/<valueflags>.
// The old parser looked for <user> at the root and silently merged nothing, so
// every types entry using a group name was about to be reported as unknown.
func TestMergeUserLimitsParsesTheRealSchema(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "cfglimitsdefinitionuser.xml")
	os.WriteFile(up, []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<user_lists>
    <usageflags>
        <user name="TownVillage">
            <usage name="Town" />
            <usage name="Village" />
        </user>
    </usageflags>
    <valueflags>
        <user name="Tier1234">
            <value name="Tier1" />
            <value name="Tier4" />
        </user>
    </valueflags>
</user_lists>`), 0o644)

	l := newLimits()
	mergeUserLimits(l, up)
	if !l.usages["townvillage"] {
		t.Error("usage group TownVillage was not merged")
	}
	if !l.values["tier1234"] {
		t.Error("value group Tier1234 was not merged")
	}
	// A usage group is not a valid <value>; keeping them separate turns a
	// swallowed mistake back into a reported one.
	if l.values["townvillage"] {
		t.Error("usage group leaked into the value set")
	}
}

// The flat hand-written variant must keep working.
func TestMergeUserLimitsAcceptsFlatVariant(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "cfglimitsdefinitionuser.xml")
	os.WriteFile(up, []byte(`<lists><user name="Tier1234"><value name="Tier1"/></user></lists>`), 0o644)
	l := newLimits()
	mergeUserLimits(l, up)
	if !l.usages["tier1234"] || !l.values["tier1234"] {
		t.Fatal("flat user group not merged into usages/values")
	}
}

func ptr(v int) *int { return &v }

func TestCheckTypeSanity(t *testing.T) {
	cases := []struct {
		name  string
		ty    dztypes.Type
		wants string // substring the message must contain; "" = no issue
	}{
		{"min above nominal", dztypes.Type{Name: "AKM", Nominal: ptr(13), Min: ptr(18)}, "greater than nominal"},
		// The guard that keeps this quiet on vanilla: 172 chernarusplus types
		// legitimately read nominal=0 min=1.
		{"nominal zero is fine", dztypes.Type{Name: "Nail", Nominal: ptr(0), Min: ptr(1)}, ""},
		{"lifetime zero with nominal", dztypes.Type{Name: "Rag", Nominal: ptr(5), Lifetime: ptr(0)}, "despawns the instant"},
		{"lifetime zero without nominal is fine", dztypes.Type{Name: "Wreck", Nominal: ptr(0), Lifetime: ptr(0)}, ""},
		{"inverted quant", dztypes.Type{Name: "Can", QuantMin: ptr(80), QuantMax: ptr(20)}, "greater than quantmax"},
		{"quant -1 is fine", dztypes.Type{Name: "Can", QuantMin: ptr(-1), QuantMax: ptr(-1)}, ""},
		{"quant out of range", dztypes.Type{Name: "Mag", QuantMin: ptr(30), QuantMax: ptr(300)}, "percentage"},
		{"empty name", dztypes.Type{Name: "   ", Nominal: ptr(1)}, "empty name attribute"},
		{"negative restock", dztypes.Type{Name: "X", Restock: ptr(-5)}, "negative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkTypeSanity("types.xml", &c.ty)
			if c.wants == "" {
				if len(got) != 0 {
					t.Fatalf("expected no issue, got %v", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected an issue containing %q, got none", c.wants)
			}
			if !strings.Contains(got[0].Message, c.wants) {
				t.Fatalf("message %q does not contain %q", got[0].Message, c.wants)
			}
		})
	}
}

// One misspelled usage shared by many types must produce one line, not one per
// type — otherwise a single mod typo buries every other finding.
func TestUnknownRefsAreAggregated(t *testing.T) {
	l := newLimits()
	l.loaded = true
	l.usages["military"] = true

	agg := newUnknownAgg()
	for _, n := range []string{"AKM", "M4", "Mosin"} {
		agg.collect("types.xml", &dztypes.Type{
			Name:   n,
			Usages: []dztypes.NamedRef{{Name: "Miltary"}},
		}, l)
	}
	got := agg.issues()
	if len(got) != 1 {
		t.Fatalf("expected 1 aggregated issue, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "3 types") {
		t.Errorf("aggregated message should carry the count, got %q", got[0].Message)
	}
}

func TestCheckEvents(t *testing.T) {
	dir := t.TempDir()
	ev := filepath.Join(dir, "events.xml")
	os.WriteFile(ev, []byte(`<events>
  <event name="Good"><nominal>5</nominal><min>1</min><max>10</max><active>1</active></event>
  <event name="Ambient"><nominal>5</nominal><min>100</min><max>0</max><active>1</active></event>
  <event name="Bad"><nominal>5</nominal><min>10</min><max>5</max><active>1</active></event>
  <event name="Off"><nominal>7</nominal><active>0</active></event>
  <event name="Good"><nominal>1</nominal><active>1</active></event>
</events>`), 0o644)
	sp := filepath.Join(dir, "cfgeventspawns.xml")
	os.WriteFile(sp, []byte(`<eventposdef>
  <event name="Good"><pos x="1" z="2"/></event>
  <event name="Ghost"><pos x="1" z="2"/></event>
</eventposdef>`), 0o644)

	got := checkEvents(ev, sp)
	joined := ""
	for _, i := range got {
		joined += i.Message + "\n"
	}
	for _, want := range []string{
		`duplicate event "Good"`,
		`event "Bad": min (10) is greater than max (5)`,
		`spawn positions are defined for event "Ghost"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	// max="0" means unlimited — vanilla ambient events read min=100 max=0 and
	// must not be flagged.
	if strings.Contains(joined, `event "Ambient"`) {
		t.Errorf("max=0 (unlimited) was wrongly flagged:\n%s", joined)
	}
}

// Braces inside a quoted value are data, not structure.
func TestValidateCFGIgnoresBracesInStrings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.cfg")
	os.WriteFile(p, []byte("hostname = \"My { server\";\nclass Missions\n{\n\tclass DayZ\n\t{\n\t};\n};\n"), 0o644)
	if is := validateCFG(p); is != nil {
		t.Fatalf("false positive on braces inside a string: %v", is.Message)
	}
	p2 := filepath.Join(dir, "broken.cfg")
	os.WriteFile(p2, []byte("class Missions\n{\n\tclass DayZ\n\t{\n"), 0o644)
	if is := validateCFG(p2); is == nil {
		t.Fatal("genuinely unbalanced braces were not reported")
	}
}
