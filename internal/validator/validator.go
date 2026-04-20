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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	dztypes "dayzmanager/internal/types"
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
