// Copyright (c) 2026 Aristarh Ucolov.
//
// Minimal round-trip-friendly parser for DayZ server.cfg.
// server.cfg uses a semicolon-terminated key=value syntax with `class Name { ... };`
// blocks and // line comments. We preserve unknown keys and whole blocks verbatim.
package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dayzmanager/internal/util"
)

type ServerCfgEntryKind int

const (
	EntryKV ServerCfgEntryKind = iota
	EntryClass       // class Foo { ... };
	EntryComment     // // ...
	EntryBlank
)

type ServerCfgEntry struct {
	Kind    ServerCfgEntryKind
	Key     string
	Value   string // raw value as written (may include surrounding quotes)
	Comment string // trailing // comment
	Raw     string // verbatim content for Class/Comment/Blank
}

type ServerCfg struct {
	Entries []ServerCfgEntry
}

var (
	reKV      = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*;\s*(?://\s*(.*))?\s*$`)
	reClass   = regexp.MustCompile(`^\s*class\s+[A-Za-z_][A-Za-z0-9_]*\b`)
)

func LoadServerCfg(path string) (*ServerCfg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &ServerCfg{}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<20)

	var inClass int // brace depth
	var classBuf strings.Builder

	for scan.Scan() {
		line := scan.Text()

		if inClass > 0 {
			classBuf.WriteString(line)
			classBuf.WriteByte('\n')
			inClass += strings.Count(line, "{") - strings.Count(line, "}")
			if inClass <= 0 {
				cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryClass, Raw: classBuf.String()})
				classBuf.Reset()
				inClass = 0
			}
			continue
		}

		trim := strings.TrimSpace(line)
		if trim == "" {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryBlank, Raw: ""})
			continue
		}
		if strings.HasPrefix(trim, "//") {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryComment, Raw: line})
			continue
		}
		if reClass.MatchString(line) {
			classBuf.WriteString(line)
			classBuf.WriteByte('\n')
			inClass = strings.Count(line, "{") - strings.Count(line, "}")
			if inClass == 0 && strings.Contains(line, "};") {
				cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryClass, Raw: classBuf.String()})
				classBuf.Reset()
			}
			continue
		}
		if m := reKV.FindStringSubmatch(line); m != nil {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{
				Kind: EntryKV, Key: m[1], Value: strings.TrimSpace(m[2]), Comment: m[3],
			})
			continue
		}
		// Unknown / malformed — keep verbatim.
		cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryComment, Raw: line})
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *ServerCfg) Save(path string) error {
	var b strings.Builder
	for _, e := range c.Entries {
		switch e.Kind {
		case EntryKV:
			b.WriteString(e.Key)
			b.WriteString(" = ")
			b.WriteString(e.Value)
			b.WriteByte(';')
			if e.Comment != "" {
				b.WriteString("  // ")
				b.WriteString(e.Comment)
			}
			b.WriteByte('\n')
		case EntryClass, EntryComment:
			b.WriteString(strings.TrimRight(e.Raw, "\n"))
			b.WriteByte('\n')
		case EntryBlank:
			b.WriteByte('\n')
		}
	}
	return atomicWrite(path, []byte(b.String()))
}

// Get returns the value (unquoted if it was quoted) and whether the key was found.
func (c *ServerCfg) Get(key string) (string, bool) {
	for _, e := range c.Entries {
		if e.Kind == EntryKV && e.Key == key {
			return unquote(e.Value), true
		}
	}
	return "", false
}

// Set updates or inserts a key=value pair. When val is a string it is quoted;
// ints/bools are rendered bare.
func (c *ServerCfg) Set(key string, val interface{}) {
	rendered := renderValue(val)
	for i := range c.Entries {
		if c.Entries[i].Kind == EntryKV && c.Entries[i].Key == key {
			c.Entries[i].Value = rendered
			return
		}
	}
	c.Entries = append(c.Entries, ServerCfgEntry{Kind: EntryKV, Key: key, Value: rendered})
}

// SetMissionTemplate rewrites the template="..." line inside class Missions/class DayZ.
func (c *ServerCfg) SetMissionTemplate(tmpl string) bool {
	for i := range c.Entries {
		if c.Entries[i].Kind != EntryClass {
			continue
		}
		raw := c.Entries[i].Raw
		if !strings.Contains(raw, "class Missions") {
			continue
		}
		re := regexp.MustCompile(`template\s*=\s*"[^"]*"`)
		if !re.MatchString(raw) {
			return false
		}
		c.Entries[i].Raw = re.ReplaceAllString(raw, fmt.Sprintf(`template="%s"`, tmpl))
		return true
	}
	return false
}

// MissionTemplate extracts the currently configured mission template.
func (c *ServerCfg) MissionTemplate() string {
	for _, e := range c.Entries {
		if e.Kind != EntryClass || !strings.Contains(e.Raw, "class Missions") {
			continue
		}
		m := regexp.MustCompile(`template\s*=\s*"([^"]*)"`).FindStringSubmatch(e.Raw)
		if len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// AsMap returns a flat key->unquoted-string map (kv entries only). Handy for the UI.
func (c *ServerCfg) AsMap() map[string]string {
	m := make(map[string]string, len(c.Entries))
	for _, e := range c.Entries {
		if e.Kind == EntryKV {
			m[e.Key] = unquote(e.Value)
		}
	}
	return m
}

// ---------------------------------------------------------------------------

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

func renderValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		// If caller already quoted it, leave alone; else detect numeric/bool shorthand.
		if _, err := strconv.Atoi(v); err == nil {
			return v
		}
		if v == "true" || v == "false" {
			return v
		}
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			return v
		}
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func atomicWrite(path string, data []byte) error {
	_ = util.BackupBeforeWrite(path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
