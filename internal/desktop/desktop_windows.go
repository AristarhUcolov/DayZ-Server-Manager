// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"

	"dayzmanager/internal/launcher"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW     = user32.NewProc("CallWindowProcW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pMessageBoxW         = user32.NewProc("MessageBoxW")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pLoadImageW          = user32.NewProc("LoadImageW")
	pLoadIconW           = user32.NewProc("LoadIconW")
	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pSetDpiAwareness     = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	pShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pGetWindowPlacement  = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement  = user32.NewProc("SetWindowPlacement")
	pMonitorFromRect     = user32.NewProc("MonitorFromRect")
	pSetAppUserModelID   = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
)

// appUserModelID groups the panel's windows and its pinned taskbar icon as one
// app, whichever folder the exe runs from.
const appUserModelID = "AristarhUcolov.DayZServerManager"

// webView2Download is Microsoft's Evergreen bootstrapper for the WebView2
// runtime (built into Windows 11, missing on some Windows 10/Server installs).
const webView2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

const (
	wmClose       = 0x0010
	wmEndSession  = 0x0016
	wmNull        = 0x0000
	wmApp         = 0x8000
	wmTray        = wmApp + 7 // not WM_APP itself: go-webview2 uses that for Dispatch
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	swHide        = 0
	swShow        = 5
	swRestore     = 9
	gwlpWndProc   = ^uintptr(3) // -4
	mbYesNoCancel = 0x3
	mbYesNo       = 0x4
	mbIconQ       = 0x20
	mbIconInfo    = 0x40
	mbIconWarn    = 0x30
	idYes         = 6
	idNo          = 7
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	nifInfo       = 0x10
	niifInfo      = 0x1
	mfString      = 0x0
	mfSeparator   = 0x800
	tpmReturnCmd  = 0x100
	tpmRightBtn   = 0x2

	cmdOpen    = 1
	cmdBrowser = 2
	cmdExit    = 3
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

// windowPlacement is WINDOWPLACEMENT.
type windowPlacement struct {
	Length   uint32
	Flags    uint32
	ShowCmd  uint32
	PtMin    point
	PtMax    point
	RcNormal rect
}

// savedWindow is where the window was when the panel last closed.
type savedWindow struct {
	X, Y, W, H int32
	Maximized  bool
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

func init() {
	// WebView2 and every window it owns must live on one OS thread.
	runtime.LockOSThread()
}

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func messageBox(hwnd uintptr, textS, caption string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(u16(textS))), uintptr(unsafe.Pointer(u16(caption))), flags)
	return int(r)
}

func appIcon() uintptr {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	// goversioninfo embeds the exe icon as resource 1.
	if h, _, _ := pLoadImageW.Call(hinst, 1, 1, 0, 0, 0x40|0x8000); h != 0 { // LR_DEFAULTSIZE|LR_SHARED
		return h
	}
	h, _, _ := pLoadIconW.Call(0, 32512) // IDI_APPLICATION
	return h
}

// shell is the desktop runtime state (all touched on the UI thread).
type shell struct {
	inst     *launcher.Instance
	url      string
	w        webview2.WebView
	hwnd     uintptr // main window
	origProc uintptr
	trayWnd  uintptr
	trayOn   bool
	tipShown bool
	quitOnce sync.Once
	gone     atomic.Bool // the window's message loop has ended
}

var theShell *shell

