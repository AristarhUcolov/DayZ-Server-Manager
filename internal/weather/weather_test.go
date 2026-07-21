// Copyright (c) 2026 Aristarh Ucolov.
package weather

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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

// Weather that flips like a light switch was the #1 complaint: the old fixed
// template ramped over 120 s and let overcast swing the entire 0..1 range in a
// single re-roll. These lock in the gentler defaults.
func TestTransitionProfilesAffectRenderedXML(t *testing.T) {
	base := Params{Enable: true, Overcast: 0.5, Dynamic: true}

	smooth := Render(Params{Enable: base.Enable, Overcast: base.Overcast, Dynamic: true, Transition: TransitionSmooth})
	fast := Render(Params{Enable: base.Enable, Overcast: base.Overcast, Dynamic: true, Transition: TransitionFast})

	if !strings.Contains(smooth, `time="1800"`) {
		t.Errorf("smooth profile should ramp over 1800s:\n%s", smooth)
	}
	if !strings.Contains(fast, `time="120"`) {
		t.Errorf("fast profile should ramp over 120s")
	}
	// Gentle steps: overcast may move at most 0.3 per re-roll when smooth.
	if !strings.Contains(smooth, `<changelimits min="0" max="0.3" />`) {
		t.Errorf("smooth overcast step should cap at 0.3:\n%s", smooth)
	}
	if !strings.Contains(fast, `<changelimits min="0" max="1" />`) {
		t.Errorf("fast overcast step should allow the full range")
	}
}

// An unset/unknown Transition must default to smooth rather than the old
// abrupt behaviour.
func TestTransitionDefaultsToSmooth(t *testing.T) {
	out := Render(Params{Enable: true, Dynamic: true})
	if !strings.Contains(out, `time="1800"`) {
		t.Errorf("empty Transition should default to smooth")
	}
	if !strings.Contains(Render(Params{Enable: true, Dynamic: true, Transition: "nonsense"}), `time="1800"`) {
		t.Errorf("unknown Transition should default to smooth")
	}
}

// A static preset stays pinned (no drift) but should still ease in.
func TestStaticPresetPinsButStillRamps(t *testing.T) {
	out := Render(Params{Enable: true, Overcast: 0.8, Dynamic: false, Transition: TransitionSmooth})
	if !strings.Contains(out, `<changelimits min="0" max="0" />`) {
		t.Errorf("static weather must not drift:\n%s", out)
	}
	if !strings.Contains(out, `time="1800"`) {
		t.Errorf("static weather should still use the transition ramp")
	}
}

// Parse must report the transition the file actually encodes.
func TestParseDetectsTransition(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfgweather.xml")
	for _, want := range []string{TransitionSmooth, TransitionNormal, TransitionFast} {
		if err := os.WriteFile(p, []byte(Render(Params{Enable: true, Dynamic: true, Transition: want})), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Parse(p)
		if err != nil {
			t.Fatal(err)
		}
		if got.Transition != want {
			t.Errorf("round-trip transition = %q, want %q", got.Transition, want)
		}
	}
}
