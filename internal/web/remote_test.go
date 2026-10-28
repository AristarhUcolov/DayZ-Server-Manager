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
	descs     map[string]string // "TCP/8787" → description
	lookupErr error             // returned by MappingInfo
	deleted   []string
	added     []string
}

func (f *fakeRouter) MappingInfo(_ context.Context, proto string, port int) (string, string, error) {
	if f.lookupErr != nil {
		return "", "", f.lookupErr
	}
	k := portKey(portState{Proto: proto, Port: port})
	return f.mapped[k], f.descs[k], nil
}

func (f *fakeRouter) AddPortMapping(_ context.Context, proto string, port int, desc string) error {
	k := portKey(portState{Proto: proto, Port: port})
	if to := f.mapped[k]; to != "" && to != "10.0.0.5" {
		return &upnp.SOAPError{Code: 718, Description: "ConflictInMappingEntry"}
	}
	f.mapped[k] = "10.0.0.5"
	if f.descs == nil {
		f.descs = map[string]string{}
	}
	f.descs[k] = desc
	f.added = append(f.added, k)
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
	_, still := reconcilePorts(context.Background(), r, "10.0.0.5", "DayZ Server Manager (PC)", nil, []portState{panel})
	if len(still) != 1 || len(r.deleted) != 0 {
		t.Fatalf("network error: still=%v deleted=%v — the forward must be kept for a retry", still, r.deleted)
	}

	r = &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.5"}, lookupErr: &upnp.SOAPError{Code: 401, Description: "Invalid Action"}}
	_, still = reconcilePorts(context.Background(), r, "10.0.0.5", "DayZ Server Manager (PC)", nil, []portState{panel})
	if len(still) != 0 || len(r.deleted) != 1 {
		t.Fatalf("lookup refused: still=%v deleted=%v — our forward must be deleted blind", still, r.deleted)
	}

	r = &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.9"}}
	_, still = reconcilePorts(context.Background(), r, "10.0.0.5", "DayZ Server Manager (PC)", nil, []portState{panel})
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

// After a restart the PC often gets a new LAN address. Its own forward still
// points at the old one: it is moved here, while a forward that belongs to
// another device is reported, never taken.
func TestReconcileMovesOwnForwardToNewAddress(t *testing.T) {
	panel := portState{Proto: "TCP", Port: 8787, Purpose: "panel"}
	own := "DayZ Server Manager (PC)"
	cases := []struct {
		name  string
		desc  string
		owned bool
		moved bool
	}{
		{"this PC's description", own, false, true},
		{"older plain description, made by us", "DayZ Server Manager", true, true},
		{"router without descriptions, made by us", "", true, true},
		{"another program's forward", "Plex", true, false},
		{"plain description, never made by us", "DayZ Server Manager", false, false},
		{"another PC running the manager", "DayZ Server Manager (OTHER)", true, false},
	}
	for _, c := range cases {
		r := &fakeRouter{mapped: map[string]string{"TCP/8787": "10.0.0.7"}, descs: map[string]string{"TCP/8787": c.desc}}
		var prev []portState
		if c.owned {
			prev = []portState{panel}
		}
		results, still := reconcilePorts(context.Background(), r, "10.0.0.5", own, []portState{panel}, prev)
		ok := len(results) == 1 && results[0].OK
		if ok != c.moved {
			t.Fatalf("%s: forward moved = %v, want %v (results %+v)", c.name, ok, c.moved, results)
		}
		if c.moved && (r.mapped["TCP/8787"] != "10.0.0.5" || r.descs["TCP/8787"] != own || len(still) != 1) {
			t.Fatalf("%s: forward now %s/%q, still=%v", c.name, r.mapped["TCP/8787"], r.descs["TCP/8787"], still)
		}
		if !c.moved && r.mapped["TCP/8787"] != "10.0.0.7" {
			t.Fatalf("%s: another device's forward was taken", c.name)
		}
	}
}
