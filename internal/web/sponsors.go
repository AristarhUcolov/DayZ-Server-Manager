// Copyright (c) 2026 Aristarh Ucolov.
//
// Sponsors — people who chipped in to keep the manager growing. Shown on the
// Sponsors page as a thank-you and a nudge for others. The message and reply
// are testimonials, kept verbatim in whatever language they were written; only
// the surrounding UI chrome is translated.
//
// To add a sponsor: append an entry below. Newest first.
package web

import "net/http"

// Sponsor is one supporter entry.
type Sponsor struct {
	Name    string `json:"name"`              // display name, or "Аноним" / "Anonymous"
	Amount  string `json:"amount,omitempty"`  // e.g. "500 ₽"
	Date    string `json:"date,omitempty"`    // free-form, e.g. "2026-08"
	Message string `json:"message,omitempty"` // what the sponsor wrote, verbatim
	Reply   string `json:"reply,omitempty"`   // the developer's reply, verbatim
	Link    string `json:"link,omitempty"`    // Steam / social, when the sponsor gave one
	Anon    bool   `json:"anon,omitempty"`    // shown as anonymous; can claim a real name
}

// sponsors is the supporter roll, newest first.
var sponsors = []Sponsor{
	{
		Name:    "Аноним",
		Amount:  "500 ₽",
		Date:    "2026-08",
		Message: "Менеджер хороший, надеюсь будет поддержка для Linux",
		Reply:   "Спасибо большое за поддержку Проекта, мы обязательно добавим.",
		Anon:    true,
	},
}

func (h *handlers) sponsorsList(w http.ResponseWriter, r *http.Request) {
	out := sponsors
	if out == nil {
		out = []Sponsor{}
	}
	writeJSON(w, map[string]interface{}{"sponsors": out})
}
