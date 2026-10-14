// DayZ Server Manager — browser build (dayz-manager-web).
// Copyright (c) 2026 Aristarh Ucolov. All rights reserved.
//
// Runs in a console window and opens the panel in the default browser. The
// desktop build (cmd/panel, dayz-manager-panel) shows the same panel in its own
// app window instead.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"runtime"
	"time"

	"dayzmanager/internal/launcher"
)

func main() {
	var (
		webPort   = flag.Int("port", 8787, "web panel port")
		bindAddr  = flag.String("bind", "", "web panel bind address (blank = follow settings.exposure: 127.0.0.1 for local, all interfaces for LAN)")
		noBrowser = flag.Bool("no-browser", false, "do not auto-open the browser")
		showVer   = flag.Bool("version", false, "print version and exit")
		resetAuth = flag.Bool("reset-login", false, "turn the panel login off (forgotten password), then start as usual")
		setLogin  = flag.String("set-login", "", "turn the panel login on for this user name (asks for the password), then start")
		exposure  = flag.String("exposure", "", "save the panel exposure before starting: local | lan | internet (internet needs a login)")
		install   = flag.Bool("install", false, "add the manager to the Start menu / applications menu and the desktop, then exit")
		uninstall = flag.Bool("uninstall", false, "remove what -install added, then exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s v%s by %s\n", launcher.AppName, launcher.Version, launcher.AppAuthor)
		return
	}
	if *install || *uninstall {
		runInstall(*install)
		return
	}
	if *resetAuth || *setLogin != "" || *exposure != "" {
		// A running manager holds the login and settings in memory and writes
		// them back: changing the files under it would report success and do
		// nothing (and -exposure is only applied by a fresh start).
		if url, ok := launcher.RunningURL(*webPort); ok {
			log.Fatalf("%s is already running at %s — stop it first, then run with -reset-login / -set-login / -exposure again", launcher.AppName, url)
		}
	}
	if *resetAuth {
		if dir, err := launcher.ResetLogin(); err != nil {
			log.Fatalf("reset login: %v", err)
		} else {
			fmt.Printf("Panel login turned off (%s). Set a new password in Settings → Access & security.\n", dir)
		}
	}
	if *setLogin != "" {
		if err := launcher.SetLoginInteractive(*setLogin); err != nil {
			log.Fatalf("set login: %v", err)
		}
	}

	inst, err := launcher.Start(launcher.Options{Version: launcher.Version, Port: *webPort, Bind: *bindAddr, Mode: "web", Exposure: *exposure})
	var running *launcher.ErrAlreadyRunning
	if errors.As(err, &running) {
		// A second double-click: the manager is already up — just show it.
		fmt.Printf("%s is already running: %s\n", launcher.AppName, running.URL)
		if !*noBrowser && !launcher.OpenBrowser(running.URL) && !launcher.IsTerminal() {
			launcher.MessageBox(launcher.AppName, launcher.AppName+" is already running: "+running.URL)
		}
		return
	}
	if err != nil {
		// Started from a file manager there is no terminal to read this in.
		if !launcher.IsTerminal() {
			launcher.MessageBox(launcher.AppName, err.Error())
		}
		log.Fatalf("%v", err)
	}
	inst.ExitOnSignal()
	opened := true
	if !*noBrowser {
		time.Sleep(300 * time.Millisecond) // let the listener settle before a browser asks
		opened = launcher.OpenBrowser(inst.PanelURL())
	}
	// Where the panel is and how to reach it — on Linux this terminal is often
	// all the user sees.
	fmt.Print(inst.Banner(opened))
	if !opened && !launcher.IsTerminal() {
		// No terminal and no browser: the only way to say where the panel is.
		go launcher.MessageBox(launcher.AppName, inst.Banner(false))
	}

	<-inst.Context().Done()
	inst.App.Log.Printf("shutting down...")
	inst.Shutdown()
}

// runInstall adds (or removes) the Linux applications-menu launcher.
func runInstall(install bool) {
	var (
		paths []string
		err   error
	)
	if install {
		paths, err = launcher.InstallDesktop("web")
	} else {
		paths, err = launcher.UninstallDesktop()
	}
	for _, p := range paths {
		fmt.Println("  " + p)
	}
	if err != nil {
		log.Fatalf("%v", err)
	}
	if install {
		where := "the applications menu"
		if runtime.GOOS == "windows" {
			where = "the Start menu and on the desktop"
		}
		fmt.Println(launcher.AppName + " is in " + where + " now (it opens the panel in your browser).")
	} else {
		fmt.Println("Removed.")
	}
}
