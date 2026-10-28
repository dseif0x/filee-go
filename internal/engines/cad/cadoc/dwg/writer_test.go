package dwg

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

var writerTestVersions = []cadoc.Version{cadoc.AC1014, cadoc.AC1015, cadoc.AC1018, cadoc.AC1024, cadoc.AC1027, cadoc.AC1032}

// writerTestDocument builds a document with most entity and object types.
func writerTestDocument(t *testing.T) *cadoc.Document {
	t.Helper()
	doc := cadoc.NewDocument()
	doc.Header.CodePage = "ANSI_1252"

	red := cadoc.NewLayer("Rot – ä")
	red.Color = cadoc.ColorIndex(1)
	red.LineWeight = cadoc.LineWeightW35
	_ = doc.Layers.Add(red)
	dashed := cadoc.NewLineType("DASHED")
	dashed.Description = "Dashed __ __"
	dashed.Segments = []cadoc.LineTypeSegment{{Length: 0.5, Scale: 1}, {Length: -0.25, Scale: 1}}
	_ = doc.LineTypes.Add(dashed)
	text := cadoc.NewLineType("TEXTLT")
	std, _ := doc.TextStyles.Get(cadoc.DefaultTextStyleName)
	text.Segments = []cadoc.LineTypeSegment{{Length: 1, Scale: 1}, {Length: -0.2, Flags: cadoc.LineTypeShapeFlagsText,
		Text: "GAS", Style: std, Scale: 0.1}, {Length: -0.2, Scale: 1}}
	_ = doc.LineTypes.Add(text)
	_ = doc.UCSs.Add(cadoc.NewUCS("MyUCS"))
	v := cadoc.NewView("MyView")
	v.Height, v.Width = 10, 20
	_ = doc.Views.Add(v)
	_ = doc.AppIds.Add(cadoc.NewAppId("FILEE"))
	arial := cadoc.NewTextStyle("Arial Bold")
	arial.Filename, arial.FontFamily, arial.TrueType = "arialbd.ttf", "Arial", cadoc.FontFlagsBold
	_ = doc.TextStyles.Add(arial)

	ms := doc.ModelSpace()
	add := func(e cadoc.Entity) { ms.AddEntity(e) }

	l := cadoc.NewLine()
	l.StartPoint, l.EndPoint = cadoc.Vec3{X: 1, Y: 2}, cadoc.Vec3{X: 11, Y: 2.5, Z: 3}
	l.Layer = red
	l.LineType = dashed
	l.Color = cadoc.ColorRGB(10, 200, 30)
	l.Transparency = cadoc.NewTransparency(40)
	l.SetExtendedData("FILEE", []cadoc.ExtendedDataRecord{{Code: 1000, Value: "hällo"},
		{Code: 1002, Value: "{"}, {Code: 1070, Value: int16(7)}, {Code: 1071, Value: int32(70000)},
		{Code: 1040, Value: 2.5}, {Code: 1010, Value: cadoc.Vec3{X: 1, Y: 2, Z: 3}}, {Code: 1004, Value: []byte{1, 2, 3}},
		{Code: 1005, Value: red.Handle}, {Code: 1003, Value: "0"}, {Code: 1002, Value: "}"}})
	add(l)
	c := cadoc.NewCircle()
	c.Center, c.Radius = cadoc.Vec3{X: 5, Y: 5}, 2
	add(c)
	a := cadoc.NewArc()
	a.Center, a.Radius, a.StartAngle, a.EndAngle = cadoc.Vec3{X: 1}, 3, 0.5, 2
	a.Normal = cadoc.Vec3{X: 0, Y: 1, Z: 0}
	add(a)
	el := cadoc.NewEllipse()
	el.MajorAxisEndPoint, el.RadiusRatio = cadoc.Vec3{X: 4}, 0.5
	add(el)
	p := cadoc.NewPoint()
	p.Location = cadoc.Vec3{X: 3, Y: 3, Z: 3}
	add(p)
	xl := cadoc.NewXLine()
	add(xl)
	ray := cadoc.NewRay()
	ray.StartPoint = cadoc.Vec3{X: 2}
	add(ray)
	so := cadoc.NewSolid()
	so.SecondCorner, so.ThirdCorner, so.FourthCorner = cadoc.Vec3{X: 1}, cadoc.Vec3{Y: 1}, cadoc.Vec3{X: 1, Y: 1}
	so.Thickness = 2
	add(so)
	f3 := cadoc.NewFace3D()
	f3.SecondCorner, f3.ThirdCorner, f3.FourthCorner = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 1, Y: 1, Z: 1}, cadoc.Vec3{Y: 1}
	f3.Flags = 3
	add(f3)
	tx := cadoc.NewText()
	tx.Value, tx.InsertPoint, tx.Height, tx.Rotation = "Grüße €", cadoc.Vec3{X: 1, Y: 1}, 2.5, 0.3
	tx.HorizontalAlignment = cadoc.TextHorizontalAlignment(1)
	tx.AlignmentPoint = cadoc.Vec3{X: 4, Y: 1}
	add(tx)
	mt := cadoc.NewMText()
	mt.Value = `{\fArial|b1;Bold} \Pnext line 中文`
	mt.RectangleWidth = 20
	mt.BackgroundFillFlags = cadoc.BackgroundFillFlagsUseBackgroundFillColor
	mt.BackgroundColor = cadoc.ColorIndex(2)
	add(mt)
	lw := cadoc.NewLwPolyline()
	lw.Vertices = []cadoc.LwPolylineVertex{{Location: cadoc.Vec2{X: 0, Y: 0}}, {Location: cadoc.Vec2{X: 10, Y: 0}, Bulge: 0.5},
		{Location: cadoc.Vec2{X: 10, Y: 10}, StartWidth: 0.2, EndWidth: 0.3}}
	lw.SetClosed(true)
	lw.Elevation = 1.5
	add(lw)
	p2 := cadoc.NewPolyline2D()
	for i := 0; i < 3; i++ {
		vx := cadoc.NewVertex2D()
		vx.Location = cadoc.Vec3{X: float64(i), Y: float64(i * i)}
		vx.StartWidth, vx.EndWidth = 0.1, 0.1
		p2.Vertices = append(p2.Vertices, vx)
	}
	add(p2)
	p3 := cadoc.NewPolyline3D()
	for i := 0; i < 4; i++ {
		vx := cadoc.NewVertex3D()
		vx.Location = cadoc.Vec3{X: float64(i), Y: 1, Z: float64(i)}
		p3.Vertices = append(p3.Vertices, vx)
	}
	add(p3)
	pf := cadoc.NewPolyfaceMesh()
	for _, pt := range []cadoc.Vec3{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}} {
		vx := cadoc.NewVertexFaceMesh()
		vx.Location = pt
		pf.Vertices = append(pf.Vertices, vx)
	}
	fr := cadoc.NewVertexFaceRecord()
	fr.Index1, fr.Index2, fr.Index3, fr.Index4 = 1, 2, 3, -4
	pf.Faces = append(pf.Faces, fr)
	add(pf)
	pm := cadoc.NewPolygonMesh()
	pm.MVertexCount, pm.NVertexCount = 2, 2
	for _, pt := range []cadoc.Vec3{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1, Z: 1}} {
		vx := cadoc.NewPolygonMeshVertex()
		vx.Location = pt
		pm.Vertices = append(pm.Vertices, vx)
	}
	add(pm)
	sp := cadoc.NewSpline()
	sp.ControlPoints = []cadoc.Vec3{{}, {X: 1, Y: 2}, {X: 3, Y: 2}, {X: 4}}
	sp.Knots = []float64{0, 0, 0, 0, 1, 1, 1, 1}
	sp.Weights = []float64{1, 0.5, 0.5, 1}
	sp.Flags = cadoc.SplineFlagsRational
	add(sp)
	sp2 := cadoc.NewSpline()
	sp2.FitPoints = []cadoc.Vec3{{}, {X: 1, Y: 1}, {X: 2}}
	add(sp2)
	h := cadoc.NewHatch()
	h.IsSolid = true
	h.Paths = []*cadoc.HatchBoundaryPath{
		{Flags: cadoc.BoundaryPathFlagsExternal, Edges: []cadoc.HatchEdge{&cadoc.HatchPolylineEdge{IsClosed: true,
			Vertices: []cadoc.Vec3{{}, {X: 5}, {X: 5, Y: 5, Z: 0.3}}}}},
		{Flags: cadoc.BoundaryPathFlagsOutermost, Edges: []cadoc.HatchEdge{
			&cadoc.HatchLineEdge{Start: cadoc.Vec2{}, End: cadoc.Vec2{X: 1}},
			&cadoc.HatchArcEdge{Center: cadoc.Vec2{X: 1, Y: 1}, Radius: 1, StartAngle: -math.Pi / 2, EndAngle: 0, CounterClockWise: true},
			&cadoc.HatchEllipseEdge{Center: cadoc.Vec2{X: 1, Y: 2}, MajorAxisEndPoint: cadoc.Vec2{X: 1}, RadiusRatio: 0.5, EndAngle: math.Pi, CounterClockWise: true},
			&cadoc.HatchSplineEdge{Degree: 3, Knots: []float64{0, 0, 0, 0, 1, 1, 1, 1},
				ControlPoints: []cadoc.Vec3{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}}, FitPoints: []cadoc.Vec2{{}, {X: 1}}},
		}},
	}
	h.SeedPoints = []cadoc.Vec2{{X: 1, Y: 1}}
	add(h)
	h2 := cadoc.NewHatch()
	h2.Pattern = &cadoc.HatchPattern{Name: "ANSI31", Lines: []cadoc.HatchPatternLine{{Angle: math.Pi / 4,
		Offset: cadoc.Vec2{X: -0.1, Y: 0.1}, DashLengths: []float64{0.5, -0.2}}}}
	h2.PatternType = cadoc.HatchPatternType(1)
	h2.Paths = []*cadoc.HatchBoundaryPath{{Flags: cadoc.BoundaryPathFlagsExternal | cadoc.BoundaryPathFlagsDerived,
		Edges:    []cadoc.HatchEdge{&cadoc.HatchPolylineEdge{IsClosed: true, Vertices: []cadoc.Vec3{{}, {X: 2}, {Y: 2}}}},
		Entities: []cadoc.Entity{lw}}}
	add(h2)

	blk := cadoc.NewBlockRecord("MyBlock")
	_ = doc.BlockRecords.Add(blk)
	bl := cadoc.NewLine()
	bl.EndPoint = cadoc.Vec3{X: 1, Y: 1}
	blk.AddEntity(bl)
	ad := cadoc.NewAttributeDefinition()
	ad.Tag, ad.Prompt, ad.Value = "TAG", "Enter", "default"
	blk.AddEntity(ad)
	ins := cadoc.NewInsert(blk)
	ins.InsertPoint = cadoc.Vec3{X: 20, Y: 20}
	ins.XScale, ins.YScale, ins.ZScale = 2, 3, 1
	ins.Rotation = 0.7
	att := cadoc.NewAttribute()
	att.Tag, att.Value = "TAG", "value 1"
	ins.AddAttribute(att)
	add(ins)
	mi := cadoc.NewInsert(blk)
	mi.ColumnCount, mi.RowCount, mi.ColumnSpacing, mi.RowSpacing = 3, 2, 5, 5
	add(mi)
	anon := cadoc.NewBlockRecord("*U1")
	anon.SetFlags(cadoc.BlockTypeFlagsAnonymous)
	anon.AddEntity(cadoc.NewCircle())
	_ = doc.BlockRecords.Add(anon)
	add(cadoc.NewInsert(anon))

	dl := cadoc.NewDimensionLinear()
	dl.FirstPoint, dl.SecondPoint, dl.DefinitionPoint = cadoc.Vec3{}, cadoc.Vec3{X: 10}, cadoc.Vec3{X: 10, Y: 5}
	dl.TextMiddlePoint = cadoc.Vec3{X: 5, Y: 5}
	add(dl)
	da := cadoc.NewDimensionAligned()
	da.FirstPoint, da.SecondPoint, da.DefinitionPoint = cadoc.Vec3{}, cadoc.Vec3{X: 3, Y: 4}, cadoc.Vec3{X: 3, Y: 5}
	add(da)
	dr := cadoc.NewDimensionRadius()
	dr.DefinitionPoint, dr.AngleVertex = cadoc.Vec3{X: 5, Y: 5}, cadoc.Vec3{X: 7, Y: 5}
	add(dr)
	dd := cadoc.NewDimensionDiameter()
	dd.DefinitionPoint, dd.AngleVertex = cadoc.Vec3{X: 3, Y: 5}, cadoc.Vec3{X: 7, Y: 5}
	add(dd)
	d2 := cadoc.NewDimensionAngular2Line()
	d2.FirstPoint, d2.SecondPoint, d2.AngleVertex, d2.DefinitionPoint = cadoc.Vec3{X: 1}, cadoc.Vec3{X: 5}, cadoc.Vec3{Y: 1}, cadoc.Vec3{X: 1, Y: 5}
	d2.DimensionArc = cadoc.Vec3{X: 3, Y: 3}
	add(d2)
	d3 := cadoc.NewDimensionAngular3Pt()
	d3.AngleVertex, d3.FirstPoint, d3.SecondPoint, d3.DefinitionPoint = cadoc.Vec3{}, cadoc.Vec3{X: 5}, cadoc.Vec3{Y: 5}, cadoc.Vec3{X: 3, Y: 3}
	add(d3)
	do := cadoc.NewDimensionOrdinate()
	do.FeatureLocation, do.LeaderEndpoint = cadoc.Vec3{X: 2, Y: 3}, cadoc.Vec3{X: 2, Y: 8}
	add(do)
	ldr := cadoc.NewLeader()
	ldr.Vertices = []cadoc.Vec3{{}, {X: 2, Y: 2}, {X: 4, Y: 2}}
	add(ldr)
	tol := cadoc.NewTolerance()
	tol.Text = `{\Fgdt;j}%%v0.5`
	add(tol)
	ml := cadoc.NewMLine()
	ml.Style = doc.MLineStyles()[0]
	ml.Vertices = []cadoc.MLineVertex{{Position: cadoc.Vec3{}, Direction: cadoc.AxisX, Miter: cadoc.AxisY,
		Segments: []cadoc.MLineSegment{{Parameters: []float64{0, 0}}, {Parameters: []float64{-1, 0}}}},
		{Position: cadoc.Vec3{X: 5}, Direction: cadoc.AxisX, Miter: cadoc.AxisY,
			Segments: []cadoc.MLineSegment{{Parameters: []float64{0, 0}}, {Parameters: []float64{-1, 0}}}}}
	add(ml)
	mesh := cadoc.NewMesh()
	mesh.Vertices = []cadoc.Vec3{{}, {X: 1}, {Y: 1}}
	mesh.Faces = [][]int32{{0, 1, 2}}
	crease := 0.5
	mesh.Edges = []cadoc.MeshEdge{{Start: 0, End: 1, Crease: &crease}, {Start: 1, End: 2}}
	add(mesh)
	reg := cadoc.NewRegion()
	reg.AcisText = "400 0 1 0\n16 Autodesk AutoCAD 19 ASM 222.0.0.0 NT 0\nEnd-of-ASM-data\n"
	add(reg)
	ole := cadoc.NewOle2Frame()
	// The OLE2 data starts with a header holding the frame corners.
	ole.BinaryData = append([]byte{0x80, 0}, bytes.Repeat([]byte{0}, 120)...)
	add(ole)
	imgDef := cadoc.NewImageDefinition("pic")
	imgDef.FileName, imgDef.Size = "pic.png", cadoc.Vec2{X: 640, Y: 480}
	_ = doc.NamedDictionary(cadoc.AcadImageDictionary, true).Add(imgDef)
	img := cadoc.NewRasterImage(imgDef)
	img.Size = cadoc.Vec2{X: 640, Y: 480}
	img.ClipBoundaryVertices = []cadoc.Vec2{{X: -0.5, Y: -0.5}, {X: 639.5, Y: 479.5}}
	add(img)
	wo := cadoc.NewWipeout()
	wo.ClipType = cadoc.ClipTypePolygonal
	wo.ClipBoundaryVertices = []cadoc.Vec2{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0, Y: 0.5}, {X: -0.5, Y: -0.5}}
	add(wo)
	mld := cadoc.NewMultiLeader()
	mld.Style = doc.MLeaderStyles()[0]
	mld.ContextData.HasTextContents = true
	mld.ContextData.TextLabel = "MLeader"
	mld.ContextData.LeaderRoots = []*cadoc.MultiLeaderLeaderRoot{{ContentValid: true, Lines: []*cadoc.MultiLeaderLeaderLine{
		{Points: []cadoc.Vec3{{}, {X: 1, Y: 1}}}}}}
	add(mld)

	// Paper space viewport and objects.
	vp := cadoc.NewViewport()
	vp.Center, vp.Width, vp.Height, vp.Id = cadoc.Vec3{X: 50, Y: 50}, 80, 60, 2
	vp.FrozenLayers = []*cadoc.Layer{red}
	doc.PaperSpace().AddEntity(vp)
	g := cadoc.NewGroup("G1")
	g.Description = "group"
	g.Add(l)
	g.Add(c)
	_ = doc.NamedDictionary(cadoc.AcadGroupDictionary, true).Add(g)
	xr := &cadoc.XRecord{Entries: []cadoc.XRecordEntry{{Code: 1, Value: "text"}, {Code: 10, Value: cadoc.Vec3{X: 1}},
		{Code: 40, Value: 1.5}, {Code: 70, Value: int16(3)}, {Code: 90, Value: int32(9)}, {Code: 280, Value: int16(1)},
		{Code: 290, Value: true}, {Code: 310, Value: []byte{1, 2}}, {Code: 330, Value: l}, {Code: 160, Value: int64(1 << 40)}}}
	xr.Name = "MYXREC"
	_ = doc.RootDictionary.Add(xr)
	xd := l.CreateExtendedDictionary(l)
	xr2 := &cadoc.XRecord{Entries: []cadoc.XRecordEntry{{Code: 1, Value: "in xdict"}}}
	xr2.Name = "X"
	_ = xd.Add(xr2)
	sorts := cadoc.NewSortEntitiesTable(ms)
	sorts.Add(l, 0x7FFF)
	sorts.Add(c, 0x10)
	_ = ms.CreateExtendedDictionary(ms).Add(sorts)
	geo := cadoc.NewGeoData()
	geo.HostBlock = ms
	geo.CoordinateSystemDefinition = "<xml/>"
	_ = ms.CreateExtendedDictionary(ms).Add(func() *cadoc.GeoData { geo.Name = cadoc.GeographicDataDictionary; return geo }())
	pdfDef := &cadoc.PdfUnderlayDefinition{File: "a.pdf", Page: "1"}
	pdfDef.Name = "a - 1"
	_ = doc.NamedDictionary(cadoc.AcadPdfDefinitions, true).Add(pdfDef)
	pdf := cadoc.NewPdfUnderlay()
	pdf.Definition = pdfDef
	pdf.ClipBoundaryVertices = []cadoc.Vec2{{}, {X: 1, Y: 1}}
	add(pdf)
	field := &cadoc.Field{EvaluatorId: "_text", FieldCode: "%<\\AcVar Date>%", FormatString: "fmt",
		Value:  cadoc.CadValue{ValueType: cadoc.CadValueTypeString, Value: "x"},
		Values: []cadoc.FieldValue{{Key: "k", Value: cadoc.CadValue{ValueType: cadoc.CadValueTypeDouble, Value: 2.0}}}}
	field.Name = "F"
	fl := &cadoc.FieldList{Fields: []*cadoc.Field{field}}
	fl.Name = "FL"
	_ = doc.RootDictionary.Add(fl)
	_ = doc.NamedDictionary(cadoc.AcadFieldListDictionary, true)
	wv := &cadoc.WipeoutVariables{DisplayImageFrame: true}
	wv.Name = cadoc.AcadWipeoutVars
	_ = doc.RootDictionary.Add(wv)
	rv := &cadoc.RasterVariables{ClassVersion: 1, IsDisplayFrameShown: true}
	rv.Name = cadoc.AcadImageVars
	_ = doc.RootDictionary.Add(rv)
	dwd := cadoc.NewDictionaryWithDefault(cadoc.AcadPlotStyleName)
	ph := &cadoc.AcdbPlaceHolder{}
	ph.Name = "Normal"
	_ = dwd.Add(ph)
	dwd.DefaultEntry = ph
	_ = doc.RootDictionary.Add(dwd)
	bc := &cadoc.BookColor{Color: cadoc.ColorRGB(1, 2, 3)}
	bc.Name = "BOOK$COLOR"
	_ = doc.NamedDictionary(cadoc.AcadColorDictionary, true).Add(bc)
	cc := cadoc.NewCircle()
	cc.BookColor = bc
	cc.Color = bc.Color
	add(cc)
	mat := &cadoc.Material{Description: "steel", Raw: []cadoc.GroupCode{{Code: 70, Value: int16(1)}, {Code: 40, Value: 0.7},
		{Code: 90, Value: int32(0x102030)}, {Code: 72, Value: int16(1)}, {Code: 3, Value: "steel.png"}}}
	mat.Name = "Steel"
	_ = doc.NamedDictionary(cadoc.AcadMaterialDictionary, true).Add(mat)
	l.Material = mat
	return doc
}

