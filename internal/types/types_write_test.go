// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A types.xml with the things admins really put in one: section comments and a
// commented-out entry kept for later.
const typesSample = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<types>
    <!-- ===== my mod weapons ===== -->
    <type name="MyGun">
        <nominal>6</nominal>
        <lifetime>7200</lifetime>
        <restock>0</restock>
        <min>3</min>
        <quantmin>-1</quantmin>
        <quantmax>-1</quantmax>
        <cost>100</cost>
        <flags count_in_cargo="0" count_in_hoarder="0" count_in_map="1" count_in_player="0" crafted="0" deloot="0"/>
        <category name="weapons"/>
        <usage name="Military"/>
    </type>
    <!-- disabled until the next wipe
    <type name="OldGun">
        <nominal>2</nominal>
    </type>
    -->
    <type name="Untouched">
        <nominal>1</nominal>
        <lifetime>3600</lifetime>
        <flags count_in_cargo="1" count_in_hoarder="1" count_in_map="1" count_in_player="1" crafted="1" deloot="1"/>
        <weirdFutureElement value="7"/>
    </type>
</types>
`

func writeTypes(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "types.xml")
	if err := os.WriteFile(p, []byte(typesSample), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The regression this file exists for: editing one entry used to re-encode the
// whole document, deleting every comment and every commented-out <type>.
func TestSaveTypesPreservesCommentsAndUntouchedEntries(t *testing.T) {
	p := writeTypes(t)
	doc, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}

	ty := doc.Find("MyGun")
	if ty == nil {
		t.Fatal("MyGun not parsed")
	}
	twelve := 12
	ty.Nominal = &twelve
	doc.Upsert(*ty)

	if err := SaveTypes(p, doc); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)

	if !strings.Contains(got, "<nominal>12</nominal>") {
		t.Error("the edit was not written")
	}
	for _, want := range []string{
		"<!-- ===== my mod weapons ===== -->",
		"disabled until the next wipe",
		`<type name="OldGun">`, // the commented-out entry, still inside its comment
		// An entry nobody touched must be byte-identical, including an element
		// this struct does not model.
		`<weirdFutureElement value="7"/>`,
		`<flags count_in_cargo="1" count_in_hoarder="1" count_in_map="1" count_in_player="1" crafted="1" deloot="1"/>`,
		`standalone="yes"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q\n---\n%s", want, got)
		}
	}
	// The commented-out OldGun must not have become a live entry.
	doc2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if doc2.Find("OldGun") != nil {
		t.Error("a commented-out entry became live")
	}
	if len(doc2.Types) != 2 {
		t.Errorf("types = %d, want 2", len(doc2.Types))
	}
}

func TestSaveTypesRemovesAndAppends(t *testing.T) {
	p := writeTypes(t)
	doc, _ := Load(p)

	if n := doc.Remove("Untouched"); n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	five := 5
	doc.Upsert(Type{Name: "BrandNew", Nominal: &five, Category: &NamedRef{Name: "tools"}})
	if err := SaveTypes(p, doc); err != nil {
		t.Fatal(err)
	}

	doc2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if doc2.Find("Untouched") != nil {
		t.Error("Untouched was not removed")
	}
	if doc2.Find("BrandNew") == nil {
		t.Error("BrandNew was not appended")
	}
	out, _ := os.ReadFile(p)
	if !strings.Contains(string(out), "<!-- ===== my mod weapons ===== -->") {
		t.Error("comments were lost during add/remove")
	}
}

// A save with nothing marked dirty must not touch the file at all — no rewrite,
// no backup churn.
func TestSaveTypesIsANoOpWhenNothingChanged(t *testing.T) {
	p := writeTypes(t)
	doc, _ := Load(p)
	before, _ := os.ReadFile(p)
	if err := SaveTypes(p, doc); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("an unchanged document rewrote the file")
	}
}

// The whole point of the dirty set: a bulk edit rewrites only its own entries.
func TestBulkPatchOnlyRewritesItsTargets(t *testing.T) {
	p := writeTypes(t)
	doc, _ := Load(p)
	nine := 9
	if n := doc.BulkPatch([]string{"MyGun"}, BulkFieldPatch{Nominal: &nine}); n != 1 {
		t.Fatalf("touched %d, want 1", n)
	}
	if doc.Dirty() != 1 {
		t.Errorf("dirty = %d, want 1", doc.Dirty())
	}
	if err := SaveTypes(p, doc); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	if !strings.Contains(got, "<nominal>9</nominal>") {
		t.Error("bulk patch not written")
	}
	if !strings.Contains(got, `<weirdFutureElement value="7"/>`) {
		t.Error("bulk patch rewrote an entry it was not given")
	}
}

// A caller that mutates through the Find pointer must flag the entry, or
// SaveTypes writes nothing and the feature silently does nothing. This is
// exactly how the "apply spawn preset" action would have broken.
func TestMutationThroughFindNeedsMarkDirty(t *testing.T) {
	p := writeTypes(t)

	doc, _ := Load(p)
	ty := doc.Find("MyGun")
	twenty := 20
	ty.Nominal = &twenty
	if err := SaveTypes(p, doc); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "<nominal>20</nominal>") {
		t.Fatal("unflagged change was written — the dirty set is not being honoured")
	}

	doc2, _ := Load(p)
	ty2 := doc2.Find("MyGun")
	ty2.Nominal = &twenty
	doc2.MarkDirty(ty2.Name)
	if err := SaveTypes(p, doc2); err != nil {
		t.Fatal(err)
	}
	out2, _ := os.ReadFile(p)
	if !strings.Contains(string(out2), "<nominal>20</nominal>") {
		t.Error("a flagged change was not written")
	}
}
