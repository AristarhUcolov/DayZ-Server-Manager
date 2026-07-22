// Copyright (c) 2026 Aristarh Ucolov.
//
// Extra handlers v2: ADM log viewer, config zip backup (export/import),
// dashboard live metrics. Kept separate from handlers_ext.go to keep each file
// readable.
package web

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dztypes "dayzmanager/internal/types"

	"dayzmanager/internal/admlog"
	"dayzmanager/internal/i18n"
	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/notify"
	"dayzmanager/internal/util"
)

// ---------------------------------------------------------------------------
// ADM log viewer.

func (h *handlers) admlogRecent(w http.ResponseWriter, r *http.Request) {
	profilesDir := h.app.Cfg().ProfilesDir
	if !filepath.IsAbs(profilesDir) {
		profilesDir = filepath.Join(h.app.ServerDir, profilesDir)
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = admlog.Latest(profilesDir)
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		profAbs, _ := filepath.Abs(profilesDir)
		rel, err := filepath.Rel(profAbs, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "path must be inside profiles dir", http.StatusForbidden)
			return
		}
		path = abs
	}
	if path == "" {
		writeJSON(w, map[string]interface{}{"events": []admlog.Event{}, "path": ""})
		return
	}
	limit := 200
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	typeFilter := r.URL.Query().Get("type")
	playerFilter := r.URL.Query().Get("player")
	events, err := admlog.Recent(path, limit, typeFilter, playerFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"events": events,
		"path":   path,
		"count":  len(events),
	})
}

// ---------------------------------------------------------------------------
// Dashboard metrics.

// dashMods caches the dashboard's mod counts so the heavy mods.List walk runs
// at most once per window instead of on every 5s poll. The stored map is
// replaced wholesale (never mutated), so sharing it across requests is safe.
var dashMods struct {
	mu         sync.Mutex
	at         time.Time
	val        map[string]interface{}
	refreshing bool
}

func (h *handlers) dashboardMetrics(w http.ResponseWriter, r *http.Request) {
	out := map[string]interface{}{
		"running": h.app.ServerIsRunning(),
		"pid":     h.app.Server.PID(),
		"uptime":  h.app.Server.Uptime().Round(time.Second).String(),
		"port":    h.app.Cfg().ServerPort,
	}

	// Mods count. mods.List walks the whole mod tree (and !Workshop), which is
	// too heavy to run on every 5s poll — especially while installing mods. We
	// cache the counts for a short window; the dashboard only shows totals, not
	// sizes, so slightly stale numbers are fine.
	if h.app.Cfg().VanillaDayZPath != "" {
		dashMods.mu.Lock()
		stale := dashMods.val == nil || time.Since(dashMods.at) > 15*time.Second
		start := stale && !dashMods.refreshing
		if start {
			dashMods.refreshing = true
		}
		cur := dashMods.val
		dashMods.mu.Unlock()

		if start {
			// Refresh in the background: mods.List walks the whole mod tree and
			// !Workshop, which can take seconds on large modpacks. Running it in
			// the request stalled one dashboard poll every 15s; the refreshing
			// guard also collapses concurrent pollers into a single walk.
			// One snapshot for the whole pass: this runs on a 5s poll, and
			// re-taking it per loop would copy every slice each time.
			cfg := h.app.Cfg()
			serverDir, vanilla := h.app.ServerDir, cfg.VanillaDayZPath
			active := map[string]bool{}
			for _, name := range cfg.Mods {
				active[name] = true
			}
			for _, name := range cfg.ServerMods {
				active[name] = true
			}
			go func() {
				var v map[string]interface{}
				if list, err := mods.List(serverDir, vanilla); err == nil {
					installed, enabled := 0, 0
					for _, m := range list {
						if m.InstalledInServer {
							installed++
						}
						if active[m.Name] {
							enabled++
						}
					}
					v = map[string]interface{}{"total": len(list), "installed": installed, "active": enabled}
				}
				dashMods.mu.Lock()
				if v != nil {
					dashMods.val = v
				}
				dashMods.at = time.Now() // throttle retries to the window even on error
				dashMods.refreshing = false
				dashMods.mu.Unlock()
			}()
		}
		if cur != nil {
			out["mods"] = cur
		}
	}

	// Disk free on the drive holding the server dir.
	if free, err := util.DiskFree(h.app.ServerDir); err == nil {
		out["diskFreeBytes"] = free
	}

	// Process stats for DayZServer_x64.exe (Windows only; no-op stub elsewhere).
	if pid := h.app.Server.PID(); pid > 0 {
		if cpu, mem, err := util.ProcessStats(uint32(pid)); err == nil {
			cores := runtime.NumCPU()
			norm := cpu
			if cores > 0 {
				norm = cpu / float64(cores)
			}
			out["proc"] = map[string]interface{}{
				"cpuPercent":    norm,
				"cpuPercentRaw": cpu,
				"memBytes":      mem,
				"cores":         cores,
			}
		}
	}

	// Live player count — read from the shared RCon cache. PlayersFresh never
	// blocks: it returns the last known list and refreshes in the background at
	// most once per window, so the dashboard stays snappy and reuses the single
	// RCon connection instead of opening one per poll. (RCon is configured at
	// boot and on config change, not here.)
	if h.app.ServerIsRunning() {
		players := h.app.RCon.PlayersFresh(8 * time.Second)
		out["playerCount"] = len(players)
		out["players"] = players
	}

	// Log sources with sizes.
	out["logs"] = dzlogs.Discover(h.app.ServerDir, h.app.Cfg().ProfilesDir)

	// Recent ADM events (last 20) for the activity strip.
	profilesDir := h.app.Cfg().ProfilesDir
	if !filepath.IsAbs(profilesDir) {
		profilesDir = filepath.Join(h.app.ServerDir, profilesDir)
	}
	if admPath := admlog.Latest(profilesDir); admPath != "" {
		if events, err := admlog.Recent(admPath, 20, "", ""); err == nil {
			out["recentAdm"] = events
		}
	}

	writeJSON(w, out)
}

