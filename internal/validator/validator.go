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
		eco := filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), "cfgeconomycore.xml")
		if refs, err := dztypes.ListEconomyCE(eco); err == nil {
			for _, r := range refs {
				p := filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), r.Folder, r.Name)
				if _, err := os.Stat(p); err != nil {
					issues = append(issues, Issue{
						File:     eco,
						Severity: SevError,
						Message:  fmt.Sprintf("referenced file missing: %s/%s", r.Folder, r.Name),
					})
				}
			}
		}

		// cfglimitsdefinition.xml — the whitelist of category/usage/value/tag
		// names. Types referencing anything outside this list are silently
		// dropped by DayZ at runtime, which makes for confusing loot bugs.
		limitsPath := filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), "db", "cfglimitsdefinition.xml")
		limits, _ := loadLimits(limitsPath)
		mergeUserLimits(limits, filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), "db", "cfglimitsdefinitionuser.xml"))

		// Duplicate type names across types.xml + moded_types/*.xml.
		seen := map[string]string{}
		typesPath := filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), "db", "types.xml")
		if doc, err := dztypes.Load(typesPath); err == nil {
			for _, t := range doc.Types {
				seen[strings.ToLower(t.Name)] = typesPath
				issues = append(issues, checkLimits(typesPath, &t, limits)...)
			}
		}
		moded := dztypes.ModedDir(serverDir, missionTemplate)
		modedEntries, _ := os.ReadDir(moded)
		for _, e := range modedEntries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			p := filepath.Join(moded, e.Name())
			doc, err := dztypes.Load(p)
			if err != nil {
				continue
			}
			for _, t := range doc.Types {
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
				issues = append(issues, checkLimits(p, &t, limits)...)
			}
		}
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

func loadLimits(path string) (*limits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &limits{}, err
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
		return &limits{}, err
	}
	l := &limits{
		categories: map[string]bool{},
		usages:     map[string]bool{},
		values:     map[string]bool{},
		tags:       map[string]bool{},
		loaded:     true,
	}
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

func checkLimits(path string, t *dztypes.Type, l *limits) []Issue {
	if l == nil || !l.loaded {
		return nil
	}
	var out []Issue
	if t.Category != nil && t.Category.Name != "" && !l.categories[strings.ToLower(t.Category.Name)] {
		out = append(out, Issue{
			File: path, Severity: SevWarning,
			Message: fmt.Sprintf("type %q uses unknown category %q — not in cfglimitsdefinition.xml", t.Name, t.Category.Name),
		})
	}
	check := func(set map[string]bool, kind string, refs []dztypes.NamedRef) {
		for _, r := range refs {
			if r.Name == "" {
				continue
			}
			if !set[strings.ToLower(r.Name)] {
				out = append(out, Issue{
					File: path, Severity: SevWarning,
					Message: fmt.Sprintf("type %q uses unknown %s %q — not in cfglimitsdefinition.xml", t.Name, kind, r.Name),
				})
			}
		}
	}
	check(l.usages, "usage", t.Usages)
	check(l.values, "value", t.Values)
	check(l.tags, "tag", t.Tags)
	return out
}

// mergeUserLimits folds cfglimitsdefinitionuser.xml `<user name="X">` groups
// into the limits sets. Those user names are legal usage/value references in
// types.xml, so counting them avoids false "unknown usage/value" warnings.
func mergeUserLimits(l *limits, userPath string) {
	if l == nil || !l.loaded {
		return
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		return
	}
	type named struct {
		Name string `xml:"name,attr"`
	}
	type doc struct {
		User []named `xml:"user"`
	}
	var d doc
	if xml.Unmarshal(data, &d) != nil {
		return
	}
	for _, u := range d.User {
		if u.Name == "" {
			continue
		}
		key := strings.ToLower(u.Name)
		l.usages[key] = true
		l.values[key] = true
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
	limitsPath := filepath.Join(missionDir, "db", "cfglimitsdefinition.xml")
	limits, err := loadLimits(limitsPath)
	if err != nil || !limits.loaded {
		return nil, fmt.Errorf("cannot read cfglimitsdefinition.xml: %w", err)
	}
	mergeUserLimits(limits, filepath.Join(missionDir, "db", "cfglimitsdefinitionuser.xml"))

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
	if err := whitelistLimits(limitsPath, miss); err != nil {
		return nil, err
	}
	var out []string
	for _, kind := range []string{"category", "usage", "value", "tag"} {
		for _, name := range miss[kind] {
			out = append(out, fmt.Sprintf("added %s %q to cfglimitsdefinition.xml", kind, name))
		}
	}
	return out, nil
}

// whitelistLimits inserts the missing names into the right section of
// cfglimitsdefinition.xml, before that section's closing tag. Backs up first.
func whitelistLimits(path string, miss map[string]map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
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
		}
	}
	if err := util.BackupBeforeWrite(path); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	open, close := 0, 0
	for _, r := range string(data) {
		switch r {
		case '{':
			open++
		case '}':
			close++
		}
	}
	if open != close {
		return &Issue{File: path, Severity: SevError,
			Message: fmt.Sprintf("unbalanced braces: %d '{' vs %d '}'", open, close)}
	}
	return nil
}
