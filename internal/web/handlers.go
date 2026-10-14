// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/guide"
	"dayzmanager/internal/i18n"
	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/mods"
	"dayzmanager/internal/notify"
	"dayzmanager/internal/players"
	"dayzmanager/internal/rcon"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/updater"
	"dayzmanager/internal/util"
	"dayzmanager/internal/validator"
)

// handlers serves the API for one DayZ server of the panel. Every server has
// its own (see Server.attach); the panel-wide parts — login, remote access —
// are shared pointers, and their routes always reach the primary's handlers.
type handlers struct {
	app  *app.App
	auth *authManager // optional panel login (auth.go), shared by all servers
	chat *chatArchive // persistent chat history (chatlog.go)

	// Remote access (remote.go) — panel-wide.
	server    *Server
	panelPort int
	upnp      *upnpState
	upnpKick  chan struct{}

	// Per-server state.
	playersOnce sync.Once
	players     *players.Store // player database + killfeed (handlers_v14.go)
	metrics     struct {       // 24 h of 30 s CPU/RAM/players samples
		mu      sync.Mutex
		samples []histSample
	}
	bansMu   sync.Mutex // bans.json and the game's ban files (bans.go)
	dashMods struct {   // dashboard mod counts, refreshed at most every 15 s
		mu         sync.Mutex
		at         time.Time
		val        map[string]interface{}
		refreshing bool
	}
}

