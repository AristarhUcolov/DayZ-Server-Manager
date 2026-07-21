// Copyright (c) 2026 Aristarh Ucolov.
//
// Content checks: value combinations that are syntactically fine but that DayZ
// either clamps, warns about in the CE log, or silently ignores.
//
// Every check here was measured against the three shipped vanilla missions
// before being added. A check that fires on a stock server teaches admins to
// ignore the page, so anything with false positives on vanilla was left out —
// see the guards on `nominal > 0` and `max > 0`, which are the difference
// between 0 hits and hundreds.
package validator

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dztypes "dayzmanager/internal/types"
)

// ceFileTypes are the values DayZ accepts for <file type="…"> in
// cfgeconomycore.xml. Anything else means the file is loaded as the wrong kind
// or not at all.
var ceFileTypes = map[string]bool{
	"types": true, "spawnabletypes": true, "events": true, "economy": true,
	"globals": true, "messages": true, "randompresets": true,
}

// checkTypeSanity flags numeric combinations that make an entry behave in a way
// the admin did not intend.
func checkTypeSanity(path string, t *dztypes.Type) []Issue {
	var out []Issue
	add := func(sev Severity, format string, a ...interface{}) {
		out = append(out, Issue{File: path, Severity: sev,
			Message: fmt.Sprintf("type %q: "+format, append([]interface{}{t.Name}, a...)...)})
	}

	if strings.TrimSpace(t.Name) == "" {
		return []Issue{{File: path, Severity: SevError,
			Message: "a <type> has an empty name attribute — DayZ ignores the whole entry"}}
	}

	// min > nominal. The `nominal > 0` guard is essential: hundreds of vanilla
	// non-CE entries legitimately carry nominal=0 with min=1.
	if t.Nominal != nil && *t.Nominal > 0 && t.Min != nil && *t.Min > *t.Nominal {
		add(SevWarning, "min (%d) is greater than nominal (%d) — DayZ clamps to nominal and logs a CE warning", *t.Min, *t.Nominal)
	}

	// lifetime 0 with a real nominal: the item despawns as it spawns. Without
	// the nominal guard this fires on every static wreck in vanilla.
	if t.Nominal != nil && *t.Nominal > 0 && t.Lifetime != nil && *t.Lifetime <= 0 {
		add(SevError, "lifetime = %d with nominal = %d — the item despawns the instant it spawns", *t.Lifetime, *t.Nominal)
	}

	if t.Restock != nil && *t.Restock < 0 {
		add(SevWarning, "restock = %d is negative", *t.Restock)
	}

	// quantmin/quantmax are percentages, or -1 to disable. Writing absolute
	// round counts here is a very common mistake.
	if t.QuantMin != nil && t.QuantMax != nil &&
		*t.QuantMin != -1 && *t.QuantMax != -1 && *t.QuantMin > *t.QuantMax {
		add(SevError, "quantmin (%d) is greater than quantmax (%d) — the item spawns with an undefined fill level", *t.QuantMin, *t.QuantMax)
	}
	for _, q := range []struct {
		name string
		v    *int
	}{{"quantmin", t.QuantMin}, {"quantmax", t.QuantMax}} {
		if q.v != nil && *q.v != -1 && (*q.v < 0 || *q.v > 100) {
			add(SevError, "%s = %d is out of range — it is a percentage: use -1 (disabled) or 0..100", q.name, *q.v)
		}
	}
	return out
}

// unknownAgg collects unknown category/usage/value/tag references and reports
// each distinct name once. A mod that misspells one usage across 400 types used
// to produce 400 identical warnings, which buries everything else.
type unknownAgg struct {
	// kind -> file -> lowercase name -> {original, count, firstType}
	seen map[string]map[string]map[string]*unknownRef
}

type unknownRef struct {
	orig      string
	count     int
	firstType string
}

func newUnknownAgg() *unknownAgg {
	return &unknownAgg{seen: map[string]map[string]map[string]*unknownRef{}}
}

func (u *unknownAgg) note(kind, file, name, typeName string) {
	byFile := u.seen[kind]
	if byFile == nil {
		byFile = map[string]map[string]*unknownRef{}
		u.seen[kind] = byFile
	}
	byName := byFile[file]
	if byName == nil {
		byName = map[string]*unknownRef{}
		byFile[file] = byName
	}
	key := strings.ToLower(name)
	if r := byName[key]; r != nil {
		r.count++
		return
	}
	byName[key] = &unknownRef{orig: name, count: 1, firstType: typeName}
}

// collect records every reference in t that is not in the whitelist.
func (u *unknownAgg) collect(file string, t *dztypes.Type, l *limits) {
	if l == nil || !l.loaded {
		return
	}
	if t.Category != nil && t.Category.Name != "" && !l.categories[strings.ToLower(t.Category.Name)] {
		u.note("category", file, t.Category.Name, t.Name)
	}
	scan := func(set map[string]bool, kind string, refs []dztypes.NamedRef) {
		for _, r := range refs {
			if r.Name != "" && !set[strings.ToLower(r.Name)] {
				u.note(kind, file, r.Name, t.Name)
			}
		}
	}
	scan(l.usages, "usage", t.Usages)
	scan(l.values, "value", t.Values)
	scan(l.tags, "tag", t.Tags)
}

