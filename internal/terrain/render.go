// Copyright (c) 2026 Aristarh Ucolov.

package terrain

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
)

// RenderSize picks the picture size for a world: about 3.75 m a pixel
// (Chernarus: 4096), between 2048 and 4096 pixels.
func RenderSize(worldSize float64) int {
	n := int(math.Round(worldSize / 3.75))
	if n < 2048 {
		n = 2048
	}
	if n > 4096 {
		n = 4096
	}
	return n
}

type rgb struct{ r, g, b float64 }

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

// Land tints by height (a topographic map's greens to browns).
var landStops = []struct {
	h float64
	c rgb
}{
	{0, rgb{223, 233, 201}},
	{60, rgb{230, 235, 203}},
	{160, rgb{238, 235, 204}},
	{300, rgb{237, 225, 196}},
	{450, rgb{228, 211, 184}},
	{650, rgb{216, 199, 177}},
	{1200, rgb{232, 228, 224}},
}

func landTint(h float64) rgb {
	if h <= landStops[0].h {
		return landStops[0].c
	}
	for i := 1; i < len(landStops); i++ {
		if h <= landStops[i].h {
			a, b := landStops[i-1], landStops[i]
			return mix(a.c, b.c, (h-a.h)/(b.h-a.h))
		}
	}
	return landStops[len(landStops)-1].c
}

var (
	seaShallow  = rgb{178, 214, 232}
	seaDeep     = rgb{108, 160, 200}
	lakeColor   = rgb{160, 203, 228}
	forestColor = rgb{168, 204, 146}
	shoreColor  = rgb{72, 118, 158}
	contourCol  = rgb{150, 118, 80}
)

func smoothstep(a, b, x float64) float64 {
	if x <= a {
		return 0
	}
	if x >= b {
		return 1
	}
	t := (x - a) / (b - a)
	return t * t * (3 - 2*t)
}

