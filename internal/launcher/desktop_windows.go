// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
)

// sudoUser has no meaning on Windows (see desktop_unix.go).
func sudoUser() string { return "" }

// sshConnection has no meaning on Windows (see desktop_unix.go).
func sshConnection() string { return "" }

func hasDesktop() bool { return true }

// DesktopCommand prepares a program to run in the user's desktop session.
func DesktopCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

// RunsAsRoot is false on Windows (no Chromium sandbox restriction applies).
func RunsAsRoot() bool { return false }

// DesktopHome is the home folder of the user browsers run as.
func DesktopHome() string { h, _ := os.UserHomeDir(); return h }

// DesktopUserDiffers is false on Windows: browsers run as the manager's user.
func DesktopUserDiffers() bool { return false }

// openDesktop opens url in the default browser. `cmd /c start "" <url>` is the
// reliable way to open it exactly once — rundll32 FileProtocolHandler is known
// to occasionally spawn a duplicate tab.
// The panel build has no console, so cmd.exe would get a new visible one that
// flashes up: CREATE_NO_WINDOW (as internal/util does for its children).
func openDesktop(url string) bool {
	c := exec.Command("cmd", "/c", "start", "", url)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if c.Start() != nil {
		return false
	}
	go func() { _ = c.Wait() }() // release the process handle
	return true
}
