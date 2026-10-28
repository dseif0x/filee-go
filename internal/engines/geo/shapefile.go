package geo

// ESRI Shapefile in and out. A shapefile is several files with the same name:
// .shp (shapes), .shx (index), .dbf (attributes), .prj (coordinate system)
// and .cpg (text encoding of the .dbf). Reading takes the .shp and finds the
// others next to it (the .shx is not needed). Point, MultiPoint, PolyLine and
// Polygon shapes are read with their Z, MultiPatch is refused. Polygon rings
// are sorted into polygons and holes by their direction (outer rings
// clockwise), with holes assigned to the ring that contains them. Writing
// makes one shapefile per kind of geometry (a shapefile holds only one), in
// WGS 84 with a .prj, polygons clockwise as the format requires.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
)

type shapeKind int

const (
	shapePoint shapeKind = iota
	shapeLine
	shapePolygon
)

// shapefileGroup is the records that go into one shapefile.
type shapefileGroup struct {
	kind    shapeKind
	records []*Feature
}

// wgs84Prj is ESRI's definition of WGS 84, written to the .prj of every shapefile Filee makes.
const wgs84Prj = `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`

const (
	shpHeaderLength = 100
	shpNoData       = -1e39
)

// sidecar finds a file next to shpPath with the same name and another extension (any letter case), or "".
func sidecar(shpPath, extension string) string {
	full, err := filepath.Abs(shpPath)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(full)
	name := strings.TrimSuffix(filepath.Base(full), filepath.Ext(full))
	exact := filepath.Join(dir, name+"."+extension)
	if st, err := os.Stat(exact); err == nil && !st.IsDir() {
		return exact
	}
	// On a case-sensitive file system the sidecar may be "NAME.DBF" or "Name.Dbf".
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		n := e.Name()
		ext := filepath.Ext(n)
		if !e.IsDir() && strings.EqualFold(strings.TrimSuffix(n, ext), name) && strings.EqualFold(strings.TrimPrefix(ext, "."), extension) {
			return filepath.Join(dir, n)
		}
	}
	return ""
}

func readShapefile(ctx context.Context, path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < shpHeaderLength || int32(binary.BigEndian.Uint32(data)) != 9994 {
		return nil, errors.New("The .shp file does not start with a shapefile header.")
	}
	headerType := int32(binary.LittleEndian.Uint32(data[32:]))
	if headerType == 31 {
		return nil, convert.Refuse("error.geo_unsupported_shape", "MultiPatch")
	}
	switch headerType {
	case 0, 1, 3, 5, 8, 11, 13, 15, 18, 21, 23, 25, 28:
	default:
		return nil, fmt.Errorf("The .shp has an unknown shape type (%d).", headerType)
	}
	declared := int64(int32(binary.BigEndian.Uint32(data[24:]))) * 2
	// Some writers leave the length wrong; what is really in the file decides.
	end := int64(len(data))
	if declared >= shpHeaderLength && declared <= int64(len(data)) {
		end = declared
	}
	ds := newDataset(baseName(path))
	var geometries []Geometry
	position := int64(shpHeaderLength)
	for position+8 <= end {
		if len(geometries)&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		contentLength := int64(int32(binary.BigEndian.Uint32(data[position+4:]))) * 2
		position += 8
		if contentLength < 4 || position+contentLength > end {
			return nil, fmt.Errorf("The .shp is cut off or damaged at shape %d.", len(geometries)+1)
		}
		g, err := readShape(ctx, data[position:position+contentLength], len(geometries)+1)
		if err != nil {
			return nil, err
		}
		geometries = append(geometries, g)
		position += contentLength
	}

	var table *dbfTable
	if dbfPath := sidecar(path, "dbf"); dbfPath != "" {
		cpg := ""
		if cpgPath := sidecar(path, "cpg"); cpgPath != "" {
			b, _ := os.ReadFile(cpgPath)
			cpg = string(b)
		}
		// Only as many rows as there are shapes: a table of millions of rows next to a few shapes is not read.
		if table, err = readDbf(ctx, dbfPath, cpg, len(geometries)); err != nil {
			return nil, err
		}
		if len(table.rows) < len(geometries) {
			return nil, fmt.Errorf("The .dbf has %d rows but the .shp has %d shapes.", len(table.rows), len(geometries))
		}
	}
	for i, g := range geometries {
		f := &Feature{Geometry: g}
		if table != nil {
			row := table.rows[i]
			if row == nil {
				continue // marked as deleted in the .dbf
			}
			for k, field := range table.fields {
				f.Properties = append(f.Properties, Prop{field.name, row[k]})
			}
		}
		ds.Features = append(ds.Features, f)
	}
	// An empty .prj says nothing about the coordinate system, so it counts as no .prj.
	if prjPath := sidecar(path, "prj"); prjPath != "" {
		b, _ := os.ReadFile(prjPath)
		wkt := strings.TrimPrefix(string(b), "\uFEFF")
		if strings.TrimSpace(wkt) != "" {
			crs, err := crsFromWkt(wkt)
			if err != nil {
				return nil, err
			}
			ds.Crs, ds.CrsDeclared = crs, true
		}
	}
	return ds, nil
}

