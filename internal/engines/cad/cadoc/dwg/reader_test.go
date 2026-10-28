package dwg

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc/dxf"
)

// The full ACadSharp sample set (sample_AC1014.dwg … sample_AC1032.dwg with
// their DXF files, ~30 MB) is too large for testdata; set ACADSHARP_SAMPLES
// to its directory to run TestReaderSamples. testdata holds a subset.
const samplesEnv = "ACADSHARP_SAMPLES"

// readTestFile reads a file, gunzipping *.gz.
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if data, err = io.ReadAll(zr); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

// readDWG reads a DWG in failsafe mode and fails the test on errors and on
// object read errors.
func readDWG(t *testing.T, name string, data []byte) *cadoc.Document {
	t.Helper()
	var errs []string
	doc, err := ReadBytes(data, ReadConfig{Failsafe: true}, func(level int, msg string) {
		if level == NotifyError {
			errs = append(errs, msg)
		}
	})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for i, e := range errs {
		if i == 10 {
			t.Errorf("%s: … %d more errors", name, len(errs)-10)
			break
		}
		t.Errorf("%s: %s", name, e)
	}
	return doc
}

func readDXF(t *testing.T, name string, data []byte) *cadoc.Document {
	t.Helper()
	doc, err := dxf.ReadBytes(data, dxf.ReadConfig{Failsafe: true}, nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return doc
}

func TestReaderSamples(t *testing.T) {
	dir := os.Getenv(samplesEnv)
	if dir == "" {
		t.Skip(samplesEnv + " not set")
	}
	for _, v := range []string{"AC1014", "AC1015", "AC1018", "AC1021", "AC1024", "AC1027", "AC1032"} {
		t.Run(v, func(t *testing.T) {
			name := "sample_" + v
			doc := readDWG(t, name+".dwg", readTestFile(t, filepath.Join(dir, name+".dwg")))
			dxfName := name + "_ascii.dxf"
			if v == "AC1014" {
				// There is no R14 DXF; the drawing is the same as the 2000 one.
				dxfName = "sample_AC1015_ascii.dxf"
			}
			ref := readDXF(t, dxfName, readTestFile(t, filepath.Join(dir, dxfName)))
			d := &docDiff{t: t, name: name, r14: v == "AC1014"}
			compareDocs(d, doc, ref)
			compareHeader(d, doc.Header, ref.Header)
			d.report(60)
		})
	}
}

func compareHeader(d *docDiff, w, x *cadoc.Header) {
	if w.InsUnits != x.InsUnits && !d.r14 {
		d.add("$INSUNITS %v/%v", w.InsUnits, x.InsUnits)
	}
	for _, c := range []struct {
		name string
		a, b float64
	}{{"$LTSCALE", w.LineTypeScale, x.LineTypeScale}, {"$TEXTSIZE", w.TextHeightDefault, x.TextHeightDefault},
		{"$DIMSCALE", w.DimensionScaleFactor, x.DimensionScaleFactor}, {"$DIMTXT", w.DimensionTextHeight, x.DimensionTextHeight},
		{"$PDSIZE", w.PointDisplaySize, x.PointDisplaySize}, {"$FILLETRAD", w.FilletRadius, x.FilletRadius}} {
		if !near(c.a, c.b) {
			d.add("%s %v/%v", c.name, c.a, c.b)
		}
	}
	if !nearVec(w.ModelSpaceExtMin, x.ModelSpaceExtMin) || !nearVec(w.ModelSpaceExtMax, x.ModelSpaceExtMax) {
		d.add("$EXTMIN/$EXTMAX %v %v / %v %v", w.ModelSpaceExtMin, w.ModelSpaceExtMax, x.ModelSpaceExtMin, x.ModelSpaceExtMax)
	}
	for _, c := range []struct{ name, a, b string }{{"$CLAYER", w.CurrentLayerName, x.CurrentLayerName},
		{"$TEXTSTYLE", w.CurrentTextStyleName, x.CurrentTextStyleName}, {"$DIMSTYLE", w.CurrentDimensionStyleName, x.CurrentDimensionStyleName},
		{"$CELTYPE", w.CurrentLineTypeName, x.CurrentLineTypeName}, {"$CMLSTYLE", w.CurrentMLineStyleName, x.CurrentMLineStyleName}} {
		if !strings.EqualFold(c.a, c.b) {
			d.add("%s %q/%q", c.name, c.a, c.b)
		}
	}
	if w.DisplayLineWeight != x.DisplayLineWeight || w.CurrentEntityLineWeight != x.CurrentEntityLineWeight {
		d.add("$LWDISPLAY/$CELWEIGHT %v %v / %v %v", w.DisplayLineWeight, w.CurrentEntityLineWeight, x.DisplayLineWeight, x.CurrentEntityLineWeight)
	}
}
