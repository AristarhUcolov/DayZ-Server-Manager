// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The log viewer's download sends the whole file of a known source only —
// the current run's or an earlier one's — never an arbitrary path.
func TestLogsDownload(t *testing.T) {
	h, dir := testHandlers(t)
	get := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.logsDownload(w, httptest.NewRequest(http.MethodGet, "/api/logs/download?id="+id, nil))
		return w
	}
	if w := get("..%2Fsecret"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown id: %d", w.Code)
	}
	if w := get("rpt"); w.Code != http.StatusNotFound {
		t.Fatalf("no RPT yet: %d", w.Code)
	}
	old := filepath.Join(dir, "DayZServer_x64_2026-10-12_10-00-00.RPT")
	cur := filepath.Join(dir, "DayZServer_x64_2026-10-13_10-00-00.RPT")
	body := strings.Repeat("line of the crashed run\n", 20000) // more than the viewer's tail
	put(t, old, body)
	put(t, cur, "fresh run\n")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	w := get("rpt")
	if w.Code != 200 || w.Body.String() != "fresh run\n" {
		t.Fatalf("current RPT: %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="DayZServer_x64_2026-10-13_10-00-00.RPT"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	w = get("rpt:DayZServer_x64_2026-10-12_10-00-00.RPT")
	if w.Code != 200 || w.Body.String() != body {
		t.Fatalf("earlier run: %d, %d bytes", w.Code, w.Body.Len())
	}
}
