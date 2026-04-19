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
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
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

		// Duplicate type names across types.xml + moded_types/*.xml.
		seen := map[string]string{}
		typesPath := filepath.Join(dztypes.MissionDir(serverDir, missionTemplate), "db", "types.xml")
		if doc, err := dztypes.Load(typesPath); err == nil {
			for _, t := range doc.Types {
				seen[strings.ToLower(t.Name)] = typesPath
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
			}
		}
	}

	return issues, nil
}

func validateXML(path string) *Issue {
	f, err := os.Open(path)
	if err != nil {
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
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
