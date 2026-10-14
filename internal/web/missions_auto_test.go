// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/server"
)

func testHandlers(t *testing.T) (*handlers, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Manager{AutoInstallMissions: true, ServerCfg: "serverDZ.cfg"}
	logger := log.New(io.Discard, "", 0)
	a := &app.App{Config: cfg, ServerDir: dir, ManagerDir: filepath.Join(dir, ".dayz-manager"), Log: logger,
		Server: server.NewController(dir, cfg, logger)}
	if err := os.MkdirAll(a.ManagerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &handlers{app: a}, dir
}

func put(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, p string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
}

// A map mod can carry its mission as a folder or as a .zip; both are found,
// and addons/ is never searched.
func TestScanMissionSourcesFoldersAndZips(t *testing.T) {
	mod := t.TempDir()
	put(t, filepath.Join(mod, "Extras", "Mission", "regular.namalsk", "cfgeconomycore.xml"), "<economycore/>")
	put(t, filepath.Join(mod, "Extras", "Mission", "empty.namalsk", "init.c"), "void main(){}")
	put(t, filepath.Join(mod, "addons", "fake.mission", "cfgeconomycore.xml"), "x")
	writeZip(t, filepath.Join(mod, "Extras", "hardcore.zip"), map[string]string{
		"hardcore.namalsk/init.c":       "void main(){}",
		"hardcore.namalsk/db/types.xml": "<types/>",
		"readme.txt":                    "hi",
	})
	got := map[string]missionSource{}
	for _, ms := range scanMissionSources(mod, 5) {
		got[ms.Name] = ms
	}
	if len(got) != 3 {
		t.Fatalf("found %v, want regular, empty and hardcore", got)
	}
	if z := got["hardcore.namalsk"]; z.Zip == "" || z.Inner != "hardcore.namalsk" {
		t.Fatalf("zip mission = %+v", z)
	}
	if got["regular.namalsk"].Dir == "" || got["empty.namalsk"].Dir == "" {
		t.Fatalf("folder missions not found: %+v", got)
	}
}

// Installing a map mod copies its missions into mpmissions, never overwrites
// one already there, and remembers where the new one came from.
func TestAutoInstallModMissions(t *testing.T) {
	h, dir := testHandlers(t)
	mod := filepath.Join(dir, "@Namalsk")
	put(t, filepath.Join(mod, "Extras", "regular.namalsk", "cfgeconomycore.xml"), "<economycore/>")
	put(t, filepath.Join(mod, "Extras", "regular.namalsk", "db", "types.xml"), "<types/>")
	writeZip(t, filepath.Join(mod, "Extras", "hardcore.namalsk.zip"), map[string]string{
		"init.c":       "void main(){}",
		"db/types.xml": "<types/>",
	})
	// An edited copy the admin already has must survive.
	put(t, filepath.Join(dir, "mpmissions", "hardcore.namalsk", "init.c"), "EDITED")

	res := h.autoInstallModMissions([]string{"@Namalsk"})
	if len(res) != 2 {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res {
		switch r.Name {
		case "regular.namalsk":
			if !r.Installed {
				t.Fatalf("regular not installed: %+v", r)
			}
		case "hardcore.namalsk":
			if !r.Existed || r.Installed {
				t.Fatalf("existing mission must be left alone: %+v", r)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mpmissions", "regular.namalsk", "db", "types.xml")); err != nil {
		t.Fatal("mission files not copied")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "mpmissions", "hardcore.namalsk", "init.c")); string(b) != "EDITED" {
		t.Fatal("an existing mission was overwritten")
	}
	if h.loadMissionOrigins()["regular.namalsk"] != "@Namalsk" {
		t.Fatal("origin not recorded")
	}

	// Switched off in Settings: nothing happens.
	h.app.Config.AutoInstallMissions = false
	if res := h.autoInstallModMissions([]string{"@Namalsk"}); res != nil {
		t.Fatalf("disabled but got %+v", res)
	}
}

// The browser's folder picker sends every file with its relative path; the
// mission inside is found and installed.
func TestImportMissionFolderUpload(t *testing.T) {
	h, dir := testHandlers(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	addFile := func(rel, content string) {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="files"; filename="`+rel+`"`)
		hdr.Set("Content-Type", "application/octet-stream")
		w, _ := mw.CreatePart(hdr)
		_, _ = w.Write([]byte(content))
	}
	addFile("Namalsk-Server/regular.namalsk/cfgeconomycore.xml", "<economycore/>")
	addFile("Namalsk-Server/regular.namalsk/db/types.xml", "<types/>")
	addFile("Namalsk-Server/README.md", "readme")
	_ = mw.WriteField("activate", "0")
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/missions/import-folder", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.missionsImportFolder(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["name"] != "regular.namalsk" {
		t.Fatalf("response %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "mpmissions", "regular.namalsk", "db", "types.xml")); err != nil {
		t.Fatal("uploaded mission not installed")
	}

	// A path escaping the upload folder is refused.
	body.Reset()
	mw = multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="files"; filename="../../evil.txt"`)
	w, _ := mw.CreatePart(hdr)
	_, _ = w.Write([]byte("x"))
	_ = mw.Close()
	req = httptest.NewRequest("POST", "/api/missions/import-folder", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec = httptest.NewRecorder()
	h.missionsImportFolder(rec, req)
	if rec.Code != 400 {
		t.Fatalf("path traversal: status %d", rec.Code)
	}
}
