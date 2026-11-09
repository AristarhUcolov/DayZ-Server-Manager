// Copyright (c) 2026 Aristarh Ucolov.
//
// Steam Workshop collections and required items.
//
// Whether a link is a collection, and what is in it, comes from Steam's
// public GetCollectionDetails API. A mod's "Required items" are only on its
// web page: that one box of the page is read, never the page's other links.
//
// Network call is opt-in and isolated to this file so the rest of the mod
// package stays offline-friendly.
package mods

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	reCollectionID = regexp.MustCompile(`[?&]id=(\d+)`)
	// An item link: …/sharedfiles/filedetails/?id= or …/workshop/filedetails/?id=.
	reItemLink = regexp.MustCompile(`filedetails/\?id=(\d+)`)
)

type CollectionResolution struct {
	CollectionID string   `json:"collectionId"`
	ChildIDs     []string `json:"childIds"`
	// Resolved: Workshop mods actually present under !Workshop, keyed by ID.
	Resolved []ResolvedMod `json:"resolved"`
	// Missing IDs the user has not yet subscribed/downloaded in Steam.
	Missing []string `json:"missing"`
}

type ResolvedMod struct {
	PublishedID string `json:"publishedId"`
	ModName     string `json:"modName"` // @Foo
	DisplayName string `json:"displayName,omitempty"`
}

// ParseCollectionURL extracts the numeric workshop ID from any Steam URL.
// Accepts bare IDs too so users can paste "1234567890" if they prefer.
func ParseCollectionURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty url")
	}
	// Bare numeric ID.
	if _, err := fmt.Sscanf(input, "%d", new(uint64)); err == nil && !strings.ContainsAny(input, "/?=&") {
		return input, nil
	}
	if !strings.HasPrefix(input, "http") {
		// Let users paste without scheme ("steamcommunity.com/.../?id=...").
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if id := u.Query().Get("id"); id != "" {
		return id, nil
	}
	if m := reCollectionID.FindStringSubmatch(input); len(m) == 2 {
		return m[1], nil
	}
	return "", fmt.Errorf("could not find collection id in URL")
}

