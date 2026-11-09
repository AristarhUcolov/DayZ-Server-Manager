// Copyright (c) 2026 Aristarh Ucolov.
package web

// The "Steam Workshop" mod source: the panel's SteamCMD is the source. Its
// download folder is what the Mods page installs from, and "Install & update
// mods", the update check and the auto-update before restarts ask the
// Workshop which installed mods are newer there, download those with
// SteamCMD and copy them into the server.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/steamcmd"
)

// wsRow is one installed Workshop mod compared with the Workshop.
type wsRow struct {
	ID      string `json:"id"`
	Mod     string `json:"mod"`
	Title   string `json:"title"`
	Size    int64  `json:"size"`
	Updated int64  `json:"updated"`
	Local   int64  `json:"local"`
	// Newer: Steam has a newer version than SteamCMD downloaded — download
	// it. Copy: the download is newer than the server's copy — copy it.
	Newer bool `json:"newer"`
	Copy  bool `json:"copy,omitempty"`
	Exact bool `json:"exact,omitempty"` // decided by Steam's content ids
}

// workshopRows compares this server's installed Workshop mods with the
// Workshop (one request for all of them), by content id wherever Steam's or
// the panel's notes know it (mods.CompareWithSteam).
func (h *handlers) workshopRows(ctx context.Context) ([]wsRow, error) {
	content := ""
	if st := h.ws(); st != nil {
		content = st.tool.ContentDir()
	}
	list, _ := mods.List(h.app.ServerDir, content)
	byID := map[string]mods.Mod{}
	var ids []string
	for _, m := range list {
		if !m.InstalledInServer || m.PublishedID == "" {
			continue
		}
		if _, dup := byID[m.PublishedID]; dup {
			continue
		}
		if m.WorkshopPath == "" && content != "" {
			// Installed under another name than SteamCMD's: its download, if
			// any, compared with the server's copy directly.
			if p := filepath.Join(content, m.PublishedID); dirExists(p) {
				m.WorkshopPath = p
				m.UpdateAvailable = mods.SourceNewer(p, m.ServerPath)
			}
		}
		byID[m.PublishedID] = m
		ids = append(ids, m.PublishedID)
	}
	out := []wsRow{}
	if len(ids) == 0 {
		return out, nil
	}
	meta, err := mods.FetchWorkshopMeta(ctx, ids)
	if err != nil && len(meta) == 0 {
		return nil, err
	}
	for id, l := range byID {
		m, ok := meta[id]
		if !ok || m.Result != 1 {
			continue
		}
		st := mods.CompareWithSteam(l, m)
		r := wsRow{ID: id, Mod: l.Name, Title: m.Title, Size: m.FileSize, Updated: m.TimeUpdated.Unix(), Exact: st.Exact}
		if !st.Local.IsZero() {
			r.Local = st.Local.Unix()
		}
		r.Newer = st.Status == mods.SteamOutdated
		r.Copy = st.Status == mods.SteamUpdate
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Mod) < strings.ToLower(out[j].Mod) })
	return out, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// workshopOutdated counts the installed Workshop mods to update (the
// auto-update check of the "steamcmd" source).
func (h *handlers) workshopOutdated(ctx context.Context) (int, error) {
	rows, err := h.workshopRows(ctx)
	n := 0
	for _, r := range rows {
		if r.Newer || r.Copy {
			n++
		}
	}
	return n, err
}

var errNoSteamLogin = errors.New("not signed in to Steam (Mods → Direct download)")

// workshopSync downloads the newer Workshop versions of this server's mods
// with SteamCMD and copies them into the server. job (may be nil) shows the
// progress on the Mods page. locked: the caller already holds the write lock
// (a restart's down-window); otherwise each copy takes it.
func (h *handlers) workshopSync(ctx context.Context, job *wsJob, locked bool) ([]string, error) {
	st := h.ws()
	if st == nil {
		return nil, errors.New("SteamCMD unavailable")
	}
	user := h.app.Cfg().SteamUser
	if !validSteamUser(user) || !st.tool.Installed() {
		return nil, errNoSteamLogin
	}
	rows, err := h.workshopRows(ctx)
	if err != nil {
		return nil, err
	}
	return h.workshopUpdate(ctx, job, locked, rows, nil)
}

