package geo

// GeoJSON (RFC 7946) in and out: FeatureCollection, Feature or a bare
// geometry on the way in; one feature per line on the way out, polygons in
// the RFC's orientation (outer ring counter-clockwise). Attributes that are
// objects or lists are kept as their JSON text. Positions beyond longitude /
// latitude / height are ignored. A "crs" member (GeoJSON 2008) is honoured for
// the systems Crs knows; any other is refused rather than read as degrees.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/doc"
)

func readGeoJSON(ctx context.Context, path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ds := newDataset(baseName(path))
	root, err := parseJSON(doc.DecodeText(data))
	if err != nil {
		return nil, fmt.Errorf("The file is not valid JSON (%v)", err)
	}
	typ, ok := root.get("type")
	if root.kind != jsonObject || !ok || typ.kind != jsonString {
		return nil, errors.New(`The file is JSON but not GeoJSON: the top level has no "type".`)
	}
	if err := readGeoJSONCrs(root, ds); err != nil {
		return nil, err
	}
	switch typ.text {
	case "FeatureCollection":
		features, ok := root.get("features")
		if !ok || features.kind != jsonArray {
			return nil, errors.New(`The FeatureCollection has no "features" list.`)
		}
		for i, f := range features.items {
			if i&0x3FF == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			feature, err := readGeoJSONFeature(f, i)
			if err != nil {
				return nil, err
			}
			ds.Features = append(ds.Features, feature)
		}
	case "Feature":
		feature, err := readGeoJSONFeature(root, 0)
		if err != nil {
			return nil, err
		}
		ds.Features = append(ds.Features, feature)
	default:
		g, err := readGeoJSONGeometry(root, "geometry")
		if err != nil {
			return nil, err
		}
		ds.Features = append(ds.Features, &Feature{Geometry: g})
	}
	return ds, nil
}

func readGeoJSONCrs(root *jsonValue, ds *Dataset) error {
	crs, ok := root.get("crs")
	if !ok || crs.kind != jsonObject {
		return nil
	}
	props, ok := crs.get("properties")
	if !ok || props.kind != jsonObject {
		return nil
	}
	name, ok := props.get("name")
	if !ok || name.kind != jsonString {
		return nil
	}
	c := crsFromName(name.text)
	if c == nil {
		return unsupportedCrs(name.text)
	}
	ds.Crs, ds.CrsDeclared = c, true
	return nil
}

func readGeoJSONFeature(f *jsonValue, index int) (*Feature, error) {
	if f.kind != jsonObject {
		return nil, fmt.Errorf("Feature %d is not an object.", index+1)
	}
	feature := &Feature{}
	attributes, hasProperties := f.get("properties")
	hasProperties = hasProperties && attributes.kind == jsonObject
	if id, ok := f.get("id"); ok && (id.kind == jsonString || id.kind == jsonNumber) {
		if _, dup := attributes.get("id"); !(hasProperties && dup) {
			feature.Properties = append(feature.Properties, Prop{"id", jsonAttribute(id)})
		}
	}
	if hasProperties {
		for _, m := range attributes.members {
			feature.Properties = append(feature.Properties, Prop{m.name, jsonAttribute(m.value)})
		}
	}
	if g, ok := f.get("geometry"); ok && g.kind == jsonObject {
		geometry, err := readGeoJSONGeometry(g, fmt.Sprintf("feature %d", index+1))
		if err != nil {
			return nil, err
		}
		feature.Geometry = geometry
	}
	return feature, nil
}

func jsonAttribute(v *jsonValue) any {
	switch v.kind {
	case jsonString:
		return v.text
	case jsonNumber:
		if !strings.ContainsAny(v.text, ".eE") {
			if i, err := strconv.ParseInt(v.text, 10, 64); err == nil {
				return i
			}
		}
		if f, ok := v.float(); ok && isFinite(f) {
			return f
		}
		return v.raw
	case jsonBool:
		return v.boolean
	case jsonNull:
		return nil
	}
	return v.raw
}

