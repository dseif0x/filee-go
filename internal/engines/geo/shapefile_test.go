package geo

// Shapefile in and out. The shapefiles and dBASE tables here are written from
// the ESRI specification, independent of the engine, for the cases GDAL won't
// write on purpose; written files are parsed here rather than read back with
// the engine.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const testWgs84Prj = `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`

type pt struct{ x, y float64 }

func le32(v int32) []byte  { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }
func dbl(v float64) []byte { return binary.LittleEndian.AppendUint64(nil, math.Float64bits(v)) }

type shapefileBuilder struct {
	shapeType int32
	records   [][]byte
}

func newShapefileBuilder(t int32) *shapefileBuilder { return &shapefileBuilder{shapeType: t} }

func (b *shapefileBuilder) add(c []byte) *shapefileBuilder {
	b.records = append(b.records, c)
	return b
}
func (b *shapefileBuilder) null() *shapefileBuilder { return b.add(le32(0)) }
func (b *shapefileBuilder) point(x, y float64) *shapefileBuilder {
	return b.add(slices.Concat(le32(1), dbl(x), dbl(y)))
}
func (b *shapefileBuilder) pointZ(x, y, z float64) *shapefileBuilder {
	return b.add(slices.Concat(le32(11), dbl(x), dbl(y), dbl(z), dbl(-1e39)))
}

func boxOf(points []pt) []byte {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range points {
		minX, minY, maxX, maxY = math.Min(minX, p.x), math.Min(minY, p.y), math.Max(maxX, p.x), math.Max(maxY, p.y)
	}
	return slices.Concat(dbl(minX), dbl(minY), dbl(maxX), dbl(maxY))
}

func (b *shapefileBuilder) multiPoint(points ...pt) *shapefileBuilder {
	c := slices.Concat(le32(8), boxOf(points), le32(int32(len(points))))
	for _, p := range points {
		c = slices.Concat(c, dbl(p.x), dbl(p.y))
	}
	return b.add(c)
}

// parts writes a PolyLine (3) or Polygon (5) record; the caller sets the ring direction.
func (b *shapefileBuilder) parts(typ int32, parts ...[]pt) *shapefileBuilder {
	var all []pt
	for _, p := range parts {
		all = append(all, p...)
	}
	c := slices.Concat(le32(typ), boxOf(all), le32(int32(len(parts))), le32(int32(len(all))))
	start := 0
	for _, p := range parts {
		c = append(c, le32(int32(start))...)
		start += len(p)
	}
	for _, p := range all {
		c = slices.Concat(c, dbl(p.x), dbl(p.y))
	}
	return b.add(c)
}

func shpHeader(shapeType int32, bytes int) []byte {
	h := make([]byte, 100)
	binary.BigEndian.PutUint32(h, 9994)
	binary.BigEndian.PutUint32(h[24:], uint32(bytes/2))
	binary.LittleEndian.PutUint32(h[28:], 1000)
	binary.LittleEndian.PutUint32(h[32:], uint32(shapeType))
	return h
}

func (b *shapefileBuilder) save(t testing.TB, folder, name, prj string, dbf *dbfBuilder) string {
	var shp, shx []byte
	for i, c := range b.records {
		shx = binary.BigEndian.AppendUint32(shx, uint32((100+len(shp))/2))
		shx = binary.BigEndian.AppendUint32(shx, uint32(len(c)/2))
		shp = binary.BigEndian.AppendUint32(shp, uint32(i+1))
		shp = binary.BigEndian.AppendUint32(shp, uint32(len(c)/2))
		shp = append(shp, c...)
	}
	path := filepath.Join(folder, name+".shp")
	os.WriteFile(path, slices.Concat(shpHeader(b.shapeType, 100+len(shp)), shp), 0o644)
	os.WriteFile(filepath.Join(folder, name+".shx"), slices.Concat(shpHeader(b.shapeType, 100+len(shx)), shx), 0o644)
	if prj != "" {
		os.WriteFile(filepath.Join(folder, name+".prj"), []byte(prj), 0o644)
	}
	if dbf != nil {
		dbf.save(t, filepath.Join(folder, name+".dbf"))
	}
	return path
}

type dbfTestField struct {
	name             string
	typ              byte
	length, decimals int
}

type dbfTestRow struct {
	deleted bool
	cells   []string
}

type dbfBuilder struct {
	fields         []dbfTestField
	rows           []dbfTestRow
	languageDriver byte
	enc            encoding.Encoding
}

func (d *dbfBuilder) field(name string, typ byte, length, decimals int) *dbfBuilder {
	d.fields = append(d.fields, dbfTestField{name, typ, length, decimals})
	return d
}

func (d *dbfBuilder) row(cells ...string) *dbfBuilder {
	d.rows = append(d.rows, dbfTestRow{false, cells})
	return d
}

func (d *dbfBuilder) deletedRow(cells ...string) *dbfBuilder {
	d.rows = append(d.rows, dbfTestRow{true, cells})
	return d
}

