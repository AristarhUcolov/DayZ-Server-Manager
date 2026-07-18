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
	Key       string   `json:"key"`             // GUID when known, else "name:<name>"
	ID        string   `json:"id,omitempty"`    // BE GUID from id=
	Name      string   `json:"name"`            // most recent name
	Aliases   []string `json:"aliases,omitempty"`
	FirstSeen string   `json:"firstSeen"` // RFC3339
	LastSeen  string   `json:"lastSeen"`
	Sessions  int      `json:"sessions"`
	PlayMin   int      `json:"playtimeMinutes"`
	Kills     int      `json:"kills"`
	Deaths    int      `json:"deaths"`
	Online    bool     `json:"online"` // filled at serve time from RCon
}

type KillEvent struct {
	At       string `json:"at"`   // RFC3339 (approximate — see package docs)
	Time     string `json:"time"` // HH:MM:SS straight from the log
	Killer   string `json:"killer,omitempty"`
	Victim   string `json:"victim"`
	Weapon   string `json:"weapon,omitempty"`
	Distance string `json:"distance,omitempty"`
	Suicide  bool   `json:"suicide,omitempty"`
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
		path: filepath.Join(managerDir, "players.json"),
		d:    db{Players: map[string]*Player{}, Offsets: map[string]int64{}},
		open: map[string]time.Time{},
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
	defer s.mu.Unlock()
	if time.Since(s.lastIn) < 15*time.Second {
		return
	}
	s.lastIn = time.Now()

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return
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

	stamp := mtime.Format(time.RFC3339)
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
		if s.apply(ev, stamp, mtime) {
			changed = true
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
		s.pushKill(KillEvent{
			At: stamp, Time: ev.Time,
			Killer: killerName, Victim: ev.Player,
			Weapon: ev.Weapon, Distance: ev.Distance,
			Suicide: killerName == "" || killerName == ev.Player,
		})
		return true
	case "death":
		p := s.player(ev.ID, ev.Player, stamp)
		if p == nil {
			return false
		}
		p.Deaths++
		s.pushKill(KillEvent{At: stamp, Time: ev.Time, Victim: ev.Player, Suicide: true})
		return true
	case "hit", "chat":
		return s.player(ev.ID, ev.Player, stamp) != nil
	}
	return false
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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
