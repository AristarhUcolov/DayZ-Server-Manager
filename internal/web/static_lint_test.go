// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"dayzmanager/internal/guide"
	"dayzmanager/internal/i18n"
)

// staticSources returns the panel's script and page sources by name.
func staticSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(path, "/vendor/") {
			return err
		}
		if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".html") {
			data, err := staticFS.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// lineOf is the 1-based line of byte offset i in src.
func lineOf(src string, i int) int { return strings.Count(src[:i], "\n") + 1 }

// callArgs returns the top-level arguments of the call whose "(" is at open.
func callArgs(src string, open int) []string {
	var args []string
	depth, start := 0, open+1
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return append(args, src[start:i])
			}
		case ',':
			if depth == 1 {
				args = append(args, src[start:i])
				start = i + 1
			}
		}
	}
	return args
}

// The DOM's own append()/prepend() print null, false and undefined as text —
// a copied server's folder check once read "…folder.null". h() skips empty
// children, but direct calls must never be handed a maybe-empty value.
func TestNoMaybeNullDOMAppend(t *testing.T) {
	call := regexp.MustCompile(`\.(append|prepend|replaceChildren|after|before)\(`)
	empty := regexp.MustCompile(`:\s*(null|false|undefined)\s*$`)
	notDOM := regexp.MustCompile(`(?i)(form|fd|params|qs|search|sp|formdata|data)\s*$`)
	var bad []string
	for name, src := range staticSources(t) {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		for _, m := range call.FindAllStringIndex(src, -1) {
			before := src[max(0, m[0]-40):m[0]]
			if notDOM.MatchString(before) {
				continue // FormData.append, URLSearchParams.append
			}
			for _, a := range callArgs(src, m[1]-1) {
				a = strings.TrimSpace(a)
				if strings.HasPrefix(a, "...") {
					continue
				}
				if empty.MatchString(a) || nullThenBranch(a) || regexp.MustCompile(`^[\w.!()\s]+&&\s*h\(`).MatchString(a) {
					bad = append(bad, name+":"+itoa(lineOf(src, m[0]))+": "+firstLine(a))
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error("append may print null/false: " + b)
	}
}

// nullThenBranch reports an argument that is a ternary with an empty "then"
// branch at its top level (`cond ? null : h(…)`), which the DOM prints as text.
// The same inside a nested h(…) call is fine: h() skips empty children.
func nullThenBranch(a string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(a); i++ {
		c := a[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '?':
			if depth != 0 || (i+1 < len(a) && (a[i+1] == '.' || a[i+1] == '?')) {
				continue // optional chaining, nullish coalescing
			}
			rest := strings.TrimSpace(a[i+1:])
			for _, e := range []string{"null", "false", "undefined"} {
				if strings.HasPrefix(rest, e) && strings.HasPrefix(strings.TrimSpace(rest[len(e):]), ":") {
					return true
				}
			}
		}
	}
	return false
}

// Every literal translation key the UI asks for exists in English (every
// other language falls back to English, and TestMergedKeysComplete keeps
// them complete), and every hover-help key exists in the English help.
// A missing key shows its raw name ("loadout.connect") on the page.
func TestUILiteralKeysExist(t *testing.T) {
	ui := []*regexp.Regexp{
		regexp.MustCompile(`\bt\(\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
		regexp.MustCompile(`\btn\(\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
		regexp.MustCompile(`\bi18n:\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
		regexp.MustCompile(`data-i18n(?:-[a-z]+)?="([a-z][\w-]*(?:\.[\w-]+)+)"`),
		regexp.MustCompile(`\bpageHeader\(\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
		regexp.MustCompile(`\bcardTitle\(\s*'[^']*',\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
		regexp.MustCompile(`\bfield\(\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`),
	}
	helpRe := regexp.MustCompile(`\bhelp\(\s*'([a-z][\w-]*(?:\.[\w-]+)+)'`)
	en := i18n.Get("en")
	help := guide.Help("en")
	missing := map[string]string{}
	for name, src := range staticSources(t) {
		for _, re := range ui {
			for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
				key := src[m[2]:m[3]]
				if _, ok := en[key]; !ok {
					missing["ui "+key] = name + ":" + itoa(lineOf(src, m[0]))
				}
			}
		}
		for _, m := range helpRe.FindAllStringSubmatchIndex(src, -1) {
			key := src[m[2]:m[3]]
			if _, ok := help[key]; !ok {
				missing["help "+key] = name + ":" + itoa(lineOf(src, m[0]))
			}
		}
	}
	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("%s used at %s has no English text", k, missing[k])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
