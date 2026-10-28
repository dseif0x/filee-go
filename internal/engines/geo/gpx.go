package geo

// GPX 1.0 / 1.1 in and out. Reading turns every waypoint into a Point, every
// route into a LineString and every track into a LineString (one segment) or
// MultiLineString (several), with name, comment, description, source, symbol,
// type, time and the point's elevation (as the height). "gpx_type" says which
// of the three a feature was. Per-point times of tracks are not kept. Writing
// puts Points into waypoints, lines into tracks (or routes when gpx_type says
// "route"), and the outlines of polygons into tracks, since GPX has no areas;
// only the fields above survive. Writing keeps point times when a line has
// them. GPX is WGS 84.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const gpxNamespace = "http://www.topografix.com/GPX/1/1"

// gpxFields are the attributes GPX can hold, in the order of its schema; "elevation" and "time" are written per point.
var gpxFields = []string{"name", "cmt", "desc", "src", "sym", "number", "type"}

func readGpx(ctx context.Context, path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	root, err := loadXML(f, xmlMaxElements)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(root.local, "gpx") {
		return nil, errors.New("The file is XML but not GPX: the root element is not <gpx>.")
	}
	ds := newDataset(baseName(path))
	for i, e := range root.elements() {
		if i&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		switch e.local {
		case "wpt":
			p, err := gpxPosition(e)
			if err != nil {
				return nil, err
			}
			ds.Features = append(ds.Features, &Feature{Geometry: &Point{p}, Properties: gpxProperties(e, "waypoint")})
		case "rte":
			var line []Coord
			for _, pt := range e.named("rtept") {
				p, err := gpxPosition(pt)
				if err != nil {
					return nil, err
				}
				line = append(line, p)
			}
			feature := &Feature{Properties: gpxProperties(e, "route")}
			if len(line) >= 2 {
				feature.Geometry = &LineString{Points: line}
			}
			ds.Features = append(ds.Features, feature)
		case "trk":
			var segments [][]Coord
			for _, s := range e.named("trkseg") {
				var line []Coord
				for _, pt := range s.named("trkpt") {
					p, err := gpxPosition(pt)
					if err != nil {
						return nil, err
					}
					line = append(line, p)
				}
				if len(line) >= 2 {
					segments = append(segments, line)
				}
			}
			feature := &Feature{Properties: gpxProperties(e, "track")}
			switch len(segments) {
			case 0:
			case 1:
				feature.Geometry = &LineString{Points: segments[0]}
			default:
				feature.Geometry = &MultiLineString{segments}
			}
			ds.Features = append(ds.Features, feature)
		}
	}
	return ds, nil
}

func gpxProperties(e *xnode, kind string) []Prop {
	props := []Prop{{"gpx_type", kind}}
	for _, name := range []string{"name", "cmt", "desc", "src", "sym", "number", "type", "time"} {
		v, _ := childText(e, name)
		if v == "" {
			continue
		}
		if name == "number" {
			props = append(props, Prop{name, typed(v)})
		} else {
			props = append(props, Prop{name, v})
		}
	}
	return props
}