func readGeoJSONGeometry(e *jsonValue, where string) (Geometry, error) {
	typ, ok := e.get("type")
	if e.kind != jsonObject || !ok || typ.kind != jsonString {
		return nil, fmt.Errorf(`The geometry of %s has no "type".`, where)
	}
	if typ.text == "GeometryCollection" {
		members, ok := e.get("geometries")
		if !ok || members.kind != jsonArray {
			return nil, fmt.Errorf(`The GeometryCollection of %s has no "geometries" list.`, where)
		}
		var parts []Geometry
		for _, m := range members.items {
			g, err := readGeoJSONGeometry(m, where)
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
		return &Collection{parts}, nil
	}
	coordinates, ok := e.get("coordinates")
	if !ok || coordinates.kind != jsonArray {
		return nil, fmt.Errorf(`The %s of %s has no "coordinates".`, typ.text, where)
	}
	switch typ.text {
	case "Point":
		if len(coordinates.items) == 0 {
			return nil, nil
		}
		c, err := geoJSONPosition(coordinates, where)
		if err != nil {
			return nil, err
		}
		return &Point{c}, nil
	case "MultiPoint":
		points, err := geoJSONLine(coordinates, where)
		if err != nil || len(points) == 0 {
			return nil, err
		}
		return &MultiPoint{points}, nil
	case "LineString":
		line, err := geoJSONLine(coordinates, where)
		if err != nil || len(line) < 2 {
			return nil, err
		}
		return &LineString{Points: line}, nil
	case "MultiLineString":
		var lines [][]Coord
		for _, l := range coordinates.items {
			line, err := geoJSONLine(l, where)
			if err != nil {
				return nil, err
			}
			if len(line) >= 2 {
				lines = append(lines, line)
			}
		}
		if len(lines) == 0 {
			return nil, nil
		}
		return &MultiLineString{lines}, nil
	case "Polygon":
		p, err := geoJSONPolygon(coordinates, where)
		if err != nil || p == nil {
			return nil, err
		}
		return p, nil
	case "MultiPolygon":
		var polygons []*Polygon
		for _, item := range coordinates.items {
			p, err := geoJSONPolygon(item, where)
			if err != nil {
				return nil, err
			}
			if p != nil {
				polygons = append(polygons, p)
			}
		}
		if len(polygons) == 0 {
			return nil, nil
		}
		return &MultiPolygon{polygons}, nil
	}
	return nil, fmt.Errorf("Unknown geometry type %q in %s.", typ.text, where)
}

func geoJSONPolygon(rings *jsonValue, where string) (*Polygon, error) {
	if rings.kind != jsonArray {
		return nil, fmt.Errorf("A polygon of %s is not a list of rings.", where)
	}
	var closed [][]Coord
	for _, r := range rings.items {
		line, err := geoJSONLine(r, where)
		if err != nil {
			return nil, err
		}
		if c := closeRing(line); c != nil {
			closed = append(closed, c)
		}
	}
	if len(closed) == 0 {
		return nil, nil
	}
	return &Polygon{closed}, nil
}

func geoJSONLine(positions *jsonValue, where string) ([]Coord, error) {
	if positions.kind != jsonArray {
		return nil, fmt.Errorf("A list of positions in %s is not a list.", where)
	}
	line := make([]Coord, 0, len(positions.items))
	for _, p := range positions.items {
		c, err := geoJSONPosition(p, where)
		if err != nil {
			return nil, err
		}
		line = append(line, c)
	}
	return line, nil
}

func geoJSONPosition(p *jsonValue, where string) (Coord, error) {
	if p.kind != jsonArray || len(p.items) < 2 {
		return Coord{}, fmt.Errorf("A position in %s needs at least longitude and latitude.", where)
	}
	c := xy(0, 0)
	for i, n := range p.items {
		f, ok := n.float()
		if !ok {
			return Coord{}, fmt.Errorf("A position in %s holds something that is not a number.", where)
		}
		switch i {
		case 0:
			c.X = f
		case 1:
			c.Y = f
		case 2:
			c.Z = f
		}
	}
	// 1e400 reads as infinity: as a longitude or latitude that is no position, as a height it is no height.
	if !isFinite(c.X) || !isFinite(c.Y) {
		return Coord{}, fmt.Errorf("A position in %s holds a number too large for a coordinate.", where)
	}
	if !isFinite(c.Z) {
		c.Z = math.NaN()
	}
	return c, nil
}

// ---- writing ----

// writeGeoJSON writes the dataset (in longitude / latitude) as a FeatureCollection, one feature per line.
func writeGeoJSON(ctx context.Context, ds *Dataset, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<16)
	w.WriteString("{\"type\":\"FeatureCollection\",\"features\":[\n")
	var b []byte
	for i, feature := range ds.Features {
		if i&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
		}
		b = geoJSONFeature(b[:0], feature, ds.SparseHeights)
		if i > 0 {
			w.WriteString(",\n")
		}
		w.Write(b)
	}
	w.WriteString("\n]}\n")
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func geoJSONFeature(b []byte, f *Feature, sparse bool) []byte {
	b = append(b, `{"type":"Feature","properties":{`...)
	written := map[string]bool{}
	var times []*time.Time
	if l, ok := f.Geometry.(*LineString); ok && l.Times != nil {
		times = l.Times
	}
	first := true
	for _, p := range f.Properties {
		if times != nil && strings.EqualFold(p.Key, "coordTimes") {
			continue
		}
		// A repeated name (two columns that only differ in case elsewhere) would make duplicate JSON keys.
		if written[p.Key] {
			continue
		}
		written[p.Key] = true
		if !first {
			b = append(b, ',')
		}
		first = false
		b = appendJSONString(b, p.Key)
		b = append(b, ':')
		b = geoJSONValue(b, p.Value)
	}
	if times != nil {
		if !first {
			b = append(b, ',')
		}
		b = append(b, `"coordTimes":[`...)
		for i, t := range times {
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
	}
	b = append(b, `},"geometry":`...)
	if f.Geometry != nil {
		b = geoJSONGeometry(b, f.Geometry, sparse)
	} else {
		b = append(b, "null"...)
	}
	return append(b, '}')
}

func geoJSONValue(b []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(b, "null"...)
	case string:
		return appendJSONString(b, x)
	case bool:
		return strconv.AppendBool(b, x)
	case int64:
		return strconv.AppendInt(b, x, 10)
	case float64:
		if isFinite(x) {
			return append(b, jsonFloat(x)...)
		}
		return append(b, "null"...)
	}
	return appendJSONString(b, toText(v))
}

