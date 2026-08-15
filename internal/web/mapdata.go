// Copyright (c) 2026 Aristarh Ucolov.
//
// Map data endpoints: which world the active mission runs, and aggregated
// death/kill positions for the heatmap. Positions come from the .ADM log the
// player database already ingests, so nothing new needs enabling on the server.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/players"
)

type mapInfo struct {
	Key      string  `json:"key"`                // "chernarus" | "livonia" | "sakhal" | <world name>
	Name     string  `json:"name"`               // display name
	Size     float64 `json:"size"`               // world edge length in metres (maps are square)
	Official bool    `json:"official"`           // one of the three shipped worlds (size is known)
	HasImage bool    `json:"hasImage"`           // an admin-supplied background image exists
	ImageVer int64   `json:"imageVer,omitempty"` // its mtime, used to bust the browser cache
}

// sanitizeKey keeps a world key filesystem- and URL-safe (it names the uploaded
// image file and the size override).
func sanitizeKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if len(k) > 40 {
		k = k[:40]
	}
	if k == "" {
		k = "unknown"
	}
	return k
}

// defaultWorldSize is the fallback square edge (metres) for a world whose real
// size we don't know — the most common DayZ terrain size. The admin can override
// per map (config.MapSizes) so points and the grid line up.
const defaultWorldSize = 15360

// worldDef is one recognised world in the registry below.
type worldDef struct {
	Key      string   // stable slug (names the uploaded image + size override)
	Name     string   // display name
	Size     float64  // square terrain edge in metres; 0 → defaultWorldSize
	Official bool     // a Bohemia-shipped world (size is authoritative)
	Aliases  []string // normalised (lowercase-alphanumeric) substrings to match in the template
}

