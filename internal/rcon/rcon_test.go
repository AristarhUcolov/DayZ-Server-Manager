// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
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
				if text, ok := strings.CutPrefix(cmd, "say -1 "); ok {
					// Relay the say to RCon clients BEFORE acknowledging it — the
					// ordering that used to show our own message twice.
					relay := "RCon admin #0: (Global) " + text
					_, _ = conn.WriteToUDP(frame(append([]byte{0x02, 0x00}, relay...)), raddr)
					_, _ = conn.WriteToUDP(frame([]byte{0x01, seq}), raddr)
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

// A command whose connection is closed while it waits must fail — it used to
// return an empty reply with a nil error, so "message sent" was reported for
// a message that never reached the server.
func TestCommandFailsWhenClosedMidFlight(t *testing.T) {
	// A server that answers the login but never replies to commands.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if pkt, err := parseHeader(buf[:n]); err == nil && len(pkt) > 0 && pkt[0] == 0x00 {
				_, _ = conn.WriteToUDP(frame([]byte{0x00, 0x01}), raddr)
			}
		}
	}()
	c, err := Dial(conn.LocalAddr().String(), "x")
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { _, err := c.Command("say -1 hello"); errc <- err }()
	time.Sleep(200 * time.Millisecond)
	_ = c.Close()
	select {
	case err := <-errc:
		if err != ErrNotConnected {
			t.Fatalf("got %v, want ErrNotConnected", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Command did not return after Close")
	}
}

// Say through the Manager records the delivered message in the chat feed.
func TestManagerSayRecordsMessage(t *testing.T) {
	addr, stop := fakeBE(t, "secret")
	defer stop()
	h, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	m := NewManager()
	m.Configure(h, port, "secret")
	defer m.Close()
	if err := m.Say("Restart in 5 min\nplease log out"); err != nil {
		t.Fatal(err)
	}
	got := m.RecentChat(5)
	if len(got) != 1 || got[0].Source != ChatFromManager || got[0].Text != "Restart in 5 min please log out" {
		t.Fatalf("recorded: %+v", got)
	}
}

// A Conn that closed itself (keepAlive's stale check) must not count as
// connected, and the next Connect must dial a fresh one.
func TestManagerDropsSelfClosedConn(t *testing.T) {
	addr, stop := fakeBE(t, "secret")
	defer stop()
	h, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	m := NewManager()
	m.Configure(h, port, "secret")
	defer m.Close()
	if err := m.Connect(); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	first := m.conn
	m.mu.Unlock()
	_ = first.Close() // what keepAlive does after staleTimeout
	if m.IsConnected() {
		t.Fatal("IsConnected reports a self-closed conn as connected")
	}
	if err := m.Connect(); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	second := m.conn
	m.mu.Unlock()
	if second == nil || second == first || !m.IsConnected() {
		t.Fatal("Connect did not replace the self-closed conn")
	}
}

// Connect must not hold the manager lock while a dial waits for a server that
// never answers (booting DayZ), or Configured/IsConnected stall for seconds.
func TestConnectDoesNotBlockStatusCalls(t *testing.T) {
	silent, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	m := NewManager()
	m.Configure("127.0.0.1", silent.LocalAddr().(*net.UDPAddr).Port, "secret")
	done := make(chan error, 1)
	go func() { done <- m.Connect() }()
	time.Sleep(200 * time.Millisecond) // let the dial start waiting
	start := time.Now()
	if !m.Configured() || m.IsConnected() {
		t.Fatal("unexpected status while dialing")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("status calls blocked %s behind the dial", d)
	}
	if err := <-done; err == nil {
		t.Fatal("dial to a silent server succeeded")
	}
}
