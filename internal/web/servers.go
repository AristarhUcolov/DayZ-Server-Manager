// Copyright (c) 2026 Aristarh Ucolov.
//
// Multi-server: the Servers page. Lists every DayZ server of the panel with
// its state and ports, adds an existing server folder, creates a new server as
// a copy of one already in the panel, renames, reorders and removes (files are
// never deleted). The server a request is for is picked in server.go.
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	srvpkg "dayzmanager/internal/server"
	"dayzmanager/internal/util"
)

func (h *handlers) registerServers(mux *http.ServeMux) {
	mux.HandleFunc("/api/servers", methods(h.serversList, http.MethodGet))
	mux.HandleFunc("/api/servers/inspect", methods(h.serversInspect, http.MethodPost))
	mux.HandleFunc("/api/servers/add", methods(h.serversAdd, http.MethodPost))
	mux.HandleFunc("/api/servers/create", methods(h.serversCreate, http.MethodPost))
	mux.HandleFunc("/api/servers/create/status", methods(h.serversCreateStatus, http.MethodGet))
	mux.HandleFunc("/api/servers/remove", methods(h.serversRemove, http.MethodPost))
	mux.HandleFunc("/api/servers/rename", methods(h.serversRename, http.MethodPost))
	mux.HandleFunc("/api/servers/move", methods(h.serversMove, http.MethodPost))
	mux.HandleFunc("/api/servers/retry", methods(h.serversRetry, http.MethodPost))
	mux.HandleFunc("/api/servers/free-ports", methods(h.serversFreePorts, http.MethodPost))
}

// serversFreePorts moves a stopped server to ports no other server uses.
func (h *handlers) serversFreePorts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	a := f.Get(req.ID)
	if a == nil {
		http.Error(w, "unknown server", http.StatusNotFound)
		return
	}
	a.WriteMu.Lock()
	p, err := f.AssignFreePorts(a)
	a.WriteMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "ports": p})
}

// fleet is the panel's server list (nil in tests that build bare handlers).
func (h *handlers) fleet() *app.Fleet {
	if h.server == nil {
		return nil
	}
	return h.server.fleet
}

// serverEntry is one row of the Servers page and the server switcher.
func serverEntry(m app.Member, primary bool) map[string]interface{} {
	out := map[string]interface{}{"id": m.ID, "name": m.Name, "dir": m.Dir, "primary": primary}
	if m.Error != "" {
		out["error"] = m.Error
	}
	a := m.App
	if a == nil {
		return out
	}
	running := a.ServerIsRunning()
	out["running"] = running
	out["stopping"] = a.Server.IsStopping()
	out["loopPaused"] = a.Server.LoopPaused()
	ports := a.Ports()
	out["ports"] = ports
	if running {
		out["uptimeSec"] = int64(a.Server.Uptime().Seconds())
		q := a.Query()
		out["listed"] = q.OK
		out["loading"] = !q.OK && q.FirstOK.IsZero() && a.Server.Uptime() < 15*time.Minute
		if q.OK && q.Info != nil {
			out["players"] = q.Info.Players
			out["maxPlayers"] = q.Info.MaxPlayers
		}
	}
	if sc, err := config.LoadServerCfg(filepath.Join(a.ServerDir, a.Cfg().ServerCfg)); err == nil {
		if v, ok := sc.Get("hostname"); ok {
			out["hostname"] = strings.Trim(strings.TrimSpace(v), `";`)
		}
		out["mission"] = sc.MissionTemplate()
	}
	return out
}

func (h *handlers) serversList(w http.ResponseWriter, r *http.Request) {
	f := h.fleet()
	if f == nil {
		writeJSON(w, map[string]interface{}{"servers": []interface{}{}})
		return
	}
	members := f.Members()
	list := make([]map[string]interface{}, 0, len(members))
	for i, m := range members {
		e := serverEntry(m, i == 0)
		if m.App != nil {
			if c := f.PortClashes(m.App); len(c) > 0 {
				e["clashes"] = c
			}
		}
		list = append(list, e)
	}
	writeJSON(w, map[string]interface{}{
		"servers":    list,
		"max":        app.MaxServers,
		"freePorts":  f.FreePorts(),
		"suggestDir": suggestDir(members),
		"job":        h.server.cloneState(),
	})
}

