// Copyright (c) 2026 Aristarh Ucolov.
//
// v1.0.0 checks: the human-factor mistakes that break a server without a clear
// message — a trailing comma in a JSON config, a missing semicolon in
// serverDZ.cfg, a mod enabled but never copied, a key never synced, a UTF-16
// file saved by the wrong editor, a stray "&" in XML — each with a fix where
// one is safe and deterministic. Every fix backs the file up first.
package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"dayzmanager/internal/config"
	"dayzmanager/internal/mods"
	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

// Options carries the server context the deeper checks need. Zero values
// simply skip the checks that depend on them.
type Options struct {
	ServerDir   string
	Mission     string
	ServerCfg   string // file name relative to ServerDir, e.g. serverDZ.cfg
	ProfilesDir string // absolute profiles folder (mod JSON configs)
	Mods        []string
	ServerMods  []string
	ServerPort  int
	RConPort    int
	WorldSize   float64 // map edge in metres; 0 = unknown
	Linux       bool    // enforce the Linux server's lower-case mod rule
}

func (o Options) missionDir() string {
	if o.Mission == "" {
		return ""
	}
	return dztypes.MissionDir(o.ServerDir, o.Mission)
}

// ---------------------------------------------------------------------------
// JSON

// jsonFiles lists the JSON configs worth checking: every *.json in the mission
// folder and in profiles (mod settings — VPP, Expansion, Trader…), skipping
// logs and anything over 8 MB.
func jsonFiles(o Options) []string {
	var out []string
	add := func(root string, maxDepth int) {
		if root == "" {
			return
		}
		base := strings.Count(filepath.Clean(root), string(filepath.Separator))
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				low := strings.ToLower(info.Name())
				if p != root && (strings.HasPrefix(low, ".") || low == "logs" || low == "storage_1" || strings.HasPrefix(low, "storage_")) {
					return filepath.SkipDir
				}
				if strings.Count(filepath.Clean(p), string(filepath.Separator))-base > maxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(p), ".json") && info.Size() < 8<<20 && !strings.Contains(info.Name(), ".bak") {
				out = append(out, p)
			}
			return nil
		})
	}
	add(o.missionDir(), 3)
	add(o.ProfilesDir, 4)
	return out
}

// lenientJSON strips // and /* */ comments and trailing commas — the two
// things people add by hand that DayZ's strict JSON loader rejects.
func lenientJSON(src []byte) []byte {
	var b bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			i++ // skip '/'
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r' || src[j] == '\n') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue // trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.Bytes()
}

func lineOf(data []byte, offset int64) int {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return bytes.Count(data[:offset], []byte{'\n'}) + 1
}

func checkJSON(o Options) []Issue {
	var out []Issue
	for _, p := range jsonFiles(o) {
		data, err := os.ReadFile(p)
		if err != nil || len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		if isUTF16(data) {
			continue // reported by checkEncoding
		}
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		var v interface{}
		err = json.Unmarshal(data, &v)
		if err == nil {
			continue
		}
		is := Issue{File: p, Severity: SevError, Code: "json.syntax",
			Message: "invalid JSON: " + err.Error() + " — DayZ (or the mod) will fail to load this file", Args: A("err", err)}
		if se, ok := err.(*json.SyntaxError); ok {
			is.Line = lineOf(data, se.Offset)
		}
		if json.Valid(lenientJSON(data)) {
			is.Fix = "json.syntax"
			is.Message = "invalid JSON (comments or a trailing comma): " + err.Error()
			is.Key = "json.lenient"
		}
		out = append(out, is)
	}
	return out
}

func fixJSON(o Options) ([]string, error) {
	var done []string
	for _, p := range jsonFiles(o) {
		data, err := os.ReadFile(p)
		if err != nil || isUTF16(data) {
			continue
		}
		clean := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		if json.Valid(clean) {
			continue
		}
		fixed := lenientJSON(clean)
		if !json.Valid(fixed) {
			continue
		}
		if err := util.BackupBeforeWrite(p); err != nil {
			return done, err
		}
		if err := writeAtomic(p, fixed); err != nil {
			return done, err
		}
		done = append(done, "removed comments / trailing commas from "+filepath.Base(p))
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// Encoding

func isUTF16(data []byte) bool {
	return len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF))
}

