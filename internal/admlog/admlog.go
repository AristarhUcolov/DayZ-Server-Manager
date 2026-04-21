// Copyright (c) 2026 Aristarh Ucolov.
//
// DayZ admin log (.ADM) parser.
//
// Each .ADM file has a header naming the server version and a timestamp. Every
// subsequent line starts with "HH:MM:SS |" followed by a free-form event. We
// recognize a small set that the admin panel cares about:
//
//   Player "<name>" (id=<guid> pos=<x,y,z>) connected
//   Player "<name>" (id=<guid> pos=<x,y,z>) disconnected
//   Player "<name>" (DEAD) (id=<guid> pos=<x,y,z>) ...          (kills / deaths)
//   Player "<name>" (id=<guid> pos=<x,y,z>) hit by Player "..."
//   Player "<name>" (id=<guid> pos=<x,y,z>) Chat("GLOBAL"): msg
//
// Anything that doesn't match falls into event type "other" with the raw tail.
package admlog

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Event struct {
	Time     string  `json:"time"`               // "HH:MM:SS" as-is from the log
	Type     string  `json:"type"`               // connect/disconnect/kill/hit/chat/death/other
	Player   string  `json:"player,omitempty"`   // subject
	Target   string  `json:"target,omitempty"`   // victim/attacker for hit/kill
	Weapon   string  `json:"weapon,omitempty"`   // "with MP5K" → "MP5K"
	Distance string  `json:"distance,omitempty"` // "from 123.4 meters" → "123.4"
	Message  string  `json:"message,omitempty"`  // chat text or raw tail
	Pos      []float64 `json:"pos,omitempty"`    // [x,y,z]
	Raw      string  `json:"raw"`
}

var (
	reLine     = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2})\s*\|\s*(.*)$`)
	rePlayer   = regexp.MustCompile(`^Player "([^"]*)"\s*(?:\(DEAD\)\s*)?\(id=[^)]*pos=<([^>]*)>\)\s*(.*)$`)
	reChat     = regexp.MustCompile(`^Chat\("([^"]*)"\):\s*(.*)$`)
	reHitBy    = regexp.MustCompile(`^(hit by|killed by)\s+(.*)$`)
	reByPlayer = regexp.MustCompile(`Player "([^"]*)"\s*(?:\(DEAD\)\s*)?\(id=[^)]*pos=<[^>]*>\)\s*(.*)$`)
	reWithWpn  = regexp.MustCompile(`(?:with|into)\s+([A-Za-z0-9_\-]+)`)
	reDistance = regexp.MustCompile(`from ([\d.]+)\s*meters?`)
	reCommas   = regexp.MustCompile(`\s*,\s*`)
)

// ParseLine turns a single .ADM line into an Event. Returns ok=false for the
// file header and blank lines.
func ParseLine(s string) (Event, bool) {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return Event{}, false
	}
	m := reLine.FindStringSubmatch(s)
	if m == nil {
		return Event{}, false
	}
	ev := Event{Time: m[1], Raw: s}
	body := strings.TrimSpace(m[2])

	pm := rePlayer.FindStringSubmatch(body)
	if pm == nil {
		ev.Type = "other"
		ev.Message = body
		return ev, true
	}
	ev.Player = pm[1]
	ev.Pos = parsePos(pm[2])
	tail := strings.TrimSpace(pm[3])

	switch {
	case tail == "connected":
		ev.Type = "connect"
	case tail == "disconnected" || strings.HasPrefix(tail, "has been disconnected"):
		ev.Type = "disconnect"
	default:
		if cm := reChat.FindStringSubmatch(tail); cm != nil {
			ev.Type = "chat"
			ev.Message = strings.TrimSpace(cm[1] + ": " + cm[2])
			return ev, true
		}
		if hm := reHitBy.FindStringSubmatch(tail); hm != nil {
			if hm[1] == "killed by" {
				ev.Type = "kill"
			} else {
				ev.Type = "hit"
			}
			rest := hm[2]
			if bm := reByPlayer.FindStringSubmatch(rest); bm != nil {
				ev.Target = bm[1]
				rest = bm[2]
			}
			if wm := reWithWpn.FindStringSubmatch(rest); wm != nil {
				ev.Weapon = wm[1]
			}
			if dm := reDistance.FindStringSubmatch(rest); dm != nil {
				ev.Distance = dm[1]
			}
			ev.Message = strings.TrimSpace(rest)
			return ev, true
		}
		if strings.Contains(tail, "(DEAD)") || strings.HasPrefix(tail, "committed suicide") || strings.Contains(body, "(DEAD)") {
			ev.Type = "death"
			ev.Message = tail
			return ev, true
		}
		ev.Type = "other"
		ev.Message = tail
	}
	return ev, true
}

func parsePos(s string) []float64 {
	parts := reCommas.Split(s, -1)
	if len(parts) < 2 {
		return nil
	}
	out := make([]float64, 0, 3)
	for _, p := range parts {
		f := parseFloat(strings.TrimSpace(p))
		out = append(out, f)
	}
	return out
}

func parseFloat(s string) float64 {
	var f float64
	var sign float64 = 1
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	}
	dot := false
	div := 1.0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			f = f*10 + float64(r-'0')
			if dot {
				div *= 10
			}
		case r == '.':
			dot = true
		default:
			return sign * f / div
		}
	}
	return sign * f / div
}

// ---------------------------------------------------------------------------
// File helpers.

// Latest returns the newest-mtime .ADM in profilesDir, or "".
func Latest(profilesDir string) string {
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	var best *candidate
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".adm") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c := candidate{path: filepath.Join(profilesDir, e.Name()), mod: info.ModTime()}
		if best == nil || c.mod.After(best.mod) {
			cc := c
			best = &cc
		}
	}
	if best == nil {
		// fall back to name-desc sort (DayZ uses timestamps in filenames)
		names := []string{}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".adm") {
				names = append(names, e.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		if len(names) > 0 {
			return filepath.Join(profilesDir, names[0])
		}
		return ""
	}
	return best.path
}

// Recent parses path and returns the last `limit` events matching `filter`.
// Reads the whole file but we cap it at 8 MB so a server that has been running
// for weeks doesn't blow up memory.
func Recent(path string, limit int, typeFilter, playerFilter string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const cap = int64(8 * 1024 * 1024)
	if st.Size() > cap {
		if _, err := f.Seek(st.Size()-cap, 0); err != nil {
			return nil, err
		}
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	typeFilter = strings.TrimSpace(strings.ToLower(typeFilter))
	playerFilter = strings.ToLower(strings.TrimSpace(playerFilter))

	ring := make([]Event, 0, limit)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first && st.Size() > cap {
			// drop first partial line after a mid-file seek
			first = false
			continue
		}
		first = false
		ev, ok := ParseLine(line)
		if !ok {
			continue
		}
		if typeFilter != "" && typeFilter != "all" && ev.Type != typeFilter {
			continue
		}
		if playerFilter != "" {
			pl := strings.ToLower(ev.Player)
			tg := strings.ToLower(ev.Target)
			if !strings.Contains(pl, playerFilter) && !strings.Contains(tg, playerFilter) {
				continue
			}
		}
		if len(ring) == limit {
			ring = ring[1:]
		}
		ring = append(ring, ev)
	}
	if err := sc.Err(); err != nil {
		return ring, err
	}
	return ring, nil
}