func (h *handlers) register(mux *http.ServeMux) {
	// Meta.
	mux.HandleFunc("/api/info", methods(h.info, http.MethodGet))
	mux.HandleFunc("/api/i18n", methods(h.i18nBundle, http.MethodGet))

	// Manager config (language, vanilla path, launch params).
	mux.HandleFunc("/api/config", methods(h.config, http.MethodGet, http.MethodPost, http.MethodPut))
	mux.HandleFunc("/api/config/finish-first-run", methods(h.finishFirstRun, http.MethodPost))

	// Multi-server: the servers of the panel (servers.go).
	h.registerServers(mux)
	// Bans, kicks and the live player list (bans.go).
	h.registerBans(mux)
	// DayZ's whitelist.txt (whitelist.go).
	h.registerWhitelist(mux)

	// Server control.
	mux.HandleFunc("/api/server/status", methods(h.serverStatus, http.MethodGet))
	mux.HandleFunc("/api/server/start", methods(h.serverStart, http.MethodPost))
	mux.HandleFunc("/api/server/stop", methods(h.serverStop, http.MethodPost))
	mux.HandleFunc("/api/server/restart", methods(h.serverRestart, http.MethodPost))
	mux.HandleFunc("/api/server/clear-crash-loop", methods(h.serverClearCrashLoop, http.MethodPost))

	// server.cfg.
	mux.HandleFunc("/api/servercfg", methods(h.serverCfg, http.MethodGet, http.MethodPost, http.MethodPut))
	mux.HandleFunc("/api/servercfg/mission", methods(h.serverCfgMission, http.MethodPost))

	// Missions.
	mux.HandleFunc("/api/missions", methods(h.missionsList, http.MethodGet))
	mux.HandleFunc("/api/missions/duplicate", methods(h.missionsDuplicate, http.MethodPost))

	// Mods.
	mux.HandleFunc("/api/mods", methods(h.modsList, http.MethodGet))
	mux.HandleFunc("/api/mods/install", methods(h.modsInstall, http.MethodPost))
	mux.HandleFunc("/api/mods/uninstall", methods(h.modsUninstall, http.MethodPost))
	mux.HandleFunc("/api/mods/update", methods(h.modsUpdate, http.MethodPost))
	mux.HandleFunc("/api/mods/update-all", methods(h.modsUpdateAll, http.MethodPost))
	mux.HandleFunc("/api/mods/sync-all", methods(h.modsSyncAll, http.MethodPost))
	mux.HandleFunc("/api/mods/sync-keys", methods(h.modsSyncKeys, http.MethodPost))
	// Workshop "is the local copy stale vs Steam?" probe.
	mux.HandleFunc("/api/mods/check-updates", methods(h.modsCheckUpdates, http.MethodPost))
	// Steam Workshop collection URL importer: fetches the public collection
	// HTML and matches child IDs to @Mod folders via meta.cpp.
	mux.HandleFunc("/api/mods/collection/resolve", methods(h.modsCollectionResolve, http.MethodPost))
	mux.HandleFunc("/api/mods/enable", methods(h.modsEnable, http.MethodPost))
	mux.HandleFunc("/api/mods/order", methods(h.modsOrder, http.MethodPost))
	// Scan a mod folder for bundled types.xml so the user can merge them
	// into the active mission with one click.
	mux.HandleFunc("/api/mods/scan-types", methods(h.modsScanTypes, http.MethodGet))
	mux.HandleFunc("/api/mods/install-types", methods(h.modsInstallTypes, http.MethodPost))

	// Types.
	mux.HandleFunc("/api/types", methods(h.typesList, http.MethodGet))
	mux.HandleFunc("/api/types/item", methods(h.typesItem, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete))
	mux.HandleFunc("/api/types/presets", methods(h.typesPresets, http.MethodGet))
	mux.HandleFunc("/api/types/apply-preset", methods(h.typesApplyPreset, http.MethodPost))
	// Bulk-edit endpoint: apply a scalar field patch to many types at once.
	mux.HandleFunc("/api/types/bulk-patch", methods(h.typesBulkPatch, http.MethodPost))
	// Inline table edits: a different patch per row, one write.
	mux.HandleFunc("/api/types/patch-many", methods(h.typesPatchMany, http.MethodPost))

	// Loot economy dashboard + CE tuning presets.
	mux.HandleFunc("/api/economy/stats", methods(h.economyStats, http.MethodGet))
	mux.HandleFunc("/api/economy/tune", methods(h.economyTune, http.MethodPost))

	// BattlEye configuration files (bans, whitelist, script filters, ...).
	mux.HandleFunc("/api/battleye/list", methods(h.battleyeList, http.MethodGet))
	mux.HandleFunc("/api/battleye/read", methods(h.battleyeRead, http.MethodGet))
	mux.HandleFunc("/api/battleye/write", methods(h.battleyeWrite, http.MethodPost))
	mux.HandleFunc("/api/battleye/bans", methods(h.battleyeBans, http.MethodGet, http.MethodPost))

	// Mission central-economy files (globals.xml, economy.xml, types.xml,
	// events.xml, cfgspawnabletypes.xml, ...). Read-only listing +
	// raw-text edit, without the strongly-typed editor's constraints.
	mux.HandleFunc("/api/mission/db/list", methods(h.missionDBList, http.MethodGet))
	mux.HandleFunc("/api/mission/db/read", methods(h.missionDBRead, http.MethodGet))
	mux.HandleFunc("/api/mission/db/write", methods(h.missionDBWrite, http.MethodPost))

	// Moded types files.
	mux.HandleFunc("/api/moded", methods(h.modedList, http.MethodGet))
	mux.HandleFunc("/api/moded/create", methods(h.modedCreate, http.MethodPost))
	mux.HandleFunc("/api/moded/delete", methods(h.modedDelete, http.MethodPost))
	mux.HandleFunc("/api/moded/register", methods(h.modedRegister, http.MethodPost))

	// Raw file browser/editor.
	mux.HandleFunc("/api/files/tree", methods(h.filesTree, http.MethodGet))
	mux.HandleFunc("/api/files/read", methods(h.filesRead, http.MethodGet))
	mux.HandleFunc("/api/files/write", methods(h.filesWrite, http.MethodPost))

	// Validator.
	mux.HandleFunc("/api/validate", methods(h.validate, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/validate/fix", methods(h.validateFix, http.MethodPost))

	// Self-update check (GitHub Releases).
	mux.HandleFunc("/api/update/check", methods(h.updateCheck, http.MethodGet))

	// Steam install detection (libraryfolders.vdf scan).
	mux.HandleFunc("/api/steam/detect", methods(h.steamDetect, http.MethodGet))

	// Import an existing configured DayZ server directory (sync).
	mux.HandleFunc("/api/import/preview", methods(h.importPreview, http.MethodPost))
	mux.HandleFunc("/api/import/apply", methods(h.importApply, http.MethodPost))

	// Backups (timestamped .bak files created before every write).
	mux.HandleFunc("/api/backups/list", methods(h.backupsList, http.MethodGet))
	mux.HandleFunc("/api/backups/all", methods(h.backupsAll, http.MethodGet))
	mux.HandleFunc("/api/backups/restore", methods(h.backupsRestore, http.MethodPost))

	// Config profiles — named snapshots of the whole configuration set.
	mux.HandleFunc("/api/config-profiles", methods(h.cfgProfilesList, http.MethodGet))
	mux.HandleFunc("/api/config-profiles/save", methods(h.cfgProfileSave, http.MethodPost))
	mux.HandleFunc("/api/config-profiles/apply", methods(h.cfgProfileApply, http.MethodPost))
	mux.HandleFunc("/api/config-profiles/diff", methods(h.cfgProfileDiff, http.MethodGet))
	mux.HandleFunc("/api/config-profiles/delete", methods(h.cfgProfileDelete, http.MethodPost))

	// Global config search — grep across the config/economy files.
	mux.HandleFunc("/api/config/search", methods(h.configSearch, http.MethodGet))

	// Mod profile configs (profiles/*ExpansionMod*/Settings/*.json etc.).
	mux.HandleFunc("/api/profiles/tree", methods(h.profilesTree, http.MethodGet))
	mux.HandleFunc("/api/profiles/read", methods(h.profilesRead, http.MethodGet))
	mux.HandleFunc("/api/profiles/write", methods(h.profilesWrite, http.MethodPost))

	// Events (events.xml zombie/vehicle/helicrash spawn tables).
	mux.HandleFunc("/api/events", methods(h.eventsList, http.MethodGet))
	mux.HandleFunc("/api/events/item", methods(h.eventsItem, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete))

	// Event spawn-point editor (cfgeventspawns.xml on the map).
	mux.HandleFunc("/api/eventspawns", methods(h.eventSpawnsList, http.MethodGet))
	mux.HandleFunc("/api/eventspawns/save", methods(h.eventSpawnsSave, http.MethodPost))
	// Player spawn-point editor (cfgplayerspawnpoints.xml on the map).
	mux.HandleFunc("/api/spawnpoints", methods(h.spawnPoints, http.MethodGet, http.MethodPost))

	// globals.xml variable form.
	mux.HandleFunc("/api/globals", methods(h.globalsGet, http.MethodGet))
	mux.HandleFunc("/api/globals/save", methods(h.globalsSave, http.MethodPost))

	// "Where does this item spawn?" reverse loot lookup.
	mux.HandleFunc("/api/loot/where", methods(h.lootWhere, http.MethodGet))

	// cfgrandompresets.xml editor (real per-item spawn chance; add/edit/delete).
	mux.HandleFunc("/api/randompresets", methods(h.randomPresetsList, http.MethodGet))
	mux.HandleFunc("/api/randompresets/save", methods(h.randomPresetsSave, http.MethodPost))

	// Mod types drift — which installed mods ship loot missing from the economy.
	mux.HandleFunc("/api/mods/drift", methods(h.modsDrift, http.MethodGet))

	// Logs.
	mux.HandleFunc("/api/logs/list", methods(h.logsList, http.MethodGet))
	mux.HandleFunc("/api/logs/read", methods(h.logsRead, http.MethodGet))
	mux.HandleFunc("/api/logs/stream", methods(h.logsStream, http.MethodGet))
	mux.HandleFunc("/api/logs/download", methods(h.logsDownload, http.MethodGet))

	// ADM log viewer.
	mux.HandleFunc("/api/admlog/recent", methods(h.admlogRecent, http.MethodGet))

	// Config zip backup (export / import).
	mux.HandleFunc("/api/backup/export", methods(h.backupExport, http.MethodGet))
	mux.HandleFunc("/api/backup/import", methods(h.backupImport, http.MethodPost))
	mux.HandleFunc("/api/backup/run", methods(h.backupRun, http.MethodPost))
	h.registerDiscord(mux)
	mux.HandleFunc("/api/players", methods(h.playersList, http.MethodGet))
	mux.HandleFunc("/api/players/note", methods(h.playerNote, http.MethodPost))
	mux.HandleFunc("/api/players/profile", methods(h.playerProfile, http.MethodGet))
	// Bans: /api/bans/add and /api/bans/remove (bans.go).
	mux.HandleFunc("/api/watchlist/activity", methods(h.watchlistActivity, http.MethodGet))
	mux.HandleFunc("/api/map/heat", methods(h.mapHeat, http.MethodGet))
	mux.HandleFunc("/api/map/live", methods(h.mapLive, http.MethodGet))
	// Admin-supplied map background image (serve / upload / remove).
	mux.HandleFunc("/api/map/image", methods(h.mapImage, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/map/image/fetch", methods(h.mapImageFetch, http.MethodPost))
	mux.HandleFunc("/api/map/image/delete", methods(h.mapImageDelete, http.MethodPost))
	mux.HandleFunc("/api/map/size", methods(h.mapSize, http.MethodPost))
	mux.HandleFunc("/api/gameplay", methods(h.gameplay, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/metrics/history", methods(h.metricsHistory, http.MethodGet))
	mux.HandleFunc("/api/spawnable", methods(h.spawnableList, http.MethodGet))
	mux.HandleFunc("/api/spawnable/item", methods(h.spawnableItem, http.MethodGet, http.MethodPost, http.MethodDelete))
	mux.HandleFunc("/api/spawnable/presets", methods(h.spawnablePresets, http.MethodGet))
	mux.HandleFunc("/api/spawnable/classnames", methods(h.spawnableClassNames, http.MethodGet))
	mux.HandleFunc("/api/guide", methods(h.guideGet, http.MethodGet))
	// v0.18.0: explain a failed start, diff a backup before restoring it, and
	// undo a wipe (wipeApply already moves the folders aside for exactly this).
	mux.HandleFunc("/api/diagnose", methods(h.diagnose, http.MethodGet))
	mux.HandleFunc("/api/health", methods(h.health, http.MethodGet))
	mux.HandleFunc("/api/sponsors", methods(h.sponsorsList, http.MethodGet))
	mux.HandleFunc("/api/changelog", methods(h.changelogList, http.MethodGet))
	mux.HandleFunc("/api/cleanup/scan", methods(h.cleanupScan, http.MethodGet))
	mux.HandleFunc("/api/cleanup", methods(h.cleanupRun, http.MethodPost))
	mux.HandleFunc("/api/backups/diff", methods(h.backupsDiff, http.MethodGet))
	mux.HandleFunc("/api/wipe/list", methods(h.wipeList, http.MethodGet))
	mux.HandleFunc("/api/wipe/restore", methods(h.wipeRestore, http.MethodPost))
	// v0.20.0: player fresh-spawn loadout (DayZ spawn-gear presets).
	mux.HandleFunc("/api/spawngear", methods(h.spawnGearList, http.MethodGet))
	mux.HandleFunc("/api/spawngear/preset", methods(h.spawnGearGet, http.MethodGet))
	mux.HandleFunc("/api/spawngear/save", methods(h.spawnGearSave, http.MethodPost))
	mux.HandleFunc("/api/spawngear/delete", methods(h.spawnGearDelete, http.MethodDelete, http.MethodPost))
	mux.HandleFunc("/api/spawngear/enable", methods(h.spawnGearEnable, http.MethodPost))

	// Dashboard live metrics.
	mux.HandleFunc("/api/dashboard/metrics", methods(h.dashboardMetrics, http.MethodGet))

	// Weather + in-game time (item 16).
	mux.HandleFunc("/api/weather", methods(h.weatherGet, http.MethodGet))
	mux.HandleFunc("/api/weather/preset", methods(h.weatherPreset, http.MethodPost))
	mux.HandleFunc("/api/weather/custom", methods(h.weatherCustom, http.MethodPost))
	mux.HandleFunc("/api/weather/time", methods(h.weatherTime, http.MethodPost))

	// Server wipe — persistence reset (item 17).
	mux.HandleFunc("/api/wipe/preview", methods(h.wipePreview, http.MethodGet))
	mux.HandleFunc("/api/wipe", methods(h.wipeApply, http.MethodPost))

	// LAN network addresses for phone / other-device access (item 9).
	mux.HandleFunc("/api/network/addresses", methods(h.networkAddresses, http.MethodGet))

	// RCon.
	mux.HandleFunc("/api/rcon/players", methods(h.rconPlayers, http.MethodGet))
	mux.HandleFunc("/api/rcon/status", methods(h.rconStatus, http.MethodGet))
	mux.HandleFunc("/api/rcon/chat/recent", methods(h.rconChatRecent, http.MethodGet))
	mux.HandleFunc("/api/rcon/say", methods(h.rconSay, http.MethodPost))
	mux.HandleFunc("/api/rcon/kick", methods(h.rconKick, http.MethodPost))
	mux.HandleFunc("/api/rcon/ban", methods(h.rconBan, http.MethodPost))
	mux.HandleFunc("/api/rcon/command", methods(h.rconCommand, http.MethodPost))

	// v1.0.0 "October Update".
	h.registerV100(mux)
	h.registerAuth(mux)
	h.registerChat(mux)
	h.registerRemote(mux)
}

// methods rejects any verb not in allowed with 405 before calling handler.
func methods(h http.HandlerFunc, allowed ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, m := range allowed {
			if r.Method == m {
				h(w, r)
				return
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------

// info is public (the login page and the launcher's "already running?" probe
// need it), so the server path is only shown to signed-in or local callers.
func (h *handlers) info(w http.ResponseWriter, r *http.Request) {
	authCfg := h.auth.snapshot()
	sess := h.auth.sessionFor(r)
	authed := !authCfg.Enabled || sess != nil
	out := map[string]interface{}{
		"name":    h.app.Name,
		"version": h.app.Version,
		"author":  h.app.Author,
		"mode":    h.app.Mode,
		"os":      runtime.GOOS,
		"auth":    map[string]interface{}{"enabled": authCfg.Enabled, "authenticated": authed},
		"local":   isLoopback(r),
	}
	if authed || isLoopback(r) {
		out["serverDir"] = h.app.ServerDir
		out["serverId"] = serverKey(h.app.ID)
		out["serverName"] = h.app.Label()
		if f := h.fleet(); f != nil {
			out["multi"] = f.Multi()
			if isLoopback(r) {
				// For another manager starting on this PC: which server
				// folders this one already runs (app.OwnerOf).
				var dirs []string
				for _, m := range f.Members() {
					dirs = append(dirs, m.Dir)
				}
				out["managedDirs"] = dirs
			}
		}
	}
	if sess != nil {
		out["auth"].(map[string]interface{})["user"] = sess.User
	}
	writeJSON(w, out)
}

func (h *handlers) i18nBundle(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("lang")
	if code == "" {
		code = h.app.Cfg().Language
	}
	writeJSON(w, map[string]interface{}{
		"locale":    code,
		"dir":       i18n.Dir(code),
		"supported": i18n.Supported(),
		"languages": i18n.Languages(),
		"messages":  i18n.Get(code),
		// Hover-help rides along with the bundle so tooltips are ready before
		// the first hover. It lives in internal/guide, not i18n: it is
		// documentation, and a new language must not have to write a manual.
		"help": guide.Help(code),
	})
}

func (h *handlers) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, h.app.Cfg())
		return
	}
	before := h.app.Cfg()
	// Merge into the current config rather than replacing it wholesale: decode
	// over a copy so any field the client omits keeps its existing value. This
	// makes partial / auto-saves safe — a stale or partial body can no longer
	// wipe unrelated fields (which is what used to reset scheduled announcements).
	// MutateConfig does the read-modify-write under configMu so the replace can't
	// tear against a concurrent save/reload.
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		if err := json.NewDecoder(r.Body).Decode(c); err != nil {
			return err
		}
		// Opening the panel to the internet without a login hands the server
		// to anyone who finds the address. Checked only on a change, so an old
		// config that already says "internet" can still save other fields.
		if c.Exposure == "internet" && before.Exposure != "internet" && !h.auth.enabled() {
			return errors.New("set a panel login (Settings → Panel login) before opening the panel to the internet")
		}
		// Checked only on a change, like the exposure: an old password must
		// not block saving everything else.
		if c.RConPassword != before.RConPassword {
			c.RConPassword = strings.TrimSpace(c.RConPassword)
			if !validRConPassword(c.RConPassword) {
				return errors.New("cfg.err.rconPassword")
			}
		}
		return normalizeConfig(c)
	}); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, app.ErrConfigSave) {
			code = http.StatusInternalServerError
		}
		if msg := err.Error(); strings.HasPrefix(msg, "cfg.err.") {
			authError(w, code, msg, nil) // an i18n key the panel translates
			return
		}
		http.Error(w, err.Error(), code)
		return
	}
	// Refresh the scheduler's snapshot so changed restart times / announcements
	// take effect immediately and the supervisor loops never read torn config.
	h.app.ScheduleSnapshotSync()
	// Keep RCon real: push the password into BattlEye's beserver_x64.cfg (so
	// RCon is actually enabled) and refresh the in-memory connection params.
	h.app.SyncBEConfig()
	h.app.ApplyRConConfig()
	// Affinity / priority take effect on the running server right away.
	after := h.app.Cfg()
	if after.DiscordEnabled && after.DiscordStatus && (!before.DiscordStatus || !before.DiscordEnabled ||
		before.DiscordWebhookURL != after.DiscordWebhookURL || before.DiscordLanguage != after.DiscordLanguage ||
		before.DiscordUsername != after.DiscordUsername || before.DiscordAvatarURL != after.DiscordAvatarURL) {
		h.app.UpdateDiscordStatus()
	}
	if after.ProcessPriority != before.ProcessPriority || !equalInts(after.CPUAffinity, before.CPUAffinity) {
		if len(after.CPUAffinity) == 0 && len(before.CPUAffinity) > 0 {
			// Cleared: give the server every core back.
			if pid := h.app.Server.PID(); pid > 0 {
				_ = util.SetAffinity(pid, nil)
			}
		}
		_ = h.app.Server.ApplyTuning()
	}
	writeJSON(w, after)
}

// normalizeConfig validates and tidies fields a POST may set, so a bad value
// is rejected with a clear message instead of silently breaking a feature.
func normalizeConfig(c *config.Manager) error {
	switch c.ModSource {
	case "", config.ModSourceClient, config.ModSourceFolder, config.ModSourceServer:
	default:
		return fmt.Errorf("unknown mod source %q", c.ModSource)
	}
	if c.ProcessPriority != "" {
		ok := false
		for _, p := range util.PriorityLevels {
			if p == c.ProcessPriority {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("unknown process priority %q", c.ProcessPriority)
		}
	}
	if c.RestartKickSeconds < 0 || c.RestartKickSeconds > 1800 {
		return fmt.Errorf("kick-before-restart must be 0-1800 seconds")
	}
	if c.GracefulStopSeconds < 0 || c.GracefulStopSeconds > 600 {
		return fmt.Errorf("graceful stop timeout must be 0-600 seconds")
	}
	switch c.Exposure {
	case "", "local", "lan", "internet":
	default:
		return fmt.Errorf("unknown exposure %q", c.Exposure)
	}
	if c.RConPort < 0 || c.RConPort > 65535 {
		return errors.New("cfg.err.rconPort")
	}
	if c.LimitFPS < 0 || c.LimitFPS > 1000 {
		return fmt.Errorf("server FPS limit must be 0 (off) or 1-1000")
	}
	if c.ChatKeepDays < 0 || c.ChatKeepDays > 36500 {
		return fmt.Errorf("chat retention must be 0 (forever) or a number of days")
	}
	c.DiscordWebhookURL = strings.TrimSpace(c.DiscordWebhookURL)
	c.DiscordPlayersWebhookURL = strings.TrimSpace(c.DiscordPlayersWebhookURL)
	for _, u := range []string{c.DiscordWebhookURL, c.DiscordPlayersWebhookURL} {
		if u != "" {
			if err := notify.ValidWebhook(u); err != nil {
				return err
			}
		}
	}
	c.DiscordMention = strings.TrimSpace(c.DiscordMention)
	if !app.ValidDiscordMention(c.DiscordMention) {
		return fmt.Errorf("Discord mention: use here, everyone, or a role ID (digits)")
	}
	if c.DiscordLanguage != "" && !i18n.Has(c.DiscordLanguage) {
		return fmt.Errorf("unknown Discord language %q", c.DiscordLanguage)
	}
	c.DiscordUsername = strings.TrimSpace(c.DiscordUsername)
	if len([]rune(c.DiscordUsername)) > 80 {
		return fmt.Errorf("the Discord name is too long (80 characters at most)")
	}
	c.DiscordAvatarURL = strings.TrimSpace(c.DiscordAvatarURL)
	if c.DiscordAvatarURL != "" && !strings.HasPrefix(c.DiscordAvatarURL, "https://") {
		return fmt.Errorf("the Discord avatar must be an https:// image address")
	}
	c.ExtraLaunchParams = strings.TrimSpace(c.ExtraLaunchParams)
	if len(c.ExtraLaunchParams) > 2000 {
		return fmt.Errorf("additional launch parameters are too long")
	}
	seen := map[int]bool{}
	cores := make([]int, 0, len(c.CPUAffinity))
	for _, n := range c.CPUAffinity {
		if n >= 0 && n < runtime.NumCPU() && !seen[n] {
			seen[n] = true
			cores = append(cores, n)
		}
	}
	sort.Ints(cores)
	if len(cores) == runtime.NumCPU() {
		cores = nil // every core selected = no pinning
	}
	c.CPUAffinity = cores
	return nil
}

// validRConPassword: BattlEye reads beserver_x64.cfg as `Key Value` plain
// text — a space splits the value and non-Latin letters never match what the
// RCon client sends, so only printable ASCII without spaces logs in.
func validRConPassword(p string) bool {
	if len(p) > 128 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x21 || p[i] > 0x7e {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// finishFirstRun records the language, mod source, exposure mode and the
// optional panel login chosen in the first-run wizard.
func (h *handlers) finishFirstRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Language        string `json:"language"`
		VanillaDayZPath string `json:"vanillaDayZPath"`
		ModSource       string `json:"modSource"`
		ModsFolderPath  string `json:"modsFolderPath"`
		Exposure        string `json:"exposure"`
		Login           *struct {
			User     string `json:"user"`
			Password string `json:"password"`
		} `json:"login"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Exposure {
	case "", "local", "lan", "internet":
	default:
		http.Error(w, "unknown exposure "+req.Exposure, http.StatusBadRequest)
		return
	}
	if req.Login != nil {
		if err := validateCredentials(req.Login.User, req.Login.Password); err != nil {
			authError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
	}
	if req.Exposure == "internet" && req.Login == nil && !h.auth.enabled() {
		authError(w, http.StatusBadRequest, "auth.err.internetNeedsLogin", nil)
		return
	}
	// Validate the path outside the lock (os.Stat), then apply every field
	// atomically — so a validation failure leaves the in-memory config untouched
	// (it used to leave Language/VanillaDayZPath half-applied on error).
	if req.ModSource == config.ModSourceFolder {
		if st, err := os.Stat(req.ModsFolderPath); err != nil || !st.IsDir() {
			http.Error(w, "mods folder does not exist: "+req.ModsFolderPath, http.StatusBadRequest)
			return
		}
	}
	if req.VanillaDayZPath != "" && (req.ModSource == "" || req.ModSource == config.ModSourceClient) {
		st, err := os.Stat(req.VanillaDayZPath)
		if err != nil || !st.IsDir() {
			http.Error(w, "vanilla DayZ path does not exist: "+req.VanillaDayZPath, http.StatusBadRequest)
			return
		}
		ws := filepath.Join(req.VanillaDayZPath, "!Workshop")
		if st, err := os.Stat(ws); err != nil || !st.IsDir() {
			http.Error(w, "!Workshop folder not found inside "+req.VanillaDayZPath+" — is this the DayZ client install?", http.StatusBadRequest)
			return
		}
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		if req.Language != "" {
			c.Language = req.Language
		}
		c.VanillaDayZPath = req.VanillaDayZPath
		switch req.ModSource {
		case config.ModSourceClient, config.ModSourceFolder, config.ModSourceServer:
			c.ModSource = req.ModSource
		}
		c.ModsFolderPath = req.ModsFolderPath
		if req.Exposure != "" {
			c.Exposure = req.Exposure
		}
		c.FirstRunDone = true
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.Login != nil && !h.auth.enabled() {
		if err := h.enableLogin(w, r, req.Login.User, req.Login.Password, true); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, h.app.Cfg())
}

// ---------------------------------------------------------------------------
// Server control.

// serverState is the shared server-status payload (status poll + dashboard).
// nextRestart comes from the supervisor itself — the exact moment it will
// restart, or the one a running countdown is heading to — and serverTime lets
// the browser correct for its own clock/timezone, so the panel countdown can
// never disagree with what players were told in-game.
func (h *handlers) serverState() map[string]interface{} {
	out := map[string]interface{}{
		"running":   h.app.ServerIsRunning(),
		"stopping":  h.app.Server.IsStopping(),
		"pid":       h.app.Server.PID(),
		"uptime":    h.app.Server.Uptime().Round(time.Second).String(),
		"uptimeSec": int64(h.app.Server.Uptime().Seconds()),
		"port":      h.app.Cfg().ServerPort,
		"adopted":   h.app.Server.Adopted(),
		// Crash-loop protection state: the loop pauses auto-restart after
		// repeated fast exits; without surfacing it the server just looks
		// mysteriously "down despite auto-restart". UI shows a resume banner.
		"loopPaused": h.app.Server.LoopPaused(),
		"serverTime": time.Now().UnixMilli(),
	}
	// Steam server-browser query: listed / still loading, and a player count
	// that works without RCon.
	if h.app.ServerIsRunning() {
		q := h.app.Query()
		out["query"] = q
		out["loading"] = !q.OK && q.FirstOK.IsZero() && h.app.Server.Uptime() < 15*time.Minute
	}
	if plan := h.app.Server.NextRestart(); plan != nil {
		out["nextRestart"] = map[string]interface{}{
			"at":           plan.At.UnixMilli(),
			"reason":       plan.Reason,
			"active":       plan.Active,
			"waitingEmpty": plan.WaitingEmpty,
			"kicking":      plan.Kicking,
		}
	}
	return out
}

func (h *handlers) serverStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.serverState())
}

func (h *handlers) serverStart(w http.ResponseWriter, r *http.Request) {
	// Take WriteMu while starting so we can't launch DayZServer mid-write.
	// Holding it only across the exec.Start call keeps the window tiny.
	h.app.WriteMu.Lock()
	err := h.app.Server.Start()
	h.app.WriteMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) serverStop(w http.ResponseWriter, r *http.Request) {
	if err := h.app.Server.Stop(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Wait (bounded) for the process to actually exit so the next status poll
	// doesn't flash "running" after a successful stop — which invited
	// double-clicks and a confusing "server already running" on quick Start.
	for i := 0; i < 30 && h.app.Server.IsRunning(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	writeJSON(w, map[string]interface{}{"status": "stopped", "running": h.app.Server.IsRunning()})
}

// serverClearCrashLoop resumes auto-restart after crash-loop protection
// paused it (3 exits in 5 minutes).
func (h *handlers) serverClearCrashLoop(w http.ResponseWriter, r *http.Request) {
	h.app.Server.ClearLoopPause()
	writeJSON(w, map[string]string{"status": "resumed"})
}

func (h *handlers) serverRestart(w http.ResponseWriter, r *http.Request) {
	if err := h.app.Server.Restart(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "restarted"})
}

// ---------------------------------------------------------------------------
// server.cfg editor.

func (h *handlers) serverCfg(w http.ResponseWriter, r *http.Request) {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	if r.Method == http.MethodGet {
		cfg, err := config.LoadServerCfg(cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{
			"path":    cfgPath,
			"values":  cfg.AsMap(),
			"mission": cfg.MissionTemplate(),
		})
		return
	}
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, v := range patch {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			// Empty value. Clear it only for string fields (password,
			// passwordAdmin, description, ...) so the user can actually REMOVE
			// a password — it used to stick around because we skipped blanks
			// wholesale. Numeric fields left blank are still skipped so we never
			// write maxPlayers = "".
			if cfg.IsQuoted(k) {
				cfg.Set(k, "")
			}
			continue
		}
		// Decide by what the file already holds, not by what the value looks
		// like. Otherwise a numeric password ("1234") is written bare —
		// `password = 1234;` — which BattlEye reads as no password at all and
		// locks everyone out, while the panel cheerfully reports "saved".
		if s, ok := v.(string); ok && (cfg.IsQuoted(k) || quotedCfgKeys[k]) {
			cfg.SetString(k, s)
			continue
		}
		cfg.Set(k, v)
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// quotedCfgKeys are serverDZ.cfg keys that must be written as quoted strings
// even when the file does not define them yet — so a first-time numeric
// password or a purely numeric hostname still lands correctly.
var quotedCfgKeys = map[string]bool{
	"password": true, "passwordAdmin": true, "hostname": true,
	"serverName": true, "description": true, "motd": true,
	"shardId": true, "template": true, "instanceId": true,
	"steamQueryPort": false,
}

func (h *handlers) serverCfgMission(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Template == "" {
		http.Error(w, "template required", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !cfg.SetMissionTemplate(req.Template) {
		http.Error(w, "no class Missions block found in server.cfg", http.StatusUnprocessableEntity)
		return
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"template": req.Template})
}

// ---------------------------------------------------------------------------
// Missions.

func (h *handlers) missionsList(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(h.app.ServerDir, "mpmissions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	active, _ := h.missionTemplate()
	type row struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	out := []row{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, row{Name: e.Name(), Active: strings.EqualFold(e.Name(), active)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, map[string]interface{}{"missions": out, "active": active})
}

func (h *handlers) missionsDuplicate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src := strings.TrimSpace(req.Source)
	dst := strings.TrimSpace(req.Target)
	if src == "" || dst == "" {
		http.Error(w, "source and target required", http.StatusBadRequest)
		return
	}
	// Base() guards against `../foo` slipping into paths.
	src = filepath.Base(src)
	dst = filepath.Base(dst)
	base := filepath.Join(h.app.ServerDir, "mpmissions")
	srcDir := filepath.Join(base, src)
	dstDir := filepath.Join(base, dst)
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		http.Error(w, "source mission does not exist", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(dstDir); err == nil {
		http.Error(w, "target mission already exists", http.StatusConflict)
		return
	}
	if err := copyDirTree(srcDir, dstDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "duplicated", "target": dst})
}

// copyDirTree recursively copies src into dst, creating dst and any
// intermediate dirs. Existing files are overwritten. Used for mission
// duplication — the caller has already verified dst doesn't exist.
func copyDirTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// ---------------------------------------------------------------------------
// Mods.

func (h *handlers) modsList(w http.ResponseWriter, r *http.Request) {
	cfg := h.app.Cfg()
	list, err := mods.List(h.app.ServerDir, cfg.ModSourceDir())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"mods":        list,
		"activeMods":  h.app.Cfg().Mods,
		"serverMods":  h.app.Cfg().ServerMods,
		"vanillaPath": cfg.VanillaDayZPath,
		"modSource":   cfg.EffectiveModSource(),
		"sourceDir":   cfg.ModSourceDir(),
	})
}

func (h *handlers) modsInstall(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src := h.app.Cfg().ModSourceDir()
	if src == "" {
		http.Error(w, mods.ErrNoSource.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Install(h.app.ServerDir, src, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "installed", "mod": name, "missions": h.autoInstallModMissions([]string{name}),
		"vehicles": h.syncModVehicles([]string{name})})
}

func (h *handlers) modsUninstall(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Uninstall(h.app.ServerDir, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Also drop from active mod list — atomically, so a concurrent config POST
	// can't write back a copy that still contains the uninstalled mod.
	_ = h.app.MutateConfig(func(c *config.Manager) error {
		c.Mods = removeOnce(c.Mods, name)
		c.ServerMods = removeOnce(c.ServerMods, name)
		return nil
	})
	// Its vehicles stop spawning (they stay in the mission, switched off).
	writeJSON(w, map[string]interface{}{"status": "uninstalled", "mod": name, "vehicles": h.syncModVehicles([]string{name})})
}

func (h *handlers) modsUpdate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	name, err := h.modName(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src := h.app.Cfg().ModSourceDir()
	if src == "" {
		http.Error(w, mods.ErrNoSource.Error(), http.StatusBadRequest)
		return
	}
	if err := mods.Update(h.app.ServerDir, src, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "updated", "mod": name, "missions": h.autoInstallModMissions([]string{name}),
		"vehicles": h.syncModVehicles([]string{name})})
}

func (h *handlers) modsUpdateAll(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	src := h.app.Cfg().ModSourceDir()
	if src == "" {
		http.Error(w, mods.ErrNoSource.Error(), http.StatusBadRequest)
		return
	}
	updated, err := mods.UpdateAll(h.app.ServerDir, src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"updated": updated, "count": len(updated), "missions": h.autoInstallModMissions(updated),
		"vehicles": h.syncModVehicles(updated)})
}

func (h *handlers) modsSyncKeys(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if err := mods.SyncKeys(h.app.ServerDir, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) modsEnable(w http.ResponseWriter, r *http.Request) {
	// Toggling a mod rewrites the launch args, which only take effect on the
	// next start — doing it while the server runs just desyncs the panel from
	// the live process. Require the server stopped, like every other config
	// write. The UI also disables the toggles, but this is authoritative.
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mod        string `json:"mod"`
		Enabled    bool   `json:"enabled"`
		ServerSide bool   `json:"serverSide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mod == "" {
		http.Error(w, "mod required", http.StatusBadRequest)
		return
	}
	if req.Enabled {
		// Guard: never add a mod to the launch args unless it is actually
		// installed in the server dir. Enabling a not-yet-installed mod would
		// make DayZServer fail to start with a cryptic "-mod" error. The UI
		// also locks the toggle, but this is the authoritative check.
		if !strings.HasPrefix(req.Mod, "@") || filepath.Base(req.Mod) != req.Mod {
			http.Error(w, "invalid mod name", http.StatusBadRequest)
			return
		}
		st, err := os.Stat(filepath.Join(h.app.ServerDir, req.Mod))
		if err != nil || !st.IsDir() {
			http.Error(w, "mod is not installed in the server directory: "+req.Mod, http.StatusBadRequest)
			return
		}
	}
	// MutateConfig: a settings auto-save racing this toggle used to write back
	// its pre-toggle copy of the mod lists, silently reverting the change.
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		target, other := &c.Mods, &c.ServerMods
		if req.ServerSide {
			target, other = &c.ServerMods, &c.Mods
		}
		if req.Enabled {
			if !contains(*target, req.Mod) {
				*target = append(append([]string(nil), *target...), req.Mod)
			}
			// A mod is loaded either for everyone (-mod) or by the server
			// alone (-serverMod) — on both lists DayZ loads it twice.
			*other = removeOnce(*other, req.Mod)
		} else {
			*target = removeOnce(*target, req.Mod)
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Vehicle mods: their cars appear on the Vehicles page with the mod, and
	// stop spawning when it is switched off.
	writeJSON(w, map[string]interface{}{"active": h.app.Cfg().Mods, "server": h.app.Cfg().ServerMods,
		"vehicles": h.syncModVehicles([]string{req.Mod})})
}

// modsOrder replaces the active mod list with a caller-supplied ordering.
// Load order matters for DayZ mods (e.g. frameworks like @CF must precede
// their consumers), so the UI exposes drag & drop that sends the new order
// here. Unknown names are refused — otherwise a rename race could silently
// reintroduce a uninstalled mod into the launch args.
func (h *handlers) modsOrder(w http.ResponseWriter, r *http.Request) {
	// Load order only takes effect on the next start, like toggling a mod —
	// require the server stopped so it can't desync from the live process.
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mods       []string `json:"mods"`
		ServerSide bool     `json:"serverSide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		known := map[string]bool{}
		current := c.Mods
		if req.ServerSide {
			current = c.ServerMods
		}
		for _, m := range current {
			known[m] = true
		}
		clean := make([]string, 0, len(req.Mods))
		seen := map[string]bool{}
		for _, m := range req.Mods {
			if !known[m] || seen[m] {
				continue
			}
			seen[m] = true
			clean = append(clean, m)
		}
		if req.ServerSide {
			c.ServerMods = clean
		} else {
			c.Mods = clean
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"active": h.app.Cfg().Mods, "server": h.app.Cfg().ServerMods})
}

func (h *handlers) modName(r *http.Request) (string, error) {
	// Try the query string first so GET callers don't trip on empty-body
	// JSON decoding (which returns io.EOF). POST callers hand us a JSON
	// body — fall through to that.
	if q := strings.TrimSpace(r.URL.Query().Get("mod")); q != "" {
		if err := mods.ValidName(q); err != nil {
			return "", err
		}
		return q, nil
	}
	var req struct {
		Mod string `json:"mod"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", fmt.Errorf("mod name (starting with '@') required")
	}
	if req.Mod == "" || !strings.HasPrefix(req.Mod, "@") {
		return "", fmt.Errorf("mod name (starting with '@') required")
	}
	if err := mods.ValidName(req.Mod); err != nil {
		return "", err
	}
	return req.Mod, nil
}

// ---------------------------------------------------------------------------
// Types.

func (h *handlers) typesList(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	doc, path, err := h.loadTypesFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		Name     string   `json:"name"`
		Nominal  *int     `json:"nominal,omitempty"`
		Min      *int     `json:"min,omitempty"`
		Lifetime *int     `json:"lifetime,omitempty"`
		Restock  *int     `json:"restock,omitempty"`
		Category string   `json:"category,omitempty"`
		Usages   []string `json:"usages,omitempty"` // for the usage filter
		Values   []string `json:"values,omitempty"` // tiers, for the tier filter
	}
	names := func(refs []dztypes.NamedRef) []string {
		if len(refs) == 0 {
			return nil
		}
		out := make([]string, 0, len(refs))
		for _, r := range refs {
			if r.Name != "" {
				out = append(out, r.Name)
			}
		}
		return out
	}
	rows := make([]row, 0, len(doc.Types))
	for _, t := range doc.Types {
		r := row{Name: t.Name, Nominal: t.Nominal, Min: t.Min, Lifetime: t.Lifetime, Restock: t.Restock,
			Usages: names(t.Usages), Values: names(t.Values)}
		if t.Category != nil {
			r.Category = t.Category.Name
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	writeJSON(w, map[string]interface{}{"file": path, "types": rows, "count": len(rows)})
}

func (h *handlers) typesItem(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	name := r.URL.Query().Get("name")
	doc, path, err := h.loadTypesFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t := doc.Find(name)
		if t == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, t)
	case http.MethodPut, http.MethodPost:
		unlock, ok := h.acquireWrite(w)
		if !ok {
			return
		}
		defer unlock()
		var t dztypes.Type
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if t.Name == "" {
			t.Name = name
		}
		doc.Upsert(t)
		if err := dztypes.SaveTypes(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "saved", "name": t.Name})
	case http.MethodDelete:
		unlock, ok := h.acquireWrite(w)
		if !ok {
			return
		}
		defer unlock()
		n := doc.Remove(name)
		if n == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := dztypes.SaveTypes(path, doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"removed": n})
	}
}

