// Copyright (c) 2026 Aristarh Ucolov.
//
// events.xml parser/writer. DayZ event definitions (zombie/vehicle/helicrash
// spawn tables). Structure:
//
//   <events>
//     <event name="AnimalCow">
//       <nominal>12</nominal>
//       <min>5</min>
//       <max>0</max>
//       <lifetime>300</lifetime>
//       <restock>0</restock>
//       <saveable>0</saveable>
//       <active>1</active>
//       <children>
//         <child lootmax="0" lootmin="0" max="-1" min="1" type="Animal_BosTaurus"/>
//       </children>
//     </event>
//   </events>
package types

import (
	"encoding/xml"
	"os"
)

type EventsDoc struct {
	XMLName xml.Name `xml:"events"`
	Events  []Event  `xml:"event"`
}

type Event struct {
	XMLName  xml.Name `xml:"event"`
	Name     string   `xml:"name,attr"`
	Nominal  *int     `xml:"nominal,omitempty"`
	Min      *int     `xml:"min,omitempty"`
	Max      *int     `xml:"max,omitempty"`
	Lifetime *int     `xml:"lifetime,omitempty"`
	Restock  *int     `xml:"restock,omitempty"`
	Saveable *int     `xml:"saveable,omitempty"`
	Active   *int     `xml:"active,omitempty"`
	Children *struct {
		Child []EventChild `xml:"child"`
	} `xml:"children,omitempty"`
}

type EventChild struct {
	XMLName xml.Name `xml:"child"`
	LootMax int      `xml:"lootmax,attr"`
	LootMin int      `xml:"lootmin,attr"`
	Max     int      `xml:"max,attr"`
	Min     int      `xml:"min,attr"`
	Type    string   `xml:"type,attr"`
}

func LoadEvents(path string) (*EventsDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := &EventsDoc{}
	if err := xml.Unmarshal(data, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// NOTE: there is deliberately no EventsDoc.Save. This struct models only the
// seven numbers the editor exposes; marshalling it back out deleted <flags>,
// <position>, <limit>, <secondary> and the three radii from every event in the
// file. Writes go through the surgical helpers in events_write.go.

func (d *EventsDoc) Find(name string) *Event {
	for i := range d.Events {
		if d.Events[i].Name == name {
			return &d.Events[i]
		}
	}
	return nil
}