func toUTF8(data []byte) []byte {
	be := data[0] == 0xFE
	data = data[2:]
	u := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if be {
			u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			u = append(u, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return []byte(string(utf16.Decode(u)))
}

// configFiles lists the text configs whose encoding matters.
func configFiles(o Options) []string {
	var out []string
	if md := o.missionDir(); md != "" {
		_ = filepath.Walk(md, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".xml", ".json", ".c":
				if !strings.Contains(info.Name(), ".bak") {
					out = append(out, p)
				}
			}
			return nil
		})
	}
	if o.ServerCfg != "" {
		out = append(out, filepath.Join(o.ServerDir, o.ServerCfg))
	}
	return out
}

func checkEncoding(o Options) []Issue {
	var out []Issue
	for _, p := range configFiles(o) {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		head := make([]byte, 2)
		n, _ := f.Read(head)
		f.Close()
		if n == 2 && isUTF16(head) {
			out = append(out, Issue{File: p, Severity: SevError, Code: "encoding.utf16", Fix: "encoding.utf16",
				Message: "saved as UTF-16 (some editors do this) — DayZ reads its configs as UTF-8/ANSI and will misread the file"})
		}
	}
	return out
}

func fixEncoding(o Options) ([]string, error) {
	var done []string
	for _, p := range configFiles(o) {
		data, err := os.ReadFile(p)
		if err != nil || !isUTF16(data) {
			continue
		}
		if err := util.BackupBeforeWrite(p); err != nil {
			return done, err
		}
		if err := writeAtomic(p, toUTF8(data)); err != nil {
			return done, err
		}
		done = append(done, "converted "+filepath.Base(p)+" from UTF-16 to UTF-8")
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// XML: a bare "&" (e.g. in a hostname-like comment or item note) breaks
// strict parsers; escaping it is always safe.

// escapeBareAmps turns every "&" that does not start a valid entity into &amp;.
func escapeBareAmps(s string) (string, int) {
	var b strings.Builder
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		ok := false
		if j < len(s) && s[j] == '#' {
			j++
			hex := j < len(s) && (s[j] == 'x' || s[j] == 'X')
			if hex {
				j++
			}
			start := j
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || (hex && strings.ContainsRune("abcdefABCDEF", rune(s[j])))) {
				j++
			}
			ok = j > start && j < len(s) && s[j] == ';'
		} else {
			start := j
			for j < len(s) && ((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z') || (j > start && s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			ok = j > start && j < len(s) && s[j] == ';'
		}
		if ok {
			b.WriteByte('&')
			continue
		}
		b.WriteString("&amp;")
		n++
	}
	return b.String(), n
}

func fixXMLAmps(files []string) ([]string, error) {
	var done []string
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fixed, n := escapeBareAmps(string(data))
		if n == 0 {
			continue
		}
		if err := util.BackupBeforeWrite(p); err != nil {
			return done, err
		}
		if err := writeAtomic(p, []byte(fixed)); err != nil {
			return done, err
		}
		done = append(done, fmt.Sprintf("escaped %d bare '&' in %s", n, filepath.Base(p)))
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// serverDZ.cfg

var (
	reCfgAssign = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)(\[\])?\s*=\s*(.*)$`)
	reCfgClass  = regexp.MustCompile(`^\s*class\s+\w+`)
)

// stripCfgComment removes a trailing // comment outside quotes.
func stripCfgComment(line string) (code, comment string) {
	inStr := false
	for i := 0; i+1 < len(line); i++ {
		switch {
		case line[i] == '"':
			inStr = !inStr
		case !inStr && line[i] == '/' && line[i+1] == '/':
			return line[:i], line[i:]
		}
	}
	return line, ""
}

// cfgMissingSemicolons returns the 1-based line numbers of simple assignments
// that do not end with ';' — DayZ then silently ignores everything after.
func cfgMissingSemicolons(src string) []int {
	var out []int
	inArray := false
	for i, line := range strings.Split(src, "\n") {
		code, _ := stripCfgComment(strings.TrimRight(line, "\r"))
		t := strings.TrimSpace(code)
		if inArray {
			if strings.Contains(t, "}") {
				inArray = false
			}
			continue
		}
		if t == "" || reCfgClass.MatchString(t) || t == "{" || strings.HasPrefix(t, "}") {
			continue
		}
		m := reCfgAssign.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		val := strings.TrimSpace(m[3])
		if strings.HasPrefix(val, "{") && !strings.Contains(val, "}") {
			inArray = true // multi-line array: motd[] = { ... };
			continue
		}
		if !strings.HasSuffix(val, ";") {
			out = append(out, i+1)
		}
	}
	return out
}

func checkServerCfg(o Options) []Issue {
	if o.ServerCfg == "" {
		return nil
	}
	path := filepath.Join(o.ServerDir, o.ServerCfg)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	src := string(raw)
	var out []Issue
	for _, ln := range cfgMissingSemicolons(src) {
		out = append(out, Issue{File: path, Line: ln, Severity: SevError, Code: "cfg.semicolon", Fix: "cfg.semicolon",
			Message: "line is missing its closing ';' — DayZ stops reading the setting (and often the ones after it)"})
	}
	// Duplicate top-level keys: the later one wins, so an edit to the first
	// "does nothing".
	seen := map[string]int{}
	depth := 0
	for i, line := range strings.Split(src, "\n") {
		code, _ := stripCfgComment(line)
		t := strings.TrimSpace(code)
		if depth == 0 {
			if m := reCfgAssign.FindStringSubmatch(t); m != nil {
				k := strings.ToLower(m[1])
				if first, dup := seen[k]; dup {
					out = append(out, Issue{File: path, Line: i + 1, Severity: SevWarning, Code: "cfg.duplicateKey",
						Message: fmt.Sprintf("%q is set twice (first on line %d) — the last value wins, so editing the first does nothing", m[1], first),
						Args:    A("key", m[1], "line", first)})
				} else {
					seen[k] = i + 1
				}
			}
		}
		depth += strings.Count(t, "{") - strings.Count(t, "}")
		if depth < 0 {
			depth = 0
		}
	}
	cfg, err := config.LoadServerCfg(path)
	if err == nil {
		if tpl := cfg.MissionTemplate(); tpl != "" {
			if st, err := os.Stat(filepath.Join(o.ServerDir, "mpmissions", tpl)); err != nil || !st.IsDir() {
				out = append(out, Issue{File: path, Severity: SevError, Code: "cfg.missionMissing",
					Message: fmt.Sprintf("template %q has no folder in mpmissions — the server cannot start this mission", tpl), Args: A("mission", tpl)})
			}
		}
		out = append(out, checkPorts(o, cfg, path)...)
	}
	return out
}

// checkPorts flags the port clashes that stop a server from being joinable or
// RCon from answering: DayZ uses the game port and the next three.
func checkPorts(o Options, cfg *config.ServerCfg, path string) []Issue {
	game := o.ServerPort
	if game <= 0 {
		return nil
	}
	inGame := func(p int) bool { return p >= game && p <= game+3 }
	var out []Issue
	query := 27016
	if v, ok := cfg.Get("steamQueryPort"); ok {
		if n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(v), `";`)); err == nil {
			query = n
		}
	}
	if inGame(query) {
		out = append(out, Issue{File: path, Severity: SevError, Code: "ports.clash",
			Message: fmt.Sprintf("steamQueryPort %d overlaps the game ports %d–%d — the server will not show up in the browser", query, game, game+3),
			Key:     "ports.query", Args: A("query", query, "from", game, "to", game+3)})
	}
	if o.RConPort > 0 && (inGame(o.RConPort) || o.RConPort == query) {
		out = append(out, Issue{File: path, Severity: SevError, Code: "ports.clash",
			Message: fmt.Sprintf("RCon port %d collides with a game/query port — RCon (and every panel feature using it) cannot bind", o.RConPort),
			Key:     "ports.rcon", Args: A("port", o.RConPort)})
	}
	return out
}

