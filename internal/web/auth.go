// Copyright (c) 2026 Aristarh Ucolov.
//
// Optional panel login. Off by default (a panel on 127.0.0.1 needs none); the
// admin turns it on in the first-run wizard or in Settings, which is what makes
// it safe to reach the panel from a phone outside the home network.
//
// Credentials live in their own file (.dayz-manager/auth.json, 0600) rather
// than in manager.json: /api/config round-trips manager.json to the browser
// and decodes arbitrary fields from it, so the hash there could be read or
// overwritten by any page that can call the API. Passwords are stored as
// PBKDF2-HMAC-SHA256 (600k iterations, random salt). Sessions are random
// 256-bit tokens in an HttpOnly, SameSite=Strict cookie; only their SHA-256
// is kept on disk (sessions.json) so a phone stays signed in across manager
// restarts without the file ever holding a usable token.
package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	sessionCookie     = "dzm_session"
	pbkdf2Iterations  = 600_000
	rememberLifetime  = 30 * 24 * time.Hour
	sessionLifetime   = 12 * time.Hour // without "remember me": idle timeout
	localTokenTTL     = 2 * time.Minute
	minPasswordLength = 8
	maxSessions       = 50
)

// authConfig is the persisted login configuration (auth.json).
type authConfig struct {
	Enabled bool   `json:"enabled"`
	User    string `json:"user"`
	Hash    string `json:"hash"` // pbkdf2-sha256$<iterations>$<salt b64>$<key b64>
	// TrustLocalApp lets the window this computer opens (desktop panel, the
	// browser tab the web build opens at start) sign in with a one-time token
	// instead of the password. Remote devices always need the password.
	TrustLocalApp bool  `json:"trustLocalApp"`
	Changed       int64 `json:"changed,omitempty"` // unix seconds of the last credential change
}

// session is one signed-in browser. ID is the SHA-256 of the cookie token —
// the token itself is never stored.
type session struct {
	ID       string `json:"id"`
	User     string `json:"user"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"lastSeen"`
	Expires  int64  `json:"expires"`
	Remember bool   `json:"remember"`
	IP       string `json:"ip"`
	Agent    string `json:"agent"`
}

type loginAttempts struct {
	failures []time.Time
	lockedTo time.Time
}

type authManager struct {
	mu         sync.Mutex
	path       string // auth.json
	sessPath   string // sessions.json
	cfg        authConfig
	sessions   map[string]*session
	dirty      bool // sessions changed since the last flush
	attempts   map[string]*loginAttempts
	globalFail []time.Time
	localToks  map[string]time.Time // sha256(token) → expiry
	log        *log.Logger
	// cookie is the session cookie's name. Browsers do not keep cookies apart
	// by port, so two managers on one machine (two servers, ports 8787 and
	// 8788) would sign each other out with a shared name.
	cookie string
}

// cookieNameFor is the session cookie name of the panel on port.
func cookieNameFor(port int) string {
	if port <= 0 {
		return sessionCookie
	}
	return sessionCookie + "_" + strconv.Itoa(port)
}

func newAuthManager(managerDir string, logger *log.Logger) *authManager {
	m := &authManager{
		path:      filepath.Join(managerDir, "auth.json"),
		sessPath:  filepath.Join(managerDir, "sessions.json"),
		sessions:  map[string]*session{},
		attempts:  map[string]*loginAttempts{},
		localToks: map[string]time.Time{},
		log:       logger,
		cookie:    sessionCookie,
	}
	m.cfg.TrustLocalApp = true
	if data, err := os.ReadFile(m.path); err == nil {
		var c authConfig
		if err := json.Unmarshal(data, &c); err != nil {
			// A corrupt auth file must not silently open the panel: keep login
			// required with no valid password, and say how to recover.
			m.logf("auth.json is unreadable (%v) — login stays required; start the manager with -reset-login to recover", err)
			m.cfg = authConfig{Enabled: true}
		} else {
			m.cfg = c
		}
	}
	if m.cfg.Enabled && (m.cfg.User == "" || m.cfg.Hash == "") {
		m.logf("panel login is enabled but has no credentials — start the manager with -reset-login to recover")
	}
	if data, err := os.ReadFile(m.sessPath); err == nil {
		var list []*session
		if json.Unmarshal(data, &list) == nil {
			now := time.Now().Unix()
			for _, s := range list {
				if s != nil && s.ID != "" && s.Expires > now {
					m.sessions[s.ID] = s
				}
			}
		}
	}
	return m
}

func (m *authManager) logf(format string, args ...interface{}) {
	if m.log != nil {
		m.log.Printf(format, args...)
	}
}