func (d *dbfBuilder) save(t testing.TB, path string) {
	recordLength := 1
	for _, f := range d.fields {
		recordLength += f.length
	}
	headerLength := 32 + 32*len(d.fields) + 1
	h := make([]byte, 32)
	h[0], h[1], h[2], h[3] = 3, 124, 1, 1
	binary.LittleEndian.PutUint32(h[4:], uint32(len(d.rows)))
	binary.LittleEndian.PutUint16(h[8:], uint16(headerLength))
	binary.LittleEndian.PutUint16(h[10:], uint16(recordLength))
	h[29] = d.languageDriver
	out := h
	for _, f := range d.fields {
		desc := make([]byte, 32)
		copy(desc, f.name)
		desc[11], desc[16], desc[17] = f.typ, byte(f.length), byte(f.decimals)
		out = append(out, desc...)
	}
	out = append(out, 0x0D)
	for _, r := range d.rows {
		if r.deleted {
			out = append(out, '*')
		} else {
			out = append(out, ' ')
		}
		for i, f := range d.fields {
			cell := bytes.Repeat([]byte{' '}, f.length)
			text := ""
			if i < len(r.cells) {
				text = r.cells[i]
			}
			raw := []byte(text)
			if d.enc != nil {
				var err error
				if raw, err = d.enc.NewEncoder().Bytes([]byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			if f.typ == 'N' || f.typ == 'F' {
				copy(cell[max(0, len(cell)-len(raw)):], raw)
			} else {
				copy(cell, raw[:min(len(raw), len(cell))])
			}
			out = append(out, cell...)
		}
	}
	out = append(out, 0x1A)
	os.WriteFile(path, out, 0o644)
}

func outer(lo, hi float64) []pt { return []pt{{lo, lo}, {lo, hi}, {hi, hi}, {hi, lo}, {lo, lo}} }
func hole(lo, hi float64) []pt  { return []pt{{lo, lo}, {hi, lo}, {hi, hi}, {lo, hi}, {lo, lo}} }

func newShapefile(t *testing.T, b *shapefileBuilder, prj string, dbf *dbfBuilder) string {
	return b.save(t, t.TempDir(), "data", prj, dbf)
}

func TestShapefilePolygonsAndHoles(t *testing.T) {
	g := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, outer(0, 10), hole(2, 4)), testWgs84Prj, nil))[0].geometry(t)
	var rings [][][]float64
	json.Unmarshal(g.Coordinates, &rings)
	if g.Type != "Polygon" || len(rings) != 2 {
		t.Fatal(g.Type)
	}
	// Holes go to the outer ring that contains them, whatever the order of the parts.
	g = toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, outer(0, 10), outer(20, 30), hole(22, 24), hole(2, 4), hole(6, 8)), testWgs84Prj, nil))[0].geometry(t)
	var polys [][][][]float64
	json.Unmarshal(g.Coordinates, &polys)
	if g.Type != "MultiPolygon" || len(polys) != 2 || len(polys[0]) != 3 || len(polys[1]) != 2 {
		t.Fatal(g.Type, len(polys))
	}
	// An island inside a hole is a polygon of its own.
	g = toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, outer(0, 10), hole(2, 8), outer(4, 6)), testWgs84Prj, nil))[0].geometry(t)
	polys = nil
	json.Unmarshal(g.Coordinates, &polys)
	if g.Type != "MultiPolygon" || len(polys) != 2 {
		t.Fatal(g.Type)
	}
	// A hole outside every outer ring is kept as a ring of its own; all counter-clockwise rings are all outer rings.
	g = toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, outer(0, 10), hole(20, 30)), testWgs84Prj, nil))[0].geometry(t)
	if g.Type != "MultiPolygon" {
		t.Fatal(g.Type)
	}
	g = toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, hole(0, 10), hole(20, 30)), testWgs84Prj, nil))[0].geometry(t)
	polys = nil
	json.Unmarshal(g.Coordinates, &polys)
	if g.Type != "MultiPolygon" || len(polys) != 2 {
		t.Fatal(g.Type)
	}
	// Open rings are closed, degenerate ones dropped; only degenerate rings: no geometry.
	g = toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(5).parts(5, []pt{{0, 0}, {0, 4}, {4, 4}, {4, 0}}, []pt{{1, 1}, {2, 2}, {1, 1}}), testWgs84Prj, nil))[0].geometry(t)
	rings = nil
	json.Unmarshal(g.Coordinates, &rings)
	if g.Type != "Polygon" || len(rings) != 1 || len(rings[0]) != 5 {
		t.Fatal(g.Type, rings)
	}
	ds, err := readShapefile(context.Background(), newShapefile(t, newShapefileBuilder(5).parts(5, []pt{{1, 1}, {2, 2}, {3, 3}, {1, 1}}), testWgs84Prj, nil))
	if err != nil || ds.Features[0].Geometry != nil {
		t.Fatal(err)
	}
}

func TestSortingTwentyThousandRingsIsQuickAndCancellable(t *testing.T) {
	var lines [][]Coord
	for i := 0; i < 10000; i++ {
		x := float64(i * 20)
		lines = append(lines, []Coord{xy(x, 0), xy(x, 10), xy(x+10, 10), xy(x+10, 0), xy(x, 0)})
		lines = append(lines, []Coord{xy(x+2, 2), xy(x+4, 2), xy(x+4, 4), xy(x+2, 4), xy(x+2, 2)})
	}
	g, err := sortRings(context.Background(), lines)
	if err != nil || len(g.(*MultiPolygon).Polygons) != 10000 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sortRings(ctx, lines); err == nil {
		t.Fatal("not cancelled")
	}
}

