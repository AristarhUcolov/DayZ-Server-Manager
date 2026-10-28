// Copyright (c) 2026 Aristarh Ucolov.
//
// Mods → Direct download: Steam Workshop mods straight onto the server with
// Valve's SteamCMD (internal/steamcmd), no game client and no download
// folder. One SteamCMD serves the whole panel (it lives in the main
// server's manager folder); a download installs into the server the request
// names. DayZ's Workshop needs an account that owns the game: the password
// is accepted only on this PC or over HTTPS, goes straight to SteamCMD, and is
// never logged or stored — SteamCMD keeps its own login token.
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/steamcmd"
	"dayzmanager/internal/util"
)

// wsItem is one Workshop item of a download job.
type wsItem struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Size  int64  `json:"size,omitempty"`
	State string `json:"state"` // queued | downloading | downloaded | installing | done | error
	Bytes int64  `json:"bytes,omitempty"`
	Mod   string `json:"mod,omitempty"` // the @folder it was installed as
	Error string `json:"error,omitempty"`
	// Gone: installed by this job, deleted from the server since (status
	// only — the list offers to install it again).
	Gone bool `json:"gone,omitempty"`
}

// wsJob is what SteamCMD is doing for the panel (one thing at a time).
type wsJob struct {
	Kind     string                `json:"kind"`  // setup | login | check | download
	State    string                `json:"state"` // running | done | error
	Started  int64                 `json:"started"`
	Items    []wsItem              `json:"items,omitempty"`
	Login    *steamcmd.LoginResult `json:"login,omitempty"`
	Error    string                `json:"error,omitempty"`
	Log      []string              `json:"log,omitempty"`
	ServerID string                `json:"serverId,omitempty"`
}

// wsState is the panel's SteamCMD: the tool, the current job and whether
// the saved login worked the last time it was tried.
type wsState struct {
	mu       sync.Mutex
	tool     *steamcmd.Tool
	job      *wsJob
	loggedIn *bool
	checked  int64
}

func (h *handlers) ws() *wsState {
	s := h.server
	if s == nil {
		return nil
	}
	s.wsOnce.Do(func() {
		s.wsSt = &wsState{tool: steamcmd.New(filepath.Join(s.app.ManagerDir, "steamcmd"))}
	})
	return s.wsSt
}