// FetchCollectionChildren returns the items of a Workshop collection, or
// [id] when id is an ordinary item: the import takes either. Steam's own
// GetCollectionDetails says which it is. (The panel used to collect every
// item link on the item's web page — a mod page links its required items,
// the author's other mods and popular ones, so pasting one mod downloaded
// others instead.)
func FetchCollectionChildren(collectionID string) ([]string, error) {
	if collectionID == "" {
		return nil, fmt.Errorf("collection id required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cols, err := FetchCollections(ctx, []string{collectionID})
	if err != nil {
		return nil, fmt.Errorf("fetch collection: %w", err)
	}
	if kids, ok := cols[collectionID]; ok {
		return kids, nil
	}
	return []string{collectionID}, nil
}

// FetchCollections asks Steam which of ids are collections and returns their
// items (a collection inside a collection is opened too); ids missing from
// the map are ordinary items.
func FetchCollections(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	top, err := collectionDetails(ctx, ids)
	if err != nil {
		return out, err
	}
	var nested []string
	for _, kids := range top {
		nested = append(nested, kids...)
	}
	inner, _ := collectionDetails(ctx, nested) // best effort
	for id, kids := range top {
		seen := map[string]bool{id: true}
		var list []string
		for _, k := range kids {
			sub, isCol := inner[k]
			if !isCol {
				sub = []string{k}
			}
			for _, s := range sub {
				if !seen[s] {
					seen[s] = true
					list = append(list, s)
				}
			}
		}
		out[id] = list
	}
	return out, nil
}

// collectionDetails is ISteamRemoteStorage/GetCollectionDetails: collection
// id → its children, for the ids that are collections.
func collectionDetails(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	var clean []string
	for _, id := range ids {
		if isNumeric(id) {
			clean = append(clean, id)
		}
	}
	for i := 0; i < len(clean); i += 100 {
		batch := clean[i:min(i+100, len(clean))]
		form := url.Values{}
		form.Set("collectioncount", fmt.Sprint(len(batch)))
		for j, id := range batch {
			form.Set(fmt.Sprintf("publishedfileids[%d]", j), id)
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, err := http.NewRequestWithContext(rctx, http.MethodPost,
			"https://api.steampowered.com/ISteamRemoteStorage/GetCollectionDetails/v1/", strings.NewReader(form.Encode()))
		if err != nil {
			cancel()
			return out, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "DayZ-Server-Manager (workshop-check)")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return out, err
		}
		var raw struct {
			Response struct {
				Details []struct {
					ID       string `json:"publishedfileid"`
					Result   int    `json:"result"`
					Children []struct {
						ID string `json:"publishedfileid"`
					} `json:"children"`
				} `json:"collectiondetails"`
			} `json:"response"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw)
		resp.Body.Close()
		cancel()
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("steam: HTTP %d", resp.StatusCode)
		}
		if err != nil {
			return out, fmt.Errorf("steam: decode: %w", err)
		}
		for _, d := range raw.Response.Details {
			if d.Result != 1 || len(d.Children) == 0 {
				continue // 9: not a collection
			}
			var kids []string
			for _, c := range d.Children {
				if isNumeric(c.ID) {
					kids = append(kids, c.ID)
				}
			}
			out[d.ID] = kids
		}
	}
	return out, nil
}

// FetchRequiredItems reads the "Required items" box of an item's public page
// (the mods it needs, CF for most). Only that box: the rest of the page links
// unrelated items. Steam's web API does not list them.
func FetchRequiredItems(ctx context.Context, id string) ([]string, error) {
	if !isNumeric(id) {
		return nil, fmt.Errorf("bad id %q", id)
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, "https://steamcommunity.com/sharedfiles/filedetails/?id="+id, nil)
	if err != nil {
		return nil, err
	}
	// Steam serves a cut-down page to unidentified clients.
	req.Header.Set("User-Agent", "Mozilla/5.0 (dayz-manager; +https://github.com/)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return requiredItemsIn(string(body), id), nil
}

// requiredItemsIn picks the item links out of a page's RequiredItems box.
func requiredItemsIn(page, self string) []string {
	i := strings.Index(page, `id="RequiredItems"`)
	if i < 0 {
		return nil
	}
	box := page[i:]
	// The box ends where the next panel of the side bar starts.
	for _, end := range []string{"<!--", `class="panel"`, `rightSectionTopTitle`} {
		if j := strings.Index(box, end); j > 0 && j < len(box) {
			box = box[:j]
		}
	}
	seen := map[string]bool{self: true}
	var out []string
	for _, m := range reItemLink.FindAllStringSubmatch(box, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// ResolveCollection matches a list of publishedids to @Mod directories in
// the mod source (client !Workshop, custom folder or SteamCMD content). Three-pass strategy:
//
//  1. Match by meta.cpp PublishedID (cheapest, exact).
//  2. For unmatched IDs, hit Steam GetPublishedFileDetails to learn the title
//     and match by normalized folder name ("@FooBar" ↔ "Foo Bar"). This
//     catches mods where the user's local meta.cpp is stale or missing,
//     which is common for older DayZ mods.
//  3. Anything still unmatched lands in Missing (user genuinely needs to
//     subscribe in Steam).
//
// The Steam call is best-effort — if it fails, fallback IDs stay missing.
func ResolveCollection(ctx context.Context, srcDir string, ids []string) (*CollectionResolution, error) {
	if srcDir == "" {
		return nil, ErrNoSource
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}

	// Snapshot source mods once with their meta.cpp so we don't re-stat.
	type wsEntry struct {
		Folder      string // "@CommunityFramework"
		PublishedID string // "" if no meta.cpp / no publishedid line
		Name        string // human-readable from meta.cpp; "" if absent
	}
	usedFolder := map[string]bool{}
	var folders []wsEntry
	for _, sm := range scanSourceList(srcDir) {
		meta := readMeta(sm.path)
		pid := meta.PublishedID
		if pid == "" && sm.steamcmd {
			pid = filepath.Base(sm.path) // SteamCMD folders ARE the published id
		}
		folders = append(folders, wsEntry{Folder: sm.name, PublishedID: pid, Name: meta.Name})
	}

	byID := map[string]ResolvedMod{}

	// Pass 1: exact PublishedID match.
	for _, f := range folders {
		if f.PublishedID == "" || !want[f.PublishedID] {
			continue
		}
		byID[f.PublishedID] = ResolvedMod{PublishedID: f.PublishedID, ModName: f.Folder, DisplayName: f.Name}
		usedFolder[f.Folder] = true
	}

	// Pass 2: Steam-aided name match for the leftover IDs.
	leftover := make([]string, 0)
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			leftover = append(leftover, id)
		}
	}
	if len(leftover) > 0 {
		// Bound the Steam call so a slow upstream never hangs the request.
		sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		remote, err := FetchWorkshopMeta(sctx, leftover)
		if err == nil {
			// Build lookup from normalized folder name → wsEntry.
			folderNorm := map[string]wsEntry{}
			for _, f := range folders {
				if usedFolder[f.Folder] {
					continue
				}
				folderNorm[normalizeName(strings.TrimPrefix(f.Folder, "@"))] = f
			}
			for id, r := range remote {
				if r.Result != 1 || r.Title == "" {
					continue
				}
				key := normalizeName(r.Title)
				if f, ok := folderNorm[key]; ok && !usedFolder[f.Folder] {
					byID[id] = ResolvedMod{PublishedID: id, ModName: f.Folder, DisplayName: f.Name}
					usedFolder[f.Folder] = true
					delete(folderNorm, key)
				}
			}
		}
	}

	res := &CollectionResolution{ChildIDs: ids}
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			res.Resolved = append(res.Resolved, r)
		} else {
			res.Missing = append(res.Missing, id)
		}
	}
	return res, nil
}

// normalizeName lowercases and strips non-alphanumerics so "Community-Online
// Tools!" and "@CommunityOnlineTools" match. DayZ mod authors are creative
// with separators, so this is the cheapest fuzzy match that is still safe.
func normalizeName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
