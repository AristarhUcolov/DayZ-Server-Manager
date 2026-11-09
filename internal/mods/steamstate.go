// Copyright (c) 2026 Aristarh Ucolov.
//
// Is the server's copy of a Workshop mod the current version?
//
// Time stamps cannot tell: Steam's time_updated moves when the author only
// edits the description or the picture, and meta.cpp's timestamp is written
// when the author packs the mod, minutes or hours before the upload ends. So
// a mod that was up to date showed as outdated. The content id can tell:
// Steam names each version of an item's files by a manifest (hcontent_file),
// Steam and SteamCMD note the manifest of what they downloaded in
// steamapps/workshop/appworkshop_221100.acf, and the panel notes the
// manifest it copied in a small file inside the server's mod folder.
package mods

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/vdf"
)

// ACFItem is one downloaded item in Steam's workshop manifest.
type ACFItem struct {
	Manifest    string
	TimeUpdated int64
	Size        int64
}

// ReadWorkshopACF reads appworkshop_221100.acf: item id → what is on disk.
func ReadWorkshopACF(path string) map[string]ACFItem {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	inst := vdf.Block(vdf.Parse(string(data)), "AppWorkshop", "WorkshopItemsInstalled")
	out := map[string]ACFItem{}
	for id, v := range inst {
		kv, _ := v.(map[string]interface{})
		if kv == nil || !isNumeric(id) {
			continue
		}
		it := ACFItem{Manifest: vdf.String(kv, "manifest")}
		it.TimeUpdated, _ = strconv.ParseInt(vdf.String(kv, "timeupdated"), 10, 64)
		it.Size, _ = strconv.ParseInt(vdf.String(kv, "size"), 10, 64)
		if it.Manifest == "0" {
			it.Manifest = ""
		}
		out[id] = it
	}
	return out
}

// WorkshopACFFor finds the Steam manifest that describes a mod's source
// copy: a SteamCMD or Steam library download (…/steamapps/workshop/content/
// 221100/<id>, also behind the DayZ Launcher's !Workshop junctions). "" for
// any other folder.
func WorkshopACFFor(modPath string) string {
	if modPath == "" {
		return ""
	}
	p := filepath.Clean(resolveSymlink(modPath))
	app := filepath.Dir(p) // …/content/221100
	if !strings.EqualFold(filepath.Base(app), "221100") || !strings.EqualFold(filepath.Base(filepath.Dir(app)), "content") {
		return ""
	}
	acf := filepath.Join(filepath.Dir(filepath.Dir(app)), "appworkshop_221100.acf")
	if _, err := os.Stat(acf); err != nil {
		return ""
	}
	return acf
}

// SourceACF is what Steam's manifest says about the source copy of a mod
// (its id from the folder or meta.cpp); ok false when nothing is known.
func SourceACF(modPath, id string) (ACFItem, bool) {
	acf := WorkshopACFFor(modPath)
	if acf == "" || id == "" {
		return ACFItem{}, false
	}
	it, ok := ReadWorkshopACF(acf)[id]
	return it, ok && (it.Manifest != "" || it.TimeUpdated > 0)
}

// markerName is the panel's note inside a server mod folder of where the
// copy came from. DayZ loads only addons/*.pbo, so the file is inert.
const markerName = ".dzm-workshop.json"

// Marker is that note.
type Marker struct {
	PublishedID string `json:"publishedId"`
	Manifest    string `json:"manifest,omitempty"`
	TimeUpdated int64  `json:"timeUpdated,omitempty"`
	InstalledAt int64  `json:"installedAt"`
}

// ReadMarker reads the note in a server mod folder (zero when absent).
func ReadMarker(modDir string) Marker {
	var m Marker
	if modDir == "" {
		return m
	}
	if data, err := os.ReadFile(filepath.Join(modDir, markerName)); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// recordSource notes, after a copy from src into dst, which Workshop version
// was copied — when Steam's manifest knows it.
func recordSource(dst, src string) {
	id := PublishedIDOf(src)
	if id == "" {
		return
	}
	it, ok := SourceACF(src, id)
	mk := Marker{PublishedID: id, InstalledAt: time.Now().Unix()}
	if ok {
		mk.Manifest, mk.TimeUpdated = it.Manifest, it.TimeUpdated
	}
	if data, err := json.Marshal(mk); err == nil {
		_ = os.WriteFile(filepath.Join(dst, markerName), data, 0o644)
	}
}

// PublishedIDOf is a mod folder's Workshop id: meta.cpp's publishedid, or a
// SteamCMD download folder's name.
func PublishedIDOf(dir string) string {
	if id := readMeta(dir).PublishedID; isNumeric(id) && id != "0" {
		return id
	}
	if base := filepath.Base(resolveSymlink(dir)); isNumeric(base) && len(base) >= 6 {
		return base
	}
	if mk := ReadMarker(dir); isNumeric(mk.PublishedID) {
		return mk.PublishedID
	}
	return ""
}

// Steam statuses of a server's Workshop mod.
const (
	SteamOK       = "ok"       // the server runs the current version
	SteamUpdate   = "update"   // the source copy is current, the server's older: copy it
	SteamOutdated = "outdated" // Steam has a newer version than what was downloaded
	SteamMissing  = "missing"  // removed from the Workshop or private
	SteamUnknown  = "unknown"
)

// SteamState compares one installed mod with the Workshop.
type SteamState struct {
	Status string
	Exact  bool      // decided by content ids, not by time
	Local  time.Time // the version the server copy is known to be
}

// CompareWithSteam decides SteamState for m (from List with the mod source)
// against Steam's details r.
func CompareWithSteam(m Mod, r WorkshopRemote) SteamState {
	if r.Result != 1 {
		return SteamState{Status: SteamMissing}
	}
	mk := ReadMarker(m.ServerPath)
	src, srcOK := ACFItem{}, false
	if m.WorkshopPath != "" {
		src, srcOK = SourceACF(m.WorkshopPath, r.PublishedID)
	}
	srcCurrent := srcOK && r.Manifest != "" && src.Manifest == r.Manifest
	if mk.Manifest != "" && r.Manifest != "" {
		st := SteamState{Exact: true, Local: unixTime(mk.TimeUpdated)}
		switch {
		case mk.Manifest == r.Manifest:
			st.Status = SteamOK
		case srcCurrent:
			st.Status = SteamUpdate
		default:
			st.Status = SteamOutdated
		}
		return st
	}
	if srcOK && src.Manifest != "" && r.Manifest != "" {
		st := SteamState{Exact: true, Local: unixTime(src.TimeUpdated)}
		switch {
		case !srcCurrent:
			st.Status = SteamOutdated
		case m.UpdateAvailable:
			st.Status = SteamUpdate
		default:
			st.Status = SteamOK // the server copy is the source copy
		}
		return st
	}
	// Nothing exact known (a mod copied by hand): fall back on time, with an
	// hour of slack for the upload of a big mod.
	local := MetaTime(m.MetaTimestamp)
	if local.IsZero() {
		local = m.ServerModifiedAt
	}
	if r.TimeUpdated.IsZero() || local.IsZero() {
		return SteamState{Status: SteamUnknown, Local: local}
	}
	if r.TimeUpdated.After(local.Add(time.Hour)) {
		return SteamState{Status: SteamOutdated, Local: local}
	}
	return SteamState{Status: SteamOK, Local: local}
}

func unixTime(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}
