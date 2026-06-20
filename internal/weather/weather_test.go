// Copyright (c) 2026 Aristarh Ucolov.
package weather

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderParseRoundTrip(t *testing.T) {
	for _, name := range Presets() {
		p, _ := Preset(name)
		dir := t.TempDir()
		path := filepath.Join(dir, "cfgweather.xml")
		if err := os.WriteFile(path, []byte(Render(p)), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Parse(path)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		near := func(a, b float64, field string) {
			if math.Abs(a-b) > 1e-6 {
				t.Errorf("%s: %s = %v, want %v", name, field, a, b)
			}
		}
		near(got.Overcast, p.Overcast, "overcast")
		near(got.Fog, p.Fog, "fog")
		near(got.Rain, p.Rain, "rain")
		near(got.Wind, p.Wind, "wind")
		near(got.Snowfall, p.Snowfall, "snowfall")
		near(got.StormDensity, p.StormDensity, "stormDensity")
		if got.Enable != p.Enable {
			t.Errorf("%s: enable = %v, want %v", name, got.Enable, p.Enable)
		}
	}
}

func TestMatchPreset(t *testing.T) {
	for _, name := range Presets() {
		p, _ := Preset(name)
		if got := MatchPreset(p); got != name {
			t.Errorf("MatchPreset(%s) = %q", name, got)
		}
	}
	// A clearly off-grid mix should read as custom.
	odd := Params{Enable: true, Overcast: 0.33, Fog: 0.44, Rain: 0.55, Wind: 13, StormDensity: 0.66}
	if got := MatchPreset(odd); got != "custom" {
		t.Errorf("MatchPreset(odd) = %q, want custom", got)
	}
}

func TestRenderEnableFlag(t *testing.T) {
	on := Render(Params{Enable: true})
	if !contains(on, `enable="1"`) {
		t.Error("enabled render missing enable=\"1\"")
	}
	off := Render(Params{Enable: false})
	if !contains(off, `enable="0"`) {
		t.Error("disabled render missing enable=\"0\"")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
