// Copyright (c) 2026 Aristarh Ucolov.
package items

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{
		"BallisticHelmet_Green": "Ballistic Helmet Green",
		"Mag_STANAG_30Rnd":      "Mag STANAG 30Rnd",
		"M4A1":                  "M4A1",
		"AKM":                   "AKM",
		"WoolGlovesFingerless":  "Wool Gloves Fingerless",
		"TShirt_Beige":          "T Shirt Beige",
		"Black":                 "Black",
	} {
		if got := Humanize(in); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

// The real game data, when the development fixture (a full DayZ server) is
// next to the repository. Skipped elsewhere.
func TestCatalogFromGameFiles(t *testing.T) {
	dir, _ := filepath.Abs(filepath.Join("..", "..", "DayZServer"))
	if _, err := os.Stat(filepath.Join(dir, "addons")); err != nil {
		t.Skip("no DayZ server fixture")
	}
	d, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	en, ru := d.Localize("en"), d.Localize("ru")
	if len(en.Items) < 1500 {
		t.Fatalf("only %d items", len(en.Items))
	}
	find := func(c Catalog, class string) *Item {
		for i := range c.Items {
			if c.Items[i].Class == class {
				return &c.Items[i]
			}
		}
		t.Fatalf("%s missing from the catalog", class)
		return nil
	}
	checks := []struct {
		class, nameEN, nameRU, cat, slot string
	}{
		{"HuntingBag", "Hunter Backpack", "Рюкзак охотника", "clothing", "Back"},
		{"Hoodie_Black", "(Black)", "(Black)", "clothing", "Body"},
		{"BallisticHelmet_Green", "(Green)", "", "clothing", "Headgear"},
		{"M4A1", "M4", "", "weapons", "Shoulder"},
		{"Mag_STANAG_30Rnd", "Standardized Mag", "", "magazines", ""},
		{"Ammo_556x45", "5.56", "", "ammo", ""},
		{"BandageDressing", "Bandage", "", "medical", ""},
		{"Apple", "Apple", "", "food", ""},
		{"ACOGOptic", "ATOG", "", "attachments", ""},
		{"RGD5Grenade", "", "", "explosives", ""},
		{"Pliers", "", "", "tools", ""},
		{"HuntingKnife", "", "", "weapons", ""},
		{"MilitaryBelt", "", "", "clothing", "Hips"},
	}
	for _, c := range checks {
		ie, ir := find(en, c.class), find(ru, c.class)
		if c.nameEN != "" && !strings.Contains(ie.Name, c.nameEN) {
			t.Errorf("%s: English name %q lacks %q", c.class, ie.Name, c.nameEN)
		}
		if c.nameRU != "" && !strings.Contains(ir.Name, c.nameRU) {
			t.Errorf("%s: Russian name %q lacks %q", c.class, ir.Name, c.nameRU)
		}
		if ie.Cat != c.cat {
			t.Errorf("%s: category %q, want %q", c.class, ie.Cat, c.cat)
		}
		if c.slot != "" && !contains(ie.Slots, c.slot) {
			t.Errorf("%s: slots %v lack %s", c.class, ie.Slots, c.slot)
		}
	}
	if !find(en, "Rag").Qty || find(en, "Hoodie_Black").Qty {
		t.Error("quantity flags wrong: a rag stacks, a hoodie does not")
	}
	// Zombies, animals, buildings and cars are not items.
	for _, it := range en.Items {
		for _, p := range []string{"ZmbM_", "ZmbF_", "Animal_", "Land_", "OffroadHatchback", "SurvivorM_"} {
			if strings.HasPrefix(it.Class, p) && it.Class != "OffroadHatchbackWheel" {
				t.Errorf("%s is not an item", it.Class)
			}
		}
	}
	male, female := 0, 0
	for _, s := range en.Survivors {
		if s.Female {
			female++
		} else {
			male++
		}
	}
	if male != 20 || female != 11 {
		t.Errorf("survivors: %d male, %d female, want 20 and 11 (%v)", male, female, en.Survivors)
	}
}

// What goes on an item: a rifle's attachment slots with the parts that fit,
// its magazines (or rounds); survivors named after their class; and the copy
// on disk.
func TestItemDetailFromGameFiles(t *testing.T) {
	dir, _ := filepath.Abs(filepath.Join("..", "..", "DayZServer"))
	if _, err := os.Stat(filepath.Join(dir, "addons")); err != nil {
		t.Skip("no DayZ server fixture")
	}
	SetCacheDir(t.TempDir())
	defer SetCacheDir("")
	// Read the archives, not the copy an earlier test left in memory.
	cache.Lock()
	cache.m = nil
	cache.Unlock()
	d, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	det, ok := d.Detail("AKM", "en")
	if !ok {
		t.Fatal("AKM has no detail")
	}
	var slots []string
	fits := map[string]int{}
	for _, s := range det.Attach {
		slots = append(slots, s.Slot)
		fits[strings.ToLower(s.Slot)] = len(s.Items)
	}
	if len(det.Attach) < 3 {
		t.Fatalf("AKM attachment slots: %v", slots)
	}
	any := false
	for _, n := range fits {
		if n > 0 {
			any = true
		}
	}
	if !any {
		t.Errorf("nothing fits any AKM slot: %v", slots)
	}
	if len(det.Mags) == 0 {
		t.Error("the AKM has no magazines")
	}
	// A weapon with an internal magazine is loaded with rounds.
	if mosin, ok := d.Detail("Mosin9130", "en"); !ok || len(mosin.Mags) != 0 || len(mosin.Ammo) == 0 {
		t.Errorf("Mosin: mags %v, ammo %v", mosin.Mags, mosin.Ammo)
	}
	for _, s := range d.Localize("en").Survivors {
		if s.Name == "Survivor" || s.Name == "" {
			t.Errorf("survivor %s is named %q", s.Class, s.Name)
		}
	}
	// A car is described by its parts: each wheel slot takes the car's wheel.
	sedan, ok := d.Detail("Sedan_02", "en")
	if !ok || sedan.Cat != "vehicles" {
		t.Fatalf("Sedan_02: %+v %v", sedan, ok)
	}
	wheel := false
	for _, s := range sedan.Attach {
		if strings.EqualFold(s.Slot, "Sedan_02_Wheel_1_1") {
			for _, r := range s.Items {
				wheel = wheel || strings.EqualFold(r.Class, "Sedan_02_Wheel")
			}
		}
	}
	if !wheel {
		t.Errorf("Sedan_02's front wheel slot does not take Sedan_02_Wheel: %+v", sedan.Attach)
	}
	cat := d.Localize("en")
	named := false
	for _, v := range cat.Vehicles {
		named = named || (strings.EqualFold(v.Class, "Sedan_02") && v.Name != "" && v.Name != "Sedan 02")
	}
	if !named {
		t.Error("Sedan_02 has no in-game name in the catalog")
	}
	for _, it := range cat.Items {
		if it.Cat == "vehicles" {
			t.Fatalf("a vehicle is offered as an item: %s", it.Class)
		}
	}
	// The copy on disk reads back the same.
	files, _ := pboFiles(dir, nil)
	back := loadDisk(fingerprint(files))
	if back == nil {
		t.Fatal("no catalog copy on disk")
	}
	det2, ok := back.Detail("AKM", "en")
	if !ok || len(det2.Attach) != len(det.Attach) || len(det2.Mags) != len(det.Mags) {
		t.Errorf("disk copy differs: %+v", det2)
	}
	if s2, ok := back.Detail("Sedan_02", "en"); !ok || len(s2.Attach) != len(sedan.Attach) {
		t.Errorf("disk copy lost the vehicles: %+v", s2)
	}
}
