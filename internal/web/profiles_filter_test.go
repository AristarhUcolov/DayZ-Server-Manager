// Copyright (c) 2026 Aristarh Ucolov.
package web

import "testing"

// The History page groups files by stripping the .bak.<timestamp> suffix, so
// the regex must match exactly the manager's format and nothing that merely
// contains ".bak.".
func TestBackupStampRe(t *testing.T) {
	match := map[string]string{
		"types.xml.bak.20260727-213520":       "types.xml",
		"serverDZ.cfg.bak.20260101-000000":     "serverDZ.cfg",
		"cfggameplay.json.bak.20260727-120000": "cfggameplay.json",
	}
	for name, wantBase := range match {
		m := backupStampRe.FindStringSubmatchIndex(name)
		if m == nil {
			t.Errorf("%q should match", name)
			continue
		}
		if got := name[:m[0]]; got != wantBase {
			t.Errorf("%q → base %q, want %q", name, got, wantBase)
		}
	}
	for _, name := range []string{
		"types.xml", "notes.bak.txt", "x.bak.2026", "a.bak.20260727-2135", // short time
		"a.bak.20260727-213520.extra", // suffix must be at end
	} {
		if backupStampRe.MatchString(name) {
			t.Errorf("%q should NOT match", name)
		}
	}
}

// The profiles editor must hide server run-logs and crash dumps (they have
// their own viewers) while keeping real config files editable.
func TestIsServerLogArtifact(t *testing.T) {
	logs := []string{
		"DayZServer_x64_2026-07-27_10-00-00.RPT",
		"DayZServer_x64_2026-07-27_10-00-00.ADM",
		"script_2026-07-27.log",
		"crash_2026-07-27_10-00-00",
		"DayZServer_x64.mdmp",
		"error.bidmp",
		"webapilog.txt",
	}
	for _, n := range logs {
		if !isServerLogArtifact(n) {
			t.Errorf("%q should be hidden as a log artifact", n)
		}
	}
	config := []string{
		"CF.json", "cfggameplay.json", "TraderConfig.txt",
		"expansion", "storage_1", "banlist.txt", "vppadminlist.txt",
	}
	for _, n := range config {
		if isServerLogArtifact(n) {
			t.Errorf("%q is config and must stay visible", n)
		}
	}
}
