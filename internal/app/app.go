// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/rcon"
	"dayzmanager/internal/server"
	"dayzmanager/internal/util"
)

// ErrConfigSave wraps a persistence failure from MutateConfig so callers can map
// it to a 500 while treating fn's own errors (bad input) as a 400.
var ErrConfigSave = errors.New("save config")

// App is the shared application context passed to every subsystem.
type App struct {
	Name, Version, Author string

	ServerDir  string
	ManagerDir string
	Log        *log.Logger
	Config     *config.Manager
	configMu   sync.Mutex
	configPath string

	Server *server.Controller
	RCon   *rcon.Manager

	// WriteMu serializes every state-mutating operation across subsystems
	// (mods install/update/uninstall, types/cfg writes, moded create/delete).
	// Prevents a second POST from racing with the requireStopped check.
	WriteMu sync.Mutex

	// Mode is how this manager runs: "web" (console + browser) or "panel"
	// (desktop app window). Set by the launcher.
	Mode string
	// RequestExit asks the manager process to exit (set by the launcher);
	// stopServer decides whether the DayZ server goes down with it.
	RequestExit func(stopServer bool)
	// workshop: the SteamCMD update hooks (exitinfo.go).
	workshop WorkshopHooks
	// LastExit is how the previous run of the manager ended when that was not
	// a normal exit (set by the launcher on the primary; nil otherwise).
	LastExit *ExitInfo
	// KeepingAwake reports whether the manager currently holds the computer
	// awake (set by the launcher).
	KeepingAwake func() bool
	// ShowWindow brings the desktop window to the front (panel mode only):
	// a second launch of the panel asks the running one to show itself.
	ShowWindow func() bool
	// Start menu / applications-menu and desktop shortcuts (set by the
	// launcher; nil where it cannot make them).
	ShortcutsInstalled func() bool
	InstallShortcuts   func() ([]string, error)
	// Starting the manager with the computer (Settings → General), set by
	// the launcher; nil where it cannot.
	Autostart    func() AutostartInfo
	SetAutostart func(on bool) error

	query queryState // Steam A2S view of the server (see query.go)

	// Multi-server (fleet.go). ID names this server in the manager ("main"
	// for the folder the manager started in); label is its display name.
	// Primary is that first server when this App is one added from the
	// Servers page (nil for the primary itself): panel-wide settings —
	// language, exposure, HTTPS, router forwards — live in its config only.
	ID      string
	Primary *App
	labelMu sync.Mutex
	label   string

	stop     chan struct{} // closed by Shutdown: ends the app's own loops
	stopOnce sync.Once

	// Discord (discord.go): the posting queue and the live status message.
	dqOnce sync.Once
	dq     *discordQueue
	status statusState

	logFile *os.File // manager.log of this server (closed by CloseLog)
}

// New opens the manager for the DayZ server in serverDir — the primary server,
// the folder the manager runs from.
func New(serverDir, name, version, author string) (*App, error) {
	return newApp(serverDir, appOptions{name: name, version: version, author: author, id: PrimaryID})
}

// NewServer opens one more DayZ server for a multi-server manager. It gets its
// own .dayz-manager folder in serverDir, exactly as if a separate manager ran
// there; the panel-wide settings come from primary.
func NewServer(serverDir string, primary *App, id, label string) (*App, error) {
	return newApp(serverDir, appOptions{name: primary.Name, version: primary.Version, author: primary.Author,
		id: id, label: label, primary: primary})
}

type appOptions struct {
	name, version, author string
	id, label             string
	primary               *App
}

