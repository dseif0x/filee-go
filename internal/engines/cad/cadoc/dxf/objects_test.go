package dxf

import (
	"math"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

type objectFixture struct {
	doc   *cadoc.Document
	line1 *cadoc.Line
	line2 *cadoc.Line
	ins   *cadoc.Insert
}

func newObjectFixture() *objectFixture {
	doc := cadoc.NewDocument()
	f := &objectFixture{doc: doc}
	ms := doc.ModelSpace()
	f.line1, f.line2 = cadoc.NewLine(), cadoc.NewLine()
	f.line1.EndPoint = cadoc.Vec3{X: 10}
	f.line2.EndPoint = cadoc.Vec3{Y: 7}
	ms.AddEntity(f.line1)
	ms.AddEntity(f.line2)
	blk := cadoc.NewBlockRecord("B1")
	_ = doc.BlockRecords.Add(blk)
	blk.AddEntity(cadoc.NewCircle())
	f.ins = cadoc.NewInsert(blk)
	ms.AddEntity(f.ins)
	root := doc.RootDictionary

	g := cadoc.NewGroup("G1")
	g.Description = "two lines"
	g.Add(f.line1)
	g.Add(f.line2)
	doc.NamedDictionary(cadoc.AcadGroupDictionary, true).Set("G1", g)

	cont, _ := doc.LineTypes.Get(cadoc.ContinuousName)
	ml := cadoc.NewMLineStyle("Custom")
	ml.Description = "desc"
	ml.FillColor = cadoc.ColorIndex(3)
	ml.StartAngle = math.Pi / 3
	ml.Elements = []cadoc.MLineStyleElement{{Offset: 1, Color: cadoc.ColorIndex(1), LineType: cont}, {Offset: -1, Color: cadoc.ColorRGB(10, 20, 30)}}
	doc.NamedDictionary(cadoc.AcadMLineStyleDictionary, true).Set("Custom", ml)

	mls := cadoc.NewMultiLeaderStyle("Custom")
	mls.LineColor = cadoc.ColorRGB(1, 2, 3)
	mls.TextColor = cadoc.ColorIndex(5)
	mls.TextHeight = 3.5
	mls.BlockContentRotation = math.Pi / 4
	mls.BlockContent = blk
	mls.TextStyle, _ = doc.TextStyles.Get(cadoc.DefaultTextStyleName)
	doc.NamedDictionary(cadoc.AcadMLeaderStyleDictionary, true).Set("Custom", mls)

	img := cadoc.NewImageDefinition("pic")
	img.FileName = "c:/pic.png"
	img.Size = cadoc.Vec2{X: 640, Y: 480}
	img.Units = cadoc.ResolutionUnit(2)
	doc.NamedDictionary(cadoc.AcadImageDictionary, true).Set("pic", img)
	root.Set(cadoc.AcadImageVars, &cadoc.RasterVariables{ClassVersion: 0, IsDisplayFrameShown: true, DisplayQuality: 1, Units: 3})
	root.Set(cadoc.AcadWipeoutVars, &cadoc.WipeoutVariables{DisplayImageFrame: true})

	xd := ms.CreateExtendedDictionary(ms)
	xr := &cadoc.XRecord{CloningFlags: cadoc.DictionaryCloningFlagsKeepExisting, Entries: []cadoc.XRecordEntry{
		{Code: 1, Value: "hello"}, {Code: 10, Value: cadoc.Vec3{X: 1, Y: 2, Z: 3}}, {Code: 40, Value: 2.5},
		{Code: 70, Value: int16(7)}, {Code: 90, Value: int32(123456)}, {Code: 290, Value: true}, {Code: 330, Value: f.line2},
	}}
	xd.Set("MYXREC", xr)
	sort := cadoc.NewSortEntitiesTable(ms)
	sort.Add(f.line2, 0x1)
	sort.Add(f.line1, 0x2)
	xd.Set(cadoc.SortEntsDictionaryName, sort)
	geo := cadoc.NewGeoData()
	geo.HostBlock = ms
	geo.DesignPoint = cadoc.Vec3{X: 1, Y: 2}
	geo.ReferencePoint = cadoc.Vec3{X: 13.4, Y: 52.5, Z: 30}
	geo.HorizontalUnitScale = 1
	geo.CoordinateSystemDefinition = strings.Repeat("WKT", 120)
	geo.Points = []cadoc.GeoMeshPoint{{Source: cadoc.Vec2{X: 1, Y: 2}, Destination: cadoc.Vec2{X: 3, Y: 4}}}
	geo.Faces = []cadoc.GeoMeshFace{{Index1: 0, Index2: 1, Index3: 2}}
	xd.Set(cadoc.GeographicDataDictionary, geo)

	sc := &cadoc.Scale{PaperUnits: 1, DrawingUnits: 3}
	sc.Name = "1:3"
	doc.NamedDictionary(cadoc.AcadScaleList, true).Set("1:3", sc)

	bc := &cadoc.BookColor{Color: cadoc.ColorRGB(200, 100, 50)}
	bc.Name = "RAL$1000"
	doc.NamedDictionary(cadoc.AcadColorDictionary, true).Set(bc.Name, bc)
	f.line1.BookColor = bc
	f.line1.Color = bc.Color

	pdf := &cadoc.PdfUnderlayDefinition{File: "a.pdf", Page: "2"}
	doc.NamedDictionary(cadoc.AcadPdfDefinitions, true).Set("a - 2", pdf)

	ixd := f.ins.CreateExtendedDictionary(f.ins)
	filt := cadoc.NewSpatialFilter()
	filt.BoundaryPoints = []cadoc.Vec2{{X: 0, Y: 0}, {X: 5, Y: 5}}
	filt.ClipFrontPlane, filt.FrontDistance = true, 2
	filt.InsertTransform[3] = 9
	filt.InverseInsertTransform[3] = -9
	fd := cadoc.NewNamedDictionary("ACAD_FILTER")
	ixd.Set("ACAD_FILTER", fd)
	fd.Set("SPATIAL", filt)
	ctx := cadoc.NewNamedDictionary("AcDbContextDataManager")
	ixd.Set(ctx.Name, ctx)
	ann := cadoc.NewNamedDictionary("ACDB_ANNOTATIONSCALES")
	ctx.Set(ann.Name, ann)
	def11, _ := doc.NamedDictionary(cadoc.AcadScaleList, false).Get("1:1")
	brc := &cadoc.BlockReferenceObjectContextData{InsertionPoint: cadoc.Vec3{X: 4}, Rotation: 0.5, XScale: 2, YScale: 2, ZScale: 1}
	brc.Version, brc.HasFileToExtensionDictionary, brc.Scale = 3, true, def11.(*cadoc.Scale)
	ann.Set("*A1", brc)
	mtc := &cadoc.MTextAttributeObjectContextData{AttachmentPoint: 5, InsertPoint: cadoc.Vec3{X: 1}, AlignmentPoint: cadoc.AxisX, Rotation: 0.25, Value290: true}
	mtc.Version, mtc.Scale = 3, sc
	ann.Set("*A2", mtc)

	plot := cadoc.NewDictionaryWithDefault(cadoc.AcadPlotStyleName)
	ph := &cadoc.AcdbPlaceHolder{}
	plot.Set("Normal", ph)
	plot.DefaultEntry = ph
	root.Set(plot.Name, plot)

	root.Set("PURGE", &cadoc.DynamicBlockPurgePreventer{Version: 1})

	child := &cadoc.Field{EvaluatorId: "_text", FieldCode: "child"}
	child.Value = cadoc.CadValue{ValueType: cadoc.CadValueTypeDouble, Value: 1.5}
	fld := &cadoc.Field{EvaluatorId: "AcVar", FieldCode: "%<\\AcVar Filename>%" + strings.Repeat("x", 300), FormatString: "%fn2",
		Children: []*cadoc.Field{child}, CadObjects: []cadoc.Object{f.line1}, EvaluationErrorMessage: "none"}
	fld.Value = cadoc.CadValue{ValueType: cadoc.CadValueTypeString, Value: "drawing.dwg"}
	fld.Values = []cadoc.FieldValue{{Key: "ACFD_FIELDTEXT_CHECKSUM", Value: cadoc.CadValue{ValueType: cadoc.CadValueTypeLong, Value: int32(42)}},
		{Key: "P", Value: cadoc.CadValue{ValueType: cadoc.CadValueTypePoint3D, Value: cadoc.Vec3{X: 1, Y: 2, Z: 3}}},
		{Key: "H", Value: cadoc.CadValue{ValueType: cadoc.CadValueTypeHandle, Value: f.line2}}}
	fl := &cadoc.FieldList{Fields: []*cadoc.Field{fld}}
	root.Set(cadoc.AcadFieldListDictionary, fl)
	return f
}

func dictObj(t *testing.T, d *cadoc.Dictionary, name string) cadoc.NonGraphicalObject {
	t.Helper()
	if d == nil {
		t.Fatalf("dictionary for %s missing", name)
	}
	o, ok := d.Get(name)
	if !ok {
		t.Fatalf("%s missing in %s", name, d.Name)
	}
	return o
}

func objNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestObjectsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version cadoc.Version
		binary  bool
	}{{"AC1032", cadoc.AC1032, false}, {"AC1018", cadoc.AC1018, false}, {"AC1032-binary", cadoc.AC1032, true}, {"AC1024", cadoc.AC1024, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newObjectFixture()
			f.doc.Header.Version = tc.version
			back, _ := roundTrip(t, f.doc, WriteConfig{Binary: tc.binary})
			ms := back.ModelSpace()
			var l1, l2 *cadoc.Line
			for _, e := range ms.Entities {
				if l, ok := e.(*cadoc.Line); ok {
					if l.EndPoint.X == 10 {
						l1 = l
					} else {
						l2 = l
					}
				}
			}
			if l1 == nil || l2 == nil {
				t.Fatal("lines missing")
			}

			g := dictObj(t, back.NamedDictionary(cadoc.AcadGroupDictionary, false), "G1").(*cadoc.Group)
			if g.Description != "two lines" || len(g.Entities) != 2 || g.Entities[0] != l1 || g.Entities[1] != l2 || !g.Selectable {
				t.Fatalf("group: %+v", g)
			}
			if len(l1.Reactors) != 1 || l1.Reactors[0] != g {
				t.Fatalf("group reactor: %v", l1.Reactors)
			}

			ml := dictObj(t, back.NamedDictionary(cadoc.AcadMLineStyleDictionary, false), "Custom").(*cadoc.MLineStyle)
			if ml.Description != "desc" || ml.FillColor.Index() != 3 || !objNear(ml.StartAngle, math.Pi/3) || len(ml.Elements) != 2 ||
				ml.Elements[0].Offset != 1 || ml.Elements[0].Color.Index() != 1 || ml.Elements[0].LineType == nil ||
				ml.Elements[0].LineType.Name != cadoc.ContinuousName || ml.Elements[1].Offset != -1 {
				t.Fatalf("mlinestyle: %+v", ml)
			}
			if tc.version >= cadoc.AC1018 && ml.Elements[1].Color != cadoc.ColorRGB(10, 20, 30) {
				t.Fatalf("element true colour: %v", ml.Elements[1].Color)
			}

			mls := dictObj(t, back.NamedDictionary(cadoc.AcadMLeaderStyleDictionary, false), "Custom").(*cadoc.MultiLeaderStyle)
			if mls.LineColor != cadoc.ColorRGB(1, 2, 3) || mls.TextColor.Index() != 5 || mls.TextHeight != 3.5 ||
				!objNear(mls.BlockContentRotation, math.Pi/4) || mls.BlockContent == nil || mls.BlockContent.Name != "B1" ||
				mls.TextStyle == nil || !mls.BlockContentColor.IsByBlock() || mls.LandingDistance != 0.36 {
				t.Fatalf("mleaderstyle: %+v", mls)
			}

			img := dictObj(t, back.NamedDictionary(cadoc.AcadImageDictionary, false), "pic").(*cadoc.ImageDefinition)
			if img.FileName != "c:/pic.png" || img.Size != (cadoc.Vec2{X: 640, Y: 480}) || img.Units != 2 || !img.IsLoaded {
				t.Fatalf("imagedef: %+v", img)
			}
			rv := dictObj(t, back.RootDictionary, cadoc.AcadImageVars).(*cadoc.RasterVariables)
			if !rv.IsDisplayFrameShown || rv.DisplayQuality != 1 || rv.Units != 3 {
				t.Fatalf("rastervariables: %+v", rv)
			}
			if !dictObj(t, back.RootDictionary, cadoc.AcadWipeoutVars).(*cadoc.WipeoutVariables).DisplayImageFrame {
				t.Fatal("wipeout variables")
			}

			xd := ms.XDictionary
			if xd == nil {
				t.Fatal("model space xdictionary missing")
			}
			xr := dictObj(t, xd, "MYXREC").(*cadoc.XRecord)
			if len(xr.Entries) != 7 || xr.CloningFlags != cadoc.DictionaryCloningFlagsKeepExisting {
				t.Fatalf("xrecord: %+v", xr.Entries)
			}
			if xr.Entries[0].Value != "hello" || xr.Entries[1].Value != (cadoc.Vec3{X: 1, Y: 2, Z: 3}) || xr.Entries[2].Value != 2.5 ||
				xr.Entries[3].Value != int16(7) || xr.Entries[4].Value != int32(123456) || xr.Entries[5].Value != true ||
				xr.Entries[6].Value != cadoc.Object(l2) {
				t.Fatalf("xrecord entries: %+v", xr.Entries)
			}
			sort := dictObj(t, xd, cadoc.SortEntsDictionaryName).(*cadoc.SortEntitiesTable)
			if sort.BlockOwner != ms || len(sort.Sorters) != 2 || sort.SortHandle(l2) != 1 || sort.SortHandle(l1) != 2 {
				t.Fatalf("sortents: %+v", sort)
			}
			geo := dictObj(t, xd, cadoc.GeographicDataDictionary).(*cadoc.GeoData)
			if geo.HostBlock != ms || geo.ReferencePoint != (cadoc.Vec3{X: 13.4, Y: 52.5, Z: 30}) || geo.DesignPoint != (cadoc.Vec3{X: 1, Y: 2}) ||
				len(geo.Points) != 1 || geo.Points[0].Destination != (cadoc.Vec2{X: 3, Y: 4}) || len(geo.Faces) != 1 || geo.Faces[0].Index3 != 2 ||
				geo.CoordinateSystemDefinition != strings.Repeat("WKT", 120) || !objNear(geo.NorthDirection.Y, 1) {
				t.Fatalf("geodata: %+v", geo)
			}
			wantVer := cadoc.GeoDataVersionR2013
			if tc.version <= cadoc.AC1021 {
				wantVer = cadoc.GeoDataVersionR2009
			} else if tc.version == cadoc.AC1024 {
				wantVer = cadoc.GeoDataVersionR2010
			}
			if geo.Version != wantVer {
				t.Fatalf("geodata version %d", geo.Version)
			}

			sc := dictObj(t, back.NamedDictionary(cadoc.AcadScaleList, false), "1:3").(*cadoc.Scale)
			if sc.PaperUnits != 1 || sc.DrawingUnits != 3 || sc.IsUnitScale {
				t.Fatalf("scale: %+v", sc)
			}
			if tc.version >= cadoc.AC1018 {
				bc := dictObj(t, back.NamedDictionary(cadoc.AcadColorDictionary, false), "RAL$1000").(*cadoc.BookColor)
				if bc.Color != cadoc.ColorRGB(200, 100, 50) || bc.BookName() != "RAL" || bc.ColorName() != "1000" {
					t.Fatalf("bookcolor: %+v", bc)
				}
				if l1.BookColor != bc {
					t.Fatal("entity book colour not linked")
				}
			}
			pdf := dictObj(t, back.NamedDictionary(cadoc.AcadPdfDefinitions, false), "a - 2").(*cadoc.PdfUnderlayDefinition)
			if pdf.File != "a.pdf" || pdf.Page != "2" {
				t.Fatalf("pdf: %+v", pdf)
			}

			var ins *cadoc.Insert
			for _, e := range ms.Entities {
				if i, ok := e.(*cadoc.Insert); ok {
					ins = i
				}
			}
			filt := ins.SpatialFilter()
			if filt == nil || len(filt.BoundaryPoints) != 2 || filt.BoundaryPoints[1] != (cadoc.Vec2{X: 5, Y: 5}) || !filt.ClipFrontPlane ||
				filt.FrontDistance != 2 || filt.InsertTransform[3] != 9 || filt.InverseInsertTransform[3] != -9 || filt.InsertTransform[0] != 1 {
				t.Fatalf("spatial filter: %+v", filt)
			}
			ctx, _ := ins.XDictionary.GetDictionary("AcDbContextDataManager")
			ann, _ := ctx.GetDictionary("ACDB_ANNOTATIONSCALES")
			brc := dictObj(t, ann, "*A1").(*cadoc.BlockReferenceObjectContextData)
			if brc.InsertionPoint.X != 4 || !objNear(brc.Rotation, 0.5) || brc.XScale != 2 || brc.Scale == nil || brc.Scale.Name != "1:1" || !brc.HasFileToExtensionDictionary || brc.Version != 3 {
				t.Fatalf("blkref context: %+v", brc)
			}
			mtc := dictObj(t, ann, "*A2").(*cadoc.MTextAttributeObjectContextData)
			if mtc.AttachmentPoint != 5 || mtc.InsertPoint.X != 1 || !objNear(mtc.Rotation, 0.25) || !mtc.Value290 || mtc.Scale == nil || mtc.Scale.Name != "1:3" {
				t.Fatalf("mtext context: %+v", mtc)
			}

			plot := dictObj(t, back.RootDictionary, cadoc.AcadPlotStyleName).(*cadoc.DictionaryWithDefault)
			ph := dictObj(t, &plot.Dictionary, "Normal")
			if _, ok := ph.(*cadoc.AcdbPlaceHolder); !ok || plot.DefaultEntry != cadoc.Object(ph) {
				t.Fatalf("plot style name: %+v", plot)
			}
			if dictObj(t, back.RootDictionary, "PURGE").(*cadoc.DynamicBlockPurgePreventer).Version != 1 {
				t.Fatal("purge preventer")
			}

			fl := dictObj(t, back.RootDictionary, cadoc.AcadFieldListDictionary).(*cadoc.FieldList)
			if len(fl.Fields) != 1 {
				t.Fatalf("fieldlist: %+v", fl)
			}
			fld := fl.Fields[0]
			if fld.EvaluatorId != "AcVar" || fld.FieldCode != "%<\\AcVar Filename>%"+strings.Repeat("x", 300) || fld.FormatString != "%fn2" ||
				fld.Value.Value != "drawing.dwg" || fld.Value.ValueType != cadoc.CadValueTypeString || len(fld.Children) != 1 ||
				fld.Children[0].Value.Value != 1.5 || len(fld.CadObjects) != 1 || fld.CadObjects[0] != cadoc.Object(l1) ||
				fld.EvaluationErrorMessage != "none" || len(fld.Values) != 3 || fld.Values[0].Value.Value != int32(42) ||
				fld.Values[1].Value.Value != (cadoc.Vec3{X: 1, Y: 2, Z: 3}) || fld.Values[2].Value.Value != cadoc.Object(l2) {
				t.Fatalf("field: %+v %+v", fld, fld.Values)
			}
		})
	}
}

