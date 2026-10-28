// Copyright (c) 2026 Aristarh Ucolov.
//
// HTTP side of the panel login (see auth.go) plus the request guard every
// request passes: LAN-only exposure, Host-header and cross-site checks (so a
// web page the admin visits cannot drive the panel even without a password),
// security headers, the HTTPS redirect, and the login gate for /api/.
package web

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// smallBody caps a request body at a few kilobytes: these endpoints take a
// name and a password, and /api/auth/login is reachable without a session —
// the general 64 MB API limit let a stranger fill the log with one request.
func smallBody(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		fn(w, r)
	}
}

func (h *handlers) registerAuth(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/status", methods(h.authStatus, http.MethodGet))
	mux.HandleFunc("/api/auth/login", methods(smallBody(h.authLogin), http.MethodPost))
	mux.HandleFunc("/api/auth/logout", methods(smallBody(h.authLogout), http.MethodPost))
	mux.HandleFunc("/api/auth/local", methods(smallBody(h.authLocal), http.MethodPost))
	mux.HandleFunc("/api/auth/settings", methods(h.authSettings, http.MethodGet))
	mux.HandleFunc("/api/auth/setup", methods(smallBody(h.authSetup), http.MethodPost))
	mux.HandleFunc("/api/auth/change", methods(smallBody(h.authChange), http.MethodPost))
	mux.HandleFunc("/api/auth/disable", methods(smallBody(h.authDisable), http.MethodPost))
	mux.HandleFunc("/api/auth/trust", methods(smallBody(h.authTrust), http.MethodPost))
	mux.HandleFunc("/api/auth/revoke", methods(smallBody(h.authRevoke), http.MethodPost))
}

// authError answers with an i18n key the page translates.
func authError(w http.ResponseWriter, code int, key string, extra map[string]interface{}) {
	body := map[string]interface{}{"error": key}
	for k, v := range extra {
		body[k] = v
	}
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func (h *handlers) authStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.auth.snapshot()
	s := h.auth.sessionFor(r)
	out := map[string]interface{}{"enabled": cfg.Enabled, "authenticated": !cfg.Enabled || s != nil}
	if s != nil {
		out["user"] = s.User
	}
	writeJSON(w, out)
}

// verifySlots bounds concurrent password checks. PBKDF2 is slow on purpose:
// without a bound, a burst of logins all passed allowAttempt before the first
// failure was recorded (so the lockout never engaged) and the parallel hashing
// competed with the DayZ server for the CPU.
var verifySlots = make(chan struct{}, 2)

// logName shortens a user-supplied name before it goes into the log.
func logName(s string) string {
	if utf8.RuneCountInString(s) > 64 {
		return string([]rune(s)[:64]) + "…"
	}
	return s
}

// checkPassword verifies credentials with brute-force protection. It writes
// the error response itself and returns false on failure.
func (h *handlers) checkPassword(w http.ResponseWriter, r *http.Request, user, password string, checkUser bool) bool {
	ip := clientIP(r)
	select {
	case verifySlots <- struct{}{}:
		defer func() { <-verifySlots }()
	case <-r.Context().Done():
		return false
	case <-time.After(10 * time.Second):
		authError(w, http.StatusTooManyRequests, "auth.err.locked", map[string]interface{}{"retryAfter": 10})
		return false
	}
	// Checked after taking a slot, so the failures of the attempts that ran
	// before this one are already counted.
	now := time.Now()
	if ok, wait := h.auth.allowAttempt(ip, now); !ok {
		authError(w, http.StatusTooManyRequests, "auth.err.locked", map[string]interface{}{"retryAfter": int(wait.Seconds()) + 1})
		return false
	}
	cfg := h.auth.snapshot()
	stored := cfg.Hash
	if stored == "" {
		stored = dummyHash()
	}
	passOK := verifyPassword(stored, password) && cfg.Hash != ""
	userOK := !checkUser || strings.EqualFold(strings.TrimSpace(user), cfg.User)
	if !passOK || !userOK {
		h.auth.recordFailure(ip, now)
		h.app.Log.Printf("panel login failed for %q from %s", logName(user), ip)
		time.Sleep(300 * time.Millisecond)
		authError(w, http.StatusUnauthorized, "auth.err.invalid", nil)
		return false
	}
	h.auth.recordSuccess(ip)
	return true
}