// ---------------------------------------------------------------------------
// Config zip backup.
//
// The backup is a straight zip of config files — no server binary, no mods.
// Restoring overwrites in-place; each file we touch gets a .bak via
// util.BackupBeforeWrite, so a bad import is reversible.

// backupItems returns a list of (absolutePath, archiveName) pairs to include
// in a backup. Missing files are silently skipped.
func (h *handlers) backupItems() [][2]string {
	root := h.app.ServerDir
	items := [][2]string{
		{filepath.Join(root, ".dayz-manager", "manager.json"), "manager.json"},
		{filepath.Join(root, "serverDZ.cfg"), "serverDZ.cfg"},
	}
	be := h.app.Cfg().BEPath
	if be == "" {
		be = "battleye"
	}
	if !filepath.IsAbs(be) {
		be = filepath.Join(root, be)
	}
	for _, name := range []string{"beserver_x64.cfg", "BEServer_x64.cfg", "beserver.cfg", "BEServer.cfg"} {
		items = append(items, [2]string{filepath.Join(be, name), "battleye/" + name})
	}
	// Mission files — resolve the current template from serverDZ.cfg. Fall
	// back to dayzOffline.chernarusplus so a fresh install with no cfg still
	// produces a useful backup.
	mission := ""
	if t, err := h.missionTemplate(); err == nil && t != "" {
		mission = filepath.Join(root, "mpmissions", t)
	}
	if mission == "" {
		mission = filepath.Join(root, "mpmissions", "dayzOffline.chernarusplus")
	}
	for _, rel := range []string{
		"init.c", "cfgeconomycore.xml", "cfgeventspawns.xml", "cfgspawnabletypes.xml",
		"cfggameplay.json", "cfgweather.xml", "cfgenvironment.xml",
		"cfgeventgroups.xml", "cfgignorelist.xml", "cfgrandompresets.xml",
		// The manager's own Auto-fix writes to these two, so a backup that
		// omitted them could not undo the panel's own edits.
		"cfglimitsdefinition.xml", "cfglimitsdefinitionuser.xml",
		// Was "db/playerspawnpoints.xml", a path that exists in no vanilla
		// mission — spawn points were silently never backed up.
		"cfgplayerspawnpoints.xml",
		"cfgundergroundtriggers.json", "cfgeffectarea.json",
		"db/globals.xml", "db/economy.xml", "db/events.xml", "db/types.xml", "db/messages.xml",
		"env/zombie_territories.xml",
	} {
		items = append(items, [2]string{filepath.Join(mission, rel), "mission/" + rel})
	}
	// Custom loot files — the manager creates these itself from the Moded page,
	// so leaving them out made the backup unable to restore its own work.
	if entries, err := os.ReadDir(filepath.Join(mission, dztypes.ModedFolder)); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			items = append(items, [2]string{
				filepath.Join(mission, dztypes.ModedFolder, e.Name()),
				"mission/" + dztypes.ModedFolder + "/" + e.Name(),
			})
		}
	}
	return items
}

func (h *handlers) backupExport(w http.ResponseWriter, r *http.Request) {
	fname := fmt.Sprintf("dayz-manager-backup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	_ = h.writeBackupZip(w)
}

// writeBackupZip streams the standard backup zip (configs + mission economy
// files + manifest) to any writer — shared by the download endpoint and the
// scheduled/on-demand disk backups.
func (h *handlers) writeBackupZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, it := range h.backupItems() {
		abs, name := it[0], it[1]
		f, err := os.Open(abs)
		if err != nil {
			continue // missing file — skip silently
		}
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			f.Close()
			continue
		}
		hdr := &zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: st.ModTime(),
		}
		zf, err := zw.CreateHeader(hdr)
		if err != nil {
			f.Close()
			continue
		}
		_, _ = io.Copy(zf, f)
		f.Close()
	}
	// manifest for sanity check on import
	manifest := map[string]interface{}{
		"app":       h.app.Name,
		"version":   h.app.Version,
		"exported":  time.Now().UTC().Format(time.RFC3339),
		"serverDir": h.app.ServerDir,
	}
	if mf, err := zw.Create("manifest.json"); err == nil {
		_ = json.NewEncoder(mf).Encode(manifest)
	}
	return zw.Close()
}

