// Copyright (c) 2026 Aristarh Ucolov.

package web

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // uploaded pictures may be PNG
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The map picture made small for a mission card: the full picture (a drawn
// map is 3 MB) is shrunk once and kept next to the other renders.

const thumbPx = 480

var thumbMu sync.Mutex

// mapThumbFile returns a small JPEG of the picture at src (cached by its
// time), or "" when it cannot be made (a WebP upload: no decoder here).
func (h *handlers) mapThumbFile(key, src string) string {
	st, err := os.Stat(src)
	if err != nil {
		return ""
	}
	dir := filepath.Join(h.mapImageDir(), "cache")
	out := filepath.Join(dir, fmt.Sprintf("%s-thumb-%d.jpg", sanitizeKey(key), st.ModTime().Unix()))
	thumbMu.Lock()
	defer thumbMu.Unlock()
	if fileExists(out) {
		return out
	}
	if strings.EqualFold(filepath.Ext(src), ".webp") {
		return ""
	}
	f, err := os.Open(src)
	if err != nil {
		return ""
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return ""
	}
	small := shrink(img, thumbPx)
	var buf bytes.Buffer
	if jpeg.Encode(&buf, small, &jpeg.Options{Quality: 82}) != nil {
		return ""
	}
	_ = os.MkdirAll(dir, 0o755)
	if old, _ := filepath.Glob(filepath.Join(dir, sanitizeKey(key)+"-thumb-*.jpg")); old != nil {
		for _, o := range old {
			_ = os.Remove(o)
		}
	}
	if writeFileAtomic(out, buf.Bytes()) != nil {
		return ""
	}
	return out
}

// shrink scales img to px on its longer side, averaging the pixels each new
// one covers.
func shrink(img image.Image, px int) *image.RGBA {
	b := img.Bounds()
	w, hgt := b.Dx(), b.Dy()
	if w <= 0 || hgt <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	ow, oh := px, px*hgt/w
	if hgt > w {
		ow, oh = px*w/hgt, px
	}
	ow, oh = max(ow, 1), max(oh, 1)
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		y0, y1 := b.Min.Y+y*hgt/oh, b.Min.Y+(y+1)*hgt/oh
		for x := 0; x < ow; x++ {
			x0, x1 := b.Min.X+x*w/ow, b.Min.X+(x+1)*w/ow
			var r, g, bl, n uint64
			// Every other pixel of the block is plenty for a thumbnail.
			for yy := y0; yy < max(y1, y0+1); yy += 2 {
				for xx := x0; xx < max(x1, x0+1); xx += 2 {
					cr, cg, cb, _ := img.At(xx, yy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			o := y*out.Stride + x*4
			out.Pix[o] = uint8(r / n >> 8)
			out.Pix[o+1] = uint8(g / n >> 8)
			out.Pix[o+2] = uint8(bl / n >> 8)
			out.Pix[o+3] = 255
		}
	}
	return out
}
