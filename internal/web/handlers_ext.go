// Copyright (c) 2026 Aristarh Ucolov.
//
// Extra handlers: Workshop collection imports, mod sync-all, BattlEye editor,
// Types bulk-edit, mission central-economy DB editor, scheduled RCon
// announcements. Kept in a separate file to avoid growing handlers.go past
// what the Read tool tolerates in one go.
package web

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"dayzmanager/internal/battleye"
	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// ---------------------------------------------------------------------------
// Mods: sync-all + collection importer.

func (h *handlers) modsSyncAll(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Only []string `json:"only"` // optional filter by @Mod names
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	res, err := mods.SyncAll(h.app.ServerDir, h.app.Config.VanillaDayZPath, req.Only)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (h *handlers) modsCollectionResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Save bool   `json:"save"` // persist to config.WorkshopCollections
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	id, err := mods.ParseCollectionURL(req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	children, err := mods.FetchCollectionChildren(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	res, err := mods.ResolveCollection(r.Context(), h.app.Config.VanillaDayZPath, children)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res.CollectionID = id
	if req.Save {
		_ = h.app.MutateConfig(func(c *config.Manager) error {
			if !contains(c.WorkshopCollections, req.URL) {
				c.WorkshopCollections = append(c.WorkshopCollections, req.URL)
			}
			return nil
		})
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------------------------
// BattlEye editor.

func (h *handlers) beDir() string {
	return battleye.Dir(h.app.ServerDir, h.app.Config.BEPath)
}

func (h *handlers) battleyeList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"dir":   h.beDir(),
		"files": battleye.List(h.beDir()),
	})
}

func (h *handlers) battleyeRead(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	content, err := battleye.Read(h.beDir(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{"name": name, "content": content})
}

func (h *handlers) battleyeWrite(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := battleye.Write(h.beDir(), req.Name, req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// BattlEye bans editor (bans.txt).
//
// Line format: `<GUID|IP> <minutes> <reason>` — minutes 0/perm = permanent.
// Hand-editing this file is the #1 way admins silently break every ban, so the
// panel provides a structured table. After a save, if the server is running,
// we issue RCon `loadBans` so changes apply without a restart.

type banEntry struct {
	ID      string `json:"id"`      // GUID or IP
	Minutes string `json:"minutes"` // raw token: number, "0", "perm", "-1"
	Reason  string `json:"reason"`
}

func (h *handlers) bansPath() string {
	return filepath.Join(h.beDir(), "bans.txt")
}

func parseBans(data string) (bans []banEntry, comments []string) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
			comments = append(comments, line)
			continue
		}
		fields := strings.Fields(trim)
		b := banEntry{ID: fields[0]}
		if len(fields) > 1 {
			b.Minutes = fields[1]
		}
		if len(fields) > 2 {
			b.Reason = strings.Join(fields[2:], " ")
		}
		bans = append(bans, b)
	}
	return
}

func (h *handlers) battleyeBans(w http.ResponseWriter, r *http.Request) {
	path := h.bansPath()
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		bans, _ := parseBans(string(data))
		if bans == nil {
			bans = []banEntry{}
		}
		writeJSON(w, map[string]interface{}{"path": path, "bans": bans})
		return
	}

	// POST: full-list replace. Unlike mission files this is safe while the
	// server runs — BattlEye re-reads bans.txt on `loadBans`.
	var req struct {
		Bans []banEntry `json:"bans"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Preserve any comment lines the admin keeps at the top of the file.
	var comments []string
	if data, err := os.ReadFile(path); err == nil {
		_, comments = parseBans(string(data))
	}
	var b strings.Builder
	for _, c := range comments {
		b.WriteString(c)
		b.WriteString("\r\n")
	}
	for _, ban := range req.Bans {
		id := strings.TrimSpace(ban.ID)
		if id == "" || strings.ContainsAny(id, " \t") {
			continue
		}
		mins := strings.TrimSpace(ban.Minutes)
		if mins == "" {
			mins = "0" // permanent
		}
		reason := strings.ReplaceAll(strings.TrimSpace(ban.Reason), "\n", " ")
		b.WriteString(id)
		b.WriteString(" ")
		b.WriteString(mins)
		if reason != "" {
			b.WriteString(" ")
			b.WriteString(reason)
		}
		b.WriteString("\r\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Apply live when possible.
	reloaded := false
	if h.app.ServerIsRunning() {
		if _, err := h.app.RCon.Command("loadBans"); err == nil {
			reloaded = true
		}
	}
	writeJSON(w, map[string]interface{}{"status": "saved", "reloaded": reloaded, "count": len(req.Bans)})
}

// ---------------------------------------------------------------------------
// Types bulk-edit.

func (h *handlers) typesBulkPatch(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File  string                 `json:"file"`
		Names []string               `json:"names"`
		Patch dztypes.BulkFieldPatch `json:"patch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Names) == 0 {
		http.Error(w, "names required", http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	touched := doc.BulkPatch(req.Names, req.Patch)
	if err := doc.Save(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"touched": touched})
}

// ---------------------------------------------------------------------------
// Mission central-economy DB editor.
//
// Covers the XML files sitting in `<mission>/db/` and the top-level
// `cfgeconomycore.xml` / `cfgspawnabletypes.xml` etc. — the ones most server
// admins hand-tune. Everything is raw text at this layer; the typed editors
// (Types, Events) stay as the higher-level alternative.

var allowedMissionDBFiles = map[string]bool{
	"types.xml":              true,
	"events.xml":             true,
	"economy.xml":            true,
	"globals.xml":            true,
	"messages.xml":           true,
	"cfgeventspawns.xml":     true,
	"cfgplayerspawnpoints.xml": true,
	"cfgweather.xml":         true,
	"cfgeffectarea.json":     true,
	"init.c":                 true,
	"cfgeconomycore.xml":     true,
	"cfgenvironment.xml":     true,
	"cfggameplay.json":       true,
	"cfgrandompresets.xml":   true,
	"cfgspawnabletypes.xml":  true,
	"cfglimitsdefinition.xml": true,
	"cfglimitsdefinitionuser.xml": true,
	"cfgundergroundtriggers.json": true,
	"cfgeventgroups.xml":          true,
	"cfgignorelist.xml":           true,
}

func (h *handlers) missionDir() (string, error) {
	tmpl, err := h.missionTemplate()
	if err != nil {
		return "", err
	}
	return filepath.Join(h.app.ServerDir, "mpmissions", tmpl), nil
}

func (h *handlers) missionDBList(w http.ResponseWriter, r *http.Request) {
	dir, err := h.missionDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	type entry struct {
		Name     string `json:"name"`
		Path     string `json:"path"` // relative to mission dir
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
		Exists   bool   `json:"exists"`
	}
	want := []struct{ path string }{
		{"init.c"},
		{"cfgeconomycore.xml"},
		{"cfgenvironment.xml"},
		{"cfggameplay.json"},
		{"cfgrandompresets.xml"},
		{"cfgspawnabletypes.xml"},
		{"cfglimitsdefinition.xml"},
		{"cfglimitsdefinitionuser.xml"},
		{"cfgundergroundtriggers.json"},
		{"cfgeffectarea.json"},
		{"db/types.xml"},
		{"db/events.xml"},
		{"db/economy.xml"},
		{"db/globals.xml"},
		{"db/messages.xml"},
		// These live at the MISSION ROOT. Listing them under db/ made the
		// editor mark them missing and disable the option — the event-spawn
		// table, the spawn points and the weather file were unreachable.
		{"cfgeventspawns.xml"},
		{"cfgplayerspawnpoints.xml"},
		{"cfgweather.xml"},
		{"cfgeventgroups.xml"},
		{"cfgignorelist.xml"},
	}
	out := make([]entry, 0, len(want))
	for _, w := range want {
		full := filepath.Join(dir, filepath.FromSlash(w.path))
		st, err := os.Stat(full)
		e := entry{Name: filepath.Base(w.path), Path: w.path}
		if err == nil && !st.IsDir() {
			e.Exists = true
			e.Size = st.Size()
			e.Modified = st.ModTime().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	writeJSON(w, map[string]interface{}{"dir": dir, "files": out})
}

func (h *handlers) missionDBResolve(rel string) (string, error) {
	dir, err := h.missionDir()
	if err != nil {
		return "", err
	}
	// Strict: rel must be either a bare allowed name or "db/<allowed>".
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid path")
	}
	base := strings.ToLower(filepath.Base(cleaned))
	if !allowedMissionDBFiles[base] {
		return "", fmt.Errorf("file not on mission-db allowlist: %s", base)
	}
	return filepath.Join(dir, filepath.FromSlash(cleaned)), nil
}

func (h *handlers) missionDBRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.missionDBResolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	// 4 MB cap — types.xml is routinely ~1 MB; anything larger is likely
	// not a hand-editable file. REFUSE rather than truncate: the editor posts
	// its buffer straight back, so serving a truncated file meant saving it
	// would cut the real one off mid-element.
	const maxRaw = 4 << 20
	if st, serr := f.Stat(); serr == nil && st.Size() > maxRaw {
		http.Error(w, fmt.Sprintf("file is %d bytes — too large for the raw editor (limit %d). Use the Types page for large loot tables.", st.Size(), maxRaw),
			http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(io.LimitReader(f, maxRaw))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"path": rel, "content": string(body)})
}

// reRawComment strips XML comments before the well-formedness check. Vanilla
// BI files put `-------` decorations inside comments, which is illegal XML —
// rejecting those would mean refusing to save a file we ourselves served.
var reRawComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// checkSyntax rejects content that DayZ would silently discard in full.
func checkSyntax(path, content string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		var probe interface{}
		if err := json.Unmarshal([]byte(content), &probe); err != nil {
			return fmt.Errorf("not valid JSON: %v — DayZ ignores the whole file on a single syntax error", err)
		}
	case ".xml":
		cleaned := reRawComment.ReplaceAllString(content, "")
		dec := xml.NewDecoder(strings.NewReader(cleaned))
		for {
			_, err := dec.Token()
			if err == nil {
				continue
			}
			if err == io.EOF {
				break
			}
			if se, ok := err.(*xml.SyntaxError); ok {
				return fmt.Errorf("not valid XML (line %d): %s — DayZ ignores the whole file on a single syntax error", se.Line, se.Msg)
			}
			return fmt.Errorf("not valid XML: %v", err)
		}
	}
	return nil
}

