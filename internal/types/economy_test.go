// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// XML does not care about attribute order, and mod installers write
// type-before-name. Requiring name-first made those entries invisible: the
// validator skipped them and the Moded page flagged a registered file as
// unregistered.
func TestListEconomyCEIgnoresAttributeOrder(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfgeconomycore.xml")
	os.WriteFile(p, []byte(`<economycore>
  <ce folder="moded_types">
    <file name="a.xml" type="types" />
    <file type="types" name="b.xml" />
    <file type="types" name="c.xml"></file>
  </ce>
</economycore>`), 0o644)

	refs, err := ListEconomyCE(p)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.Name] = r.Type
	}
	for _, n := range []string{"a.xml", "b.xml", "c.xml"} {
		if got[n] != "types" {
			t.Errorf("%s: type = %q, want \"types\" (refs=%+v)", n, got[n], refs)
		}
	}
}

// Against `<file …></file>` the old pattern matched only the opening tag and
// left an orphan `</file>`, making the file malformed.
func TestUnregisterHandlesBothFormsAndOrders(t *testing.T) {
	for _, entry := range []string{
		`<file name="x.xml" type="types" />`,
		`<file type="types" name="x.xml" />`,
		`<file type="types" name="x.xml"></file>`,
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, "cfgeconomycore.xml")
		src := `<economycore>
  <ce folder="moded_types">
    ` + entry + `
    <file name="keep.xml" type="types" />
  </ce>
</economycore>`
		os.WriteFile(p, []byte(src), 0o644)

		ok, err := UnregisterModedFile(p, "x.xml")
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", entry, ok, err)
		}
		out, _ := os.ReadFile(p)
		s := string(out)
		if strings.Contains(s, "x.xml") {
			t.Errorf("%s: entry not removed:\n%s", entry, s)
		}
		if strings.Contains(s, "</file>") {
			t.Errorf("%s: orphan </file> left behind:\n%s", entry, s)
		}
		if !strings.Contains(s, "keep.xml") {
			t.Errorf("%s: removed the wrong entry:\n%s", entry, s)
		}
	}
}
