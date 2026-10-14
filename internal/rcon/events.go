// Copyright (c) 2026 Aristarh Ucolov.
package rcon

// Player connects and disconnects from the BattlEye message stream — the
// real-time source for Discord's join/leave notifications and for linking a
// player's BattlEye GUID and IP to the player database.
//
// BattlEye pushes lines like:
//
//	Player #3 Survivor (203.0.113.42:2304) connected
//	Player #3 Survivor - BE GUID: 4a9f0c…(32 hex)
//	Verified GUID (4a9f0c…) of player #3 Survivor
//	Player #3 Survivor disconnected

import (
	"regexp"
	"strconv"
	"strings"
)

// ServerEvent is one player event from the stream.
type ServerEvent struct {
	Kind string // "connect" | "guid" | "disconnect"
	No   int    // player number on the server
	Name string
	IP   string
	GUID string
}

var (
	reConnected    = regexp.MustCompile(`^Player #(\d+) (.+) \(([0-9a-fA-F.:]+):(\d+)\) connected$`)
	reBEGUIDLine   = regexp.MustCompile(`^Player #(\d+) (.+) - (?:BE )?GUID: ([0-9a-fA-F]{32})`)
	reVerifiedGUID = regexp.MustCompile(`^Verified GUID \(([0-9a-fA-F]{32})\) of player #(\d+) (.+)$`)
	reDisconnected = regexp.MustCompile(`^Player #(\d+) (.+) disconnected$`)
)

func parseServerEvent(line string) (ServerEvent, bool) {
	line = strings.TrimSpace(line)
	if m := reConnected.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return ServerEvent{Kind: "connect", No: n, Name: m[2], IP: m[3]}, true
	}
	if m := reBEGUIDLine.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return ServerEvent{Kind: "guid", No: n, Name: m[2], GUID: strings.ToLower(m[3])}, true
	}
	if m := reVerifiedGUID.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[2])
		return ServerEvent{Kind: "guid", No: n, Name: m[3], GUID: strings.ToLower(m[1])}, true
	}
	if m := reDisconnected.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return ServerEvent{Kind: "disconnect", No: n, Name: m[2]}, true
	}
	return ServerEvent{}, false
}

// SetEventSink registers a callback for player connects/disconnects. It runs
// on the RCon reader goroutine and must not block.
func (m *Manager) SetEventSink(fn func(ServerEvent)) {
	m.chatMu.Lock()
	m.eventSink = fn
	m.chatMu.Unlock()
}

func (m *Manager) emitEvents(raw string) {
	m.chatMu.Lock()
	sink := m.eventSink
	m.chatMu.Unlock()
	if sink == nil {
		return
	}
	for _, line := range strings.Split(raw, "\n") {
		if ev, ok := parseServerEvent(strings.TrimRight(line, "\r")); ok {
			sink(ev)
		}
	}
}
