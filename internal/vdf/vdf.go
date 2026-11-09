// Copyright (c) 2026 Aristarh Ucolov.
//
// Package vdf reads Valve's KeyValues text files (.vdf, .acf): "key" "value"
// pairs and "key" { … } blocks — Steam's workshop manifests and SteamCMD's
// own settings. Comments are skipped; conditionals are not used there.
package vdf

import "strings"

// Parse returns the top level of a KeyValues text: nested blocks are
// map[string]interface{}, values strings. A key that repeats keeps the last.
func Parse(s string) map[string]interface{} {
	toks := tokens(s)
	pos := 0
	var block func() map[string]interface{}
	block = func() map[string]interface{} {
		m := map[string]interface{}{}
		for pos < len(toks) {
			t := toks[pos]
			if t == "}" {
				pos++
				return m
			}
			pos++
			if pos >= len(toks) {
				break
			}
			if toks[pos] == "{" {
				pos++
				m[t] = block()
			} else {
				m[t] = toks[pos]
				pos++
			}
		}
		return m
	}
	return block()
}

// Block follows path down nested blocks, matching keys case-insensitively
// (Steam writes "Software"/"Valve" in one file and "software"/"valve" in
// another). nil when a step is missing.
func Block(m map[string]interface{}, path ...string) map[string]interface{} {
	for _, p := range path {
		if m == nil {
			return nil
		}
		next, ok := m[p].(map[string]interface{})
		if !ok {
			next = nil
			for k, v := range m {
				if strings.EqualFold(k, p) {
					next, _ = v.(map[string]interface{})
					break
				}
			}
		}
		m = next
	}
	return m
}

// String is a value of m by key (exact, then case-insensitive); "" when it
// is missing or a block.
func String(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			s, _ := v.(string)
			return s
		}
	}
	return ""
}

func tokens(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '{' || c == '}':
			out = append(out, string(c))
		case c == '"':
			var b strings.Builder
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
			}
			out = append(out, b.String())
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		}
	}
	return out
}
