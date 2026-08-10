// Copyright (c) 2026 Aristarh Ucolov.
//
// Global config search — a plain substring grep across the server's
// configuration and economy files (the same set the backup/profile snapshot
// covers). Answers "where is this class name / setting / value defined?" in one
// place instead of opening files one by one. Results carry a server-relative
// path when the file lives under the server dir, so the UI can deep-link into
// the Files editor.
package web

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type searchMatch struct {
	File string `json:"file"`           // display label (archive-style name)
	Path string `json:"path,omitempty"` // server-relative path for deep-linking
	Line int    `json:"line"`
	Text string `json:"text"`
}

const (
	searchMaxPerFile = 40
	searchMaxTotal   = 400
	searchMaxFile    = 12 * 1024 * 1024 // don't scan absurdly large files
	searchSnippetMax = 240
)

func (h *handlers) configSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, map[string]interface{}{"query": q, "matches": []searchMatch{}, "total": 0, "tooShort": true})
		return
	}
	needle := strings.ToLower(q)

	matches := []searchMatch{}
	total := 0
	truncated := false

	for _, it := range h.backupItems() {
		if total >= searchMaxTotal {
			truncated = true
			break
		}
		abs, label := it[0], it[1]
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() || st.Size() > searchMaxFile {
			continue
		}
		rel := ""
		if rp, err := filepath.Rel(h.app.ServerDir, abs); err == nil && !strings.HasPrefix(rp, "..") {
			rel = filepath.ToSlash(rp)
		}
		perFile := 0
		f, err := os.Open(abs)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			snip := strings.TrimSpace(line)
			if len(snip) > searchSnippetMax {
				snip = snip[:searchSnippetMax] + "…"
			}
			matches = append(matches, searchMatch{File: label, Path: rel, Line: lineNo, Text: snip})
			perFile++
			total++
			if perFile >= searchMaxPerFile || total >= searchMaxTotal {
				truncated = true
				break
			}
		}
		f.Close()
	}

	writeJSON(w, map[string]interface{}{
		"query":     q,
		"matches":   matches,
		"total":     total,
		"truncated": truncated,
	})
}
