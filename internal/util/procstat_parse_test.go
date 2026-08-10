// Copyright (c) 2026 Aristarh Ucolov.
package util

import "testing"

func TestParseProcStatTicks(t *testing.T) {
	// A realistic line. utime is field 14, stime field 15 → 1234 + 567 = 1801.
	// The comm field deliberately contains a space and a ')' to prove the
	// last-')' split is right.
	line := "4242 (DayZServer (x64)) S 1 4242 4242 0 -1 4194560 100 0 0 0 1234 567 0 0 20 0 8 0 99 0 0"
	got, err := parseProcStatTicks(line)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1801 {
		t.Errorf("ticks = %d, want 1801", got)
	}

	// A plain comm with no tricky characters.
	simple := "1 (init) S 0 1 1 0 -1 4194560 100 0 0 0 10 20 0 0 20 0 1 0 5 0 0"
	if got, err := parseProcStatTicks(simple); err != nil || got != 30 {
		t.Errorf("simple ticks = %d, err = %v; want 30", got, err)
	}

	for _, bad := range []string{"", "no parens here", "1 (x) S 1 2 3"} {
		if _, err := parseProcStatTicks(bad); err == nil {
			t.Errorf("%q should fail to parse", bad)
		}
	}
}

func TestParseStatmRSSBytes(t *testing.T) {
	// statm: size resident shared text lib data dt. Field 2 (resident) = 200.
	if got := parseStatmRSSBytes("1000 200 50 1 0 100 0", 4096); got != 200*4096 {
		t.Errorf("rss = %d, want %d", got, 200*4096)
	}
	// Bad input / zero page size → 0, never a panic.
	for _, c := range []struct {
		s  string
		ps int
	}{{"", 4096}, {"only-one-field", 4096}, {"10 x 3", 4096}, {"10 20 3", 0}} {
		if got := parseStatmRSSBytes(c.s, c.ps); got != 0 {
			t.Errorf("parseStatmRSSBytes(%q,%d) = %d, want 0", c.s, c.ps, got)
		}
	}
}
