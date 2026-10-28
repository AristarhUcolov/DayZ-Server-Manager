// Copyright (c) 2026 Aristarh Ucolov.
package launcher

// The guardian: a small second copy of the program, started with
// -guard <pid>, that starts the manager again when it ends in any way but a
// normal exit.
//
// A manager that is simply gone — no "shutting down" in its log, the DayZ
// server left running with no restarts, backups or panel — is what admins
// describe as "the manager closes by itself after a while". Whatever ended it
// (a crash, the process killed from outside), the guardian brings it back
// within seconds: in the tray (or without a browser tab), picking up the
// running DayZ server, with the reason on the Dashboard. A normal exit removes
// the run marker first (Instance.RequestExit), so closing the manager on
// purpose is never undone.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"dayzmanager/internal/util"
)

const (
	guardFile = "guardian.json"
	// At most this many restarts within guardWindow: a manager that cannot
	// stay up (a broken install) must not be started over and over.
	guardMaxRestarts = 3
	guardWindow      = 15 * time.Minute
	// A start this soon after a guardian restart is that restart.
	guardRecent = 2 * time.Minute
)

// guardState is the guardian's memory between runs.
type guardState struct {
	Restarts []time.Time `json:"restarts"`
}

// guardianSupported: the guardian waits on the manager's process, which is
// only implemented for Windows and Linux.
func guardianSupported() bool { return runtime.GOOS == "windows" || runtime.GOOS == "linux" }

// startGuardian starts the guardian for this run.
func (i *Instance) startGuardian(opts Options) {
	if !guardianSupported() || i.App.Cfg().NoGuardian {
		return
	}
	// Run as a systemd service, the service manager restarts it itself (the
	// unit's Restart=); a second one would only race it.
	if os.Getenv("INVOCATION_ID") != "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := []string{"-guard", strconv.Itoa(os.Getpid()), "-dir", i.App.ServerDir, "-port", strconv.Itoa(i.port)}
	if opts.Bind != "" {
		args = append(args, "-bind", opts.Bind)
	}
	if err := startDetached(exe, i.App.ServerDir, args); err != nil {
		i.App.Log.Printf("guardian: %v", err)
	}
}

// relaunch starts the manager again (a test replaces it).
var relaunch = startDetached

// guardPause lets the port and the files go before the new manager takes
// them (a test shortens it).
var guardPause = 3 * time.Second

// startDetached starts a process that outlives this one: no console of its
// own, not in this process's group or job.
func startDetached(exe, dir string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	detach(cmd, true)
	if err := cmd.Start(); err != nil {
		// Some job objects forbid breaking away: start inside it then.
		cmd = exec.Command(exe, args...)
		cmd.Dir = dir
		detach(cmd, false)
		if err2 := cmd.Start(); err2 != nil {
			return err2
		}
	}
	return cmd.Process.Release()
}

// RunGuardian is the -guard mode. It waits for the manager (pid) to end and,
// when that was not a normal exit, starts it again on the same port. The
// server folder is the one SetServerDir chose.
func RunGuardian(pid, port int, bind string) {
	serverDir, err := ServerDir()
	if err != nil {
		return
	}
	managerDir := filepath.Join(serverDir, ".dayz-manager")
	waitExit(pid)

	// A normal exit removed the marker; a newer run owns a marker with its
	// own pid; a sign-out or shutdown ends everything on purpose.
	st, ok := readRunState(filepath.Join(managerDir, runMarkerFile))
	if !ok || st.PID != pid || st.SessionEnd {
		return
	}
	if guardDisabled(managerDir) {
		return
	}
	now := time.Now()
	gs := readGuardState(managerDir)
	recent := gs.Restarts[:0]
	for _, t := range gs.Restarts {
		if now.Sub(t) < guardWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= guardMaxRestarts {
		guardLog(managerDir, "the manager (pid %d) stopped unexpectedly again — %d restarts within %d minutes, the guardian does not start it once more",
			pid, len(recent), int(guardWindow.Minutes()))
		return
	}
	time.Sleep(guardPause)
	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := []string{"-autostart", "-dir", serverDir, "-port", strconv.Itoa(port)}
	if bind != "" {
		args = append(args, "-bind", bind)
	}
	if err := relaunch(exe, serverDir, args); err != nil {
		guardLog(managerDir, "the manager (pid %d) stopped unexpectedly — the guardian could not start it again: %v", pid, err)
		return
	}
	gs.Restarts = append(recent, now)
	writeGuardState(managerDir, gs)
	guardLog(managerDir, "the manager (pid %d) stopped unexpectedly — the guardian started it again", pid)
}

// waitExit returns when the process has ended.
func waitExit(pid int) {
	if runtime.GOOS == "windows" {
		// A handle to the process: Wait cannot be fooled by the pid being
		// reused by another program afterwards.
		if p, err := os.FindProcess(pid); err == nil {
			if _, err := p.Wait(); err == nil {
				return
			}
		}
	}
	for util.ProcessAlive(pid) {
		time.Sleep(2 * time.Second)
	}
}

// guardDisabled reads the setting from manager.json as it is now — the admin
// may have turned the guardian off after this run started.
func guardDisabled(managerDir string) bool {
	data, err := os.ReadFile(filepath.Join(managerDir, "manager.json"))
	if err != nil {
		return false
	}
	var c struct {
		NoGuardian bool `json:"noGuardian"`
	}
	return json.Unmarshal(data, &c) == nil && c.NoGuardian
}

func readGuardState(managerDir string) guardState {
	var gs guardState
	if data, err := os.ReadFile(filepath.Join(managerDir, guardFile)); err == nil {
		_ = json.Unmarshal(data, &gs)
	}
	return gs
}

func writeGuardState(managerDir string, gs guardState) {
	if data, err := json.Marshal(gs); err == nil {
		_ = os.WriteFile(filepath.Join(managerDir, guardFile), data, 0o644)
	}
}

// restartedByGuardian reports whether this start is the guardian's restart.
func restartedByGuardian(managerDir string, now time.Time) bool {
	gs := readGuardState(managerDir)
	if n := len(gs.Restarts); n > 0 {
		d := now.Sub(gs.Restarts[n-1])
		return d >= 0 && d < guardRecent
	}
	return false
}

// guardLog adds one line to the manager's log, in its format.
func guardLog(managerDir, format string, a ...interface{}) {
	f, err := os.OpenFile(filepath.Join(managerDir, "manager.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006/01/02 15:04:05"), fmt.Sprintf(format, a...))
}
