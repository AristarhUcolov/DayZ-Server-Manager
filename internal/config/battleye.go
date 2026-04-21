// Copyright (c) 2026 Aristarh Ucolov.
//
// Reader for BattlEye's beserver(_x64).cfg. DayZ keeps the RCon password
// there (not in server.cfg), which is why the panel previously showed
// "rcon not configured" even when the server was running fine — the manager
// config had no copy. We read whichever variant exists.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		// BE format is `Key Value` separated by whitespace.
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(fields[0]))
		val := strings.TrimSpace(fields[1])
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
