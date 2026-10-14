// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// One port, both protocols: plain HTTP for the local window, HTTPS for phones.
func TestSniffListenerServesHTTPAndHTTPS(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := generateSelfSigned(certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := newSniffListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			_, _ = w.Write([]byte("tls"))
		} else {
			_, _ = w.Write([]byte("plain"))
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	addr := raw.Addr().String()

	// A silent client must not block others from being accepted.
	idle, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()

	get := func(c *http.Client, url string) string {
		resp, err := c.Get(url)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	plain := &http.Client{Timeout: 5 * time.Second}
	secure := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if got := get(plain, "http://"+addr+"/"); got != "plain" {
		t.Fatalf("http: %q", got)
	}
	if got := get(secure, "https://"+addr+"/"); got != "tls" {
		t.Fatalf("https: %q", got)
	}
}
