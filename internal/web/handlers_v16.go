// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.16.0 handlers: the in-panel beginner's guide.
package web

import (
	"net/http"

	"dayzmanager/internal/guide"
)

// guideGet serves the beginner's guide for a locale. Long-form documentation
// is kept out of the i18n bundle (see internal/guide), so this is a separate
// endpoint rather than another pile of translation keys.
func (h *handlers) guideGet(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("lang")
	if code == "" {
		code = h.app.Cfg().Language
	}
	writeJSON(w, map[string]interface{}{
		"lang":     code,
		"chapters": guide.Get(code),
	})
}
