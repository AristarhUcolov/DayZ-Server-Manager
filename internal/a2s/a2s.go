// Copyright (c) 2026 Aristarh Ucolov.
//
// Package a2s asks a game server the Steam "A2S_INFO" question over UDP — the
// same query the in-game server browser sends. It tells us, without RCon,
// whether the server is actually up and listed (a DayZ process can run for a
// minute or more loading the mission before it answers), its public name and
// map, and how many players are on.
package a2s

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// Info is the subset of the A2S_INFO reply the panel uses.
type Info struct {
	Name       string `json:"name"`
	Map        string `json:"map"`
	Folder     string `json:"folder"`
	Game       string `json:"game"`
	Players    int    `json:"players"`
	MaxPlayers int    `json:"maxPlayers"`
	Version    string `json:"version"`
}

var request = append([]byte{0xFF, 0xFF, 0xFF, 0xFF, 'T'}, []byte("Source Engine Query\x00")...)

// Query sends A2S_INFO to addr ("127.0.0.1:27016"), answering a challenge if
// the server asks for one (servers updated after Dec 2020 do).
func Query(addr string, timeout time.Duration) (*Info, error) {
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	req := request
	buf := make([]byte, 1400)
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		b := buf[:n]
		if len(b) < 5 || !bytes.Equal(b[:4], []byte{0xFF, 0xFF, 0xFF, 0xFF}) {
			return nil, errors.New("not an A2S reply")
		}
		switch b[4] {
		case 'A': // S2C_CHALLENGE: resend with the 4-byte challenge appended
			if len(b) < 9 {
				return nil, errors.New("short challenge")
			}
			req = append(append([]byte(nil), request...), b[5:9]...)
			continue
		case 'I':
			return parseInfo(b[5:])
		default:
			return nil, fmt.Errorf("unexpected A2S reply type 0x%02x", b[4])
		}
	}
	return nil, errors.New("server kept answering with challenges")
}

func parseInfo(b []byte) (*Info, error) {
	r := bytes.NewReader(b)
	readStr := func() (string, error) {
		var out []byte
		for {
			c, err := r.ReadByte()
			if err != nil {
				return "", err
			}
			if c == 0 {
				return string(out), nil
			}
			out = append(out, c)
		}
	}
	var proto byte
	if err := binary.Read(r, binary.LittleEndian, &proto); err != nil {
		return nil, err
	}
	info := &Info{}
	var err error
	if info.Name, err = readStr(); err != nil {
		return nil, err
	}
	if info.Map, err = readStr(); err != nil {
		return nil, err
	}
	if info.Folder, err = readStr(); err != nil {
		return nil, err
	}
	if info.Game, err = readStr(); err != nil {
		return nil, err
	}
	var appID uint16
	var players, maxPlayers byte
	if err := binary.Read(r, binary.LittleEndian, &appID); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &players); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &maxPlayers); err != nil {
		return nil, err
	}
	info.Players, info.MaxPlayers = int(players), int(maxPlayers)
	// bots, server type, environment, visibility, VAC
	if _, err := r.Seek(5, 1); err == nil {
		info.Version, _ = readStr()
	}
	return info, nil
}
