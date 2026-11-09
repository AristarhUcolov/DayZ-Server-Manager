// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// JSON and text are compressed; pictures and archives (compressed already)
// go out as they are, with their length and range support intact.
func TestGzipSkipsCompressedData(t *testing.T) {
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte(strings.Repeat("x", 4000))...)
	h := gzipper(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Repeat(`{"a":1}`, 500)))
		case "/img":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpeg)
		case "/sniff":
			_, _ = w.Write(jpeg) // no type set: sniffed
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(strings.Repeat("<svg/>", 500)))
		case "/304":
			w.WriteHeader(http.StatusNotModified)
		}
	}))
	get := func(path string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result()
	}
	for _, p := range []string{"/json", "/svg"} {
		res := get(p)
		if res.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s must be compressed", p)
		}
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := io.ReadAll(zr); len(b) < 3000 {
			t.Fatalf("%s: %d bytes unpacked", p, len(b))
		}
	}
	for _, p := range []string{"/img", "/sniff"} {
		res := get(p)
		if res.Header.Get("Content-Encoding") != "" {
			t.Fatalf("%s is a picture: no gzip", p)
		}
		if b, _ := io.ReadAll(res.Body); len(b) != len(jpeg) {
			t.Fatalf("%s: %d bytes", p, len(b))
		}
	}
	if res := get("/304"); res.Header.Get("Content-Encoding") != "" || res.StatusCode != http.StatusNotModified {
		t.Fatal("a 304 has no body to compress")
	}
}
