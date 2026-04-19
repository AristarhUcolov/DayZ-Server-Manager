// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgeconomycore.xml register/unregister for custom types files.
//
// This implementation uses surgical regex edits over the original file bytes
// so formatting, comments, attribute order, and all non-CE content is
// preserved byte-for-byte. (An earlier xml.Unmarshal-based version dropped
// formatting and occasionally round-tripped incorrectly.)
//
// Structure recap:
//   <economycore>
//     <classes>...</classes>
//     <defaults>...</defaults>
//     <ce folder="db">
//       <file name="events.xml" type="events" />
//     </ce>
//     <ce folder="moded_types">
//       <file name="mymod_types.xml" type="types" />
//     </ce>
//   </economycore>
package types

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ModedFolder = "moded_types"

// EcoFileRef is a flattened <ce folder=X><file name=Y type=Z /> entry.
type EcoFileRef struct {
	Folder string
	Name   string
	Type   string
}

var (
	reModedCE = regexp.MustCompile(`(?s)<ce\s+folder="moded_types"\s*>(.*?)</ce>`)
	reAnyCE   = regexp.MustCompile(`(?s)<ce\s+folder="([^"]+)"\s*>(.*?)</ce>`)
	reFile    = regexp.MustCompile(`<file\s+name="([^"]+)"\s+type="([^"]+)"\s*/?>`)
)

// MissionDir returns the mission directory given a server dir and mission template
// like "dayzOffline.chernarusplus" → <serverDir>/mpmissions/dayzOffline.chernarusplus.
func MissionDir(serverDir, missionTemplate string) string {
	return filepath.Join(serverDir, "mpmissions", missionTemplate)
}

// ModedDir returns the absolute path to the moded_types folder of a mission.
func ModedDir(serverDir, missionTemplate string) string {
	return filepath.Join(MissionDir(serverDir, missionTemplate), ModedFolder)
}

// ListEconomyCE returns every <file> referenced from cfgeconomycore.xml.
func ListEconomyCE(path string) ([]EcoFileRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []EcoFileRef
	for _, ce := range reAnyCE.FindAllStringSubmatch(string(data), -1) {
		folder := ce[1]
		for _, f := range reFile.FindAllStringSubmatch(ce[2], -1) {
			out = append(out, EcoFileRef{Folder: folder, Name: f[1], Type: f[2]})
		}
	}
	return out, nil
}

// RegisteredInModed returns the set of file names currently registered in the
// moded_types CE block (lower-cased for case-insensitive matching).
func RegisteredInModed(path string) (map[string]bool, error) {
	refs, err := ListEconomyCE(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range refs {
		if r.Folder == ModedFolder {
			out[strings.ToLower(r.Name)] = true
		}
	}
	return out, nil
}

// RegisterModedFile inserts <file name="fileName" type="fileType" /> into
// the moded_types CE block, creating the block before </economycore> if
// it does not yet exist. No-op if the file is already listed.
func RegisterModedFile(ecoPath, fileName, fileType string) error {
	data, err := os.ReadFile(ecoPath)
	if err != nil {
		return err
	}
	s := string(data)
	entry := fmt.Sprintf(`<file name="%s" type="%s" />`, fileName, fileType)

	if m := reModedCE.FindStringSubmatchIndex(s); m != nil {
		inner := s[m[2]:m[3]]
		if strings.Contains(inner, fmt.Sprintf(`name="%s"`, fileName)) {
			return nil // already registered
		}
		newInner := strings.TrimRight(inner, " \t\n") + "\n        " + entry + "\n    "
		s = s[:m[2]] + newInner + s[m[3]:]
	} else {
		if !strings.Contains(s, "</economycore>") {
			return fmt.Errorf("invalid cfgeconomycore.xml: no </economycore> tag")
		}
		block := fmt.Sprintf("    <ce folder=\"moded_types\">\n        %s\n    </ce>\n", entry)
		s = strings.Replace(s, "</economycore>", block+"</economycore>", 1)
	}
	return atomicWriteFile(ecoPath, []byte(s))
}

// UnregisterModedFile removes the matching <file> line. If the enclosing
// moded_types block becomes empty it is removed too. Returns true when
// something was actually removed.
func UnregisterModedFile(ecoPath, fileName string) (bool, error) {
	data, err := os.ReadFile(ecoPath)
	if err != nil {
		return false, err
	}
	s := string(data)

	m := reModedCE.FindStringSubmatchIndex(s)
	if m == nil {
		return false, nil
	}
	inner := s[m[2]:m[3]]
	lineRe := regexp.MustCompile(`[^\S\n]*<file\s+name="` + regexp.QuoteMeta(fileName) + `"[^/]*/?>[^\S\n]*\n?`)
	if !lineRe.MatchString(inner) {
		return false, nil
	}
	newInner := lineRe.ReplaceAllString(inner, "")

	if strings.TrimSpace(newInner) == "" {
		// Remove the whole <ce folder="moded_types">...</ce> block (and its line).
		blockStart, blockEnd := m[0], m[1]
		// Eat preceding whitespace up to and including a newline so we don't leave a blank line.
		for blockStart > 0 && (s[blockStart-1] == ' ' || s[blockStart-1] == '\t') {
			blockStart--
		}
		if blockStart > 0 && s[blockStart-1] == '\n' {
			blockStart--
		}
		s = s[:blockStart] + s[blockEnd:]
	} else {
		s = s[:m[2]] + newInner + s[m[3]:]
	}
	return true, atomicWriteFile(ecoPath, []byte(s))
}

var ErrNoMission = errors.New("no mission template configured in server.cfg")

// ---------------------------------------------------------------------------

func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