func f64(c []byte, at int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(c[at:])) }

// shpHeight: heights below -1e38 mean "no data".
func shpHeight(z float64) float64 {
	if isFinite(z) && z > -1e38 {
		return z
	}
	return math.NaN()
}

func need(c []byte, length, number int) error {
	if len(c) < length {
		return fmt.Errorf("Shape %d is shorter than its type needs.", number)
	}
	return nil
}

func readShape(ctx context.Context, c []byte, number int) (Geometry, error) {
	typ := int32(binary.LittleEndian.Uint32(c))
	switch typ {
	case 0:
		return nil, nil
	case 1, 11, 21:
		if err := need(c, 20, number); err != nil {
			return nil, err
		}
		x, y := f64(c, 4), f64(c, 12)
		if !isFinite(x) || !isFinite(y) {
			return nil, nil // an empty point is written as NaN
		}
		z := math.NaN()
		if typ == 11 && len(c) >= 28 {
			z = shpHeight(f64(c, 20))
		}
		return &Point{Coord{x, y, z}}, nil
	case 8, 18, 28:
		if err := need(c, 40, number); err != nil {
			return nil, err
		}
		count := int64(int32(binary.LittleEndian.Uint32(c[36:])))
		if count < 0 || 40+16*count > int64(len(c)) {
			return nil, fmt.Errorf("Shape %d: the point count does not fit the record.", number)
		}
		n := int(count)
		zAt := 40 + 16*n + 16
		withZ := typ == 18 && int64(zAt)+8*count <= int64(len(c))
		var points []Coord
		for i := 0; i < n; i++ {
			z := math.NaN()
			if withZ {
				z = shpHeight(f64(c, zAt+8*i))
			}
			p := Coord{f64(c, 40+16*i), f64(c, 48+16*i), z}
			if p.IsFinite() {
				points = append(points, p)
			}
		}
		switch len(points) {
		case 0:
			return nil, nil
		case 1:
			return &Point{points[0]}, nil
		}
		return &MultiPoint{points}, nil
	case 3, 13, 23, 5, 15, 25:
		return readShapeParts(ctx, c, typ, number)
	case 31:
		return nil, convert.Refuse("error.geo_unsupported_shape", "MultiPatch")
	}
	return nil, fmt.Errorf("Shape %d has an unknown shape type (%d).", number, typ)
}

