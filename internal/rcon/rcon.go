// Copyright (c) 2026 Aristarh Ucolov.
//
// BattlEye RCon protocol client (UDP). Pure stdlib.
//
// Protocol recap (from the public BE RCon spec):
//
//   header           = 'B' 'E' crc32(rest) 0xFF
//   login request    = header 0x00 password
//   login response   = header 0x00 0x01(ok)/0x00(fail)
//   command request  = header 0x01 seq cmd
//   command response = header 0x01 seq payload            (single-packet)
//                    | header 0x01 seq 0x00 count idx payload   (multi-packet)
//   server message   = header 0x02 seq payload            (keep-alive + logs)
//   keep-alive       = header 0x01 seq                     (empty command)
//
// The CRC is little-endian CRC-32/IEEE of everything after the 4-byte CRC
// slot (i.e., starting at 0xFF). The only non-obvious thing: BE expects a
// bitwise-NOT of the IEEE CRC in little-endian — actually it's just the
// standard CRC-32, which is what hash/crc32.ChecksumIEEE returns.
package rcon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrAuthFailed   = errors.New("rcon: authentication failed")
	ErrTimeout      = errors.New("rcon: timeout")
	ErrNotConnected = errors.New("rcon: not connected")
)

const (
	keepAliveInterval = 30 * time.Second
	readTimeout       = 5 * time.Second
)

// Conn is a live BattlEye RCon connection.
type Conn struct {
	addr string
	pass string
	udp  *net.UDPConn

	mu       sync.Mutex
	seq      uint32
	pending  map[byte]*pending
	closed   atomic.Bool
	readErr  atomic.Value // error

	// Multipart response assembly (very rare: >1 response per seq).
	parts map[byte]map[byte][]byte // seq → partIdx → payload

	// Server message callback (logs, chat). Called from the reader goroutine
	// so it must not block.
	OnMessage func(string)
}

type pending struct {
	done chan []byte
}

// Dial connects to addr (host:port), logs in, and starts the keep-alive
// and reader goroutines.
func Dial(addr, password string) (*Conn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	u, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		addr:    addr,
		pass:    password,
		udp:     u,
		pending: map[byte]*pending{},
		parts:   map[byte]map[byte][]byte{},
	}

	// Login.
	if err := c.sendFrame(append([]byte{0x00}, []byte(password)...)); err != nil {
		_ = u.Close()
		return nil, err
	}
	_ = u.SetReadDeadline(time.Now().Add(readTimeout))
	buf := make([]byte, 4096)
	n, err := u.Read(buf)
	_ = u.SetReadDeadline(time.Time{})
	if err != nil {
		_ = u.Close()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, ErrTimeout
		}
		return nil, err
	}
	pkt, err := parseHeader(buf[:n])
	if err != nil {
		_ = u.Close()
		return nil, err
	}
	if len(pkt) < 2 || pkt[0] != 0x00 {
		_ = u.Close()
		return nil, fmt.Errorf("rcon: unexpected login reply: % x", pkt)
	}
	if pkt[1] != 0x01 {
		_ = u.Close()
		return nil, ErrAuthFailed
	}

	go c.readLoop()
	go c.keepAlive()
	return c, nil
}

// Command sends an RCon command and waits for the server's reply.
func (c *Conn) Command(cmd string) (string, error) {
	if c.closed.Load() {
		return "", ErrNotConnected
	}
	seq := byte(atomic.AddUint32(&c.seq, 1))
	p := &pending{done: make(chan []byte, 4)}
	c.mu.Lock()
	c.pending[seq] = p
	c.mu.Unlock()

	frame := append([]byte{0x01, seq}, []byte(cmd)...)
	if err := c.sendFrame(frame); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return "", err
	}

	select {
	case reply := <-p.done:
		return string(reply), nil
	case <-time.After(readTimeout):
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return "", ErrTimeout
	}
}

// Close tears down the connection.
func (c *Conn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := c.udp.Close()
	c.mu.Lock()
	for _, p := range c.pending {
		close(p.done)
	}
	c.pending = map[byte]*pending{}
	c.mu.Unlock()
	return err
}

// ---------------------------------------------------------------------------

func (c *Conn) sendFrame(payload []byte) error {
	body := append([]byte{0xFF}, payload...)
	crc := crc32.ChecksumIEEE(body)
	out := make([]byte, 0, 6+len(payload)+1)
	out = append(out, 'B', 'E')
	out = binary.LittleEndian.AppendUint32(out, crc)
	out = append(out, body...)
	_, err := c.udp.Write(out)
	return err
}

// parseHeader validates and strips the BE header, returning the payload
// (everything after the 0xFF byte).
func parseHeader(b []byte) ([]byte, error) {
	if len(b) < 7 || b[0] != 'B' || b[1] != 'E' {
		return nil, fmt.Errorf("rcon: bad header")
	}
	gotCRC := binary.LittleEndian.Uint32(b[2:6])
	body := b[6:]
	if len(body) < 1 || body[0] != 0xFF {
		return nil, fmt.Errorf("rcon: missing 0xFF")
	}
	if crc32.ChecksumIEEE(body) != gotCRC {
		return nil, fmt.Errorf("rcon: bad crc")
	}
	return body[1:], nil
}

func (c *Conn) readLoop() {
	buf := make([]byte, 8192)
	for {
		if c.closed.Load() {
			return
		}
		n, err := c.udp.Read(buf)
		if err != nil {
			c.readErr.Store(err)
			return
		}
		pkt, err := parseHeader(buf[:n])
		if err != nil || len(pkt) < 1 {
			continue
		}
		switch pkt[0] {
		case 0x01:
			c.handleCommandReply(pkt[1:])
		case 0x02:
			// Server message — ack + forward.
			if len(pkt) >= 2 {
				seq := pkt[1]
				_ = c.sendFrame([]byte{0x02, seq})
				if c.OnMessage != nil {
					c.OnMessage(string(pkt[2:]))
				}
			}
		}
	}
}

func (c *Conn) handleCommandReply(b []byte) {
	if len(b) < 1 {
		return
	}
	seq := b[0]
	rest := b[1:]
	// Multipart: rest[0] == 0x00, rest[1] == total, rest[2] == index.
	if len(rest) >= 3 && rest[0] == 0x00 {
		total := rest[1]
		idx := rest[2]
		payload := rest[3:]
		c.mu.Lock()
		m, ok := c.parts[seq]
		if !ok {
			m = map[byte][]byte{}
			c.parts[seq] = m
		}
		m[idx] = payload
		if byte(len(m)) == total {
			var full []byte
			for i := byte(0); i < total; i++ {
				full = append(full, m[i]...)
			}
			delete(c.parts, seq)
			p := c.pending[seq]
			delete(c.pending, seq)
			c.mu.Unlock()
			if p != nil {
				p.done <- full
			}
			return
		}
		c.mu.Unlock()
		return
	}
	// Single-packet reply.
	c.mu.Lock()
	p := c.pending[seq]
	delete(c.pending, seq)
	c.mu.Unlock()
	if p != nil {
		p.done <- rest
	}
}

func (c *Conn) keepAlive() {
	t := time.NewTicker(keepAliveInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if c.closed.Load() {
				return
			}
			seq := byte(atomic.AddUint32(&c.seq, 1))
			_ = c.sendFrame([]byte{0x01, seq})
		}
	}
}
