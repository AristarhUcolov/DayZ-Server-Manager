// Copyright (c) 2026 Aristarh Ucolov.
//
// Bans. The ban itself lives where the game reads it:
//
//   - ban.txt in the server folder — DayZ's own list. It takes the 44-character
//     player ID from the admin log (.ADM), and per the file's own header "bans
//     can be added/removed while the server is running and will come in effect
//     immediately, kicking the player". This is the main mechanism: it works
//     for every player the admin log has seen, online or not.
//   - battleye/bans.txt — BattlEye's list, by GUID (32 hex) or IP, as
//     `<GUID|IP> <expiry> <reason>` where expiry is -1 (permanent) or the Unix
//     time the ban ends. Used for players only known through RCon.
//
// Neither file can say who banned someone, why, or for how long in a way the
// panel can show — and DayZ's list has no expiry at all. So every ban the
// panel makes is also recorded in .dayz-manager/bans.json; a loop lifts timed
// bans when they end.
package web

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/players"
	"dayzmanager/internal/util"
)

// banRecord is one ban the panel made (active or lifted).
type banRecord struct {
	ID       string `json:"id"`
	Key      string `json:"key,omitempty"`    // player database key
	Name     string `json:"name,omitempty"`   // player name when banned
	DayZID   string `json:"dayzId,omitempty"` // 44-char player ID → ban.txt
	GUID     string `json:"guid,omitempty"`   // BattlEye GUID → battleye/bans.txt
	IP       string `json:"ip,omitempty"`     // BattlEye IP ban
	Steam64  string `json:"steam64,omitempty"`
	Reason   string `json:"reason,omitempty"`
	At       int64  `json:"at"`    // unix seconds
	Until    int64  `json:"until"` // unix seconds; 0 = permanent
	By       string `json:"by,omitempty"`
	Lifted   int64  `json:"lifted,omitempty"`   // unix seconds; 0 = still active
	LiftedBy string `json:"liftedBy,omitempty"` // "expired" or who lifted it
}

func (b banRecord) active() bool { return b.Lifted == 0 }

type banFile struct {
	Records []banRecord `json:"records"`
	// Moderation log: kicks and messages are not bans, but belong to a
	// player's history all the same.
	Log []modEvent `json:"log,omitempty"`
}

