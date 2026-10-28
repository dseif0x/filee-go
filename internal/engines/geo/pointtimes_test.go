package geo

// Point times and sparse heights (FIT activities) through every writer.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/testutil"
)

var (
	ptFirst = mustTime("2026-10-24T01:02:03Z")
	ptMid   = mustTime("2026-10-24T01:03:03Z")
	ptLast  = mustTime("2026-10-24T01:04:03Z")
)

func rideDataset(points []Coord, times []*timeT, sparse bool) *Dataset {
	ds := newDataset("ride")
	ds.SparseHeights = sparse
	ds.Features = []*Feature{{Geometry: &LineString{Points: points, Times: times}, Properties: []Prop{{"type", "running"}}}}
	return ds
}

func samePoints(t *testing.T, ds *Dataset, points []Coord) {
	t.Helper()
	line, ok := ds.Features[0].Geometry.(*LineString)
	if !ok || len(line.Points) != len(points) {
		t.Fatalf("%T", ds.Features[0].Geometry)
	}
	for i, p := range points {
		q := line.Points[i]
		if q.X != p.X || q.Y != p.Y || q.HasZ() != p.HasZ() || p.HasZ() && q.Z != p.Z {
			t.Fatalf("%d: %v != %v", i, q, p)
		}
	}
}

func TestMappingALineKeepsItsTimes(t *testing.T) {
	times := []*timeT{&ptFirst, nil, &ptLast}
	line := &LineString{Points: []Coord{{10, 20, 100}, xy(11, 21), {12, 22, 300}}, Times: times}
	mapped := line.Map(func(c Coord) Coord { c.X++; return c }).(*LineString)
	if &mapped.Times[0] != &times[0] || mapped.Points[0].X != 11 || mapped.Points[2].X != 13 {
		t.Fatal("map")
	}
	if newDataset("x").SparseHeights {
		t.Fatal("sparse by default")
	}
}

func TestWritersKeepTimesAndSparseHeightsInTheirOwnForms(t *testing.T) {
	all := []Coord{{10, 20, 100}, {11, 21, 200}, {12, 22, 300}}
	partial := []Coord{all[0], xy(11, 21), all[2]}
	for _, timeCase := range []string{"all", "partial", "none"} {
		for _, partialHeight := range []bool{false, true} {
			t.Run(fmt.Sprint(timeCase, partialHeight), func(t *testing.T) {
				var times []*timeT
				switch timeCase {
				case "all":
					times = []*timeT{&ptFirst, &ptMid, &ptLast}
				case "partial":
					times = []*timeT{&ptFirst, nil, &ptLast}
				}
				points := all
				if partialHeight {
					points = partial
				}
				ds := rideDataset(points, times, true)
				dir := t.TempDir()
				gpx, kml, kmz, geojson := filepath.Join(dir, "ride.gpx"), filepath.Join(dir, "ride.kml"), filepath.Join(dir, "ride.kmz"), filepath.Join(dir, "ride.geojson")
				ctx := context.Background()
				if lossy, err := writeGpx(ctx, ds, gpx); err != nil || lossy {
					t.Fatal(err, lossy)
				}
				if writeKml(ctx, ds, kml, false) != nil || writeKml(ctx, ds, kmz, true) != nil || writeGeoJSON(ctx, ds, geojson) != nil {
					t.Fatal("write")
				}
				trkpts := xmlRoot(t, gpx).DescendantsLocal("trkpt")
				for i, p := range trkpts {
					var names []string
					for _, e := range p.Elements() {
						names = append(names, e.Name.Local)
					}
					want := []string{}
					if points[i].HasZ() {
						want = append(want, "ele")
						if p.ChildLocal("ele").Value() != height(points[i].Z) {
							t.Fatal("ele")
						}
					}
					if times != nil && times[i] != nil {
						want = append(want, "time")
						if p.ChildLocal("time").Value() != formatTime(*times[i]) {
							t.Fatal("time")
						}
					}
					if strings.Join(names, ",") != strings.Join(want, ",") {
						t.Fatal(names, want)
					}
				}
				back, _ := readGpx(ctx, gpx)
				samePoints(t, back, points)

				kmlBytes, _ := os.ReadFile(kml)
				if testutil.ReadZip(t, kmz)["doc.kml"] != string(kmlBytes) {
					t.Fatal("kmz differs from kml")
				}
				root := xmlRoot(t, kml)
				isTrack := timeCase == "all" && !partialHeight
				if (len(root.DescendantsLocal("Track")) == 1) != isTrack {
					t.Fatal("track")
				}
				if isTrack {
					var coords []string
					for _, c := range root.DescendantsLocal("coord") {
						coords = append(coords, c.Value())
					}
					if strings.Join(coords, ";") != "10 20 100;11 21 200;12 22 300" || len(root.DescendantsLocal("when")) != 3 {
						t.Fatal(coords)
					}
				} else {
					want := "10,20,100 11,21,200 12,22,300"
					if partialHeight {
						want = "10,20,100 11,21 12,22,300"
					}
					if root.DescendantsLocal("coordinates")[0].Value() != want {
						t.Fatal(root.DescendantsLocal("coordinates")[0].Value())
					}
				}
				var coordTimes string
				for _, d := range root.DescendantsLocal("Data") {
					if d.A("name") == "coordTimes" {
						coordTimes = d.ChildLocal("value").Value()
					}
				}
				if (coordTimes != "") != (times != nil && !isTrack) {
					t.Fatal("coordTimes", coordTimes)
				}
				if coordTimes != "" {
					var want []any
					for _, tm := range times {
						if tm != nil {
							want = append(want, formatTime(*tm))
						} else {
							want = append(want, nil)
						}
					}
					b, _ := json.Marshal(want)
					if coordTimes != string(b) {
						t.Fatal(coordTimes)
					}
				}
				for _, f := range []string{kml, kmz} {
					back, err := readKml(ctx, f, f == kmz, maxKmlBytes)
					if err != nil {
						t.Fatal(err)
					}
					samePoints(t, back, points)
				}

				c := readCollection(t, geojson)
				keys := c.Features[0].propKeys(t)
				wantKeys := "type"
				if times != nil {
					wantKeys = "type,coordTimes"
				}
				if strings.Join(keys, ",") != wantKeys {
					t.Fatal(keys)
				}
				var positions [][]float64
				json.Unmarshal(c.Features[0].geometry(t).Coordinates, &positions)
				for i, p := range positions {
					if (len(p) == 3) != points[i].HasZ() {
						t.Fatal(p)
					}
				}
				back, _ = readGeoJSON(ctx, geojson)
				samePoints(t, back, points)
			})
		}
	}
}

