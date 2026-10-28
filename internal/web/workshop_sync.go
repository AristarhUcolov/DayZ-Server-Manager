// Copyright (c) 2026 Aristarh Ucolov.
package web

// The "Steam Workshop" mod source: the panel's SteamCMD is the source. Its
// download folder is what the Mods page installs from, and "Install & update
// mods", the update check and the auto-update before restarts ask the
// Workshop which installed mods are newer there, download those with
// SteamCMD and copy them into the server.

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
)

// wsRow is one installed Workshop mod compared with the Workshop.
type wsRow struct {
	ID      string `json:"id"`
	Mod     string `json:"mod"`
	Title   string `json:"title"`
	Size    int64  `json:"size"`
	Updated int64  `json:"updated"`
	Local   int64  `json:"local"`
	Newer   bool   `json:"newer"`
}

// workshopRows compares this server's installed Workshop mods with the
// Workshop (one request for all of them).
func (h *handlers) workshopRows(ctx context.Context) ([]wsRow, error) {
	list, _ := mods.List(h.app.ServerDir, "")
	type local struct {
		name string
		at   time.Time
	}
	byID := map[string]local{}
	var ids []string
	for _, m := range list {
		if !m.InstalledInServer || m.PublishedID == "" {
			continue
		}
		at := m.ServerModifiedAt
		if t := mods.MetaTime(m.MetaTimestamp); !t.IsZero() {
			at = t
		}
		byID[m.PublishedID] = local{m.Name, at}
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
		r := wsRow{ID: id, Mod: l.name, Title: m.Title, Size: m.FileSize, Updated: m.TimeUpdated.Unix(), Local: l.at.Unix()}
		// A minute of slack: the copy's time is when it was written.
		r.Newer = !l.at.IsZero() && m.TimeUpdated.After(l.at.Add(time.Minute))
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Mod) < strings.ToLower(out[j].Mod) })
	return out, nil
}

// workshopOutdated counts the installed Workshop mods that are newer on the
// Workshop (the auto-update check of the "steamcmd" source).
func (h *handlers) workshopOutdated(ctx context.Context) (int, error) {
	rows, err := h.workshopRows(ctx)
	n := 0
	for _, r := range rows {
		if r.Newer {
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
	var ids []string
	for _, r := range rows {
		if r.Newer {
			ids = append(ids, r.ID)
			if job != nil {
				st.mu.Lock()
				job.Items = append(job.Items, wsItem{ID: r.ID, Title: r.Title, Size: r.Size, State: "queued", Mod: r.Mod})
				st.mu.Unlock()
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	h.app.Log.Printf("SteamCMD: updating %d Workshop mod(s) as %s", len(ids), user)
	var stop chan struct{}
	if job != nil {
		stop = make(chan struct{})
		go h.trackProgress(st, job, stop)
	}
	res, login, derr := st.tool.Download(ctx, user, ids)
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
	ok := map[string]bool{}
	for _, r := range res {
		ok[r.ID] = r.OK
	}
	var updated []string
	content := st.tool.ContentDir()
	for i, id := range ids {
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
		name := mods.SourceName(content, id)
		if name == "" {
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
			ierr = mods.Install(h.app.ServerDir, content, name)
		} else {
			h.app.WriteMu.Lock()
			ierr = mods.Install(h.app.ServerDir, content, name)
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
