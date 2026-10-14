// Copyright (c) 2026 Aristarh Ucolov.
//
// Real map layers, generated from the server's OWN mission files — no bundled
// or downloaded imagery (Bohemia's rules forbid redistributing game files, and
// every DayZ map image online is exactly that). Every mission ships:
//
//   - areaflags.map: the Central Economy's area grid (header: grid w/h, world
//     w/h in metres, usage bits; then a 32-bit usage mask per cell and a value
//     (tier) mask per cell, south row first). Non-zero cells are land, so it
//     doubles as an accurate land/sea mask, and the value bits ARE the loot
//     tiers (Tier1..Tier4, Unique — in cfglimitsdefinition.xml order).
//   - mapgrouppos.xml: the position of every lootable building — towns,
//     villages and bases appear exactly where they are.
//   - env/*_territories.xml: infected and animal zones (x, z, r).
//   - cfgeffectarea.json: contaminated zones.
//
// Together they give an accurate map for ANY world, modded ones included, and
// areaflags' header gives the real world size, so points line up without the
// admin typing a size in.
package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type areaFlagsHeader struct {
	W, H      int     // grid cells
	WorldW    float64 // metres
	WorldH    float64
	UsageBits int
	valueBits int // derived from the file size
	offValue  int64
}

// readAreaFlagsHeader parses the 24-byte header and derives the value-layer
// layout from the file size (Chernarus/Sakhal store 8 bits per cell, Livonia
// packs 4 — the size tells which).
func readAreaFlagsHeader(path string) (*areaFlagsHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var raw [6]uint32
	if err := binary.Read(f, binary.LittleEndian, &raw); err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	h := &areaFlagsHeader{W: int(raw[0]), H: int(raw[1]), WorldW: float64(raw[2]), WorldH: float64(raw[3]), UsageBits: int(raw[4])}
	if h.W <= 0 || h.H <= 0 || h.W > 16384 || h.H > 16384 || h.WorldW < 500 || h.WorldW > 100000 || h.UsageBits <= 0 || h.UsageBits%8 != 0 {
		return nil, errors.New("not an areaflags.map")
	}
	n := int64(h.W) * int64(h.H)
	h.offValue = 24 + n*int64(h.UsageBits/8)
	rest := st.Size() - h.offValue
	switch {
	case rest == n:
		h.valueBits = 8
	case rest == n/2:
		h.valueBits = 4
	default:
		return nil, fmt.Errorf("unexpected areaflags.map layout (%d value bytes for %d cells)", rest, n)
	}
	return h, nil
}

// tierColors by value-flag name. Unknown names fall back to grey.
var tierColors = map[string]color.NRGBA{
	"tier1":  {0x4c, 0xaf, 0x50, 0xb0},
	"tier2":  {0xe6, 0xc2, 0x29, 0xb8},
	"tier3":  {0xf0, 0x8a, 0x24, 0xc0},
	"tier4":  {0xe0, 0x3a, 0x3a, 0xc8},
	"unique": {0x9c, 0x4d, 0xcc, 0xc8},
}

var landColor = color.NRGBA{0xd8, 0xd0, 0xb4, 0xff}

