// Copyright (c) 2026 Aristarh Ucolov.
//
// BattlEye configuration files editor. DayZ's BE reads bans.txt /
// whitelist.txt for access control and scripts.txt / createvehicle.txt /
// remoteexec.txt etc. as runtime filter rules. Users used to hand-edit
// these with Notepad; the manager exposes them as a single tidy API.
package battleye

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/util"
)

// Known is the whitelist of filenames the panel is allowed to read/write.
// A strict allowlist is easier to reason about than sanitizing the full
// filesystem — BattlEye only cares about this fixed set anyway.
var Known = []string{
	"bans.txt",
	"whitelist.txt",
	"scripts.txt",
	"createvehicle.txt",
	"deletevehicle.txt",
	"remoteexec.txt",
	"attachto.txt",
	"publicvariable.txt",
	"setvariable.txt",
	"setpos.txt",
	"setdamage.txt",
	"mpeventhandler.txt",
	"selectplayer.txt",
	"waypointcondition.txt",
	"waypointstatement.txt",
	"publicvariableval.txt",
	"addmagazinecargo.txt",
	"addweaponcargo.txt",
	"addbackpackcargo.txt",
	"teamswitch.txt",
	"addweapon.txt",
	"addmagazine.txt",
	"addbackpack.txt",
	"admins.txt",
}

func isKnown(name string) bool {
	lower := strings.ToLower(name)
	for _, k := range Known {
		if k == lower {
			return true
		}
	}
	return false
}

// Dir returns the absolute BE directory (handles relative BEPath).
func Dir(serverDir, bePath string) string {
	if bePath == "" {
		bePath = "battleye"
	}
	if filepath.IsAbs(bePath) {
		return bePath
	}
	return filepath.Join(serverDir, bePath)
}

type FileInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Exists   bool   `json:"exists"`
	LineHint string `json:"lineHint,omitempty"` // "5 bans" / "12 rules"
}

// List returns every recognized BE file (including non-existent placeholders
// so the UI can offer "create" for them consistently).
func List(beDir string) []FileInfo {
	out := make([]FileInfo, 0, len(Known))
	for _, name := range Known {
		fi := FileInfo{Name: name}
		full := filepath.Join(beDir, name)
		st, err := os.Stat(full)
		if err == nil && !st.IsDir() {
			fi.Exists = true
			fi.Size = st.Size()
			fi.LineHint = lineHint(full)
		}
		out = append(out, fi)
	}
	return out
}

// Read loads a known BE file. Missing files return empty content (not an
// error) — creating via the editor's "save" should be the default flow.
func Read(beDir, name string) (string, error) {
	if !isKnown(name) {
		return "", fmt.Errorf("unknown battleye file: %s", name)
	}
	full := filepath.Join(beDir, filepath.Base(name))
	data, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Write atomically overwrites a BE file. A timestamped .bak is created
// first so a bad filter that locks everyone out can be reverted.
func Write(beDir, name, content string) error {
	if !isKnown(name) {
		return fmt.Errorf("unknown battleye file: %s", name)
	}
	if err := os.MkdirAll(beDir, 0o755); err != nil {
		return err
	}
	full := filepath.Join(beDir, filepath.Base(name))
	_ = util.BackupBeforeWrite(full)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// lineHint counts non-blank, non-comment lines so the UI can tell users
// "5 bans" instead of "217 bytes" at a glance. BE uses ';' / '//' for
// comments in filter files and plain GUIDs in bans.txt.
func lineHint(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		n++
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d entries", n)
}
