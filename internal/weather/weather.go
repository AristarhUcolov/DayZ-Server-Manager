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
	Transition   string  `json:"transition"`   // smooth | normal | fast (see transitions)
}

// Transition presets control how GRADUALLY weather moves.
//
// Three XML knobs decide this, and getting them wrong is why weather can feel
// like a light switch:
//
//	<current time="…">   seconds to ramp to a newly chosen value
//	<timelimits min max> seconds between weather re-rolls
//	<changelimits min max> how far a channel may move in one re-roll
//
// Vanilla DayZ ramps over ~30 minutes with small steps. "fast" reproduces the
// old behaviour of this manager (2-minute ramps, full-range steps), which is
// what made dynamic weather change abruptly.
type transitionProfile struct {
	Time     int // <current time="">
	Duration int // <current duration="">
	TimeMin  int // <timelimits min="">
	TimeMax  int // <timelimits max="">
	// Per-channel maximum step for <changelimits max="">.
	StepOvercast float64
	StepFog      float64
	StepRain     float64
	StepWind     float64
	StepSnow     float64
}

const (
	TransitionSmooth = "smooth"
	TransitionNormal = "normal"
	TransitionFast   = "fast"
)

// TransitionOrder is the display order for the UI picker.
var TransitionOrder = []string{TransitionSmooth, TransitionNormal, TransitionFast}

var transitions = map[string]transitionProfile{
	// Vanilla-like: long ramps, gentle steps — weather creeps in.
	TransitionSmooth: {Time: 1800, Duration: 600, TimeMin: 1800, TimeMax: 3600,
		StepOvercast: 0.3, StepFog: 0.1, StepRain: 0.4, StepWind: 4, StepSnow: 0.2},
	// Noticeable but not jarring.
	TransitionNormal: {Time: 600, Duration: 300, TimeMin: 900, TimeMax: 1800,
		StepOvercast: 0.5, StepFog: 0.2, StepRain: 0.6, StepWind: 8, StepSnow: 0.3},
	// The pre-0.16 behaviour: swings the whole range in two minutes.
	TransitionFast: {Time: 120, Duration: 240, TimeMin: 300, TimeMax: 600,
		StepOvercast: 1, StepFog: 0.3, StepRain: 1, StepWind: 20, StepSnow: 0.3},
}

// profileFor resolves a Transition name, defaulting to smooth.
func profileFor(name string) transitionProfile {
	if p, ok := transitions[strings.ToLower(strings.TrimSpace(name))]; ok {
		return p
	}
	return transitions[TransitionSmooth]
}

// presetOrder fixes the display order; presets holds the values.
var presetOrder = []string{"clear", "cloudy", "foggy", "rainy", "storm", "snowy", "dynamic"}

var presets = map[string]Params{
	"clear":   {Enable: true, Overcast: 0.0, Fog: 0.0, Rain: 0.0, Wind: 3, StormDensity: 0.0, Transition: TransitionSmooth},
	"cloudy":  {Enable: true, Overcast: 0.6, Fog: 0.05, Rain: 0.0, Wind: 6, StormDensity: 0.0, Transition: TransitionSmooth},
	"foggy":   {Enable: true, Overcast: 0.5, Fog: 0.7, Rain: 0.0, Wind: 2, StormDensity: 0.0, Transition: TransitionSmooth},
	"rainy":   {Enable: true, Overcast: 0.9, Fog: 0.1, Rain: 0.8, Wind: 10, StormDensity: 0.3, Transition: TransitionSmooth},
	"storm":   {Enable: true, Overcast: 1.0, Fog: 0.15, Rain: 1.0, Wind: 18, StormDensity: 1.0, Transition: TransitionSmooth},
	"snowy":   {Enable: true, Overcast: 0.8, Fog: 0.2, Rain: 0.0, Wind: 8, Snowfall: 0.8, StormDensity: 0.0, Transition: TransitionSmooth},
	"dynamic": {Enable: true, Overcast: 0.45, Fog: 0.05, Rain: 0.0, Wind: 8, StormDensity: 1.0, Dynamic: true, Transition: TransitionSmooth},
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
	p.Transition = detectTransition(sectionCurrentTime(s, "overcast"))
	p.Dynamic = detectDynamic(s)
	return p, nil
}

// detectDynamic works out whether the file lets the weather move.
//
// Render writes a static channel as limits min==max with changelimits 0..0,
// and a dynamic one as a wide band with a non-zero change step. Either signal
// alone is enough: a hand-written file may widen the limits without touching
// changelimits, or vice versa. Overcast is the channel every preset writes.
func detectDynamic(xml string) bool {
	for _, section := range []string{"overcast", "rain", "fog"} {
		lo, hi, ok := sectionLimits(xml, section, "limits")
		if ok && hi > lo {
			return true
		}
		_, chHi, ok := sectionLimits(xml, section, "changelimits")
		if ok && chHi > 0 {
			return true
		}
	}
	return false
}

