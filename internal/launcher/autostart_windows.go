// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"errors"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"dayzmanager/internal/app"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// winQuote quotes one argument for a Windows command line.
func winQuote(s string) string { return syscall.EscapeArg(s) }

// AutostartInfo reports whether the manager for serverDir starts when the
// user signs in to Windows.
func AutostartInfo(serverDir string) app.AutostartInfo {
	info := app.AutostartInfo{Supported: true, Kind: "registry", Desktop: true}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if !errors.Is(err, registry.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	defer k.Close()
	v, _, err := k.GetStringValue(autostartID(serverDir))
	switch {
	case errors.Is(err, registry.ErrNotExist):
	case err != nil:
		info.Error = err.Error()
	default:
		info.Enabled, info.Detail = true, v
	}
	return info
}

// SetAutostart adds or removes the sign-in entry.
func SetAutostart(serverDir string, port int, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	name := autostartID(serverDir)
	if !on {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	parts := []string{winQuote(exe)}
	for _, a := range autostartArgs(serverDir, port) {
		parts = append(parts, winQuote(a))
	}
	return k.SetStringValue(name, strings.Join(parts, " "))
}
