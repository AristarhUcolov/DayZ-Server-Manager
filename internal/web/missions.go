// Copyright (c) 2026 Aristarh Ucolov.
//
// Missions (maps). A community map is two things: the map mod (@DeerIsle,
// @Namalsk…) and its mission folder, which has to sit in mpmissions/ and be
// named in serverDZ.cfg's template. Many map mods ship that mission inside the
// mod; others publish it as a .zip on GitHub. Admins used to copy it over by
// hand and, when dropping a map, forget the leftover mission. This page finds
// missions inside installed mods, installs them (or a .zip) into mpmissions,
// activates them, and removes them to a recoverable trash — optionally
// together with the map mod.
package web

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
)

// isMissionDir recognises a DayZ mission folder by its central-economy files.
func isMissionDir(dir string) bool {
	has := func(rel string) bool {
		st, err := os.Stat(filepath.Join(dir, rel))
		return err == nil && !st.IsDir()
	}
	if has("cfgeconomycore.xml") {
		return true
	}
	// init.c plus either the economy database or a mission-style folder name
	// ("empty.deerisle") — some map missions ship without db/.
	return has("init.c") && (has(filepath.Join("db", "types.xml")) || has(filepath.Join("db", "events.xml")) ||
		missionNameRe.MatchString(filepath.Base(dir)))
}

// findMissions walks root (bounded depth) for mission folders. It never
// descends into a found mission, into addons (PBOs) or keys.
func findMissions(root string, maxDepth int) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			st, err := os.Stat(p) // follows junctions
			if err != nil || !st.IsDir() {
				continue
			}
			low := strings.ToLower(e.Name())
			if low == "addons" || low == "keys" || strings.HasPrefix(low, ".") {
				continue
			}
			if isMissionDir(p) {
				out = append(out, p)
				continue
			}
			walk(p, depth+1)
		}
	}
	walk(root, 0)
	return out
}

func (h *handlers) mpmissionsDir() string { return filepath.Join(h.app.ServerDir, "mpmissions") }

// missionOrigins maps an installed mission name → the mod it came from, so the
// mission can be offered for removal when the map mod is uninstalled.
func (h *handlers) missionOriginsPath() string {
	return filepath.Join(h.app.ManagerDir, "mission-origins.json")
}

func (h *handlers) loadMissionOrigins() map[string]string {
	out := map[string]string{}
	if data, err := os.ReadFile(h.missionOriginsPath()); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func (h *handlers) saveMissionOrigins(m map[string]string) {
	if data, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = writeFileAtomic(h.missionOriginsPath(), data)
	}
}

// safeMissionName keeps a mission folder name to one plain path segment.
// Trailing dots/spaces are refused too: Windows strips them, so "..." would
// resolve to mpmissions itself and "foo." would alias "foo".
func safeMissionName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.TrimRight(s, ". ") != s || strings.ContainsAny(s, `/\:*?"<>|`) {
		return "", fmt.Errorf("invalid mission name %q", s)
	}
	return s, nil
}

func dirStats(dir string) (size int64, files int) {
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			size += info.Size()
			files++
		}
		return nil
	})
	return
}

type missionRow struct {
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	World     string `json:"world"`
	Official  bool   `json:"official"`
	Size      int64  `json:"size"`
	Files     int    `json:"files"`
	Valid     bool   `json:"valid"` // looks like a mission (economy files present)
	FromMod   string `json:"fromMod,omitempty"`
	ModActive bool   `json:"modActive"`
	WorldKey  string `json:"worldKey"`  // map key (schematic / map image lookup)
	WorldSize int    `json:"worldSize"` // edge length in metres
}

func (h *handlers) missionsFull(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.mpmissionsDir())
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	active, _ := h.missionTemplate()
	origins := h.loadMissionOrigins()
	cfg := h.app.Cfg()
	activeMods := map[string]bool{}
	for _, m := range append(append([]string(nil), cfg.Mods...), cfg.ServerMods...) {
		activeMods[strings.ToLower(m)] = true
	}
	out := []missionRow{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(h.mpmissionsDir(), e.Name())
		size, files := dirStats(p)
		wi := worldFromTemplate(e.Name())
		row := missionRow{Name: e.Name(), Active: strings.EqualFold(e.Name(), active), World: wi.Name,
			Official: wi.Official, Size: size, Files: files, Valid: isMissionDir(p), FromMod: origins[e.Name()],
			WorldKey: wi.Key, WorldSize: int(wi.Size)}
		if row.FromMod != "" {
			row.ModActive = activeMods[strings.ToLower(row.FromMod)]
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, map[string]interface{}{"missions": out, "active": active, "dir": h.mpmissionsDir()})
}