func (h *handlers) missionDBWrite(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := h.missionDBResolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The typed editors already refuse malformed input; the raw editor writes
	// to the same files, so it must too. DayZ silently ignores an entire XML or
	// JSON file on a single syntax error, which turns a typo here into a server
	// that boots with no loot and no message anywhere.
	if err := checkSyntax(full, req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(full)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// Scheduled announcements.

// (The old /api/announcements endpoint was removed — scheduled announcements
// are now part of the manager config and saved via /api/config, item #2.)

// ---------------------------------------------------------------------------
// Mods: Workshop staleness check.
//
// Compares the *Workshop-side* mod folder mtime (i.e. the user's local Steam
// download) against Steam's published time_updated. A mod is "outdated" when
// the local Workshop copy is older than what Steam reports — meaning the
// user needs to run Steam to pull the new files before SyncAll can copy them
// into the server dir. We surface PublishedID, local time, remote time, and
// the diff so the frontend can render a clear stale/up-to-date state.
//
// Mods without a meta.cpp PublishedID are skipped (e.g. hand-rolled @Mods).

func (h *handlers) modsCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if h.app.Config.VanillaDayZPath == "" {
		http.Error(w, mods.ErrNoVanillaPath.Error(), http.StatusBadRequest)
		return
	}
	list, err := mods.List(h.app.ServerDir, h.app.Config.VanillaDayZPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	idToMod := map[string]*mods.Mod{}
	ids := make([]string, 0, len(list))
	for i := range list {
		m := &list[i]
		if m.PublishedID == "" {
			continue
		}
		idToMod[m.PublishedID] = m
		ids = append(ids, m.PublishedID)
	}
	remote, err := mods.FetchWorkshopMeta(r.Context(), ids)
	if err != nil {
		// Partial results are still useful — return what we got with the error.
		writeJSON(w, map[string]interface{}{
			"error":   err.Error(),
			"results": buildCheckResults(idToMod, remote),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"results": buildCheckResults(idToMod, remote),
	})
}

func buildCheckResults(idToMod map[string]*mods.Mod, remote map[string]mods.WorkshopRemote) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(idToMod))
	for id, m := range idToMod {
		row := map[string]interface{}{
			"name":         m.Name,
			"publishedId":  id,
			"localUpdated": m.WorkshopModifiedAt,
		}
		r, ok := remote[id]
		if !ok {
			row["status"] = "unknown"
			out = append(out, row)
			continue
		}
		row["title"] = r.Title
		row["remoteUpdated"] = r.TimeUpdated
		row["remoteResult"] = r.Result
		switch {
		case r.Result != 1:
			row["status"] = "missing" // ID was removed/private on Steam
		case r.TimeUpdated.IsZero():
			row["status"] = "unknown"
		case m.WorkshopModifiedAt.IsZero():
			row["status"] = "unknown"
		case r.TimeUpdated.After(m.WorkshopModifiedAt.Add(2 * time.Minute)):
			row["status"] = "outdated"
		default:
			row["status"] = "ok"
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, _ := out[i]["name"].(string)
		aj, _ := out[j]["name"].(string)
		return strings.ToLower(ai) < strings.ToLower(aj)
	})
	return out
}
