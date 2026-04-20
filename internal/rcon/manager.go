// Copyright (c) 2026 Aristarh Ucolov.
//
// High-level RCon manager used by the web handlers. Maintains a single
// connection, reconnects on demand, and parses the stock `players` reply.
package rcon

import (
	"bufio"
	"fmt"
	"strings"
	"sync"
)

type Player struct {
	ID     int    `json:"id"`
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Ping   int    `json:"ping"`
	GUID   string `json:"guid"`
	Name   string `json:"name"`
	Lobby  bool   `json:"lobby"`
}

type Manager struct {
	mu   sync.Mutex
	conn *Conn
	host string
	port int
	pass string
}

func NewManager() *Manager { return &Manager{} }

// Configure stores connection params. Does not open the socket.
func (m *Manager) Configure(host string, port int, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.host == host && m.port == port && m.pass == password {
		return
	}
	m.host, m.port, m.pass = host, port, password
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// Connect opens (or reuses) the connection. Cheap if already open.
func (m *Manager) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		return nil
	}
	if m.host == "" || m.port == 0 || m.pass == "" {
		return fmt.Errorf("rcon: not configured (set host/port/password in settings)")
	}
	c, err := Dial(fmt.Sprintf("%s:%d", m.host, m.port), m.pass)
	if err != nil {
		return err
	}
	m.conn = c
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// Command sends a raw RCon command, reconnecting once on transport errors.
func (m *Manager) Command(cmd string) (string, error) {
	if err := m.Connect(); err != nil {
		return "", err
	}
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()
	out, err := c.Command(cmd)
	if err == nil {
		return out, nil
	}
	// one retry.
	m.Close()
	if err := m.Connect(); err != nil {
		return "", err
	}
	m.mu.Lock()
	c = m.conn
	m.mu.Unlock()
	return c.Command(cmd)
}

func (m *Manager) Players() ([]Player, error) {
	raw, err := m.Command("players")
	if err != nil {
		return nil, err
	}
	return parsePlayers(raw), nil
}

func (m *Manager) Say(msg string) error {
	_, err := m.Command("say -1 " + msg)
	return err
}

func (m *Manager) SayTo(playerID int, msg string) error {
	_, err := m.Command(fmt.Sprintf("say %d %s", playerID, msg))
	return err
}

func (m *Manager) Kick(playerID int, reason string) error {
	cmd := fmt.Sprintf("kick %d", playerID)
	if reason != "" {
		cmd += " " + reason
	}
	_, err := m.Command(cmd)
	return err
}

// Ban uses GUID-ban when possible, falls back to ID.
func (m *Manager) Ban(playerID int, minutes int, reason string) error {
	cmd := fmt.Sprintf("ban %d %d %s", playerID, minutes, reason)
	_, err := m.Command(cmd)
	return err
}

func (m *Manager) Shutdown() error {
	_, err := m.Command("#shutdown")
	return err
}

// parsePlayers parses the BE `players` output:
//
//   Players on server:
//   [#] [IP Address]:[Port] [Ping] [GUID] [Name]
//   --------------------------------------------------
//   0   203.0.113.42:2304    43   4a9f...(OK) MyNick
//   1   198.51.100.17:2304   -1   ...         AnotherGuy (Lobby)
//   (2 players in total)
//
func parsePlayers(raw string) []Player {
	var out []Player
	sc := bufio.NewScanner(strings.NewReader(raw))
	inTable := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "---") {
			inTable = true
			continue
		}
		if !inTable || strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "(") {
			break // "(N players in total)"
		}
		// Trim leading whitespace, split by whitespace (max 5 fields).
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		p := Player{}
		fmt.Sscanf(fields[0], "%d", &p.ID)
		if colon := strings.LastIndex(fields[1], ":"); colon != -1 {
			p.IP = fields[1][:colon]
			fmt.Sscanf(fields[1][colon+1:], "%d", &p.Port)
		}
		fmt.Sscanf(fields[2], "%d", &p.Ping)
		p.GUID = strings.TrimSuffix(strings.TrimSuffix(fields[3], "(OK)"), "(?)")
		name := strings.Join(fields[4:], " ")
		if strings.HasSuffix(name, "(Lobby)") {
			p.Lobby = true
			name = strings.TrimSpace(strings.TrimSuffix(name, "(Lobby)"))
		}
		p.Name = name
		out = append(out, p)
	}
	return out
}
