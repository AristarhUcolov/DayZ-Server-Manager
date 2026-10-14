// Copyright (c) 2026 Aristarh Ucolov.
package guide

import (
	"encoding/json"
	"testing"
)

// Every key of the vanilla cfggameplay.json — groups included — has a
// description in English and Russian, so no field of the Gameplay editor is
// left without its explanation. (Keys used to drift: four were misspelled and
// thirty were missing.)
func TestGameplayDefaultsHaveHelp(t *testing.T) {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(GameplayDefaults), &root); err != nil {
		t.Fatalf("GameplayDefaults is not valid JSON: %v", err)
	}
	var walk func(m map[string]interface{})
	n := 0
	walk = func(m map[string]interface{}) {
		for k, v := range m {
			n++
			if gameplayHelpEN[k] == "" {
				t.Errorf("no English help for %q", k)
			}
			if gameplayHelpRU[k] == "" {
				t.Errorf("no Russian help for %q", k)
			}
			if sub, ok := v.(map[string]interface{}); ok {
				walk(sub)
			}
		}
	}
	walk(root)
	if n < 70 {
		t.Fatalf("only %d keys walked — the defaults look truncated", n)
	}
	// The temperatures are left out of the defaults (they differ per map) but
	// files carry them, so they need help too.
	for _, k := range []string{"environmentMinTemps", "environmentMaxTemps"} {
		if gameplayHelpEN[k] == "" || gameplayHelpRU[k] == "" {
			t.Errorf("no help for %q", k)
		}
	}
}
