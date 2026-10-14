// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"math"
	"net/http/httptest"
	"testing"
)

// A value JSON cannot encode must surface as an error, never as a 200 with an
// empty body (which the page reports as "Unexpected end of JSON input").
func TestWriteJSONNeverSendsAnEmptyOK(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]float64{"x": math.NaN()})
	if rec.Code != 500 || rec.Body.Len() == 0 {
		t.Fatalf("got %d with %d bytes, want a 500 with a message", rec.Code, rec.Body.Len())
	}
	rec = httptest.NewRecorder()
	writeJSON(rec, map[string]int{"ok": 1})
	if rec.Code != 200 || rec.Body.String() != "{\"ok\":1}\n" {
		t.Fatalf("normal value: %d %q", rec.Code, rec.Body.String())
	}
}
