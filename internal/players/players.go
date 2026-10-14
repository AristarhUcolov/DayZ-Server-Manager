// Copyright (c) 2026 Aristarh Ucolov.
//
// Persistent player database + killfeed built from DayZ admin logs (.ADM).
//
// The store ingests ADM files incrementally: for every file it remembers the
// byte offset it has parsed up to, so each pass only reads the newly appended
// tail. A file that shrank (log rotation) restarts from zero. Aggregates are
// therefore monotonic and never double-counted.
//
// Timestamps: ADM lines carry only HH:MM:SS, no date. Each parsed chunk is
// stamped with the file's mtime at scan — for the live tail that is within a
// minute of the truth, which is all first/last-seen and session math needs.
package players

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/admlog"
)

type Player struct {
	Key string `json:"key"` // the player ID when known, else "name:<name>"
	// ID is DayZ's player ID from the admin log's id= (44 characters, the
	// SHA-256 of the Steam64 ID). It is what the server's own ban.txt and
	// whitelist.txt take — NOT the BattlEye GUID.
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"` // most recent name
	Aliases   []string `json:"aliases,omitempty"`
	FirstSeen string   `json:"firstSeen"` // RFC3339
	LastSeen  string   `json:"lastSeen"`
	Sessions  int      `json:"sessions"`
	PlayMin   int      `json:"playtimeMinutes"`
	Kills     int      `json:"kills"`
	Deaths    int      `json:"deaths"`
	Online    bool     `json:"online"` // filled at serve time from RCon
	// BattlEye's GUID (32 hex) and the last IP address, learnt from RCon's
	// player list while the player was online (matched by name).
	BEGUID string `json:"beGuid,omitempty"`
	LastIP string `json:"ip,omitempty"`
	// Admin-set metadata, persisted alongside the derived stats. ADM ingestion
	// never touches these — a note or a watch flag survives every log pass.
	Note  string `json:"note,omitempty"`
	Watch bool   `json:"watch,omitempty"`
}

type KillEvent struct {
	At       string `json:"at"`   // RFC3339 (approximate — see package docs)
	Time     string `json:"time"` // HH:MM:SS straight from the log
	Killer   string `json:"killer,omitempty"`
	Victim   string `json:"victim"`
	Weapon   string `json:"weapon,omitempty"`
	Distance string `json:"distance,omitempty"`
	// Source names a non-player killer as written in the log
	// ("ZmbM_HermitSkinny_Beard", "Animal_UrsusArctos", "FallDamage").
	Source string `json:"source,omitempty"`
	// Kind is what actually happened: "pvp", "env" (infected/animal/fall/
	// starvation) or "suicide". Everything without a player killer used to be
	// reported as a suicide, which on a normal server is most deaths.
	Kind    string `json:"kind"`
	Suicide bool   `json:"suicide,omitempty"` // kept for older UI builds
	// Pos is the victim's world position [x, z] (the death location), when the
	// log line carried one. Drives the death/kill heatmap.
	Pos []float64 `json:"pos,omitempty"`
}

type db struct {
	Players  map[string]*Player `json:"players"`
	Killfeed []KillEvent        `json:"killfeed"`
	Offsets  map[string]int64   `json:"offsets"`
}

type Store struct {
	mu   sync.Mutex
	path string
	d    db
	// open sessions: player key → connect time. Not persisted — a manager
	// restart just misses one session's playtime, never corrupts totals.
	open   map[string]time.Time
	lastIn time.Time
	// Last-known ground position per player key, from any positioned ADM line
	// (connect / hit / chat). In-memory only — this is "where were they last
	// seen", used by the live map, not something to persist. Vanilla DayZ does
	// not log positions continuously, so these can be stale between events.
	lastPos   map[string][]float64
	lastPosAt map[string]time.Time
	// Recent connects of watch-flagged players — a transient notification feed
	// for the panel, not persisted (a fresh feed after a restart is fine).
	watchLog []WatchEvent

	// OnLive, when set, receives events from the live tail of the admin log
	// (not the history read at start-up) — Discord's kill feed and watchlist
	// pings. Called after the store's lock is released.
	OnLive  func(LiveEvent)
	opened  time.Time
	pending []LiveEvent
}

// LiveEvent is a fresh admin-log event with the player it concerns.
type LiveEvent struct {
	Event  admlog.Event
	At     time.Time
	Player Player // the subject, as stored after the event
}

// WatchEvent is one connect by a watch-flagged player.
type WatchEvent struct {
	Name string `json:"name"`
	At   string `json:"at"` // RFC3339
}

