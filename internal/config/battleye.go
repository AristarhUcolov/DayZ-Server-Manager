// Copyright (c) 2026 Aristarh Ucolov.
//
// Reader for BattlEye's beserver(_x64).cfg. DayZ keeps the RCon password
// there (not in server.cfg), which is why the panel previously showed
// "rcon not configured" even when the server was running fine — the manager
// config had no copy. We read whichever variant exists.
package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dayzmanager/internal/util"
)

type BEConfig struct {
	Path         string
	RConPort     int
	RConPassword string
	RConIP       string
}

// FindBEConfig looks for the RCon config BattlEye uses. DayZ writes
// beserver_x64.cfg (lowercase) but some community tools use BEServer.cfg,
// so we accept both. Returns an empty value (no error) if nothing found.
func FindBEConfig(beDir string) *BEConfig {
	if beDir == "" {
		return nil
	}
	candidates := []string{
		"beserver_x64.cfg",
		"BEServer_x64.cfg",
		"beserver.cfg",
		"BEServer.cfg",
	}
	for _, name := range candidates {
		p := filepath.Join(beDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return parseBEConfig(p)
		}
	}
	return nil
}

// EnsureBEConfig makes sure beserver_x64.cfg in beDir declares the RCon
// password (and a port, if the file doesn't already have one). DayZ stores the
// RCon credential HERE, not in server.cfg — and BattlEye only enables RCon when
// this file exists with an RConPassword. On a fresh install the file is absent,
// which is exactly why the panel reports "rcon: not configured" no matter what
// password the user types: nothing was ever written where BattlEye looks.
//
// We create or update the file in place, preserving any other BE settings and
// any pre-existing RConPort. Returns true if the file changed. The change takes
// effect the next time the server starts (BattlEye reads it at launch).
func EnsureBEConfig(beDir, password string, port int) (bool, error) {
	if beDir == "" {
		return false, errors.New("BattlEye path is not set")
	}
	if password == "" {
		return false, nil // nothing to enforce
	}
	if err := os.MkdirAll(beDir, 0o755); err != nil {
		return false, err
	}

	// Update an existing file (any casing) in place; otherwise create the
	// canonical lowercase name DayZ itself writes.
	path := filepath.Join(beDir, "beserver_x64.cfg")
	if be := FindBEConfig(beDir); be != nil && be.Path != "" {
		path = be.Path
	}

	orig := ""
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		orig = strings.ReplaceAll(string(data), "\r\n", "\n")
		lines = strings.Split(orig, "\n")
	}

	hasKey := func(key string) bool {
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
				continue
			}
			f := strings.Fields(t)
			if len(f) > 0 && strings.EqualFold(f[0], key) {
				return true
			}
		}
		return false
	}
	setKey := func(key, val string) {
		for i, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
				continue
			}
			f := strings.Fields(t)
			if len(f) > 0 && strings.EqualFold(f[0], key) {
				lines[i] = key + " " + val
				return
			}
		}
		lines = append(lines, key+" "+val)
	}

	setKey("RConPassword", password)
	// Only introduce a port if the file doesn't already specify one — never
	// clobber a working port the operator set themselves.
	if port > 0 && !hasKey("RConPort") {
		setKey("RConPort", strconv.Itoa(port))
	}

	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if orig == out {
		return false, nil
	}
	tmp := path + ".tmp"
	// The only writer in the codebase that did not keep a copy — and it is the
	// one holding the RCon credentials.
	_ = util.BackupBeforeWrite(path)
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func parseBEConfig(path string) *BEConfig {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := &BEConfig{Path: path}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		// BE format is `Key Value` separated by ANY whitespace — tabs are as
		// common as spaces in files BattlEye writes itself.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		val := strings.Join(fields[1:], " ")
		switch key {
		case "rconpassword":
			out.RConPassword = val
		case "rconport":
			if n, err := strconv.Atoi(val); err == nil {
				out.RConPort = n
			}
		case "rconip":
			out.RConIP = val
		}
	}
	return out
}
