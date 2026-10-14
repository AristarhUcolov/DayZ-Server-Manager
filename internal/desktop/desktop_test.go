// Copyright (c) 2026 Aristarh Ucolov.

package desktop

import "testing"

// Snap browsers are symlinks to /usr/bin/snap: the snap's name must come from
// the unresolved path (before, resolving first lost it, so the confined
// browser got a profile folder it may not write to and every snap browser
// counted as the same program).
func TestSnapApp(t *testing.T) {
	cases := []struct{ path, real, want string }{
		{"/snap/bin/chromium", "/usr/bin/snap", "chromium"},
		{"/snap/bin/opera", "/usr/bin/snap", "opera"},
		{"/snap/bin/brave.browser", "/usr/bin/snap", "brave"},
		{"/usr/local/bin/chromium", "/usr/bin/snap", "chromium"},
		{"/snap/chromium/2890/usr/lib/chromium-browser/chrome", "/snap/chromium/2890/usr/lib/chromium-browser/chrome", "chromium"},
		{"/usr/bin/google-chrome", "/opt/google/chrome/google-chrome", ""},
		{"/usr/bin/snap", "/usr/bin/snap", ""},
		{`C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Program Files\Google\Chrome\Application\chrome.exe`, ""},
	}
	for _, c := range cases {
		if got := snapApp(c.path, c.real); got != c.want {
			t.Errorf("snapApp(%q, %q) = %q, want %q", c.path, c.real, got, c.want)
		}
	}
}