const (
	maxKillfeed = 500
	maxAliases  = 8
	// A cold start on a huge ADM only reads the newest chunk — ancient
	// history isn't worth minutes of parsing.
	maxColdParse = 16 * 1024 * 1024
)

func Open(managerDir string) *Store {
	s := &Store{
		path:      filepath.Join(managerDir, "players.json"),
		d:         db{Players: map[string]*Player{}, Offsets: map[string]int64{}},
		open:      map[string]time.Time{},
		lastPos:   map[string][]float64{},
		lastPosAt: map[string]time.Time{},
		opened:    time.Now(),
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.d)
		if s.d.Players == nil {
			s.d.Players = map[string]*Player{}
		}
		if s.d.Offsets == nil {
			s.d.Offsets = map[string]int64{}
		}
	}
	return s
}

func (s *Store) save() {
	data, err := json.MarshalIndent(&s.d, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// Ingest parses every .ADM in profilesDir incrementally. Throttled to one
// pass per 15s — callers can invoke it opportunistically on every request.
func (s *Store) Ingest(profilesDir string) {
	s.mu.Lock()
	live := s.ingestLocked(profilesDir)
	hook := s.OnLive
	s.mu.Unlock()
	if hook != nil {
		for _, ev := range live {
			hook(ev)
		}
	}
}

func (s *Store) ingestLocked(profilesDir string) []LiveEvent {
	s.pending = nil
	if time.Since(s.lastIn) < 15*time.Second {
		return nil
	}
	s.lastIn = time.Now()

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil
	}
	changed := false
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".adm") {
			continue
		}
		full := filepath.Join(profilesDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if s.ingestFile(full, info.Size(), info.ModTime()) {
			changed = true
		}
	}
	if changed {
		s.save()
	}
	out := s.pending
	s.pending = nil
	return out
}

// ingestFile reads the unparsed tail of one ADM file. Returns true when any
// event was applied. Caller holds s.mu.
func (s *Store) ingestFile(path string, size int64, mtime time.Time) bool {
	off := s.d.Offsets[path]
	if size < off {
		off = 0 // rotated/truncated — start over
	}
	if size == off {
		return false
	}
	if size-off > maxColdParse {
		off = size - maxColdParse
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return false
	}
	data := make([]byte, size-off)
	n, _ := io.ReadFull(f, data)
	data = data[:n]
	// Only consume up to the last complete line — the writer may be mid-line.
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return false
	}
	chunk := data[:last+1]
	s.d.Offsets[path] = off + int64(last+1)

	// Stamping every event with the file's mtime made a connect and its
	// disconnect share a timestamp, so every session lasted 0 minutes. Rebuild
	// the real moment from the log's header date plus each line's HH:MM:SS.
	day := s.headerDay(path, mtime)
	prev := time.Duration(-1)

	changed := false
	sc := bufio.NewScanner(bytes.NewReader(chunk))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	first := off > 0
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			// After a mid-file seek the first "line" may be a partial one.
			if off > 0 && !strings.HasPrefix(line, "0") && !strings.HasPrefix(line, "1") && !strings.HasPrefix(line, "2") {
				continue
			}
		}
		ev, ok := admlog.ParseLine(line)
		if !ok {
			continue
		}
		at := mtime
		if tod, ok := admlog.TimeOfDay(ev.Time); ok && !day.IsZero() {
			// A log that runs past midnight wraps back to 00:00 — roll the day
			// forward when the clock goes backwards.
			if prev >= 0 && tod < prev {
				day = day.AddDate(0, 0, 1)
			}
			prev = tod
			at = day.Add(tod)
		}
		if s.apply(ev, at.Format(time.RFC3339), at) {
			changed = true
			// Only what happened while the manager runs is news.
			if s.OnLive != nil && !at.Before(s.opened.Add(-2*time.Minute)) && time.Since(at) < 10*time.Minute {
				le := LiveEvent{Event: ev, At: at}
				if p := s.d.Players[keyFor(ev.ID, ev.Player)]; p != nil {
					le.Player = *p
				}
				s.pending = append(s.pending, le)
			}
		}
	}
	return changed
}

func keyFor(id, name string) string {
	id = strings.TrimSpace(id)
	if id != "" && !strings.EqualFold(id, "unknown") {
		return id
	}
	return "name:" + name
}