func writerTestWrite(t *testing.T, doc *cadoc.Document, v cadoc.Version, cfg WriteConfig) ([]byte, []string) {
	t.Helper()
	doc.Header.SetVersion(v)
	var notes []string
	var buf bytes.Buffer
	err := Write(&buf, doc, cfg, func(level int, msg string) {
		notes = append(notes, msg)
	})
	if err != nil {
		t.Fatalf("%s: %v", v, err)
	}
	return buf.Bytes(), notes
}

// writerTestExpected returns the handles that must be in the object map.
func writerTestExpected(t *testing.T, doc *cadoc.Document, v cadoc.Version) map[uint64]string {
	t.Helper()
	w := &writer{doc: doc, version: v, index: doc.Index(), writableMemo: map[cadoc.Object]bool{},
		queued: map[cadoc.Object]bool{}}
	out := map[uint64]string{}
	doc.Walk(func(o cadoc.Object) {
		if w.writable(o) {
			out[o.Base().Handle] = o.ObjectName()
		}
	})
	return out
}

func TestWriterVersions(t *testing.T) {
	for _, v := range writerTestVersions {
		t.Run(v.String(), func(t *testing.T) {
			doc := writerTestDocument(t)
			data, notes := writerTestWrite(t, doc, v, WriteConfig{})
			f := wtParse(t, data)
			if f.version != v {
				t.Fatalf("version %s, want %s", f.version, v)
			}
			for h, name := range writerTestExpected(t, doc, v) {
				if _, ok := f.handles[h]; !ok {
					t.Errorf("%s %X not in the handle map", name, h)
				}
			}
			for h := range f.handles {
				if _, ok := doc.Index()[h]; !ok && !(v < cadoc.AC1018 && f.objects[h].typ == int16(cadoc.ObjectTypeVPEntHdrCtrlObj)) {
					t.Errorf("handle %X written but not in the document", h)
				}
			}
			// Spot checks of object types.
			for _, c := range []struct {
				o   cadoc.Object
				typ cadoc.ObjectType
			}{{doc.Layers, cadoc.ObjectTypeLayerControlObj}, {doc.ModelSpace(), cadoc.ObjectTypeBlockHeader},
				{doc.RootDictionary, cadoc.ObjectTypeDictionary}} {
				if got := f.objects[c.o.Base().Handle].typ; got != int16(c.typ) {
					t.Errorf("%s: type %d, want %d", c.o.ObjectName(), got, c.typ)
				}
			}
			for _, n := range notes {
				if strings.Contains(n, "not found") {
					t.Errorf("notification: %s", n)
				}
			}
			t.Logf("%s: %d bytes, %d objects, notes %q", v, len(data), len(f.handles), notes)
		})
	}
}

