package geo

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/testutil"
	"github.com/dseif0x/filee-go/internal/xdom"
	"golang.org/x/text/encoding/korean"
	xunicode "golang.org/x/text/encoding/unicode"
)

func TestEdgesMirrorTheOriginal(t *testing.T) {
	e := New().Edges()
	// 5×4 pairs, shp→shp, geojson→geojson, 5 to CSV, 5 from CSV, FIT to 5 and to CSV.
	if len(e) != 20+2+5+5+5+1 {
		t.Fatal(len(e))
	}
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, from := range append(slices.Clone(Formats), "csv", "fit") {
		for _, to := range append(slices.Clone(Formats), "csv") {
			if from == to && from != "shp" && from != "geojson" {
				continue
			}
			if r := p.Plan(from, to, convert.Options{}); r == nil || len(r.Steps) != 1 {
				t.Fatalf("%s → %s", from, to)
			}
		}
	}
	if p.Plan("geojson", "fit", convert.Options{}) != nil {
		t.Fatal("FIT is read only")
	}
}

func TestGdalShapefilePointsKeepTextNumbersBooleansAndDates(t *testing.T) {
	features := toGeoJSONFeatures(t, copyFixture(t, "gdal_points", "shp"))
	if len(features) != 3 {
		t.Fatal(len(features))
	}
	if p := features[0].position(t); !sameFloats(p, []float64{126.978, 37.5665}, 0) {
		t.Fatal(p)
	}
	p := features[0].props(t)
	if p["name"] != "서울 City Hall" || p["n"].(json.Number).String() != "1" || p["v"].(json.Number).String() != "1.5" || p["ok"] != true || p["day"] != "2024-02-29" {
		t.Fatal(p)
	}
	q := features[1].props(t)
	if q["name"] != "Café Eiffel – Paris" || q["n"].(json.Number).String() != "-20" || q["v"].(json.Number).String() != "0.001" || q["ok"] != false {
		t.Fatal(q)
	}
	r := features[2].props(t)
	if r["name"] != "北京 / London" || r["v"].(json.Number).String() != "-12345.6789" || r["day"] != nil {
		t.Fatal(r)
	}
}

func TestGdalShapefilePolygonsGetHolesAndPartsInGeoJSONWinding(t *testing.T) {
	features := toGeoJSONFeatures(t, copyFixture(t, "gdal_polygons", "shp"))
	g := features[0].geometry(t)
	var rings [][][]float64
	json.Unmarshal(g.Coordinates, &rings)
	if g.Type != "Polygon" || len(rings) != 2 || ringArea(rings[0]) <= 0 || ringArea(rings[1]) >= 0 {
		t.Fatal(g.Type, len(rings))
	}
	if a := ringArea(rings[0])/2 + ringArea(rings[1])/2; a != 96 {
		t.Fatal(a)
	}
	g2 := features[1].geometry(t)
	var polys [][][][]float64
	json.Unmarshal(g2.Coordinates, &polys)
	if g2.Type != "MultiPolygon" || len(polys) != 2 || features[1].props(t)["label"] != "two parts" {
		t.Fatal(g2.Type)
	}
}

func TestGdalUtmShapefileIsConvertedWithItsPrj(t *testing.T) {
	features := toGeoJSONFeatures(t, copyFixture(t, "gdal_utm33", "shp"))
	expected := []struct {
		name     string
		lon, lat float64
	}{{"Vienna", 16.3738, 48.2082}, {"Prague", 14.4378, 50.0755}, {"Berlin", 13.4050, 52.5200}}
	for i, e := range expected {
		p := features[i].position(t)
		if features[i].props(t)["name"] != e.name || !sameFloats(p, []float64{e.lon, e.lat}, 1e-8) {
			t.Fatal(i, p)
		}
	}
}

func TestGdal3dLinesKeepHeightsInEveryFormat(t *testing.T) {
	shp := copyFixture(t, "gdal_linesz", "shp")
	g := toGeoJSONFeatures(t, shp)[0].geometry(t)
	if g.Type != "LineString" || string(g.Coordinates) != "[[0,0,10],[1,1,20.5],[2,0,30]]" {
		t.Fatal(g.Type, string(g.Coordinates))
	}
	kml, _ := os.ReadFile(convertOK(t, shp, "kml")[0])
	if !strings.Contains(string(kml), "0,0,10 1,1,20.5 2,0,30") {
		t.Fatal(string(kml))
	}
	gpx, _ := os.ReadFile(convertOK(t, shp, "gpx")[0])
	root, err := xdom.ParseBytes(gpx)
	if err != nil {
		t.Fatal(err)
	}
	var ele []string
	for _, e := range root.DescendantsLocal("ele") {
		ele = append(ele, e.Value())
	}
	if strings.Join(ele, " ") != "10 20.5 30" {
		t.Fatal(ele)
	}
	csv, _ := os.ReadFile(convertOK(t, shp, "csv")[0])
	if !strings.Contains(string(csv), "LINESTRING Z (0 0 10, 1 1 20.5, 2 0 30)") {
		t.Fatal(string(csv))
	}
}

