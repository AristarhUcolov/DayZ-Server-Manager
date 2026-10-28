// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"time"

	"dayzmanager/internal/util"
)

// AutostartInfo describes starting the manager together with the computer
// (Settings → General). The launcher fills it in; the panel shows it.
type AutostartInfo struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Kind      string `json:"kind"`              // "registry" (Windows sign-in) | "xdg" (Linux desktop login)
	Detail    string `json:"detail,omitempty"`  // the command that runs, or the autostart file
	Desktop   bool   `json:"desktop"`           // a desktop session exists (Linux: else see Systemd)
	Systemd   string `json:"systemd,omitempty"` // a service unit for Linux servers without a desktop
	Error     string `json:"error,omitempty"`
}

// StartOnLaunch starts this DayZ server shortly after the manager starts when
// its settings ask for it ("Start the server with the manager") and it is not
// already running — a server the manager adopted is left alone.
func (a *App) StartOnLaunch(delay time.Duration) {
	cfg := a.Cfg()
	if !cfg.StartServerOnLaunch || !cfg.FirstRunDone {
		return
	}
	go util.SafeGo(a.Log, "start with the manager", func() {
		time.Sleep(delay)
		if a.ServerIsRunning() {
			return
		}
		a.WriteMu.Lock()
		err := a.Server.Start()
		a.WriteMu.Unlock()
		if err != nil {
			a.Log.Printf("start with the manager: %v", err)
			return
		}
		a.Log.Printf("started the DayZ server (Start with the manager is on)")
	})
}