// modEvent is one moderation action for the player's history.
type modEvent struct {
	At     int64  `json:"at"`
	Action string `json:"action"` // "kick" | "ban" | "unban" | "expired" | "message"
	Key    string `json:"key,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
	Until  int64  `json:"until,omitempty"`
}

const (
	maxBanRecords = 1000
	maxModLog     = 500
)

var (
	reDayZID  = regexp.MustCompile(`^[A-Za-z0-9+/_-]{43}=$`)
	reBEGUID  = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	reSteam64 = regexp.MustCompile(`^7656119\d{10}$`)
)

// dayzIDFromSteam64 is DayZ's player ID for a Steam account: the URL-safe
// base64 of the SHA-256 of the Steam64 ID written in decimal.
func dayzIDFromSteam64(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.URLEncoding.EncodeToString(sum[:])
}

// sameDayZID reports whether two IDs of ban.txt / whitelist.txt name the same
// account: equal, or one is the Steam64 ID the other (the admin log's player
// ID) derives from. ban.txt lines written by hand or by other tools are often
// Steam64 IDs, while the panel and the admin log use the derived ID.
func sameDayZID(a, b string) bool {
	switch {
	case a == "" || b == "":
		return false
	case a == b:
		return true
	case reSteam64.MatchString(a) && reDayZID.MatchString(b):
		return dayzIDFromSteam64(a) == b
	case reSteam64.MatchString(b) && reDayZID.MatchString(a):
		return dayzIDFromSteam64(b) == a
	}
	return false
}

// accountID is the admin-log player ID an ID of those files stands for.
func accountID(id string) string {
	if reSteam64.MatchString(id) {
		return dayzIDFromSteam64(id)
	}
	return id
}

// beGUIDFromSteam64 is BattlEye's GUID for a Steam account: MD5 of "BE" and
// the Steam64 ID as 8 little-endian bytes.
func beGUIDFromSteam64(s string) string {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return ""
	}
	var b [10]byte
	b[0], b[1] = 'B', 'E'
	binary.LittleEndian.PutUint64(b[2:], n)
	sum := md5.Sum(b[:])
	return hex.EncodeToString(sum[:])
}

// ------------------------------------------------------------------ storage

func (h *handlers) banStorePath() string { return filepath.Join(h.app.ManagerDir, "bans.json") }
func (h *handlers) dayzBanPath() string  { return filepath.Join(h.app.ServerDir, "ban.txt") }

func (h *handlers) loadBanFile() banFile {
	var f banFile
	if data, err := os.ReadFile(h.banStorePath()); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	return f
}

func (h *handlers) saveBanFile(f banFile) error {
	// Keep every active ban; trim the oldest lifted ones.
	if len(f.Records) > maxBanRecords {
		sort.SliceStable(f.Records, func(i, j int) bool {
			if f.Records[i].active() != f.Records[j].active() {
				return f.Records[i].active()
			}
			return f.Records[i].At > f.Records[j].At
		})
		f.Records = f.Records[:maxBanRecords]
	}
	if len(f.Log) > maxModLog {
		f.Log = f.Log[len(f.Log)-maxModLog:]
	}
	data, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(h.banStorePath(), data)
}

func newBanID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ------------------------------------------------------------------ ban.txt

// dzBan is one entry of DayZ's ban.txt.
type dzBan struct {
	ID      string `json:"id"`
	Comment string `json:"comment,omitempty"`
}

// parseDayZBans reads ban.txt entries: the first token of each non-comment
// line, and the "// …" comment after it.
func parseDayZBans(data string) []dzBan {
	var out []dzBan
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		id, comment := line, ""
		if i := strings.Index(line, "//"); i >= 0 {
			id, comment = strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+2:])
		}
		if f := strings.Fields(id); len(f) > 0 {
			out = append(out, dzBan{ID: f[0], Comment: comment})
		}
	}
	return out
}

// eol is the line ending a text file already uses (CRLF on a Windows-made
// file), so lines the panel adds match the rest.
func eol(data string) string {
	if strings.Contains(data, "\r\n") || (data == "" && filepath.Separator == '\\') {
		return "\r\n"
	}
	return "\n"
}

// panelNote builds the "// …" comment the panel puts after an ID it writes:
// the non-empty parts and the DZM mark (lines the panel added).
func panelNote(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = asciiNote(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(append(out, "DZM"), " | ")
}

// shownNote is a ban.txt / whitelist.txt comment for the panel: without the
// DZM mark.
func shownNote(c string) string {
	var out []string
	for _, p := range strings.Split(c, "|") {
		if p = strings.TrimSpace(p); p != "" && p != "DZM" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// asciiNote keeps a comment for ban.txt to printable ASCII: the file is read
// by the game, and its own header is plain ASCII.
func asciiNote(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f && r != '/' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// addDayZBan appends id to ban.txt (no-op when it is already there).
func addDayZBan(path, id, note string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	text := string(data)
	for _, b := range parseDayZBans(text) {
		if b.ID == id {
			return false, nil
		}
	}
	nl := eol(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += nl
	}
	line := id
	if note = asciiNote(note); note != "" {
		line += "\t// " + note
	}
	text += line + nl
	_ = util.BackupBeforeWrite(path)
	return true, writeFileAtomic(path, []byte(text))
}

// removeDayZBan drops every line banning id from ban.txt, keeping the rest of
// the file (header, comments, other bans) exactly as it was.
func removeDayZBan(path, id string) (bool, error) {
	return removeDayZLines(path, func(v string) bool { return v == id })
}

// removeDayZLines drops every entry whose ID match accepts, keeping the rest
// of the file exactly as it was.
func removeDayZLines(path string, match func(id string) bool) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	kept := lines[:0]
	removed := false
	for _, line := range lines {
		if bs := parseDayZBans(line); len(bs) == 1 && match(bs[0].ID) {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return false, nil
	}
	_ = util.BackupBeforeWrite(path)
	return true, writeFileAtomic(path, []byte(strings.Join(kept, "")))
}

// ------------------------------------------------------- battleye/bans.txt

// beBanExpiry reads the second column of BattlEye's bans.txt: -1 (or 0, or
// "perm") is permanent; a Unix time is when the ban ends. Older panel versions
// wrote a number of minutes there; a value too small to be a Unix time is read
// as minutes counted from the file's last write (mod).
func beBanExpiry(tok string, mod time.Time) (perm bool, until int64) {
	tok = strings.TrimSpace(strings.ToLower(tok))
	if tok == "" || tok == "perm" || tok == "-1" || tok == "0" {
		return true, 0
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil || n < 0 {
		return true, 0
	}
	if n < 1_000_000_000 { // legacy "minutes left"
		return false, mod.Add(time.Duration(n) * time.Minute).Unix()
	}
	return false, n
}

// beBanToken renders an expiry for bans.txt.
func beBanToken(until int64) string {
	if until <= 0 {
		return "-1"
	}
	return strconv.FormatInt(until, 10)
}

func (h *handlers) addBEBan(id string, until int64, reason string) error {
	path := h.bansPath()
	data, _ := os.ReadFile(path)
	bans, comments := parseBans(string(data))
	out := bans[:0]
	for _, b := range bans {
		if !strings.EqualFold(strings.TrimSpace(b.ID), id) {
			out = append(out, b)
		}
	}
	out = append(out, banEntry{ID: id, Minutes: beBanToken(until), Reason: asciiNote(reason)})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = util.BackupBeforeWrite(path)
	return writeFileAtomic(path, banFileBytes(out, comments))
}

func (h *handlers) removeBEBan(id string) (bool, error) {
	path := h.bansPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	bans, comments := parseBans(string(data))
	out := bans[:0]
	removed := false
	for _, b := range bans {
		if strings.EqualFold(strings.TrimSpace(b.ID), id) {
			removed = true
			continue
		}
		out = append(out, b)
	}
	if !removed {
		return false, nil
	}
	_ = util.BackupBeforeWrite(path)
	return true, writeFileAtomic(path, banFileBytes(out, comments))
}

// reloadBE makes BattlEye re-read bans.txt when the server runs.
func (h *handlers) reloadBE() bool {
	if !h.app.ServerIsRunning() || !h.app.RCon.Configured() {
		return false
	}
	_, err := h.app.RCon.Command("loadBans")
	return err == nil
}

// ------------------------------------------------------------------ API

func (h *handlers) registerBans(mux *http.ServeMux) {
	mux.HandleFunc("/api/bans", methods(h.bansList, http.MethodGet))
	mux.HandleFunc("/api/bans/add", methods(h.bansAdd, http.MethodPost))
	mux.HandleFunc("/api/bans/remove", methods(h.bansRemove, http.MethodPost))
	mux.HandleFunc("/api/players/kick", methods(h.playerKick, http.MethodPost))
	mux.HandleFunc("/api/players/online", methods(h.playersOnline, http.MethodGet))
}

// actor names who did something: the signed-in user, else "panel".
func (h *handlers) actor(r *http.Request) string {
	if h.auth != nil {
		if s := h.auth.sessionFor(r); s != nil && s.User != "" {
			return s.User
		}
	}
	return ""
}

// banView is a ban for the Players page: a panel record, or an entry the
// panel did not make (added by hand or by another tool).
type banView struct {
	banRecord
	Source  string `json:"source"` // "panel" | "dayz" | "battleye"
	Expired bool   `json:"expired,omitempty"`
}

func (h *handlers) bansList(w http.ResponseWriter, r *http.Request) {
	h.bansMu.Lock()
	f := h.loadBanFile()
	h.bansMu.Unlock()
	now := time.Now().Unix()
	var active, history []banView
	known := map[string]bool{}
	for _, b := range f.Records {
		v := banView{banRecord: b, Source: "panel"}
		if b.active() {
			active = append(active, v)
			for _, id := range []string{b.DayZID, strings.ToLower(b.GUID), b.IP} {
				if id != "" {
					known[id] = true
				}
			}
		} else {
			history = append(history, v)
		}
	}
	// Entries in the game's files the panel did not make.
	names := h.namesByID()
	if data, err := os.ReadFile(h.dayzBanPath()); err == nil {
		for _, b := range parseDayZBans(string(data)) {
			// The entry's own ID stays the ban's handle (unbanning removes that
			// line); a Steam64 ID is matched through the IDs derived from it —
			// the player database knows players by those, and a ban the panel
			// made for the same account must not be listed twice.
			v := banView{banRecord: banRecord{ID: "dayz:" + b.ID, DayZID: b.ID, Reason: shownNote(b.Comment)}, Source: "dayz"}
			if reSteam64.MatchString(b.ID) {
				v.Steam64, v.DayZID, v.GUID = b.ID, dayzIDFromSteam64(b.ID), beGUIDFromSteam64(b.ID)
			}
			if known[b.ID] || known[v.DayZID] {
				continue
			}
			if n, ok := names[v.DayZID]; ok {
				v.Name, v.Key = n[0], n[1]
			} else if n, ok := names[v.GUID]; ok && v.GUID != "" {
				v.Name, v.Key = n[0], n[1]
			}
			active = append(active, v)
		}
	}
	if st, err := os.Stat(h.bansPath()); err == nil {
		data, _ := os.ReadFile(h.bansPath())
		bans, _ := parseBans(string(data))
		for _, b := range bans {
			id := strings.TrimSpace(b.ID)
			if known[strings.ToLower(id)] || known[id] {
				continue
			}
			perm, until := beBanExpiry(b.Minutes, st.ModTime())
			v := banView{banRecord: banRecord{ID: "be:" + id, Reason: b.Reason}, Source: "battleye"}
			if reBEGUID.MatchString(id) {
				v.GUID = strings.ToLower(id)
				if n, ok := names[v.GUID]; ok {
					v.Name, v.Key = n[0], n[1]
				}
			} else {
				v.IP = id
			}
			if !perm {
				v.Until = until
				v.Expired = until <= now
			}
			active = append(active, v)
		}
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].At > active[j].At })
	sort.SliceStable(history, func(i, j int) bool { return history[i].Lifted > history[j].Lifted })
	if len(history) > 200 {
		history = history[:200]
	}
	if active == nil {
		active = []banView{}
	}
	if history == nil {
		history = []banView{}
	}
	writeJSON(w, map[string]interface{}{"active": active, "history": history, "now": now})
}

// namesByID maps player IDs and BattlEye GUIDs to [name, key] from the player
// database.
func (h *handlers) namesByID() map[string][2]string {
	out := map[string][2]string{}
	list, _ := h.playersDB().Snapshot(0)
	for _, p := range list {
		if p.ID != "" {
			out[p.ID] = [2]string{p.Name, p.Key}
		}
		if p.BEGUID != "" {
			out[p.BEGUID] = [2]string{p.Name, p.Key}
		}
	}
	return out
}

func (h *handlers) bansAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key      string `json:"key"`      // a player of the database
		PlayerNo *int   `json:"playerNo"` // an online player (RCon number)
		Target   string `json:"target"`   // typed: Steam64, player ID, GUID or IP
		Minutes  int    `json:"minutes"`  // 0 = permanent
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Minutes < 0 || req.Minutes > 10*365*24*60 {
		http.Error(w, "duration must be 0 (permanent) or up to 10 years", http.StatusBadRequest)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > 200 {
		req.Reason = req.Reason[:200]
	}
	rec := banRecord{ID: newBanID(), Reason: req.Reason, At: time.Now().Unix(), By: h.actor(r)}
	if req.Minutes > 0 {
		rec.Until = rec.At + int64(req.Minutes)*60
	}
	var online *int // RCon number of the player, when online
	db := h.playersDB()
	switch {
	case req.Key != "":
		p, ok := db.Get(req.Key)
		if !ok {
			http.Error(w, "unknown player", http.StatusNotFound)
			return
		}
		rec.Key, rec.Name, rec.GUID = p.Key, p.Name, p.BEGUID
		if reDayZID.MatchString(p.ID) {
			rec.DayZID = p.ID
		}
	case req.PlayerNo != nil:
		ps, err := h.app.RCon.PlayersCached(5 * time.Second)
		if err != nil {
			http.Error(w, "RCon: "+err.Error(), http.StatusBadGateway)
			return
		}
		for _, p := range ps {
			if p.ID == *req.PlayerNo {
				rec.Name, rec.IP = p.Name, ""
				if reBEGUID.MatchString(p.GUID) {
					rec.GUID = strings.ToLower(p.GUID)
				}
				no := p.ID
				online = &no
				break
			}
		}
		if online == nil {
			http.Error(w, "the player is no longer online", http.StatusNotFound)
			return
		}
		// The admin log may know the same player: ban their ID too.
		list, _ := db.Snapshot(0)
		for _, p := range list {
			if (rec.GUID != "" && p.BEGUID == rec.GUID) && reDayZID.MatchString(p.ID) {
				rec.Key, rec.DayZID = p.Key, p.ID
				break
			}
		}
	default:
		t := strings.TrimSpace(req.Target)
		switch {
		case reSteam64.MatchString(t):
			rec.Steam64, rec.DayZID, rec.GUID = t, dayzIDFromSteam64(t), beGUIDFromSteam64(t)
		case reDayZID.MatchString(t):
			rec.DayZID = t
		case reBEGUID.MatchString(t):
			rec.GUID = strings.ToLower(t)
		case isIP(t):
			rec.IP = t
		default:
			http.Error(w, "enter a Steam64 ID (17 digits), a player ID from the admin log (44 characters), a BattlEye GUID (32 characters) or an IP address", http.StatusBadRequest)
			return
		}
		if n, ok := h.namesByID()[rec.DayZID]; ok && rec.DayZID != "" {
			rec.Name, rec.Key = n[0], n[1]
		} else if n, ok := h.namesByID()[rec.GUID]; ok && rec.GUID != "" {
			rec.Name, rec.Key = n[0], n[1]
		}
	}
	rconOnly := false
	if rec.DayZID == "" && rec.GUID == "" && rec.IP == "" {
		if online == nil {
			http.Error(w, "this player has no ID in the admin log yet and was not seen online with RCon — ban them while they are online, or by Steam64 ID", http.StatusBadRequest)
			return
		}
		// Online but BattlEye has not verified the GUID yet: let BattlEye
		// ban the player number itself (it kicks too).
		rconOnly = true
	}

	h.bansMu.Lock()
	defer h.bansMu.Unlock()
	if rconOnly {
		if err := h.app.RCon.Ban(*online, req.Minutes, kickReason(rec)); err != nil {
			rconFail(w, err)
			return
		}
		h.app.RCon.InvalidatePlayers()
		// No panel record: the ban lives in BattlEye's own list (bans.txt,
		// by the GUID BattlEye resolves), which the Bans tab shows and can
		// lift. The history still has it.
		f := h.loadBanFile()
		f.Log = append(f.Log, modEvent{At: rec.At, Action: "ban", Key: rec.Key, Name: rec.Name, Reason: rec.Reason, By: rec.By, Until: rec.Until})
		if err := h.saveBanFile(f); err != nil {
			h.app.Log.Printf("bans.json: %v", err)
		}
		h.app.Log.Printf("ban (BattlEye, player #%d): %s until %s — %s", *online, rec.Name, untilText(rec.Until), rec.Reason)
		h.notifyModeration("ban", rec.Name, rec.Reason, rec.By, rec.Until)
		writeJSON(w, map[string]interface{}{"ok": true, "ban": rec})
		return
	}
	parts := []string{rec.Name, rec.Reason}
	if rec.Until > 0 {
		parts = append(parts, "until "+time.Unix(rec.Until, 0).Format("2006-01-02 15:04"))
	}
	note := panelNote(parts...)
	if rec.DayZID != "" {
		if _, err := addDayZBan(h.dayzBanPath(), rec.DayZID, note); err != nil {
			http.Error(w, "ban.txt: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	beUsed := false
	// BattlEye too whenever the GUID is known: the only ban for a player the
	// admin log never named, and a second lock for everyone else — BattlEye
	// refuses the reconnect even if ban.txt is edited by hand later.
	if rec.GUID != "" {
		if err := h.addBEBan(rec.GUID, rec.Until, rec.Reason); err != nil {
			http.Error(w, "bans.txt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		beUsed = true
	}
	if rec.IP != "" {
		if err := h.addBEBan(rec.IP, rec.Until, rec.Reason); err != nil {
			http.Error(w, "bans.txt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		beUsed = true
	}
	reloaded := false
	if beUsed {
		reloaded = h.reloadBE()
	}
	// DayZ kicks a player whose ID lands in ban.txt by itself. BattlEye does
	// not kick on loadBans: kick the online player by number.
	if online != nil && rec.DayZID == "" {
		_ = h.app.RCon.Kick(*online, kickReason(rec))
		h.app.RCon.InvalidatePlayers()
	}
	f := h.loadBanFile()
	// One active record per player: a new ban replaces the old one.
	for i := range f.Records {
		o := &f.Records[i]
		if o.active() && ((rec.DayZID != "" && o.DayZID == rec.DayZID) || (rec.GUID != "" && o.GUID == rec.GUID) || (rec.IP != "" && o.IP == rec.IP)) {
			o.Lifted, o.LiftedBy = rec.At, "replaced"
		}
	}
	f.Records = append(f.Records, rec)
	f.Log = append(f.Log, modEvent{At: rec.At, Action: "ban", Key: rec.Key, Name: rec.Name, Reason: rec.Reason, By: rec.By, Until: rec.Until})
	if err := h.saveBanFile(f); err != nil {
		h.app.Log.Printf("bans.json: %v", err)
	}
	h.app.Log.Printf("ban: %s (%s) until %s — %s", nonEmpty(rec.Name, rec.DayZID+rec.GUID+rec.IP), nonEmpty(rec.By, "panel"), untilText(rec.Until), rec.Reason)
	h.notifyModeration("ban", rec.Name, rec.Reason, rec.By, rec.Until)
	writeJSON(w, map[string]interface{}{"ok": true, "ban": rec, "reloaded": reloaded})
}

func kickReason(rec banRecord) string {
	r := rec.Reason
	if r == "" {
		r = "Banned"
	}
	if rec.Until > 0 {
		r += " (until " + time.Unix(rec.Until, 0).Format("2006-01-02 15:04") + ")"
	}
	return r
}

func untilText(until int64) string {
	if until <= 0 {
		return "permanent"
	}
	return time.Unix(until, 0).Format("2006-01-02 15:04")
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func isIP(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || p == "" {
			return false
		}
	}
	return true
}

// bansRemove lifts a ban: a panel record by id, or an entry of the game's
// files ("dayz:<id>", "be:<guid|ip>").
func (h *handlers) bansRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "ban id required", http.StatusBadRequest)
		return
	}
	h.bansMu.Lock()
	defer h.bansMu.Unlock()
	rec, err := h.liftLocked(req.ID, nonEmpty(h.actor(r), "panel"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.notifyModeration("unban", rec.Name, "", h.actor(r), 0)
	writeJSON(w, map[string]interface{}{"ok": true})
}

// liftLocked removes a ban from the game's files and marks its record lifted.
// Caller holds bansMu.
func (h *handlers) liftLocked(id, by string) (banRecord, error) {
	f := h.loadBanFile()
	now := time.Now().Unix()
	var rec banRecord
	switch {
	case strings.HasPrefix(id, "dayz:"):
		rec = banRecord{DayZID: strings.TrimPrefix(id, "dayz:")}
	case strings.HasPrefix(id, "be:"):
		v := strings.TrimPrefix(id, "be:")
		if reBEGUID.MatchString(v) {
			rec = banRecord{GUID: strings.ToLower(v)}
		} else {
			rec = banRecord{IP: v}
		}
	default:
		found := false
		for _, b := range f.Records {
			if b.ID == id {
				rec, found = b, true
				break
			}
		}
		if !found {
			return rec, errors.New("unknown ban")
		}
	}
	if rec.DayZID != "" {
		// Every line for the account: the list shows a Steam64 line and the
		// panel's line for the same player as one ban, so both go.
		if _, err := removeDayZLines(h.dayzBanPath(), func(id string) bool { return sameDayZID(id, rec.DayZID) }); err != nil {
			return rec, fmt.Errorf("ban.txt: %w", err)
		}
	}
	beChanged := false
	for _, v := range []string{rec.GUID, rec.IP} {
		if v == "" {
			continue
		}
		ok, err := h.removeBEBan(v)
		if err != nil {
			return rec, fmt.Errorf("bans.txt: %w", err)
		}
		beChanged = beChanged || ok
	}
	if beChanged {
		h.reloadBE()
	}
	// Every active record for the same IDs is lifted.
	for i := range f.Records {
		o := &f.Records[i]
		if !o.active() {
			continue
		}
		if o.ID == rec.ID || sameDayZID(o.DayZID, rec.DayZID) || (rec.GUID != "" && o.GUID == rec.GUID) || (rec.IP != "" && o.IP == rec.IP) {
			o.Lifted, o.LiftedBy = now, by
			if rec.Name == "" {
				rec.Name, rec.Key = o.Name, o.Key
			}
		}
	}
	action := "unban"
	if by == "expired" {
		action = "expired"
	}
	f.Log = append(f.Log, modEvent{At: now, Action: action, Key: rec.Key, Name: rec.Name, By: by})
	if err := h.saveBanFile(f); err != nil {
		h.app.Log.Printf("bans.json: %v", err)
	}
	h.app.Log.Printf("unban: %s (%s)", nonEmpty(rec.Name, rec.DayZID+rec.GUID+rec.IP), by)
	return rec, nil
}

// bansExpiryLoop lifts timed bans when they end — DayZ's ban.txt has no
// expiry of its own.
func (h *handlers) bansExpiryLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		h.expireBans()
	}
}

func (h *handlers) expireBans() {
	h.bansMu.Lock()
	defer h.bansMu.Unlock()
	f := h.loadBanFile()
	now := time.Now().Unix()
	for _, b := range f.Records {
		if b.active() && b.Until > 0 && b.Until <= now {
			rec, err := h.liftLocked(b.ID, "expired")
			if err != nil {
				h.app.Log.Printf("lift expired ban %s: %v", b.ID, err)
				continue
			}
			h.notifyModeration("expired", rec.Name, "", "", 0)
		}
	}
}

// banFor finds the active ban of a player (panel record, or an entry in the
// game's files) — for the player profile.
func (h *handlers) banFor(dayzID, guid string) *banView {
	h.bansMu.Lock()
	f := h.loadBanFile()
	h.bansMu.Unlock()
	for _, b := range f.Records {
		if b.active() && ((dayzID != "" && b.DayZID == dayzID) || (guid != "" && b.GUID == guid)) {
			return &banView{banRecord: b, Source: "panel"}
		}
	}
	if dayzID != "" {
		if data, err := os.ReadFile(h.dayzBanPath()); err == nil {
			for _, b := range parseDayZBans(string(data)) {
				// The player's ID, or the Steam64 ID it derives from.
				if sameDayZID(b.ID, dayzID) {
					v := &banView{banRecord: banRecord{ID: "dayz:" + b.ID, DayZID: dayzID, Reason: shownNote(b.Comment)}, Source: "dayz"}
					if b.ID != dayzID {
						v.Steam64 = b.ID
					}
					return v
				}
			}
		}
	}
	if guid != "" {
		if st, err := os.Stat(h.bansPath()); err == nil {
			data, _ := os.ReadFile(h.bansPath())
			bans, _ := parseBans(string(data))
			for _, b := range bans {
				if strings.EqualFold(strings.TrimSpace(b.ID), guid) {
					perm, until := beBanExpiry(b.Minutes, st.ModTime())
					v := &banView{banRecord: banRecord{ID: "be:" + guid, GUID: guid, Reason: b.Reason}, Source: "battleye"}
					if !perm {
						if until <= time.Now().Unix() {
							continue
						}
						v.Until = until
					}
					return v
				}
			}
		}
	}
	return nil
}

// bannedAccounts is the set of admin-log player IDs that ban.txt or an active
// panel ban names (a Steam64 line counts as the ID it derives).
func (h *handlers) bannedAccounts() map[string]bool {
	h.bansMu.Lock()
	f := h.loadBanFile()
	h.bansMu.Unlock()
	out := map[string]bool{}
	for _, b := range f.Records {
		if b.active() && b.DayZID != "" {
			out[b.DayZID] = true
		}
	}
	if data, err := os.ReadFile(h.dayzBanPath()); err == nil {
		for _, b := range parseDayZBans(string(data)) {
			out[accountID(b.ID)] = true
		}
	}
	return out
}

// modHistory is the moderation log of one player, newest first.
func (h *handlers) modHistory(key, name string) []modEvent {
	h.bansMu.Lock()
	f := h.loadBanFile()
	h.bansMu.Unlock()
	var out []modEvent
	for i := len(f.Log) - 1; i >= 0 && len(out) < 50; i-- {
		e := f.Log[i]
		if (key != "" && e.Key == key) || (e.Key == "" && name != "" && e.Name == name) {
			out = append(out, e)
		}
	}
	return out
}

// logModeration records a kick or message in the moderation log.
func (h *handlers) logModeration(e modEvent) {
	h.bansMu.Lock()
	defer h.bansMu.Unlock()
	f := h.loadBanFile()
	f.Log = append(f.Log, e)
	if err := h.saveBanFile(f); err != nil {
		h.app.Log.Printf("bans.json: %v", err)
	}
}

// playerKick kicks an online player with a reason and logs it.
func (h *handlers) playerKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerNo int    `json:"playerNo"`
		Reason   string `json:"reason"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := ""
	if ps, err := h.app.RCon.PlayersCached(5 * time.Second); err == nil {
		for _, p := range ps {
			if p.ID == req.PlayerNo {
				name = p.Name
			}
		}
	}
	if name == "" {
		http.Error(w, "the player is no longer online", http.StatusNotFound)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if err := h.app.RCon.Kick(req.PlayerNo, reason); err != nil {
		rconFail(w, err)
		return
	}
	h.app.RCon.InvalidatePlayers()
	h.logModeration(modEvent{At: time.Now().Unix(), Action: "kick", Key: req.Key, Name: name, Reason: reason, By: h.actor(r)})
	h.notifyModeration("kick", name, reason, h.actor(r), 0)
	writeJSON(w, map[string]interface{}{"ok": true})
}

// playersOnline is the live list for the Players page: RCon's players joined
// with the database (profile key, playtime) — and it links GUIDs and IPs.
func (h *handlers) playersOnline(w http.ResponseWriter, r *http.Request) {
	if !h.app.ServerIsRunning() {
		// Whether RCon is set up, too: BattlEye reads its password at start, so
		// the page can say "password first, then start" instead of costing a
		// second restart.
		writeJSON(w, map[string]interface{}{"running": false, "configured": h.app.RCon.Configured(), "players": []interface{}{}})
		return
	}
	if !h.app.RCon.Configured() {
		writeJSON(w, map[string]interface{}{"running": true, "rcon": "notConfigured", "players": []interface{}{}})
		return
	}
	ps, err := h.app.RCon.PlayersCached(8 * time.Second)
	if err != nil {
		writeJSON(w, map[string]interface{}{"running": true, "rcon": "error", "error": err.Error(), "errorKey": rconErrKey(err), "players": []interface{}{}})
		return
	}
	db := h.playersDB()
	refs := make([]players.OnlineRef, 0, len(ps))
	for _, p := range ps {
		refs = append(refs, players.OnlineRef{Name: p.Name, GUID: p.GUID, IP: p.IP})
	}
	db.LinkRCon(refs)
	byGUID := map[string]string{}
	playMin := map[string]int{}
	list, _ := db.Snapshot(0)
	for _, p := range list {
		if p.BEGUID != "" {
			byGUID[p.BEGUID] = p.Key
		}
		playMin[p.Key] = p.PlayMin
	}
	starts := db.SessionStarts()
	out := make([]map[string]interface{}, 0, len(ps))
	for _, p := range ps {
		row := map[string]interface{}{"no": p.ID, "name": p.Name, "ping": p.Ping, "ip": p.IP,
			"guid": strings.ToLower(p.GUID), "lobby": p.Lobby}
		if k, ok := byGUID[strings.ToLower(p.GUID)]; ok {
			row["key"] = k
			// This session's start (from the admin log's connect line) and
			// the total time played before it.
			row["playtimeMinutes"] = playMin[k]
			if t0, ok := starts[k]; ok {
				row["since"] = t0.Format(time.RFC3339)
			}
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]interface{}{"running": true, "rcon": "ok", "players": out})
}
