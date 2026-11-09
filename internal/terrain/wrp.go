// Copyright (c) 2026 Aristarh Ucolov.
//
// Package terrain reads a DayZ world — the binarized terrain (.wrp) inside
// the world's PBO that every server already has — and draws a topographic map
// of it: relief and contour lines from the height grid, the sea, lakes and
// rivers, forests from the tree objects, roads, railways and the outline of
// every building from the map data the game draws its own map with, and the
// place names of the world's config.
//
// It all happens on the admin's computer from the admin's own server files,
// and nothing is sent anywhere: the panel ships no map art (Bohemia's rules
// forbid redistributing game files, which every DayZ map picture online is).
// Encrypted worlds (the DLC maps come as .ebo) are not opened.
//
// The format is Bohemia's OPRW, version 32 for DayZ:
//
//	"OPRW", version, "0FNE", 5 bytes
//	layer grid (2×u32), terrain grid (2×u32), layer cell size (f32)
//	geography and sound quadtrees, peaks (u32 n + n×3 f32), materials quadtree
//	an Enfusion block (u32 unpacked size + LZO1X)
//	heights: terrain grid × f32, LZO1X with no length in front, row 0 south
//	materials (u32 n + n×2 strings), models (u32 n + n strings)
//	classed models (u32 n + n×(2 strings, 3 f32, 8 bytes))
//	a quadtree, u32 objects size, a quadtree, u32 map info size
//	… (two LZO blocks, road networks)
//	objects: 64 bytes each (id, model, 3×3 matrix, position, 8 bytes)
//	map info: typed records the game's own 2D map is drawn from
//
// The last two end the file, so they are found from its end.
package terrain

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"dayzmanager/internal/pbo"
)

// ErrUnsupported: not a terrain this reader knows (another version).
var ErrUnsupported = errors.New("terrain: unsupported world format")

// Quad is a footprint from the map data: four corners (x, z), in order
// around the shape.
type Quad [4][2]float32

// Line is a polyline of map points (x, z).
type Line [][2]float32

// Point is a map point (x, z) and a height or a size, depending on use.
type Point struct{ X, Z, V float32 }

// Water is one water surface of the world (a pond or a river piece): its
// corners and the height of the water. Baked: a piece whose own shape holds
// its heights (Livonia's river), so the level is found from the river bed.
type Water struct {
	Corners Quad
	Level   float32
	Baked   bool
}

// Road kinds.
const (
	RoadMain = iota
	RoadPaved
	RoadTrack
	roadKinds
)

// Building kinds (by the map data's own type).
const (
	BuildHouse    = iota // houses, ruins, tourist spots
	BuildIndustry        // other buildings (sheds, barracks, tanks, piers)
	BuildChurch          // churches and chapels
	BuildLandmark        // hospital, fuel station
	BuildWall            // fences and walls
	buildKinds
)

// World is what the map is drawn from.
type World struct {
	Version uint32
	Size    float64   // edge of the world square, metres
	GridN   int       // terrain grid points per edge
	Cell    float64   // metres between grid points
	Heights []float32 // GridN×GridN, row 0 = south edge

	Peaks     []Point // x, z, height
	Roads     [roadKinds][]Quad
	Buildings [buildKinds][]Quad
	Rail      []Line
	Power     []Line
	Rocks     []Point
	Water     []Water

	// Trees counted per terrain cell (bushes count a third).
	Trees []uint16
	// ForestBlur: how far (in cells) tree counts are smoothed; sparse counts
	// (a navigation mesh world's) need more. 0 means 1.
	ForestBlur int

	// A world read from its navigation mesh (navmesh.go): the floors of its
	// buildings as polygons, and the water level per grid point (NaN where
	// there is no inland water).
	FromNavmesh bool
	Floors      [][][2]float32
	WaterLevel  []float32

	Models int
	Places []Place // filled by ReadPlaces
}

// HeightAt is the bilinear terrain height at a map point.
func (w *World) HeightAt(x, z float64) float64 {
	n := w.GridN
	gx, gz := x/w.Cell, z/w.Cell
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
	x1, z1 := x0+1, z0+1
	if x1 >= n {
		x1 = n - 1
	}
	if z1 >= n {
		z1 = n - 1
	}
	fx, fz := gx-float64(x0), gz-float64(z0)
	h00 := float64(w.Heights[z0*n+x0])
	h10 := float64(w.Heights[z0*n+x1])
	h01 := float64(w.Heights[z1*n+x0])
	h11 := float64(w.Heights[z1*n+x1])
	return (h00*(1-fx)+h10*fx)*(1-fz) + (h01*(1-fx)+h11*fx)*fz
}

