// Copyright (c) 2026 Aristarh Ucolov.
//
// The in-panel beginner's guide and the hover-help texts.
//
// Long-form documentation deliberately lives OUTSIDE the i18n bundle: that
// bundle is UI chrome and every locale must translate all of it (enforced by a
// parity test). A manual is a different beast, and mixing the two would mean a
// new language could not be added until someone had written the whole manual.
//
// The translatable text lives in text/<lang>.json, one file per language.
// Everything that is NOT translatable — which chapter carries which icon,
// which screenshot, and which panel route each step links to — stays here in
// Go, so it cannot drift between eleven copies. shape_test.go asserts every
// language matches the English structure exactly.
package guide

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed text/*.json
var textFS embed.FS

// Step is one numbered instruction inside a chapter. Route, when set, renders
// as a button that jumps straight to that section of the panel.
type Step struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Route string `json:"route,omitempty"`
}

// Chapter is one topic of the guide.
type Chapter struct {
	ID    string   `json:"id"`
	Icon  string   `json:"icon"`  // id of an inline SVG sprite symbol
	Title string   `json:"title"`
	Intro string   `json:"intro"`
	Image string   `json:"image,omitempty"` // screenshot under /img, per language
	Steps []Step   `json:"steps,omitempty"`
	Tips  []string `json:"tips,omitempty"` // gotchas worth calling out
}

// chapterSpec is the language-independent skeleton of one chapter.
type chapterSpec struct {
	ID   string
	Icon string
	Shot string   // screenshot base name; resolved per language at read time
	// Routes is parallel to the chapter's steps: the panel route each step
	// links to, or "" for a step with no button.
	Routes []string
}

// specs defines the chapter order and everything non-translatable about them.
var specs = []chapterSpec{
	{ID: "start", Icon: "i-dashboard", Shot: "dashboard", Routes: []string{"", "settings", "dashboard"}},
	{ID: "mods", Icon: "i-mods", Shot: "mods", Routes: []string{"", "mods", "", ""}},
	{ID: "economy", Icon: "i-types", Shot: "types", Routes: []string{"types", "", "", ""}},
	{ID: "attachments", Icon: "i-attach", Shot: "attachments", Routes: []string{"attachments", "", ""}},
	{ID: "rcon", Icon: "i-rcon", Shot: "players", Routes: []string{"rcon", "", "settings"}},
	{ID: "weather", Icon: "i-weather", Shot: "weather", Routes: []string{"weather", "", ""}},
	{ID: "maintenance", Icon: "i-validator", Shot: "validator", Routes: []string{"settings", "validator", "logs"}},
	{ID: "remote", Icon: "i-server", Shot: "settings", Routes: []string{"settings", ""}},
}

// bundle mirrors one text/<lang>.json file.
type bundle struct {
	Chapters []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Intro string `json:"intro"`
		Steps []struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"steps"`
		Tips []string `json:"tips"`
	} `json:"chapters"`
	Help map[string]string `json:"help"`
}

var (
	loadOnce sync.Once
	bundles  map[string]*bundle
)

func load() {
	bundles = map[string]*bundle{}
	entries, err := textFS.ReadDir("text")
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := textFS.ReadFile("text/" + name)
		if err != nil {
			continue
		}
		var b bundle
		if json.Unmarshal(data, &b) != nil {
			continue
		}
		bundles[strings.TrimSuffix(name, ".json")] = &b
	}
}

func bundleFor(lang string) *bundle {
	loadOnce.Do(load)
	if b := bundles[strings.ToLower(strings.TrimSpace(lang))]; b != nil {
		return b
	}
	return bundles["en"]
}

// Languages returns the language codes the guide is translated into.
func Languages() []string {
	loadOnce.Do(load)
	out := make([]string, 0, len(bundles))
	for k := range bundles {
		out = append(out, k)
	}
	return out
}

// Get returns the guide for a locale, falling back to English.
func Get(lang string) []Chapter {
	code := strings.ToLower(strings.TrimSpace(lang))
	b := bundleFor(code)
	if b == nil {
		return nil
	}
	loadOnce.Do(load)
	if _, ok := bundles[code]; !ok {
		code = "en"
	}

	byID := map[string]int{}
	for i := range b.Chapters {
		byID[b.Chapters[i].ID] = i
	}

	out := make([]Chapter, 0, len(specs))
	for _, sp := range specs {
		i, ok := byID[sp.ID]
		if !ok {
			continue // a translation missing a chapter simply omits it
		}
		src := b.Chapters[i]
		ch := Chapter{
			ID:    sp.ID,
			Icon:  sp.Icon,
			Title: src.Title,
			Intro: src.Intro,
			// Screenshots are per language, so a Korean reader sees a Korean
			// panel. shotFor falls back to English when a language has none.
			Image: shotFor(sp.Shot, code),
		}
		for si, st := range src.Steps {
			route := ""
			if si < len(sp.Routes) {
				route = sp.Routes[si]
			}
			ch.Steps = append(ch.Steps, Step{Title: st.Title, Body: st.Body, Route: route})
		}
		ch.Tips = append(ch.Tips, src.Tips...)
		out = append(out, ch)
	}
	return out
}

// shotSet lists the languages that have their own screenshots. It is a var so
// the capture script and the tests agree on one source of truth.
var shotSet = map[string]bool{
	"en": true, "ru": true, "de": true, "es": true, "fr": true, "it": true,
	"pt": true, "md": true, "zh": true, "ja": true, "ko": true,
}

func shotFor(base, lang string) string {
	if base == "" {
		return ""
	}
	if !shotSet[lang] {
		lang = "en"
	}
	return fmt.Sprintf("/img/%s/%s.webp", lang, base)
}

// Help returns the hover-tooltip texts for a locale. Missing keys fall back to
// English per key, so a half-translated language shows English for the rest
// rather than a raw key.
func Help(lang string) map[string]string {
	en := bundleFor("en")
	out := map[string]string{}
	if en != nil {
		for k, v := range en.Help {
			out[k] = v
		}
	}
	if b := bundleFor(lang); b != nil && b != en {
		for k, v := range b.Help {
			if strings.TrimSpace(v) != "" {
				out[k] = v
			}
		}
	}
	return out
}
