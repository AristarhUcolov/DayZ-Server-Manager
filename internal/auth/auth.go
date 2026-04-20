// Copyright (c) 2026 Aristarh Ucolov.
//
// Panel authentication.
//
// Password hashing: PBKDF2-HMAC-SHA256 with a 16-byte random salt and 120 000
// iterations. Implemented here because we hold a stdlib-only dependency
// policy (crypto/pbkdf2 only landed in Go 1.23; target is 1.22).
//
// Sessions are an in-memory map {token -> expiry}, HttpOnly +
// SameSite=Strict cookie. Panel auth is single-user (admin) — the goal is
// "don't let anyone on the LAN edit types.xml", not a full multi-user ACL.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	pbkdf2Iterations = 120_000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16

	SessionCookieName = "dzm_session"
	sessionTTL        = 8 * time.Hour
)

// ---------------------------------------------------------------------------
// Hashing.

// HashPassword derives a PBKDF2-HMAC-SHA256 hash with a fresh 16-byte salt.
// Returns (hexHash, hexSalt).
func HashPassword(password string) (string, string, error) {
	if password == "" {
		return "", "", errors.New("password must not be empty")
	}
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	dk := pbkdf2([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	return hex.EncodeToString(dk), hex.EncodeToString(salt), nil
}

// VerifyPassword returns true if password+stored salt hashes to stored hash.
// Uses constant-time comparison.
func VerifyPassword(password, hashHex, saltHex string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}
	got := pbkdf2([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2 is an inline PBKDF2-HMAC-SHA256 implementation (RFC 2898).
// Kept local so the package stays stdlib-only.
func pbkdf2(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)

	for block := 1; block <= blocks; block++ {
		// U_1 = PRF(password, salt || INT(block))
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		var be [4]byte
		binary.BigEndian.PutUint32(be[:], uint32(block))
		mac.Write(be[:])
		u := mac.Sum(nil)
		t := make([]byte, hashLen)
		copy(t, u)
		for i := 2; i <= iter; i++ {
			mac := hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

// ---------------------------------------------------------------------------
// Sessions.

type session struct {
	expires time.Time
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func NewStore() *Store { return &Store{sessions: map[string]*session{}} }

// Create returns a fresh session token.
func (s *Store) Create() (string, error) {
	tok, err := randToken(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[tok] = &session{expires: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	return tok, nil
}

// Valid reports whether token is present and unexpired.
func (s *Store) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(sess.expires) {
		delete(s.sessions, token)
		return false
	}
	// sliding expiry on each valid use
	sess.expires = time.Now().Add(sessionTTL)
	return true
}

func (s *Store) Destroy(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *Store) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.sessions {
		if now.After(v.expires) {
			delete(s.sessions, k)
		}
	}
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// Middleware.

// CookieFor builds a session cookie. Secure=false so it works over plain
// HTTP on LAN — operators who terminate TLS upstream can flip the bit.
func CookieFor(token string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	}
}

func ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

// Middleware rejects requests lacking a valid session cookie with 401,
// except for the paths in exempt. Static assets bypass the wrapper entirely
// via routing, so this only guards /api/*.
func Middleware(store *Store, requireAuth func() bool, exempt []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requireAuth() {
				next.ServeHTTP(w, r)
				return
			}
			for _, p := range exempt {
				if r.URL.Path == p || strings.HasPrefix(r.URL.Path, p+"/") {
					next.ServeHTTP(w, r)
					return
				}
			}
			c, err := r.Cookie(SessionCookieName)
			if err != nil || c.Value == "" || !store.Valid(c.Value) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
