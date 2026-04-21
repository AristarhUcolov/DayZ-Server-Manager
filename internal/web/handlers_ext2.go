// Copyright (c) 2026 Aristarh Ucolov.
//
// Extra handlers v2: ADM log viewer, config zip backup (export/import),
// dashboard live metrics. Kept separate from handlers_ext.go to keep each file
// readable.
package web

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/admlog"
	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/util"
)

// ---------------------------------------------------------------------------
// ADM log viewer.

func (h *handlers) admlogRecent(w http.ResponseWriter, r *http.Request) {
	profilesDir := h.app.Config.ProfilesDir
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

func (h *handlers) dashboardMetrics(w http.ResponseWriter, r *http.Request) {
	out := map[string]interface{}{
		"running": h.app.ServerIsRunning(),
		"pid":     h.app.Server.PID(),
		"uptime":  h.app.Server.Uptime().Round(time.Second).String(),
		"port":    h.app.Config.ServerPort,
	}

	// Mods count (installed on the manager, mirrored to the server dir).
	if h.app.Config.VanillaDayZPath != "" {
		if list, err := mods.List(h.app.ServerDir, h.app.Config.VanillaDayZPath); err == nil {
			active := map[string]bool{}
			for _, name := range h.app.Config.Mods {
				active[name] = true
			}
			for _, name := range h.app.Config.ServerMods {
				active[name] = true
			}
			installed := 0
			enabled := 0
			for _, m := range list {
				if m.InstalledInServer {
					installed++
				}
				if active[m.Name] {
					enabled++
				}
			}
			out["mods"] = map[string]interface{}{
				"total":     len(list),
				"installed": installed,
				"active":    enabled,
			}
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
				"cpuPercent":     norm,
				"cpuPercentRaw":  cpu,
				"memBytes":       mem,
				"cores":          cores,
			}
		}
	}

	// Live player count — best-effort, only when the server is up.
	if h.app.ServerIsRunning() {
		h.app.ApplyRConConfig()
		if players, err := h.app.RCon.Players(); err == nil {
			out["playerCount"] = len(players)
			out["players"] = players
		}
	}

	// Log sources with sizes.
	out["logs"] = dzlogs.Discover(h.app.ServerDir, h.app.Config.ProfilesDir)

	// Recent ADM events (last 20) for the activity strip.
	profilesDir := h.app.Config.ProfilesDir
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
	be := h.app.Config.BEPath
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
		"cfggameplay.json", "cfgweather.xml",
		"db/globals.xml", "db/economy.xml", "db/events.xml", "db/types.xml", "db/messages.xml",
		"db/playerspawnpoints.xml",
		"env/zombie_territories.xml",
	} {
		items = append(items, [2]string{filepath.Join(mission, rel), "mission/" + rel})
	}
	return items
}

func (h *handlers) backupExport(w http.ResponseWriter, r *http.Request) {
	fname := fmt.Sprintf("dayz-manager-backup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)

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

