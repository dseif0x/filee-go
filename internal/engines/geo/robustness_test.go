package geo

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Damaged files of every format are read or refused: never a panic.
func TestDamagedFilesAreReadOrRefusedNeverACrash(t *testing.T) {
	ctx := context.Background()
	for _, set := range []string{"gdal_points", "gdal_polygons", "gdal_linesz", "gdal_utm33"} {
		for _, part := range []string{"shp", "dbf", "prj"} {
			shp := copyFixture(t, set, "shp")
			target := changeExt(shp, part)
			whole, _ := os.ReadFile(target)
			for n := 0; n < len(whole); n += max(1, len(whole)/97) {
				os.WriteFile(target, whole[:n], 0o644)
				readShapefile(ctx, shp)
			}
			for i := 0; i < len(whole); i++ {
				c := append([]byte(nil), whole...)
				c[i] ^= 0xFF
				os.WriteFile(target, c, 0o644)
				readShapefile(ctx, shp)
			}
		}
	}
	texts := map[string]string{"geojson": pointsGeoJSON, "kml": kmlSample, "gpx": gpxSample,
		"csv": "id,WKT,lat\n1,\"POLYGON ((0 0, 4 0, 4 4, 0 0))\",3\n2,\"MULTIPOINT ((1 1), (2 2))\",4\n"}
	dir := t.TempDir()
	for format, text := range texts {
		path := filepath.Join(dir, "x."+format)
		for n := 0; n < len(text); n++ {
			os.WriteFile(path, []byte(text[:n]), 0o644)
			Read(ctx, path, format)
		}
	}
	r := rand.New(rand.NewSource(1))
	for _, format := range []string{"geojson", "kml", "kmz", "gpx", "shp", "csv"} {
		for i := 0; i < 200; i++ {
			b := make([]byte, r.Intn(300))
			r.Read(b)
			path := filepath.Join(dir, "r."+format)
			os.WriteFile(path, b, 0o644)
			if ds, err := Read(ctx, path, format); err == nil && len(ds.Features) > 0 && format != "csv" {
				t.Fatalf("random bytes read as %s", format)
			}
		}
	}
}
