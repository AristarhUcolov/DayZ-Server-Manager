// Copyright (c) 2026 Aristarh Ucolov.
//
// Package upnp is a minimal UPnP Internet Gateway Device client: find the
// home router (SSDP), read its WAN address and add/remove port forwards
// (WANIPConnection / WANPPPConnection SOAP). It is what lets "open the panel
// from work" and "make the server visible in the browser" work without the
// admin logging into the router — when the router allows UPnP.
package upnp

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Gateway is a discovered router service.
type Gateway struct {
	ControlURL  string // absolute SOAP endpoint
	ServiceType string // urn:schemas-upnp-org:service:WANIPConnection:1 (or :2 / WANPPPConnection:1)
	LocalIP     string // this machine's address on the router's network
	Name        string // friendly name of the device, when given
}

var searchTargets = []string{
	"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
	"urn:schemas-upnp-org:device:InternetGatewayDevice:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// ErrNoGateway means no UPnP router answered (UPnP off in the router, or not
// supported).
var ErrNoGateway = errors.New("no UPnP router found (UPnP may be disabled in the router)")

// Discover finds the first usable gateway on any local IPv4 interface.
func Discover(ctx context.Context, wait time.Duration) (*Gateway, error) {
	locations := map[string]bool{}
	var order []string
	ssdp := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	// Send from every IPv4 interface address: with VPN or Hyper-V adapters a
	// single unbound socket may multicast on the wrong interface.
	var conns []net.PacketConn
	for _, ip := range localIPv4s() {
		if c, err := net.ListenPacket("udp4", ip+":0"); err == nil {
			conns = append(conns, c)
		}
	}
	if len(conns) == 0 {
		if c, err := net.ListenPacket("udp4", ":0"); err == nil {
			conns = append(conns, c)
		}
	}
	if len(conns) == 0 {
		return nil, ErrNoGateway
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	type hit struct{ loc string }
	hits := make(chan hit, 64)
	done := make(chan struct{})
	for _, c := range conns {
		for _, st := range searchTargets {
			msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: " + st +
				"\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\n\r\n"
			_, _ = c.WriteTo([]byte(msg), ssdp)
		}
		_ = c.SetReadDeadline(deadline)
		go func(c net.PacketConn) {
			buf := make([]byte, 2048)
			for {
				n, _, err := c.ReadFrom(buf)
				if err != nil {
					return
				}
				for _, line := range strings.Split(string(buf[:n]), "\r\n") {
					if i := strings.IndexByte(line, ':'); i > 0 && strings.EqualFold(strings.TrimSpace(line[:i]), "location") {
						select {
						case hits <- hit{strings.TrimSpace(line[i+1:])}:
						case <-done:
							return
						}
					}
				}
			}
		}(c)
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
collect:
	for {
		select {
		case h := <-hits:
			if !locations[h.loc] {
				locations[h.loc] = true
				order = append(order, h.loc)
			}
		case <-timer.C:
			break collect
		case <-ctx.Done():
			break collect
		}
	}
	close(done)
	var lastErr error = ErrNoGateway
	for _, loc := range order {
		gw, err := fromDescription(ctx, loc)
		if err == nil {
			return gw, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func localIPv4s() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					out = append(out, ip4.String())
				}
			}
		}
	}
	return out
}

// device description XML (only what is needed).
type xmlDevice struct {
	FriendlyName string       `xml:"friendlyName"`
	Services     []xmlService `xml:"serviceList>service"`
	Devices      []xmlDevice  `xml:"deviceList>device"`
}

type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

// client talks to the router only: no redirects (a reply could otherwise
// bounce a request to any address), bounded time.
var client = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// isOwnAddress reports whether ip belongs to this computer.
func isOwnAddress(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

func fromDescription(ctx context.Context, loc string) (*Gateway, error) {
	u, err := url.Parse(loc)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("bad gateway location %q", loc)
	}
	// Only accept a router on the local network — never fetch an arbitrary
	// address a spoofed SSDP reply could point at.
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !(ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return nil, fmt.Errorf("gateway %q is not on the local network", u.Hostname())
	}
	if isOwnAddress(ip) {
		return nil, fmt.Errorf("gateway %q is this computer", u.Hostname())
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, loc, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var root xmlRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return nil, fmt.Errorf("gateway description: %w", err)
	}
	base := u
	if root.URLBase != "" {
		if b, err := url.Parse(root.URLBase); err == nil && b.Host != "" {
			base = b
		}
	}
	svc, ok := findWANService(root.Device)
	if !ok {
		return nil, errors.New("the router has no WAN connection service")
	}
	ctrl, err := base.Parse(svc.ControlURL)
	if err != nil {
		return nil, err
	}
	// The SOAP endpoint must be the router that answered — a description
	// pointing it elsewhere (this computer's own panel, say) is refused.
	if ctrl.Scheme != "http" && ctrl.Scheme != "https" || !strings.EqualFold(ctrl.Hostname(), u.Hostname()) {
		return nil, fmt.Errorf("router control address %q is not the router itself", ctrl.Host)
	}
	gw := &Gateway{ControlURL: ctrl.String(), ServiceType: svc.ServiceType, Name: root.Device.FriendlyName}
	// The local address the router sees: the source address of a route to it.
	if c, err := net.Dial("udp4", net.JoinHostPort(u.Hostname(), "1900")); err == nil {
		gw.LocalIP = c.LocalAddr().(*net.UDPAddr).IP.String()
		_ = c.Close()
	}
	return gw, nil
}

func findWANService(d xmlDevice) (xmlService, bool) {
	// Prefer WANIPConnection over WANPPPConnection; IP v2 over v1.
	var best xmlService
	rank := func(t string) int {
		switch {
		case strings.Contains(t, "WANIPConnection:2"):
			return 3
		case strings.Contains(t, "WANIPConnection:1"):
			return 2
		case strings.Contains(t, "WANPPPConnection:1"):
			return 1
		}
		return 0
	}
	var walk func(xmlDevice)
	walk = func(d xmlDevice) {
		for _, s := range d.Services {
			if rank(s.ServiceType) > rank(best.ServiceType) && s.ControlURL != "" {
				best = s
			}
		}
		for _, sub := range d.Devices {
			walk(sub)
		}
	}
	walk(d)
	return best, rank(best.ServiceType) > 0
}

// SOAPError is a UPnP error reply (e.g. 718 ConflictInMappingEntry).
type SOAPError struct {
	Code        int
	Description string
}

func (e *SOAPError) Error() string {
	return fmt.Sprintf("router refused (UPnP error %d: %s)", e.Code, e.Description)
}

func (g *Gateway) soap(ctx context.Context, action string, args [][2]string) (map[string]string, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	b.WriteString(`<u:` + action + ` xmlns:u="` + g.ServiceType + `">`)
	for _, a := range args {
		b.WriteString("<" + a[0] + ">")
		_ = xml.EscapeText(&b, []byte(a[1]))
		b.WriteString("</" + a[0] + ">")
	}
	b.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, g.ControlURL, strings.NewReader(b.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+g.ServiceType+"#"+action+`"`)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	vals := leafValues(body)
	if resp.StatusCode != http.StatusOK {
		code, _ := strconv.Atoi(vals["errorCode"])
		desc := vals["errorDescription"]
		if desc == "" {
			desc = resp.Status
		}
		return nil, &SOAPError{Code: code, Description: desc}
	}
	return vals, nil
}

// leafValues collects element-name → text for every leaf element (namespaces
// dropped) — the replies are flat enough for that.
func leafValues(body []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(body))
	var stack []string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if len(stack) > 0 {
				name := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if _, seen := out[name]; !seen {
					out[name] = strings.TrimSpace(text.String())
				}
			}
			text.Reset()
		}
	}
	return out
}

