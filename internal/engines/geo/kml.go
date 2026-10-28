package geo

// KML and KMZ in and out. Reading takes every Placemark wherever it sits
// (Document, Folder, nested folders) with its name, description and
// ExtendedData (Data and SimpleData) as attributes, and Point, LineString,
// LinearRing, Polygon, MultiGeometry and Google's gx:Track as geometry.
// Styles, icons, overlays, network links and time spans are not data of the
// features and are left out. KMZ is a ZIP with the .kml inside (doc.kml
// first). Writing makes one Document with a Placemark per feature; attributes
// go to ExtendedData. A line with a time and a height on every point (a FIT
// activity) is written as a gx:Track; other timed lines keep their times as
// coordTimes data. KML positions are always WGS 84 longitude, latitude, height.

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	kmlNamespace = "http://www.opengis.net/kml/2.2"
	gxNamespace  = "http://www.google.com/kml/ext/2.2"
	// maxKmlBytes is the largest .kml a KMZ may hold, declared or really unpacked.
	maxKmlBytes         = 256 << 20
	kmlMaxGeometryDepth = 32
)

func readKml(ctx context.Context, path string, zipped bool, maxBytes int64) (*Dataset, error) {
	var root *xnode
	if zipped {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, fmt.Errorf("The file is not a valid KMZ archive (%v)", err)
		}
		defer zr.Close()
		var entry *zip.File
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, "doc.kml") {
				entry = f
				break
			}
		}
		if entry == nil {
			for _, f := range zr.File {
				if strings.HasSuffix(strings.ToLower(f.Name), ".kml") {
					entry = f
					break
				}
			}
		}
		if entry == nil {
			return nil, errors.New("The KMZ has no .kml file inside.")
		}
		if entry.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("The .kml inside the KMZ is too large (%d MB, the limit is %d MB).", entry.UncompressedSize64>>20, maxBytes>>20)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("The file is not a valid KMZ archive (%v)", err)
		}
		defer rc.Close()
		// The size in a ZIP header can be wrong: the unpacked bytes are counted as they are read.
		limited := &limitedReader{r: rc, n: maxBytes, message: fmt.Sprintf("The .kml inside the KMZ is larger than %d MB.", maxBytes>>20)}
		if root, err = loadXML(limited, xmlMaxElements); err != nil {
			if limited.n < 0 {
				return nil, errors.New(limited.message)
			}
			return nil, err
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if root, err = loadXML(f, xmlMaxElements); err != nil {
			return nil, err
		}
	}
	if !strings.EqualFold(root.local, "kml") {
		return nil, errors.New("The file is XML but not KML: the root element is not <kml>.")
	}
	ds := newDataset(baseName(path))
	var err error
	count := 0
	root.descendants(func(n *xnode) {
		if err != nil || n.local != "Placemark" {
			return
		}
		count++
		if count&0x3FF == 0 {
			if err = ctx.Err(); err != nil {
				return
			}
		}
		var f *Feature
		if f, err = readPlacemark(n); err == nil {
			ds.Features = append(ds.Features, f)
		}
	})
	if err != nil {
		return nil, err
	}
	return ds, nil
}

func childText(n *xnode, local string) (string, bool) {
	c := n.first(local)
	if c == nil {
		return "", false
	}
	return strings.TrimSpace(c.value()), true
}

func readPlacemark(p *xnode) (*Feature, error) {
	f := &Feature{}
	if name, _ := childText(p, "name"); name != "" {
		f.Properties = append(f.Properties, Prop{"name", name})
	}
	if d, _ := childText(p, "description"); d != "" {
		f.Properties = append(f.Properties, Prop{"description", d})
	}
	names := map[string]bool{}
	for _, pr := range f.Properties {
		names[strings.ToLower(pr.Key)] = true
	}
	p.descendants(func(d *xnode) {
		if d.local != "Data" && d.local != "SimpleData" {
			return
		}
		k, _ := d.attr("name")
		if k == "" {
			return
		}
		value := d.value()
		if d.local == "Data" {
			if v, ok := childText(d, "value"); ok {
				value = v
			}
		}
		if !names[strings.ToLower(k)] {
			names[strings.ToLower(k)] = true
			f.Properties = append(f.Properties, Prop{k, typed(strings.TrimSpace(value))})
		}
	})
	var geometries []Geometry
	for _, e := range p.elements() {
		g, err := readKmlGeometry(e, 0)
		if err != nil {
			return nil, err
		}
		if g != nil {
			geometries = append(geometries, g)
		}
	}
	switch len(geometries) {
	case 0:
	case 1:
		f.Geometry = geometries[0]
	default:
		f.Geometry = &Collection{geometries}
	}
	return f, nil
}

