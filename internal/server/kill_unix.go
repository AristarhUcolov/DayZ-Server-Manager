// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package server

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"dayzmanager/internal/util"
)

// setProcAttr starts the server in its own process group so a stop can signal
// the whole tree (the server and anything it spawns) at once.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// ensureExecutable restores the exec bit a Windows-side upload or an archive
// extraction often strips from the Linux DayZServer binary — the start would
// otherwise fail with a bare "permission denied".
func ensureExecutable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode()&0o111 != 0 {
		return nil
	}
	if err := os.Chmod(path, st.Mode()|0o755); err != nil {
		return fmt.Errorf("%s is not executable and chmod +x failed: %w", path, err)
	}
	return nil
}

// forceKill asks the server to terminate (SIGTERM, which the Linux server
// handles like a console close), then SIGKILLs whatever is left after ten
// seconds. Children we started get the whole process group signalled.
func forceKill(p *runningProc) {
	target := p.pid
	if p.cmd != nil {
		target = -p.pid // the process group created by Setpgid
	}
	_ = syscall.Kill(target, syscall.SIGTERM)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !util.ProcessAlive(p.pid) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = syscall.Kill(target, syscall.SIGKILL)
}
