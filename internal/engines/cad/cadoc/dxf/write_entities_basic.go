package dxf

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func init() {
	registerEntityWriter(&cadoc.Line{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Line)
		o := w.o
		o.str(100, cadoc.SubclassLine)
		o.vec3(10, x.StartPoint)
		o.vec3(11, x.EndPoint)
		o.dbl(39, x.Thickness)
		o.vec3(210, x.Normal)
	})
	registerEntityWriter(&cadoc.Circle{}, func(w *writer, e cadoc.Entity) { w.writeCircle(e.(*cadoc.Circle)) })
	registerEntityWriter(&cadoc.Arc{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Arc)
		w.writeCircle(&x.Circle)
		w.o.str(100, cadoc.SubclassArc)
		w.o.angle(50, x.StartAngle)
		w.o.angle(51, x.EndAngle)
	})
	registerEntityWriter(&cadoc.Ellipse{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Ellipse)
		o := w.o
		o.str(100, cadoc.SubclassEllipse)
		o.vec3(10, x.Center)
		o.vec3(11, x.MajorAxisEndPoint)
		o.vec3(210, x.Normal)
		o.dbl(40, x.RadiusRatio)
		o.dbl(41, x.StartParameter)
		o.dbl(42, x.EndParameter)
	})
	registerEntityWriter(&cadoc.Point{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Point)
		o := w.o
		o.str(100, cadoc.SubclassPoint)
		o.vec3(10, x.Location)
		o.dbl(39, x.Thickness)
		o.vec3(210, x.Normal)
		o.angle(50, x.Rotation)
	})
	registerEntityWriter(&cadoc.XLine{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.XLine)
		w.o.str(100, cadoc.SubclassXLine)
		w.o.vec3(10, x.FirstPoint)
		w.o.vec3(11, x.Direction)
	})
	registerEntityWriter(&cadoc.Ray{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Ray)
		w.o.str(100, cadoc.SubclassRay)
		w.o.vec3(10, x.StartPoint)
		w.o.vec3(11, x.Direction)
	})
	registerEntityWriter(&cadoc.Solid{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Solid)
		o := w.o
		o.str(100, cadoc.SubclassSolid)
		o.vec3(10, x.FirstCorner)
		o.vec3(11, x.SecondCorner)
		o.vec3(12, x.ThirdCorner)
		o.vec3(13, x.FourthCorner)
		o.dbl(39, x.Thickness)
		o.vec3(210, x.Normal)
	})
	registerEntityWriter(&cadoc.Face3D{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Face3D)
		o := w.o
		o.str(100, cadoc.SubclassFace3d)
		o.vec3(10, x.FirstCorner)
		o.vec3(11, x.SecondCorner)
		o.vec3(12, x.ThirdCorner)
		o.vec3(13, x.FourthCorner)
		o.integer(70, int64(x.Flags))
	})
	registerEntityWriter(&cadoc.Shape{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Shape)
		o := w.o
		o.str(100, cadoc.SubclassShape)
		o.dbl(39, x.Thickness)
		o.vec3(10, x.InsertionPoint)
		o.dbl(40, x.Size)
		o.str(2, x.ShapeName)
		o.angle(50, x.Rotation)
		o.dbl(41, x.RelativeXScale)
		o.angle(51, x.ObliqueAngle)
		o.vec3(210, x.Normal)
	})
	registerEntityWriter(&cadoc.Tolerance{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Tolerance)
		o := w.o
		o.str(100, cadoc.SubclassTolerance)
		o.str(3, dimStyleName(x.Style))
		o.vec3(10, x.InsertionPoint)
		o.str(1, x.Text)
		o.vec3(210, x.Normal)
		o.vec3(11, x.Direction)
	})
	registerEntityWriter(&cadoc.TextEntity{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.TextEntity)
		w.writeText(x, true)
	})
	registerEntityWriter(&cadoc.AttributeEntity{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.AttributeEntity)
		w.writeText(&x.TextEntity, false)
		w.o.str(100, cadoc.SubclassAttribute)
		w.writeAttributeBase(&x.AttributeBase)
	})
	registerEntityWriter(&cadoc.AttributeDefinition{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.AttributeDefinition)
		w.writeText(&x.TextEntity, false)
		w.o.str(100, cadoc.SubclassAttributeDefinition)
		w.o.str(3, x.Prompt)
		w.writeAttributeBase(&x.AttributeBase)
	})
	registerEntityWriter(&cadoc.MText{}, func(w *writer, e cadoc.Entity) { w.writeMText(e.(*cadoc.MText), true) })
	registerEntityWriter(&cadoc.Insert{}, func(w *writer, e cadoc.Entity) { w.writeInsert(e.(*cadoc.Insert)) })
	registerEntityWriter(&cadoc.LwPolyline{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.LwPolyline)
		o := w.o
		o.str(100, cadoc.SubclassLwPolyline)
		o.integer(90, int64(len(x.Vertices)))
		o.integer(70, int64(x.Flags))
		if x.ConstantWidth != 0 {
			o.dbl(43, x.ConstantWidth)
		}
		o.dbl(38, x.Elevation)
		o.dbl(39, x.Thickness)
		for _, v := range x.Vertices {
			o.vec2(10, v.Location)
			o.dbl(40, v.StartWidth)
			o.dbl(41, v.EndWidth)
			o.dbl(42, v.Bulge)
		}
		o.vec3(210, x.Normal)
	})
	poly := func(w *writer, e cadoc.Entity) { w.writePolyline(e.(cadoc.Polyline)) }
	registerEntityWriter(&cadoc.Polyline2D{}, poly)
	registerEntityWriter(&cadoc.Polyline3D{}, poly)
	registerEntityWriter(&cadoc.PolyfaceMesh{}, poly)
	registerEntityWriter(&cadoc.PolygonMesh{}, poly)
	vertex := func(w *writer, e cadoc.Entity) { w.writeVertex(e.(cadoc.Vertex)) }
	registerEntityWriter(&cadoc.Vertex2D{}, vertex)
	registerEntityWriter(&cadoc.Vertex3D{}, vertex)
	registerEntityWriter(&cadoc.VertexFaceMesh{}, vertex)
	registerEntityWriter(&cadoc.VertexFaceRecord{}, vertex)
	registerEntityWriter(&cadoc.PolygonMeshVertex{}, vertex)
	registerEntityWriter(&cadoc.UnknownEntity{}, func(w *writer, e cadoc.Entity) {
		for _, g := range e.(*cadoc.UnknownEntity).Groups {
			w.o.any(g.Code, g.Value)
		}
	})
}

