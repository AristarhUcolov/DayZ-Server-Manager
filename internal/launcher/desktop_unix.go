// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// desktopSession is the graphical session a browser window must open in, and
// how to run a program inside it.
//
// The usual reasons the panel "only opened a terminal" on Linux:
//   - the manager runs as root (sudo): Chrome/Chromium refuse to start as root
//     without --no-sandbox, and root may not reach the user's display at all
//     (sudo drops WAYLAND_DISPLAY / XDG_RUNTIME_DIR / XAUTHORITY);
//   - a launcher started it without DISPLAY / WAYLAND_DISPLAY in the
//     environment although a desktop is running.
//
// So when we are root through sudo, browsers are started as the user who ran
// sudo, with that user's session variables; and the display is found from its
// sockets when the variables are missing.
type desktopSession struct {
	ok     bool     // a graphical session exists
	asUser string   // run programs as this user (we are root via sudo); "" = as ourselves
	home   string   // home of the user the browser runs as
	env    []string // session variables to add (DISPLAY, WAYLAND_DISPLAY, ...)
}

var (
	sessOnce sync.Once
	sess     desktopSession
)

func session() desktopSession {
	sessOnce.Do(func() { sess = detectSession() })
	return sess
}

// sudoUser is the desktop user behind a root process started with sudo,
// pkexec or doas ("" when not applicable).
func sudoUser() string {
	for _, k := range []string{"SUDO_USER", "DOAS_USER"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" && v != "root" {
			return v
		}
	}
	if id := strings.TrimSpace(os.Getenv("PKEXEC_UID")); id != "" && id != "0" {
		if u, err := user.LookupId(id); err == nil {
			return u.Username
		}
	}
	return ""
}

// sshConnection is the SSH_CONNECTION ("client port server port") of the SSH
// login the manager was started from, "" when there is none. sudo, doas,
// pkexec and "su -" drop it from the environment, so as root it is looked up
// in the parent processes (root can read their environment). "sudo su -"
// leaves no SUDO_USER either, so this does not depend on sudoUser().
func sshConnection() string {
	if v := os.Getenv("SSH_CONNECTION"); v != "" {
		return v
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return ""
	}
	pid := os.Getppid()
	for i := 0; i < 16 && pid > 1; i++ {
		if env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid)); err == nil {
			if v := environValue(env, "SSH_CONNECTION"); v != "" {
				return v
			}
		}
		st, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			break
		}
		pid = statusPPid(st)
	}
	return ""
}