func (h *handlers) authLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		authError(w, http.StatusBadRequest, "auth.err.invalid", nil)
		return
	}
	if !h.auth.enabled() {
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	if !h.checkPassword(w, r, req.User, req.Password, true) {
		return
	}
	user := h.auth.snapshot().User
	if err := h.auth.newSession(w, r, user, req.Remember); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.app.Log.Printf("panel login: %s from %s", user, clientIP(r))
	writeJSON(w, map[string]interface{}{"ok": true, "user": user})
}

func (h *handlers) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.auth.cookie); err == nil && c.Value != "" {
		id := tokenID(c.Value)
		h.auth.mu.Lock()
		if _, ok := h.auth.sessions[id]; ok {
			delete(h.auth.sessions, id)
			h.auth.dirty = true
			_ = h.auth.flushLocked()
		}
		h.auth.mu.Unlock()
	}
	h.auth.clearCookie(w, r)
	writeJSON(w, map[string]interface{}{"ok": true})
}

// authLocal trades the one-time token the manager put into the URL it opened
// on this computer for a session. Loopback only.
func (h *handlers) authLocal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !isLoopback(r) || req.Token == "" || !h.auth.consumeLocalToken(req.Token) {
		authError(w, http.StatusUnauthorized, "auth.err.invalid", nil)
		return
	}
	user := h.auth.snapshot().User
	if err := h.auth.newSession(w, r, user, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "user": user})
}

type sessionView struct {
	ID       string `json:"id"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"lastSeen"`
	IP       string `json:"ip"`
	Agent    string `json:"agent"`
	Remember bool   `json:"remember"`
	Current  bool   `json:"current"`
}

func (h *handlers) authSettings(w http.ResponseWriter, r *http.Request) {
	cfg := h.auth.snapshot()
	cur := ""
	if c, err := r.Cookie(h.auth.cookie); err == nil {
		cur = tokenID(c.Value)
	}
	list := []sessionView{}
	h.auth.mu.Lock()
	for _, s := range h.auth.sessions {
		// The public id is a prefix of the hash: enough to revoke, useless to
		// forge a cookie.
		list = append(list, sessionView{ID: s.ID[:16], Created: s.Created, LastSeen: s.LastSeen,
			IP: s.IP, Agent: s.Agent, Remember: s.Remember, Current: s.ID == cur})
	}
	h.auth.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].LastSeen > list[j].LastSeen })
	writeJSON(w, map[string]interface{}{
		"enabled":       cfg.Enabled,
		"user":          cfg.User,
		"trustLocalApp": cfg.TrustLocalApp,
		"sessions":      list,
		"exposure":      h.app.Cfg().Exposure,
	})
}

// enableLogin stores new credentials and signs the caller in.
func (h *handlers) enableLogin(w http.ResponseWriter, r *http.Request, user, password string, trust bool) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	user = strings.TrimSpace(user)
	h.auth.mu.Lock()
	h.auth.cfg = authConfig{Enabled: true, User: user, Hash: hash, TrustLocalApp: trust, Changed: time.Now().Unix()}
	h.auth.sessions = map[string]*session{}
	h.auth.dirty = true
	err = h.auth.saveConfigLocked()
	if err == nil {
		err = h.auth.flushLocked()
	}
	h.auth.mu.Unlock()
	if err != nil {
		return err
	}
	h.app.Log.Printf("panel login enabled for %q", user)
	return h.auth.newSession(w, r, user, true)
}