func fixServerCfgSemicolons(o Options) ([]string, error) {
	if o.ServerCfg == "" {
		return nil, nil
	}
	path := filepath.Join(o.ServerDir, o.ServerCfg)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(string(raw), "\n")
	bad := cfgMissingSemicolons(string(raw))
	if len(bad) == 0 {
		return nil, nil
	}
	for _, ln := range bad {
		line := lines[ln-1]
		cr := strings.HasSuffix(line, "\r")
		line = strings.TrimSuffix(line, "\r")
		code, comment := stripCfgComment(line)
		trimmed := strings.TrimRight(code, " \t")
		pad := code[len(trimmed):]
		line = trimmed + ";" + pad + comment
		if cr {
			line += "\r"
		}
		lines[ln-1] = line
	}
	if err := util.BackupBeforeWrite(path); err != nil {
		return nil, err
	}
	if err := writeAtomic(path, []byte(strings.Join(lines, "\n"))); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("added the missing ';' on %d line(s) of %s", len(bad), filepath.Base(path))}, nil
}

// ---------------------------------------------------------------------------
// init.c

func checkInitC(o Options) []Issue {
	md := o.missionDir()
	if md == "" {
		return nil
	}
	path := filepath.Join(md, "init.c")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	braces, parens := 0, 0
	inStr, inLine, inBlock := false, false, false
	s := string(data)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
			}
		case inBlock:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlock = false
				i++
			}
		case inStr:
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			inLine = true
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			inBlock = true
		case c == '{':
			braces++
		case c == '}':
			braces--
		case c == '(':
			parens++
		case c == ')':
			parens--
		}
	}
	var out []Issue
	if braces != 0 {
		out = append(out, Issue{File: path, Severity: SevError, Code: "initc.braces",
			Message: fmt.Sprintf("unbalanced { } (%+d) — the mission script will not compile and the server stops at start", braces),
			Args:    A("n", fmt.Sprintf("%+d", braces))})
	}
	if parens != 0 {
		out = append(out, Issue{File: path, Severity: SevError, Code: "initc.braces",
			Message: fmt.Sprintf("unbalanced ( ) (%+d) — the mission script will not compile and the server stops at start", parens),
			Key:     "initc.parens", Args: A("n", fmt.Sprintf("%+d", parens))})
	}
	return out
}

