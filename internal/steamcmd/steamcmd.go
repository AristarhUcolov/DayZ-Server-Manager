// Copyright (c) 2026 Aristarh Ucolov.
//
// Package steamcmd runs Valve's command-line Steam client to download Steam
// Workshop mods for DayZ (app 221100) straight onto the server — no game
// client and no download folder to set up.
//
// DayZ's Workshop content is served only to an account that owns the game,
// so SteamCMD has to log in once. The password goes from the panel straight
// to SteamCMD and is never stored by the panel; SteamCMD then keeps its own
// login token (like the Steam client) and later downloads need only the
// account name.
//
// SteamCMD buffers its output when it is not writing to a console, so the
// panel never waits on a prompt: every run has stdin closed and
// @NoPromptForPassword set, credentials go on the command line, and the
// result is read when the run ends. Progress while it runs comes from the
// files on disk (partial downloads, its own console log).
package steamcmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/util"
)

// DayZApp is DayZ's Steam app id; its Workshop items download under it.
const DayZApp = "221100"

// Tool is one SteamCMD installation.
type Tool struct {
	Dir string
	mu  sync.Mutex // one run at a time: SteamCMD locks its own files
}

// New returns the tool kept in dir (installed or not).
func New(dir string) *Tool { return &Tool{Dir: dir} }

// Exe is the program to run.
func (t *Tool) Exe() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(t.Dir, "steamcmd.exe")
	}
	return filepath.Join(t.Dir, "steamcmd.sh")
}

// Installed reports whether SteamCMD is there.
func (t *Tool) Installed() bool {
	st, err := os.Stat(t.Exe())
	return err == nil && !st.IsDir()
}

// ContentDir is where downloaded DayZ Workshop items land, one <id> folder
// each — the layout the panel's mod source already understands.
func (t *Tool) ContentDir() string {
	return filepath.Join(t.Dir, "steamapps", "workshop", "content", DayZApp)
}

// DownloadingDir holds an item while it downloads.
func (t *Tool) DownloadingDir() string {
	return filepath.Join(t.Dir, "steamapps", "workshop", "downloads", DayZApp)
}

// ConsoleLog is SteamCMD's own log of what it printed.
func (t *Tool) ConsoleLog() string { return filepath.Join(t.Dir, "logs", "console_log.txt") }

func downloadURL() string {
	if runtime.GOOS == "windows" {
		return "https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip"
	}
	return "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"
}

// Install downloads SteamCMD from Valve and runs it until it has updated
// itself (the first runs download the rest of it, about 40 MB, and exit
// with code 7 to restart).
func (t *Tool) Install(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return err
	}
	if !t.Installed() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "DayZ-Server-Manager")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("download SteamCMD: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download SteamCMD: HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return fmt.Errorf("download SteamCMD: %w", err)
		}
		if runtime.GOOS == "windows" {
			err = unzip(data, t.Dir)
		} else {
			err = untgz(data, t.Dir)
		}
		if err != nil {
			return fmt.Errorf("unpack SteamCMD: %w", err)
		}
	}
	for i := 0; i < 4; i++ {
		_, code, err := t.runLocked(ctx, 20*time.Minute, "+quit")
		if err != nil {
			return err
		}
		if code != 7 { // 7: updated itself, run again
			return nil
		}
	}
	return nil
}

// LoginResult is what a login attempt came to.
type LoginResult struct {
	OK     bool   `json:"ok"`
	Need   string `json:"need,omitempty"`   // "guard" (e-mail code) | "2fa" (authenticator code)
	Mobile bool   `json:"mobile,omitempty"` // Steam asked to confirm in the mobile app
	Reason string `json:"reason,omitempty"` // password | badcode | ratelimit | nocache | network | timeout | unknown
	Detail string `json:"detail,omitempty"` // SteamCMD's own words
}

