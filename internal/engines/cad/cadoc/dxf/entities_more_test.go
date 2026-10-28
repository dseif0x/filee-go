package dxf

import (
	"math"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func eNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func eNearV(a, b cadoc.Vec3) bool { return eNear(a.X, b.X) && eNear(a.Y, b.Y) && eNear(a.Z, b.Z) }

// roundTripEntity writes a document holding e in model space in each
// version (and once binary) and returns the entity read back each time.
func roundTripEntity(t *testing.T, build func(doc *cadoc.Document) cadoc.Entity, versions ...cadoc.Version) []cadoc.Entity {
	t.Helper()
	if len(versions) == 0 {
		versions = []cadoc.Version{cadoc.AC1032, cadoc.AC1015}
	}
	var out []cadoc.Entity
	run := func(v cadoc.Version, binary bool) {
		doc := cadoc.NewDocument()
		doc.Header.SetVersion(v)
		e := build(doc)
		doc.ModelSpace().AddEntity(e)
		back, _ := roundTrip(t, doc, WriteConfig{Binary: binary})
		ents := back.ModelSpace().Entities
		if len(ents) != 1 {
			t.Fatalf("%s binary=%v: %d entities", v, binary, len(ents))
		}
		out = append(out, ents[0])
	}
	for _, v := range versions {
		run(v, false)
	}
	run(versions[0], true)
	return out
}

func TestSplineRoundTrip(t *testing.T) {
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		s := cadoc.NewSpline()
		s.Flags = cadoc.SplineFlagsRational | cadoc.SplineFlagsPlanar
		s.ControlPoints = []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 2, Z: 0}, {X: 3, Y: 2, Z: 0}, {X: 4, Y: 0, Z: 0}}
		s.Weights = []float64{1, 0.5, 0.5, 1}
		s.Knots = []float64{0, 0, 0, 0, 1, 1, 1, 1}
		s.FitPoints = []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 4, Y: 0, Z: 0}}
		s.StartTangent = cadoc.Vec3{X: 1}
		return s
	}) {
		s := e.(*cadoc.Spline)
		if len(s.ControlPoints) != 4 || !eNearV(s.ControlPoints[1], cadoc.Vec3{X: 1, Y: 2}) || len(s.Knots) != 8 ||
			len(s.Weights) != 4 || len(s.FitPoints) != 2 || s.Degree != 3 || !s.IsRational() || s.StartTangent.X != 1 {
			t.Fatalf("%+v", s)
		}
	}
}

func buildHatch(*cadoc.Document) cadoc.Entity {
	h := cadoc.NewHatch()
	h.Pattern = &cadoc.HatchPattern{Name: "ANSI31", Lines: []cadoc.HatchPatternLine{
		{Angle: math.Pi / 4, Offset: cadoc.Vec2{X: -0.1, Y: 0.1}, DashLengths: []float64{0.5, -0.25}}}}
	h.PatternAngle = math.Pi / 4
	h.PatternScale = 2
	h.Elevation = 3
	h.Paths = []*cadoc.HatchBoundaryPath{
		{Flags: cadoc.BoundaryPathFlagsExternal | cadoc.BoundaryPathFlagsPolyline,
			Edges: []cadoc.HatchEdge{&cadoc.HatchPolylineEdge{IsClosed: true, Vertices: []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0.5}, {X: 10, Y: 10, Z: 0}}}}},
		{Flags: cadoc.BoundaryPathFlagsOutermost, Edges: []cadoc.HatchEdge{
			&cadoc.HatchLineEdge{Start: cadoc.Vec2{X: 1, Y: 1}, End: cadoc.Vec2{X: 2, Y: 1}},
			&cadoc.HatchArcEdge{Center: cadoc.Vec2{X: 2, Y: 2}, Radius: 1, StartAngle: -math.Pi / 2, EndAngle: 0, CounterClockWise: true},
			&cadoc.HatchEllipseEdge{Center: cadoc.Vec2{X: 2, Y: 2}, MajorAxisEndPoint: cadoc.Vec2{X: 1}, RadiusRatio: 0.5, EndAngle: math.Pi, CounterClockWise: true},
			&cadoc.HatchSplineEdge{Degree: 3, Knots: []float64{0, 0, 0, 0, 1, 1, 1, 1},
				ControlPoints: []cadoc.Vec3{{X: 0, Y: 0, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 3, Y: 0, Z: 1}}},
		}},
	}
	h.SeedPoints = []cadoc.Vec2{{X: 5, Y: 5}}
	return h
}