func (h *handlers) typesPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, dztypes.BuiltinPresets())
}

func (h *handlers) typesApplyPreset(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		File     string   `json:"file"`
		Names    []string `json:"names"`
		PresetID string   `json:"presetId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var preset *dztypes.Preset
	for _, p := range dztypes.BuiltinPresets() {
		if p.ID == req.PresetID {
			p := p
			preset = &p
			break
		}
	}
	if preset == nil {
		http.Error(w, "unknown preset", http.StatusBadRequest)
		return
	}
	doc, path, err := h.loadTypesFile(req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	applied := 0
	for _, name := range req.Names {
		t := doc.Find(name)
		if t == nil {
			continue
		}
		// Mutating through the Find pointer does not flag the entry, and
		// SaveTypes only writes flagged ones — without this the preset would
		// apply in memory and never reach the file.
		doc.MarkDirty(t.Name)
		t.Usages = mergeNamed(t.Usages, preset.Usages)
		t.Values = mergeNamed(t.Values, preset.Values)
		t.Tags = mergeNamed(t.Tags, preset.Tags)
		if preset.Nominal != nil {
			t.Nominal = preset.Nominal
		}
		if preset.Min != nil {
			t.Min = preset.Min
		}
		if preset.Lifetime != nil {
			t.Lifetime = preset.Lifetime
		}
		if preset.Restock != nil {
			t.Restock = preset.Restock
		}
		if preset.Category != "" {
			t.Category = &dztypes.NamedRef{Name: preset.Category}
		}
		applied++
	}
	if err := dztypes.SaveTypes(path, doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"applied": applied})
}

// loadTypesFile picks the file:
// - "" or "types.xml"    → mpmissions/<mission>/db/types.xml
// - otherwise            → mpmissions/<mission>/moded_types/<file>
func (h *handlers) loadTypesFile(file string) (*dztypes.TypesDoc, string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return nil, "", err
	}
	var path string
	if file == "" || file == "types.xml" {
		path = filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "types.xml")
	} else {
		clean := filepath.Base(file) // prevent traversal
		path = filepath.Join(dztypes.ModedDir(h.app.ServerDir, mission), clean)
	}
	doc, err := dztypes.Load(path)
	if err != nil {
		return nil, path, err
	}
	return doc, path, nil
}

// ---------------------------------------------------------------------------
// Moded types files.

func (h *handlers) modedList(w http.ResponseWriter, r *http.Request) {
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := dztypes.ModedDir(h.app.ServerDir, mission)
	entries, _ := os.ReadDir(dir)

	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	registered, _ := dztypes.RegisteredInModed(ecoPath) // ignore not-exist

	type entry struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		Modified   string `json:"modified"`
		Registered bool   `json:"registered"`
		Types      int    `json:"types"`            // <type> entries in it
		Broken     bool   `json:"broken,omitempty"` // not parseable as a types file
	}
	out := []entry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
			continue
		}
		info, _ := e.Info()
		en := entry{
			Name:       e.Name(),
			Size:       info.Size(),
			Modified:   info.ModTime().Format(time.RFC3339),
			Registered: registered[strings.ToLower(e.Name())],
		}
		if doc, err := dztypes.Load(filepath.Join(dir, e.Name())); err == nil {
			en.Types = len(doc.Types)
		} else {
			en.Broken = true
		}
		out = append(out, en)
	}
	writeJSON(w, map[string]interface{}{"folder": dir, "files": out})
}

func (h *handlers) modedCreate(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		FileName     string `json:"fileName"`
		AutoRegister bool   `json:"autoRegister"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !strings.HasSuffix(strings.ToLower(req.FileName), ".xml") {
		http.Error(w, "file name must end with .xml", http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := dztypes.ModedDir(h.app.ServerDir, mission)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clean := filepath.Base(req.FileName)
	fp := filepath.Join(dir, clean)
	if _, err := os.Stat(fp); err == nil {
		http.Error(w, "file already exists", http.StatusConflict)
		return
	}
	stub := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<types>\n</types>\n")
	if err := os.WriteFile(fp, stub, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.AutoRegister {
		ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
		if err := dztypes.RegisterModedFile(ecoPath, clean, "types"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]string{"file": clean})
}

func (h *handlers) modedDelete(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		FileName string `json:"fileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clean := filepath.Base(req.FileName)
	fp := filepath.Join(dztypes.ModedDir(h.app.ServerDir, mission), clean)
	// A hand-made types file has no other copy: move it to the manager's trash
	// (like a deleted mission) instead of removing it for good.
	trashed := ""
	if _, err := os.Stat(fp); err == nil {
		dir := filepath.Join(h.app.ManagerDir, "trash", "moded", sanitizeKey(mission))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		trashed = filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+clean)
		if err := os.Rename(fp, trashed); err != nil {
			// Another volume: copy, then remove.
			data, rerr := os.ReadFile(fp)
			if rerr == nil {
				rerr = os.WriteFile(trashed, data, 0o644)
			}
			if rerr == nil {
				rerr = os.Remove(fp)
			}
			if rerr != nil {
				http.Error(w, rerr.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	_, _ = dztypes.UnregisterModedFile(ecoPath, clean)
	writeJSON(w, map[string]string{"status": "deleted", "trash": trashed})
}

// modedRegister lists an existing moded_types file in cfgeconomycore.xml —
// DayZ ignores a types file nobody registered.
func (h *handlers) modedRegister(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		FileName string `json:"fileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clean := filepath.Base(req.FileName)
	if !strings.HasSuffix(strings.ToLower(clean), ".xml") {
		http.Error(w, "file name must end with .xml", http.StatusBadRequest)
		return
	}
	if st, err := os.Stat(filepath.Join(dztypes.ModedDir(h.app.ServerDir, mission), clean)); err != nil || st.IsDir() {
		http.Error(w, "file not found in moded_types", http.StatusNotFound)
		return
	}
	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	if err := dztypes.RegisterModedFile(ecoPath, clean, "types"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "registered", "file": clean})
}

// ---------------------------------------------------------------------------
// Raw files tree/editor. Confined to the server directory.

// binaryExts lists file extensions the editor refuses to open. Opening a
// multi-megabyte PE binary as text would freeze the browser (it did — that
// was the reported RPT/files crash). Users interested in non-DayZ files
// can still reach them via the OS file explorer.
var binaryExts = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".pbo": true, ".bin": true, ".bisign": true, ".bikey": true,
	".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	".pak": true, ".mdk": true, ".mtk": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true, ".tga": true, ".paa": true,
	".wav": true, ".ogg": true, ".mp3": true, ".mp4": true,
	".p3d": true, ".rvmat": true, ".edds": true, ".tex": true,
}

// hiddenFiles lists file/dir names the editor hides entirely. Keeps the
// tree focused on server configuration (profiles, mpmissions, cfg/xml)
// rather than cluttering it with the manager's own state or backup trash.
var hiddenNames = map[string]bool{
	".dayz-manager": true,
	"thumbs.db":     true,
	".ds_store":     true,
	"node_modules":  true,
}

func isBinaryFile(name string) bool {
	return binaryExts[strings.ToLower(filepath.Ext(name))]
}

// serverBinaryNames are the DayZ server binary names on either platform. The
// Linux ones have no extension, so binaryExts alone would not catch them, and
// Start chmods +x and runs whatever file carries that name.
var serverBinaryNames = map[string]bool{
	"dayzserver": true, "dayzserver_x64": true, "dayzserver_x64.exe": true,
}

// writeRefusal says why the file editor must not overwrite full, or "" when it
// may. Besides binary extensions it refuses the server binary by name and any
// extension-less file with an exec bit (Linux ELF binaries such as the manager
// itself). Files with an extension are not judged by exec bit: on NTFS/SMB
// mounts every file reports 0777 and configs must stay editable.
func writeRefusal(full string) string {
	base := filepath.Base(full)
	if isBinaryFile(base) {
		return "binary file — not editable in the panel"
	}
	if serverBinaryNames[strings.ToLower(base)] {
		return "server binary — not editable in the panel"
	}
	if filepath.Ext(base) == "" {
		if st, err := os.Stat(full); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return "executable file — not editable in the panel"
		}
	}
	return ""
}

