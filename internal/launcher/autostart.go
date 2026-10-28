// Copyright (c) 2026 Aristarh Ucolov.
package launcher

// Starting the manager together with the computer. The entry runs this exe
// with -autostart (no browser tab, the desktop window stays in the tray) and
// -dir (the server folder: at sign-in the working directory is not it).
// Windows: a value under HKCU\...\CurrentVersion\Run. Linux: an XDG autostart
// file for the desktop login, and a ready systemd unit for servers without a
// desktop. One entry per server folder, so two managers for two folders can
// both start.

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dirOverride is the -dir flag: the server folder, whatever the working
// directory and the exe's own folder are.
var dirOverride string

// SetServerDir makes ServerDir return dir (the -dir flag).
func SetServerDir(dir string) {
	if dir = strings.TrimSpace(dir); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	dirOverride = dir
}

// autostartID names this server folder's entry.
func autostartID(serverDir string) string {
	sum := sha1.Sum([]byte(strings.ToLower(filepath.Clean(serverDir))))
	return "dayz-server-manager-" + hex.EncodeToString(sum[:4])
}

// autostartArgs are the arguments the entry starts the manager with.
func autostartArgs(serverDir string, port int) []string {
	args := []string{"-autostart", "-dir", serverDir}
	if port != 0 && port != 8787 {
		args = append(args, "-port", strconv.Itoa(port))
	}
	return args
}

// executable is this program's real path (symlinks resolved).
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}
