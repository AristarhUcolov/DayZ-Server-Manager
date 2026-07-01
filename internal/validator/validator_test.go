// Copyright (c) 2026 Aristarh Ucolov.
package validator

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := whitelistLimits(path, miss); err != nil {
		t.Fatal(err)
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

func TestMergeUserLimits(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "cfglimitsdefinitionuser.xml")
	os.WriteFile(up, []byte(`<lists><user name="Tier1234"><value name="Tier1"/></user></lists>`), 0o644)
	l := &limits{usages: map[string]bool{}, values: map[string]bool{}, categories: map[string]bool{}, tags: map[string]bool{}, loaded: true}
	mergeUserLimits(l, up)
	if !l.usages["tier1234"] || !l.values["tier1234"] {
		t.Fatal("user group name not merged into usages/values")
	}
}
