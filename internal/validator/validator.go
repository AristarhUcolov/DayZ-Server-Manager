// Copyright (c) 2026 Aristarh Ucolov.
//
// Validator for the DayZ server file set.
//
//   - XML: .xml files are parsed with encoding/xml. Errors carry line numbers.
//   - CFG: server.cfg — we do a best-effort brace/semicolon check.
//   - Cross-file: cfgeconomycore.xml referenced files must exist on disk; types
//     inside moded_types/*.xml must not duplicate a name already in types.xml
//     (duplicates cause "type already defined" issues in-game).
package validator

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	dztypes "dayzmanager/internal/types"
	"dayzmanager/internal/util"
)

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

type Issue struct {
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	// Code names the kind of problem (stable, e.g. "cfg.semicolon") so the UI
	// can group, translate and explain it; Fix is set when AutoFixWith can
	// repair this kind — pass it back in `only` to fix just that kind.
	Code string `json:"code,omitempty"`
	Fix  string `json:"fix,omitempty"`
	// Key and Args let the UI show the message in the panel's language: the
	// text is i18n key "vmsg.<Key>" (Key defaults to Code) with Args filled
	// into its {placeholders}. Message stays the English text for logs and for
	// a message without a translation.
	Key  string            `json:"key,omitempty"`
	Args map[string]string `json:"args,omitempty"`
	// Rel is File relative to the server directory (forward slashes), set by
	// the web layer so the UI can open the file in its editor at Line.
	Rel string `json:"rel,omitempty"`
}

// A is shorthand for an Issue's Args: alternating names and values.
func A(kv ...interface{}) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = fmt.Sprint(kv[i+1])
	}
	return m
}

// ValidateAll runs every check against the server directory and returns a flat
// issue list. The caller is responsible for deciding what to surface in the UI.
func ValidateAll(serverDir, missionTemplate string) ([]Issue, error) {
	return ValidateWith(Options{ServerDir: serverDir, Mission: missionTemplate})
}

