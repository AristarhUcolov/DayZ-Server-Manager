// Copyright (c) 2026 Aristarh Ucolov.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manager is the manager's own configuration (stored next to the exe under
// .dayz-manager/manager.json). Distinct from the DayZ server.cfg.
type Manager struct {
	FirstRunDone bool   `json:"firstRunDone"`
	Language     string `json:"language"` // "ru" | "en"

	// Path to the *client* DayZ install that owns the Steam !Workshop folder.
	// Used to pull mods into the server dir on demand.
	VanillaDayZPath string `json:"vanillaDayZPath"`

	// Where mods come from. "client" (default): the !Workshop folder of the
	// client install above. "folder": ModsFolderPath — any folder holding @Mod
	// folders, or SteamCMD's workshop/content/221100 (<id> folders), for hosts
	// with no game client. "server": no source at all — mods are uploaded into
	// the server directory by hand, and auto-update is unavailable.
	ModSource      string `json:"modSource,omitempty"`
	ModsFolderPath string `json:"modsFolderPath,omitempty"`

	// Launch params (mirror the reference .bat file).
	ServerName   string   `json:"serverName"`
	ServerPort   int      `json:"serverPort"`
	ServerCfg    string   `json:"serverCfg"`
	CPUCount     int      `json:"cpuCount"`
	DoLogs       bool     `json:"doLogs"`
	AdminLog     bool     `json:"adminLog"`
	NetLog       bool     `json:"netLog"`
	FreezeCheck  bool     `json:"freezeCheck"`
	FilePatching bool     `json:"filePatching"`
	BEPath       string   `json:"bePath"`      // absolute or relative to server dir
	ProfilesDir  string   `json:"profilesDir"` // absolute or relative
	Mods         []string `json:"mods"`        // mod folder names, e.g. "@CF"
	ServerMods   []string `json:"serverMods"`  // mods only loaded server-side

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

	// AutoInstallMissions: when a mod is installed, updated or synced from the
	// panel, copy any map mission it carries (a folder or a .zip inside the
	// mod) into mpmissions. Existing missions are never overwritten and
	// nothing is activated automatically.
	AutoInstallMissions bool `json:"autoInstallMissions"`

	// AutoModVehiclesOff turns off adding modded vehicles by themselves: by
	// default, when a mod is switched on (or installed/updated while on), the
	// vehicles it ships server files for are added to the mission, and
	// switching the mod off switches them off. Inverted so that existing
	// settings files (without the field) get the automatic behaviour.
	AutoModVehiclesOff bool `json:"autoModVehiclesOff,omitempty"`

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

	// RestartWarnMessage is the in-game countdown text; "{m}" is replaced by the
	// minutes left. Empty = the built-in English default, so servers can
	// announce in their players' language.
	RestartWarnMessage string `json:"restartWarnMessage,omitempty"`

	// Kick-before-restart: RestartKickSeconds before a scheduled/auto restart
	// the server is locked (RestartLock, BattlEye "#lock") and every player is
	// kicked with RestartKickMessage — and anyone who reconnects inside that
	// window is kicked again. Players' characters save on disconnect, so this
	// also avoids rollbacks. 0 = off.
	RestartKickSeconds int    `json:"restartKickSeconds"`
	RestartLock        bool   `json:"restartLock"`
	RestartKickMessage string `json:"restartKickMessage,omitempty"`

	// GracefulStopSeconds: on Stop/Restart, first ask the server to shut down
	// cleanly over RCon (#shutdown — it saves the world and players), and only
	// force-kill if it is still running after this many seconds. 0 = always
	// force-kill immediately (the old behaviour, same as the reference .bat).
	GracefulStopSeconds int `json:"gracefulStopSeconds"`

	// CPU affinity (logical core indexes, 0-based; empty = all cores) and
	// process priority ("" = leave as the OS starts it; else one of
	// util.PriorityLevels) applied to DayZServer right after every start.
	CPUAffinity     []int  `json:"cpuAffinity"`
	ProcessPriority string `json:"processPriority,omitempty"`

	// Scheduled RCon broadcasts — "say all" messages pushed on a daily
	// schedule. Useful for rules reminders, discord links, event pings.
	ScheduledAnnouncements []ScheduledAnnouncement `json:"scheduledAnnouncements"`

	// Interval RCon broadcasts — "say all" messages repeated every N minutes
	// while the server is running (e.g. every 30 min: "Join our Discord").
	IntervalAnnouncements []IntervalAnnouncement `json:"intervalAnnouncements"`

	// Config-profile swaps on a daily schedule (applied inside a restart).
	ScheduledProfiles []ScheduledProfile `json:"scheduledProfiles"`

	// Saved quick broadcast messages ("announcement presets") — one-click sends
	// from the RCon page. Persisted here so they survive a manager restart.
	BroadcastPresets []string `json:"broadcastPresets"`

	// World-size overrides for the interactive map, keyed by world (e.g. a modded
	// map whose edge length differs from the 15360 default). Metres.
	MapSizes map[string]int `json:"mapSizes,omitempty"`

	// Workshop mod collection URLs users can re-import in one click.
	// Stored as raw URLs or bare IDs.
	WorkshopCollections []string `json:"workshopCollections"`

	// Crash watchdog: when the DayZ process exits WITHOUT the manager asking
	// it to (crash), start it again after a short grace period. Crash-loop
	// protection (3 exits / 5 min) still pauses everything.
	RestartOnCrash bool `json:"restartOnCrash"`

	// Smart restarts (item F). RestartOnlyWhenEmpty defers a scheduled/interval
	// restart while players are online, re-checking until the server empties, so
	// nobody is kicked mid-session. RestartEmptyMaxWaitMinutes caps that wait (0 =
	// wait indefinitely) — past it, the restart proceeds with the normal RCon
	// countdown. RestartOnHighMem cycles the server when the DayZServer process
	// RSS passes RestartMemThresholdMB (memory creep on long uptimes); the same
	// empty-server rule then applies to that restart too.
	RestartOnlyWhenEmpty       bool `json:"restartOnlyWhenEmpty"`
	RestartEmptyMaxWaitMinutes int  `json:"restartEmptyMaxWaitMinutes"`
	RestartOnHighMem           bool `json:"restartOnHighMem"`
	RestartMemThresholdMB      int  `json:"restartMemThresholdMB"`

	// Automatic backups: write the same zip the manual "Download backup"
	// produces into .dayz-manager/backups/ every N hours (0 = off), keeping
	// the newest BackupKeep files.
	BackupIntervalHours int `json:"backupIntervalHours"`
	BackupKeep          int `json:"backupKeep"`

	// Discord notifications: POST server lifecycle events (started, crashed,
	// restarts, mod updates, backups) to a webhook URL.
	DiscordWebhookURL string `json:"discordWebhookURL"`
	DiscordEnabled    bool   `json:"discordEnabled"`
	// DiscordEvents switches single events on or off; an event missing here
	// uses its default (app.DiscordCatalog). DiscordPlayersWebhookURL is an
	// optional second channel for player activity (joins, kills, chat).
	// DiscordMention is pinged on alerts (crash, failed restart, crash loop):
	// "" nobody, "here", "everyone", or a role / user ID. DiscordUsername and
	// DiscordAvatarURL override the webhook's own name and picture.
	// DiscordLanguage is the messages' language ("" = the panel's).
	// DiscordStatus keeps one message in the channel up to date with the
	// server's state instead of posting new ones.
	DiscordEvents            map[string]bool `json:"discordEvents,omitempty"`
	DiscordPlayersWebhookURL string          `json:"discordPlayersWebhookURL,omitempty"`
	DiscordMention           string          `json:"discordMention,omitempty"`
	DiscordUsername          string          `json:"discordUsername,omitempty"`
	DiscordAvatarURL         string          `json:"discordAvatarURL,omitempty"`
	DiscordLanguage          string          `json:"discordLanguage,omitempty"`
	DiscordStatus            bool            `json:"discordStatus,omitempty"`

	// LimitFPS caps the server simulation rate (-limitFPS=N). 0 = DayZ's own
	// default (no parameter passed).
	LimitFPS int `json:"limitFPS"`
	// ExtraLaunchParams are additional DayZServer parameters appended verbatim
	// after the managed ones (e.g. "-scrAllowFileWrite"), for flags the panel
	// has no dedicated field for.
	ExtraLaunchParams string `json:"extraLaunchParams,omitempty"`

	// ChatKeepDays is how long the chat archive keeps day files (0 = forever).
	ChatKeepDays int `json:"chatKeepDays"`

	// PanelHTTPS serves the panel over HTTPS as well (same port; plain HTTP
	// from other devices is redirected). Uses .dayz-manager/tls/cert.pem +
	// key.pem when present, otherwise a generated self-signed certificate.
	// Takes effect after the manager restarts, like Exposure.
	PanelHTTPS bool `json:"panelHTTPS"`

	// UPnPPanel / UPnPGame ask the router (UPnP IGD) to forward the panel port
	// (TCP) and the game ports (UDP game port..+3 and the Steam query port).
	UPnPPanel bool `json:"upnpPanel"`
	UPnPGame  bool `json:"upnpGame"`
}