func TestShapesOfEveryKindAreRead(t *testing.T) {
	path := newShapefile(t, newShapefileBuilder(1).point(1, 2).null().pointZ(3, 4, 50).pointZ(5, 6, -1e39).point(math.NaN(), math.NaN()).
		multiPoint(pt{1, 1}, pt{2, 2}).parts(3, []pt{{0, 0}, {1, 1}}, []pt{{2, 2}, {3, 3}}), testWgs84Prj, nil)
	ds, err := readShapefile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	g := ds.Features
	if len(g) != 7 || g[1].Geometry != nil || g[4].Geometry != nil {
		t.Fatal(len(g))
	}
	if p := g[2].Geometry.(*Point).Position; p.Z != 50 {
		t.Fatal(p)
	}
	if p := g[3].Geometry.(*Point).Position; p.HasZ() {
		t.Fatal(p) // no data is not a height
	}
	if _, ok := g[5].Geometry.(*MultiPoint); !ok {
		t.Fatal(g[5].Geometry)
	}
	if _, ok := g[6].Geometry.(*MultiLineString); !ok {
		t.Fatal(g[6].Geometry)
	}
}

func TestDbfFieldTypes(t *testing.T) {
	dbf := (&dbfBuilder{}).field("NAME", 'C', 10, 0).field("N0", 'N', 5, 0).field("N3", 'N', 10, 3).field("F2", 'F', 8, 2).
		field("FLAG", 'L', 1, 0).field("DAY", 'D', 8, 0).field("MEMO", 'M', 10, 0).field("NEG", 'N', 6, 1).
		row("abc", "   42", "     1.500", "    3.14", "T", "20240229", "0000000001", "  -2.5").
		row("", "", "", "", "?", "20241399", "", "*****").
		row("ünï", "12.0", "0", "  -0.5", "n", "     ", "", "   0.0")
	f := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(1, 1).point(2, 2).point(3, 3), testWgs84Prj, dbf))
	r0, r1, r2 := f[0].props(t), f[1].props(t), f[2].props(t)
	num := func(v any) string {
		if n, ok := v.(json.Number); ok {
			return n.String()
		}
		return fmt.Sprint(v)
	}
	if r0["NAME"] != "abc" || num(r0["N0"]) != "42" || num(r0["N3"]) != "1.5" || num(r0["F2"]) != "3.14" || r0["FLAG"] != true ||
		r0["DAY"] != "2024-02-29" || r0["MEMO"] != nil || num(r0["NEG"]) != "-2.5" {
		t.Fatal(r0)
	}
	if r1["NAME"] != "" || r1["N0"] != nil || r1["N3"] != nil || r1["FLAG"] != nil || r1["DAY"] != nil || r1["NEG"] != nil {
		t.Fatal(r1)
	}
	if num(r2["N0"]) != "12" || r2["FLAG"] != false || num(r2["F2"]) != "-0.5" {
		t.Fatal(r2)
	}
}

func TestDbfDeletedRowsMissingTablesAndRowCounts(t *testing.T) {
	f := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(1, 1).point(2, 2).point(3, 3), testWgs84Prj,
		(&dbfBuilder{}).field("ID", 'N', 3, 0).row("1").deletedRow("2").row("3")))
	if len(f) != 2 || f[0].props(t)["ID"].(json.Number).String() != "1" || f[1].props(t)["ID"].(json.Number).String() != "3" {
		t.Fatal(len(f))
	}
	if keys := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(1, 1), testWgs84Prj, nil))[0].propKeys(t); len(keys) != 0 {
		t.Fatal(keys)
	}
	if f := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(1, 1).point(2, 2), testWgs84Prj, (&dbfBuilder{}).field("ID", 'N', 3, 0).row("1").row("2").row("3"))); len(f) != 2 {
		t.Fatal(len(f))
	}
	key, detail := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(1, 1).point(2, 2), testWgs84Prj, (&dbfBuilder{}).field("ID", 'N', 3, 0).row("1")), "geojson")
	if key != "error.conversion_failed" || !strings.Contains(detail, "1 rows but the .shp has 2") {
		t.Fatal(key, detail)
	}
	// Fields of length zero read as empty.
	if f := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(1, 1), testWgs84Prj,
		(&dbfBuilder{}).field("C", 'C', 0, 0).field("N", 'N', 0, 0).field("L", 'L', 0, 0).field("I", 'I', 0, 0).field("B", 'B', 0, 0).row())); len(f) != 1 {
		t.Fatal(len(f))
	}
}

func TestDbfTextIsDecodedWithTheCpgTheLanguageDriverOrByDetection(t *testing.T) {
	for _, c := range []struct {
		text   string
		enc    encoding.Encoding
		driver byte
		cpg    string
	}{
		{"서울특별시 종로구", korean.EUCKR, 0x4E, ""},
		{"서울특별시 종로구", korean.EUCKR, 0, "949"},
		{"서울특별시 종로구", korean.EUCKR, 0, "CP949"},
		{"서울특별시 종로구", korean.EUCKR, 0, ""},
		{"서울 Ünïcödé 北京", nil, 0, "UTF-8"},
		{"서울 Ünïcödé 北京", nil, 0, ""},
		{"Müller - Café", charmap.Windows1252, 0x57, ""},
		{"Müller - Café", charmap.Windows1252, 0x03, ""},
		{"Müller - Café", charmap.Windows1252, 0, "1252"},
		{"Müller", charmap.Windows1252, 0, "ISO-8859-1"},
		{"Müller", charmap.Windows1252, 0, "LATIN1"},
		{"Müller", charmap.Windows1252, 0, ""},
		{"北京市朝阳区", simplifiedchinese.GBK, 0x4D, ""},
		{"北京市朝阳区", simplifiedchinese.GBK, 0, "GBK"},
		{"日本語テキスト", japanese.ShiftJIS, 0x13, ""},
		{"Привет мир", charmap.Windows1251, 0xC9, ""},
		{"Привет мир", charmap.Windows1251, 0, "windows-1251"},
	} {
		folder := t.TempDir()
		dbf := (&dbfBuilder{enc: c.enc, languageDriver: c.driver}).field("NAME", 'C', 40, 0).row(c.text)
		shp := newShapefileBuilder(1).point(1, 1).save(t, folder, "enc", testWgs84Prj, dbf)
		if c.cpg != "" {
			os.WriteFile(filepath.Join(folder, "enc.cpg"), []byte(c.cpg), 0o644)
		}
		if got := toGeoJSONFeatures(t, shp)[0].props(t)["NAME"]; got != c.text {
			t.Fatalf("%q (cpg %q, driver %x): %q", c.text, c.cpg, c.driver, got)
		}
	}
	// Korean code page names in a .cpg.
	for _, cpg := range []string{"UHC", "MS949", "windows-949", "KS_C_5601-1987", "cp949"} {
		folder := t.TempDir()
		shp := newShapefileBuilder(1).point(1, 1).save(t, folder, "k", testWgs84Prj, (&dbfBuilder{enc: korean.EUCKR}).field("NAME", 'C', 20, 0).row("서울"))
		os.WriteFile(filepath.Join(folder, "k.cpg"), []byte(cpg), 0o644)
		if got := toGeoJSONFeatures(t, shp)[0].props(t)["NAME"]; got != "서울" {
			t.Fatal(cpg, got)
		}
	}
}

