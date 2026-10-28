package geo

// Geospatial CSV in and out. Reading finds the location of each row in a WKT
// column (wkt, geom, geometry, the_geom, shape) or in a pair of longitude /
// latitude columns (lon, lng, long, longitude, x and lat, latitude, y; a
// decimal comma is accepted); the other columns become attributes, typed as
// numbers or true / false when they look like them. Rows with no usable
// location stay in the data without a geometry. Writing gives longitude and
// latitude columns (and elevation) when every geometry is a point, otherwise
// one WKT column; the attributes follow. CSV has no coordinate system, so it
// is always WGS 84.

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
)

var (
	wktNames    = []string{"wktgeo", "wkt", "geom", "geometry", "thegeom", "wktgeom", "wktgeometry", "geometrywkt", "shape", "geometria"}
	lonNames    = []string{"longitude", "lon", "lng", "long", "lons", "longitud", "longitudine", "lond", "xcoord", "xcoordinate", "x", "easting", "lonx"}
	latNames    = []string{"latitude", "lat", "lats", "latitud", "latitudine", "latd", "ycoord", "ycoordinate", "y", "northing", "laty"}
	heightNames = []string{"elevation", "elev", "altitude", "alt", "z", "height", "ele"}
)

// readCSVRows reads a CSV (any delimiter, any encoding) without its empty rows.
func readCSVRows(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := doc.DecodeText(data)
	var rows [][]string
	for _, row := range spreadsheet.ParseCSV(text, spreadsheet.DetectDelimiter(text)) {
		if slices.ContainsFunc(row, func(c string) bool { return c != "" }) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func readGeoCSV(ctx context.Context, path string) (*Dataset, error) {
	rows, err := readCSVRows(path)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, convert.Refuse("error.geo_empty", "")
	}
	header := make([]string, len(rows[0]))
	keys := make([]string, len(rows[0]))
	for i, h := range rows[0] {
		header[i] = strings.TrimLeft(strings.TrimSpace(h), "\uFEFF")
		keys[i] = key(header[i])
	}
	find := func(candidates []string) int {
		for _, c := range candidates {
			if i := slices.Index(keys, c); i >= 0 {
				return i
			}
		}
		return -1
	}
	wkt, lon, lat := find(wktNames), find(lonNames), find(latNames)
	if wkt < 0 && (lon < 0 || lat < 0) {
		return nil, convert.Refuse("error.geo_csv_no_coordinates", strings.Join(header[:min(8, len(header))], ", "))
	}
	heightColumn := -1
	if wkt < 0 {
		heightColumn = find(heightNames)
	}
	ds := newDataset(baseName(path))
	names := uniqueColumnNames(header)
	used := map[int]bool{}
	if wkt >= 0 {
		used[wkt] = true
	} else {
		used[lon], used[lat] = true, true
	}
	located := 0
	for r := 1; r < len(rows); r++ {
		if r&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		row := rows[r]
		cell := func(c int) string {
			if c < len(row) {
				return strings.TrimSpace(row[c])
			}
			return ""
		}
		var g Geometry
		if wkt >= 0 {
			if text := cell(wkt); text != "" {
				if g, err = parseWkt(text); err != nil {
					return nil, fmt.Errorf("Row %d: the %s column is not valid WKT (%v).", r+1, header[wkt], err)
				}
			}
		} else if x, ok := csvNumber(cell(lon)); ok {
			if y, ok := csvNumber(cell(lat)); ok {
				z := math.NaN()
				if heightColumn >= 0 {
					if h, ok := csvNumber(cell(heightColumn)); ok {
						z = h
					}
				}
				g = &Point{Coord{x, y, z}}
			}
		}
		if g != nil {
			located++
		}
		f := &Feature{Geometry: g}
		// A row shorter than the header has nothing in the columns it lacks:
		// those are not made up for every row.
		for c := 0; c < min(len(header), len(row)); c++ {
			if !used[c] {
				f.Properties = append(f.Properties, Prop{names[c], typed(cell(c))})
			}
		}
		ds.Features = append(ds.Features, f)
	}
	if len(ds.Features) == 0 {
		return nil, convert.Refuse("error.geo_empty", "")
	}
	if located == 0 {
		detail := ""
		if wkt >= 0 {
			detail = header[wkt]
		} else {
			detail = header[lon] + ", " + header[lat]
		}
		return nil, convert.Refuse("error.geo_csv_no_locations", detail)
	}
	ds.RowsWithoutLocation = located < len(ds.Features)
	return ds, nil
}

// uniqueColumnNames: blanks named "columnN" and repeats numbered, so every attribute has its own name.
func uniqueColumnNames(header []string) []string {
	seen := map[string]bool{}
	// The next number to try for a repeated name: a header of 100,000 identical names costs 100,000 steps.
	next := map[string]int{}
	names := make([]string, len(header))
	for i, h := range header {
		name := h
		if name == "" {
			name = fmt.Sprintf("column%d", i+1)
		}
		unique := name
		if seen[strings.ToLower(unique)] {
			n := next[strings.ToLower(name)]
			if n == 0 {
				n = 2
			}
			for {
				unique = fmt.Sprintf("%s_%d", name, n)
				if !seen[strings.ToLower(unique)] {
					break
				}
				n++
			}
			next[strings.ToLower(name)] = n + 1
		}
		seen[strings.ToLower(unique)] = true
		names[i] = unique
	}
	return names
}

// csvNumber reads a coordinate: "12.5", "-3", or "12,5" with a decimal comma.
func csvNumber(text string) (float64, bool) {
	if text == "" {
		return 0, false
	}
	if v, ok := xmlNumber(text); ok {
		return v, true
	}
	if strings.Count(text, ",") == 1 && !strings.Contains(text, ".") {
		if v, ok := xmlNumber(strings.Replace(text, ",", ".", 1)); ok {
			return v, true
		}
	}
	return 0, false
}

// ---- writing ----

func writeGeoCSV(ctx context.Context, ds *Dataset, path string) error {
	columns := ds.Columns()
	var geometries []Geometry
	for _, f := range ds.Features {
		if f.Geometry != nil {
			geometries = append(geometries, f.Geometry)
		}
	}
	// An attribute that already is a longitude or latitude column would be
	// mistaken for the geometry when the CSV is read again, so such a dataset
	// gets its geometry as WKT.
	ambiguous := slices.ContainsFunc(columns, func(c string) bool {
		k := key(c)
		return slices.Contains(lonNames, k) || slices.Contains(latNames, k)
	})
	points := !ambiguous && len(geometries) > 0
	withHeight := false
	for _, g := range geometries {
		if _, ok := g.(*Point); !ok {
			points = false
		}
		withHeight = withHeight || hasZ(g)
	}
	withHeight = withHeight && points
	geometryColumns := []string{"wkt"}
	if points {
		geometryColumns = []string{"longitude", "latitude"}
		if withHeight {
			geometryColumns = append(geometryColumns, "elevation")
		}
	}
	// An attribute that already has one of these names keeps it; the geometry column gets "_geo" after it.
	taken := map[string]bool{}
	for _, c := range columns {
		taken[strings.ToLower(c)] = true
	}
	var header []string
	for _, name := range geometryColumns {
		unique := name
		for taken[strings.ToLower(unique)] {
			unique += "_geo"
		}
		taken[strings.ToLower(unique)] = true
		header = append(header, unique)
	}
	header = append(header, columns...)

	rows := make(chan []string)
	go func() {
		defer close(rows)
		for i, f := range ds.Features {
			if i&0x3FF == 0 && ctx.Err() != nil {
				return
			}
			row := make([]string, 0, len(header))
			if points {
				if p, ok := f.Geometry.(*Point); ok {
					row = append(row, degrees(p.Position.X), degrees(p.Position.Y))
					if withHeight {
						if p.Position.HasZ() {
							row = append(row, height(p.Position.Z))
						} else {
							row = append(row, "")
						}
					}
				} else {
					for range geometryColumns {
						row = append(row, "")
					}
				}
			} else if f.Geometry != nil {
				row = append(row, writeWkt(f.Geometry))
			} else {
				row = append(row, "")
			}
			values := map[string]any{}
			for _, p := range f.Properties {
				if _, ok := values[p.Key]; !ok {
					values[p.Key] = p.Value
				}
			}
			for _, c := range columns {
				row = append(row, toText(values[c]))
			}
			rows <- row
		}
	}()
	if err := writeCSVRows(path, header, rows); err != nil {
		for range rows {
		}
		return err
	}
	return ctx.Err()
}

func quoteCSV(v string) string {
	if strings.ContainsAny(v, ",\"\r\n") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// writeCSVRows writes UTF-8 with BOM and CRLF line ends, quotes only when
// needed (what Excel opens correctly everywhere), through a partial file that
// replaces path at the end.
func writeCSVRows(path string, header []string, rows <-chan []string) error {
	partial := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+strconv.FormatInt(int64(os.Getpid()), 36)+".filee-tmp")
	f, err := os.Create(partial)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<16)
	w.WriteString("\uFEFF")
	line := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				w.WriteByte(',')
			}
			w.WriteString(quoteCSV(c))
		}
		w.WriteString("\r\n")
	}
	line(header)
	for row := range rows {
		line(row)
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(partial, path)
	}
	if err != nil {
		os.Remove(partial)
	}
	return err
}
