// Copyright (c) 2026 Aristarh Ucolov.

package terrain

// A world whose terrain file is encrypted (the DLC maps come as .ebo) is
// drawn from what the server has in the open: the navigation mesh the
// infected and animals walk on (navmesh.nm in the world's _nav PBO — a
// Recast/Detour tile set) and the mission's own files.
//
//   - The mesh covers every walkable surface with heights — the ground, the
//     seabed and the water — so it gives the relief and the water; its areas
//     tell ground (1), deep water (5), shallow water and streams (2, 3), the
//     floors inside buildings (8, 12, 13) and fences (48–52).
//   - mapgroupcluster*.xml places the stumps, fallen and fruit trees of the
//     forests, and the pieces of the gravel and dirt roads (with their
//     direction).
//   - areaflags.map says what is land, for the stretches the mesh leaves out.
//
// No paved roads and no place names: those live only in the encrypted world.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LocateNavmesh finds a navigation mesh (navmesh.nm) in a PBO.
func LocateNavmesh(pboPath string) (Source, error) {
	return locateAny(pboPath, func(name string) bool { return strings.HasSuffix(name, ".nm") })
}

// Navmesh area kinds (the DayZ navmesh's area ids).
func navAreaKind(area byte) (ground, water, floor bool) {
	switch {
	case area == 1, area >= 48 && area <= 52:
		return true, false, false
	case area == 5, area == 2, area == 3:
		return false, true, false
	case area == 8, area == 12, area == 13:
		return false, false, true
	}
	return false, false, false
}