func TestSidecarsAreFoundWhateverTheLetterCase(t *testing.T) {
	folder := t.TempDir()
	newShapefileBuilder(1).point(1, 2).save(t, folder, "mixed", testWgs84Prj, (&dbfBuilder{}).field("ID", 'N', 3, 0).row("7"))
	os.Rename(filepath.Join(folder, "mixed.dbf"), filepath.Join(folder, "mixed.DBF"))
	os.Rename(filepath.Join(folder, "mixed.prj"), filepath.Join(folder, "mixed.Prj"))
	if f := toGeoJSONFeatures(t, filepath.Join(folder, "mixed.shp")); f[0].props(t)["ID"].(json.Number).String() != "7" {
		t.Fatal(f[0].props(t))
	}
}

const utm33Prj = `PROJCS["WGS_1984_UTM_Zone_33N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",15.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`

func TestShapefileCoordinateSystems(t *testing.T) {
	for _, prj := range []string{utm33Prj, "\uFEFF" + utm33Prj + "\r\n", utm33Prj + "\n\n"} {
		p := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(602065.2074110014, 5340353.594303575), prj, nil))[0].position(t)
		if !sameFloats(p, []float64{16.3738, 48.2082}, 5e-9) {
			t.Fatal(p)
		}
	}
	if p := toGeoJSONFeatures(t, newShapefile(t, newShapefileBuilder(1).point(16.37, 48.2), "", nil))[0].position(t); !sameFloats(p, []float64{16.37, 48.2}, 0) {
		t.Fatal(p)
	}
	if key, _ := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(602065.2, 5340353.6), "", nil), "geojson"); key != "error.geo_unknown_crs" {
		t.Fatal(key)
	}
	for _, prj := range []string{" ", "\r\n"} {
		if key, _ := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(602065.2, 5340353.6), prj, nil), "geojson"); key != "error.geo_unknown_crs" {
			t.Fatal(key) // an empty .prj counts as none
		}
	}
	laea := `PROJCS["ETRS89_LAEA_Europe",GEOGCS["GCS_ETRS_1989",DATUM["D_ETRS_1989",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Lambert_Azimuthal_Equal_Area"],PARAMETER["False_Easting",4321000.0],PARAMETER["False_Northing",3210000.0],PARAMETER["Central_Meridian",10.0],PARAMETER["Latitude_Of_Origin",52.0],UNIT["Meter",1.0]]`
	if key, detail := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(4321000, 3210000), laea, nil), "geojson"); key != "error.geo_unsupported_crs" || !strings.Contains(detail, "ETRS89_LAEA_Europe") {
		t.Fatal(key, detail)
	}
	if key, _ := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(1, 1), "hello", nil), "geojson"); key != "error.geo_unsupported_crs" {
		t.Fatal(key)
	}
	if key, _ := convertFails(t, newShapefile(t, newShapefileBuilder(1).point(5e9, 5e9), utm33Prj, nil), "geojson"); key != "error.geo_bad_coordinates" {
		t.Fatal(key)
	}
	dhdn := `PROJCS["DHDN_3_Degree_Gauss_Zone_3",GEOGCS["GCS_Deutsches_Hauptdreiecksnetz",DATUM["D_Deutsches_Hauptdreiecksnetz",SPHEROID["Bessel_1841",6377397.155,299.1528128]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Gauss_Kruger"],PARAMETER["False_Easting",3500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",9.0],PARAMETER["Scale_Factor",1.0],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	if r := convertTo(t, newShapefile(t, newShapefileBuilder(1).point(3500000, 5400000), dhdn, nil), "geojson"); r.err != nil || r.notice != "notice.geo_datum_assumed" {
		t.Fatal(r.err, r.notice)
	}
}

func TestDamagedAndUnsupportedShapefiles(t *testing.T) {
	patch := newShapefileBuilder(31).add(slices.Concat(le32(31), make([]byte, 60))).save(t, t.TempDir(), "patch", testWgs84Prj, nil)
	if key, detail := convertFails(t, patch, "geojson"); key != "error.geo_unsupported_shape" || !strings.Contains(detail, "MultiPatch") {
		t.Fatal(key, detail)
	}
	good := newShapefile(t, newShapefileBuilder(1).point(1, 1).point(2, 2), testWgs84Prj, nil)
	data, _ := os.ReadFile(good)
	write := func(b []byte) string {
		p := filepath.Join(t.TempDir(), "d.shp")
		os.WriteFile(p, b, 0o644)
		return p
	}
	notShp := slices.Clone(data)
	notShp[3] = 0
	cut := slices.Clone(data[:len(data)-4])
	binary.BigEndian.PutUint32(cut[24:], uint32(len(data)/2))
	badCount := newShapefile(t, newShapefileBuilder(8).add(slices.Concat(le32(8), make([]byte, 32), le32(1_000_000_000))), testWgs84Prj, nil)
	badParts := newShapefile(t, newShapefileBuilder(5).add(slices.Concat(le32(5), make([]byte, 32), le32(1), le32(1), le32(5), dbl(0), dbl(0))), testWgs84Prj, nil)
	for _, c := range []struct{ path, want string }{
		{write(notShp), "shapefile header"}, {write(data[:50]), "shapefile header"}, {write(cut), "cut off or damaged at shape 2"},
		{badCount, "point count"}, {badParts, "part 1 starts outside"},
	} {
		if _, detail := convertFails(t, c.path, "geojson"); !strings.Contains(detail, c.want) {
			t.Fatalf("%q: %q", c.want, detail)
		}
	}
	// A wrong length in the header does not hide the shapes.
	wrong := slices.Clone(data)
	binary.BigEndian.PutUint32(wrong[24:], 10)
	if f := toGeoJSONFeatures(t, write(wrong)); len(f) != 2 {
		t.Fatal(len(f))
	}
	// A damaged .dbf.
	folder := t.TempDir()
	shp := newShapefileBuilder(1).point(1, 1).save(t, folder, "x", testWgs84Prj, nil)
	os.WriteFile(filepath.Join(folder, "x.dbf"), []byte("short"), 0o644)
	if _, detail := convertFails(t, shp, "geojson"); !strings.Contains(detail, ".dbf") {
		t.Fatal(detail)
	}
}

// ---- writing, checked against the specification ----

type shpParsed struct {
	typ     int32
	records [][]byte
	data    []byte
}

func parseShp(t *testing.T, path string) shpParsed {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int32(binary.BigEndian.Uint32(data)) != 9994 || binary.LittleEndian.Uint32(data[28:]) != 1000 || int(binary.BigEndian.Uint32(data[24:]))*2 != len(data) {
		t.Fatal("header")
	}
	var records [][]byte
	pos := 100
	for pos < len(data) {
		if int(binary.BigEndian.Uint32(data[pos:])) != len(records)+1 {
			t.Fatal("record number")
		}
		n := int(binary.BigEndian.Uint32(data[pos+4:])) * 2
		records = append(records, data[pos+8:pos+8+n])
		pos += 8 + n
	}
	return shpParsed{int32(binary.LittleEndian.Uint32(data[32:])), records, data}
}

func checkShx(t *testing.T, shp string) {
	shx, _ := os.ReadFile(changeExt(shp, "shx"))
	data, _ := os.ReadFile(shp)
	if int(binary.BigEndian.Uint32(shx[24:]))*2 != len(shx) || !bytes.Equal(shx[32:100], data[32:100]) {
		t.Fatal("shx header")
	}
	pos := 100
	for i := 0; i < (len(shx)-100)/8; i++ {
		offset := int(binary.BigEndian.Uint32(shx[100+8*i:])) * 2
		words := binary.BigEndian.Uint32(shx[104+8*i:])
		if offset != pos || words != binary.BigEndian.Uint32(data[offset+4:]) {
			t.Fatal("shx entry", i)
		}
		pos += 8 + int(words)*2
	}
}

type dbfParsed struct {
	rows, recordLength, headerLength int
	fields                           []dbfTestField
	data                             []byte
}

func parseDbfFile(t *testing.T, path string) dbfParsed {
	data, _ := os.ReadFile(path)
	d := dbfParsed{rows: int(binary.LittleEndian.Uint32(data[4:])), headerLength: int(binary.LittleEndian.Uint16(data[8:])), recordLength: int(binary.LittleEndian.Uint16(data[10:])), data: data}
	at := 32
	for data[at] != 0x0D {
		d.fields = append(d.fields, dbfTestField{strings.TrimRight(string(data[at:at+11]), "\x00"), data[at+11], int(data[at+16]), int(data[at+17])})
		at += 32
	}
	sum := 1
	for _, f := range d.fields {
		sum += f.length
	}
	if data[0] != 3 || d.headerLength != at+1 || d.recordLength != sum || len(data) != d.headerLength+d.rows*d.recordLength+1 || data[len(data)-1] != 0x1A {
		t.Fatal("dbf layout")
	}
	return d
}

func (d dbfParsed) cell(row, field int) string {
	start := d.headerLength + row*d.recordLength + 1
	for _, f := range d.fields[:field] {
		start += f.length
	}
	return string(d.data[start : start+d.fields[field].length])
}

func featureCollection(features ...string) string {
	return `{"type":"FeatureCollection","features":[` + strings.Join(features, ",") + "]}"
}

func feature(geometry, properties string) string {
	if properties == "" {
		properties = "{}"
	}
	return `{"type":"Feature","properties":` + properties + `,"geometry":` + geometry + `}`
}

func toShapefile(t *testing.T, json, name string) (string, []string) {
	files := convertOK(t, writeText(t, name, json), "shp")
	return first(files, "shp"), files
}

func TestWrittenPointsMakeAFullSetOfConsistentFiles(t *testing.T) {
	shp, files := toShapefile(t, featureCollection(
		feature(`{"type":"Point","coordinates":[10.5,20.25]}`, `{"name":"A","n":1}`),
		feature(`{"type":"Point","coordinates":[-5,40]}`, `{"name":"B","n":2}`)), "in.geojson")
	var exts []string
	for _, f := range files {
		exts = append(exts, strings.TrimPrefix(filepath.Ext(f), "."))
	}
	sort.Strings(exts)
	if strings.Join(exts, ",") != "cpg,dbf,prj,shp,shx" {
		t.Fatal(exts)
	}
	p := parseShp(t, shp)
	if p.typ != 1 || len(p.records) != 2 || len(p.records[0]) != 20 || f64(p.records[0], 4) != 10.5 || f64(p.records[0], 12) != 20.25 {
		t.Fatal(p.typ)
	}
	if f64(p.data, 36) != -5 || f64(p.data, 44) != 20.25 || f64(p.data, 52) != 10.5 || f64(p.data, 60) != 40 {
		t.Fatal("bounds")
	}
	checkShx(t, shp)
	d := parseDbfFile(t, changeExt(shp, "dbf"))
	if d.rows != 2 || fmt.Sprint(d.fields) != "[{name 67 1 0} {n 78 1 0}]" || d.cell(1, 0) != "B" {
		t.Fatal(d.fields)
	}
	cpg, _ := os.ReadFile(changeExt(shp, "cpg"))
	prj, _ := os.ReadFile(changeExt(shp, "prj"))
	if string(cpg) != "UTF-8" || !strings.HasPrefix(string(prj), `GEOGCS["GCS_WGS_1984"`) {
		t.Fatal(string(cpg), string(prj))
	}
}

func TestWrittenPolygonsAreClockwiseWithHolesCounterClockwise(t *testing.T) {
	shp, _ := toShapefile(t, featureCollection(feature(`{"type":"Polygon","coordinates":[[[0,0],[4,0],[4,4],[0,4],[0,0]],[[1,1],[1,2],[2,2],[2,1],[1,1]]]}`, "")), "in.geojson")
	p := parseShp(t, shp)
	r := p.records[0]
	if p.typ != 5 || binary.LittleEndian.Uint32(r[36:]) != 2 || binary.LittleEndian.Uint32(r[40:]) != 10 || binary.LittleEndian.Uint32(r[48:]) != 5 {
		t.Fatal(p.typ)
	}
	area := func(from, to int) float64 {
		var s float64
		for i := from; i < to-1; i++ {
			s += f64(r, 52+16*i)*f64(r, 60+16*(i+1)) - f64(r, 52+16*(i+1))*f64(r, 60+16*i)
		}
		return s / 2
	}
	if area(0, 5) != -16 || area(5, 10) != 1 {
		t.Fatal(area(0, 5), area(5, 10))
	}
	checkShx(t, shp)
}

func TestHeightMakesZShapeTypes(t *testing.T) {
	points, _ := toShapefile(t, featureCollection(feature(`{"type":"Point","coordinates":[1,2,30]}`, ""), feature(`{"type":"Point","coordinates":[3,4]}`, "")), "z1.geojson")
	p := parseShp(t, points)
	if p.typ != 11 || f64(p.records[0], 20) != 30 || f64(p.records[1], 20) != 0 || len(p.records[0]) != 36 {
		t.Fatal(p.typ)
	}
	lines, _ := toShapefile(t, featureCollection(feature(`{"type":"LineString","coordinates":[[0,0,5],[1,1,6]]}`, "")), "z2.geojson")
	polygons, _ := toShapefile(t, featureCollection(feature(`{"type":"Polygon","coordinates":[[[0,0,1],[1,0,1],[1,1,1],[0,0,1]]]}`, "")), "z3.geojson")
	if parseShp(t, lines).typ != 13 || parseShp(t, polygons).typ != 15 {
		t.Fatal("z types")
	}
	checkShx(t, lines)
	multi, _ := toShapefile(t, featureCollection(feature(`{"type":"MultiPoint","coordinates":[[1,1],[2,2],[3,3]]}`, "")), "m.geojson")
	single, _ := toShapefile(t, featureCollection(feature(`{"type":"MultiPoint","coordinates":[[1,1]]}`, "")), "s.geojson")
	if parseShp(t, multi).typ != 8 || parseShp(t, single).typ != 1 {
		t.Fatal("multipoint types")
	}
}

func TestMixedGeometryIsSplitIntoOneShapefilePerKind(t *testing.T) {
	json := featureCollection(
		feature(`{"type":"Point","coordinates":[1,1]}`, `{"a":1}`),
		feature(`{"type":"LineString","coordinates":[[0,0],[1,1]]}`, `{"a":2}`),
		feature(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`, `{"a":3}`),
		feature(`{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[5,5]},{"type":"LineString","coordinates":[[2,2],[3,3]]}]}`, `{"a":4}`),
		feature("null", `{"a":5}`))
	r := convertTo(t, writeText(t, "mixed.geojson", json), "shp")
	if r.err != nil || r.notice != "notice.geo_shapefile_split" || len(r.outputs) != 15 {
		t.Fatal(r.err, r.notice, len(r.outputs))
	}
	var shapes []string
	for _, o := range r.outputs {
		if strings.HasSuffix(o, ".shp") {
			shapes = append(shapes, o)
		}
	}
	sort.Strings(shapes)
	var names []string
	for _, s := range shapes {
		names = append(names, filepath.Base(s))
	}
	if strings.Join(names, ",") != "mixed_lines.shp,mixed_points.shp,mixed_polygons.shp" {
		t.Fatal(names)
	}
	lines, points := parseShp(t, shapes[0]), parseShp(t, shapes[1])
	if lines.typ != 3 || len(lines.records) != 2 || points.typ != 1 || len(points.records) != 3 || binary.LittleEndian.Uint32(points.records[2]) != 0 || parseShp(t, shapes[2]).typ != 5 {
		t.Fatal("groups")
	}
	for _, s := range shapes {
		checkShx(t, s)
	}
	d := parseDbfFile(t, changeExt(shapes[0], "dbf"))
	if strings.TrimSpace(d.cell(0, 0)) != "2" || strings.TrimSpace(d.cell(1, 0)) != "4" {
		t.Fatal("collection attributes")
	}
	// Features without any geometry make a point shapefile of null shapes.
	shp, _ := toShapefile(t, featureCollection(feature("null", `{"a":1}`), feature("null", `{"a":2}`)), "null.geojson")
	if p := parseShp(t, shp); len(p.records) != 2 || binary.LittleEndian.Uint32(p.records[0]) != 0 || parseDbfFile(t, changeExt(shp, "dbf")).rows != 2 {
		t.Fatal("null shapes")
	}
}

