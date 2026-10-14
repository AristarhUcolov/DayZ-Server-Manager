// Copyright (c) 2026 Aristarh Ucolov.
package updater

import "testing"

func TestPickAsset(t *testing.T) {
	v1 := []string{"dayz-manager-web.exe", "dayz-manager-panel.exe", "dayz-manager-web-linux", "dayz-manager-panel-linux", "checksums.txt"}
	old := []string{"dayz-manager.exe", "dayz-manager-linux"}
	cases := []struct {
		names      []string
		mode, goos string
		want       int
	}{
		{v1, "web", "windows", 0},
		{v1, "panel", "windows", 1},
		{v1, "web", "linux", 2},
		{v1, "panel", "linux", 3},
		{v1, "", "windows", 0},
		{old, "panel", "windows", 0}, // pre-1.0 release: the single build
		{old, "web", "linux", 1},
		{[]string{"notes.txt"}, "web", "windows", -1},
	}
	for _, c := range cases {
		if got := pickAsset(c.names, c.mode, c.goos); got != c.want {
			t.Errorf("pickAsset(%v, %q, %q) = %d, want %d", c.names, c.mode, c.goos, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("1.0.0", "0.31.0") != 1 || compareVersions("0.31.0", "1.0.0") != -1 || compareVersions("1.0.0", "1.0.0-rc1") != 0 {
		t.Fatal("compareVersions ordering is wrong")
	}
}
