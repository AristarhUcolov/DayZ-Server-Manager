// Copyright (c) 2026 Aristarh Ucolov.
//
// Package desktop shows the panel as an application window (the
// dayz-manager-panel build). Windows uses an embedded WebView2 window with a
// tray icon; everywhere else — and on Windows machines without the WebView2
// runtime — the panel opens in an installed Chromium-family browser in "app"
// mode (its own window, no tabs or address bar).
package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/i18n"
	"dayzmanager/internal/launcher"
)

// text returns a UI string for the desktop shell in the manager's language.
// runningText is a close/exit question, in its several-servers form when more
// than one DayZ server of the panel runs.
func runningText(inst *launcher.Instance, key string) string {
	if inst != nil {
		if n := inst.RunningServers(); n > 1 {
			return strings.ReplaceAll(text(inst, key+".multi"), "{n}", strconv.Itoa(n))
		}
	}
	return text(inst, key)
}

func text(inst *launcher.Instance, key string) string {
	lang := "en"
	if inst != nil {
		lang = inst.App.Cfg().Language
	}
	if s, ok := i18n.Get(lang)[key]; ok {
		return s
	}
	return key
}

// appBrowsers lists Chromium-family browsers that support --app, in order.
func appBrowsers() []string {
	if runtime.GOOS == "windows" {
		var out []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
			base := os.Getenv(env)
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			)
		}
		return out
	}
	return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"microsoft-edge", "microsoft-edge-stable", "brave-browser", "vivaldi", "opera"}
}

// appBrowser is a Chromium-family browser that can open an app window.
type appBrowser struct {
	path    string // executable (or the flatpak command)
	flatpak string // flatpak application id, run as "flatpak run <id>"
}

// appBrowserCandidates lists the installed app-capable browsers, best first:
// regular installs (incl. snap wrappers), then flatpak apps on Linux.
func appBrowserCandidates() []appBrowser {
	var out []appBrowser
	seen := map[string]bool{}
	for _, b := range appBrowsers() {
		p := b
		if filepath.IsAbs(b) {
			if st, err := os.Stat(b); err != nil || st.IsDir() {
				continue
			}
		} else {
			lp, err := exec.LookPath(b)
			if err != nil {
				continue
			}
			p = lp
		}
		key := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			key = r
		}
		if snap := snapApp(p, key); snap != "" {
			key = "snap:" + snap // every snap command resolves to /usr/bin/snap
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, appBrowser{path: p})
	}
	if runtime.GOOS == "linux" && !launcher.RunsAsRoot() {
		if fp, err := exec.LookPath("flatpak"); err == nil {
			for _, id := range []string{"com.google.Chrome", "org.chromium.Chromium", "com.brave.Browser",
				"com.microsoft.Edge", "io.github.ungoogled_software.ungoogled_chromium"} {
				if launcher.DesktopCommand(fp, "info", id).Run() == nil {
					out = append(out, appBrowser{path: fp, flatpak: id})
				}
			}
		}
	}
	return out
}

// profileDir is where the app window keeps its browser profile (its own, so
// it is a separate process the manager can wait on). Sandboxed browsers and a
// browser running as another user cannot write into the manager's folder.
func profileDir(inst *launcher.Instance, b appBrowser) string {
	if b.flatpak != "" {
		return "" // a flatpak browser only sees its own sandbox
	}
	home := launcher.DesktopHome()
	real := b.path
	if r, err := filepath.EvalSymlinks(b.path); err == nil {
		real = r
	}
	if name := snapApp(b.path, real); name != "" && home != "" {
		// snap confinement: writable only under ~/snap/<name>/
		return filepath.Join(home, "snap", name, "common", "dayz-manager-panel")
	}
	if launcher.DesktopUserDiffers() && home != "" {
		return filepath.Join(home, ".cache", "dayz-manager-panel")
	}
	if inst != nil {
		return filepath.Join(inst.App.ManagerDir, "panel-window")
	}
	return filepath.Join(os.TempDir(), "dayz-manager-panel")
}

