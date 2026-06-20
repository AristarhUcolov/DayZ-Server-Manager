// Copyright (c) 2026 Aristarh Ucolov.
package server

import (
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"dayzmanager/internal/config"
)

func TestShiftHHMM(t *testing.T) {
	cases := []struct {
		in    string
		delta int
		want  string
	}{
		{"03:00", -5, "02:55"},
		{"03:00", 0, "03:00"},
		{"00:02", -5, "23:57"},  // wraps backward across midnight
		{"23:58", 5, "00:03"},   // wraps forward across midnight
		{"12:30", -90, "11:00"}, // multi-hour shift
		{"not-a-time", -5, "not-a-time"},
		{"", -5, ""},
	}
	for _, c := range cases {
		if got := shiftHHMM(c.in, c.delta); got != c.want {
			t.Errorf("shiftHHMM(%q, %d) = %q, want %q", c.in, c.delta, got, c.want)
		}
	}
}

func TestMaxWarnMinutes(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{[]int{5, 3, 1}, 5},
		{[]int{1, 9, 4}, 9},
		{nil, 0},
		{[]int{}, 0},
		{[]int{0}, 0},
	}
	for _, c := range cases {
		if got := maxWarnMinutes(c.in); got != c.want {
			t.Errorf("maxWarnMinutes(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestScheduleConfigConcurrent exercises the snapshot under concurrent writers
// and readers. Run with -race: it guards the invariant that the supervisor
// loops read an immutable snapshot instead of the shared *config.Manager,
// which would otherwise be a data race (torn slice header → panic in a
// goroutine outside the HTTP recoverer → whole manager down).
func TestScheduleConfigConcurrent(t *testing.T) {
	c := NewController(t.TempDir(), &config.Manager{}, log.New(io.Discard, "", 0))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c.SetScheduleConfig(&config.Manager{
					AutoRestartEnabled:     true,
					AutoRestartSeconds:     14390,
					RestartWarnMinutes:     []int{5, 3, 1},
					ScheduledRestarts:      []string{"03:00", "12:00"},
					ScheduledAnnouncements: []config.ScheduledAnnouncement{{Time: "10:00", Message: "hi", Enabled: true}},
				})
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := c.schedSnapshot()
				_ = maxWarnMinutes(s.restartWarnMinutes)
				for _, x := range s.scheduledRestarts {
					_ = shiftHHMM(x, -5)
				}
				for _, a := range s.announcements {
					_ = a.Time
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
