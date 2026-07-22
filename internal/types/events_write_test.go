// Copyright (c) 2026 Aristarh Ucolov.
package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real vanilla event: everything below nominal/min/max is unmodelled by the
// Event struct, and the old xml.Marshal round-trip deleted all of it.
const vanillaEvents = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<events>
    <!-- helicopter crash sites -->
    <event name="StaticHeliCrash">
        <nominal>3</nominal>
        <min>0</min>
        <max>0</max>
        <lifetime>2100</lifetime>
        <restock>0</restock>
        <saferadius>1000</saferadius>
        <distanceradius>1000</distanceradius>
        <cleanupradius>1000</cleanupradius>
        <secondary>InfectedArmy</secondary>
        <flags deletable="1" init_random="0" remove_damaged="0"/>
        <position>fixed</position>
        <limit>child</limit>
        <active>1</active>
        <children>
            <child lootmax="15" lootmin="10" max="3" min="1" type="Wreck_UH1Y"/>
        </children>
    </event>
    <event name="AnimalCow">
        <nominal>12</nominal>
        <min>5</min>
        <max>0</max>
        <lifetime>300</lifetime>
        <restock>0</restock>
        <saferadius>200</saferadius>
        <flags deletable="0" init_random="0" remove_damaged="1"/>
        <position>fixed</position>
        <limit>custom</limit>
        <active>1</active>
    </event>
