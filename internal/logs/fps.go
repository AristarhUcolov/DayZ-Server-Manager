// Copyright (c) 2026 Aristarh Ucolov.

package logs

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// With logAverageFps = N in serverDZ.cfg (and the -doLogs launch flag) the
// server writes "Average server FPS: 59.97 (measured interval: 60 s)" into its
// .RPT log every N seconds. That line is the only place DayZ reports how fast
// the simulation runs, so the dashboard reads the newest one from there.
var reAvgFPS = regexp.MustCompile(`(?i)average\s+server\s+fps\s*:\s*([0-9]+(?:[.,][0-9]+)?)`)

// fpsWindow is how much of the end of the log is searched. Even a chatty
// server writes far less than this between two FPS lines at sane intervals.
const fpsWindow = 512 << 10

var fpsCache struct {
	sync.Mutex
	path string
	size int64
	mod  time.Time
	fps  float64
	ok   bool
}

// LastServerFPS returns the most recent average server FPS found in the RPT
// log at path and when that log was last written. ok is false when the file is
// missing or holds no FPS line (logAverageFps off, or the server just started).
// The result is cached until the file changes, so polling it is cheap.
func LastServerFPS(path string) (fps float64, written time.Time, ok bool) {
	if path == "" {
		return 0, time.Time{}, false
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, false
	}
	fpsCache.Lock()
	defer fpsCache.Unlock()
	if fpsCache.path == path && fpsCache.size == st.Size() && fpsCache.mod.Equal(st.ModTime()) {
		return fpsCache.fps, st.ModTime(), fpsCache.ok
	}
	fps, ok = scanFPS(path, st.Size())
	fpsCache.path, fpsCache.size, fpsCache.mod = path, st.Size(), st.ModTime()
	fpsCache.fps, fpsCache.ok = fps, ok
	return fps, st.ModTime(), ok
}

func scanFPS(path string, size int64) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	off := size - fpsWindow
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	n, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return 0, false
	}
	buf = buf[:n]
	// DayZ writes the RPT as UTF-8, but strip UTF-16 NULs just in case a tool
	// re-saved it, so the pattern still matches.
	if bytes.IndexByte(buf, 0) >= 0 {
		buf = bytes.ReplaceAll(buf, []byte{0}, nil)
	}
	all := reAvgFPS.FindAllSubmatch(buf, -1)
	if len(all) == 0 {
		return 0, false
	}
	v := bytes.Replace(all[len(all)-1][1], []byte{','}, []byte{'.'}, 1)
	fps, err := strconv.ParseFloat(string(v), 64)
	if err != nil || fps < 0 || fps > 10000 {
		return 0, false
	}
	return fps, true
}