// worldRegistry recognises official and popular community worlds from the mission
// template name (e.g. "dayzOffline.chernarusplus", "regular.deerisle"). It's
// matched fuzzily (see worldFromTemplate) so a modded map installed and selected
// on the server is recognised automatically. Anything not listed still gets its
// own slot via the fallback in worldFromTemplate — so every world is supported,
// listed or not.
//
// Sizes: the three official worlds are authoritative (Sakhal is 15360 — an
// earlier 16384 here was wrong and mis-scaled points). Community sizes come from
// a map-developer's compiled size list that
// matched every value we could independently verify (Chernarus 15360, Livonia
// 12800, Deer Isle 16384, Chiemsee 10240, Rostow 14336, Takistan 12800) and are
// all valid terrain-grid sizes (multiples of 1024). Any that's still off is
// admin-overridable per map in the UI. Worlds with no size fall back to the
// default and can be overridden too. This list covers the real, identifiable
// community maps; dev builds, WIP and one-off terrains aren't listed but still
// work via the fallback (their own slot, default size). Add more worlds here as
// needed — one line each. Order matters: the first alias hit wins, so official
// worlds come first, and aliases are distinctive tokens (never short fragments)
// to avoid matching a different map by accident.
var worldRegistry = []worldDef{
	{Key: "chernarus", Name: "Chernarus", Size: 15360, Official: true, Aliases: []string{"chernarusplus", "chernarus"}},
	{Key: "livonia", Name: "Livonia", Size: 12800, Official: true, Aliases: []string{"livonia", "enoch"}},
	{Key: "sakhal", Name: "Sakhal", Size: 15360, Official: true, Aliases: []string{"sakhal"}},
	// Community worlds (alphabetical). Sizes from the verified developer list.
	{Key: "alteria", Name: "Alteria", Size: 8192, Aliases: []string{"alteria"}},
	{Key: "anastara", Name: "Anastara", Size: 10240, Aliases: []string{"anastara"}},
	{Key: "antoria", Name: "Antoria", Aliases: []string{"antoria"}},
	{Key: "arcadia", Name: "Arcadia", Aliases: []string{"arcadia"}},
	{Key: "arsteinen", Name: "Arsteinen", Size: 15360, Aliases: []string{"arsteinen"}},
	{Key: "avalon", Name: "Avalon", Aliases: []string{"avalon"}},
	{Key: "azalea", Name: "Azalea", Aliases: []string{"azalea"}},
	{Key: "banov", Name: "Banov", Size: 15360, Aliases: []string{"banov"}},
	{Key: "barrington", Name: "Barrington", Size: 10240, Aliases: []string{"barrington"}},
	{Key: "bearisland", Name: "Bear Island", Size: 10240, Aliases: []string{"bearisland"}},
	{Key: "bitterroot", Name: "Bitterroot", Size: 12288, Aliases: []string{"bitterroot"}},
	{Key: "chiemsee", Name: "Chiemsee", Size: 10240, Aliases: []string{"chiemsee"}},
	{Key: "deadfall", Name: "Deadfall", Size: 10240, Aliases: []string{"deadfall"}},
	{Key: "deerisle", Name: "Deer Isle", Size: 16384, Aliases: []string{"deerisle"}},
	{Key: "doorcounty", Name: "Door County", Aliases: []string{"doorcounty"}},
	{Key: "esseker", Name: "Esseker", Size: 12800, Aliases: []string{"esseker"}},
	{Key: "evelone", Name: "Evelone", Aliases: []string{"evelone"}},
	{Key: "fogfall", Name: "FogFall", Size: 20480, Aliases: []string{"fogfall"}},
	{Key: "hashima", Name: "Hashima Islands", Size: 5120, Aliases: []string{"hashima"}},
	{Key: "iztek", Name: "Iztek", Size: 8192, Aliases: []string{"iztek"}},
	{Key: "japan", Name: "Japan", Aliases: []string{"japan"}},
	{Key: "malvinas", Name: "Malvinas", Aliases: []string{"malvinas"}},
	{Key: "melkart", Name: "Melkart", Size: 20480, Aliases: []string{"melkart"}},
	{Key: "mensk", Name: "Mensk Island", Aliases: []string{"mensk"}},
	{Key: "metropolis", Name: "Metropolis", Aliases: []string{"metropolis"}},
	{Key: "mysteryisland", Name: "Mystery Island", Aliases: []string{"mysteryisland"}},
	{Key: "namalsk", Name: "Namalsk", Size: 12800, Aliases: []string{"namalsk"}},
	{Key: "newyork", Name: "New York", Aliases: []string{"newyork"}},
	{Key: "novikostok", Name: "Novikostok", Aliases: []string{"novikostok"}},
	{Key: "nyheim", Name: "Nyheim", Aliases: []string{"nyheim"}},
	{Key: "orsek", Name: "Orsek", Aliases: []string{"orsek"}},
	{Key: "pripyat", Name: "Pripyat", Size: 20480, Aliases: []string{"pripyat"}},
	{Key: "prisonisland", Name: "Prison Island", Aliases: []string{"prisonisland"}},
	{Key: "prominsk", Name: "Prominsk", Aliases: []string{"prominsk"}},
	{Key: "raman", Name: "Raman", Size: 32768, Aliases: []string{"raman"}},
	{Key: "rostow", Name: "Rostow", Size: 14336, Aliases: []string{"rostow", "rostov"}},
	{Key: "sahinkaya", Name: "Sahinkaya", Aliases: []string{"sahinkaya"}},
	{Key: "sahrani", Name: "Sahrani", Aliases: []string{"sahrani"}},
	{Key: "sangudo", Name: "Sangudo", Aliases: []string{"sangudo"}},
	{Key: "sarov", Name: "Sarov", Aliases: []string{"sarov"}},
	{Key: "saxonya", Name: "Saxonya", Aliases: []string{"saxonya"}},
	{Key: "scalasaig", Name: "Scalasaig", Aliases: []string{"scalasaig"}},
	{Key: "scotland", Name: "Scotland", Aliases: []string{"scotland"}},
	{Key: "siberia", Name: "Siberia", Aliases: []string{"siberia"}},
	{Key: "stuartisland", Name: "Stuart Island", Aliases: []string{"stuartisland"}},
	{Key: "swansisland", Name: "Swans Island", Size: 2048, Aliases: []string{"swansisland"}},
	{Key: "takistan", Name: "Takistan", Size: 12800, Aliases: []string{"takistan"}},
	{Key: "togenia", Name: "Togenia", Aliases: []string{"togenia"}},
	{Key: "valning", Name: "Valning", Size: 10240, Aliases: []string{"valning"}},
	{Key: "vela", Name: "Vela", Size: 10240, Aliases: []string{"vela"}},
	{Key: "yiprit", Name: "Yiprit", Size: 20480, Aliases: []string{"yiprit"}},
	{Key: "zaha", Name: "Zaha", Aliases: []string{"zaha"}},
}