func isHidden(name string) bool {
	lower := strings.ToLower(name)
	if hiddenNames[lower] {
		return true
	}
	// Strip per-backup timestamp suffix (.bak.2026-04-20T12-00-00).
	if strings.Contains(lower, ".bak") {
		return true
	}
	if strings.HasSuffix(lower, ".tmp") {
		return true
	}
	return false
}

func (h *handlers) filesTree(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type node struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		IsDir  bool   `json:"isDir"`
		Size   int64  `json:"size"`
		Binary bool   `json:"binary,omitempty"`
	}
	out := []node{}
	for _, e := range entries {
		name := e.Name()
		if isHidden(name) {
			continue
		}
		if !e.IsDir() && isBinaryFile(name) {
			continue
		}
		info, _ := e.Info()
		child := filepath.ToSlash(filepath.Join(rel, name))
		out = append(out, node{Name: name, Path: child, IsDir: e.IsDir(), Size: sizeOrZero(info)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, map[string]interface{}{"path": rel, "entries": out})
}

// Files above this size would freeze the browser if rendered inline. A
// fresh DayZ types.xml is ~600 KB, the largest legitimate hand-edited file
// in a mission tree is well under 2 MB, so this is a safe ceiling.
const maxEditableBytes = 2 * 1024 * 1024

func (h *handlers) filesRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isBinaryFile(filepath.Base(full)) {
		http.Error(w, "binary file — not editable in the panel", http.StatusUnsupportedMediaType)
		return
	}
	st, err := os.Stat(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if st.Size() > maxEditableBytes {
		http.Error(w, fmt.Sprintf("file too large (%d bytes) — panel editor limit is %d", st.Size(), maxEditableBytes), http.StatusRequestEntityTooLarge)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"path": rel, "content": string(body)})
}