// ReadNavmesh builds a World from a navigation mesh and the mission folder
// (missionDir may be ""). worldSize is the world's edge in metres (0: from
// the mesh).
func ReadNavmesh(src Source, missionDir string, worldSize float64) (*World, error) {
	f, err := os.Open(src.PBO)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sec := io.NewSectionReader(f, src.Offset, src.Size)
	var hdr [48]byte
	if _, err := io.ReadFull(sec, hdr[:]); err != nil {
		return nil, err
	}
	if string(hdr[:4]) != "TESM" {
		return nil, fmt.Errorf("%w (not a navigation mesh)", ErrUnsupported)
	}
	le := binary.LittleEndian
	tw, th := int(le.Uint32(hdr[12:])), int(le.Uint32(hdr[16:]))
	tileW := float64(math.Float32frombits(le.Uint32(hdr[32:])))
	if tw <= 0 || th <= 0 || tw > 4096 || th > 4096 || tileW <= 0 || tileW > 1000 {
		return nil, fmt.Errorf("%w (navmesh %dx%d tiles of %.0f m)", ErrUnsupported, tw, th, tileW)
	}
	if worldSize <= 0 {
		worldSize = math.Max(float64(tw), float64(th)) * tileW
	}
	n := 2048
	w := &World{Version: 0, Size: worldSize, GridN: n, Cell: worldSize / float64(n), FromNavmesh: true}
	cells := n * n
	nan := float32(math.NaN())
	ground := make([]float32, cells)
	bed := make([]float32, cells)
	top := make([]float32, cells)
	for i := range ground {
		ground[i], bed[i], top[i] = nan, nan, nan
	}

	// The tiles, in file order (the offsets grow with the index).
	tiles := tw * th
	offs := make([]uint32, tiles)
	if err := binary.Read(bufio.NewReader(io.NewSectionReader(sec, 48, int64(tiles)*4)), le, offs); err != nil {
		return nil, fmt.Errorf("terrain: navmesh tile table: %w", err)
	}
	dataAt := int64(48 + tiles*4)
	order := make([]int, 0, tiles)
	for i := range offs {
		order = append(order, i)
	}
	sort.Slice(order, func(a, b int) bool { return offs[order[a]] < offs[order[b]] })
	br := bufio.NewReaderSize(io.NewSectionReader(sec, dataAt, src.Size-dataAt), 1<<20)
	var pos int64
	// A record: 16 bytes of the set, then Detour's tile header — 100 bytes
	// in Detour's own format (version 7), 104 in DayZ's (12: one more field).
	var head [116]byte
	buf := make([]byte, 0, 64<<10)
	cell := w.Cell
	// set splats a height into a grid point.
	for _, ti := range order {
		at := int64(offs[ti])
		if at < pos {
			continue
		}
		if _, err := br.Discard(int(at - pos)); err != nil {
			break
		}
		pos = at
		if _, err := io.ReadFull(br, head[:]); err != nil {
			break
		}
		pos += int64(len(head))
		if string(head[16:20]) != "VAND" {
			continue
		}
		switch ver := le.Uint32(head[20:]); {
		case ver >= 10:
			if _, err := br.Discard(4); err != nil {
				return nil, fmt.Errorf("terrain: navmesh: %w", err)
			}
			pos += 4
		case ver < 7:
			continue // a format from before Detour's verts-then-polys layout
		}
		pc, vc := int(int32(le.Uint32(head[40:]))), int(int32(le.Uint32(head[44:])))
		if pc < 0 || vc < 0 || pc > 1<<16 || vc > 1<<16 {
			continue
		}
		need := vc*12 + pc*32
		if cap(buf) < need {
			buf = make([]byte, need)
		}
		buf = buf[:need]
		if _, err := io.ReadFull(br, buf); err != nil {
			break
		}
		pos += int64(need)
		vert := func(i int) [3]float32 {
			o := i * 12
			return [3]float32{
				math.Float32frombits(le.Uint32(buf[o:])),
				math.Float32frombits(le.Uint32(buf[o+4:])),
				math.Float32frombits(le.Uint32(buf[o+8:])),
			}
		}
		for p := 0; p < pc; p++ {
			b := vc*12 + p*32
			k := int(buf[b+30])
			if k < 3 || k > 6 {
				continue
			}
			isGround, isWater, isFloor := navAreaKind(buf[b+31] & 0x3f)
			if !isGround && !isWater && !isFloor {
				continue
			}
			var pts [6][3]float32
			ok := true
			for q := 0; q < k; q++ {
				vi := int(le.Uint16(buf[b+4+q*2:]))
				if vi >= vc {
					ok = false
					break
				}
				pts[q] = vert(vi)
			}
			if !ok {
				continue
			}
			if isFloor {
				poly := make([][2]float32, k)
				for q := 0; q < k; q++ {
					poly[q] = [2]float32{pts[q][0], pts[q][2]}
				}
				w.Floors = append(w.Floors, poly)
				continue
			}
			for q := 1; q+1 < k; q++ {
				rasterTri(pts[0], pts[q], pts[q+1], cell, n, func(i int, y float32) {
					if isGround {
						if g := ground[i]; g != g || y > g {
							ground[i] = y
						}
						return
					}
					if v := bed[i]; v != v || y < v {
						bed[i] = y
					}
					if v := top[i]; v != v || y > v {
						top[i] = y
					}
				})
			}
		}
	}

	land := landMask(filepath.Join(missionDir, "areaflags.map"), n)
	w.Heights = make([]float32, cells)
	w.WaterLevel = make([]float32, cells)
	seaHole := make([]bool, cells)
	for i := 0; i < cells; i++ {
		w.WaterLevel[i] = nan
		g, b, t := ground[i], bed[i], top[i]
		switch {
		case t == t && t > 1.5 && (g != g || t >= g-0.3):
			// Inland water (a lake, a river narrower than a cell): the
			// water wins over its banks. Shallow water may be walked on, so
			// its own surface is all the mesh has — the bed goes a little
			// below it.
			h := b
			if g == g && g < h {
				h = g
			}
			if t-h < 0.6 {
				h = t - 0.6
			}
			w.Heights[i] = h
			w.WaterLevel[i] = t
		case g == g:
			w.Heights[i] = g
		case b == b && t-b < 0.5:
			// Only the surface of the sea: its depth comes from the seabed
			// round it (fillPyramid).
			w.Heights[i] = nan
			seaHole[i] = true
		case b == b:
			w.Heights[i] = float32(math.Min(float64(b), -3))
		default:
			w.Heights[i] = nan
			seaHole[i] = land != nil && !land[i]
		}
	}
	fillPyramid(w.Heights, n)
	// What the mesh left out of the sea stays sea, what it left out of the
	// land stays land.
	for i := 0; i < cells; i++ {
		if seaHole[i] && w.Heights[i] >= -1 {
			w.Heights[i] = -1
		}
	}

	readClusters(w, missionDir)
	return w, nil
}