func TestAttributeColumnsGetTheDbfTypeThatHoldsTheirValues(t *testing.T) {
	rows := []string{
		`{"i":-12,"f":1.5,"b":true,"s":"text","mixed":1,"nums":2,"allnull":null,"big":123456789012,"tiny":0.0000001}`,
		`{"i":7,"f":-0.125,"b":false,"s":"länger","mixed":"x","nums":3.25,"allnull":null,"big":5,"tiny":2}`,
		`{"i":null,"f":null,"b":null,"s":null,"mixed":null,"nums":null,"allnull":null,"big":null,"tiny":null}`,
	}
	var features []string
	for _, r := range rows {
		features = append(features, feature(`{"type":"Point","coordinates":[1,1]}`, r))
	}
	shp, _ := toShapefile(t, featureCollection(features...), "types.geojson")
	d := parseDbfFile(t, changeExt(shp, "dbf"))
	f := map[string]dbfTestField{}
	index := map[string]int{}
	for i, x := range d.fields {
		f[x.name], index[x.name] = x, i
	}
	check := func(name string, typ byte, length, decimals int) {
		if x := f[name]; x.typ != typ || length >= 0 && x.length != length || decimals >= 0 && x.decimals != decimals {
			t.Fatalf("%s: %+v", name, x)
		}
	}
	check("i", 'N', 3, 0)
	check("f", 'N', -1, 3)
	check("b", 'L', 1, 0)
	check("s", 'C', 7, 0)
	check("mixed", 'C', -1, -1)
	check("nums", 'N', -1, 2)
	check("allnull", 'C', 1, 0)
	check("big", 'N', 12, 0)
	check("tiny", 'N', -1, 7)
	for _, c := range [][2]string{
		{strings.TrimSpace(d.cell(0, index["i"])), "-12"}, {strings.TrimSpace(d.cell(1, index["f"])), "-0.125"},
		{d.cell(0, index["b"]), "T"}, {d.cell(1, index["b"]), "F"}, {d.cell(2, index["b"]), "?"},
		{strings.TrimRight(d.cell(1, index["s"]), " "), "länger"}, {strings.TrimSpace(d.cell(0, index["tiny"])), "0.0000001"},
	} {
		if c[0] != c[1] {
			t.Fatalf("%q != %q", c[0], c[1])
		}
	}
	if strings.TrimSpace(d.cell(2, index["i"])) != "" {
		t.Fatal("null number")
	}
}

