// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"strconv"
	"strings"
	"testing"
)

// frame wraps payload in the BE RCon header exactly like sendFrame does, for
// the fake server's replies.
func frame(payload []byte) []byte {
	body := append([]byte{0xFF}, payload...)
	crc := crc32.ChecksumIEEE(body)
	out := []byte{'B', 'E'}
	out = binary.LittleEndian.AppendUint32(out, crc)
	return append(out, body...)
}

// fakeBE starts a minimal in-process BattlEye RCon server that accepts the
// given password, answers login, replies to "players" with a canned table, and
// ignores keepalives. Returns its address and a stop func.
func fakeBE(t *testing.T, password string) (string, func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
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
			case 0x00: // login
				ok := byte(0x00)
				if string(pkt[1:]) == password {
					ok = 0x01
				}
				_, _ = conn.WriteToUDP(frame([]byte{0x00, ok}), raddr)
			case 0x01: // command
				if len(pkt) < 2 {
					continue
				}
				seq := pkt[1]
				cmd := string(pkt[2:])
				if cmd == "" { // keepalive — no reply
					continue
				}
				resp := "Players on server:\n" +
					"[#] [IP Address]:[Port] [Ping] [GUID] [Name]\n" +
					"--------------------------------------------------\n" +
					"0   127.0.0.1:2304    43   abcd1234(OK) TestPlayer\n" +
					"(1 players in total)"
				_, _ = conn.WriteToUDP(frame(append([]byte{0x01, seq}, []byte(resp)...)), raddr)
			}
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }
}

func TestDialAndCommandRoundTrip(t *testing.T) {
	addr, stop := fakeBE(t, "secret")
	defer stop()

	c, err := Dial(addr, "secret")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	out, err := c.Command("players")
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if !strings.Contains(out, "Players on server") {
		t.Fatalf("unexpected response: %q", out)
	}

	// And via the Manager (configure + cached players), exercising the full path.
	h, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	m := NewManager()
	m.Configure(h, port, "secret")
	players, err := m.PlayersCached(0)
	if err != nil {
		t.Fatalf("PlayersCached: %v", err)
	}
	if len(players) != 1 || players[0].Name != "TestPlayer" {
		t.Fatalf("parsePlayers: got %+v, want 1 player TestPlayer", players)
	}
}

func TestDialWrongPassword(t *testing.T) {
	addr, stop := fakeBE(t, "secret")
	defer stop()
	if _, err := Dial(addr, "wrong"); err != ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}
