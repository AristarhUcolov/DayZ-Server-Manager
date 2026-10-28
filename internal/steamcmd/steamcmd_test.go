// Copyright (c) 2026 Aristarh Ucolov.
package steamcmd

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogin(t *testing.T) {
	cases := []struct {
		name, out string
		ok        bool
		need, why string
		mobile    bool
	}{
		{"ok", "Logging in user 'bob' [U:1:12345] to Steam Public...OK\nWaiting for client config...OK\nWaiting for user info...OK\n", true, "", "", false},
		{"password", "Logging in user 'bob' to Steam Public...FAILED (Invalid Password)\n", false, "", "password", false},
		{"guard", "Logging in user 'bob' to Steam Public...\nThis computer has not been authenticated for your account using Steam Guard.\nPlease check your email for the message from Steam, and enter the Steam Guard\n code from that message.\nSteam Guard code:FAILED (Account Logon Denied)\n", false, "guard", "", false},
		{"2fa", "Logging in user 'bob' to Steam Public...\nEnter the current code from your Steam Guard Mobile Authenticator app\nTwo-factor code:FAILED (Account Login Denied Need Two Factor)\n", false, "2fa", "", false},
		{"bad 2fa code", "Logging in user 'bob' to Steam Public...FAILED (Two-factor code mismatch)\n", false, "2fa", "badcode", false},
		{"bad mail code", "Logging in user 'bob' to Steam Public...FAILED (Invalid Login Auth Code)\n", false, "guard", "badcode", false},
		{"mobile ok", "Logging in user 'bob' [U:1:1] to Steam Public...\nPlease confirm the login in the Steam Mobile app on your phone.\n\nWaiting for confirmation...\nWaiting for confirmation...OK\nWaiting for user info...OK\n", true, "", "", true},
		{"rate limit", "Logging in user 'bob' to Steam Public...FAILED (Rate Limit Exceeded)\n", false, "", "ratelimit", false},
		{"no cache", "Logging in user 'bob' to Steam Public...\nCached credentials not found.\nFAILED (No cached credentials and @NoPromptForPassword is set)\n", false, "", "nocache", false},
	}
	for _, c := range cases {
		r := ParseLogin(c.out)
		if r.OK != c.ok || r.Need != c.need || r.Reason != c.why || r.Mobile != c.mobile {
			t.Errorf("%s: got %+v", c.name, r)
		}
	}
}

func TestParseDownloads(t *testing.T) {
	out := `Downloading item 1559212036 ...
Success. Downloaded item 1559212036 to "C:\steamcmd\steamapps\workshop\content\221100\1559212036" (412345678 bytes)
Downloading item 2116151222 ...
ERROR! Download item 2116151222 failed (Access Denied).
ERROR! Timeout downloading item 2545327648
`
	res := ParseDownloads(out, []string{"1559212036", "2116151222", "2545327648", "1"})
	if !res[0].OK || res[0].Bytes != 412345678 || res[0].Path == "" {
		t.Errorf("success: %+v", res[0])
	}
	if res[1].OK || res[1].Error != "Access Denied" {
		t.Errorf("failure: %+v", res[1])
	}
	if res[2].Error != "Timeout" || res[3].Error != "no result" {
		t.Errorf("timeout / missing: %+v %+v", res[2], res[3])
	}
}

// Archive entries cannot escape the install folder.
func TestUnzipStaysInside(t *testing.T) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("steamcmd.exe")
	w.Write([]byte("MZ"))
	w, _ = zw.Create("../evil.txt")
	w.Write([]byte("x"))
	zw.Close()
	dir := t.TempDir()
	if err := unzip(b.Bytes(), filepath.Join(dir, "steamcmd")); err == nil {
		t.Fatal("an entry outside the folder was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("evil.txt was written outside")
	}
}