func TestFieldNamesTextAndColumnsThatDoNotFit(t *testing.T) {
	props := `{"population_2020":1,"population_2021":2,"population_2022":3,"my col!":4,"도시이름도시이름도시":5,"short":6,"Short":7,"":8}`
	r := convertTo(t, writeText(t, "names.geojson", featureCollection(feature(`{"type":"Point","coordinates":[1,1]}`, props))), "shp")
	if r.err != nil || r.notice != "notice.geo_field_names" {
		t.Fatal(r.err, r.notice)
	}
	var names []string
	for _, f := range parseDbfFile(t, changeExt(first(r.outputs, "shp"), "dbf")).fields {
		names = append(names, f.name)
	}
	if strings.Join(names, ",") != "population,populati_1,populati_2,my_col_,도시이,short,Short_1,FIELD" {
		t.Fatal(names)
	}
	korean := strings.Repeat("한", 100) // 300 bytes
	r = convertTo(t, writeText(t, "long.geojson", featureCollection(feature(`{"type":"Point","coordinates":[1,1]}`, `{"t":"`+korean+`","e":"`+strings.Repeat("x", 300)+`"}`))), "shp")
	if r.notice != "notice.geo_text_cut" {
		t.Fatal(r.notice)
	}
	shp := first(r.outputs, "shp")
	d := parseDbfFile(t, changeExt(shp, "dbf"))
	if d.fields[0].length != 254 || d.fields[1].length != 254 || strings.TrimRight(d.cell(0, 0), " ") != strings.Repeat("한", 84) || d.cell(0, 1) != strings.Repeat("x", 254) {
		t.Fatal("cut")
	}
	if toGeoJSONFeatures(t, shp)[0].props(t)["t"] != strings.Repeat("한", 84) {
		t.Fatal("read back")
	}
	var wide []string
	for i := 1; i <= 300; i++ {
		wide = append(wide, `"c`+strconv.Itoa(i)+`":`+strconv.Itoa(i))
	}
	r = convertTo(t, writeText(t, "wide.geojson", featureCollection(feature(`{"type":"Point","coordinates":[1,1]}`, "{"+strings.Join(wide, ",")+"}"))), "shp")
	d = parseDbfFile(t, changeExt(first(r.outputs, "shp"), "dbf"))
	if r.notice != "notice.geo_columns_dropped" || len(d.fields) != 255 || d.fields[254].name != "c255" {
		t.Fatal(r.notice, len(d.fields))
	}
	shp, _ = toShapefile(t, featureCollection(feature(`{"type":"Point","coordinates":[1,1]}`, ""), feature(`{"type":"Point","coordinates":[2,2]}`, "")), "fid.geojson")
	d = parseDbfFile(t, changeExt(shp, "dbf"))
	if len(d.fields) != 1 || d.fields[0].name != "FID" || strings.TrimSpace(d.cell(0, 0)) != "0" || strings.TrimSpace(d.cell(1, 0)) != "1" {
		t.Fatal(d.fields)
	}
}

