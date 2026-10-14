// Copyright (c) 2026 Aristarh Ucolov.
package mods

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestMetaTimeDecodesDotNetBinary(t *testing.T) {
	// A real meta.cpp value (DateTime.ToBinary, UTC kind).
	got := MetaTime(5249743004478005813)
	want := time.Date(2022, 12, 3, 0, 0, 0, 0, time.UTC)
	if got.Year() != want.Year() || got.Month() != want.Month() || got.Day() != want.Day() {
		t.Fatalf("MetaTime = %v, want %v", got, want.Format("2006-01-02"))
	}
	if !MetaTime(0).IsZero() || !MetaTime(12345).IsZero() || !MetaTime(-1).IsZero() {
		t.Fatal("zero/implausible timestamps must decode to the zero time")
	}
	if MetaTime(1695822543).Year() != 2023 {
		t.Fatal("plain Unix seconds should still decode")
	}
}

// Regression: with real meta.cpp timestamps on both copies, List used to set
// WorkshopModifiedAt = time.Unix(<5e18>, 0) — a date JSON cannot encode — so
// /api/mods answered 200 with an empty body ("Unexpected end of JSON input").
func TestListWithRealMetaTimestampsEncodes(t *testing.T) {
	src, server := t.TempDir(), t.TempDir()
	meta := func(ts string) string {
		return "protocol = 1;\npublishedid = 1559212036;\nname = \"Community Framework\";\ntimestamp = " + ts + ";\n"
	}
	writeFile(t, filepath.Join(src, "@CF", "meta.cpp"), meta("5249743004478005813"))
	writeFile(t, filepath.Join(src, "@CF", "addons", "cf.pbo"), "x")
	writeFile(t, filepath.Join(server, "@CF", "meta.cpp"), meta("5249700000000000000"))
	writeFile(t, filepath.Join(server, "@CF", "addons", "cf.pbo"), "x")

	list, err := List(server, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(list); err != nil {
		t.Fatalf("mod list does not encode: %v", err)
	}
	if len(list) != 1 || !list[0].UpdateAvailable {
		t.Fatalf("newer Workshop publish not detected: %+v", list)
	}
	if list[0].WorkshopModifiedAt.Year() != 2022 {
		t.Fatalf("WorkshopModifiedAt = %v, want the decoded 2022 publish date", list[0].WorkshopModifiedAt)
	}
}