func TestHatchRoundTrip(t *testing.T) {
	for _, e := range roundTripEntity(t, buildHatch) {
		h := e.(*cadoc.Hatch)
		if h.Pattern.Name != "ANSI31" || len(h.Pattern.Lines) != 1 || len(h.Pattern.Lines[0].DashLengths) != 2 ||
			!eNear(h.PatternAngle, math.Pi/4) || h.PatternScale != 2 || h.Elevation != 3 {
			t.Fatalf("pattern %+v %+v", h, h.Pattern)
		}
		if len(h.SeedPoints) != 1 || h.SeedPoints[0] != (cadoc.Vec2{X: 5, Y: 5}) {
			t.Fatalf("seeds %v", h.SeedPoints)
		}
		if len(h.Paths) != 2 || len(h.Paths[1].Edges) != 4 {
			t.Fatalf("paths %+v", h.Paths)
		}
		pl := h.Paths[0].Edges[0].(*cadoc.HatchPolylineEdge)
		if !pl.IsClosed || len(pl.Vertices) != 3 || pl.Vertices[1].Z != 0.5 {
			t.Fatalf("polyline %+v", pl)
		}
		arc := h.Paths[1].Edges[1].(*cadoc.HatchArcEdge)
		if !eNear(arc.StartAngle, -math.Pi/2) || !arc.CounterClockWise {
			t.Fatalf("arc %+v", arc)
		}
		sp := h.Paths[1].Edges[3].(*cadoc.HatchSplineEdge)
		if len(sp.ControlPoints) != 4 || len(sp.Knots) != 8 {
			t.Fatalf("spline %+v", sp)
		}
	}
}

func TestSolidHatchGradient(t *testing.T) {
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		h := cadoc.NewHatch()
		h.IsSolid = true
		h.Paths = []*cadoc.HatchBoundaryPath{{Flags: cadoc.BoundaryPathFlagsPolyline, Edges: []cadoc.HatchEdge{
			&cadoc.HatchPolylineEdge{IsClosed: true, Vertices: []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 1, Y: 1, Z: 0}}}}}}
		h.GradientColor = &cadoc.HatchGradientPattern{Enabled: true, Name: "LINEAR", Colors: []cadoc.GradientColor{
			{Value: 0, Color: cadoc.ColorIndex(5)}, {Value: 1, Color: cadoc.ColorRGB(10, 20, 30)}}}
		return h
	}, cadoc.AC1032) {
		h := e.(*cadoc.Hatch)
		g := h.GradientColor
		if !h.IsSolid || !g.Enabled || g.Name != "LINEAR" || len(g.Colors) != 2 || g.Colors[1].Color != cadoc.ColorRGB(10, 20, 30) {
			t.Fatalf("%+v", g)
		}
	}
}

func dimBlock(doc *cadoc.Document) *cadoc.BlockRecord {
	b := cadoc.NewBlockRecord(doc.UniqueBlockName("*D"))
	b.SetFlags(cadoc.BlockTypeFlagsAnonymous)
	b.AddEntity(cadoc.NewLine())
	_ = doc.BlockRecords.Add(b)
	return b
}