func TestManyRandomPositionsSurviveAtFullPrecision(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	var features []string
	var points []pt
	for i := 0; i < 5000; i++ {
		p := pt{r.Float64()*360 - 180, r.Float64()*180 - 90}
		points = append(points, p)
		features = append(features, feature(`{"type":"Point","coordinates":[`+strconv.FormatFloat(p.x, 'g', -1, 64)+`,`+strconv.FormatFloat(p.y, 'g', -1, 64)+`]}`, `{"i":`+strconv.Itoa(i)+`}`))
	}
	shp, _ := toShapefile(t, featureCollection(features...), "random.geojson")
	out := toGeoJSONFeatures(t, shp)
	for i, p := range points {
		q := out[i].position(t)
		if math.Abs(q[0]-p.x) >= 1e-9 || math.Abs(q[1]-p.y) >= 1e-9 || out[i].props(t)["i"].(json.Number).String() != strconv.Itoa(i) {
			t.Fatal(i)
		}
	}
}

func TestALeftoverSiblingIsNeverOverwritten(t *testing.T) {
	folder := t.TempDir()
	input := filepath.Join(folder, "orphan.geojson")
	os.WriteFile(input, []byte(featureCollection(feature(`{"type":"Point","coordinates":[1,1]}`, ""))), 0o644)
	out := filepath.Join(folder, "out")
	os.Mkdir(out, 0o755)
	os.WriteFile(filepath.Join(out, "orphan.dbf"), []byte("someone else's file"), 0o644)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	alloc := convert.AllocatorFunc(func(ext, suffix string) string {
		// Like the app's allocator: the next free name for the .shp only.
		for n := 1; ; n++ {
			name := "orphan" + suffix
			if n > 1 {
				name += " (" + strconv.Itoa(n) + ")"
			}
			p := filepath.Join(out, name+"."+ext)
			if !exists(p) {
				return p
			}
		}
	})
	step := &convert.Step{InputPath: input, From: "geojson", To: "shp", Output: alloc}
	files, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "orphan.dbf")); string(b) != "someone else's file" {
		t.Fatal("overwritten")
	}
	for _, f := range files {
		if !strings.HasPrefix(filepath.Base(f), "orphan_2.") {
			t.Fatal(f)
		}
	}
}

