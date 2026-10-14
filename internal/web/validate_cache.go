// Copyright (c) 2026 Aristarh Ucolov.
//
// The Validator page and the Health tile each ran a full validation on every
// visit — seconds on a near-empty server, much longer on a big modded mission.
// The last result is kept per server and reused while nothing it reads has
// changed: the fingerprint covers the mission, mod configs, server.cfg, mod
// folders, keys and the launch settings. The page can also show the last
// result at once (peek) and refresh it behind.
package web

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/validator"
)

// validateMaxAge bounds how long a result is reused even when the fingerprint
// matches: some inputs (a mod's own keys, Linux case checks inside mod
// folders) are not part of it.
const validateMaxAge = 15 * time.Minute

type validateResult struct {
	issues []validator.Issue
	at     time.Time
	print  string
}

var validateCache = struct {
	mu   sync.Mutex
	last map[string]*validateResult // server dir → last run
	runs map[string]*sync.Mutex     // server dir → one run at a time
}{last: map[string]*validateResult{}, runs: map[string]*sync.Mutex{}}

// validateFingerprint hashes what a validation reads: the options and the
// size+mtime of the config files involved. Persistence (storage_*) and logs
// are left out — they change all the time and are never validated.
func validateFingerprint(o validator.Options) string {
	sum := sha1.New()
	opts, _ := json.Marshal(o)
	sum.Write(opts)
	stamp := func(p string, info os.FileInfo) {
		fmt.Fprintf(sum, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
	}
	walk := func(root string, maxDepth int, exts ...string) {
		if root == "" {
			return
		}
		base := strings.Count(filepath.Clean(root), string(filepath.Separator))
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				low := strings.ToLower(info.Name())
				if p != root && (strings.HasPrefix(low, "storage_") || strings.HasPrefix(low, ".") || low == "logs" ||
					strings.Count(filepath.Clean(p), string(filepath.Separator))-base > maxDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			for _, e := range exts {
				if ext == e {
					stamp(p, info)
					break
				}
			}
			return nil
		})
	}
	if o.Mission != "" {
		walk(filepath.Join(o.ServerDir, "mpmissions", o.Mission), 8, ".xml", ".json", ".c")
	}
	walk(o.ProfilesDir, 4, ".json")
	// The server root (server.cfg, @mod folders appearing or going) and keys/.
	for _, d := range []string{o.ServerDir, filepath.Join(o.ServerDir, "keys")} {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if info, err := e.Info(); err == nil && (e.IsDir() || strings.EqualFold(filepath.Ext(e.Name()), ".cfg") ||
				strings.EqualFold(filepath.Ext(e.Name()), ".bikey")) {
				stamp(filepath.Join(d, e.Name()), info)
			}
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// cachedValidation returns the last result for this server and whether it is
// still current. Never runs a validation.
func (h *handlers) cachedValidation(o validator.Options) (*validateResult, bool) {
	validateCache.mu.Lock()
	r := validateCache.last[h.app.ServerDir]
	validateCache.mu.Unlock()
	if r == nil {
		return nil, false
	}
	return r, time.Since(r.at) < validateMaxAge && r.print == validateFingerprint(o)
}

// runValidation validates now (or reuses a current result unless force) and
// stores the result. Concurrent callers for one server wait for a single run.
func (h *handlers) runValidation(o validator.Options, force bool) (*validateResult, bool, error) {
	validateCache.mu.Lock()
	run := validateCache.runs[h.app.ServerDir]
	if run == nil {
		run = &sync.Mutex{}
		validateCache.runs[h.app.ServerDir] = run
	}
	validateCache.mu.Unlock()
	run.Lock()
	defer run.Unlock()
	if !force {
		if r, fresh := h.cachedValidation(o); fresh {
			return r, true, nil
		}
	}
	fp := validateFingerprint(o)
	issues, err := validator.ValidateWith(o)
	if err != nil {
		return nil, false, err
	}
	if issues == nil {
		issues = []validator.Issue{}
	}
	for i := range issues {
		if rel, err := filepath.Rel(h.app.ServerDir, issues[i].File); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			issues[i].Rel = filepath.ToSlash(rel)
		}
	}
	r := &validateResult{issues: issues, at: time.Now(), print: fp}
	validateCache.mu.Lock()
	validateCache.last[h.app.ServerDir] = r
	validateCache.mu.Unlock()
	return r, false, nil
}

// forgetValidation drops the cached result, e.g. after an auto-fix.
func (h *handlers) forgetValidation() {
	validateCache.mu.Lock()
	delete(validateCache.last, h.app.ServerDir)
	validateCache.mu.Unlock()
}
