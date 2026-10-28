// Package geo converts geospatial vector data in Go: GeoJSON, KML, KMZ, GPX,
// Shapefile and CSV (a WKT column, or longitude and latitude columns) into
// each other through one in-memory model (Dataset); Garmin FIT activities are
// read into it too. FIT goes to CSV straight from its records. Whatever the
// source's coordinate system (a .prj or a GeoJSON crs), every result is
// longitude / latitude in WGS 84.
package geo

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/formats"
)

// Formats are the geospatial formats, each readable and writable.
var Formats = []string{"geojson", "kml", "kmz", "gpx", "shp"}

// ReadOnlyFormats are the geospatial formats that are only read.
var ReadOnlyFormats = []string{"fit"}

// Converter is the geospatial engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string   { return "geo" }
func (*Converter) Name() string { return "Geospatial vector data (built-in)" }

// MaxParallelism: whole files are read into memory, so only a few run at once.
func (*Converter) MaxParallelism() int { return max(1, min(runtime.NumCPU()/2, 4)) }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range Formats {
		for _, to := range Formats {
			if from != to {
				e = append(e, convert.E(from, to))
			}
		}
	}
	// The same format again rewrites the file in WGS 84: a projected Shapefile
	// or a GeoJSON with an old "crs" member becomes longitude / latitude, and a
	// Shapefile with mixed geometries is split by kind.
	e = append(e, convert.E("shp", "shp"), convert.E("geojson", "geojson"))
	for _, f := range Formats {
		e = append(e, convert.E(f, "csv"))
	}
	for _, f := range Formats {
		e = append(e, convert.E("csv", f))
	}
	for _, from := range ReadOnlyFormats {
		for _, to := range Formats {
			e = append(e, convert.E(from, to))
		}
	}
	for _, from := range ReadOnlyFormats {
		e = append(e, convert.E(from, "csv"))
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("GeoJSON, KML, KMZ, GPX, Shapefile ↔ each other and CSV; Garmin FIT activities → geospatial formats and CSV", "built-in")
}

func baseName(path string) string { return formats.NameWithoutExtension(path) }

// notices collects the notes of a conversion. A file carries one note, so the
// most important one is reported: the assumed datum can move data by hundreds
// of metres, the rest only say what a format could not hold.
type notices map[string]bool

var noticePriority = []string{
	"notice.geo_datum_assumed", "notice.geo_gpx_lossy", "notice.geo_heights_dropped", "notice.geo_text_cut", "notice.geo_columns_dropped",
	"notice.geo_rows_without_location", "notice.geo_field_names", "notice.geo_shapefile_split",
}

func (n notices) report(sink func(string)) {
	if sink == nil {
		return
	}
	for _, k := range noticePriority {
		if n[k] {
			sink(k)
			return
		}
	}
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	notes := notices{}
	var activity *fitActivity
	var ds *Dataset
	var err error
	if step.From == "fit" {
		if activity, err = readFit(ctx, step.InputPath, fitMaxFileBytes, fitMaxRecords); err != nil {
			return nil, err
		}
		if step.To != "csv" {
			if ds, err = fitToDataset(activity, step.InputPath); err != nil {
				return nil, err
			}
		}
	} else if ds, err = Read(ctx, step.InputPath, step.From); err != nil {
		return nil, err
	}
	progress.Report(0.4)
	if ds != nil {
		if err := toWgs84(ds, notes); err != nil {
			return nil, err
		}
		if len(ds.Features) == 0 {
			return nil, convert.Refuse("error.geo_empty", "")
		}
		if ds.RowsWithoutLocation {
			notes["notice.geo_rows_without_location"] = true
		}
		// A GPX track has no start time, but its points keep theirs: the start is lost only when none has one.
		if activity != nil && step.To == "gpx" {
			if l, ok := ds.Features[0].Geometry.(*LineString); ok && l.Times == nil && ds.Features[0].FindText("time") != "" {
				notes["notice.geo_gpx_lossy"] = true
			}
		}
	}
	progress.Report(0.6)
	var outputs []string
	if ds != nil {
		err = write(ctx, ds, step, &outputs, notes)
	} else if activity != nil {
		if csv := step.Output.Allocate("csv", ""); csv != "" {
			outputs = append(outputs, csv)
			err = writeFitCsv(activity, csv)
		}
	}
	if err != nil {
		for _, o := range outputs {
			os.Remove(o)
		}
		return nil, err
	}
	notes.report(step.Notice)
	progress.Report(1)
	return outputs, nil
}

// Read reads a geospatial file of format (not FIT) into a dataset in its own coordinate system.
func Read(ctx context.Context, path, format string) (*Dataset, error) {
	switch format {
	case "geojson":
		return readGeoJSON(ctx, path)
	case "kml":
		return readKml(ctx, path, false, maxKmlBytes)
	case "kmz":
		return readKml(ctx, path, true, maxKmlBytes)
	case "gpx":
		return readGpx(ctx, path)
	case "shp":
		return readShapefile(ctx, path)
	case "csv":
		return readGeoCSV(ctx, path)
	}
	return nil, fmt.Errorf("%s is not a geospatial format.", format)
}

