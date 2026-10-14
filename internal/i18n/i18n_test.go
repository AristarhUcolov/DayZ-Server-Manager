// Copyright (c) 2026 Aristarh Ucolov.
package i18n

import (
	"strings"
	"testing"
)

// TestLocaleParity ensures every locale defines exactly the same key set as the
// English base — no missing keys (which would fall back to English) and no
// stray keys (typos that would never be read).
func TestLocaleParity(t *testing.T) {
	for code, loc := range locales {
		if code == "en" {
			continue
		}
		for k := range en {
			if _, ok := loc[k]; !ok {
				t.Errorf("locale %q missing key %q", code, k)
			}
		}
		for k := range loc {
			if _, ok := en[k]; !ok {
				t.Errorf("locale %q has stray key %q not in en", code, k)
			}
		}
	}
}

// TestGetFallsBackToEnglish verifies Get overlays on English so a hypothetical
// missing key still resolves to a real string.
func TestGetFallsBackToEnglish(t *testing.T) {
	for _, code := range Supported() {
		b := Get(code)
		if len(b) < len(en) {
			t.Errorf("Get(%q) returned %d keys, want >= %d", code, len(b), len(en))
		}
		if b["nav.dashboard"] == "" {
			t.Errorf("Get(%q) has empty nav.dashboard", code)
		}
	}
}

// TestMoldovanAlias verifies the legacy "ro" code still resolves to the Moldovan
// ("md") bundle so a saved "ro" preference doesn't silently drop to English.
func TestMoldovanAlias(t *testing.T) {
	if Name("ro") != "Moldovenească" {
		t.Errorf(`Name("ro") = %q, want "Moldovenească"`, Name("ro"))
	}
	if Get("ro")["nav.dashboard"] != Get("md")["nav.dashboard"] {
		t.Errorf("Get(\"ro\") did not resolve to the md bundle")
	}
	if Get("md")["nav.settings"] == en["nav.settings"] {
		t.Errorf("md bundle looks untranslated for nav.settings")
	}
}

// TestNoEnglishFallbackLeftover spot-checks a few of the long, hard hints across
// every non-English locale: they must differ from the English text, proving the
// translation is real and not the English overlay showing through.
func TestNoEnglishFallbackLeftover(t *testing.T) {
	sample := []string{"validator.fix.hint", "settings.autoupdate.check.hint", "wipe.warning", "rcon.notConfigured.hint"}
	for code, loc := range locales {
		if code == "en" {
			continue
		}
		for _, k := range sample {
			if loc[k] == en[k] {
				t.Errorf("locale %q key %q still equals English (untranslated)", code, k)
			}
		}
	}
}

// TestMergedKeysComplete ensures every side-by-side key (merge.go) carries a
// non-empty translation for every supported language — a missing one would
// otherwise only surface as a parity failure far from the key's definition.
func TestMergedKeysComplete(t *testing.T) {
	for _, set := range mergedSets {
		for key, byLang := range set {
			// The languages written in Go must all be present; JSON languages
			// (lang/*.json) carry the key themselves and may be overridden here.
			for _, l := range localeOrder {
				if _, fromJSON := jsonLangCodes()[l.Code]; fromJSON {
					continue
				}
				if byLang[l.Code] == "" {
					t.Errorf("merged key %q has no %q translation", key, l.Code)
				}
			}
			for code := range byLang {
				if _, ok := locales[code]; !ok {
					t.Errorf("merged key %q has unknown language %q", key, code)
				}
			}
		}
	}
}

func jsonLangCodes() map[string]bool {
	out := map[string]bool{}
	entries, _ := langFS.ReadDir("lang")
	for _, e := range entries {
		out[normalize(strings.TrimSuffix(e.Name(), ".json"))] = true
	}
	return out
}

// Every JSON language file must load: a broken file is skipped at runtime
// (the manager must still start), so this test is where it gets noticed.
func TestJSONLanguagesLoad(t *testing.T) {
	for _, e := range jsonLangErrors {
		t.Errorf("language file failed to load: %s", e)
	}
	for code := range jsonLangCodes() {
		if Name(code) == "" {
			t.Errorf("JSON language %q is not in the picker", code)
		}
	}
}

func TestMatchBrowserLanguage(t *testing.T) {
	cases := map[string]string{"en-US": "en", "ru": "ru", "RU-ru": "ru", "ro-RO": "md", "xx-YY": "", "": ""}
	// Brazilian Portuguese uses the Portuguese bundle.
	cases["pt-BR"] = "pt"
	cases["pt-PT"] = "pt"
	if _, ok := locales["zh-tw"]; ok {
		cases["zh-TW"] = "zh-tw"
		cases["zh-Hant-HK"] = "zh-tw"
		cases["zh-CN"] = "zh"
	}
	for tag, want := range cases {
		if got := Match(tag); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
	if Dir("en") != "ltr" {
		t.Error("English must be left-to-right")
	}
	if _, ok := locales["ar"]; ok && Dir("ar") != "rtl" {
		t.Error("Arabic must be right-to-left")
	}
}