func dimStyleName(s *cadoc.DimensionStyle) string {
	if s == nil {
		return cadoc.DefaultDimStyleName
	}
	return s.Name
}

func textStyleName(s *cadoc.TextStyle) string {
	if s == nil {
		return cadoc.DefaultTextStyleName
	}
	return s.Name
}

func (w *writer) writeCircle(x *cadoc.Circle) {
	o := w.o
	o.str(100, cadoc.SubclassCircle)
	o.vec3(10, x.Center)
	o.dbl(39, x.Thickness)
	o.dbl(40, x.Radius)
	o.vec3(210, x.Normal)
}

// writeText writes the AcDbText part of TEXT, ATTRIB and ATTDEF.
func (w *writer) writeText(t *cadoc.TextEntity, isText bool) {
	o := w.o
	o.str(100, cadoc.SubclassText)
	o.dbl(39, t.Thickness)
	o.vec3(10, t.InsertPoint)
	o.dbl(40, t.Height)
	o.str(1, t.Value)
	if t.Rotation != 0 {
		o.angle(50, t.Rotation)
	}
	if t.WidthFactor != 1 {
		o.dbl(41, t.WidthFactor)
	}
	if t.ObliqueAngle != 0 {
		o.angle(51, t.ObliqueAngle)
	}
	o.str(7, textStyleName(t.Style))
	if t.Mirror != 0 {
		o.integer(71, int64(t.Mirror))
	}
	if t.HorizontalAlignment != 0 {
		o.integer(72, int64(t.HorizontalAlignment))
	}
	o.vec3(11, t.AlignmentPoint)
	o.vec3(210, t.Normal)
	if isText {
		o.str(100, cadoc.SubclassText)
		if t.VerticalAlignment != 0 {
			o.integer(73, int64(t.VerticalAlignment))
		}
	}
}