// rasterTri calls set for every grid point inside the triangle (map metres,
// points cell apart) with the height there, and for the grid points of its
// corners — a triangle smaller than a cell still leaves its mark.
func rasterTri(a, b, c [3]float32, cell float64, n int, set func(i int, y float32)) {
	ax, az := float64(a[0])/cell, float64(a[2])/cell
	bx, bz := float64(b[0])/cell, float64(b[2])/cell
	cx, cz := float64(c[0])/cell, float64(c[2])/cell
	for _, p := range [3][3]float64{{ax, az, float64(a[1])}, {bx, bz, float64(b[1])}, {cx, cz, float64(c[1])}} {
		i, j := int(math.Round(p[0])), int(math.Round(p[1]))
		if i >= 0 && j >= 0 && i < n && j < n {
			set(j*n+i, float32(p[2]))
		}
	}
	d := (bz-cz)*(ax-cx) + (cx-bx)*(az-cz)
	if math.Abs(d) < 1e-9 {
		return
	}
	i0, i1 := max(int(math.Ceil(math.Min(ax, math.Min(bx, cx)))), 0), min(int(math.Floor(math.Max(ax, math.Max(bx, cx)))), n-1)
	j0, j1 := max(int(math.Ceil(math.Min(az, math.Min(bz, cz)))), 0), min(int(math.Floor(math.Max(az, math.Max(bz, cz)))), n-1)
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			x, z := float64(i), float64(j)
			l1 := ((bz-cz)*(x-cx) + (cx-bx)*(z-cz)) / d
			l2 := ((cz-az)*(x-cx) + (ax-cx)*(z-cz)) / d
			l3 := 1 - l1 - l2
			if l1 < -1e-6 || l2 < -1e-6 || l3 < -1e-6 {
				continue
			}
			set(j*n+i, float32(l1*float64(a[1])+l2*float64(b[1])+l3*float64(c[1])))
		}
	}
}

// fillPyramid gives the points the mesh left out (inside buildings, under
// cliffs, stretches of open sea) values from their known surroundings: the
// grid is halved until every point of the smallest one is known, and the
// holes are filled back up level by level, then smoothed a little — any
// size of hole, in time proportional to the grid.
func fillPyramid(h []float32, n int) {
	type level struct {
		n int
		v []float32
	}
	levels := []level{{n, h}}
	for cur := levels[0]; cur.n > 8; {
		m := (cur.n + 1) / 2
		v := make([]float32, m*m)
		for j := 0; j < m; j++ {
			for i := 0; i < m; i++ {
				var s float32
				c := 0
				for dj := 0; dj < 2; dj++ {
					for di := 0; di < 2; di++ {
						ii, jj := 2*i+di, 2*j+dj
						if ii < cur.n && jj < cur.n {
							if x := cur.v[jj*cur.n+ii]; x == x {
								s += x
								c++
							}
						}
					}
				}
				if c > 0 {
					v[j*m+i] = s / float32(c)
				} else {
					v[j*m+i] = float32(math.NaN())
				}
			}
		}
		cur = level{m, v}
		levels = append(levels, cur)
	}
	// The smallest level: spread known values until none is missing.
	top := levels[len(levels)-1]
	for pass := 0; pass < 64; pass++ {
		missing := 0
		for j := 0; j < top.n; j++ {
			for i := 0; i < top.n; i++ {
				k := j*top.n + i
				if top.v[k] == top.v[k] {
					continue
				}
				var s float32
				c := 0
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					ii, jj := i+d[0], j+d[1]
					if ii >= 0 && jj >= 0 && ii < top.n && jj < top.n {
						if x := top.v[jj*top.n+ii]; x == x {
							s += x
							c++
						}
					}
				}
				if c > 0 {
					top.v[k] = s / float32(c)
				} else {
					missing++
				}
			}
		}
		if missing == 0 {
			break
		}
	}
	for i := range top.v {
		if top.v[i] != top.v[i] {
			top.v[i] = 0
		}
	}
	// Back down: a missing point takes its parent's value.
	for l := len(levels) - 2; l >= 0; l-- {
		cur, up := levels[l], levels[l+1]
		holes := make([]int, 0)
		for j := 0; j < cur.n; j++ {
			for i := 0; i < cur.n; i++ {
				k := j*cur.n + i
				if cur.v[k] != cur.v[k] {
					cur.v[k] = up.v[(j/2)*up.n+i/2]
					holes = append(holes, k)
				}
			}
		}
		// Smooth the filled points so the parent's blocks do not show.
		for pass := 0; pass < 2; pass++ {
			for _, k := range holes {
				i, j := k%cur.n, k/cur.n
				var s float32
				c := 0
				for _, d := range [5][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					ii, jj := i+d[0], j+d[1]
					if ii >= 0 && jj >= 0 && ii < cur.n && jj < cur.n {
						s += cur.v[jj*cur.n+ii]
						c++
					}
				}
				cur.v[k] = s / float32(c)
			}
		}
	}
}

