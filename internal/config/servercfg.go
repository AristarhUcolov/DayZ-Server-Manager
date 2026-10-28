// Copyright (c) 2026 Aristarh Ucolov.
//
// Minimal round-trip-friendly parser for DayZ server.cfg.
// server.cfg uses a semicolon-terminated key=value syntax with `class Name { ... };`
// blocks and // line comments. We preserve unknown keys and whole blocks verbatim.
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dayzmanager/internal/util"
)

type ServerCfgEntryKind int

const (
	EntryKV      ServerCfgEntryKind = iota
	EntryClass                      // class Foo { ... };
	EntryComment                    // // ...
	EntryBlank
)

type ServerCfgEntry struct {
	Kind    ServerCfgEntryKind
	Key     string
	Value   string // raw value as written (may include surrounding quotes)
	Comment string // trailing // comment
	// Raw is the verbatim line(s) as read. Save writes it back unchanged, so
	// the admin's spacing, comment alignment and tabs survive an edit; a
	// key=value line is only rewritten when its value changes, and then
	// just the value inside it.
	Raw   string
	dirty bool // the value changed since the file was read (or the key is new)
}

type ServerCfg struct {
	Entries []ServerCfgEntry
	crlf    bool // the file uses Windows line endings; Save keeps them
	noFinal bool // the last line has no line ending; Save keeps it that way
}

var (
	// A quoted value may hold a ';' (or a doubled "" quote) — matching up to
	// the first ';' cut `description = "PvE; no raids";` in half.
	reKV = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*("(?:[^"]|"")*"|[^;]*?)\s*;\s*(?://\s*(.*))?\s*$`)
	// The same line split around its value: prefix, value, the rest.
	reKVSplit = regexp.MustCompile(`^(\s*[A-Za-z_][A-Za-z0-9_]*\s*=\s*)("(?:[^"]|"")*"|[^;]*?)(\s*;.*)$`)
	reClass   = regexp.MustCompile(`^\s*class\s+[A-Za-z_][A-Za-z0-9_]*\b`)
)

func LoadServerCfg(path string) (*ServerCfg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &ServerCfg{}
	// Keep the file's own line endings (Windows tools write CRLF).
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	cfg.crlf = bytes.Contains(head[:n], []byte("\r\n"))
	// …and a last line without a line ending (DayZ's own file ends so).
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err == nil {
			cfg.noFinal = last[0] != '\n'
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<20)

	// Class collection tracks a multi-line `class Foo { ... };` block.
	// `collecting` flips true on the header line and stays true until the
	// first `{` opens the block and the matching `}` closes it. Using a
	// separate flag (not just brace depth) is required because stock DayZ
	// configs place the opening brace on its own next line, so depth is 0
	// for multiple iterations after the header — we must keep appending
	// until we've actually seen the body close.
	var collecting bool
	var depth int
	var sawOpen bool
	var classBuf strings.Builder

	for scan.Scan() {
		line := scan.Text()

		if collecting {
			classBuf.WriteString(line)
			classBuf.WriteByte('\n')
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if strings.Contains(line, "{") {
				sawOpen = true
			}
			if sawOpen && depth <= 0 {
				cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryClass, Raw: classBuf.String()})
				classBuf.Reset()
				collecting = false
				sawOpen = false
				depth = 0
			}
			continue
		}

		trim := strings.TrimSpace(line)
		if trim == "" {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryBlank, Raw: line})
			continue
		}
		if strings.HasPrefix(trim, "//") {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryComment, Raw: line})
			continue
		}
		if reClass.MatchString(line) {
			classBuf.WriteString(line)
			classBuf.WriteByte('\n')
			collecting = true
			depth = strings.Count(line, "{") - strings.Count(line, "}")
			if strings.Contains(line, "{") {
				sawOpen = true
			}
			// Rare single-line case: `class Foo { a=1; };`.
			if sawOpen && depth <= 0 {
				cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryClass, Raw: classBuf.String()})
				classBuf.Reset()
				collecting = false
				sawOpen = false
				depth = 0
			}
			continue
		}
		if m := reKV.FindStringSubmatch(line); m != nil {
			cfg.Entries = append(cfg.Entries, ServerCfgEntry{
				Kind: EntryKV, Key: m[1], Value: strings.TrimSpace(m[2]), Comment: m[3], Raw: line,
			})
			continue
		}
		// Unknown / malformed — keep verbatim.
		cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryComment, Raw: line})
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	// Flush any still-open class block so truncated/malformed files do not
	// silently lose content on round-trip.
	if collecting && classBuf.Len() > 0 {
		cfg.Entries = append(cfg.Entries, ServerCfgEntry{Kind: EntryClass, Raw: classBuf.String()})
	}
	return cfg, nil
}