func (h *handlers) filesWrite(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := h.resolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The editor never opens binaries; refuse to overwrite one (a text body
	// over DayZServer_x64.exe or a .pbo would break the server).
	if reason := writeRefusal(full); reason != "" {
		http.Error(w, reason, http.StatusUnsupportedMediaType)
		return
	}
	if st, err := os.Stat(full); err == nil && st.IsDir() {
		http.Error(w, "that path is a folder", http.StatusBadRequest)
		return
	}
	_ = util.BackupBeforeWrite(full)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------
// Validator.

// ---------------------------------------------------------------------------
// Logs.

func (h *handlers) logsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, dzlogs.WithPrevious(h.app.ServerDir, h.app.Cfg().ProfilesDir))
}

// logsRead returns a tail snapshot of a log file. Cap is deliberately tight
// (128 KB default, 512 KB max) — the DayZ RPT grows to tens of megabytes and
// dumping that into the DOM freezes the browser. Callers can request more
// via ?bytes= up to the cap.
func (h *handlers) logsRead(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !h.knownLogID(id) {
		http.Error(w, "unknown log id", http.StatusBadRequest)
		return
	}
	path := dzlogs.Resolve(h.app.ServerDir, h.app.Cfg().ProfilesDir, id)
	if path == "" { // e.g. no .RPT written yet — an empty log, not an error
		writeJSON(w, map[string]interface{}{"path": "", "content": "", "size": 0, "end": 0, "truncated": false, "missing": true})
		return
	}
	const (
		defaultBytes = 128 * 1024
		hardCap      = 512 * 1024
	)
	want := int64(defaultBytes)
	if v := r.URL.Query().Get("bytes"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			if n > hardCap {
				n = hardCap
			}
			want = n
		}
	}
	f, err := os.Open(path)
	if err != nil {
		// Not written yet: the viewer says it waits for the file.
		writeJSON(w, map[string]interface{}{"path": path, "content": "", "size": 0, "end": 0, "truncated": false, "missing": true})
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	size := int64(0)
	offset := int64(0)
	truncated := false
	if st != nil {
		size = st.Size()
		if size > want {
			offset = size - want
			truncated = true
		}
	}
	_, _ = f.Seek(offset, 0)
	body, _ := io.ReadAll(f)
	// Where this snapshot ends — the live stream continues from exactly here
	// (the file may have grown since Stat).
	end := offset + int64(len(body))
	// If we seeked mid-line, drop the first (partial) line so the UI does
	// not display a misleading fragment at the top.
	if offset > 0 {
		if nl := strings.IndexByte(string(body), '\n'); nl >= 0 {
			body = body[nl+1:]
		}
	}
	writeJSON(w, map[string]interface{}{
		"path":      path,
		"content":   string(body),
		"size":      size,
		"offset":    offset,
		"end":       end,
		"truncated": truncated,
	})
}

// knownLogID reports whether id names one of the log sources (earlier runs'
// RPT / ADM included).
func (h *handlers) knownLogID(id string) bool {
	for _, s := range dzlogs.WithPrevious(h.app.ServerDir, h.app.Cfg().ProfilesDir) {
		if s.ID == id {
			return true
		}
	}
	return false
}