// ExternalIP is the router's WAN address.
func (g *Gateway) ExternalIP(ctx context.Context) (string, error) {
	v, err := g.soap(ctx, "GetExternalIPAddress", nil)
	if err != nil {
		return "", err
	}
	ip := v["NewExternalIPAddress"]
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("router reported no external address")
	}
	return ip, nil
}

// AddPortMapping forwards external port → this machine's same port. A
// permanent lease is asked first; routers that only take timed leases get one
// hour (the caller renews well before that).
func (g *Gateway) AddPortMapping(ctx context.Context, proto string, port int, desc string) error {
	add := func(lease int) error {
		_, err := g.soap(ctx, "AddPortMapping", [][2]string{
			{"NewRemoteHost", ""},
			{"NewExternalPort", strconv.Itoa(port)},
			{"NewProtocol", proto},
			{"NewInternalPort", strconv.Itoa(port)},
			{"NewInternalClient", g.LocalIP},
			{"NewEnabled", "1"},
			{"NewPortMappingDescription", desc},
			{"NewLeaseDuration", strconv.Itoa(lease)},
		})
		return err
	}
	err := add(0)
	var se *SOAPError
	if errors.As(err, &se) && (se.Code == 725 || se.Code == 402 || se.Code == 501) {
		err = add(3600)
	}
	return err
}

// DeletePortMapping removes a forward added earlier.
func (g *Gateway) DeletePortMapping(ctx context.Context, proto string, port int) error {
	_, err := g.soap(ctx, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(port)},
		{"NewProtocol", proto},
	})
	return err
}

// MappedTo reports which internal client an external port currently forwards
// to ("" when there is no such mapping).
func (g *Gateway) MappedTo(ctx context.Context, proto string, port int) (string, error) {
	client, _, err := g.MappingInfo(ctx, proto, port)
	return client, err
}

// MappingInfo reports the internal client an external port forwards to and
// the forward's description ("", "" when there is no such mapping). Some
// routers leave the description empty.
func (g *Gateway) MappingInfo(ctx context.Context, proto string, port int) (client, desc string, err error) {
	v, err := g.soap(ctx, "GetSpecificPortMappingEntry", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(port)},
		{"NewProtocol", proto},
	})
	var se *SOAPError
	if errors.As(err, &se) && se.Code == 714 { // NoSuchEntryInArray
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return v["NewInternalClient"], v["NewPortMappingDescription"], nil
}

// IsPublic reports whether ip is a public internet address (not private,
// CGNAT, loopback or link-local). A router whose WAN address is not public
// sits behind another NAT — usually the provider's CGNAT — and port forwards
// on it cannot make anything reachable from the internet.
func IsPublic(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil || p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() || p.IsUnspecified() {
		return false
	}
	cgnat := &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	return !cgnat.Contains(p)
}
