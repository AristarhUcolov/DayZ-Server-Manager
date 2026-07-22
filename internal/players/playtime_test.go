// Copyright (c) 2026 Aristarh Ucolov.
package players

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A real .ADM: a header carrying the date, then lines carrying only HH:MM:SS.
const admSample = `AdminLog started on 2026-07-20 at 18:00:00
18:05:00 | Player "Survivor" (id=ABC123= pos=<100, 200, 5>) connected
18:52:00 | Player "Survivor" (id=ABC123= pos=<140, 240, 5>) disconnected
19:10:00 | Player "Survivor" (id=ABC123= pos=<100, 200, 5>) connected
21:38:00 | Player "Survivor" (id=ABC123= pos=<180, 260, 5>) disconnected
`

// Every event used to be stamped with the FILE's mtime, so a connect and its
// disconnect shared a timestamp: every session lasted zero minutes and the
// Playtime column read 0 for everyone. Last seen was the scan time, not the
// event time.
func TestPlaytimeComesFromTheLogNotTheFileMtime(t *testing.T) {
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	adm := filepath.Join(profiles, "DayZServer_x64.ADM")
	if err := os.WriteFile(adm, []byte(admSample), 0o644); err != nil {
		t.Fatal(err)
	}

	st := Open(filepath.Join(dir, ".dayz-manager"))
	st.Ingest(profiles)

	list, _ := st.Snapshot(10)
	if len(list) != 1 {
		t.Fatalf("players = %d, want 1", len(list))
	}
	p := list[0]

	// 47 minutes + 2h28m = 195 minutes.
	if p.PlayMin < 190 || p.PlayMin > 200 {
		t.Errorf("playtime = %d min, want ~195", p.PlayMin)
	}
	if p.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", p.Sessions)
	}

	// Last seen must be the event's moment, not "now".
	last, err := time.Parse(time.RFC3339, p.LastSeen)
	if err != nil {
		t.Fatalf("lastSeen %q does not parse: %v", p.LastSeen, err)
	}
	if last.Year() != 2026 || last.Month() != time.July || last.Day() != 20 {
		t.Errorf("lastSeen = %s, want 2026-07-20 (the log's date, not the scan date)", p.LastSeen)
	}
	if last.Hour() != 21 || last.Minute() != 38 {
		t.Errorf("lastSeen time = %02d:%02d, want 21:38", last.Hour(), last.Minute())
	}
}

// A session that crosses midnight must not come out negative: the line clock
// wraps to 00:00 while the day moves forward.
func TestSessionAcrossMidnight(t *testing.T) {
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	os.MkdirAll(profiles, 0o755)
	os.WriteFile(filepath.Join(profiles, "s.ADM"), []byte(
		`AdminLog started on 2026-07-20 at 23:00:00
23:30:00 | Player "NightOwl" (id=NN= pos=<1, 2, 3>) connected
00:15:00 | Player "NightOwl" (id=NN= pos=<1, 2, 3>) disconnected
`), 0o644)

	st := Open(filepath.Join(dir, ".dayz-manager"))
	st.Ingest(profiles)

	list, _ := st.Snapshot(10)
	if len(list) != 1 {
		t.Fatalf("players = %d, want 1", len(list))
	}
	if got := list[0].PlayMin; got < 40 || got > 50 {
		t.Errorf("playtime across midnight = %d min, want ~45", got)
	}
}