// Run shows the panel. attachURL is set when another manager already runs —
// the window then just views it and closing it only closes the window.
func Run(inst *launcher.Instance, attachURL string) error {
	url := attachURL
	if inst != nil {
		url = inst.PanelURL()
	}
	if _, err := webviewloader.GetInstalledVersion(); err != nil {
		return runWithoutWebView(inst, url)
	}
	if pSetDpiAwareness.Find() == nil {
		pSetDpiAwareness.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
	scale := 1.0
	if pGetDpiForSystem.Find() == nil {
		if dpi, _, _ := pGetDpiForSystem.Call(); dpi > 0 {
			scale = float64(dpi) / 96
		}
	}
	if pSetAppUserModelID.Find() == nil {
		pSetAppUserModelID.Call(uintptr(unsafe.Pointer(u16(appUserModelID))))
	}
	dataDir := ""
	if inst != nil {
		dataDir = inst.App.ManagerDir + `\webview2`
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  dataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  launcher.AppName + " v" + launcher.Version,
			Width:  uint(1360 * scale),
			Height: uint(880 * scale),
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return runWithoutWebView(inst, url)
	}
	s := &shell{inst: inst, url: url, w: w, hwnd: uintptr(w.Window())}
	theShell = s
	w.SetSize(1000, 640, webview2.HintMin)
	s.restorePlacement()
	w.Navigate(url)
	if inst != nil && !StartHidden && !launcher.ShortcutsInstalled() && !s.flag("shortcuts-offered") {
		// Once: put the panel in the Start menu and on the desktop, like an
		// installed program. Asked after the page is up, not over a blank window.
		go func() {
			time.Sleep(5 * time.Second)
			w.Dispatch(s.offerShortcuts)
		}()
	}

	// Intercept the window's close button.
	s.origProc, _, _ = pSetWindowLongPtrW.Call(s.hwnd, gwlpWndProc, syscall.NewCallback(mainWndProc))

	if inst != nil {
		s.addTray()
		if StartHidden {
			// Started with Windows: only the tray icon, and one note where
			// the manager went.
			pShowWindow.Call(s.hwnd, swHide)
			s.tipShown = true
			s.balloon(text(inst, "desktop.tray.title"), text(inst, "desktop.tray.autostart"))
		}
		inst.App.ShowWindow = func() bool {
			if s.gone.Load() {
				return false // no window any more: the caller opens a new one
			}
			w.Dispatch(s.show)
			return true
		}
		// Any exit (tray, dialog, the panel's own Exit button) ends here.
		go func() {
			<-inst.Context().Done()
			w.Dispatch(s.quit)
		}()
	}
	w.Run()
	s.gone.Store(true)
	s.removeTray()
	return nil
}

func (s *shell) show() {
	if s.hwnd == 0 { // browser fallback: no window of our own
		launcher.OpenBrowser(panelURL(s.inst, s.url))
		return
	}
	if r, _, _ := pIsIconic.Call(s.hwnd); r != 0 {
		pShowWindow.Call(s.hwnd, swRestore)
	} else {
		pShowWindow.Call(s.hwnd, swShow)
	}
	pSetForegroundWindow.Call(s.hwnd)
}

func (s *shell) hide() {
	s.savePlacement()
	pShowWindow.Call(s.hwnd, swHide)
	if !s.tipShown {
		s.tipShown = true
		s.balloon(text(s.inst, "desktop.tray.title"), text(s.inst, "desktop.tray.hidden"))
	}
}

// quit leaves the message loop (UI thread only).
func (s *shell) quit() {
	s.quitOnce.Do(func() {
		if isWindowVisible(s.hwnd) {
			s.savePlacement()
		}
		s.removeTray()
		s.w.Terminate()
	})
}

// ---------------------------------------------------------------- window state

func isWindowVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := pIsWindowVisible.Call(hwnd)
	return r != 0
}

func (s *shell) placementFile() string {
	if s.inst == nil {
		return ""
	}
	return filepath.Join(s.inst.App.ManagerDir, "window.json")
}

// restorePlacement puts the window where it was last time — when that spot
// is still on a monitor (a screen may have been unplugged since).
func (s *shell) restorePlacement() {
	path := s.placementFile()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var p savedWindow
	if json.Unmarshal(data, &p) != nil || p.W < 400 || p.H < 300 {
		return
	}
	rc := rect{p.X, p.Y, p.X + p.W, p.Y + p.H}
	if mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&rc)), 0); mon == 0 { // MONITOR_DEFAULTTONULL
		return
	}
	wp := windowPlacement{ShowCmd: 1, RcNormal: rc} // SW_SHOWNORMAL
	if p.Maximized {
		wp.ShowCmd = 3 // SW_SHOWMAXIMIZED
	}
	wp.Length = uint32(unsafe.Sizeof(wp))
	pSetWindowPlacement.Call(s.hwnd, uintptr(unsafe.Pointer(&wp)))
}