// renderAreaFlags draws the land mask ("land") or loot tiers ("tiers") at
// out×out pixels, north up. valueNames maps value-bit index → flag name.
func renderAreaFlags(path, mode string, out int, valueNames []string) (image.Image, error) {
	hd, err := readAreaFlagsHeader(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img := image.NewNRGBA(image.Rect(0, 0, out, out))
	ub := hd.UsageBits / 8
	usageRow := make([]byte, hd.W*ub)
	valRowLen := hd.W
	if hd.valueBits == 4 {
		valRowLen = (hd.W + 1) / 2
	}
	valRow := make([]byte, valRowLen)
	// Precompute the colour per value mask: the highest set tier wins.
	palette := make([]color.NRGBA, 256)
	for m := 1; m < 256; m++ {
		best := color.NRGBA{0x9e, 0x9e, 0x9e, 0x90}
		for bit := 7; bit >= 0; bit-- {
			if m&(1<<uint(bit)) == 0 || bit >= len(valueNames) {
				continue
			}
			if c, ok := tierColors[strings.ToLower(valueNames[bit])]; ok {
				best = c
				break
			}
		}
		palette[m] = best
	}
	for oy := 0; oy < out; oy++ {
		// Row 0 of the file is the south edge; row 0 of the image is north.
		gy := (out - 1 - oy) * hd.H / out
		if mode == "land" {
			if _, err := f.ReadAt(usageRow, 24+int64(gy)*int64(hd.W*ub)); err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
		}
		var vOff int64
		if hd.valueBits == 8 {
			vOff = hd.offValue + int64(gy)*int64(hd.W)
		} else {
			vOff = hd.offValue + int64(gy)*int64(hd.W)/2
		}
		if _, err := f.ReadAt(valRow, vOff); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for ox := 0; ox < out; ox++ {
			gx := ox * hd.W / out
			var v int
			if hd.valueBits == 8 {
				v = int(valRow[gx])
			} else {
				b := valRow[gx/2]
				if gx%2 == 1 {
					v = int(b >> 4)
				} else {
					v = int(b & 0x0f)
				}
			}
			switch mode {
			case "land":
				used := v != 0
				if !used {
					for k := 0; k < ub; k++ {
						if usageRow[gx*ub+k] != 0 {
							used = true
							break
						}
					}
				}
				if used {
					img.SetNRGBA(ox, oy, landColor)
				}
			default:
				if v != 0 {
					img.SetNRGBA(ox, oy, palette[v&0xff])
				}
			}
		}
	}
	return img, nil
}

// missionValueNames returns the value-flag names in cfglimitsdefinition.xml
// order — the bit order areaflags.map uses.
func missionValueNames(missionDir string) []string {
	data, err := os.ReadFile(filepath.Join(missionDir, "cfglimitsdefinition.xml"))
	if err != nil {
		return []string{"Tier1", "Tier2", "Tier3", "Tier4", "Unique"}
	}
	var doc struct {
		Values []struct {
			Name string `xml:"name,attr"`
		} `xml:"valueflags>value"`
	}
	if xml.Unmarshal(data, &doc) != nil || len(doc.Values) == 0 {
		return []string{"Tier1", "Tier2", "Tier3", "Tier4", "Unique"}
	}
	out := make([]string, len(doc.Values))
	for i, v := range doc.Values {
		out[i] = v.Name
	}
	return out
}

func (h *handlers) activeMissionDir() string {
	mission, err := h.missionTemplate()
	if err != nil || mission == "" {
		return ""
	}
	return filepathJoinMission(h.app.ServerDir, mission)
}

func filepathJoinMission(serverDir, mission string) string {
	return filepath.Join(serverDir, "mpmissions", mission)
}

// areaFlagsWorldSize returns the world edge from the active mission's
// areaflags.map header (0 when unavailable).
func (h *handlers) areaFlagsWorldSize() float64 {
	md := h.activeMissionDir()
	if md == "" {
		return 0
	}
	hd, err := readAreaFlagsHeader(filepath.Join(md, "areaflags.map"))
	if err != nil {
		return 0
	}
	return hd.WorldW
}

// ---------------------------------------------------------------------------

var layerMu sync.Mutex // one render at a time; renders are cached on disk

// mapLayersInfo lists which real-data layers the active mission provides.
func (h *handlers) mapLayersInfo(w http.ResponseWriter, r *http.Request) {
	md := h.activeMissionDir()
	out := map[string]interface{}{"map": h.currentMap()}
	if md == "" {
		writeJSON(w, out)
		return
	}
	has := func(rel string) (bool, int64) {
		st, err := os.Stat(filepath.Join(md, rel))
		if err != nil {
			return false, 0
		}
		return true, st.ModTime().Unix()
	}
	af, afV := has("areaflags.map")
	if af {
		if _, err := readAreaFlagsHeader(filepath.Join(md, "areaflags.map")); err != nil {
			af = false
		}
	}
	mg, mgV := has("mapgrouppos.xml")
	env, _ := has(filepath.Join("env", "zombie_territories.xml"))
	eff, _ := has("cfgeffectarea.json")
	out["layers"] = map[string]bool{"land": af, "tiers": af, "buildings": mg, "infected": env, "wildlife": env, "toxic": eff}
	out["ver"] = map[string]int64{"areaflags": afV, "buildings": mgV}
	out["tierNames"] = missionValueNames(md)
	writeJSON(w, out)
}

// mapLayerImage serves the land-mask or loot-tier PNG for the active mission,
// rendering it once and caching it next to the other map images.
func (h *handlers) mapLayerImage(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("layer")
	if mode != "land" && mode != "tiers" {
		http.Error(w, "unknown layer", http.StatusBadRequest)
		return
	}
	md := h.activeMissionDir()
	src := filepath.Join(md, "areaflags.map")
	st, err := os.Stat(src)
	if md == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	mission, _ := h.missionTemplate()
	const size = 2048
	cache := filepath.Join(h.mapImageDir(), "cache",
		fmt.Sprintf("%s-%s-%d-%d.png", sanitizeKey(mission), mode, size, st.ModTime().Unix()))
	layerMu.Lock()
	if _, err := os.Stat(cache); err != nil {
		img, rerr := renderAreaFlags(src, mode, size, missionValueNames(md))
		if rerr != nil {
			layerMu.Unlock()
			http.Error(w, rerr.Error(), http.StatusUnprocessableEntity)
			return
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err == nil {
			_ = os.MkdirAll(filepath.Dir(cache), 0o755)
			// Drop stale renders of this mission/layer first.
			if old, _ := filepath.Glob(filepath.Join(filepath.Dir(cache), sanitizeKey(mission)+"-"+mode+"-*.png")); old != nil {
				for _, o := range old {
					_ = os.Remove(o)
				}
			}
			_ = writeFileAtomic(cache, buf.Bytes())
		}
	}
	layerMu.Unlock()
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, cache)
}

var reGroupPos = regexp.MustCompile(`<group\s+name="([^"]*)"\s+pos="([-\d.]+)\s+[-\d.]+\s+([-\d.]+)"`)

// buildingKind buckets a building class for colouring.
func buildingKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "mil_") || strings.Contains(n, "military") || strings.Contains(n, "barracks") || strings.Contains(n, "guardhouse") || strings.Contains(n, "tisy"):
		return "mil"
	case strings.Contains(n, "ind_") || strings.Contains(n, "factory") || strings.Contains(n, "power") || strings.Contains(n, "tank") || strings.Contains(n, "misc_silo") || strings.Contains(n, "garage"):
		return "ind"
	case strings.Contains(n, "wreck") || strings.Contains(n, "misc_"):
		return "misc"
	}
	return "house"
}

