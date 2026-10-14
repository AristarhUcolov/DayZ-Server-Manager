// Copyright (c) 2026 Aristarh Ucolov.
//
// Config profiles — named snapshots of the game configuration.
// A profile is a zip stored under .dayz-manager/config-profiles/<slug>.zip,
// holding the files backupItems() knows about (serverDZ.cfg, the mission
// economy files, custom loot files, manager.json) minus BattlEye's config.
//
// Unlike the automatic History backups, profiles are user-named and meant to be
// switched between deliberately — e.g. "Weekend PvP" vs "Weekday PvE". Applying
// one restores every game file (a .bak of each current file is kept first) and,
// like every other config write, requires the server to be stopped. Of
// manager.json only the launch settings are applied (see mergeProfileLaunch):
// schedules, Discord, RCon and panel access stay as they are live.
package web

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/util"
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

// profileSlugFor picks the file a profile named name is saved to: the newest
// existing profile with that name (any case), so saving again updates it, or
// else a slug no other profile uses. slugify keeps only a–z and 0–9, so «PvP
// выходные» and «PvP будни» both came out as "pvp" (the second silently
// replaced the first), and «Выходные» got a new timestamp slug on every save.
func (h *handlers) profileSlugFor(name string) string {
	dir := h.cfgProfilesDir()
	entries, _ := os.ReadDir(dir)
	taken := map[string]bool{}
	match, matchCreated := "", ""
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		taken[strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))] = true
		p, err := h.readProfileMeta(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.Name), name) && (match == "" || p.Created > matchCreated) {
			match, matchCreated = p.Slug, p.Created
		}
	}
	if match != "" {
		return match
	}
	base := slugify(name)
	if base != "" && !taken[base] {
		return base
	}
	if base == "" {
		base = "profile"
	}
	sum := sha1.Sum([]byte(strings.ToLower(name)))
	tag := hex.EncodeToString(sum[:4])
	slug := base + "-" + tag
	for i := 2; taken[slug]; i++ {
		slug = fmt.Sprintf("%s-%s-%d", base, tag, i)
	}
	return slug
}

// cfgProfileSave snapshots the current config set into a named profile. Saving
// with an existing name updates that profile, and so does passing its slug
// (the list's "Update from current").
func (h *handlers) cfgProfileSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
		Slug string `json:"slug"`
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
	dir := h.cfgProfilesDir()
	slug := ""
	if s := strings.TrimSpace(req.Slug); s != "" {
		slug = filepath.Base(s)
		if _, err := os.Stat(filepath.Join(dir, slug+".zip")); err != nil {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
	} else {
		slug = h.profileSlugFor(name)
	}
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
		if profileSkips(arcName) {
			continue
		}
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
	restored, skipped, err := h.applyProfileBySlug(req.Slug)
	if err != nil {
		code := http.StatusInternalServerError
		if os.IsNotExist(err) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, map[string]interface{}{"restored": restored, "skipped": skipped, "count": len(restored)})
}