// Render draws the world as a topographic map of size×size pixels, north up.
func (w *World) Render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	n := w.GridN
	mpp := w.Size / float64(size) // metres per pixel

	// Light from the north-west, shading precomputed per grid point.
	// slope: height change per metre, for the line widths.
	shade := make([]float32, n*n)
	slope := make([]float32, n*n)
	const exag = 1.6
	lx, lz, ly := -0.5, 0.5, math.Sqrt(0.5)
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			im, ip := max(i-1, 0), min(i+1, n-1)
			jm, jp := max(j-1, 0), min(j+1, n-1)
			gx := float64(w.Heights[j*n+ip]-w.Heights[j*n+im]) / (float64(ip-im) * w.Cell)
			gz := float64(w.Heights[jp*n+i]-w.Heights[jm*n+i]) / (float64(jp-jm) * w.Cell)
			slope[j*n+i] = float32(math.Hypot(gx, gz))
			dx, dz := gx*exag, gz*exag
			l := math.Sqrt(dx*dx + dz*dz + 1)
			s := (-dx*lx - dz*lz + ly) / l / ly // 1 on flat ground
			shade[j*n+i] = float32(s)
		}
	}
	// Forest density: the tree counts smoothed over ~3 cells.
	dens := make([]float32, n*n)
	for i, c := range w.Trees {
		dens[i] = float32(c)
	}
	dens = boxBlur(dens, n, 1)
	dens = boxBlur(dens, n, 1)
	// Water surfaces (ponds, rivers, streams) per pixel: which surface covers
	// it (the highest when several do); the water shows where the ground is
	// below it, as in the game.
	waterAt := make([]uint16, size*size)
	w.settleBakedWater()
	for k, wt := range w.Water {
		if k >= math.MaxUint16 {
			break
		}
		var q [4][2]float64
		for c, p := range wt.Corners {
			q[c] = [2]float64{float64(p[0]) / mpp, (w.Size - float64(p[1])) / mpp}
		}
		rasterPixels(q, size, func(x, y int) {
			o := y*size + x
			if cur := waterAt[o]; cur == 0 || wt.Level > w.Water[cur-1].Level {
				waterAt[o] = uint16(k + 1)
			}
		})
	}
	// Forest strength for the tree count of a ~7 m cell.
	forestA := func(d float64) float64 {
		k := w.Cell * w.Cell / 56.25 // counts are per cell; scale to 7.5 m cells
		return smoothstep(0.22*k, 0.85*k, d)
	}
	minor := 10.0
	if mpp > 5 {
		minor = 20
	}
	major := minor * 5

	sample := func(arr []float32, gx, gz float64) float64 {
		if gx < 0 {
			gx = 0
		}
		if gz < 0 {
			gz = 0
		}
		if gx > float64(n-1) {
			gx = float64(n - 1)
		}
		if gz > float64(n-1) {
			gz = float64(n - 1)
		}
		x0, z0 := int(gx), int(gz)
		x1, z1 := min(x0+1, n-1), min(z0+1, n-1)
		fx, fz := gx-float64(x0), gz-float64(z0)
		a := float64(arr[z0*n+x0])*(1-fx) + float64(arr[z0*n+x1])*fx
		b := float64(arr[z1*n+x0])*(1-fx) + float64(arr[z1*n+x1])*fx
		return a*(1-fz) + b*fz
	}

	rows := make(chan int, size)
	for y := 0; y < size; y++ {
		rows <- y
	}
	close(rows)
	var wg sync.WaitGroup
	for k := 0; k < runtime.NumCPU(); k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for py := range rows {
				z := w.Size - (float64(py)+0.5)*mpp
				gz := z / w.Cell
				for px := 0; px < size; px++ {
					x := (float64(px) + 0.5) * mpp
					gx := x / w.Cell
					h := sample(w.Heights, gx, gz)
					grad := sample(slope, gx, gz) * mpp // height change over a pixel
					s := sample(shade, gx, gz)
					lv := math.NaN()
					if k := waterAt[py*size+px]; k > 0 {
						lv = float64(w.Water[k-1].Level)
					}
					var c rgb
					switch {
					case h < 0 && !(lv == lv && lv > 0):
						// The sea: deeper is darker, the shore a line.
						c = mix(seaShallow, seaDeep, smoothstep(0, 60, -h))
						c = shadeBy(c, 1+(s-1)*0.12)
						if a := lineAlpha(h, 0, grad, 0.75); a > 0 {
							c = mix(c, shoreColor, a*0.8)
						}
					case lv == lv && h < lv+grad*0.5:
						// Ponds, rivers and streams; a stream narrower than a
						// pixel still shows, fainter.
						land := shadeBy(landTint(h), math.Max(0.55, math.Min(1.18, 1+(s-1)*0.55)))
						wc := mix(lakeColor, seaDeep, smoothstep(0, 12, lv-h)*0.5)
						a := 1.0
						if grad > 0 {
							a = math.Max(0.35, math.Min(1, (lv-h)/grad+0.5))
						}
						c = mix(land, wc, a)
						if la := lineAlpha(h, lv, grad, 0.6); la > 0 {
							c = mix(c, shoreColor, la*0.55)
						}
					default:
						c = landTint(h)
						if a := forestA(sample(dens, gx, gz)); a > 0 {
							c = mix(c, forestColor, a*0.85)
						}
						mult := 1 + (s-1)*0.55
						c = shadeBy(c, math.Max(0.55, math.Min(1.18, mult)))
						// Contour lines: index lines darker.
						if grad > 0.004 {
							lm := math.Round(h/major) * major
							if a := lineAlpha(h, lm, grad, 0.6); a > 0 && lm > 0 {
								c = mix(c, contourCol, a*0.42)
							} else {
								ln := math.Round(h/minor) * minor
								if a := lineAlpha(h, ln, grad, 0.45); a > 0 && ln > 0 {
									c = mix(c, contourCol, a*0.22)
								}
							}
						}
						// The coast line on the land side too.
						if a := lineAlpha(h, 0, grad, 0.75); a > 0 {
							c = mix(c, shoreColor, a*0.8)
						}
					}
					o := py*img.Stride + px*4
					img.Pix[o] = clamp8(c.r)
					img.Pix[o+1] = clamp8(c.g)
					img.Pix[o+2] = clamp8(c.b)
					img.Pix[o+3] = 255
				}
			}
		}()
	}
	wg.Wait()

	// Map data on top, in a map's order: tracks, roads, rail, walls,
	// buildings.
	toPx := func(p [2]float32) [2]float64 {
		return [2]float64{float64(p[0]) / mpp, (w.Size - float64(p[1])) / mpp}
	}
	quadPx := func(q Quad) [4][2]float64 {
		return [4][2]float64{toPx(q[0]), toPx(q[1]), toPx(q[2]), toPx(q[3])}
	}
	type layer struct {
		quads  []Quad
		fill   color.RGBA
		margin float64 // pixels added round the shape (a casing)
	}
	draw := func(ls ...layer) {
		for _, l := range ls {
			for _, q := range l.quads {
				fillQuad(img, quadPx(q), l.fill, l.margin)
			}
		}
	}
	draw(
		layer{w.Roads[RoadTrack], color.RGBA{176, 140, 96, 210}, 0.15},
		layer{w.Roads[RoadPaved], color.RGBA{120, 104, 84, 230}, 0.7},
		layer{w.Roads[RoadMain], color.RGBA{150, 84, 36, 240}, 0.8},
		layer{w.Roads[RoadPaved], color.RGBA{255, 251, 240, 255}, 0},
		layer{w.Roads[RoadMain], color.RGBA{247, 178, 92, 255}, 0},
	)
	for _, l := range w.Power {
		strokeLine(img, l, toPx, 0.35, color.RGBA{110, 110, 120, 120})
	}
	for _, l := range w.Rail {
		strokeLine(img, l, toPx, 1.1, color.RGBA{60, 60, 66, 230})
	}
	draw(layer{w.Buildings[BuildWall], color.RGBA{120, 116, 110, 150}, 0})
	outline := color.RGBA{86, 72, 62, 235}
	draw(
		layer{w.Buildings[BuildHouse], outline, 0.55},
		layer{w.Buildings[BuildIndustry], color.RGBA{78, 84, 96, 235}, 0.55},
		layer{w.Buildings[BuildChurch], outline, 0.55},
		layer{w.Buildings[BuildLandmark], outline, 0.55},
		layer{w.Buildings[BuildHouse], color.RGBA{176, 150, 128, 255}, 0},
		layer{w.Buildings[BuildIndustry], color.RGBA{158, 166, 178, 255}, 0},
		layer{w.Buildings[BuildChurch], color.RGBA{150, 112, 150, 255}, 0},
		layer{w.Buildings[BuildLandmark], color.RGBA{214, 112, 104, 255}, 0},
	)
	for _, r := range w.Rocks {
		p := toPx([2]float32{r.X, r.Z})
		fillDot(img, p, 1.3, color.RGBA{120, 116, 112, 160})
	}
	return img
}