func (w *writer) writeAttributeBase(a *cadoc.AttributeBase) {
	o := w.o
	if w.version >= cadoc.AC1027 {
		o.integer(280, int64(a.Version))
	}
	o.str(2, a.Tag)
	o.integer(70, int64(a.Flags))
	o.integer(73, 0)
	if a.VerticalAlignment != 0 {
		o.integer(74, int64(a.VerticalAlignment))
	}
	if w.version > cadoc.AC1027 && a.AttributeType != cadoc.AttributeTypeSingleLine {
		o.integer(71, int64(a.AttributeType))
		o.integer(72, 0)
		o.vec3(11, a.AlignmentPoint)
		if a.MText != nil {
			o.str(101, "Embedded Object")
			w.writeMText(a.MText, false)
		}
	}
}

func (w *writer) writeMText(m *cadoc.MText, subclass bool) {
	o := w.o
	if subclass {
		o.str(100, cadoc.SubclassMText)
	}
	o.vec3(10, m.InsertPoint)
	o.dbl(40, m.Height)
	o.dbl(41, m.RectangleWidth)
	if subclass {
		o.dbl(44, m.LineSpacing)
	}
	if w.version >= cadoc.AC1021 {
		o.dbl(46, m.RectangleHeight)
	}
	o.integer(71, int64(m.AttachmentPoint))
	o.integer(72, int64(m.DrawingDirection))
	w.longText(1, 3, m.Value)
	o.str(7, textStyleName(m.Style))
	o.vec3(11, m.AlignmentPoint)
	o.vec3(210, m.Normal)
	o.integer(73, int64(m.LineSpacingStyle))
	if !subclass {
		o.dbl(44, m.LineSpacing)
	}
	if w.version >= cadoc.AC1018 && m.BackgroundFillFlags != cadoc.BackgroundFillFlagsNone {
		o.integer(90, int64(m.BackgroundFillFlags))
		if m.BackgroundFillFlags&(cadoc.BackgroundFillFlagsUseBackgroundFillColor|cadoc.BackgroundFillFlagsTextFrame) != 0 {
			o.integer(63, int64(m.BackgroundColor.ApproxIndex()))
			if m.BackgroundColor.IsTrueColor() {
				o.integer(421, int64(m.BackgroundColor.TrueColor()))
			}
			o.dbl(45, m.BackgroundScale)
			o.integer(441, int64(m.BackgroundTransparency.AlphaValue()))
		}
	}
	if !m.HasColumns() || w.version < cadoc.AC1032 {
		return
	}
	cd := &m.ColumnData
	o.str(101, "Embedded Object")
	o.integer(70, 1)
	o.vec3(10, m.AlignmentPoint)
	o.vec3(11, m.InsertPoint)
	o.dbl(40, m.RectangleWidth)
	o.dbl(41, m.RectangleHeight)
	o.dbl(42, m.HorizontalWidth)
	o.dbl(43, m.VerticalHeight)
	o.integer(71, int64(cd.ColumnType))
	o.integer(72, int64(cd.ColumnCount))
	o.dbl(44, cd.Width)
	o.dbl(45, cd.Gutter)
	o.boolean(73, cd.AutoHeight)
	o.boolean(74, cd.FlowReversed)
	for _, h := range cd.Heights {
		o.dbl(46, h)
	}
}

