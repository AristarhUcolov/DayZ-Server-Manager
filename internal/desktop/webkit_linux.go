// Copyright (c) 2026 Aristarh Ucolov.
//go:build linux

package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"

	"dayzmanager/internal/launcher"
)

// The Linux desktop build shows the panel in a real application window —
// WebKitGTK, the engine GNOME apps use — the way the Windows build uses
// WebView2. The libraries are opened at run time (no cgo, one static binary
// for every distribution): a desktop without WebKitGTK simply gets the
// browser window it got before. On Debian/Ubuntu/Mint the package is
// libwebkit2gtk-4.1-0 (or -4.0-37 on older releases).

func init() {
	// GTK and every window it owns live on the main OS thread, which only
	// the main goroutine is guaranteed to run on when locked from init.
	runtime.LockOSThread()
}

var errNoWebKit = errors.New("WebKitGTK is not available")

type gtkAPI struct {
	initCheck     func(argc, argv uintptr) bool
	windowNew     func(kind int32) uintptr
	setTitle      func(win uintptr, title string)
	setDefSize    func(win uintptr, w, h int32)
	setIconFile   func(win uintptr, path string, gerr uintptr) bool
	present       func(win uintptr)
	containerAdd  func(container, widget uintptr)
	showAll       func(w uintptr)
	destroy       func(w uintptr)
	main          func()
	mainQuit      func()
	webViewNew    func() uintptr
	loadURI       func(view uintptr, uri string)
	runJS         func(view uintptr, script string, cancellable, cb, data uintptr)
	signal        func(instance uintptr, signal string, handler, data, destroyData uintptr, flags int32) uint64
	idleAdd       func(fn, data uintptr) uint32
	navDecision   func(decision uintptr) uintptr // webkit_navigation_policy_decision_get_navigation_action
	actionRequest func(action uintptr) uintptr   // webkit_navigation_action_get_request
	requestURI    func(req uintptr) string       // webkit_uri_request_get_uri
	ignore        func(decision uintptr)         // webkit_policy_decision_ignore
}

var (
	gtkOnce sync.Once
	gtk     *gtkAPI
	gtkErr  error
)

// openLib tries each soname until one loads.
func openLib(names ...string) (uintptr, error) {
	var last error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		last = err
	}
	return 0, last
}

// loadGTK opens GTK 3 and WebKitGTK and binds the functions the window uses.
// Any missing piece means "no app window here", never a crash.
func loadGTK() (api *gtkAPI, err error) {
	defer func() {
		if r := recover(); r != nil {
			api, err = nil, errNoWebKit
		}
	}()
	gtkLib, err := openLib("libgtk-3.so.0")
	if err != nil {
		return nil, errNoWebKit
	}
	wk, err := openLib("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
	if err != nil {
		return nil, errNoWebKit
	}
	gobj, err := openLib("libgobject-2.0.so.0")
	if err != nil {
		return nil, errNoWebKit
	}
	glib, err := openLib("libglib-2.0.so.0")
	if err != nil {
		return nil, errNoWebKit
	}
	a := &gtkAPI{}
	purego.RegisterLibFunc(&a.initCheck, gtkLib, "gtk_init_check")
	purego.RegisterLibFunc(&a.windowNew, gtkLib, "gtk_window_new")
	purego.RegisterLibFunc(&a.setTitle, gtkLib, "gtk_window_set_title")
	purego.RegisterLibFunc(&a.setDefSize, gtkLib, "gtk_window_set_default_size")
	purego.RegisterLibFunc(&a.setIconFile, gtkLib, "gtk_window_set_icon_from_file")
	purego.RegisterLibFunc(&a.present, gtkLib, "gtk_window_present")
	purego.RegisterLibFunc(&a.containerAdd, gtkLib, "gtk_container_add")
	purego.RegisterLibFunc(&a.showAll, gtkLib, "gtk_widget_show_all")
	purego.RegisterLibFunc(&a.destroy, gtkLib, "gtk_widget_destroy")
	purego.RegisterLibFunc(&a.main, gtkLib, "gtk_main")
	purego.RegisterLibFunc(&a.mainQuit, gtkLib, "gtk_main_quit")
	purego.RegisterLibFunc(&a.webViewNew, wk, "webkit_web_view_new")
	purego.RegisterLibFunc(&a.loadURI, wk, "webkit_web_view_load_uri")
	purego.RegisterLibFunc(&a.runJS, wk, "webkit_web_view_run_javascript")
	purego.RegisterLibFunc(&a.navDecision, wk, "webkit_navigation_policy_decision_get_navigation_action")
	purego.RegisterLibFunc(&a.actionRequest, wk, "webkit_navigation_action_get_request")
	purego.RegisterLibFunc(&a.requestURI, wk, "webkit_uri_request_get_uri")
	purego.RegisterLibFunc(&a.ignore, wk, "webkit_policy_decision_ignore")
	purego.RegisterLibFunc(&a.signal, gobj, "g_signal_connect_data")
	purego.RegisterLibFunc(&a.idleAdd, glib, "g_idle_add")
	return a, nil
}

// ---------------------------------------------------------------- dispatch

var (
	queueMu sync.Mutex
	queue   []func()
	idleCB  uintptr
)

// dispatch runs fn on the GTK thread (g_idle_add is safe from any thread).
func dispatch(fn func()) {
	queueMu.Lock()
	queue = append(queue, fn)
	queueMu.Unlock()
	gtk.idleAdd(idleCB, 0)
}

func runQueue(_ uintptr) uintptr {
	queueMu.Lock()
	q := queue
	queue = nil
	queueMu.Unlock()
	for _, fn := range q {
		fn()
	}
	return 0 // G_SOURCE_REMOVE
}

// ---------------------------------------------------------------- window

type gtkShell struct {
	inst    *launcher.Instance
	base    string // the panel's origin: navigations elsewhere open in the browser
	win     uintptr
	view    uintptr
	closing bool
}

var theGTK *gtkShell

// runWebKit shows the panel in a WebKitGTK window and returns when it closes
// (or the manager exits). errNoWebKit when this desktop cannot show one.
func runWebKit(inst *launcher.Instance, url string) error {
	if os.Geteuid() == 0 || launcher.DesktopUserDiffers() {
		// GTK as root on the user's display is refused by most desktops;
		// the browser path starts the browser as the desktop user instead.
		return errNoWebKit
	}
	gtkOnce.Do(func() { gtk, gtkErr = loadGTK() })
	if gtkErr != nil {
		return gtkErr
	}
	// The DMA-BUF renderer shows a blank window on some GPU drivers and VMs;
	// the panel needs no GPU path.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	if !gtk.initCheck(0, 0) {
		return errNoWebKit
	}
	idleCB = purego.NewCallback(runQueue)

	s := &gtkShell{inst: inst, base: origin(url)}
	theGTK = s
	s.win = gtk.windowNew(0) // GTK_WINDOW_TOPLEVEL
	gtk.setTitle(s.win, launcher.AppName+" v"+launcher.Version)
	gtk.setDefSize(s.win, 1360, 880)
	if icon := iconFile(); icon != "" {
		gtk.setIconFile(s.win, icon, 0)
	}
	s.view = gtk.webViewNew()
	gtk.containerAdd(s.win, s.view)

	gtk.signal(s.win, "delete-event", purego.NewCallback(onDelete), 0, 0, 0)
	gtk.signal(s.win, "destroy", purego.NewCallback(onDestroy), 0, 0, 0)
	gtk.signal(s.view, "decide-policy", purego.NewCallback(onDecidePolicy), 0, 0, 0)

	gtk.loadURI(s.view, url)
	gtk.showAll(s.win)

	if inst != nil {
		inst.App.ShowWindow = func() bool {
			dispatch(func() { gtk.present(s.win) })
			return true
		}
		go func() {
			<-inst.Context().Done()
			dispatch(s.quit)
		}()
	}
	gtk.main()
	return nil
}

