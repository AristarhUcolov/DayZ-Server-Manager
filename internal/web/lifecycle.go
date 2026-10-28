// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
)

// The manager's own health as the dashboard shows it: how the previous run
// ended when that was not a normal exit (a crash, with what Go printed, or
// the process ended from outside), the last time the computer slept, and the
// "keep the computer awake" setting.

var sleepSeen struct {
	mu        sync.Mutex
	dismissed time.Time // the sleep (by its end) the admin closed
}

func (h *handlers) registerLifecycle(mux *http.ServeMux) {
	mux.HandleFunc("/api/app/lastexit", methods(h.appLastExit, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/api/app/keepawake", methods(h.appKeepAwake, http.MethodGet, http.MethodPost))
}

// appLastExit answers what the dashboard should report; POST {"what":
// "exit"|"sleep"} closes that notice.
func (h *handlers) appLastExit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			What string `json:"what"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.What {
		case "exit":
			if h.app.LastExit != nil {
				h.app.LastExit.Dismissed = true
			}
		case "sleep":
			_, to := app.LastSleep()
			sleepSeen.mu.Lock()
			sleepSeen.dismissed = to
			sleepSeen.mu.Unlock()
		}
	}
	out := map[string]interface{}{}
	if e := h.app.LastExit; e != nil && !e.Dismissed {
		out["exit"] = e
	}
	if from, to := app.LastSleep(); !to.IsZero() {
		sleepSeen.mu.Lock()
		seen := sleepSeen.dismissed.Equal(to)
		sleepSeen.mu.Unlock()
		if !seen {
			out["sleep"] = map[string]string{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339)}
		}
	}
	out["keepAwake"] = h.app.Cfg().KeepAwake
	if h.app.KeepingAwake != nil {
		out["awake"] = h.app.KeepingAwake()
	}
	writeJSON(w, out)
}

// appKeepAwake reads or sets the keep-awake mode: "" (while a DayZ server
// runs), "always" or "never" — and the guardian (on unless turned off),
// which starts the manager again if it stops unexpectedly. A POST changes
// what it names.
func (h *handlers) appKeepAwake(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Mode     *string `json:"mode"`
			Guardian *bool   `json:"guardian"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Mode != nil && *req.Mode != "" && *req.Mode != "always" && *req.Mode != "never" {
			http.Error(w, "mode must be empty, always or never", http.StatusBadRequest)
			return
		}
		if err := h.app.MutateConfig(func(c *config.Manager) error {
			if req.Mode != nil {
				c.KeepAwake = *req.Mode
			}
			if req.Guardian != nil {
				c.NoGuardian = !*req.Guardian
			}
			return nil
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	out := map[string]interface{}{"mode": h.app.Cfg().KeepAwake, "guardian": !h.app.Cfg().NoGuardian}
	if h.app.KeepingAwake != nil {
		out["awake"] = h.app.KeepingAwake()
	}
	writeJSON(w, out)
}