// workshopUpdate brings the given rows up to date: the ones newer on Steam
// are downloaded with SteamCMD (force: these ids even when they look current
// — an explicit "Update"), then every downloaded or newer copy is copied into
// the server folder that already holds it.
func (h *handlers) workshopUpdate(ctx context.Context, job *wsJob, locked bool, rows []wsRow, force map[string]bool) ([]string, error) {
	st := h.ws()
	user := h.app.Cfg().SteamUser
	var ids, all []string
	modOf := map[string]string{}
	for _, r := range rows {
		if !r.Newer && !r.Copy && !force[r.ID] {
			continue
		}
		all = append(all, r.ID)
		modOf[r.ID] = r.Mod
		if r.Newer || force[r.ID] {
			ids = append(ids, r.ID)
		}
		if job != nil {
			st.mu.Lock()
			job.Items = append(job.Items, wsItem{ID: r.ID, Title: r.Title, Size: r.Size, State: "queued", Mod: r.Mod})
			st.mu.Unlock()
		}
	}
	if len(all) == 0 {
		return nil, nil
	}
	ok := map[string]bool{}
	var res []steamcmd.ItemResult
	var derr error
	if len(ids) > 0 {
		h.app.Log.Printf("SteamCMD: updating %d Workshop mod(s) as %s", len(ids), user)
		var stop chan struct{}
		if job != nil {
			stop = make(chan struct{})
			go h.trackProgress(st, job, stop)
		}
		var login steamcmd.LoginResult
		res, login, derr = st.tool.Download(ctx, user, ids)
		if stop != nil {
			close(stop)
		}
		if !login.OK && (login.Reason != "" || login.Need != "") {
			no := false
			st.mu.Lock()
			st.loggedIn = &no
			if job != nil {
				job.Login = &login
				for i := range job.Items {
					job.Items[i].State, job.Items[i].Error = "error", "login"
				}
			}
			st.mu.Unlock()
			return nil, errNoSteamLogin
		}
		for _, r := range res {
			ok[r.ID] = r.OK
		}
	}
	for _, id := range all {
		if _, downloaded := ok[id]; !downloaded {
			ok[id] = true // a copy-only row: the download is already there
		}
	}
	var updated []string
	content := st.tool.ContentDir()
	for i, id := range all {
		setState := func(state, e string) {
			if job == nil {
				return
			}
			st.mu.Lock()
			for k := range job.Items {
				if job.Items[k].ID == id {
					job.Items[k].State, job.Items[k].Error = state, e
				}
			}
			st.mu.Unlock()
		}
		if !ok[id] {
			msg := "download failed"
			for _, r := range res {
				if r.ID == id && r.Error != "" {
					msg = r.Error
				}
			}
			if derr != nil && msg == "download failed" {
				msg = derr.Error()
			}
			setState("error", msg)
			continue
		}
		name := modOf[id]
		if name == "" {
			name = h.wsTarget(content, id, h.installedByID())
		}
		if name == "" || !dirExists(filepath.Join(content, id)) {
			setState("error", errNotAMod.Error())
			continue
		}
		if !locked && h.app.ServerIsRunning() {
			setState("error", "server running")
			continue
		}
		setState("installing", "")
		var ierr error
		if locked {
			ierr = mods.InstallFrom(h.app.ServerDir, filepath.Join(content, id), name)
		} else {
			h.app.WriteMu.Lock()
			ierr = mods.InstallFrom(h.app.ServerDir, filepath.Join(content, id), name)
			h.app.WriteMu.Unlock()
		}
		if ierr != nil {
			setState("error", ierr.Error())
			continue
		}
		setState("done", "")
		updated = append(updated, name)
		_ = i
	}
	if len(updated) > 0 {
		h.app.Log.Printf("SteamCMD: updated %s", strings.Join(updated, ", "))
	}
	return updated, nil
}