// ---------------------------------------------------------------------------
// Mods

func (o Options) activeMods() []string {
	return append(append([]string(nil), o.Mods...), o.ServerMods...)
}

func (o Options) modDir(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(o.ServerDir, name)
}

func checkMods(o Options) []Issue {
	var out []Issue
	cfgFile := "manager.json"
	seen := map[string]bool{}
	keysDir := filepath.Join(o.ServerDir, "keys")
	haveKeys := map[string]bool{}
	if entries, err := os.ReadDir(keysDir); err == nil {
		for _, e := range entries {
			haveKeys[strings.ToLower(e.Name())] = true
		}
	}
	linux := o.Linux || runtime.GOOS == "linux"
	var missingKeys []string
	for _, m := range o.activeMods() {
		k := strings.ToLower(m)
		if seen[k] {
			out = append(out, Issue{File: cfgFile, Severity: SevWarning, Code: "mods.duplicate", Fix: "mods.duplicate",
				Message: fmt.Sprintf("%s is in the launch list twice", m), Args: A("mod", m)})
			continue
		}
		seen[k] = true
		dir := o.modDir(m)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			out = append(out, Issue{File: cfgFile, Severity: SevError, Code: "mods.missing", Fix: "mods.missing",
				Message: fmt.Sprintf("%s is enabled but its folder is not in the server directory — the server will not start", m), Args: A("mod", m)})
			continue
		}
		for _, key := range modKeyNames(dir) {
			if !haveKeys[strings.ToLower(key)] {
				missingKeys = append(missingKeys, m)
				break
			}
		}
		if linux {
			if probs := mods.CaseProblems(dir); len(probs) > 0 {
				out = append(out, Issue{File: dir, Severity: SevError, Code: "mods.linuxcase", Fix: "mods.linuxcase",
					Message: fmt.Sprintf("%s has upper-case names the Linux server will not load: %s", m, strings.Join(firstN(probs, 4), ", ")),
					Args:    A("mod", m, "files", strings.Join(firstN(probs, 4), ", "))})
			}
		}
	}
	if len(missingKeys) > 0 {
		sort.Strings(missingKeys)
		out = append(out, Issue{File: keysDir, Severity: SevError, Code: "mods.keys", Fix: "mods.keys",
			Message: fmt.Sprintf("the signing key (.bikey) of %s is not in keys/ — players get kicked with a signature error", strings.Join(missingKeys, ", ")),
			Args:    A("mods", strings.Join(missingKeys, ", "))})
	}
	return out
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(append([]string(nil), s[:n]...), "…")
	}
	return s
}

