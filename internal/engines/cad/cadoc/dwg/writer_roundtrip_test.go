package dwg

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// writerTestSummary is what a write → read round trip must keep.
type writerTestSummary struct {
	entities map[string]int
	layers   []string
	ltypes   []string
	styles   []string
	blocks   []string
	geometry []string
}

func writerTestSummarize(doc *cadoc.Document, keep func(cadoc.Entity) bool) writerTestSummary {
	s := writerTestSummary{entities: map[string]int{}}
	for _, l := range doc.Layers.Entries() {
		s.layers = append(s.layers, l.Name)
	}
	for _, l := range doc.LineTypes.Entries() {
		s.ltypes = append(s.ltypes, strings.ToUpper(l.Name))
	}
	for _, t := range doc.TextStyles.Entries() {
		s.styles = append(s.styles, t.Name)
	}
	for _, b := range doc.BlockRecords.Entries() {
		if b.IsAnonymous() {
			continue
		}
		s.blocks = append(s.blocks, strings.ToUpper(b.Name))
	}
	f := func(v float64) string { return fmt.Sprintf("%.6f", v) }
	p3 := func(v cadoc.Vec3) string { return f(v.X) + "," + f(v.Y) + "," + f(v.Z) }
	for _, e := range doc.Entities() {
		if keep != nil && !keep(e) {
			continue
		}
		s.entities[e.ObjectName()]++
		var g string
		switch x := e.(type) {
		case *cadoc.Line:
			g = "LINE " + p3(x.StartPoint) + " " + p3(x.EndPoint) + " " + x.LayerName()
		case *cadoc.Arc:
			g = "ARC " + p3(x.Center) + " " + f(x.Radius) + " " + f(x.StartAngle) + " " + f(x.EndAngle)
		case *cadoc.Circle:
			g = "CIRCLE " + p3(x.Center) + " " + f(x.Radius)
		case *cadoc.LwPolyline:
			g = fmt.Sprintf("LWPOLYLINE %d %v", len(x.Vertices), x.IsClosed())
			for _, v := range x.Vertices {
				g += " " + f(v.Location.X) + "," + f(v.Location.Y) + "," + f(v.Bulge)
			}
		case *cadoc.TextEntity:
			g = "TEXT " + writerTestUnescape(x.Value) + " " + p3(x.InsertPoint) + " " + f(x.Height)
		case *cadoc.MText:
			g = "MTEXT " + writerTestUnescape(x.Value) + " " + p3(x.InsertPoint)
		case *cadoc.Insert:
			g = "INSERT " + strings.ToUpper(x.Block.Name) + " " + p3(x.InsertPoint) + " " + f(x.XScale) + " " + f(x.YScale)
			if strings.HasPrefix(x.Block.Name, "*") {
				g = "INSERT * " + p3(x.InsertPoint)
			}
			for _, a := range x.Attributes {
				g += " " + a.Tag + "=" + a.Value
			}
		case *cadoc.Ellipse:
			g = "ELLIPSE " + p3(x.Center) + " " + p3(x.MajorAxisEndPoint) + " " + f(x.RadiusRatio)
		case *cadoc.Spline:
			g = fmt.Sprintf("SPLINE %d %d %d", len(x.ControlPoints), len(x.Knots), len(x.FitPoints))
		case *cadoc.Hatch:
			g = fmt.Sprintf("HATCH %d %v", len(x.Paths), x.IsSolid)
		case cadoc.Dimension:
			b := x.Dimension()
			g = "DIMENSION " + p3(b.DefinitionPoint)
		case *cadoc.Polyline2D:
			g = fmt.Sprintf("POLYLINE2D %d", len(x.Vertices))
		case *cadoc.Polyline3D:
			g = fmt.Sprintf("POLYLINE3D %d", len(x.Vertices))
		case *cadoc.PolyfaceMesh:
			g = fmt.Sprintf("PFACE %d %d", len(x.Vertices), len(x.Faces))
		}
		if g != "" {
			s.geometry = append(s.geometry, g)
		}
	}
	for _, l := range [][]string{s.layers, s.ltypes, s.styles, s.blocks, s.geometry} {
		sort.Strings(l)
	}
	return s
}

