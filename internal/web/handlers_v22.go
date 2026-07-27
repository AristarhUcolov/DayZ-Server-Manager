// Copyright (c) 2026 Aristarh Ucolov.
//
// v0.22.0: the Server health page. One place that answers "is anything wrong?"
// by folding together the four signals the manager already computes but that
// were scattered across pages: is the process up (or stuck in a crash loop),
// is there disk room, does the config validate, and — if it won't start — why.
package web

import (
	"net/http"
	"time"

	dzlogs "dayzmanager/internal/logs"
	"dayzmanager/internal/util"
	"dayzmanager/internal/validator"
)

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	running := h.app.ServerIsRunning()

	// Disk free on the volume the server lives on. Best-effort: 0 on error.
	var diskFree uint64
	if f, err := util.DiskFree(h.app.ServerDir); err == nil {
		diskFree = f
	}

	// Config validity — the same pass the Validator page runs.
	var vErr, vWarn int
	mission, _ := h.missionTemplate()
	if issues, err := validator.ValidateAll(h.app.ServerDir, mission); err == nil {
		for _, is := range issues {
			switch is.Severity {
			case validator.SevError:
				vErr++
			case validator.SevWarning:
				vWarn++
			}
		}
	}

	// Start-failure diagnosis — only meaningful counts here; the detailed
	// findings are rendered separately by the existing diagnosis card.
	var dFatal, dWarn int
	for _, f := range dzlogs.Diagnose(h.app.ServerDir, h.app.Cfg().ProfilesDir) {
		switch f.Severity {
		case dzlogs.SevFatal:
			dFatal++
		case dzlogs.SevWarn:
			dWarn++
		}
	}

	writeJSON(w, map[string]interface{}{
		"server": map[string]interface{}{
			"running":    running,
			"loopPaused": h.app.Server.LoopPaused(),
			"pid":        h.app.Server.PID(),
			"uptime":     h.app.Server.Uptime().Round(time.Second).String(),
		},
		"disk":      map[string]interface{}{"free": diskFree},
		"validator": map[string]interface{}{"errors": vErr, "warnings": vWarn, "total": vErr + vWarn},
		"startup":   map[string]interface{}{"fatal": dFatal, "warnings": dWarn},
	})
}
