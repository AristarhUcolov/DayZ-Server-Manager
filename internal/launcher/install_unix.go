// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

// Linux desktop integration. The usual reasons "the file does not open" on a
// Linux desktop:
//
//   - the browser saved the download without the executable bit: the file
//     manager then opens it as a document, or says there is no application
//     for it;
//   - GNOME Files (and others) do not run programs on a double click at all;
//   - started from a file manager, any error is printed to a terminal nobody
//     sees.
//
// -install puts a launcher in the applications menu (and on the desktop), so
// the manager starts like any other app; MessageBox shows errors and the panel
// address as a window when there is no terminal.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// appIconSVG is the manager's icon (the same as the panel's favicon).
const appIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="256" height="256">` +
	`<rect width="24" height="24" rx="6" fill="#ff7a2b"/>` +
	`<g fill="none" stroke="#14080a" stroke-width="1.8" stroke-linecap="round">` +
	`<rect x="5.5" y="6" width="13" height="5" rx="1.5"/><rect x="5.5" y="13" width="13" height="5" rx="1.5"/>` +
	`<circle cx="8.5" cy="8.5" r=".2"/><circle cx="8.5" cy="15.5" r=".2"/></g></svg>`

// AppIconSVG is the manager's icon, for windows that need it as a file.
func AppIconSVG() []byte { return []byte(appIconSVG) }

const desktopID = "dayz-server-manager"

// IsTerminal reports whether the manager was started from a terminal (its
// output is seen) — not from a file manager or an applications menu.
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// MessageBox shows a message in a window where the desktop offers one
// (zenity, kdialog, xmessage, else a notification), and always prints it.
// Blocks until the window is closed.
func MessageBox(title, body string) {
	fmt.Println(title + ": " + body)
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display dialog %q with title %q buttons {\"OK\"}", body, title)
		_ = exec.Command("osascript", "-e", script).Run()
		return
	}
	if !hasDesktop() {
		return
	}
	cands := [][]string{
		// --no-markup: zenity reads the text as Pango markup, and an error
		// with "<" or "&" in it would show as an empty window.
		{"zenity", "--info", "--no-wrap", "--no-markup", "--title=" + title, "--text=" + body},
		{"kdialog", "--title", title, "--msgbox", body},
		{"yad", "--info", "--title=" + title, "--text=" + body, "--button=OK"},
		{"xmessage", "-center", title + "\n\n" + body},
		{"notify-send", "-a", title, title, body},
	}
	for _, c := range cands {
		p, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		if err := DesktopCommand(p, c[1:]...).Run(); err == nil {
			return
		}
	}
}

// desktopUser is who the launcher is installed for: the desktop user (the
// one who ran sudo, when the manager runs as root through it).
func desktopUser() (home string, uid, gid int) {
	uid, gid = -1, -1
	home = DesktopHome()
	if os.Geteuid() == 0 {
		if name := sudoUser(); name != "" {
			if u, err := user.Lookup(name); err == nil {
				home = u.HomeDir
				uid, _ = strconv.Atoi(u.Uid)
				gid, _ = strconv.Atoi(u.Gid)
			}
		}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return
}

func dataHome(home string) string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" && os.Geteuid() != 0 {
		return v
	}
	return filepath.Join(home, ".local", "share")
}

// desktopEntry is the .desktop file for this build of the manager.
func desktopEntry(exe, mode, appName string) string {
	terminal := "false"
	comment := "DayZ dedicated server manager — panel in its own window"
	if mode == "web" {
		// The browser build lives in a console window: logs and the address.
		terminal = "true"
		comment = "DayZ dedicated server manager — opens the panel in the browser"
	}
	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(exe) + `"`
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=" + appName,
		"Comment=" + comment,
		"Exec=" + quoted,
		"Path=" + filepath.Dir(exe),
		"Icon=" + desktopID,
		"Terminal=" + terminal,
		"Categories=Game;Utility;",
		"Keywords=DayZ;server;manager;",
		"StartupNotify=true",
		"StartupWMClass=DayZServerManager",
		"",
	}, "\n")
}

// InstallDesktop adds the manager to the applications menu (and to the
// desktop when there is one) and makes sure the program file is executable.
// mode is "panel" or "web". Returns what was written.
func InstallDesktop(mode string) ([]string, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("-install is for Linux desktops")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	_ = os.Chmod(exe, 0o755)
	home, uid, gid := desktopUser()
	if home == "" {
		return nil, errors.New("no home folder to install into")
	}
	name := AppName
	file := desktopID + ".desktop"
	if mode == "web" {
		name += " (browser)"
		file = desktopID + "-web.desktop"
	}
	entry := desktopEntry(exe, mode, name)
	share := dataHome(home)
	var written []string
	write := func(path, content string, perm os.FileMode) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), perm); err != nil {
			return err
		}
		if uid >= 0 {
			_ = os.Chown(path, uid, gid)
			_ = os.Chown(filepath.Dir(path), uid, gid)
		}
		written = append(written, path)
		return nil
	}
	if err := write(filepath.Join(share, "icons", "hicolor", "scalable", "apps", desktopID+".svg"), appIconSVG, 0o644); err != nil {
		return written, err
	}
	menu := filepath.Join(share, "applications", file)
	if err := write(menu, entry, 0o755); err != nil {
		return written, err
	}
	// A shortcut on the desktop, marked trusted so GNOME starts it on a
	// double click instead of asking or opening it as text.
	if desk := desktopDir(home); desk != "" {
		shortcut := filepath.Join(desk, file)
		if err := write(shortcut, entry, 0o755); err == nil {
			if p, err := exec.LookPath("gio"); err == nil {
				_ = DesktopCommand(p, "set", shortcut, "metadata::trusted", "true").Run()
			}
		}
	}
	for _, tool := range [][]string{
		{"update-desktop-database", filepath.Join(share, "applications")},
		{"gtk-update-icon-cache", "-f", "-t", filepath.Join(share, "icons", "hicolor")},
	} {
		if p, err := exec.LookPath(tool[0]); err == nil {
			_ = DesktopCommand(p, tool[1:]...).Run()
		}
	}
	return written, nil
}

// UninstallDesktop removes what InstallDesktop wrote.
func UninstallDesktop() ([]string, error) {
	home, _, _ := desktopUser()
	share := dataHome(home)
	var removed []string
	paths := []string{
		filepath.Join(share, "applications", desktopID+".desktop"),
		filepath.Join(share, "applications", desktopID+"-web.desktop"),
		filepath.Join(share, "icons", "hicolor", "scalable", "apps", desktopID+".svg"),
	}
	if desk := desktopDir(home); desk != "" {
		paths = append(paths, filepath.Join(desk, desktopID+".desktop"), filepath.Join(desk, desktopID+"-web.desktop"))
	}
	for _, p := range paths {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed, nil
}

// desktopDir is the user's desktop folder (XDG user dirs), "" when absent.
func desktopDir(home string) string {
	if data, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "XDG_DESKTOP_DIR="); ok {
				v = strings.Trim(v, `"`)
				v = strings.Replace(v, "$HOME", home, 1)
				if st, err := os.Stat(v); err == nil && st.IsDir() && v != home {
					return v
				}
			}
		}
	}
	d := filepath.Join(home, "Desktop")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return ""
}

// desktopInstalled reports whether -install already put a launcher in the
// applications menu.
func desktopInstalled() bool {
	home, _, _ := desktopUser()
	share := dataHome(home)
	for _, f := range []string{desktopID + ".desktop", desktopID + "-web.desktop"} {
		if _, err := os.Stat(filepath.Join(share, "applications", f)); err == nil {
			return true
		}
	}
	return false
}
