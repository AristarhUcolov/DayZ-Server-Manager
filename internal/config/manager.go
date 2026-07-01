// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manager is the manager's own configuration (stored next to the exe under
// .dayz-manager/manager.json). Distinct from the DayZ server.cfg.
type Manager struct {
	FirstRunDone  bool   `json:"firstRunDone"`
	Language      string `json:"language"` // "ru" | "en"

	// Path to the *client* DayZ install that owns the Steam !Workshop folder.
	// Used to pull mods into the server dir on demand.
	VanillaDayZPath string `json:"vanillaDayZPath"`

	// Launch params (mirror the reference .bat file).
	ServerName  string   `json:"serverName"`
	ServerPort  int      `json:"serverPort"`
	ServerCfg   string   `json:"serverCfg"`
	CPUCount    int      `json:"cpuCount"`
	DoLogs      bool     `json:"doLogs"`
	AdminLog    bool     `json:"adminLog"`
	NetLog      bool     `json:"netLog"`
	FreezeCheck bool     `json:"freezeCheck"`
	FilePatching bool    `json:"filePatching"`
	BEPath      string   `json:"bePath"`      // absolute or relative to server dir
	ProfilesDir string   `json:"profilesDir"` // absolute or relative
	Mods        []string `json:"mods"`        // mod folder names, e.g. "@CF"
	ServerMods  []string `json:"serverMods"`  // mods only loaded server-side

	// Scheduling.
	AutoRestartSeconds int  `json:"autoRestartSeconds"` // 0 = disabled
	AutoRestartEnabled bool `json:"autoRestartEnabled"`

	// Mod auto-update.
	// AutoUpdateModsOnRestart: before any auto/scheduled restart, copy newer
	// mod files from the client !Workshop into the server dir (done in the
	// restart down-window while file locks are clear). No extra restarts.
	// AutoUpdateCheckMinutes: when > 0, poll the local !Workshop on this cadence
	// and, if any active mod is newer than the server copy, run an update +
	// restart-with-countdown. 0 disables the poller.
	AutoUpdateModsOnRestart bool `json:"autoUpdateModsOnRestart"`
	AutoUpdateCheckMinutes  int  `json:"autoUpdateCheckMinutes"`

	// Exposure mode: "local" (127.0.0.1 only) vs "lan"/"internet" (bind
	// 0.0.0.0 so other devices on the network — e.g. a phone — can reach the
	// panel). Applied via the bind address chosen on startup (see main.go), so
	// a change here only takes effect after the manager is restarted.
	Exposure string `json:"exposure"` // "local" | "lan" | "internet"

	// BattlEye RCon settings. Port defaults to ServerPort+1 per DayZ
	// convention. Password is cached here so the panel can re-connect after
	// a server restart without asking the user.
	RConPort     int    `json:"rconPort"`
	RConPassword string `json:"rconPassword"`

	// Scheduled daily restarts ("HH:MM" local time, 24h). Runs in addition
	// to AutoRestartSeconds — both can be enabled.
	ScheduledRestarts []string `json:"scheduledRestarts"`

	// Minutes before a scheduled/auto restart to broadcast via RCon.
	// Default [5, 3, 1]. Set empty to disable warnings.
	RestartWarnMinutes []int `json:"restartWarnMinutes"`

	// Scheduled RCon broadcasts — "say all" messages pushed on a daily
	// schedule. Useful for rules reminders, discord links, event pings.
	ScheduledAnnouncements []ScheduledAnnouncement `json:"scheduledAnnouncements"`

	// Interval RCon broadcasts — "say all" messages repeated every N minutes
	// while the server is running (e.g. every 30 min: "Join our Discord").
	IntervalAnnouncements []IntervalAnnouncement `json:"intervalAnnouncements"`

	// Workshop mod collection URLs users can re-import in one click.
	// Stored as raw URLs or bare IDs.
	WorkshopCollections []string `json:"workshopCollections"`
}

// ScheduledAnnouncement fires once a day at Time (HH:MM, local) when Enabled.
type ScheduledAnnouncement struct {
	Time    string `json:"time"`    // "HH:MM"
	Message string `json:"message"`
	Enabled bool   `json:"enabled"`
}

// IntervalAnnouncement repeats Message every IntervalMinutes while the server
// is running and Enabled (e.g. every 30 minutes).
type IntervalAnnouncement struct {
	IntervalMinutes int    `json:"intervalMinutes"`
	Message         string `json:"message"`
	Enabled         bool   `json:"enabled"`
}

func defaultManager() *Manager {
	return &Manager{
		FirstRunDone:       false,
		Language:           "en",
		ServerName:         "DayZ Local Server",
		ServerPort:         2302,
		ServerCfg:          "serverDZ.cfg",
		CPUCount:           4,
		DoLogs:             true,
		AdminLog:           true,
		NetLog:             true,
		FreezeCheck:        true,
		FilePatching:       false,
		BEPath:             "battleye",
		ProfilesDir:        "profiles",
		Mods:               []string{},
		ServerMods:         []string{},
		AutoRestartSeconds: 14390,
		AutoRestartEnabled: false,
		Exposure:           "local",

		RestartWarnMinutes: []int{5, 3, 1},
	}
}

func LoadManager(path string) (*Manager, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := defaultManager()
		if err := SaveManager(path, c); err != nil {
			return nil, err
		}
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manager config: %w", err)
	}
	c := defaultManager()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse manager config: %w", err)
	}
	return c, nil
}

func SaveManager(path string, c *Manager) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
