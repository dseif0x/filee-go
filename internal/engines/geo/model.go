package geo

// The in-memory model every geospatial vector format is read into and written
// from: features (a geometry and an ordered list of attributes) in longitude /
// latitude. Each reader produces it and each writer consumes it, so GeoJSON,
// KML, KMZ, GPX, Shapefile and CSV convert to each other through this one shape.

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Coord is one position: X = longitude and Y = latitude in degrees once a file
// is read, Z the height or NaN.
type Coord struct{ X, Y, Z float64 }

func xy(x, y float64) Coord { return Coord{x, y, math.NaN()} }

// HasZ reports whether the position has a height.
func (c Coord) HasZ() bool { return !math.IsNaN(c.Z) }

// IsFinite reports whether X and Y are real numbers.
func (c Coord) IsFinite() bool { return isFinite(c.X) && isFinite(c.Y) }

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// Geometry is a point, line, polygon, a collection of them, or a collection of mixed kinds.
type Geometry interface {
	// TypeName is the OGC name of the type, e.g. "Polygon".
	TypeName() string
	// Coordinates are every position of the geometry, in order.
	Coordinates() []Coord
	// Map returns the same geometry with f applied to every position.
	Map(f func(Coord) Coord) Geometry
	// Parts are the simple parts: the geometry itself unless it is a collection or a Multi* geometry.
	Parts() []Geometry
}

func hasZ(g Geometry) bool {
	for _, c := range g.Coordinates() {
		if c.HasZ() {
			return true
		}
	}
	return false
}

func mapLine(line []Coord, f func(Coord) Coord) []Coord {
	out := make([]Coord, len(line))
	for i, c := range line {
		out[i] = f(c)
	}
	return out
}

type Point struct{ Position Coord }

func (g *Point) TypeName() string                 { return "Point" }
func (g *Point) Coordinates() []Coord             { return []Coord{g.Position} }
func (g *Point) Map(f func(Coord) Coord) Geometry { return &Point{f(g.Position)} }
func (g *Point) Parts() []Geometry                { return []Geometry{g} }

type MultiPoint struct{ Points []Coord }

func (g *MultiPoint) TypeName() string                 { return "MultiPoint" }
func (g *MultiPoint) Coordinates() []Coord             { return g.Points }
func (g *MultiPoint) Map(f func(Coord) Coord) Geometry { return &MultiPoint{mapLine(g.Points, f)} }
func (g *MultiPoint) Parts() []Geometry {
	out := make([]Geometry, len(g.Points))
	for i, p := range g.Points {
		out[i] = &Point{p}
	}
	return out
}

// LineString is a line; Times are UTC times aligned with Points (nil entries
// for unknown times), or nil when point times are unknown.
type LineString struct {
	Points []Coord
	Times  []*time.Time
}

func (g *LineString) TypeName() string     { return "LineString" }
func (g *LineString) Coordinates() []Coord { return g.Points }
func (g *LineString) Map(f func(Coord) Coord) Geometry {
	return &LineString{mapLine(g.Points, f), g.Times}
}
func (g *LineString) Parts() []Geometry { return []Geometry{g} }

type MultiLineString struct{ Lines [][]Coord }

func (g *MultiLineString) TypeName() string { return "MultiLineString" }
func (g *MultiLineString) Coordinates() []Coord {
	var out []Coord
	for _, l := range g.Lines {
		out = append(out, l...)
	}
	return out
}
func (g *MultiLineString) Map(f func(Coord) Coord) Geometry {
	out := make([][]Coord, len(g.Lines))
	for i, l := range g.Lines {
		out[i] = mapLine(l, f)
	}
	return &MultiLineString{out}
}
func (g *MultiLineString) Parts() []Geometry {
	out := make([]Geometry, len(g.Lines))
	for i, l := range g.Lines {
		out[i] = &LineString{Points: l}
	}
	return out
}

// Polygon: the first ring is the outer boundary, the others are holes. Rings are closed (first = last).
type Polygon struct{ Rings [][]Coord }

func (g *Polygon) TypeName() string { return "Polygon" }
func (g *Polygon) Coordinates() []Coord {
	var out []Coord
	for _, r := range g.Rings {
		out = append(out, r...)
	}
	return out
}
func (g *Polygon) Map(f func(Coord) Coord) Geometry {
	out := make([][]Coord, len(g.Rings))
	for i, r := range g.Rings {
		out[i] = mapLine(r, f)
	}
	return &Polygon{out}
}
func (g *Polygon) Parts() []Geometry { return []Geometry{g} }

type MultiPolygon struct{ Polygons []*Polygon }

func (g *MultiPolygon) TypeName() string { return "MultiPolygon" }
func (g *MultiPolygon) Coordinates() []Coord {
	var out []Coord
	for _, p := range g.Polygons {
		out = append(out, p.Coordinates()...)
	}
	return out
}
func (g *MultiPolygon) Map(f func(Coord) Coord) Geometry {
	out := make([]*Polygon, len(g.Polygons))
	for i, p := range g.Polygons {
		out[i] = p.Map(f).(*Polygon)
	}
	return &MultiPolygon{out}
}
func (g *MultiPolygon) Parts() []Geometry {
	out := make([]Geometry, len(g.Polygons))
	for i, p := range g.Polygons {
		out[i] = p
	}
	return out
}

// Collection holds geometries of any kinds in one feature (GeoJSON GeometryCollection, KML MultiGeometry).
type Collection struct{ Geometries []Geometry }

