// Copyright (c) 2026 Aristarh Ucolov.
//
// Config profiles — named snapshots of the whole server configuration set.
// A profile is a zip stored under .dayz-manager/config-profiles/<slug>.zip,
// holding exactly the files backupItems() knows about (serverDZ.cfg, the
// mission economy files, BattlEye config, custom loot files, manager.json).
//
// Unlike the automatic History backups, profiles are user-named and meant to be
// switched between deliberately — e.g. "Weekend PvP" vs "Weekday PvE". Applying
// one restores every file (a .bak of each current file is kept first) and, like
// every other config write, requires the server to be stopped.
package web

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type cfgProfile struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Note    string `json:"note,omitempty"`
	Created string `json:"created"`
	Files   int    `json:"files"`
	Size    int64  `json:"size"`
}

func (h *handlers) cfgProfilesDir() string {
	return filepath.Join(h.app.ManagerDir, "config-profiles")
}

// slugify turns a display name into a safe file base: lowercase, alnum runs
// joined by '-', everything else dropped. Non-Latin names collapse to empty, so
// the caller falls back to a timestamp slug.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

// cfgProfilesList enumerates saved profiles, newest first.
func (h *handlers) cfgProfilesList(w http.ResponseWriter, r *http.Request) {
	dir := h.cfgProfilesDir()
	entries, _ := os.ReadDir(dir)
	out := []cfgProfile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		p, err := h.readProfileMeta(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	writeJSON(w, map[string]interface{}{"profiles": out})
}

// readProfileMeta opens a profile zip and reads its manifest for display.
func (h *handlers) readProfileMeta(path string) (cfgProfile, error) {
	slug := strings.TrimSuffix(filepath.Base(path), ".zip")
	p := cfgProfile{Slug: slug, Name: slug}
	st, err := os.Stat(path)
	if err != nil {
		return p, err
	}
	p.Size = st.Size()
	p.Created = st.ModTime().UTC().Format(time.RFC3339)
	zr, err := zip.OpenReader(path)
	if err != nil {
		return p, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name == "manifest.json" {
			if rc, err := zf.Open(); err == nil {
				var m struct {
					Name, Note, Created string
				}
				_ = json.NewDecoder(rc).Decode(&m)
				rc.Close()
				if m.Name != "" {
					p.Name = m.Name
				}
				p.Note = m.Note
				if m.Created != "" {
					p.Created = m.Created
				}
			}
			continue
		}
		if !zf.FileInfo().IsDir() {
			p.Files++
		}
	}
	return p, nil
}

// cfgProfileSave snapshots the current config set into a named profile. Saving
// with an existing name overwrites that profile (an intentional "update").
func (h *handlers) cfgProfileSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}
	slug := slugify(name)
	if slug == "" {
		slug = "profile-" + time.Now().Format("20060102-150405")
	}
	dir := h.cfgProfilesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dir, slug+".zip")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.writeProfileZip(f, name, strings.TrimSpace(req.Note)); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, _ := h.readProfileMeta(path)
	writeJSON(w, p)
}

// writeProfileZip writes the backup file set plus a manifest carrying the
// display name and note.
func (h *handlers) writeProfileZip(w io.Writer, name, note string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	files := 0
	for _, it := range h.backupItems() {
		abs, arcName := it[0], it[1]
		f, err := os.Open(abs)
		if err != nil {
			continue
		}
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			f.Close()
			continue
		}
		zf, err := zw.CreateHeader(&zip.FileHeader{Name: arcName, Method: zip.Deflate, Modified: st.ModTime()})
		if err != nil {
			f.Close()
			continue
		}
		_, _ = io.Copy(zf, f)
		f.Close()
		files++
	}
	manifest := map[string]interface{}{
		"app":     h.app.Name,
		"version": h.app.Version,
		"profile": true,
		"name":    name,
		"note":    note,
		"created": time.Now().UTC().Format(time.RFC3339),
		"files":   files,
	}
	if mf, err := zw.Create("manifest.json"); err == nil {
		_ = json.NewEncoder(mf).Encode(manifest)
	}
	return zw.Close()
}

// cfgProfileApply restores every file in a profile over the live config. A .bak
// of each current file is kept first. Requires the server stopped.
func (h *handlers) cfgProfileApply(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Slug) == "" {
		http.Error(w, "profile slug required", http.StatusBadRequest)
		return
	}
	path := filepath.Join(h.cfgProfilesDir(), filepath.Base(req.Slug)+".zip")
	zr, err := zip.OpenReader(path)
	if err != nil {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	defer zr.Close()

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
	for _, n := range restored {
		if n == "manager.json" {
			if err := h.app.ReloadConfig(); err != nil {
				http.Error(w, "reload config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			// A restored manager.json can carry a different schedule / RCon
			// password — refresh the derived state the same way /api/config does.
			h.app.Server.SetScheduleConfig(h.app.Config)
			h.app.SyncBEConfig()
			h.app.ApplyRConConfig()
			break
		}
	}
	writeJSON(w, map[string]interface{}{"restored": restored, "skipped": skipped, "count": len(restored)})
}

// cfgProfileDelete removes a saved profile.
func (h *handlers) cfgProfileDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Slug) == "" {
		http.Error(w, "profile slug required", http.StatusBadRequest)
		return
	}
	path := filepath.Join(h.cfgProfilesDir(), filepath.Base(req.Slug)+".zip")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}
