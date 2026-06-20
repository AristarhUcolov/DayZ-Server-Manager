// Copyright (c) 2026 Aristarh Ucolov.
//
// Weather control for DayZ missions. The mission's cfgweather.xml governs the
// in-game weather system: an overcast / fog / rain / wind / snowfall / storm
// model, each with a "current" target plus the limits and change-rates that
// drive how it evolves. The vanilla file ships with enable="0" (the whole file
// ignored), so applying any preset here also flips enable on.
//
// We expose a handful of intuitive knobs (Params) instead of the full XML, and
// on write regenerate a complete, known-good cfgweather.xml from a template —
// predictable results beat surgically editing a file most users never touch.
// A backup is taken before every write by the caller.
//
// Static presets pin each channel (limits min==max==actual) so the chosen
// weather holds. The "dynamic" preset widens the limits so weather drifts
// naturally over time. DayZ reads this file once at mission load, so changes
// require a server restart — the handler enforces server-stopped.
package weather

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dayzmanager/internal/util"
)

// Params is the simplified, user-facing weather state.
type Params struct {
	Enable       bool    `json:"enable"`
	Overcast     float64 `json:"overcast"`     // 0..1 cloud cover
	Fog          float64 `json:"fog"`          // 0..1
	Rain         float64 `json:"rain"`         // 0..1
	Wind         float64 `json:"wind"`         // m/s, 0..20
	Snowfall     float64 `json:"snowfall"`     // 0..1 (snow worlds, e.g. Sakhal)
	StormDensity float64 `json:"stormDensity"` // 0..1 lightning density
	Dynamic      bool    `json:"dynamic"`      // weather drifts vs stays pinned
}

// presetOrder fixes the display order; presets holds the values.
var presetOrder = []string{"clear", "cloudy", "foggy", "rainy", "storm", "snowy", "dynamic"}

var presets = map[string]Params{
	"clear":   {Enable: true, Overcast: 0.0, Fog: 0.0, Rain: 0.0, Wind: 3, StormDensity: 0.0},
	"cloudy":  {Enable: true, Overcast: 0.6, Fog: 0.05, Rain: 0.0, Wind: 6, StormDensity: 0.0},
	"foggy":   {Enable: true, Overcast: 0.5, Fog: 0.7, Rain: 0.0, Wind: 2, StormDensity: 0.0},
	"rainy":   {Enable: true, Overcast: 0.9, Fog: 0.1, Rain: 0.8, Wind: 10, StormDensity: 0.3},
	"storm":   {Enable: true, Overcast: 1.0, Fog: 0.15, Rain: 1.0, Wind: 18, StormDensity: 1.0},
	"snowy":   {Enable: true, Overcast: 0.8, Fog: 0.2, Rain: 0.0, Wind: 8, Snowfall: 0.8, StormDensity: 0.0},
	"dynamic": {Enable: true, Overcast: 0.45, Fog: 0.05, Rain: 0.0, Wind: 8, StormDensity: 1.0, Dynamic: true},
}

// Presets returns the preset names in display order.
func Presets() []string { return append([]string(nil), presetOrder...) }