type availableMission struct {
	Mod       string `json:"mod"`
	Path      string `json:"path"`            // folder (or .zip) relative to the mod folder
	Zip       bool   `json:"zip,omitempty"`   // Path is an archive holding the mission
	Inner     string `json:"inner,omitempty"` // mission folder inside the archive
	Name      string `json:"name"`            // mission folder name
	World     string `json:"world"`
	Installed bool   `json:"installed"`
	Where     string `json:"where"` // "server" | "source"
}

// missionsAvailable lists mission folders shipped inside installed mods (and
// mods in the mod source), so a map's mission is one click from mpmissions.
func (h *handlers) missionsAvailable(w http.ResponseWriter, r *http.Request) {
	installed := map[string]bool{}
	if entries, err := os.ReadDir(h.mpmissionsDir()); err == nil {
		for _, e := range entries {
			installed[strings.ToLower(e.Name())] = true
		}
	}
	seen := map[string]bool{}
	var out []availableMission
	scan := func(modName, modDir, where string) {
		for _, ms := range scanMissionSources(modDir, 5) {
			at := ms.Dir
			if ms.Zip != "" {
				at = ms.Zip
			}
			rel, _ := filepath.Rel(modDir, at)
			key := strings.ToLower(modName + "|" + rel + "|" + ms.Inner)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, availableMission{Mod: modName, Path: filepath.ToSlash(rel), Zip: ms.Zip != "", Inner: ms.Inner,
				Name: ms.Name, World: worldFromTemplate(ms.Name).Name, Installed: installed[strings.ToLower(ms.Name)], Where: where})
		}
	}
	if list, err := mods.List(h.app.ServerDir, h.app.Cfg().ModSourceDir()); err == nil {
		for _, m := range list {
			if m.InstalledInServer {
				scan(m.Name, m.ServerPath, "server")
			} else if m.WorkshopPath != "" {
				scan(m.Name, m.WorkshopPath, "source")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod+out[i].Name < out[j].Mod+out[j].Name })
	if out == nil {
		out = []availableMission{}
	}
	writeJSON(w, map[string]interface{}{"missions": out})
}

// resolveModMission maps (mod, rel path[, archive inner folder]) to a mission
// source inside that mod.
func (h *handlers) resolveModMission(mod, rel string, isZip bool, inner string) (missionSource, error) {
	if !isZip {
		dir, err := h.resolveModMissionDir(mod, rel)
		return missionSource{Dir: dir, Name: filepath.Base(dir)}, err
	}
	base, err := h.modBase(mod)
	if err != nil {
		return missionSource{}, err
	}
	p := filepath.Clean(filepath.Join(base, filepath.FromSlash(rel)))
	if !withinAny(p, []string{base}) || !strings.EqualFold(filepath.Ext(p), ".zip") {
		return missionSource{}, errors.New("not a mission archive")
	}
	for _, d := range zipMissionDirs(p) {
		if d == inner {
			name := path.Base(d)
			if d == "" {
				name = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			}
			return missionSource{Zip: p, Inner: d, Name: name}, nil
		}
	}
	return missionSource{}, errors.New("no such mission in the archive")
}

