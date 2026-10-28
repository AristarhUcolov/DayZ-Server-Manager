// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/rcon"
)

func TestPBKDF2Vectors(t *testing.T) {
	// PBKDF2-HMAC-SHA256 test vectors (RFC 7914 §11 style, widely published).
	cases := []struct {
		iter int
		want string
	}{
		{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(pbkdf2SHA256([]byte("password"), []byte("salt"), c.iter, 32))
		if got != c.want {
			t.Errorf("iter %d: got %s want %s", c.iter, got, c.want)
		}
	}
}

func TestHashAndVerify(t *testing.T) {
	defer func(n int) { hashIterations = n }(hashIterations)
	hashIterations = 1000
	h, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(h, "correct horse") {
		t.Fatal("right password rejected")
	}
	for _, bad := range []string{"", "correct hors", "Correct horse"} {
		if verifyPassword(h, bad) {
			t.Fatalf("wrong password %q accepted", bad)
		}
	}
	for _, broken := range []string{"", "plain", "pbkdf2-sha256$x$y$z", "pbkdf2-sha256$10$AAAA$AAAA", "md5$1000$AAAA$AAAA"} {
		if verifyPassword(broken, "x") {
			t.Fatalf("malformed hash %q accepted", broken)
		}
	}
}

// testServer builds the guard + auth endpoints around a stub protected API.
func testServer(t *testing.T, exposure string) (*Server, http.Handler) {
	t.Helper()
	hashIterations = 1000
	t.Cleanup(func() { hashIterations = pbkdf2Iterations })
	a := &app.App{Config: &config.Manager{Exposure: exposure}, ManagerDir: t.TempDir(),
		Log: log.New(io.Discard, "", 0), RCon: rcon.NewManager()}
	h := &handlers{app: a, auth: newAuthManager(a.ManagerDir, a.Log), panelPort: 8787,
		upnp: &upnpState{}, upnpKick: make(chan struct{}, 1)}
	s := &Server{app: a, h: h, port: 8787}
	h.server = s
	mux := http.NewServeMux()
	h.registerAuth(mux)
	mux.HandleFunc("/api/info", h.info)
	mux.HandleFunc("/api/secret", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("page")) })
	return s, s.guard(mux)
}

func do(hd http.Handler, method, path, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	hd.ServeHTTP(w, r)
	return w
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) {
		if c != nil {
			r.AddCookie(c)
		}
	}
}

func sessionCookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestLoginFlow(t *testing.T) {
	_, hd := testServer(t, "lan")
	// No login configured: the API is open.
	if w := do(hd, "GET", "/api/secret", "", nil); w.Code != 200 {
		t.Fatalf("open panel: %d", w.Code)
	}
	// Turn the login on (the caller is signed in by the setup call).
	w := do(hd, "POST", "/api/auth/setup", `{"user":"admin","password":"hunter22!"}`, nil)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	setupCookie := sessionCookieOf(w)
	if setupCookie == nil || !setupCookie.HttpOnly || setupCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("setup cookie missing or not HttpOnly/Strict: %+v", setupCookie)
	}
	if w := do(hd, "GET", "/api/secret", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d, want 401", w.Code)
	}
	if w := do(hd, "GET", "/api/secret", "", withCookie(setupCookie)); w.Code != 200 {
		t.Fatalf("setup session rejected: %d", w.Code)
	}
	// Static page and public endpoints stay reachable for the login screen.
	if w := do(hd, "GET", "/", "", nil); w.Code != 200 {
		t.Fatalf("page: %d", w.Code)
	}
	if w := do(hd, "GET", "/api/auth/status", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"authenticated":false`) {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	// Wrong password, wrong user, then the right one (user is case-insensitive).
	if w := do(hd, "POST", "/api/auth/login", `{"user":"admin","password":"nope-nope"}`, nil); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := do(hd, "POST", "/api/auth/login", `{"user":"root","password":"hunter22!"}`, nil); w.Code != 401 {
		t.Fatalf("wrong user: %d", w.Code)
	}
	w = do(hd, "POST", "/api/auth/login", `{"user":"ADMIN","password":"hunter22!","remember":true}`, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	c := sessionCookieOf(w)
	if c == nil || c.MaxAge <= 0 {
		t.Fatalf("remember-me cookie should be persistent: %+v", c)
	}
	if w := do(hd, "GET", "/api/secret", "", withCookie(c)); w.Code != 200 {
		t.Fatalf("session rejected: %d", w.Code)
	}
	// Logout kills the session server-side.
	do(hd, "POST", "/api/auth/logout", "", withCookie(c))
	if w := do(hd, "GET", "/api/secret", "", withCookie(c)); w.Code != 401 {
		t.Fatalf("after logout: %d", w.Code)
	}
	// Changing the password signs other devices out.
	if w := do(hd, "POST", "/api/auth/change", `{"current":"hunter22!","password":"new-password-1"}`, withCookie(setupCookie)); w.Code != 200 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	if w := do(hd, "GET", "/api/secret", "", withCookie(setupCookie)); w.Code != 401 {
		t.Fatalf("old session survived a password change: %d", w.Code)
	}
}

func TestLoginLockout(t *testing.T) {
	s, hd := testServer(t, "lan")
	if err := func() error {
		w := httptest.NewRecorder()
		return s.h.enableLogin(w, httptest.NewRequest("POST", "/", nil), "admin", "hunter22!", true)
	}(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if w := do(hd, "POST", "/api/auth/login", `{"user":"admin","password":"wrong-pass"}`, nil); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := do(hd, "POST", "/api/auth/login", `{"user":"admin","password":"hunter22!"}`, nil)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "retryAfter") {
		t.Fatalf("locked address got %d %s, want 429", w.Code, w.Body)
	}
	// Another address is not affected.
	w = do(hd, "POST", "/api/auth/login", `{"user":"admin","password":"hunter22!"}`, func(r *http.Request) { r.RemoteAddr = "192.168.1.20:4000" })
	if w.Code != 200 {
		t.Fatalf("other address: %d", w.Code)
	}
}

func TestGuardCrossSiteAndHost(t *testing.T) {
	_, hd := testServer(t, "local")
	evil := func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }
	if w := do(hd, "POST", "/api/secret", "{}", evil); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d, want 403", w.Code)
	}
	same := func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:8787") }
	if w := do(hd, "POST", "/api/secret", "{}", same); w.Code != 200 {
		t.Fatalf("same-origin POST: %d", w.Code)
	}
	if w := do(hd, "POST", "/api/secret", "{}", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); w.Code != 403 {
		t.Fatalf("Sec-Fetch-Site cross-site: %d", w.Code)
	}
	if w := do(hd, "POST", "/api/secret", "{}", nil); w.Code != 200 {
		t.Fatalf("non-browser POST: %d", w.Code)
	}
	// DNS rebinding: a foreign name pointing at 127.0.0.1.
	rebind := func(r *http.Request) { r.Host = "attacker.example:8787" }
	if w := do(hd, "GET", "/api/secret", "", rebind); w.Code != http.StatusForbidden {
		t.Fatalf("rebinding host: %d, want 403", w.Code)
	}
	if w := do(hd, "GET", "/api/secret", "", func(r *http.Request) { r.Host = "localhost:8787" }); w.Code != 200 {
		t.Fatalf("localhost host: %d", w.Code)
	}
}

func TestGuardLANOnly(t *testing.T) {
	_, hd := testServer(t, "lan")
	for addr, want := range map[string]int{
		"8.8.8.8:1000":     http.StatusForbidden,
		"192.168.1.5:1000": 200,
		"10.0.0.7:1000":    200,
		"100.101.2.3:1000": 200, // Tailscale / CGNAT range
		"[fd12::1]:1000":   200,
		"127.0.0.1:1000":   200,
	} {
		addr := addr
		w := do(hd, "GET", "/api/secret", "", func(r *http.Request) { r.RemoteAddr = addr })
		if w.Code != want {
			t.Errorf("%s: %d, want %d", addr, w.Code, want)
		}
	}
	// LAN names are accepted without a login; public DNS names are not.
	if w := do(hd, "GET", "/api/secret", "", func(r *http.Request) { r.Host = "gaming-pc.local:8787" }); w.Code != 200 {
		t.Errorf(".local host: %d", w.Code)
	}
	if w := do(hd, "GET", "/api/secret", "", func(r *http.Request) { r.Host = "example.com" }); w.Code != 403 {
		t.Errorf("public host without login: %d", w.Code)
	}
}

func TestLocalLoginToken(t *testing.T) {
	s, hd := testServer(t, "local")
	if u := s.LocalLoginURL("http://127.0.0.1:8787/"); u != "http://127.0.0.1:8787/" {
		t.Fatalf("token minted without a login: %s", u)
	}
	if err := s.h.enableLogin(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "admin", "hunter22!", true); err != nil {
		t.Fatal(err)
	}
	u := s.LocalLoginURL("http://127.0.0.1:8787/")
	i := strings.Index(u, "#autologin=")
	if i < 0 {
		t.Fatalf("no token in %s", u)
	}
	tok := u[i+len("#autologin="):]
	remote := func(r *http.Request) { r.RemoteAddr = "192.168.1.9:5000" }
	// Refused either way: with "Local only" the guard already turns
	// another machine away (403); the endpoint's own loopback check says 401.
	if w := do(hd, "POST", "/api/auth/local", `{"token":"`+tok+`"}`, remote); w.Code != 401 && w.Code != 403 {
		t.Fatalf("token accepted from another machine: %d", w.Code)
	}
	// The remote attempt must not have burned... it did consume nothing: the
	// loopback check runs first.
	w := do(hd, "POST", "/api/auth/local", `{"token":"`+tok+`"}`, nil)
	if w.Code != 200 || sessionCookieOf(w) == nil {
		t.Fatalf("local token: %d %s", w.Code, w.Body)
	}
	if w := do(hd, "POST", "/api/auth/local", `{"token":"`+tok+`"}`, nil); w.Code != 401 {
		t.Fatalf("token reused: %d", w.Code)
	}
	// Trust off: no token in the URL.
	s.h.auth.mu.Lock()
	s.h.auth.cfg.TrustLocalApp = false
	s.h.auth.mu.Unlock()
	if u := s.LocalLoginURL("http://127.0.0.1:8787/"); strings.Contains(u, "autologin") {
		t.Fatalf("token minted with trust off: %s", u)
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	s, hd := testServer(t, "local")
	if err := s.h.enableLogin(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "admin", "hunter22!", true); err != nil {
		t.Fatal(err)
	}
	w := do(hd, "POST", "/api/auth/login", `{"user":"admin","password":"hunter22!","remember":true}`, nil)
	c := sessionCookieOf(w)
	if c == nil {
		t.Fatal("no cookie")
	}
	// A new manager process reading the same directory.
	m2 := newAuthManager(s.app.ManagerDir, nil)
	r := httptest.NewRequest("GET", "/api/secret", nil)
	r.AddCookie(c)
	if m2.sessionFor(r) == nil {
		t.Fatal("remembered session lost across restart")
	}
	if !m2.enabled() {
		t.Fatal("login config lost across restart")
	}
	if err := ResetLogin(s.app.ManagerDir); err != nil {
		t.Fatal(err)
	}
	if m3 := newAuthManager(s.app.ManagerDir, nil); m3.enabled() || m3.sessionFor(r) != nil {
		t.Fatal("-reset-login did not turn the login off")
	}
}

// "Local only" refuses other devices even on a connection that was
// opened before the switch from Local network (the listener has moved, an
// open keep-alive connection has not).
func TestLocalOnlyRefusesOtherDevices(t *testing.T) {
	_, hd := testServer(t, "local")
	lan := func(r *http.Request) { r.RemoteAddr = "192.168.1.9:5000" }
	if w := do(hd, "GET", "/api/secret", "", lan); w.Code != 403 || !strings.Contains(w.Body.String(), "guard.localOnly") {
		t.Fatalf("LAN device on a local-only panel: %d %s", w.Code, w.Body)
	}
	if w := do(hd, "GET", "/api/secret", "", nil); w.Code != 200 {
		t.Fatalf("this computer: %d", w.Code)
	}
}