// writerTestUnescape decodes \U+XXXX, which pre-2007 files use for
// characters outside the code page.
func writerTestUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i+7 <= len(s) && s[i] == '\\' && s[i+1] == 'U' && s[i+2] == '+' {
			var r rune
			if _, err := fmt.Sscanf(s[i+3:i+7], "%04X", &r); err == nil {
				b.WriteRune(r)
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func writerTestCompare(t *testing.T, want, got writerTestSummary) {
	t.Helper()
	for k, n := range want.entities {
		if got.entities[k] != n {
			t.Errorf("%s: %d entities read back, want %d", k, got.entities[k], n)
		}
	}
	cmp := func(what string, a, b []string) {
		count := map[string]int{}
		for _, x := range a {
			count[x]++
		}
		for _, x := range b {
			count[x]--
		}
		var diffs []string
		for x, n := range count {
			switch {
			case n > 0:
				diffs = append(diffs, "lost: "+x)
			case n < 0:
				diffs = append(diffs, "new:  "+x)
			}
		}
		sort.Strings(diffs)
		if len(diffs) > 0 {
			if len(diffs) > 10 {
				diffs = diffs[:10]
			}
			t.Errorf("%s differ:\n%s", what, strings.Join(diffs, "\n"))
		}
	}
	cmp("layers", want.layers, got.layers)
	cmp("line types", want.ltypes, got.ltypes)
	cmp("text styles", want.styles, got.styles)
	cmp("blocks", want.blocks, got.blocks)
	cmp("geometry", want.geometry, got.geometry)
}

func TestWriterRoundTrip(t *testing.T) {
	for _, v := range writerTestVersions {
		t.Run(v.String(), func(t *testing.T) {
			doc := writerTestDocument(t)
			data, _ := writerTestWrite(t, doc, v, WriteConfig{})
			w := &writer{doc: doc, version: v}
			want := writerTestSummarize(doc, func(e cadoc.Entity) bool { ok, _ := w.entitySupported(e); return ok })
			var notes []string
			back, err := ReadBytes(data, ReadConfig{}, func(level int, msg string) {
				if level >= writerLevelWarning {
					notes = append(notes, msg)
				}
			})
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			for _, n := range notes {
				t.Logf("read note: %s", n)
			}
			if back.Header.Version != v {
				t.Errorf("version %s", back.Header.Version)
			}
			writerTestCompare(t, want, writerTestSummarize(back, nil))
			writerTestCheckDetails(t, doc, back)
		})
	}
}

// writerTestCheckDetails compares properties the summary doesn't cover.
func writerTestCheckDetails(t *testing.T, want, got *cadoc.Document) {
	t.Helper()
	l, ok := got.Layers.Get("Rot – ä")
	if !ok {
		t.Fatalf("layer with non-ASCII name lost")
	}
	if l.Color.Index() != 1 || got.Header.Version >= cadoc.AC1015 && l.LineWeight != cadoc.LineWeightW35 {
		t.Errorf("layer colour %v, weight %d", l.Color, l.LineWeight)
	}
	var line *cadoc.Line
	for _, e := range got.Entities() {
		if x, ok := e.(*cadoc.Line); ok && x.Layer != nil && x.Layer.Name == l.Name {
			line = x
		}
	}
	if line == nil {
		t.Fatalf("line on the red layer lost")
	}
	if line.LineTypeName() != "DASHED" {
		t.Errorf("line type %q", line.LineTypeName())
	}
	if got.Header.Version >= cadoc.AC1018 {
		if r, g, b := line.Color.RGB(); !line.Color.IsTrueColor() || r != 10 || g != 200 || b != 30 {
			t.Errorf("true colour %v", line.Color)
		}
	}
	if got.Header.Version >= cadoc.AC1024 && line.Transparency.Value() != 40 {
		// The alpha byte round trip loses a little precision.
		if d := line.Transparency.Value() - 40; d < -1 || d > 1 {
			t.Errorf("transparency %d", line.Transparency.Value())
		}
	}
	x, ok := line.GetExtendedData("FILEE")
	if !ok {
		t.Errorf("xdata lost")
	} else if len(x.Records) == 0 || x.Records[0].String() != "hällo" {
		t.Errorf("xdata %v", x.Records)
	}
	if ts, ok := got.TextStyles.Get("Arial Bold"); !ok || ts.FontFamily != "Arial" || ts.TrueType != cadoc.FontFlagsBold {
		t.Errorf("TrueType text style lost: %+v", ts)
	}
	if line.XDictionary == nil {
		t.Errorf("extension dictionary lost")
	}
	if len(got.Groups()) == 0 || len(got.Groups()[0].Entities) != 2 {
		t.Errorf("group lost")
	}
	for _, e := range got.Entities() {
		switch d := e.(type) {
		case *cadoc.DimensionLinear:
			m, _ := d.Measurement()
			if math.Abs(m-10) > 1e-9 || got.Header.Version >= cadoc.AC1015 && math.Abs(d.MeasurementValue-10) > 1e-9 {
				t.Errorf("linear dimension measurement %g / %g", m, d.MeasurementValue)
			}
		case *cadoc.MText:
			if !strings.Contains(writerTestUnescape(d.Value), "中文") {
				t.Errorf("mtext %q", d.Value)
			}
		}
	}
	if got.Header.HandleSeed < want.Header.HandleSeed-1 {
		t.Errorf("handle seed %X, want %X", got.Header.HandleSeed, want.Header.HandleSeed)
	}
}

func TestWriterRoundTripDXFSamples(t *testing.T) {
	for _, path := range writerTestDXFFiles(t) {
		for _, v := range writerTestVersions {
			t.Run(filepath.Base(path)+"/"+v.String(), func(t *testing.T) {
				doc := writerTestReadDXF(t, path)
				data, _ := writerTestWrite(t, doc, v, WriteConfig{})
				w := &writer{doc: doc, version: v, cfg: WriteConfig{}}
				want := writerTestSummarize(doc, func(e cadoc.Entity) bool { ok, _ := w.entitySupported(e); return ok })
				back, err := ReadBytes(data, ReadConfig{}, nil)
				if err != nil {
					t.Fatalf("read back: %v", err)
				}
				writerTestCompare(t, want, writerTestSummarize(back, nil))
			})
		}
	}
}

func TestWriterRoundTripTwice(t *testing.T) {
	// Writing what was read must give the same objects again.
	for _, v := range writerTestVersions {
		doc := writerTestDocument(t)
		data, _ := writerTestWrite(t, doc, v, WriteConfig{})
		back, err := ReadBytes(data, ReadConfig{}, nil)
		if err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		var buf bytes.Buffer
		if err := Write(&buf, back, WriteConfig{}, nil); err != nil {
			t.Fatalf("%s: rewrite: %v", v, err)
		}
		f := wtParse(t, buf.Bytes())
		if n := len(wtParse(t, data).handles); len(f.handles) < n-2 {
			t.Errorf("%s: %d objects after the second write, %d after the first", v, len(f.handles), n)
		}
	}
}