// Preset returns a copy of the named preset.
func Preset(name string) (Params, bool) {
	p, ok := presets[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// MatchPreset returns the name of the preset closest to p, or "custom" if none
// is a good match. Compares the actual channel values (not Dynamic).
func MatchPreset(p Params) string {
	best, bestDist := "custom", 0.18 // threshold: within this distance = a match
	for _, name := range presetOrder {
		q := presets[name]
		d := math.Abs(p.Overcast-q.Overcast) +
			math.Abs(p.Fog-q.Fog) +
			math.Abs(p.Rain-q.Rain) +
			math.Abs(p.Snowfall-q.Snowfall) +
			math.Abs(p.StormDensity-q.StormDensity) +
			math.Abs(p.Wind-q.Wind)/20.0
		if d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

// Parse reads the current weather state from a cfgweather.xml for display.
// Missing channels default to 0; a missing file is reported as the caller's
// error to handle.
func Parse(path string) (Params, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Params{}, err
	}
	s := string(data)
	p := Params{
		Enable:       parseBoolAttr(reEnable.FindStringSubmatch(s)),
		Overcast:     sectionActual(s, "overcast"),
		Fog:          sectionActual(s, "fog"),
		Rain:         sectionActual(s, "rain"),
		Wind:         sectionActual(s, "windMagnitude"),
		Snowfall:     sectionActual(s, "snowfall"),
		StormDensity: parseFloatAttr(reStormDensity.FindStringSubmatch(s)),
	}
	return p, nil
}

// Write regenerates cfgweather.xml from p, backing up the existing file first.
func Write(path string, p Params) error {
	if err := util.BackupBeforeWrite(path); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(Render(p)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Render produces a complete cfgweather.xml for p.
func Render(p Params) string {
	clamp := func(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
	oc := clamp(p.Overcast, 0, 1)
	fog := clamp(p.Fog, 0, 1)
	rain := clamp(p.Rain, 0, 1)
	wind := clamp(p.Wind, 0, 20)
	snow := clamp(p.Snowfall, 0, 1)
	storm := clamp(p.StormDensity, 0, 1)

	// limit builds a "<limits>" pair: pinned (min==max==v) when static, or a
	// wide [lo,hi] band when dynamic so the value drifts.
	limit := func(v, lo, hi float64) (float64, float64) {
		if p.Dynamic {
			return lo, hi
		}
		return v, v
	}
	// change builds a "<changelimits>" pair: 0 when static (no drift), a band
	// when dynamic.
	change := func(lo, hi float64) (float64, float64) {
		if p.Dynamic {
			return lo, hi
		}
		return 0, 0
	}

	ocMin, ocMax := limit(oc, 0, 1)
	ocCMin, ocCMax := change(0, 1)
	fogMin, fogMax := limit(fog, 0, 1)
	fogCMin, fogCMax := change(0, 0.3)
	rainMin, rainMax := limit(rain, 0, 1)
	rainCMin, rainCMax := change(0, 1)
	windMin, windMax := limit(wind, 0, 20)
	windCMin, windCMax := change(0, 20)
	snowMin, snowMax := limit(snow, 0, 1)
	snowCMin, snowCMax := change(0, 0.3)

	en := boolAttr(p.Enable)
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<!-- Generated by DayZ Server Manager. 'enable' must be 1 for this file to apply. -->
<weather reset="0" enable="%s">
    <overcast>
        <current actual="%s" time="120" duration="240" />
        <limits min="%s" max="%s" />
        <timelimits min="600" max="900" />
        <changelimits min="%s" max="%s" />
    </overcast>
    <fog>
        <current actual="%s" time="120" duration="240" />
        <limits min="%s" max="%s" />
        <timelimits min="900" max="900" />
        <changelimits min="%s" max="%s" />
    </fog>
    <rain>
        <current actual="%s" time="60" duration="120" />
        <limits min="%s" max="%s" />
        <timelimits min="60" max="120" />
        <changelimits min="%s" max="%s" />
        <thresholds min="0.6" max="1.0" end="60" />
    </rain>
    <windMagnitude>
        <current actual="%s" time="120" duration="240" />
        <limits min="%s" max="%s" />
        <timelimits min="120" max="240" />
        <changelimits min="%s" max="%s" />
    </windMagnitude>
    <windDirection>
        <current actual="0.0" time="120" duration="240" />
        <limits min="-3.14" max="3.14" />
        <timelimits min="60" max="120" />
        <changelimits min="-1.0" max="1.0" />
    </windDirection>
    <snowfall>
        <current actual="%s" time="120" duration="240" />
        <limits min="%s" max="%s" />
        <timelimits min="300" max="3600" />
        <changelimits min="%s" max="%s" />
        <thresholds min="1.0" max="1.0" end="120" />
    </snowfall>
    <storm density="%s" threshold="0.9" timeout="45"/>
</weather>
`,
		en,
		f(oc), f(ocMin), f(ocMax), f(ocCMin), f(ocCMax),
		f(fog), f(fogMin), f(fogMax), f(fogCMin), f(fogCMax),
		f(rain), f(rainMin), f(rainMax), f(rainCMin), f(rainCMax),
		f(wind), f(windMin), f(windMax), f(windCMin), f(windCMax),
		f(snow), f(snowMin), f(snowMax), f(snowCMin), f(snowCMax),
		f(storm),
	)
}

// ---------------------------------------------------------------------------

var (
	reEnable       = regexp.MustCompile(`<weather[^>]*\benable\s*=\s*"([^"]*)"`)
	reStormDensity = regexp.MustCompile(`<storm[^>]*\bdensity\s*=\s*"([^"]*)"`)
)

// sectionActual pulls the <current actual="..."> value out of a named channel
// block (e.g. "overcast"), returning 0 if absent.
func sectionActual(xml, section string) float64 {
	re := regexp.MustCompile(`(?s)<` + section + `>.*?<current[^>]*\bactual\s*=\s*"([^"]*)"`)
	return parseFloatAttr(re.FindStringSubmatch(xml))
}

func parseFloatAttr(m []string) float64 {
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	if err != nil {
		return 0
	}
	return v
}

func parseBoolAttr(m []string) bool {
	if len(m) < 2 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(m[1])) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func boolAttr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