func modKeyNames(dir string) []string {
	root := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		root = r
	}
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.EqualFold(filepath.Ext(p), ".bikey") {
			out = append(out, info.Name())
		}
		return nil
	})
	return out
}

func fixModKeys(o Options) ([]string, error) {
	var names []string
	for _, m := range o.activeMods() {
		if st, err := os.Stat(o.modDir(m)); err == nil && st.IsDir() && !filepath.IsAbs(m) {
			names = append(names, m)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	if err := mods.SyncKeys(o.ServerDir, names); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("copied the .bikey files of %d active mod(s) into keys/", len(names))}, nil
}

func fixLinuxCase(o Options) ([]string, error) {
	var done []string
	for _, m := range o.activeMods() {
		dir := o.modDir(m)
		if len(mods.CaseProblems(dir)) == 0 {
			continue
		}
		n, err := mods.NormalizeForLinux(dir)
		if err != nil {
			return done, fmt.Errorf("%s: %w", m, err)
		}
		done = append(done, fmt.Sprintf("lower-cased %d name(s) in %s for the Linux server", n, m))
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// cfgeconomycore.xml

var (
	reCEBlock = regexp.MustCompile(`(?s)(<ce\s+folder="([^"]+)"\s*>)(.*?)(</ce>)`)
	reCEFile  = regexp.MustCompile(`<file\b[^>]*/?>`)
)

// fixCEMissing comments out <file> entries whose file does not exist, so the
// economy stops failing on them (the line stays, so re-enabling is one edit).
func fixCEMissing(o Options) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	eco := filepath.Join(md, "cfgeconomycore.xml")
	data, err := os.ReadFile(eco)
	if err != nil {
		return nil, nil
	}
	var done []string
	masked := maskXMLComments(string(data))
	src := string(data)
	var b strings.Builder
	last := 0
	for _, blk := range reCEBlock.FindAllStringSubmatchIndex(masked, -1) {
		folder := masked[blk[4]:blk[5]]
		innerStart, innerEnd := blk[6], blk[7]
		b.WriteString(src[last:innerStart])
		inner := src[innerStart:innerEnd]
		innerMasked := masked[innerStart:innerEnd]
		pos := 0
		for _, fl := range reCEFile.FindAllStringIndex(innerMasked, -1) {
			tag := inner[fl[0]:fl[1]]
			name, _ := ceFileAttrs(tag)
			b.WriteString(inner[pos:fl[0]])
			if name != "" {
				if _, err := os.Stat(filepath.Join(md, folder, name)); err != nil {
					b.WriteString("<!-- missing file, disabled by DayZ Server Manager: " + tag + " -->")
					done = append(done, fmt.Sprintf("disabled the reference to missing %s/%s in cfgeconomycore.xml", folder, name))
					pos = fl[1]
					continue
				}
			}
			b.WriteString(tag)
			pos = fl[1]
		}
		b.WriteString(inner[pos:])
		last = innerEnd
	}
	b.WriteString(src[last:])
	if len(done) == 0 {
		return nil, nil
	}
	if err := util.BackupBeforeWrite(eco); err != nil {
		return nil, err
	}
	return done, writeAtomic(eco, []byte(b.String()))
}

func ceFileAttrs(tag string) (name, typ string) {
	for _, a := range regexp.MustCompile(`([A-Za-z_][\w.-]*)\s*=\s*"([^"]*)"`).FindAllStringSubmatch(tag, -1) {
		switch strings.ToLower(a[1]) {
		case "name":
			name = a[2]
		case "type":
			typ = a[2]
		}
	}
	return
}

// closestCEType maps a mistyped type= value to the one valid value within edit
// distance 2, or "" when there is no single clear match.
func closestCEType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	best, bestD, ties := "", 3, 0
	for t := range ceFileTypes {
		d := levenshtein(v, t)
		switch {
		case d < bestD:
			best, bestD, ties = t, d, 0
		case d == bestD:
			ties++
		}
	}
	if bestD <= 2 && ties == 0 {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func fixCETypes(o Options) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	eco := filepath.Join(md, "cfgeconomycore.xml")
	data, err := os.ReadFile(eco)
	if err != nil {
		return nil, nil
	}
	src := string(data)
	var done []string
	src = reCEFile.ReplaceAllStringFunc(src, func(tag string) string {
		_, typ := ceFileAttrs(tag)
		if typ == "" || ceFileTypes[strings.ToLower(typ)] {
			return tag
		}
		if fix := closestCEType(typ); fix != "" {
			done = append(done, fmt.Sprintf("corrected type=%q to %q in cfgeconomycore.xml", typ, fix))
			return strings.Replace(tag, `"`+typ+`"`, `"`+fix+`"`, 1)
		}
		return tag
	})
	if len(done) == 0 {
		return nil, nil
	}
	if err := util.BackupBeforeWrite(eco); err != nil {
		return nil, err
	}
	return done, writeAtomic(eco, []byte(src))
}

// ---------------------------------------------------------------------------
// cfgspawnabletypes.xml

func parseChance(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1, true
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func checkSpawnableChances(missionDir string) []Issue {
	path := filepath.Join(missionDir, "cfgspawnabletypes.xml")
	sts, err := dztypes.LoadSpawnable(path)
	if err != nil {
		return nil
	}
	var out []Issue
	// A slot's chance is a probability (0..1); an item's chance is a relative
	// weight among the slot's items (BI's own files use 40.00 / 30.00), so an
	// item only needs to be a non-negative number.
	for _, st := range sts {
		bad := 0
		for _, g := range append(append([]dztypes.SpawnGroup{}, st.Attachments...), st.Cargo...) {
			if f, ok := parseChance(g.Chance); !ok || f < 0 || f > 1 {
				bad++
			}
			for _, it := range g.Items {
				if f, ok := parseChance(it.Chance); !ok || f < 0 {
					bad++
				}
			}
		}
		if bad > 0 {
			out = append(out, Issue{File: path, Severity: SevError, Code: "spawnable.chance", Fix: "spawnable.chance",
				Message: fmt.Sprintf("%q has %d invalid chance value(s) — a slot chance must be 0.0–1.0 (0.25 = 25%%), an item weight must not be negative", st.Name, bad),
				Args:    A("name", st.Name, "n", bad)})
		}
	}
	return out
}

func clampChance(s string) (string, bool) {
	f, ok := parseChance(s)
	switch {
	case !ok:
		return "1.00", true
	case f < 0:
		return "0.00", true
	case f > 1 && f <= 100:
		return fmt.Sprintf("%.2f", f/100), true // "25" meant 25 %
	case f > 1:
		return "1.00", true
	}
	return s, false
}

func fixSpawnable(o Options, chances, presets bool) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	path := filepath.Join(md, "cfgspawnabletypes.xml")
	sts, err := dztypes.LoadSpawnable(path)
	if err != nil {
		return nil, nil
	}
	known := presetNames(filepath.Join(md, "cfgrandompresets.xml"))
	var done []string
	backedUp := false
	for i := range sts {
		st := &sts[i]
		changed, clamped := false, false
		fixGroups := func(gs []dztypes.SpawnGroup) []dztypes.SpawnGroup {
			out := gs[:0]
			for _, g := range gs {
				if presets && g.Preset != "" && len(known) > 0 && !known[strings.ToLower(g.Preset)] {
					changed = true
					done = append(done, fmt.Sprintf("removed %q's slot that referenced missing preset %q", st.Name, g.Preset))
					continue
				}
				if chances {
					if c, ch := clampChance(g.Chance); ch && g.Chance != "" {
						g.Chance, changed, clamped = c, true, true
					}
					for k := range g.Items {
						// Item chances are weights: only non-numbers and negatives are wrong.
						if f, ok := parseChance(g.Items[k].Chance); (!ok || f < 0) && g.Items[k].Chance != "" {
							g.Items[k].Chance, changed, clamped = "0.00", true, true
						}
					}
				}
				out = append(out, g)
			}
			return out
		}
		st.Attachments = fixGroups(st.Attachments)
		st.Cargo = fixGroups(st.Cargo)
		if !changed {
			continue
		}
		if clamped {
			done = append(done, fmt.Sprintf("corrected %q's invalid chances (slots 0.0–1.0, no negative weights)", st.Name))
		}
		if !backedUp {
			if err := util.BackupBeforeWrite(path); err != nil {
				return done, err
			}
			backedUp = true
		}
		if err := dztypes.SaveSpawnableType(path, st); err != nil {
			return done, err
		}
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// globals.xml

var reGlobalVar = regexp.MustCompile(`(?m)^[ \t]*<var\s+name="([^"]+)"[^>]*/>[ \t]*\r?\n?`)

func checkGlobals(missionDir string) []Issue {
	path := filepath.Join(missionDir, "db", "globals.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Issue
	for _, m := range reGlobalVar.FindAllStringSubmatch(maskXMLComments(string(data)), -1) {
		k := strings.ToLower(m[1])
		if seen[k] {
			out = append(out, Issue{File: path, Severity: SevWarning, Code: "globals.duplicate", Fix: "globals.duplicate",
				Message: fmt.Sprintf("variable %q is defined twice — only one value is used", m[1]), Args: A("name", m[1])})
		}
		seen[k] = true
	}
	return out
}

func fixGlobals(o Options) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	path := filepath.Join(md, "db", "globals.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	src := string(data)
	masked := maskXMLComments(src)
	seen := map[string]bool{}
	var b strings.Builder
	last, removed := 0, 0
	for _, m := range reGlobalVar.FindAllStringSubmatchIndex(masked, -1) {
		k := strings.ToLower(masked[m[2]:m[3]])
		if seen[k] {
			b.WriteString(src[last:m[0]])
			last = m[1]
			removed++
			continue
		}
		seen[k] = true
	}
	if removed == 0 {
		return nil, nil
	}
	b.WriteString(src[last:])
	if err := util.BackupBeforeWrite(path); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("removed %d duplicate variable(s) from globals.xml (kept the first)", removed)}, writeAtomic(path, []byte(b.String()))
}

// ---------------------------------------------------------------------------
// types.xml numeric fixes

func fixTypeNumbers(o Options, only map[string]bool) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	files := []string{filepath.Join(md, "db", "types.xml")}
	if entries, err := os.ReadDir(dztypes.ModedDir(o.ServerDir, o.Mission)); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				files = append(files, filepath.Join(dztypes.ModedDir(o.ServerDir, o.Mission), e.Name()))
			}
		}
	}
	want := func(code string) bool { return len(only) == 0 || only[code] }
	var done []string
	for _, p := range files {
		doc, err := dztypes.Load(p)
		if err != nil {
			continue
		}
		counts := map[string]int{}
		for i := range doc.Types {
			t := &doc.Types[i]
			changed := false
			if want("types.minNominal") && t.Nominal != nil && *t.Nominal > 0 && t.Min != nil && *t.Min > *t.Nominal {
				v := *t.Nominal
				t.Min = &v
				changed = true
				counts["min lowered to nominal"]++
			}
			if want("types.restockNeg") && t.Restock != nil && *t.Restock < 0 {
				v := 0
				t.Restock = &v
				changed = true
				counts["negative restock set to 0"]++
			}
			if want("types.quantRange") {
				for _, q := range []**int{&t.QuantMin, &t.QuantMax} {
					if *q != nil && **q != -1 && (**q < 0 || **q > 100) {
						v := **q
						if v < 0 {
							v = 0
						} else {
							v = 100
						}
						*q = &v
						changed = true
						counts["quantity clamped to 0–100"]++
					}
				}
			}
			if want("types.quantOrder") && t.QuantMin != nil && t.QuantMax != nil && *t.QuantMin != -1 && *t.QuantMax != -1 && *t.QuantMin > *t.QuantMax {
				t.QuantMin, t.QuantMax = t.QuantMax, t.QuantMin
				changed = true
				counts["quantmin/quantmax swapped"]++
			}
			if changed {
				doc.MarkDirty(t.Name)
			}
		}
		if doc.Dirty() == 0 {
			continue
		}
		if err := util.BackupBeforeWrite(p); err != nil {
			return done, err
		}
		if err := dztypes.SaveTypes(p, doc); err != nil {
			return done, err
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			done = append(done, fmt.Sprintf("%s: %d × %s", filepath.Base(p), counts[k], k))
		}
	}
	return done, nil
}

