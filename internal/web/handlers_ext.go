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
	"errors"
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
	if h.app.Cfg().ModSourceDir() == "" {
		http.Error(w, mods.ErrNoSource.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Only []string `json:"only"` // optional filter by @Mod names
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	res, err := mods.SyncAll(h.app.ServerDir, h.app.Cfg().ModSourceDir(), req.Only)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	touched := append(append([]string(nil), res.Installed...), res.Updated...)
	writeJSON(w, struct {
		*mods.SyncAllResult
		Missions []autoMission  `json:"missions"`
		Vehicles []autoVehicles `json:"vehicles"`
	}{res, h.autoInstallModMissions(touched), h.syncModVehicles(touched)})
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
	if h.app.Cfg().ModSourceDir() == "" {
		http.Error(w, mods.ErrNoSource.Error(), http.StatusBadRequest)
		return
	}
	res, err := mods.ResolveCollection(r.Context(), h.app.Cfg().ModSourceDir(), children)
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
	return battleye.Dir(h.app.ServerDir, h.app.Cfg().BEPath)
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
		code := http.StatusInternalServerError
		if errors.Is(err, battleye.ErrNotEditable) {
			code = http.StatusBadRequest
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// BattlEye bans editor (bans.txt).
//
// Line format: `<GUID|IP> <expiry> <reason>` — expiry is -1 for a permanent
// ban or the Unix time the ban ends (what BattlEye itself writes). Versions of
// the panel before 1.0 wrote a number of minutes there, which BattlEye read as
// a date in 1970: such timed bans ended at once. Hand-editing this file is the
// #1 way admins silently break every ban, so the panel provides a structured
// table. After a save, if the server is running, we issue RCon `loadBans` so
// changes apply without a restart.

type banEntry struct {
	ID      string `json:"id"`      // GUID or IP
	Minutes string `json:"minutes"` // the raw second column (kept for older clients)
	Reason  string `json:"reason"`
	// Read: what the second column means. Write: Perm or Until (Unix seconds)
	// set the expiry; when neither is given, Minutes is read as minutes left.
	Perm    bool  `json:"perm"`
	Until   int64 `json:"until,omitempty"`
	Expired bool  `json:"expired,omitempty"`
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

// banFileBytes renders a bans.txt from a ban list, keeping any leading comment
// lines. Shared by the bans editor and the per-player ban/unban actions so the
// file format never diverges.
func banFileBytes(bans []banEntry, comments []string) []byte {
	var b strings.Builder
	for _, c := range comments {
		b.WriteString(c)
		b.WriteString("\r\n")
	}
	for _, ban := range bans {
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
	return []byte(b.String())
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
		mod := time.Now()
		if st, err := os.Stat(path); err == nil {
			mod = st.ModTime()
		}
		now := time.Now().Unix()
		for i := range bans {
			bans[i].Perm, bans[i].Until = beBanExpiry(bans[i].Minutes, mod)
			bans[i].Expired = !bans[i].Perm && bans[i].Until <= now
		}
		writeJSON(w, map[string]interface{}{"path": path, "bans": bans, "now": now})
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
	// Write the expiry the way BattlEye reads it.
	now := time.Now()
	for i := range req.Bans {
		b := &req.Bans[i]
		switch {
		case b.Perm:
			b.Minutes = "-1"
		case b.Until > 0:
			b.Minutes = beBanToken(b.Until)
		default:
			// An older client: minutes left (0 / blank = permanent).
			perm, until := beBanExpiry(b.Minutes, now)
			if perm {
				b.Minutes = "-1"
			} else {
				b.Minutes = beBanToken(until)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(path)
	if err := writeFileAtomic(path, banFileBytes(req.Bans, comments)); err != nil {
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
	if err := dztypes.SaveTypes(path, doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"touched": touched})
}

// typesPatchMany applies its own patch to each named type and writes the file
// once. The Types page saves its inline edits through it: one bulk-patch call
// per row rewrote the whole file (and took a backup) per row, and since only
// five backups are kept, saving six rows pruned the copy of the file as it was
// before the save.
func (h *handlers) typesPatchMany(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File string `json:"file"`
		Rows []struct {
			Name  string                 `json:"name"`
			Patch dztypes.BulkFieldPatch `json:"patch"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Rows) == 0 {
		http.Error(w, "rows required", http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	touched := 0
	for _, row := range req.Rows {
		touched += doc.BulkPatch([]string{row.Name}, row.Patch)
	}
	if err := dztypes.SaveTypes(path, doc); err != nil {
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
	"types.xml":                   true,
	"events.xml":                  true,
	"economy.xml":                 true,
	"globals.xml":                 true,
	"messages.xml":                true,
	"cfgeventspawns.xml":          true,
	"cfgplayerspawnpoints.xml":    true,
	"cfgweather.xml":              true,
	"cfgeffectarea.json":          true,
	"init.c":                      true,
	"cfgeconomycore.xml":          true,
	"cfgenvironment.xml":          true,
	"cfggameplay.json":            true,
	"cfgrandompresets.xml":        true,
	"cfgspawnabletypes.xml":       true,
	"cfglimitsdefinition.xml":     true,
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
	// Works without a mod source too (server-only hosting): the installed
	// copies' meta.cpp timestamps are compared with Steam, so the admin still
	// learns which mods need a fresh upload.
	list, err := mods.List(h.app.ServerDir, h.app.Cfg().ModSourceDir())
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

// localUpdated is when the local copy was published on the Workshop: meta.cpp's
// timestamp when present (exact), else the source copy's newest file, else the
// server copy's.
func localUpdated(m *mods.Mod) time.Time {
	if t := mods.MetaTime(m.MetaTimestamp); !t.IsZero() {
		return t
	}
	if !m.WorkshopModifiedAt.IsZero() {
		return m.WorkshopModifiedAt
	}
	return m.ServerModifiedAt
}

func buildCheckResults(idToMod map[string]*mods.Mod, remote map[string]mods.WorkshopRemote) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(idToMod))
	for id, m := range idToMod {
		row := map[string]interface{}{
			"name":         m.Name,
			"publishedId":  id,
			"localUpdated": localUpdated(m),
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
		case localUpdated(m).IsZero():
			row["status"] = "unknown"
		case r.TimeUpdated.After(localUpdated(m).Add(2 * time.Minute)):
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