func TestDimensionsRoundTrip(t *testing.T) {
	builds := []func(doc *cadoc.Document) cadoc.Entity{
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionLinear()
			d.FirstPoint, d.SecondPoint, d.Rotation = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 5, Y: 1}, math.Pi/6
			d.Text = "<> mm"
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionAligned()
			d.FirstPoint, d.SecondPoint = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 5, Y: 1}
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionRadius()
			d.AngleVertex, d.LeaderLength = cadoc.Vec3{X: 3}, 2
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionDiameter()
			d.AngleVertex, d.LeaderLength = cadoc.Vec3{X: 3}, 2
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionAngular2Line()
			d.FirstPoint, d.SecondPoint, d.AngleVertex, d.DimensionArc = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 2}, cadoc.Vec3{Y: 1}, cadoc.Vec3{X: 1, Y: 1}
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionAngular3Pt()
			d.FirstPoint, d.SecondPoint, d.AngleVertex = cadoc.Vec3{X: 1}, cadoc.Vec3{Y: 1}, cadoc.Vec3{}
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionOrdinate()
			d.FeatureLocation, d.LeaderEndpoint = cadoc.Vec3{X: 1, Y: 2}, cadoc.Vec3{X: 1, Y: 5}
			d.Flags |= cadoc.DimensionTypeOrdinateTypeX
			d.Block = dimBlock(doc)
			return d
		},
		func(doc *cadoc.Document) cadoc.Entity {
			d := cadoc.NewDimensionArc()
			d.FirstPoint, d.SecondPoint, d.Center = cadoc.Vec3{X: 1}, cadoc.Vec3{Y: 1}, cadoc.Vec3{}
			d.StartAngle, d.EndAngle, d.HasLeader, d.LeaderPoint1 = 0, math.Pi/2, true, cadoc.Vec3{X: 2, Y: 2}
			d.Block = dimBlock(doc)
			return d
		},
	}
	for i, b := range builds {
		var want cadoc.Entity
		got := roundTripEntity(t, func(doc *cadoc.Document) cadoc.Entity { want = b(doc); return want })
		for _, g := range got {
			if g.ObjectName() != want.ObjectName() || g.SubclassMarker() != want.SubclassMarker() {
				t.Fatalf("%d: %T read back as %T", i, want, g)
			}
			d := g.(cadoc.Dimension).Dimension()
			if d.Block == nil || d.Block.Name != want.(cadoc.Dimension).Dimension().Block.Name || d.Style == nil {
				t.Fatalf("%d: block/style %+v", i, d)
			}
			switch x := g.(type) {
			case *cadoc.DimensionLinear:
				if !eNear(x.Rotation, math.Pi/6) || x.SecondPoint != (cadoc.Vec3{X: 5, Y: 1}) || x.Text != "<> mm" {
					t.Fatalf("linear %+v", x)
				}
			case *cadoc.DimensionOrdinate:
				if !x.IsOrdinateTypeX() || x.LeaderEndpoint.Y != 5 {
					t.Fatalf("ordinate %+v", x)
				}
			case *cadoc.DimensionArc:
				if !x.HasLeader || !eNear(x.EndAngle, math.Pi/2) || x.LeaderPoint1.X != 2 {
					t.Fatalf("arc %+v", x)
				}
			case *cadoc.DimensionAngular2Line:
				if x.DimensionArc != (cadoc.Vec3{X: 1, Y: 1}) {
					t.Fatalf("2line %+v", x)
				}
			case *cadoc.DimensionRadius:
				if x.AngleVertex.X != 3 || x.LeaderLength != 2 {
					t.Fatalf("radius %+v", x)
				}
			}
		}
	}
}

func TestLeaderAndMLine(t *testing.T) {
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		l := cadoc.NewLeader()
		l.Vertices = []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 1, Z: 0}, {X: 2, Y: 1, Z: 0}}
		l.PathType = cadoc.LeaderPathTypeSpline
		l.TextHeight = 2.5
		return l
	}) {
		l := e.(*cadoc.Leader)
		if len(l.Vertices) != 3 || l.Vertices[2].X != 2 || !l.ArrowHeadEnabled || l.Style == nil || l.PathType != cadoc.LeaderPathTypeSpline {
			t.Fatalf("%+v", l)
		}
	}
	for _, e := range roundTripEntity(t, func(doc *cadoc.Document) cadoc.Entity {
		m := cadoc.NewMLine()
		m.Style = doc.MLineStyles()[0]
		m.StartPoint = cadoc.Vec3{X: 1}
		for _, x := range []float64{1, 5} {
			m.Vertices = append(m.Vertices, cadoc.MLineVertex{Position: cadoc.Vec3{X: x}, Direction: cadoc.AxisX, Miter: cadoc.AxisY,
				Segments: []cadoc.MLineSegment{{Parameters: []float64{0.5, 4}}, {Parameters: []float64{-0.5}, AreaFillParameters: []float64{1}}}})
		}
		return m
	}) {
		m := e.(*cadoc.MLine)
		if len(m.Vertices) != 2 || m.Vertices[1].Position.X != 5 || len(m.Vertices[0].Segments) != 2 ||
			len(m.Vertices[0].Segments[0].Parameters) != 2 || len(m.Vertices[0].Segments[1].AreaFillParameters) != 1 {
			t.Fatalf("%+v", m)
		}
		if m.Style == nil || m.Style.Name != "Standard" {
			t.Fatalf("style %+v", m.Style)
		}
	}
}