func (w *writer) writeInsert(x *cadoc.Insert) {
	o := w.o
	o.str(100, x.SubclassMarker())
	if len(x.Attributes) > 0 {
		o.integer(66, 1)
	}
	name := ""
	if x.Block != nil {
		name = x.Block.Name
	}
	o.str(2, name)
	o.vec3(10, x.InsertPoint)
	o.dbl(41, x.XScale)
	o.dbl(42, x.YScale)
	o.dbl(43, x.ZScale)
	o.angle(50, x.Rotation)
	o.integer(70, int64(max(x.ColumnCount, 1)))
	o.integer(71, int64(max(x.RowCount, 1)))
	o.dbl(44, x.ColumnSpacing)
	o.dbl(45, x.RowSpacing)
	o.vec3(210, x.Normal)
	if len(x.Attributes) == 0 {
		return
	}
	// The insert's xdata comes before its attributes.
	w.xdata(x)
	saved := x.ExtendedData
	x.ExtendedData = nil
	defer func() { x.ExtendedData = saved }()
	for _, a := range x.Attributes {
		w.writeEntityData(a)
	}
	w.writeSeqend(x.AttributesSeqend, x)
}

func (w *writer) writePolyline(p cadoc.Polyline) {
	o := w.o
	b := p.Polyline()
	flags := b.Flags
	switch p.(type) {
	case *cadoc.Polyline3D:
		flags |= cadoc.PolylineFlagsPolyline3D
	case *cadoc.PolyfaceMesh:
		flags |= cadoc.PolylineFlagsPolyfaceMesh
	case *cadoc.PolygonMesh:
		flags |= cadoc.PolylineFlagsPolygonMesh
	}
	o.str(100, p.SubclassMarker())
	o.integer(66, 1)
	o.dbl(10, 0)
	o.dbl(20, 0)
	o.dbl(30, b.Elevation)
	if b.Thickness != 0 {
		o.dbl(39, b.Thickness)
	}
	o.integer(70, int64(flags))
	if b.StartWidth != 0 {
		o.dbl(40, b.StartWidth)
	}
	if b.EndWidth != 0 {
		o.dbl(41, b.EndWidth)
	}
	switch x := p.(type) {
	case *cadoc.PolygonMesh:
		o.integer(71, int64(x.MVertexCount))
		o.integer(72, int64(x.NVertexCount))
		o.integer(73, int64(x.MSmoothSurfaceDensity))
		o.integer(74, int64(x.NSmoothSurfaceDensity))
	case *cadoc.PolyfaceMesh:
		o.integer(71, int64(len(x.Vertices)))
		o.integer(72, int64(len(x.Faces)))
	}
	o.integer(75, int64(b.SmoothSurface))
	o.vec3(210, b.Normal)
	vs := p.VertexEntities()
	if len(vs) == 0 {
		return
	}
	w.xdata(p)
	saved := b.ExtendedData
	b.ExtendedData = nil
	defer func() { b.ExtendedData = saved }()
	for _, v := range vs {
		w.writeEntityData(v)
	}
	w.writeSeqend(b.Seqend, p)
}

func (w *writer) writeVertex(v cadoc.Vertex) {
	o := w.o
	b := v.Vertex()
	flags := b.Flags
	switch v.(type) {
	case *cadoc.Vertex3D:
		flags |= cadoc.VertexFlagsPolylineVertex3D
	case *cadoc.VertexFaceMesh:
		flags |= cadoc.VertexFlagsPolygonMesh3D | cadoc.VertexFlagsPolyFaceMeshVertex
	case *cadoc.VertexFaceRecord:
		flags |= cadoc.VertexFlagsPolyFaceMeshVertex
	case *cadoc.PolygonMeshVertex:
		flags |= cadoc.VertexFlagsPolygonMesh3D
	}
	if _, face := v.(*cadoc.VertexFaceRecord); !face {
		o.str(100, cadoc.SubclassVertex)
	}
	o.str(100, v.SubclassMarker())
	o.vec3(10, b.Location)
	if b.StartWidth != 0 {
		o.dbl(40, b.StartWidth)
	}
	if b.EndWidth != 0 {
		o.dbl(41, b.EndWidth)
	}
	if b.Bulge != 0 {
		o.dbl(42, b.Bulge)
	}
	o.integer(70, int64(flags))
	o.angle(50, b.CurveTangent)
	if f, ok := v.(*cadoc.VertexFaceRecord); ok {
		o.integer(71, int64(f.Index1))
		o.integer(72, int64(f.Index2))
		o.integer(73, int64(f.Index3))
		o.integer(74, int64(f.Index4))
	}
}
