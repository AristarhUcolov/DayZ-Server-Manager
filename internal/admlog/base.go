// Copyright (c) 2026 Aristarh Ucolov.
package admlog

import (
	"regexp"
	"strings"
)

// Base is one base-building line of the admin log. DayZ writes them when
// serverDZ.cfg has adminLogPlacement = 1 (tents, kits, traps placed) and
// adminLogBuildActions = 1 (parts built or destroyed, stashes, flags).
type Base struct {
	// Kind groups the actions for the map: "build" (placed, built, a flag
	// raised or lowered), "raid" (a part or a lock destroyed) or "stash"
	// (a stash dug in or out).
	Kind string `json:"kind"`
	// Action is what exactly happened: placed, built, destroyed, flag-up,
	// flag-down, dug-in or dug-out.
	Action string `json:"action"`
	Object string `json:"object,omitempty"` // "Fence Kit", "Fence", "combination lock", "UndergroundStash"
	Class  string `json:"class,omitempty"`  // the placed item's class ("FenceKit")
	Part   string `json:"part,omitempty"`   // the built part ("wall_base_down")
	Tool   string `json:"tool,omitempty"`   // what it was done with ("Hammer", "hands")
}

var (
	// placed Fence Kit<FenceKit>
	rePlaced = regexp.MustCompile(`^placed (.*?)\s*<([^<>]*)>\s*$`)
	// Built wall_base_down on Fence with Hammer
	reBuilt = regexp.MustCompile(`^Built (\S+) on (.+?) with (.+)$`)
	// destroyed Fence with Sledgehammer / destroyed combination lock with Hacksaw
	reDestroyed = regexp.MustCompile(`^destroyed (.+?) with (.+)$`)
	// has raised Flag_Chernarus on TerritoryFlag at <…>
	reFlag = regexp.MustCompile(`^has (raised|lowered) (\S+) on (\S+)`)
	// Player SurvivorM_Boris<…> Dug in UndergroundStash<…> at position <…>
	reDug = regexp.MustCompile(`Dug (in|out) ([A-Za-z0-9_]+)`)
)

// BaseAction recognises a base-building line. ev is a parsed line about a
// player; the player's position (ev.Pos) is where it happened.
func BaseAction(ev Event) (Base, bool) {
	if ev.Type != "other" || ev.Player == "" {
		return Base{}, false
	}
	tail := strings.TrimSpace(ev.Message)
	if m := rePlaced.FindStringSubmatch(tail); m != nil {
		obj := strings.TrimSpace(m[1])
		if obj == "Nameless Object" {
			obj = ""
		}
		return Base{Kind: "build", Action: "placed", Object: obj, Class: m[2]}, true
	}
	if m := reBuilt.FindStringSubmatch(tail); m != nil {
		return Base{Kind: "build", Action: "built", Part: m[1], Object: strings.TrimSpace(m[2]), Tool: strings.TrimSpace(m[3])}, true
	}
	if m := reDestroyed.FindStringSubmatch(tail); m != nil {
		return Base{Kind: "raid", Action: "destroyed", Object: strings.TrimSpace(m[1]), Tool: strings.TrimSpace(m[2])}, true
	}
	if m := reFlag.FindStringSubmatch(tail); m != nil {
		act := "flag-up"
		if m[1] == "lowered" {
			act = "flag-down"
		}
		return Base{Kind: "build", Action: act, Object: m[3], Class: m[2]}, true
	}
	if m := reDug.FindStringSubmatch(tail); m != nil {
		return Base{Kind: "stash", Action: "dug-" + m[1], Object: m[2]}, true
	}
	return Base{}, false
}