// sectionLimits reads <limits min max> or <changelimits min max> from a named
// channel block. ok is false when the element is absent.
func sectionLimits(xml, section, tag string) (min, max float64, ok bool) {
	re := regexp.MustCompile(`(?s)<` + section + `>.*?<` + tag +
		`[^>]*\bmin\s*=\s*"([^"]*)"[^>]*\bmax\s*=\s*"([^"]*)"`)
	m := re.FindStringSubmatch(xml)
	if len(m) < 3 {
		return 0, 0, false
	}
	return parseFloatAttr([]string{"", m[1]}), parseFloatAttr([]string{"", m[2]}), true
}

// detectTransition maps an overcast ramp time back onto the nearest profile so
// the UI shows what the file actually does.
func detectTransition(rampSeconds float64) string {
	best, bestDist := TransitionSmooth, math.Inf(1)
	for _, name := range TransitionOrder {
		if d := math.Abs(rampSeconds - float64(transitions[name].Time)); d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
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
	// capped by the transition profile when dynamic. The cap is what makes the
	// difference between weather easing in and weather flipping like a switch.
	change := func(step float64) (float64, float64) {
		if p.Dynamic {
			return 0, step
		}
		return 0, 0
	}

	tp := profileFor(p.Transition)

	ocMin, ocMax := limit(oc, 0, 1)
	ocCMin, ocCMax := change(tp.StepOvercast)
	fogMin, fogMax := limit(fog, 0, 1)
	fogCMin, fogCMax := change(tp.StepFog)
	rainMin, rainMax := limit(rain, 0, 1)
	rainCMin, rainCMax := change(tp.StepRain)
	windMin, windMax := limit(wind, 0, 20)
	windCMin, windCMax := change(tp.StepWind)
	snowMin, snowMax := limit(snow, 0, 1)
	snowCMin, snowCMax := change(tp.StepSnow)

	en := boolAttr(p.Enable)
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	d := func(v int) string { return strconv.Itoa(v) }

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<!-- Generated by DayZ Server Manager. 'enable' must be 1 for this file to apply. -->
<weather reset="0" enable="%s">
    <overcast>
        <current actual="%s" time="%s" duration="%s" />
        <limits min="%s" max="%s" />
        <timelimits min="%s" max="%s" />
        <changelimits min="%s" max="%s" />
    </overcast>
    <fog>
        <current actual="%s" time="%s" duration="%s" />
        <limits min="%s" max="%s" />
        <timelimits min="%s" max="%s" />
        <changelimits min="%s" max="%s" />
    </fog>
    <rain>
        <current actual="%s" time="%s" duration="%s" />
        <limits min="%s" max="%s" />
        <timelimits min="%s" max="%s" />
        <changelimits min="%s" max="%s" />
        <thresholds min="0.6" max="1.0" end="60" />
    </rain>
    <windMagnitude>
        <current actual="%s" time="%s" duration="%s" />
        <limits min="%s" max="%s" />
        <timelimits min="%s" max="%s" />
        <changelimits min="%s" max="%s" />
    </windMagnitude>
    <windDirection>
        <current actual="0.0" time="%s" duration="%s" />
        <limits min="-3.14" max="3.14" />
        <timelimits min="%s" max="%s" />
        <changelimits min="-1.0" max="1.0" />
    </windDirection>
    <snowfall>
        <current actual="%s" time="%s" duration="%s" />
        <limits min="%s" max="%s" />
        <timelimits min="%s" max="%s" />
        <changelimits min="%s" max="%s" />
        <thresholds min="1.0" max="1.0" end="120" />
    </snowfall>
    <storm density="%s" threshold="0.9" timeout="45"/>
</weather>
`,
		en,
		f(oc), d(tp.Time), d(tp.Duration), f(ocMin), f(ocMax), d(tp.TimeMin), d(tp.TimeMax), f(ocCMin), f(ocCMax),
		f(fog), d(tp.Time), d(tp.Duration), f(fogMin), f(fogMax), d(tp.TimeMin), d(tp.TimeMax), f(fogCMin), f(fogCMax),
		f(rain), d(tp.Time), d(tp.Duration), f(rainMin), f(rainMax), d(tp.TimeMin), d(tp.TimeMax), f(rainCMin), f(rainCMax),
		f(wind), d(tp.Time), d(tp.Duration), f(windMin), f(windMax), d(tp.TimeMin), d(tp.TimeMax), f(windCMin), f(windCMax),
		d(tp.Time), d(tp.Duration), d(tp.TimeMin), d(tp.TimeMax),
		f(snow), d(tp.Time), d(tp.Duration), f(snowMin), f(snowMax), d(tp.TimeMin), d(tp.TimeMax), f(snowCMin), f(snowCMax),
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

// sectionCurrentTime pulls <current time="..."> from a named channel block.
func sectionCurrentTime(xml, section string) float64 {
	re := regexp.MustCompile(`(?s)<` + section + `>.*?<current[^>]*\btime\s*=\s*"([^"]*)"`)
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