// wsSync is "Install & update mods" of the Steam Workshop source.
func (h *handlers) wsSync(w http.ResponseWriter, r *http.Request) {
	if !validSteamUser(h.app.Cfg().SteamUser) {
		authError(w, http.StatusBadRequest, "workshop.needLogin", nil)
		return
	}
	if h.app.ServerIsRunning() {
		authError(w, http.StatusConflict, "guard.serverRunning", nil)
		return
	}
	h.wsStart(w, "sync", nil, func(st *wsState, job *wsJob) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		updated, err := h.workshopSync(ctx, job, false)
		st.mu.Lock()
		defer st.mu.Unlock()
		switch {
		case errors.Is(err, errNoSteamLogin):
			job.State, job.Error = "error", "login"
		case err != nil:
			job.State, job.Error = "error", err.Error()
		case len(job.Items) > 0 && len(updated) == 0:
			job.State = "error"
		}
	})
}

// wsUpdateMods updates the given installed Workshop mods now ({"ids": […]}):
// downloaded again with SteamCMD and copied over the server folders that
// hold them — the Mods table's Update button with the Steam Workshop source.
func (h *handlers) wsUpdateMods(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validSteamUser(h.app.Cfg().SteamUser) {
		authError(w, http.StatusBadRequest, "workshop.needLogin", nil)
		return
	}
	if h.app.ServerIsRunning() {
		authError(w, http.StatusConflict, "guard.serverRunning", nil)
		return
	}
	installed := h.installedByID()
	force := map[string]bool{}
	for _, id := range req.IDs {
		if m := reWSID.FindStringSubmatch(id); m != nil && m[1] == id && installed[id] != "" {
			force[id] = true
		}
	}
	if len(force) == 0 {
		http.Error(w, "none of these mods is installed from the Workshop", http.StatusBadRequest)
		return
	}
	h.wsStart(w, "sync", nil, func(st *wsState, job *wsJob) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		rows, _ := h.workshopRows(ctx) // titles and sizes; Steam may be unreachable
		var pick []wsRow
		have := map[string]bool{}
		for _, rr := range rows {
			if force[rr.ID] {
				pick = append(pick, rr)
				have[rr.ID] = true
			}
		}
		for id := range force {
			if !have[id] {
				pick = append(pick, wsRow{ID: id, Mod: installed[id], Title: installed[id]})
			}
		}
		sort.Slice(pick, func(i, j int) bool { return pick[i].Mod < pick[j].Mod })
		updated, err := h.workshopUpdate(ctx, job, false, pick, force)
		st.mu.Lock()
		defer st.mu.Unlock()
		switch {
		case errors.Is(err, errNoSteamLogin):
			job.State, job.Error = "error", "login"
		case err != nil:
			job.State, job.Error = "error", err.Error()
		case len(updated) == 0:
			job.State = "error"
		}
	})
}

// workshopHooks are this server's update hooks for the auto-update before
// restarts (see app.WorkshopHooks).
func (h *handlers) workshopHooks() app.WorkshopHooks {
	return app.WorkshopHooks{
		Sync: func(ctx context.Context) ([]string, error) {
			return h.workshopSync(ctx, nil, true)
		},
		Outdated: h.workshopOutdated,
	}
}

// syncSteamCMDSource keeps the "steamcmd" mod source pointing at the
// panel's SteamCMD download folder.
func (h *handlers) syncSteamCMDSource() {
	cfg := h.app.Cfg()
	if cfg.EffectiveModSource() != config.ModSourceSteamCMD {
		return
	}
	st := h.ws()
	if st == nil {
		return
	}
	if want := st.tool.ContentDir(); cfg.SteamCMDContent != want {
		if h.app.MutateConfig(func(c *config.Manager) error {
			c.SteamCMDContent = want
			return nil
		}) == nil {
			h.app.ScheduleSnapshotSync() // the auto-update reads the source from it
		}
	}
}
