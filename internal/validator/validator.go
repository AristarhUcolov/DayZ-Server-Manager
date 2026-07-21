// Copyright (c) 2026 Aristarh Ucolov.
//
// Validator for the DayZ server file set.
//
// * XML: .xml files are parsed with encoding/xml. Errors carry line numbers.
// * CFG: server.cfg — we do a best-effort brace/semicolon check.
// * Cross-file: cfgeconomycore.xml referenced files must exist on disk; types
//   inside moded_types/*.xml must not duplicate a name already in types.xml
//   (duplicates cause "type already defined" issues in-game).
package validator

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

type Issue struct {
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// ValidateAll runs every check against the server directory and returns a flat
// issue list. The caller is responsible for deciding what to surface in the UI.
func ValidateAll(serverDir, missionTemplate string) ([]Issue, error) {
	var issues []Issue

	// XML files under mpmissions.
	missionDir := filepath.Join(serverDir, "mpmissions")
	err := filepath.Walk(missionDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == ".xml" {
			if is := validateXML(path); is != nil {
				issues = append(issues, *is)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// server.cfg files at server root.
	entries, _ := os.ReadDir(serverDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".cfg" {
			if is := validateCFG(filepath.Join(serverDir, e.Name())); is != nil {
				issues = append(issues, *is)
			}
		}
	}

	// Cross-file: cfgeconomycore.xml referenced files.
	if missionTemplate != "" {
		missionDir := dztypes.MissionDir(serverDir, missionTemplate)
		eco := filepath.Join(missionDir, "cfgeconomycore.xml")
		if refs, err := dztypes.ListEconomyCE(eco); err == nil {
			for _, r := range refs {
				p := filepath.Join(missionDir, r.Folder, r.Name)
				if _, err := os.Stat(p); err != nil {
					issues = append(issues, Issue{
						File:     eco,
						Severity: SevError,
						Message:  fmt.Sprintf("referenced file missing: %s/%s", r.Folder, r.Name),
					})
				}
				// A typo in type= makes DayZ load the file as the wrong kind
				// or ignore it, with no message anywhere.
				if r.Type != "" && !ceFileTypes[strings.ToLower(r.Type)] {
					issues = append(issues, Issue{
						File:     eco,
						Severity: SevError,
						Message:  fmt.Sprintf("%s/%s declares type=%q, which DayZ does not recognise", r.Folder, r.Name, r.Type),
					})
				}
			}
		}

		// cfglimitsdefinition.xml — the whitelist of category/usage/value/tag
		// names. Types referencing anything outside this list are silently
		// dropped by DayZ at runtime, which makes for confusing loot bugs.
		//
		// These two files sit at the MISSION ROOT. They were read from db/ up
		// to v0.15.0, so loadLimits always failed, every flag check returned
		// nothing, and the page reported "no issues" on a broken server.
		limitsPath := filepath.Join(missionDir, "cfglimitsdefinition.xml")
		limits, limitsErr := loadLimits(limitsPath)
		mergeUserLimits(limits, filepath.Join(missionDir, "cfglimitsdefinitionuser.xml"))
		if !limits.loaded {
			// A check that silently does nothing is worse than no check: say so.
			issues = append(issues, Issue{
				File:     limitsPath,
				Severity: SevWarning,
				Message:  fmt.Sprintf("cannot read cfglimitsdefinition.xml (%v) — category/usage/value/tag checks were skipped", limitsErr),
			})
		}

		// unknownRefs aggregates unknown names so one misspelling shared by 400
		// modded types produces one line instead of 400 identical ones.
		unknown := newUnknownAgg()

		// Duplicate type names across types.xml + moded_types/*.xml.
		seen := map[string]string{}
		typesPath := filepath.Join(missionDir, "db", "types.xml")
		if doc, err := dztypes.Load(typesPath); err != nil {
			if !os.IsNotExist(err) {
				issues = append(issues, Issue{File: typesPath, Severity: SevError,
					Message: fmt.Sprintf("cannot parse as a types file: %v", err)})
			}
		} else {
			for i := range doc.Types {
				t := &doc.Types[i]
				key := strings.ToLower(t.Name)
				// Duplicates inside one file were never checked: DayZ keeps
				// only one of them, and which one is not defined.
				if _, dup := seen[key]; dup {
					issues = append(issues, Issue{File: typesPath, Severity: SevError,
						Message: fmt.Sprintf("duplicate type %q in the same file — DayZ keeps only one of them", t.Name)})
				}
				seen[key] = typesPath
				unknown.collect(typesPath, t, limits)
				issues = append(issues, checkTypeSanity(typesPath, t)...)
			}
		}
		moded := dztypes.ModedDir(serverDir, missionTemplate)
		modedEntries, _ := os.ReadDir(moded)
		registered, _ := dztypes.RegisteredInModed(eco)
		for _, e := range modedEntries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			p := filepath.Join(moded, e.Name())
			// A moded types file DayZ never loads is the single most common
			// cause of "my custom loot does not spawn".
			if len(registered) > 0 && !registered[strings.ToLower(e.Name())] {
				issues = append(issues, Issue{File: p, Severity: SevError,
					Message: fmt.Sprintf("%s is not registered in cfgeconomycore.xml — DayZ will not load it", e.Name())})
			}
			doc, err := dztypes.Load(p)
			if err != nil {
				// Swallowing this meant a moded file with the wrong root
				// element produced no output at all.
				issues = append(issues, Issue{File: p, Severity: SevError,
					Message: fmt.Sprintf("cannot parse as a types file: %v", err)})
				continue
			}
			for i := range doc.Types {
				t := &doc.Types[i]
				key := strings.ToLower(t.Name)
				if prev, dup := seen[key]; dup {
					issues = append(issues, Issue{
						File:     p,
						Severity: SevWarning,
						Message:  fmt.Sprintf("duplicate type %q (also in %s)", t.Name, filepath.Base(prev)),
					})
				} else {
					seen[key] = p
				}
				unknown.collect(p, t, limits)
				issues = append(issues, checkTypeSanity(p, t)...)
			}
		}
		issues = append(issues, unknown.issues()...)

		// events.xml + the spawn positions that reference it.
		issues = append(issues, checkEvents(filepath.Join(missionDir, "db", "events.xml"),
			filepath.Join(missionDir, "cfgeventspawns.xml"))...)
		// cfgspawnabletypes.xml preset references.
		issues = append(issues, checkSpawnable(missionDir)...)
	}

	return issues, nil
}

// reXMLComment matches an entire <!-- ... --> block, including newlines.
// BI's vanilla XML uses `-------` decorations inside comments which is illegal
// per XML spec. DayZ's own loader ignores those, so we strip comments out
// before handing bytes to Go's strict parser — otherwise every default mission
// reports false errors on perfectly shipping files.
var reXMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

func validateXML(path string) *Issue {
	raw, err := os.ReadFile(path)
	if err != nil {
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
	// Replace each comment with the same number of newlines so line numbers
	// from the decoder still point at meaningful positions in the file.
	cleaned := reXMLComment.ReplaceAllFunc(raw, func(m []byte) []byte {
		return bytes.Repeat([]byte{'\n'}, bytes.Count(m, []byte{'\n'}))
	})
	dec := xml.NewDecoder(bytes.NewReader(cleaned))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == nil {
			continue
		}
		if err.Error() == "EOF" {
			return nil
		}
		if se, ok := err.(*xml.SyntaxError); ok {
			return &Issue{File: path, Line: se.Line, Severity: SevError, Message: se.Msg}
		}
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
}

// limits mirrors the small subset of cfglimitsdefinition.xml we care about:
// the set of category/usage/value/tag names that DayZ will accept. Anything
// referenced in types.xml that is not in these sets will be dropped silently
// at runtime.
type limits struct {
	categories map[string]bool
	usages     map[string]bool
	values     map[string]bool
	tags       map[string]bool
	loaded     bool
}

// newLimits returns an empty, unloaded limits set with every map ready to use.
func newLimits() *limits {
	return &limits{
		categories: map[string]bool{},
		usages:     map[string]bool{},
		values:     map[string]bool{},
		tags:       map[string]bool{},
	}
}

func loadLimits(path string) (*limits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return newLimits(), err
	}
	type named struct {
		Name string `xml:"name,attr"`
	}
	type doc struct {
		XMLName    xml.Name `xml:"lists"`
		Categories struct {
			Category []named `xml:"category"`
		} `xml:"categories"`
		Tags struct {
			Tag []named `xml:"tag"`
		} `xml:"tags"`
		Usage struct {
			Usage []named `xml:"usage"`
		} `xml:"usageflags"`
		Value struct {
			Value []named `xml:"value"`
		} `xml:"valueflags"`
	}
	var d doc
	if err := xml.Unmarshal(data, &d); err != nil {
		return newLimits(), err
	}
	l := newLimits()
	l.loaded = true
	for _, n := range d.Categories.Category {
		l.categories[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Usage.Usage {
		l.usages[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Value.Value {
		l.values[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Tags.Tag {
		l.tags[strings.ToLower(n.Name)] = true
	}
	return l, nil
}


// mergeUserLimits folds cfglimitsdefinitionuser.xml `<user name="X">` groups
// into the limits sets. Those user names are legal usage/value references in
// types.xml, so counting them avoids false "unknown usage/value" warnings.
func mergeUserLimits(l *limits, userPath string) {
	if l == nil || l.usages == nil {
		return
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		return
	}
	type named struct {
		Name string `xml:"name,attr"`
	}
	// The real file nests <user> inside <usageflags>/<valueflags>:
	//
	//   <user_lists><usageflags><user name="TownVillage">…</user></usageflags>
	//
	// The old `xml:"user"` at root level matched nothing, so every types entry
	// using a group name like Tier1234 or TownVillage was about to be reported
	// as unknown — and Auto-fix would have written a colliding duplicate name
	// into cfglimitsdefinition.xml.
	type group struct {
		User []named `xml:"user"`
	}
	type doc struct {
		Usage group  `xml:"usageflags"`
		Value group  `xml:"valueflags"`
		Root  []named `xml:"user"` // hand-written flat variant, still accepted
	}
	var d doc
	if xml.Unmarshal(data, &d) != nil {
		return
	}
	add := func(set map[string]bool, us []named) {
		for _, u := range us {
			if u.Name != "" {
				set[strings.ToLower(u.Name)] = true
			}
		}
	}
	add(l.usages, d.Usage.User)
	add(l.values, d.Value.User)
	for _, u := range d.Root {
		if u.Name != "" {
			l.usages[strings.ToLower(u.Name)] = true
			l.values[strings.ToLower(u.Name)] = true
		}
	}
}

// AutoFix applies the safe, reversible validator fixes and returns a
// human-readable list of what it did. Currently: whitelist every unknown
// usage/value/tag/category referenced by types into cfglimitsdefinition.xml
// (the canonical way to stop DayZ silently dropping modded loot). A .bak is
// written before the file is touched. Server must be stopped (caller enforces).
func AutoFix(serverDir, missionTemplate string) ([]string, error) {
	if missionTemplate == "" {
		return nil, errors.New("no mission configured")
	}
	missionDir := dztypes.MissionDir(serverDir, missionTemplate)
	limitsPath := filepath.Join(missionDir, "cfglimitsdefinition.xml")
	limits, err := loadLimits(limitsPath)
	if err != nil || !limits.loaded {
		return nil, fmt.Errorf("cannot read cfglimitsdefinition.xml: %w", err)
	}
	mergeUserLimits(limits, filepath.Join(missionDir, "cfglimitsdefinitionuser.xml"))

	// Collect unknown names per kind (preserve original casing for writing).
	miss := map[string]map[string]string{ // kind -> lower -> original
		"category": {}, "usage": {}, "value": {}, "tag": {},
	}
	consider := func(set map[string]bool, kind, name string) {
		if name == "" {
			return
		}
		if !set[strings.ToLower(name)] {
			miss[kind][strings.ToLower(name)] = name
		}
	}
	scan := func(doc *dztypes.TypesDoc) {
		for i := range doc.Types {
			t := &doc.Types[i]
			if t.Category != nil {
				consider(limits.categories, "category", t.Category.Name)
			}
			for _, r := range t.Usages {
				consider(limits.usages, "usage", r.Name)
			}
			for _, r := range t.Values {
				consider(limits.values, "value", r.Name)
			}
			for _, r := range t.Tags {
				consider(limits.tags, "tag", r.Name)
			}
		}
	}
	if doc, err := dztypes.Load(filepath.Join(missionDir, "db", "types.xml")); err == nil {
		scan(doc)
	}
	moded := dztypes.ModedDir(serverDir, missionTemplate)
	if entries, err := os.ReadDir(moded); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			if doc, err := dztypes.Load(filepath.Join(moded, e.Name())); err == nil {
				scan(doc)
			}
		}
	}

	total := 0
	for _, m := range miss {
		total += len(m)
	}
	if total == 0 {
		return nil, nil
	}
	written, err := whitelistLimits(limitsPath, miss)
	if err != nil {
		return nil, err
	}
	// Report the names that were really inserted. Reporting `miss` meant a
	// section we failed to find was still announced as fixed.
	var out []string
	for _, kind := range []string{"category", "usage", "value", "tag"} {
		for _, name := range written[kind] {
			out = append(out, fmt.Sprintf("added %s %q to cfglimitsdefinition.xml", kind, name))
		}
	}
	return out, nil
}

// whitelistLimits inserts the missing names into the right section of
// cfglimitsdefinition.xml, before that section's closing tag. Backs up first.
func whitelistLimits(path string, miss map[string]map[string]string) (map[string][]string, error) {
	written := map[string][]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := string(data)
	// kind -> (element tag, section closing tag)
	sections := []struct{ kind, elem, closeTag string }{
		{"category", "category", "</categories>"},
		{"usage", "usage", "</usageflags>"},
		{"value", "value", "</valueflags>"},
		{"tag", "tag", "</tags>"},
	}
	for _, sec := range sections {
		names := miss[sec.kind]
		if len(names) == 0 {
			continue
		}
		var b strings.Builder
		for _, orig := range names {
			b.WriteString(fmt.Sprintf("\t\t<%s name=\"%s\"/>\n", sec.elem, xmlEscapeAttr(orig)))
		}
		if idx := strings.LastIndex(s, sec.closeTag); idx != -1 {
			s = s[:idx] + b.String() + "\t" + s[idx:]
		} else if end := strings.LastIndex(s, "</lists>"); end != -1 {
			// Section missing entirely — create it.
			plural := map[string]string{"category": "categories", "usage": "usageflags", "value": "valueflags", "tag": "tags"}[sec.kind]
			block := fmt.Sprintf("\t<%s>\n%s\t</%s>\n", plural, b.String(), plural)
			s = s[:end] + block + s[end:]
		} else {
			// Neither anchor found: this is not a limits file we understand,
			// and guessing would corrupt it.
			return nil, fmt.Errorf("cannot find where to insert %s names in %s — file has no <%s> section and no </lists>",
				sec.kind, filepath.Base(path), sec.kind)
		}
		for _, orig := range names {
			written[sec.kind] = append(written[sec.kind], orig)
		}
	}
	if err := util.BackupBeforeWrite(path); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return written, nil
}

func xmlEscapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func validateCFG(path string) *Issue {
	data, err := os.ReadFile(path)
	if err != nil {
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
	// Braces inside a quoted value are data, not structure: a
	// `hostname = "My { server";` used to be reported as unbalanced.
	// (`close` also shadowed the builtin here.)
	opened, closed, inStr := 0, 0, false
	for _, r := range string(data) {
		switch {
		case r == '"':
			inStr = !inStr
		case r == '\n':
			inStr = false // an unterminated quote must not swallow the file
		case inStr:
		case r == '{':
			opened++
		case r == '}':
			closed++
		}
	}
	if opened != closed {
		return &Issue{File: path, Severity: SevError,
			Message: fmt.Sprintf("unbalanced braces: %d '{' vs %d '}'", opened, closed)}
	}
	return nil
}