// normWorld reduces a template to lowercase alphanumerics for fuzzy matching, so
// "dayzOffline.ChernarusPlus", "Chernarus+" and "chernarusplus" all compare equal.
func normWorld(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// worldFromTemplate resolves the DayZ world of the active mission from its
// template folder name. It first tries the registry (fuzzy alias match) and
// falls back to keying an unknown world by the token after the last dot, so it
// keeps its own uploaded image + size override.
func worldFromTemplate(template string) mapInfo {
	norm := normWorld(template)
	for _, wd := range worldRegistry {
		for _, a := range wd.Aliases {
			if strings.Contains(norm, a) {
				size := wd.Size
				if size == 0 {
					size = defaultWorldSize
				}
				return mapInfo{Key: wd.Key, Name: wd.Name, Size: size, Official: wd.Official}
			}
		}
	}
	// Unknown/custom world: key it by the world name (the part after the last
	// dot) so each map keeps its own uploaded image and size override.
	world := template
	if i := strings.LastIndex(template, "."); i >= 0 {
		world = template[i+1:]
	}
	key := sanitizeKey(world)
	name := world
	if strings.TrimSpace(name) == "" {
		name = key
	}
	return mapInfo{Key: key, Name: name, Size: defaultWorldSize}
}

func (h *handlers) currentMap() mapInfo {
	tpl, _ := h.missionTemplate()
	mi := worldFromTemplate(tpl)
	// Admin-set world-size override (for modded maps whose real size differs).
	if sz := h.app.Cfg().MapSizes[mi.Key]; sz > 0 {
		mi.Size = float64(sz)
	}
	if p := h.mapImageFile(mi.Key); p != "" {
		mi.HasImage = true
		if st, err := os.Stat(p); err == nil {
			mi.ImageVer = st.ModTime().Unix()
		}
	}
	return mi
}

// mapImageExts are the picture formats accepted for a custom map background.
var mapImageExts = []string{".webp", ".png", ".jpg", ".jpeg"}

func (h *handlers) mapImageDir() string {
	return filepath.Join(h.app.ManagerDir, "maps")
}

// mapImageFile returns the stored background image for a map key, or "" when
// none has been uploaded. Base() guards against a key with path separators.
func (h *handlers) mapImageFile(key string) string {
	key = filepath.Base(key)
	if key == "" || key == "." {
		return ""
	}
	for _, ext := range mapImageExts {
		p := filepath.Join(h.mapImageDir(), key+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (h *handlers) mapImageKey(r *http.Request) string {
	if k := strings.TrimSpace(r.URL.Query().Get("map")); k != "" {
		return filepath.Base(k)
	}
	return h.currentMap().Key
}

// mapImage serves (GET) or stores (POST) the admin-supplied background image for
// a map. The image is a top-down picture covering the whole world square; the
// interactive map stretches it to the world bounds so positions line up.
//
// The imagery is always admin-provided (uploaded or fetched from a URL the admin
// chooses) — the manager ships no third-party map art. See mapImageFetch.
func (h *handlers) mapImage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.mapImageUpload(w, r)
		return
	}
	p := h.mapImageFile(h.mapImageKey(r))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	// Long cache — the URL carries an mtime version, so a new upload busts it.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, p)
}

const maxMapImageBytes = 25 << 20 // 25 MB — a full-map export is a few MB

func (h *handlers) mapImageUpload(w http.ResponseWriter, r *http.Request) {
	key := h.mapImageKey(r)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", http.StatusBadRequest)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !contains(mapImageExts, ext) {
		http.Error(w, "image must be .webp, .png, .jpg or .jpeg", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMapImageBytes+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) > maxMapImageBytes {
		http.Error(w, "image too large (max 25 MB)", http.StatusRequestEntityTooLarge)
		return
	}
	if err := h.storeMapImage(key, ext, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "map": key})
}

// storeMapImage writes data as the background image for key, replacing any
// existing image (possibly under a different extension). Shared by the upload
// and URL-fetch paths.
func (h *handlers) storeMapImage(key, ext string, data []byte) error {
	if err := os.MkdirAll(h.mapImageDir(), 0o755); err != nil {
		return err
	}
	for _, e := range mapImageExts {
		_ = os.Remove(filepath.Join(h.mapImageDir(), key+e))
	}
	return os.WriteFile(filepath.Join(h.mapImageDir(), key+ext), data, 0o644)
}

// imageExtFor picks a stored extension from the HTTP content-type, the URL path,
// then a content sniff — returning "" when the bytes aren't a supported image.
func imageExtFor(contentType, urlPath string, data []byte) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	}
	if ext := strings.ToLower(path.Ext(urlPath)); contains(mapImageExts, ext) {
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	}
	switch sn := http.DetectContentType(data); {
	case strings.HasPrefix(sn, "image/webp"):
		return ".webp"
	case strings.HasPrefix(sn, "image/png"):
		return ".png"
	case strings.HasPrefix(sn, "image/jpeg"):
		return ".jpg"
	}
	return ""
}