func readKmlGeometry(e *xnode, depth int) (Geometry, error) {
	if depth > kmlMaxGeometryDepth {
		return nil, fmt.Errorf("The KML nests geometries more than %d levels deep.", kmlMaxGeometryDepth)
	}
	switch e.local {
	case "Point":
		points, err := kmlCoordinates(e)
		if err != nil || len(points) == 0 {
			return nil, err
		}
		return &Point{points[0]}, nil
	case "LineString", "LinearRing":
		line, err := kmlCoordinates(e)
		if err != nil || len(line) < 2 {
			return nil, err
		}
		return &LineString{Points: line}, nil
	case "Track", "MultiTrack":
		// gx:Track lists <gx:coord>lon lat alt</gx:coord>; a MultiTrack holds several.
		tracks := []*xnode{e}
		if e.local == "MultiTrack" {
			tracks = e.named("Track")
		}
		var lines [][]Coord
		for _, t := range tracks {
			line, err := kmlTrack(t)
			if err != nil {
				return nil, err
			}
			if len(line) >= 2 {
				lines = append(lines, line)
			}
		}
		switch len(lines) {
		case 0:
			return nil, nil
		case 1:
			return &LineString{Points: lines[0]}, nil
		}
		return &MultiLineString{lines}, nil
	case "Polygon":
		var boundaries []*xnode
		for _, b := range e.elements() {
			if b.local == "outerBoundaryIs" || b.local == "innerBoundaryIs" {
				boundaries = append(boundaries, b)
			}
		}
		sort.SliceStable(boundaries, func(i, j int) bool {
			return boundaries[i].local == "outerBoundaryIs" && boundaries[j].local != "outerBoundaryIs"
		})
		var rings [][]Coord
		for _, b := range boundaries {
			var ring *xnode
			b.descendants(func(n *xnode) {
				if ring == nil && n.local == "LinearRing" {
					ring = n
				}
			})
			if ring == nil {
				continue
			}
			line, err := kmlCoordinates(ring)
			if err != nil {
				return nil, err
			}
			if c := closeRing(line); c != nil {
				rings = append(rings, c)
			}
		}
		if len(rings) == 0 {
			return nil, nil
		}
		return &Polygon{rings}, nil
	case "MultiGeometry":
		var parts []Geometry
		for _, c := range e.elements() {
			g, err := readKmlGeometry(c, depth+1)
			if err != nil {
				return nil, err
			}
			if g != nil {
				parts = append(parts, g)
			}
		}
		if len(parts) == 0 {
			return nil, nil
		}
		if len(parts) == 1 {
			return parts[0], nil
		}
		allPoints, allLines, allPolygons := true, true, true
		for _, p := range parts {
			_, isPoint := p.(*Point)
			_, isLine := p.(*LineString)
			_, isPolygon := p.(*Polygon)
			allPoints, allLines, allPolygons = allPoints && isPoint, allLines && isLine, allPolygons && isPolygon
		}
		switch {
		case allPoints:
			mp := &MultiPoint{}
			for _, p := range parts {
				mp.Points = append(mp.Points, p.(*Point).Position)
			}
			return mp, nil
		case allLines:
			ml := &MultiLineString{}
			for _, p := range parts {
				ml.Lines = append(ml.Lines, p.(*LineString).Points)
			}
			return ml, nil
		case allPolygons:
			mp := &MultiPolygon{}
			for _, p := range parts {
				mp.Polygons = append(mp.Polygons, p.(*Polygon))
			}
			return mp, nil
		}
		return &Collection{parts}, nil
	}
	return nil, nil
}

func xmlNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !isFinite(v) || strings.ContainsAny(s, "_xXpP") {
		return 0, false
	}
	return v, true
}

func shorten(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:40]) + "…"
}

// kmlCoordinates are the positions of the <coordinates> child: "lon,lat[,alt]" tuples separated by white space.
func kmlCoordinates(e *xnode) ([]Coord, error) {
	c := e.first("coordinates")
	var line []Coord
	if c == nil {
		return line, nil
	}
	// Some writers put spaces after the commas ("1, 2, 3"): glue those back before splitting on white space.
	text := glueCommas(c.value())
	for _, tuple := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) {
		parts := strings.Split(tuple, ",")
		if len(parts) < 2 {
			return nil, fmt.Errorf("A KML position is not \"longitude,latitude\": %q.", shorten(tuple))
		}
		x, ok1 := xmlNumber(parts[0])
		y, ok2 := xmlNumber(parts[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("A KML position is not \"longitude,latitude\": %q.", shorten(tuple))
		}
		z := math.NaN()
		if len(parts) > 2 {
			if h, ok := xmlNumber(parts[2]); ok {
				z = h
			}
		}
		line = append(line, Coord{x, y, z})
	}
	return line, nil
}

// glueCommas drops the white space next to commas (a run of white space
// elsewhere shrinks to its first character), in linear time.
func glueCommas(text string) string {
	if !strings.Contains(text, ",") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	var space rune // white space seen since the last character: kept unless a comma is next to it
	afterComma := false
	for _, ch := range text {
		if unicode.IsSpace(ch) {
			if !afterComma && space == 0 {
				space = ch
			}
			continue
		}
		if ch != ',' && space != 0 {
			b.WriteRune(space)
		}
		space = 0
		b.WriteRune(ch)
		afterComma = ch == ','
	}
	if space != 0 {
		b.WriteRune(space)
	}
	return b.String()
}

func kmlTrack(t *xnode) ([]Coord, error) {
	var line []Coord
	for _, c := range t.named("coord") {
		parts := strings.Fields(c.value())
		var x, y float64
		ok := len(parts) >= 2
		if ok {
			var ok1, ok2 bool
			x, ok1 = xmlNumber(parts[0])
			y, ok2 = xmlNumber(parts[1])
			ok = ok1 && ok2
		}
		if !ok {
			return nil, fmt.Errorf("A KML track position is not \"longitude latitude\": %q.", shorten(c.value()))
		}
		z := math.NaN()
		if len(parts) > 2 {
			if h, ok := xmlNumber(parts[2]); ok {
				z = h
			}
		}
		line = append(line, Coord{x, y, z})
	}
	return line, nil
}

// ---- writing ----

