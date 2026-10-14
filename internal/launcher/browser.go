// Copyright (c) 2026 Aristarh Ucolov.

package launcher

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// launchWait is how long a freshly started browser may take to fail. A
// browser that is still running after it — or that exited cleanly, having
// handed the address to an already open window — counts as opened.
const launchWait = 4 * time.Second

var (
	logMu   sync.Mutex
	logHook func(format string, a ...interface{})
)

// logf writes to the manager log once one exists (Start installs it).
func logf(format string, a ...interface{}) {
	logMu.Lock()
	f := logHook
	logMu.Unlock()
	if f != nil {
		f(format, a...)
	}
}

func setLogHook(f func(format string, a ...interface{})) {
	logMu.Lock()
	logHook = f
	logMu.Unlock()
}

// tailBuffer keeps the last bytes written to it (a child's stderr), so a
// browser that refuses to start can say why in the log.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 4096 {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-2048:]...)
		t.buf.Reset()
		t.buf.Write(keep)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimSpace(t.buf.String())
	if i := strings.LastIndexByte(s, '\n'); i >= 0 && len(s)-i < 300 {
		// the last line is usually the reason; keep a little context
		if j := strings.LastIndexByte(s[:i], '\n'); j >= 0 {
			s = s[j+1:]
		}
	}
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}

// Launched describes a program right after it was started.
type Launched struct {
	// Done receives the exit status once the program ends (nil when it was
	// handed off: it already exited cleanly).
	Done <-chan error
	// HandedOff: it exited successfully at once — typically a browser that
	// passed the address to an instance that was already open.
	HandedOff bool
}

// Launch starts cmd and waits up to wait for it to fail. A program that is
// still running then is launched; one that exited cleanly was handed off; one
// that exited with an error failed, and the error carries the end of its
// error output. The process is always reaped.
func Launch(cmd *exec.Cmd, wait time.Duration) (Launched, error) {
	var errOut tailBuffer
	if cmd.Stderr == nil {
		cmd.Stderr = &errOut
	}
	if err := cmd.Start(); err != nil {
		return Launched{}, fmt.Errorf("%s: %w", filepath.Base(cmd.Path), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return Launched{HandedOff: true}, nil
		}
		if msg := errOut.String(); msg != "" {
			return Launched{}, fmt.Errorf("%s: %v: %s", filepath.Base(cmd.Path), err, msg)
		}
		return Launched{}, fmt.Errorf("%s: %v", filepath.Base(cmd.Path), err)
	case <-time.After(wait):
		return Launched{Done: done}, nil
	}
}

// TryStart is Launch for programs nobody waits on.
func TryStart(cmd *exec.Cmd, wait time.Duration) error {
	_, err := Launch(cmd, wait)
	return err
}

// OpenBrowser opens url in a browser of this computer's desktop and reports
// whether one started. On Linux it handles root/sudo sessions and missing
// session variables, and falls back from xdg-open to common browsers.
func OpenBrowser(url string) bool { return openDesktop(url) }