// mapImageFetch downloads a map background from a URL the admin supplies and
// stores it in the per-map slot (same validation/limits as an upload). The fetch
// is server-side so the picture works offline afterwards and no third party is
// hit per viewer. The manager ships no imagery — the admin points it at a source
// they have the right to use.
func (h *handlers) mapImageFetch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Map string `json:"map"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key := sanitizeKey(req.Map)
	if key == "" || key == "unknown" {
		key = h.currentMap().Key
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "a valid http(s) image URL is required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	greq, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	greq.Header.Set("User-Agent", "DayZ-Server-Manager")
	resp, err := http.DefaultClient.Do(greq)
	if err != nil {
		http.Error(w, "fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("fetch failed: HTTP %d", resp.StatusCode), http.StatusBadGateway)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMapImageBytes+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if len(data) > maxMapImageBytes {
		http.Error(w, "image too large (max 25 MB)", http.StatusRequestEntityTooLarge)
		return
	}
	ext := imageExtFor(resp.Header.Get("Content-Type"), u.Path, data)
	if ext == "" {
		http.Error(w, "URL is not a supported image (.webp/.png/.jpg/.jpeg)", http.StatusUnsupportedMediaType)
		return
	}
	if err := h.storeMapImage(key, ext, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "map": key})
}

// mapSize sets the world-size override for a map key (metres), so a modded map
// with a different edge length plots points and grid correctly.
func (h *handlers) mapSize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Map  string `json:"map"`
		Size int    `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key := sanitizeKey(req.Map)
	if key == "" {
		http.Error(w, "map key required", http.StatusBadRequest)
		return
	}
	if req.Size < 1000 || req.Size > 65536 {
		http.Error(w, "size out of range (1000–65536)", http.StatusBadRequest)
		return
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		if c.MapSizes == nil {
			c.MapSizes = map[string]int{}
		}
		c.MapSizes[key] = req.Size
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "map": key, "size": req.Size})
}