// Source is a world inside a PBO: the archive and where the .wrp lies.
type Source struct {
	PBO    string // the archive
	Entry  string // the .wrp's name inside
	Offset int64  // where its bytes start in the archive
	Size   int64
}

// limits keep a corrupt or hostile file from asking for absurd memory.
const (
	maxGrid    = 8192
	maxStrings = 1 << 20
	maxString  = 4096
)

type reader struct {
	ra   io.ReaderAt // the .wrp's bytes
	size int64
	r    *bufio.Reader
	pos  int64
	err  error
}

func newReader(ra io.ReaderAt, size int64) *reader {
	return &reader{ra: ra, size: size, r: bufio.NewReaderSize(io.NewSectionReader(ra, 0, size), 1<<20)}
}

// seek continues reading at pos.
func (r *reader) seek(pos int64) {
	r.pos = pos
	r.r.Reset(io.NewSectionReader(r.ra, pos, r.size-pos))
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		r.fail(err)
		return nil
	}
	r.pos += int64(n)
	return b
}

func (r *reader) skip(n int64) {
	if r.err != nil {
		return
	}
	if _, err := r.r.Discard(int(n)); err != nil {
		r.fail(err)
	}
	r.pos += n
}

func (r *reader) u8() byte {
	b := r.bytes(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) u16() uint16 {
	b := r.bytes(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *reader) u32() uint32 {
	b := r.bytes(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) f32() float32 { return math.Float32frombits(r.u32()) }

func (r *reader) str() string {
	if r.err != nil {
		return ""
	}
	s, err := r.r.ReadString(0)
	if err != nil {
		r.fail(err)
		return ""
	}
	r.pos += int64(len(s))
	if len(s) > maxString {
		r.fail(errors.New("terrain: name too long"))
		return ""
	}
	return s[:len(s)-1]
}

// quadtree skips one of the format's quadtrees: a flag byte, then either one
// 4-byte value or a node — 16 bits saying which of its 16 children are nodes
// and which 4-byte leaves.
func (r *reader) quadtree() {
	if r.u8() == 0 {
		r.skip(4)
		return
	}
	var node func(depth int)
	node = func(depth int) {
		if depth > 16 {
			r.fail(errors.New("terrain: quadtree too deep"))
			return
		}
		flags := r.u16()
		for i := 0; i < 16 && r.err == nil; i++ {
			if flags&(1<<i) != 0 {
				node(depth + 1)
			} else {
				r.skip(4)
			}
		}
	}
	node(0)
}

// lzo reads an LZO1X block of known unpacked size. The compressed length is
// not stored, so it decodes from a window that surely holds the block and
// then moves past what it used.
func (r *reader) lzo(n int) []byte {
	if r.err != nil {
		return nil
	}
	// Compressed data is rarely bigger than the data; allow a margin for
	// incompressible blocks.
	window := int64(n) + int64(n)/8 + 1024
	if rest := r.size - r.pos; window > rest {
		window = rest
	}
	if window <= 0 {
		r.fail(io.ErrUnexpectedEOF)
		return nil
	}
	b := make([]byte, window)
	got, err := r.ra.ReadAt(b, r.pos)
	if got == 0 && err != nil {
		r.fail(err)
		return nil
	}
	out, used, err := lzo1x(b[:got], n)
	if err != nil {
		r.fail(err)
		return nil
	}
	r.seek(r.pos + int64(used))
	return out
}

// Read parses the world at src. Heavy parts (objects, map data) stream from
// the file; only what the map needs is kept.
func Read(src Source) (*World, error) {
	f, err := os.Open(src.PBO)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := newReader(io.NewSectionReader(f, src.Offset, src.Size), src.Size)
	if string(r.bytes(4)) != "OPRW" {
		return nil, ErrUnsupported
	}
	w := &World{Version: r.u32()}
	if w.Version != 32 || string(r.bytes(4)) != "0FNE" {
		return nil, fmt.Errorf("%w (OPRW version %d)", ErrUnsupported, w.Version)
	}
	r.skip(5)
	layerX, layerY := int(r.u32()), int(r.u32())
	gridX, gridY := int(r.u32()), int(r.u32())
	layerCell := float64(r.f32())
	if r.err != nil {
		return nil, r.err
	}
	if layerX <= 0 || layerX != layerY || gridX <= 0 || gridX != gridY || gridX > maxGrid || layerCell <= 0 || layerCell > 1000 {
		return nil, fmt.Errorf("%w (grid %dx%d, layers %dx%d)", ErrUnsupported, gridX, gridY, layerX, layerY)
	}
	w.GridN = gridX
	w.Size = layerCell * float64(layerX)
	w.Cell = w.Size / float64(gridX)

	r.quadtree() // geography
	r.quadtree() // sound map
	nPeaks := int(r.u32())
	if nPeaks > 1<<20 {
		return nil, ErrUnsupported
	}
	w.Peaks = make([]Point, 0, nPeaks)
	for i := 0; i < nPeaks && r.err == nil; i++ {
		x, h, z := r.f32(), r.f32(), r.f32()
		w.Peaks = append(w.Peaks, Point{X: x, Z: z, V: h})
	}
	r.quadtree() // materials
	// The Enfusion block: its unpacked size, then the data.
	if n := int(r.u32()); n > 0 && n < 1<<28 {
		r.lzo(n)
	} else if r.err == nil {
		return nil, fmt.Errorf("%w (block of %d bytes)", ErrUnsupported, n)
	}
	raw := r.lzo(gridX * gridY * 4)
	if r.err != nil {
		return nil, fmt.Errorf("terrain: heights: %w", r.err)
	}
	w.Heights = make([]float32, gridX*gridY)
	for i := range w.Heights {
		w.Heights[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	raw = nil
	// Big worlds are drawn from a grid of at most 2048 points: the picture
	// has 4096 pixels at most, and the drawing's memory grows with the grid.
	for w.GridN > 2048 && w.GridN%2 == 0 {
		w.Heights = halveGrid(w.Heights, w.GridN)
		w.GridN /= 2
		w.Cell *= 2
	}

	nMat := int(r.u32())
	if nMat > maxStrings {
		return nil, ErrUnsupported
	}
	for i := 0; i < nMat && r.err == nil; i++ {
		r.str()
		r.str()
	}
	nModels := int(r.u32())
	if nModels > maxStrings {
		return nil, ErrUnsupported
	}
	models := make([]string, 0, nModels)
	for i := 0; i < nModels && r.err == nil; i++ {
		models = append(models, strings.ToLower(r.str()))
	}
	w.Models = len(models)
	nClassed := int(r.u32())
	if nClassed > maxStrings {
		return nil, ErrUnsupported
	}
	for i := 0; i < nClassed && r.err == nil; i++ {
		r.str()
		r.str()
		r.skip(12 + 8)
	}
	r.quadtree()
	objSize := int64(r.u32())
	r.quadtree()
	infoSize := int64(r.u32())
	if r.err != nil {
		return nil, fmt.Errorf("terrain: %w", r.err)
	}
	if objSize%64 != 0 || objSize+infoSize > src.Size-r.pos {
		return nil, fmt.Errorf("%w (objects %d, map info %d)", ErrUnsupported, objSize, infoSize)
	}
	infoAt := src.Size - infoSize
	objAt := infoAt - objSize
	if err := w.readObjects(io.NewSectionReader(f, src.Offset+objAt, objSize), models); err != nil {
		return nil, err
	}
	if err := w.readMapInfo(io.NewSectionReader(f, src.Offset+infoAt, infoSize)); err != nil {
		return nil, err
	}
	return w, nil
}

// halveGrid averages each 2×2 block of an n×n grid.
func halveGrid(in []float32, n int) []float32 {
	m := n / 2
	out := make([]float32, m*m)
	for j := 0; j < m; j++ {
		for i := 0; i < m; i++ {
			a := in[(2*j)*n+2*i] + in[(2*j)*n+2*i+1] + in[(2*j+1)*n+2*i] + in[(2*j+1)*n+2*i+1]
			out[j*m+i] = a / 4
		}
	}
	return out
}

// readObjects keeps what the map needs from the world's objects: trees and
// bushes (counted per cell, for the forests) and water surfaces.
func (w *World) readObjects(sr io.Reader, models []string) error {
	type kind uint8
	const (
		other kind = iota
		tree
		bush
		water
	)
	kinds := make([]kind, len(models))
	waterSize := make([][2]float32, len(models))
	bakedWater := make([]bool, len(models))
	for i, m := range models {
		base := m
		if j := strings.LastIndexByte(m, '\\'); j >= 0 {
			base = m[j+1:]
		}
		switch {
		case strings.Contains(m, `\tree\`) && strings.HasPrefix(base, "t_"):
			kinds[i] = tree
		case strings.Contains(m, `\bush\`) && strings.HasPrefix(base, "b_"):
			kinds[i] = bush
		case strings.Contains(m, `\water`):
			if wd, ln, baked, ok := waterModelSize(base); ok {
				kinds[i] = water
				waterSize[i] = [2]float32{wd, ln}
				bakedWater[i] = baked
			}
		}
	}
	n := w.GridN
	w.Trees = make([]uint16, n*n)
	bushAcc := make([]uint8, n*n)
	br := bufio.NewReaderSize(sr, 1<<20)
	var rec [64]byte
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(rec[8+i*4:])) }
	for {
		if _, err := io.ReadFull(br, rec[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("terrain: objects: %w", err)
		}
		mi := int(binary.LittleEndian.Uint32(rec[4:]))
		if mi < 0 || mi >= len(kinds) || kinds[mi] == other {
			continue
		}
		x, y, z := f(9), f(10), f(11)
		switch kinds[mi] {
		case tree, bush:
			cx, cz := int(float64(x)/w.Cell), int(float64(z)/w.Cell)
			if cx < 0 || cz < 0 || cx >= n || cz >= n {
				continue
			}
			i := cz*n + cx
			if kinds[mi] == bush {
				// Three bushes make a tree.
				bushAcc[i]++
				if bushAcc[i] < 3 {
					continue
				}
				bushAcc[i] = 0
			}
			if w.Trees[i] < math.MaxUint16 {
				w.Trees[i]++
			}
		case water:
			// The model's axes in the world: the matrix rows (x axis, up,
			// z axis), each carrying the object's scale.
			ax := [2]float32{f(0), f(2)}
			az := [2]float32{f(6), f(8)}
			hw, hl := waterSize[mi][0]/2, waterSize[mi][1]/2
			var q Quad
			for k, s := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
				q[k] = [2]float32{
					x + ax[0]*s[0]*hw + az[0]*s[1]*hl,
					z + ax[1]*s[0]*hw + az[1]*s[1]*hl,
				}
			}
			w.Water = append(w.Water, Water{Corners: q, Level: y, Baked: bakedWater[mi]})
		}
	}
}

// waterModelSize reads a water surface's width and length from its model
// name: pond_50x50, lake_50x50, enoch_50x50_moss (50×50 m), river_24_12 (24
// wide, 12 long), stream_14_24_l30 (14 wide, 24 long, bending). Pieces with
// no size (enoch_river_17, enoch_wolisko_4) are 50 m tiles shaped in the
// model itself (baked). Stream beds and stones are decoration.
func waterModelSize(base string) (w, l float32, baked, ok bool) {
	name := strings.TrimSuffix(base, ".p3d")
	if strings.Contains(name, "streambed") || strings.Contains(name, "stone") {
		return 0, 0, false, false
	}
	for _, part := range strings.Split(name, "_") {
		var a, b float32
		if n, err := fmt.Sscanf(part, "%fx%f", &a, &b); err == nil && n == 2 && a > 0 && b > 0 {
			return a, b, false, true
		}
	}
	switch {
	case strings.HasPrefix(name, "pond_small"):
		return 20, 20, false, true
	case strings.HasPrefix(name, "pond_big"):
		return 40, 40, false, true
	case strings.HasPrefix(name, "river_"), strings.HasPrefix(name, "stream_"):
		parts := strings.Split(name, "_")
		var a, b float32
		if len(parts) >= 3 {
			if _, err := fmt.Sscanf(parts[1]+" "+parts[2], "%f %f", &a, &b); err == nil && a > 0 && b > 0 {
				return a, b, false, true
			}
		}
	}
	for _, k := range []string{"river", "pond", "lake", "wolisko"} {
		if strings.Contains(name, k) {
			return 50, 50, true, true
		}
	}
	return 0, 0, false, false
}

// Map info record types (Bohemia's MapType numbering; DayZ adds 36–43).
var infoSize = map[uint32]int{}

func init() {
	for _, t := range []uint32{0, 1, 2, 10, 11, 13, 14, 15, 16, 17, 22, 23, 26, 27, 30, 36, 37, 38, 39, 40} {
		infoSize[t] = 12 // object id, x, z
	}
	for _, t := range []uint32{24, 31, 32} {
		infoSize[t] = 36 // object id, 4 corners
	}
	for _, t := range []uint32{25, 33} {
		infoSize[t] = 24 // colour, flags, 4 floats
	}
	for _, t := range []uint32{3, 4, 5, 6, 7, 8, 9, 12, 18, 19, 20, 21, 28, 29, 41, 42, 43} {
		infoSize[t] = 40 // object id, 4 corners, colour
	}
	infoSize[34] = 20 // power line: object id, 2 points
	infoSize[35] = 29 // railway: object id, 3 points, 1 byte
}

// readMapInfo keeps the roads, buildings, railways, power lines and rocks of
// the map data.
func (w *World) readMapInfo(sr io.Reader) error {
	br := bufio.NewReaderSize(sr, 1<<20)
	buf := make([]byte, 64)
	fl := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:])) }
	quad := func() Quad {
		// Stored as two opposite edges (0-1 and 2-3): walk round as 0,1,3,2.
		return Quad{{fl(1), fl(2)}, {fl(3), fl(4)}, {fl(7), fl(8)}, {fl(5), fl(6)}}
	}
	var tb [4]byte
	for {
		if _, err := io.ReadFull(br, tb[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("terrain: map info: %w", err)
		}
		t := binary.LittleEndian.Uint32(tb[:])
		size, ok := infoSize[t]
		if !ok {
			// An unknown record type: everything after it is unreadable,
			// but what came before is good.
			return nil
		}
		if _, err := io.ReadFull(br, buf[:size]); err != nil {
			return fmt.Errorf("terrain: map info: %w", err)
		}
		switch t {
		case 32:
			w.Roads[RoadMain] = append(w.Roads[RoadMain], quad())
		case 24:
			w.Roads[RoadPaved] = append(w.Roads[RoadPaved], quad())
		case 31:
			w.Roads[RoadTrack] = append(w.Roads[RoadTrack], quad())
		case 4, 28, 29, 41, 42, 43:
			w.Buildings[BuildHouse] = append(w.Buildings[BuildHouse], quad())
		case 3, 12:
			w.Buildings[BuildIndustry] = append(w.Buildings[BuildIndustry], quad())
		case 8, 9:
			w.Buildings[BuildChurch] = append(w.Buildings[BuildChurch], quad())
		case 18, 19:
			w.Buildings[BuildLandmark] = append(w.Buildings[BuildLandmark], quad())
		case 20, 21:
			w.Buildings[BuildWall] = append(w.Buildings[BuildWall], quad())
		case 34:
			w.Power = append(w.Power, Line{{fl(1), fl(2)}, {fl(3), fl(4)}})
		case 35:
			w.Rail = append(w.Rail, Line{{fl(1), fl(2)}, {fl(5), fl(6)}, {fl(3), fl(4)}})
		case 11:
			w.Rocks = append(w.Rocks, Point{X: fl(1), Z: fl(2)})
		}
	}
}

// Locate finds the world's terrain in a PBO: the entry <world>.wrp (any .wrp
// when world is ""), stored unpacked (worlds always are).
func Locate(pboPath, world string) (Source, error) {
	return locateAny(pboPath, func(name string) bool {
		return strings.HasSuffix(name, ".wrp") && (world == "" || strings.EqualFold(strings.TrimSuffix(name, ".wrp"), world))
	})
}

// locateAny finds the first entry of a PBO whose lower-case base name passes
// match; it must be stored unpacked.
func locateAny(pboPath string, match func(name string) bool) (Source, error) {
	f, err := pbo.Open(pboPath)
	if err != nil {
		return Source{}, err
	}
	for _, e := range f.Entries {
		name := strings.ToLower(e.Name)
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		if !match(name) {
			continue
		}
		if e.Original != 0 && e.Original != e.Size {
			return Source{}, fmt.Errorf("%w (packed entry)", ErrUnsupported)
		}
		return Source{PBO: pboPath, Entry: e.Name, Offset: e.Offset, Size: int64(e.Size)}, nil
	}
	return Source{}, os.ErrNotExist
}
