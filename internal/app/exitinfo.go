// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"context"
	"sync"
	"time"
)

// ExitInfo describes a previous run of the manager that did not end with a
// normal exit.
type ExitInfo struct {
	// Kind is "crash" (the manager failed — Crash holds what Go printed) or
	// "killed" (it was ended from outside: the computer slept and lost power,
	// Windows signed out or shut down, a remote session timed out, Task
	// Manager...).
	Kind    string    `json:"kind"`
	Started time.Time `json:"started"`
	// LastSeen is the last heartbeat of that run — about when it ended.
	LastSeen time.Time `json:"lastSeen"`
	Crash    string    `json:"crash,omitempty"`
	// Fault is Windows' own record of the crash (Application Error 1000):
	// the module that failed and the exception code.
	Fault string `json:"fault,omitempty"`
	// AutoRestarted: the guardian started the manager again by itself.
	AutoRestarted bool `json:"autoRestarted,omitempty"`
	// Dismissed hides the notice (the admin closed it).
	Dismissed bool `json:"dismissed,omitempty"`
}

// WorkshopHooks update the server's Workshop mods through the panel's
// SteamCMD (mod source "steamcmd"); the web layer, which owns SteamCMD, sets
// them. Sync runs while the server is stopped and the write lock is held.
type WorkshopHooks struct {
	Sync     func(ctx context.Context) ([]string, error)
	Outdated func(ctx context.Context) (int, error)
}

var workshopHooksMu sync.Mutex

// SetWorkshopHooks installs the SteamCMD update hooks.
func (a *App) SetWorkshopHooks(h WorkshopHooks) {
	workshopHooksMu.Lock()
	a.workshop = h
	workshopHooksMu.Unlock()
}

func (a *App) workshopHooks() WorkshopHooks {
	workshopHooksMu.Lock()
	defer workshopHooksMu.Unlock()
	return a.workshop
}

// sleepState is the last time the computer slept while the manager ran.
type sleepState struct {
	mu       sync.Mutex
	from, to time.Time
}

var lastSleep sleepState

// NoteSleep records that the computer slept from from to to (the manager's
// heartbeat stopped): the server and the panel were down meanwhile.
func (a *App) NoteSleep(from, to time.Time) {
	lastSleep.mu.Lock()
	lastSleep.from, lastSleep.to = from, to
	lastSleep.mu.Unlock()
}

// LastSleep is the last time the computer slept while the manager ran (zero
// times when it did not).
func LastSleep() (from, to time.Time) {
	lastSleep.mu.Lock()
	defer lastSleep.mu.Unlock()
	return lastSleep.from, lastSleep.to
}