// ---------------------------------------------------------------------------
// cfgeventspawns.xml bounds (by the real world size)

func fixSpawnBounds(o Options) ([]string, error) {
	md := o.missionDir()
	if md == "" {
		return nil, nil
	}
	path := filepath.Join(md, "cfgeventspawns.xml")
	doc, err := dztypes.LoadEventSpawns(path)
	if err != nil {
		return nil, nil
	}
	limit := boundsLimit(o.WorldSize)
	removed := 0
	for i := range doc.Events {
		kept := doc.Events[i].Pos[:0]
		for _, p := range doc.Events[i].Pos {
			if p.X < 0 || p.Z < 0 || p.X > limit || p.Z > limit {
				removed++
				continue
			}
			kept = append(kept, p)
		}
		doc.Events[i].Pos = kept
	}
	if removed == 0 {
		return nil, nil
	}
	if err := util.BackupBeforeWrite(path); err != nil {
		return nil, err
	}
	if err := dztypes.SaveEventSpawns(path, doc); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("removed %d spawn position(s) outside the map from cfgeventspawns.xml", removed)}, nil
}

// boundsLimit is the largest valid coordinate: the world edge (plus a small
// margin for points placed right on the border) or 20000 when unknown.
func boundsLimit(size float64) float64 {
	if size > 0 {
		return size + 50
	}
	return 20000
}

// ---------------------------------------------------------------------------
// helpers

var reXMLCommentAll = regexp.MustCompile(`(?s)<!--.*?-->`)

// maskXMLComments blanks comment bodies (same length) so regex scans never
// match inside a comment while byte offsets stay valid for the source.
func maskXMLComments(s string) string {
	return reXMLCommentAll.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
