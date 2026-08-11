// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgrandompresets.xml parser — reusable cargo/attachment groups that
// cfgspawnabletypes references by name. Each group has a spawn chance and a
// weighted list of items; exactly like the per-weapon attachment slots, but
// shared across many spawnable types. This is the data behind the presets
// library, which shows the *real* per-item chance (group chance × item weight ÷
// sum of weights) instead of the raw numbers.
//
//	<randompresets>
//	  <cargo chance="0.30" name="mtAmmoBox">
//	    <item name="AmmoBox_556x45_20Rnd" chance="0.20"/>
//	  </cargo>
//	  <attachments chance="1.00" name="apAKM">
//	    <item name="AK_Suppressor" chance="0.20"/>
//	  </attachments>
//	</randompresets>
package types

import (
	"bytes"
	"encoding/xml"
	"os"
	"strconv"
	"strings"
)

type PresetItem struct {
	XMLName xml.Name `xml:"item" json:"-"`
	Name    string   `xml:"name,attr" json:"name"`
	Chance  float64  `xml:"chance,attr" json:"chance"`
}

type RandomPreset struct {
	Name   string       `xml:"name,attr" json:"name"`
	Chance float64      `xml:"chance,attr" json:"chance"`
	Items  []PresetItem `xml:"item" json:"items"`
}

type RandomPresetsDoc struct {
	XMLName     xml.Name       `xml:"randompresets" json:"-"`
	Cargo       []RandomPreset `xml:"cargo" json:"-"`
	Attachments []RandomPreset `xml:"attachments" json:"-"`
}

func LoadRandomPresets(path string) (*RandomPresetsDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc RandomPresetsDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// fmtChance renders a chance with the shortest exact decimal (0.3, 1, 0.05) so
// re-saving never invents precision the admin didn't type.
func fmtChance(v float64) string {
	if v < 0 {
		v = 0
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func attrEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// MarshalRandomPresets renders a cfgrandompresets.xml byte-for-byte in DayZ's
// own style: tab indentation, self-closing <item> tags, chance before name on
// groups. Kept hand-rolled (not encoding/xml) so the output matches vanilla and
// stays diff-friendly across saves.
func MarshalRandomPresets(doc *RandomPresetsDoc) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header) // <?xml version="1.0" encoding="UTF-8"?>\n
	b.WriteString("<randompresets>\n")
	writeGroup := func(tag string, ps []RandomPreset) {
		for _, p := range ps {
			if strings.TrimSpace(p.Name) == "" {
				continue
			}
			b.WriteString("\t<" + tag + " chance=\"" + fmtChance(p.Chance) + "\" name=\"" + attrEsc(p.Name) + "\">\n")
			for _, it := range p.Items {
				if strings.TrimSpace(it.Name) == "" {
					continue
				}
				b.WriteString("\t\t<item name=\"" + attrEsc(it.Name) + "\" chance=\"" + fmtChance(it.Chance) + "\"/>\n")
			}
			b.WriteString("\t</" + tag + ">\n")
		}
	}
	writeGroup("cargo", doc.Cargo)
	writeGroup("attachments", doc.Attachments)
	b.WriteString("</randompresets>\n")
	return b.Bytes()
}