func gpxPosition(p *xnode) (Coord, error) {
	lonText, _ := p.attr("lon")
	latText, _ := p.attr("lat")
	lon, ok1 := xmlNumber(lonText)
	lat, ok2 := xmlNumber(latText)
	if !ok1 || !ok2 {
		return Coord{}, fmt.Errorf("A GPX <%s> has no valid lat and lon.", p.local)
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 360 {
		return Coord{}, fmt.Errorf("A GPX <%s> is outside the Earth (lat %s, lon %s).", p.local, number(lat), number(lon))
	}
	z := math.NaN()
	if ele := p.first("ele"); ele != nil {
		if v, ok := xmlNumber(ele.value()); ok {
			z = v
		}
	}
	return Coord{lon, lat, z}, nil
}

// writeGpx writes a GPX 1.1 file; it reports whether some attributes or polygon areas could not be kept.
func writeGpx(ctx context.Context, ds *Dataset, path string) (bool, error) {
	f, err := os.Create(path)
	if err != nil {
		return false, err
	}
	lossy := false
	x := newXMLWriter(f)
	x.start("gpx", "version", "1.1", "creator", "Filee", "xmlns", gpxNamespace)
	kept := map[string]bool{"gpx_type": true, "time": true}
	for _, field := range gpxFields {
		kept[field] = true
	}
	for i, feature := range ds.Features {
		if i&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				f.Close()
				return false, err
			}
		}
		if feature.Geometry == nil {
			continue
		}
		for _, p := range feature.Properties {
			if !kept[strings.ToLower(p.Key)] && toText(p.Value) != "" {
				lossy = true
			}
		}
		asRoute := strings.EqualFold(feature.FindText("gpx_type"), "route")
		// Points become waypoints; every line, and the outline of every polygon,
		// is a segment of the feature's track (or a route of its own), so one
		// feature stays one track.
		var lines []*LineString
		outlines := false
		for _, part := range feature.Geometry.Parts() {
			switch v := part.(type) {
			case *Point:
				gpxWritePoint(x, "wpt", v.Position, feature, true, nil)
			case *LineString:
				lines = append(lines, v)
			case *Polygon:
				lossy, outlines = true, true
				for _, r := range v.Rings {
					lines = append(lines, &LineString{Points: r})
				}
			}
		}
		if len(lines) > 0 {
			gpxWriteLines(x, lines, feature, asRoute && !outlines)
		}
	}
	x.end()
	err = x.flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return lossy, err
}

func gpxWriteLines(x *xmlWriter, lines []*LineString, f *Feature, route bool) {
	if route {
		// A route is one line; extra parts become routes of their own.
		for _, l := range lines {
			x.start("rte")
			gpxWriteFields(x, f, true)
			for _, p := range l.Points {
				gpxWritePoint(x, "rtept", p, f, false, nil)
			}
			x.end()
		}
		return
	}
	x.start("trk")
	gpxWriteFields(x, f, true)
	for _, l := range lines {
		x.start("trkseg")
		for i, p := range l.Points {
			var t *time.Time
			if l.Times != nil {
				t = l.Times[i]
			}
			gpxWritePoint(x, "trkpt", p, f, false, t)
		}
		x.end()
	}
	x.end()
}

func gpxWritePoint(x *xmlWriter, element string, p Coord, f *Feature, withFields bool, pointTime *time.Time) {
	x.start(element, "lat", degrees(p.Y), "lon", degrees(p.X))
	if p.HasZ() {
		x.element("ele", height(p.Z))
	}
	if pointTime != nil {
		x.element("time", formatTime(*pointTime))
	}
	if withFields {
		if t, ok := parseAnyTime(f.FindText("time")); ok {
			x.element("time", formatTime(t))
		}
		gpxWriteFields(x, f, false)
	}
	x.end()
}

// gpxWriteFields writes the GPX fields in schema order; "number" belongs to routes and tracks only.
func gpxWriteFields(x *xmlWriter, f *Feature, trackFields bool) {
	for _, field := range gpxFields {
		if field == "number" && !trackFields || field == "sym" && trackFields {
			continue
		}
		text := f.FindText(field)
		if text == "" {
			continue
		}
		if field == "number" {
			if _, err := strconv.ParseUint(text, 10, 32); err != nil || strings.HasPrefix(text, "+") {
				continue
			}
		}
		x.element(field, cleanXML(text))
	}
}

var timeLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02 15:04", "2006-01-02", "2006/01/02 15:04:05", "2006/01/02",
	"2006-01-02T15:04:05.999999999Z0700", "2006-01-02T15:04:05.999999999-07", time.RFC1123, time.RFC1123Z,
}

// parseAnyTime reads a time attribute in any common ISO form (UTC when it names no zone).
func parseAnyTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