// suggestDir proposes a folder for a new server: next to the primary, its
// name with the next free number ("DayZServer" → "DayZServer2").
func suggestDir(members []app.Member) string {
	if len(members) == 0 {
		return ""
	}
	base := members[0].Dir
	parent, name := filepath.Dir(base), filepath.Base(base)
	name = strings.TrimRight(name, "0123456789")
	if name == "" {
		name = "DayZServer"
	}
	for n := 2; n < 100; n++ {
		cand := filepath.Join(parent, fmt.Sprintf("%s%d", name, n))
		taken := false
		for _, m := range members {
			if strings.EqualFold(filepath.Clean(m.Dir), cand) {
				taken = true
			}
		}
		if _, err := os.Stat(cand); err == nil || taken {
			continue
		}
		return cand
	}
	return ""
}

// serversInspect checks a folder typed on the Servers page, so the form can
// say what will happen before anything is added or copied.
func (h *handlers) serversInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	out := map[string]interface{}{}
	dir, err := f.CheckNewDir(req.Dir)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	out["dir"] = dir
	st, err := os.Stat(dir)
	switch {
	case err != nil:
		out["exists"] = false
	case !st.IsDir():
		out["error"] = "this is a file, not a folder"
	default:
		out["exists"] = true
		_, isServer := srvpkg.FindServerExe(dir)
		out["dayz"] = isServer
		entries, _ := os.ReadDir(dir)
		out["empty"] = len(entries) == 0
		if _, err := os.Stat(filepath.Join(dir, ".dayz-manager", "manager.json")); err == nil {
			out["managedBefore"] = true
		}
		if url, busy := app.OwnerOf(dir, h.app.ManagerDir); busy {
			out["error"] = "another DayZ Server Manager runs this server: " + url
		}
	}
	writeJSON(w, out)
}

func (h *handlers) serversAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	a, err := f.Add(req.Dir, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.server.adopt(a)
	writeJSON(w, map[string]interface{}{"ok": true, "id": a.ID})
}

// adopt wires a server just added to the panel: its API and loops, the
// launcher's ownership mark, and a running DayZ server already in its folder.
func (s *Server) adopt(a *app.App) {
	if s.OnServerAdded != nil {
		s.OnServerAdded(a)
	}
	s.attach(a)
}

func (h *handlers) serversRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := f.Remove(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.server.detach(req.ID)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) serversRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := f.Rename(req.ID, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) serversMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string `json:"id"`
		Delta int    `json:"delta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if req.Delta < 0 {
		req.Delta = -1
	} else {
		req.Delta = 1
	}
	if err := f.Move(req.ID, req.Delta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) serversRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if inst, ok := h.server.instance(req.ID); ok && inst != nil {
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	a, err := f.Retry(req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.server.adopt(a)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (s *Server) instance(id string) (*instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.insts[serverKey(id)]
	return inst, ok
}

// ------------------------------------------------------------- create a copy

// cloneJob is the one "create a new server as a copy" job that can run at a
// time: copying a DayZ server is gigabytes, so it runs in the background and
// the page polls its progress.
type cloneJob struct {
	mu      sync.Mutex
	State   string `json:"state"` // "" | "copying" | "done" | "error"
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Total   int64  `json:"total"`
	Copied  int64  `json:"copied"`
	Files   int    `json:"files"`
	Current string `json:"current,omitempty"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"` // the new server, when done
	Started int64  `json:"started"`
}

func (s *Server) cloneState() map[string]interface{} {
	j := &s.clone
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State == "" {
		return nil
	}
	return map[string]interface{}{"state": j.State, "name": j.Name, "dir": j.Dir, "total": j.Total,
		"copied": j.Copied, "files": j.Files, "current": j.Current, "error": j.Error, "id": j.ID, "started": j.Started}
}

