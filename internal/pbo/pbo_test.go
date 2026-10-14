// Copyright (c) 2026 Aristarh Ucolov.
package pbo

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// rapClass builds a binarized class for tests: parent, values, sub-classes.
type rapClass struct {
	parent  string
	ints    map[string]int32
	strs    map[string]string
	arrays  map[string][]string
	classes []struct {
		name string
		c    *rapClass
	}
}

// encodeRap writes a raP file with root as the root class.
func encodeRap(root *rapClass) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x00raP")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(8))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // enum offset (unused)
	type fix struct {
		at int
		c  *rapClass
	}
	var pending []fix
	var write func(c *rapClass)
	write = func(c *rapClass) {
		buf.WriteString(c.parent)
		buf.WriteByte(0)
		n := len(c.ints) + len(c.strs) + len(c.arrays) + len(c.classes)
		buf.WriteByte(byte(n)) // < 128 in tests
		for k, v := range c.ints {
			buf.Write([]byte{1, 2})
			buf.WriteString(k)
			buf.WriteByte(0)
			_ = binary.Write(&buf, binary.LittleEndian, v)
		}
		for k, v := range c.strs {
			buf.Write([]byte{1, 0})
			buf.WriteString(k)
			buf.WriteByte(0)
			buf.WriteString(v)
			buf.WriteByte(0)
		}
		for k, arr := range c.arrays {
			buf.WriteByte(2)
			buf.WriteString(k)
			buf.WriteByte(0)
			buf.WriteByte(byte(len(arr)))
			for _, s := range arr {
				buf.WriteByte(0)
				buf.WriteString(s)
				buf.WriteByte(0)
			}
		}
		for _, sc := range c.classes {
			buf.WriteByte(0)
			buf.WriteString(sc.name)
			buf.WriteByte(0)
			pending = append(pending, fix{at: buf.Len(), c: sc.c})
			_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		}
	}
	write(root)
	for len(pending) > 0 {
		f := pending[0]
		pending = pending[1:]
		off := buf.Len()
		b := buf.Bytes()
		binary.LittleEndian.PutUint32(b[f.at:], uint32(off))
		write(f.c)
	}
	return buf.Bytes()
}

func sub(name string, c *rapClass) struct {
	name string
	c    *rapClass
} {
	return struct {
		name string
		c    *rapClass
	}{name, c}
}

// writePBO makes an archive with a product entry and the given plain files.
func writePBO(t *testing.T, path string, files map[string][]byte, order []string) {
	t.Helper()
	var hdr, data bytes.Buffer
	u32 := func(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.LittleEndian, v) }
	hdr.WriteByte(0)
	u32(&hdr, 0x56657273)
	for i := 0; i < 4; i++ {
		u32(&hdr, 0)
	}
	hdr.WriteString("prefix\x00mymod\\cars\x00\x00")
	for _, name := range order {
		hdr.WriteString(name)
		hdr.WriteByte(0)
		u32(&hdr, 0)
		u32(&hdr, uint32(len(files[name])))
		u32(&hdr, 0)
		u32(&hdr, 0)
		u32(&hdr, uint32(len(files[name])))
		data.Write(files[name])
	}
	hdr.WriteByte(0)
	for i := 0; i < 5; i++ {
		u32(&hdr, 0)
	}
	out := append(hdr.Bytes(), data.Bytes()...)
	out = append(out, 0)
	out = append(out, make([]byte, 20)...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPBOAndBinarizedConfig(t *testing.T) {
	car := &rapClass{parent: "CarScript", ints: map[string]int32{"scope": 0}, arrays: map[string][]string{"attachments": {"MyCarWheel_1_1", "CarBattery"}}}
	black := &rapClass{parent: "MyCar_Base", ints: map[string]int32{"scope": 2}, strs: map[string]string{"displayName": "My Car"}}
	vehicles := &rapClass{}
	vehicles.classes = append(vehicles.classes, sub("MyCar_Base", car), sub("MyCar_Black", black))
	root := &rapClass{}
	root.classes = append(root.classes, sub("CfgVehicles", vehicles))
	cfg := encodeRap(root)

	path := filepath.Join(t.TempDir(), "cars.pbo")
	writePBO(t, path, map[string][]byte{"readme.txt": []byte("hello"), "config.bin": cfg}, []string{"readme.txt", "config.bin"})
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Properties["prefix"] != `mymod\cars` || len(f.Entries) != 2 {
		t.Fatalf("header: %+v %v", f.Entries, f.Properties)
	}
	cfgs := f.Find("CONFIG.BIN")
	if len(cfgs) != 1 {
		t.Fatalf("find: %+v", cfgs)
	}
	data, err := f.Read(cfgs[0])
	if err != nil || !bytes.Equal(data, cfg) {
		t.Fatalf("read: %v", err)
	}
	tree, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	v := tree.Child("cfgvehicles")
	if v == nil || len(v.Classes) != 2 {
		t.Fatalf("CfgVehicles: %+v", v)
	}
	b := v.Child("MyCar_Black")
	if b.Parent != "MyCar_Base" {
		t.Fatalf("parent %q", b.Parent)
	}
	if s, _ := b.Number("scope"); s != 2 {
		t.Fatalf("scope %v", s)
	}
	if d, _ := b.String("displayname"); d != "My Car" {
		t.Fatalf("displayName %q", d)
	}
	if a := v.Child("MyCar_Base").Strings("attachments"); len(a) != 2 || a[1] != "CarBattery" {
		t.Fatalf("attachments %v", a)
	}
}

func TestTextConfig(t *testing.T) {
	src := `#include "x.hpp"
// comment
class CfgPatches { class MyCars { units[] = {}; requiredAddons[] = {"DZ_Vehicles_Wheeled"}; }; };
class CfgVehicles
{
	class CarScript;
	class MyCar_Base: CarScript
	{
		scope = 0;
		attachments[] = {"MyCarWheel_1_1", "MyCarWheel_1_2", "CarBattery"};
		/* block
		   comment */
	};
	class MyCar_Red: MyCar_Base { scope = 2; displayName = "Red ""car"""; };
	class MyCarWheel: CarWheel { scope = 2; inventorySlot[] = {"MyCarWheel_1_1","MyCarWheel_1_2"}; };
};`
	tree, err := ParseConfig([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	v := tree.Child("CfgVehicles")
	if v == nil {
		t.Fatal("no CfgVehicles")
	}
	red := v.Child("MyCar_Red")
	if red == nil || red.Parent != "MyCar_Base" {
		t.Fatalf("red: %+v", red)
	}
	if s, _ := red.Number("scope"); s != 2 {
		t.Fatalf("scope %v", s)
	}
	if d, _ := red.String("displayName"); d != `Red "car"` {
		t.Fatalf("displayName %q", d)
	}
	if a := v.Child("MyCar_Base").Strings("attachments"); len(a) != 3 {
		t.Fatalf("attachments %v", a)
	}
	if s := v.Child("MyCarWheel").Strings("inventorySlot"); len(s) != 2 {
		t.Fatalf("inventorySlot %v", s)
	}
}

func TestLZSS(t *testing.T) {
	// 'a' 'b' 'c' literals, then a back-reference: distance 3, length 6.
	out, err := lzss([]byte{0x07, 'a', 'b', 'c', 3, 3}, 9)
	if err != nil || string(out) != "abcabcabc" {
		t.Fatalf("lzss = %q, %v", out, err)
	}
	if _, err := lzss([]byte{0x00, 1}, 5); err == nil {
		t.Fatal("truncated input must fail")
	}
}
