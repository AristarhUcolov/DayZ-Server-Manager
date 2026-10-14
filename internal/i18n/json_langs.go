// Copyright (c) 2026 Aristarh Ucolov.
//
// Languages shipped as JSON (lang/<code>.json): one self-contained file per
// language — its native name, direction and every UI string. Adding a
// language is dropping in a file; TestLocaleParity still demands the full key
// set, and Get() overlays on English so nothing can surface as a raw key.
//
// This file sorts before merge.go, so its init() registers the JSON languages
// before the side-by-side key sets are merged: a later key can carry, say, a
// "pl" translation right next to the others.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed lang/*.json
var langFS embed.FS

type langFile struct {
	Name     string            `json:"name"`
	RTL      bool              `json:"rtl,omitempty"`
	Messages map[string]string `json:"messages"`
}

// pickerOrder is the order of the language picker: the two primary languages,
// then roughly by region. Languages not listed are appended alphabetically.
var pickerOrder = []string{
	"en", "ru", "de", "fr", "es", "it", "pt", "pl", "cs", "hu",
	"nl", "sv", "md", "tr", "ar", "hi", "th", "vi", "id", "zh", "zh-tw", "ja", "ko",
}

// jsonLangErrors collects files that failed to load (reported by a test
// rather than crashing the manager at start-up).
var jsonLangErrors []string

func init() {
	entries, _ := langFS.ReadDir("lang")
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		code := normalize(strings.TrimSuffix(e.Name(), ".json"))
		data, err := langFS.ReadFile("lang/" + e.Name())
		var f langFile
		if err == nil {
			err = json.Unmarshal(data, &f)
		}
		if err != nil || f.Name == "" || len(f.Messages) == 0 {
			jsonLangErrors = append(jsonLangErrors, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		locales[code] = Bundle(f.Messages)
		localeOrder = append(localeOrder, Language{Code: code, Name: f.Name, RTL: f.RTL})
	}
	rank := map[string]int{}
	for i, c := range pickerOrder {
		rank[c] = i
	}
	pos := func(c string) int {
		if r, ok := rank[c]; ok {
			return r
		}
		return len(pickerOrder)
	}
	sort.SliceStable(localeOrder, func(i, j int) bool {
		pi, pj := pos(localeOrder[i].Code), pos(localeOrder[j].Code)
		if pi != pj {
			return pi < pj
		}
		return localeOrder[i].Code < localeOrder[j].Code
	})
}

// Dir is the text direction of a locale: "rtl" or "ltr".
func Dir(code string) string {
	code = normalize(code)
	for _, l := range localeOrder {
		if l.Code == code && l.RTL {
			return "rtl"
		}
	}
	return "ltr"
}

// Match picks the supported locale for a browser language tag ("pt-BR",
// "zh-Hant-TW", "ro"), or "" when none fits. Regional variants map to their
// own bundle when one exists (zh-TW), else to the base language (pt-BR → pt).
func Match(tag string) string {
	t := normalize(tag)
	if t == "" {
		return ""
	}
	if _, ok := locales[t]; ok {
		return t
	}
	base, rest, _ := strings.Cut(t, "-")
	has := func(c string) bool { _, ok := locales[c]; return ok }
	switch base {
	case "zh":
		if (strings.Contains(rest, "hant") || strings.Contains(rest, "tw") || strings.Contains(rest, "hk") || strings.Contains(rest, "mo")) && has("zh-tw") {
			return "zh-tw"
		}
	case "ro", "mo":
		return "md"
	}
	if has(base) {
		return base
	}
	return ""
}
