// Copyright (c) 2026 Aristarh Ucolov.
package launcher

// The manager's own run: a marker with a heartbeat, so a run that did not end
// normally is explained on the next start (a crash — with what Go printed —
// or the process being ended from outside); the time the computer slept; and
// keeping the computer awake while a DayZ server runs.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/util"
)

const (
	runMarkerFile = "run.json"
	crashLogFile  = "crash.log"
	beatEvery     = time.Minute
	// A heartbeat this late means the process was frozen: the computer slept
	// or hibernated.
	sleepGap = 3 * beatEvery
	// crash.log is cut back to its tail when it grows past this.
	crashLogMax = 1 << 20
)

// runState is the marker file of a running manager. It is removed on a normal
// exit, so finding one at start means the last run ended some other way.
type runState struct {
	PID      int       `json:"pid"`
	Version  string    `json:"version"`
	Started  time.Time `json:"started"`
	LastSeen time.Time `json:"lastSeen"`
	// SessionEnd: Windows told the window the session was ending (sign-out,
	// shutdown) — the end that followed is no mystery.
	SessionEnd bool `json:"sessionEnd,omitempty"`
}

type lifecycle struct {
	mu        sync.Mutex
	path      string
	st        runState
	awake     bool
	crashFile *os.File
	exiting   bool // an exit was asked for: the marker is gone for good
}

// startLifecycle runs once the instance exists.
func (i *Instance) startLifecycle(version string) {
	dir := i.App.ManagerDir
	lc := &lifecycle{path: filepath.Join(dir, runMarkerFile)}
	i.life = lc

	crashPath := filepath.Join(dir, crashLogFile)
	if prev, ok := readRunState(lc.path); ok && !prev.SessionEnd {
		info := &app.ExitInfo{Kind: "killed", Started: prev.Started, LastSeen: prev.LastSeen,
			AutoRestarted: restartedByGuardian(dir, time.Now())}
		if text := lastCrash(crashPath); text != "" {
			info.Kind, info.Crash = "crash", text
		}
		// What Go could not print (a failure inside a Windows component)
		// Windows records itself.
		if fault := windowsFault(prev.PID, prev.LastSeen); fault != "" {
			info.Kind, info.Fault = "crash", fault
		}
		i.App.LastExit = info
		i.App.Log.Printf("the previous run (started %s) did not exit normally — %s; last seen %s",
			prev.Started.Format(time.DateTime), info.Kind, prev.LastSeen.Format(time.DateTime))
		if info.Fault != "" {
			i.App.Log.Printf("Windows recorded the crash: %s", info.Fault)
		}
	}

	// Go prints a crash (an unrecovered panic, a fatal runtime error) to
	// stderr, which the windowed build does not have: keep it in a file.
	trimTail(crashPath, crashLogMax)
	if f, err := os.OpenFile(crashPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "\n===== %s v%s started %s (pid %d) =====\n", AppName, version, time.Now().Format(time.RFC3339), os.Getpid())
		if debug.SetCrashOutput(f, debug.CrashOptions{}) == nil {
			lc.crashFile = f // stays open: the runtime writes to it on a crash
		} else {
			f.Close()
		}
	}

	now := time.Now()
	lc.st = runState{PID: os.Getpid(), Version: version, Started: now, LastSeen: now}
	lc.write()
	i.App.KeepingAwake = func() bool {
		lc.mu.Lock()
		defer lc.mu.Unlock()
		return lc.awake
	}
	go util.SafeLoop(i.App.Log, "heartbeat", func() { i.heartbeat(lc) })
	go util.SafeLoop(i.App.Log, "keep awake", func() { i.keepAwakeLoop(lc) })
}

// endLifecycle marks a normal exit.
func (i *Instance) endLifecycle() {
	if i.life == nil {
		return
	}
	i.life.mu.Lock()
	if i.life.awake {
		util.SetKeepAwake(false)
		i.life.awake = false
	}
	i.life.mu.Unlock()
	_ = os.Remove(i.life.path)
}

// markExiting: an exit was asked for (the Exit button, the tray, closing the
// window or the console). The run marker goes at once, so the guardian does
// not start the manager again even when the shutdown is cut short (Windows
// ends a closed console window's process after a few seconds).
func (i *Instance) markExiting() {
	if i.life == nil {
		return
	}
	i.life.mu.Lock()
	i.life.exiting = true
	i.life.mu.Unlock()
	_ = os.Remove(i.life.path)
}

// MarkSessionEnd records that Windows is ending the session (sign-out or
// shutdown): the process is about to be ended from outside, which the next
// start must not report as a problem.
func (i *Instance) MarkSessionEnd() {
	if i.life == nil {
		return
	}
	i.life.mu.Lock()
	i.life.st.SessionEnd = true
	i.life.mu.Unlock()
	i.life.write()
}

func (i *Instance) heartbeat(lc *lifecycle) {
	t := time.NewTicker(beatEvery)
	defer t.Stop()
	for {
		select {
		case <-i.ctx.Done():
			return
		case now := <-t.C:
			lc.mu.Lock()
			last := lc.st.LastSeen
			lc.st.LastSeen = time.Now()
			lc.mu.Unlock()
			if gap := now.Sub(last); gap > sleepGap {
				// Frozen for a while: the computer slept. Everything —
				// the DayZ server, the panel, remote access — was down.
				i.App.Log.Printf("the computer was asleep from %s to %s", last.Format(time.DateTime), now.Format(time.DateTime))
				for _, a := range i.apps() {
					a.NoteSleep(last, now)
				}
			}
			lc.write()
		}
	}
}

// keepAwakeLoop holds the computer awake while the setting asks for it:
// while a DayZ server runs (the default), always, or never.
func (i *Instance) keepAwakeLoop(lc *lifecycle) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		mode := i.App.Cfg().KeepAwake
		want := mode == "always" || (mode == "" && i.AnyServerRunning())
		lc.mu.Lock()
		changed := want != lc.awake
		lc.awake = want
		lc.mu.Unlock()
		if changed {
			util.SetKeepAwake(want)
			if want {
				i.App.Log.Printf("keeping the computer awake (no sleep while %s)", map[bool]string{true: "the manager runs", false: "a DayZ server runs"}[mode == "always"])
			} else {
				i.App.Log.Printf("the computer may sleep again")
			}
		}
		select {
		case <-i.ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (i *Instance) apps() []*app.App {
	if i.Fleet != nil {
		return i.Fleet.Apps()
	}
	return []*app.App{i.App}
}

func (lc *lifecycle) write() {
	lc.mu.Lock()
	if lc.exiting {
		lc.mu.Unlock()
		return
	}
	data, err := json.MarshalIndent(lc.st, "", " ")
	lc.mu.Unlock()
	if err != nil {
		return
	}
	tmp := lc.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, lc.path)
	}
}

func readRunState(path string) (runState, bool) {
	var st runState
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &st) != nil || st.Started.IsZero() {
		return st, false
	}
	return st, true
}

// lastCrash returns what the runtime printed after the newest run header in
// crash.log — the previous run's crash — or "" when that run printed nothing.
func lastCrash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(data)
	i := strings.LastIndex(s, "\n===== ")
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return ""
	}
	text := strings.TrimSpace(rest[nl+1:])
	const max = 16 << 10
	if len(text) > max {
		text = text[:max] + "\n…"
	}
	return text
}

// trimTail keeps the last half of a file that grew past max.
func trimTail(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= max {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	data = data[len(data)-int(max/2):]
	if i := strings.Index(string(data), "\n===== "); i >= 0 {
		data = data[i:]
	}
	_ = os.WriteFile(path, data, 0o644)
}