// ValidateWith is ValidateAll with the server context the deeper checks need
// (launch mods, ports, server.cfg name, profiles folder, world size).
func ValidateWith(o Options) ([]Issue, error) {
	serverDir, missionTemplate := o.ServerDir, o.Mission
	var issues []Issue

	// XML syntax: the active mission only (the others are not loaded, so their
	// errors were noise), every mission when none is configured. Results are
	// cached per file by size+mtime, so re-validating is near-instant.
	missionDir := filepath.Join(serverDir, "mpmissions")
	if missionTemplate != "" {
		missionDir = dztypes.MissionDir(serverDir, missionTemplate)
	}
	err := filepath.Walk(missionDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == ".xml" && !strings.Contains(info.Name(), ".bak") {
			if is := validateXMLCached(path, info); is != nil {
				issues = append(issues, *is)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// server.cfg files at server root.
	entries, _ := os.ReadDir(serverDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".cfg" {
			if is := validateCFG(filepath.Join(serverDir, e.Name())); is != nil {
				issues = append(issues, *is)
			}
		}
	}

	// Cross-file: cfgeconomycore.xml referenced files.
	if missionTemplate != "" {
		missionDir := dztypes.MissionDir(serverDir, missionTemplate)
		eco := filepath.Join(missionDir, "cfgeconomycore.xml")
		if refs, err := dztypes.ListEconomyCE(eco); err == nil {
			for _, r := range refs {
				p := filepath.Join(missionDir, r.Folder, r.Name)
				if _, err := os.Stat(p); err != nil {
					issues = append(issues, Issue{
						File:     eco,
						Severity: SevError,
						Code:     "ce.missing",
						Fix:      "ce.missing",
						Message:  fmt.Sprintf("referenced file missing: %s/%s — the economy fails to load it", r.Folder, r.Name),
						Args:     A("file", r.Folder+"/"+r.Name),
					})
				}
				// A typo in type= makes DayZ load the file as the wrong kind
				// or ignore it, with no message anywhere.
				if r.Type != "" && !ceFileTypes[strings.ToLower(r.Type)] {
					is := Issue{
						File:     eco,
						Severity: SevError,
						Code:     "ce.type",
						Message:  fmt.Sprintf("%s/%s declares type=%q, which DayZ does not recognise", r.Folder, r.Name, r.Type),
						Args:     A("file", r.Folder+"/"+r.Name, "type", r.Type),
					}
					if fix := closestCEType(r.Type); fix != "" {
						is.Fix = "ce.type"
						is.Message += fmt.Sprintf(" (did you mean %q?)", fix)
						is.Key, is.Args["fix"] = "ce.typeSuggest", fix
					}
					issues = append(issues, is)
				}
			}
		}

		// cfglimitsdefinition.xml — the whitelist of category/usage/value/tag
		// names. Types referencing anything outside this list are silently
		// dropped by DayZ at runtime, which makes for confusing loot bugs.
		//
		// These two files sit at the MISSION ROOT. They were read from db/ up
		// to v0.15.0, so loadLimits always failed, every flag check returned
		// nothing, and the page reported "no issues" on a broken server.
		limitsPath := filepath.Join(missionDir, "cfglimitsdefinition.xml")
		limits, limitsErr := loadLimits(limitsPath)
		mergeUserLimits(limits, filepath.Join(missionDir, "cfglimitsdefinitionuser.xml"))
		if !limits.loaded {
			// A check that silently does nothing is worse than no check: say so.
			issues = append(issues, Issue{
				File:     limitsPath,
				Severity: SevWarning,
				Code:     "limits.unreadable",
				Message:  fmt.Sprintf("cannot read cfglimitsdefinition.xml (%v) — category/usage/value/tag checks were skipped", limitsErr),
				Args:     A("err", limitsErr),
			})
		}

		// unknownRefs aggregates unknown names so one misspelling shared by 400
		// modded types produces one line instead of 400 identical ones.
		unknown := newUnknownAgg()

		// Duplicate type names across types.xml + moded_types/*.xml.
		seen := map[string]string{}
		typesPath := filepath.Join(missionDir, "db", "types.xml")
		if doc, err := dztypes.Load(typesPath); err != nil {
			if !os.IsNotExist(err) {
				issues = append(issues, Issue{File: typesPath, Severity: SevError, Code: "types.parse",
					Message: fmt.Sprintf("cannot parse as a types file: %v", err), Args: A("err", err)})
			}
		} else {
			for i := range doc.Types {
				t := &doc.Types[i]
				key := strings.ToLower(t.Name)
				// Duplicates inside one file were never checked: DayZ keeps
				// only one of them, and which one is not defined.
				if _, dup := seen[key]; dup {
					issues = append(issues, Issue{File: typesPath, Severity: SevError, Code: "types.duplicate", Fix: "types.duplicate",
						Message: fmt.Sprintf("duplicate type %q in the same file — DayZ keeps only one of them", t.Name), Args: A("name", t.Name)})
				}
				seen[key] = typesPath
				unknown.collect(typesPath, t, limits)
				issues = append(issues, checkTypeSanity(typesPath, t)...)
			}
		}
		moded := dztypes.ModedDir(serverDir, missionTemplate)
		modedEntries, _ := os.ReadDir(moded)
		registered, _ := dztypes.RegisteredInModed(eco)
		for _, e := range modedEntries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			p := filepath.Join(moded, e.Name())
			// A moded types file DayZ never loads is the single most common
			// cause of "my custom loot does not spawn".
			if len(registered) > 0 && !registered[strings.ToLower(e.Name())] {
				issues = append(issues, Issue{File: p, Severity: SevError, Code: "moded.unregistered", Fix: "moded.unregistered",
					Message: fmt.Sprintf("%s is not registered in cfgeconomycore.xml — DayZ will not load it", e.Name()), Args: A("file", e.Name())})
			}
			doc, err := dztypes.Load(p)
			if err != nil {
				// Swallowing this meant a moded file with the wrong root
				// element produced no output at all.
				issues = append(issues, Issue{File: p, Severity: SevError, Code: "types.parse",
					Message: fmt.Sprintf("cannot parse as a types file: %v", err), Args: A("err", err)})
				continue
			}
			for i := range doc.Types {
				t := &doc.Types[i]
				key := strings.ToLower(t.Name)
				if prev, dup := seen[key]; dup {
					is := Issue{
						File:     p,
						Severity: SevWarning,
						Code:     "types.crossdup",
						Message:  fmt.Sprintf("duplicate type %q (also in %s)", t.Name, filepath.Base(prev)),
						Args:     A("name", t.Name, "other", filepath.Base(prev)),
					}
					// Only a moded copy of a BASE name is auto-removable; two
					// moded files clashing needs a human decision.
					if strings.EqualFold(filepath.Base(prev), "types.xml") {
						is.Fix = "types.crossdup"
					}
					issues = append(issues, is)
				} else {
					seen[key] = p
				}
				unknown.collect(p, t, limits)
				issues = append(issues, checkTypeSanity(p, t)...)
			}
		}
		issues = append(issues, unknown.issues()...)

		// events.xml + the spawn positions that reference it.
		issues = append(issues, checkEvents(missionDir)...)
		// Spawn coordinates that fall off the map (typos).
		issues = append(issues, checkEventSpawnBounds(filepath.Join(missionDir, "cfgeventspawns.xml"), o.WorldSize)...)
		// cfgspawnabletypes.xml preset references and chance ranges.
		issues = append(issues, checkSpawnable(missionDir)...)
		issues = append(issues, checkSpawnableChances(missionDir)...)
		issues = append(issues, checkGlobals(missionDir)...)
		issues = append(issues, checkInitC(o)...)
	}

	// v1.0.0: human-factor checks across the whole server.
	issues = append(issues, checkJSON(o)...)
	issues = append(issues, checkEncoding(o)...)
	issues = append(issues, checkServerCfg(o)...)
	issues = append(issues, checkMods(o)...)

	return issues, nil
}

// reXMLComment matches an entire <!-- ... --> block, including newlines.
// BI's vanilla XML uses `-------` decorations inside comments which is illegal
// per XML spec. DayZ's own loader ignores those, so we strip comments out
// before handing bytes to Go's strict parser — otherwise every default mission
// reports false errors on perfectly shipping files.
var reXMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

type xmlCacheEntry struct {
	size  int64
	mtime int64
	issue *Issue
}

var (
	xmlCacheMu sync.Mutex
	xmlCache   = map[string]xmlCacheEntry{}
)

// validateXMLCached skips re-parsing a file that has not changed since the
// last run (mapgroup files are tens of MB; parsing them every time was most of
// the validator's run time).
func validateXMLCached(path string, info os.FileInfo) *Issue {
	size, mtime := info.Size(), info.ModTime().UnixNano()
	xmlCacheMu.Lock()
	e, ok := xmlCache[path]
	xmlCacheMu.Unlock()
	if ok && e.size == size && e.mtime == mtime {
		if e.issue == nil {
			return nil
		}
		cp := *e.issue
		return &cp
	}
	is := validateXML(path)
	xmlCacheMu.Lock()
	xmlCache[path] = xmlCacheEntry{size: size, mtime: mtime, issue: is}
	xmlCacheMu.Unlock()
	return is
}

func validateXML(path string) *Issue {
	raw, err := os.ReadFile(path)
	if err != nil {
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
	// Replace each comment with the same number of newlines so line numbers
	// from the decoder still point at meaningful positions in the file.
	cleaned := reXMLComment.ReplaceAllFunc(raw, func(m []byte) []byte {
		return bytes.Repeat([]byte{'\n'}, bytes.Count(m, []byte{'\n'}))
	})
	dec := xml.NewDecoder(bytes.NewReader(cleaned))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == nil {
			continue
		}
		if err.Error() == "EOF" {
			return nil
		}
		if se, ok := err.(*xml.SyntaxError); ok {
			is := &Issue{File: path, Line: se.Line, Severity: SevError, Code: "xml.syntax", Message: se.Msg, Args: A("err", se.Msg)}
			// A bare "&" is the one syntax error with a safe mechanical fix.
			if strings.Contains(se.Msg, "character entity") {
				is.Fix = "xml.amp"
			}
			return is
		}
		return &Issue{File: path, Severity: SevError, Code: "xml.syntax", Message: err.Error(), Args: A("err", err)}
	}
}

// limits mirrors the small subset of cfglimitsdefinition.xml we care about:
// the set of category/usage/value/tag names that DayZ will accept. Anything
// referenced in types.xml that is not in these sets will be dropped silently
// at runtime.
type limits struct {
	categories map[string]bool
	usages     map[string]bool
	values     map[string]bool
	tags       map[string]bool
	loaded     bool
}

// newLimits returns an empty, unloaded limits set with every map ready to use.
func newLimits() *limits {
	return &limits{
		categories: map[string]bool{},
		usages:     map[string]bool{},
		values:     map[string]bool{},
		tags:       map[string]bool{},
	}
}

func loadLimits(path string) (*limits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return newLimits(), err
	}
	type named struct {
		Name string `xml:"name,attr"`
	}
	type doc struct {
		XMLName    xml.Name `xml:"lists"`
		Categories struct {
			Category []named `xml:"category"`
		} `xml:"categories"`
		Tags struct {
			Tag []named `xml:"tag"`
		} `xml:"tags"`
		Usage struct {
			Usage []named `xml:"usage"`
		} `xml:"usageflags"`
		Value struct {
			Value []named `xml:"value"`
		} `xml:"valueflags"`
	}
	var d doc
	if err := xml.Unmarshal(data, &d); err != nil {
		return newLimits(), err
	}
	l := newLimits()
	l.loaded = true
	for _, n := range d.Categories.Category {
		l.categories[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Usage.Usage {
		l.usages[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Value.Value {
		l.values[strings.ToLower(n.Name)] = true
	}
	for _, n := range d.Tags.Tag {
		l.tags[strings.ToLower(n.Name)] = true
	}
	return l, nil
}

// mergeUserLimits folds cfglimitsdefinitionuser.xml `<user name="X">` groups
// into the limits sets. Those user names are legal usage/value references in
// types.xml, so counting them avoids false "unknown usage/value" warnings.
func mergeUserLimits(l *limits, userPath string) {
	if l == nil || l.usages == nil {
		return
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		return
	}
	type named struct {
		Name string `xml:"name,attr"`
	}
	// The real file nests <user> inside <usageflags>/<valueflags>:
	//
	//   <user_lists><usageflags><user name="TownVillage">…</user></usageflags>
	//
	// The old `xml:"user"` at root level matched nothing, so every types entry
	// using a group name like Tier1234 or TownVillage was about to be reported
	// as unknown — and Auto-fix would have written a colliding duplicate name
	// into cfglimitsdefinition.xml.
	type group struct {
		User []named `xml:"user"`
	}
	type doc struct {
		Usage group   `xml:"usageflags"`
		Value group   `xml:"valueflags"`
		Root  []named `xml:"user"` // hand-written flat variant, still accepted
	}
	var d doc
	if xml.Unmarshal(data, &d) != nil {
		return
	}
	add := func(set map[string]bool, us []named) {
		for _, u := range us {
			if u.Name != "" {
				set[strings.ToLower(u.Name)] = true
			}
		}
	}
	add(l.usages, d.Usage.User)
	add(l.values, d.Value.User)
	for _, u := range d.Root {
		if u.Name != "" {
			l.usages[strings.ToLower(u.Name)] = true
			l.values[strings.ToLower(u.Name)] = true
		}
	}
}

// AutoFix applies the safe, reversible validator fixes and returns a
// human-readable list of what it did. Both fixes are purely additive — they
// never change existing values, only register things DayZ is silently ignoring:
//
//  1. Whitelist every unknown usage/value/tag/category referenced by types into
//     cfglimitsdefinition.xml (the canonical way to stop DayZ dropping modded
//     loot).
//  2. Register any moded_types/*.xml file that is missing from cfgeconomycore.xml
//     — the single most common cause of "my custom loot does not spawn".
//
// A .bak is written before each file is touched. Server must be stopped (caller
// enforces). A read failure on one file never blocks the other fix.
func AutoFix(serverDir, missionTemplate string) ([]string, error) {
	return AutoFixWith(Options{ServerDir: serverDir, Mission: missionTemplate}, nil)
}

// AutoFixWith runs the fixes for the given issue codes (all of them when only
// is empty). Mod-list fixes (mods.missing / mods.duplicate) change the manager
// config and are applied by the caller, which owns it.
func AutoFixWith(o Options, only []string) ([]string, error) {
	serverDir, missionTemplate := o.ServerDir, o.Mission
	if missionTemplate == "" {
		return nil, errors.New("no mission configured")
	}
	want := map[string]bool{}
	for _, c := range only {
		want[c] = true
	}
	do := func(code string) bool { return len(want) == 0 || want[code] }
	missionDir := dztypes.MissionDir(serverDir, missionTemplate)
	moded := dztypes.ModedDir(serverDir, missionTemplate)
	var out []string

	// ---- 1. whitelist unknown category/usage/value/tag ----
	limitsPath := filepath.Join(missionDir, "cfglimitsdefinition.xml")
	limits, limitsErr := loadLimits(limitsPath)
	if limitsErr == nil && limits.loaded && do("types.unknownFlag") {
		mergeUserLimits(limits, filepath.Join(missionDir, "cfglimitsdefinitionuser.xml"))
		miss := map[string]map[string]string{ // kind -> lower -> original
			"category": {}, "usage": {}, "value": {}, "tag": {},
		}
		consider := func(set map[string]bool, kind, name string) {
			if name != "" && !set[strings.ToLower(name)] {
				miss[kind][strings.ToLower(name)] = name
			}
		}
		scan := func(doc *dztypes.TypesDoc) {
			for i := range doc.Types {
				t := &doc.Types[i]
				if t.Category != nil {
					consider(limits.categories, "category", t.Category.Name)
				}
				for _, r := range t.Usages {
					consider(limits.usages, "usage", r.Name)
				}
				for _, r := range t.Values {
					consider(limits.values, "value", r.Name)
				}
				for _, r := range t.Tags {
					consider(limits.tags, "tag", r.Name)
				}
			}
		}
		if doc, err := dztypes.Load(filepath.Join(missionDir, "db", "types.xml")); err == nil {
			scan(doc)
		}
		if entries, err := os.ReadDir(moded); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
					continue
				}
				if doc, err := dztypes.Load(filepath.Join(moded, e.Name())); err == nil {
					scan(doc)
				}
			}
		}
		total := 0
		for _, m := range miss {
			total += len(m)
		}
		if total > 0 {
			written, err := whitelistLimits(limitsPath, miss)
			if err != nil {
				return nil, err
			}
			for _, kind := range []string{"category", "usage", "value", "tag"} {
				for _, name := range written[kind] {
					out = append(out, fmt.Sprintf("added %s %q to cfglimitsdefinition.xml", kind, name))
				}
			}
		}
	}

	// ---- 2. register unregistered moded_types files ----
	eco := filepath.Join(missionDir, "cfgeconomycore.xml")
	// Only touch cfgeconomycore.xml when a moded_types block already exists (the
	// same guard the validator uses): if the admin registers custom loot some
	// other way, adding a block would be presumptuous.
	if registered, err := dztypes.RegisteredInModed(eco); err == nil && len(registered) > 0 && do("moded.unregistered") {
		if entries, err := os.ReadDir(moded); err == nil {
			backedUp := false
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
					continue
				}
				if registered[strings.ToLower(e.Name())] {
					continue
				}
				// Only register files that really parse as types files — never
				// pull an unrelated stray XML into the central economy.
				if _, err := dztypes.Load(filepath.Join(moded, e.Name())); err != nil {
					continue
				}
				if !backedUp {
					if err := util.BackupBeforeWrite(eco); err != nil {
						return nil, err
					}
					backedUp = true
				}
				if err := dztypes.RegisterModedFile(eco, e.Name(), "types"); err != nil {
					return nil, fmt.Errorf("register %s: %w", e.Name(), err)
				}
				out = append(out, fmt.Sprintf("registered %s in cfgeconomycore.xml", e.Name()))
			}
		}
	}

	// ---- 3. remove duplicate <type> entries (keep the first) ----
	// A repeated name in one file makes DayZ pick one at random; collapsing to
	// the first makes the loot deterministic. Only files that actually have a
	// duplicate are backed up and rewritten; comments survive untouched.
	dedupOne := func(p string) error {
		doc, err := dztypes.Load(p)
		if err != nil {
			return nil // parse failures are reported by ValidateAll, not fixed here
		}
		seen := map[string]bool{}
		hasDup := false
		for i := range doc.Types {
			k := strings.ToLower(doc.Types[i].Name)
			if seen[k] {
				hasDup = true
				break
			}
			seen[k] = true
		}
		if !hasDup {
			return nil
		}
		if err := util.BackupBeforeWrite(p); err != nil {
			return err
		}
		removed, err := dztypes.DedupTypesFile(p)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, n := range removed {
			counts[n]++
		}
		names := make([]string, 0, len(counts))
		for n := range counts {
			names = append(names, n)
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf("removed %d duplicate <type> %s in %s (%s)",
			len(removed), plural(len(removed), "entry", "entries"), filepath.Base(p), strings.Join(names, ", ")))
		return nil
	}
	if do("types.duplicate") {
		if err := dedupOne(filepath.Join(missionDir, "db", "types.xml")); err != nil {
			return nil, err
		}
	}
	if entries, err := os.ReadDir(moded); err == nil && do("types.duplicate") {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
				continue
			}
			if err := dedupOne(filepath.Join(moded, e.Name())); err != nil {
				return nil, err
			}
		}
	}

	// ---- 4. strip moded_types entries that duplicate a base types.xml name ----
	// A moded file redefining a vanilla type name is a cross-file duplicate:
	// DayZ's resolution is undefined, so the base types.xml effectively wins and
	// the moded copy is dead weight. Remove only the moded copy (types.xml is
	// never touched) so the loot is deterministic; comments are preserved.
	baseNames := map[string]bool{}
	if doc, err := dztypes.Load(filepath.Join(missionDir, "db", "types.xml")); err == nil {
		for i := range doc.Types {
			baseNames[strings.ToLower(doc.Types[i].Name)] = true
		}
	}
	if len(baseNames) > 0 && do("types.crossdup") {
		if entries, err := os.ReadDir(moded); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xml") {
					continue
				}
				p := filepath.Join(moded, e.Name())
				doc, err := dztypes.Load(p)
				if err != nil {
					continue
				}
				dup := map[string]bool{}
				for i := range doc.Types {
					if k := strings.ToLower(doc.Types[i].Name); baseNames[k] {
						dup[k] = true
					}
				}
				if len(dup) == 0 {
					continue
				}
				if err := util.BackupBeforeWrite(p); err != nil {
					return nil, err
				}
				removed, err := dztypes.RemoveTypesInSet(p, dup)
				if err != nil {
					return nil, err
				}
				if len(removed) > 0 {
					sort.Strings(removed)
					out = append(out, fmt.Sprintf("removed %d type %s from %s that duplicate types.xml (%s)",
						len(removed), plural(len(removed), "entry", "entries"), filepath.Base(p), strings.Join(removed, ", ")))
				}
			}
		}
	}

	// ---- 5. drop orphan spawn positions (event not in events.xml) ----
	// Positions for an event that events.xml no longer defines are ignored by
	// DayZ — the classic leftover from renaming an event. Remove the dead blocks
	// so cfgeventspawns.xml matches reality.
	// Every registered events file counts: a mod's events (Expansion's, a car
	// mod's) live outside db/events.xml, and judging only by it used to delete
	// their spawn points.
	if known, ok := missionEventNames(missionDir); ok && do("eventspawns.orphan") {
		spawnsPath := filepath.Join(missionDir, "cfgeventspawns.xml")
		if spDoc, err := dztypes.LoadEventSpawns(spawnsPath); err == nil {
			kept := spDoc.Events[:0]
			var orphans []string
			for _, ev := range spDoc.Events {
				if known[strings.ToLower(ev.Name)] {
					kept = append(kept, ev)
				} else {
					orphans = append(orphans, ev.Name)
				}
			}
			if len(orphans) > 0 {
				spDoc.Events = kept
				if err := util.BackupBeforeWrite(spawnsPath); err != nil {
					return nil, err
				}
				if err := dztypes.SaveEventSpawns(spawnsPath, spDoc); err != nil {
					return nil, err
				}
				sort.Strings(orphans)
				out = append(out, fmt.Sprintf("removed spawn positions for %d orphan %s in cfgeventspawns.xml (%s)",
					len(orphans), plural(len(orphans), "event", "events"), strings.Join(orphans, ", ")))
			}
		}
	}

	// ---- v1.0.0 fixes ----
	type step struct {
		codes []string
		run   func() ([]string, error)
	}
	steps := []step{
		{[]string{"encoding.utf16"}, func() ([]string, error) { return fixEncoding(o) }},
		{[]string{"json.syntax"}, func() ([]string, error) { return fixJSON(o) }},
		{[]string{"cfg.semicolon"}, func() ([]string, error) { return fixServerCfgSemicolons(o) }},
		{[]string{"xml.amp"}, func() ([]string, error) { return fixXMLAmps(xmlFilesWithEntityErrors(serverDir)) }},
		{[]string{"ce.missing"}, func() ([]string, error) { return fixCEMissing(o) }},
		{[]string{"ce.type"}, func() ([]string, error) { return fixCETypes(o) }},
		{[]string{"types.minNominal", "types.restockNeg", "types.quantRange", "types.quantOrder"}, func() ([]string, error) {
			sub := map[string]bool{}
			for _, c := range []string{"types.minNominal", "types.restockNeg", "types.quantRange", "types.quantOrder"} {
				if do(c) {
					sub[c] = true
				}
			}
			return fixTypeNumbers(o, sub)
		}},
		{[]string{"spawnable.chance", "spawnable.preset"}, func() ([]string, error) {
			return fixSpawnable(o, do("spawnable.chance"), do("spawnable.preset"))
		}},
		{[]string{"globals.duplicate"}, func() ([]string, error) { return fixGlobals(o) }},
		{[]string{"eventspawns.bounds"}, func() ([]string, error) { return fixSpawnBounds(o) }},
		{[]string{"mods.keys"}, func() ([]string, error) { return fixModKeys(o) }},
		{[]string{"mods.linuxcase"}, func() ([]string, error) { return fixLinuxCase(o) }},
	}
	for _, st := range steps {
		run := false
		for _, c := range st.codes {
			if do(c) {
				run = true
			}
		}
		if !run {
			continue
		}
		done, err := st.run()
		out = append(out, done...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// xmlFilesWithEntityErrors lists the mission XML files whose parse fails on a
// bare "&" — the only XML syntax error with a safe mechanical fix.
func xmlFilesWithEntityErrors(serverDir string) []string {
	var out []string
	_ = filepath.Walk(filepath.Join(serverDir, "mpmissions"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.EqualFold(filepath.Ext(path), ".xml") {
			return nil
		}
		if is := validateXML(path); is != nil && is.Fix == "xml.amp" {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// plural picks the singular or plural word for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// whitelistLimits inserts the missing names into the right section of
// cfglimitsdefinition.xml, before that section's closing tag. Backs up first.
func whitelistLimits(path string, miss map[string]map[string]string) (map[string][]string, error) {
	written := map[string][]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := string(data)
	// kind -> (element tag, section closing tag)
	sections := []struct{ kind, elem, closeTag string }{
		{"category", "category", "</categories>"},
		{"usage", "usage", "</usageflags>"},
		{"value", "value", "</valueflags>"},
		{"tag", "tag", "</tags>"},
	}
	for _, sec := range sections {
		names := miss[sec.kind]
		if len(names) == 0 {
			continue
		}
		var b strings.Builder
		for _, orig := range names {
			b.WriteString(fmt.Sprintf("\t\t<%s name=\"%s\"/>\n", sec.elem, xmlEscapeAttr(orig)))
		}
		if idx := strings.LastIndex(s, sec.closeTag); idx != -1 {
			s = s[:idx] + b.String() + "\t" + s[idx:]
		} else if end := strings.LastIndex(s, "</lists>"); end != -1 {
			// Section missing entirely — create it.
			plural := map[string]string{"category": "categories", "usage": "usageflags", "value": "valueflags", "tag": "tags"}[sec.kind]
			block := fmt.Sprintf("\t<%s>\n%s\t</%s>\n", plural, b.String(), plural)
			s = s[:end] + block + s[end:]
		} else {
			// Neither anchor found: this is not a limits file we understand,
			// and guessing would corrupt it.
			return nil, fmt.Errorf("cannot find where to insert %s names in %s — file has no <%s> section and no </lists>",
				sec.kind, filepath.Base(path), sec.kind)
		}
		for _, orig := range names {
			written[sec.kind] = append(written[sec.kind], orig)
		}
	}
	if err := util.BackupBeforeWrite(path); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return written, nil
}

func xmlEscapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func validateCFG(path string) *Issue {
	data, err := os.ReadFile(path)
	if err != nil {
		return &Issue{File: path, Severity: SevError, Message: err.Error()}
	}
	// Braces inside a quoted value are data, not structure: a
	// `hostname = "My { server";` used to be reported as unbalanced.
	// (`close` also shadowed the builtin here.)
	opened, closed, inStr := 0, 0, false
	for _, r := range string(data) {
		switch {
		case r == '"':
			inStr = !inStr
		case r == '\n':
			inStr = false // an unterminated quote must not swallow the file
		case inStr:
		case r == '{':
			opened++
		case r == '}':
			closed++
		}
	}
	if opened != closed {
		return &Issue{File: path, Severity: SevError, Code: "cfg.braces",
			Message: fmt.Sprintf("unbalanced braces: %d '{' vs %d '}'", opened, closed), Args: A("open", opened, "close", closed)}
	}
	return nil
}
