// Copyright (c) 2026 Aristarh Ucolov.
//
// globals.xml parser/writer — DayZ central-economy global variables (cleanup
// lifetimes, spawn distances, login/logout timers, flag refresh, night length,
// etc.). A flat list of typed variables:
//
//	<variables>
//	  <var name="AnimalMaxCount" type="0" value="100"/>
//	  <var name="CleanupAvoidance" type="1" value="0.35"/>
//	</variables>
//
// type 0 = integer, type 1 = float. Values are kept as strings so a float's
// exact written form ("0.35", "1.0") survives an edit untouched.
package types

import (
	"bytes"
	"encoding/xml"
	"os"
	"strings"
)

type GlobalVar struct {
	XMLName xml.Name `xml:"var" json:"-"`
	Name    string   `xml:"name,attr" json:"name"`
	Type    string   `xml:"type,attr" json:"type"`
	Value   string   `xml:"value,attr" json:"value"`
}

type GlobalsDoc struct {
	XMLName xml.Name    `xml:"variables" json:"-"`
	Vars    []GlobalVar `xml:"var" json:"vars"`
}

func LoadGlobals(path string) (*GlobalsDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc GlobalsDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// SetValue updates one variable's value (case-insensitive name match). Returns
// false when the variable is not present — the editor never invents new globals.
func (d *GlobalsDoc) SetValue(name, value string) bool {
	for i := range d.Vars {
		if strings.EqualFold(d.Vars[i].Name, name) {
			d.Vars[i].Value = value
			return true
		}
	}
	return false
}

// SaveGlobals writes the document back with an XML header, tab indent and
// self-closing <var/> elements to match the game's own style.
func SaveGlobals(path string, doc *GlobalsDoc) error {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "\t")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	buf.WriteByte('\n')
	out := bytes.ReplaceAll(buf.Bytes(), []byte("></var>"), []byte("/>"))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