func shadeBy(c rgb, k float64) rgb { return rgb{c.r * k, c.g * k, c.b * k} }

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// lineAlpha is the coverage of an iso-line at level for a point of height h
// on ground changing grad metres a pixel: a line half a pixel wide (width
// in pixels), anti-aliased.
func lineAlpha(h, level, grad, width float64) float64 {
	if grad <= 0 {
		return 0
	}
	d := math.Abs(h-level) / grad // pixels to the line
	return math.Max(0, math.Min(1, width+0.5-d))
}

// boxBlur averages each point with its neighbours r points away.
func boxBlur(in []float32, n, r int) []float32 {
	tmp := make([]float32, n*n)
	out := make([]float32, n*n)
	k := float32(2*r + 1)
	for j := 0; j < n; j++ {
		row := in[j*n : j*n+n]
		for i := 0; i < n; i++ {
			var s float32
			for d := -r; d <= r; d++ {
				s += row[min(max(i+d, 0), n-1)]
			}
			tmp[j*n+i] = s / k
		}
	}
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			var s float32
			for d := -r; d <= r; d++ {
				s += tmp[min(max(j+d, 0), n-1)*n+i]
			}
			out[j*n+i] = s / k
		}
	}
	return out
}

// rasterPixels calls fn for every pixel whose centre is inside the quad
// (pixel coordinates), grown by half a pixel so neighbouring pieces meet.
func rasterPixels(p [4][2]float64, size int, fn func(x, y int)) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, v := range p {
		minX, maxX = math.Min(minX, v[0]), math.Max(maxX, v[0])
		minY, maxY = math.Min(minY, v[1]), math.Max(maxY, v[1])
	}
	x0, x1 := max(int(math.Floor(minX)), 0), min(int(math.Ceil(maxX)), size-1)
	y0, y1 := max(int(math.Floor(minY)), 0), min(int(math.Ceil(maxY)), size-1)
	if x1 < x0 || y1 < y0 || x1-x0 > 1500 || y1-y0 > 1500 {
		return // off the map, or not a water piece
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if insideQuad(p, float64(x)+0.5, float64(y)+0.5, 0.5) {
				fn(x, y)
			}
		}
	}
}

// insideQuad tests a point against a convex quad, with a margin (units of the
// quad) added round it.
func insideQuad(p [4][2]float64, x, y, margin float64) bool {
	area := 0.0
	for k := 0; k < 4; k++ {
		a, b := p[k], p[(k+1)%4]
		area += a[0]*b[1] - b[0]*a[1]
	}
	if math.Abs(area) < 1e-9 {
		return false
	}
	sign := 1.0
	if area < 0 {
		sign = -1
	}
	for k := 0; k < 4; k++ {
		a, b := p[k], p[(k+1)%4]
		ex, ey := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(ex, ey)
		if l < 1e-9 {
			continue
		}
		// Signed distance to the edge's line, positive inside.
		d := sign * (ex*(y-a[1]) - ey*(x-a[0])) / l
		if d < -margin {
			return false
		}
	}
	return true
}