func TestMultiLeaderRoundTrip(t *testing.T) {
	for _, e := range roundTripEntity(t, func(doc *cadoc.Document) cadoc.Entity {
		m := cadoc.NewMultiLeader()
		m.Style = doc.MLeaderStyles()[0]
		m.LineColor = cadoc.ColorIndex(3)
		m.TextColor = cadoc.ColorRGB(1, 2, 3)
		m.EnableLanding = true
		m.TextStyle, _ = doc.TextStyles.Get("Standard")
		c := m.ContextData
		c.HasTextContents = true
		c.TextLabel = "Note\\Pline"
		c.TextLocation = cadoc.Vec3{X: 5, Y: 5}
		c.TextHeight = 2
		c.TextStyle = m.TextStyle
		c.LeaderRoots = []*cadoc.MultiLeaderLeaderRoot{{ConnectionPoint: cadoc.Vec3{X: 4, Y: 5}, Direction: cadoc.AxisX,
			LandingDistance: 1, Lines: []*cadoc.MultiLeaderLeaderLine{{Points: []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 2, Y: 3, Z: 0}}, Index: 0}}}}
		return m
	}, cadoc.AC1032, cadoc.AC1021) {
		m := e.(*cadoc.MultiLeader)
		c := m.ContextData
		if c == nil || !c.HasTextContents || c.TextLabel != "Note\\Pline" || c.TextLocation.X != 5 || c.TextStyle == nil {
			t.Fatalf("context %+v", c)
		}
		if len(c.LeaderRoots) != 1 || len(c.LeaderRoots[0].Lines) != 1 || len(c.LeaderRoots[0].Lines[0].Points) != 2 ||
			c.LeaderRoots[0].ConnectionPoint.X != 4 || c.LeaderRoots[0].LandingDistance != 1 {
			t.Fatalf("roots %+v", c.LeaderRoots)
		}
		if m.Style == nil || m.LineColor != cadoc.ColorIndex(3) || m.TextColor != cadoc.ColorRGB(1, 2, 3) || !m.EnableLanding {
			t.Fatalf("%+v", m)
		}
	}
}

func TestMeshViewportMisc(t *testing.T) {
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		m := cadoc.NewMesh()
		m.Vertices = []cadoc.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 1, Y: 1, Z: 0}, {X: 0, Y: 1, Z: 1}}
		m.Faces = [][]int32{{0, 1, 2}, {0, 2, 3}}
		c := 0.5
		m.Edges = []cadoc.MeshEdge{{Start: 0, End: 1, Crease: &c}, {Start: 1, End: 2}}
		return m
	}, cadoc.AC1032, cadoc.AC1024) {
		m := e.(*cadoc.Mesh)
		if len(m.Vertices) != 4 || len(m.Faces) != 2 || len(m.Faces[1]) != 3 || m.Faces[1][2] != 3 || len(m.Edges) != 2 ||
			m.Edges[0].Crease == nil || *m.Edges[0].Crease != 0.5 {
			t.Fatalf("%+v", m)
		}
	}
	for _, e := range roundTripEntity(t, func(doc *cadoc.Document) cadoc.Entity {
		v := cadoc.NewViewport()
		v.Center, v.Width, v.Height, v.ViewHeight, v.Id = cadoc.Vec3{X: 5, Y: 5}, 10, 8, 20, 2
		v.ViewCenter = cadoc.Vec2{X: 3}
		v.TwistAngle = 0.25
		l0, _ := doc.Layers.Get("0")
		v.FrozenLayers = []*cadoc.Layer{l0}
		v.Status = cadoc.ViewportStatusFlags(32864)
		return v
	}) {
		v := e.(*cadoc.Viewport)
		if v.Width != 10 || v.ViewHeight != 20 || v.Id != 2 || !eNear(v.TwistAngle, 0.25) || len(v.FrozenLayers) != 1 || v.Status != 32864 {
			t.Fatalf("%+v", v)
		}
	}
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		w := cadoc.NewWipeout()
		w.InsertPoint, w.Size = cadoc.Vec3{X: 1}, cadoc.Vec2{X: 1, Y: 1}
		w.ClipType = cadoc.ClipTypePolygonal
		w.ClipBoundaryVertices = []cadoc.Vec2{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0, Y: 0.5}}
		return w
	}) {
		w := e.(*cadoc.Wipeout)
		if w.InsertPoint.X != 1 || w.ClipType != cadoc.ClipTypePolygonal || len(w.ClipBoundaryVertices) != 3 {
			t.Fatalf("%+v", w)
		}
	}
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		o := cadoc.NewOle2Frame()
		o.SourceApplication = "Paint"
		o.BinaryData = make([]byte, 300)
		o.BinaryData[299] = 7
		return o
	}) {
		o := e.(*cadoc.Ole2Frame)
		if o.SourceApplication != "Paint" || len(o.BinaryData) != 300 || o.BinaryData[299] != 7 {
			t.Fatalf("%+v", o)
		}
	}
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		p := cadoc.NewProxyEntity()
		p.ClassNumber = 500
		p.ProxyGraphics = []byte{1, 2, 3}
		p.BinaryData = []byte{4, 5}
		return p
	}) {
		p := e.(*cadoc.ProxyEntity)
		if !bytesEq(p.ProxyGraphics, []byte{1, 2, 3}) || !bytesEq(p.BinaryData, []byte{4, 5}) || p.ClassNumber != 500 {
			t.Fatalf("%+v", p)
		}
	}
}

