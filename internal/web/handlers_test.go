// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The Linux server binary "DayZServer" has no extension, so the binaryExts
// check let the file editor overwrite it with a script that Start then ran.
func TestFilesWriteRefusesServerBinary(t *testing.T) {
	h, dir := testHandlers(t)
	for _, name := range []string{"DayZServer", "dayzserver_x64", "DayZServer_x64.exe"} {
		p := filepath.Join(dir, name)
		put(t, p, "ELF")
		rec := httptest.NewRecorder()
		h.filesWrite(rec, httptest.NewRequest("POST", "/api/files/write", strings.NewReader(`{"path":"`+name+`","content":"#!/bin/sh\necho pwned\n"}`)))
		if rec.Code == 200 {
			t.Errorf("filesWrite(%s) = 200, want refusal", name)
		}
		if b, _ := os.ReadFile(p); string(b) != "ELF" {
			t.Errorf("%s was overwritten: %q", name, b)
		}
	}
	// A plain config next to it stays editable.
	put(t, filepath.Join(dir, "serverDZ.cfg"), "hostname = \"x\";\n")
	rec := httptest.NewRecorder()
	h.filesWrite(rec, httptest.NewRequest("POST", "/api/files/write", strings.NewReader(`{"path":"serverDZ.cfg","content":"hostname = \"y\";\n"}`)))
	if rec.Code != 200 {
		t.Fatalf("filesWrite(serverDZ.cfg) = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestWriteRefusalExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no exec bits on Windows")
	}
	dir := t.TempDir()
	elf := filepath.Join(dir, "dayz-manager-linux")
	put(t, elf, "ELF")
	if err := os.Chmod(elf, 0o755); err != nil {
		t.Fatal(err)
	}
	if writeRefusal(elf) == "" {
		t.Error("extension-less executable should be refused")
	}
	// Configs on NTFS/SMB mounts report 0777 — an extension keeps them editable.
	cfg := filepath.Join(dir, "serverDZ.cfg")
	put(t, cfg, "x")
	if err := os.Chmod(cfg, 0o777); err != nil {
		t.Fatal(err)
	}
	if r := writeRefusal(cfg); r != "" {
		t.Errorf("writeRefusal(serverDZ.cfg) = %q, want editable", r)
	}
}