func (h *handlers) serversCreateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"job": h.server.cloneState()})
}

func (h *handlers) serversCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Dir    string `json:"dir"`
		Source string `json:"source"` // server id to copy (default: the primary)
		Mods   bool   `json:"mods"`   // copy the @mod folders too
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := h.fleet()
	if f == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	src := f.Get(req.Source)
	if src == nil {
		http.Error(w, "unknown source server", http.StatusBadRequest)
		return
	}
	if len(f.Members()) >= app.MaxServers {
		http.Error(w, fmt.Sprintf("one manager runs at most %d servers", app.MaxServers), http.StatusBadRequest)
		return
	}
	dir, err := f.CheckNewDir(req.Dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		http.Error(w, "the folder is not empty — choose a new or empty folder for the copy", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filepath.Base(dir)
	}
	files, total, err := cloneFileList(src, req.Mods)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if free, err := util.DiskFree(existingParent(dir)); err == nil && free > 0 && free < uint64(total)+512<<20 {
		http.Error(w, fmt.Sprintf("not enough free disk space: the copy needs %s, %s is free",
			humanBytes(total+512<<20), humanBytes(int64(free))), http.StatusBadRequest)
		return
	}
	j := &h.server.clone
	j.mu.Lock()
	if j.State == "copying" {
		j.mu.Unlock()
		http.Error(w, "a copy is already being made — wait for it to finish", http.StatusConflict)
		return
	}
	j.State, j.Name, j.Dir, j.Total, j.Copied, j.Files, j.Current, j.Error, j.ID =
		"copying", name, dir, total, 0, len(files), "", "", ""
	j.Started = time.Now().UnixMilli()
	j.mu.Unlock()

	ports := f.FreePorts()
	go util.SafeGo(h.app.Log, "server copy", func() {
		id, err := h.server.runClone(src, dir, name, files, ports, req.Mods)
		j.mu.Lock()
		defer j.mu.Unlock()
		if err != nil {
			j.State, j.Error = "error", err.Error()
			h.app.Log.Printf("create server copy in %s: %v", dir, err)
			return
		}
		j.State, j.ID, j.Current = "done", id, ""
	})
	writeJSON(w, map[string]interface{}{"ok": true, "ports": ports})
}

// cloneFile is one file to copy, relative to the source server folder.
type cloneFile struct {
	rel  string
	size int64
	mode fs.FileMode
}

// cloneFileList lists what a copy of src takes: the game files, missions,
// keys, BattlEye settings and (when mods) the @mod folders — not the
// manager's own folder, profiles (logs, crash dumps), the persistence of each
// mission (storage_*: the new server starts with a fresh world), log files or
// BattlEye's runtime files.
func cloneFileList(src *app.App, mods bool) ([]cloneFile, int64, error) {
	root := src.ServerDir
	cfg := src.Cfg()
	skipDirs := map[string]bool{".dayz-manager": true, "profiles": true, "logs": true, "crashlogs": true}
	if p := strings.TrimSpace(cfg.ProfilesDir); p != "" && !filepath.IsAbs(p) {
		skipDirs[strings.ToLower(filepath.ToSlash(filepath.Clean(p)))] = true
	}
	var out []cloneFile
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable entry: skip it
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		relSlash := strings.ToLower(filepath.ToSlash(rel))
		base := strings.ToLower(d.Name())
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow links out of the server folder
		}
		if d.IsDir() {
			switch {
			case skipDirs[relSlash]:
				return filepath.SkipDir
			case !mods && strings.HasPrefix(base, "@") && !strings.Contains(relSlash, "/"):
				return filepath.SkipDir
			case strings.HasPrefix(base, "storage_") && strings.HasPrefix(relSlash, "mpmissions/"):
				return filepath.SkipDir
			case relSlash == "steamapps/workshop":
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		switch ext := strings.ToLower(filepath.Ext(base)); {
		case ext == ".rpt" || ext == ".adm" || ext == ".log" || ext == ".mdmp" || ext == ".bidmp" || ext == ".dmp":
			return nil
		case strings.HasPrefix(relSlash, "battleye/") && strings.HasPrefix(base, "beserver_x64_active_"):
			return nil
		case strings.Contains(base, ".bak."):
			return nil // the panel's own edit backups
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, cloneFile{rel: rel, size: info.Size(), mode: info.Mode().Perm()})
		total += info.Size()
		return nil
	})
	return out, total, err
}