func TestKmlNameAndDescriptionBecomeAttributes(t *testing.T) {
	f := toGeoJSONFeatures(t, pointsFile(t, "kml"))
	if len(f) != 2 || !sameFloats(f[0].position(t), []float64{10.5, 20.25}, 0) || f[0].props(t)["name"] != "A" || f[1].props(t)["description"] != "second" {
		t.Fatal(f)
	}
}

func TestGpxWaypointElevationBecomesTheHeight(t *testing.T) {
	f := toGeoJSONFeatures(t, pointsFile(t, "gpx"))
	if len(f) != 2 || !sameFloats(f[0].position(t), []float64{10.5, 20.25, 12.5}, 0) || f[0].props(t)["gpx_type"] != "waypoint" || f[1].props(t)["name"] != "B" {
		t.Fatal(f)
	}
}

func TestGeoJSONIsRewrittenOneFeaturePerLine(t *testing.T) {
	out := convertOK(t, pointsFile(t, "geojson"), "geojson")[0]
	data, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[1], `{"type":"Feature",`) {
		t.Fatal(lines)
	}
	if len(readCollection(t, out).Features) != 2 {
		t.Fatal("features")
	}
}

func TestEveryFormatConvertsIntoEveryOtherAndBack(t *testing.T) {
	all := []string{"kml", "kmz", "gpx", "shp", "csv", "geojson"}
	source := pointsFile(t, "geojson")
	made := map[string]string{"geojson": source}
	for _, f := range all {
		if f != "geojson" {
			made[f] = first(convertOK(t, source, f), f)
		}
	}
	for _, from := range all {
		for _, to := range all {
			if from == to {
				continue
			}
			step := first(convertOK(t, made[from], to), to)
			final := step
			if to != "geojson" {
				final = first(convertOK(t, step, "geojson"), "geojson")
			}
			features := readCollection(t, final).Features
			if len(features) != 2 {
				t.Fatalf("%s → %s: %d features", from, to, len(features))
			}
			if p := features[0].position(t); !sameFloats(p[:2], []float64{10.5, 20.25}, 0) {
				t.Fatalf("%s → %s: %v", from, to, p)
			}
			if p := features[1].position(t); !sameFloats(p[:2], []float64{-5, 40}, 0) {
				t.Fatalf("%s → %s: %v", from, to, p)
			}
			if features[0].props(t)["name"] != "A" {
				t.Fatalf("%s → %s loses the name", from, to)
			}
		}
	}
}

