package dxf

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

var writeVersions = []cadoc.Version{cadoc.AC1015, cadoc.AC1018, cadoc.AC1021, cadoc.AC1024, cadoc.AC1027, cadoc.AC1032}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func nearV(a, b cadoc.Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }

// basicDoc builds a document with one of every basic entity.
func basicDoc(v cadoc.Version) *cadoc.Document {
	doc := cadoc.NewDocument()
	doc.Header.SetVersion(v)
	red := cadoc.NewLayer("Red")
	red.Color = cadoc.ColorIndex(1)
	_ = doc.Layers.Add(red)
	ms := doc.ModelSpace()

	l := cadoc.NewLine()
	l.StartPoint, l.EndPoint = cadoc.Vec3{X: 1, Y: 2}, cadoc.Vec3{X: 3, Y: 4, Z: 5}
	l.Layer = red
	l.Color = cadoc.ColorRGB(10, 20, 30)
	l.LineWeight = cadoc.LineWeightW50
	l.LineTypeScale = 2
	ms.AddEntity(l)

	c := cadoc.NewCircle()
	c.Center, c.Radius, c.Thickness = cadoc.Vec3{X: 5, Y: 5}, 2.5, 1
	ms.AddEntity(c)

	a := cadoc.NewArc()
	a.Center, a.Radius, a.StartAngle, a.EndAngle = cadoc.Vec3{X: -1}, 3, math.Pi/6, math.Pi*1.5
	a.Normal = cadoc.Vec3{Z: -1}
	ms.AddEntity(a)

	e := cadoc.NewEllipse()
	e.Center, e.MajorAxisEndPoint, e.RadiusRatio = cadoc.Vec3{X: 10}, cadoc.Vec3{X: 4, Y: 1}, 0.5
	e.StartParameter, e.EndParameter = 0.25, 4
	ms.AddEntity(e)

	p := cadoc.NewPoint()
	p.Location = cadoc.Vec3{X: 7, Y: 8, Z: 9}
	ms.AddEntity(p)

	xl := cadoc.NewXLine()
	xl.FirstPoint, xl.Direction = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 0.6, Y: 0.8}
	ms.AddEntity(xl)
	ray := cadoc.NewRay()
	ray.StartPoint, ray.Direction = cadoc.Vec3{Y: 1}, cadoc.Vec3{Y: 1}
	ms.AddEntity(ray)

	so := cadoc.NewSolid()
	so.FirstCorner, so.SecondCorner, so.ThirdCorner, so.FourthCorner = cadoc.Vec3{}, cadoc.Vec3{X: 1}, cadoc.Vec3{Y: 1}, cadoc.Vec3{X: 1, Y: 1}
	ms.AddEntity(so)

	f := cadoc.NewFace3D()
	f.FirstCorner, f.SecondCorner, f.ThirdCorner, f.FourthCorner = cadoc.Vec3{}, cadoc.Vec3{X: 1}, cadoc.Vec3{X: 1, Y: 1, Z: 1}, cadoc.Vec3{Y: 1}
	f.Flags = cadoc.InvisibleEdgeFlagsSecond
	ms.AddEntity(f)

	t := cadoc.NewText()
	t.Value, t.InsertPoint, t.Height, t.Rotation = "Hello ^ world", cadoc.Vec3{X: 1, Y: 1}, 2.5, math.Pi/4
	t.HorizontalAlignment, t.VerticalAlignment = cadoc.TextHorizontalAlignmentCenter, cadoc.TextVerticalAlignmentTypeMiddle
	t.AlignmentPoint = cadoc.Vec3{X: 2, Y: 2}
	t.WidthFactor, t.ObliqueAngle = 0.8, 0.1
	t.Mirror = cadoc.TextMirrorFlagBackward
	ms.AddEntity(t)

	m := cadoc.NewMText()
	m.Value = strings.Repeat("Long mtext \\Pline ", 40)
	m.InsertPoint, m.Height, m.RectangleWidth = cadoc.Vec3{X: 3, Y: 3}, 1.5, 40
	m.AttachmentPoint = cadoc.AttachmentPointTypeMiddleCenter
	m.LineSpacing = 1.25
	ms.AddEntity(m)

	lw := cadoc.NewLwPolyline()
	lw.Vertices = []cadoc.LwPolylineVertex{{Location: cadoc.Vec2{X: 0, Y: 0}, Bulge: 0.5}, {Location: cadoc.Vec2{X: 10}, StartWidth: 1, EndWidth: 2}, {Location: cadoc.Vec2{X: 10, Y: 10}}}
	lw.SetClosed(true)
	lw.Elevation = 3
	ms.AddEntity(lw)

	p2 := cadoc.NewPolyline2D()
	for i := 0; i < 3; i++ {
		v := cadoc.NewVertex2D()
		v.Location = cadoc.Vec3{X: float64(i), Y: float64(i * i)}
		v.Bulge = 0.1 * float64(i)
		p2.Vertices = append(p2.Vertices, v)
	}
	p2.Flags = cadoc.PolylineFlagsClosedPolylineOrClosedPolygonMeshInM
	ms.AddEntity(p2)

	p3 := cadoc.NewPolyline3D()
	for i := 0; i < 4; i++ {
		v := cadoc.NewVertex3D()
		v.Location = cadoc.Vec3{X: float64(i), Z: float64(i)}
		p3.Vertices = append(p3.Vertices, v)
	}
	ms.AddEntity(p3)

	pf := cadoc.NewPolyfaceMesh()
	for _, q := range []cadoc.Vec3{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}} {
		v := cadoc.NewVertexFaceMesh()
		v.Location = q
		pf.Vertices = append(pf.Vertices, v)
	}
	face := cadoc.NewVertexFaceRecord()
	face.Index1, face.Index2, face.Index3, face.Index4 = 1, 2, -3, 4
	pf.Faces = append(pf.Faces, face)
	ms.AddEntity(pf)

	pm := cadoc.NewPolygonMesh()
	pm.MVertexCount, pm.NVertexCount = 2, 2
	for _, q := range []cadoc.Vec3{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1, Z: 1}} {
		v := cadoc.NewPolygonMeshVertex()
		v.Location = q
		pm.Vertices = append(pm.Vertices, v)
	}
	ms.AddEntity(pm)

	// A block with an attribute definition, inserted with an attribute.
	blk := cadoc.NewBlockRecord("BLK")
	blk.BlockEntity.BasePoint = cadoc.Vec3{X: 1, Y: 1}
	blk.BlockEntity.Comments = "a block"
	_ = doc.BlockRecords.Add(blk)
	bl := cadoc.NewLine()
	bl.EndPoint = cadoc.Vec3{X: 1}
	blk.AddEntity(bl)
	def := cadoc.NewAttributeDefinition()
	def.Tag, def.Prompt, def.Value, def.Height = "TAG", "Enter", "default", 1
	blk.AddEntity(def)
	ins := cadoc.NewInsert(blk)
	ins.InsertPoint, ins.XScale, ins.YScale, ins.Rotation = cadoc.Vec3{X: 20, Y: 20}, 2, 3, math.Pi/2
	att := cadoc.NewAttribute()
	att.Tag, att.Value, att.Height, att.InsertPoint = "TAG", "value", 1, cadoc.Vec3{X: 20, Y: 20}
	att.Flags = cadoc.AttributeFlagsHidden
	ins.AddAttribute(att)
	ms.AddEntity(ins)
	mi := cadoc.NewInsert(blk)
	mi.ColumnCount, mi.RowCount, mi.ColumnSpacing, mi.RowSpacing = 3, 2, 5, 6
	ms.AddEntity(mi)

	tol := cadoc.NewTolerance()
	tol.Text, tol.InsertionPoint = "{\\Fgdt;j}%%v0.1", cadoc.Vec3{X: 4}
	ms.AddEntity(tol)

	// Paper space content.
	ps := doc.PaperSpace()
	pl := cadoc.NewLine()
	pl.EndPoint = cadoc.Vec3{X: 100, Y: 100}
	ps.AddEntity(pl)
	return doc
}