// runClone copies the files, gives the copy its own ports and settings, and
// adds it to the panel. Returns the new server's id.
func (s *Server) runClone(src *app.App, dir, name string, files []cloneFile, ports app.Ports, mods bool) (string, error) {
	j := &s.clone
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 1<<20)
	for _, f := range files {
		j.mu.Lock()
		j.Current = filepath.ToSlash(f.rel)
		j.mu.Unlock()
		n, err := copyOne(filepath.Join(src.ServerDir, f.rel), filepath.Join(dir, f.rel), f.mode, buf)
		if err != nil {
			return "", fmt.Errorf("copy %s: %w", f.rel, err)
		}
		j.mu.Lock()
		j.Copied += n
		j.mu.Unlock()
	}

	// The copy's own settings: the source's, on free ports. Without the mods
	// copied, their names would point at folders the new server lacks.
	cfg := src.Cfg()
	cfg.ServerPort, cfg.RConPort = ports.Game, ports.RCon
	cfg.FirstRunDone = true
	if !mods {
		cfg.Mods, cfg.ServerMods = nil, nil
	}
	if err := os.MkdirAll(filepath.Join(dir, ".dayz-manager"), 0o755); err != nil {
		return "", err
	}
	if err := config.SaveManager(filepath.Join(dir, ".dayz-manager", "manager.json"), &cfg); err != nil {
		return "", err
	}
	// steamQueryPort: two servers on one query port hide each other in the
	// server browser.
	cfgPath := filepath.Join(dir, cfg.ServerCfg)
	if sc, err := config.LoadServerCfg(cfgPath); err == nil {
		sc.Set("steamQueryPort", ports.Query)
		if err := sc.Save(cfgPath); err != nil {
			return "", fmt.Errorf("server.cfg: %w", err)
		}
	}
	// BattlEye listens for RCon where the panel will connect.
	beDir := cfg.BEPath
	if beDir == "" {
		beDir = "battleye"
	}
	if !filepath.IsAbs(beDir) {
		beDir = filepath.Join(dir, beDir)
	} else if within(src.ServerDir, beDir) {
		// An absolute path inside the source server: point at the copy.
		rel, _ := filepath.Rel(src.ServerDir, beDir)
		beDir = filepath.Join(dir, rel)
	}
	if be := config.FindBEConfig(beDir); be != nil && be.RConPassword != "" {
		if _, err := config.EnsureBEConfig(beDir, be.RConPassword, ports.RCon, true); err != nil {
			return "", fmt.Errorf("beserver_x64.cfg: %w", err)
		}
	}

	a, err := s.fleet.Add(dir, name)
	if err != nil {
		return "", err
	}
	s.adopt(a)
	a.Log.Printf("created as a copy of %q (%s) — game port %d, query port %d, RCon port %d",
		src.Label(), src.ServerDir, ports.Game, ports.Query, ports.RCon)
	return a.ID, nil
}

func copyOne(from, to string, mode fs.FileMode, buf []byte) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return 0, err
	}
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.CopyBuffer(out, in, buf)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if st, err := os.Stat(from); err == nil {
		_ = os.Chtimes(to, st.ModTime(), st.ModTime())
	}
	return n, nil
}

// existingParent is the nearest folder of path that exists (for the free-space
// check of a folder about to be created).
func existingParent(path string) string {
	for p := path; ; {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// within reports whether path is inside dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
