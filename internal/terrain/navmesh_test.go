// Copyright (c) 2026 Aristarh Ucolov.

package terrain

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"dayzmanager/internal/pbo"
)

// navTile builds one Detour tile record of a DayZ navmesh: the 16-byte set
// prefix, the 104-byte header (version 12), the vertices and the polygons
// (each: vertex indices and its area).
func navTile(verts [][3]float32, polys [][]uint16, areas []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	w := func(v interface{}) { _ = binary.Write(&b, le, v) }
	var tile bytes.Buffer
	tw := func(v interface{}) { _ = binary.Write(&tile, le, v) }
	tile.WriteString("VAND")
	tw(uint32(12))                         // version
	tw([4]int32{})                         // x, y, layer, userId
	tw(int32(len(polys)))                  // polyCount
	tw(int32(len(verts)))                  // vertCount
	tw([7]int32{})                         // links … offMeshBase
	tw([3]float32{1.5, 0.3, 0.45})         // walkable height, radius, climb
	tw([7]float32{0, 0, 0, 50, 10, 50, 4}) // bmin, bmax, quant
	tw(uint32(12))                         // DayZ's extra field
	for _, v := range verts {
		tw(v)
	}
	for k, p := range polys {
		tw(uint32(0)) // first link
		var idx, neis [6]uint16
		copy(idx[:], p)
		tw(idx)
		tw(neis)
		tw(uint16(1))
		tile.WriteByte(byte(len(p)))
		tile.WriteByte(areas[k])
	}
	w([2]uint32{0, 1 << 16})
	w(uint32(tile.Len()))
	w(uint32(0))
	b.Write(tile.Bytes())
	return b.Bytes()
}

