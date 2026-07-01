// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/rcon"
	"dayzmanager/internal/server"
)

// ErrConfigSave wraps a persistence failure from MutateConfig so callers can map
// it to a 500 while treating fn's own errors (bad input) as a 400.
var ErrConfigSave = errors.New("save config")

// App is the shared application context passed to every subsystem.
type App struct {
	Name, Version, Author string

	ServerDir   string
	ManagerDir  string
	Log         *log.Logger
	Config      *config.Manager
	configMu    sync.Mutex
	configPath  string

	Server *server.Controller
	RCon   *rcon.Manager

	// WriteMu serializes every state-mutating operation across subsystems
	// (mods install/update/uninstall, types/cfg writes, moded create/delete).
	// Prevents a second POST from racing with the requireStopped check.
	WriteMu sync.Mutex
}

func New(serverDir, name, version, author string) (*App, error) {
	managerDir := filepath.Join(serverDir, ".dayz-manager")
	if err := os.MkdirAll(managerDir, 0o755); err != nil {
		return nil, fmt.Errorf("create manager dir: %w", err)
	}

	logFile, err := os.OpenFile(
		filepath.Join(managerDir, "manager.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	logger := log.New(newTee(os.Stdout, logFile), "", log.LstdFlags)
	// Route the mods package's copy failures into the shared manager log.
	mods.Logger = logger
	// Route opt-in RCon protocol traces (DAYZ_RCON_DEBUG=1) into the same log.
	rcon.Logger = logger

	cfgPath := filepath.Join(managerDir, "manager.json")
	cfg, err := config.LoadManager(cfgPath)
	if err != nil {
		return nil, err
	}

	ctrl := server.NewController(serverDir, cfg, logger)
	rc := rcon.NewManager()
	// Wire the broadcaster so auto-restart can announce a countdown.
	ctrl.Broadcast = rc

	// Mod auto-update hooks (item 2). Injected as closures so the server
	// package needn't import mods. They take the vanilla path from the
	// controller's race-safe schedule snapshot, closing over only the constant
	// server dir — never the shared *config.Manager.
	ctrl.UpdateMods = func(vanillaPath string) ([]string, error) {
		return mods.UpdateAll(serverDir, vanillaPath)
	}
	ctrl.ModUpdatesAvailable = func(vanillaPath string) (bool, error) {
		list, err := mods.List(serverDir, vanillaPath)
		if err != nil {
			return false, err
		}
		for _, m := range list {
			if m.UpdateAvailable {
				return true, nil
			}
		}
		return false, nil
	}

	a := &App{
		Name:       name,
		Version:    version,
		Author:     author,
		ServerDir:  serverDir,
		ManagerDir: managerDir,
		Log:        logger,
		Config:     cfg,
		configPath: cfgPath,
		Server:     ctrl,
		RCon:       rc,
	}

	// Configure RCon from beserver_x64.cfg / manager.json up front so the
	// scheduler's countdown warnings and announcements can connect without
	// waiting for a Settings save. If a password is already configured but the
	// BE file is missing (e.g. carried over in manager.json), materialise it.
	a.ApplyRConConfig()
	a.SyncBEConfig()

	// Start the scheduling supervisor once, at boot. The three loops it spawns
	// (daily restarts, announcements, interval auto-restart) each gate on
	// IsRunning() and the relevant config flags, so they idle harmlessly until
	// the server is up and a schedule is configured. This call was previously
	// missing entirely, which is why scheduled restarts/announcements never
	// fired no matter what the user configured in Settings.
	ctrl.StartAutoRestartLoop()

	return a, nil
}

// ApplyRConConfig reconfigures the RCon manager from the current Config.
// Call after any config update or server restart.
//
// If the manager config does not have an RCon password set, we fall back to
// reading battleye/beserver_x64.cfg — that's where DayZ actually stores the
// credential, so most users never have to type it twice. The manager
// override still wins so the Settings page can force a specific password.
func (a *App) ApplyRConConfig() {
	port := a.Config.RConPort
	password := a.Config.RConPassword

	if password == "" || port == 0 {
		beDir := a.Config.BEPath
		if beDir != "" && !filepath.IsAbs(beDir) {
			beDir = filepath.Join(a.ServerDir, beDir)
		}
		if be := config.FindBEConfig(beDir); be != nil {
			if password == "" {
				password = be.RConPassword
			}
			if port == 0 && be.RConPort != 0 {
				port = be.RConPort
			}
		}
	}

	if port == 0 {
		// +4 keeps RCon clear of the game port and the Steam query ports
		// (ServerPort .. +3), which DayZ also opens.
		port = a.Config.ServerPort + 4
	}
	a.RCon.Configure("127.0.0.1", port, password)
}

// beResolvedDir returns the absolute BattlEye directory from config.
func (a *App) beResolvedDir() string {
	beDir := a.Config.BEPath
	if beDir == "" {
		beDir = "battleye"
	}
	if !filepath.IsAbs(beDir) {
		beDir = filepath.Join(a.ServerDir, beDir)
	}
	return beDir
}

// SyncBEConfig mirrors the RCon password from manager.json into BattlEye's
// beserver_x64.cfg so RCon is actually enabled on the next server start. No-op
// when no password is set. This is what makes the panel's "set RCon password"
// flow real instead of just storing a value the game never sees.
func (a *App) SyncBEConfig() {
	if a.Config.RConPassword == "" {
		return
	}
	port := a.Config.RConPort
	if port == 0 {
		port = a.Config.ServerPort + 4
	}
	changed, err := config.EnsureBEConfig(a.beResolvedDir(), a.Config.RConPassword, port)
	if err != nil {
		a.Log.Printf("ensure beserver_x64.cfg: %v", err)
		return
	}
	if changed {
		a.Log.Printf("RCon password written to beserver_x64.cfg (takes effect on next server start)")
	}
}

func (a *App) SaveConfig() error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return config.SaveManager(a.configPath, a.Config)
}

// MutateConfig atomically applies fn to a working copy of the config under
// configMu and, when fn succeeds, commits the copy and persists it. Serializing
// the read-modify-write under the same lock as SaveConfig/ReloadConfig means the
// wholesale `*a.Config = ...` replace can no longer tear against a concurrent
// save, reload, or another mutation. fn must not retain the pointer past return.
// A persistence failure is wrapped in ErrConfigSave; fn's own error is returned
// as-is so handlers can distinguish bad input (400) from a save failure (500).
func (a *App) MutateConfig(fn func(*config.Manager) error) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	working := *a.Config
	if err := fn(&working); err != nil {
		return err
	}
	*a.Config = working
	if err := config.SaveManager(a.configPath, a.Config); err != nil {
		return fmt.Errorf("%w: %v", ErrConfigSave, err)
	}
	return nil
}

// ReloadConfig re-reads manager.json from disk and replaces the in-memory
// config. Used after a backup restore so the rest of the process sees the
// restored values without a restart.
func (a *App) ReloadConfig() error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := config.LoadManager(a.configPath)
	if err != nil {
		return err
	}
	*a.Config = *cfg
	// Keep the scheduler snapshot in sync with the restored config.
	a.Server.SetScheduleConfig(a.Config)
	return nil
}

func (a *App) Close() error {
	if a.Server != nil {
		a.Server.StopAutoRestartLoop()
		_ = a.Server.Stop()
	}
	return nil
}

// ServerIsRunning is a convenience guard used across handlers.
// Most write endpoints must reject when the server is running because
// DayZServer holds file locks on its working set.
func (a *App) ServerIsRunning() bool {
	return a.Server.IsRunning()
}

// ---------------------------------------------------------------------------

type teeWriter struct{ a, b *os.File }

func newTee(a, b *os.File) *teeWriter { return &teeWriter{a: a, b: b} }

func (t *teeWriter) Write(p []byte) (int, error) {
	_, _ = t.a.Write(p)
	return t.b.Write(p)
}
