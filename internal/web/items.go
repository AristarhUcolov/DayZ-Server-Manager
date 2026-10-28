// Copyright (c) 2026 Aristarh Ucolov.
//
// The item catalog for the pickers (Player loadout first): every item of the
// game and of the server's mods with its name in the panel's language, the
// player slots it fits and a category — see internal/items.
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dayzmanager/internal/items"
)

// catalogData is the item catalog of this server's game files and mods.
func (h *handlers) catalogData() (*items.Data, error) {
	cfg := h.app.Cfg()
	// The server's mods, as it loads them: client mods and server-side mods.
	var modDirs []string
	seen := map[string]bool{}
	for _, m := range append(append([]string{}, cfg.Mods...), cfg.ServerMods...) {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		dir := m
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(h.app.ServerDir, m)
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() && !seen[strings.ToLower(dir)] {
			seen[strings.ToLower(dir)] = true
			modDirs = append(modDirs, dir)
		}
	}
	data, err := items.Load(h.app.ServerDir, modDirs)
	if err != nil && h.server != nil && h.server.fleet != nil {
		// A server folder without the game's archives (a mission-only copy):
		// the panel's main server has the same game.
		if main := h.server.fleet.Get("main"); main != nil && main != h.app {
			data, err = items.Load(main.ServerDir, modDirs)
		}
	}
	return data, err
}

// itemsCatalog is GET /api/items/catalog?lang=xx.
func (h *handlers) itemsCatalog(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = h.app.Cfg().Language
	}
	data, err := h.catalogData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	cat := data.Localize(lang)
	// Items the mission's types files spawn that no readable config describes
	// (a mod whose archives could not be read, say): listed by class name, so
	// the pickers still offer everything the server can spawn.
	extra := 0
	if names, err := h.typesClassNames(); err == nil {
		for _, n := range names {
			if !data.Known(n) {
				cat.Items = append(cat.Items, items.Item{Class: n, Name: items.Humanize(n), Cat: "other", Mod: "?"})
				extra++
			}
		}
	}
	writeJSON(w, map[string]interface{}{
		"items": cat.Items, "survivors": cat.Survivors, "pbos": cat.PBOs, "mods": cat.Mods,
		"built": cat.Built, "fromTypes": extra, "slots": items.PlayerSlots,
	})
}

// itemsDetail is GET /api/items/detail?class=AKM&lang=xx: what goes on and
// into one item — its attachment slots with the items that fit, a weapon's
// magazines, whether it has cargo space.
func (h *handlers) itemsDetail(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = h.app.Cfg().Language
	}
	data, err := h.catalogData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	det, ok := data.Detail(r.URL.Query().Get("class"), lang)
	if !ok {
		writeJSON(w, map[string]interface{}{"c": r.URL.Query().Get("class"), "attach": []interface{}{}, "unknown": true})
		return
	}
	writeJSON(w, det)
}
