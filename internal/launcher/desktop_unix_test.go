// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package launcher

import "testing"

// sudo drops SSH_CONNECTION, so it is read from the parents' /proc entries.
func TestProcParsers(t *testing.T) {
	env := []byte("USER=admin\x00SSH_CONNECTION=203.0.113.9 50022 198.51.100.4 22\x00TERM=xterm\x00")
	if got := environValue(env, "SSH_CONNECTION"); got != "203.0.113.9 50022 198.51.100.4 22" {
		t.Errorf("environValue = %q", got)
	}
	if got := environValue(env, "SSH"); got != "" {
		t.Errorf("a key prefix must not match: %q", got)
	}
	status := []byte("Name:\tsudo\nUmask:\t0022\nState:\tS (sleeping)\nTgid:\t4242\nPid:\t4242\nPPid:\t4100\nTracerPid:\t0\n")
	if got := statusPPid(status); got != 4100 {
		t.Errorf("statusPPid = %d", got)
	}
	if got := statusPPid([]byte("Name:\tx\n")); got != 0 {
		t.Errorf("statusPPid without PPid = %d", got)
	}
}