// logsDownload sends a whole log file: the viewer shows only the tail, and
// from a phone the folder button can only show a path.
func (h *handlers) logsDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !h.knownLogID(id) {
		http.Error(w, "unknown log id", http.StatusBadRequest)
		return
	}
	path := dzlogs.Resolve(h.app.ServerDir, h.app.Cfg().ProfilesDir, id)
	if path == "" {
		http.Error(w, "the log file does not exist yet", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "the log file does not exist yet", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(filepath.Base(path))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	// The size at open: the server may still be appending to the file.
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	_, _ = io.CopyN(w, f, st.Size())
}

// logsStream is a Server-Sent Events endpoint. EventSource on the client
// keeps the socket open; the server writes "data: <chunk>\n\n" as it reads.
func (h *handlers) logsStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !h.knownLogID(id) {
		http.Error(w, "unknown log id", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	resolve := func() string { return dzlogs.Resolve(h.app.ServerDir, h.app.Cfg().ProfilesDir, id) }
	exists := func(p string) bool {
		if p == "" {
			return false
		}
		_, err := os.Stat(p)
		return err == nil
	}

	// Where to start: the SSE id of the last event the browser saw (on an
	// automatic reconnect) is "<file>|<offset>"; otherwise the end of the
	// snapshot the page just loaded (?offset=); otherwise the last 64 KB.
	path := resolve()
	start := int64(-1)
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			start = n
		}
	}
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if i := strings.LastIndexByte(last, '|'); i > 0 {
			if n, err := strconv.ParseInt(last[i+1:], 10, 64); err == nil && n >= 0 {
				start = n
				if last[:i] != filepath.Base(path) {
					start = 0 // a newer file replaced the one the browser followed
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": stream open\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		// A log that does not exist yet (server never started, no .RPT this
		// run) is waited for instead of failing: a 404 made EventSource give
		// up for good, so the log never appeared once the server started.
		for !exists(path) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			path, start = resolve(), 0
		}
		if start < 0 {
			if st, err := os.Stat(path); err == nil {
				start = st.Size() - 64*1024
			}
			if start < 0 {
				start = 0
			}
		}

		// DayZ writes a new timestamped .RPT/.ADM on every start: follow the
		// newest file instead of staying on the previous run's log.
		stop := make(chan struct{})
		switched := make(chan string, 1)
		go func(cur string) {
			t := time.NewTicker(3 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					close(stop)
					return
				case <-t.C:
					if p := resolve(); p != "" && p != cur {
						switched <- p
						close(stop)
						return
					}
				}
			}
		}(path)

		base, pos := filepath.Base(path), start
		var partial []byte // a line the writer has not finished yet
		_ = dzlogs.TailFrom(stop, path, start, func(chunk []byte) error {
			pos += int64(len(chunk))
			data := append(partial, chunk...)
			cut := bytes.LastIndexByte(data, '\n')
			if cut < 0 && len(data) < 64*1024 {
				partial = data
				return nil
			}
			if cut < 0 { // an absurdly long line: send what we have
				cut = len(data)
			}
			complete := strings.TrimSuffix(string(data[:cut]), "\r")
			partial = append([]byte(nil), data[min(cut+1, len(data)):]...)
			// One event per line — never split a line across two events or
			// emit a blank event for the trailing newline. The last event
			// carries the resume position (the start of the unfinished line).
			lines := strings.Split(complete, "\n")
			for i, line := range lines {
				if i == len(lines)-1 {
					if _, err := fmt.Fprintf(w, "id: %s|%d\n", base, pos-int64(len(partial))); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", strings.TrimSuffix(line, "\r")); err != nil {
					return err
				}
			}
			flusher.Flush()
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		select {
		case p := <-switched:
			path, start = p, 0
			fmt.Fprintf(w, "data: \ndata: ===== %s =====\n\n", filepath.Base(p))
			flusher.Flush()
		default:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// RCon.

func (h *handlers) rconPlayers(w http.ResponseWriter, r *http.Request) {
	// Served from the shared 2s cache so the RCon page's 5s poll reuses the one
	// connection instead of opening (and logging in) a fresh one each time.
	players, err := h.app.RCon.PlayersCached(2 * time.Second)
	if err != nil {
		rconFail(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"players": players, "count": len(players)})
}

// rconErrKey names the common RCon failures with the i18n key the panel shows
// for them ("" for anything else).
func rconErrKey(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, rcon.ErrNotConfigured):
		return "rcon.err.notConfigured"
	case errors.Is(err, rcon.ErrAuthFailed):
		return "rcon.err.auth"
	case errors.Is(err, rcon.ErrTimeout):
		return "rcon.err.timeout"
	}
	return ""
}

// rconFail answers a failed RCon call: a known failure as {error: <i18n key>,
// message} that the panel translates, anything else as plain text.
func rconFail(w http.ResponseWriter, err error) {
	if k := rconErrKey(err); k != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": k, "message": err.Error()})
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

// rconStatus tells the RCon page what the connection runs on: BattlEye loads
// its password when the server starts (beserver_x64.cfg → *_active_*.cfg), so
// one saved in the panel since then is pending until the next restart — the
// panel keeps logging in with the live one meanwhile.
func (h *handlers) rconStatus(w http.ResponseWriter, r *http.Request) {
	running := h.app.ServerIsRunning()
	rc := h.app.RCon
	out := map[string]interface{}{
		"running":    running,
		"configured": rc.Configured(),
		"connected":  rc.IsConnected(),
	}
	if running {
		rc.TouchConnect()
		if be := h.app.ActiveBEConfig(); be != nil {
			saved := h.app.Cfg().RConPassword
			out["activeFile"] = filepath.Base(be.Path)
			out["activePort"] = be.RConPort
			out["pending"] = saved != "" && be.RConPassword != "" && be.RConPassword != saved
		}
		if err := rc.LastConnectErr(); err != nil {
			out["error"] = err.Error()
			out["errorKey"] = rconErrKey(err)
		}
	}
	writeJSON(w, out)
}

// rconChatRecent returns recent in-game chat captured from the BattlEye message
// stream (see internal/rcon/chat.go). It never blocks on the network: it kicks a
// throttled background connect so the stream stays warm while the Chat page is
// open, then returns the ring buffer plus connection state so the UI can show a
// clear "RCon not connected / not configured" message instead of a blank feed.
func (h *handlers) rconChatRecent(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 300 {
			limit = n
		}
	}
	rc := h.app.RCon
	rc.TouchConnect()
	out := map[string]interface{}{
		"messages":   rc.RecentChat(limit),
		"connected":  rc.IsConnected(),
		"configured": rc.Configured(),
	}
	if h.app.ServerIsRunning() {
		if err := rc.LastConnectErr(); err != nil {
			out["errorKey"] = rconErrKey(err)
		}
	}
	writeJSON(w, out)
}

func (h *handlers) rconSay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message  string `json:"message"`
		PlayerID *int   `json:"playerId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	var err error
	if req.PlayerID != nil {
		err = h.app.RCon.SayTo(*req.PlayerID, req.Message)
	} else {
		err = h.app.RCon.Say(req.Message)
	}
	if err != nil {
		rconFail(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID int    `json:"playerId"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.app.RCon.Kick(req.PlayerID, req.Reason); err != nil {
		rconFail(w, err)
		return
	}
	h.app.RCon.InvalidatePlayers() // reflect the kick on the next poll
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID int    `json:"playerId"`
		Minutes  int    `json:"minutes"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.app.RCon.Ban(req.PlayerID, req.Minutes, req.Reason); err != nil {
		rconFail(w, err)
		return
	}
	h.app.RCon.InvalidatePlayers() // reflect the ban on the next poll
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *handlers) rconCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		http.Error(w, "command required", http.StatusBadRequest)
		return
	}
	out, err := h.app.RCon.Command(req.Command)
	if err != nil {
		rconFail(w, err)
		return
	}
	writeJSON(w, map[string]string{"output": out})
}

// ---------------------------------------------------------------------------
// events.xml editor.

// eventsList lists the events of every events file the mission uses —
// db/events.xml and each one registered in cfgeconomycore.xml (mods, the
// panel's dzm_vehicles) — with how many spawn points cfgeventspawns.xml gives
// each. An event defined again in a later file is flagged: the first wins.
func (h *handlers) eventsList(w http.ResponseWriter, r *http.Request) {
	doc, path, err := h.loadEventsFile()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mission, _ := h.missionTemplate()
	missionDir := dztypes.MissionDir(h.app.ServerDir, mission)
	type row struct {
		Name     string `json:"name"`
		File     string `json:"file"` // relative to the mission
		Nominal  *int   `json:"nominal,omitempty"`
		Min      *int   `json:"min,omitempty"`
		Max      *int   `json:"max,omitempty"`
		Lifetime *int   `json:"lifetime,omitempty"`
		Active   *int   `json:"active,omitempty"`
		Children int    `json:"children"`
		Points   int    `json:"points"`        // positions in cfgeventspawns.xml
		Position string `json:"position"`      // fixed | player | uniform
		Dup      bool   `json:"dup,omitempty"` // defined in an earlier file too
	}
	points := map[string]int{}
	if sp, err := dztypes.LoadEventSpawns(filepath.Join(missionDir, "cfgeventspawns.xml")); err == nil {
		for _, e := range sp.Events {
			points[strings.ToLower(e.Name)] += len(e.Pos)
		}
	}
	out := []row{}
	seen := map[string]bool{}
	files := []string{}
	var problems []string
	add := func(rel string, d *dztypes.EventsDoc) {
		files = append(files, rel)
		for _, e := range d.Events {
			key := strings.ToLower(e.Name)
			r := row{Name: e.Name, File: rel, Nominal: e.Nominal, Min: e.Min, Max: e.Max, Lifetime: e.Lifetime,
				Active: e.Active, Points: points[key], Position: strings.ToLower(strings.TrimSpace(e.Position)), Dup: seen[key]}
			if e.Children != nil {
				r.Children = len(e.Children.Child)
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	add("db/events.xml", doc)
	for _, f := range missionEcoFiles(missionDir, "events") {
		if strings.EqualFold(f.Rel, "db/events.xml") {
			continue
		}
		d, err := dztypes.LoadEvents(f.Path)
		if err != nil {
			problems = append(problems, f.Rel+": "+err.Error())
			continue
		}
		add(f.Rel, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, map[string]interface{}{"file": path, "files": files, "events": out, "count": len(out), "problems": problems})
}

func (h *handlers) eventsItem(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	doc, path, err := h.loadEventsFileRel(r.URL.Query().Get("file"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		e := doc.Find(name)
		if e == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, e)
	case http.MethodPut, http.MethodPost:
		if !h.requireStopped(w) {
			return
		}
		var e dztypes.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if e.Name == "" {
			e.Name = name
		}
		if e.Name == "" {
			http.Error(w, "event name required", http.StatusBadRequest)
			return
		}
		// Surgical: patch the edited numbers in place. A full re-encode used to
		// strip <flags>, <position>, <limit>, <secondary> and the three radii
		// from every event in the file — see internal/types/events_write.go.
		if doc.Find(e.Name) != nil {
			fields := map[string]int{}
			for tag, v := range map[string]*int{
				"nominal": e.Nominal, "min": e.Min, "max": e.Max,
				"lifetime": e.Lifetime, "restock": e.Restock,
				"saveable": e.Saveable, "active": e.Active,
			} {
				if v != nil {
					fields[tag] = *v
				}
			}
			if _, err := dztypes.PatchEvent(path, e.Name, fields); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The UI has always posted the children table; the handler used to
			// drop it and still answer "saved". Loot tables for helicrashes and
			// vehicle events live here, so that made the whole table decorative.
			if e.Children != nil {
				if _, err := dztypes.PatchEventChildren(path, e.Name, e.Children.Child); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		} else if err := dztypes.AppendEvent(path, &e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "saved", "name": e.Name})
	case http.MethodDelete:
		if !h.requireStopped(w) {
			return
		}
		n, err := dztypes.DeleteEventBlock(path, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]interface{}{"removed": n})
	}
}

// loadEventsFileRel opens one of the mission's events files by its path
// relative to the mission ("" = db/events.xml). Only files the mission
// registers in cfgeconomycore.xml are accepted.
func (h *handlers) loadEventsFileRel(rel string) (*dztypes.EventsDoc, string, error) {
	if rel == "" || strings.EqualFold(rel, "db/events.xml") {
		return h.loadEventsFile()
	}
	mission, err := h.missionTemplate()
	if err != nil {
		return nil, "", err
	}
	for _, f := range missionEcoFiles(dztypes.MissionDir(h.app.ServerDir, mission), "events") {
		if strings.EqualFold(f.Rel, rel) {
			doc, err := dztypes.LoadEvents(f.Path)
			return doc, f.Path, err
		}
	}
	return nil, "", fmt.Errorf("%q is not an events file of the mission", rel)
}

func (h *handlers) loadEventsFile() (*dztypes.EventsDoc, string, error) {
	mission, err := h.missionTemplate()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "db", "events.xml")
	doc, err := dztypes.LoadEvents(path)
	if err != nil {
		return nil, path, err
	}
	return doc, path, nil
}

// ---------------------------------------------------------------------------
// Validator.

func (h *handlers) updateCheck(w http.ResponseWriter, r *http.Request) {
	res := updater.Check(r.Context(), h.app.Version, h.app.Mode)
	writeJSON(w, res)
}

// validatorOptions gathers the server context the deeper validator checks
// need: launch mods, ports, server.cfg, profiles and the world size.
func (h *handlers) validatorOptions() validator.Options {
	cfg := h.app.Cfg()
	mission, _ := h.missionTemplate()
	rconPort := cfg.RConPort
	if rconPort == 0 {
		if be := config.FindBEConfig(h.beDir()); be != nil && be.RConPort != 0 {
			rconPort = be.RConPort
		}
	}
	o := validator.Options{
		ServerDir:   h.app.ServerDir,
		Mission:     mission,
		ServerCfg:   cfg.ServerCfg,
		ProfilesDir: h.profilesAbs(),
		Mods:        cfg.Mods,
		ServerMods:  cfg.ServerMods,
		ServerPort:  cfg.ServerPort,
		RConPort:    rconPort,
	}
	// Only a real size: the 15360 default guess would make the bounds check
	// (and its auto-fix) drop valid positions on a larger unknown map. Left 0,
	// the validator falls back to its generous ceiling.
	if mission != "" {
		if mi := h.currentMap(); mi.sizeKnown {
			o.WorldSize = mi.Size
		}
	}
	return o
}

// validate runs the validator, reusing the last result while nothing it reads
// has changed (validate_cache.go). ?force=1 always runs; ?peek=1 never does —
// it returns the last result, if any, with fresh saying whether it is current.
func (h *handlers) validate(w http.ResponseWriter, r *http.Request) {
	o := h.validatorOptions()
	q := r.URL.Query()
	if q.Get("peek") == "1" {
		res, fresh := h.cachedValidation(o)
		if res == nil {
			writeJSON(w, map[string]interface{}{"none": true})
			return
		}
		writeJSON(w, map[string]interface{}{"issues": res.issues, "count": len(res.issues),
			"checkedAt": res.at.Unix(), "cached": true, "fresh": fresh})
		return
	}
	res, cached, err := h.runValidation(o, q.Get("force") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"issues": res.issues, "count": len(res.issues),
		"checkedAt": res.at.Unix(), "cached": cached, "fresh": true})
}

// validateFix applies the safe auto-fixes (whitelist unknown loot flags into
// cfglimitsdefinition.xml) and returns what it changed. Requires the server
// stopped — it edits a mission file.
func (h *handlers) validateFix(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	if _, err := h.missionTemplate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Optional body {"codes": [...]}: fix only those kinds of problem.
	var req struct {
		Codes []string `json:"codes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	want := map[string]bool{}
	for _, c := range req.Codes {
		want[c] = true
	}
	all := len(want) == 0
	o := h.validatorOptions()
	fixed, err := validator.AutoFixWith(o, req.Codes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Launch-list fixes live in manager.json, which only this layer owns:
	// drop enabled mods whose folder is gone, and duplicate entries.
	if all || want["mods.missing"] || want["mods.duplicate"] {
		var removed []string
		_ = h.app.MutateConfig(func(c *config.Manager) error {
			clean := func(list []string) []string {
				seen := map[string]bool{}
				out := make([]string, 0, len(list))
				for _, m := range list {
					k := strings.ToLower(m)
					if seen[k] && (all || want["mods.duplicate"]) {
						removed = append(removed, m+" (duplicate)")
						continue
					}
					seen[k] = true
					dir := m
					if !filepath.IsAbs(dir) {
						dir = filepath.Join(h.app.ServerDir, m)
					}
					if st, err := os.Stat(dir); (err != nil || !st.IsDir()) && (all || want["mods.missing"]) {
						removed = append(removed, m+" (folder missing)")
						continue
					}
					out = append(out, m)
				}
				return out
			}
			c.Mods = clean(c.Mods)
			c.ServerMods = clean(c.ServerMods)
			return nil
		})
		for _, m := range removed {
			fixed = append(fixed, "removed "+m+" from the launch mod list")
		}
	}
	if fixed == nil {
		fixed = []string{}
	}
	writeJSON(w, map[string]interface{}{"fixed": fixed, "count": len(fixed)})
}

// ---------------------------------------------------------------------------
// Steam detection.

func (h *handlers) steamDetect(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"installs": util.FindDayZInstalls(), "workshop": util.FindWorkshopContent()})
}

// ---------------------------------------------------------------------------
// Mod types.xml auto-install.
//
// scan walks @ModName/ and looks for XML files that parse as a <types> root —
// the loot-table format DayZ expects under moded_types. install copies a
// chosen file into the active mission's moded_types folder and registers it
// in cfgeconomycore.xml so the server actually loads it on next start.

func (h *handlers) modsScanTypes(w http.ResponseWriter, r *http.Request) {
	// scan-types is a GET — modName() decodes a JSON body, which on a GET
	// produces io.EOF and surfaces as the "EOF" toast users have been
	// seeing. Read the mod name from the query string instead.
	name := strings.TrimSpace(r.URL.Query().Get("mod"))
	if name == "" || !strings.HasPrefix(name, "@") {
		http.Error(w, "mod name (starting with '@') required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(name, `/\`) {
		http.Error(w, "invalid mod name", http.StatusBadRequest)
		return
	}
	// Also probe the workshop side: many !Workshop mods carry their loot
	// XML under @Mod/dz/... that the server-side copy may not yet have.
	dir := filepath.Join(h.app.ServerDir, name)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if alt, err := mods.SourcePath(h.app.Cfg().ModSourceDir(), name); err == nil {
			if st, err := os.Stat(alt); err == nil && st.IsDir() {
				dir = alt
			}
		}
	}
	// !Workshop entries are usually NTFS junctions; resolve before walking
	// so filepath.Walk descends into the real folder.
	if r, err := filepath.EvalSymlinks(dir); err == nil && r != "" {
		dir = r
	}
	type hit struct {
		Path  string `json:"path"`  // absolute path
		Rel   string `json:"rel"`   // relative to mod folder
		Types int    `json:"types"` // how many <type> entries; 0 if not a types doc
		Size  int64  `json:"size"`
		Kind  string `json:"kind"` // "types" | "xml" | "json"
	}
	var hits []hit
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info == nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext != ".xml" && ext != ".json" {
			return nil
		}
		if info.Size() > 10*1024*1024 { // skip absurdly-large files
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		entry := hit{
			Path: p, Rel: filepath.ToSlash(rel), Size: info.Size(),
			Kind: strings.TrimPrefix(ext, "."),
		}
		// Only XML can be a DayZ types doc. We try the strict parser first
		// and tag the file so the UI can highlight known-good targets, but
		// non-matching XMLs still appear so the user can pick one whose
		// author named it differently (cfgeconomycore.xml, events.xml, etc).
		if ext == ".xml" {
			if doc, err := dztypes.Load(p); err == nil && len(doc.Types) > 0 {
				entry.Kind = "types"
				entry.Types = len(doc.Types)
			}
		}
		hits = append(hits, entry)
		return nil
	})
	// Sort: types-rooted files first (best candidates), then by rel path.
	sort.Slice(hits, func(i, j int) bool {
		if (hits[i].Kind == "types") != (hits[j].Kind == "types") {
			return hits[i].Kind == "types"
		}
		return strings.ToLower(hits[i].Rel) < strings.ToLower(hits[j].Rel)
	})
	writeJSON(w, map[string]interface{}{"mod": name, "scannedDir": dir, "files": hits})
}

