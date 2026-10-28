// Copyright (c) 2026 Aristarh Ucolov.
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dayzmanager/internal/config"
	"dayzmanager/internal/pbo"
)

// The tweaks mod appears with the first vehicle setting, is listed as a
// server-side mod, carries the classes in its script, and goes away (folder
// and list entry) once nothing is set.
func TestServerTweaksModFollowsSettings(t *testing.T) {
	a := newPrimary(t)
	dir := filepath.Join(a.ServerDir, TweaksMod)

	if err := a.SyncServerTweaks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("no settings, yet the mod was written")
	}

	if err := a.MutateConfig(func(c *config.Manager) error {
		c.ServerMods = []string{"@SomeServerMod"}
		c.VehicleTweaks = []config.VehicleTweak{
			{Event: "VehicleCivilianSedan", Classes: []string{"CivilianSedan", "CivilianSedan_Black"},
				Fuel: &config.FluidRange{Min: 0.5, Max: 1}, Coolant: &config.FluidRange{Min: 1, Max: 1}},
			{Event: "VehicleBoat", Classes: []string{"Boat_01_Orange"}, Fuel: &config.FluidRange{Min: 1, Max: 0.25}},
			{Event: "VehicleTruck01", Classes: []string{"Truck_01_Covered"}}, // nothing set
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncServerTweaks(); err != nil {
		t.Fatal(err)
	}
	if got := a.Cfg().ServerMods; len(got) != 2 || got[1] != TweaksMod {
		t.Fatalf("server mods %v", got)
	}
	f, err := pbo.Open(filepath.Join(dir, "addons", "dzm_servertweaks.pbo"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Properties["prefix"] != "DZM_ServerTweaks" {
		t.Errorf("prefix %q", f.Properties["prefix"])
	}
	var script string
	for _, e := range f.Entries {
		data, _ := f.Read(e)
		if strings.HasSuffix(e.Name, ".c") {
			script = string(data)
		}
		if e.Name == "config.cpp" {
			if c, err := pbo.ParseConfig(data); err != nil || c.Child("CfgMods") == nil {
				t.Errorf("config.cpp does not parse: %v", err)
			}
		}
	}
	for _, want := range []string{
		`if (type == "civiliansedan") { lo = 0.5; hi = 1.0; return true; }`,
		`if (type == "civiliansedan_black") { lo = 0.5; hi = 1.0; return true; }`,
		`if (type == "boat_01_orange") { lo = 0.25; hi = 1.0; return true; }`, // swapped into order
		"modded class CarScript", "modded class BoatScript", "modded class MotorbikeScript",
		"LeakAll(CarFluid.COOLANT);",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "truck_01_covered") {
		t.Error("a vehicle without settings made it into the script")
	}
	// Braces balance (a broken script would stop the server from starting).
	if strings.Count(script, "{") != strings.Count(script, "}") {
		t.Error("unbalanced braces in the script")
	}

	if err := a.MutateConfig(func(c *config.Manager) error { c.VehicleTweaks = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncServerTweaks(); err != nil {
		t.Fatal(err)
	}
	if got := a.Cfg().ServerMods; len(got) != 1 || got[0] != "@SomeServerMod" {
		t.Fatalf("server mods after clearing %v", got)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the mod folder stayed behind")
	}
}

// A fresh character's food, water and warmth go into a 5_Mission script of
// their own; the mod then declares only the module it carries.
func TestServerTweaksSpawnStats(t *testing.T) {
	a := newPrimary(t)
	full, most := 1.0, 0.75
	if err := a.MutateConfig(func(c *config.Manager) error {
		c.SpawnStats = &config.SpawnStats{Food: &full, Water: &most}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncServerTweaks(); err != nil {
		t.Fatal(err)
	}
	f, err := pbo.Open(filepath.Join(a.ServerDir, TweaksMod, "addons", "dzm_servertweaks.pbo"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range f.Entries {
		data, _ := f.Read(e)
		files[strings.ReplaceAll(e.Name, "\\", "/")] = string(data)
	}
	if _, ok := files["Scripts/4_World/DZM_VehicleTweaks.c"]; ok {
		t.Error("a vehicle script without vehicle settings")
	}
	script := files["Scripts/5_Mission/DZM_SpawnStats.c"]
	for _, want := range []string{
		"modded class MissionServer",
		"PlayerBase player = super.OnClientNewEvent(identity, pos, ctx);",
		"player.GetStatEnergy().Set(player.GetStatEnergy().GetMax() * 1.0);",
		"player.GetStatWater().Set(player.GetStatWater().GetMax() * 0.75);",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "HeatBuffer") || strings.Count(script, "{") != strings.Count(script, "}") {
		t.Errorf("unset warmth written or unbalanced braces:\n%s", script)
	}
	cfg := files["config.cpp"]
	if c, err := pbo.ParseConfig([]byte(cfg)); err != nil || c.Child("CfgMods") == nil {
		t.Fatalf("config.cpp does not parse: %v\n%s", err, cfg)
	}
	if !strings.Contains(cfg, "missionScriptModule") || strings.Contains(cfg, "worldScriptModule") || !strings.Contains(cfg, `dependencies[] = {"Mission"};`) {
		t.Errorf("config.cpp modules:\n%s", cfg)
	}
}

// A folder with the mod's name that this code did not make is never deleted.
func TestServerTweaksKeepsForeignFolder(t *testing.T) {
	a := newPrimary(t)
	dir := filepath.Join(a.ServerDir, TweaksMod)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncServerTweaks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("a folder the panel did not make was removed")
	}
}