func TestGeoJSONInputFormsAreAllAccepted(t *testing.T) {
	collection := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"a":1}}]}`
	feature := `{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"a":1}}`
	bare := `{"type":"Point","coordinates":[1,2]}`
	for _, text := range []string{collection, feature, bare, "\uFEFF" + feature, "// comment\n" + feature + "\n", strings.ReplaceAll(feature, "}}", "},}")} {
		f := toGeoJSONFeatures(t, writeText(t, "input.geojson", text))
		if len(f) != 1 || !sameFloats(f[0].position(t), []float64{1, 2}, 0) {
			t.Fatal(text)
		}
	}
}

func TestGeoJSONInUTF16AndAnOldCodePageIsRead(t *testing.T) {
	text := `{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"n":"유니코드 서울"}}`
	le, _ := xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM).NewEncoder().String(text)
	be, _ := xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM).NewEncoder().String(text)
	kr, _ := korean.EUCKR.NewEncoder().String(text)
	for _, encoded := range []string{le, be, kr} {
		f := toGeoJSONFeatures(t, writeText(t, "text.geojson", encoded))
		if f[0].props(t)["n"] != "유니코드 서울" {
			t.Fatal(f[0].props(t))
		}
	}
}

func TestGeoJSONAttributesKeepTypesAndNestedValuesBecomeJSONText(t *testing.T) {
	text := `{"type":"FeatureCollection","features":[{"type":"Feature","id":"abc-1","properties":{"s":"x","i":42,"big":12345678901234567890,"f":1.25,"b":true,"nil":null,"list":[1,2],"obj":{"k":"v"}},"geometry":{"type":"Point","coordinates":[3,4]}}]}`
	p := toGeoJSONFeatures(t, writeText(t, "types.geojson", text))[0].props(t)
	if p["id"] != "abc-1" || p["i"].(json.Number).String() != "42" || p["f"].(json.Number).String() != "1.25" || p["b"] != true || p["nil"] != nil ||
		p["list"] != "[1,2]" || p["obj"] != `{"k":"v"}` {
		t.Fatal(p)
	}
	if big, _ := p["big"].(json.Number).Float64(); big < 1.2e19 {
		t.Fatal(p["big"])
	}
}

func TestGeoJSONFeatureWithoutGeometryStaysAFeature(t *testing.T) {
	text := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"n":1},"geometry":null},{"type":"Feature","properties":{"n":2}},{"type":"Feature","properties":{"n":3},"geometry":{"type":"Point","coordinates":[]}},{"type":"Feature","properties":{"n":4},"geometry":{"type":"LineString","coordinates":[[1,1]]}}]}`
	path := writeText(t, "nogeom.geojson", text)
	ds, err := readGeoJSON(t.Context(), path)
	if err != nil || len(ds.Features) != 4 {
		t.Fatal(err)
	}
	for _, f := range ds.Features {
		if f.Geometry != nil {
			t.Fatal("geometry")
		}
	}
	// Written out, none of them has a geometry; with no position anywhere the coordinates check passes.
	out := convertOK(t, writeText(t, "mixed.geojson", strings.Replace(text, `"geometry":null`, `"geometry":{"type":"Point","coordinates":[5,6]}`, 1)), "geojson")[0]
	features := readCollection(t, out).Features
	if len(features) != 4 || features[1].geometry(t) != nil || features[3].geometry(t) != nil {
		t.Fatal("features")
	}
}

func TestGeoJSONPolygonRingsAreClosedAndWoundByTheRFC(t *testing.T) {
	text := `{"type":"Polygon","coordinates":[[[0,0],[0,4],[4,4],[4,0]],[[1,1],[2,1],[2,2],[1,2],[1,1]]]}`
	g := toGeoJSONFeatures(t, writeText(t, "ring.geojson", text))[0].geometry(t)
	var rings [][][]float64
	json.Unmarshal(g.Coordinates, &rings)
	if len(rings[0]) != 5 || ringArea(rings[0]) <= 0 || ringArea(rings[1]) >= 0 {
		t.Fatal(rings)
	}
}

func TestGeoJSONCrsMember(t *testing.T) {
	web := `{"type":"FeatureCollection","crs":{"type":"name","properties":{"name":"urn:ogc:def:crs:EPSG::3857"}},"features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[1113194.9079327357,1118889.9748579597]}}]}`
	if p := toGeoJSONFeatures(t, writeText(t, "web.geojson", web))[0].position(t); !sameFloats(p, []float64{10, 10}, 1e-8) {
		t.Fatal(p)
	}
	key, detail := convertFails(t, writeText(t, "laea.geojson", strings.Replace(web, "EPSG::3857", "EPSG::3035", 1)), "kml")
	if key != "error.geo_unsupported_crs" || !strings.Contains(detail, "EPSG::3035") {
		t.Fatal(key, detail)
	}
	if key, _ := convertFails(t, writeText(t, "utm.geojson", `{"type":"Point","coordinates":[602065.2,5340353.6]}`), "kml"); key != "error.geo_unknown_crs" {
		t.Fatal(key)
	}
}

func TestTextWithUnicodeAndControlCharactersIsWrittenSafely(t *testing.T) {
	text := "{\"type\":\"Feature\",\"properties\":{\"name\":\"a\\u0001b <&> \\\"q\\\" 한국어\\n2nd\"},\"geometry\":{\"type\":\"Point\",\"coordinates\":[1,2]}}"
	path := writeText(t, "ctl.geojson", text)
	want := "ab <&> \"q\" 한국어\n2nd"
	kml, _ := os.ReadFile(convertOK(t, path, "kml")[0])
	root, err := xdom.ParseBytes(kml)
	if err != nil {
		t.Fatal(err)
	}
	if got := root.DescendantsLocal("Placemark")[0].ChildLocal("name").Value(); got != want {
		t.Fatalf("%q", got)
	}
	gpx, _ := os.ReadFile(convertOK(t, path, "gpx")[0])
	root, err = xdom.ParseBytes(gpx)
	if err != nil {
		t.Fatal(err)
	}
	if got := root.DescendantsLocal("name")[0].Value(); got != want {
		t.Fatalf("%q", got)
	}
	csv, _ := os.ReadFile(convertOK(t, path, "csv")[0])
	if !strings.Contains(string(csv), "\"a\u0001b <&> \"\"q\"\" 한국어\n2nd\"") {
		t.Fatalf("%q", csv)
	}
}

