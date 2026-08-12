// Copyright (c) 2026 Aristarh Ucolov.
//
// cfgplayerspawnpoints.xml parser/writer — where players spawn: fresh (new
// character), hop (server hop) and travel. Each section carries three tuning
// blocks (spawn_params, generator_params, group_params) and one or more
// generator_posbubbles, each a set of named <group>s of <pos x z> points.
//
//	<playerspawnpoints>
//	  <fresh>
//	    <spawn_params><min_dist_player>65</min_dist_player>…</spawn_params>
//	    <generator_params><grid_density>4</grid_density>…</generator_params>
//	    <group_params><lifetime>120</lifetime>…</group_params>
//	    <generator_posbubbles>
//	      <group name="WestCherno"><pos x="6063.0" z="1931.9" /></group>
//	    </generator_posbubbles>
//	  </fresh>
//	  <hop>…</hop>
//	  <travel>…</travel>
//	</playerspawnpoints>
//
// The three param blocks are modelled as ordered key/value leaves so any field
// — even one this build has never seen — round-trips unchanged. Multiple
// generator_posbubbles per section (Sakhal ships two) are preserved via each
// group's Block index.
package types

import (
	"bytes"
	"encoding/xml"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ---- parsing structs ----

type psLeaf struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}
type psParamBlock struct {
	Leaves []psLeaf `xml:",any"`
}
type psPosXML struct {
	X float64 `xml:"x,attr"`
	Z float64 `xml:"z,attr"`
}
type psGroupXML struct {
	Name string     `xml:"name,attr"`
	Pos  []psPosXML `xml:"pos"`
}
type psBubbles struct {
	Groups []psGroupXML `xml:"group"`
}
type psSectionXML struct {
	Spawn   *psParamBlock `xml:"spawn_params"`
	Gen     *psParamBlock `xml:"generator_params"`
	GroupP  *psParamBlock `xml:"group_params"`
	Bubbles []psBubbles   `xml:"generator_posbubbles"`
}
type psDocXML struct {
	XMLName xml.Name      `xml:"playerspawnpoints"`
	Fresh   *psSectionXML `xml:"fresh"`
	Hop     *psSectionXML `xml:"hop"`
	Travel  *psSectionXML `xml:"travel"`
}

// ---- public JSON model ----

type PSKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type PSPos struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}
type PSGroup struct {
	Name  string  `json:"name"`
	Block int     `json:"block"` // which generator_posbubbles block it came from
	Pos   []PSPos `json:"pos"`
}
type PSSection struct {
	Present     bool      `json:"present"`
	SpawnParams []PSKV    `json:"spawnParams"`
	GenParams   []PSKV    `json:"genParams"`
	GroupParams []PSKV    `json:"groupParams"`
	Groups      []PSGroup `json:"groups"`
}
type PlayerSpawnDoc struct {
	Fresh  PSSection `json:"fresh"`
	Hop    PSSection `json:"hop"`
	Travel PSSection `json:"travel"`
}

func LoadPlayerSpawns(path string) (*PlayerSpawnDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw psDocXML
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return &PlayerSpawnDoc{
		Fresh:  convSection(raw.Fresh),
		Hop:    convSection(raw.Hop),
		Travel: convSection(raw.Travel),
	}, nil
}

func convSection(s *psSectionXML) PSSection {
	if s == nil {
		return PSSection{}
	}
	toKV := func(b *psParamBlock) []PSKV {
		if b == nil {
			return nil
		}
		kv := make([]PSKV, 0, len(b.Leaves))
		for _, l := range b.Leaves {
			kv = append(kv, PSKV{Key: l.XMLName.Local, Value: strings.TrimSpace(l.Value)})
		}
		return kv
	}
	out := PSSection{Present: true, SpawnParams: toKV(s.Spawn), GenParams: toKV(s.Gen), GroupParams: toKV(s.GroupP)}
	for bi, bl := range s.Bubbles {
		for _, g := range bl.Groups {
			pg := PSGroup{Name: g.Name, Block: bi}
			for _, p := range g.Pos {
				pg.Pos = append(pg.Pos, PSPos{X: p.X, Z: p.Z})
			}
			out.Groups = append(out.Groups, pg)
		}
	}
	return out
}

// psCoord renders a coordinate with the shortest exact decimal so re-saving an
// untouched point never invents precision.
func psCoord(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// MarshalPlayerSpawns renders cfgplayerspawnpoints.xml in DayZ's own style: tab
// indent, self-closing <pos>, param leaves in order, one generator_posbubbles
// per distinct group Block. Absent sections are omitted.
func MarshalPlayerSpawns(doc *PlayerSpawnDoc) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>` + "\n")
	b.WriteString("<playerspawnpoints>\n")

	writeSection := func(tag string, s *PSSection) {
		if !s.Present {
			return
		}
		b.WriteString("\t<" + tag + ">\n")
		writeParams := func(pt string, kvs []PSKV) {
			b.WriteString("\t\t<" + pt + ">\n")
			for _, kv := range kvs {
				if strings.TrimSpace(kv.Key) == "" {
					continue
				}
				b.WriteString("\t\t\t<" + kv.Key + ">" + attrEsc(kv.Value) + "</" + kv.Key + ">\n")
			}
			b.WriteString("\t\t</" + pt + ">\n")
		}
		writeParams("spawn_params", s.SpawnParams)
		writeParams("generator_params", s.GenParams)
		writeParams("group_params", s.GroupParams)

		// Re-emit one generator_posbubbles per distinct block, preserving the
		// split BI ships (Sakhal) while a new group defaults to block 0.
		byBlock := map[int][]PSGroup{}
		var blocks []int
		for _, g := range s.Groups {
			if strings.TrimSpace(g.Name) == "" {
				continue
			}
			if _, ok := byBlock[g.Block]; !ok {
				blocks = append(blocks, g.Block)
			}
			byBlock[g.Block] = append(byBlock[g.Block], g)
		}
		sort.Ints(blocks)
		if len(blocks) == 0 {
			b.WriteString("\t\t<generator_posbubbles/>\n")
		}
		for _, bi := range blocks {
			b.WriteString("\t\t<generator_posbubbles>\n")
			for _, g := range byBlock[bi] {
				b.WriteString("\t\t\t<group name=\"" + attrEsc(g.Name) + "\">\n")
				for _, p := range g.Pos {
					b.WriteString("\t\t\t\t<pos x=\"" + psCoord(p.X) + "\" z=\"" + psCoord(p.Z) + "\" />\n")
				}
				b.WriteString("\t\t\t</group>\n")
			}
			b.WriteString("\t\t</generator_posbubbles>\n")
		}
		b.WriteString("\t</" + tag + ">\n")
	}
	writeSection("fresh", &doc.Fresh)
	writeSection("hop", &doc.Hop)
	writeSection("travel", &doc.Travel)
	b.WriteString("</playerspawnpoints>\n")
	return b.Bytes()
}