// fillQuad paints a quad (pixel coordinates) with 4×4 samples a pixel, grown
// by margin pixels.
func fillQuad(img *image.RGBA, p [4][2]float64, c color.RGBA, margin float64) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, v := range p {
		minX, maxX = math.Min(minX, v[0]), math.Max(maxX, v[0])
		minY, maxY = math.Min(minY, v[1]), math.Max(maxY, v[1])
	}
	b := img.Rect
	x0 := max(int(math.Floor(minX-margin-1)), b.Min.X)
	x1 := min(int(math.Ceil(maxX+margin+1)), b.Max.X-1)
	y0 := max(int(math.Floor(minY-margin-1)), b.Min.Y)
	y1 := min(int(math.Ceil(maxY+margin+1)), b.Max.Y-1)
	if x1 < x0 || y1 < y0 || x1-x0 > 2000 || y1-y0 > 2000 {
		return
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			hit := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					if insideQuad(p, float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4, margin) {
						hit++
					}
				}
			}
			if hit > 0 {
				blend(img, x, y, c, float64(hit)/16)
			}
		}
	}
}

func blend(img *image.RGBA, x, y int, c color.RGBA, cover float64) {
	a := cover * float64(c.A) / 255
	o := y*img.Stride + x*4
	img.Pix[o] = uint8(float64(img.Pix[o])*(1-a) + float64(c.R)*a + 0.5)
	img.Pix[o+1] = uint8(float64(img.Pix[o+1])*(1-a) + float64(c.G)*a + 0.5)
	img.Pix[o+2] = uint8(float64(img.Pix[o+2])*(1-a) + float64(c.B)*a + 0.5)
}

// strokeLine draws a polyline of map points half-width pixels wide.
func strokeLine(img *image.RGBA, l Line, toPx func([2]float32) [2]float64, half float64, c color.RGBA) {
	for i := 0; i+1 < len(l); i++ {
		a, b := toPx(l[i]), toPx(l[i+1])
		dx, dy := b[0]-a[0], b[1]-a[1]
		ln := math.Hypot(dx, dy)
		if ln < 1e-6 || ln > 3000 {
			continue
		}
		nx, ny := -dy/ln*half, dx/ln*half
		fillQuad(img, [4][2]float64{{a[0] + nx, a[1] + ny}, {b[0] + nx, b[1] + ny}, {b[0] - nx, b[1] - ny}, {a[0] - nx, a[1] - ny}}, c, 0.2)
	}
}

func fillDot(img *image.RGBA, p [2]float64, r float64, c color.RGBA) {
	b := img.Rect
	for y := max(int(p[1]-r-1), b.Min.Y); y <= min(int(p[1]+r+1), b.Max.Y-1); y++ {
		for x := max(int(p[0]-r-1), b.Min.X); x <= min(int(p[0]+r+1), b.Max.X-1); x++ {
			d := math.Hypot(float64(x)+0.5-p[0], float64(y)+0.5-p[1])
			if a := math.Max(0, math.Min(1, r+0.5-d)); a > 0 {
				blend(img, x, y, c, a)
			}
		}
	}
}

// settleBakedWater gives the pieces whose model holds its own heights a
// level: a metre and a half above the lowest ground under them (the river
// bed falls along a piece, so less leaves gaps).
func (w *World) settleBakedWater() {
	n := w.GridN
	for k := range w.Water {
		wt := &w.Water[k]
		if !wt.Baked {
			continue
		}
		lo := math.Inf(1)
		minX, minZ, maxX, maxZ := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, c := range wt.Corners {
			minX, maxX = math.Min(minX, float64(c[0])), math.Max(maxX, float64(c[0]))
			minZ, maxZ = math.Min(minZ, float64(c[1])), math.Max(maxZ, float64(c[1]))
		}
		for j := max(int(minZ/w.Cell), 0); j <= min(int(maxZ/w.Cell)+1, n-1); j++ {
			for i := max(int(minX/w.Cell), 0); i <= min(int(maxX/w.Cell)+1, n-1); i++ {
				lo = math.Min(lo, float64(w.Heights[j*n+i]))
			}
		}
		if math.IsInf(lo, 1) {
			wt.Level = -1e6 // off the map: never shows
			continue
		}
		wt.Level = float32(lo + 1.5)
		wt.Baked = false
	}
}