func TestDefaultWritersFillMissingHeightsWithZeroAndKeepOldCoordTimes(t *testing.T) {
	ds := rideDataset([]Coord{{10, 20, 100}, xy(11, 21), {12, 22, 300}}, nil, false)
	ds.Features[0].Properties = append(ds.Features[0].Properties, Prop{"coordTimes", `["old"]`})
	dir := t.TempDir()
	ctx := context.Background()
	kml, js, shp := filepath.Join(dir, "old.kml"), filepath.Join(dir, "old.geojson"), filepath.Join(dir, "old.shp")
	writeKml(ctx, ds, kml, false)
	writeGeoJSON(ctx, ds, js)
	if _, err := writeShapefile(ctx, planShapefiles(ds)[0], shp, false); err != nil {
		t.Fatal(err)
	}
	k, _ := os.ReadFile(kml)
	j, _ := os.ReadFile(js)
	if !strings.Contains(string(k), "11,21,0") || !strings.Contains(string(j), "[11,21,0]") {
		t.Fatal(string(k), string(j))
	}
	if readCollection(t, js).Features[0].props(t)["coordTimes"] != `["old"]` {
		t.Fatal("coordTimes")
	}
	back, err := readShapefile(ctx, shp)
	if err != nil || back.Features[0].Geometry.(*LineString).Points[1].Z != 0 {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(shp)
	if data[32] != 13 {
		t.Fatal(data[32])
	}
}

func TestMissingHeightsStay2DAndGeneratedTimesReplaceTheOldAttribute(t *testing.T) {
	ds := rideDataset([]Coord{xy(10, 20), xy(11, 21)}, []*timeT{&ptFirst, nil}, true)
	ds.Features[0].Properties = append(ds.Features[0].Properties, Prop{"coordTimes", `["old"]`})
	dir := t.TempDir()
	ctx := context.Background()
	kml, js := filepath.Join(dir, "plain.kml"), filepath.Join(dir, "plain.geojson")
	writeKml(ctx, ds, kml, false)
	writeGeoJSON(ctx, ds, js)
	root := xmlRoot(t, kml)
	if root.DescendantsLocal("coordinates")[0].Value() != "10,20 11,21" {
		t.Fatal("coordinates")
	}
	for _, d := range root.DescendantsLocal("Data") {
		if d.A("name") == "coordTimes" && d.ChildLocal("value").Value() != `["2026-10-24T01:02:03Z",null]` {
			t.Fatal(d.Value())
		}
	}
	data, _ := os.ReadFile(js)
	if !strings.Contains(string(data), `"coordTimes":["2026-10-24T01:02:03Z",null]`) || strings.Contains(string(data), "old") {
		t.Fatal(string(data))
	}
	back, _ := readKml(ctx, kml, false, maxKmlBytes)
	samePoints(t, back, []Coord{xy(10, 20), xy(11, 21)})
	_ = math.NaN
}
