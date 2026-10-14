// Copyright (c) 2026 Aristarh Ucolov.
package i18n

// tr is one key's text in every UI language. Keys added from v1.0.0 on are
// declared this way — all eleven translations side by side — instead of being
// scattered over eleven locale files, which made it far too easy to forget one.
// init() merges them into the per-language bundles, so TestLocaleParity still
// sees one flat key set; TestMergedKeysComplete guards that every entry carries
// every language.
type tr map[string]string

// mergedSets lists every side-by-side key set to fold into the bundles.
var mergedSets = []map[string]tr{v100, v100b, v100c}

func init() {
	for _, set := range mergedSets {
		for key, byLang := range set {
			for code, b := range locales {
				if s, ok := byLang[code]; ok {
					b[key] = s
				}
			}
		}
	}
}