func (s *shell) savePlacement() {
	path := s.placementFile()
	if path == "" || s.hwnd == 0 {
		return
	}
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := pGetWindowPlacement.Call(s.hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return
	}
	p := savedWindow{X: wp.RcNormal.Left, Y: wp.RcNormal.Top,
		W: wp.RcNormal.Right - wp.RcNormal.Left, H: wp.RcNormal.Bottom - wp.RcNormal.Top,
		Maximized: wp.ShowCmd == 3}
	if data, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// flag reports whether a one-time question was already asked (and marks it).
func (s *shell) flag(name string) bool {
	if s.inst == nil {
		return true
	}
	p := filepath.Join(s.inst.App.ManagerDir, name)
	if _, err := os.Stat(p); err == nil {
		return true
	}
	_ = os.WriteFile(p, []byte(time.Now().Format(time.RFC3339)), 0o644)
	return false
}

// offerShortcuts asks once whether to add Start menu and desktop shortcuts.
func (s *shell) offerShortcuts() {
	if messageBox(s.hwnd, text(s.inst, "desktop.shortcuts.offer"), launcher.AppName, mbYesNo|mbIconQ) != idYes {
		return
	}
	if _, err := launcher.InstallDesktop("panel"); err != nil {
		messageBox(s.hwnd, err.Error(), launcher.AppName, mbIconWarn)
		return
	}
	s.balloon(launcher.AppName, text(s.inst, "desktop.shortcuts.done"))
}

// requestExit asks whether to stop the DayZ server (when it runs) and exits.
func (s *shell) requestExit() {
	if s.inst == nil {
		s.quit()
		return
	}
	stop := true
	if s.inst.AnyServerRunning() {
		switch messageBox(s.hwnd, runningText(s.inst, "desktop.exit.running"), launcher.AppName, mbYesNoCancel|mbIconQ) {
		case idYes:
			stop = true
		case idNo:
			stop = false
		default:
			return
		}
	}
	s.inst.RequestExit(stop) // the Context watcher calls quit()
}

func mainWndProc(hwnd, m, wp, lp uintptr) uintptr {
	s := theShell
	if s != nil && s.inst != nil && m == wmEndSession && wp != 0 {
		// Windows signs out or shuts down and ends this process when the
		// message returns: note it, so the next start does not report it as
		// a problem.
		s.inst.MarkSessionEnd()
	}
	if s != nil && m == wmClose {
		if s.inst == nil {
			s.quit()
			return 0
		}
		if !s.inst.AnyServerRunning() {
			s.inst.RequestExit(true)
			return 0
		}
		// Server running: keep it (tray), stop it and exit, or go back.
		switch messageBox(hwnd, runningText(s.inst, "desktop.close.running"), launcher.AppName, mbYesNoCancel|mbIconQ) {
		case idYes:
			s.hide()
		case idNo:
			s.inst.RequestExit(true)
		}
		return 0
	}
	r, _, _ := pCallWindowProcW.Call(s.origProc, hwnd, m, wp, lp)
	return r
}

// ---------------------------------------------------------------- tray

func trayWndProc(hwnd, m, wp, lp uintptr) uintptr {
	s := theShell
	if s != nil && m == wmTray {
		switch lp & 0xffff {
		case wmLButtonUp, wmLButtonDbl:
			s.show()
		case wmRButtonUp:
			s.menu()
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func (s *shell) addTray() {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := u16("DayZServerManagerTray")
	wc := wndClassEx{LpfnWndProc: syscall.NewCallback(trayWndProc), HInstance: hinst, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// A plain hidden top-level window (message-only windows get no tray
	// callbacks on some Windows builds).
	s.trayWnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("DZM tray"))),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if s.trayWnd == 0 {
		return
	}
	nid := s.nid()
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTray
	nid.HIcon = appIcon()
	copy(nid.SzTip[:], syscall.StringToUTF16(launcher.AppName))
	if r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r != 0 {
		s.trayOn = true
	}
}

func (s *shell) nid() notifyIconData {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = s.trayWnd
	nid.UID = 1
	return nid
}

func (s *shell) balloon(title, body string) {
	if !s.trayOn {
		return
	}
	nid := s.nid()
	nid.UFlags = nifInfo
	nid.DwInfoFlags = niifInfo
	copy(nid.SzInfoTitle[:63], syscall.StringToUTF16(title))
	copy(nid.SzInfo[:255], syscall.StringToUTF16(body))
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func (s *shell) removeTray() {
	if s.trayOn {
		nid := s.nid()
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		s.trayOn = false
	}
	if s.trayWnd != 0 {
		pDestroyWindow.Call(s.trayWnd)
		s.trayWnd = 0
	}
}

func (s *shell) menu() {
	hm, _, _ := pCreatePopupMenu.Call()
	if hm == 0 {
		return
	}
	defer pDestroyMenu.Call(hm)
	add := func(id uintptr, label string) {
		pAppendMenuW.Call(hm, mfString, id, uintptr(unsafe.Pointer(u16(label))))
	}
	add(cmdOpen, text(s.inst, "desktop.tray.open"))
	add(cmdBrowser, text(s.inst, "desktop.tray.browser"))
	pAppendMenuW.Call(hm, mfSeparator, 0, 0)
	add(cmdExit, text(s.inst, "desktop.tray.exit"))
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(s.trayWnd) // required, or the menu never closes
	cmd, _, _ := pTrackPopupMenu.Call(hm, tpmReturnCmd|tpmRightBtn, uintptr(pt.X), uintptr(pt.Y), 0, s.trayWnd, 0)
	pPostMessageW.Call(s.trayWnd, wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		s.show()
	case cmdBrowser:
		launcher.OpenBrowser(panelURL(s.inst, s.url))
	case cmdExit:
		s.requestExit()
	}
}

// ---------------------------------------------------------------- fallback

// runWithoutWebView is used when the WebView2 runtime is missing (Windows
// Server 2019/2022 and some stripped Windows 10 installs): the panel opens as
// an Edge/Chrome app window, or in the default browser, and a tray icon lets
// the admin reopen it or exit.
func runWithoutWebView(inst *launcher.Instance, url string) error {
	if inst == nil {
		if !runAppBrowser(nil, url) {
			launcher.OpenBrowser(url)
		}
		return nil
	}
	s := &shell{inst: inst, url: url}
	theShell = s
	s.addTray()
	if StartHidden {
		// Started with Windows: the tray icon only — no browser, no question.
		s.balloon(text(inst, "desktop.tray.title"), text(inst, "desktop.tray.autostart"))
	} else {
		s.balloon(text(inst, "desktop.tray.title"), text(inst, "desktop.noWebView"))
		// The app window needs Microsoft's WebView2 runtime: offer it (once a
		// "No" is said, the panel just keeps using the browser).
		if !s.flag("webview2-declined") {
			if messageBox(0, text(inst, "desktop.webview.offer"), launcher.AppName, mbYesNo|mbIconQ) == idYes {
				_ = os.Remove(filepath.Join(inst.App.ManagerDir, "webview2-declined"))
				launcher.OpenBrowser(webView2Download)
			}
		}
		go func() {
			if !runAppBrowser(inst, url) {
				launcher.OpenBrowser(url)
			}
		}()
	}
	inst.App.ShowWindow = func() bool { launcher.OpenBrowser(panelURL(inst, url)); return true }
	// Tray message loop until the manager exits.
	go func() {
		<-inst.Context().Done()
		pPostMessageW.Call(s.trayWnd, wmClose, 0, 0)
	}()
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		if m.Hwnd == s.trayWnd && m.Message == wmClose {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	s.removeTray()
	return nil
}

// Alert shows a native error box (used before the window exists).
func Alert(title, body string) {
	messageBox(0, body, title, mbIconWarn)
}

var _ = mbYesNo
var _ = mbIconInfo
var _ = pPostQuitMessage
