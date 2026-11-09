// Copyright (c) 2026 Aristarh Ucolov.
//
// The map picture drawn from the server's own world (internal/terrain): the
// relief, water, forests, roads and buildings of the active mission's world,
// drawn once on this computer and kept as that world's map picture, with the
// world's place names on top. Nothing is downloaded and nothing leaves the
// machine — the files are the ones DayZ itself loads.
//
// The first map page opened draws it by itself when the world has no
// picture yet; the admin can draw it again (after a game update) or replace
// it with a picture of their own. Encrypted worlds (the DLC maps come as
// .ebo) are left alone: the mission's land and buildings layers stay.
package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/terrain"
	"dayzmanager/internal/util"
)

// terrainRenderVersion: bump when the drawing changes, so generated pictures
// are drawn again.
const terrainRenderVersion = 1

// terrainMeta is kept next to a generated picture (maps/<key>.terrain.json).
type terrainMeta struct {
	V       int             `json:"v"`
	PBO     string          `json:"pbo"`
	Entry   string          `json:"entry"`
	PBOSize int64           `json:"pboSize"`
	PBOTime int64           `json:"pboTime"`
	Pixels  int             `json:"pixels"`
	World   float64         `json:"world"` // edge in metres
	At      int64           `json:"at"`
	Places  []terrain.Place `json:"places,omitempty"`
	StrPBO  string          `json:"strPbo,omitempty"` // the game's stringtable
}

// terrainFound is where the active mission's world is.
type terrainFound struct {
	World     string
	Src       terrain.Source
	Found     bool
	Encrypted bool // only an .ebo holds it
	StrPBO    string
	at        time.Time
}

type terrainJob struct {
	State   string `json:"state"` // drawing | done | error
	Phase   string `json:"phase,omitempty"`
	Err     string `json:"error,omitempty"`
	Started int64  `json:"started"`
}

var terrains = struct {
	sync.Mutex
	jobs  map[string]*terrainJob
	found map[string]terrainFound
	deep  map[string]bool // a background search is running
	strs  map[string]terrain.StringTable
}{jobs: map[string]*terrainJob{}, found: map[string]terrainFound{}, deep: map[string]bool{}, strs: map[string]terrain.StringTable{}}

// One drawing at a time: it holds a few hundred MB while it works.
var terrainDrawMu sync.Mutex

func (h *handlers) terrainKey(mapKey string) string { return h.app.ServerDir + "|" + mapKey }

func (h *handlers) terrainMetaPath(key string) string {
	return filepath.Join(h.mapImageDir(), filepath.Base(key)+".terrain.json")
}

// terrainAutoPath marks that the picture was drawn by itself once: removing
// it later is the admin's choice and is not undone.
func (h *handlers) terrainAutoPath(key string) string {
	return filepath.Join(h.mapImageDir(), filepath.Base(key)+".terrain-auto")
}

func (h *handlers) readTerrainMeta(key string) *terrainMeta {
	data, err := os.ReadFile(h.terrainMetaPath(key))
	if err != nil {
		return nil
	}
	var m terrainMeta
	if json.Unmarshal(data, &m) != nil || m.V == 0 {
		return nil
	}
	// It describes the picture only while the picture is ours.
	if p := h.mapImageFile(key); p == "" || !strings.EqualFold(filepath.Ext(p), ".jpg") {
		return nil
	}
	return &m
}

