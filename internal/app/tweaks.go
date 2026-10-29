// Copyright (c) 2026 Aristarh Ucolov.
//
// Server tweaks: settings vanilla DayZ hard-codes in script, out of reach of
// any config file. Today that is how full the tank (and, for cars, the
// radiator) of vehicles the Central Economy spawns is — CarScript, BoatScript
// and MotorbikeScript fill a random 0–35 % of the tank in EEOnCECreate
// (scripts/4_World/Entities/Vehicles, 1.29) — how fed, watered and warm a
// fresh character starts (PlayerStatsPCO: energy and water 600 of 5000, a
// neutral heat buffer), and whether the spawn gear's weapons have a round in
// the chamber (the spawn-gear system loads them with Weapon_Base.SpawnAmmo,
// whose default flag chambers one).
//
// The panel generates a small server-side mod, @DZM_ServerTweaks, that
// overrides those functions for what is configured. It is loaded with
// -serverMod, so players download nothing and no keys are involved. SyncServerTweaks rebuilds it before every server start (the
// archive cannot be replaced while the server has it open) and adds it to, or
// removes it from, the server-side mods.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dayzmanager/internal/config"
	"dayzmanager/internal/pbo"
)

// TweaksMod is the generated mod's folder in the server directory.
const TweaksMod = "@DZM_ServerTweaks"

const (
	tweaksPrefix = "DZM_ServerTweaks"
	tweaksMarker = "dzm-generated.txt"
)

// ActiveTweaks are the tweaks that change anything.
func ActiveTweaks(list []config.VehicleTweak) []config.VehicleTweak {
	var out []config.VehicleTweak
	for _, vt := range list {
		if len(vt.Classes) > 0 && (vt.Fuel != nil || vt.Coolant != nil) {
			out = append(out, vt)
		}
	}
	return out
}

// SyncServerTweaks writes the tweaks mod for the current settings (or removes
// it when nothing is set) and keeps the server-side mod list in step. The
// server must not be running: it holds the archive open.
func (a *App) SyncServerTweaks() error {
	cfg := a.Cfg()
	active := ActiveTweaks(cfg.VehicleTweaks)
	stats := cfg.SpawnStats.Active()
	unchambered := ChamberClasses(cfg.Unchambered)
	chamber := len(unchambered) > 0
	dir := filepath.Join(a.ServerDir, TweaksMod)
	listed := false
	for _, m := range cfg.ServerMods {
		if strings.EqualFold(strings.TrimSpace(m), TweaksMod) {
			listed = true
		}
	}
	if len(active) == 0 && !stats && !chamber {
		if listed {
			if err := a.MutateConfig(func(c *config.Manager) error {
				c.ServerMods = withoutMod(c.ServerMods, TweaksMod)
				return nil
			}); err != nil {
				return err
			}
		}
		// Only ever the folder this code made (its marker file says so).
		if _, err := os.Stat(filepath.Join(dir, tweaksMarker)); err == nil {
			_ = os.RemoveAll(dir)
		}
		return nil
	}
	files := []pbo.WriteFile{{Name: "config.cpp", Data: []byte(tweaksConfig(len(active) > 0 || chamber, stats || chamber))}}
	if len(active) > 0 {
		files = append(files, pbo.WriteFile{Name: "Scripts/4_World/DZM_VehicleTweaks.c", Data: []byte(TweaksScript(active))})
	}
	if chamber {
		files = append(files, pbo.WriteFile{Name: "Scripts/4_World/DZM_Chamber.c", Data: []byte(ChamberScript(unchambered))})
	}
	if stats || chamber {
		files = append(files, pbo.WriteFile{Name: "Scripts/5_Mission/DZM_SpawnStats.c", Data: []byte(SpawnStatsScript(cfg.SpawnStats, chamber))})
	}
	if err := pbo.Write(filepath.Join(dir, "addons", "dzm_servertweaks.pbo"), [][2]string{{"prefix", tweaksPrefix}}, files); err != nil {
		return fmt.Errorf("write %s: %w", TweaksMod, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "mod.cpp"), []byte(tweaksModCpp), 0o644)
	_ = os.WriteFile(filepath.Join(dir, tweaksMarker), []byte(tweaksReadme), 0o644)
	if !listed {
		return a.MutateConfig(func(c *config.Manager) error {
			c.ServerMods = append(withoutMod(c.ServerMods, TweaksMod), TweaksMod)
			return nil
		})
	}
	return nil
}

func withoutMod(list []string, mod string) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		if !strings.EqualFold(strings.TrimSpace(m), mod) {
			out = append(out, m)
		}
	}
	return out
}

