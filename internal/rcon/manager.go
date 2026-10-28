// Copyright (c) 2026 Aristarh Ucolov.
//
// High-level RCon manager used by the web handlers. Maintains a single
// connection, reconnects on demand, and parses the stock `players` reply.
package rcon

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Player struct {
	ID    int    `json:"id"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Ping  int    `json:"ping"`
	GUID  string `json:"guid"`
	Name  string `json:"name"`
	Lobby bool   `json:"lobby"`
}

type Manager struct {
	mu   sync.Mutex
	conn *Conn
	host string
	port int
	pass string

	// cmdMu serializes whole commands so two callers (e.g. the dashboard poll
	// and the RCon page) can never run on the connection concurrently. Without
	// this, an abandoned/slow command could Close the socket while another was
	// mid-flight, triggering a reconnect cascade — visible as a new "RCon admin
	// logged in" in the BE log every few seconds, plus needless load.
	cmdMu sync.Mutex

	// Player-list cache shared by the dashboard and the RCon page so neither
	// opens its own connection per poll.
	cacheMu    sync.Mutex
	cache      []Player
	cacheAt    time.Time
	cacheErr   error
	refreshing bool

	// Live chat ring buffer, fed by the BattlEye message stream (OnMessage)
	// and by messages the manager sends (recordSent). chatSink receives every
	// new message for the persistent archive; sentEcho dedupes BattlEye's
	// relay of our own `say`.
	chatMu    sync.Mutex
	chat      []ChatMsg
	chatSink  func(ChatMsg)
	eventSink func(ServerEvent) // player connects/disconnects (events.go)
	sentEcho  []sentEcho

	// Throttle for TouchConnect's background dial (guarded by mu).
	touching bool
	touchAt  time.Time

	// creds, when set, supplies the login at every dial (and, throttled, at
	// every Configured check). BattlEye's live password can differ from the
	// one in the panel or in beserver_x64.cfg — BattlEye renames that file to
	// beserver_x64_active_*.cfg at start and keeps what it loaded until the
	// next restart — so the candidates are re-read instead of cached.
	creds   func() Creds
	credsAt time.Time
	cands   []string // passwords to try, the one that last worked first

	// A rejected login is not retried with the same candidates for a while:
	// every poll would otherwise log another failed admin login on the server.
	authFailKey string
	authFailAt  time.Time
	connErr     error // the last dial's failure, nil once connected
}

// LastConnectErr is why the last connection attempt failed (nil when it
// succeeded or none was made) — the panel shows it instead of a bare
// "not connected".
func (m *Manager) LastConnectErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.liveConn() != nil {
		return nil
	}
	return m.connErr
}

// Creds is what the manager logs in with. Passwords are tried in order until
// BattlEye accepts one.
type Creds struct {
	Host      string
	Port      int
	Passwords []string
}

var (
	// ErrNotConfigured: there is no password to log in with.
	ErrNotConfigured = errors.New("rcon: not configured — set RConPassword in battleye/beserver_x64.cfg (or override in Settings)")
	// ErrPasswordMismatch wraps ErrAuthFailed when BattlEye rejected every
	// password the manager knows.
	ErrPasswordMismatch = fmt.Errorf("%w: BattlEye is running with another password (it loads beserver_x64.cfg only when the server starts — restart the server)", ErrAuthFailed)
)

// authRetryAfter is how long a rejected set of passwords rests before the
// next attempt (any change to the candidates retries at once).
const authRetryAfter = 20 * time.Second

// SetCredentials makes the manager ask fn for the login whenever it needs it.
func (m *Manager) SetCredentials(fn func() Creds) {
	m.mu.Lock()
	m.creds = fn
	m.refreshLocked(true)
	m.mu.Unlock()
}

// Reload re-reads the credentials now (after a config change, server start or
// stop) and drops a connection they no longer cover.
func (m *Manager) Reload() {
	m.mu.Lock()
	m.refreshLocked(true)
	m.mu.Unlock()
}

// refreshLocked re-reads the credentials from the provider (at most every few
// seconds unless force) and drops a connection made with ones that are gone.
// Caller holds m.mu.
func (m *Manager) refreshLocked(force bool) {
	if m.creds == nil || (!force && time.Since(m.credsAt) < 3*time.Second) {
		return
	}
	m.credsAt = time.Now()
	c := m.creds()
	var cands []string
	seen := map[string]bool{}
	for _, p := range c.Passwords {
		if p != "" && !seen[p] {
			seen[p] = true
			cands = append(cands, p)
		}
	}
	// The password that worked last goes first: no failed login on reconnect.
	if seen[m.pass] {
		cands = moveFront(cands, m.pass)
	}
	stale := c.Host != m.host || c.Port != m.port || (m.pass != "" && !seen[m.pass])
	m.host, m.port, m.cands = c.Host, c.Port, cands
	if !seen[m.pass] {
		m.pass = ""
		if len(cands) > 0 {
			m.pass = cands[0]
		}
	}
	if stale && m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

func moveFront(list []string, v string) []string {
	out := []string{v}
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func NewManager() *Manager { return &Manager{} }

// Configure stores connection params. Does not open the socket.
func (m *Manager) Configure(host string, port int, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.host == host && m.port == port && m.pass == password {
		return
	}
	m.host, m.port, m.pass = host, port, password
	m.cands = nil
	if password != "" {
		m.cands = []string{password}
	}
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// Connect opens (or reuses) the connection. Cheap if already open.
func (m *Manager) Connect() error {
	_, err := m.connect()
	return err
}

// liveConn returns the open connection, or nil. A Conn that closed itself
// (keepAlive's stale check after a server stop/restart) is dropped here so it
// is never reported as connected or reused. Caller holds m.mu.
func (m *Manager) liveConn() *Conn {
	if m.conn != nil && m.conn.closed.Load() {
		m.conn = nil
	}
	return m.conn
}

// connect returns the open connection, dialing a new one if needed. m.mu is
// NOT held across Dial: a dial to a server that is still booting waits the
// full read timeout, and holding the lock would stall every Configured /
// IsConnected caller (dashboard, chat poll) for that long.
func (m *Manager) connect() (*Conn, error) {
	m.mu.Lock()
	if c := m.liveConn(); c != nil {
		m.mu.Unlock()
		return c, nil
	}
	m.refreshLocked(true)
	host, port := m.host, m.port
	cands := append([]string(nil), m.cands...)
	key := fmt.Sprintf("%s:%d\x00%s", host, port, strings.Join(cands, "\x00"))
	if m.authFailKey == key && time.Since(m.authFailAt) < authRetryAfter {
		m.mu.Unlock()
		return nil, ErrPasswordMismatch
	}
	if host == "" || port == 0 || len(cands) == 0 {
		m.connErr = ErrNotConfigured
		m.mu.Unlock()
		return nil, ErrNotConfigured
	}
	m.mu.Unlock()
	// Capture the live BattlEye message stream (chat + kill/connect lines) into
	// the chat ring buffer. Wired on every (re)connect since dial makes a fresh
	// Conn each time. See chat.go.
	addr := fmt.Sprintf("%s:%d", host, port)
	var c *Conn
	var err error
	used := ""
	for _, pw := range cands {
		if c, err = dial(addr, pw, m.onServerMessage); err == nil {
			used = pw
			break
		}
		if !errors.Is(err, ErrAuthFailed) {
			break // no answer at all — another password won't change that
		}
	}
	if err != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		if errors.Is(err, ErrAuthFailed) {
			m.authFailKey, m.authFailAt = key, time.Now()
			err = ErrPasswordMismatch
		}
		m.connErr = err
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.host != host || m.port != port || !contains(m.cands, used) {
		_ = c.Close()
		return nil, fmt.Errorf("rcon: configuration changed during connect")
	}
	if cur := m.liveConn(); cur != nil {
		_ = c.Close() // a concurrent dial (TouchConnect vs a command) won
		return cur, nil
	}
	m.pass = used
	m.cands = moveFront(m.cands, used)
	m.authFailKey, m.connErr = "", nil
	m.conn = c
	return c, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Configured reports whether host/port/password are set (RCon can be attempted).
func (m *Manager) Configured() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshLocked(false)
	return m.host != "" && m.port != 0 && len(m.cands) > 0
}

// LastPlayersErr returns the error from the most recent player-list refresh (nil
// when the last refresh succeeded). Lets the dashboard tell "RCon not connected"
// apart from a genuinely empty server.
func (m *Manager) LastPlayersErr() error {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	return m.cacheErr
}

// IsConnected reports whether the RCon socket is currently open.
func (m *Manager) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveConn() != nil
}

// TouchConnect triggers a throttled, non-blocking connection attempt so the live
// BattlEye message stream (chat) stays open while a client is watching the Chat
// page — without ever blocking the caller on a 5 s dial timeout when the server
// is down. A no-op if already connected, unconfigured, or attempted recently.
func (m *Manager) TouchConnect() {
	m.mu.Lock()
	m.refreshLocked(false)
	if m.liveConn() != nil || m.host == "" || m.port == 0 || len(m.cands) == 0 ||
		m.touching || time.Since(m.touchAt) < 5*time.Second {
		m.mu.Unlock()
		return
	}
	m.touching = true
	m.mu.Unlock()
	go func() {
		defer guard("connect", nil)
		defer func() {
			m.mu.Lock()
			m.touching, m.touchAt = false, time.Now()
			m.mu.Unlock()
		}()
		_ = m.Connect()
	}()
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// Command sends a raw RCon command, reconnecting once on transport errors.
// Fully serialized: only one command runs at a time on the single connection.
func (m *Manager) Command(cmd string) (string, error) {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()

	c, err := m.connect()
	if err != nil {
		return "", err
	}
	out, err := c.Command(cmd)
	if err == nil {
		return out, nil
	}
	// one retry on a fresh connection.
	m.dropConn(c)
	if c, err = m.connect(); err != nil {
		return "", err
	}
	return c.Command(cmd)
}

// dropConn closes c and forgets it, unless the manager has already moved on
// to another connection (Configure/Close ran meanwhile).
func (m *Manager) dropConn(c *Conn) {
	_ = c.Close()
	m.mu.Lock()
	if m.conn == c {
		m.conn = nil
	}
	m.mu.Unlock()
}

func (m *Manager) Players() ([]Player, error) {
	raw, err := m.Command("players")
	if err != nil {
		return nil, err
	}
	return parsePlayers(raw), nil
}

// PlayersFresh returns the cached player list immediately (never blocks) and
// kicks off a single background refresh when the cache is older than maxAge.
// Used by the dashboard so a slow/unreachable RCon can never stall the page.
func (m *Manager) PlayersFresh(maxAge time.Duration) []Player {
	m.cacheMu.Lock()
	cached := m.cache
	stale := time.Since(m.cacheAt) > maxAge
	start := stale && !m.refreshing
	if start {
		m.refreshing = true
	}
	m.cacheMu.Unlock()

	if start {
		go func() {
			defer guard("player list", func() {
				m.cacheMu.Lock()
				m.refreshing = false
				m.cacheMu.Unlock()
			})
			p, err := m.Players()
			m.cacheMu.Lock()
			if err == nil {
				m.cache = p
			}
			m.cacheAt, m.cacheErr, m.refreshing = time.Now(), err, false
			m.cacheMu.Unlock()
		}()
	}
	return cached
}

// InvalidatePlayers marks the player cache stale so the next read refetches.
// Called after a kick/ban so the change shows up immediately.
func (m *Manager) InvalidatePlayers() {
	m.cacheMu.Lock()
	m.cacheAt = time.Time{}
	m.cacheMu.Unlock()
}

// PlayersCached returns the cached list if it's younger than maxAge; otherwise
// it fetches live (serialized), updates the cache, and returns the result. Used
// by the RCon page, which wants accurate data and can tolerate a brief wait.
func (m *Manager) PlayersCached(maxAge time.Duration) ([]Player, error) {
	m.cacheMu.Lock()
	if time.Since(m.cacheAt) <= maxAge && m.cacheErr == nil && m.cache != nil {
		p := m.cache
		m.cacheMu.Unlock()
		return p, nil
	}
	m.cacheMu.Unlock()

	p, err := m.Players()
	m.cacheMu.Lock()
	if err == nil {
		m.cache = p
	}
	m.cacheAt, m.cacheErr = time.Now(), err
	m.cacheMu.Unlock()
	return p, err
}

// Say broadcasts to every player. Delivered messages are recorded in the chat
// feed/archive as sent by the manager (scheduled announcements and restart
// warnings go through here too, so the archive shows what players saw).
func (m *Manager) Say(msg string) error {
	msg = sanitizeSay(msg)
	if msg == "" {
		return fmt.Errorf("empty message")
	}
	m.expectEcho(msg) // before sending: the relay can beat Command's return
	if _, err := m.Command("say -1 " + msg); err != nil {
		m.forgetEcho(msg)
		return err
	}
	m.recordSent("Global", "", msg)
	return nil
}

// SayTo sends a private message to one player (by RCon player number).
func (m *Manager) SayTo(playerID int, msg string) error {
	msg = sanitizeSay(msg)
	if msg == "" {
		return fmt.Errorf("empty message")
	}
	m.expectEcho(msg)
	if _, err := m.Command(fmt.Sprintf("say %d %s", playerID, msg)); err != nil {
		m.forgetEcho(msg)
		return err
	}
	m.recordSent("Direct", m.playerName(playerID), msg)
	return nil
}

// playerName looks a player number up in the cached player list.
func (m *Manager) playerName(id int) string {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	for _, p := range m.cache {
		if p.ID == id {
			return p.Name
		}
	}
	return fmt.Sprintf("#%d", id)
}

func (m *Manager) Kick(playerID int, reason string) error {
	cmd := fmt.Sprintf("kick %d", playerID)
	if reason != "" {
		cmd += " " + reason
	}
	_, err := m.Command(cmd)
	return err
}

// Ban uses GUID-ban when possible, falls back to ID.
func (m *Manager) Ban(playerID int, minutes int, reason string) error {
	cmd := fmt.Sprintf("ban %d %d %s", playerID, minutes, reason)
	_, err := m.Command(cmd)
	return err
}

// Shutdown asks the server to save and exit (#shutdown). The server often goes
// down before it acknowledges, so a missing reply counts as sent — and the
// command is never retried (Command's retry would fire it twice).
func (m *Manager) Shutdown() error {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()
	c, err := m.connect()
	if err != nil {
		return err
	}
	if _, err := c.Command("#shutdown"); err != nil && !errors.Is(err, ErrTimeout) {
		return err
	}
	return nil
}

// Lock stops (true) or re-allows (false) players joining (#lock / #unlock).
func (m *Manager) Lock(lock bool) error {
	cmd := "#lock"
	if !lock {
		cmd = "#unlock"
	}
	_, err := m.Command(cmd)
	return err
}

// KickAll disconnects every player currently online with reason, returning how
// many were kicked. Used by the pre-restart window.
func (m *Manager) KickAll(reason string) (int, error) {
	ps, err := m.Players()
	if err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, p := range ps {
		if err := m.Kick(p.ID, reason); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	m.InvalidatePlayers()
	return n, firstErr
}

// parsePlayers parses the BE `players` output:
//
//	Players on server:
//	[#] [IP Address]:[Port] [Ping] [GUID] [Name]
//	--------------------------------------------------
//	0   203.0.113.42:2304    43   4a9f...(OK) MyNick
//	1   198.51.100.17:2304   -1   ...         AnotherGuy (Lobby)
//	(2 players in total)
func parsePlayers(raw string) []Player {
	var out []Player
	sc := bufio.NewScanner(strings.NewReader(raw))
	inTable := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "---") {
			inTable = true
			continue
		}
		if !inTable || strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "(") {
			break // "(N players in total)"
		}
		// Trim leading whitespace, split by whitespace (max 5 fields).
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		p := Player{}
		fmt.Sscanf(fields[0], "%d", &p.ID)
		if colon := strings.LastIndex(fields[1], ":"); colon != -1 {
			p.IP = fields[1][:colon]
			fmt.Sscanf(fields[1][colon+1:], "%d", &p.Port)
		}
		fmt.Sscanf(fields[2], "%d", &p.Ping)
		p.GUID = strings.TrimSuffix(strings.TrimSuffix(fields[3], "(OK)"), "(?)")
		name := strings.Join(fields[4:], " ")
		if strings.HasSuffix(name, "(Lobby)") {
			p.Lobby = true
			name = strings.TrimSpace(strings.TrimSuffix(name, "(Lobby)"))
		}
		p.Name = name
		out = append(out, p)
	}
	return out
}
