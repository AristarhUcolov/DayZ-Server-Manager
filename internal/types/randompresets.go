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