func (s *Store) player(id, name, stamp string) *Player {
	if name == "" && id == "" {
		return nil
	}
	k := keyFor(id, name)
	p := s.d.Players[k]
	if p == nil {
		p = &Player{Key: k, ID: strings.TrimSpace(id), Name: name, FirstSeen: stamp}
		s.d.Players[k] = p
	}
	if name != "" && p.Name != name {
		if p.Name != "" && !contains(p.Aliases, p.Name) && len(p.Aliases) < maxAliases {
			p.Aliases = append(p.Aliases, p.Name)
		}
		p.Name = name
	}
	if p.FirstSeen == "" {
		p.FirstSeen = stamp
	}
	p.LastSeen = stamp
	return p
}

func (s *Store) apply(ev admlog.Event, stamp string, at time.Time) bool {
	switch ev.Type {
	case "connect":
		p := s.player(ev.ID, ev.Player, stamp)
		if p == nil {
			return false
		}
		p.Sessions++
		s.open[p.Key] = at
		s.recordPos(p.Key, xz(ev.Pos), at)
		if p.Watch {
			s.watchLog = append(s.watchLog, WatchEvent{Name: p.Name, At: at.Format(time.RFC3339)})
			if len(s.watchLog) > 50 {
				s.watchLog = s.watchLog[len(s.watchLog)-50:]
			}
		}
		return true
	case "disconnect":
		p := s.player(ev.ID, ev.Player, stamp)
		if p == nil {
			return false
		}
		if t0, ok := s.open[p.Key]; ok {
			if d := at.Sub(t0); d > 0 && d < 24*time.Hour {
				p.PlayMin += int(d.Minutes())
			}
			delete(s.open, p.Key)
		}
		return true
	case "kill":
		// Line shape: Player "Victim" (DEAD) ... killed by Player "Killer".
		victim := s.player(ev.ID, ev.Player, stamp)
		if victim != nil {
			victim.Deaths++
		}
		var killerName string
		if ev.Target != "" {
			if killer := s.player(ev.TargetID, ev.Target, stamp); killer != nil {
				killer.Kills++
				killerName = killer.Name
			}
		}
		kind := "env"
		switch {
		case killerName != "" && killerName != ev.Player:
			kind = "pvp"
		case killerName == ev.Player && killerName != "",
			strings.Contains(strings.ToLower(ev.Message), "suicide"):
			kind = "suicide"
		}
		s.pushKill(KillEvent{
			At: stamp, Time: ev.Time,
			Killer: killerName, Victim: ev.Player,
			Weapon: ev.Weapon, Distance: ev.Distance,
			Source: ev.Source, Kind: kind,
			Suicide: kind == "suicide",
			Pos:     xz(ev.Pos),
		})
		return true
	case "death":
		p := s.player(ev.ID, ev.Player, stamp)
		if p == nil {
			return false
		}
		p.Deaths++
		// A bare death line is bleeding, starvation, hypothermia — environment,
		// not a suicide, unless the log says so.
		kind := "env"
		if strings.Contains(strings.ToLower(ev.Message), "suicide") {
			kind = "suicide"
		}
		s.pushKill(KillEvent{At: stamp, Time: ev.Time, Victim: ev.Player,
			Kind: kind, Suicide: kind == "suicide", Source: ev.Source,
			Pos: xz(ev.Pos)})
		return true
	case "hit", "chat":
		p := s.player(ev.ID, ev.Player, stamp)
		if p == nil {
			return false
		}
		s.recordPos(p.Key, xz(ev.Pos), at)
		return true
	}
	return false
}

// recordPos stores a player's last-known ground position. Caller holds s.mu.
// A nil position (line without pos=<…>) is ignored so the previous fix stands.
func (s *Store) recordPos(key string, pos []float64, at time.Time) {
	if pos == nil {
		return
	}
	s.lastPos[key] = pos
	s.lastPosAt[key] = at
}

// LivePos is one player's last-known location for the live map.
type LivePos struct {
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Z      float64 `json:"z"`
	AgeSec int     `json:"ageSec"` // seconds since the position was logged
}

// LivePositions returns the last-known location of every player the store has
// seen a position for. The caller decides who is actually online (via RCon).
func (s *Store) LivePositions() []LivePos {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LivePos, 0, len(s.lastPos))
	now := time.Now()
	for k, pos := range s.lastPos {
		if len(pos) < 2 {
			continue
		}
		name := k
		if p := s.d.Players[k]; p != nil && p.Name != "" {
			name = p.Name
		}
		age := int(now.Sub(s.lastPosAt[k]).Seconds())
		if age < 0 {
			age = 0
		}
		out = append(out, LivePos{Key: k, Name: name, X: pos[0], Z: pos[1], AgeSec: age})
	}
	return out
}