// TweaksScript is the generated 4_World script.
func TweaksScript(active []config.VehicleTweak) string {
	type entry struct {
		class    string
		min, max float64
	}
	var fuel, cool []entry
	for _, vt := range active {
		for _, c := range vt.Classes {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "" || strings.ContainsAny(c, "\"\\\r\n") {
				continue
			}
			if vt.Fuel != nil {
				fuel = append(fuel, entry{c, clamp01(vt.Fuel.Min), clamp01(vt.Fuel.Max)})
			}
			if vt.Coolant != nil {
				cool = append(cool, entry{c, clamp01(vt.Coolant.Min), clamp01(vt.Coolant.Max)})
			}
		}
	}
	sort.Slice(fuel, func(i, j int) bool { return fuel[i].class < fuel[j].class })
	sort.Slice(cool, func(i, j int) bool { return cool[i].class < cool[j].class })
	lookup := func(name string, list []entry) string {
		var b strings.Builder
		fmt.Fprintf(&b, "\tstatic bool %s(string type, out float lo, out float hi)\n\t{\n\t\ttype.ToLower();\n", name)
		for _, e := range list {
			lo, hi := e.min, e.max
			if hi < lo {
				lo, hi = hi, lo
			}
			fmt.Fprintf(&b, "\t\tif (type == \"%s\") { lo = %s; hi = %s; return true; }\n", e.class, f3(lo), f3(hi))
		}
		b.WriteString("\t\treturn false;\n\t}\n")
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`// Generated by DayZ Server Manager - the panel rewrites this file before
// every server start. Change the values on its Vehicles page.
//
// How full the tank (and a car's radiator) is when the Central Economy
// spawns a vehicle. Vanilla fills a random 0-35 % of the tank here.

class DZM_VehicleTweaks
{
`)
	b.WriteString(lookup("Fuel", fuel))
	b.WriteString("\n")
	b.WriteString(lookup("Coolant", cool))
	b.WriteString(`}

modded class CarScript
{
	override void EEOnCECreate()
	{
		super.EEOnCECreate();
		float lo, hi;
		if (DZM_VehicleTweaks.Fuel(GetType(), lo, hi))
		{
			LeakAll(CarFluid.FUEL);
			Fill(CarFluid.FUEL, GetFluidCapacity(CarFluid.FUEL) * Math.RandomFloatInclusive(lo, hi));
		}
		if (DZM_VehicleTweaks.Coolant(GetType(), lo, hi))
		{
			LeakAll(CarFluid.COOLANT);
			Fill(CarFluid.COOLANT, GetFluidCapacity(CarFluid.COOLANT) * Math.RandomFloatInclusive(lo, hi));
		}
	}
}

modded class BoatScript
{
	override void EEOnCECreate()
	{
		super.EEOnCECreate();
		float lo, hi;
		if (DZM_VehicleTweaks.Fuel(GetType(), lo, hi))
		{
			LeakAll(BoatFluid.FUEL);
			Fill(BoatFluid.FUEL, GetFluidCapacity(BoatFluid.FUEL) * Math.RandomFloatInclusive(lo, hi));
		}
	}
}

modded class MotorbikeScript
{
	override void EEOnCECreate()
	{
		super.EEOnCECreate();
		float lo, hi;
		if (DZM_VehicleTweaks.Fuel(GetType(), lo, hi))
		{
			LeakAll(MotorbikeFluid.FUEL);
			Fill(MotorbikeFluid.FUEL, GetFluidCapacity(MotorbikeFluid.FUEL) * Math.RandomFloatInclusive(lo, hi));
		}
	}
}
`)
	return b.String()
}

// SpawnStatsScript is the generated 5_Mission script: a fresh character's
// food, water and warmth, set once the mission has created it (with its spawn
// gear on). Each value is a share of the stat's maximum, so the script keeps
// working if Bohemia changes the scale. chamber: the 4_World ChamberScript is
// in the mod too, and the spawn gear is loaded while it is told so.
func SpawnStatsScript(s *config.SpawnStats, chamber bool) string {
	var b strings.Builder
	b.WriteString(`// Generated by DayZ Server Manager - the panel rewrites this file before
// every server start. Change the values on its Player loadout page.
//
// How fed, watered and warm a fresh character starts. Vanilla: energy and
// water 600 of 5000, a neutral heat buffer.

class DZM_SpawnStats
{
	static void Apply(PlayerBase player)
	{
		if (!player || !player.GetPlayerStats())
			return;
`)
	set := func(stat string, v *float64) {
		if v != nil {
			fmt.Fprintf(&b, "\t\tplayer.%s().Set(player.%s().GetMax() * %s);\n", stat, stat, f3(clamp01(*v)))
		}
	}
	if s != nil {
		set("GetStatEnergy", s.Food)
		set("GetStatWater", s.Water)
		set("GetStatHeatBuffer", s.Warmth)
	}
	b.WriteString(`	}
}

modded class MissionServer
{
	override PlayerBase OnClientNewEvent(PlayerIdentity identity, vector pos, ParamsReadContext ctx)
	{
`)
	if chamber {
		b.WriteString(`		DZM_Chamber.s_Spawning = true;
		PlayerBase player = super.OnClientNewEvent(identity, pos, ctx);
		DZM_Chamber.s_Spawning = false;
`)
	} else {
		b.WriteString("\t\tPlayerBase player = super.OnClientNewEvent(identity, pos, ctx);\n")
	}
	b.WriteString(`		DZM_SpawnStats.Apply(player);
		return player;
	}
}
`)
	return b.String()
}