func (g *Collection) TypeName() string { return "GeometryCollection" }
func (g *Collection) Coordinates() []Coord {
	var out []Coord
	for _, m := range g.Geometries {
		out = append(out, m.Coordinates()...)
	}
	return out
}
func (g *Collection) Map(f func(Coord) Coord) Geometry {
	out := make([]Geometry, len(g.Geometries))
	for i, m := range g.Geometries {
		out[i] = m.Map(f)
	}
	return &Collection{out}
}
func (g *Collection) Parts() []Geometry {
	var out []Geometry
	for _, m := range g.Geometries {
		out = append(out, m.Parts()...)
	}
	return out
}

// Prop is one attribute. Value is a string, int64, float64, bool or nil.
type Prop struct {
	Key   string
	Value any
}

// Feature is a geometry (nil when the record has no location) and its attributes in file order.
type Feature struct {
	Geometry   Geometry
	Properties []Prop
}

// Find returns the value of the first attribute called one of names (compared without case).
func (f *Feature) Find(names ...string) (any, bool) {
	for _, n := range names {
		for _, p := range f.Properties {
			if strings.EqualFold(p.Key, n) {
				return p.Value, true
			}
		}
	}
	return nil, false
}

// FindText is Find as text; "" for a missing or empty value.
func (f *Feature) FindText(names ...string) string {
	if v, ok := f.Find(names...); ok {
		return toText(v)
	}
	return ""
}

// Dataset is everything read from one file.
type Dataset struct {
	// Name is a name for the data, e.g. the file name; KML and GPX use it as the document name.
	Name     string
	Features []*Feature
	// Crs is the coordinate system the positions are in until the engine converts them to WGS 84.
	Crs Crs
	// CrsDeclared: the file said which system it uses (a .prj, a GeoJSON crs member).
	CrsDeclared bool
	// RowsWithoutLocation: some rows of a CSV had no usable location and so have no geometry.
	RowsWithoutLocation bool
	// SparseHeights: a missing height must stay absent rather than be written as zero.
	SparseHeights bool
}

func newDataset(name string) *Dataset { return &Dataset{Name: name, Crs: wgs84} }

// Columns are the attribute names of all features, in the order they first appear.
func (d *Dataset) Columns() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range d.Features {
		for _, p := range f.Properties {
			if !seen[p.Key] {
				seen[p.Key] = true
				out = append(out, p.Key)
			}
		}
	}
	return out
}

// ---- text forms ----

func trimNumber(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// degrees writes a coordinate with up to ten decimals (about 0.01 mm), no exponent.
func degrees(v float64) string { return trimNumber(strconv.FormatFloat(v, 'f', 10, 64)) }

// height writes a height with up to six decimals.
func height(v float64) string { return trimNumber(strconv.FormatFloat(v, 'f', 6, 64)) }

// number is the shortest text that reads back as the same number, never with
// an exponent a spreadsheet would trip on.
func number(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 0) {
		if v > 0 {
			return "∞"
		}
		return "-∞"
	}
	a := math.Abs(v)
	if a != 0 && a < 1e-5 {
		return trimNumber(strconv.FormatFloat(v, 'f', 20, 64))
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// toText is an attribute value as text: numbers as number, booleans as true / false, nil as empty.
func toText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return number(x)
	}
	return ""
}

// typed is text from a file as the most specific attribute value: whole
// number, number, true / false, else the text.
func typed(text string) any {
	if text == "" {
		return nil
	}
	if len([]rune(text)) > 24 {
		return text
	}
	// "007" and "+1" are identifiers or phone numbers, not numbers.
	digits := strings.TrimPrefix(text, "-")
	if digits == "" || len(digits) > 1 && digits[0] == '0' && digits[1] != '.' {
		return text
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		return v
	}
	if (digits[0] >= '0' && digits[0] <= '9' || digits[0] == '.') && !strings.ContainsAny(text, "_xXpP") {
		if v, err := strconv.ParseFloat(text, 64); err == nil && isFinite(v) {
			return v
		}
	}
	switch text {
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	return text
}

// ---- rings ----

func sameXY(a, b Coord) bool { return a.X == b.X && a.Y == b.Y }

// closeRing returns the ring closed (first position repeated at the end), or
// nil if it has fewer than three distinct positions and so encloses nothing.
func closeRing(ring []Coord) []Coord {
	if len(ring) < 3 {
		return nil
	}
	points := ring
	if !sameXY(ring[0], ring[len(ring)-1]) {
		points = append(append([]Coord(nil), ring...), ring[0])
	}
	if len(points) < 4 {
		return nil
	}
	return points
}

// signedArea is twice the area with a sign: positive when the ring runs counter-clockwise (X right, Y up).
func signedArea(ring []Coord) float64 {
	var sum float64
	for i := 0; i+1 < len(ring); i++ {
		sum += ring[i].X*ring[i+1].Y - ring[i+1].X*ring[i].Y
	}
	return sum
}

// oriented is the ring in the direction asked for (reversed when it runs the other way).
func oriented(ring []Coord, counterClockwise bool) []Coord {
	a := signedArea(ring)
	if a == 0 || (a > 0) == counterClockwise {
		return ring
	}
	out := make([]Coord, len(ring))
	for i, c := range ring {
		out[len(ring)-1-i] = c
	}
	return out
}

// ringContains: point lies inside ring (even-odd rule).
func ringContains(ring []Coord, p Coord) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		if (ring[i].Y > p.Y) != (ring[j].Y > p.Y) &&
			p.X < (ring[j].X-ring[i].X)*(p.Y-ring[i].Y)/(ring[j].Y-ring[i].Y)+ring[i].X {
			inside = !inside
		}
	}
	return inside
}

// key lowercases the letters and digits of a name ("Lat (deg)" → "latdeg").
func key(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// asciiKey keeps only ASCII letters and digits, lower case (projection and parameter names).
func asciiKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

const isoTime = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Format(isoTime) }