func (h *handlers) authSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User          string `json:"user"`
		Password      string `json:"password"`
		TrustLocalApp *bool  `json:"trustLocalApp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.auth.enabled() {
		// Changing existing credentials must prove the old password.
		authError(w, http.StatusConflict, "auth.err.alreadyOn", nil)
		return
	}
	if err := validateCredentials(req.User, req.Password); err != nil {
		authError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	trust := true
	if req.TrustLocalApp != nil {
		trust = *req.TrustLocalApp
	}
	if err := h.enableLogin(w, r, req.User, req.Password, trust); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) authChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := h.auth.snapshot()
	if !cfg.Enabled {
		authError(w, http.StatusConflict, "auth.err.off", nil)
		return
	}
	newUser := strings.TrimSpace(req.User)
	if newUser == "" {
		newUser = cfg.User
	}
	newPass := req.Password
	if newPass == "" {
		newPass = req.Current // only the username changes
	}
	if err := validateCredentials(newUser, newPass); err != nil {
		authError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if !h.checkPassword(w, r, "", req.Current, false) {
		return
	}
	// New credentials sign every other device out; this one gets a new session.
	if err := h.enableLogin(w, r, newUser, newPass, cfg.TrustLocalApp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) authDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.auth.enabled() {
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	if h.app.Cfg().Exposure == "internet" {
		authError(w, http.StatusConflict, "auth.err.internetNeedsLogin", nil)
		return
	}
	if !h.checkPassword(w, r, "", req.Current, false) {
		return
	}
	h.auth.mu.Lock()
	h.auth.cfg.Enabled = false
	h.auth.cfg.Hash = ""
	h.auth.cfg.Changed = time.Now().Unix()
	h.auth.sessions = map[string]*session{}
	h.auth.dirty = true
	err := h.auth.saveConfigLocked()
	if err == nil {
		err = h.auth.flushLocked()
	}
	h.auth.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.auth.clearCookie(w, r)
	h.app.Log.Printf("panel login disabled")
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *handlers) authTrust(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TrustLocalApp bool `json:"trustLocalApp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.auth.mu.Lock()
	h.auth.cfg.TrustLocalApp = req.TrustLocalApp
	err := h.auth.saveConfigLocked()
	h.auth.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// authRevoke signs out one device (by the id prefix /api/auth/settings shows)