func readShapeParts(ctx context.Context, c []byte, typ int32, number int) (Geometry, error) {
	if err := need(c, 44, number); err != nil {
		return nil, err
	}
	parts := int64(int32(binary.LittleEndian.Uint32(c[36:])))
	count := int64(int32(binary.LittleEndian.Uint32(c[40:])))
	pointsAt := 44 + 4*parts
	if parts < 0 || count < 0 || pointsAt+16*count > int64(len(c)) {
		return nil, fmt.Errorf("Shape %d: the part and point counts do not fit the record.", number)
	}
	zAt := pointsAt + 16*count + 16
	withZ := (typ == 13 || typ == 15) && zAt+8*count <= int64(len(c))
	starts := make([]int64, parts+1)
	for p := int64(0); p < parts; p++ {
		starts[p] = int64(int32(binary.LittleEndian.Uint32(c[44+4*p:])))
		if starts[p] < 0 || starts[p] > count || p > 0 && starts[p] < starts[p-1] {
			return nil, fmt.Errorf("Shape %d: the part %d starts outside the points.", number, p+1)
		}
	}
	starts[parts] = count
	lines := make([][]Coord, 0, parts)
	for p := int64(0); p < parts; p++ {
		if p%1024 == 1023 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		line := make([]Coord, 0, starts[p+1]-starts[p])
		for i := starts[p]; i < starts[p+1]; i++ {
			at := int(pointsAt + 16*i)
			z := math.NaN()
			if withZ {
				z = shpHeight(f64(c, int(zAt+8*i)))
			}
			pt := Coord{f64(c, at), f64(c, at+8), z}
			if pt.IsFinite() {
				line = append(line, pt)
			}
		}
		lines = append(lines, line)
	}
	if typ == 3 || typ == 13 || typ == 23 {
		var kept [][]Coord
		for _, l := range lines {
			if len(l) >= 2 {
				kept = append(kept, l)
			}
		}
		switch len(kept) {
		case 0:
			return nil, nil
		case 1:
			return &LineString{Points: kept[0]}, nil
		}
		return &MultiLineString{kept}, nil
	}
	return sortRings(ctx, lines)
}

type areaRing struct {
	ring []Coord
	area float64
}

