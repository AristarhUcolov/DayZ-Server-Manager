// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func markerCall(t *testing.T, h *handlers, body string) (int, []mapMarker, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.mapMarkersEdit(rec, httptest.NewRequest("POST", "/api/map/markers/edit", strings.NewReader(body)))
	var out struct {
		Markers []mapMarker `json:"markers"`
		ID      string      `json:"id"`
	}
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out.Markers, out.ID
}

func TestMapMarkers(t *testing.T) {
	h, _ := profileApp(t)
	code, list, id := markerCall(t, h, `{"op":"add","marker":{"x":4512.345,"z":10234.5,"label":"  Wolves base ","color":"teal"}}`)
	if code != 200 || len(list) != 1 || id == "" {
		t.Fatalf("add: %d %+v", code, list)
	}
	if m := list[0]; m.Label != "Wolves base" || m.Color != "red" || m.X != 4512.35 || m.At == "" {
		t.Errorf("stored %+v", m)
	}
	if code, _, _ := markerCall(t, h, `{"op":"add","marker":{"x":1,"z":1,"label":"   "}}`); code != 400 {
		t.Errorf("a marker without a label was accepted (%d)", code)
	}
	code, list, _ = markerCall(t, h, `{"op":"update","marker":{"id":"`+id+`","x":100,"z":200,"label":"Trader","note":"safe zone","color":"green"}}`)
	if code != 200 || len(list) != 1 || list[0].Label != "Trader" || list[0].Color != "green" || list[0].ID != id {
		t.Fatalf("update: %d %+v", code, list)
	}
	// The list survives a reload from disk.
	rec := httptest.NewRecorder()
	h.mapMarkers(rec, httptest.NewRequest("GET", "/api/map/markers", nil))
	if !strings.Contains(rec.Body.String(), `"Trader"`) {
		t.Errorf("GET: %s", rec.Body.String())
	}
	if code, _, _ := markerCall(t, h, `{"op":"update","marker":{"id":"nope","x":1,"z":1,"label":"x"}}`); code != 404 {
		t.Errorf("updating a missing marker: %d", code)
	}
	code, list, _ = markerCall(t, h, `{"op":"delete","marker":{"id":"`+id+`"}}`)
	if code != 200 || len(list) != 0 {
		t.Fatalf("delete: %d %+v", code, list)
	}
}
