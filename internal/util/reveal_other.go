// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Reveal opens the desktop file manager at path (its folder for a file).
func Reveal(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !CanReveal() {
		return ErrNoDesktop
	}
	if runtime.GOOS == "darwin" {
		if st.IsDir() {
			return exec.Command("open", path).Start()
		}
		return exec.Command("open", "-R", path).Start()
	}
	dir := path
	if !st.IsDir() {
		dir = filepath.Dir(path)
	}
	return exec.Command("xdg-open", dir).Start()
}