// TestXRecordDanglingHandle: a handle entry whose object is gone is left out.
func TestXRecordDanglingHandle(t *testing.T) {
	f := newObjectFixture()
	xd := f.doc.ModelSpace().XDictionary
	o, _ := xd.Get("MYXREC")
	f.doc.ModelSpace().RemoveEntity(f.line2)
	back, _ := roundTrip(t, f.doc, WriteConfig{})
	_ = o
	xr := dictObj(t, back.ModelSpace().XDictionary, "MYXREC").(*cadoc.XRecord)
	if len(xr.Entries) != 6 {
		t.Fatalf("entries %d", len(xr.Entries))
	}
}

const materialDXF = `  0
SECTION
  2
OBJECTS
  0
DICTIONARY
  5
C
330
0
100
AcDbDictionary
  3
ACAD_MATERIAL
350
D
  3
ACAD_VISUALSTYLE
350
F
  0
DICTIONARY
  5
D
330
C
100
AcDbDictionary
  3
Global
350
E
  0
MATERIAL
  5
E
330
D
100
AcDbMaterial
  1
Global
  2
global material
 70
0
 40
1.0
  0
DICTIONARY
  5
F
330
C
100
AcDbDictionary
  3
Wireframe
350
10
  0
VISUALSTYLE
  5
10
330
F
100
AcDbVisualStyle
  2
Wireframe
 70
4
 71
1
  0
ENDSEC
  0
EOF
`

func TestReadMaterialVisualStyle(t *testing.T) {
	doc, err := ReadBytes([]byte(materialDXF), ReadConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := dictObj(t, doc.NamedDictionary(cadoc.AcadMaterialDictionary, false), "Global").(*cadoc.Material)
	if m.Description != "global material" || len(m.Raw) != 2 || m.Raw[0].Code != 70 || m.Raw[1].Value != 1.0 {
		t.Fatalf("material: %+v", m)
	}
	v := dictObj(t, doc.NamedDictionary(cadoc.AcadVisualStyle, false), "Wireframe").(*cadoc.VisualStyle)
	if v.Description != "Wireframe" || v.Type != 4 || len(v.Raw) != 1 {
		t.Fatalf("visual style: %+v", v)
	}
	// Written back, they are left out without dangling references.
	if _, data := roundTrip(t, doc, WriteConfig{}); strings.Contains(string(data), "  0\r\nVISUALSTYLE\r\n") || strings.Contains(string(data), "  0\r\nMATERIAL\r\n") {
		t.Fatal("visual style written")
	}
}
