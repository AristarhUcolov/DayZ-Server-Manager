// Copyright (c) 2026 Aristarh Ucolov.
package rcon

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestManagerConcurrentCache hammers the shared player cache from many
// goroutines to prove the cmdMu / cacheMu design is race-free. The manager is
// left unconfigured, so Connect() fails immediately (no real socket) and we
// only exercise the caching/serialization paths. Run with -race.
func TestManagerConcurrentCache(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = m.PlayersFresh(5 * time.Millisecond)
				_, _ = m.PlayersCached(5 * time.Millisecond)
				m.InvalidatePlayers()
			}
		}()
	}
	wg.Wait()
	// Let any background refreshes kicked off by PlayersFresh drain.
	time.Sleep(50 * time.Millisecond)
}

func TestUnconfiguredCommandErrors(t *testing.T) {
	m := NewManager()
	_, err := m.Command("players")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
}
