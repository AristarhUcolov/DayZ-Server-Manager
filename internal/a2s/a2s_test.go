// Copyright (c) 2026 Aristarh Ucolov.
package a2s

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// TestQueryWithChallenge runs a fake server that first demands a challenge,
// then answers A2S_INFO like DayZ does.
func TestQueryWithChallenge(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 1500)
		for i := 0; i < 2; i++ {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req := buf[:n]
			if !bytes.HasSuffix(req, []byte{1, 2, 3, 4}) {
				_, _ = pc.WriteTo([]byte{0xFF, 0xFF, 0xFF, 0xFF, 'A', 1, 2, 3, 4}, addr)
				continue
			}
			var b bytes.Buffer
			b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 'I', 17})
			for _, s := range []string{"My DayZ", "chernarusplus", "dayz", "DayZ"} {
				b.WriteString(s)
				b.WriteByte(0)
			}
			b.Write([]byte{0x2C, 0x03, 7, 60, 0, 'd', 'w', 0, 1})
			b.WriteString("1.26.0")
			b.WriteByte(0)
			_, _ = pc.WriteTo(b.Bytes(), addr)
		}
	}()
	info, err := Query(pc.LocalAddr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "My DayZ" || info.Map != "chernarusplus" || info.Players != 7 || info.MaxPlayers != 60 || info.Version != "1.26.0" {
		t.Fatalf("info = %+v", info)
	}
}

func TestQueryTimeout(t *testing.T) {
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()
	if _, err := Query(pc.LocalAddr().String(), 300*time.Millisecond); err == nil {
		t.Fatal("silent server did not time out")
	}
}
