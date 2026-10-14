// Copyright (c) 2026 Aristarh Ucolov.
//
// BattlEye files editor. DayZ's BattlEye reads bans.txt for bans and
// BEServer_x64.cfg for its settings (RCon port and password). The panel lists
// those plus any other .txt already in the folder — the classic Arma list of
// script filters (scripts.txt, createvehicle.txt…) is not something DayZ reads,
// and offering 24 empty placeholders of it hid the one file that matters.
package battleye

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dayzmanager/internal/util"
)

// Known lists the files offered even before they exist.
var Known = []string{"bans.txt"}

// ErrNotEditable is returned for names the panel will not read or write.
var ErrNotEditable = errors.New("not an editable battleye file")

func isKnown(lower string) bool {
	for _, k := range Known {
		if k == lower {
			return true
		}
	}
	return false
}

// isSettings reports BattlEye's settings file in any of its spellings
// (beserver_x64.cfg, BEServer.cfg…) and BattlEye's *_active_* copy of it.
func isSettings(lower string) bool {
	return strings.HasPrefix(lower, "beserver") && strings.HasSuffix(lower, ".cfg")
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
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Exists bool   `json:"exists"`
	// Kind is "bans", "settings" (BEServer_x64.cfg), "active" (the copy
	// BattlEye renames it to for the current run) or "file" (another .txt).
	Kind string `json:"kind"`
	// ReadOnly: the settings files are shown with the RCon password masked;
	// the password is set on the RCon page.
	ReadOnly bool   `json:"readOnly,omitempty"`
	LineHint string `json:"lineHint,omitempty"` // "5 entries"
}

func stat(beDir, name, kind string) FileInfo {
	fi := FileInfo{Name: name, Kind: kind, ReadOnly: kind == "settings" || kind == "active"}
	full := filepath.Join(beDir, name)
	if st, err := os.Stat(full); err == nil && !st.IsDir() {
		fi.Exists = true
		fi.Size = st.Size()
		if kind != "settings" && kind != "active" {
			fi.LineHint = lineHint(full)
		}
	}
	return fi
}

// List returns bans.txt (even when missing), the settings file, the newest
// active copy of it, and every other .txt in the folder.
func List(beDir string) []FileInfo {
	out := []FileInfo{stat(beDir, "bans.txt", "bans")}
	entries, _ := os.ReadDir(beDir)
	var others []FileInfo
	active, activeMod := "", int64(0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		switch {
		case isSettings(lower) && strings.Contains(lower, "_active_"):
			if info, err := e.Info(); err == nil && (active == "" || info.ModTime().UnixNano() > activeMod) {
				active, activeMod = e.Name(), info.ModTime().UnixNano()
			}
		case isSettings(lower):
			out = append(out, stat(beDir, e.Name(), "settings"))
		case strings.HasSuffix(lower, ".txt") && !isKnown(lower):
			others = append(others, stat(beDir, e.Name(), "file"))
		}
	}
	if active != "" {
		out = append(out, stat(beDir, active, "active"))
	}
	sort.Slice(others, func(i, j int) bool { return strings.ToLower(others[i].Name) < strings.ToLower(others[j].Name) })
	return append(out, others...)
}

// checkName accepts a bare file name in the folder: a known file, any .txt
// and (for reading only) the settings file.
func checkName(name string, write bool) (string, error) {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	if base != name || strings.HasPrefix(base, ".") {
		return "", fmt.Errorf("%w: %s", ErrNotEditable, name)
	}
	if isSettings(lower) {
		if write {
			return "", fmt.Errorf("%w: %s is read-only here — set the RCon password on the RCon page", ErrNotEditable, name)
		}
		return base, nil
	}
	if isKnown(lower) || strings.HasSuffix(lower, ".txt") {
		return base, nil
	}
	return "", fmt.Errorf("%w: %s", ErrNotEditable, name)
}

// Read loads a BE file. Missing files return empty content (not an error) —
// creating via the editor's "save" should be the default flow. The settings
// file comes back with its RCon password masked.
func Read(beDir, name string) (string, error) {
	base, err := checkName(name, false)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(beDir, base))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if isSettings(strings.ToLower(base)) {
		return MaskPassword(string(data)), nil
	}
	return string(data), nil
}

// MaskPassword hides the value of every RConPassword line.
func MaskPassword(cfg string) string {
	lines := strings.Split(cfg, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if len(trim) >= len("RConPassword") && strings.EqualFold(trim[:len("RConPassword")], "RConPassword") {
			cr := ""
			if strings.HasSuffix(line, "\r") {
				cr = "\r"
			}
			lines[i] = "RConPassword ********" + cr
		}
	}
	return strings.Join(lines, "\n")
}

// Write atomically overwrites a BE file. A timestamped .bak is created
// first so a bad edit can be reverted.
func Write(beDir, name, content string) error {
	base, err := checkName(name, true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(beDir, 0o755); err != nil {
		return err
	}
	full := filepath.Join(beDir, base)
	_ = util.BackupBeforeWrite(full)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// lineHint counts non-blank, non-comment lines so the UI can tell users
// "5 entries" instead of "217 bytes" at a glance.
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
