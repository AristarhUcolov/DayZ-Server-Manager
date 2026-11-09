// Copyright (c) 2026 Aristarh Ucolov.

package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"dayzmanager/internal/pbo"
	dztypes "dayzmanager/internal/types"
)

// cfggameplay.json settings DayZ does not read are ignored without a word:
// a misspelt key, one from an old game version, one in the wrong section.
// The keys it reads are the fields of its own data classes
// (3_Game/CfgGameplayDataJson.c in dta/scripts.pbo), so the check follows
// every game update with no list kept here.

// gpSchema maps a data class to its fields: field → the class of a nested
// section ("" for a plain value).
type gpSchema map[string]map[string]string

// retiredGameplayKeys are keys found in real files (Bohemia's own template
// among them) that DayZ never reads, with the one it does.
var retiredGameplayKeys = map[string]string{
	"disableColdAreaBuildingCheck": "disableColdAreaPlacementCheck",
}

var (
	reGpClass = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reGpField = regexp.MustCompile(`^\s*(?:ref\s+)?([A-Za-z_][A-Za-z0-9_<>]*)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=[^;]*)?;`)
)

// parseGameplaySchema reads the data classes out of the script: the fields
// declared directly in each class body (method bodies are deeper).
func parseGameplaySchema(src string) gpSchema {
	out := gpSchema{}
	depth := 0
	cur := ""
	for _, line := range strings.Split(src, "\n") {
		code := line
		if i := strings.Index(code, "//"); i >= 0 {
			code = code[:i]
		}
		if depth == 0 {
			if m := reGpClass.FindStringSubmatch(code); m != nil {
				cur = m[1]
				if out[cur] == nil {
					out[cur] = map[string]string{}
				}
			}
		}
		if depth == 1 && cur != "" && !strings.Contains(code, "(") {
			if m := reGpField.FindStringSubmatch(code); m != nil {
				typ, name := m[1], m[2]
				switch typ {
				case "return", "static", "const", "override", "proto", "private", "protected":
				default:
					nested := ""
					if strings.HasPrefix(typ, "ITEM_") {
						nested = typ
					}
					out[cur][name] = nested
				}
			}
		}
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if depth < 0 {
			depth = 0
		}
		if depth == 0 && strings.Contains(code, "}") {
			cur = ""
		}
	}
	if len(out["CfgGameplayJson"]) == 0 {
		return nil
	}
	return out
}

var gpSchemaCache struct {
	sync.Mutex
	key    string
	schema gpSchema
}

// gameplaySchema reads the schema from the server's own scripts (nil when
// they are not there); kept per scripts.pbo build.
func gameplaySchema(serverDir string) gpSchema {
	p := filepath.Join(serverDir, "dta", "scripts.pbo")
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
	gpSchemaCache.Lock()
	defer gpSchemaCache.Unlock()
	if gpSchemaCache.key == key {
		return gpSchemaCache.schema
	}
	var schema gpSchema
	if f, err := pbo.Open(p); err == nil {
		for _, e := range f.Find("CfgGameplayDataJson.c") {
			if data, err := f.Read(e); err == nil {
				schema = parseGameplaySchema(string(data))
				break
			}
		}
	}
	gpSchemaCache.key, gpSchemaCache.schema = key, schema
	return schema
}

// checkGameplayKeys lists the keys of the active mission's cfggameplay.json
// that the game does not read.
func checkGameplayKeys(o Options) []Issue {
	if o.Mission == "" {
		return nil
	}
	schema := gameplaySchema(o.ServerDir)
	if schema == nil {
		return nil
	}
	path := filepath.Join(dztypes.MissionDir(o.ServerDir, o.Mission), "cfggameplay.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]interface{}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &doc) != nil {
		return nil // a syntax error is checkJSON's to report
	}
	var out []Issue
	var walk func(obj map[string]interface{}, class, where string)
	walk = func(obj map[string]interface{}, class, where string) {
		fields := schema[class]
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			at := k
			if where != "" {
				at = where + "." + k
			}
			nested, ok := fields[k]
			if !ok {
				out = append(out, unknownGameplayKey(path, at, k, fields, where))
				continue
			}
			if sub, isObj := obj[k].(map[string]interface{}); isObj && nested != "" {
				walk(sub, nested, at)
			}
		}
	}
	walk(doc, "CfgGameplayJson", "")
	return out
}

func unknownGameplayKey(path, at, key string, fields map[string]string, where string) Issue {
	sugg := retiredGameplayKeys[key]
	if _, ok := fields[sugg]; !ok {
		sugg = ""
	}
	if sugg == "" {
		best, bestD := "", 4
		for f := range fields {
			if strings.EqualFold(f, key) {
				best, bestD = f, 0
				break
			}
			if d := editDistance(strings.ToLower(f), strings.ToLower(key)); d < bestD {
				best, bestD = f, d
			}
		}
		sugg = best
	}
	section := where
	if section == "" {
		section = "—"
	}
	if sugg != "" {
		return Issue{File: path, Severity: SevWarning, Code: "gameplay.key",
			Message: fmt.Sprintf("cfggameplay.json: %s is not a setting this DayZ version reads (did you mean %s?) — DayZ ignores it", at, sugg),
			Key:     "gameplay.keyMeant", Args: A("key", at, "meant", sugg, "section", section)}
	}
	return Issue{File: path, Severity: SevWarning, Code: "gameplay.key",
		Message: fmt.Sprintf("cfggameplay.json: %s is not a setting this DayZ version reads — DayZ ignores it", at),
		Key:     "gameplay.key", Args: A("key", at, "section", section)}
}

// editDistance is the Levenshtein distance of two short strings.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