const kmlSample = `<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2" xmlns:gx="http://www.google.com/kml/ext/2.2">
  <Document>
    <name>Sample</name>
    <Style id="s"><IconStyle><scale>2</scale></IconStyle></Style>
    <Folder>
      <name>Outer</name>
      <Folder>
        <Placemark>
          <name>Point with data</name>
          <description><![CDATA[<b>bold</b> text]]></description>
          <ExtendedData>
            <Data name="population"><value>1234</value></Data>
            <Data name="code"><value>007</value></Data>
            <SchemaData schemaUrl="#x"><SimpleData name="area">12.5</SimpleData></SchemaData>
          </ExtendedData>
          <Point><coordinates>10.5, 20.25, 99</coordinates></Point>
        </Placemark>
      </Folder>
      <Placemark><name>Line</name><LineString><coordinates>
        0,0
        1,1,5
        2,0
      </coordinates></LineString></Placemark>
      <Placemark><name>Polygon</name><Polygon>
        <outerBoundaryIs><LinearRing><coordinates>0,0 4,0 4,4 0,4 0,0</coordinates></LinearRing></outerBoundaryIs>
        <innerBoundaryIs><LinearRing><coordinates>1,1 1,2 2,2 2,1 1,1</coordinates></LinearRing></innerBoundaryIs>
      </Polygon></Placemark>
      <Placemark><name>Several points</name><MultiGeometry><Point><coordinates>1,1</coordinates></Point><Point><coordinates>2,2</coordinates></Point></MultiGeometry></Placemark>
      <Placemark><name>Mixed</name><MultiGeometry><Point><coordinates>1,1</coordinates></Point><LineString><coordinates>0,0 3,3</coordinates></LineString></MultiGeometry></Placemark>
      <Placemark><name>Track</name><gx:Track><gx:coord>5 6 7</gx:coord><gx:coord>6 7 8</gx:coord></gx:Track></Placemark>
      <Placemark><name>No shape</name></Placemark>
    </Folder>
    <GroundOverlay><name>Not a feature</name></GroundOverlay>
  </Document>
</kml>`

func TestKmlPlacemarksFromNestedFoldersWithAllGeometryKinds(t *testing.T) {
	f := toGeoJSONFeatures(t, writeText(t, "sample.kml", kmlSample))
	if len(f) != 7 {
		t.Fatal(len(f))
	}
	p := f[0].props(t)
	if p["name"] != "Point with data" || p["description"] != "<b>bold</b> text" || p["population"].(json.Number).String() != "1234" || p["code"] != "007" || p["area"].(json.Number).String() != "12.5" {
		t.Fatal(p)
	}
	if !sameFloats(f[0].position(t), []float64{10.5, 20.25, 99}, 0) {
		t.Fatal(f[0].position(t))
	}
	types := []string{}
	for _, x := range f[1:6] {
		types = append(types, x.geometry(t).Type)
	}
	if strings.Join(types, ",") != "LineString,Polygon,MultiPoint,GeometryCollection,LineString" || f[6].geometry(t) != nil {
		t.Fatal(types)
	}
	var track [][]float64
	json.Unmarshal(f[5].geometry(t).Coordinates, &track)
	if track[0][2] != 7 {
		t.Fatal(track)
	}
}

func TestKmlOlderNamespaceNoNamespaceAndOddCoordinates(t *testing.T) {
	for _, kml := range []string{
		`<kml xmlns="http://earth.google.com/kml/2.1"><Placemark><Point><coordinates>1,2</coordinates></Point></Placemark></kml>`,
		`<kml><Placemark><Point><coordinates>1,2</coordinates></Point></Placemark></kml>`,
		`<kml xmlns="http://www.opengis.net/kml/2.2"><Placemark><Point><coordinates>  1 , 2  </coordinates></Point></Placemark></kml>`,
	} {
		if p := toGeoJSONFeatures(t, writeText(t, "old.kml", kml))[0].position(t); !sameFloats(p, []float64{1, 2}, 0) {
			t.Fatal(p)
		}
	}
}

func TestKmzIsAZipWithDocKml(t *testing.T) {
	kmz := convertOK(t, pointsFile(t, "geojson"), "kmz")[0]
	entries := testutil.ReadZip(t, kmz)
	if len(entries) != 1 || entries["doc.kml"] == "" {
		t.Fatal(entries)
	}
	odd := filepath.Join(t.TempDir(), "odd.kmz")
	testutil.Zip(t, odd, "images/icon.png", "not really a picture", "files/places.kml", kmlSample)
	if f := toGeoJSONFeatures(t, odd); len(f) != 7 {
		t.Fatal(len(f))
	}
}