func TestWriterUnsupportedVersions(t *testing.T) {
	for _, v := range []cadoc.Version{cadoc.AC1021, cadoc.AC1012, cadoc.AC1009, cadoc.VersionUnknown} {
		doc := cadoc.NewDocument()
		doc.Header.Version = v
		if err := Write(&bytes.Buffer{}, doc, WriteConfig{}, nil); err == nil {
			t.Errorf("%s: no error", v)
		}
	}
}

func TestWriterEmptyDocument(t *testing.T) {
	for _, v := range writerTestVersions {
		doc := cadoc.NewDocument()
		data, _ := writerTestWrite(t, doc, v, WriteConfig{})
		f := wtParse(t, data)
		if len(f.handles) < 20 {
			t.Errorf("%s: only %d objects", v, len(f.handles))
		}
	}
}

func TestWriterDeterministic(t *testing.T) {
	doc := writerTestDocument(t)
	a, _ := writerTestWrite(t, doc, cadoc.AC1032, WriteConfig{})
	b, _ := writerTestWrite(t, doc, cadoc.AC1032, WriteConfig{})
	if !bytes.Equal(a, b) {
		t.Errorf("writing twice gives different files")
	}
}

func TestWriterSkipsUnsupported(t *testing.T) {
	doc := cadoc.NewDocument()
	ms := doc.ModelSpace()
	sh := cadoc.NewShape()
	ms.AddEntity(sh)
	bad := cadoc.NewLine()
	bad.Normal = cadoc.Vec3{}
	ms.AddEntity(bad)
	tbl := cadoc.NewTableEntity()
	tbl.Block = cadoc.NewBlockRecord("*T1")
	_ = doc.BlockRecords.Add(tbl.Block)
	ms.AddEntity(tbl)
	_, notes := writerTestWrite(t, doc, cadoc.AC1018, WriteConfig{})
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "Invalid entity LINE") || !strings.Contains(joined, "ACAD_TABLE") {
		t.Errorf("notifications %q", notes)
	}
	f := wtParse(t, mustWrite(t, doc, cadoc.AC1018, WriteConfig{WriteShapes: true}))
	if _, ok := f.handles[sh.Handle]; !ok {
		t.Errorf("shape not written with WriteShapes")
	}
	if _, ok := f.handles[bad.Handle]; ok {
		t.Errorf("invalid line written")
	}
}