// landMask reads areaflags.map (the Central Economy's grid: any flag set
// means land) at n×n points, row 0 south; nil when it cannot be read.
func landMask(path string, n int) []bool {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var raw [6]uint32
	if binary.Read(f, binary.LittleEndian, &raw) != nil {
		return nil
	}
	gw, gh, ubits := int(raw[0]), int(raw[1]), int(raw[4])
	if gw <= 0 || gh <= 0 || gw > 16384 || gh > 16384 || ubits <= 0 || ubits%8 != 0 {
		return nil
	}
	ub := ubits / 8
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	valAt := int64(24) + int64(gw)*int64(gh)*int64(ub)
	half := st.Size()-valAt == int64(gw)*int64(gh)/2
	out := make([]bool, n*n)
	row := make([]byte, gw*ub)
	vrow := make([]byte, gw)
	for j := 0; j < n; j++ {
		gy := j * gh / n
		if _, err := f.ReadAt(row, 24+int64(gy)*int64(gw*ub)); err != nil {
			return nil
		}
		vo := valAt + int64(gy)*int64(gw)
		vl := gw
		if half {
			vo, vl = valAt+int64(gy)*int64(gw)/2, (gw+1)/2
		}
		if _, err := f.ReadAt(vrow[:vl], vo); err != nil && !errors.Is(err, io.EOF) {
			return nil
		}
		for i := 0; i < n; i++ {
			gx := i * gw / n
			used := false
			for k := 0; k < ub; k++ {
				if row[gx*ub+k] != 0 {
					used = true
					break
				}
			}
			if !used {
				if half {
					b := vrow[gx/2]
					if gx%2 == 1 {
						b >>= 4
					}
					used = b&0x0f != 0
				} else {
					used = vrow[gx] != 0
				}
			}
			out[j*n+i] = used
		}
	}
	return out
}

var reCluster = regexp.MustCompile(`<group\s+name="([^"]+)"\s+pos="([-\d.eE]+)\s+[-\d.eE]+\s+([-\d.eE]+)"([^>]*)>`)
var reClusterAngle = regexp.MustCompile(`\ba="([-\d.eE]+)"`)

// AddClusterForests gives a world whose terrain file has no trees this panel
// recognises (a modded map with its own tree models) the forests of its
// mission's clusters.
func AddClusterForests(w *World, missionDir string) {
	for _, c := range w.Trees {
		if c > 0 {
			return
		}
	}
	roads := w.Roads
	readClusters(w, missionDir)
	// The terrain file's own roads stay; only the forests are new.
	w.Roads = roads
}

// readClusters reads the mission's mapgroupcluster*.xml: tree-like points
// make the forests (scaled to the tree counts a terrain file would give),
// road pieces become short stretches of track along their direction.
func readClusters(w *World, missionDir string) {
	if missionDir == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(missionDir, "mapgroupcluster*.xml"))
	n := w.GridN
	counts := make([]float32, n*n)
	any := false
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, m := range reCluster.FindAllSubmatch(data, -1) {
			name := strings.ToLower(string(m[1]))
			x, err1 := strconv.ParseFloat(string(m[2]), 64)
			z, err2 := strconv.ParseFloat(string(m[3]), 64)
			if err1 != nil || err2 != nil {
				continue
			}
			switch {
			case strings.HasPrefix(name, "road") || strings.HasPrefix(name, "path"):
				a := 0.0
				if am := reClusterAngle.FindSubmatch(m[4]); am != nil {
					a, _ = strconv.ParseFloat(string(am[1]), 64)
				}
				r := a * math.Pi / 180
				dx, dz := math.Cos(r), math.Sin(r) // along the road
				half, wide := 7.5, 1.6
				kind := RoadTrack
				if strings.HasPrefix(name, "roadgrav") {
					half, wide, kind = 7.5, 2.4, RoadPaved
				}
				px, pz := -dz, dx
				q := Quad{
					{float32(x - dx*half + px*wide), float32(z - dz*half + pz*wide)},
					{float32(x + dx*half + px*wide), float32(z + dz*half + pz*wide)},
					{float32(x + dx*half - px*wide), float32(z + dz*half - pz*wide)},
					{float32(x - dx*half - px*wide), float32(z - dz*half - pz*wide)},
				}
				w.Roads[kind] = append(w.Roads[kind], q)
			case strings.Contains(name, "haybale"), strings.Contains(name, "straw"), strings.Contains(name, "manure"), strings.Contains(name, "quarry"):
			default:
				i, j := int(x/w.Cell), int(z/w.Cell)
				if i >= 0 && j >= 0 && i < n && j < n {
					counts[j*n+i]++
					any = true
				}
			}
		}
	}
	if !any {
		return
	}
	// A cluster point stands for about fourteen trees of a forest.
	w.Trees = make([]uint16, n*n)
	for i, c := range counts {
		w.Trees[i] = uint16(math.Min(float64(c)*14, math.MaxUint16))
	}
	w.ForestBlur = 4
}
