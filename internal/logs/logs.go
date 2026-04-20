// Copyright (c) 2026 Aristarh Ucolov.
//
// Log discovery and tailing.
//
// The panel exposes four streams:
//
//   stdout — captured by the manager when it launches DayZServer_x64
//   RPT    — DayZ's own runtime profile log, at <profiles>/*.RPT
//   ADM    — admin log, at <profiles>/*.ADM
//   script — script debug log, at <profiles>/scripts/scripts.log (DZ 1.20+)
//
// RPT / ADM names are per-launch ("DayZServer_x64_YYYY-MM-DD_HH-MM-SS.RPT").
// We pick the newest one each time the user opens the Logs tab.
package logs

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Source struct {
	ID    string `json:"id"`    // stable id used by the frontend
	Label string `json:"label"` // short human label
	Path  string `json:"path"`  // absolute path (resolved at request time)
	Exists bool  `json:"exists"`
	Size  int64  `json:"size"`
}

// Discover returns the four sources with the latest-by-mtime file per kind.
// Pass profilesDir absolute or relative to serverDir.
func Discover(serverDir, profilesDir string) []Source {
	if !filepath.IsAbs(profilesDir) {
		profilesDir = filepath.Join(serverDir, profilesDir)
	}
	stdout := filepath.Join(serverDir, ".dayz-manager", "server.stdout.log")
	return []Source{
		fillStat(Source{ID: "stdout", Label: "Manager stdout", Path: stdout}),
		fillStat(Source{ID: "rpt", Label: "DayZ RPT", Path: newestByExt(profilesDir, ".rpt")}),
		fillStat(Source{ID: "adm", Label: "Admin log (ADM)", Path: newestByExt(profilesDir, ".adm")}),
		fillStat(Source{ID: "script", Label: "Script log", Path: filepath.Join(profilesDir, "scripts", "scripts.log")}),
	}
}

// Resolve returns the path for a given source id, or empty string if unknown.
func Resolve(serverDir, profilesDir, id string) string {
	for _, s := range Discover(serverDir, profilesDir) {
		if s.ID == id {
			return s.Path
		}
	}
	return ""
}

func fillStat(s Source) Source {
	if s.Path == "" {
		return s
	}
	if st, err := os.Stat(s.Path); err == nil {
		s.Exists = true
		s.Size = st.Size()
	}
	return s
}

// newestByExt returns the newest-mtime file in dir with a matching extension
// (case-insensitive). Empty string if none.
func newestByExt(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best os.FileInfo
	var bestPath string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ext) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == nil || info.ModTime().After(best.ModTime()) {
			best = info
			bestPath = filepath.Join(dir, e.Name())
		}
	}
	if bestPath == "" {
		// sort by name desc as a fallback (DayZ timestamps in filename)
		names := []string{}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
				names = append(names, e.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		if len(names) > 0 {
			bestPath = filepath.Join(dir, names[0])
		}
	}
	return bestPath
}

// ---------------------------------------------------------------------------
// Tail.

// Tail opens path, seeks to the end minus tailBytes (or 0), and then streams
// appended content to out until ctx is done. Handles file rotation (size
// shrink) by re-opening the file.
func Tail(ctx <-chan struct{}, path string, tailBytes int64, out func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	offset := st.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx:
			return nil
		default:
		}
		n, err := f.Read(buf)
		if n > 0 {
			if werr := out(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			// Detect rotation: new file size < our current position.
			cur, _ := f.Seek(0, io.SeekCurrent)
			if st2, serr := os.Stat(path); serr == nil && st2.Size() < cur {
				_ = f.Close()
				nf, nerr := os.Open(path)
				if nerr != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				f = nf
			}
			select {
			case <-ctx:
				return nil
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		if err != nil {
			return err
		}
	}
}
