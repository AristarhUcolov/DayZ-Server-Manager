// Copyright (c) 2026 Aristarh Ucolov.

package web

import (
	"bytes"
	"os"
	"sync"
	"time"

	"dayzmanager/internal/server"
)

// serverKnows reports whether the DayZ server program in serverDir reads a
// serverDZ.cfg key — new keys come with game updates (thirdPersonView with
// 1.30), and the panel offers them only when the server will use them. The
// program is searched for the key's name once per build (its size and time).
func serverKnows(serverDir, key string) bool {
	exe, ok := server.FindServerExe(serverDir)
	if !ok {
		return false
	}
	st, err := os.Stat(exe)
	if err != nil {
		return false
	}
	ck := exeKey{path: exe, size: st.Size(), mod: st.ModTime(), key: key}
	exeKeys.Lock()
	defer exeKeys.Unlock()
	if v, ok := exeKeys.seen[ck]; ok {
		return v
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return false
	}
	v := bytes.Contains(data, []byte(key))
	if exeKeys.seen == nil {
		exeKeys.seen = map[exeKey]bool{}
	}
	exeKeys.seen[ck] = v
	return v
}

type exeKey struct {
	path string
	size int64
	mod  time.Time
	key  string
}

var exeKeys struct {
	sync.Mutex
	seen map[exeKey]bool
}
