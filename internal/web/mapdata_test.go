// Copyright (c) 2026 Aristarh Ucolov.
package web

import "testing"

func TestWorldFromTemplate(t *testing.T) {
	cases := []struct {
		template string
		key      string
		name     string
		size     float64
		official bool
	}{
		// Official worlds — authoritative sizes, various template spellings.
		{"dayzOffline.chernarusplus", "chernarus", "Chernarus", 15360, true},
		{"regular.ChernarusPlus", "chernarus", "Chernarus", 15360, true},
		{"dayzOffline.enoch", "livonia", "Livonia", 12800, true},
		{"dayzOffline.livonia", "livonia", "Livonia", 12800, true},
		{"dayzOffline.sakhal", "sakhal", "Sakhal", 16384, true},
		// Community worlds — recognised by fuzzy alias; known sizes filled in.
		{"dayzOffline.deerisle", "deerisle", "Deer Isle", 16384, false},
		{"empty.chiemsee", "chiemsee", "Chiemsee", 10240, false},
		{"dayzOffline.rostow", "rostow", "Rostow", 14336, false},
		{"regular.Namalsk", "namalsk", "Namalsk", defaultWorldSize, false}, // size unknown → default
		{"dayzOffline.banov", "banov", "Banov", defaultWorldSize, false},
		{"custom.pripyat_winter", "pripyat", "Pripyat", defaultWorldSize, false}, // substring match
		// Variants of a listed world fold onto its base entry (fuzzy alias).
		{"regular.banovfrost", "banov", "Banov", defaultWorldSize, false},
		{"dayzOffline.takistanplus", "takistan", "Takistan", 12800, false},
		{"custom.pripyatgamma", "pripyat", "Pripyat", defaultWorldSize, false},
		{"dayzOffline.deadfall", "deadfall", "Deadfall", defaultWorldSize, false},
		{"empty.sahrani", "sahrani", "Sahrani", defaultWorldSize, false},
		// Unknown/custom world — keyed by its own token, its own slot.
		{"dayzOffline.someRandomMap", "somerandommap", "someRandomMap", defaultWorldSize, false},
	}
	for _, c := range cases {
		mi := worldFromTemplate(c.template)
		if mi.Key != c.key || mi.Name != c.name || mi.Size != c.size || mi.Official != c.official {
			t.Errorf("worldFromTemplate(%q) = {key:%q name:%q size:%v official:%v}, want {key:%q name:%q size:%v official:%v}",
				c.template, mi.Key, mi.Name, mi.Size, mi.Official, c.key, c.name, c.size, c.official)
		}
	}
}

func TestImageExtFor(t *testing.T) {
	cases := []struct {
		ct, urlPath, want string
	}{
		{"image/png", "/x", ".png"},
		{"image/webp; charset=binary", "/x", ".webp"},
		{"image/jpeg", "/x.jpg", ".jpg"},
		{"", "/maps/chernarus.webp", ".webp"},
		{"", "/maps/foo.jpeg", ".jpg"},
		{"text/html", "/not-an-image", ""},
	}
	for _, c := range cases {
		if got := imageExtFor(c.ct, c.urlPath, nil); got != c.want {
			t.Errorf("imageExtFor(%q,%q) = %q, want %q", c.ct, c.urlPath, got, c.want)
		}
	}
}
