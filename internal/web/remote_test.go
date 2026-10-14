// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"context"
	"errors"
	"testing"

	"dayzmanager/internal/upnp"
)

// fakeRouter is a portMapper whose lookups can fail.
type fakeRouter struct {
	mapped    map[string]string // "TCP/8787" → internal client
	lookupErr error             // returned by MappedTo
	deleted   []string
}

func (f *fakeRouter) MappedTo(_ context.Context, proto string, port int) (string, error) {
	if f.lookupErr != nil {
		return "", f.lookupErr
	}
	return f.mapped[portKey(portState{Proto: proto, Port: port})], nil
}

func (f *fakeRouter) AddPortMapping(_ context.Context, proto string, port int, _ string) error {
	f.mapped[portKey(portState{Proto: proto, Port: port})] = "10.0.0.5"
	return nil
}

func (f *fakeRouter) DeletePortMapping(_ context.Context, proto string, port int) error {
	k := portKey(portState{Proto: proto, Port: port})
	f.deleted = append(f.deleted, k)
	delete(f.mapped, k)
	return nil
}

// A forward that is no longer wanted is never forgotten while its state is
// unknown: a network error keeps it for the next sync, and a router that
// refuses the lookup gets a blind delete of our own forward.
func TestReconcileKeepsOrDeletesUnwantedForwards(t *testing.T) {
	panel := portState{Proto: "TCP", Port: 8787, Purpose: "panel"}

	r := &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.5"}, lookupErr: errors.New("i/o timeout")}
	_, still := reconcilePorts(context.Background(), r, "10.0.0.5", nil, []portState{panel})
	if len(still) != 1 || len(r.deleted) != 0 {
		t.Fatalf("network error: still=%v deleted=%v — the forward must be kept for a retry", still, r.deleted)
	}

	r = &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.5"}, lookupErr: &upnp.SOAPError{Code: 401, Description: "Invalid Action"}}
	_, still = reconcilePorts(context.Background(), r, "10.0.0.5", nil, []portState{panel})
	if len(still) != 0 || len(r.deleted) != 1 {
		t.Fatalf("lookup refused: still=%v deleted=%v — our forward must be deleted blind", still, r.deleted)
	}

	r = &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.9"}}
	_, still = reconcilePorts(context.Background(), r, "10.0.0.5", nil, []portState{panel})
	if len(still) != 0 || len(r.deleted) != 0 {
		t.Fatalf("re-pointed elsewhere: still=%v deleted=%v — not ours any more", still, r.deleted)
	}
}

// The background loop re-syncs when the wanted forwards change (a new game or
// RCon port) instead of waiting for the 20-minute timer.
func TestUPnPStaleFollowsWantedPorts(t *testing.T) {
	h, _ := testHandlers(t)
	h.upnp = &upnpState{}
	h.upnp.synced = portsKey(h.wantedPorts(h.app.Cfg()))
	if h.upnpStale() {
		t.Fatal("nothing changed, nothing to sync")
	}
	h.upnp.synced = "TCP/1"
	if !h.upnpStale() {
		t.Fatal("changed forwards must trigger a sync")
	}
}
