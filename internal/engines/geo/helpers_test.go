package geo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Two points, A (10.5, 20.25) and B (-5, 40), written the way GDAL writes them.
const pointsGeoJSON = `{
"type": "FeatureCollection",
"name": "points",
"crs": { "type": "name", "properties": { "name": "urn:ogc:def:crs:OGC:1.3:CRS84" } },
"features": [
{ "type": "Feature", "properties": { "name": "A", "n": 1 }, "geometry": { "type": "Point", "coordinates": [ 10.5, 20.25 ] } },
{ "type": "Feature", "properties": { "name": "B", "n": 2 }, "geometry": { "type": "Point", "coordinates": [ -5.0, 40.0 ] } }
]
}`

const pointsKml = `<?xml version="1.0" encoding="utf-8" ?>
<kml xmlns="http://www.opengis.net/kml/2.2">
<Document id="root_doc">
<Folder><name>points</name>
  <Placemark>
	<name>A</name>
	<description>first</description>
      <Point><coordinates>10.5,20.25</coordinates></Point>
  </Placemark>
  <Placemark>
	<name>B</name>
	<description>second</description>
      <Point><coordinates>-5,40</coordinates></Point>
  </Placemark>
</Folder>
</Document></kml>`

const pointsGpx = `<?xml version="1.0"?>
<gpx version="1.1" creator="GDAL 3.9.1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns="http://www.topografix.com/GPX/1/1" xsi:schemaLocation="http://www.topografix.com/GPX/1/1 http://www.topografix.com/GPX/1/1/gpx.xsd">
<metadata>
<bounds minlat="20.250000000000000" minlon="-5.000000000000000" maxlat="40.000000000000000" maxlon="10.500000000000000"/>
</metadata>
<wpt lat="20.25" lon="10.5">
  <ele>12.5</ele>
  <name>A</name>
</wpt>
<wpt lat="40.0" lon="-5.0">
  <ele>0.0</ele>
  <name>B</name>
</wpt>
</gpx>`

func writeText(t testing.TB, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func pointsFile(t testing.TB, ext string) string {
	switch ext {
	case "geojson":
		return writeText(t, "points.geojson", pointsGeoJSON)
	case "kml":
		return writeText(t, "points.kml", pointsKml)
	}
	return writeText(t, "points.gpx", pointsGpx)
}

// copyFixture copies every file of the reference set name into a fresh folder; returns the path of its ext file.
func copyFixture(t testing.TB, name, ext string) string {
	t.Helper()
	folder := t.TempDir()
	matches, _ := filepath.Glob(filepath.Join("testdata", name+".*"))
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(folder, filepath.Base(m)), data, 0o644)
	}
	return filepath.Join(folder, name+"."+ext)
}

type result struct {
	outputs []string
	notice  string
	err     error
}

func formatOf(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return ext
}

func convertTo(t testing.TB, input, to string) result {
	t.Helper()
	dir := t.TempDir()
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	var r result
	step := &convert.Step{InputPath: input, From: formatOf(input), To: to, Preset: presets.New("test", to), WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, base+suffix+"."+ext) }),
		Notice: func(k string) { r.notice = k }}
	r.outputs, r.err = New().Convert(context.Background(), step, nil)
	return r
}

func convertOK(t testing.TB, input, to string) []string {
	t.Helper()
	r := convertTo(t, input, to)
	if r.err != nil {
		t.Fatalf("%s → %s: %v", filepath.Base(input), to, r.err)
	}
	return r.outputs
}

func first(outputs []string, ext string) string {
	for _, o := range outputs {
		if strings.HasSuffix(o, "."+ext) {
			return o
		}
	}
	return ""
}

// convertFails expects a failure and returns its key ("error.conversion_failed" for a plain error) and detail.
func convertFails(t testing.TB, input, to string) (string, string) {
	t.Helper()
	r := convertTo(t, input, to)
	if r.err == nil {
		t.Fatalf("%s → %s did not fail", filepath.Base(input), to)
	}
	var refused *convert.RefusedError
	if errors.As(r.err, &refused) {
		return refused.Key, refused.Detail
	}
	return "error.conversion_failed", r.err.Error()
}

type gjFeature struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
	Geometry   json.RawMessage `json:"geometry"`
}

type gjCollection struct {
	Type     string      `json:"type"`
	Features []gjFeature `json:"features"`
}

func readCollection(t testing.TB, path string) gjCollection {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c gjCollection
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return c
}

func toGeoJSONFeatures(t testing.TB, input string) []gjFeature {
	t.Helper()
	return readCollection(t, first(convertOK(t, input, "geojson"), "geojson")).Features
}

func (f gjFeature) props(t testing.TB) map[string]any {
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(string(f.Properties)))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f gjFeature) propKeys(t testing.TB) []string {
	d := json.NewDecoder(strings.NewReader(string(f.Properties)))
	d.Token()
	var keys []string
	for d.More() {
		k, _ := d.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		d.Decode(&skip)
	}
	return keys
}

type gjGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func (f gjFeature) geometry(t testing.TB) *gjGeometry {
	if string(f.Geometry) == "null" || len(f.Geometry) == 0 {
		return nil
	}
	var g gjGeometry
	if err := json.Unmarshal(f.Geometry, &g); err != nil {
		t.Fatal(err)
	}
	return &g
}

func (f gjFeature) position(t testing.TB) []float64 {
	t.Helper()
	g := f.geometry(t)
	if g == nil {
		t.Fatal("no geometry")
	}
	var p []float64
	if err := json.Unmarshal(g.Coordinates, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func ringArea(ring [][]float64) float64 {
	var s float64
	for i := 0; i+1 < len(ring); i++ {
		s += ring[i][0]*ring[i+1][1] - ring[i+1][0]*ring[i][1]
	}
	return s
}

func sameFloats(a, b []float64, tol float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		d := a[i] - b[i]
		if d < -tol || d > tol {
			return false
		}
	}
	return true
}

type timeT = time.Time

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
