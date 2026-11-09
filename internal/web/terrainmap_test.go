// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/pbo"
)

func writeWorldPBO(t *testing.T, path, wrp string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pbo.Write(path, [][2]string{{"prefix", "test\\world"}}, []pbo.WriteFile{
		{Name: wrp, Data: []byte("OPRW not a real world")},
		{Name: "config.bin", Data: []byte("x")},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorldToken(t *testing.T) {
	for in, want := range map[string]string{
		"dayzOffline.chernarusplus": "chernarusplus",
		"empty.DeerIsle":            "deerisle",
		"namalsk":                   "namalsk",
	} {
		if got := worldToken(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

// The world is found in the game's addons, in a map mod named after it, and
// (searching every archive) in a mod that names its archive otherwise; a DLC
// world that only comes as .ebo is reported, never opened.
func TestFindWorld(t *testing.T) {
	dir := t.TempDir()
	writeWorldPBO(t, filepath.Join(dir, "addons", "worlds_chernarusplus.pbo"), "ChernarusPlus.wrp")
	writeWorldPBO(t, filepath.Join(dir, "@DeerIsle", "addons", "deerisle_world.pbo"), "deerisle.wrp")
	writeWorldPBO(t, filepath.Join(dir, "@Banov", "Addons", "terrain_data.pbo"), "banov.wrp")
	if err := os.MkdirAll(filepath.Join(dir, "sakhal", "addons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sakhal", "addons", "worlds_sakhal.ebo"), []byte("encrypted"), 0o644); err != nil {
		t.Fatal(err)
	}

	if f := findWorld(dir, "chernarusplus", false); !f.Found || !filepathHasSuffix(f.Src.PBO, "worlds_chernarusplus.pbo") || f.Src.Entry != "ChernarusPlus.wrp" {
		t.Fatalf("chernarus: %+v", f)
	}
	if f := findWorld(dir, "deerisle", false); !f.Found {
		t.Fatalf("a map mod named after its world: %+v", f)
	}
	if f := findWorld(dir, "banov", false); f.Found {
		t.Fatal("the quick search only opens archives named after the world")
	}
	if f := findWorld(dir, "banov", true); !f.Found || !filepathHasSuffix(f.Src.PBO, "terrain_data.pbo") {
		t.Fatalf("the full search opens every mod archive: %+v", f)
	}
	if f := findWorld(dir, "sakhal", true); f.Found || !f.Encrypted {
		t.Fatalf("sakhal: %+v", f)
	}
	if f := findWorld(dir, "nowhere", true); f.Found || f.Encrypted {
		t.Fatalf("nowhere: %+v", f)
	}
}

func filepathHasSuffix(p, s string) bool { return filepath.Base(p) == s }

// The description of a drawn picture counts only while the picture is the
// drawn one: the admin's own upload (or removing it) clears it.
func TestTerrainMetaFollowsThePicture(t *testing.T) {
	h, _ := testHandlers(t)
	key := "chernarus"
	if err := os.MkdirAll(h.mapImageDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.mapImageDir(), key+".jpg"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.terrainMetaPath(key), []byte(`{"v":1,"world":15360,"places":[{"name":"Черногорск","latin":"Chernogorsk","type":"Capital","x":6778,"z":2321}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := h.readTerrainMeta(key)
	if m == nil || m.World != 15360 {
		t.Fatalf("meta: %+v", m)
	}
	pl := h.terrainPlaces(m, "en")
	if len(pl) != 1 || pl[0][0] != "Chernogorsk" || pl[0][1] != "Capital" {
		t.Fatalf("places: %v", pl)
	}
	if ru := h.terrainPlaces(m, "ru"); ru[0][0] != "Черногорск" {
		t.Fatalf("russian: %v", ru)
	}
	if err := h.storeMapImage(key, ".png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if h.readTerrainMeta(key) != nil || fileExists(h.terrainMetaPath(key)) {
		t.Fatal("an uploaded picture is the admin's, not the drawn one")
	}
}

// thirdPersonView (DayZ 1.30) replaces disable3rdPerson: writing it removes
// the old key so the two can never disagree; the server program tells
// whether it reads the new key.
func TestThirdPersonViewReplacesTheOldKey(t *testing.T) {
	h, dir := testHandlers(t)
	cfgPath := filepath.Join(dir, "serverDZ.cfg")
	put(t, cfgPath, "hostname = \"x\";\ndisable3rdPerson = 0;\n")
	req := httptest.NewRequest(http.MethodPost, "/api/servercfg", strings.NewReader(`{"thirdPersonView": 2}`))
	rec := httptest.NewRecorder()
	h.serverCfg(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(got), "disable3rdPerson") || !strings.Contains(string(got), "thirdPersonView = 2;") {
		t.Fatalf("serverDZ.cfg:\n%s", got)
	}

	if serverKnows(dir, "thirdPersonView") {
		t.Fatal("no server program, nothing known")
	}
	put(t, filepath.Join(dir, "DayZServer_x64.exe"), "MZ...thirdPersonView...disable3rdPerson")
	if !serverKnows(dir, "thirdPersonView") || serverKnows(dir, "someFutureKey") {
		t.Fatal("serverKnows must read the program")
	}
}

// The mission card's picture is the map picture made small (and kept).
func TestMapThumb(t *testing.T) {
	h, _ := testHandlers(t)
	if err := os.MkdirAll(h.mapImageDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	big := image.NewRGBA(image.Rect(0, 0, 1200, 1200))
	for i := range big.Pix {
		big.Pix[i] = 200
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, big, nil); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(h.mapImageDir(), "chernarus.jpg")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	p := h.mapThumbFile("chernarus", src)
	if p == "" {
		t.Fatal("no thumbnail")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil || cfg.Width != thumbPx || cfg.Height != thumbPx {
		t.Fatalf("thumbnail %dx%d (%v)", cfg.Width, cfg.Height, err)
	}
	if again := h.mapThumbFile("chernarus", src); again != p {
		t.Fatal("the thumbnail is made once")
	}
}