// snapApp names the snap a browser command belongs to ("" when it is not a
// snap). path is the command as found on PATH, real the same with symlinks
// resolved. Snap commands are symlinks /snap/bin/<snap>[.<app>] ->
// /usr/bin/snap, so the snap's name is only in the unresolved path.
func snapApp(path, real string) string {
	path, real = filepath.ToSlash(path), filepath.ToSlash(real)
	if strings.HasPrefix(path, "/snap/bin/") || (real == "/usr/bin/snap" && path != real) {
		base := path[strings.LastIndex(path, "/")+1:]
		return strings.SplitN(base, ".", 2)[0]
	}
	for _, p := range []string{path, real} {
		if strings.HasPrefix(p, "/snap/") {
			if name := strings.SplitN(strings.TrimPrefix(p, "/snap/"), "/", 2)[0]; name != "" && name != "bin" {
				return name // /snap/<snap>/<revision>/...
			}
		}
	}
	return ""
}

// startAppBrowser starts one browser in app mode. withProfile=false leaves the
// profile to the browser (the retry for confined browsers that refuse ours).
func startAppBrowser(inst *launcher.Instance, b appBrowser, url string, withProfile bool) (launcher.Launched, error) {
	args := []string{"--app=" + url, "--no-first-run", "--no-default-browser-check",
		"--window-size=1360,880", "--class=DayZServerManager"}
	if withProfile {
		if dir := profileDir(inst, b); dir != "" {
			args = append(args, "--user-data-dir="+dir)
		}
	}
	if launcher.RunsAsRoot() {
		args = append(args, "--no-sandbox") // Chromium refuses to start as root otherwise
	}
	var cmd *exec.Cmd
	if b.flatpak != "" {
		cmd = launcher.DesktopCommand(b.path, append([]string{"run", b.flatpak}, args...)...)
	} else {
		cmd = launcher.DesktopCommand(b.path, args...)
	}
	return launcher.Launch(cmd, 4*time.Second)
}

func logf(inst *launcher.Instance, format string, a ...interface{}) {
	if inst != nil {
		inst.App.Log.Printf(format, a...)
	}
}

// panelURL is the address a window on this computer opens: with a panel login
// it carries a fresh one-time sign-in token (see launcher.Instance.PanelURL).
func panelURL(inst *launcher.Instance, fallback string) string {
	if inst != nil {
		return inst.PanelURL()
	}
	return fallback
}

// runAppBrowser opens url in an app-mode browser window and blocks until the
// window is closed or the manager exits. Each installed browser is tried in
// turn: one that fails straight away (no display, running as root, a
// sandboxed browser refusing the profile folder) no longer ends the attempt —
// before, that left the user with nothing but a terminal. Returns false when
// no browser could open the window.
func runAppBrowser(inst *launcher.Instance, url string) bool {
	for _, b := range appBrowserCandidates() {
		started, err := startAppBrowser(inst, b, url, true)
		if err != nil {
			logf(inst, "panel window: %v", err)
			if started, err = startAppBrowser(inst, b, url, false); err != nil {
				logf(inst, "panel window (default profile): %v", err)
				continue
			}
		}
		var ctxDone <-chan struct{}
		if inst != nil {
			ctxDone = inst.Context().Done()
		}
		if started.HandedOff {
			// The browser passed the window to an instance that was already
			// running and exited: nothing to wait on — keep the manager up
			// until it is exited from the panel.
			logf(inst, "panel window handed off to the running browser — exit the manager from Settings")
			if ctxDone != nil {
				<-ctxDone
			}
			return true
		}
		select {
		case <-started.Done:
			if inst != nil {
				if inst.AnyServerRunning() {
					inst.App.Log.Printf("panel window closed — the manager keeps running for the DayZ server; reopen the panel to manage it")
					fmt.Println(text(inst, "desktop.keptRunning") + " " + inst.URL)
					<-ctxDone
				} else {
					inst.RequestExit(true)
				}
			}
		case <-ctxDone:
			// The manager exits; the window goes with the browser process.
		}
		return true
	}
	return false
}