func TestKmlOutputHasOnePlacemarkPerFeatureWithExtendedData(t *testing.T) {
	kml, _ := os.ReadFile(convertOK(t, copyFixture(t, "gdal_points", "shp"), "kml")[0])
	root, err := xdom.ParseBytes(kml)
	if err != nil {
		t.Fatal(err)
	}
	placemarks := root.DescendantsLocal("Placemark")
	if len(placemarks) != 3 || placemarks[0].ChildLocal("name").Value() != "서울 City Hall" {
		t.Fatal(len(placemarks))
	}
	data := map[string]string{}
	for _, d := range placemarks[0].ChildLocal("ExtendedData").AllLocal("Data") {
		data[d.A("name")] = d.ChildLocal("value").Value()
	}
	if data["n"] != "1" || data["ok"] != "true" || data["name"] != "" {
		t.Fatal(data)
	}
	if c := placemarks[0].DescendantsLocal("coordinates")[0].Value(); c != "126.978,37.5665" {
		t.Fatal(c)
	}
}

func TestBrokenKmlKmzAndGpxFailWithAReason(t *testing.T) {
	for _, text := range []string{"not xml at all", "<kml><Placemark>", "<html><body/></html>"} {
		if key, detail := convertFails(t, writeText(t, "bad.kml", text), "geojson"); key != "error.conversion_failed" || detail == "" {
			t.Fatal(text, key)
		}
	}
	if _, detail := convertFails(t, writeText(t, "nan.kml", `<kml><Placemark><Point><coordinates>abc,2</coordinates></Point></Placemark></kml>`), "geojson"); !strings.Contains(detail, "abc,2") {
		t.Fatal(detail)
	}
	if _, detail := convertFails(t, writeText(t, "fake.kmz", "this is not a zip"), "geojson"); !strings.Contains(detail, "KMZ") {
		t.Fatal(detail)
	}
	empty := filepath.Join(t.TempDir(), "empty.kmz")
	testutil.Zip(t, empty, "readme.txt", "")
	if _, detail := convertFails(t, empty, "geojson"); !strings.Contains(detail, "no .kml") {
		t.Fatal(detail)
	}
	for _, text := range []string{`<gpx><wpt lat="91" lon="0"/></gpx>`, `<gpx><wpt lat="0" lon="500"/></gpx>`, `<gpx><wpt lon="0"/></gpx>`,
		`<gpx><wpt lat="north" lon="0"/></gpx>`, `<notgpx/>`, `<gpx><wpt lat="1" lon="2">`} {
		if key, detail := convertFails(t, writeText(t, "bad.gpx", text), "geojson"); key != "error.conversion_failed" || detail == "" {
			t.Fatal(text, key)
		}
	}
}

const gpxSample = `<?xml version="1.0"?>
<gpx version="1.1" creator="test" xmlns="http://www.topografix.com/GPX/1/1">
  <metadata><name>file</name></metadata>
  <wpt lat="47.5" lon="8.25"><ele>412.5</ele><time>2024-05-01T10:00:00Z</time><name>Summit</name><cmt>comment</cmt><desc>Nice view</desc><sym>Flag</sym><type>peak</type></wpt>
  <rte><name>Route 1</name><rtept lat="1" lon="2"/><rtept lat="3" lon="4"/></rte>
  <trk><name>Track 1</name><number>3</number>
    <trkseg><trkpt lat="10" lon="20"><ele>5</ele></trkpt><trkpt lat="11" lon="21"/></trkseg>
    <trkseg><trkpt lat="12" lon="22"/><trkpt lat="13" lon="23"/></trkseg>
  </trk>
  <trk><name>Empty track</name></trk>
</gpx>`

func TestGpxWaypointsRoutesAndTracksBecomeFeatures(t *testing.T) {
	f := toGeoJSONFeatures(t, writeText(t, "sample.gpx", gpxSample))
	if len(f) != 4 {
		t.Fatal(len(f))
	}
	w := f[0].props(t)
	if w["name"] != "Summit" || w["type"] != "peak" || w["sym"] != "Flag" || w["time"] != "2024-05-01T10:00:00Z" || !sameFloats(f[0].position(t), []float64{8.25, 47.5, 412.5}, 0) {
		t.Fatal(w)
	}
	if f[1].props(t)["gpx_type"] != "route" || f[1].geometry(t).Type != "LineString" || f[2].props(t)["number"].(json.Number).String() != "3" ||
		f[2].geometry(t).Type != "MultiLineString" || f[3].geometry(t) != nil {
		t.Fatal(f)
	}
}