func bytesEq(a, b []byte) bool { return string(a) == string(b) }

func TestModelerGeometry(t *testing.T) {
	sat := "400 0 1 0\n16 ASM-Nucleus 4 2 0\nbody $-1 -1 $-1 $1 $-1 $-1 #\nEnd-of-ACIS-data"
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		s := cadoc.NewSolid3D()
		s.AcisText = sat
		return s
	}, cadoc.AC1015, cadoc.AC1018) {
		s := e.(*cadoc.Solid3D)
		if s.AcisText != sat {
			t.Fatalf("%q", s.AcisText)
		}
	}
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		r := cadoc.NewRegion()
		r.AcisText = strings.Repeat("x ", 200) + "\nEnd-of-ACIS-data"
		return r
	}, cadoc.AC1015) {
		if got := e.(*cadoc.Region).AcisText; got != strings.Repeat("x ", 200)+"\nEnd-of-ACIS-data" {
			t.Fatalf("%q", got)
		}
	}
}

// AutoCAD-style snippets.
const acadHatch = `  0
SECTION
  2
ENTITIES
  0
HATCH
  5
2A
330
1F
100
AcDbEntity
  8
0
100
AcDbHatch
 10
0.0
 20
0.0
 30
0.0
210
0.0
220
0.0
230
1.0
  2
SOLID
 70
     1
 71
     0
 91
        1
 92
        1
 93
        4
 72
     1
 10
0.0
 20
0.0
 11
10.0
 21
0.0
 72
     1
 10
10.0
 20
0.0
 11
10.0
 21
10.0
 72
     2
 10
5.0
 20
10.0
 40
5.0
 50
0.0
 51
180.0
 73
     1
 72
     1
 10
0.0
 20
10.0
 11
0.0
 21
0.0
 97
        0
 75
     1
 76
     1
 98
        1
 10
5.0
 20
5.0
450
        0
451
        0
460
0.0
461
0.0
452
        0
462
1.0
453
        2
463
0.0
 63
     5
421
      255
463
1.0
 63
     2
421
 16776960
470
LINEAR
  0
DIMENSION
  5
2B
330
1F
100
AcDbEntity
  8
0
100
AcDbDimension
  2
*D1
 10
10.0
 20
5.0
 30
0.0
 11
5.0
 21
5.5
 31
0.0
 70
    32
 71
     5
 42
10.0
  3
Standard
100
AcDbAlignedDimension
 13
0.0
 23
0.0
 33
0.0
 14
10.0
 24
0.0
 34
0.0
 50
0.0
100
AcDbRotatedDimension
  0
ENDSEC
  0
EOF
`

