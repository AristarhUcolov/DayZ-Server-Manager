// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"dayzmanager/internal/app"
)

// autostartFile is the XDG autostart entry for serverDir (in the desktop
// user's home, also when the manager runs through sudo).
func autostartFile(serverDir string) (string, int, int) {
	home, uid, gid := desktopUser()
	if home == "" {
		return "", uid, gid
	}
	conf := filepath.Join(home, ".config")
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" && os.Geteuid() != 0 {
		conf = v
	}
	return filepath.Join(conf, "autostart", autostartID(serverDir)+".desktop"), uid, gid
}

// desktopQuote quotes one Exec= argument (Desktop Entry spec).
func desktopQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
}

// AutostartInfo reports whether the manager for serverDir starts at the
// desktop login, plus a systemd unit for servers without a desktop.
func AutostartInfo(serverDir string) app.AutostartInfo {
	info := app.AutostartInfo{Supported: runtime.GOOS == "linux", Kind: "xdg", Desktop: hasDesktop()}
	path, _, _ := autostartFile(serverDir)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			info.Enabled = true
		}
		info.Detail = path
	}
	if exe, err := executable(); err == nil {
		info.Systemd = systemdUnit(exe, serverDir)
	}
	return info
}

// systemdUnit is a service that runs the manager at boot on a server without
// a desktop session.
func systemdUnit(exe, serverDir string) string {
	who := ""
	if name := sudoUser(); name != "" {
		who = name
	} else if u, err := user.Current(); err == nil {
		who = u.Username
	}
	args := []string{exe}
	args = append(args, autostartArgs(serverDir, 0)...)
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'\\") {
			args[i] = desktopQuote(a)
		}
	}
	lines := []string{
		"[Unit]",
		"Description=" + AppName,
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=" + serverDir,
		"ExecStart=" + strings.Join(args, " "),
		"Restart=on-failure",
	}
	if who != "" && who != "root" {
		lines = append(lines, "User="+who)
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n")
}

// SetAutostart adds or removes the desktop-login entry.
func SetAutostart(serverDir string, port int, on bool) error {
	path, uid, gid := autostartFile(serverDir)
	if path == "" {
		return os.ErrNotExist
	}
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	_ = os.Chmod(exe, 0o755)
	exec := []string{desktopQuote(exe)}
	for _, a := range autostartArgs(serverDir, port) {
		exec = append(exec, desktopQuote(a))
	}
	entry := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=" + AppName,
		"Comment=Starts the DayZ Server Manager at login",
		"Exec=" + strings.Join(exec, " "),
		"Path=" + serverDir,
		"Icon=" + desktopID,
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return err
	}
	if uid >= 0 {
		_ = os.Chown(path, uid, gid)
		_ = os.Chown(filepath.Dir(path), uid, gid)
	}
	return nil
}
