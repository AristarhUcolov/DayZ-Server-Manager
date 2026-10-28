// Copyright (c) 2026 Aristarh Ucolov.
package pbo

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "addons", "test.pbo")
	cfg := []byte("class CfgPatches { class T { units[] = {}; requiredAddons[] = {\"DZ_Data\"}; }; };\n")
	script := []byte("modded class CarScript {}\n")
	err := Write(path, [][2]string{{"prefix", "T_Mod"}}, []WriteFile{
		{Name: "config.cpp", Data: cfg},
		{Name: "Scripts/4_World/t.c", Data: script},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Properties["prefix"] != "T_Mod" {
		t.Errorf("prefix %q", f.Properties["prefix"])
	}
	if len(f.Entries) != 2 || f.Entries[1].Name != `Scripts\4_World\t.c` {
		t.Fatalf("entries %+v", f.Entries)
	}
	for i, want := range [][]byte{cfg, script} {
		got, err := f.Read(f.Entries[i])
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("entry %d: %q, %v", i, got, err)
		}
	}
	if c, err := ParseConfig(cfg); err != nil || c.Child("CfgPatches") == nil {
		t.Errorf("config does not parse: %v", err)
	}
	// The trailer: a zero byte and the SHA-1 of everything before it.
	data, _ := os.ReadFile(path)
	body, tail := data[:len(data)-21], data[len(data)-21:]
	sum := sha1.Sum(body)
	if tail[0] != 0 || !bytes.Equal(tail[1:], sum[:]) {
		t.Error("bad SHA-1 trailer")
	}
}
