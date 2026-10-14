// Copyright (c) 2026 Aristarh Ucolov.
//
// Package launcher boots the manager the same way for both builds:
//
//	dayz-manager-web    the browser build: a console window, the panel opens
//	                    in the default browser (what dayz-manager.exe was);
//	dayz-manager-panel  the desktop build: the panel in its own app window,
//	                    no browser, no address to type.
//
// It resolves the server directory, starts the app and the web server on a
// listener it opened itself (so a busy port is detected up front), and —
// when another manager for the same server already answers on that port —
// reports it, so the caller can reuse it instead of starting a second copy.
package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/web"
)

const (
	AppName   = "DayZ Server Manager"
	AppAuthor = "Aristarh Ucolov"
)

// Options configure a start.
type Options struct {
	Version string
	Port    int
	Bind    string // "" = follow settings.exposure
	Mode    string // "web" | "panel" (shown in /api/info, drives exit behaviour)
	// Exposure, when set (local | lan | internet), is saved to the settings
	// before the panel starts — the -exposure flag, for servers without a
	// desktop where the Settings page cannot be reached yet.
	Exposure string
}

// Instance is a running manager.
type Instance struct {
	App    *app.App   // the primary server (the folder the manager runs from)
	Fleet  *app.Fleet // every server of the panel
	Server *web.Server
	URL    string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	stopOnce   sync.Once
	stopServer bool
	bind       string // the address the panel listens on ("" = all interfaces)
	port       int
}

// ErrAlreadyRunning is returned (with the running manager's URL) when another
// manager for this server already answers on the port.
type ErrAlreadyRunning struct{ URL string }

func (e *ErrAlreadyRunning) Error() string { return "the manager is already running at " + e.URL }

// ServerDir picks the DayZ server directory: the working directory when it
// holds a DayZ server (running from a shortcut / terminal), else the exe's.
func ServerDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	dir := filepath.Dir(exePath)
	if cwd, _ := os.Getwd(); cwd != "" && SniffDayZ(cwd) {
		dir = cwd
	}
	return dir, nil
}

