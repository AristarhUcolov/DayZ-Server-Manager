// Copyright (c) 2026 Aristarh Ucolov.
package guide

import (
	"sort"
	"strings"
	"testing"
)

// Every translation must match the English structure exactly. A language that
// drops a step or a tip would silently render a shorter chapter, and a step
// count that disagrees with specs.Routes would attach a jump button to the
// wrong step.
func TestEveryLanguageMatchesEnglishShape(t *testing.T) {
	en := Get("en")
	if len(en) != len(specs) {
		t.Fatalf("English guide has %d chapters, specs define %d", len(en), len(specs))
	}

	for _, lang := range Languages() {
		if lang == "en" {
			continue
		}
		got := Get(lang)
		if len(got) != len(en) {
			t.Errorf("%s: %d chapters, want %d", lang, len(got), len(en))
			continue
		}
		for i := range en {
			if got[i].ID != en[i].ID {
				t.Errorf("%s: chapter %d is %q, want %q", lang, i, got[i].ID, en[i].ID)
				continue
			}
			if len(got[i].Steps) != len(en[i].Steps) {
				t.Errorf("%s/%s: %d steps, want %d", lang, en[i].ID, len(got[i].Steps), len(en[i].Steps))
			}
			if len(got[i].Tips) != len(en[i].Tips) {
				t.Errorf("%s/%s: %d tips, want %d", lang, en[i].ID, len(got[i].Tips), len(en[i].Tips))
			}
		}
	}
}

// Untranslated text is worse than an obvious gap: it looks finished.
func TestNoEmptyOrUntranslatedText(t *testing.T) {
	en := Get("en")
	for _, lang := range Languages() {
		for ci, ch := range Get(lang) {
			if strings.TrimSpace(ch.Title) == "" || strings.TrimSpace(ch.Intro) == "" {
				t.Errorf("%s/%s: empty title or intro", lang, ch.ID)
			}
			for si, st := range ch.Steps {
				if strings.TrimSpace(st.Title) == "" || strings.TrimSpace(st.Body) == "" {
					t.Errorf("%s/%s step %d: empty title or body", lang, ch.ID, si)
				}
			}
			for ti, tip := range ch.Tips {
				if strings.TrimSpace(tip) == "" {
					t.Errorf("%s/%s tip %d: empty", lang, ch.ID, ti)
				}
			}
			// A non-English chapter whose intro is byte-identical to English
			// was almost certainly copy-pasted and never translated.
			if lang != "en" && ci < len(en) && ch.Intro == en[ci].Intro {
				t.Errorf("%s/%s: intro is identical to English — untranslated?", lang, ch.ID)
			}
		}
	}
}

// Every step that specs says has a route must actually carry it, and no step
// may point at a route that does not exist in the panel.
func TestStepRoutesAreValid(t *testing.T) {
	known := map[string]bool{
		"dashboard": true, "guide": true, "server": true, "gameplay": true,
		"mods": true, "events": true, "weather": true, "types": true,
		"moded": true, "attachments": true, "missiondb": true, "files": true,
		"profiles": true, "battleye": true, "logs": true, "admlog": true,
		"players": true, "rcon": true, "validator": true, "sync": true,
		"wipe": true, "settings": true,
	}
	for _, lang := range Languages() {
		for ci, ch := range Get(lang) {
			for si, st := range ch.Steps {
				want := ""
				if si < len(specs[ci].Routes) {
					want = specs[ci].Routes[si]
				}
				if st.Route != want {
					t.Errorf("%s/%s step %d: route %q, want %q", lang, ch.ID, si, st.Route, want)
				}
				if st.Route != "" && !known[st.Route] {
					t.Errorf("%s/%s step %d: route %q is not a panel route", lang, ch.ID, si, st.Route)
				}
			}
		}
	}
}

// Help must be complete in every language — these are short strings shown on
// hover, and an English tooltip in a Korean panel is a visible gap.
func TestHelpKeysAreCompleteInEveryLanguage(t *testing.T) {
	loadOnce.Do(load)
	en := bundles["en"]
	if en == nil {
		t.Fatal("no English bundle")
	}
	var enKeys []string
	for k := range en.Help {
		enKeys = append(enKeys, k)
	}
	sort.Strings(enKeys)

	for lang, b := range bundles {
		if lang == "en" {
			continue
		}
		var missing []string
		for _, k := range enKeys {
			if strings.TrimSpace(b.Help[k]) == "" {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s: %d help keys missing: %v", lang, len(missing), missing)
		}
		for k := range b.Help {
			if _, ok := en.Help[k]; !ok {
				t.Errorf("%s: help key %q does not exist in English", lang, k)
			}
		}
	}
}

// The chapter screenshot must resolve to a real language directory.
func TestChapterImagesUseTheRequestedLanguage(t *testing.T) {
	for _, lang := range Languages() {
		for _, ch := range Get(lang) {
			if ch.Image == "" {
				t.Errorf("%s/%s: no screenshot", lang, ch.ID)
				continue
			}
			want := "/img/" + lang + "/"
			if !shotSet[lang] {
				want = "/img/en/"
			}
			if !strings.HasPrefix(ch.Image, want) {
				t.Errorf("%s/%s: image %q does not start with %q", lang, ch.ID, ch.Image, want)
			}
		}
	}
}
