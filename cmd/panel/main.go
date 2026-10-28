// DayZ Server Manager — desktop build (dayz-manager-panel).
// Copyright (c) 2026 Aristarh Ucolov. All rights reserved.
//
// The same manager as the browser build, shown in its own application window:
// no browser, no address to type. On Windows it is a native WebView2 window
// with a tray icon; on Linux an app-mode browser window.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"dayzmanager/internal/desktop"
	"dayzmanager/internal/launcher"
)

func main() {
	var (
		webPort = flag.Int("port", 8787, "panel port")
		bind    = flag.String("bind", "", "panel bind address (blank = follow settings.exposure)")
		showVer = flag.Bool("version", false, "print version and exit")
		reset   = flag.Bool("reset-login", false, "turn the panel login off (forgotten password), then start as usual")
		login   = flag.String("set-login", "", "turn the panel login on for this user name (asks for the password), then start")
		expo    = flag.String("exposure", "", "save the panel exposure before starting: local | lan | internet (internet needs a login)")
		install = flag.Bool("install", false, "add the manager to the Start menu / applications menu and the desktop, then exit")
		uninst  = flag.Bool("uninstall", false, "remove what -install added, then exit")
		autost  = flag.Bool("autostart", false, "started together with the computer: stay in the tray, no window")
		dir     = flag.String("dir", "", "the DayZ server folder (default: the working folder when it holds a server, else the program's folder)")
		guard   = flag.Int("guard", 0, "internal: watch the manager with this process id and start it again if it stops unexpectedly")
	)
	flag.Parse()
	launcher.SetServerDir(*dir)
	if *guard > 0 {
		launcher.RunGuardian(*guard, *webPort, *bind)
		return
	}
	if *showVer {
		fmt.Printf("%s (desktop) v%s by %s\n", launcher.AppName, launcher.Version, launcher.AppAuthor)
		return
	}
	if *install || *uninst {
		var (
			paths []string
			err   error
		)
		if *install {
			paths, err = launcher.InstallDesktop("panel")
		} else {
			paths, err = launcher.UninstallDesktop()
		}
		for _, p := range paths {
			fmt.Println("  " + p)
		}
		if err != nil {
			desktop.Alert(launcher.AppName, err.Error())
			os.Exit(1)
		}
		if *install {
			msg := launcher.AppName + " is in the applications menu and on the desktop now."
			if runtime.GOOS == "windows" {
				msg = launcher.AppName + " is in the Start menu and on the desktop now."
			}
			fmt.Println(msg)
			if !launcher.IsTerminal() {
				desktop.Alert(launcher.AppName, msg)
			}
		}
		return
	}
	if *reset || *login != "" || *expo != "" {
		// A running manager holds the login and settings in memory and writes
		// them back: changing the files under it would do nothing.
		if url, ok := launcher.RunningURL(*webPort); ok {
			desktop.Alert(launcher.AppName, launcher.AppName+" is already running at "+url+
				" — stop it first, then run with -reset-login / -set-login / -exposure again")
			os.Exit(1)
		}
	}
	if *reset {
		if _, err := launcher.ResetLogin(); err != nil {
			desktop.Alert(launcher.AppName, "reset login: "+err.Error())
			os.Exit(1)
		}
	}
	if *login != "" {
		if err := launcher.SetLoginInteractive(*login); err != nil {
			desktop.Alert(launcher.AppName, "set login: "+err.Error())
			os.Exit(1)
		}
	}

	inst, err := launcher.Start(launcher.Options{Version: launcher.Version, Port: *webPort, Bind: *bind, Mode: "panel", Exposure: *expo})
	var running *launcher.ErrAlreadyRunning
	if errors.As(err, &running) {
		if *autost {
			return // started at sign-in while it already runs: nothing to show
		}
		// Already running: a panel just brings its window forward; a browser
		// build gets a window attached to it.
		if askToShow(running.URL) {
			return
		}
		_ = desktop.Run(nil, running.URL)
		return
	}
	if err != nil {
		desktop.Alert(launcher.AppName, err.Error())
		os.Exit(1)
	}
	inst.ExitOnSignal()
	if runtime.GOOS != "windows" {
		// Started from a terminal (or a file manager that opened one): say
		// where the panel is, whatever happens with the window.
		fmt.Print(inst.Banner(true))
	}
	desktop.StartHidden = *autost
	if err := desktop.Run(inst, ""); err != nil {
		inst.App.Log.Printf("desktop window: %v", err)
	}
	if inst.Context().Err() == nil && inst.AnyServerRunning() {
		// The window went away without anyone asking the manager to exit
		// (the browser engine failed): never take the DayZ server down with
		// it. The manager keeps running — launching the panel again shows a
		// new window, and Exit in Settings ends it.
		inst.App.Log.Printf("the panel window closed unexpectedly — the manager keeps running for the DayZ server at %s", inst.URL)
		<-inst.Context().Done()
	}
	inst.RequestExit(true) // no-op when an exit was already requested
	inst.App.Log.Printf("shutting down...")
	inst.Shutdown()
}

// askToShow asks a running panel to bring its window forward.
func askToShow(url string) bool {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Post(url+"api/app/show", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return bytes.Contains(buf.Bytes(), []byte(`"shown":true`))
}
