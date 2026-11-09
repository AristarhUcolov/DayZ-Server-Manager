// Copyright (c) 2026 Aristarh Ucolov.

package terrain

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"unicode"

	"dayzmanager/internal/pbo"
)

// Place is a named spot of the world's own map: a town, a hill, a bay.
type Place struct {
	Class string  `json:"class"` // config class: Settlement_Chernogorsk
	Name  string  `json:"name"`  // as the world names it: "Черногорск" or a $STR_ key
	Latin string  `json:"latin"` // from the class name: "Chernogorsk"
	Type  string  `json:"type"`  // Capital, City, Village, Local, Hill, Marine, Camp, Ruin…
	X     float64 `json:"x"`
	Z     float64 `json:"z"`
}

// ReadPlaces lists the place names of the world in the PBO's config.bin
// (CfgWorlds → <world> → Names). world picks the class when the config holds
// several ("" takes the first with names).
func ReadPlaces(pboPath, world string) ([]Place, error) {
	f, err := pbo.Open(pboPath)
	if err != nil {
		return nil, err
	}
	for _, e := range f.Find("config.bin") {
		data, err := f.Read(e)
		if err != nil {
			continue
		}
		root, err := pbo.ParseConfig(data)
		if err != nil {
			continue
		}
		worlds := root.Child("CfgWorlds")
		if worlds == nil {
			continue
		}
		var pick *pbo.Class
		for _, c := range worlds.Classes {
			if c.Child("Names") == nil {
				continue
			}
			if pick == nil || strings.EqualFold(c.Name, world) {
				pick = c
			}
		}
		if pick == nil {
			continue
		}
		var out []Place
		for _, c := range pick.Child("Names").Classes {
			name, _ := c.String("name")
			typ, _ := c.String("type")
			pos, _ := c.Values["position"].([]interface{})
			if len(pos) < 2 {
				continue
			}
			x, ok1 := pos[0].(float64)
			z, ok2 := pos[1].(float64)
			if !ok1 || !ok2 {
				continue
			}
			out = append(out, Place{Class: c.Name, Name: strings.TrimSpace(name), Latin: latinName(c.Name), Type: typ, X: x, Z: z})
		}
		return out, nil
	}
	return nil, errors.New("terrain: no place names in the world's config")
}

// latinName turns a config class into a readable name: the part after the
// kind ("Settlement_NovayaPetrovka" → "Novaya Petrovka", "Branzow_castle" →
// "Branzow castle").
func latinName(class string) string {
	s := class
	if i := strings.IndexByte(s, '_'); i > 0 {
		switch strings.ToLower(s[:i]) {
		case "settlement", "camp", "hill", "ruin", "marine", "local", "localoffice", "viewpoint", "railroadstation", "station", "city", "village", "name":
			s = s[i+1:]
		}
	}
	s = strings.ReplaceAll(s, "_", " ")
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(rs[i-1]) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return class
	}
	rs = []rune(out)
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}

// StringTable maps $STR_ keys (lower case, without the $) to their text per
// language column ("english", "russian", …).
type StringTable map[string]map[string]string

// ReadStringTable reads the game's stringtable.csv from a PBO (DayZ keeps it
// in dta/languagecore.pbo), keeping only the keys asked for.
func ReadStringTable(pboPath string, keys map[string]bool) (StringTable, error) {
	f, err := pbo.Open(pboPath)
	if err != nil {
		return nil, err
	}
	es := f.Find("stringtable.csv")
	if len(es) == 0 {
		return nil, errors.New("terrain: no stringtable.csv")
	}
	data, err := f.Read(es[0])
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		return nil, err
	}
	for i := range head {
		head[i] = strings.ToLower(strings.TrimSpace(head[i]))
	}
	out := StringTable{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(rec) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(rec[0]), "$"))
		if !keys[key] {
			continue
		}
		m := map[string]string{}
		for i := 1; i < len(rec) && i < len(head); i++ {
			if v := strings.TrimSpace(rec[i]); v != "" {
				m[head[i]] = v
			}
		}
		out[key] = m
	}
	return out, nil
}

// stringColumn is the stringtable column for a panel language.
var stringColumn = map[string]string{
	"en": "english", "ru": "russian", "de": "german", "fr": "french", "it": "italian",
	"es": "spanish", "pt": "portuguese", "cs": "czech", "pl": "polish", "hu": "hungarian",
	"zh": "chinesesimp", "zh-tw": "chinese", "ja": "japanese",
}

// Label is the name to show for a place in the panel language: the
// stringtable's text for a $STR_ name, the world's own name in Russian
// (Chernarus names its places in Cyrillic), the class-derived Latin name
// otherwise.
func (p Place) Label(lang string, st StringTable) string {
	name := p.Name
	if strings.HasPrefix(name, "$") {
		if m := st[strings.ToLower(strings.TrimPrefix(name, "$"))]; m != nil {
			if v := m[stringColumn[lang]]; v != "" {
				return v
			}
			if v := m["english"]; v != "" {
				return v
			}
			if v := m["original"]; v != "" {
				return v
			}
		}
		return p.Latin
	}
	if name == "" {
		return p.Latin
	}
	if hasCyrillic(name) && lang != "ru" {
		return p.Latin
	}
	return name
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}