</events>
`

func writeEvents(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "events.xml")
	if err := os.WriteFile(p, []byte(vanillaEvents), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The regression this file exists for: editing one event must not strip
// unmodelled elements from that event OR from any other.
func TestPatchEventPreservesUnmodelledElements(t *testing.T) {
	p := writeEvents(t)

	ok, err := PatchEvent(p, "StaticHeliCrash", map[string]int{"nominal": 7, "lifetime": 3600})
	if err != nil || !ok {
		t.Fatalf("patch: ok=%v err=%v", ok, err)
	}

	out, _ := os.ReadFile(p)
	got := string(out)

	if !strings.Contains(got, "<nominal>7</nominal>") {
		t.Error("nominal was not updated")
	}
	if !strings.Contains(got, "<lifetime>3600</lifetime>") {
		t.Error("lifetime was not updated")
	}
	// Everything the struct does not model must still be there, for BOTH events.
	for _, want := range []string{
		"<saferadius>1000</saferadius>",
		"<distanceradius>1000</distanceradius>",
		"<cleanupradius>1000</cleanupradius>",
		"<secondary>InfectedArmy</secondary>",
		`<flags deletable="1" init_random="0" remove_damaged="0"/>`,
		"<position>fixed</position>",
		"<limit>child</limit>",
		`<child lootmax="15" lootmin="10" max="3" min="1" type="Wreck_UH1Y"/>`,
		// The untouched event, byte for byte.
		"<saferadius>200</saferadius>",
		`<flags deletable="0" init_random="0" remove_damaged="1"/>`,
		"<limit>custom</limit>",
		// And the comment.
		"<!-- helicopter crash sites -->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q\n---\n%s", want, got)
		}
	}
}

// An element the file does not have yet must land in DayZ's expected order,
// not simply be appended at the end.
func TestPatchEventInsertsMissingFieldInOrder(t *testing.T) {
	p := writeEvents(t)
	if _, err := PatchEvent(p, "AnimalCow", map[string]int{"saveable": 1}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	// Scope to the edited event — the other one has its own <active>.
	start := strings.Index(got, `<event name="AnimalCow">`)
	if start < 0 {
		t.Fatalf("AnimalCow disappeared:\n%s", got)
	}
	block := got[start : start+strings.Index(got[start:], "</event>")]
	if !strings.Contains(block, "<saveable>1</saveable>") {
		t.Fatalf("saveable was not inserted:\n%s", block)
	}
	// saveable comes after <limit> and before <active>.
	iLimit := strings.Index(block, "<limit>custom</limit>")
	iSave := strings.Index(block, "<saveable>1</saveable>")
	iActive := strings.Index(block, "<active>1</active>")
	if !(iLimit < iSave && iSave < iActive) {
		t.Errorf("saveable landed out of order (limit=%d saveable=%d active=%d)\n%s", iLimit, iSave, iActive, block)
	}
}

func TestPatchEventUnknownName(t *testing.T) {
	p := writeEvents(t)
	before, _ := os.ReadFile(p)
	ok, err := PatchEvent(p, "NoSuchEvent", map[string]int{"nominal": 1})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("reported success for an event that is not in the file")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("the file was modified for a no-op patch")
	}
}

func TestDeleteEventBlock(t *testing.T) {
	p := writeEvents(t)
	n, err := DeleteEventBlock(p, "AnimalCow")
	if err != nil || n != 1 {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	if strings.Contains(got, `name="AnimalCow"`) {
		t.Error("event was not removed")
	}
	// The other event, and the comment, survive.
	if !strings.Contains(got, "<secondary>InfectedArmy</secondary>") ||
		!strings.Contains(got, "<!-- helicopter crash sites -->") {
		t.Errorf("delete damaged the rest of the file:\n%s", got)
	}
	if n, _ := DeleteEventBlock(p, "AnimalCow"); n != 0 {
		t.Error("deleting a missing event reported success")
	}
}

func TestAppendEvent(t *testing.T) {
	p := writeEvents(t)
	five, one := 5, 1
	if err := AppendEvent(p, &Event{Name: "MyEvent", Nominal: &five, Active: &one}); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find("MyEvent") == nil {
		t.Fatal("appended event does not parse back")
	}
	if len(doc.Events) != 3 {
		t.Errorf("events = %d, want 3", len(doc.Events))
	}
	out, _ := os.ReadFile(p)
	if !strings.Contains(string(out), "<secondary>InfectedArmy</secondary>") {
		t.Error("appending damaged an existing event")
	}
}

// The events editor draws a children table with add/delete buttons and posts
// it, but the handler used to write only the scalar fields — so editing a
// helicrash's loot table said "Saved" and changed nothing.
func TestPatchEventChildren(t *testing.T) {
	p := writeEvents(t)

	ok, err := PatchEventChildren(p, "StaticHeliCrash", []EventChild{
		{Type: "Wreck_Mi8", LootMin: 5, LootMax: 9, Min: 1, Max: 2},
		{Type: "Wreck_UH1Y", LootMin: 3, LootMax: 6, Min: 1, Max: 1},
		{Type: "   "}, // a blank row in the UI must not become an entry
	})
	if err != nil || !ok {
		t.Fatalf("patch: ok=%v err=%v", ok, err)
	}

	doc, err := LoadEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	e := doc.Find("StaticHeliCrash")
	if e == nil || e.Children == nil {
		t.Fatal("children missing after patch")
	}
	if len(e.Children.Child) != 2 {
		t.Fatalf("children = %d, want 2 (blank row must be dropped)", len(e.Children.Child))
	}
	if e.Children.Child[0].Type != "Wreck_Mi8" || e.Children.Child[0].LootMax != 9 {
		t.Errorf("first child wrong: %+v", e.Children.Child[0])
	}

	// Everything the Event struct does not model must still be there.
	out, _ := os.ReadFile(p)
	for _, want := range []string{
		"<secondary>InfectedArmy</secondary>",
		`<flags deletable="1" init_random="0" remove_damaged="0"/>`,
		"<!-- helicopter crash sites -->",
		"<saferadius>200</saferadius>", // the other event, untouched
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("lost %q", want)
		}
	}

	// An empty list removes the block entirely.
	if _, err := PatchEventChildren(p, "StaticHeliCrash", nil); err != nil {
		t.Fatal(err)
	}
	out2, _ := os.ReadFile(p)
	if strings.Contains(string(out2), "<children>") {
		t.Error("children block was not removed")
	}
}
