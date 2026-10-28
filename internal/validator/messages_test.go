// Copyright (c) 2026 Aristarh Ucolov.
package validator

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"dayzmanager/internal/i18n"
)

// Every issue and every repair the validator reports has its text in the
// panel's languages ("vmsg.<key>", "vfix.<key>"); without one the page falls
// back to English in the middle of a translated page.
func TestMessagesAreTranslated(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	issueRe := regexp.MustCompile(`(?:Code|Key):\s*"([A-Za-z0-9.]+)"`)
	fixRe := regexp.MustCompile(`\bF\("([A-Za-z0-9.]+)"`)
	// Keys built at run time.
	issues := map[string]bool{"types.unknownFlagMany": true}
	fixes := map[string]bool{"num.minNominal": true, "num.restockNeg": true, "num.quantRange": true, "num.quantOrder": true,
		"modDuplicate": true, "modMissing": true}
	for _, f := range files {
		if len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range issueRe.FindAllStringSubmatch(string(src), -1) {
			issues[m[1]] = true
		}
		for _, m := range fixRe.FindAllStringSubmatch(string(src), -1) {
			if m[1][len(m[1])-1] != '.' { // "num."+k: listed above
				fixes[m[1]] = true
			}
		}
	}
	// ports.clash only ever goes out with a Key of its own.
	delete(issues, "ports.clash")
	if len(issues) < 30 || len(fixes) < 20 {
		t.Fatalf("found only %d issue and %d repair keys — the patterns no longer match the code", len(issues), len(fixes))
	}
	for _, lang := range []string{"en", "ru"} {
		for k := range issues {
			if i18n.Text(lang, "vmsg."+k) == "vmsg."+k || i18n.Text(lang, "vmsg."+k) == "" {
				t.Errorf("%s: no text for vmsg.%s", lang, k)
			}
		}
		for k := range fixes {
			if i18n.Text(lang, "vfix."+k) == "vfix."+k || i18n.Text(lang, "vfix."+k) == "" {
				t.Errorf("%s: no text for vfix.%s", lang, k)
			}
		}
	}
}