// mapBuildings returns every lootable building position of the active mission
// as flat [x, z, x, z…] integer lists per kind (compact: ~11k buildings).
func (h *handlers) mapBuildings(w http.ResponseWriter, r *http.Request) {
	md := h.activeMissionDir()
	data, err := os.ReadFile(filepath.Join(md, "mapgrouppos.xml"))
	if md == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	out := map[string][]int{"house": {}, "mil": {}, "ind": {}, "misc": {}}
	for _, m := range reGroupPos.FindAllSubmatch(data, -1) {
		x, e1 := strconv.ParseFloat(string(m[2]), 64)
		z, e2 := strconv.ParseFloat(string(m[3]), 64)
		if e1 != nil || e2 != nil {
			continue
		}
		k := buildingKind(string(m[1]))
		out[k] = append(out[k], int(x+0.5), int(z+0.5))
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, out)
}

var reZone = regexp.MustCompile(`<zone\b[^>]*\bx="([-\d.]+)"[^>]*\bz="([-\d.]+)"[^>]*\br="([-\d.]+)"`)

// mapZones returns infected and wildlife territories and contaminated areas.
func (h *handlers) mapZones(w http.ResponseWriter, r *http.Request) {
	md := h.activeMissionDir()
	if md == "" {
		writeJSON(w, map[string]interface{}{})
		return
	}
	type zone struct {
		X    float64 `json:"x"`
		Z    float64 `json:"z"`
		R    float64 `json:"r"`
		Kind string  `json:"k"`
	}
	infected, wildlife, toxic := []zone{}, []zone{}, []zone{}
	files, _ := filepath.Glob(filepath.Join(md, "env", "*_territories.xml"))
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		kind := strings.TrimSuffix(filepath.Base(p), "_territories.xml")
		for _, m := range reZone.FindAllSubmatch(data, -1) {
			x, _ := strconv.ParseFloat(string(m[1]), 64)
			z, _ := strconv.ParseFloat(string(m[2]), 64)
			rr, _ := strconv.ParseFloat(string(m[3]), 64)
			zn := zone{X: x, Z: z, R: rr, Kind: kind}
			if kind == "zombie" {
				infected = append(infected, zn)
			} else {
				wildlife = append(wildlife, zn)
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(md, "cfgeffectarea.json")); err == nil {
		var doc struct {
			Areas []struct {
				AreaName string `json:"AreaName"`
				Data     struct {
					Pos    []float64 `json:"Pos"`
					Radius float64   `json:"Radius"`
				} `json:"Data"`
			} `json:"Areas"`
		}
		if json.Unmarshal(data, &doc) == nil {
			for _, a := range doc.Areas {
				if len(a.Data.Pos) >= 3 && a.Data.Radius > 0 {
					toxic = append(toxic, zone{X: a.Data.Pos[0], Z: a.Data.Pos[2], R: a.Data.Radius, Kind: a.AreaName})
				}
			}
		}
	}
	writeJSON(w, map[string]interface{}{"infected": infected, "wildlife": wildlife, "toxic": toxic})
}