func TestReadAutoCADSnippets(t *testing.T) {
	doc, err := ReadBytes([]byte(acadHatch), ReadConfig{CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ents := doc.ModelSpace().Entities
	if len(ents) != 2 {
		t.Fatalf("%d entities", len(ents))
	}
	h := ents[0].(*cadoc.Hatch)
	if !h.IsSolid || len(h.Paths) != 1 || len(h.Paths[0].Edges) != 4 || len(h.SeedPoints) != 1 || h.Style != 1 {
		t.Fatalf("%+v", h)
	}
	arc := h.Paths[0].Edges[2].(*cadoc.HatchArcEdge)
	if !eNear(arc.EndAngle, math.Pi) || arc.Radius != 5 {
		t.Fatalf("%+v", arc)
	}
	if g := h.GradientColor; len(g.Colors) != 2 || g.Colors[1].Color != cadoc.ColorRGB(255, 255, 0) {
		t.Fatalf("%+v", g)
	}
	d, ok := ents[1].(*cadoc.DimensionLinear)
	if !ok || d.SecondPoint.X != 10 || d.MeasurementValue != 10 {
		t.Fatalf("%T %+v", ents[1], ents[1])
	}
}

func TestReadR12Dimension(t *testing.T) {
	src := "  0\nSECTION\n  2\nENTITIES\n  0\nDIMENSION\n  8\n0\n  2\n*D3\n 10\n1.0\n 20\n0.0\n 11\n0.5\n 21\n0.2\n 70\n     4\n 15\n3.0\n 25\n0.0\n 40\n0.0\n  0\nENDSEC\n  0\nEOF\n"
	doc, err := ReadBytes([]byte(src), ReadConfig{CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := doc.ModelSpace().Entities[0].(*cadoc.DimensionRadius)
	if !ok || r.AngleVertex.X != 3 {
		t.Fatalf("%T", doc.ModelSpace().Entities[0])
	}
}

func TestImageAndUnderlay(t *testing.T) {
	for _, e := range roundTripEntity(t, func(doc *cadoc.Document) cadoc.Entity {
		def := cadoc.NewImageDefinition("pic")
		def.FileName = "pic.png"
		_ = doc.NamedDictionary(cadoc.AcadImageDictionary, true).Add(def)
		img := cadoc.NewRasterImage(def)
		img.InsertPoint, img.Size = cadoc.Vec3{X: 2}, cadoc.Vec2{X: 640, Y: 480}
		img.Fade = 10
		return img
	}) {
		img := e.(*cadoc.RasterImage)
		if img.InsertPoint.X != 2 || img.Size.Y != 480 || img.Fade != 10 || img.Brightness != 50 {
			t.Fatalf("%+v", img)
		}
	}
	for _, e := range roundTripEntity(t, func(*cadoc.Document) cadoc.Entity {
		p := cadoc.NewPdfUnderlay()
		p.InsertPoint, p.XScale, p.Rotation = cadoc.Vec3{Y: 3}, 2, 0.5
		p.ClipBoundaryVertices = []cadoc.Vec2{{X: 0, Y: 0}, {X: 1, Y: 1}}
		return p
	}, cadoc.AC1032, cadoc.AC1021) {
		p := e.(*cadoc.PdfUnderlay)
		if p.InsertPoint.Y != 3 || p.XScale != 2 || !eNear(p.Rotation, 0.5) || len(p.ClipBoundaryVertices) != 2 {
			t.Fatalf("%+v", p)
		}
	}
}

func TestReadTableAndSurface(t *testing.T) {
	src := "  0\nSECTION\n  2\nENTITIES\n" +
		"  0\nACAD_TABLE\n  5\n30\n100\nAcDbEntity\n  8\n0\n100\nAcDbBlockReference\n  2\n*T1\n 10\n1.0\n 20\n2.0\n 30\n0.0\n" +
		"100\nAcDbTable\n280\n0\n342\n0\n343\n0\n 11\n1.0\n 21\n0.0\n 31\n0.0\n 90\n22\n 91\n1\n 92\n2\n141\n5.0\n142\n10.0\n142\n12.0\n" +
		"171\n1\n172\n0\n173\n0\n174\n0\n 1\nHello\n171\n1\n172\n0\n173\n0\n174\n0\n 1\nWorld\n" +
		"  0\nEXTRUDEDSURFACE\n  5\n31\n100\nAcDbEntity\n  8\n0\n100\nAcDbModelerGeometry\n 70\n1\n  1\noonp\n100\nAcDbSurface\n 71\n6\n 72\n7\n100\nAcDbExtrudedSurface\n 90\n1\n" +
		"  0\nENDSEC\n  0\nE" + "OF\n"
	doc, err := ReadBytes([]byte(src), ReadConfig{CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ents := doc.ModelSpace().Entities
	tb := ents[0].(*cadoc.TableEntity)
	if tb.InsertPoint.Y != 2 || len(tb.Rows) != 1 || len(tb.Columns) != 2 || len(tb.Cells) != 2 || tb.Cells[1].Text != "World" {
		t.Fatalf("%+v", tb)
	}
	sf := ents[1].(*cadoc.Surface)
	if sf.ObjectName() != cadoc.TokenEntityExtrudedSurface || sf.UIsolines != 6 || sf.VIsolines != 7 || sf.AcisText != "001/" || len(sf.Groups) != 2 {
		t.Fatalf("%+v", sf)
	}
}
