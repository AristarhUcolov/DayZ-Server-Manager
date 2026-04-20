// Copyright (c) 2026 Aristarh Ucolov.
//
// Small helper used everywhere the manager rewrites a DayZ-owned file.
// Before the new contents are written we snapshot the current file to
// `<file>.bak.<YYYYMMDD-HHMMSS>`, then trim all but the five newest
// backups sitting next to it. Lets the user recover from a bad save
// without running a full Git repo over mpmissions.
package util

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxBackups = 5

// BackupBeforeWrite copies path → path.bak.<timestamp> if the file exists.
// It is a best-effort operation; callers ignore errors (the worst case is
// we failed to save a backup — the actual save will still proceed).
func BackupBeforeWrite(path string) error {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil // nothing to back up
	}
	ts := time.Now().Format("20060102-150405")
	bak := path + ".bak." + ts
	if err := copyFile(path, bak); err != nil {
		return err
	}
	pruneOld(path)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func pruneOld(path string) {
	dir := filepath.Dir(path)
	base := filepath.Base(path) + ".bak."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type bak struct {
		name string
		mod  time.Time
	}
	var list []bak
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), base) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, bak{e.Name(), info.ModTime()})
	}
	if len(list) <= maxBackups {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	for _, b := range list[maxBackups:] {
		_ = os.Remove(filepath.Join(dir, b.name))
	}
}