// or every device except the caller's.
func (h *handlers) authRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Others bool   `json:"others"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cur := ""
	if c, err := r.Cookie(h.auth.cookie); err == nil {
		cur = tokenID(c.Value)
	}
	n := 0
	h.auth.mu.Lock()
	for id := range h.auth.sessions {
		if (req.Others && id != cur) || (req.ID != "" && len(req.ID) >= 8 && strings.HasPrefix(id, req.ID)) {
			delete(h.auth.sessions, id)
			n++
		}
	}
	if n > 0 {
		h.auth.dirty = true
		_ = h.auth.flushLocked()
	}
	h.auth.mu.Unlock()
	writeJSON(w, map[string]interface{}{"ok": true, "revoked": n})
}

// ---------------------------------------------------------------- the guard

// publicAPI lists the /api/ endpoints that work without a session: what the
// login page itself needs, and the loopback-only helpers the launcher uses to
// find and raise a running manager.
func publicAPI(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/info", "/api/i18n", "/api/auth/status", "/api/auth/login", "/api/auth/logout", "/api/auth/local":
		return true
	case "/api/app/show":
		return isLoopback(r)
	}
	return false
}

func setSecurityHeaders(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "same-origin")
	hd.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
		"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; "+
		"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
}

func stateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// crossSite reports a browser request made by another site (CSRF). Requests
// without Origin/Sec-Fetch-Site (curl, the launcher) are not browsers.
func crossSite(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		if o == "null" {
			return true
		}
		u, err := url.Parse(o)
		if err != nil || u.Host == "" {
			return true
		}
		if strings.EqualFold(u.Host, r.Host) {
			return false
		}
		// A reverse proxy on this machine that rewrites Host but forwards the
		// original one.
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && isLoopback(r) && strings.EqualFold(u.Host, fh) {
			return false
		}
		return true
	}
	return r.Header.Get("Sec-Fetch-Site") == "cross-site"
}

var (
	hostNameOnce sync.Once
	hostNameLC   string
)

func machineName() string {
	hostNameOnce.Do(func() {
		if n, err := os.Hostname(); err == nil {
			hostNameLC = strings.ToLower(n)
		}
	})
	return hostNameLC
}

// hostAllowed is the DNS-rebinding defence for a panel WITHOUT a login: a
// foreign web page could point its own domain at 127.0.0.1 / a LAN address and
// then read the API as "same origin". Such requests carry that domain in the
// Host header, so only addresses and local names are accepted. With a login
// the session cookie is bound to the real host, so any Host is fine.
func hostAllowed(host, exposure string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	if h == "" || net.ParseIP(h) != nil || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	switch exposure {
	case "internet":
		return true
	case "lan":
		if !strings.Contains(h, ".") || h == machineName() {
			return true
		}
		for _, suf := range []string{".local", ".lan", ".home", ".internal", ".home.arpa", ".localdomain", ".ts.net"} {
			if strings.HasSuffix(h, suf) {
				return true
			}
		}
		if mn := machineName(); mn != "" && strings.HasPrefix(h, mn+".") {
			return true
		}
	}
	return false
}

// localNets caches this machine's interface networks (for "is this peer on my
// network?" with global IPv6 prefixes, which are not "private" addresses).
var localNets struct {
	sync.Mutex
	nets []*net.IPNet
	at   time.Time
}

func interfaceNets() []*net.IPNet {
	localNets.Lock()
	defer localNets.Unlock()
	if time.Since(localNets.at) < 30*time.Second {
		return localNets.nets
	}
	var out []*net.IPNet
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				out = append(out, n)
			}
		}
	}
	localNets.nets, localNets.at = out, time.Now()
	return out
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isLocalPeer reports whether ip belongs to the local network: loopback,
// private (RFC 1918, fc00::/7), link-local, 100.64/10 (CGNAT — also where
// Tailscale/ZeroTier style VPNs put their peers), or a subnet of one of this
// machine's own interfaces.
func isLocalPeer(ipStr string) bool {
	s := strings.Trim(ipStr, "[]")
	// A link-local IPv6 peer arrives as "fe80::1%eth0"; ParseIP rejects the
	// zone, which used to lock such LAN devices out.
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip) {
		return true
	}
	// Our own global IPv6 prefixes count as the local network (IPv6 LANs use
	// public addresses). Not IPv4: on a rented server the NIC sits in the
	// provider's public subnet, and its neighbours are strangers.
	if ip.To4() != nil {
		return false
	}
	for _, n := range interfaceNets() {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// guard wraps the whole panel.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		exposure := s.app.Exposure()
		authOn := s.h.auth.enabled()

		// Devices outside the local network get in only when the panel is
		// deliberately open to the internet AND protected by a login. Every
		// other case — Local network, Local only started with -bind 0.0.0.0,
		// or "internet" left without a login by -reset-login or a restored
		// backup — is treated as local network only.
		if !isLocalPeer(clientIP(r)) && !(exposure == "internet" && authOn) {
			key := "guard.lanOnly"
			if exposure == "internet" {
				key = "guard.needLogin"
			}
			writeBlocked(w, r, http.StatusForbidden, key)
			return
		}
		// "Local only": the listener moves to loopback when the
		// setting changes, but a connection a phone already had open would
		// still be served — refuse it here (unless -bind chose the address).
		if (exposure == "local" || exposure == "") && !s.bindFixed && !isLoopback(r) {
			writeBlocked(w, r, http.StatusForbidden, "guard.localOnly")
			return
		}
		// Without a login, "internet" is local-network-only (above) — and the
		// Host check must be the LAN one too, or a DNS-rebinding page opened
		// on a LAN device could drive the panel through its browser.
		hostExposure := exposure
		if exposure == "internet" && !authOn {
			hostExposure = "lan"
		}
		if !authOn && !hostAllowed(r.Host, hostExposure) {
			writeBlocked(w, r, http.StatusForbidden, "guard.badHost")
			return
		}
		if stateChanging(r.Method) && crossSite(r) {
			writeBlocked(w, r, http.StatusForbidden, "guard.crossSite")
			return
		}
		// HTTPS on: send other devices to the encrypted address. Loopback keeps
		// plain HTTP (the desktop window and local tools).
		if s.httpsOn.Load() && r.TLS == nil && !isLoopback(r) {
			target := "https://" + r.Host + r.URL.RequestURI()
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			} else {
				writeBlocked(w, r, http.StatusForbidden, "guard.useHttps")
			}
			return
		}
		if authOn && strings.HasPrefix(r.URL.Path, "/api/") && !publicAPI(r) && s.h.auth.sessionFor(r) == nil {
			authError(w, http.StatusUnauthorized, "auth.required", map[string]interface{}{"auth": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guardMessages are shown to a blocked browser (the page itself never loads,
// so they cannot come from the i18n bundle); English + Russian, the key
// travels along for the API.
var guardMessages = map[string][2]string{
	"guard.localOnly": {"This panel is set to be opened on the server computer only. To reach it from other devices, open Settings → Access & security on that computer and choose Local network.",
		"Панель настроена на открытие только на компьютере с сервером. Чтобы открывать её с других устройств, на этом компьютере откройте Настройки → Доступ и безопасность и выберите «Локальная сеть»."},
	"guard.lanOnly": {"This panel only accepts connections from the local network. To reach it from the internet, open Settings → Access & security on the server computer, set a login and choose Internet.",
		"Панель принимает подключения только из локальной сети. Чтобы открыть её из интернета, на компьютере с сервером откройте Настройки → Доступ и безопасность, задайте вход по паролю и выберите «Интернет»."},
	"guard.needLogin": {"This panel is set to be reachable from the internet but has no login, so only devices on the local network may open it. Set a login in Settings → Access & security on the server computer.",
		"Панель настроена на доступ из интернета, но у неё нет входа по паролю, поэтому открыть её можно только из локальной сети. Задайте вход в Настройки → Доступ и безопасность на компьютере с сервером."},
	"guard.badHost": {"This address is not accepted while the panel has no login password. Open the panel by its IP address, or set a login in Settings → Access & security.",
		"Этот адрес не принимается, пока у панели нет пароля. Откройте панель по IP-адресу или задайте вход в Настройки → Доступ и безопасность."},
	"guard.crossSite": {"Request refused: it came from another website. If you use a reverse proxy, make it pass the original Host header (nginx: proxy_set_header Host $host;).",
		"Запрос отклонён: он пришёл с другого сайта. Если вы используете обратный прокси, передавайте исходный заголовок Host (nginx: proxy_set_header Host $host;)."},
	"guard.useHttps": {"This panel requires HTTPS from other devices — open it with https://.",
		"Эта панель требует HTTPS с других устройств — откройте её через https://."},
}

func writeBlocked(w http.ResponseWriter, r *http.Request, code int, key string) {
	msg := guardMessages[key]
	if strings.HasPrefix(r.URL.Path, "/api/") {
		authError(w, code, key, map[string]interface{}{"message": msg[0]})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	page := `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>DayZ Server Manager</title></head><body style="font-family:system-ui,sans-serif;background:#0c0f16;color:#e6e9ef;` +
		`display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:16px;box-sizing:border-box">` +
		`<div style="max-width:560px;line-height:1.5"><h2 style="margin-top:0">DayZ Server Manager</h2><p>` +
		htmlEscape(msg[0]) + `</p><p style="opacity:.75">` + htmlEscape(msg[1]) + `</p></div></body></html>`
	_, _ = w.Write([]byte(page))
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}
