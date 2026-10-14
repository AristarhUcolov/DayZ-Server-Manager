// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
)

// countingBE is a tiny BattlEye RCon server that counts login attempts:
// logins with password succeed, others fail.
func countingBE(t *testing.T, password string) (string, int, *atomic.Int32, func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	var logins atomic.Int32
	go func() {
		buf := make([]byte, 4096)
		for {
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			pkt, err := parseHeader(buf[:n])
			if err != nil || len(pkt) < 1 {
				continue
			}
			switch pkt[0] {
			case 0x00:
				logins.Add(1)
				ok := byte(0x00)
				if string(pkt[1:]) == password {
					ok = 0x01
				}
				_, _ = conn.WriteToUDP(frame([]byte{0x00, ok}), raddr)
			case 0x01:
				if len(pkt) > 2 { // a command (keep-alives carry none)
					_, _ = conn.WriteToUDP(frame([]byte{0x01, pkt[1]}), raddr)
				}
			}
		}
	}()
	h, p, _ := net.SplitHostPort(conn.LocalAddr().String())
	port, _ := strconv.Atoi(p)
	return h, port, &logins, func() { _ = conn.Close() }
}

// The panel's password and BattlEye's live one can differ (BattlEye loads its
// file only at server start): every known password is tried, and the one that
// worked is tried first from then on.
func TestManagerTriesEveryKnownPassword(t *testing.T) {
	host, port, logins, stop := countingBE(t, "live-one")
	defer stop()

	m := NewManager()
	m.SetCredentials(func() Creds {
		return Creds{Host: host, Port: port, Passwords: []string{"saved-in-panel", "", "live-one", "saved-in-panel"}}
	})
	if !m.Configured() {
		t.Fatal("not configured with two candidate passwords")
	}
	if err := m.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := logins.Load(); got != 2 {
		t.Fatalf("logins = %d, want 2 (the wrong one, then the live one)", got)
	}
	// Reconnecting starts with the password that worked.
	m.Close()
	logins.Store(0)
	if err := m.Connect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if got := logins.Load(); got != 1 {
		t.Fatalf("reconnect logins = %d, want 1", got)
	}
	m.Close()
}

// When BattlEye rejects every password the error says so (and still matches
// ErrAuthFailed), and the same set is not hammered again on every poll — a
// changed set is tried at once.
func TestManagerRejectedPasswordsBackOff(t *testing.T) {
	host, port, logins, stop := countingBE(t, "live-one")
	defer stop()

	pw := atomic.Value{}
	pw.Store("wrong")
	m := NewManager()
	m.SetCredentials(func() Creds {
		return Creds{Host: host, Port: port, Passwords: []string{pw.Load().(string)}}
	})
	err := m.Connect()
	if !errors.Is(err, ErrAuthFailed) || !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("err = %v, want the password-mismatch auth error", err)
	}
	if !errors.Is(m.LastConnectErr(), ErrAuthFailed) {
		t.Fatalf("LastConnectErr = %v", m.LastConnectErr())
	}
	for i := 0; i < 3; i++ {
		if err := m.Connect(); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("retry %d: %v", i, err)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Fatalf("logins = %d, want 1 (rejected set rests)", got)
	}

	pw.Store("live-one") // e.g. the server restarted and loaded a new file
	if err := m.Connect(); err != nil {
		t.Fatalf("connect after the password changed: %v", err)
	}
	if m.LastConnectErr() != nil {
		t.Fatalf("LastConnectErr after success = %v", m.LastConnectErr())
	}
	m.Close()
}

func TestManagerWithoutPasswordIsNotConfigured(t *testing.T) {
	m := NewManager()
	m.SetCredentials(func() Creds { return Creds{Host: "127.0.0.1", Port: 2306} })
	if m.Configured() {
		t.Fatal("configured without any password")
	}
	if err := m.Connect(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
