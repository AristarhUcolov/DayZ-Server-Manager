// Copyright (c) 2026 Aristarh Ucolov.
//
// Surgical writer for types.xml.
//
// TypesDoc.Save re-encoded the whole document with encoding/xml, which cannot
// represent comments — so editing one item's nominal deleted every comment in
// the file, including the `<!-- <type name="OldGun"> … -->` blocks admins use
// to disable an item without losing its numbers. On a modded server this is
// the most heavily commented file there is. It also expanded every
// self-closing <flags .../> into <flags></flags>, growing the shipped
// chernarusplus types.xml from 868 KB to 920 KB on a no-op save.
//
// This writer touches only the <type> blocks that were actually changed. Every
// other byte — comments, indentation, attribute order, elements this struct
// does not model — survives untouched. Same approach as events_write.go and
// spawnable.go.
package types

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// typeBlockNameRe finds one <type name="X"> block. <type> never nests, so a
// non-greedy match is exact.
func typesBlockRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)[ \t]*<type\s+name="` + regexp.QuoteMeta(name) + `"\s*(?:/>|>.*?</type>)[ \t]*\r?\n?`)
}

// RenderType produces one <type> block in the shape DayZ ships: four-space
// indentation, self-closing flags/category/usage/value/tag.
func RenderType(t *Type) string {
	var b strings.Builder
	b.WriteString(`    <type name="` + xmlAttr(t.Name) + `">` + "\n")
	num := func(tag string, v *int) {
		if v != nil {
			fmt.Fprintf(&b, "        <%s>%d</%s>\n", tag, *v, tag)
		}
	}
	// Vanilla order. DayZ's parser does not require it, but a diff against the
	// original file is unreadable if we shuffle it.
	num("nominal", t.Nominal)
	num("lifetime", t.Lifetime)
	num("restock", t.Restock)
	num("min", t.Min)
	num("quantmin", t.QuantMin)
	num("quantmax", t.QuantMax)
	num("cost", t.Cost)
	if t.Flags != nil {
		fmt.Fprintf(&b, "        <flags count_in_cargo=\"%d\" count_in_hoarder=\"%d\" count_in_map=\"%d\" count_in_player=\"%d\" crafted=\"%d\" deloot=\"%d\"/>\n",
			t.Flags.CountInCargo, t.Flags.CountInHoarder, t.Flags.CountInMap,
			t.Flags.CountInPlayer, t.Flags.Crafted, t.Flags.Deloot)
	}
	if t.Category != nil && t.Category.Name != "" {
		b.WriteString(`        <category name="` + xmlAttr(t.Category.Name) + `"/>` + "\n")
	}
	for _, u := range t.Usages {
		if u.Name != "" {
			b.WriteString(`        <usage name="` + xmlAttr(u.Name) + `"/>` + "\n")
		}
	}
	for _, v := range t.Values {
		if v.Name != "" {
			b.WriteString(`        <value name="` + xmlAttr(v.Name) + `"/>` + "\n")
		}
	}
	for _, tg := range t.Tags {
		if tg.Name != "" {
			b.WriteString(`        <tag name="` + xmlAttr(tg.Name) + `"/>` + "\n")
		}
	}
	b.WriteString("    </type>\n")
	return b.String()
}

// SaveTypes writes only the entries the caller marked dirty, leaving the rest
// of the file byte-for-byte identical. A dirty name that is no longer in the
// document is removed; one that is not yet in the file is appended.
func SaveTypes(path string, doc *TypesDoc) error {
	if doc == nil || len(doc.dirty) == 0 {
		return nil // nothing was changed — do not touch the file at all
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(data)

	byName := map[string]*Type{}
	for i := range doc.Types {
		byName[strings.ToLower(doc.Types[i].Name)] = &doc.Types[i]
	}

	for key := range doc.dirty {
		t := byName[key]
		name := key
		if t != nil {
			name = t.Name // original casing, for the regex and the rendered block
		} else {
			// Removed: recover the casing from the file so the match is exact.
			if orig, ok := findTypeName(src, key); ok {
				name = orig
			}
		}
		re := typesBlockRe(name)
		loc := re.FindStringIndex(maskComments(src))
		switch {
		case loc != nil && t != nil: // changed
			src = src[:loc[0]] + RenderType(t) + src[loc[1]:]
		case loc != nil && t == nil: // deleted
			src = src[:loc[0]] + src[loc[1]:]
		case loc == nil && t != nil: // new
			idx := strings.LastIndex(maskComments(src), "</types>")
			if idx < 0 {
				return fmt.Errorf("types.xml: closing </types> not found")
			}
			src = src[:idx] + RenderType(t) + src[idx:]
		}
	}

	if err := writeAtomic(path, []byte(src)); err != nil {
		return err
	}
	doc.dirty = nil
	return nil
}

// findTypeName recovers the file's own casing for a lower-cased name.
func findTypeName(src, lowerName string) (string, bool) {
	re := regexp.MustCompile(`<type\s+name="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(maskComments(src), -1) {
		if strings.EqualFold(m[1], lowerName) {
			return m[1], true
		}
	}
	return "", false
}
