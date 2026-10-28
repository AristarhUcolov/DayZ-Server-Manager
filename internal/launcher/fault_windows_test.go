// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package launcher

import (
	"testing"
	"time"
)

const faultSample = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Application Error'/><EventID>1000</EventID><TimeCreated SystemTime='2026-10-27T16:31:02.1234567Z'/></System><EventData><Data Name='AppName'>OtherApp.exe</Data><Data Name='AppVersion'>1.0.0.0</Data><Data Name='AppTimeStamp'>0</Data><Data Name='ModuleName'>other.dll</Data><Data Name='ModuleVersion'>1.0</Data><Data Name='ModuleTimeStamp'>0</Data><Data Name='ExceptionCode'>c0000005</Data><Data Name='FaultingOffset'>0</Data><Data Name='ProcessId'>0x291c</Data></EventData></Event>` +
	`<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Application Error'/><EventID>1000</EventID><TimeCreated SystemTime='2026-10-27T16:30:12.1234567Z'/></System><EventData><Data Name='AppName'>dayz-manager-panel.exe</Data><Data Name='AppVersion'>1.0.0.0</Data><Data Name='AppTimeStamp'>0</Data><Data Name='ModuleName'>EmbeddedBrowserWebView.dll</Data><Data Name='ModuleVersion'>130.0</Data><Data Name='ModuleTimeStamp'>0</Data><Data Name='ExceptionCode'>c0000409</Data><Data Name='FaultingOffset'>0</Data><Data Name='ProcessId'>0x291c</Data></EventData></Event>` +
	// An older Windows: the same data without names, by position.
	`<Event><System><TimeCreated SystemTime='2026-10-27T18:00:00Z'/></System><EventData><Data>dayz-manager.exe</Data><Data>1.0.0.0</Data><Data>0</Data><Data>ntdll.dll</Data><Data>10.0</Data><Data>0</Data><Data>c0000374</Data><Data>0</Data><Data>0x10</Data></EventData></Event>`

func TestParseFault(t *testing.T) {
	seen := time.Date(2026, 10, 27, 16, 29, 0, 0, time.UTC)
	if got := parseFault(faultSample, "dayz-manager-panel.exe", 0x291c, seen); got != "EmbeddedBrowserWebView.dll, 0xc0000409 (fail fast)" {
		t.Fatalf("panel crash = %q", got)
	}
	// Another run of the same program is not this one.
	if got := parseFault(faultSample, "dayz-manager-panel.exe", 1234, seen); got != "" {
		t.Fatalf("wrong pid matched: %q", got)
	}
	if got := parseFault(faultSample, "DAYZ-MANAGER.EXE", 0x10, seen); got != "ntdll.dll, 0xc0000374 (heap corruption)" {
		t.Fatalf("positional data = %q", got)
	}
	if got := parseFault(faultSample, "absent.exe", 1, seen); got != "" {
		t.Fatalf("matched another program: %q", got)
	}
}
