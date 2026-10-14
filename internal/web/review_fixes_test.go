// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dztypes "dayzmanager/internal/types"
)

// Saving one event's points into a cfgeventspawns.xml that does not parse
// used to start from an empty document and wipe every other event.
func TestEventSpawnsSaveRefusesUnreadableFile(t *testing.T) {
	h, _, m := vehicleFixture(t)
	p := filepath.Join(m, "cfgeventspawns.xml")
	broken := "<eventposdef>\n\t<event name=\"VehicleCivilianSedan\">\n\t\t<pos x=\"1\" z=\"2\"\n</eventposdef>"
	put(t, p, broken)
	rec := httptest.NewRecorder()
	h.eventSpawnsSave(rec, httptest.NewRequest("POST", "/api/eventspawns/save", strings.NewReader(`{"name":"X","pos":[{"x":1,"z":2,"a":0}]}`)))
	if rec.Code == 200 {
		t.Fatal("saving into an unreadable file must be refused")
	}
	if got, _ := os.ReadFile(p); string(got) != broken {
		t.Fatalf("the file was rewritten:\n%s", got)
	}
}

// Positions shipped for several maps: the list for the current map wins, a
// list with points off the map is ignored, and an undecidable choice takes
// nothing rather than another map's points.
func TestPickPositionsChoosesTheCurrentMap(t *testing.T) {
	cher := posSource{path: "/m/@Cars/Chernarus/cfgeventspawns.xml", pos: []dztypes.SpawnPos{{X: 100, Z: 200}}}
	livo := posSource{path: "/m/@Cars/Livonia/cfgeventspawns.xml", pos: []dztypes.SpawnPos{{X: 300, Z: 400}}}
	far := posSource{path: "/m/@Cars/Big/cfgeventspawns.xml", pos: []dztypes.SpawnPos{{X: 20000, Z: 1}}}
	if got := pickPositions([]posSource{cher, livo}, []string{"chernarus", "chernarusplus"}, 15360); len(got) != 1 || got[0].X != 100 {
		t.Fatalf("by map name: %v", got)
	}
	if got := pickPositions([]posSource{far, livo}, []string{"chernarus"}, 15360); len(got) != 1 || got[0].X != 300 {
		t.Fatalf("off-map list must be dropped: %v", got)
	}
	if got := pickPositions([]posSource{cher, livo}, []string{"sakhal"}, 15360); got != nil {
		t.Fatalf("ambiguous choice must take nothing: %v", got)
	}
	if got := pickPositions([]posSource{cher}, []string{"sakhal"}, 15360); len(got) != 1 {
		t.Fatalf("a single fitting list is used: %v", got)
	}
}

// The manager regenerates only certificates it generated itself.
func TestOwnCertificateIsRecognised(t *testing.T) {
	dir := t.TempDir()
	cp, kp := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := generateSelfSigned(cp, kp); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cp)
	block, _ := pem.Decode(data)
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !isOwnCert(c) {
		t.Fatal("a generated certificate must be recognised as ours")
	}
	if d := c.NotAfter.Sub(c.NotBefore); d.Hours() > 825*24 {
		t.Fatalf("validity %v exceeds Apple's 825-day limit", d)
	}
	c.Subject.Organization = []string{"Let's Encrypt"}
	if isOwnCert(c) {
		t.Fatal("an admin's certificate must not be taken for ours")
	}
}
