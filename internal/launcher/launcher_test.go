// Copyright (c) 2026 Aristarh Ucolov.

package launcher

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// -bind values must reach net.Listen as a host JoinHostPort can bracket:
// "::" used to become ":::8787" and fail as "no free port".
func TestNormalizeBind(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0": "", "::": "", "[::]": "", "[::1]": "::1", "::1": "::1",
		"127.0.0.1": "127.0.0.1", " 192.168.1.5 ": "192.168.1.5",
	} {
		if got := normalizeBind(in); got != want {
			t.Errorf("normalizeBind(%q) = %q, want %q", in, got, want)
		}
	}
	if got := "http://" + net.JoinHostPort(DisplayHost(normalizeBind("::1")), "8787") + "/"; got != "http://[::1]:8787/" {
		t.Errorf("IPv6 panel URL = %q", got)
	}
}

// The recovery flags refuse to run under a live manager: RunningURL must see
// one that answers for this server, and nothing else.
func TestRunningURL(t *testing.T) {
	dir, err := ServerDir()
	if err != nil {
		t.Fatal(err)
	}
	// One fake server at a time: RunningURL also scans the next 20 ports.
	check := func(name, serverDir string) (string, bool) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "serverDir": serverDir})
		}))
		defer srv.Close()
		_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
		port, _ := strconv.Atoi(p)
		url, ok := RunningURL(port)
		// a manager that fell back to a later port is found from the first one
		if url2, ok2 := RunningURL(port - 5); ok2 != ok || url2 != url {
			t.Errorf("%s: scanning from port-5 gave %q %v, from the port %q %v", name, url2, ok2, url, ok)
		}
		return url, ok
	}
	if url, ok := check(AppName, dir); !ok || url == "" {
		t.Fatalf("a manager for this server must be found, got %q %v", url, ok)
	}
	if _, ok := check(AppName, dir+"-other"); ok {
		t.Fatal("a manager for another server is not this one")
	}
	if _, ok := check("something else", dir); ok {
		t.Fatal("another program on the port is not a manager")
	}
}

// The ssh hint names the address the admin's SSH login reached, else a public
// one — never a container bridge sorted first.
func TestSSHHostAndVirtualIfaces(t *testing.T) {
	if got := sshHost("203.0.113.9 50022 198.51.100.4 22", []string{"10.0.0.5"}); got != "198.51.100.4" {
		t.Errorf("from SSH_CONNECTION: %q", got)
	}
	if got := sshHost("", []string{"10.0.0.5", "198.51.100.4"}); got != "198.51.100.4" {
		t.Errorf("public first: %q", got)
	}
	if got := sshHost("::1 5000 ::1 22", []string{"192.168.1.20"}); got != "192.168.1.20" {
		t.Errorf("loopback SSH falls back: %q", got)
	}
	if got := sshHost("", nil); got != "" {
		t.Errorf("no addresses: %q", got)
	}
	for _, n := range []string{"docker0", "br-1a2b3c", "veth12ab", "virbr0", "lxcbr0", "cni0", "flannel.1"} {
		if !virtualIface(n) {
			t.Errorf("%s is a virtual bridge", n)
		}
	}
	for _, n := range []string{"eth0", "ens3", "enp0s31f6", "wlan0", "Ethernet"} {
		if virtualIface(n) {
			t.Errorf("%s is a real interface", n)
		}
	}
}