func mustWrite(t *testing.T, doc *cadoc.Document, v cadoc.Version, cfg WriteConfig) []byte {
	t.Helper()
	data, _ := writerTestWrite(t, doc, v, cfg)
	return data
}

func TestWriterLZ77RoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	inputs := [][]byte{make([]byte, 0x7400), bytes.Repeat([]byte("ABCDEFGH"), 3000)}
	random := make([]byte, 0x7400)
	rng.Read(random)
	inputs = append(inputs, random)
	mixed := make([]byte, 0x7400)
	for i := range mixed {
		if i%300 < 150 {
			mixed[i] = byte(rng.Intn(4))
		} else {
			mixed[i] = byte(i)
		}
	}
	inputs = append(inputs, mixed)
	// Far repeats (> 0x4000) exercise the long offset opcodes.
	far := make([]byte, 0x7400)
	rng.Read(far)
	copy(far[0x5000:], far[0x100:0x900])
	inputs = append(inputs, far)
	for i, in := range inputs {
		out := &wMemStream{}
		wCompressAC18(in, 0, len(in), out)
		got := wtDecompressAC18(out.bytes())
		if !bytes.Equal(got, in) {
			t.Errorf("input %d: round trip differs (%d → %d → %d bytes)", i, len(in), out.length(), len(got))
		}
	}
}