// ResetLogin turns the login off (the -reset-login recovery flag).
func ResetLogin(managerDir string) error {
	m := newAuthManager(managerDir, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Enabled = false
	m.cfg.Hash = ""
	m.sessions = map[string]*session{}
	m.dirty = true
	if err := m.saveConfigLocked(); err != nil {
		return err
	}
	return m.flushLocked()
}

// SetLoginCredentials turns the panel login on with the given name and
// password from the command line (-set-login) — for a server without a
// desktop, where the Settings page cannot be reached before the panel is
// exposed. Every signed-in device is signed out.
func SetLoginCredentials(managerDir, user, password string) error {
	if err := validateCredentials(user, password); err != nil {
		return errors.New(credentialError(err.Error()))
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(managerDir, 0o755); err != nil {
		return err
	}
	m := newAuthManager(managerDir, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	trust := m.cfg.TrustLocalApp || !m.cfg.Enabled
	m.cfg = authConfig{Enabled: true, User: strings.TrimSpace(user), Hash: hash, TrustLocalApp: trust, Changed: time.Now().Unix()}
	m.sessions = map[string]*session{}
	m.dirty = true
	if err := m.saveConfigLocked(); err != nil {
		return err
	}
	return m.flushLocked()
}

// LoginConfigured reports whether the panel login is on for managerDir.
func LoginConfigured(managerDir string) bool {
	return newAuthManager(managerDir, nil).enabled()
}

// credentialError turns validateCredentials' i18n key into English for the
// command line.
func credentialError(key string) string {
	switch key {
	case "auth.err.user":
		return "the user name must be 1-64 characters"
	case "auth.err.short":
		return fmt.Sprintf("the password must be at least %d characters", minPasswordLength)
	case "auth.err.long":
		return "the password is too long"
	}
	return key
}

func (m *authManager) enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Enabled
}

func (m *authManager) snapshot() authConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

func (m *authManager) saveConfigLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(m.path, data)
}

func (m *authManager) flushLocked() error {
	if !m.dirty {
		return nil
	}
	list := make([]*session, 0, len(m.sessions))
	now := time.Now().Unix()
	for _, s := range m.sessions {
		if s.Remember && s.Expires > now { // browser-session logins die with the manager anyway
			list = append(list, s)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created < list[j].Created })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(m.sessPath, data); err != nil {
		return err
	}
	m.dirty = false
	return nil
}

// flush persists session changes (lastSeen updates are batched).
func (m *authManager) flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.flushLocked(); err != nil {
		m.logf("save sessions: %v", err)
	}
}

// writePrivateFile writes data atomically with owner-only permissions.
func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------------------------------------------------------------- passwords

// pbkdf2SHA256 is PBKDF2 (RFC 8018) with HMAC-SHA256 — the standard library of
// Go 1.22 has no KDF, and pulling golang.org/x/crypto in for one function is
// not worth a dependency.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)
	var buf [4]byte
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0], buf[1], buf[2], buf[3] = byte(block>>24), byte(block>>16), byte(block>>8), byte(block)
		prf.Write(buf[:])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)
		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for x := range u {
				t[x] ^= u[x]
			}
		}
	}
	return dk[:keyLen]
}

// hashIterations is a var only so tests can hash quickly; stored hashes carry
// their own count, so verification never depends on it.
var hashIterations = pbkdf2Iterations

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	iter := hashIterations
	key := pbkdf2SHA256([]byte(password), salt, iter, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash keeps a failed lookup as slow as a real check (no user enumeration
// by timing). Computed on first use — 600k iterations are not free at start-up.
var (
	dummyOnce sync.Once
	dummyVal  string
)

func dummyHash() string {
	dummyOnce.Do(func() { dummyVal, _ = hashPassword("dummy-password-for-timing") })
	return dummyVal
}

func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 || iter > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// validateCredentials checks a new username/password pair.
func validateCredentials(user, password string) error {
	user = strings.TrimSpace(user)
	if user == "" || utf8.RuneCountInString(user) > 64 {
		return errors.New("auth.err.user")
	}
	if utf8.RuneCountInString(password) < minPasswordLength {
		return errors.New("auth.err.short")
	}
	if len(password) > 1024 {
		return errors.New("auth.err.long")
	}
	return nil
}

// ---------------------------------------------------------------- sessions

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// clientIP is the TCP peer address. X-Forwarded-For is deliberately ignored:
// it is trivially spoofable, and a reverse proxy in front simply shows up as
// its own address.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.Trim(r.RemoteAddr, "[]")
	}
	return host
}

