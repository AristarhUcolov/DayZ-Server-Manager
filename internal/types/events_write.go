// Copyright (c) 2026 Aristarh Ucolov.
//
// Surgical writer for events.xml.
//
// The Event struct models the seven numbers the editor exposes. It does NOT
// model <saferadius>, <distanceradius>, <cleanupradius>, <flags>, <position>,
// <limit> or <secondary> — and a full xml.Marshal round-trip deleted every one
// of them from EVERY event in the file, not just the edited one. Measured on
// vanilla chernarusplus: 52167 bytes in, 38565 out; all 59 <flags>, 59
// <position>, 59 <limit>, 7 <secondary> and 118 of each radius gone. Nudging
// one helicrash's nominal cost the map its helicrash positioning, its infected
// escorts and its cleanup radii, silently.
//
// So we never re-encode. We rewrite the individual child elements inside the
// one <event> block being edited, and every other byte of the file — including
// elements DayZ or a mod may add in the future — survives untouched.
package types

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// eventFieldOrder is the order DayZ's own files use. It matters: the parser is
// order-sensitive, so an element we have to insert must land in the right slot.
var eventFieldOrder = []string{
	"nominal", "min", "max", "lifetime", "restock",
	"saferadius", "distanceradius", "cleanupradius",
	"secondary", "flags", "position", "limit",
	"saveable", "active",
}

// EditableEventFields are the elements the editor may change. Anything outside
// this list is off limits to the writer by construction.
var EditableEventFields = []string{
	"nominal", "min", "max", "lifetime", "restock", "saveable", "active",
}

// eventBlockRe matches one <event name="X">…</event> block. <event> never
// nests, so a non-greedy match is exact.
func eventBlockRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)[ \t]*<event\s+name="` + regexp.QuoteMeta(name) + `"\s*(?:/>|>.*?</event>)[ \t]*\r?\n?`)
}

func childElemRe(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<` + tag + `>\s*[^<]*</` + tag + `>`)
}

// PatchEvent sets the given numeric fields on one event, leaving every other
// byte of events.xml alone. Reports false when the event is not in the file.
func PatchEvent(path, name string, fields map[string]int) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	src := string(data)

	loc := eventBlockRe(name).FindStringIndex(maskComments(src))
	if loc == nil {
		return false, nil
	}
	block := src[loc[0]:loc[1]]
	patched, err := patchEventBlock(block, fields)
	if err != nil {
		return false, err
	}
	return true, writeAtomic(path, []byte(src[:loc[0]]+patched+src[loc[1]:]))
}

// patchEventBlock rewrites the requested child elements of a single block.
func patchEventBlock(block string, fields map[string]int) (string, error) {
	// Indentation of the existing children, so an inserted element lines up.
	indent := "        "
	if m := regexp.MustCompile(`\n([ \t]+)<`).FindStringSubmatch(block); m != nil {
		indent = m[1]
	}

	for _, tag := range EditableEventFields {
		v, ok := fields[tag]
		if !ok {
			continue
		}
		want := fmt.Sprintf("<%s>%d</%s>", tag, v, tag)
		if re := childElemRe(tag); re.MatchString(block) {
			block = re.ReplaceAllLiteralString(block, want)
			continue
		}
		// Absent: insert it in vanilla order, before the first element that
		// comes after it — or right before </event> if there is none.
		inserted := false
		for _, later := range fieldsAfter(tag) {
			if idx := strings.Index(block, "<"+later); idx >= 0 {
				// Rewind to the start of that element's own line.
				lineStart := strings.LastIndex(block[:idx], "\n")
				if lineStart < 0 {
					continue
				}
				block = block[:lineStart+1] + indent + want + "\n" + block[lineStart+1:]
				inserted = true
				break
			}
		}
		if !inserted {
			idx := strings.LastIndex(block, "</event>")
			if idx < 0 {
				return "", fmt.Errorf("event block has no closing tag")
			}
			lineStart := strings.LastIndex(block[:idx], "\n")
			if lineStart < 0 {
				lineStart = idx - 1
			}
			block = block[:lineStart+1] + indent + want + "\n" + block[lineStart+1:]
		}
	}
	return block, nil
}