func geoJSONGeometry(b []byte, g Geometry, sparse bool) []byte {
	b = append(b, `{"type":`...)
	b = appendJSONString(b, g.TypeName())
	if c, ok := g.(*Collection); ok {
		b = append(b, `,"geometries":[`...)
		for i, m := range c.Geometries {
			if i > 0 {
				b = append(b, ',')
			}
			b = geoJSONGeometry(b, m, sparse)
		}
		return append(b, "]}"...)
	}
	z := hasZ(g)
	b = append(b, `,"coordinates":`...)
	switch x := g.(type) {
	case *Point:
		b = geoJSONPositionOut(b, x.Position, z, sparse)
	case *MultiPoint:
		b = geoJSONLineOut(b, x.Points, z, sparse)
	case *LineString:
		b = geoJSONLineOut(b, x.Points, z, sparse)
	case *MultiLineString:
		b = append(b, '[')
		for i, l := range x.Lines {
			if i > 0 {
				b = append(b, ',')
			}
			b = geoJSONLineOut(b, l, z, sparse)
		}
		b = append(b, ']')
	case *Polygon:
		b = geoJSONPolygonOut(b, x, z, sparse)
	case *MultiPolygon:
		b = append(b, '[')
		for i, p := range x.Polygons {
			if i > 0 {
				b = append(b, ',')
			}
			b = geoJSONPolygonOut(b, p, z, sparse)
		}
		b = append(b, ']')
	}
	return append(b, '}')
}

func geoJSONPolygonOut(b []byte, p *Polygon, z, sparse bool) []byte {
	b = append(b, '[')
	for i, r := range p.Rings {
		if i > 0 {
			b = append(b, ',')
		}
		b = geoJSONLineOut(b, oriented(r, i == 0), z, sparse)
	}
	return append(b, ']')
}

func geoJSONLineOut(b []byte, points []Coord, z, sparse bool) []byte {
	b = append(b, '[')
	for i, c := range points {
		if i > 0 {
			b = append(b, ',')
		}
		b = geoJSONPositionOut(b, c, z, sparse)
	}
	return append(b, ']')
}

func geoJSONPositionOut(b []byte, c Coord, z, sparse bool) []byte {
	b = append(b, '[')
	b = append(b, degrees(c.X)...)
	b = append(b, ',')
	b = append(b, degrees(c.Y)...)
	if z && (!sparse || c.HasZ()) {
		h := 0.0
		if c.HasZ() {
			h = c.Z
		}
		b = append(b, ',')
		b = append(b, height(h)...)
	}
	return append(b, ']')
}