// ScheduledAnnouncement fires once a day at Time (HH:MM, local) when Enabled.
type ScheduledAnnouncement struct {
	Time    string `json:"time"` // "HH:MM"
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

// ScheduledProfile applies a saved config profile at a daily HH:MM time. The
// swap runs inside a restart (announced with the usual countdown), so it only
// fires while the server is running — e.g. a "Weekend" profile Friday evening.
type ScheduledProfile struct {
	Time    string `json:"time"`    // "HH:MM"
	Profile string `json:"profile"` // profile slug (see /api/config-profiles)
	Enabled bool   `json:"enabled"`
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

		RestartWarnMinutes:  []int{5, 3, 1},
		RestartLock:         true,
		AutoInstallMissions: true,
		GracefulStopSeconds: 30,

		RestartEmptyMaxWaitMinutes: 60,
		RestartMemThresholdMB:      8000,

		ChatKeepDays: 90,
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
	// Notepad and PowerShell 5.1 save "UTF-8 with BOM"; encoding/json rejects
	// the BOM, which would treat a valid hand-edited file as corrupt and reset
	// every setting to defaults.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	c := defaultManager()
	if err := json.Unmarshal(data, c); err != nil {
		// A broken manager.json (hand edit, disk trouble) must not keep the
		// manager from starting at all: set the file aside for recovery and
		// start with defaults. The caller logs the RecoveredError.
		backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
		if rerr := os.Rename(path, backup); rerr != nil {
			return nil, fmt.Errorf("parse manager config: %w", err)
		}
		c = defaultManager()
		if serr := SaveManager(path, c); serr != nil {
			return nil, serr
		}
		return c, &RecoveredError{Backup: backup, Cause: err}
	}
	return c, nil
}

// RecoveredError reports that manager.json could not be parsed and the
// manager started with default settings; the broken file is at Backup.
type RecoveredError struct {
	Backup string
	Cause  error
}

func (e *RecoveredError) Error() string {
	return fmt.Sprintf("manager.json was unreadable (%v) — started with default settings; the broken file was kept as %s", e.Cause, e.Backup)
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

// Clone returns a deep copy: no slice or map is shared with m. The app hands
// out clones as read snapshots and mutates a clone before committing, so a
// handler editing (say) MapSizes can never write a map another goroutine is
// reading — Go aborts the whole process on that ("concurrent map read and
// map write"), it is not a recoverable panic. nil stays nil and empty stays
// empty, so the JSON the panel sees does not change.
func (m Manager) Clone() Manager {
	c := m
	c.Mods = cloneSlice(m.Mods)
	c.ServerMods = cloneSlice(m.ServerMods)
	c.ScheduledRestarts = cloneSlice(m.ScheduledRestarts)
	c.RestartWarnMinutes = cloneSlice(m.RestartWarnMinutes)
	c.CPUAffinity = cloneSlice(m.CPUAffinity)
	c.ScheduledAnnouncements = cloneSlice(m.ScheduledAnnouncements)
	c.IntervalAnnouncements = cloneSlice(m.IntervalAnnouncements)
	c.ScheduledProfiles = cloneSlice(m.ScheduledProfiles)
	c.BroadcastPresets = cloneSlice(m.BroadcastPresets)
	c.WorkshopCollections = cloneSlice(m.WorkshopCollections)
	if m.MapSizes != nil {
		c.MapSizes = make(map[string]int, len(m.MapSizes))
		for k, v := range m.MapSizes {
			c.MapSizes[k] = v
		}
	}
	if m.DiscordEvents != nil {
		c.DiscordEvents = make(map[string]bool, len(m.DiscordEvents))
		for k, v := range m.DiscordEvents {
			c.DiscordEvents[k] = v
		}
	}
	return c
}

func cloneSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append(make([]T, 0, len(s)), s...)
}

// Mod source modes (see Manager.ModSource).
const (
	ModSourceClient = "client" // the DayZ client install's !Workshop folder
	ModSourceFolder = "folder" // any folder holding @Mods or SteamCMD <id> folders
	ModSourceServer = "server" // no source: mods are placed in the server dir by hand
)

// EffectiveModSource returns the configured mod source, defaulting to the
// client !Workshop (the only mode that existed before v1.0.0).
func (m Manager) EffectiveModSource() string {
	switch m.ModSource {
	case ModSourceFolder, ModSourceServer:
		return m.ModSource
	}
	return ModSourceClient
}

// ModSourceDir is the folder mods are installed/updated FROM: the client's
// !Workshop, a custom mods folder, or "" when there is no source (server-only
// hosting — mods are uploaded by hand and auto-update is unavailable).
func (m Manager) ModSourceDir() string {
	switch m.EffectiveModSource() {
	case ModSourceFolder:
		return m.ModsFolderPath
	case ModSourceServer:
		return ""
	}
	if m.VanillaDayZPath == "" {
		return ""
	}
	return filepath.Join(m.VanillaDayZPath, "!Workshop")
}