// worldToken is the world name of a mission template ("dayzOffline.
// chernarusplus" → "chernarusplus").
func worldToken(template string) string {
	if i := strings.LastIndex(template, "."); i >= 0 {
		template = template[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(template))
}

// terrainFind looks for the active mission's world among the game's addons,
// the DLC folders and the mods (results kept ten minutes). Archives named
// after the world are checked at once; when none holds it, every other mod
// archive is searched in the background and the next map page has the
// answer — a page never waits for a long search.
func (h *handlers) terrainFind() terrainFound {
	tpl, err := h.missionTemplate()
	if err != nil {
		return terrainFound{}
	}
	world := worldToken(tpl)
	if world == "" {
		return terrainFound{}
	}
	ck := h.app.ServerDir + "|" + world
	terrains.Lock()
	if f, ok := terrains.found[ck]; ok && time.Since(f.at) < 10*time.Minute {
		terrains.Unlock()
		return f
	}
	terrains.Unlock()
	f := findWorld(h.app.ServerDir, world, false)
	f.at = time.Now()
	terrains.Lock()
	terrains.found[ck] = f
	deep := !f.Found && !f.Encrypted && !terrains.deep[ck]
	if deep {
		terrains.deep[ck] = true
	}
	terrains.Unlock()
	if deep {
		dir := h.app.ServerDir
		go util.SafeGo(h.app.Log, "world search", func() {
			d := findWorld(dir, world, true)
			d.at = time.Now()
			terrains.Lock()
			terrains.found[ck] = d
			delete(terrains.deep, ck)
			terrains.Unlock()
		})
	}
	return f
}

// findWorld searches serverDir/addons and every <folder>/addons of the
// server (DLC worlds, mods) for <world>.wrp: the archives named after the
// world, and with deep every other mod archive too.
func findWorld(serverDir, world string, deep bool) terrainFound {
	out := terrainFound{World: world}
	if p := filepath.Join(serverDir, "dta", "languagecore.pbo"); fileExists(p) {
		out.StrPBO = p
	}
	base := addonsDir(serverDir)
	var dirs []string
	if base != "" {
		dirs = append(dirs, base)
	}
	if ents, err := os.ReadDir(serverDir); err == nil {
		for _, e := range ents {
			if !e.IsDir() && e.Type()&(os.ModeSymlink|os.ModeIrregular) == 0 {
				continue
			}
			if d := addonsDir(filepath.Join(serverDir, e.Name())); d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	var named, others []string
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := strings.ToLower(e.Name())
			full := filepath.Join(d, e.Name())
			switch {
			case strings.HasSuffix(name, ".ebo"):
				if strings.Contains(name, world) {
					out.Encrypted = true
				}
			case strings.HasSuffix(name, ".pbo"):
				if strings.Contains(name, world) {
					named = append(named, full)
				} else if d != base {
					others = append(others, full)
				}
			}
		}
	}
	try := func(list []string, budget time.Duration) bool {
		start := time.Now()
		for _, p := range list {
			if time.Since(start) > budget {
				return false
			}
			if src, err := terrain.Locate(p, world); err == nil {
				out.Src, out.Found, out.Encrypted = src, true, false
				return true
			}
		}
		return false
	}
	if !try(named, 20*time.Second) && deep {
		try(others, 2*time.Minute)
	}
	return out
}

// addonsDir is dir/addons in whatever case it is written ("" if none).
func addonsDir(dir string) string {
	for _, n := range []string{"addons", "Addons", "ADDONS"} {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// terrainStatus is the state for the map pages: what can be drawn, what is
// being drawn, whether the picture is ours. It also starts the first drawing
// of a world that has no picture yet.
func (h *handlers) terrainStatus(lang string) map[string]interface{} {
	mi := h.currentMap()
	key := mi.Key
	out := map[string]interface{}{"state": "none"}
	if key == "" || key == "unknown" {
		return out
	}
	f := h.terrainFind()
	meta := h.readTerrainMeta(key)
	switch {
	case f.Found:
		out["state"] = "available"
	case f.Encrypted:
		out["state"] = "encrypted"
	}
	terrains.Lock()
	job := terrains.jobs[h.terrainKey(key)]
	var jc *terrainJob
	if job != nil {
		cp := *job
		jc = &cp
	}
	terrains.Unlock()
	if jc != nil && jc.State == "drawing" {
		out["state"] = "drawing"
		out["phase"] = jc.Phase
	} else if jc != nil && jc.State == "error" {
		out["error"] = jc.Err
	}
	if meta != nil {
		out["generated"] = true
		out["at"] = meta.At
		out["places"] = h.terrainPlaces(meta, lang)
	}
	// Drawn by itself: the first time a world without a picture is shown,
	// and again when the drawing improved or the game updated the world.
	if f.Found && (jc == nil || jc.State != "drawing") {
		auto := false
		switch {
		case meta == nil && h.mapImageFile(key) == "" && !fileExists(h.terrainAutoPath(key)):
			auto = true
		case meta != nil && (meta.V < terrainRenderVersion || sourceChanged(meta, f.Src)) && (jc == nil || jc.State != "error"):
			auto = true
		}
		if auto && h.startTerrainDraw(key, f, false) {
			out["state"] = "drawing"
			out["phase"] = "read"
		}
	}
	return out
}

func sourceChanged(m *terrainMeta, src terrain.Source) bool {
	st, err := os.Stat(src.PBO)
	if err != nil {
		return false
	}
	return !strings.EqualFold(m.PBO, src.PBO) || m.PBOSize != st.Size() || m.PBOTime != st.ModTime().Unix()
}

// terrainPlaces are the world's names for the panel language, compact:
// [label, type, x, z].
func (h *handlers) terrainPlaces(meta *terrainMeta, lang string) [][]interface{} {
	var st terrain.StringTable
	need := map[string]bool{}
	for _, p := range meta.Places {
		if strings.HasPrefix(p.Name, "$") {
			need[strings.ToLower(strings.TrimPrefix(p.Name, "$"))] = true
		}
	}
	if len(need) > 0 && meta.StrPBO != "" {
		terrains.Lock()
		st = terrains.strs[meta.StrPBO]
		terrains.Unlock()
		if st == nil {
			if t, err := terrain.ReadStringTable(meta.StrPBO, need); err == nil {
				st = t
				terrains.Lock()
				terrains.strs[meta.StrPBO] = t
				terrains.Unlock()
			}
		}
	}
	out := make([][]interface{}, 0, len(meta.Places))
	for _, p := range meta.Places {
		if p.Type == "ViewPoint" {
			continue // dozens of lookouts would bury the towns
		}
		out = append(out, []interface{}{p.Label(lang, st), p.Type, round1(p.X), round1(p.Z)})
	}
	return out
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// startTerrainDraw starts drawing key's picture in the background (false
// when one is already being drawn). manual: the admin asked, so it replaces
// any picture; a drawing that started by itself gives way to a picture the
// admin adds meanwhile.
func (h *handlers) startTerrainDraw(key string, f terrainFound, manual bool) bool {
	jk := h.terrainKey(key)
	terrains.Lock()
	if j := terrains.jobs[jk]; j != nil && j.State == "drawing" {
		terrains.Unlock()
		return false
	}
	job := &terrainJob{State: "drawing", Phase: "read", Started: time.Now().Unix()}
	terrains.jobs[jk] = job
	terrains.Unlock()
	_ = os.MkdirAll(h.mapImageDir(), 0o755)
	_ = os.WriteFile(h.terrainAutoPath(key), []byte(time.Now().Format(time.RFC3339)), 0o644)
	go util.SafeGo(h.app.Log, "map drawing", func() {
		err := h.drawTerrain(key, f, manual, func(phase string) {
			terrains.Lock()
			job.Phase = phase
			terrains.Unlock()
		})
		terrains.Lock()
		if err != nil {
			job.State, job.Err = "error", err.Error()
		} else {
			job.State = "done"
		}
		terrains.Unlock()
		if err != nil {
			h.app.Log.Printf("map: could not draw %s from %s: %v", key, f.Src.PBO, err)
		}
	})
	return true
}

// drawTerrain reads the world, draws it and stores the picture as key's map
// picture.
func (h *handlers) drawTerrain(key string, f terrainFound, manual bool, phase func(string)) error {
	terrainDrawMu.Lock()
	defer terrainDrawMu.Unlock()
	defer debug.FreeOSMemory() // hand the drawing's memory back at once
	start := time.Now()
	w, err := terrain.Read(f.Src)
	if err != nil {
		return err
	}
	places, _ := terrain.ReadPlaces(f.Src.PBO, f.World)
	phase("draw")
	px := terrain.RenderSize(w.Size)
	img := w.Render(px)
	phase("save")
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 86}); err != nil {
		return err
	}
	img = nil
	st, err := os.Stat(f.Src.PBO)
	if err != nil {
		return err
	}
	dir := h.mapImageDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// A picture the admin added meanwhile (it has no description of ours)
	// stays, unless they asked for this drawing.
	if !manual && h.mapImageFile(key) != "" && !fileExists(h.terrainMetaPath(key)) {
		return errors.New("a map picture was added meanwhile")
	}
	for _, e := range mapImageExts {
		if e != ".jpg" {
			_ = os.Remove(filepath.Join(dir, key+e))
		}
	}
	if err := writeFileAtomic(filepath.Join(dir, key+".jpg"), buf.Bytes()); err != nil {
		return err
	}
	meta := terrainMeta{V: terrainRenderVersion, PBO: f.Src.PBO, Entry: f.Src.Entry, PBOSize: st.Size(), PBOTime: st.ModTime().Unix(),
		Pixels: px, World: w.Size, At: time.Now().Unix(), Places: places, StrPBO: f.StrPBO}
	data, _ := json.Marshal(meta)
	if err := writeFileAtomic(h.terrainMetaPath(key), data); err != nil {
		return err
	}
	h.app.Log.Printf("map: drew %s from %s (%d px, %.0f m) in %v", key, filepath.Base(f.Src.PBO), px, w.Size, time.Since(start).Round(time.Second/10))
	return nil
}

// mapTerrain: GET the drawing's state (and the place names, ?lang=).
func (h *handlers) mapTerrain(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = h.app.Cfg().Language
	}
	writeJSON(w, h.terrainStatus(lang))
}

// mapTerrainDraw: POST to draw (again) the active world's picture from the
// server's files, replacing the current picture.
func (h *handlers) mapTerrainDraw(w http.ResponseWriter, r *http.Request) {
	mi := h.currentMap()
	if mi.Key == "" || mi.Key == "unknown" {
		http.Error(w, "no active mission", http.StatusBadRequest)
		return
	}
	terrains.Lock()
	for k := range terrains.found { // look again
		if strings.HasPrefix(k, h.app.ServerDir+"|") {
			delete(terrains.found, k)
		}
	}
	terrains.Unlock()
	f := h.terrainFind()
	if !f.Found {
		authError(w, http.StatusNotFound, map[bool]string{true: "map.terrain.encrypted", false: "map.terrain.notFound"}[f.Encrypted], nil)
		return
	}
	if !h.startTerrainDraw(mi.Key, f, true) {
		authError(w, http.StatusConflict, "map.terrain.busy", nil)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}