func TestReadNavmesh(t *testing.T) {
	dir := t.TempDir()
	// One 50 m tile: ground at 20 m on the west half, a pond (water 1 m
	// deep, surface at 10 m) on the east half, and a building floor.
	verts := [][3]float32{
		{0, 20, 0}, {25, 20, 0}, {25, 20, 50}, {0, 20, 50},
		{25, 9, 0}, {50, 9, 0}, {50, 9, 50}, {25, 9, 50},
		{25, 10, 0}, {50, 10, 0}, {50, 10, 50}, {25, 10, 50},
		{5, 20.5, 5}, {15, 20.5, 5}, {15, 20.5, 15},
	}
	polys := [][]uint16{{0, 1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10, 11}, {12, 13, 14}}
	areas := []byte{1, 2, 2, 8}
	tileRec := navTile(verts, polys, areas)
	var nm bytes.Buffer
	le := binary.LittleEndian
	nm.WriteString("TESM")
	_ = binary.Write(&nm, le, [4]uint32{9, 1, 1, 1})
	_ = binary.Write(&nm, le, [5]float32{0, 0, 0, 50, 50})
	_ = binary.Write(&nm, le, [2]uint32{1, 1 << 20})
	_ = binary.Write(&nm, le, uint32(0)) // the tile's offset
	nm.Write(tileRec)
	pboPath := filepath.Join(dir, "worlds_test_nav.pbo")
	if err := pbo.Write(pboPath, [][2]string{{"prefix", `DZ\worlds\test\navmesh`}}, []pbo.WriteFile{{Name: "navmesh.nm", Data: nm.Bytes()}}); err != nil {
		t.Fatal(err)
	}
	src, err := LocateNavmesh(pboPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ReadNavmesh(src, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !w.FromNavmesh || len(w.Floors) != 1 || len(w.Floors[0]) != 3 {
		t.Fatalf("floors: %+v", w.Floors)
	}
	at := func(x, z float64) (float32, float32) {
		i, j := int(x/w.Cell), int(z/w.Cell)
		k := j*w.GridN + i
		return w.Heights[k], w.WaterLevel[k]
	}
	if h, lv := at(10, 25); math.Abs(float64(h)-20) > 0.01 || lv == lv {
		t.Fatalf("ground: h=%v water=%v", h, lv)
	}
	if h, lv := at(40, 25); lv != 10 || h >= lv {
		t.Fatalf("pond: h=%v water=%v", h, lv)
	}
	// Not a navmesh.
	bad := filepath.Join(dir, "bad_nav.pbo")
	if err := pbo.Write(bad, nil, []pbo.WriteFile{{Name: "navmesh.nm", Data: []byte("nothing of the kind, long enough to read a header from it at all")}}); err != nil {
		t.Fatal(err)
	}
	bsrc, _ := LocateNavmesh(bad)
	if _, err := ReadNavmesh(bsrc, "", 0); err == nil {
		t.Fatal("a file that is not a navmesh must fail")
	}
}

func TestFillPyramid(t *testing.T) {
	n := 64
	h := make([]float32, n*n)
	nan := float32(math.NaN())
	for i := range h {
		h[i] = nan
	}
	h[0] = -10     // south-west corner
	h[n*n-1] = 10  // north-east corner
	h[32*n+32] = 0 // the middle
	fillPyramid(h, n)
	for i, v := range h {
		if v != v || v < -10.01 || v > 10.01 {
			t.Fatalf("point %d: %v", i, v)
		}
	}
	if !(h[1] < h[n*n-2]) {
		t.Fatal("the fill must follow the known values")
	}
}

func TestRasterTriAndPolygons(t *testing.T) {
	n := 10
	got := map[int]float32{}
	// A triangle over the grid points (1,1)…(5,1)…(1,5), height = x.
	rasterTri([3]float32{1, 1, 1}, [3]float32{5, 5, 1}, [3]float32{1, 1, 5}, 1, n, func(i int, y float32) { got[i] = y })
	if y, ok := got[1*n+2]; !ok || math.Abs(float64(y)-2) > 1e-4 {
		t.Fatalf("inside point: %v %v", y, ok)
	}
	if _, ok := got[5*n+5]; ok {
		t.Fatal("a point outside the triangle")
	}
	sq := [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}}
	if !insidePoly(sq, 2, 2) || insidePoly(sq, 5, 2) {
		t.Fatal("insidePoly")
	}
	rev := [][2]float64{{0, 0}, {0, 4}, {4, 4}, {4, 0}}
	if !insidePoly(rev, 1, 3) {
		t.Fatal("either winding")
	}
}

func TestLandMaskAndClusters(t *testing.T) {
	dir := t.TempDir()
	// A 4×4 areaflags.map: 8-bit usage, 8-bit values; land on the west half.
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, [6]uint32{4, 4, 400, 400, 8, 0})
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			if i < 2 {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		}
	}
	b.Write(make([]byte, 16)) // values
	if err := os.WriteFile(filepath.Join(dir, "areaflags.map"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	land := landMask(filepath.Join(dir, "areaflags.map"), 8)
	if land == nil || !land[0] || land[7] {
		t.Fatalf("land mask: %v", land)
	}
	xml := `<map>
    <group name="PiceaAbies_stump" pos="10.0 5.0 10.0" a="0" />
    <group name="PiceaAbies_fallen" pos="12.0 5.0 11.0" a="0" />
    <group name="RoadGrav_25" pos="100.0 5.0 100.0" a="90.0" />
    <group name="Pathdirt_10" pos="200.0 5.0 100.0" a="0" />
    <group name="Haybale" pos="300.0 5.0 300.0" a="0" />
</map>`
	if err := os.WriteFile(filepath.Join(dir, "mapgroupcluster01.xml"), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &World{Size: 400, GridN: 40, Cell: 10}
	readClusters(w, dir)
	if len(w.Roads[RoadPaved]) != 1 || len(w.Roads[RoadTrack]) != 1 || w.ForestBlur == 0 {
		t.Fatalf("roads %d/%d blur %d", len(w.Roads[RoadPaved]), len(w.Roads[RoadTrack]), w.ForestBlur)
	}
	if w.Trees[1*40+1] != 28 {
		t.Fatalf("two cluster trees in one cell: %d", w.Trees[1*40+1])
	}
	// The gravel road at 90° runs along z: its corners differ in z, not x.
	q := w.Roads[RoadPaved][0]
	if math.Abs(float64(q[1][1]-q[0][1])) < 10 {
		t.Fatalf("road direction: %v", q)
	}
	// A terrain world with trees of its own keeps them.
	w2 := &World{Size: 400, GridN: 40, Cell: 10, Trees: make([]uint16, 1600)}
	w2.Trees[5] = 3
	AddClusterForests(w2, dir)
	if w2.ForestBlur != 0 || len(w2.Roads[RoadTrack]) != 0 {
		t.Fatal("a world with trees keeps its own forests and roads")
	}
	w3 := &World{Size: 400, GridN: 40, Cell: 10, Trees: make([]uint16, 1600)}
	AddClusterForests(w3, dir)
	if w3.ForestBlur == 0 || len(w3.Roads[RoadTrack]) != 0 {
		t.Fatal("no trees of its own: the clusters' forests, but not their roads")
	}
}
