// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeAreaFlags builds a synthetic areaflags.map: w×h cells, 32-bit usage,
// value layer at valueBits (8 or 4), with value set on the SOUTH-WEST cell only.
func writeAreaFlags(t *testing.T, w, h int, world uint32, valueBits int) string {
	t.Helper()
	var b bytes.Buffer
	for _, v := range []uint32{uint32(w), uint32(h), world, world, 32, 0} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.Write(make([]byte, w*h*4)) // usage: all zero
	n := w * h
	if valueBits == 8 {
		vals := make([]byte, n)
		vals[0] = 0x08 // row 0 (south), col 0 (west): Tier4
		b.Write(vals)
	} else {
		vals := make([]byte, n/2)
		vals[0] = 0x04 // low nibble = cell 0: Tier3 (bit 2)
		b.Write(vals)
	}
	p := filepath.Join(t.TempDir(), "areaflags.map")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAreaFlagsHeaderAndRender(t *testing.T) {
	names := []string{"Tier1", "Tier2", "Tier3", "Tier4", "Unique"}
	for _, bits := range []int{8, 4} {
		p := writeAreaFlags(t, 16, 16, 12800, bits)
		hd, err := readAreaFlagsHeader(p)
		if err != nil {
			t.Fatalf("%d-bit: %v", bits, err)
		}
		if hd.WorldW != 12800 || hd.valueBits != bits {
			t.Fatalf("%d-bit header = %+v", bits, hd)
		}
		img, err := renderAreaFlags(p, "tiers", 16, names)
		if err != nil {
			t.Fatal(err)
		}
		// South-west cell must land in the image's BOTTOM-left (north up).
		if _, _, _, a := img.At(0, 15).RGBA(); a == 0 {
			t.Errorf("%d-bit: south-west cell not drawn bottom-left", bits)
		}
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("%d-bit: north-west corner should be empty (sea)", bits)
		}
		land, err := renderAreaFlags(p, "land", 16, names)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, a := land.At(0, 15).RGBA(); a == 0 {
			t.Errorf("%d-bit: land mask missing the flagged cell", bits)
		}
	}
	if _, err := readAreaFlagsHeader(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestBuildingKind(t *testing.T) {
	for name, want := range map[string]string{
		"Land_Mil_Barracks1": "mil", "Land_Ind_Factory_Big": "ind", "Land_House_1W12_Brown": "house",
		"Land_Wreck_Caravan_WBrown": "misc", "Land_Mil_Tower_Small": "mil",
	} {
		if got := buildingKind(name); got != want {
			t.Errorf("buildingKind(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestStockAreaFlags reads the real vanilla files when the reference server is
// present: Chernarus is 15360 m (8-bit tiers), Livonia 12800 m (4-bit).
func TestStockAreaFlags(t *testing.T) {
	base := filepath.Join("..", "..", "DayZServer", "mpmissions")
	for mission, want := range map[string]float64{"dayzOffline.chernarusplus": 15360, "dayzOffline.enoch": 12800, "dayzOffline.sakhal": 15360} {
		p := filepath.Join(base, mission, "areaflags.map")
		if _, err := os.Stat(p); err != nil {
			t.Skip("reference DayZServer fixture not present")
		}
		hd, err := readAreaFlagsHeader(p)
		if err != nil {
			t.Fatalf("%s: %v", mission, err)
		}
		if hd.WorldW != want {
			t.Errorf("%s: world %v, want %v", mission, hd.WorldW, want)
		}
	}
}
