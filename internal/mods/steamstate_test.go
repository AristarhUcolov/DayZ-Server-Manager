// Copyright (c) 2026 Aristarh Ucolov.
package mods

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testACF = `"AppWorkshop"
{
	"appid"		"221100"
	"WorkshopItemsInstalled"
	{
		"1559212036"
		{
			"size"		"1234"
			"timeupdated"		"1700000000"
			"manifest"		"111"
		}
		"1564026768"
		{
			"size"		"99"
			"timeupdated"		"1700000500"
			"manifest"		"222"
		}
	}
	"WorkshopItemDetails"
	{
		"1559212036"
		{
			"manifest"		"111"
		}
	}
}
`

// steamLibrary lays out a SteamCMD download folder with two items and their
// appworkshop manifest; it returns the content folder.
func steamLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	content := filepath.Join(root, "steamapps", "workshop", "content", "221100")
	for id, name := range map[string]string{"1559212036": "CF", "1564026768": "Community-Online-Tools"} {
		dir := filepath.Join(content, id)
		os.MkdirAll(filepath.Join(dir, "addons"), 0o755)
		os.WriteFile(filepath.Join(dir, "addons", "x.pbo"), []byte(id), 0o644)
		os.WriteFile(filepath.Join(dir, "meta.cpp"), []byte("protocol = 1;\nname = \""+name+"\";\n"), 0o644)
	}
	os.WriteFile(filepath.Join(root, "steamapps", "workshop", "appworkshop_221100.acf"), []byte(testACF), 0o644)
	return content
}

func TestReadWorkshopACF(t *testing.T) {
	content := steamLibrary(t)
	acf := WorkshopACFFor(filepath.Join(content, "1559212036"))
	if acf == "" {
		t.Fatal("the manifest next to a SteamCMD download was not found")
	}
	items := ReadWorkshopACF(acf)
	if it := items["1559212036"]; it.Manifest != "111" || it.TimeUpdated != 1700000000 || it.Size != 1234 {
		t.Errorf("CF = %+v", it)
	}
	if len(items) != 2 {
		t.Errorf("items = %v", items)
	}
	if WorkshopACFFor(t.TempDir()) != "" {
		t.Error("a plain folder has no Steam manifest")
	}
}

// A SteamCMD download installs into the server under the name it is given,
// and the server copy notes the Steam version it is.
func TestInstallFromWritesMarker(t *testing.T) {
	content := steamLibrary(t)
	server := t.TempDir()
	if err := InstallFrom(server, filepath.Join(content, "1564026768"), "@COT"); err != nil {
		t.Fatal(err)
	}
	mk := ReadMarker(filepath.Join(server, "@COT"))
	if mk.PublishedID != "1564026768" || mk.Manifest != "222" || mk.TimeUpdated != 1700000500 {
		t.Fatalf("marker = %+v", mk)
	}
	// meta.cpp of the download has no publishedid: List still knows the id.
	list, _ := List(server, "")
	if len(list) != 1 || list[0].PublishedID != "1564026768" {
		t.Fatalf("list = %+v", list)
	}
	// Updating over it swaps the copy and keeps the note current.
	if err := InstallFrom(server, filepath.Join(content, "1564026768"), "@COT"); err != nil {
		t.Fatal(err)
	}
	if ReadMarker(filepath.Join(server, "@COT")).Manifest != "222" {
		t.Error("marker lost on update")
	}
}

// The comparison trusts content ids over times: a description edit on Steam
// (newer time, same manifest) is not an update.
func TestCompareWithSteam(t *testing.T) {
	content := steamLibrary(t)
	server := t.TempDir()
	if err := InstallFrom(server, filepath.Join(content, "1559212036"), "@CF"); err != nil {
		t.Fatal(err)
	}
	m := Mod{Name: "@CF", InstalledInServer: true, ServerPath: filepath.Join(server, "@CF"), PublishedID: "1559212036"}
	later := time.Unix(1700000000, 0).Add(48 * time.Hour)

	if st := CompareWithSteam(m, WorkshopRemote{PublishedID: "1559212036", Result: 1, Manifest: "111", TimeUpdated: later}); st.Status != SteamOK || !st.Exact {
		t.Errorf("same manifest, later time = %+v, want ok", st)
	}
	if st := CompareWithSteam(m, WorkshopRemote{PublishedID: "1559212036", Result: 1, Manifest: "999", TimeUpdated: later}); st.Status != SteamOutdated {
		t.Errorf("new manifest = %+v, want outdated", st)
	}
	if st := CompareWithSteam(m, WorkshopRemote{PublishedID: "1559212036", Result: 9}); st.Status != SteamMissing {
		t.Errorf("removed item = %+v", st)
	}

	// The download is newer than the server copy: copy it, no download.
	os.Remove(filepath.Join(server, "@CF", markerName))
	acf := filepath.Join(filepath.Dir(filepath.Dir(content)), "appworkshop_221100.acf")
	os.WriteFile(acf, []byte(strings.Replace(testACF, `"111"`, `"333"`, 1)), 0o644)
	m.WorkshopPath = filepath.Join(content, "1559212036")
	m.UpdateAvailable = true
	if st := CompareWithSteam(m, WorkshopRemote{PublishedID: "1559212036", Result: 1, Manifest: "333", TimeUpdated: later}); st.Status != SteamUpdate {
		t.Errorf("source current, server older = %+v, want update", st)
	}
	m.UpdateAvailable = false
	if st := CompareWithSteam(m, WorkshopRemote{PublishedID: "1559212036", Result: 1, Manifest: "333", TimeUpdated: later}); st.Status != SteamOK {
		t.Errorf("server copy = source copy = Steam: %+v, want ok", st)
	}

	// Nothing exact known: time, with an hour of slack for the upload.
	hand := Mod{Name: "@X", InstalledInServer: true, ServerPath: t.TempDir(), ServerModifiedAt: time.Unix(1700000000, 0)}
	if st := CompareWithSteam(hand, WorkshopRemote{Result: 1, TimeUpdated: time.Unix(1700000000+1800, 0)}); st.Status != SteamOK {
		t.Errorf("30 min later = %+v, want ok", st)
	}
	if st := CompareWithSteam(hand, WorkshopRemote{Result: 1, TimeUpdated: time.Unix(1700000000+7200, 0)}); st.Status != SteamOutdated {
		t.Errorf("2 h later = %+v, want outdated", st)
	}
}

// Only the page's Required items box counts — not the author's other mods or
// the popular items the page also links.
func TestRequiredItemsIn(t *testing.T) {
	page := `<a href="https://steamcommunity.com/sharedfiles/filedetails/?id=111111">Popular</a>
<div class="rightSectionTopTitle condensed">Required items</div>
<div class="requiredItemsContainer" id="RequiredItems">
  <a href="https://steamcommunity.com/workshop/filedetails/?id=1559212036" target="_blank"><div class="requiredItem">CF</div></a>
  <a href="https://steamcommunity.com/workshop/filedetails/?id=2545327648"><div class="requiredItem">Dabs</div></a>
</div>
</div>
<!-- created by -->
<a href="https://steamcommunity.com/sharedfiles/filedetails/?id=222222">More from the author</a>`
	got := requiredItemsIn(page, "1564026768")
	if strings.Join(got, ",") != "1559212036,2545327648" {
		t.Errorf("required = %v", got)
	}
	if requiredItemsIn(`<a href="/sharedfiles/filedetails/?id=123456">x</a>`, "1") != nil {
		t.Error("a page without the box has no required items")
	}
}