// sortRings sorts rings into polygons with holes: outer rings run clockwise,
// holes counter-clockwise. A hole is tried against the outer rings smallest
// first, so it stops at the first (smallest) one that contains it.
func sortRings(ctx context.Context, lines [][]Coord) (Geometry, error) {
	var rings []areaRing
	for l, line := range lines {
		if l%1024 == 1023 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if r := closeRing(line); r != nil {
			if a := signedArea(r); a != 0 {
				rings = append(rings, areaRing{r, a})
			}
		}
	}
	if len(rings) == 0 {
		return nil, nil
	}
	var outers, holes []areaRing
	for _, r := range rings {
		if r.area < 0 {
			outers = append(outers, r)
		} else {
			holes = append(holes, r)
		}
	}
	if len(outers) == 0 {
		// Every ring runs counter-clockwise: the writer used the other convention, so they are all outer rings.
		outers, holes = holes, nil
	}
	type box struct{ minX, minY, maxX, maxY float64 }
	bounds := make([]box, len(outers))
	for i, o := range outers {
		if i%1024 == 1023 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		b := box{math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64}
		for _, p := range o.ring {
			b.minX, b.minY = math.Min(b.minX, p.X), math.Min(b.minY, p.Y)
			b.maxX, b.maxY = math.Max(b.maxX, p.X), math.Max(b.maxY, p.Y)
		}
		bounds[i] = b
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	smallestFirst := make([]int, len(outers))
	for i := range smallestFirst {
		smallestFirst[i] = i
	}
	sort.SliceStable(smallestFirst, func(a, b int) bool {
		return math.Abs(outers[smallestFirst[a]].area) < math.Abs(outers[smallestFirst[b]].area)
	})
	assigned := make([][][]Coord, len(outers))
	for i, o := range outers {
		assigned[i] = [][]Coord{o.ring}
	}
	for h, hole := range holes {
		if (h+1)%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		probe := hole.ring[0]
		found := false
		for _, i := range smallestFirst {
			b := bounds[i]
			if probe.X < b.minX || probe.X > b.maxX || probe.Y < b.minY || probe.Y > b.maxY {
				continue
			}
			if !ringContains(outers[i].ring, probe) {
				continue
			}
			assigned[i] = append(assigned[i], hole.ring)
			found = true
			break
		}
		if !found {
			assigned = append(assigned, [][]Coord{hole.ring}) // a hole outside every outer ring is a ring of its own
		}
	}
	polygons := make([]*Polygon, len(assigned))
	for i, rs := range assigned {
		polygons[i] = &Polygon{rs}
	}
	if len(polygons) == 1 {
		return polygons[0], nil
	}
	return &MultiPolygon{polygons}, nil
}

// planShapefiles sorts the features into the shapefiles to write: one per
// kind of geometry present. Collections are split into their members (each a
// record with the same attributes); features without geometry go to the first file.
func planShapefiles(ds *Dataset) []*shapefileGroup {
	buckets := map[shapeKind][]*Feature{}
	var without []*Feature
	for _, f := range ds.Features {
		if f.Geometry == nil {
			without = append(without, f)
			continue
		}
		for _, part := range flattenCollections(f.Geometry) {
			k := kindOf(part)
			buckets[k] = append(buckets[k], &Feature{Geometry: part, Properties: f.Properties})
		}
	}
	var groups []*shapefileGroup
	for _, k := range []shapeKind{shapePoint, shapeLine, shapePolygon} {
		if len(buckets[k]) > 0 {
			groups = append(groups, &shapefileGroup{k, buckets[k]})
		}
	}
	if len(groups) == 0 {
		groups = append(groups, &shapefileGroup{kind: shapePoint})
	}
	groups[0].records = append(groups[0].records, without...)
	return groups
}

func flattenCollections(g Geometry) []Geometry {
	if c, ok := g.(*Collection); ok {
		var out []Geometry
		for _, m := range c.Geometries {
			out = append(out, flattenCollections(m)...)
		}
		return out
	}
	return []Geometry{g}
}

func kindOf(g Geometry) shapeKind {
	switch g.(type) {
	case *Point, *MultiPoint:
		return shapePoint
	case *LineString, *MultiLineString:
		return shapeLine
	}
	return shapePolygon
}

type extent struct{ minX, minY, maxX, maxY, minZ, maxZ float64 }

func newExtent() *extent {
	return &extent{math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64}
}

func (e *extent) empty() bool { return e.minX > e.maxX }

func (e *extent) add(c Coord) {
	e.minX, e.maxX = math.Min(e.minX, c.X), math.Max(e.maxX, c.X)
	e.minY, e.maxY = math.Min(e.minY, c.Y), math.Max(e.maxY, c.Y)
	z := 0.0
	if c.HasZ() {
		z = c.Z
	}
	e.minZ, e.maxZ = math.Min(e.minZ, z), math.Max(e.maxZ, z)
}

func shpFileHeader(shapeType int32, lengthInWords int32, b *extent) []byte {
	h := make([]byte, shpHeaderLength)
	binary.BigEndian.PutUint32(h, 9994)
	binary.BigEndian.PutUint32(h[24:], uint32(lengthInWords))
	binary.LittleEndian.PutUint32(h[28:], 1000)
	binary.LittleEndian.PutUint32(h[32:], uint32(shapeType))
	values := []float64{0, 0, 0, 0, 0, 0, 0, 0}
	if !b.empty() {
		values = []float64{b.minX, b.minY, b.maxX, b.maxY, b.minZ, b.maxZ, 0, 0}
	}
	for i, v := range values {
		binary.LittleEndian.PutUint64(h[36+8*i:], math.Float64bits(v))
	}
	return h
}

func changeExt(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + "." + ext
}

// writeShapefile writes group as shpPath with its .shx, .dbf, .prj and .cpg beside it.
func writeShapefile(ctx context.Context, g *shapefileGroup, shpPath string, dropHeights bool) (dbfLosses, error) {
	withZ := false
	multi := false
	for _, r := range g.records {
		if r.Geometry != nil && !dropHeights && hasZ(r.Geometry) {
			withZ = true
		}
		if mp, ok := r.Geometry.(*MultiPoint); ok && len(mp.Points) != 1 {
			multi = true
		}
	}
	var shapeType int32
	switch {
	case g.kind == shapePoint && multi && withZ:
		shapeType = 18
	case g.kind == shapePoint && multi:
		shapeType = 8
	case g.kind == shapePoint && withZ:
		shapeType = 11
	case g.kind == shapePoint:
		shapeType = 1
	case g.kind == shapeLine && withZ:
		shapeType = 13
	case g.kind == shapeLine:
		shapeType = 3
	case withZ:
		shapeType = 15
	default:
		shapeType = 5
	}
	bounds := newExtent()
	length := int64(shpHeaderLength)
	type entry struct{ offset, words int32 }
	var index []entry
	f, err := os.Create(shpPath)
	if err != nil {
		return dbfLosses{}, err
	}
	w := bufio.NewWriterSize(f, 1<<16)
	w.Write(make([]byte, shpHeaderLength))
	head := make([]byte, 8)
	for n, r := range g.records {
		if n&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				f.Close()
				return dbfLosses{}, err
			}
		}
		content, err := encodeShape(r.Geometry, shapeType, bounds)
		if err != nil {
			f.Close()
			return dbfLosses{}, err
		}
		length += 8 + int64(len(content))
		if length > math.MaxInt32 {
			f.Close()
			return dbfLosses{}, convert.Refuse("error.geo_too_large", "")
		}
		binary.BigEndian.PutUint32(head, uint32(n+1))
		binary.BigEndian.PutUint32(head[4:], uint32(len(content)/2))
		w.Write(head)
		w.Write(content)
		index = append(index, entry{int32((length - 8 - int64(len(content))) / 2), int32(len(content) / 2)})
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return dbfLosses{}, err
	}
	if _, err := f.WriteAt(shpFileHeader(shapeType, int32(length/2), bounds), 0); err != nil {
		f.Close()
		return dbfLosses{}, err
	}
	if err := f.Close(); err != nil {
		return dbfLosses{}, err
	}

	shx := shpFileHeader(shapeType, int32((shpHeaderLength+8*len(index))/2), bounds)
	for _, e := range index {
		shx = binary.BigEndian.AppendUint32(shx, uint32(e.offset))
		shx = binary.BigEndian.AppendUint32(shx, uint32(e.words))
	}
	if err := os.WriteFile(changeExt(shpPath, "shx"), shx, 0o644); err != nil {
		return dbfLosses{}, err
	}
	losses, err := writeDbf(ctx, changeExt(shpPath, "dbf"), g.records)
	if err != nil {
		return losses, err
	}
	if err := os.WriteFile(changeExt(shpPath, "prj"), []byte(wgs84Prj), 0o644); err != nil {
		return losses, err
	}
	return losses, os.WriteFile(changeExt(shpPath, "cpg"), []byte("UTF-8"), 0o644)
}