// environValue reads key from a /proc/<pid>/environ blob (NUL-separated).
func environValue(env []byte, key string) string {
	for _, kv := range strings.Split(string(env), "\x00") {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// statusPPid reads the parent pid from a /proc/<pid>/status blob (0 if absent).
func statusPPid(status []byte) int {
	for _, line := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(line, "PPid:"); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

func detectSession() desktopSession {
	if runtime.GOOS == "darwin" {
		return desktopSession{ok: true}
	}
	s := desktopSession{}
	home, _ := os.UserHomeDir()
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if os.Geteuid() == 0 {
		if name := sudoUser(); name != "" {
			if u, err := user.Lookup(name); err == nil && u.Uid != "0" {
				s.asUser = u.Username
				home = u.HomeDir
				runtimeDir = "/run/user/" + u.Uid
			}
		}
	}
	s.home = home

	display := os.Getenv("DISPLAY")
	wayland := os.Getenv("WAYLAND_DISPLAY")
	// Over SSH with no forwarded display there is no screen to open a window
	// on — even if someone happens to be logged in at the machine.
	remote := sshConnection() != "" && display == "" && wayland == ""
	if !remote && wayland == "" && runtimeDir != "" {
		if m, _ := filepath.Glob(filepath.Join(runtimeDir, "wayland-*")); len(m) > 0 {
			for _, p := range m {
				if !strings.HasSuffix(p, ".lock") {
					wayland = filepath.Base(p)
					break
				}
			}
		}
	}
	if !remote && display == "" {
		if m, _ := filepath.Glob("/tmp/.X11-unix/X*"); len(m) > 0 {
			display = ":" + strings.TrimPrefix(filepath.Base(m[0]), "X")
		}
	}
	if display == "" && wayland == "" {
		return s // headless
	}
	s.ok = true
	add := func(k, v string) {
		if v != "" {
			s.env = append(s.env, k+"="+v)
		}
	}
	add("DISPLAY", display)
	add("WAYLAND_DISPLAY", wayland)
	if s.asUser != "" || os.Getenv("XDG_RUNTIME_DIR") == "" {
		if st, err := os.Stat(runtimeDir); err == nil && st.IsDir() {
			add("XDG_RUNTIME_DIR", runtimeDir)
		}
	}
	if s.asUser != "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		if runtimeDir != "" {
			if _, err := os.Stat(filepath.Join(runtimeDir, "bus")); err == nil {
				add("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(runtimeDir, "bus"))
			}
		}
	}
	if xa := os.Getenv("XAUTHORITY"); xa == "" || s.asUser != "" {
		for _, c := range []string{xa, filepath.Join(home, ".Xauthority"), filepath.Join(runtimeDir, "gdm", "Xauthority")} {
			if c == "" {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				add("XAUTHORITY", c)
				break
			}
		}
	}
	if s.asUser != "" {
		add("HOME", home)
	}
	return s
}

func hasDesktop() bool { return session().ok }

// DesktopCommand prepares a program to run inside the desktop session — as the
// logged-in user when the manager itself runs as root through sudo, with the
// session variables it needs.
func DesktopCommand(name string, args ...string) *exec.Cmd {
	s := session()
	if s.asUser == "" {
		cmd := exec.Command(name, args...)
		if len(s.env) > 0 {
			cmd.Env = append(os.Environ(), s.env...) // later entries win
		}
		return cmd
	}
	for _, wrapper := range []string{"sudo", "runuser"} {
		if p, err := exec.LookPath(wrapper); err == nil {
			full := append([]string{"-u", s.asUser, "--", "env"}, s.env...)
			full = append(full, name)
			return exec.Command(p, append(full, args...)...)
		}
	}
	return exec.Command(name, args...)
}

// RunsAsRoot reports whether programs started through DesktopCommand run as
// root (a desktop session logged in as root): Chromium-family browsers then
// need --no-sandbox.
func RunsAsRoot() bool { return os.Geteuid() == 0 && session().asUser == "" }

// DesktopHome is the home folder of the user browsers run as.
func DesktopHome() string { return session().home }

// DesktopUserDiffers reports whether browsers run as another user than the
// manager (root through sudo) — they cannot write into the manager's folders.
func DesktopUserDiffers() bool { return session().asUser != "" }

// chromiumNames are Chromium-family browsers, in order of preference.
var chromiumNames = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
	"microsoft-edge", "microsoft-edge-stable", "brave-browser", "vivaldi"}

// openDesktop opens url in a browser of the desktop session. It tries the
// desktop's own opener first, then common browsers, and only reports success
// when the program did not fail straight away.
func openDesktop(url string) bool {
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("open", url)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // see below
		return TryStart(cmd, launchWait) == nil
	}
	if !session().ok {
		return false
	}
	var cands [][]string
	if RunsAsRoot() {
		// A browser started as root: Firefox runs, Chromium needs --no-sandbox,
		// and xdg-open would pick whatever the default is (often refusing).
		cands = append(cands, []string{"firefox", url})
		for _, b := range chromiumNames {
			cands = append(cands, []string{b, "--no-sandbox", url})
		}
		cands = append(cands, []string{"xdg-open", url})
	} else {
		cands = [][]string{{"xdg-open", url}, {"gio", "open", url}, {"sensible-browser", url},
			{"x-www-browser", url}, {"firefox", url}}
		for _, b := range chromiumNames {
			cands = append(cands, []string{b, url})
		}
	}
	for _, c := range cands {
		p, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := DesktopCommand(p, c[1:]...)
		// Its own session: a browser this starts becomes the user's main
		// browser, and in the manager's process group Ctrl+C or closing the
		// terminal would take it down with all its tabs. (The desktop build's
		// app window is not started here — it should close with the manager.)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := TryStart(cmd, launchWait); err != nil {
			logf("open browser: %v", err)
			continue
		}
		return true
	}
	return false
}
