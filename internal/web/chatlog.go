// Copyright (c) 2026 Aristarh Ucolov.
//
// Persistent chat archive. Every chat line the RCon stream delivers — and every
// message the manager sends — is appended to .dayz-manager/chat/YYYY-MM-DD.jsonl
// (one JSON object per line, local date). The live feed keeps only the newest
// few hundred lines in memory; the archive is what lets an admin look up what
// was said last Tuesday, search it, or export a day for a ban appeal. Files
// older than Settings → chat retention are removed.
package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/rcon"
)

var reChatDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type chatArchive struct {
	dir      string
	keepDays func() int
	ch       chan rcon.ChatMsg
	log      *log.Logger

	mu      sync.Mutex // serializes file writes against reads
	dropped int        // messages lost to a full queue (logged once per burst)

	// Line counts per day file, recounted only when the file changed — a
	// listing must not re-read months of chat every time the tab opens.
	countMu sync.Mutex
	counts  map[string]chatCount
}

type chatCount struct {
	size  int64
	mod   time.Time
	count int
}

func newChatArchive(managerDir string, keepDays func() int, logger *log.Logger) *chatArchive {
	return &chatArchive{
		dir:      filepath.Join(managerDir, "chat"),
		keepDays: keepDays,
		ch:       make(chan rcon.ChatMsg, 1024),
		log:      logger,
	}
}

// push queues a message for writing. Called from the RCon reader goroutine —
// never blocks; a full queue (disk stalled) drops rather than stalling RCon.
func (a *chatArchive) push(m rcon.ChatMsg) {
	select {
	case a.ch <- m:
	default:
		a.mu.Lock()
		a.dropped++
		a.mu.Unlock()
	}
}

func dayOf(ms int64) string { return time.UnixMilli(ms).Format("2006-01-02") }

// run writes queued messages until ctx ends. One file handle per day.
func (a *chatArchive) run(ctx context.Context) {
	a.prune()
	pruneTick := time.NewTicker(6 * time.Hour)
	defer pruneTick.Stop()
	var (
		f    *os.File
		bw   *bufio.Writer
		open string
	)
	closeFile := func() {
		if bw != nil {
			_ = bw.Flush()
		}
		if f != nil {
			_ = f.Close()
		}
		f, bw, open = nil, nil, ""
	}
	defer closeFile()
	write := func(m rcon.ChatMsg) {
		day := dayOf(m.At)
		a.mu.Lock()
		defer a.mu.Unlock()
		if day != open {
			closeFile()
			if err := os.MkdirAll(a.dir, 0o755); err != nil {
				a.logf("chat archive: %v", err)
				return
			}
			nf, err := os.OpenFile(filepath.Join(a.dir, day+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				a.logf("chat archive: %v", err)
				return
			}
			f, bw, open = nf, bufio.NewWriter(nf), day
		}
		data, err := json.Marshal(m)
		if err != nil {
			return
		}
		_, _ = bw.Write(append(data, '\n'))
	}
	flush := func() {
		a.mu.Lock()
		if bw != nil {
			if err := bw.Flush(); err != nil {
				a.logf("chat archive: %v", err)
			}
		}
		if a.dropped > 0 {
			a.logf("chat archive: %d message(s) dropped — the disk could not keep up", a.dropped)
			a.dropped = 0
		}
		a.mu.Unlock()
	}
	for {
		select {
		case <-ctx.Done():
			// Drain what is already queued so a shutdown loses nothing.
			for {
				select {
				case m := <-a.ch:
					write(m)
				default:
					flush()
					return
				}
			}
		case m := <-a.ch:
			write(m)
			// Batch whatever else is already waiting, then flush once.
		batch:
			for {
				select {
				case m2 := <-a.ch:
					write(m2)
				default:
					break batch
				}
			}
			flush()
		case <-pruneTick.C:
			a.prune()
		}
	}
}

func (a *chatArchive) logf(format string, args ...interface{}) {
	if a.log != nil {
		a.log.Printf(format, args...)
	}
}

// prune removes day files older than the retention (0 = keep forever).
func (a *chatArchive) prune() {
	keep := 0
	if a.keepDays != nil {
		keep = a.keepDays()
	}
	if keep <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -keep).Format("2006-01-02")
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		day := strings.TrimSuffix(e.Name(), ".jsonl")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || !reChatDay.MatchString(day) {
			continue
		}
		if day < cutoff {
			_ = os.Remove(filepath.Join(a.dir, e.Name()))
		}
	}
}

type chatDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Size  int64  `json:"size"`
}

// dayFiles lists the archived day names (YYYY-MM-DD), newest first.
func (a *chatArchive) dayFiles() []string {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		day := strings.TrimSuffix(e.Name(), ".jsonl")
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") && reChatDay.MatchString(day) {
			out = append(out, day)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// days lists the archived days with message counts, newest first.
func (a *chatArchive) days() []chatDay {
	out := []chatDay{}
	a.countMu.Lock()
	defer a.countMu.Unlock()
	if a.counts == nil {
		a.counts = map[string]chatCount{}
	}
	for _, day := range a.dayFiles() {
		path := filepath.Join(a.dir, day+".jsonl")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		c, ok := a.counts[day]
		if !ok || c.size != info.Size() || !c.mod.Equal(info.ModTime()) {
			a.mu.Lock()
			data, err := os.ReadFile(path)
			a.mu.Unlock()
			if err != nil {
				continue
			}
			c = chatCount{size: info.Size(), mod: info.ModTime(), count: bytes.Count(data, []byte{'\n'})}
			a.counts[day] = c
		}
		out = append(out, chatDay{Date: day, Count: c.count, Size: c.size})
	}
	return out
}

// readDay returns the messages of one day, oldest first.
func (a *chatArchive) readDay(day string) ([]rcon.ChatMsg, error) {
	if !reChatDay.MatchString(day) {
		return nil, fmt.Errorf("bad date %q", day)
	}
	a.mu.Lock()
	data, err := os.ReadFile(filepath.Join(a.dir, day+".jsonl"))
	a.mu.Unlock()
	if os.IsNotExist(err) {
		return []rcon.ChatMsg{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []rcon.ChatMsg{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m rcon.ChatMsg
		if json.Unmarshal(line, &m) == nil { // a torn last line is skipped
			out = append(out, m)
		}
	}
	return out, nil
}

// recent returns up to n of the newest archived messages, oldest first —
// used to refill the live feed after a manager restart.
func (a *chatArchive) recent(n int) []rcon.ChatMsg {
	var out []rcon.ChatMsg
	for _, day := range a.dayFiles() {
		msgs, err := a.readDay(day)
		if err != nil {
			continue
		}
		out = append(msgs, out...)
		if len(out) >= n {
			break
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func chatMatches(m rcon.ChatMsg, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(m.Text), q) ||
		strings.Contains(strings.ToLower(m.Name), q) ||
		strings.Contains(strings.ToLower(m.To), q)
}

// ---------------------------------------------------------------- handlers

func (h *handlers) registerChat(mux *http.ServeMux) {
	mux.HandleFunc("/api/chat/days", methods(h.chatDays, http.MethodGet))
	mux.HandleFunc("/api/chat/archive", methods(h.chatArchiveDay, http.MethodGet))
	mux.HandleFunc("/api/chat/export", methods(h.chatExport, http.MethodGet))
}

func (h *handlers) chatDays(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"days": h.chat.days(), "keepDays": h.app.Cfg().ChatKeepDays})
}

func (h *handlers) chatArchiveDay(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	msgs, err := h.chat.readDay(day)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := 5000
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	out := make([]rcon.ChatMsg, 0, len(msgs))
	for _, m := range msgs {
		if chatMatches(m, q) {
			out = append(out, m)
		}
	}
	truncated := false
	if len(out) > limit {
		out, truncated = out[len(out)-limit:], true
	}
	writeJSON(w, map[string]interface{}{"date": day, "messages": out, "total": len(msgs), "truncated": truncated})
}

// chatExport downloads one day as plain text, one line per message.
func (h *handlers) chatExport(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	msgs, err := h.chat.readDay(day)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var b strings.Builder
	for _, m := range msgs {
		who := m.Name
		switch m.Source {
		case rcon.ChatFromManager:
			who = "[Manager]"
			if m.To != "" {
				who += " → " + m.To
			}
		case rcon.ChatFromAdmin:
			who = "[" + m.Name + "]"
		}
		fmt.Fprintf(&b, "%s [%s] %s: %s\r\n", time.UnixMilli(m.At).Format("15:04:05"), m.Channel, who, m.Text)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="chat-`+day+`.txt"`)
	_, _ = w.Write([]byte("\xef\xbb\xbf" + b.String())) // BOM: Notepad shows Cyrillic/CJK names right
}