// newSession signs a browser in and sets the cookie.
func (m *authManager) newSession(w http.ResponseWriter, r *http.Request, user string, remember bool) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	now := time.Now()
	life := sessionLifetime
	if remember {
		life = rememberLifetime
	}
	agent := r.UserAgent()
	if len(agent) > 200 {
		agent = agent[:200]
	}
	s := &session{ID: tokenID(token), User: user, Created: now.Unix(), LastSeen: now.Unix(),
		Expires: now.Add(life).Unix(), Remember: remember, IP: clientIP(r), Agent: agent}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.pruneLocked(now)
	m.dirty = true
	_ = m.flushLocked()
	m.mu.Unlock()
	c := &http.Cookie{Name: m.cookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil}
	if remember {
		c.MaxAge = int(rememberLifetime / time.Second)
	}
	http.SetCookie(w, c)
	return nil
}

// pruneLocked drops expired sessions and caps how many are kept.
func (m *authManager) pruneLocked(now time.Time) {
	for id, s := range m.sessions {
		if s.Expires <= now.Unix() {
			delete(m.sessions, id)
			m.dirty = true
		}
	}
	if len(m.sessions) <= maxSessions {
		return
	}
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].LastSeen < list[j].LastSeen })
	for _, s := range list[:len(list)-maxSessions] {
		delete(m.sessions, s.ID)
	}
	m.dirty = true
}

// sessionFor returns the live session of the request (sliding expiry), or nil.
func (m *authManager) sessionFor(r *http.Request) *session {
	c, err := r.Cookie(m.cookie)
	if err != nil || c.Value == "" {
		return nil
	}
	id := tokenID(c.Value)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil
	}
	if s.Expires <= now.Unix() || !strings.EqualFold(s.User, m.cfg.User) {
		delete(m.sessions, id)
		m.dirty = true
		return nil
	}
	// Sliding expiry; persisted lazily (lastSeen moves at most once a minute).
	if now.Unix()-s.LastSeen >= 60 {
		s.LastSeen = now.Unix()
		life := sessionLifetime
		if s.Remember {
			life = rememberLifetime
		}
		s.Expires = now.Add(life).Unix()
		s.IP = clientIP(r)
		m.dirty = true
	}
	cp := *s
	return &cp
}

func (m *authManager) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: m.cookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
}

// ---------------------------------------------------------------- brute force

// allowAttempt reports whether ip may try a password now; when not, how long
// it must wait. Five failures in 15 minutes lock the address with a doubling
// back-off (30 s … 15 min); a burst of failures from many addresses slows
// everyone down for a minute.
func (m *authManager) allowAttempt(ip string, now time.Time) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.attempts[ip]; a != nil && now.Before(a.lockedTo) {
		return false, a.lockedTo.Sub(now)
	}
	recent := m.globalFail[:0]
	for _, t := range m.globalFail {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	m.globalFail = recent
	if len(recent) >= 60 {
		return false, time.Minute - now.Sub(recent[0])
	}
	return true, 0
}

func (m *authManager) recordFailure(ip string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[ip]
	if a == nil {
		if len(m.attempts) > 10_000 { // bounded memory under a spray
			m.attempts = map[string]*loginAttempts{}
		}
		a = &loginAttempts{}
		m.attempts[ip] = a
	}
	keep := a.failures[:0]
	for _, t := range a.failures {
		if now.Sub(t) < 15*time.Minute {
			keep = append(keep, t)
		}
	}
	a.failures = append(keep, now)
	if n := len(a.failures); n >= 5 {
		backoff := 30 * time.Second << uint(n-5)
		if backoff > 15*time.Minute || backoff <= 0 {
			backoff = 15 * time.Minute
		}
		a.lockedTo = now.Add(backoff)
	}
	m.globalFail = append(m.globalFail, now)
}

func (m *authManager) recordSuccess(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attempts, ip)
}

// ---------------------------------------------------------------- local token

// LocalLoginURL returns the panel URL the manager opens on this computer. With
// login on and "trust this computer's window" set, it carries a one-time token
// in the fragment (never sent in requests, never logged) that the page trades
// for a session — so the admin at the PC is not asked for the password while
// every remote device is.
func (s *Server) LocalLoginURL(base string) string {
	m := s.h.auth
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.Enabled || !m.cfg.TrustLocalApp {
		return base
	}
	token, err := randomToken()
	if err != nil {
		return base
	}
	now := time.Now()
	for id, exp := range m.localToks {
		if now.After(exp) {
			delete(m.localToks, id)
		}
	}
	m.localToks[tokenID(token)] = now.Add(localTokenTTL)
	return strings.TrimSuffix(base, "/") + "/#autologin=" + token
}

func (m *authManager) consumeLocalToken(token string) bool {
	id := tokenID(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.localToks[id]
	delete(m.localToks, id)
	return ok && time.Now().Before(exp) && m.cfg.Enabled && m.cfg.TrustLocalApp
}