// SniffDayZ reports whether dir holds a DayZ dedicated server.
func SniffDayZ(dir string) bool {
	// Windows ships DayZServer_x64.exe; the Linux dedicated server is DayZServer.
	for _, name := range []string{"DayZServer_x64.exe", "DayZServer"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// DisplayHost picks the address shown in the log and opened in the browser.
// Always a loopback IP, never "localhost": Windows resolves localhost to ::1
// first, and an IPv4-only listener then produced a tab that never loaded.
func DisplayHost(bind string) string {
	if bind == "" || bind == "0.0.0.0" || bind == "::" {
		return "127.0.0.1"
	}
	return bind
}

// normalizeBind turns a -bind value into the host to listen on: the
// any-address forms become "" (dual-stack, all interfaces) and a bracketed
// IPv6 literal loses its brackets.
func normalizeBind(bind string) string {
	bind = strings.TrimSpace(bind)
	if strings.HasPrefix(bind, "[") && strings.HasSuffix(bind, "]") {
		bind = bind[1 : len(bind)-1]
	}
	if bind == "0.0.0.0" || bind == "::" {
		return ""
	}
	return bind
}

// probe asks whatever listens at url whether it is a DayZ Server Manager for
// serverDir.
func probe(url, serverDir string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "api/info")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var info struct {
		Name      string `json:"name"`
		ServerDir string `json:"serverDir"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil || info.Name != AppName {
		return false
	}
	return serverDir == "" || strings.EqualFold(filepath.Clean(info.ServerDir), filepath.Clean(serverDir))
}

// RunningURL reports whether a manager for this server already answers on
// port — or on one of the next ports Start falls back to when port is taken by
// another program — and its URL. The command-line flags that rewrite the
// manager's files must not run then: the running manager keeps them in memory
// and writes them back.
func RunningURL(port int) (string, bool) {
	serverDir, err := ServerDir()
	if err != nil {
		return "", false
	}
	if port == 0 {
		port = 8787
	}
	for p := port; p <= port+20 && p <= 65535; p++ {
		url := fmt.Sprintf("http://127.0.0.1:%d/", p)
		if probe(url, serverDir) {
			return url, true
		}
	}
	return "", false
}

// Start boots the manager. If the port is taken by a manager for the same
// server, it returns *ErrAlreadyRunning; if it is taken by something else, the
// next free port (up to +20) is used and logged.
func Start(opts Options) (*Instance, error) {
	serverDir, err := ServerDir()
	if err != nil {
		return nil, err
	}
	if opts.Port == 0 {
		opts.Port = 8787
	}
	// Probe BEFORE initialising: app.New adopts a running DayZ server, and a
	// second copy that adopted and then bailed out would stop it.
	if url := fmt.Sprintf("http://127.0.0.1:%d/", opts.Port); probe(url, serverDir) {
		return nil, &ErrAlreadyRunning{URL: url}
	}
	// This folder may also be a server another manager runs from its Servers
	// page (multi-server): open that panel instead of supervising it twice.
	if url, busy := app.OwnerOf(serverDir, filepath.Join(serverDir, ".dayz-manager")); busy {
		return nil, &ErrAlreadyRunning{URL: url}
	}
	a, err := app.New(serverDir, AppName, opts.Version, AppAuthor)
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	a.Mode = opts.Mode
	setLogHook(a.Log.Printf)
	a.Log.Printf("%s v%s (%s) — server dir: %s", AppName, opts.Version, opts.Mode, serverDir)
	if err := applyExposure(a, opts.Exposure); err != nil {
		a.Detach()
		return nil, err
	}
	a.Log.Printf("language: %s", i18n.Name(a.Config.Language))
	a.ShortcutsInstalled = desktopInstalled
	a.InstallShortcuts = func() ([]string, error) { return InstallDesktop(opts.Mode) }

	bind := opts.Bind
	if bind == "" {
		// "lan" and "internet" expose the panel on all interfaces so other
		// devices can reach it; only "local" stays loopback-only. An empty host
		// listens dual-stack — binding "0.0.0.0" was IPv4-only, so
		// http://localhost (::1 first on Windows) hit a dead socket.
		if a.Config.Exposure == "internet" || a.Config.Exposure == "lan" {
			bind = ""
		} else {
			bind = "127.0.0.1"
		}
	} else {
		bind = normalizeBind(bind)
	}

	var ln net.Listener
	port := opts.Port
	for try := 0; try <= 20; try++ {
		// JoinHostPort brackets IPv6 hosts ("::1" alone made ":::1:8787").
		ln, err = net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
		if err == nil {
			break
		}
		var addrErr *net.AddrError
		var dnsErr *net.DNSError
		if errors.As(err, &addrErr) || errors.As(err, &dnsErr) {
			// a bad -bind value, not a busy port: the next port fails the same way
			a.Detach()
			return nil, fmt.Errorf("cannot listen on %q: %w", bind, err)
		}
		if try == 0 {
			a.Log.Printf("port %d is in use by another program — trying the next ones", port)
		}
		port++
	}
	if ln == nil {
		a.Detach() // never stop a server we merely adopted
		return nil, fmt.Errorf("no free port from %d to %d: %w", opts.Port, port, err)
	}

	// The other servers of the panel (multi-server), each with its own
	// supervisor, scheduler and RCon.
	fleet := app.OpenFleet(a)
	for _, srvApp := range fleet.Apps() {
		app.ClaimOwner(srvApp.ManagerDir, a.ManagerDir, port)
		srvApp.Mode = opts.Mode
	}
	if n := len(fleet.Members()); n > 1 {
		a.Log.Printf("multi-server: %d servers in the panel", n)
	}

	srv := web.New(fleet, bind, port)
	ctx, cancel := context.WithCancel(context.Background())
	inst := &Instance{App: a, Fleet: fleet, Server: srv, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		URL: "http://" + net.JoinHostPort(DisplayHost(bind), strconv.Itoa(port)) + "/", stopServer: true, port: port}
	a.RequestExit = inst.RequestExit
	srv.OnServerAdded = func(added *app.App) {
		app.ClaimOwner(added.ManagerDir, a.ManagerDir, port)
		added.Mode = opts.Mode
		added.RequestExit = inst.RequestExit
	}
	for _, srvApp := range fleet.Apps() {
		srvApp.RequestExit = inst.RequestExit
	}
	go func() {
		defer close(inst.done)
		if err := srv.Serve(ctx, ln); err != nil {
			a.Log.Printf("web server stopped: %v", err)
		}
	}()
	a.Log.Printf("panel: %s", inst.URL)
	inst.bind = bind
	if a.Config.Exposure == "internet" && !srv.LoginEnabled() {
		a.Log.Printf("WARNING: the panel is set to Internet but has no login — until one is set (Settings → Access & security), only devices on the local network can open it.")
	}
	return inst, nil
}

// Context is cancelled when the manager should exit.
func (i *Instance) Context() context.Context { return i.ctx }

// PanelURL is the address to open on THIS computer. With a panel login and
// "don't ask on this computer" on, it carries a fresh one-time sign-in token;
// otherwise it is the plain URL. Call it per window/tab opened.
func (i *Instance) PanelURL() string {
	if i.Server == nil {
		return i.URL
	}
	return i.Server.LocalLoginURL(i.URL)
}

// ResetLogin turns the panel login off for the server in the working/exe
// directory (the -reset-login recovery flag for a forgotten password).
func ResetLogin() (string, error) {
	dir, err := ServerDir()
	if err != nil {
		return "", err
	}
	managerDir := filepath.Join(dir, ".dayz-manager")
	return managerDir, web.ResetLogin(managerDir)
}

// RequestExit asks the manager to exit; stopServer decides whether the DayZ
// server is stopped too (otherwise it keeps running and is picked up by the
// next manager start).
func (i *Instance) RequestExit(stopServer bool) {
	i.stopOnce.Do(func() {
		i.stopServer = stopServer
		i.cancel()
	})
}

// Shutdown stops the web server and — unless the exit asked to keep it — the
// DayZ server. Safe to call once the context is done.
func (i *Instance) Shutdown() {
	i.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = i.Server.Stop(ctx)
	apps := []*app.App{i.App}
	if i.Fleet != nil {
		apps = i.Fleet.Apps()
	}
	// Every server stops (or is let go) at once: one by one, a slow graceful
	// shutdown would hold the others.
	var wg sync.WaitGroup
	for _, a := range apps {
		wg.Add(1)
		go func(a *app.App) {
			defer wg.Done()
			if i.stopServer {
				_ = a.Close()
			} else {
				a.Detach()
				if a.ServerIsRunning() {
					a.Log.Printf("manager exited — the DayZ server keeps running and will be picked up on the next start")
				}
			}
			app.ReleaseOwner(a.ManagerDir)
		}(a)
	}
	wg.Wait()
}

// RunningServers counts the DayZ servers of the panel that are running.
func (i *Instance) RunningServers() int {
	n := 0
	apps := []*app.App{i.App}
	if i.Fleet != nil {
		apps = i.Fleet.Apps()
	}
	for _, a := range apps {
		if a.ServerIsRunning() {
			n++
		}
	}
	return n
}

// AnyServerRunning reports whether any DayZ server of the panel is running —
// closing the window then asks whether to stop them.
func (i *Instance) AnyServerRunning() bool {
	if i.Fleet != nil {
		return i.Fleet.AnyRunning()
	}
	return i.App.ServerIsRunning()
}

// ExitOnSignal wires Ctrl+C / SIGTERM (and the console window's close button,
// which Windows delivers as SIGTERM) to a normal exit that stops the server.
func (i *Instance) ExitOnSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		i.RequestExit(true)
	}()
}

// HasDesktop reports whether a graphical session is available.
func HasDesktop() bool { return hasDesktop() }

// ShortcutsInstalled reports whether -install already added the manager to
// the Start menu / applications menu.
func ShortcutsInstalled() bool { return desktopInstalled() }

// Fatal logs and exits (used before a logger exists).
func Fatal(format string, a ...interface{}) {
	log.Fatalf(format, a...)
}

var errNoBrowser = errors.New("no app-mode capable browser found")