func TestWriterBitPrimitives(t *testing.T) {
	w := newWBits(cadoc.AC1018, &wMemStream{}, wUTF16Encoder)
	shorts := []int16{0, 1, 255, 256, -1, 300, 32767, -32768}
	longs := []int32{0, 1, 255, 256, -1, 70000}
	doubles := []float64{0, 1, -1, 0.5, 1e300}
	w.writeBit(true)
	for _, s := range shorts {
		w.writeBitShort(s)
	}
	for _, l := range longs {
		w.writeBitLong(l)
	}
	for _, d := range doubles {
		w.writeBitDouble(d)
	}
	w.writeBitLongLong(0x123456789)
	pairs := [][2]float64{{1, 1}, {1.0000001, 1}, {2, 1}, {-3.25, 7}, {1e-12, 0}}
	for _, p := range pairs {
		w.writeBitDoubleWithDefault(p[0], p[1])
	}
	w.handleReference(wRefHardPointer, 0x1234)
	w.handleReference(wRefSoftOwnership, 0)
	w.writeSpearShift()

	r := &wtBits{b: w.ms.bytes()}
	if !r.bit() {
		t.Fatal("bit")
	}
	for _, s := range shorts {
		if got := r.bs(); got != s {
			t.Errorf("BS %d read %d", s, got)
		}
	}
	for _, l := range longs {
		if got := r.bl(); got != l {
			t.Errorf("BL %d read %d", l, got)
		}
	}
	for _, d := range doubles {
		if got := r.bd(); got != d {
			t.Errorf("BD %g read %g", d, got)
		}
	}
	if got := r.bll(); got != 0x123456789 {
		t.Errorf("BLL read %X", got)
	}
	for _, p := range pairs {
		if got := r.dd(p[1]); got != p[0] {
			t.Errorf("DD %g (default %g) read %g", p[0], p[1], got)
		}
	}
	if code, h := r.handle(); code != 5 || h != 0x1234 {
		t.Errorf("handle %d %X", code, h)
	}
	if code, h := r.handle(); code != 2 || h != 0 {
		t.Errorf("handle %d %X", code, h)
	}
}

