// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgspawnabletypes.xml — weapon/item attachment ("обвесы") and cargo presets.
//
// Schema recap:
//
//	<spawnabletypes>
//	  <type name="AKM">
//	    <hoarder/>                                  (optional)
//	    <damage min="0.0" max="0.4"/>               (optional)
//	    <tag name="floor"/>                         (optional, repeatable)
//	    <attachments chance="1.00">                 slot: 100% spawns something
//	      <item name="Mag_AKM_30Rnd" chance="1.00"/>
//	    </attachments>
//	    <attachments chance="0.35">                 slot: 35% chance
//	      <item name="KobraOptic"  chance="0.60"/>  weighted pick — ONE of these
//	      <item name="PSO1Optic"   chance="0.40"/>
//	    </attachments>
//	    <cargo chance="0.30">…</cargo>              (containers)
//	  </type>
//	</spawnabletypes>
//
// Semantics that matter for the UI: the `chance` on a group is the probability
// the slot spawns at all; the `chance` on each item is a RELATIVE WEIGHT among
// that group's items — DayZ picks exactly one. Weights need not sum to 1.
//
// Writing strategy: SURGICAL. Only the edited <type> block is replaced (or
// appended); every other byte of the file — comments, formatting, unmodelled
// elements from future DayZ versions or mods — is preserved exactly. A full
// parse+re-encode would silently drop anything this file doesn't model.
package types

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dayzmanager/internal/util"
)

// SpawnItem is one candidate inside a group. Chance is kept as the raw string
// so "1.00" round-trips as "1.00" rather than being reformatted to "1".
type SpawnItem struct {
	Name   string `json:"name"`
	Chance string `json:"chance,omitempty"`
}

// SpawnGroup is one <attachments> or <cargo> slot.
type SpawnGroup struct {
	Chance string      `json:"chance,omitempty"`
	Preset string      `json:"preset,omitempty"` // <attachments preset="…"/> from cfgrandompresets.xml
	Items  []SpawnItem `json:"items,omitempty"`
}

// SpawnableType is one <type> entry.
type SpawnableType struct {
	Name        string       `json:"name"`
	Hoarder     bool         `json:"hoarder,omitempty"`
	DamageMin   string       `json:"damageMin,omitempty"`
	DamageMax   string       `json:"damageMax,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Attachments []SpawnGroup `json:"attachments,omitempty"`
	Cargo       []SpawnGroup `json:"cargo,omitempty"`
}

// ---- decode-only mirrors -------------------------------------------------

type xmlSpawnItem struct {
	Name   string `xml:"name,attr"`
	Chance string `xml:"chance,attr"`
}

type xmlSpawnGroup struct {
	Chance string         `xml:"chance,attr"`
	Preset string         `xml:"preset,attr"`
	Items  []xmlSpawnItem `xml:"item"`
}

type xmlDamage struct {
	Min string `xml:"min,attr"`
	Max string `xml:"max,attr"`
}

type xmlNamedTag struct {
	Name string `xml:"name,attr"`
}

type xmlSpawnType struct {
	Name        string          `xml:"name,attr"`
	Hoarder     *struct{}       `xml:"hoarder"`
	Damage      *xmlDamage      `xml:"damage"`
	Tags        []xmlNamedTag   `xml:"tag"`
	Attachments []xmlSpawnGroup `xml:"attachments"`
	Cargo       []xmlSpawnGroup `xml:"cargo"`
}

type xmlSpawnDoc struct {
	XMLName xml.Name       `xml:"spawnabletypes"`
	Types   []xmlSpawnType `xml:"type"`
}

// SpawnablePath returns the cfgspawnabletypes.xml path for a mission.
func SpawnablePath(serverDir, missionTemplate string) string {
	return MissionDir(serverDir, missionTemplate) + string(os.PathSeparator) + "cfgspawnabletypes.xml"
}

// LoadSpawnable parses every <type> in cfgspawnabletypes.xml.
func LoadSpawnable(path string) ([]SpawnableType, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc xmlSpawnDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse cfgspawnabletypes.xml: %w", err)
	}
	out := make([]SpawnableType, 0, len(doc.Types))
	for _, t := range doc.Types {
		st := SpawnableType{Name: t.Name, Hoarder: t.Hoarder != nil}
		if t.Damage != nil {
			st.DamageMin, st.DamageMax = t.Damage.Min, t.Damage.Max
		}
		for _, tag := range t.Tags {
			st.Tags = append(st.Tags, tag.Name)
		}
		st.Attachments = convGroups(t.Attachments)
		st.Cargo = convGroups(t.Cargo)
		out = append(out, st)
	}
	return out, nil
}

func convGroups(in []xmlSpawnGroup) []SpawnGroup {
	var out []SpawnGroup
	for _, g := range in {
		grp := SpawnGroup{Chance: g.Chance, Preset: g.Preset}
		for _, it := range g.Items {
			grp.Items = append(grp.Items, SpawnItem{Name: it.Name, Chance: it.Chance})
		}
		out = append(out, grp)
	}
	return out
}

// ---- rendering + surgical write ------------------------------------------

// Render produces the <type>…</type> block for one entry, indented to match
// the DayZ-shipped file style (4 spaces).
func (s *SpawnableType) Render() string {
	var b strings.Builder
	b.WriteString(`    <type name="` + xmlAttr(s.Name) + `">` + "\n")
	if s.Hoarder {
		b.WriteString("        <hoarder/>\n")
	}
	if s.DamageMin != "" || s.DamageMax != "" {
		b.WriteString(`        <damage min="` + xmlAttr(orZero(s.DamageMin)) +
			`" max="` + xmlAttr(orZero(s.DamageMax)) + `"/>` + "\n")
	}
	for _, tag := range s.Tags {
		if strings.TrimSpace(tag) == "" {
			continue
		}
		b.WriteString(`        <tag name="` + xmlAttr(tag) + `"/>` + "\n")
	}
	renderGroups(&b, "attachments", s.Attachments)
	renderGroups(&b, "cargo", s.Cargo)
	b.WriteString("    </type>")
	return b.String()
}

