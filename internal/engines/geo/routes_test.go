package geo_test

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/all"
)

// A shapefile or a FIT activity reaches JSON and spreadsheets through CSV and the data or spreadsheet engines.
func TestGeoFilesReachTablesThroughCsv(t *testing.T) {
	p := all.Catalog().Planner("geo", "data", "spreadsheet", "model3d")
	for _, c := range [][2]string{{"shp", "json"}, {"shp", "yaml"}, {"fit", "json"}, {"kml", "xlsx"}} {
		r := p.Plan(c[0], c[1], convert.Options{})
		if r == nil || len(r.Steps) != 2 || r.Steps[0].Converter.ID() != "geo" {
			t.Fatalf("%s → %s: %+v", c[0], c[1], r)
		}
	}
	for _, c := range [][2]string{{"stl", "glb"}, {"fbx", "png"}, {"geojson", "kmz"}} {
		if r := p.Plan(c[0], c[1], convert.Options{}); r == nil || len(r.Steps) != 1 {
			t.Fatalf("%s → %s", c[0], c[1])
		}
	}
}
