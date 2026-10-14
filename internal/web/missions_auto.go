// Copyright (c) 2026 Aristarh Ucolov.
//
// Map missions that travel inside mods. A map mod may carry its mission as a
// folder (Extras/, Mission/, mpmissions/…) or as a .zip next to its addons.
// Installing, updating or syncing a mod copies any such mission into
// mpmissions automatically (never overwriting one already there); the Mods and
// Missions pages list the ones still waiting; and a mission folder can be
// imported straight from disk with the browser's folder picker.
package web

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// missionSource is a mission found inside a mod or an upload: a folder on
// disk, or a folder inside a .zip archive.
type missionSource struct {
	Dir   string // mission folder on disk; "" when packed in Zip
	Zip   string // archive holding the mission
	Inner string // mission folder inside the archive ("" = archive root)
	Name  string // folder name it gets in mpmissions
}

// missionNameRe matches mission folder names: "dayzOffline.chernarusplus",
// "regular.namalsk", "empty.deerisle" — a template and a world, dot-separated.
var missionNameRe = regexp.MustCompile(`^[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+$`)

// maxScannedZip bounds the archives opened while looking for missions (their
// directory is read, not their contents — this only skips absurd files).
const maxScannedZip = 2 << 30

// scanMissionSources walks root (bounded depth) for mission folders and for
// .zip archives that contain one. addons/ and keys/ are never entered.
func scanMissionSources(root string, maxDepth int) []missionSource {
	var out []missionSource
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
			if err != nil {
				continue
			}
			low := strings.ToLower(e.Name())
			if !st.IsDir() {
				if strings.HasSuffix(low, ".zip") && st.Size() < maxScannedZip {
					for _, inner := range zipMissionDirs(p) {
						name := path.Base(inner)
						if inner == "" {
							name = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
						}
						out = append(out, missionSource{Zip: p, Inner: inner, Name: name})
					}
				}
				continue
			}
			if low == "addons" || low == "keys" || strings.HasPrefix(low, ".") {
				continue
			}
			if isMissionDir(p) {
				out = append(out, missionSource{Dir: p, Name: e.Name()})
				continue
			}
			walk(p, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// zipMissionDirs lists the mission folders inside an archive (slash paths,
// "" for the archive root), without extracting anything.
func zipMissionDirs(zipPath string) []string {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil
	}
	defer zr.Close()
	files := map[string]bool{}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			files[strings.ToLower(strings.TrimPrefix(path.Clean("/"+f.Name), "/"))] = true
		}
	}
	join := func(dir, name string) string {
		if dir == "" {
			return name
		}
		return dir + "/" + name
	}
	found := map[string]bool{}
	for f := range files {
		dir, base := path.Split(f)
		dir = strings.TrimSuffix(dir, "/")
		switch base {
		case "cfgeconomycore.xml":
			found[dir] = true
		case "init.c":
			if files[join(dir, "db/types.xml")] || files[join(dir, "db/events.xml")] || missionNameRe.MatchString(path.Base(dir)) {
				found[dir] = true
			}
		}
	}
	// Keep the original case of the folder names and drop missions nested
	// inside another found one.
	var out []string
	for _, f := range zr.File {
		d := strings.TrimSuffix(strings.TrimPrefix(path.Clean("/"+f.Name), "/"), "/")
		if f.FileInfo().IsDir() {
			continue
		}
		d = path.Dir(d)
		if d == "." {
			d = ""
		}
		if found[strings.ToLower(d)] {
			delete(found, strings.ToLower(d))
			out = append(out, d)
		}
	}
	sort.Strings(out)
	var outer []string
	for _, d := range out {
		nested := false
		for _, o := range outer {
			if o == "" || strings.HasPrefix(d, o+"/") {
				nested = true
				break
			}
		}
		if !nested {
			outer = append(outer, d)
		}
	}
	return outer
}