func (h *handlers) registerWorkshopDL(mux *http.ServeMux) {
	mux.HandleFunc("/api/workshop/dl/status", methods(h.wsStatus, http.MethodGet))
	mux.HandleFunc("/api/workshop/dl/setup", methods(h.wsSetup, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/login", methods(h.wsLogin, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/check", methods(h.wsCheck, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/forget", methods(h.wsForget, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/lookup", methods(h.wsLookup, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/sync", methods(h.wsSync, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/download", methods(h.wsDownload, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/updates", methods(h.wsUpdates, http.MethodPost))
	mux.HandleFunc("/api/workshop/dl/clear", methods(h.wsClear, http.MethodPost))
}

// wsClear takes one finished row off the download list ({id}), or the whole
// list ({}): the list is only a report, the mods stay as they are.
func (h *handlers) wsClear(w http.ResponseWriter, r *http.Request) {
	st := h.ws()
	if st == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.job == nil {
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	if st.job.State == "running" {
		authError(w, http.StatusConflict, "workshop.busy", nil)
		return
	}
	if req.ID == "" {
		st.job = nil
	} else {
		items := st.job.Items[:0:0]
		for _, it := range st.job.Items {
			if it.ID != req.ID {
				items = append(items, it)
			}
		}
		st.job.Items = items
		if len(items) == 0 && (st.job.Kind == "download" || st.job.Kind == "sync") {
			st.job = nil
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) wsStatus(w http.ResponseWriter, r *http.Request) {
	st := h.ws()
	if st == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	st.mu.Lock()
	var job *wsJob
	if st.job != nil {
		cp := *st.job
		cp.Items = append([]wsItem(nil), st.job.Items...)
		job = &cp
		// A mod this job installed and the Mods table deleted since: the
		// row says so instead of "installed".
		if cp.State != "running" && cp.ServerID == h.app.ID {
			for i := range cp.Items {
				it := &cp.Items[i]
				if it.State == "done" && it.Mod != "" && mods.ValidName(it.Mod) == nil {
					if _, err := os.Stat(filepath.Join(h.app.ServerDir, it.Mod)); os.IsNotExist(err) {
						it.Gone = true
					}
				}
			}
		}
	}
	out := map[string]interface{}{
		"installed": st.tool.Installed(),
		"user":      h.app.Cfg().SteamUser,
		"loggedIn":  st.loggedIn,
		"checked":   st.checked,
		"job":       job,
		"secure":    isLoopback(r) || (h.server != nil && h.server.httpsOn.Load()),
		"running":   h.app.ServerIsRunning(),
		"dir":       st.tool.Dir,
	}
	st.mu.Unlock()
	writeJSON(w, out)
}

// start begins a job unless one is running; fn runs in the background.
func (h *handlers) wsStart(w http.ResponseWriter, kind string, items []wsItem, fn func(st *wsState, job *wsJob)) bool {
	st := h.ws()
	if st == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return false
	}
	st.mu.Lock()
	if st.job != nil && st.job.State == "running" {
		st.mu.Unlock()
		authError(w, http.StatusConflict, "workshop.busy", nil)
		return false
	}
	job := &wsJob{Kind: kind, State: "running", Started: time.Now().Unix(), Items: items, ServerID: h.app.ID}
	st.job = job
	st.mu.Unlock()
	go util.SafeGo(h.app.Log, "steamcmd "+kind, func() {
		fn(st, job)
		st.mu.Lock()
		if job.State == "running" {
			job.State = "done"
		}
		st.mu.Unlock()
	})
	writeJSON(w, map[string]interface{}{"ok": true})
	return true
}

func (h *handlers) wsSetup(w http.ResponseWriter, r *http.Request) {
	h.wsStart(w, "setup", nil, func(st *wsState, job *wsJob) {
		h.app.Log.Printf("SteamCMD: installing into %s", st.tool.Dir)
		if err := st.tool.Install(context.Background()); err != nil {
			st.mu.Lock()
			job.State, job.Error = "error", err.Error()
			st.mu.Unlock()
			h.app.Log.Printf("SteamCMD: install failed: %v", err)
			return
		}
		h.app.Log.Printf("SteamCMD: installed")
	})
}

// wsLogin signs in for the first time (password, maybe a Steam Guard code).
func (h *handlers) wsLogin(w http.ResponseWriter, r *http.Request) {
	if !(isLoopback(r) || (h.server != nil && h.server.httpsOn.Load())) {
		// A password over plain HTTP from another device could be read on
		// the way: only here, or with the panel's HTTPS on.
		authError(w, http.StatusForbidden, "workshop.login.insecure", nil)
		return
	}
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.User = strings.TrimSpace(req.User)
	req.Code = strings.TrimSpace(req.Code)
	if !validSteamUser(req.User) || req.Password == "" || strings.ContainsAny(req.Password, "\r\n") || !validCode(req.Code) {
		http.Error(w, "a Steam account name and password are needed", http.StatusBadRequest)
		return
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error { c.SteamUser = req.User; return nil }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pass := req.Password
	h.wsStart(w, "login", nil, func(st *wsState, job *wsJob) {
		h.app.Log.Printf("SteamCMD: signing in as %s", req.User) // never the password
		res := st.tool.Login(context.Background(), req.User, pass, req.Code)
		h.wsLoginDone(st, job, res)
	})
}

// wsCheck tries the saved login (no password).
func (h *handlers) wsCheck(w http.ResponseWriter, r *http.Request) {
	user := h.app.Cfg().SteamUser
	if !validSteamUser(user) {
		http.Error(w, "no Steam account set", http.StatusBadRequest)
		return
	}
	h.wsStart(w, "check", nil, func(st *wsState, job *wsJob) {
		res := st.tool.Login(context.Background(), user, "", "")
		h.wsLoginDone(st, job, res)
	})
}

func (h *handlers) wsLoginDone(st *wsState, job *wsJob, res steamcmd.LoginResult) {
	st.mu.Lock()
	defer st.mu.Unlock()
	job.Login = &res
	ok := res.OK
	st.loggedIn, st.checked = &ok, time.Now().Unix()
	if !res.OK {
		job.State = "error"
	}
	h.app.Log.Printf("SteamCMD: sign-in %s", map[bool]string{true: "OK", false: "failed: " + res.Reason + res.Need + " " + res.Detail}[res.OK])
}

func (h *handlers) wsForget(w http.ResponseWriter, r *http.Request) {
	st := h.ws()
	if st == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := st.tool.Forget(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st.mu.Lock()
	no := false
	st.loggedIn = &no
	st.mu.Unlock()
	_ = h.app.MutateConfig(func(c *config.Manager) error { c.SteamUser = ""; return nil })
	writeJSON(w, map[string]interface{}{"ok": true})
}

var (
	reSteamUser = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,64}$`)
	reCode      = regexp.MustCompile(`^[A-Za-z0-9]{0,10}$`)
	reWSID      = regexp.MustCompile(`(?:[?&]id=|^)(\d{6,20})`)
)

func validSteamUser(u string) bool { return reSteamUser.MatchString(u) }
func validCode(c string) bool      { return reCode.MatchString(c) }

// workshopIDs pulls Workshop item ids out of pasted text: links
// (…/filedetails/?id=123), bare ids, one per line or comma separated.
func workshopIDs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == ';' || r == '\t' }) {
		m := reWSID.FindStringSubmatch(strings.TrimSpace(f))
		if m != nil && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// installedByID maps Workshop ids to the @folders installed in this server.
func (h *handlers) installedByID() map[string]string {
	out := map[string]string{}
	list, _ := mods.List(h.app.ServerDir, "")
	for _, m := range list {
		if m.InstalledInServer && m.PublishedID != "" {
			out[m.PublishedID] = m.Name
		}
	}
	return out
}

// wsLookup resolves pasted links (collections expand to their items).
func (h *handlers) wsLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ids := workshopIDs(req.Text)
	if len(ids) == 0 {
		http.Error(w, "no Workshop links or ids found", http.StatusBadRequest)
		return
	}
	if len(ids) > 200 {
		ids = ids[:200]
	}
	// A collection link looks like an item link: ask for its children.
	var all []string
	seen := map[string]bool{}
	for _, id := range ids {
		kids, err := mods.FetchCollectionChildren(id)
		if err == nil && len(kids) > 0 {
			for _, k := range kids {
				if !seen[k] {
					seen[k] = true
					all = append(all, k)
				}
			}
			continue
		}
		if !seen[id] {
			seen[id] = true
			all = append(all, id)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	meta, err := mods.FetchWorkshopMeta(ctx, all)
	if err != nil && len(meta) == 0 {
		http.Error(w, "Steam: "+err.Error(), http.StatusBadGateway)
		return
	}
	inst := h.installedByID()
	type row struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Size      int64  `json:"size"`
		Updated   int64  `json:"updated"`
		Found     bool   `json:"found"`
		DayZ      bool   `json:"dayz"`
		Installed string `json:"installed,omitempty"`
		Preview   string `json:"preview,omitempty"`
	}
	out := make([]row, 0, len(all))
	for _, id := range all {
		m, ok := meta[id]
		rr := row{ID: id, Installed: inst[id]}
		if ok && m.Result == 1 {
			rr.Found, rr.Title, rr.Size, rr.Preview = true, m.Title, m.FileSize, m.Preview
			rr.DayZ = m.AppID == 0 || m.AppID == 221100
			if !m.TimeUpdated.IsZero() {
				rr.Updated = m.TimeUpdated.Unix()
			}
		}
		out = append(out, rr)
	}
	writeJSON(w, map[string]interface{}{"items": out})
}

// wsDownload downloads items with SteamCMD and installs them into this
// server (enabled as client or server-side mods).
func (h *handlers) wsDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Size  int64  `json:"size"`
		} `json:"items"`
		ServerSide bool `json:"serverSide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user := h.app.Cfg().SteamUser
	if !validSteamUser(user) {
		authError(w, http.StatusBadRequest, "workshop.needLogin", nil)
		return
	}
	if h.app.ServerIsRunning() {
		authError(w, http.StatusConflict, "guard.serverRunning", nil)
		return
	}
	var items []wsItem
	var ids []string
	for _, it := range req.Items {
		if m := reWSID.FindStringSubmatch(it.ID); m != nil && m[1] == it.ID {
			items = append(items, wsItem{ID: it.ID, Title: it.Title, Size: it.Size, State: "queued"})
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		http.Error(w, "nothing to download", http.StatusBadRequest)
		return
	}
	serverSide := req.ServerSide
	h.wsStart(w, "download", items, func(st *wsState, job *wsJob) {
		// Progress: the size of each item's folder while SteamCMD works.
		stop := make(chan struct{})
		go h.trackProgress(st, job, stop)
		h.app.Log.Printf("SteamCMD: downloading %d Workshop item(s) as %s", len(ids), user)
		res, login, err := st.tool.Download(context.Background(), user, ids)
		close(stop)
		st.mu.Lock()
		if !login.OK && (login.Reason != "" || login.Need != "") {
			no := false
			st.loggedIn = &no
			job.Login = &login
			job.State, job.Error = "error", "login"
			for i := range job.Items {
				job.Items[i].State, job.Items[i].Error = "error", "login"
			}
			st.mu.Unlock()
			return
		}
		if login.OK {
			yes := true
			st.loggedIn, st.checked = &yes, time.Now().Unix()
		}
		byID := map[string]steamcmd.ItemResult{}
		for _, r := range res {
			byID[r.ID] = r
		}
		for i := range job.Items {
			it := &job.Items[i]
			r := byID[it.ID]
			if r.OK {
				it.State = "downloaded"
				if r.Bytes > 0 {
					it.Bytes = r.Bytes
				}
			} else {
				it.State, it.Error = "error", r.Error
				if it.Error == "" && err != nil {
					it.Error = err.Error()
				}
			}
		}
		st.mu.Unlock()

		// Install what arrived. The server may have been started meanwhile:
		// then the copies wait (files of a running server are locked).
		for i := range job.Items {
			st.mu.Lock()
			it := job.Items[i]
			st.mu.Unlock()
			if it.State != "downloaded" {
				continue
			}
			if h.app.ServerIsRunning() {
				st.mu.Lock()
				job.Items[i].State, job.Items[i].Error = "error", "server running"
				st.mu.Unlock()
				continue
			}
			st.mu.Lock()
			job.Items[i].State = "installing"
			st.mu.Unlock()
			name, ierr := h.wsInstall(st.tool.ContentDir(), it.ID, serverSide)
			st.mu.Lock()
			if ierr != nil {
				job.Items[i].State, job.Items[i].Error = "error", ierr.Error()
			} else {
				job.Items[i].State, job.Items[i].Mod = "done", name
			}
			st.mu.Unlock()
		}
		st.mu.Lock()
		failed := 0
		for _, it := range job.Items {
			if it.State != "done" {
				failed++
			}
		}
		if failed == len(job.Items) {
			job.State = "error"
		}
		st.mu.Unlock()
	})
}

// wsInstall copies one downloaded item into the server as @Name, merges its
// keys and switches it on (client mod, or server-side when asked).
func (h *handlers) wsInstall(contentDir, id string, serverSide bool) (string, error) {
	name := mods.SourceName(contentDir, id)
	if name == "" {
		return "", errNotAMod
	}
	h.app.WriteMu.Lock()
	defer h.app.WriteMu.Unlock()
	if err := mods.Install(h.app.ServerDir, contentDir, name); err != nil {
		return "", err
	}
	err := h.app.MutateConfig(func(c *config.Manager) error {
		if contains(c.Mods, name) || contains(c.ServerMods, name) {
			return nil // already on (an update): keep where it is
		}
		if serverSide {
			c.ServerMods = append(append([]string(nil), c.ServerMods...), name)
		} else {
			c.Mods = append(append([]string(nil), c.Mods...), name)
		}
		return nil
	})
	h.app.Log.Printf("SteamCMD: installed Workshop item %s as %s", id, name)
	return name, err
}

var errNotAMod = &wsError{"the download is not a DayZ mod (no addons folder or meta.cpp)"}

type wsError struct{ s string }

func (e *wsError) Error() string { return e.s }

// wsUpdates lists installed Workshop mods with a newer version on Steam.
func (h *handlers) wsUpdates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.workshopRows(ctx)
	if err != nil {
		http.Error(w, "Steam: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]interface{}{"mods": rows})
}

// trackProgress shows each queued item's folder size while SteamCMD
// downloads, until stop closes.
func (h *handlers) trackProgress(st *wsState, job *wsJob, stop chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			h.app.Log.Printf("BUG: download progress panicked: %v", r)
		}
	}()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		st.mu.Lock()
		for i := range job.Items {
			it := &job.Items[i]
			if it.State != "queued" && it.State != "downloading" {
				continue
			}
			n := steamcmd.FolderSize(filepath.Join(st.tool.DownloadingDir(), it.ID))
			if n2 := steamcmd.FolderSize(filepath.Join(st.tool.ContentDir(), it.ID)); n2 > n {
				n = n2
			}
			if n > 0 {
				it.State, it.Bytes = "downloading", n
			}
		}
		job.Log = st.tool.LogTail(4)
		st.mu.Unlock()
	}
}