// mapImageDelete removes a map's custom background image.
func (h *handlers) mapImageDelete(w http.ResponseWriter, r *http.Request) {
	key := h.mapImageKey(r)
	for _, e := range mapImageExts {
		_ = os.Remove(filepath.Join(h.mapImageDir(), key+e))
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

type heatPoint struct {
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	Kind string  `json:"kind"` // pvp | env | suicide
}

// mapHeat returns the map metadata and every death/kill position the killfeed
// still holds. The heatmap is a picture of recent activity (the killfeed is
// capped), which is exactly what "where do fights happen" wants.
func (h *handlers) mapHeat(w http.ResponseWriter, r *http.Request) {
	db := h.playersDB()
	db.Ingest(h.profilesAbs())
	_, kills := db.Snapshot(0)

	points := make([]heatPoint, 0, len(kills))
	var pvp, env, suicide int
	for _, k := range kills {
		if len(k.Pos) < 2 {
			continue
		}
		kind := k.Kind
		if kind == "" {
			if k.Suicide {
				kind = "suicide"
			} else if k.Killer != "" {
				kind = "pvp"
			} else {
				kind = "env"
			}
		}
		switch kind {
		case "pvp":
			pvp++
		case "suicide":
			suicide++
		default:
			env++
		}
		points = append(points, heatPoint{X: k.Pos[0], Z: k.Pos[1], Kind: kind})
	}

	writeJSON(w, map[string]interface{}{
		"map":    h.currentMap(),
		"points": points,
		"total":  len(points),
		"counts": map[string]int{"pvp": pvp, "env": env, "suicide": suicide},
	})
}

// liveWindow bounds how old a last-known position may be to still be plotted
// when we can't confirm the player is online (RCon down/unconfigured). Keeps the
// best-effort map from showing hours-old ghosts.
const liveWindow = 30 * time.Minute

// livePlot is a plotted position plus whether RCon currently confirms the player
// is online (vs a best-effort last-known point). Embeds LivePos so its fields
// (key/name/x/z/ageSec) flatten into the same JSON object.
type livePlot struct {
	players.LivePos
	Online bool `json:"online"`
}

// mapLive returns player positions for the live map. Positions come from the
// .ADM log, which vanilla DayZ writes only on connect, combat and chat — so a
// position can be stale (surfaced as its age). This is best-effort:
//   - When RCon answers, we know exactly who's online: online players are always
//     plotted (marked online), and recently-seen players (< liveWindow) are also
//     shown as last-known so the map isn't empty between their ADM lines.
//   - When RCon is down or unconfigured, we can't know who's online, so we plot
//     every recently-seen position (< liveWindow) as last-known. The frontend
//     surfaces this honestly instead of showing a blank map.
func (h *handlers) mapLive(w http.ResponseWriter, r *http.Request) {
	db := h.playersDB()
	db.Ingest(h.profilesAbs())

	running := h.app.ServerIsRunning()
	online := map[string]bool{}
	rconErr := ""
	rconOK := false
	if running && h.app.RCon.Configured() {
		list := h.app.RCon.PlayersFresh(10 * time.Second)
		if err := h.app.RCon.LastPlayersErr(); err != nil {
			rconErr = err.Error()
		} else {
			rconOK = true
		}
		for _, p := range list {
			online[strings.ToLower(p.Name)] = true
		}
	}

	windowSec := int(liveWindow.Seconds())
	plotted := []livePlot{}
	if running {
		for _, lp := range db.LivePositions() {
			isOnline := rconOK && online[strings.ToLower(lp.Name)]
			// Drop ancient last-known points; keep confirmed-online players at any age.
			if !isOnline && lp.AgeSec > windowSec {
				continue
			}
			plotted = append(plotted, livePlot{LivePos: lp, Online: isOnline})
		}
	}

	out := map[string]interface{}{
		"map":            h.currentMap(),
		"players":        plotted,
		"running":        running,
		"onlineCount":    len(online),
		"rconConfigured": h.app.RCon.Configured(),
	}
	if rconErr != "" {
		out["rconError"] = rconErr
	}
	writeJSON(w, out)
}