// ChamberClasses cleans the configured weapon classes for the script: lower
// case, trimmed, sorted, no duplicates and nothing that could break out of
// an Enforce string.
func ChamberClasses(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range list {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] || strings.ContainsAny(c, "\"\\\r\n") {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// ChamberScript is the generated 4_World script: the weapons a fresh
// character's spawn gear loads without a round in the chamber. The spawn-gear
// system loads a weapon with Weapon_Base.SpawnAmmo, whose default flag
// (WeaponWithAmmoFlags.CHAMBER) chambers a round; for these classes, and only
// while MissionServer equips a new character, the chamber flags are dropped —
// the magazine goes in full and the player chambers a round. A weapon that
// can only be loaded through its chamber spawns empty then.
func ChamberScript(classes []string) string {
	var b strings.Builder
	b.WriteString(`// Generated by DayZ Server Manager - the panel rewrites this file before
// every server start. Change it on its Player loadout page.
//
// Weapons a fresh character's spawn gear loads without a round in the
// chamber: the magazine goes in full, the player chambers a round.

class DZM_Chamber
{
	static bool s_Spawning;

	static bool Listed(string type)
	{
		type.ToLower();
`)
	for _, c := range classes {
		fmt.Fprintf(&b, "\t\tif (type == \"%s\") return true;\n", c)
	}
	b.WriteString(`		return false;
	}
}

modded class Weapon_Base
{
	override bool SpawnAmmo( string magazineType = "", int flags = WeaponWithAmmoFlags.CHAMBER )
	{
		if (DZM_Chamber.s_Spawning && DZM_Chamber.Listed(GetType()))
		{
			int dzmChamberFlags = WeaponWithAmmoFlags.CHAMBER | WeaponWithAmmoFlags.CHAMBER_RNG | WeaponWithAmmoFlags.CHAMBER_RNG_SPORADIC;
			flags &= ~dzmChamberFlags;
		}
		return super.SpawnAmmo(magazineType, flags);
	}
}
`)
	return b.String()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// f3 prints a float the way Enforce Script reads it ("0.5", "1.0").
func f3(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

// tweaksConfig is the mod's config.cpp with the script modules it carries:
// 4_World sees CarScript and the other vehicles, 5_Mission MissionServer.
func tweaksConfig(world, mission bool) string {
	var deps []string
	var defs strings.Builder
	module := func(name, folder string) {
		fmt.Fprintf(&defs, "\t\t\tclass %s\n\t\t\t{\n\t\t\t\tvalue = \"\";\n\t\t\t\tfiles[] = {\"DZM_ServerTweaks/Scripts/%s\"};\n\t\t\t};\n", name, folder)
	}
	if world {
		deps = append(deps, `"World"`)
		module("worldScriptModule", "4_World")
	}
	if mission {
		deps = append(deps, `"Mission"`)
		module("missionScriptModule", "5_Mission")
	}
	return `class CfgPatches
{
	class DZM_ServerTweaks
	{
		units[] = {};
		weapons[] = {};
		requiredVersion = 0.1;
		requiredAddons[] = {"DZ_Data", "DZ_Scripts"};
	};
};

class CfgMods
{
	class DZM_ServerTweaks
	{
		dir = "DZM_ServerTweaks";
		name = "DayZ Server Manager tweaks";
		author = "DayZ Server Manager";
		type = "mod";
		dependencies[] = {` + strings.Join(deps, ", ") + `};
		class defs
		{
` + defs.String() + `		};
	};
};
`
}

const tweaksModCpp = `name = "DayZ Server Manager tweaks";
author = "DayZ Server Manager";
overview = "Generated by DayZ Server Manager: vehicle fuel and radiator at spawn, a fresh character's food, water and warmth, and whether its weapons have a round in the chamber. Server-side only.";
`

const tweaksReadme = `This folder is generated by DayZ Server Manager and loaded as a server-side
mod (-serverMod). It sets how full the tank (and a car's radiator) is when the
economy spawns a vehicle - see the panel's Vehicles page - and how fed, watered
and warm a fresh character starts and whether its weapons have a round in the
chamber - see its Player loadout page. Players download nothing. The panel
rewrites it before every server start and removes it when nothing is set;
editing it by hand is pointless.
`