// encodeShape is the content of one record: its shape type, then the shape.
func encodeShape(g Geometry, fileType int32, b *extent) ([]byte, error) {
	z := fileType == 11 || fileType == 13 || fileType == 15 || fileType == 18
	switch x := g.(type) {
	case nil:
		return []byte{0, 0, 0, 0}, nil
	case *Point:
		return encodePoints(fileType, []Coord{x.Position}, b, z), nil
	case *MultiPoint:
		return encodePoints(fileType, x.Points, b, z), nil
	case *LineString:
		return encodeParts(fileType, [][]Coord{x.Points}, b, z), nil
	case *MultiLineString:
		return encodeParts(fileType, x.Lines, b, z), nil
	case *Polygon:
		return encodeParts(fileType, shapeRings([]*Polygon{x}), b, z), nil
	case *MultiPolygon:
		return encodeParts(fileType, shapeRings(x.Polygons), b, z), nil
	}
	return nil, fmt.Errorf("A %s can't be written to a shapefile record.", g.TypeName())
}

// shapeRings: outer rings clockwise, holes counter-clockwise.
func shapeRings(polygons []*Polygon) [][]Coord {
	var rings [][]Coord
	for _, p := range polygons {
		for i, r := range p.Rings {
			rings = append(rings, oriented(r, i != 0))
		}
	}
	return rings
}

