// Copyright (c) 2026 Aristarh Ucolov.
//
// Optional HTTPS for the panel (Settings → Remote access). The same port
// serves both protocols: the first byte of each connection decides — a TLS
// handshake starts with 0x16, plain HTTP with a letter — so http://127.0.0.1
// keeps working for the desktop window while phones use https://.
//
// Certificate: .dayz-manager/tls/cert.pem + key.pem. If they are missing (or
// expired) a self-signed one is generated for this machine's names and
// addresses; browsers warn once about it. Admins with a real certificate
// (e.g. Let's Encrypt for their DDNS name) simply replace the two files.
package web

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func (s *Server) panelTLSConfig() (*tls.Config, error) {
	dir := filepath.Join(s.app.ManagerDir, "tls")
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err == nil && !isOwnCert(leaf) {
			// The admin's own certificate (from a real CA): never replaced —
			// only reported when it runs out.
			if time.Now().After(leaf.NotAfter) {
				s.app.Log.Printf("WARNING: the panel certificate in %s expired on %s — replace cert.pem/key.pem", dir, leaf.NotAfter.Format("2006-01-02"))
			}
			return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
		}
		// Ours is renewed a month before it expires, and when it is valid
		// longer than Apple accepts (825 days — earlier builds made 5-year
		// certificates that iPhones and Safari refuse outright).
		if err == nil && time.Now().Add(30*24*time.Hour).Before(leaf.NotAfter) &&
			leaf.NotAfter.Sub(leaf.NotBefore) <= 825*24*time.Hour {
			return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
		}
		s.app.Log.Printf("panel certificate expired or about to — generating a new self-signed one")
	}
	if err := generateSelfSigned(certPath, keyPath); err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	s.app.Log.Printf("generated a self-signed panel certificate in %s", dir)
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// ownCertOrg marks the certificates this manager generates.
const ownCertOrg = "DayZ Server Manager (self-signed)"

func isOwnCert(c *x509.Certificate) bool {
	for _, o := range c.Subject.Organization {
		if o == ownCertOrg {
			return true
		}
	}
	return false
}

func generateSelfSigned(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "DayZ Server Manager", Organization: []string{ownCertOrg}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(397 * 24 * time.Hour), // Apple's limit for TLS server certificates is 825 days
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}
	if host != "" {
		tpl.DNSNames = append(tpl.DNSNames, host)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				tpl.IPAddresses = append(tpl.IPAddresses, n.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	if err := writePrivateFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// sniffListener hands out plain or TLS connections depending on the first
// byte the client sends. Classification runs per connection in its own
// goroutine so a silent client cannot stall Accept for everyone else.
type sniffListener struct {
	net.Listener
	conf  *tls.Config
	conns chan net.Conn
	errs  chan error
	done  chan struct{}
	once  sync.Once
}

func newSniffListener(ln net.Listener, conf *tls.Config) net.Listener {
	s := &sniffListener{Listener: ln, conf: conf, conns: make(chan net.Conn), errs: make(chan error, 1), done: make(chan struct{})}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case s.errs <- fmt.Errorf("accept loop panicked: %v", r):
				default:
				}
			}
		}()
		s.acceptLoop()
	}()
	return s
}

func (s *sniffListener) acceptLoop() {
	var delay time.Duration
	for {
		c, err := s.Listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				select {
				case s.errs <- err:
				case <-s.done:
				}
				return
			}
			// Anything else (too many open files, an aborted handshake) is
			// transient: back off and keep accepting, as http.Server does.
			// Returning here used to leave the panel deaf until a restart.
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			select {
			case <-time.After(delay):
			case <-s.done:
				return
			}
			continue
		}
		delay = 0
		go func() {
			defer func() {
				if r := recover(); r != nil {
					_ = c.Close()
				}
			}()
			s.classify(c)
		}()
	}
}

func (s *sniffListener) classify(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	b, err := br.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		_ = c.Close()
		return
	}
	var out net.Conn = &peekedConn{Conn: c, r: br}
	if b[0] == 0x16 { // TLS handshake record
		out = tls.Server(out, s.conf)
	}
	select {
	case s.conns <- out:
	case <-s.done:
		_ = out.Close()
	}
}

func (s *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-s.conns:
		return c, nil
	case err := <-s.errs:
		return nil, err
	case <-s.done:
		return nil, net.ErrClosed
	}
}

func (s *sniffListener) Close() error {
	s.once.Do(func() { close(s.done) })
	return s.Listener.Close()
}

// peekedConn replays the byte(s) consumed by the sniff.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.r.Read(b) }
