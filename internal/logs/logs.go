// Copyright (c) 2026 Aristarh Ucolov.
//
// Log discovery and tailing.
//
// The panel exposes four streams:
//
//	stdout — captured by the manager when it launches DayZServer_x64
//	RPT    — DayZ's own runtime profile log, at <profiles>/*.RPT
//	ADM    — admin log, at <profiles>/*.ADM
//	script — script debug log, at <profiles>/scripts/scripts.log (DZ 1.20+)
//
// RPT / ADM names are per-launch ("DayZServer_x64_YYYY-MM-DD_HH-MM-SS.RPT").
// We pick the newest one each time the user opens the Logs tab.
package logs

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Source struct {
	ID     string `json:"id"`    // stable id used by the frontend
	Label  string `json:"label"` // short human label
	Path   string `json:"path"`  // absolute path (resolved at request time)
	Exists bool   `json:"exists"`
	Size   int64  `json:"size"`
	// An earlier run's RPT / ADM (see WithPrevious): its kind ("rpt", "adm")
	// and when that run started (unix seconds; the file name's stamp, else
	// the file's last write).
	Kind    string `json:"kind,omitempty"`
	Prev    bool   `json:"prev,omitempty"`
	Started int64  `json:"started,omitempty"`
}

// PrevRuns is how many earlier RPT / ADM files the log viewer offers per kind.
// DayZ starts a new file on every launch, so after a crash and a watchdog
// restart the crashed run's log is no longer the newest one.
const PrevRuns = 5

// prevKinds are the per-launch logs whose earlier runs are listed.
var prevKinds = []struct{ id, ext, label string }{
	{"rpt", ".rpt", "DayZ RPT"},
	{"adm", ".adm", "Admin log (ADM)"},
}

// reRunStamp is the start time DayZ writes into a log's name:
// DayZServer_x64_2026-10-13_03-12-01.RPT.
var reRunStamp = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{2})`)

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

// WithPrevious is Discover plus the earlier runs' RPT and ADM files, newest
// first and up to PrevRuns per kind, as "rpt:<file>" / "adm:<file>".
func WithPrevious(serverDir, profilesDir string) []Source {
	out := Discover(serverDir, profilesDir)
	dir := absProfiles(serverDir, profilesDir)
	for _, k := range prevKinds {
		cur := ""
		for _, s := range out {
			if s.ID == k.id {
				cur = s.Path
			}
		}
		n := 0
		for _, f := range filesByAge(dir, k.ext) {
			if n == PrevRuns {
				break
			}
			path := filepath.Join(dir, f.Name())
			if path == cur {
				continue
			}
			n++
			out = append(out, Source{ID: k.id + ":" + f.Name(), Kind: k.id, Label: k.label, Path: path,
				Exists: true, Size: f.Size(), Prev: true, Started: runStarted(f)})
		}
	}
	return out
}

// Resolve returns the path for a given source id, or empty string if unknown.
// "rpt:<file>" / "adm:<file>" name an earlier run's log in the profiles folder.
func Resolve(serverDir, profilesDir, id string) string {
	if kind, name, ok := strings.Cut(id, ":"); ok {
		for _, k := range prevKinds {
			// A bare file name of the right kind: never a path out of the folder.
			if k.id != kind || name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) ||
				!strings.EqualFold(filepath.Ext(name), k.ext) {
				continue
			}
			path := filepath.Join(absProfiles(serverDir, profilesDir), name)
			if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
				return path
			}
		}
		return ""
	}
	for _, s := range Discover(serverDir, profilesDir) {
		if s.ID == id {
			return s.Path
		}
	}
	return ""
}

func absProfiles(serverDir, profilesDir string) string {
	if !filepath.IsAbs(profilesDir) {
		return filepath.Join(serverDir, profilesDir)
	}
	return profilesDir
}

// filesByAge lists the files in dir with a matching extension, newest first
// (by last write, then by name — the name carries the start time).
func filesByAge(dir, ext string) []os.FileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []os.FileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ext) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ModTime().Equal(out[j].ModTime()) {
			return out[i].ModTime().After(out[j].ModTime())
		}
		return out[i].Name() > out[j].Name()
	})
	return out
}

// runStarted is when the run that wrote a log started: the stamp in its name,
// else the file's last write.
func runStarted(f os.FileInfo) int64 {
	if m := reRunStamp.FindStringSubmatch(f.Name()); m != nil {
		n := make([]int, 6)
		for i := range n {
			n[i], _ = strconv.Atoi(m[i+1])
		}
		if n[1] >= 1 && n[1] <= 12 && n[2] >= 1 && n[2] <= 31 {
			return time.Date(n[0], time.Month(n[1]), n[2], n[3], n[4], n[5], 0, time.Local).Unix()
		}
	}
	return f.ModTime().Unix()
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
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	offset := st.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	return TailFrom(ctx, path, offset, out)
}

// TailFrom streams path starting at byte offset — typically where a snapshot
// the client already shows ended, so nothing is sent twice. An offset past the
// end means the file was replaced since, so it starts over from the top.
func TailFrom(ctx <-chan struct{}, path string, offset int64, out func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	// Closure, not "defer f.Close()": f is swapped on rotation below and the
	// replacement must be the one closed.
	defer func() { f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	if offset < 0 || offset > st.Size() {
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
				nf, nerr := os.Open(path)
				if nerr != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				_ = f.Close()
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