// TestWriterMergedSizes checks the R2007+ object layout: the RL size and the
// string stream flag at the end of the main data.
func TestWriterMergedSizes(t *testing.T) {
	m := newWMerged(cadoc.AC1032, wUTF16Encoder)
	m.savePositionForSize()
	m.writeBitShort(5)
	m.writeVariableText("abc")
	m.handleReference(wRefHardPointer, 0x42)
	m.writeSpearShift()
	data := m.m.ms.bytes()
	r := &wtBits{b: data}
	total := r.rawLong()
	if r.bs() != 5 {
		t.Fatal("BS")
	}
	// The flag bit is the last bit of the main + string data.
	flag := &wtBits{b: data, pos: int(total) - 1}
	if !flag.bit() {
		t.Errorf("string stream flag not set")
	}
	hr := &wtBits{b: data, pos: int(m.savedPositionInBits())}
	if code, h := hr.handle(); code != 5 || h != 0x42 {
		t.Errorf("handle stream: %d %X", code, h)
	}
}

func TestWriterCRCs(t *testing.T) {
	if got := wCRC8(0xC0C1, []byte("123456789")); got == 0 {
		t.Errorf("crc8 zero")
	}
	if writerCRC8Table[1] != 0xC0C1 || writerCRC8Table[255] != 0x4040 {
		t.Errorf("crc8 table %04X %04X", writerCRC8Table[1], writerCRC8Table[255])
	}
	if writerMagicSequence[0] != 0x29 || writerMagicSequence[1] != 0x23 {
		t.Errorf("magic sequence starts %02X %02X", writerMagicSequence[0], writerMagicSequence[1])
	}
}

func TestWriterConfig(t *testing.T) {
	doc := writerTestDocument(t)
	data, _ := writerTestWrite(t, doc, cadoc.AC1032, WriteConfig{SkipXData: true, SkipXRecords: true})
	f := wtParse(t, data)
	doc.Walk(func(o cadoc.Object) {
		if _, ok := o.(*cadoc.XRecord); ok {
			if _, written := f.handles[o.Base().Handle]; written {
				t.Errorf("XRECORD %X written with SkipXRecords", o.Base().Handle)
			}
		}
	})
	back, err := ReadBytes(data, ReadConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	back.Walk(func(o cadoc.Object) {
		if len(o.Base().ExtendedData) > 0 {
			t.Errorf("%s %X has xdata with SkipXData", o.ObjectName(), o.Base().Handle)
		}
	})
}

func TestWriterWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.dwg")
	doc := writerTestDocument(t)
	doc.Header.SetVersion(cadoc.AC1018)
	if err := WriteFile(path, doc, WriteConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wtParse(t, data)
}