// xz reduces a DayZ world position to the map ground plane [x, z]. The .ADM
// log writes pos=<east, north, altitude> — the altitude is the LAST component,
// so the ground plane is the first two, not [0] and [2]. Returns nil when no
// position was logged, so the field stays omitted for lines without a pos=<…>.
func xz(pos []float64) []float64 {
	if len(pos) < 2 {
		return nil
	}
	return []float64{pos[0], pos[1]}
}

func (s *Store) pushKill(k KillEvent) {
	s.d.Killfeed = append(s.d.Killfeed, k)
	if len(s.d.Killfeed) > maxKillfeed {
		s.d.Killfeed = s.d.Killfeed[len(s.d.Killfeed)-maxKillfeed:]
	}
}

// Snapshot returns players sorted by most-recently-seen and the newest
// killfeed entries (latest first, up to limit).
func (s *Store) Snapshot(killLimit int) ([]Player, []KillEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Player, 0, len(s.d.Players))
	for _, p := range s.d.Players {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	kf := s.d.Killfeed
	if killLimit > 0 && len(kf) > killLimit {
		kf = kf[len(kf)-killLimit:]
	}
	rev := make([]KillEvent, len(kf))
	for i, k := range kf {
		rev[len(kf)-1-i] = k
	}
	return out, rev
}

// SetMeta records an admin note and watch flag on a player, keyed by their
// stable Key (GUID or name:<name>). Returns false when the key is unknown.
// The metadata is persisted immediately and never overwritten by ADM ingestion.
func (s *Store) SetMeta(key, note string, watch bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.d.Players[key]
	if p == nil {
		return false
	}
	p.Note = strings.TrimSpace(note)
	p.Watch = watch
	s.save()
	return true
}

// OnlineRef is one player of RCon's live list.
type OnlineRef struct {
	Name, GUID, IP string
}

// LinkRCon attaches BattlEye GUIDs and IP addresses from RCon's player list to
// the players of the database. A player is matched by GUID when already
// linked, else by name — but only when exactly one player of that name is in
// an open session (connected in the admin log): "Survivor" is the default
// name of every new character, so a name alone proves nothing. Returns how
// many players changed.
func (s *Store) LinkRCon(online []OnlineRef) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, o := range online {
		guid := strings.ToLower(strings.TrimSpace(o.GUID))
		if len(guid) != 32 {
			guid = ""
		}
		var match *Player
		if guid != "" {
			for _, p := range s.d.Players {
				if p.BEGUID == guid {
					match = p
					break
				}
			}
		}
		if match == nil {
			count := 0
			for key, p := range s.d.Players {
				if p.Name != o.Name {
					continue
				}
				if _, open := s.open[key]; !open {
					continue
				}
				count++
				match = p
			}
			if count != 1 {
				match = nil
			}
		}
		if match == nil {
			continue
		}
		changed := false
		if guid != "" && match.BEGUID != guid {
			match.BEGUID, changed = guid, true
		}
		if ip := strings.TrimSpace(o.IP); ip != "" && match.LastIP != ip {
			match.LastIP, changed = ip, true
		}
		if changed {
			n++
		}
	}
	if n > 0 {
		s.save()
	}
	return n
}

// Get returns one player by key.
func (s *Store) Get(key string) (Player, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.d.Players[key]
	if p == nil {
		return Player{}, false
	}
	return *p, true
}

// InSession reports which players have an open session in the admin log
// (connected, not yet disconnected), by key.
func (s *Store) InSession() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.open))
	for k := range s.open {
		out[k] = true
	}
	return out
}

// SessionStarts is when each player with an open session connected, by key —
// how long someone online has been in (a fresh join next to a fight reads
// differently from a regular).
func (s *Store) SessionStarts() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time, len(s.open))
	for k, t := range s.open {
		out[k] = t
	}
	return out
}

// WatchActivity returns the recent connects of watch-flagged players, oldest
// first. Used by the panel's watchlist ping.
func (s *Store) WatchActivity() []WatchEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WatchEvent, len(s.watchLog))
	copy(out, s.watchLog)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// headerDay reads the date from an .ADM header, falling back to the file's
// mtime when the header is missing or unparseable.
func (s *Store) headerDay(path string, mtime time.Time) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Date(mtime.Year(), mtime.Month(), mtime.Day(), 0, 0, 0, 0, mtime.Location())
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < 5 && sc.Scan(); i++ {
		if d, ok := admlog.HeaderDate(sc.Text()); ok {
			return d
		}
	}
	return time.Date(mtime.Year(), mtime.Month(), mtime.Day(), 0, 0, 0, 0, mtime.Location())
}