// writeKml writes a .kml, or a .kmz (a ZIP holding doc.kml) when zipped.
func writeKml(ctx context.Context, ds *Dataset, path string, zipped bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if !zipped {
		err = writeKmlDocument(ctx, ds, f)
	} else {
		zw := zip.NewWriter(f)
		var w io.Writer
		if w, err = zw.Create("doc.kml"); err == nil {
			if err = writeKmlDocument(ctx, ds, w); err == nil {
				err = zw.Close()
			}
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func writeKmlDocument(ctx context.Context, ds *Dataset, w io.Writer) error {
	x := newXMLWriter(w)
	x.start("kml", "xmlns", kmlNamespace)
	x.start("Document")
	x.element("name", cleanXML(ds.Name))
	for i, f := range ds.Features {
		if i&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		writePlacemark(x, f, ds.SparseHeights)
	}
	x.end()
	x.end()
	return x.flush()
}

func writePlacemark(x *xmlWriter, f *Feature, sparse bool) {
	x.start("Placemark")
	line, _ := f.Geometry.(*LineString)
	track := line != nil && isTrack(line)
	name := f.FindText("name", "title", "label")
	if name != "" {
		x.element("name", cleanXML(name))
	}
	description := f.FindText("description", "desc", "comment")
	if description != "" {
		x.element("description", cleanXML(description))
	}
	nameKey := firstKey(f, "name", "title", "label")
	descKey := firstKey(f, "description", "desc", "comment")
	var extra []Prop
	for _, p := range f.Properties {
		if name != "" && strings.EqualFold(p.Key, nameKey) || description != "" && strings.EqualFold(p.Key, descKey) {
			continue
		}
		if line != nil && line.Times != nil && strings.EqualFold(p.Key, "coordTimes") {
			continue
		}
		extra = append(extra, p)
	}
	timesAsData := line != nil && line.Times != nil && !track
	if len(extra) > 0 || timesAsData {
		x.start("ExtendedData")
		used := map[string]bool{}
		for _, p := range extra {
			if used[p.Key] {
				continue
			}
			used[p.Key] = true
			x.start("Data", "name", cleanXML(p.Key))
			x.element("value", cleanXML(toText(p.Value)))
			x.end()
		}
		if timesAsData {
			b := []byte{'['}
			for i, t := range line.Times {
				if i > 0 {
					b = append(b, ',')
				}
				if t != nil {
					b = appendJSONString(b, formatTime(*t))
				} else {
					b = append(b, "null"...)
				}
			}
			b = append(b, ']')
			x.start("Data", "name", "coordTimes")
			x.element("value", string(b))
			x.end()
		}
		x.end()
	}
	if f.Geometry != nil {
		writeKmlGeometry(x, f.Geometry, sparse)
	}
	x.end()
}

func firstKey(f *Feature, names ...string) string {
	for _, n := range names {
		for _, p := range f.Properties {
			if strings.EqualFold(p.Key, n) && toText(p.Value) != "" {
				return p.Key
			}
		}
	}
	return ""
}

// isTrack: gx:Track needs a time and a height for every point (Google's
// reference reads gx:coord as longitude, latitude and altitude, and a missing
// height is not made up).
func isTrack(l *LineString) bool {
	if l.Times == nil {
		return false
	}
	for _, t := range l.Times {
		if t == nil {
			return false
		}
	}
	for _, p := range l.Points {
		if !p.HasZ() {
			return false
		}
	}
	return true
}

func writeKmlGeometry(x *xmlWriter, g Geometry, sparse bool) {
	switch v := g.(type) {
	case *Point:
		x.start("Point")
		writeKmlCoordinates(x, []Coord{v.Position}, sparse)
		x.end()
	case *LineString:
		if isTrack(v) {
			x.start("gx:Track", "xmlns:gx", gxNamespace)
			for _, t := range v.Times {
				x.element("when", formatTime(*t))
			}
			for _, p := range v.Points {
				x.element("gx:coord", degrees(p.X)+" "+degrees(p.Y)+" "+height(p.Z))
			}
		} else {
			x.start("LineString")
			writeKmlCoordinates(x, v.Points, sparse)
		}
		x.end()
	case *Polygon:
		x.start("Polygon")
		for i, r := range v.Rings {
			if i == 0 {
				x.start("outerBoundaryIs")
			} else {
				x.start("innerBoundaryIs")
			}
			x.start("LinearRing")
			writeKmlCoordinates(x, oriented(r, i == 0), sparse)
			x.end()
			x.end()
		}
		x.end()
	default:
		// Several parts of one feature: MultiGeometry holds the simple geometries.
		x.start("MultiGeometry")
		for _, p := range g.Parts() {
			writeKmlGeometry(x, p, sparse)
		}
		x.end()
	}
}

func writeKmlCoordinates(x *xmlWriter, points []Coord, sparse bool) {
	z := false
	for _, p := range points {
		z = z || p.HasZ()
	}
	parts := make([]string, len(points))
	for i, p := range points {
		if z && (!sparse || p.HasZ()) {
			h := 0.0
			if p.HasZ() {
				h = p.Z
			}
			parts[i] = degrees(p.X) + "," + degrees(p.Y) + "," + height(h)
		} else {
			parts[i] = degrees(p.X) + "," + degrees(p.Y)
		}
	}
	x.element("coordinates", strings.Join(parts, " "))
}
