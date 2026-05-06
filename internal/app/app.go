// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
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

	cfgPath := filepath.Join(managerDir, "manager.json")
	cfg, err := config.LoadManager(cfgPath)
	if err != nil {
		return nil, err
	}

	ctrl := server.NewController(serverDir, cfg, logger)
	rc := rcon.NewManager()
	// Wire the broadcaster so auto-restart can announce a countdown.
	ctrl.Broadcast = rc

	return &App{
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
	}, nil
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
		port = a.Config.ServerPort + 1 // DayZ default
	}
	a.RCon.Configure("127.0.0.1", port, password)
}

func (a *App) SaveConfig() error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return config.SaveManager(a.configPath, a.Config)
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
	return nil
}

func (a *App) Close() error {
	if a.Server != nil {
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
