// Copyright (c) 2026 Aristarh Ucolov.

package types

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Raw-block helpers: copy one <event>/<type> definition from a file a mod
// ships into a mission file byte for byte — re-rendering would drop the
// fields the models don't carry (<flags>, <position>, <limit>, …).

// EventBlock returns the live <event name="X"> block of src (comments are
// ignored), or false.
func EventBlock(src, name string) (string, bool) {
	return liveBlock(src, eventBlockRe(name))
}

// TypeBlock returns the live <type name="X"> block of src — the same shape in
// types.xml and cfgspawnabletypes.xml — or false.
func TypeBlock(src, name string) (string, bool) {
	return liveBlock(src, typeBlockRe(name))
}

func liveBlock(src string, re *regexp.Regexp) (string, bool) {
	loc := re.FindStringIndex(maskComments(src))
	if loc == nil {
		return "", false
	}
	block := strings.TrimRight(src[loc[0]:loc[1]], " \t\r\n")
	return strings.TrimLeft(block, " \t") + "\n", true
}

// EnsureXMLFile creates path holding an empty <root></root> document when it
// does not exist yet.
func EnsureXMLFile(path, root string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\" ?>\n<%s>\n</%s>\n", root, root)
	return writeAtomic(path, []byte(body))
}

// indentBlocks joins blocks, indented like DayZ's own files.
func indentBlocks(blocks []string) string {
	var b strings.Builder
	for _, bl := range blocks {
		for _, line := range strings.Split(strings.TrimRight(bl, "\r\n"), "\n") {
			line = strings.TrimRight(line, "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				line = "    " + line
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// AppendBlocks inserts blocks just before the closing </root> tag of path
// (backing the file up first, like every writer here).
func AppendBlocks(path, root string, blocks []string) error {
	if len(blocks) == 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(data)
	idx := strings.LastIndex(maskComments(src), "</"+root+">")
	if idx < 0 {
		return fmt.Errorf("%s: closing </%s> not found", filepath.Base(path), root)
	}
	return writeAtomic(path, []byte(src[:idx]+indentBlocks(blocks)+src[idx:]))
}

// CreateXMLFile writes a new <root> document holding blocks in one go — no
// backup of an empty skeleton first. It refuses to overwrite a file.
func CreateXMLFile(path, root string, blocks []string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\" ?>\n<%s>\n%s</%s>\n", root, indentBlocks(blocks), root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var reCEFolder = func(folder string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<ce\s+folder="` + regexp.QuoteMeta(folder) + `"\s*>(.*?)</ce>`)
}

// RegisterCEFile lists <file name="…" type="…"/> under <ce folder="…"> in
// cfgeconomycore.xml, creating the block when needed. No-op when present.
func RegisterCEFile(ecoPath, folder, fileName, fileType string) error {
	return RegisterCEFiles(ecoPath, folder, [][2]string{{fileName, fileType}})
}

// RegisterCEFiles registers several files of one folder in a single write
// (one backup that still holds the file as it was). Commented-out blocks and
// <file> lines do not count as registered.
func RegisterCEFiles(ecoPath, folder string, files [][2]string) error {
	data, err := os.ReadFile(ecoPath)
	if err != nil {
		return err
	}
	s := string(data)
	changed := false
	for _, f := range files {
		fileName, fileType := f[0], f[1]
		entry := fmt.Sprintf(`<file name="%s" type="%s" />`, fileName, fileType)
		masked := maskComments(s)
		if m := reCEFolder(folder).FindStringSubmatchIndex(masked); m != nil {
			present := false
			for _, ft := range reFileTag.FindAllStringSubmatch(masked[m[2]:m[3]], -1) {
				if name, _ := fileAttrs(ft[1]); strings.EqualFold(name, fileName) {
					present = true
				}
			}
			if present {
				continue
			}
			inner := s[m[2]:m[3]]
			newInner := strings.TrimRight(inner, " \t\r\n") + "\n        " + entry + "\n    "
			s = s[:m[2]] + newInner + s[m[3]:]
		} else {
			idx := strings.LastIndex(masked, "</economycore>")
			if idx < 0 {
				return fmt.Errorf("invalid cfgeconomycore.xml: no </economycore> tag")
			}
			block := fmt.Sprintf("    <ce folder=\"%s\">\n        %s\n    </ce>\n", folder, entry)
			s = s[:idx] + block + s[idx:]
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return atomicWriteFile(ecoPath, []byte(s))
}