func fieldsAfter(tag string) []string {
	for i, f := range eventFieldOrder {
		if f == tag {
			return eventFieldOrder[i+1:]
		}
	}
	return nil
}

// RenderEvent produces a minimal <event> block for a brand-new entry.
func RenderEvent(e *Event) string {
	var b strings.Builder
	b.WriteString(`    <event name="` + xmlAttr(e.Name) + `">` + "\n")
	num := func(tag string, v *int) {
		if v != nil {
			fmt.Fprintf(&b, "        <%s>%d</%s>\n", tag, *v, tag)
		}
	}
	num("nominal", e.Nominal)
	num("min", e.Min)
	num("max", e.Max)
	num("lifetime", e.Lifetime)
	num("restock", e.Restock)
	num("saveable", e.Saveable)
	num("active", e.Active)
	if e.Children != nil {
		b.WriteString("        <children>\n")
		for _, c := range e.Children.Child {
			fmt.Fprintf(&b, "            <child lootmax=\"%d\" lootmin=\"%d\" max=\"%d\" min=\"%d\" type=\"%s\"/>\n",
				c.LootMax, c.LootMin, c.Max, c.Min, xmlAttr(c.Type))
		}
		b.WriteString("        </children>\n")
	}
	b.WriteString("    </event>\n")
	return b.String()
}

// AppendEvent adds a new <event> block just before </events>.
func AppendEvent(path string, e *Event) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(data)
	idx := strings.LastIndex(maskComments(src), "</events>")
	if idx < 0 {
		return fmt.Errorf("events.xml: closing </events> not found")
	}
	return writeAtomic(path, []byte(src[:idx]+RenderEvent(e)+src[idx:]))
}

// DeleteEventBlock removes every live <event> with this name and returns how
// many were removed. Commented-out blocks are left alone.
func DeleteEventBlock(path, name string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	src := string(data)
	locs := eventBlockRe(name).FindAllStringIndex(maskComments(src), -1)
	if len(locs) == 0 {
		return 0, nil
	}
	for i := len(locs) - 1; i >= 0; i-- {
		src = src[:locs[i][0]] + src[locs[i][1]:]
	}
	return len(locs), writeAtomic(path, []byte(src))
}

// reChildrenBlock matches a whole <children> block inside an event.
var reChildrenBlock = regexp.MustCompile(`(?s)[ \t]*<children>.*?</children>[ \t]*\r?\n?`)

// PatchEventChildren replaces the <children> block of one event, inserting it
// before </event> when absent and removing it when kids is empty.
//
// The events editor has always drawn a full children table with add and delete
// buttons, but the handler only ever wrote the seven scalar fields — so editing
// a helicrash's loot table reported "Saved" and changed nothing.
func PatchEventChildren(path, name string, kids []EventChild) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	src := string(data)
	loc := eventBlockRe(name).FindStringIndex(maskComments(src))
	if loc == nil {
		return false, nil
	}
	block := src[loc[0]:loc[1]]

	var b strings.Builder
	if len(kids) > 0 {
		b.WriteString("        <children>\n")
		for _, c := range kids {
			if strings.TrimSpace(c.Type) == "" {
				continue // a blank row in the UI is not an entry
			}
			fmt.Fprintf(&b, "            <child lootmax=\"%d\" lootmin=\"%d\" max=\"%d\" min=\"%d\" type=\"%s\"/>\n",
				c.LootMax, c.LootMin, c.Max, c.Min, xmlAttr(c.Type))
		}
		b.WriteString("        </children>\n")
	}

	switch {
	case reChildrenBlock.MatchString(block):
		block = reChildrenBlock.ReplaceAllLiteralString(block, b.String())
	case b.Len() > 0:
		idx := strings.LastIndex(block, "</event>")
		if idx < 0 {
			return false, fmt.Errorf("event %q has no closing tag", name)
		}
		lineStart := strings.LastIndex(block[:idx], "\n")
		if lineStart < 0 {
			lineStart = idx - 1
		}
		block = block[:lineStart+1] + b.String() + block[lineStart+1:]
	}
	return true, writeAtomic(path, []byte(src[:loc[0]]+block+src[loc[1]:]))
}