// modBase is the folder of an installed mod, or of the mod in the mod source.
func (h *handlers) modBase(mod string) (string, error) {
	if mod == "" || strings.ContainsAny(mod, `/\`) {
		return "", errors.New("invalid mod")
	}
	base := filepath.Join(h.app.ServerDir, mod)
	if _, err := os.Stat(base); err != nil {
		src, err2 := mods.SourcePath(h.app.Cfg().ModSourceDir(), mod)
		if err2 != nil {
			return "", fmt.Errorf("mod %s is not installed", mod)
		}
		base = src
	}
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	return base, nil
}

// resolveModMissionDir maps (mod, rel path) to a mission folder on disk.
func (h *handlers) resolveModMissionDir(mod, rel string) (string, error) {
	base, err := h.modBase(mod)
	if err != nil {
		return "", err
	}
	p := filepath.Clean(filepath.Join(base, filepath.FromSlash(rel)))
	if !withinAny(p, []string{base}) || !isMissionDir(p) {
		return "", errors.New("not a mission folder")
	}
	return p, nil
}

// installMission copies src into mpmissions/name; with overwrite an existing
// folder is moved to the trash first (never deleted outright).
func (h *handlers) installMission(src, name string, overwrite bool) error {
	dst := filepath.Join(h.mpmissionsDir(), name)
	if _, err := os.Stat(dst); err == nil {
		if !overwrite {
			return errMissionExists
		}
		if _, err := h.trashMission(name); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(h.mpmissionsDir(), 0o755); err != nil {
		return err
	}
	if err := copyDirTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return nil
}

var errMissionExists = errors.New("a mission with this name already exists")

func (h *handlers) activateMission(name string) error {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return err
	}
	if !cfg.SetMissionTemplate(name) {
		return errors.New("no class Missions block found in server.cfg")
	}
	return cfg.Save(cfgPath)
}

func (h *handlers) missionsInstall(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mod       string `json:"mod"`
		Path      string `json:"path"`
		Zip       bool   `json:"zip"`
		Inner     string `json:"inner"`
		Name      string `json:"name"`
		Activate  bool   `json:"activate"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, err := h.resolveModMission(req.Mod, req.Path, req.Zip, req.Inner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := req.Name
	if name == "" {
		name = src.Name
	}
	if name, err = safeMissionName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.installMissionSource(src, name, req.Overwrite); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errMissionExists) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	origins := h.loadMissionOrigins()
	origins[name] = req.Mod
	h.saveMissionOrigins(origins)
	if req.Activate {
		if err := h.activateMission(name); err != nil {
			http.Error(w, "installed, but activating failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"status": "installed", "name": name, "activated": req.Activate})
}

const maxMissionZip = 512 << 20 // 512 MB — missions with big mapgroup files are ~50 MB

// missionsImport installs a mission from an uploaded .zip (e.g. a GitHub
// release): the archive is unpacked to a temp dir, the mission folder inside is
// located, then installed like any other.
func (h *handlers) missionsImport(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	r.Body = http.MaxBytesReader(w, r.Body, maxMissionZip)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("zip")
	if err != nil {
		http.Error(w, "missing zip file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	tmp, err := os.MkdirTemp(h.app.ManagerDir, "mission-import-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmp)
	zipPath := filepath.Join(tmp, "upload.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out.Close()
	extract := filepath.Join(tmp, "x")
	if err := unzipSafe(zipPath, extract, ""); err != nil {
		http.Error(w, "cannot read zip: "+err.Error(), http.StatusBadRequest)
		return
	}
	var src string
	if isMissionDir(extract) {
		src = extract
	} else if found := findMissions(extract, 5); len(found) > 0 {
		src = found[0]
	} else {
		http.Error(w, "no mission folder (cfgeconomycore.xml / init.c + db) found in the zip", http.StatusUnprocessableEntity)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = filepath.Base(src)
		if src == extract {
			name = strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename))
		}
	}
	if name, err = safeMissionName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	overwrite := r.FormValue("overwrite") == "1"
	if err := h.installMission(src, name, overwrite); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errMissionExists) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	activated := r.FormValue("activate") == "1"
	if activated {
		if err := h.activateMission(name); err != nil {
			http.Error(w, "installed, but activating failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"status": "installed", "name": name, "activated": activated})
}

// unzipSafe extracts zipPath into dst, refusing entries that would escape it.
// A non-empty prefix (slash path) extracts only that folder of the archive.
func unzipSafe(zipPath, dst, prefix string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		if prefix != "" {
			name := strings.TrimPrefix(path.Clean("/"+f.Name), "/")
			if name != prefix && !strings.HasPrefix(name, prefix+"/") {
				continue
			}
		}
		p := filepath.Join(dst, filepath.FromSlash(f.Name))
		if !withinAny(filepath.Clean(p), []string{dst}) {
			return fmt.Errorf("unsafe path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > 4<<30 {
			return errors.New("zip expands beyond 4 GB")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(p)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *handlers) missionTrashDir() string {
	return filepath.Join(h.app.ManagerDir, "trash", "missions")
}

// trashMission moves a mission folder to .dayz-manager/trash/missions so a
// deletion can be undone by moving it back.
func (h *handlers) trashMission(name string) (string, error) {
	src := filepath.Join(h.mpmissionsDir(), name)
	dir := h.missionTrashDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405"))
	if err := os.Rename(src, dst); err != nil {
		// Different volume: copy then remove.
		if err := copyDirTree(src, dst); err != nil {
			return "", err
		}
		if err := os.RemoveAll(src); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func (h *handlers) missionsDelete(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, err := safeMissionName(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if active, _ := h.missionTemplate(); strings.EqualFold(active, name) {
		http.Error(w, "this is the active mission — activate another one first", http.StatusConflict)
		return
	}
	if _, err := os.Stat(filepath.Join(h.mpmissionsDir(), name)); err != nil {
		http.Error(w, "mission not found", http.StatusNotFound)
		return
	}
	dst, err := h.trashMission(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	origins := h.loadMissionOrigins()
	delete(origins, name)
	h.saveMissionOrigins(origins)
	writeJSON(w, map[string]interface{}{"status": "deleted", "trash": dst, "entry": filepath.Base(dst)})
}

// trashStampRe splits a trash folder name into the mission name and the
// moment it was deleted (trashMission appends -YYYYMMDD-HHMMSS).
var trashStampRe = regexp.MustCompile(`^(.+)-(\d{8}-\d{6})$`)

func splitTrashEntry(entry string) (string, time.Time) {
	if m := trashStampRe.FindStringSubmatch(entry); m != nil {
		if at, err := time.ParseInLocation("20060102-150405", m[2], time.Local); err == nil {
			return m[1], at
		}
	}
	return entry, time.Time{}
}

// trashEntryPath resolves a trash folder name to its path, refusing anything
// that is not one plain folder name inside the trash.
func (h *handlers) trashEntryPath(entry string) (string, error) {
	entry, err := safeMissionName(entry)
	if err != nil || filepath.Base(entry) != entry {
		return "", errors.New("invalid trash entry")
	}
	p := filepath.Join(h.missionTrashDir(), entry)
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return "", errors.New("not in the trash")
	}
	return p, nil
}

// missionsTrash lists deleted missions, newest first, so one can be put back.
func (h *handlers) missionsTrash(w http.ResponseWriter, r *http.Request) {
	type trashed struct {
		Entry  string `json:"entry"` // folder name in the trash
		Name   string `json:"name"`  // the mission's own name
		At     string `json:"at,omitempty"`
		Size   int64  `json:"size"`
		Taken  bool   `json:"taken"` // mpmissions has a mission of that name again
		sortAt time.Time
	}
	out := []trashed{}
	entries, _ := os.ReadDir(h.missionTrashDir())
	for _, e := range entries {
		p := filepath.Join(h.missionTrashDir(), e.Name())
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		name, at := splitTrashEntry(e.Name())
		size, _ := dirStats(p)
		row := trashed{Entry: e.Name(), Name: name, Size: size, sortAt: at}
		if !at.IsZero() {
			row.At = at.Format(time.RFC3339)
		}
		if _, err := os.Stat(filepath.Join(h.mpmissionsDir(), name)); err == nil {
			row.Taken = true
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].sortAt.Equal(out[j].sortAt) {
			return out[i].sortAt.After(out[j].sortAt)
		}
		return out[i].Entry < out[j].Entry
	})
	writeJSON(w, map[string]interface{}{"missions": out, "dir": h.missionTrashDir()})
}

// missionsRestore moves a deleted mission from the trash back into
// mpmissions under its own name. It never replaces a mission that is there.
func (h *handlers) missionsRestore(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Entry string `json:"entry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, err := h.trashEntryPath(req.Entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	name, _ := splitTrashEntry(filepath.Base(src))
	if name, err = safeMissionName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dst := filepath.Join(h.mpmissionsDir(), name)
	if _, err := os.Stat(dst); err == nil {
		http.Error(w, "mpmissions already has a mission named "+name+" — rename or delete it first", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(h.mpmissionsDir(), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(src, dst); err != nil {
		// Different volume: copy then remove.
		if err := copyDirTree(src, dst); err != nil {
			_ = os.RemoveAll(dst)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = os.RemoveAll(src)
	}
	writeJSON(w, map[string]interface{}{"status": "restored", "name": name})
}

// missionsTrashDelete removes a deleted mission for good.
func (h *handlers) missionsTrashDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Entry string `json:"entry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := h.trashEntryPath(req.Entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := os.RemoveAll(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "deleted"})
}

func (h *handlers) missionsActivate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, err := safeMissionName(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(filepath.Join(h.mpmissionsDir(), name)); err != nil {
		http.Error(w, "mission not found", http.StatusNotFound)
		return
	}
	if err := h.activateMission(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "activated", "name": name})
}

// missionsLinked lists installed missions that came from (or ship inside) a
// mod — asked before uninstalling a map mod so its mission can go too.
func (h *handlers) missionsLinked(w http.ResponseWriter, r *http.Request) {
	mod := r.URL.Query().Get("mod")
	active, _ := h.missionTemplate()
	set := map[string]bool{}
	for name, from := range h.loadMissionOrigins() {
		if strings.EqualFold(from, mod) {
			set[name] = true
		}
	}
	if !strings.ContainsAny(mod, `/\`) && mod != "" {
		for _, ms := range scanMissionSources(filepath.Join(h.app.ServerDir, mod), 5) {
			name := ms.Name
			if st, err := os.Stat(filepath.Join(h.mpmissionsDir(), name)); err == nil && st.IsDir() {
				set[name] = true
			}
		}
	}
	type linked struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	out := []linked{}
	for n := range set {
		out = append(out, linked{Name: n, Active: strings.EqualFold(n, active)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, map[string]interface{}{"missions": out})
}
