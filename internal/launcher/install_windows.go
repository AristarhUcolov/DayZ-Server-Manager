// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// IsTerminal: the Windows builds always know where their output goes (the
// browser build has its console; the panel shows dialogs).
func IsTerminal() bool { return true }

// MessageBox prints the message (the panel build shows native dialogs itself).
func MessageBox(title, body string) { fmt.Println(title + ": " + body) }

// shortcutName is the file name of the shortcuts -install creates.
const shortcutName = AppName + ".lnk"

// startMenuDir is the user's Start menu Programs folder.
func startMenuDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
}

// psQuote makes s a single-quoted PowerShell string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// InstallDesktop puts the manager in the Start menu and on the desktop: a
// shortcut to this exe that starts in the server folder, with the app icon —
// so the panel opens like any installed program. The desktop folder comes
// from Windows itself (it may be redirected, e.g. to OneDrive).
func InstallDesktop(mode string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir, err := ServerDir()
	if err != nil {
		return nil, err
	}
	desc := AppName + " — " + dir
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$sh = New-Object -ComObject WScript.Shell",
		"foreach ($d in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('Desktop'))) {",
		"  if (-not $d) { continue }",
		"  $p = Join-Path $d " + psQuote(shortcutName),
		"  $s = $sh.CreateShortcut($p)",
		"  $s.TargetPath = " + psQuote(exe),
		"  $s.WorkingDirectory = " + psQuote(dir),
		"  $s.IconLocation = " + psQuote(exe+",0"),
		"  $s.Description = " + psQuote(desc),
		"  $s.Save()",
		"  Write-Output $p",
		"}",
	}, "\n")
	out, err := powershell(script)
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	if err != nil {
		return paths, fmt.Errorf("create shortcuts: %v %s", err, strings.TrimSpace(out))
	}
	if len(paths) == 0 {
		return nil, errors.New("create shortcuts: Windows reported no folders")
	}
	return paths, nil
}

// UninstallDesktop removes the shortcuts InstallDesktop created.
func UninstallDesktop() ([]string, error) {
	script := strings.Join([]string{
		"foreach ($d in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('Desktop'))) {",
		"  if (-not $d) { continue }",
		"  $p = Join-Path $d " + psQuote(shortcutName),
		"  if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Force; Write-Output $p }",
		"}",
	}, "\n")
	out, err := powershell(script)
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return paths, err
}

// desktopInstalled reports whether the Start menu shortcut exists.
func desktopInstalled() bool {
	_, err := os.Stat(filepath.Join(startMenuDir(), shortcutName))
	return err == nil
}

// powershell runs a script without a console window flashing up.
func powershell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	return string(out), err
}
