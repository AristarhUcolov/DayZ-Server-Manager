// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"path/filepath"

	"dayzmanager/internal/config"
	"dayzmanager/internal/validator"
)

// beforeStart runs right before the DayZ server starts: the port check, then
// putting back what a server update may have replaced while the manager ran —
// the RCon password in BattlEye's config, the registration of the panel's mod
// vehicles, and the fresh-spawn loadouts a stock cfggameplay.json no longer
// lists — and writing the panel's server-side tweaks mod for the current
// settings. A repair that fails is logged, never a reason not to start.
func (f *Fleet) beforeStart(a *App) error {
	if err := f.portClash(a); err != nil {
		return err
	}
	a.healBeforeStart()
	return nil
}

func (a *App) healBeforeStart() {
	a.SyncBEConfig()
	// Before the launch arguments are built: it may add or drop the mod.
	if err := a.SyncServerTweaks(); err != nil {
		a.Log.Printf("before start: server tweaks: %v", err)
	}
	cfg := a.Cfg()
	sc, err := config.LoadServerCfg(filepath.Join(a.ServerDir, cfg.ServerCfg))
	if err != nil {
		return
	}
	mission := sc.MissionTemplate()
	if mission == "" {
		return
	}
	// The Start button and "start with the manager" already hold WriteMu
	// around Start (a plain Lock here would deadlock them); a restart from
	// the scheduler does not, and then the repair takes it itself.
	if a.WriteMu.TryLock() {
		defer a.WriteMu.Unlock()
	}
	done, err := validator.HealAfterUpdate(validator.Options{ServerDir: a.ServerDir, Mission: mission, ServerCfg: cfg.ServerCfg})
	for _, d := range done {
		a.Log.Printf("before start: %s (the server files were replaced)", d)
	}
	if err != nil {
		a.Log.Printf("before start: %v", err)
	}
}