func TestGpxOutputKeepsFieldsInSchemaOrder(t *testing.T) {
	geojson := first(convertOK(t, writeText(t, "sample.gpx", gpxSample), "geojson"), "geojson")
	data, _ := os.ReadFile(convertOK(t, geojson, "gpx")[0])
	root, err := xdom.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if root.A("version") != "1.1" {
		t.Fatal("version")
	}
	var names []string
	for _, e := range root.ChildLocal("wpt").Elements() {
		names = append(names, e.Name.Local)
	}
	if strings.Join(names, ",") != "ele,time,name,cmt,desc,sym,type" || root.ChildLocal("wpt").ChildLocal("time").Value() != "2024-05-01T10:00:00Z" {
		t.Fatal(names)
	}
	if len(root.AllLocal("rte")) != 1 || len(root.AllLocal("trk")) != 1 || len(root.ChildLocal("trk").AllLocal("trkseg")) != 2 {
		t.Fatal("route or track")
	}
	trk := root.ChildLocal("trk").Elements()
	if trk[0].Name.Local != "name" || trk[1].Name.Local != "number" {
		t.Fatal(trk[0].Name, trk[1].Name)
	}
}

func TestGpxOutputSaysWhenPolygonsCouldNotBeKept(t *testing.T) {
	r := convertTo(t, copyFixture(t, "gdal_polygons", "shp"), "gpx")
	if r.err != nil || r.notice != "notice.geo_gpx_lossy" {
		t.Fatal(r.err, r.notice)
	}
	data, _ := os.ReadFile(r.outputs[0])
	root, _ := xdom.ParseBytes(data)
	tracks := root.AllLocal("trk")
	if len(tracks) != 2 || len(tracks[0].AllLocal("trkseg")) != 2 || len(tracks[1].AllLocal("trkseg")) != 2 {
		t.Fatal(len(tracks))
	}
}

func TestCsvLongitudeAndLatitudeColumnsAreFoundByName(t *testing.T) {
	for _, csv := range []string{
		"name,lat,lon\nA,48.8566,2.3522\n",
		"Name,Latitude,Longitude\nA,48.8566,2.3522\n",
		"name,LAT,LNG\nA,48.8566,2.3522\n",
		"name,y,x\nA,48.8566,2.3522\n",
		"name;lat;lon\nA;48,8566;2,3522\n",
		"name\tlatitude\tlongitude\nA\t48.8566\t2.3522\n",
		"\uFEFFname,\"lat\",\"long\"\r\n\"A\",\"48.8566\",\"2.3522\"\r\n",
	} {
		r := convertTo(t, writeText(t, "points.csv", csv), "geojson")
		if r.err != nil || r.notice != "" {
			t.Fatal(csv, r.err, r.notice)
		}
		f := readCollection(t, r.outputs[0]).Features[0]
		keys := f.propKeys(t)
		if !sameFloats(f.position(t), []float64{2.3522, 48.8566}, 0) || len(keys) != 1 || !strings.EqualFold(keys[0], "name") || f.props(t)[keys[0]] != "A" {
			t.Fatal(csv, keys)
		}
	}
}

func TestCsvAttributeValuesAreTyped(t *testing.T) {
	f := toGeoJSONFeatures(t, writeText(t, "points.csv", "id,code,height,flag,note,lon,lat,elevation\n1,007,12.5,true,\"a,b\",1,2,300\n"))[0]
	p := f.props(t)
	if p["id"].(json.Number).String() != "1" || p["code"] != "007" || p["height"].(json.Number).String() != "12.5" || p["flag"] != true || p["note"] != "a,b" {
		t.Fatal(p)
	}
	if !sameFloats(f.position(t), []float64{1, 2, 300}, 0) {
		t.Fatal(f.position(t))
	}
}

func TestCsvWktColumnGivesAnyGeometry(t *testing.T) {
	csv := "id,WKT\n1,\"POINT (1 2)\"\n2,\"LINESTRING (0 0, 1 1)\"\n3,\"POLYGON ((0 0, 4 0, 4 4, 0 0))\"\n4,\"SRID=4326;MULTIPOINT ((1 1), (2 2))\"\n5,\n"
	r := convertTo(t, writeText(t, "points.csv", csv), "geojson")
	if r.err != nil || r.notice != "notice.geo_rows_without_location" {
		t.Fatal(r.err, r.notice)
	}
	var types []string
	for _, f := range readCollection(t, r.outputs[0]).Features {
		if g := f.geometry(t); g != nil {
			types = append(types, g.Type)
		} else {
			types = append(types, "-")
		}
	}
	if strings.Join(types, ",") != "Point,LineString,Polygon,MultiPoint,-" {
		t.Fatal(types)
	}
}

