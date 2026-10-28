// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/a2s"
	"dayzmanager/internal/config"
	"dayzmanager/internal/util"
)

// QueryState is the latest Steam-query view of the server: whether it answers
// the server-browser query (i.e. has finished loading and is listed), and what
// it reports. Works with no RCon at all.
type QueryState struct {
	OK      bool      `json:"ok"`
	Info    *a2s.Info `json:"info,omitempty"`
	At      time.Time `json:"at"`
	Port    int       `json:"port"`
	Error   string    `json:"error,omitempty"`
	FirstOK time.Time `json:"firstOk,omitempty"` // first answer of this run
}

type queryState struct {
	mu  sync.Mutex
	st  QueryState
	gen time.Time // StartedAt of the run the state belongs to
}

// steamQueryPort reads steamQueryPort from server.cfg (DayZ default 27016).
func (a *App) steamQueryPort() int {
	cfg, err := config.LoadServerCfg(filepath.Join(a.ServerDir, a.Cfg().ServerCfg))
	if err == nil {
		if v, ok := cfg.Get("steamQueryPort"); ok {
			if n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(v), `";`)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 27016
}

// SteamQueryPort is the server's Steam query port (serverDZ.cfg, default 27016).
func (a *App) SteamQueryPort() int { return a.steamQueryPort() }

// queryLoop polls the server every 10 s while it runs.
func (a *App) queryLoop() {
	for {
		select {
		case <-a.stop:
			return
		case <-time.After(10 * time.Second):
		}
		started := a.Server.StartedAt()
		a.query.mu.Lock()
		if started.IsZero() || !started.Equal(a.query.gen) {
			a.query.st = QueryState{} // new run (or stopped): forget the old answers
			a.query.gen = started
		}
		a.query.mu.Unlock()
		if started.IsZero() {
			continue
		}
		port := a.steamQueryPort()
		info, err := a2s.Query(fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		a.query.mu.Lock()
		now := time.Now()
		a.query.st.At, a.query.st.Port = now, port
		if err != nil {
			a.query.st.OK, a.query.st.Error = false, err.Error()
		} else {
			a.query.st.OK, a.query.st.Info, a.query.st.Error = true, info, ""
			if a.query.st.FirstOK.IsZero() {
				a.query.st.FirstOK = now
				// The mission has loaded and the server is listed: players
				// can join now — the moment Discord's "online" means.
				detail := fmt.Sprintf("%d\x1f%d\x1f%s", info.Players, info.MaxPlayers, info.Map)
				go util.SafeGo(a.Log, "online notice", func() { a.Notify("online", detail) })
			}
		}
		a.query.mu.Unlock()
	}
}

// Query returns the latest Steam-query state (zero when the server is down).
func (a *App) Query() QueryState {
	a.query.mu.Lock()
	defer a.query.mu.Unlock()
	return a.query.st
}

// QueryPlayers returns the player count from the Steam query when it answered
// recently, or -1.
func (a *App) QueryPlayers() int {
	st := a.Query()
	if st.OK && st.Info != nil && time.Since(st.At) < 45*time.Second {
		return st.Info.Players
	}
	return -1
}
