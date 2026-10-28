// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/util"
)

var (
	reEvent     = regexp.MustCompile(`(?s)<Event[ >].*?</Event>`)
	reEventTime = regexp.MustCompile(`<TimeCreated SystemTime=['"]([^'"]+)['"]`)
	reEventData = regexp.MustCompile(`<Data(?: Name=['"]([^'"]*)['"])?>([^<]*)</Data>`)
)

// Positions of Application Error 1000 data on Windows builds that do not
// name them.
var faultFields = []string{"AppName", "AppVersion", "AppTimeStamp", "ModuleName", "ModuleVersion",
	"ModuleTimeStamp", "ExceptionCode", "FaultingOffset", "ProcessId"}

// windowsFault looks for Windows' record of this program's crash
// (Application log, "Application Error" 1000) after lastSeen and describes
// it: the module that failed and the exception code. "" when there is none —
// the process was ended from outside, or Go printed the crash itself.
func windowsFault(pid int, lastSeen time.Time) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wevtutil", "qe", "Application",
		"/q:*[System[Provider[@Name='Application Error'] and EventID=1000]]", "/c:30", "/rd:true", "/f:xml")
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseFault(string(out), filepath.Base(exe), pid, lastSeen)
}

// parseFault picks this program's crash out of wevtutil's XML.
func parseFault(xml, exeName string, pid int, lastSeen time.Time) string {
	for _, ev := range reEvent.FindAllString(xml, -1) {
		fields := map[string]string{}
		for i, m := range reEventData.FindAllStringSubmatch(ev, -1) {
			name := m[1]
			if name == "" && i < len(faultFields) {
				name = faultFields[i]
			}
			fields[name] = strings.TrimSpace(m[2])
		}
		if !strings.EqualFold(fields["AppName"], exeName) {
			continue
		}
		if p := fields["ProcessId"]; p != "" {
			if n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(p), "0x"), 16, 64); err == nil && int(n) != pid {
				continue
			}
		} else if m := reEventTime.FindStringSubmatch(ev); m != nil {
			// No pid to compare: the crash must come after the run's last
			// sign of life (minus a minute of heartbeat spacing).
			if t, err := time.Parse(time.RFC3339Nano, m[1]); err == nil && t.Before(lastSeen.Add(-time.Minute)) {
				continue
			}
		}
		module := fields["ModuleName"]
		if module == "" {
			module = "?"
		}
		code := strings.ToLower(strings.TrimPrefix(strings.ToLower(fields["ExceptionCode"]), "0x"))
		desc := module + ", 0x" + code
		if what := exceptionName(code); what != "" {
			desc += " (" + what + ")"
		}
		return desc
	}
	return ""
}

// exceptionName names the exception codes a crash usually has.
func exceptionName(code string) string {
	switch code {
	case "c0000005":
		return "access violation"
	case "c0000409":
		return "fail fast"
	case "c00000fd":
		return "stack overflow"
	case "c0000374":
		return "heap corruption"
	case "80000003":
		return "breakpoint"
	case "e06d7363":
		return "C++ exception"
	case "c0000135":
		return "DLL not found"
	}
	return ""
}