func renderGroups(b *strings.Builder, tag string, groups []SpawnGroup) {
	for _, g := range groups {
		// A preset group references cfgrandompresets.xml and carries no items.
		if strings.TrimSpace(g.Preset) != "" {
			b.WriteString(`        <` + tag + ` chance="` + xmlAttr(orOne(g.Chance)) +
				`" preset="` + xmlAttr(g.Preset) + `"/>` + "\n")
			continue
		}
		items := make([]SpawnItem, 0, len(g.Items))
		for _, it := range g.Items {
			if strings.TrimSpace(it.Name) != "" {
				items = append(items, it)
			}
		}
		if len(items) == 0 {
			continue // an empty slot would just confuse DayZ — drop it
		}
		b.WriteString(`        <` + tag + ` chance="` + xmlAttr(orOne(g.Chance)) + `">` + "\n")
		for _, it := range items {
			b.WriteString(`            <item name="` + xmlAttr(it.Name) +
				`" chance="` + xmlAttr(orOne(it.Chance)) + `"/>` + "\n")
		}
		b.WriteString(`        </` + tag + ">\n")
	}
}

func orOne(s string) string {
	if strings.TrimSpace(s) == "" {
		return "1.00"
	}
	return strings.TrimSpace(s)
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0.0"
	}
	return strings.TrimSpace(s)
}

func xmlAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// ValidChance reports whether s is a parseable, sane probability/weight.
func ValidChance(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true // defaulted on render
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f >= 0
}

// typeBlockRe builds a regex matching one <type name="X"> block (or the
// self-closing form). <type> never nests, so a non-greedy match is exact.
func typeBlockRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)[ \t]*<type\s+name="` + regexp.QuoteMeta(name) + `"\s*(?:/>|>.*?</type>)[ \t]*\r?\n?`)
}

// SaveSpawnableType writes one <type> block into cfgspawnabletypes.xml,
// replacing the existing entry with the same name or appending a new one just
// before </spawnabletypes>. Every other byte of the file is untouched.
func SaveSpawnableType(path string, st *SpawnableType) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(data)
	block := st.Render() + "\n"

	if re := typeBlockRe(st.Name); re.MatchString(src) {
		src = re.ReplaceAllLiteralString(src, block)
	} else {
		idx := strings.LastIndex(src, "</spawnabletypes>")
		if idx < 0 {
			return fmt.Errorf("cfgspawnabletypes.xml: closing </spawnabletypes> not found")
		}
		src = src[:idx] + block + src[idx:]
	}
	return writeAtomic(path, []byte(src))
}

// DeleteSpawnableType removes a <type> block by name. Returns false when the
// entry was not present.
func DeleteSpawnableType(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	src := string(data)
	re := typeBlockRe(name)
	if !re.MatchString(src) {
		return false, nil
	}
	return true, writeAtomic(path, []byte(re.ReplaceAllLiteralString(src, "")))
}

func writeAtomic(path string, data []byte) error {
	_ = util.BackupBeforeWrite(path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
