// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Changing who may open the panel — and HTTPS — moves the listener while the
// manager runs: no restart, the same port, and the panel keeps answering.
func TestApplyAccessMovesTheListener(t *testing.T) {
	s, hd := testServer(t, "local")
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	if err != nil {
		t.Fatal(err)
	}
	s.port, s.bind = port, "127.0.0.1"
	s.next, s.installed = make(chan net.Listener, 1), make(chan struct{}, 1)
	s.http = &http.Server{Handler: hd, ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); _ = s.http.Close() })

	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true}}
	get := func(scheme string) error {
		var err error
		for try := 0; try < 20; try++ {
			var resp *http.Response
			if resp, err = client.Get(fmt.Sprintf("%s://127.0.0.1:%d/api/secret", scheme, port)); err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 || string(body) != "ok" {
					return fmt.Errorf("%s: %d %s", scheme, resp.StatusCode, body)
				}
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
		return err
	}
	if err := get("http"); err != nil {
		t.Fatal(err)
	}

	// Local network: every interface.
	s.app.Config.Exposure = "lan"
	if ch := s.ApplyAccess(); !ch.Applied || ch.Bind != "" {
		t.Fatalf("to LAN: %+v", ch)
	}
	if err := get("http"); err != nil {
		t.Fatalf("after moving to all interfaces: %v", err)
	}

	// HTTPS on the same port, still no restart.
	s.app.Config.PanelHTTPS = true
	if ch := s.ApplyAccess(); !ch.Applied || !ch.HTTPS {
		t.Fatalf("HTTPS on: %+v", ch)
	}
	if err := get("https"); err != nil {
		t.Fatalf("HTTPS after the switch: %v", err)
	}

	// Back to this computer only.
	s.app.Config.Exposure, s.app.Config.PanelHTTPS = "local", false
	if ch := s.ApplyAccess(); !ch.Applied || ch.Bind != "127.0.0.1" || ch.HTTPS {
		t.Fatalf("back to local: %+v", ch)
	}
	if err := get("http"); err != nil {
		t.Fatalf("after moving back: %v", err)
	}
	select {
	case err := <-served:
		t.Fatalf("Serve ended during the switches: %v", err)
	default:
	}

	// Changes in quick succession (quick saves) must each finish — a second
	// switch used to wait forever on a listener Serve had not taken yet.
	done := make(chan struct{})
	go func() {
		defer close(done)
		var mu sync.Mutex
		for i := 0; i < 6; i++ {
			mu.Lock()
			if i%2 == 0 {
				s.app.Config.Exposure = "lan"
			} else {
				s.app.Config.Exposure = "local"
			}
			mu.Unlock()
			if ch := s.ApplyAccess(); !ch.Applied {
				t.Errorf("switch %d: %+v", i, ch)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("quick successive access changes deadlocked")
	}
	if err := get("http"); err != nil {
		t.Fatalf("after quick changes: %v", err)
	}
	s.app.Config.Exposure = "local"
	s.ApplyAccess()

	// -bind chose the address: an access change keeps it.
	s.FixBind()
	s.app.Config.Exposure = "lan"
	if ch := s.ApplyAccess(); !ch.Applied || ch.Bind != "127.0.0.1" {
		t.Fatalf("with -bind: %+v", ch)
	}
}
