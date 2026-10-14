// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package util

import (
	"os"
	"os/exec"
	"syscall"
)

// Reveal opens Explorer at path: a folder is opened, a file is selected in its
// folder. The command line is passed raw because explorer.exe parses
// /select,"path" itself and Go's argument quoting would break it.
func Reveal(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	line := `explorer.exe "` + path + `"`
	if !st.IsDir() {
		line = `explorer.exe /select,"` + path + `"`
	}
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	// explorer.exe exits non-zero even on success, so only a failure to
	// launch it at all counts as an error.
	return cmd.Start()
}
