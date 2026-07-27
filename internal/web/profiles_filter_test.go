// Copyright (c) 2026 Aristarh Ucolov.
package web

import "testing"

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
