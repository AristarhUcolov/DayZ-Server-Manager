// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"sync"
	"testing"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
)

// The web layer used to read the shared *config.Manager directly, while
// MutateConfig replaced it wholesale under a lock — a genuine race that
// `go test -race` reported against dashboardMetrics, which polls every 5s and
// ranges over the Mods slice. Reading through App.Cfg() takes the same lock and
// copies the slices, so the reader can never see a torn header.
//
// Run with -race; without it this test only proves the accessor works.
func TestCfgSnapshotIsRaceFree(t *testing.T) {
	a := &app.App{Config: &config.Manager{
		Mods:       []string{"@CF"},
		ServerMods: []string{},
	}}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: the shape MutateConfig commits.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Persisting fails (no configPath in a bare App) but the
			// in-memory commit happens first, which is the racy part.
			_ = a.MutateConfig(func(c *config.Manager) error {
				c.Mods = append([]string(nil), "@CF", "@Expansion")
				c.ServerMods = append([]string(nil), "@Admin")
				c.ServerPort = 2302 + i%10
				return nil
			})
		}
	}()

	// Readers: what a handler and the metrics poller do.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				cfg := a.Cfg()
				n := 0
				for range cfg.Mods {
					n++
				}
				for range cfg.ServerMods {
					n++
				}
				_ = cfg.ServerPort
			}
		}()
	}

	// Enough iterations for the detector to see the pair.
	for i := 0; i < 2000; i++ {
		_ = a.Cfg()
	}
	close(stop)
	wg.Wait()
}

// MapSizes used to be shared between the live config, Cfg() snapshots and the
// working copy MutateConfig hands to the map-size handler — an in-place map
// write next to a reader is a fatal runtime error, not a panic. Run with -race.
func TestCfgMapSizesNotShared(t *testing.T) {
	a := &app.App{Config: &config.Manager{MapSizes: map[string]int{"chernarusplus": 15360}}}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = a.MutateConfig(func(c *config.Manager) error {
				c.MapSizes["custom"] = 10000 + i%7
				return nil
			})
		}
	}()
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := a.Cfg()
				for k, v := range snap.MapSizes {
					_, _ = k, v
				}
			}
		}()
	}
	// A snapshot taken now must not change when the config changes later.
	snap := a.Cfg()
	before := snap.MapSizes["chernarusplus"]
	_ = a.MutateConfig(func(c *config.Manager) error { c.MapSizes["chernarusplus"] = 1; return nil })
	if snap.MapSizes["chernarusplus"] != before {
		t.Fatal("Cfg() snapshot shares its map with the live config")
	}
	close(stop)
	wg.Wait()
}
