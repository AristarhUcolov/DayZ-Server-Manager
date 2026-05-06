// DayZ Server Manager
// Copyright (c) 2026 Aristarh Ucolov. All rights reserved.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/web"
)

const (
	appName    = "DayZ Server Manager"
	appVersion = "0.10.0"
	appAuthor  = "Aristarh Ucolov"
)

func main() {
	var (
		webPort   = flag.Int("port", 8787, "web panel port")
		bindAddr  = flag.String("bind", "", "web panel bind address (blank = follow settings.exposure: 127.0.0.1 for local, 0.0.0.0 for LAN)")
		noBrowser = flag.Bool("no-browser", false, "do not auto-open the browser")
		showVer   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s v%s by %s\n", appName, appVersion, appAuthor)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to resolve executable: %v", err)
	}
	serverDir := filepath.Dir(exePath)
	if cwd, _ := os.Getwd(); cwd != "" && sniffDayZ(cwd) {
		serverDir = cwd
	}

	a, err := app.New(serverDir, appName, appVersion, appAuthor)
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	defer a.Close()

	a.Log.Printf("%s v%s — server dir: %s", appName, appVersion, serverDir)
	a.Log.Printf("language: %s", i18n.Name(a.Config.Language))

	effectiveBind := *bindAddr
	if effectiveBind == "" {
		if a.Config.Exposure == "internet" {
			effectiveBind = "0.0.0.0"
		} else {
			effectiveBind = "127.0.0.1"
		}
	}

	srv := web.New(a, effectiveBind, *webPort)
	ctx, cancel := signalContext()
	defer cancel()

	go func() {
		if err := srv.Start(ctx); err != nil {
			a.Log.Printf("web server stopped: %v", err)
		}
	}()

	url := fmt.Sprintf("http://%s:%d/", displayHost(effectiveBind), *webPort)
	a.Log.Printf("panel: %s", url)
	if !*noBrowser {
		time.AfterFunc(400*time.Millisecond, func() { openBrowser(url) })
	}

	<-ctx.Done()
	a.Log.Printf("shutting down...")
	shutdownCtx, sdCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer sdCancel()
	_ = srv.Stop(shutdownCtx)
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}

func sniffDayZ(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "DayZServer_x64.exe"))
	return err == nil
}

func displayHost(bind string) string {
	if bind == "0.0.0.0" {
		return "localhost"
	}
	return bind
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