func entitiesByType[T cadoc.Entity](r *cadoc.BlockRecord) []T {
	var out []T
	for _, e := range r.Entities {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}

func one[T cadoc.Entity](t *testing.T, r *cadoc.BlockRecord) T {
	t.Helper()
	all := entitiesByType[T](r)
	if len(all) == 0 {
		var zero T
		t.Fatalf("no %T in %s", zero, r.Name)
	}
	return all[0]
}

func checkBasic(t *testing.T, doc *cadoc.Document) {
	t.Helper()
	ms := doc.ModelSpace()
	l := one[*cadoc.Line](t, ms)
	if !nearV(l.EndPoint, cadoc.Vec3{X: 3, Y: 4, Z: 5}) || l.LayerName() != "Red" || l.LineWeight != cadoc.LineWeightW50 || l.LineTypeScale != 2 {
		t.Errorf("line: %+v layer %s", l.EndPoint, l.LayerName())
	}
	if doc.Header.Version >= cadoc.AC1018 && l.Color != cadoc.ColorRGB(10, 20, 30) {
		t.Errorf("line colour %v", l.Color)
	}
	c := one[*cadoc.Circle](t, ms)
	if c.Radius != 2.5 || c.Thickness != 1 {
		t.Errorf("circle %+v", c)
	}
	a := one[*cadoc.Arc](t, ms)
	if !near(a.StartAngle, math.Pi/6) || !near(a.EndAngle, math.Pi*1.5) || a.Normal.Z != -1 {
		t.Errorf("arc %v %v %v", a.StartAngle, a.EndAngle, a.Normal)
	}
	e := one[*cadoc.Ellipse](t, ms)
	if e.RadiusRatio != 0.5 || e.EndParameter != 4 || e.MajorAxisEndPoint.Y != 1 {
		t.Errorf("ellipse %+v", e)
	}
	if p := one[*cadoc.Point](t, ms); p.Location.Z != 9 {
		t.Errorf("point %+v", p.Location)
	}
	if x := one[*cadoc.XLine](t, ms); !near(x.Direction.Y, 0.8) {
		t.Errorf("xline %+v", x.Direction)
	}
	one[*cadoc.Ray](t, ms)
	if s := one[*cadoc.Solid](t, ms); s.FourthCorner != (cadoc.Vec3{X: 1, Y: 1}) {
		t.Errorf("solid %+v", s.FourthCorner)
	}
	if f := one[*cadoc.Face3D](t, ms); f.Flags != cadoc.InvisibleEdgeFlagsSecond || f.ThirdCorner.Z != 1 {
		t.Errorf("3dface %+v", f)
	}
	tx := one[*cadoc.TextEntity](t, ms)
	if tx.Value != "Hello ^ world" || tx.Height != 2.5 || !near(tx.Rotation, math.Pi/4) || tx.HorizontalAlignment != cadoc.TextHorizontalAlignmentCenter ||
		tx.VerticalAlignment != cadoc.TextVerticalAlignmentTypeMiddle || tx.Mirror != cadoc.TextMirrorFlagBackward || !near(tx.ObliqueAngle, 0.1) || tx.Style == nil {
		t.Errorf("text %+v", tx)
	}
	m := one[*cadoc.MText](t, ms)
	if m.Value != strings.Repeat("Long mtext \\Pline ", 40) || m.RectangleWidth != 40 || m.AttachmentPoint != cadoc.AttachmentPointTypeMiddleCenter || m.LineSpacing != 1.25 {
		t.Errorf("mtext %q", m.Value)
	}
	lw := one[*cadoc.LwPolyline](t, ms)
	if len(lw.Vertices) != 3 || !lw.IsClosed() || lw.Vertices[0].Bulge != 0.5 || lw.Vertices[1].EndWidth != 2 || lw.Elevation != 3 {
		t.Errorf("lwpolyline %+v", lw)
	}
	p2 := one[*cadoc.Polyline2D](t, ms)
	if len(p2.Vertices) != 3 || !p2.IsClosed() || p2.Vertices[2].Location.Y != 4 || !near(p2.Vertices[2].Bulge, 0.2) || p2.Seqend == nil {
		t.Errorf("polyline2d %+v", p2)
	}
	if p3 := one[*cadoc.Polyline3D](t, ms); len(p3.Vertices) != 4 || p3.Vertices[3].Location.Z != 3 {
		t.Errorf("polyline3d %d", len(p3.Vertices))
	}
	pf := one[*cadoc.PolyfaceMesh](t, ms)
	if len(pf.Vertices) != 4 || len(pf.Faces) != 1 || pf.Faces[0].Index3 != -3 {
		t.Errorf("pface %d %d", len(pf.Vertices), len(pf.Faces))
	}
	pm := one[*cadoc.PolygonMesh](t, ms)
	if pm.MVertexCount != 2 || pm.NVertexCount != 2 || len(pm.Vertices) != 4 {
		t.Errorf("pmesh %+v", pm)
	}
	inserts := entitiesByType[*cadoc.Insert](ms)
	if len(inserts) != 2 {
		t.Fatalf("inserts %d", len(inserts))
	}
	ins := inserts[0]
	if ins.Block == nil || ins.Block.Name != "BLK" || ins.XScale != 2 || ins.YScale != 3 || !near(ins.Rotation, math.Pi/2) {
		t.Errorf("insert %+v", ins)
	}
	if len(ins.Attributes) != 1 || ins.Attributes[0].Value != "value" || ins.Attributes[0].Tag != "TAG" || ins.Attributes[0].Flags != cadoc.AttributeFlagsHidden {
		t.Errorf("attributes %+v", ins.Attributes)
	}
	if mi := inserts[1]; !mi.IsMultiple() || mi.ColumnCount != 3 || mi.RowCount != 2 || mi.RowSpacing != 6 {
		t.Errorf("minsert %+v", mi)
	}
	blk, ok := doc.BlockRecords.Get("blk")
	if !ok || blk.BlockEntity.BasePoint != (cadoc.Vec3{X: 1, Y: 1}) || len(blk.Entities) != 2 {
		t.Fatalf("block %v", blk)
	}
	def := blk.AttributeDefinitions()
	if len(def) != 1 || def[0].Prompt != "Enter" || def[0].Value != "default" {
		t.Errorf("attdef %+v", def)
	}
	if tol := one[*cadoc.Tolerance](t, ms); tol.Text != "{\\Fgdt;j}%%v0.1" {
		t.Errorf("tolerance %q", tol.Text)
	}
	ps := doc.PaperSpace()
	if ps == nil || len(ps.Entities) < 1 {
		t.Errorf("paper space entities: %v", ps)
	} else if pl := one[*cadoc.Line](t, ps); pl.EndPoint.X != 100 {
		t.Errorf("paper line %+v", pl.EndPoint)
	}
}

func TestRoundTripBasicEntities(t *testing.T) {
	for _, v := range writeVersions {
		for _, bin := range []bool{false, true} {
			name := v.String()
			if bin {
				name += "-binary"
			}
			t.Run(name, func(t *testing.T) {
				doc := basicDoc(v)
				back, data := roundTrip(t, doc, WriteConfig{Binary: bin})
				if bin != IsBinary(data) {
					t.Fatal("binary flag")
				}
				if back.Header.Version != v {
					t.Fatalf("version %s", back.Header.Version)
				}
				checkBasic(t, back)
				// A second round trip keeps everything (and the handles).
				again, _ := roundTrip(t, back, WriteConfig{Binary: bin})
				checkBasic(t, again)
				if a, b := back.ModelSpace().Entities[0].Base().Handle, again.ModelSpace().Entities[0].Base().Handle; a != b {
					t.Errorf("handle changed %X -> %X", a, b)
				}
			})
		}
	}
}

func TestRoundTripTables(t *testing.T) {
	doc := cadoc.NewDocument()
	doc.Header.SetVersion(cadoc.AC1032)
	dash := cadoc.NewLineType("DASHED2")
	dash.Description = "Dashed __ __"
	dash.Segments = []cadoc.LineTypeSegment{{Length: 0.5, Scale: 1}, {Length: -0.25, Scale: 1}, {Length: 0, Scale: 1}}
	_ = doc.LineTypes.Add(dash)
	lay := cadoc.NewLayer("Walls")
	lay.Handle = 0x500
	lay.Color = cadoc.ColorRGB(200, 100, 50)
	lay.IsOn = false
	lay.Flags = cadoc.LayerFlagsFrozen | cadoc.LayerFlagsLocked
	lay.LineType = dash
	lay.LineWeight = cadoc.LineWeightW35
	lay.PlotFlag = false
	_ = doc.Layers.Add(lay)
	ts := cadoc.NewTextStyle("Arial Bold")
	ts.Filename, ts.FontFamily, ts.TrueType = "arialbd.ttf", "Arial", cadoc.FontFlagsBold|cadoc.FontFlagsItalic
	ts.Height, ts.Width, ts.ObliqueAngle = 2, 0.9, 0.2
	_ = doc.TextStyles.Add(ts)
	ds := cadoc.NewDimensionStyle("ISO-25")
	ds.ArrowSize, ds.TextHeight, ds.DecimalPlaces, ds.Style = 2.5, 2.5, 1, ts
	ds.DimensionLineColor = cadoc.ColorIndex(3)
	ds.PostFix = "<> mm"
	_ = doc.DimensionStyles.Add(ds)
	ucs := cadoc.NewUCS("Tilted")
	ucs.Origin, ucs.XAxis, ucs.YAxis = cadoc.Vec3{X: 1, Y: 2, Z: 3}, cadoc.Vec3{Y: 1}, cadoc.Vec3{X: -1}
	_ = doc.UCSs.Add(ucs)
	view := cadoc.NewView("Front")
	view.Height, view.Width, view.Center, view.Direction = 10, 20, cadoc.Vec2{X: 5, Y: 6}, cadoc.Vec3{Z: 1}
	_ = doc.Views.Add(view)
	_ = doc.AppIds.Add(cadoc.NewAppId("MYAPP"))
	vp, _ := doc.VPorts.Get("*Active")
	vp.ViewHeight, vp.Center = 123, cadoc.Vec2{X: 7, Y: 8}

	l := cadoc.NewLine()
	l.Layer = lay
	l.SetExtendedData("MYAPP", []cadoc.ExtendedDataRecord{{Code: 1000, Value: "hi"}, {Code: 1040, Value: 1.5},
		{Code: 1070, Value: int16(7)}, {Code: 1071, Value: int32(70000)}, {Code: 1010, Value: cadoc.Vec3{X: 1, Y: 2, Z: 3}},
		{Code: 1002, Value: "{"}, {Code: 1005, Value: lay.Handle}, {Code: 1002, Value: "}"}})
	doc.ModelSpace().AddEntity(l)

	for _, bin := range []bool{false, true} {
		back, _ := roundTrip(t, doc, WriteConfig{Binary: bin})
		bl, ok := back.Layers.Get("walls")
		if !ok || bl.IsOn || bl.Flags&cadoc.LayerFlagsFrozen == 0 || bl.Color != cadoc.ColorRGB(200, 100, 50) ||
			bl.LineTypeName() != "DASHED2" || bl.LineWeight != cadoc.LineWeightW35 || bl.PlotFlag {
			t.Errorf("layer %+v", bl)
		}
		blt, _ := back.LineTypes.Get("DASHED2")
		if blt == nil || len(blt.Segments) != 3 || blt.Segments[1].Length != -0.25 || !blt.IsComplex() || blt.Description != "Dashed __ __" {
			t.Errorf("linetype %+v", blt)
		}
		bts, _ := back.TextStyles.Get("Arial Bold")
		if bts == nil || bts.FontFamily != "Arial" || bts.TrueType != cadoc.FontFlagsBold|cadoc.FontFlagsItalic || bts.Width != 0.9 || !near(bts.ObliqueAngle, 0.2) {
			t.Errorf("text style %+v", bts)
		}
		bds, _ := back.DimensionStyles.Get("ISO-25")
		if bds == nil || bds.ArrowSize != 2.5 || bds.DecimalPlaces != 1 || bds.Style == nil || bds.Style.Name != "Arial Bold" ||
			bds.DimensionLineColor != cadoc.ColorIndex(3) || bds.Suffix() != " mm" {
			t.Errorf("dimstyle %+v", bds)
		}
		bu, _ := back.UCSs.Get("Tilted")
		if bu == nil || bu.Origin.Z != 3 || bu.XAxis.Y != 1 {
			t.Errorf("ucs %+v", bu)
		}
		bv, _ := back.Views.Get("Front")
		if bv == nil || bv.Width != 20 || bv.Center.Y != 6 {
			t.Errorf("view %+v", bv)
		}
		bvp, _ := back.VPorts.Get("*ACTIVE")
		if bvp == nil || bvp.ViewHeight != 123 {
			t.Errorf("vport %+v", bvp)
		}
		line := one[*cadoc.Line](t, back.ModelSpace())
		x, ok := line.GetExtendedData("MYAPP")
		if !ok || len(x.Records) != 8 || x.Records[0].String() != "hi" || x.Records[3].Int32() != 70000 || x.Records[4].Point() != (cadoc.Vec3{X: 1, Y: 2, Z: 3}) {
			t.Errorf("xdata %+v", x)
		} else if x.Records[6].Handle() != bl.Handle {
			t.Errorf("xdata handle %X, layer %X", x.Records[6].Handle(), bl.Handle)
		}
	}
}

func TestRoundTripHeader(t *testing.T) {
	doc := cadoc.NewDocument()
	h := doc.Header
	h.SetVersion(cadoc.AC1027)
	h.InsUnits = cadoc.UnitsTypeMillimeters
	h.LineTypeScale = 3.5
	h.PointDisplayMode = 34
	h.ModelSpaceInsertionBase = cadoc.Vec3{X: 1, Y: 2, Z: 3}
	h.TextHeightDefault = 7
	h.CurrentLayerName = "0"
	h.MeasurementUnits = cadoc.MeasurementUnitsEnglish
	h.ModelSpaceExtMin, h.ModelSpaceExtMax = cadoc.Vec3{X: -1}, cadoc.Vec3{X: 100, Y: 50}
	back, _ := roundTrip(t, doc, WriteConfig{HeaderVariables: []string{"$EXTMIN", "$EXTMAX"}})
	bh := back.Header
	if bh.InsUnits != cadoc.UnitsTypeMillimeters || bh.LineTypeScale != 3.5 || bh.PointDisplayMode != 34 ||
		bh.ModelSpaceInsertionBase != h.ModelSpaceInsertionBase || bh.TextHeightDefault != 7 ||
		bh.MeasurementUnits != cadoc.MeasurementUnitsEnglish || bh.ModelSpaceExtMax.Y != 50 {
		t.Errorf("header %+v", bh)
	}
	if d := bh.CreateDateTime.Sub(h.CreateDateTime); d > 1e9 || d < -1e9 {
		t.Errorf("create date %v vs %v", bh.CreateDateTime, h.CreateDateTime)
	}
	// Every variable can be written and read back.
	all, _ := roundTrip(t, doc, WriteConfig{WriteAllHeaderVariables: true})
	if all.Header.LineTypeScale != 3.5 {
		t.Error("all variables")
	}
}

func TestReadR12EntitiesOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/r12_entities.dxf")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ReadBytes(data, ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := doc.ModelSpace()
	if ms == nil || len(ms.Entities) != 4 {
		t.Fatalf("model space: %v", ms)
	}
	line := one[*cadoc.Line](t, ms)
	if line.LayerName() != "WALLS" || line.EndPoint.Y != 5 || line.Color != cadoc.ColorIndex(1) {
		t.Errorf("line %+v", line)
	}
	if _, ok := doc.Layers.Get("WALLS"); !ok {
		t.Error("layer WALLS not created")
	}
	if txt := one[*cadoc.TextEntity](t, ms); txt.Value != "café" {
		t.Errorf("text %q (no code page: invalid UTF-8 falls back to Windows-1252)", txt.Value)
	}
	pl := one[*cadoc.Polyline2D](t, ms)
	if len(pl.Vertices) != 3 || pl.Vertices[1].Bulge != 1 || !pl.IsClosed() {
		t.Errorf("polyline %+v", pl)
	}
	if ps := doc.PaperSpace(); ps == nil || len(entitiesByType[*cadoc.Line](ps)) != 1 {
		t.Errorf("paper space line (67=1) missing")
	}
	if _, ok := doc.Layers.Get("0"); !ok {
		t.Error("missing layer 0")
	}
	if len(doc.Layouts()) != 2 {
		t.Errorf("layouts %d", len(doc.Layouts()))
	}
	for _, v := range []cadoc.Version{cadoc.AC1015, cadoc.AC1032} {
		doc.Header.SetVersion(v)
		back, _ := roundTrip(t, doc, WriteConfig{})
		if len(back.ModelSpace().Entities) != 4 {
			t.Errorf("%s: %d entities", v, len(back.ModelSpace().Entities))
		}
	}
}

