// Copyright (c) 2026 Aristarh Ucolov.
package web

import "testing"

// BattlEye reads beserver_x64.cfg as `Key Value` text: a space splits the
// password and non-Latin letters never match what the client sends, so the
// panel refuses such a password instead of writing one that cannot log in.
func TestValidRConPassword(t *testing.T) {
	for pw, want := range map[string]bool{
		"":                        true, // none set: RCon off
		"S3cret_pass-1!":          true,
		"a~b|c{d}":                true,
		"two words":               false,
		"пароль":                  false,
		"tab\tinside":             false,
		string(make([]byte, 129)): false,
	} {
		if got := validRConPassword(pw); got != want {
			t.Errorf("validRConPassword(%q) = %v, want %v", pw, got, want)
		}
	}
}