func putF64(b []byte, at int, v float64) { binary.LittleEndian.PutUint64(b[at:], math.Float64bits(v)) }

func encodePoints(fileType int32, points []Coord, b *extent, z bool) []byte {
	for _, p := range points {
		b.add(p)
	}
	if fileType == 1 || fileType == 11 {
		size := 20
		if fileType == 11 {
			size = 36
		}
		c := make([]byte, size)
		binary.LittleEndian.PutUint32(c, uint32(fileType))
		putF64(c, 4, points[0].X)
		putF64(c, 12, points[0].Y)
		if fileType == 11 {
			h := 0.0
			if points[0].HasZ() {
				h = points[0].Z
			}
			putF64(c, 20, h)
			putF64(c, 28, shpNoData)
		}
		return c
	}
	n := len(points)
	size := 40 + 16*n
	if z {
		size += 16 + 8*n
	}
	c := make([]byte, size)
	binary.LittleEndian.PutUint32(c, uint32(fileType))
	writeBox(c, points)
	binary.LittleEndian.PutUint32(c[36:], uint32(n))
	for i, p := range points {
		putF64(c, 40+16*i, p.X)
		putF64(c, 48+16*i, p.Y)
	}
	if z {
		writeHeights(c, 40+16*n, points)
	}
	return c
}

func encodeParts(fileType int32, parts [][]Coord, b *extent, z bool) []byte {
	var all []Coord
	for _, p := range parts {
		all = append(all, p...)
	}
	for _, p := range all {
		b.add(p)
	}
	size := 44 + 4*len(parts) + 16*len(all)
	if z {
		size += 16 + 8*len(all)
	}
	c := make([]byte, size)
	binary.LittleEndian.PutUint32(c, uint32(fileType))
	writeBox(c, all)
	binary.LittleEndian.PutUint32(c[36:], uint32(len(parts)))
	binary.LittleEndian.PutUint32(c[40:], uint32(len(all)))
	start := 0
	for i, p := range parts {
		binary.LittleEndian.PutUint32(c[44+4*i:], uint32(start))
		start += len(p)
	}
	pointsAt := 44 + 4*len(parts)
	for i, p := range all {
		putF64(c, pointsAt+16*i, p.X)
		putF64(c, pointsAt+16*i+8, p.Y)
	}
	if z {
		writeHeights(c, pointsAt+16*len(all), all)
	}
	return c
}

func writeBox(c []byte, points []Coord) {
	minX, minY, maxX, maxY := math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64
	for _, p := range points {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	if len(points) == 0 {
		minX, minY, maxX, maxY = 0, 0, 0, 0
	}
	putF64(c, 4, minX)
	putF64(c, 12, minY)
	putF64(c, 20, maxX)
	putF64(c, 28, maxY)
}

func writeHeights(c []byte, at int, points []Coord) {
	lo, hi := math.MaxFloat64, -math.MaxFloat64
	heights := make([]float64, len(points))
	for i, p := range points {
		if p.HasZ() {
			heights[i] = p.Z
		}
		lo, hi = math.Min(lo, heights[i]), math.Max(hi, heights[i])
	}
	if len(points) == 0 {
		lo, hi = 0, 0
	}
	putF64(c, at, lo)
	putF64(c, at+8, hi)
	for i, h := range heights {
		putF64(c, at+16+8*i, h)
	}
}