func (u *unknownAgg) issues() []Issue {
	var out []Issue
	for _, kind := range []string{"category", "usage", "value", "tag"} {
		byFile := u.seen[kind]
		files := make([]string, 0, len(byFile))
		for f := range byFile {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			byName := byFile[f]
			names := make([]string, 0, len(byName))
			for k := range byName {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				r := byName[k]
				msg := fmt.Sprintf("unknown %s %q — not in cfglimitsdefinition.xml, so DayZ drops it (used by type %q)",
					kind, r.orig, r.firstType)
				if r.count > 1 {
					msg = fmt.Sprintf("unknown %s %q — not in cfglimitsdefinition.xml, so DayZ drops it (used by %d types, first: %q)",
						kind, r.orig, r.count, r.firstType)
				}
				out = append(out, Issue{File: f, Severity: SevWarning, Message: msg})
			}
		}
	}
	return out
}

// checkEvents validates events.xml and the spawn positions that reference it.
func checkEvents(eventsPath, spawnsPath string) []Issue {
	doc, err := dztypes.LoadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Issue{{File: eventsPath, Severity: SevError,
			Message: fmt.Sprintf("cannot parse events.xml: %v", err)}}
	}

	var out []Issue
	seen := map[string]bool{}
	for i := range doc.Events {
		e := &doc.Events[i]
		key := strings.ToLower(e.Name)
		if seen[key] {
			out = append(out, Issue{File: eventsPath, Severity: SevError,
				Message: fmt.Sprintf("duplicate event %q — DayZ keeps only one of them", e.Name)})
		}
		seen[key] = true

		// max="0" means unlimited, which is why vanilla ambient events read
		// min="100" max="0" — comparing without this guard is all noise.
		if e.Min != nil && e.Max != nil && *e.Max > 0 && *e.Min > *e.Max {
			out = append(out, Issue{File: eventsPath, Severity: SevWarning,
				Message: fmt.Sprintf("event %q: min (%d) is greater than max (%d)", e.Name, *e.Min, *e.Max)})
		}
		// Deliberately NOT checked: active=0 with a nominal set. That fires 8
		// to 16 times on every stock mission — BI keeps seasonal events
		// (Christmas tree, Santa crash, spooky infected) fully configured but
		// switched off, which is exactly how admins use it too.
		if e.Children != nil {
			for _, c := range e.Children.Child {
				if c.Max > 0 && c.Min > c.Max {
					out = append(out, Issue{File: eventsPath, Severity: SevWarning,
						Message: fmt.Sprintf("event %q child %q: min (%d) is greater than max (%d)", e.Name, c.Type, c.Min, c.Max)})
				}
			}
		}
	}

	// Spawn positions for an event that no longer exists are dead config — the
	// classic result of renaming an event and forgetting this file. Only this
	// direction is checked: plenty of events legitimately have no positions.
	for _, name := range eventSpawnNames(spawnsPath) {
		if !seen[strings.ToLower(name)] {
			out = append(out, Issue{File: spawnsPath, Severity: SevWarning,
				Message: fmt.Sprintf("spawn positions are defined for event %q, which is not in events.xml — they are ignored", name)})
		}
	}
	return out
}

// eventSpawnNames lists the event names cfgeventspawns.xml defines positions
// for. Returns nothing if the file is absent or unreadable — a missing file is
// not an error worth two reports.
func eventSpawnNames(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Events []struct {
			Name string `xml:"name,attr"`
		} `xml:"event"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Events))
	for _, e := range doc.Events {
		if e.Name != "" {
			out = append(out, e.Name)
		}
	}
	return out
}

// checkSpawnable validates cfgspawnabletypes.xml against cfgrandompresets.xml.
//
// Deliberately NOT checked here: whether the class names exist in types.xml.
// Measured on vanilla, that produces ~17 hits per mission for items that only
// spawn from events, plus every attachment (attachments correctly have no
// types.xml entry). The attachments editor already flags unknown classes
// inline, which is the right place for a hint that needs context.
func checkSpawnable(missionDir string) []Issue {
	path := filepath.Join(missionDir, "cfgspawnabletypes.xml")
	sts, err := dztypes.LoadSpawnable(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Issue{{File: path, Severity: SevError,
			Message: fmt.Sprintf("cannot parse cfgspawnabletypes.xml: %v", err)}}
	}

	presets := presetNames(filepath.Join(missionDir, "cfgrandompresets.xml"))
	var out []Issue
	seen := map[string]bool{}
	for i := range sts {
		st := &sts[i]
		key := strings.ToLower(st.Name)
		if seen[key] {
			out = append(out, Issue{File: path, Severity: SevWarning,
				Message: fmt.Sprintf("duplicate entry for %q — DayZ keeps only one of them", st.Name)})
		}
		seen[key] = true

		if len(presets) == 0 {
			continue // no presets file: nothing to check against
		}
		for _, g := range append(append([]dztypes.SpawnGroup{}, st.Attachments...), st.Cargo...) {
			if g.Preset != "" && !presets[strings.ToLower(g.Preset)] {
				out = append(out, Issue{File: path, Severity: SevError,
					Message: fmt.Sprintf("%q references preset %q, which is not defined in cfgrandompresets.xml — the slot spawns nothing", st.Name, g.Preset)})
			}
		}
	}
	return out
}

func presetNames(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Cargo []struct {
			Name string `xml:"name,attr"`
		} `xml:"cargo"`
		Attachments []struct {
			Name string `xml:"name,attr"`
		} `xml:"attachments"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, c := range doc.Cargo {
		out[strings.ToLower(c.Name)] = true
	}
	for _, a := range doc.Attachments {
		out[strings.ToLower(a.Name)] = true
	}
	return out
}
