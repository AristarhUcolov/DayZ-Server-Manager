// Copyright (c) 2026 Aristarh Ucolov.
package i18n

import "testing"

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