func (h *handlers) modsInstallTypes(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Mod  string `json:"mod"`
		Rel  string `json:"rel"`      // path relative to the mod folder
		Name string `json:"fileName"` // target name in moded_types/
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Mod, "@") || strings.ContainsAny(req.Mod, `/\`) {
		http.Error(w, "invalid mod name", http.StatusBadRequest)
		return
	}
	if strings.Contains(req.Rel, "..") {
		http.Error(w, "invalid relative path", http.StatusBadRequest)
		return
	}
	src := filepath.Join(h.app.ServerDir, req.Mod, filepath.FromSlash(req.Rel))
	if _, err := os.Stat(src); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mission, err := h.missionTemplate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Sanitize target name: alphanumerics/dashes/underscores + .xml.
	target := strings.TrimSpace(req.Name)
	if target == "" {
		target = strings.ReplaceAll(strings.TrimPrefix(req.Mod, "@"), " ", "_") + "_types.xml"
	}
	target = filepath.Base(target)
	if !strings.HasSuffix(strings.ToLower(target), ".xml") {
		target += ".xml"
	}
	modedDir := dztypes.ModedDir(h.app.ServerDir, mission)
	if err := os.MkdirAll(modedDir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dst := filepath.Join(modedDir, target)
	data, err := os.ReadFile(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = util.BackupBeforeWrite(dst)
	if err := writeFileAtomic(dst, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ecoPath := filepath.Join(dztypes.MissionDir(h.app.ServerDir, mission), "cfgeconomycore.xml")
	_ = dztypes.RegisterModedFile(ecoPath, target, "types")
	writeJSON(w, map[string]string{"status": "installed", "file": target})
}

// ---------------------------------------------------------------------------
// Import / sync from an existing DayZ server install.
//
// Preview reads the foreign serverDZ.cfg + launch artifacts and returns what
// the manager *would* absorb: mission template, mod names detected in the
// directory, key lines from the cfg. Apply copies selected mods into the
// current server dir, merges the foreign cfg into ours, and updates
// manager.json. This lets admins with a pre-existing server adopt the
// manager without recreating everything by hand.

type importReq struct {
	SourceDir string `json:"sourceDir"`
	CopyMods  bool   `json:"copyMods"`
	CopyCfg   bool   `json:"copyCfg"`
	Mission   string `json:"mission"`
}

func (h *handlers) importPreview(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, ok := h.importSource(w, req.SourceDir)
	if !ok {
		return
	}
	cfgPath := filepath.Join(src, "serverDZ.cfg")
	preview := map[string]interface{}{"sourceDir": src}
	if _, err := os.Stat(cfgPath); err == nil {
		if cfg, err := config.LoadServerCfg(cfgPath); err == nil {
			preview["cfg"] = cfg.AsMap()
			preview["mission"] = cfg.MissionTemplate()
		}
	}
	// Detect @-prefixed mod folders at the top level — the universal DayZ
	// convention for client-side mods. Excludes keys/battleye/mpmissions/etc.
	var foundMods []string
	if entries, err := os.ReadDir(src); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "@") {
				foundMods = append(foundMods, e.Name())
			}
		}
	}
	preview["mods"] = foundMods
	var missions, missing []string
	if entries, err := os.ReadDir(filepath.Join(src, "mpmissions")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				missions = append(missions, e.Name())
				// Missions this server lacks get copied along on import.
				if _, err := os.Stat(filepath.Join(h.app.ServerDir, "mpmissions", e.Name())); err != nil {
					missing = append(missing, e.Name())
				}
			}
		}
	}
	preview["missions"] = missions
	preview["missingMissions"] = missing
	writeJSON(w, preview)
}

// importSource validates the folder to import from. Its errors are i18n keys
// the page shows in the panel's language.
func (h *handlers) importSource(w http.ResponseWriter, dir string) (string, bool) {
	src := strings.TrimSpace(dir)
	if src == "" {
		authError(w, http.StatusBadRequest, "sync.err.noSource", map[string]interface{}{"message": "sourceDir is required"})
		return "", false
	}
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		authError(w, http.StatusBadRequest, "sync.err.notFound", map[string]interface{}{"message": "source directory not found"})
		return "", false
	}
	// Refuse to import from the very folder we're running in — it would loop
	// and overwrite the manager's own files.
	absSrc, _ := filepath.Abs(src)
	absDst, _ := filepath.Abs(h.app.ServerDir)
	if strings.EqualFold(absSrc, absDst) {
		authError(w, http.StatusBadRequest, "sync.err.same", map[string]interface{}{"message": "source and destination are the same directory"})
		return "", false
	}
	return src, true
}

// importMission makes sure mpmissions/<name> exists in this server, copying it
// from the source server when it is missing (aside first, renamed on success,
// like the mods). Returns whether the folder was copied.
func (h *handlers) importMission(src, name string) (bool, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return false, fmt.Errorf("invalid mission name %q", name)
	}
	dst := filepath.Join(h.app.ServerDir, "mpmissions", name)
	if st, err := os.Stat(dst); err == nil && st.IsDir() {
		return false, nil
	}
	from := filepath.Join(src, "mpmissions", name)
	if st, err := os.Stat(from); err != nil || !st.IsDir() {
		return false, fmt.Errorf("mission %s is in neither server's mpmissions", name)
	}
	tmp := dst + ".importing"
	_ = os.RemoveAll(tmp)
	if err := copyDirTree(from, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	return true, nil
}

func (h *handlers) importApply(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()

	var req importReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, ok := h.importSource(w, req.SourceDir)
	if !ok {
		return
	}

	var copied, failed []string
	if req.CopyMods {
		if entries, err := os.ReadDir(src); err == nil {
			for _, e := range entries {
				if !e.IsDir() || !strings.HasPrefix(e.Name(), "@") {
					continue
				}
				dst := filepath.Join(h.app.ServerDir, e.Name())
				if _, err := os.Stat(dst); err == nil {
					continue // skip already-present mods
				}
				// Copy aside and rename on success. A failure used to leave a
				// half-copied folder at the destination: the error was
				// discarded, the user was told nothing, and the Stat guard
				// above then skipped that mod on every later import — so a
				// broken mod stayed broken forever.
				tmp := dst + ".importing"
				_ = os.RemoveAll(tmp)
				if err := copyDirTree(filepath.Join(src, e.Name()), tmp); err != nil {
					_ = os.RemoveAll(tmp)
					failed = append(failed, fmt.Sprintf("%s: %v", e.Name(), err))
					continue
				}
				if err := os.Rename(tmp, dst); err != nil {
					_ = os.RemoveAll(tmp)
					failed = append(failed, fmt.Sprintf("%s: %v", e.Name(), err))
					continue
				}
				copied = append(copied, e.Name())
			}
		}
	}

	if req.CopyCfg {
		srcCfg := filepath.Join(src, "serverDZ.cfg")
		if _, err := os.Stat(srcCfg); err == nil {
			data, err := os.ReadFile(srcCfg)
			if err == nil {
				dstCfg := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
				_ = util.BackupBeforeWrite(dstCfg)
				// Don't swallow this: a failed server.cfg copy during import used
				// to look like success, leaving the server on its old config.
				if err := writeFileAtomic(dstCfg, data); err != nil {
					h.app.Log.Printf("import: failed to write %s: %v", h.app.Cfg().ServerCfg, err)
				}
			}
		}
	}
	// The mission: the one picked, or else the template of a serverDZ.cfg just
	// copied over. Its folder comes along when this server lacks it — import
	// used to switch the template to a mission that was never copied, and the
	// server then failed to start.
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	mission := strings.TrimSpace(req.Mission)
	if mission == "" && req.CopyCfg {
		if cfg, err := config.LoadServerCfg(cfgPath); err == nil {
			mission = cfg.MissionTemplate()
		}
	}
	copiedMission, missionErr := "", ""
	if mission != "" {
		if did, err := h.importMission(src, mission); err != nil {
			missionErr = err.Error()
			h.app.Log.Printf("import: mission %s: %v", mission, err)
		} else if did {
			copiedMission = mission
		}
	}
	// Never point the server at a mission folder it does not have.
	if req.Mission != "" && missionErr == "" {
		if cfg, err := config.LoadServerCfg(cfgPath); err == nil {
			cfg.SetMissionTemplate(mission)
			_ = cfg.Save(cfgPath)
		}
	}
	// Register the copied mods atomically after the file copies finish.
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		for _, name := range copied {
			if !contains(c.Mods, name) {
				c.Mods = append(c.Mods, name)
			}
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"copiedMods": copied, "failedMods": failed,
		"mission": mission, "copiedMission": copiedMission, "missionError": missionErr})
}

// ---------------------------------------------------------------------------
// Backups. Every atomic write goes through util.BackupBeforeWrite which
// creates <file>.bak.<timestamp> siblings. This endpoint lists them per file
// and offers restore (which itself backs up the current file before swapping).

func (h *handlers) backupsList(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := filepath.Dir(full)
	base := filepath.Base(full)
	entries, _ := os.ReadDir(dir)
	type bak struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Time int64  `json:"time"`
	}
	var out []bak
	prefix := base + ".bak."
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, _ := e.Info()
		out = append(out, bak{
			Name: e.Name(),
			Size: sizeOrZero(info),
			Time: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	writeJSON(w, map[string]interface{}{"path": rel, "backups": out})
}

func (h *handlers) backupsRestore(w http.ResponseWriter, r *http.Request) {
	unlock, ok := h.acquireWrite(w)
	if !ok {
		return
	}
	defer unlock()
	var req struct {
		Path   string `json:"path"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := h.resolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Validate backup name: must be a sibling of `full`, start with its base
	// + ".bak.", and not contain path separators.
	if strings.ContainsAny(req.Backup, `/\`) {
		http.Error(w, "invalid backup name", http.StatusBadRequest)
		return
	}
	base := filepath.Base(full)
	if !strings.HasPrefix(req.Backup, base+".bak.") {
		http.Error(w, "backup name does not belong to this file", http.StatusBadRequest)
		return
	}
	bakPath := filepath.Join(filepath.Dir(full), req.Backup)
	data, err := os.ReadFile(bakPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = util.BackupBeforeWrite(full)
	if err := writeFileAtomic(full, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "restored"})
}

// ---------------------------------------------------------------------------
// Mod profile config editor. Mods like Expansion, Namalsk, DayZ-Dog write
// their own config files under the server's profile folder. We expose
// recursive tree/read/write just like the general files editor, but rooted
// at the profiles directory so the server doesn't need to be stopped for
// typical cosmetic settings edits.

func (h *handlers) profilesRoot() string {
	p := h.app.Cfg().ProfilesDir
	if p == "" {
		p = "profiles"
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(h.app.ServerDir, p)
	}
	return p
}

func (h *handlers) profilesResolve(rel string) (string, error) {
	root := h.profilesRoot()
	clean := filepath.Clean("/" + rel)
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	full := filepath.Join(root, clean)
	absRoot, _ := filepath.Abs(root)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absRoot) {
		return "", fmt.Errorf("path escapes profiles dir")
	}
	return full, nil
}

// isServerLogArtifact reports whether a profiles-dir filename is a server log
// or crash dump rather than editable config. These are read-only run output
// with their own viewers (Logs, Admin-log), so the config editor hides them.
func isServerLogArtifact(name string) bool {
	n := strings.ToLower(name)
	switch strings.ToLower(filepath.Ext(n)) {
	case ".rpt", ".adm", ".log", ".mdmp", ".dmp", ".bidmp":
		return true
	}
	// A few DayZ artifacts have no tidy extension.
	return strings.HasPrefix(n, "crash_") || strings.Contains(n, "webapilog")
}

func (h *handlers) profilesTree(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.profilesResolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		// Empty profiles dir isn't an error — the server just hasn't been
		// started yet, so return an empty list so the UI can render a hint.
		if os.IsNotExist(err) {
			writeJSON(w, map[string]interface{}{"path": rel, "entries": []interface{}{}})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type node struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	out := []node{}
	for _, e := range entries {
		// The profiles dir is also where the server dumps its run logs (.RPT,
		// .ADM, crash dumps, script/webapi logs). Those have their own reader
		// under Logs and Admin-log; showing them here only buries the mod config
		// files an admin actually came to edit. Hide them from the tree.
		if !e.IsDir() && isServerLogArtifact(e.Name()) {
			continue
		}
		info, _ := e.Info()
		child := filepath.ToSlash(filepath.Join(rel, e.Name()))
		out = append(out, node{Name: e.Name(), Path: child, IsDir: e.IsDir(), Size: sizeOrZero(info)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, map[string]interface{}{"path": rel, "entries": out})
}

func (h *handlers) profilesRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, err := h.profilesResolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"path": rel, "content": string(body)})
}

func (h *handlers) profilesWrite(w http.ResponseWriter, r *http.Request) {
	// Note: profiles edits do NOT require the server to be stopped — most
	// are read on server startup but rewritten by the mod itself at runtime,
	// so the race here is no worse than the mod's own write patterns.
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full, err := h.profilesResolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = util.BackupBeforeWrite(full)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// ---------------------------------------------------------------------------

func (h *handlers) missionTemplate() (string, error) {
	cfgPath := filepath.Join(h.app.ServerDir, h.app.Cfg().ServerCfg)
	cfg, err := config.LoadServerCfg(cfgPath)
	if err != nil {
		return "", fmt.Errorf("read server.cfg: %w", err)
	}
	t := cfg.MissionTemplate()
	if t == "" {
		return "", dztypes.ErrNoMission
	}
	return t, nil
}

// resolve cleans rel and ensures it stays within serverDir.
func (h *handlers) resolve(rel string) (string, error) {
	clean := filepath.Clean("/" + rel) // absolute-style clean
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	full := filepath.Join(h.app.ServerDir, clean)
	absBase, _ := filepath.Abs(h.app.ServerDir)
	absFull, _ := filepath.Abs(full)
	// Confinement check. The Clean("/"+rel) trick above already collapses any
	// ".." against the root, so `clean` can't escape — but compare with a
	// trailing separator anyway so a sibling like "<serverdir>-secret" can never
	// satisfy a bare prefix match. Allow absFull == absBase (the dir itself).
	if absFull != absBase && !strings.HasPrefix(absFull, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes server dir")
	}
	return full, nil
}

// requireStopped is retained as the simple-path guard for write handlers.
// Most handlers now call acquireWrite (which takes the cross-subsystem lock
// before checking running), so this is kept for read paths that only need
// the running check.
func (h *handlers) requireStopped(w http.ResponseWriter) bool {
	if unlock, ok := h.acquireWrite(w); ok {
		unlock()
		return true
	}
	return false
}

// acquireWrite serializes all state-mutating operations across subsystems
// AND verifies the DayZ server is stopped — in a single atomic step. Without
// this, a POST /api/server/start could race with a concurrent POST
// /api/types/item that had already passed its requireStopped check, leaving
// DayZ to boot against a half-written file.
//
// Caller pattern:
//
//	unlock, ok := h.acquireWrite(w)
//	if !ok { return }
//	defer unlock()
func (h *handlers) acquireWrite(w http.ResponseWriter) (func(), bool) {
	h.app.WriteMu.Lock()
	if h.app.ServerIsRunning() {
		h.app.WriteMu.Unlock()
		// An i18n key, so the page says it in the panel's language.
		authError(w, http.StatusConflict, "guard.serverRunning", map[string]interface{}{"message": "server is running — stop it before editing files"})
		return nil, false
	}
	return h.app.WriteMu.Unlock, true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	// Encode fully before writing: a value that cannot be encoded (an
	// out-of-range time, a NaN) used to produce a 200 with an EMPTY body, which
	// the page could only report as "Unexpected end of JSON input".
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("writeJSON: %v", err)
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(data, '\n'))
}

// writeFileAtomic writes data via a temp file + rename, so a crash or a full
// disk mid-write can never leave a half-written config that DayZ then refuses
// to load. Callers BackupBeforeWrite separately when they want an undo point.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func sizeOrZero(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeOnce(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func mergeNamed(a, b []dztypes.NamedRef) []dztypes.NamedRef {
	seen := map[string]struct{}{}
	for _, x := range a {
		seen[strings.ToLower(x.Name)] = struct{}{}
	}
	for _, x := range b {
		if _, ok := seen[strings.ToLower(x.Name)]; ok {
			continue
		}
		a = append(a, x)
		seen[strings.ToLower(x.Name)] = struct{}{}
	}
	return a
}