func TestR12BlocksAndMeshes(t *testing.T) {
	data, err := os.ReadFile("testdata/r12_blocks.dxf")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ReadBytes(data, ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	blk, ok := doc.BlockRecords.Get("FACE")
	if !ok {
		t.Fatal("block FACE")
	}
	pf := one[*cadoc.PolyfaceMesh](t, blk)
	if len(pf.Vertices) != 3 || len(pf.Faces) != 1 || pf.Faces[0].Index3 != 3 {
		t.Errorf("pface %d/%d", len(pf.Vertices), len(pf.Faces))
	}
	if len(entitiesByType[*cadoc.Seqend](blk)) != 1 {
		t.Error("stray SEQEND kept as an entity")
	}
	ins := one[*cadoc.Insert](t, doc.ModelSpace())
	if ins.Block != blk || len(ins.Attributes) != 1 || ins.Attributes[0].Value != "v" {
		t.Errorf("insert %+v", ins)
	}
	if len(doc.ModelSpace().Entities) != 1 {
		t.Errorf("model space %d", len(doc.ModelSpace().Entities))
	}
	doc.Header.SetVersion(cadoc.AC1018)
	back, _ := roundTrip(t, doc, WriteConfig{})
	if b, _ := back.BlockRecords.Get("FACE"); b == nil || len(entitiesByType[*cadoc.PolyfaceMesh](b)) != 1 {
		t.Error("written mesh")
	}
}

func TestCodePageKorean(t *testing.T) {
	doc := cadoc.NewDocument()
	doc.Header.SetVersion(cadoc.AC1018)
	doc.Header.CodePage = "ANSI_949"
	lay := cadoc.NewLayer("벽체")
	_ = doc.Layers.Add(lay)
	txt := cadoc.NewText()
	txt.Value = "안녕하세요 도면"
	txt.Layer = lay
	doc.ModelSpace().AddEntity(txt)
	odd := cadoc.NewText()
	odd.Value = "€ and ☃"
	doc.ModelSpace().AddEntity(odd)
	back, data := roundTrip(t, doc, WriteConfig{})
	if !bytes.Contains(data, []byte("ANSI_949")) {
		t.Error("code page name")
	}
	// 안녕 in CP949 is BE C8 B3 E7.
	if !bytes.Contains(data, []byte{0xBE, 0xC8, 0xB3, 0xE7}) {
		t.Error("text not encoded in CP949")
	}
	texts := entitiesByType[*cadoc.TextEntity](back.ModelSpace())
	if texts[0].Value != "안녕하세요 도면" || texts[0].LayerName() != "벽체" {
		t.Errorf("korean text %q on %q", texts[0].Value, texts[0].LayerName())
	}
	if texts[1].Value != `€ and \U+2603` {
		t.Errorf("unencodable characters: %q", texts[1].Value)
	}
	// KCS5601 is the same code page.
	doc.Header.CodePage = "KCS5601"
	back, _ = roundTrip(t, doc, WriteConfig{})
	if v := entitiesByType[*cadoc.TextEntity](back.ModelSpace())[0].Value; v != "안녕하세요 도면" {
		t.Errorf("KCS5601: %q", v)
	}
	// AC1021+ is UTF-8 whatever the code page says.
	doc.Header.SetVersion(cadoc.AC1032)
	doc.Header.CodePage = "ANSI_1252"
	odd.Value = "€ and ☃"
	back, data = roundTrip(t, doc, WriteConfig{})
	if !bytes.Contains(data, []byte("안녕하세요")) {
		t.Error("UTF-8 output")
	}
	if v := entitiesByType[*cadoc.TextEntity](back.ModelSpace())[1].Value; v != "€ and ☃" {
		t.Errorf("utf-8 %q", v)
	}
}

func TestCodePagesReadOthers(t *testing.T) {
	cases := []struct {
		page string
		raw  []byte
		want string
	}{
		{"ANSI_1252", []byte{0x63, 0x61, 0x66, 0xE9}, "café"},
		{"ANSI_1251", []byte{0xCF, 0xF0, 0xE8}, "При"},
		{"ANSI_936", []byte{0xD6, 0xD0, 0xCE, 0xC4}, "中文"},
		{"ANSI_950", []byte{0xA4, 0xA4, 0xA4, 0xE5}, "中文"},
		{"ANSI_932", []byte{0x93, 0xFA, 0x96, 0x7B}, "日本"},
		{"GB2312", []byte{0xD6, 0xD0}, "中"},
		{"BIG5", []byte{0xA4, 0xA4}, "中"},
	}
	for _, c := range cases {
		src := "0\nSECTION\n2\nHEADER\n9\n$ACADVER\n1\nAC1015\n9\n$DWGCODEPAGE\n3\n" + c.page +
			"\n0\nENDSEC\n0\nSECTION\n2\nENTITIES\n0\nTEXT\n8\n0\n10\n0\n20\n0\n40\n1\n1\n" + string(c.raw) + "\n0\nENDSEC\n0\nEOF\n"
		doc, err := ReadBytes([]byte(src), ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if v := one[*cadoc.TextEntity](t, doc.ModelSpace()).Value; v != c.want {
			t.Errorf("%s: %q, want %q", c.page, v, c.want)
		}
	}
}

func TestUnknownEntityKept(t *testing.T) {
	src := "0\nSECTION\n2\nHEADER\n9\n$ACADVER\n1\nAC1032\n0\nENDSEC\n0\nSECTION\n2\nENTITIES\n0\nFANCY\n5\n2F\n330\n1F\n100\nAcDbEntity\n8\n0\n100\nAcDbFancy\n10\n1.5\n20\n2.5\n1\nhello\n0\nENDSEC\n0\nEOF\n"
	doc, err := ReadBytes([]byte(src), ReadConfig{Failsafe: true, CreateDefaults: true, KeepUnknownEntities: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	u := one[*cadoc.UnknownEntity](t, doc.ModelSpace())
	if u.DxfName != "FANCY" || len(u.Groups) != 4 {
		t.Fatalf("unknown %+v", u)
	}
	var buf bytes.Buffer
	if err := Write(&buf, doc, WriteConfig{WriteUnknownEntities: true}, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("AcDbFancy")) {
		t.Error("unknown entity not written")
	}
	dropped, _ := ReadBytes([]byte(src), ReadConfig{Failsafe: true, CreateDefaults: true}, nil)
	if len(dropped.ModelSpace().Entities) != 0 {
		t.Error("unknown entity not dropped by default")
	}
}

func TestHandlesAssigned(t *testing.T) {
	doc := basicDoc(cadoc.AC1032)
	back, _ := roundTrip(t, doc, WriteConfig{})
	// New entities next to read ones get fresh handles.
	l := cadoc.NewLine()
	back.ModelSpace().AddEntity(l)
	dup := cadoc.NewCircle()
	dup.Handle = back.ModelSpace().Entities[0].Base().Handle
	back.ModelSpace().AddEntity(dup)
	roundTrip(t, back, WriteConfig{})
	if l.Handle == 0 || dup.Handle == back.ModelSpace().Entities[0].Base().Handle {
		t.Errorf("handles %X %X", l.Handle, dup.Handle)
	}
}

func TestSniff(t *testing.T) {
	v, cp, bin := Sniff([]byte("0\nSECTION\n2\nHEADER\n9\n$ACADVER\n1\nAC1018\n9\n$DWGCODEPAGE\n3\nANSI_949\n0\nENDSEC\n"))
	if v != cadoc.AC1018 || cp != "ANSI_949" || bin {
		t.Errorf("%s %s %v", v, cp, bin)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := ReadBytes([]byte("this is not a dxf\nat all\n"), ReadConfig{}, nil); err == nil {
		t.Error("no error for garbage")
	}
}
