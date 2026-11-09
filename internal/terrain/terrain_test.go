// Copyright (c) 2026 Aristarh Ucolov.

package terrain

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLZO1X(t *testing.T) {
	// Literals only: 17+5 announces five bytes, then the end marker.
	got, used, err := lzo1x([]byte{22, 'h', 'e', 'l', 'l', 'o', 0x11, 0, 0}, 5)
	if err != nil || string(got) != "hello" || used != 9 {
		t.Fatalf("literals: %q %d %v", got, used, err)
	}
	// "abcabcabcabc": three literals, then a match 3 back for 9 bytes
	// (M3: 32|7 → length 7+2, distance 1+(8>>2)=3), then the end.
	got, _, err = lzo1x([]byte{20, 'a', 'b', 'c', 32 | 7, 8, 0, 0x11, 0, 0}, 12)
	if err != nil || string(got) != "abcabcabcabc" {
		t.Fatalf("match: %q %v", got, err)
	}
	// Truncated and wrong-size streams fail instead of panicking.
	for _, bad := range [][]byte{{22, 'h', 'e'}, {32 | 7, 8, 0}, {0}, {}} {
		if _, _, err := lzo1x(bad, 5); err == nil {
			t.Fatalf("%v: expected an error", bad)
		}
	}
	if _, _, err := lzo1x([]byte{22, 'h', 'e', 'l', 'l', 'o', 0x11, 0, 0}, 4); err == nil {
		t.Fatal("more data than asked for must fail")
	}
}

func TestWaterModelSize(t *testing.T) {
	for name, want := range map[string][3]float32{
		"pond_50x50.p3d":       {50, 50, 0},
		"lake_50x50.p3d":       {50, 50, 0},
		"enoch_50x50_moss.p3d": {50, 50, 0},
		"river_24_12.p3d":      {24, 12, 0},
		"stream_14_24_l30.p3d": {14, 24, 0},
		"pond_small_06.p3d":    {20, 20, 0},
		"enoch_river_17.p3d":   {50, 50, 1},
		"enoch_wolisko_4.p3d":  {50, 50, 1},
	} {
		a, b, baked, ok := waterModelSize(name)
		if !ok || a != want[0] || b != want[1] || baked != (want[2] == 1) {
			t.Errorf("%s: %v %v baked=%v %v", name, a, b, baked, ok)
		}
	}
	for _, name := range []string{"streambed_leaf_long_straight.p3d", "stones_sand_group_3.p3d", "water_station.p3d"} {
		if _, _, _, ok := waterModelSize(name); ok {
			t.Errorf("%s is not water", name)
		}
	}
}

func TestLatinName(t *testing.T) {
	for in, want := range map[string]string{
		"Settlement_Chernogorsk":    "Chernogorsk",
		"Settlement_NovayaPetrovka": "Novaya Petrovka",
		"Hill_Zelenayagora":         "Zelenayagora",
		"Branzow_castle":            "Branzow castle",
		"Marine_Tikhiyzaliv":        "Tikhiyzaliv",
	} {
		if got := latinName(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	p := Place{Name: "Черногорск", Latin: "Chernogorsk"}
	if p.Label("ru", nil) != "Черногорск" || p.Label("en", nil) != "Chernogorsk" {
		t.Fatal("Cyrillic names show in Russian only")
	}
	st := StringTable{"str_cfgworlds_enoch_branzowcastle": {"english": "Branzow Castle", "german": "Burg Branzow"}}
	q := Place{Name: "$STR_CfgWorlds_Enoch_BranzowCastle", Latin: "Branzow castle"}
	if q.Label("de", st) != "Burg Branzow" || q.Label("ko", st) != "Branzow Castle" || q.Label("en", nil) != "Branzow castle" {
		t.Fatal("stringtable names")
	}
}

// TestRenderWorld renders a real world when DZM_TERRAIN_PBO names a world
// PBO (e.g. a DayZ server's addons/worlds_chernarusplus.pbo); the picture
// goes to DZM_TERRAIN_OUT.
func TestRenderWorld(t *testing.T) {
	pboPath := os.Getenv("DZM_TERRAIN_PBO")
	if pboPath == "" {
		t.Skip("set DZM_TERRAIN_PBO to a world PBO")
	}
	src, err := Locate(pboPath, "")
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	w, err := Read(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read in %v: %.0f m, grid %d, models %d, water %d, roads %d/%d/%d, buildings %d/%d/%d/%d walls %d, rail %d, power %d, rocks %d",
		time.Since(t0), w.Size, w.GridN, w.Models, len(w.Water),
		len(w.Roads[RoadMain]), len(w.Roads[RoadPaved]), len(w.Roads[RoadTrack]),
		len(w.Buildings[BuildHouse]), len(w.Buildings[BuildIndustry]), len(w.Buildings[BuildChurch]), len(w.Buildings[BuildLandmark]),
		len(w.Buildings[BuildWall]), len(w.Rail), len(w.Power), len(w.Rocks))
	places, err := ReadPlaces(pboPath, "")
	t.Logf("places: %d (%v)", len(places), err)
	t1 := time.Now()
	size := RenderSize(w.Size)
	img := w.Render(size)
	t.Logf("rendered %d px in %v", size, time.Since(t1))
	out := os.Getenv("DZM_TERRAIN_OUT")
	if out == "" {
		return
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		t.Fatal(err)
	}
	t.Logf("jpeg %d bytes", buf.Len())
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Close-ups for checking the details (800 px, real size).
	for i, c := range [][2]int{{size * 40 / 100, size * 82 / 100}, {size * 64 / 100, size * 84 / 100}, {size * 22 / 100, size * 38 / 100}} {
		r := image.Rect(c[0], c[1], c[0]+800, c[1]+800).Intersect(img.Rect)
		var cb bytes.Buffer
		if err := jpeg.Encode(&cb, img.SubImage(r), &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(out, filepath.Ext(out)) + fmt.Sprintf("-crop%d.jpg", i)
		if err := os.WriteFile(name, cb.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHalveGrid(t *testing.T) {
	in := []float32{
		1, 3, 10, 10,
		1, 3, 10, 10,
		0, 0, -4, 4,
		0, 0, 4, -4,
	}
	got := halveGrid(in, 4)
	want := []float32{2, 10, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v, want %v", got, want)
		}
	}
}