func TestAFailedShapefileConversionLeavesNothingBehind(t *testing.T) {
	folder := t.TempDir()
	input := filepath.Join(folder, "broken.geojson")
	os.WriteFile(input, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1]}}]}`), 0o644)
	step := &convert.Step{InputPath: input, From: "geojson", To: "shp",
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(folder, "broken"+suffix+"."+ext) })}
	if _, err := New().Convert(context.Background(), step, nil); err == nil {
		t.Fatal("no error")
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 1 {
		t.Fatal(entries)
	}
}

func TestDropHeightsWritesTwoDimensionalRecords(t *testing.T) {
	first := mustTime("2026-10-24T01:02:03Z")
	last := mustTime("2026-10-24T01:04:03Z")
	ds := newDataset("ride")
	ds.SparseHeights = true
	ds.Features = []*Feature{
		{Geometry: &LineString{Points: []Coord{{10, 20, 100}, xy(11, 21), {12, 22, 300}}, Times: []*timeT{&first, nil, &last}}, Properties: []Prop{{"type", "running"}}},
		{Geometry: &Point{Coord{14, 24, 40}}},
	}
	dir := t.TempDir()
	for _, g := range planShapefiles(ds) {
		path := filepath.Join(dir, fmt.Sprint(g.kind)+".shp")
		if _, err := writeShapefile(context.Background(), g, path, true); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		want := uint32(3)
		if g.kind == shapePoint {
			want = 1
		}
		if binary.LittleEndian.Uint32(data[32:]) != want || binary.LittleEndian.Uint32(data[108:]) != want {
			t.Fatal(g.kind)
		}
		back, err := readShapefile(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		in, out := g.records[0].Geometry.Coordinates(), back.Features[0].Geometry.Coordinates()
		for i := range in {
			if in[i].X != out[i].X || in[i].Y != out[i].Y || out[i].HasZ() {
				t.Fatal(in, out)
			}
		}
	}
}