func TestCsvRowsWithoutALocationAreKept(t *testing.T) {
	r := convertTo(t, writeText(t, "points.csv", "n,lat,lon\n1,10,20\n2,,\n3,abc,30\n4,5,6\n"), "geojson")
	f := readCollection(t, r.outputs[0]).Features
	if len(f) != 4 || f[1].geometry(t) != nil || f[2].geometry(t) != nil || r.notice != "notice.geo_rows_without_location" {
		t.Fatal(len(f), r.notice)
	}
}

func TestCsvThatCannotGiveLocationsIsRefused(t *testing.T) {
	for _, c := range []struct{ csv, key string }{
		{"name,value\nA,1\n", "error.geo_csv_no_coordinates"},
		{"name,lat\nA,1\n", "error.geo_csv_no_coordinates"},
		{"lat,lon\n,\n,\n", "error.geo_empty"},
		{"lat,lon\nnorth,east\n", "error.geo_csv_no_locations"},
		{"lat,lon\n", "error.geo_empty"},
		{"easting,northing\n602065.2,5340353.6\n", "error.geo_unknown_crs"},
		{"x,y\n602065.2,5340353.6\n", "error.geo_unknown_crs"},
		{"lat,lon\n95,10\n", "error.geo_unknown_crs"},
	} {
		if key, _ := convertFails(t, writeText(t, "bad.csv", c.csv), "geojson"); key != c.key {
			t.Fatalf("%q: %s", c.csv, key)
		}
	}
	key, detail := convertFails(t, writeText(t, "badwkt.csv", "id,wkt\n1,POINT (1 2)\n2,POINT (oops)\n"), "geojson")
	if key != "error.conversion_failed" || !strings.Contains(detail, "Row 3") {
		t.Fatal(key, detail)
	}
}