func (c *ServerCfg) Save(path string) error {
	var b strings.Builder
	for _, e := range c.Entries {
		switch e.Kind {
		case EntryKV:
			b.WriteString(e.render())
		case EntryClass, EntryComment:
			b.WriteString(strings.TrimRight(e.Raw, "\n"))
		case EntryBlank:
			b.WriteString(e.Raw)
		}
		b.WriteByte('\n')
	}
	out := b.String()
	if c.noFinal {
		out = strings.TrimSuffix(out, "\n")
	}
	if c.crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return atomicWrite(path, []byte(out))
}

// render writes a key=value entry: the line as read when nothing changed, the
// same line with only the value swapped when it did, and a fresh line for a
// key the file did not have.
func (e ServerCfgEntry) render() string {
	if e.Raw != "" {
		if !e.dirty {
			return e.Raw
		}
		if m := reKVSplit.FindStringSubmatchIndex(e.Raw); m != nil {
			return e.Raw[:m[4]] + e.Value + e.Raw[m[5]:]
		}
	}
	line := e.Key + " = " + e.Value + ";"
	if e.Comment != "" {
		line += "  // " + e.Comment
	}
	return line
}

// Get returns the value (unquoted if it was quoted) and whether the key was found.
func (c *ServerCfg) Get(key string) (string, bool) {
	// Case-insensitive: serverDZ.cfg keys are, and a config spelling
	// `enablecfggameplayfile` used to read as absent and then get appended a
	// second time by Set.
	for _, e := range c.Entries {
		if e.Kind == EntryKV && strings.EqualFold(e.Key, key) {
			return unquote(e.Value), true
		}
	}
	return "", false
}

// IsQuoted reports whether the existing value for key is a quoted string (e.g.
// password, passwordAdmin, description) as opposed to a bare number/bool. The
// editor uses this so it can CLEAR a string field to "" without clobbering a
// numeric field that was simply left blank.
func (c *ServerCfg) IsQuoted(key string) bool {
	for _, e := range c.Entries {
		if e.Kind == EntryKV && strings.EqualFold(e.Key, key) {
			v := strings.TrimSpace(e.Value)
			return len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"'
		}
	}
	return false
}

// Set updates or inserts a key=value pair. When val is a string it is quoted;
// ints/bools are rendered bare.
func (c *ServerCfg) Set(key string, val interface{}) {
	c.setRendered(key, renderValue(val))
}

// setRendered updates EVERY entry with this key (a file that defines a key
// twice used to keep the second, stale definition, which is the one the server
// actually reads) and appends only when there is none.
func (c *ServerCfg) setRendered(key, rendered string) {
	found := false
	for i := range c.Entries {
		if c.Entries[i].Kind == EntryKV && strings.EqualFold(c.Entries[i].Key, key) {
			if c.Entries[i].Value != rendered {
				c.Entries[i].Value = rendered
				c.Entries[i].dirty = true
			}
			found = true
		}
	}
	if !found {
		c.Entries = append(c.Entries, ServerCfgEntry{Kind: EntryKV, Key: key, Value: rendered, dirty: true})
	}
}

// SetMissionTemplate rewrites the template="..." line inside class Missions/class DayZ.
// If the block does not exist yet (fresh serverDZ.cfg from a trimmed template),
// we append a standards-shaped block so the server will actually pick up the
// setting on next start. Returns true in all cases except when the class block
// exists but is so malformed we cannot splice into it safely.
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
		if re.MatchString(raw) {
			// ReplaceAllLiteralString so any `$` in the template name is
			// treated as a literal (not a regex back-reference).
			c.Entries[i].Raw = re.ReplaceAllLiteralString(raw, fmt.Sprintf(`template="%s"`, tmpl))
			return true
		}
		// class Missions exists but has no template line — inject one.
		idx := strings.Index(raw, "class DayZ")
		if idx < 0 {
			return false
		}
		brace := strings.Index(raw[idx:], "{")
		if brace < 0 {
			return false
		}
		pos := idx + brace + 1
		injected := fmt.Sprintf("\n\t\ttemplate=\"%s\";", tmpl)
		c.Entries[i].Raw = raw[:pos] + injected + raw[pos:]
		return true
	}
	// No block at all — append a fresh one.
	c.Entries = append(c.Entries, ServerCfgEntry{
		Kind: EntryClass,
		Raw: fmt.Sprintf(
			"class Missions\n{\n\tclass DayZ\n\t{\n\t\ttemplate=\"%s\";\n\t};\n};\n",
			tmpl,
		),
	})
	return true
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

// quoteCfg wraps a value in quotes for server.cfg. Enfusion escapes an
// embedded quote by DOUBLING it, not with a backslash — a backslash-escaped
// quote is parsed as a literal backslash followed by the end of the string.
func quoteCfg(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

// SetString writes a value that must stay a quoted string no matter what it
// looks like. renderValue un-quotes anything that parses as a number, which
// turned a numeric password ("1234") into a bare `password = 1234;` and locked
// every player out of the server while the panel reported success.
func (c *ServerCfg) SetString(key, val string) {
	c.setRendered(key, quoteCfg(val))
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
		return quoteCfg(v)
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