// installMissionSource copies a found mission into mpmissions/name.
func (h *handlers) installMissionSource(ms missionSource, name string, overwrite bool) error {
	if ms.Zip == "" {
		return h.installMission(ms.Dir, name, overwrite)
	}
	tmp, err := os.MkdirTemp(h.app.ManagerDir, "mission-zip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := unzipSafe(ms.Zip, tmp, ms.Inner); err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(ms.Zip), err)
	}
	src := filepath.Join(tmp, filepath.FromSlash(ms.Inner))
	if !isMissionDir(src) {
		return errors.New("no mission folder found in " + filepath.Base(ms.Zip))
	}
	return h.installMission(src, name, overwrite)
}

// autoMission reports what happened to one mission found inside a mod.
type autoMission struct {
	Mod       string `json:"mod"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"` // copied into mpmissions just now
	Existed   bool   `json:"existed"`   // already there — left untouched
	Error     string `json:"error,omitempty"`
}

// autoInstallModMissions copies the missions shipped inside the given
// (installed) mods into mpmissions. Existing missions are never overwritten —
// an admin's edited copy matters more than the mod's pristine one — and
// nothing is activated: switching maps stays a deliberate step.
func (h *handlers) autoInstallModMissions(modNames []string) []autoMission {
	if !h.app.Cfg().AutoInstallMissions || len(modNames) == 0 {
		return nil
	}
	origins := h.loadMissionOrigins()
	changed := false
	var out []autoMission
	for _, mod := range modNames {
		if mod == "" || strings.ContainsAny(mod, `/\`) {
			continue
		}
		for _, ms := range scanMissionSources(filepath.Join(h.app.ServerDir, mod), 5) {
			name, err := safeMissionName(ms.Name)
			if err != nil {
				continue
			}
			res := autoMission{Mod: mod, Name: name}
			if _, err := os.Stat(filepath.Join(h.mpmissionsDir(), name)); err == nil {
				res.Existed = true
			} else if err := h.installMissionSource(ms, name, false); err != nil {
				res.Error = err.Error()
			} else {
				res.Installed = true
				origins[name] = mod
				changed = true
				h.app.Log.Printf("mission %s copied from %s into mpmissions", name, mod)
			}
			out = append(out, res)
		}
	}
	if changed {
		h.saveMissionOrigins(origins)
	}
	return out
}

// missionsImportFolder installs a mission uploaded as a folder (the browser's
// folder picker): every part is a file whose filename is its path relative to
// the picked folder. Works from any machine that can open the panel.
func (h *handlers) missionsImportFolder(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	r.Body = http.MaxBytesReader(w, r.Body, maxMissionZip)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tmp, err := os.MkdirTemp(h.app.ManagerDir, "mission-folder-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmp)
	root := filepath.Join(tmp, "x")
	fields := map[string]string{}
	files := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if part.FormName() != "files" {
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			fields[part.FormName()] = strings.TrimSpace(string(b))
			part.Close()
			continue
		}
		// Part.FileName() strips directories; the relative path is the point.
		_, params, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		rel := strings.ReplaceAll(params["filename"], `\`, "/")
		p := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		if rel == "" || !withinAny(p, []string{root}) || p == root {
			part.Close()
			http.Error(w, "unsafe file path: "+rel, http.StatusBadRequest)
			return
		}
		if files++; files > 50000 {
			part.Close()
			http.Error(w, "too many files", http.StatusRequestEntityTooLarge)
			return
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			part.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		f, err := os.Create(p)
		if err != nil {
			part.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(f, part)
		f.Close()
		part.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	var src string
	if isMissionDir(root) {
		src = root
	} else if found := findMissions(root, 5); len(found) > 0 {
		src = found[0]
	} else {
		http.Error(w, "no mission folder (cfgeconomycore.xml / init.c + db) found in the chosen folder", http.StatusUnprocessableEntity)
		return
	}
	name := fields["name"]
	if name == "" {
		name = filepath.Base(src)
	}
	if name, err = safeMissionName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.installMission(src, name, fields["overwrite"] == "1"); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errMissionExists) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	activated := fields["activate"] == "1"
	if activated {
		if err := h.activateMission(name); err != nil {
			http.Error(w, "installed, but activating failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"status": "installed", "name": name, "activated": activated})
}