func TestCsvOutput(t *testing.T) {
	read := func(path string) string {
		b, _ := os.ReadFile(path)
		return strings.TrimPrefix(string(b), "\uFEFF")
	}
	points := read(convertOK(t, pointsFile(t, "geojson"), "csv")[0])
	if !strings.HasPrefix(points, "longitude,latitude,name,n\r\n10.5,20.25,A,1\r\n-5,40,B,2\r\n") {
		t.Fatalf("%q", points)
	}
	polygons := read(convertOK(t, copyFixture(t, "gdal_polygons", "shp"), "csv")[0])
	if !strings.HasPrefix(polygons, "wkt,id,label\r\n") || !strings.Contains(polygons, `"POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0), (2 2, 2 4, 4 4, 4 2, 2 2))",1,with hole`) {
		t.Fatalf("%q", polygons)
	}
	// An attribute already called longitude: the geometry goes to WKT, so it reads back unchanged.
	text := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"LONGITUDE":111.0,"LATITUDE":222.0,"n":1},"geometry":{"type":"Point","coordinates":[1,2]}}]}`
	csv := convertOK(t, writeText(t, "attrs.geojson", text), "csv")[0]
	if !strings.HasPrefix(read(csv), "wkt,LONGITUDE,LATITUDE,n") {
		t.Fatal(read(csv))
	}
	f := toGeoJSONFeatures(t, csv)[0]
	if !sameFloats(f.position(t), []float64{1, 2}, 0) || f.props(t)["LONGITUDE"].(json.Number).String() != "111" {
		t.Fatal(f.props(t))
	}
	// Commas, quotes and line breaks are quoted and read back.
	quoted := convertOK(t, writeText(t, "quote.geojson", "{\"type\":\"Feature\",\"properties\":{\"t\":\"a,b \\\"c\\\"\\nd\"},\"geometry\":{\"type\":\"Point\",\"coordinates\":[1,2]}}"), "csv")[0]
	if got := toGeoJSONFeatures(t, quoted)[0].props(t)["t"]; got != "a,b \"c\"\nd" {
		t.Fatalf("%q", got)
	}
}

func TestFilesWithNoFeaturesAreRefused(t *testing.T) {
	for _, c := range []struct{ text, ext string }{
		{`{"type":"FeatureCollection","features":[]}`, "geojson"}, {`<kml><Document/></kml>`, "kml"}, {`<gpx/>`, "gpx"},
	} {
		to := "geojson"
		if c.ext == "geojson" {
			to = "kml"
		}
		if key, _ := convertFails(t, writeText(t, "empty."+c.ext, c.text), to); key != "error.geo_empty" {
			t.Fatal(c.ext, key)
		}
	}
}

func TestGeoJSONThatIsNotGeoJSONFails(t *testing.T) {
	for _, text := range []string{"   ", "[1,2,3]", `{"a":1}`, `{"type":"FeatureCollection"}`, `{"type":"Banana","coordinates":[1,2]}`,
		`{"type":"Point"}`, `{"type":"Point","coordinates":"x"}`, `{"type":"Point","coordinates":[1]}`, `{"type":"Point","coordinates":["a","b"]}`,
		`{"type":"Polygon","coordinates":[1]}`, `{"type":"FeatureCollection","features":[1]}`, `{"type":"Point","coordinates":[1,2`,
		strings.Repeat("[", 100_000) + strings.Repeat("]", 100_000)} {
		if key, detail := convertFails(t, writeText(t, "bad.geojson", text), "kml"); key != "error.conversion_failed" || detail == "" {
			t.Fatal(text[:min(len(text), 40)], key)
		}
	}
}

func TestGeoJSONNumbersBeyondTheDoubleRange(t *testing.T) {
	// As a height it is no height; as an attribute it stays text; as a position it is refused.
	f := toGeoJSONFeatures(t, writeText(t, "big.geojson", `{"type":"Feature","properties":{"v":1e400},"geometry":{"type":"Point","coordinates":[1,2,1e400]}}`))[0]
	if p := f.position(t); len(p) != 2 || f.props(t)["v"] != "1e400" {
		t.Fatal(p, f.props(t))
	}
	if key, _ := convertFails(t, writeText(t, "bad.geojson", `{"type":"Point","coordinates":[1e400,2]}`), "kml"); key != "error.conversion_failed" {
		t.Fatal(key)
	}
}

func TestXmlThatAsksForAFileOrAnEntityBombIsRefused(t *testing.T) {
	secret := writeText(t, "secret.txt", "TOP SECRET")
	xxe := `<?xml version="1.0"?><!DOCTYPE kml [<!ENTITY s SYSTEM "file://` + filepath.ToSlash(secret) + `">]><kml><Placemark><name>&s;</name><Point><coordinates>1,2</coordinates></Point></Placemark></kml>`
	r := convertTo(t, writeText(t, "xxe.kml", xxe), "geojson")
	if r.err == nil {
		t.Fatal("DOCTYPE accepted")
	}
	bomb := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;">]><gpx><wpt lat="1" lon="2"><name>&lol2;</name></wpt></gpx>`
	if r := convertTo(t, writeText(t, "bomb.gpx", bomb), "geojson"); r.err == nil {
		t.Fatal("entity bomb accepted")
	}
}

func TestDeepAndHugeXmlIsRefused(t *testing.T) {
	deep := "<kml>" + strings.Repeat("<Folder>", 300) + strings.Repeat("</Folder>", 300) + "</kml>"
	if _, detail := convertFails(t, writeText(t, "deep.kml", deep), "geojson"); !strings.Contains(detail, "levels deep") {
		t.Fatal(detail)
	}
	if _, err := loadXML(strings.NewReader("<kml>"+strings.Repeat("<a/>", 200)+"</kml>"), 100); err == nil || !strings.Contains(err.Error(), "more than 100 elements") {
		t.Fatal(err)
	}
	geometry := "<kml><Placemark>" + strings.Repeat("<MultiGeometry>", 40) + "<Point><coordinates>1,2</coordinates></Point>" + strings.Repeat("</MultiGeometry>", 40) + "</Placemark></kml>"
	if _, detail := convertFails(t, writeText(t, "nested.kml", geometry), "geojson"); !strings.Contains(detail, "32 levels") {
		t.Fatal(detail)
	}
	// A long run of white space in coordinates is read in linear time.
	long := "<kml><Placemark><LineString><coordinates>1,2" + strings.Repeat(" ", 2_000_000) + ",3 4,5</coordinates></LineString></Placemark></kml>"
	convertOK(t, writeText(t, "spaces.kml", long), "geojson")
}

func TestAKmzWhoseKmlIsTooLargeIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.kmz")
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("doc.kml")
	w.Write([]byte("<kml>" + strings.Repeat(" ", 4096) + "</kml>"))
	zw.Close()
	f.Close()
	if _, err := readKml(t.Context(), path, true, 1024); err == nil || !strings.Contains(err.Error(), "KMZ") {
		t.Fatal(err)
	}
}
