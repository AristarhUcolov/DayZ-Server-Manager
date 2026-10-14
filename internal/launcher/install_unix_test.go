// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The menu entry runs the exact program (quoted per the desktop-entry spec,
// spaces and all), starts in its folder, and keeps a terminal only for the
// browser build.
func TestDesktopEntry(t *testing.T) {
	e := desktopEntry("/home/a/DayZ Server/dayz-manager-panel-linux", "panel", "DayZ Server Manager")
	for _, want := range []string{
		`Exec="/home/a/DayZ Server/dayz-manager-panel-linux"`,
		"Path=/home/a/DayZ Server",
		"Terminal=false",
		"Icon=" + desktopID,
		"StartupWMClass=DayZServerManager",
	} {
		if !strings.Contains(e, want) {
			t.Errorf("entry lacks %q:\n%s", want, e)
		}
	}
	if w := desktopEntry("/opt/x/dayz-manager-web-linux", "web", "x"); !strings.Contains(w, "Terminal=true") {
		t.Errorf("browser build needs its terminal:\n%s", w)
	}
	if q := desktopEntry(`/a/$b"c`, "panel", "x"); !strings.Contains(q, `Exec="/a/\$b\"c"`) {
		t.Errorf("special characters not escaped:\n%s", q)
	}
}

func TestDesktopDirFromUserDirs(t *testing.T) {
	home := t.TempDir()
	desk := filepath.Join(home, "Рабочий стол")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "user-dirs.dirs"),
		[]byte("XDG_DESKTOP_DIR=\"$HOME/Рабочий стол\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := desktopDir(home); got != desk {
		t.Errorf("desktopDir = %q, want %q", got, desk)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/opt/dzm/app"); got != "/opt/dzm/app" {
		t.Errorf("plain path quoted: %q", got)
	}
	if got := shellQuote("/home/a/DayZ Server/app"); got != "'/home/a/DayZ Server/app'" {
		t.Errorf("space: %q", got)
	}
	if got := shellQuote("/x/it's"); got != `'/x/it'\''s'` {
		t.Errorf("apostrophe: %q", got)
	}
}