func newApp(serverDir string, o appOptions) (*App, error) {
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
	ok := false
	defer func() {
		if !ok {
			_ = logFile.Close() // a failed open must not keep the folder locked
		}
	}()
	var logger *log.Logger
	if o.primary == nil {
		logger = log.New(newTee(os.Stdout, logFile), "", log.LstdFlags)
		// Libraries write to the standard logger (the desktop window's
		// browser engine, say — and its log.Fatal ends the program): keep
		// those lines in the manager log too, not on a console the windowed
		// build does not have.
		log.SetOutput(newTee(os.Stdout, logFile))
		// Route the mods package's copy failures into the shared manager log.
		mods.Logger = logger
		// Route opt-in RCon protocol traces (DAYZ_RCON_DEBUG=1) into the same log.
		rcon.Logger = logger
	} else {
		// The console shows every server's lines; the prefix says whose.
		logger = log.New(newTee(os.Stdout, logFile), "["+o.label+"] ", log.LstdFlags|log.Lmsgprefix)
	}

	cfgPath := filepath.Join(managerDir, "manager.json")
	_, statErr := os.Stat(cfgPath)
	fresh := errors.Is(statErr, os.ErrNotExist)
	cfg, err := config.LoadManager(cfgPath)
	var recovered *config.RecoveredError
	if errors.As(err, &recovered) {
		logger.Printf("WARNING: %v", recovered)
	} else if err != nil {
		return nil, err
	}
	if fresh && o.primary != nil {
		// A server added to the panel starts with the primary's mod source:
		// the same PC, the same DayZ client and Workshop folder.
		p := o.primary.Cfg()
		cfg.VanillaDayZPath, cfg.ModSource, cfg.ModsFolderPath = p.VanillaDayZPath, p.ModSource, p.ModsFolderPath
		cfg.FirstRunDone = true
		if err := config.SaveManager(cfgPath, cfg); err != nil {
			return nil, err
		}
	}

	ctrl := server.NewController(serverDir, cfg, logger)
	rc := rcon.NewManager()
	// Wire the broadcaster so auto-restart can announce a countdown.
	ctrl.Broadcast = rc

	// Mod auto-update hooks. Injected as closures so the server package needn't
	// import mods. They take the mod source dir from the controller's race-safe
	// schedule snapshot, closing over only the constant server dir — never the
	// shared *config.Manager.
	var a *App // the hooks below run long after it is set
	steamcmdMode := func() (WorkshopHooks, bool) {
		if a == nil || a.Cfg().EffectiveModSource() != config.ModSourceSteamCMD {
			return WorkshopHooks{}, false
		}
		hk := a.workshopHooks()
		return hk, hk.Sync != nil && hk.Outdated != nil
	}
	ctrl.UpdateMods = func(sourceDir string) ([]string, error) {
		if hk, ok := steamcmdMode(); ok {
			// Steam Workshop source: download what is newer on the
			// Workshop with SteamCMD, then install it.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			return hk.Sync(ctx)
		}
		return mods.UpdateAll(serverDir, sourceDir)
	}
	ctrl.ModUpdatesAvailable = func(sourceDir string) (bool, error) {
		if hk, ok := steamcmdMode(); ok {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			n, err := hk.Outdated(ctx)
			return n > 0, err
		}
		list, err := mods.List(serverDir, sourceDir)
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

	a = &App{
		Name:       o.name,
		Version:    o.version,
		Author:     o.author,
		ServerDir:  serverDir,
		ManagerDir: managerDir,
		Log:        logger,
		Config:     cfg,
		configPath: cfgPath,
		Server:     ctrl,
		RCon:       rc,
		ID:         o.id,
		Primary:    o.primary,
		label:      o.label,
		stop:       make(chan struct{}),
		logFile:    logFile,
	}
	if a.label == "" {
		a.label = filepath.Base(serverDir)
	}

	// The controller's own starts (watchdog, restart, retry) and the mod
	// update/profile swap before them take the same lock as every web write.
	ctrl.WriteLock = &a.WriteMu

	// Lifecycle events → Discord. The controller calls this from its own
	// goroutines; the HTTP POST runs detached so nothing in server management
	// ever waits on Discord.
	ctrl.Notify = a.Notify

	// Online-player-count hook for smart "restart only when empty". Returns -1
	// when the count can't be determined (server down / RCon unavailable), so the
	// controller falls back to a normal countdown instead of deferring forever.
	ctrl.PlayerCount = func() int {
		if !ctrl.IsRunning() {
			return -1
		}
		ps, err := rc.PlayersCached(15 * time.Second)
		if err != nil {
			// No RCon: the Steam server-browser query still counts players.
			return a.QueryPlayers()
		}
		return len(ps)
	}

	// RCon-backed server control: a clean #shutdown before any force-kill, and
	// the pre-restart lock/kick window. All three fail fast (and the controller
	// falls back) when RCon isn't configured.
	ctrl.GracefulShutdown = func() error {
		if !rc.Configured() {
			return errors.New("RCon is not configured")
		}
		return rc.Shutdown()
	}
	ctrl.LockServer = func(lock bool) error {
		if !rc.Configured() {
			return errors.New("RCon is not configured")
		}
		return rc.Lock(lock)
	}
	ctrl.KickAll = func(reason string) (int, error) {
		if !rc.Configured() {
			return 0, errors.New("RCon is not configured")
		}
		return rc.KickAll(reason)
	}

	// RCon reads its login from BattlEye's files / manager.json whenever it
	// dials, so the scheduler's countdown warnings and announcements can
	// connect without waiting for a Settings save. If a password is already
	// configured but the BE file is missing (e.g. carried over in
	// manager.json), materialise it.
	rc.SetCredentials(a.rconCreds)
	a.SyncBEConfig()

	// Pick up a DayZ server still running from an earlier manager session
	// (closed panel, crash, exe update) so it is supervised — not shown as
	// "stopped" while a second Start would fail on the port.
	ctrl.AdoptRunning()

	// Start the scheduling supervisor once, at boot. The loops it spawns
	// (daily restarts, announcements, interval auto-restart, ...) each gate on
	// IsRunning() and the relevant config flags, so they idle harmlessly until
	// the server is up and a schedule is configured.
	ctrl.StartAutoRestartLoop()
	go util.SafeLoop(a.Log, "Steam query loop", a.queryLoop)
	go util.SafeLoop(a.Log, "Discord status loop", a.discordStatusLoop)

	ok = true
	return a, nil
}

// ApplyRConConfig makes the RCon manager re-read its login now. Call after any
// config update or server restart.
func (a *App) ApplyRConConfig() {
	a.RCon.Reload()
}

// rconCreds is the RCon login, re-read by the RCon manager before it dials.
//
// While the server runs, BattlEye's beserver_x64_active_*.cfg is the truth:
// BattlEye renames beserver_x64.cfg to it at start and keeps that password
// (and port) until the next restart. A password changed in the panel or in
// beserver_x64.cfg since then is not live yet — and when there was no
// beserver_x64.cfg at all, BattlEye made up a password of its own. So the live
// one goes first and the others (manager.json, then beserver_x64.cfg — DayZ's
// own place for it, so most users never type it twice) are tried after it.
func (a *App) rconCreds() rcon.Creds {
	return a.rconCredsFor(a.Server.IsRunning())
}

func (a *App) rconCredsFor(running bool) rcon.Creds {
	cfg := a.Cfg() // snapshot: MutateConfig rewrites *a.Config concurrently
	var active *config.BEConfig
	if running {
		active = a.ActiveBEConfig()
	}
	file := config.FindBEConfig(a.beResolvedDir())

	c := rcon.Creds{Host: "127.0.0.1"}
	switch {
	case active != nil && active.RConPort != 0:
		c.Port = active.RConPort
	case cfg.RConPort != 0:
		c.Port = cfg.RConPort
	case file != nil && file.RConPort != 0:
		c.Port = file.RConPort
	default:
		// +4 keeps RCon clear of the game port and the Steam query ports
		// (ServerPort .. +3), which DayZ also opens.
		c.Port = cfg.ServerPort + 4
	}
	// BattlEye listens on every interface unless RConIP names one address.
	for _, be := range []*config.BEConfig{active, file} {
		if be != nil {
			if ip := strings.TrimSpace(be.RConIP); ip != "" && ip != "0.0.0.0" && ip != "::" && ip != "*" {
				c.Host = ip
			}
			break
		}
	}
	if active != nil {
		c.Passwords = append(c.Passwords, active.RConPassword)
	}
	c.Passwords = append(c.Passwords, cfg.RConPassword)
	if file != nil {
		c.Passwords = append(c.Passwords, file.RConPassword)
	}
	return c
}

// ActiveBEConfig is the config BattlEye loaded for the current run (nil when
// none is found). It lives in the BattlEye folder; a server started without
// -BEpath keeps it under the profiles folder instead.
func (a *App) ActiveBEConfig() *config.BEConfig {
	if be := config.FindActiveBEConfig(a.beResolvedDir()); be != nil {
		return be
	}
	prof := a.Cfg().ProfilesDir
	if prof == "" {
		prof = "profiles"
	}
	if !filepath.IsAbs(prof) {
		prof = filepath.Join(a.ServerDir, prof)
	}
	return config.FindActiveBEConfig(filepath.Join(prof, "BattlEye"))
}

// beResolvedDir returns the absolute BattlEye directory from config.
func (a *App) beResolvedDir() string {
	beDir := a.Cfg().BEPath
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
	cfg := a.Cfg()
	if cfg.RConPassword == "" {
		return
	}
	// A port the admin set explicitly is written even over an existing
	// RConPort — ApplyRConConfig connects to it, so BattlEye must listen there.
	// The derived default never clobbers a port already in the file.
	port, forcePort := cfg.RConPort, cfg.RConPort != 0
	if port == 0 {
		port = cfg.ServerPort + 4
	}
	changed, err := config.EnsureBEConfig(a.beResolvedDir(), cfg.RConPassword, port, forcePort)
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

// Exposure is the panel exposure mode, read without copying the whole config
// (the request guard asks on every request).
func (a *App) Exposure() string {
	if a.Primary != nil {
		return a.Primary.Exposure()
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return a.Config.Exposure
}

// ScheduleSnapshotSync refreshes the server controller's schedule snapshot
// from a consistent copy of the config (never from the live pointer).
func (a *App) ScheduleSnapshotSync() {
	cfg := a.Cfg()
	a.Server.SetScheduleConfig(&cfg)
}

// Cfg returns a snapshot of the configuration, taken under the same lock that
// MutateConfig commits under.
//
// internal/server solved this for its supervisor loops with schedSnapshot; the
// web layer had no equivalent and read the shared *config.Manager directly from
// ~50 places, three of them background goroutines on timers. `go test -race`
// reported a genuine race between MutateConfig's wholesale replace and
// dashboardMetrics, which polls every 5s and ranges over the Mods slice — a
// torn slice header there is a panic, not a stale read.
//
// Slices are copied because copying the struct alone shares their backing
// arrays, which is exactly the dangerous part.
func (a *App) Cfg() config.Manager {
	a.configMu.Lock()
	c := a.Config.Clone()
	a.configMu.Unlock()
	if a.Primary != nil {
		// Taken after releasing our lock: the only lock order between two
		// apps is added server → primary, never the other way.
		p := a.Primary.Cfg()
		panelWide(&c, &p)
	}
	return c
}

// panelWide copies the settings that belong to the whole panel rather than to
// one DayZ server — interface language, exposure, HTTPS, router forwards, the
// first-run flag and the dismissed checklist — from src into dst.
func panelWide(dst, src *config.Manager) {
	dst.FirstRunDone = src.FirstRunDone
	dst.SetupDismissed = src.SetupDismissed
	dst.Language = src.Language
	dst.Exposure = src.Exposure
	dst.PanelHTTPS = src.PanelHTTPS
	dst.UPnPPanel = src.UPnPPanel
	dst.UPnPGame = src.UPnPGame
	dst.RemoteWatch = src.RemoteWatch
	dst.SteamUser = src.SteamUser
	dst.KeepAwake = src.KeepAwake
}

func panelWideEqual(a, b *config.Manager) bool {
	return a.FirstRunDone == b.FirstRunDone && a.SetupDismissed == b.SetupDismissed && a.Language == b.Language && a.Exposure == b.Exposure &&
		a.PanelHTTPS == b.PanelHTTPS && a.UPnPPanel == b.UPnPPanel && a.UPnPGame == b.UPnPGame &&
		a.RemoteWatch == b.RemoteWatch && a.SteamUser == b.SteamUser && a.KeepAwake == b.KeepAwake
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
	// A deep copy: fn may edit slices/maps in place without touching data
	// that Cfg() snapshots handed out earlier are still reading.
	working := a.Config.Clone()
	if a.Primary != nil {
		// fn sees (and may change) the live panel-wide values; a change to
		// them is the primary's to save.
		p := a.Primary.Cfg()
		panelWide(&working, &p)
		before := working
		if err := fn(&working); err != nil {
			return err
		}
		if !panelWideEqual(&before, &working) {
			changed := working
			if err := a.Primary.MutateConfig(func(pc *config.Manager) error {
				panelWide(pc, &changed)
				return nil
			}); err != nil {
				return err
			}
		}
	} else if err := fn(&working); err != nil {
		return err
	}
	// Persist before committing: on a failed save the live config must stay
	// what is on disk and in the scheduler snapshot, not show unsaved values.
	if err := config.SaveManager(a.configPath, &working); err != nil {
		return fmt.Errorf("%w: %v", ErrConfigSave, err)
	}
	*a.Config = working
	// Refresh the controller's race-safe snapshot on EVERY commit — buildArgs
	// and the scheduler loops read only the snapshot, so a mutation that skips
	// this would launch/schedule with stale values.
	a.Server.SetScheduleConfig(a.Config)
	return nil
}

// ReloadConfig re-reads manager.json from disk and replaces the in-memory
// config. Used after a backup restore so the rest of the process sees the
// restored values without a restart.
func (a *App) ReloadConfig() error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := config.LoadManager(a.configPath)
	var recovered *config.RecoveredError
	if errors.As(err, &recovered) {
		// The file was unreadable and LoadManager put defaults in its place:
		// keep the settings in memory — better than defaults — and write them
		// back, so disk and memory agree. The caller reports the error.
		if serr := config.SaveManager(a.configPath, a.Config); serr != nil {
			return fmt.Errorf("%v; restoring the previous settings failed too: %w", err, serr)
		}
		return err
	}
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
	a.endLoops()
	return nil
}

// endLoops stops the app's own background work (Steam query loop, RCon
// connection) — for an app that is closed or removed from the panel.
func (a *App) endLoops() {
	a.stopOnce.Do(func() {
		if a.stop != nil {
			close(a.stop)
		}
		if a.RCon != nil {
			a.RCon.Close()
		}
	})
}

// CloseLog stops writing this server's manager.log — a server removed from the
// panel must not keep a file in its folder open. Later lines (if any) still
// reach the console.
func (a *App) CloseLog() {
	if a.Log != nil {
		a.Log.SetOutput(os.Stdout)
	}
	if a.logFile != nil {
		_ = a.logFile.Close()
		a.logFile = nil
	}
}

// Label is the server's display name in the panel.
func (a *App) Label() string {
	a.labelMu.Lock()
	defer a.labelMu.Unlock()
	return a.label
}

// SetLabel renames the server in the panel (and in its log lines).
func (a *App) SetLabel(name string) {
	a.labelMu.Lock()
	a.label = name
	a.labelMu.Unlock()
	if a.Primary != nil {
		a.Log.SetPrefix("[" + name + "] ")
	}
}

// Detach stops the manager's supervision without stopping the DayZ server —
// used when the admin exits the panel but keeps the server running. The next
// manager start adopts the still-running server.
func (a *App) Detach() {
	if a.Server != nil {
		a.Server.StopAutoRestartLoop()
	}
	a.endLoops()
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