func (s *gtkShell) quit() {
	if s.closing {
		return
	}
	s.closing = true
	gtk.destroy(s.win) // → destroy → gtk_main_quit
}

// onDelete is the window's close button. With a DayZ server running the page
// asks what to do (keep it running, stop it, or cancel) through the same
// dialog as the panel's Exit button; otherwise the manager exits.
func onDelete(_, _, _ uintptr) uintptr {
	s := theGTK
	if s == nil || s.inst == nil || s.closing {
		return 0 // let it close
	}
	if !s.inst.AnyServerRunning() {
		s.inst.RequestExit(true)
		return 1
	}
	gtk.runJS(s.view, "window.dzmAskExit ? window.dzmAskExit() : (location.hash = '#settings')", 0, 0, 0)
	return 1 // keep the window until the page's dialog answers
}

func onDestroy(_, _ uintptr) uintptr {
	gtk.mainQuit()
	return 0
}

// Policy decision types (WebKitPolicyDecisionType).
const (
	policyNavigation = 0
	policyNewWindow  = 1
)

// onDecidePolicy keeps the window on the panel: links to other sites (the
// guide's sources, sponsors, GitHub) open in the user's browser.
func onDecidePolicy(_, decision, kind, _ uintptr) uintptr {
	s := theGTK
	if s == nil || (kind != policyNavigation && kind != policyNewWindow) {
		return 0
	}
	action := gtk.navDecision(decision)
	if action == 0 {
		return 0
	}
	req := gtk.actionRequest(action)
	if req == 0 {
		return 0
	}
	uri := gtk.requestURI(req)
	if uri == "" || strings.HasPrefix(uri, s.base) || strings.HasPrefix(uri, "about:") || strings.HasPrefix(uri, "blob:") || strings.HasPrefix(uri, "data:") {
		return 0
	}
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "mailto:") {
		gtk.ignore(decision)
		openExternal(uri)
		return 1
	}
	return 0
}

// openExternal hands a link to the desktop's browser.
func openExternal(uri string) {
	cmd := exec.Command("xdg-open", uri)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
		return
	}
	launcher.OpenBrowser(uri)
}

// origin is scheme://host:port/ of url.
func origin(url string) string {
	i := strings.Index(url, "://")
	if i < 0 {
		return url
	}
	if j := strings.Index(url[i+3:], "/"); j >= 0 {
		return url[:i+3+j+1]
	}
	return url + "/"
}

// iconFile writes the app icon where GTK can read it ("" when it cannot).
func iconFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(dir, "dayz-server-manager", "icon.svg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(p, launcher.AppIconSVG(), 0o644); err != nil {
		return ""
	}
	return p
}
