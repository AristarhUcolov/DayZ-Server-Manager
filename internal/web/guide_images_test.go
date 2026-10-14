// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"io/fs"
	"strings"
	"testing"

	"dayzmanager/internal/guide"
	"dayzmanager/internal/i18n"
)

// Every guide chapter's screenshot is in the binary for every language — a
// new chapter used to ship pointing at images that did not exist (404s on the
// Guide page).
func TestGuideImagesExist(t *testing.T) {
	for _, code := range i18n.Supported() {
		for _, ch := range guide.Get(code) {
			if ch.Image == "" {
				continue
			}
			p := "static" + ch.Image
			if _, err := fs.Stat(staticFS, p); err != nil {
				t.Errorf("%s: chapter %q points to %s, which is not embedded", code, ch.ID, strings.TrimPrefix(p, "static"))
			}
		}
	}
}