// runBackupToDisk writes a backup zip into .dayz-manager/backups/ and prunes
// old ones down to `keep`. Returns the created file path.
func (h *handlers) runBackupToDisk(keep int) (string, error) {
	dir := filepath.Join(h.app.ManagerDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("backup-%s.zip", time.Now().Format("20060102-150405")))
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if err := h.writeBackupZip(f); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	// Prune: newest `keep` stay.
	if keep < 1 {
		keep = 10
	}
	entries, _ := os.ReadDir(dir)
	type bak struct {
		name string
		mod  time.Time
	}
	var list []bak
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		if info, err := e.Info(); err == nil {
			list = append(list, bak{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	for i := keep; i < len(list); i++ {
		_ = os.Remove(filepath.Join(dir, list[i].name))
	}
	return path, nil
}

// discordTest sends a test message to the webhook from the request body (NOT
// the saved config) so the user can verify a URL before enabling it.
func (h *handlers) discordTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b := i18n.Get(h.app.Cfg().Language)
	if err := notify.Discord(req.URL, b["discord.test"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "sent"})
}

// backupRun creates an on-demand disk backup (same zip as the download).
func (h *handlers) backupRun(w http.ResponseWriter, r *http.Request) {
	path, err := h.runBackupToDisk(h.app.Cfg().BackupKeep)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok", "path": path})
}

// autoBackupLoop writes a scheduled backup every BackupIntervalHours. The
// cadence derives from the newest existing backup file, so manager restarts
// don't reset the clock. Runs for the process lifetime; exits with ctx.
func (h *handlers) autoBackupLoop(ctx context.Context) {
	dir := filepath.Join(h.app.ManagerDir, "backups")
	newest := func() time.Time {
		var t time.Time
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().After(t) {
				t = info.ModTime()
			}
		}
		return t
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
		hours := h.app.Cfg().BackupIntervalHours
		if hours <= 0 {
			continue
		}
		if time.Since(newest()) < time.Duration(hours)*time.Hour {
			continue
		}
		if path, err := h.runBackupToDisk(h.app.Cfg().BackupKeep); err != nil {
			h.app.Log.Printf("auto-backup: %v", err)
		} else {
			h.app.Log.Printf("auto-backup written: %s", path)
			go h.app.NotifyDiscord("backup")
		}
	}
}

func (h *handlers) backupImport(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("zip")
	if err != nil {
		http.Error(w, "missing zip field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 128<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zr, err := zip.NewReader(newByteReaderAt(data), int64(len(data)))
	if err != nil {
		http.Error(w, "bad zip: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Build the inverse mapping from archive name → absolute path.
	targets := map[string]string{}
	for _, it := range h.backupItems() {
		targets[it[1]] = it[0]
	}

	restored := []string{}
	skipped := []string{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(zf.Name, `\`, `/`)
		if name == "manifest.json" {
			continue
		}
		abs, ok := targets[name]
		if !ok {
			skipped = append(skipped, name)
			continue
		}
		if err := restoreOne(zf, abs); err != nil {
			http.Error(w, fmt.Sprintf("restore %s: %v", name, err), http.StatusInternalServerError)
			return
		}
		restored = append(restored, name)
	}

	// Reload config if manager.json was restored.
	for _, n := range restored {
		if n == "manager.json" {
			if err := h.app.ReloadConfig(); err != nil {
				http.Error(w, "reload config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			break
		}
	}
	writeJSON(w, map[string]interface{}{
		"uploaded": fh.Filename,
		"restored": restored,
		"skipped":  skipped,
	})
}

func restoreOne(zf *zip.File, abs string) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	// keep a .bak of the current file (no-op if it doesn't exist)
	_ = util.BackupBeforeWrite(abs)

	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	tmp := abs + ".tmp"
	tf, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(tf, rc); err != nil {
		tf.Close()
		os.Remove(tmp)
		return err
	}
	if err := tf.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// byteReaderAt adapts a []byte to io.ReaderAt for zip.NewReader without pulling
// in bytes.Reader just to avoid the one import.
type byteReaderAt struct{ b []byte }

func newByteReaderAt(b []byte) *byteReaderAt { return &byteReaderAt{b: b} }

func (r *byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(r.b)) {
		return 0, io.EOF
	}
	n := copy(p, r.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