// toWgs84 puts every position into longitude / latitude, and refuses data
// whose positions can't be: coordinates outside the Earth with no coordinate
// system to explain them are most likely a projected system the file doesn't name.
func toWgs84(ds *Dataset, notes notices) error {
	crs := ds.Crs
	if crs.Approximate() {
		notes["notice.geo_datum_assumed"] = true
	}
	for i, f := range ds.Features {
		if f.Geometry == nil {
			continue
		}
		converted := f.Geometry
		if !crs.IsIdentity() {
			converted = f.Geometry.Map(crs.ToLonLat)
		}
		if !inRange(converted) {
			if ds.CrsDeclared && !crs.IsGeographic() {
				return convert.Refuse("error.geo_bad_coordinates", crs.Name())
			}
			detail := ""
			if ds.CrsDeclared {
				detail = crs.Name()
			}
			return convert.Refuse("error.geo_unknown_crs", detail)
		}
		if converted != f.Geometry {
			ds.Features[i] = &Feature{Geometry: converted, Properties: f.Properties}
		}
	}
	ds.Crs = wgs84
	return nil
}

func inRange(g Geometry) bool {
	for _, c := range g.Coordinates() {
		if !c.IsFinite() || c.Y < -90.000001 || c.Y > 90.000001 || c.X < -360 || c.X > 360 {
			return false
		}
	}
	return true
}

func write(ctx context.Context, ds *Dataset, step *convert.Step, outputs *[]string, notes notices) error {
	switch step.To {
	case "geojson", "kml", "kmz", "gpx", "csv":
		path := step.Output.Allocate(step.To, "")
		if path == "" {
			return nil
		}
		*outputs = append(*outputs, path)
		switch step.To {
		case "geojson":
			return writeGeoJSON(ctx, ds, path)
		case "kml", "kmz":
			return writeKml(ctx, ds, path, step.To == "kmz")
		case "gpx":
			lossy, err := writeGpx(ctx, ds, path)
			if lossy {
				notes["notice.geo_gpx_lossy"] = true
			}
			return err
		}
		return writeGeoCSV(ctx, ds, path)
	case "shp":
		return writeShapefiles(ctx, ds, step, outputs, notes)
	}
	return fmt.Errorf("%s is not a geospatial format.", step.To)
}

var siblingExtensions = []string{"shx", "dbf", "prj", "cpg"}

func siblings(shp string) []string {
	out := make([]string, len(siblingExtensions))
	for i, e := range siblingExtensions {
		out[i] = changeExt(shp, e)
	}
	return out
}

// writeShapefiles writes one shapefile per kind of geometry; each is the .shp with its .shx, .dbf, .prj and .cpg.
func writeShapefiles(ctx context.Context, ds *Dataset, step *convert.Step, outputs *[]string, notes notices) error {
	groups := planShapefiles(ds)
	if len(groups) > 1 {
		notes["notice.geo_shapefile_split"] = true
	}
	for _, g := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		suffix := ""
		if len(groups) > 1 {
			suffix = map[shapeKind]string{shapePoint: "_points", shapeLine: "_lines", shapePolygon: "_polygons"}[g.kind]
		}
		shp, err := allocateShapefile(step.Output, suffix)
		if err != nil {
			return err
		}
		if shp == "" {
			continue
		}
		*outputs = append(*outputs, shp)
		*outputs = append(*outputs, siblings(shp)...)
		dropHeights := false
		if ds.SparseHeights {
			some, missing := false, false
			for _, r := range g.records {
				if r.Geometry == nil {
					continue
				}
				for _, c := range r.Geometry.Coordinates() {
					if c.HasZ() {
						some = true
					} else {
						missing = true
					}
				}
			}
			dropHeights = some && missing
		}
		losses, err := writeShapefile(ctx, g, shp, dropHeights)
		if err != nil {
			return err
		}
		if dropHeights {
			notes["notice.geo_heights_dropped"] = true
		}
		if losses.textCut {
			notes["notice.geo_text_cut"] = true
		}
		if losses.columnsDropped {
			notes["notice.geo_columns_dropped"] = true
		}
		if losses.namesChanged {
			notes["notice.geo_field_names"] = true
		}
	}
	return nil
}

// allocateShapefile finds a path for a .shp whose sibling files (same name,
// other extensions) are all free. The allocator only knows the .shp, so when
// an old .dbf or .prj of the same name is lying around the next name is tried,
// rather than writing over files that don't belong to this shapefile. When
// the .shp itself is being overwritten (the user's choice), its siblings are
// overwritten with it.
func allocateShapefile(out convert.OutputAllocator, suffix string) (string, error) {
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	for attempt := 1; attempt <= 100; attempt++ {
		s := suffix
		if attempt > 1 {
			s = fmt.Sprintf("%s_%d", suffix, attempt)
		}
		path := out.Allocate("shp", s)
		if path == "" {
			return "", nil
		}
		if exists(path) || !slices.ContainsFunc(siblings(path), exists) {
			return path, nil
		}
	}
	return "", fmt.Errorf("No free name for the shapefile.")
}
