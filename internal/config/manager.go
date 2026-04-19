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

	// Exposure mode: "local" (127.0.0.1 only) vs "internet" (bind 0.0.0.0).
	// Informational here — actually applied via CLI flag on startup — but we
	// keep the user preference here so the UI can reflect it.
	Exposure string `json:"exposure"` // "local" | "internet"
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