// Login signs user in. With a password it is the first sign-in (code is the
// Steam Guard code when one was asked for); without, it checks that SteamCMD
// still has a valid saved login. Up to five minutes for a confirmation in the
// Steam mobile app.
func (t *Tool) Login(ctx context.Context, user, pass, code string) LoginResult {
	args := []string{"+@ShutdownOnFailedCommand", "1", "+@NoPromptForPassword", "1", "+login", user}
	if pass != "" {
		args = append(args, pass)
		if code != "" {
			args = append(args, code)
		}
	}
	args = append(args, "+quit")
	out, _, err := t.Run(ctx, 5*time.Minute, args...)
	r := ParseLogin(out)
	if err != nil && !r.OK && r.Reason == "" {
		r.Reason = "timeout"
		if !errors.Is(err, context.DeadlineExceeded) {
			r.Reason, r.Detail = "unknown", err.Error()
		}
	}
	return r
}

// Forget removes SteamCMD's saved login, so the next sign-in asks for the
// password again.
func (t *Tool) Forget() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, name := range []string{"config.vdf", "loginusers.vdf"} {
		if err := os.Remove(filepath.Join(t.Dir, "config", name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	matches, _ := filepath.Glob(filepath.Join(t.Dir, "ssfn*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
	return nil
}

var (
	reLoginOK   = regexp.MustCompile(`(?i)(Logging in user .*\.\.\.OK|Waiting for user info\.\.\.OK)`)
	reFailed    = regexp.MustCompile(`(?i)FAILED[ :]*\(([^)]+)\)`)
	reMobile    = regexp.MustCompile(`(?i)confirm the login in the Steam Mobile app`)
	reGuard     = regexp.MustCompile(`(?i)(Steam Guard code|has not been authenticated for your account using Steam Guard|Account Logon Denied)`)
	re2FA       = regexp.MustCompile(`(?i)(Two-factor code:|Mobile Authenticator|Need Two Factor)`)
	reNoCache   = regexp.MustCompile(`(?i)(cached credentials|NoPromptForPassword|No password)`)
	reDownOK    = regexp.MustCompile(`(?i)Success\. Downloaded item (\d+) to "([^"]+)"(?: \((\d+) bytes\))?`)
	reDownFail  = regexp.MustCompile(`(?i)ERROR! Download item (\d+) failed \(([^)]+)\)`)
	reDownTime  = regexp.MustCompile(`(?i)ERROR! Timeout downloading item (\d+)`)
	reLoginLine = regexp.MustCompile(`(?i)Logging in user[^\r\n]*`)
)

// ParseLogin reads a login run's output.
func ParseLogin(out string) LoginResult {
	var r LoginResult
	r.Mobile = reMobile.MatchString(out)
	if m := reLoginLine.FindString(out); m != "" {
		r.Detail = strings.TrimSpace(m)
	}
	fail := reFailed.FindStringSubmatch(out)
	if fail == nil && reLoginOK.MatchString(out) {
		r.OK = true
		return r
	}
	reason := ""
	if fail != nil {
		reason = strings.TrimSpace(fail[1])
		r.Detail = reason
	}
	low := strings.ToLower(reason)
	switch {
	case strings.Contains(low, "invalid password"):
		r.Reason = "password"
	case strings.Contains(low, "invalid login auth code"), strings.Contains(low, "code mismatch"):
		r.Reason = "badcode"
		if strings.Contains(low, "two-factor") {
			r.Need = "2fa"
		} else {
			r.Need = "guard"
		}
	case strings.Contains(low, "rate limit"):
		r.Reason = "ratelimit"
	case re2FA.MatchString(out) || strings.Contains(low, "two factor"):
		r.Need = "2fa"
	case reGuard.MatchString(out):
		r.Need = "guard"
	case reNoCache.MatchString(out):
		r.Reason = "nocache"
	case strings.Contains(low, "no connection"), strings.Contains(low, "timeout"), strings.Contains(low, "service unavailable"):
		r.Reason = "network"
	case reason != "":
		r.Reason = "unknown"
	}
	return r
}

// ItemResult is one Workshop item of a download run.
type ItemResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Path  string `json:"path,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
	Error string `json:"error,omitempty"`
}

// ParseDownloads reads a download run's per-item results; ids without a
// result line get errNoResult.
func ParseDownloads(out string, ids []string) []ItemResult {
	got := map[string]ItemResult{}
	for _, m := range reDownOK.FindAllStringSubmatch(out, -1) {
		r := ItemResult{ID: m[1], OK: true, Path: m[2]}
		fmt.Sscan(m[3], &r.Bytes)
		got[m[1]] = r
	}
	for _, m := range reDownFail.FindAllStringSubmatch(out, -1) {
		if _, ok := got[m[1]]; !ok {
			got[m[1]] = ItemResult{ID: m[1], Error: strings.TrimSpace(m[2])}
		}
	}
	for _, m := range reDownTime.FindAllStringSubmatch(out, -1) {
		if _, ok := got[m[1]]; !ok {
			got[m[1]] = ItemResult{ID: m[1], Error: "Timeout"}
		}
	}
	out2 := make([]ItemResult, 0, len(ids))
	for _, id := range ids {
		if r, ok := got[id]; ok {
			out2 = append(out2, r)
		} else {
			out2 = append(out2, ItemResult{ID: id, Error: "no result"})
		}
	}
	return out2
}

// Download fetches Workshop items with user's saved login. The login result
// tells a sign-in problem apart from a failed item.
func (t *Tool) Download(ctx context.Context, user string, ids []string) ([]ItemResult, LoginResult, error) {
	args := []string{"+@ShutdownOnFailedCommand", "0", "+@NoPromptForPassword", "1", "+login", user}
	for _, id := range ids {
		args = append(args, "+workshop_download_item", DayZApp, id, "validate")
	}
	args = append(args, "+quit")
	out, _, err := t.Run(ctx, 3*time.Hour, args...)
	lr := ParseLogin(out)
	if !lr.OK && lr.Reason == "" && lr.Need == "" && !reDownOK.MatchString(out) {
		lr.Reason = "unknown"
	}
	res := ParseDownloads(out, ids)
	// A file on disk is the truth when the summary line went missing.
	for i := range res {
		if !res[i].OK {
			p := filepath.Join(t.ContentDir(), res[i].ID)
			if st, e := os.Stat(p); e == nil && st.IsDir() && res[i].Error == "no result" && lr.OK {
				res[i] = ItemResult{ID: res[i].ID, OK: true, Path: p}
			}
		}
	}
	return res, lr, err
}

// Run runs SteamCMD once with args (stdin closed) and returns its output.
func (t *Tool) Run(ctx context.Context, timeout time.Duration, args ...string) (string, int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runLocked(ctx, timeout, args...)
}

func (t *Tool) runLocked(ctx context.Context, timeout time.Duration, args ...string) (string, int, error) {
	if !t.Installed() {
		return "", -1, errors.New("SteamCMD is not installed")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, t.Exe(), args...)
	cmd.Dir = t.Dir
	util.HideWindow(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if cctx.Err() != nil {
		return buf.String(), code, cctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		err = nil // a non-zero exit is reported by the output, not as a failure to run
	}
	return buf.String(), code, err
}

// LogTail returns the last lines of SteamCMD's console log (what it is
// doing right now — its stdout arrives only at the end).
func (t *Tool) LogTail(n int) []string {
	data, err := os.ReadFile(t.ConsoleLog())
	if err != nil {
		return nil
	}
	if len(data) > 64<<10 {
		data = data[len(data)-64<<10:]
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", "\n"), "\n")
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func unzip(data []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		dst, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(dst, 0o755)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 256<<20))
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func untgz(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		dst, err := safeJoin(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(dst, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(tr, 256<<20))
			if err != nil {
				return err
			}
			mode := os.FileMode(h.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			if err := os.WriteFile(dst, b, mode|0o200); err != nil {
				return err
			}
		}
	}
}

// safeJoin keeps an archive entry inside dir.
func safeJoin(dir, name string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(name))
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return p, nil
}

// FolderSize is the size of a folder tree (a download in progress).
func FolderSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}
