// Copyright (c) 2026 Aristarh Ucolov.
//
// Start-failure diagnosis.
//
// "My server won't start" is the single most common support question, and the
// answer is almost always sitting in the RPT — a file most admins have never
// opened, several megabytes long, where the one line that matters is buried
// among thousands of warnings.
//
// This reads the tail of the RPT and the manager's stdout capture and matches a
// small set of patterns that actually stop a server. Every rule is a heuristic
// over log text, so a finding is a strong hint, not a verdict: the raw line is
// always included so the admin can judge it, and "nothing matched" never means
// "nothing is wrong".
package logs

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Severity of a finding. Fatal means the server cannot run with this.
const (
	SevFatal = "fatal"
	SevWarn  = "warning"
)

// Finding is one recognised problem.
type Finding struct {
	Severity string `json:"severity"`
	// Code is a stable identifier the UI translates; Title/Detail are the
	// English fallback for anything the UI does not have a key for.
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Subject is the thing the rule identified — a mod folder, a .pbo, a port.
	// The UI substitutes it into a TRANSLATED detail, so the explanation can be
	// in the admin's language while still naming the offending file.
	Subject string `json:"subject,omitempty"`
	// Line is the raw log line that triggered the match, so the admin can see
	// the evidence rather than trusting the classification.
	Line   string `json:"line"`
	Source string `json:"source"` // "rpt" | "stdout" | "script"
}

// rule matches one class of failure.
type rule struct {
	code  string
	sev   string
	re    *regexp.Regexp
	title string
	// detail may use $1 etc. from the regex to name the offending file/mod.
	detail string
}

// rules are ordered: the first match for a given code wins, and fatal rules
// are listed before advisory ones. Patterns are deliberately narrow — a rule
// that fires on a healthy server is worse than no rule at all.
var rules = []rule{
	{
		code: "mod_missing", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)cannot open file ['"]?([^'"\n]*@[^'"\n\\/]+[^'"\n]*)['"]?`),
		title: "A mod folder listed in -mod is not on the server",
		detail: "DayZ could not open $1. The mod is in the launch parameters but its folder is missing or " +
			"incomplete — install or re-sync it on the Mods page, then start again.",
	},
	{
		code: "mod_missing_generic", sev: SevFatal,
		re:     regexp.MustCompile(`(?i)cannot open file ['"]?([^'"\n]+)['"]?`),
		title:  "A file the server needs is missing",
		detail: "DayZ could not open $1. Check that the path exists and that no antivirus has quarantined it.",
	},
	{
		code: "bad_signature", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)(bad version|wrong signature|signature .* not found|bad signature) .*?([\w.@\-]+\.(?:pbo|bikey))`),
		title: "A mod file is unsigned or its key does not match",
		detail: "$2 failed BattlEye's signature check. Re-sync the mod's .bikey on the Mods page — do NOT lower " +
			"verifySignatures, which would also let modified clients in.",
	},
	{
		code: "script_error", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)^\s*(Compiling|Can't compile) .*(script|mission)`),
		title: "A script failed to compile",
		detail: "The server stopped while compiling scripts. The lines just after this one in the RPT name the " +
			"file and line number — usually a mod that does not match the current DayZ version.",
	},
	{
		code: "mission_missing", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)(mission .*(not found|missing)|cannot find mission)`),
		title: "The mission in server.cfg does not exist",
		detail: "The template named in serverDZ.cfg has no matching folder under mpmissions/. Pick an existing " +
			"mission on the Server page.",
	},
	{
		code: "port_busy", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)(address already in use|bind failed|failed to bind|port .*already)`),
		title: "The server port is already taken",
		detail: "Another process — very often a previous DayZ server that has not fully exited — holds the port. " +
			"Wait a few seconds, or change the port on the Server page.",
	},
	{
		code: "config_parse", sev: SevFatal,
		re:    regexp.MustCompile(`(?i)(config\s*:\s*some input after end of data|error.*parsing.*\.cfg|unexpected end of file)`),
		title: "serverDZ.cfg is malformed",
		detail: "The config parser stopped early. A missing semicolon or an unbalanced brace is the usual cause — " +
			"the Validator checks brace balance.",
	},
	{
		code: "no_economy", sev: SevWarn,
		re:    regexp.MustCompile(`(?i)(cannot load .*cfgeconomycore|economy.*failed to load|\[CE\]\[Error\])`),
		title: "The central economy failed to load",
		detail: "The server may run but spawn no loot. Run the Validator: a missing file referenced from " +
			"cfgeconomycore.xml, or an unknown usage/value name, is the usual cause.",
	},
	{
		code: "missing_key", sev: SevWarn,
		re:     regexp.MustCompile(`(?i)(key .*not found|no key for|unknown key) .*?([\w.@\-]+\.bikey)?`),
		title:  "A mod key is missing from keys/",
		detail: "Clients using that mod will be kicked for a bad signature. Use Sync keys on the Mods page.",
	},
}

// tailLines returns up to n lines from the end of a file, without loading the
// whole thing — an RPT can be tens of megabytes.
func tailLines(path string, n int, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if st.Size() > maxBytes {
		start = st.Size() - maxBytes
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	ring := make([]string, 0, n)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring, nil
}

// Diagnose scans the newest RPT and the manager's stdout capture for known
// start failures. Findings are de-duplicated by code, newest match kept, and
// ordered fatal-first.
func Diagnose(serverDir, profilesDir string) []Finding {
	var out []Finding
	seen := map[string]bool{}

	scan := func(sourceID, path string) {
		if path == "" {
			return
		}
		lines, err := tailLines(path, 4000, 2<<20)
		if err != nil {
			return
		}
		// Walk backwards: the most recent occurrence is the relevant one.
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			for _, r := range rules {
				if seen[r.code] {
					continue
				}
				m := r.re.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				// mod_missing is the specific case of mod_missing_generic; if
				// the specific one already matched, skip the generic report.
				if r.code == "mod_missing_generic" && seen["mod_missing"] {
					continue
				}
				seen[r.code] = true
				// The last non-empty capture is the thing worth naming.
				subject := ""
				for k := len(m) - 1; k >= 1; k-- {
					if v := strings.TrimSpace(m[k]); v != "" {
						subject = v
						break
					}
				}
				out = append(out, Finding{
					Severity: r.sev,
					Code:     r.code,
					Title:    r.title,
					Detail:   expand(r.detail, m),
					Subject:  subject,
					Line:     truncate(line, 300),
					Source:   sourceID,
				})
			}
		}
	}

	for _, s := range Discover(serverDir, profilesDir) {
		switch s.ID {
		case "rpt", "stdout", "script":
			if s.Exists {
				scan(s.ID, s.Path)
			}
		}
	}

	// Fatal first, then the order the rules are declared in.
	fatal := out[:0:0]
	warn := out[:0:0]
	for _, f := range out {
		if f.Severity == SevFatal {
			fatal = append(fatal, f)
		} else {
			warn = append(warn, f)
		}
	}
	return append(fatal, warn...)
}

// expand substitutes $1..$9 from a regex match into a detail string.
func expand(detail string, m []string) string {
	for i := len(m) - 1; i >= 1; i-- {
		v := strings.TrimSpace(m[i])
		if v == "" {
			v = "the file"
		}
		detail = strings.ReplaceAll(detail, fmt.Sprintf("$%d", i), v)
	}
	return detail
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