// applyProfileBySlug restores every game file in a saved profile over the live
// config and applies its launch settings. The caller must ensure the server is
// stopped (the HTTP path via acquireWrite, the scheduler via the restart
// down-window). Also used by scheduled profile swaps.
func (h *handlers) applyProfileBySlug(slug string) (restored, skipped []string, err error) {
	path := filepath.Join(h.cfgProfilesDir(), filepath.Base(slug)+".zip")
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()

	targets := map[string]string{}
	for _, it := range h.backupItems() {
		targets[it[1]] = it[0]
	}
	restored = []string{}
	skipped = []string{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(zf.Name, `\`, `/`)
		if name == "manifest.json" {
			continue
		}
		if profileSkips(name) {
			skipped = append(skipped, name)
			continue
		}
		if name == "manager.json" {
			raw, err := readZipEntry(zf, 4<<20)
			if err == nil {
				err = h.app.MutateConfig(func(c *config.Manager) error { return mergeProfileLaunch(c, raw) })
			}
			if err != nil {
				return restored, skipped, fmt.Errorf("apply launch settings: %w", err)
			}
			restored = append(restored, name)
			continue
		}
		abs, ok := targets[name]
		if !ok {
			skipped = append(skipped, name)
			continue
		}
		if err := restoreOne(zf, abs); err != nil {
			return restored, skipped, fmt.Errorf("restore %s: %w", name, err)
		}
		restored = append(restored, name)
	}
	return restored, skipped, nil
}

// profileSkips names the archive entries a profile never applies: BattlEye's
// config holds the RCon port and password, which belong to the panel like its
// other RCon settings — an older copy put back an old password. Profiles saved
// before this still carry it.
func profileSkips(name string) bool {
	return strings.HasPrefix(name, "battleye/")
}

// mergeProfileLaunch copies the launch settings stored in a profile's
// manager.json into dst: the mod lists, the server.cfg file and the launch
// flags. Everything else in manager.json — scheduled restarts and profile
// swaps, announcements, Discord, RCon, panel access, backups — stays as it is
// live. Restoring the whole file rolled back the very schedule that fired the
// swap (a profile is saved before any schedule exists), so the swap back
// never came, and Discord and RCon settings reverted with it.
func mergeProfileLaunch(dst *config.Manager, raw []byte) error {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	// Starting from the live values keeps them for keys an older or
	// hand-edited file lacks.
	src := dst.Clone()
	if err := json.Unmarshal(raw, &src); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) == nil {
		if _, ok := keys["extraLaunchParams"]; !ok {
			src.ExtraLaunchParams = "" // the file omits it when empty
		}
	}
	if src.Mods == nil {
		src.Mods = []string{}
	}
	if src.ServerMods == nil {
		src.ServerMods = []string{}
	}
	dst.Mods, dst.ServerMods = src.Mods, src.ServerMods
	if src.ServerCfg != "" {
		dst.ServerCfg = src.ServerCfg
	}
	dst.CPUCount, dst.LimitFPS, dst.ExtraLaunchParams = src.CPUCount, src.LimitFPS, src.ExtraLaunchParams
	dst.DoLogs, dst.AdminLog, dst.NetLog = src.DoLogs, src.AdminLog, src.NetLog
	dst.FreezeCheck, dst.FilePatching = src.FreezeCheck, src.FilePatching
	return nil
}

// readZipEntry reads one archive entry, refusing anything larger than max.
func readZipEntry(zf *zip.File, max int64) ([]byte, error) {
	if zf.UncompressedSize64 > uint64(max) {
		return nil, fmt.Errorf("%s is too large", zf.Name)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max))
}

// cfgProfileDiff tells what applying a profile would change, so the confirm
// can list it instead of a blind "every config is overwritten": each file whose
// content differs (with changed line counts), files the profile would create,
// and the launch settings (mods, flags) that would switch.
func (h *handlers) cfgProfileDiff(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if slug == "" {
		http.Error(w, "profile slug required", http.StatusBadRequest)
		return
	}
	zr, err := zip.OpenReader(filepath.Join(h.cfgProfilesDir(), filepath.Base(slug)+".zip"))
	if err != nil {
		code := http.StatusInternalServerError
		if os.IsNotExist(err) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer zr.Close()

	type fileChange struct {
		Path    string `json:"path"`
		Status  string `json:"status"` // "changed" | "new"
		Added   int    `json:"added,omitempty"`
		Removed int    `json:"removed,omitempty"`
	}
	type launchChange struct {
		Key  string `json:"key"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	targets := map[string]string{}
	for _, it := range h.backupItems() {
		targets[it[1]] = it[0]
	}
	files := []fileChange{}
	launch := []launchChange{}
	same := 0
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, `\`, `/`)
		if zf.FileInfo().IsDir() || name == "manifest.json" || profileSkips(name) {
			continue
		}
		if name == "manager.json" {
			raw, err := readZipEntry(zf, 4<<20)
			if err != nil {
				continue
			}
			live := h.app.Cfg()
			next := live.Clone()
			if mergeProfileLaunch(&next, raw) != nil {
				continue
			}
			add := func(key, from, to string) {
				if from != to {
					launch = append(launch, launchChange{key, from, to})
				}
			}
			add("mods", strings.Join(live.Mods, ";"), strings.Join(next.Mods, ";"))
			add("serverMods", strings.Join(live.ServerMods, ";"), strings.Join(next.ServerMods, ";"))
			add("serverCfg", live.ServerCfg, next.ServerCfg)
			add("cpuCount", strconv.Itoa(live.CPUCount), strconv.Itoa(next.CPUCount))
			add("limitFPS", strconv.Itoa(live.LimitFPS), strconv.Itoa(next.LimitFPS))
			add("extraLaunchParams", live.ExtraLaunchParams, next.ExtraLaunchParams)
			for _, f := range []struct {
				key      string
				from, to bool
			}{
				{"doLogs", live.DoLogs, next.DoLogs}, {"adminLog", live.AdminLog, next.AdminLog},
				{"netLog", live.NetLog, next.NetLog}, {"freezeCheck", live.FreezeCheck, next.FreezeCheck},
				{"filePatching", live.FilePatching, next.FilePatching},
			} {
				add(f.key, strconv.FormatBool(f.from), strconv.FormatBool(f.to))
			}
			continue
		}
		abs, ok := targets[name]
		if !ok {
			continue
		}
		prof, err := readZipEntry(zf, 16<<20)
		if err != nil {
			continue
		}
		cur, err := os.ReadFile(abs)
		if err != nil {
			if os.IsNotExist(err) {
				files = append(files, fileChange{Path: name, Status: "new"})
			}
			continue
		}
		if bytes.Equal(cur, prof) {
			same++
			continue
		}
		// Old = the live file, new = the profile: "+" is what applying brings.
		d := util.DiffLines(string(cur), string(prof), 0)
		files = append(files, fileChange{Path: name, Status: "changed", Added: d.Added, Removed: d.Removed})
	}
	writeJSON(w, map[string]interface{}{"files": files, "launch": launch, "same": same})
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
