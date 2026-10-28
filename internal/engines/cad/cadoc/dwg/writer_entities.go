package dwg

import (
	"encoding/binary"
	"encoding/hex"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// writeEntity is DwgObjectWriter.writeEntity: the common data, the type's
// data, then the attributes or vertices with their SEQEND.
func (w *wObjectWriter) writeEntity(e cadoc.Entity) {
	var children []cadoc.Entity
	var seqend *cadoc.Seqend

	if !w.writeCommonEntityData(e) {
		return
	}
	switch x := e.(type) {
	case *cadoc.Arc:
		w.writeCircle(&x.Circle)
		w.sw.writeBitDouble(x.StartAngle)
		w.sw.writeBitDouble(x.EndAngle)
	case *cadoc.Circle:
		w.writeCircle(x)
	case cadoc.Dimension:
		w.writeDimension(x)
	case *cadoc.Ellipse:
		w.sw.write3BitDouble(x.Center)
		w.sw.write3BitDouble(x.MajorAxisEndPoint)
		w.sw.write3BitDouble(x.Normal)
		w.sw.writeBitDouble(x.RadiusRatio)
		w.sw.writeBitDouble(x.StartParameter)
		w.sw.writeBitDouble(x.EndParameter)
	case *cadoc.Insert:
		w.writeInsert(x)
		for _, a := range x.Attributes {
			children = append(children, a)
		}
		seqend = x.AttributesSeqend
	case *cadoc.Face3D:
		w.writeFace3D(x)
	case *cadoc.Hatch:
		w.writeHatch(x)
	case *cadoc.Leader:
		w.writeLeader(x)
	case *cadoc.Line:
		w.writeLine(x)
	case *cadoc.LwPolyline:
		w.writeLwPolyline(x)
	case *cadoc.Mesh:
		w.writeMesh(x)
	case *cadoc.MLine:
		w.writeMLine(x)
	case *cadoc.MText:
		w.writeMText(x)
	case *cadoc.MultiLeader:
		w.writeMultiLeader(x)
	case *cadoc.Ole2Frame:
		w.writeOle2Frame(x)
	case *cadoc.PdfUnderlay:
		w.writePdfUnderlay(x)
	case *cadoc.Point:
		w.sw.write3BitDouble(x.Location)
		w.sw.writeBitThickness(x.Thickness)
		w.sw.writeBitExtrusion(x.Normal)
		w.sw.writeBitDouble(x.Rotation)
	case *cadoc.PolyfaceMesh:
		w.writePolyfaceMesh(x)
		// Vertices before faces, as the owned handles list them (ACadSharp
		// writes the faces first, which breaks the R13-R2000 entity chain).
		for _, v := range x.Vertices {
			children = append(children, v)
		}
		for _, f := range x.Faces {
			children = append(children, f)
		}
		seqend = x.Seqend
	case *cadoc.Polyline2D:
		w.writePolyline2D(x)
		for _, v := range x.Vertices {
			children = append(children, v)
		}
		seqend = x.Seqend
	case *cadoc.Polyline3D:
		w.writePolyline3D(x)
		for _, v := range x.Vertices {
			children = append(children, v)
		}
		seqend = x.Seqend
	case *cadoc.PolygonMesh:
		w.writePolygonMesh(x)
		for _, v := range x.Vertices {
			children = append(children, v)
		}
		seqend = x.Seqend
	case *cadoc.ProxyEntity:
		w.writeProxyEntity(x)
	case *cadoc.Ray:
		w.sw.write3BitDouble(x.StartPoint)
		w.sw.write3BitDouble(x.Direction)
	case *cadoc.Shape:
		w.writeShape(x)
	case *cadoc.Solid:
		w.writeSolid(x)
	case *cadoc.Solid3D:
		w.writeModelerGeometry(&x.ModelerGeometry, x.ObjectName())
		if w.r2007 {
			// History ID.
			w.sw.handleReference(wRefHardOwnership, 0)
		}
	case *cadoc.Spline:
		w.writeSpline(x)
	case *cadoc.Region:
		w.writeModelerGeometry(&x.ModelerGeometry, x.ObjectName())
	case *cadoc.CadBody:
		w.writeModelerGeometry(&x.ModelerGeometry, x.ObjectName())
	case *cadoc.RasterImage:
		w.writeCadImage(&x.WipeoutBase)
	case *cadoc.Wipeout:
		w.writeCadImage(&x.WipeoutBase)
	case *cadoc.AttributeEntity:
		w.writeCommonAttData(&x.AttributeBase)
	case *cadoc.AttributeDefinition:
		w.writeCommonAttData(&x.AttributeBase)
		if w.r2010 {
			w.sw.writeByte(x.Version)
		}
		w.sw.writeVariableText(x.Prompt)
	case *cadoc.TextEntity:
		w.writeTextEntity(x)
	case *cadoc.Tolerance:
		w.writeTolerance(x)
	case *cadoc.Vertex2D:
		w.writeVertex2D(x)
	case *cadoc.VertexFaceRecord:
		w.sw.writeBitShort(x.Index1)
		w.sw.writeBitShort(x.Index2)
		w.sw.writeBitShort(x.Index3)
		w.sw.writeBitShort(x.Index4)
	case *cadoc.Vertex3D:
		w.writeVertex(&x.VertexBase)
	case *cadoc.VertexFaceMesh:
		w.writeVertex(&x.VertexBase)
	case *cadoc.PolygonMeshVertex:
		w.writeVertex(&x.VertexBase)
	case *cadoc.Viewport:
		w.writeViewport(x)
	case *cadoc.XLine:
		w.sw.write3BitDouble(x.FirstPoint)
		w.sw.write3BitDouble(x.Direction)
	case *cadoc.Seqend:
		// A stray SEQEND has only the common data.
	default:
		w.note(writerLevelNotImplemented, "Entity not implemented : %s", e.ObjectName())
		return
	}
	w.registerObject(e)
	w.writeChildEntities(children, seqend)
}

// writeChildEntities writes attributes or vertices linked among themselves,
// then the SEQEND.
func (w *wObjectWriter) writeChildEntities(children []cadoc.Entity, seqend *cadoc.Seqend) {
	if len(children) == 0 {
		return
	}
	prevHolder, nextHolder := w.prev, w.next
	w.prev, w.next = nil, nil
	for i, c := range children {
		w.next = nil
		if i+1 < len(children) {
			w.next = children[i+1]
		}
		w.writeEntity(c)
		w.prev = c
	}
	w.prev, w.next = prevHolder, nextHolder
	if seqend != nil {
		w.writeSeqend(seqend)
	}
}

// writeSeqend writes a SEQEND without entity links (they break R2000-).
func (w *wObjectWriter) writeSeqend(s *cadoc.Seqend) {
	prevHolder, nextHolder := w.prev, w.next
	w.prev, w.next = nil, nil
	if w.writeCommonEntityData(s) {
		w.registerObject(s)
	}
	w.prev, w.next = prevHolder, nextHolder
}

func (w *wObjectWriter) writeCircle(c *cadoc.Circle) {
	w.sw.write3BitDouble(c.Center)
	w.sw.writeBitDouble(c.Radius)
	w.sw.writeBitThickness(c.Thickness)
	w.sw.writeBitExtrusion(c.Normal)
}

func (w *wObjectWriter) textStyleOrStandard(s *cadoc.TextStyle) uint64 {
	if s != nil {
		return w.handleOf(s)
	}
	return w.handleOf(w.textStyleByName(cadoc.DefaultTextStyleName))
}

func (w *wObjectWriter) dimStyleOrStandard(s *cadoc.DimensionStyle) *cadoc.DimensionStyle {
	if s != nil {
		return s
	}
	if d, ok := w.doc.DimensionStyles.Get(cadoc.DefaultDimStyleName); ok {
		return d
	}
	return nil
}

func (w *wObjectWriter) writeCommonAttData(a *cadoc.AttributeBase) {
	w.writeTextEntity(&a.TextEntity)
	if w.r2010 {
		w.sw.writeByte(a.Version)
	}
	if w.r2018 {
		if a.MText == nil {
			w.sw.writeByte(byte(cadoc.AttributeTypeSingleLine))
		} else {
			w.sw.writeByte(byte(a.AttributeType))
			if a.AttributeType == cadoc.AttributeTypeMultiLine || a.AttributeType == cadoc.AttributeTypeConstantMultiLine {
				w.writeEntityMode(a.MText)
				w.writeMText(a.MText)
				// Annotative data.
				w.sw.writeBitShort(0)
			}
		}
	}
	w.sw.writeVariableText(a.Tag)
	// Field length (unused).
	w.sw.writeBitShort(0)
	w.sw.writeByte(byte(a.Flags))
	if w.r2007 {
		w.sw.writeBit(a.IsLocked)
	}
}

func (w *wObjectWriter) writeDimension(d cadoc.Dimension) {
	b := d.Dimension()
	s := w.sw
	if w.r2010 {
		s.writeByte(b.Version)
	}
	s.write3BitDouble(b.Normal)
	s.write2RawDouble(b.TextMiddlePoint.XY())
	s.writeBitDouble(b.InsertionPoint.Z)
	var flags byte
	if !b.IsTextUserDefinedLocation() {
		flags = 1
	}
	s.writeByte(flags)
	s.writeVariableText(b.Text)
	s.writeBitDouble(b.TextRotation)
	s.writeBitDouble(b.HorizontalDirection)
	// Insertion scale and rotation of the anonymous block.
	s.write3BitDouble(cadoc.Vec3{X: 1, Y: 1, Z: 1})
	s.writeBitDouble(0)
	if w.r2000 {
		s.writeBitShort(int16(b.AttachmentPoint))
		s.writeBitShort(int16(b.LineSpacingStyle))
		s.writeBitDouble(b.LineSpacingFactor)
		// The actual measurement, recomputed like ACadSharp's Measurement.
		m, err := d.Measurement()
		if err != nil {
			m = b.MeasurementValue
		}
		s.writeBitDouble(m)
	}
	if w.r2007 {
		s.writeBit(false)
		s.writeBit(b.FlipArrow1)
		s.writeBit(b.FlipArrow2)
	}
	s.write2RawDouble(b.InsertionPoint.XY())
	s.handleReference(wRefHardPointer, w.handleOf(w.dimStyleOrStandard(b.Style)))
	s.handleReference(wRefHardPointer, w.handleOf(b.Block))

	switch x := d.(type) {
	case *cadoc.DimensionLinear:
		w.writeDimensionAligned(&x.DimensionAligned)
		s.writeBitDouble(x.Rotation)
	case *cadoc.DimensionAligned:
		w.writeDimensionAligned(x)
	case *cadoc.DimensionRadius:
		s.write3BitDouble(x.DefinitionPoint)
		s.write3BitDouble(x.AngleVertex)
		s.writeBitDouble(x.LeaderLength)
	case *cadoc.DimensionAngular2Line:
		s.write2RawDouble(x.DimensionArc.XY())
		s.write3BitDouble(x.FirstPoint)
		s.write3BitDouble(x.SecondPoint)
		s.write3BitDouble(x.AngleVertex)
		s.write3BitDouble(x.DefinitionPoint)
	case *cadoc.DimensionAngular3Pt:
		s.write3BitDouble(x.DefinitionPoint)
		s.write3BitDouble(x.FirstPoint)
		s.write3BitDouble(x.SecondPoint)
		s.write3BitDouble(x.AngleVertex)
	case *cadoc.DimensionArc:
		s.write3BitDouble(x.DefinitionPoint)
		s.write3BitDouble(x.FirstPoint)
		s.write3BitDouble(x.SecondPoint)
		s.write3BitDouble(x.Center)
		s.writeBit(x.IsPartial)
		s.writeBitDouble(x.StartAngle)
		s.writeBitDouble(x.EndAngle)
		s.writeBit(x.HasLeader)
		s.write3BitDouble(x.LeaderPoint1)
		s.write3BitDouble(x.LeaderPoint2)
	case *cadoc.DimensionDiameter:
		s.write3BitDouble(x.AngleVertex)
		s.write3BitDouble(x.DefinitionPoint)
		s.writeBitDouble(x.LeaderLength)
	case *cadoc.DimensionOrdinate:
		s.write3BitDouble(x.DefinitionPoint)
		s.write3BitDouble(x.FeatureLocation)
		s.write3BitDouble(x.LeaderEndpoint)
		var f byte
		if x.IsOrdinateTypeX() {
			f = 1
		}
		s.writeByte(f)
	}
}

func (w *wObjectWriter) writeDimensionAligned(d *cadoc.DimensionAligned) {
	w.sw.write3BitDouble(d.FirstPoint)
	w.sw.write3BitDouble(d.SecondPoint)
	w.sw.write3BitDouble(d.DefinitionPoint)
	w.sw.writeBitDouble(d.ExtLineRotation)
}

func (w *wObjectWriter) writeInsert(ins *cadoc.Insert) {
	s := w.sw
	s.write3BitDouble(ins.InsertPoint)
	if w.r13_14 {
		s.writeBitDouble(ins.XScale)
		s.writeBitDouble(ins.YScale)
		s.writeBitDouble(ins.ZScale)
	}
	if w.r2000 {
		switch {
		case ins.XScale == 1 && ins.YScale == 1 && ins.ZScale == 1:
			s.write2Bits(3)
		case ins.XScale == ins.YScale && ins.XScale == ins.ZScale:
			s.write2Bits(2)
			s.writeRawDouble(ins.XScale)
		case ins.XScale == 1:
			s.write2Bits(1)
			s.writeBitDoubleWithDefault(ins.YScale, 1)
			s.writeBitDoubleWithDefault(ins.ZScale, 1)
		default:
			s.write2Bits(0)
			s.writeRawDouble(ins.XScale)
			s.writeBitDoubleWithDefault(ins.YScale, ins.XScale)
			s.writeBitDoubleWithDefault(ins.ZScale, ins.XScale)
		}
	}
	s.writeBitDouble(ins.Rotation)
	s.write3BitDouble(ins.Normal)
	hasAtts := ins.HasAttributes()
	s.writeBit(hasAtts)
	if w.r2004 && hasAtts {
		s.writeBitLong(int32(len(ins.Attributes)))
	}
	if ins.IsMultiple() {
		s.writeBitShort(int16(ins.ColumnCount))
		s.writeBitShort(int16(ins.RowCount))
		s.writeBitDouble(ins.ColumnSpacing)
		s.writeBitDouble(ins.RowSpacing)
	}
	s.handleReference(wRefHardPointer, w.handleOf(ins.Block))
	if !hasAtts {
		return
	}
	if w.r13_15 {
		s.handleReference(wRefSoftPointer, ins.Attributes[0].Handle)
		s.handleReference(wRefSoftPointer, ins.Attributes[len(ins.Attributes)-1].Handle)
	} else {
		for _, a := range ins.Attributes {
			s.handleReference(wRefHardOwnership, a.Handle)
		}
	}
	s.handleReference(wRefHardOwnership, w.objHandle(ins.AttributesSeqend))
}

func (w *wObjectWriter) writeFace3D(f *cadoc.Face3D) {
	s := w.sw
	if w.r13_14 {
		s.write3BitDouble(f.FirstCorner)
		s.write3BitDouble(f.SecondCorner)
		s.write3BitDouble(f.ThirdCorner)
		s.write3BitDouble(f.FourthCorner)
		s.writeBitShort(int16(f.Flags))
		return
	}
	noFlags := f.Flags == 0
	s.writeBit(noFlags)
	zIsZero := f.FirstCorner.Z == 0
	s.writeBit(zIsZero)
	s.writeRawDouble(f.FirstCorner.X)
	s.writeRawDouble(f.FirstCorner.Y)
	if !zIsZero {
		s.writeRawDouble(f.FirstCorner.Z)
	}
	s.write3BitDoubleWithDefault(f.SecondCorner, f.FirstCorner)
	s.write3BitDoubleWithDefault(f.ThirdCorner, f.SecondCorner)
	s.write3BitDoubleWithDefault(f.FourthCorner, f.ThirdCorner)
	if !noFlags {
		s.writeBitShort(int16(f.Flags))
	}
}

func (w *wObjectWriter) writeHatch(h *cadoc.Hatch) {
	s := w.sw
	if w.r2004 {
		g := h.GradientColor
		if g == nil {
			g = &cadoc.HatchGradientPattern{}
		}
		s.writeBitLong(int32(wB2S(g.Enabled)))
		s.writeBitLong(g.Reserved)
		s.writeBitDouble(g.Angle)
		s.writeBitDouble(g.Shift)
		s.writeBitLong(int32(wB2S(g.IsSingleColorGradient)))
		s.writeBitDouble(g.ColorTint)
		s.writeBitLong(int32(len(g.Colors)))
		for _, c := range g.Colors {
			s.writeBitDouble(c.Value)
			s.writeCmColor(c.Color, false)
		}
		s.writeVariableText(g.Name)
	}
	s.writeBitDouble(h.Elevation)
	s.write3BitDouble(h.Normal)
	pattern := h.Pattern
	if pattern == nil {
		pattern = &cadoc.HatchPattern{}
	}
	s.writeVariableText(pattern.Name)
	s.writeBit(h.IsSolid)
	s.writeBit(h.IsAssociative)
	s.writeBitLong(int32(len(h.Paths)))
	derived := false
	for _, p := range h.Paths {
		flags := p.EffectiveFlags()
		s.writeBitLong(int32(flags))
		if flags&cadoc.BoundaryPathFlagsDerived != 0 {
			derived = true
		}
		if flags&cadoc.BoundaryPathFlagsPolyline != 0 {
			pl := p.Edges[0].(*cadoc.HatchPolylineEdge)
			bulge := pl.HasBulge()
			s.writeBit(bulge)
			s.writeBit(pl.IsClosed)
			s.writeBitLong(int32(len(pl.Vertices)))
			for _, v := range pl.Vertices {
				s.write2RawDouble(v.XY())
				if bulge {
					s.writeBitDouble(v.Z)
				}
			}
		} else {
			// A polyline edge can only be a path of its own (ACadSharp throws
			// for one mixed with other edges): those are left out.
			edges := make([]cadoc.HatchEdge, 0, len(p.Edges))
			for _, e := range p.Edges {
				if _, ok := e.(*cadoc.HatchPolylineEdge); ok || e == nil {
					w.note(writerLevelWarning, "Hatch %X: polyline edge mixed with other edges left out", h.Handle)
					continue
				}
				edges = append(edges, e)
			}
			s.writeBitLong(int32(len(edges)))
			for _, e := range edges {
				s.writeByte(byte(e.EdgeType()))
				switch x := e.(type) {
				case *cadoc.HatchLineEdge:
					s.write2RawDouble(x.Start)
					s.write2RawDouble(x.End)
				case *cadoc.HatchArcEdge:
					s.write2RawDouble(x.Center)
					s.writeBitDouble(x.Radius)
					s.writeBitDouble(x.StartAngle)
					s.writeBitDouble(x.EndAngle)
					s.writeBit(x.CounterClockWise)
				case *cadoc.HatchEllipseEdge:
					s.write2RawDouble(x.Center)
					s.write2RawDouble(x.MajorAxisEndPoint)
					s.writeBitDouble(x.RadiusRatio)
					s.writeBitDouble(x.StartAngle)
					s.writeBitDouble(x.EndAngle)
					s.writeBit(x.CounterClockWise)
				case *cadoc.HatchSplineEdge:
					s.writeBitLong(x.Degree)
					s.writeBit(x.IsRational)
					s.writeBit(x.IsPeriodic)
					s.writeBitLong(int32(len(x.Knots)))
					s.writeBitLong(int32(len(x.ControlPoints)))
					for _, k := range x.Knots {
						s.writeBitDouble(k)
					}
					for _, cp := range x.ControlPoints {
						s.write2RawDouble(cp.XY())
						if x.IsRational {
							s.writeBitDouble(cp.Z)
						}
					}
					if w.r2010 {
						s.writeBitLong(int32(len(x.FitPoints)))
						if len(x.FitPoints) > 0 {
							for _, fp := range x.FitPoints {
								s.write2RawDouble(fp)
							}
							s.write2RawDouble(x.StartTangent)
							s.write2RawDouble(x.EndTangent)
						}
					}
				}
			}
		}
		var bounds []uint64
		for _, e := range p.Entities {
			if hh := w.handleOf(e); hh != 0 {
				bounds = append(bounds, hh)
			}
		}
		s.writeBitLong(int32(len(bounds)))
		for _, hh := range bounds {
			s.handleReference(wRefSoftPointer, hh)
		}
	}
	s.writeBitShort(int16(h.Style))
	s.writeBitShort(int16(h.PatternType))
	if !h.IsSolid {
		s.writeBitDouble(h.PatternAngle)
		s.writeBitDouble(h.PatternScale)
		s.writeBit(h.IsDouble)
		s.writeBitShort(int16(len(pattern.Lines)))
		for _, l := range pattern.Lines {
			s.writeBitDouble(l.Angle)
			s.write2BitDouble(l.BasePoint)
			s.write2BitDouble(l.Offset)
			s.writeBitShort(int16(len(l.DashLengths)))
			for _, d := range l.DashLengths {
				s.writeBitDouble(d)
			}
		}
	}
	if derived {
		s.writeBitDouble(h.PixelSize)
	}
	s.writeBitLong(int32(len(h.SeedPoints)))
	for _, p := range h.SeedPoints {
		s.write2RawDouble(p)
	}
}

func (w *wObjectWriter) writeLeader(l *cadoc.Leader) {
	s := w.sw
	s.writeBit(false)
	s.writeBitShort(int16(l.CreationType))
	s.writeBitShort(int16(l.PathType))
	s.writeBitLong(int32(len(l.Vertices)))
	for _, v := range l.Vertices {
		s.write3BitDouble(v)
	}
	var origin cadoc.Vec3
	if len(l.Vertices) > 0 {
		origin = l.Vertices[0]
	}
	s.write3BitDouble(origin)
	s.write3BitDouble(l.Normal)
	s.write3BitDouble(l.HorizontalDirection)
	s.write3BitDouble(l.BlockOffset)
	if w.version >= cadoc.AC1014 {
		s.write3BitDouble(l.AnnotationOffset)
	}
	style := w.dimStyleOrStandard(l.Style)
	if style == nil {
		style = cadoc.NewDimensionStyle(cadoc.DefaultDimStyleName)
	}
	if w.r13_14 {
		s.writeBitDouble(style.DimensionLineGap)
	}
	if w.version <= cadoc.AC1021 {
		s.writeBitDouble(l.TextHeight)
		s.writeBitDouble(l.TextWidth)
	}
	s.writeBit(l.HookLineDirection == cadoc.HookLineDirectionSame)
	s.writeBit(l.ArrowHeadEnabled)
	if w.r13_14 {
		s.writeBitShort(0)
		s.writeBitDouble(style.ArrowSize * style.ScaleFactor)
		s.writeBit(false)
		s.writeBit(false)
		s.writeBitShort(0)
		s.writeBitShort(0)
		s.writeBit(false)
		s.writeBit(false)
	}
	if w.r2000 {
		s.writeBitShort(0)
		s.writeBit(false)
		s.writeBit(false)
	}
	// Associated annotation: ACadSharp writes null.
	s.handleReference(wRefHardPointer, w.handleOf(l.AssociatedAnnotation))
	s.handleReference(wRefHardPointer, w.handleOf(w.dimStyleOrStandard(l.Style)))
}

func (w *wObjectWriter) writeLine(l *cadoc.Line) {
	s := w.sw
	if w.r13_14 {
		s.write3BitDouble(l.StartPoint)
		s.write3BitDouble(l.EndPoint)
	}
	if w.r2000 {
		zeroZ := l.StartPoint.Z == 0 && l.EndPoint.Z == 0
		s.writeBit(zeroZ)
		s.writeRawDouble(l.StartPoint.X)
		s.writeBitDoubleWithDefault(l.EndPoint.X, l.StartPoint.X)
		s.writeRawDouble(l.StartPoint.Y)
		s.writeBitDoubleWithDefault(l.EndPoint.Y, l.StartPoint.Y)
		if !zeroZ {
			s.writeRawDouble(l.StartPoint.Z)
			s.writeBitDoubleWithDefault(l.EndPoint.Z, l.StartPoint.Z)
		}
	}
	s.writeBitThickness(l.Thickness)
	s.writeBitExtrusion(l.Normal)
}

func (w *wObjectWriter) writeLwPolyline(p *cadoc.LwPolyline) {
	s := w.sw
	bulges, widths := false, false
	for _, v := range p.Vertices {
		if v.Bulge != 0 {
			bulges = true
		}
		if v.StartWidth != 0 || v.EndWidth != 0 {
			widths = true
		}
	}
	var flags int16
	if p.Flags&cadoc.LwPolylineFlagsPlinegen != 0 {
		flags |= 0x100
	}
	if p.Flags&cadoc.LwPolylineFlagsClosed != 0 {
		flags |= 0x200
	}
	if p.ConstantWidth != 0 {
		flags |= 0x4
	}
	if p.Elevation != 0 {
		flags |= 0x8
	}
	if p.Thickness != 0 {
		flags |= 2
	}
	if p.Normal != cadoc.AxisZ {
		flags |= 1
	}
	if bulges {
		flags |= 0x10
	}
	if widths {
		flags |= 0x20
	}
	s.writeBitShort(flags)
	if p.ConstantWidth != 0 {
		s.writeBitDouble(p.ConstantWidth)
	}
	if p.Elevation != 0 {
		s.writeBitDouble(p.Elevation)
	}
	if p.Thickness != 0 {
		s.writeBitDouble(p.Thickness)
	}
	if p.Normal != cadoc.AxisZ {
		s.write3BitDouble(p.Normal)
	}
	n := int32(len(p.Vertices))
	s.writeBitLong(n)
	if bulges {
		s.writeBitLong(n)
	}
	if widths {
		s.writeBitLong(n)
	}
	if w.r13_14 {
		for _, v := range p.Vertices {
			s.write2RawDouble(v.Location)
		}
	}
	if w.r2000 && n > 0 {
		last := p.Vertices[0]
		s.write2RawDouble(last.Location)
		for _, v := range p.Vertices[1:] {
			s.write2BitDoubleWithDefault(v.Location, last.Location)
			last = v
		}
	}
	if bulges {
		for _, v := range p.Vertices {
			s.writeBitDouble(v.Bulge)
		}
	}
	if widths {
		for _, v := range p.Vertices {
			s.writeBitDouble(v.StartWidth)
			s.writeBitDouble(v.EndWidth)
		}
	}
}

func (w *wObjectWriter) writeMesh(m *cadoc.Mesh) {
	s := w.sw
	s.writeBitShort(m.Version)
	s.writeBit(m.BlendCrease)
	s.writeBitLong(m.SubdivisionLevel)
	s.writeBitLong(int32(len(m.Vertices)))
	for _, v := range m.Vertices {
		s.write3BitDouble(v)
	}
	var nfaces int32
	for _, f := range m.Faces {
		nfaces += 1 + int32(len(f))
	}
	s.writeBitLong(nfaces)
	for _, f := range m.Faces {
		s.writeBitLong(int32(len(f)))
		for _, i := range f {
			s.writeBitLong(i)
		}
	}
	s.writeBitLong(int32(len(m.Edges)))
	for _, e := range m.Edges {
		s.writeBitLong(e.Start)
		s.writeBitLong(e.End)
	}
	s.writeBitLong(int32(len(m.Edges)))
	for _, e := range m.Edges {
		var c float64
		if e.Crease != nil {
			c = *e.Crease
		}
		s.writeBitDouble(c)
	}
	s.writeBitLong(0)
}

func (w *wObjectWriter) writeMLine(m *cadoc.MLine) {
	s := w.sw
	s.writeBitDouble(m.ScaleFactor)
	s.writeByte(byte(m.Justification))
	s.write3BitDouble(m.StartPoint)
	s.write3BitDouble(m.Normal)
	if m.Flags&cadoc.MLineFlagsClosed != 0 {
		s.writeBitShort(3)
	} else {
		s.writeBitShort(1)
	}
	nlines := 0
	if len(m.Vertices) > 0 {
		nlines = len(m.Vertices[0].Segments)
	}
	s.writeByte(byte(nlines))
	s.writeBitShort(int16(len(m.Vertices)))
	for _, v := range m.Vertices {
		s.write3BitDouble(v.Position)
		s.write3BitDouble(v.Direction)
		s.write3BitDouble(v.Miter)
		for i := 0; i < nlines; i++ {
			var seg cadoc.MLineSegment
			if i < len(v.Segments) {
				seg = v.Segments[i]
			}
			s.writeBitShort(int16(len(seg.Parameters)))
			for _, p := range seg.Parameters {
				s.writeBitDouble(p)
			}
			s.writeBitShort(int16(len(seg.AreaFillParameters)))
			for _, p := range seg.AreaFillParameters {
				s.writeBitDouble(p)
			}
		}
	}
	s.handleReference(wRefHardPointer, w.handleOf(m.Style))
}

// writeModelerGeometry writes the SAT data of REGION, 3DSOLID and BODY.
// From R2013 the ACIS data lives in the AcDsData section, which ACadSharp
// doesn't write, so the geometry is empty there.
func (w *wObjectWriter) writeModelerGeometry(g *cadoc.ModelerGeometry, name string) {
	s := w.sw
	var sat []byte
	if g.AcisText != "" {
		sat = []byte(g.AcisText)
	} else if len(g.AcisData) > 0 && !g.IsBinaryAcisData() {
		sat = g.AcisData
	}
	if !w.r2013 {
		if len(sat) == 0 && len(g.AcisData) > 0 && g.IsBinaryAcisData() {
			w.note(writerLevelWarning, "Binary ACIS data cannot be written for %s in %s, the geometry will be empty",
				name, w.version)
		}
		s.writeBit(len(sat) == 0)
		if len(sat) > 0 {
			s.writeBit(false)
			// Version 1: SAT blocks.
			s.writeBitShort(1)
			data := make([]byte, len(sat))
			for i, b := range sat {
				if b > 0x20 && b < 0x9F {
					b = 0x9F - b
				}
				data[i] = b
			}
			s.writeBitLong(int32(len(data)))
			s.writeBytes(data)
			s.writeBitLong(0)
			return
		}
	}
	// No wireframe data.
	s.writeBit(false)
	if w.r2007 {
		s.writeBitLong(0)
	}
	if w.r2013 {
		guid, ok := wParseGUID(g.Guid)
		s.writeBit(ok)
		w.writeGUID(guid)
		s.writeBitLong(0)
	}
}

// wParseGUID parses "{XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}" into the
// System.Guid byte layout.
func wParseGUID(s string) (g [16]byte, ok bool) {
	s = strings.Trim(strings.TrimSpace(s), "{}")
	s = strings.ReplaceAll(s, "-", "")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return g, false
	}
	binary.LittleEndian.PutUint32(g[0:], binary.BigEndian.Uint32(b[0:]))
	binary.LittleEndian.PutUint16(g[4:], binary.BigEndian.Uint16(b[4:]))
	binary.LittleEndian.PutUint16(g[6:], binary.BigEndian.Uint16(b[6:]))
	copy(g[8:], b[8:])
	return g, !wAllZero(g[:])
}

func (w *wObjectWriter) writeGUID(g [16]byte) {
	w.sw.writeBitLong(int32(binary.LittleEndian.Uint32(g[0:])))
	w.sw.writeBitShort(int16(binary.LittleEndian.Uint16(g[4:])))
	w.sw.writeBitShort(int16(binary.LittleEndian.Uint16(g[6:])))
	for _, b := range g[8:] {
		w.sw.writeByte(b)
	}
}

func (w *wObjectWriter) writeMText(m *cadoc.MText) {
	s := w.sw
	s.write3BitDouble(m.InsertPoint)
	s.write3BitDouble(m.Normal)
	s.write3BitDouble(m.AlignmentPoint)
	s.writeBitDouble(m.RectangleWidth)
	if w.r2007 {
		s.writeBitDouble(m.RectangleHeight)
	}
	s.writeBitDouble(m.Height)
	s.writeBitShort(int16(m.AttachmentPoint))
	s.writeBitShort(int16(m.DrawingDirection))
	// Extents height and width (not in DXF).
	s.writeBitDouble(0)
	s.writeBitDouble(0)
	s.writeVariableText(m.Value)
	s.handleReference(wRefHardPointer, w.textStyleOrStandard(m.Style))
	if w.r2000 {
		s.writeBitShort(int16(m.LineSpacingStyle))
		s.writeBitDouble(m.LineSpacing)
		s.writeBit(false)
	}
	if w.r2004 {
		s.writeBitLong(int32(m.BackgroundFillFlags))
		if m.BackgroundFillFlags&cadoc.BackgroundFillFlagsUseBackgroundFillColor != 0 ||
			w.version > cadoc.AC1027 && m.BackgroundFillFlags&cadoc.BackgroundFillFlagsTextFrame != 0 {
			s.writeBitDouble(m.BackgroundScale)
			s.writeCmColor(m.BackgroundColor, false)
			s.writeBitLong(int32(m.BackgroundTransparency.Value()))
		}
	}
	if !w.r2018 {
		return
	}
	s.writeBit(!m.IsAnnotative)
	if m.IsAnnotative {
		return
	}
	s.writeBitShort(4)
	s.writeBit(true)
	s.handleReference(wRefHardPointer, 0)
	s.writeBitLong(int32(m.AttachmentPoint))
	s.write3BitDouble(m.AlignmentPoint)
	s.write3BitDouble(m.InsertPoint)
	s.writeBitDouble(m.RectangleWidth)
	s.writeBitDouble(m.RectangleHeight)
	s.writeBitDouble(m.HorizontalWidth)
	s.writeBitDouble(m.VerticalHeight)
	cd := m.ColumnData
	s.writeBitShort(int16(cd.ColumnType))
	if cd.ColumnType != cadoc.ColumnTypeNoColumns {
		s.writeBitLong(cd.ColumnCount)
		s.writeBitDouble(cd.Width)
		s.writeBitDouble(cd.Gutter)
		s.writeBit(cd.AutoHeight)
		s.writeBit(cd.FlowReversed)
		if !cd.AutoHeight && cd.ColumnType == cadoc.ColumnTypeDynamicColumns {
			for _, h := range cd.Heights {
				s.writeBitDouble(h)
			}
		}
	}
}

func (w *wObjectWriter) writeMultiLeader(m *cadoc.MultiLeader) {
	s := w.sw
	if w.r2010 {
		s.writeBitShort(2)
	}
	ctx := m.ContextData
	if ctx == nil {
		ctx = cadoc.NewMultiLeaderObjectContextData()
	}
	w.writeMultiLeaderAnnotContextSubObject(ctx)
	s.handleReference(wRefHardPointer, w.handleOf(m.Style))
	s.writeBitLong(int32(m.PropertyOverrideFlags))
	s.writeBitShort(int16(m.PathType))
	s.writeCmColor(m.LineColor, false)
	s.handleReference(wRefHardPointer, w.handleOf(m.LeaderLineType))
	s.writeBitLong(int32(int16(m.LeaderLineWeight)))
	s.writeBit(m.EnableLanding)
	s.writeBit(m.EnableDogleg)
	s.writeBitDouble(m.LandingDistance)
	s.handleReference(wRefHardPointer, w.handleOf(m.Arrowhead))
	s.writeBitDouble(m.ArrowheadSize)
	s.writeBitShort(int16(m.ContentType))
	s.handleReference(wRefHardPointer, w.handleOf(m.TextStyle))
	s.writeBitShort(int16(m.TextLeftAttachment))
	s.writeBitShort(int16(m.TextRightAttachment))
	s.writeBitShort(int16(m.TextAngle))
	s.writeBitShort(int16(m.TextAlignment))
	s.writeCmColor(m.TextColor, false)
	s.writeBit(m.TextFrame)
	s.handleReference(wRefHardPointer, w.handleOf(m.BlockContent))
	s.writeCmColor(m.BlockContentColor, false)
	s.write3BitDouble(m.BlockContentScale)
	s.writeBitDouble(m.BlockContentRotation)
	s.writeBitShort(int16(m.BlockContentConnection))
	s.writeBit(m.EnableAnnotationScale)
	if w.version <= cadoc.AC1021 {
		// Arrow heads (not written).
		s.writeBitLong(0)
	}
	s.writeBitLong(int32(len(m.BlockAttributes)))
	for _, a := range m.BlockAttributes {
		s.handleReference(wRefHardPointer, w.handleOf(a.AttributeDefinition))
		s.writeVariableText(a.Text)
		s.writeBitShort(a.Index)
		s.writeBitDouble(a.Width)
	}
	s.writeBit(m.TextDirectionNegative)
	s.writeBitShort(m.TextAligninIPE)
	s.writeBitShort(int16(m.TextAttachmentPoint))
	s.writeBitDouble(m.ScaleFactor)
	if w.r2010 {
		s.writeBitShort(int16(m.TextAttachmentDirection))
		s.writeBitShort(int16(m.TextBottomAttachment))
		s.writeBitShort(int16(m.TextTopAttachment))
	}
	if w.r2013 {
		s.writeBit(m.ExtendedToText)
	}
}

func (w *wObjectWriter) writeMultiLeaderAnnotContextSubObject(c *cadoc.MultiLeaderObjectContextData) {
	s := w.sw
	var roots []*cadoc.MultiLeaderLeaderRoot
	for _, r := range c.LeaderRoots {
		if r != nil {
			roots = append(roots, r)
		}
	}
	s.writeBitLong(int32(len(roots)))
	for _, r := range roots {
		w.writeLeaderRoot(r)
	}
	s.writeBitDouble(c.ScaleFactor)
	s.write3BitDouble(c.ContentBasePoint)
	s.writeBitDouble(c.TextHeight)
	s.writeBitDouble(c.ArrowheadSize)
	s.writeBitDouble(c.LandingGap)
	s.writeBitShort(int16(c.TextLeftAttachment))
	s.writeBitShort(int16(c.TextRightAttachment))
	s.writeBitShort(int16(c.TextAlignment))
	s.writeBitShort(int16(c.BlockContentConnection))
	s.writeBit(c.HasTextContents)
	if c.HasTextContents {
		s.writeVariableText(c.TextLabel)
		s.write3BitDouble(c.TextNormal)
		s.handleReference(wRefHardPointer, w.handleOf(c.TextStyle))
		s.write3BitDouble(c.TextLocation)
		s.write3BitDouble(c.Direction)
		s.writeBitDouble(c.TextRotation)
		s.writeBitDouble(c.BoundaryWidth)
		s.writeBitDouble(c.BoundaryHeight)
		s.writeBitDouble(c.LineSpacingFactor)
		s.writeBitShort(int16(c.LineSpacing))
		s.writeCmColor(c.TextColor, false)
		s.writeBitShort(int16(c.TextAttachmentPoint))
		s.writeBitShort(int16(c.FlowDirection))
		s.writeCmColor(c.BackgroundFillColor, false)
		s.writeBitDouble(c.BackgroundScaleFactor)
		s.writeBitLong(c.BackgroundTransparency)
		s.writeBit(c.BackgroundFillEnabled)
		s.writeBit(c.BackgroundMaskFillOn)
		s.writeBitShort(c.ColumnType)
		s.writeBit(c.TextHeightAutomatic)
		s.writeBitDouble(c.ColumnWidth)
		s.writeBitDouble(c.ColumnGutter)
		s.writeBit(c.ColumnFlowReversed)
		s.writeBitLong(int32(len(c.ColumnSizes)))
		for _, v := range c.ColumnSizes {
			s.writeBitDouble(v)
		}
		s.writeBit(c.WordBreak)
		s.writeBit(false)
	} else {
		s.writeBit(c.HasContentsBlock)
		if c.HasContentsBlock {
			s.handleReference(wRefSoftPointer, w.handleOf(c.BlockContent))
			s.write3BitDouble(c.BlockContentNormal)
			s.write3BitDouble(c.BlockContentLocation)
			s.write3BitDouble(c.BlockContentScale)
			s.writeBitDouble(c.BlockContentRotation)
			s.writeCmColor(c.BlockContentColor, false)
			// The 4x4 matrix column by column (M00, M10, M20, M30, M01, …)
			// from the row-major DXF order.
			m := c.TransformationMatrix
			for col := 0; col < 4; col++ {
				for row := 0; row < 4; row++ {
					s.writeBitDouble(m[row*4+col])
				}
			}
		}
	}
	s.write3BitDouble(c.BasePoint)
	s.write3BitDouble(c.BaseDirection)
	s.write3BitDouble(c.BaseVertical)
	s.writeBit(c.NormalReversed)
	if w.r2010 {
		s.writeBitShort(int16(c.TextTopAttachment))
		s.writeBitShort(int16(c.TextBottomAttachment))
	}
}

func (w *wObjectWriter) writeLeaderRoot(r *cadoc.MultiLeaderLeaderRoot) {
	s := w.sw
	s.writeBit(r.ContentValid)
	s.writeBit(r.Unknown)
	s.write3BitDouble(r.ConnectionPoint)
	s.write3BitDouble(r.Direction)
	s.writeBitLong(int32(len(r.BreakStartEndPointsPairs)))
	for _, p := range r.BreakStartEndPointsPairs {
		s.write3BitDouble(p.StartPoint)
		s.write3BitDouble(p.EndPoint)
	}
	s.writeBitLong(r.LeaderIndex)
	s.writeBitDouble(r.LandingDistance)
	var lines []*cadoc.MultiLeaderLeaderLine
	for _, l := range r.Lines {
		if l != nil {
			lines = append(lines, l)
		}
	}
	s.writeBitLong(int32(len(lines)))
	for _, l := range lines {
		w.writeLeaderLine(l)
	}
	if w.r2010 {
		s.writeBitShort(int16(r.TextAttachmentDirection))
	}
}

func (w *wObjectWriter) writeLeaderLine(l *cadoc.MultiLeaderLeaderLine) {
	s := w.sw
	s.writeBitLong(int32(len(l.Points)))
	for _, p := range l.Points {
		s.write3BitDouble(p)
	}
	// Break info: the model keeps one segment index with its pairs.
	if len(l.BreakInfo) > 0 {
		s.writeBitLong(1)
		s.writeBitLong(l.SegmentIndex)
		s.writeBitLong(int32(len(l.BreakInfo)))
		for _, p := range l.BreakInfo {
			s.write3BitDouble(p.StartPoint)
			s.write3BitDouble(p.EndPoint)
		}
	} else {
		s.writeBitLong(0)
	}
	s.writeBitLong(l.Index)
	if w.r2010 {
		s.writeBitShort(int16(l.PathType))
		s.writeCmColor(l.LineColor, false)
		s.handleReference(wRefHardPointer, w.handleOf(l.LineType))
		s.writeBitLong(int32(int16(l.LineWeight)))
		s.writeBitDouble(l.ArrowheadSize)
		s.handleReference(wRefHardPointer, w.handleOf(l.Arrowhead))
		s.writeBitLong(int32(int16(l.OverrideFlags)))
	}
}

func (w *wObjectWriter) writeOle2Frame(o *cadoc.Ole2Frame) {
	s := w.sw
	s.writeBitShort(o.Version)
	if w.r2000 {
		s.writeBitShort(0)
	}
	s.writeBitLong(int32(len(o.BinaryData)))
	s.writeBytes(o.BinaryData)
	if w.r2000 {
		s.writeByte(3)
	}
}

func (w *wObjectWriter) writePdfUnderlay(u *cadoc.PdfUnderlay) {
	s := w.sw
	s.write3BitDouble(u.Normal)
	s.write3BitDouble(u.InsertPoint)
	s.writeBitDouble(u.Rotation)
	s.writeBitDouble(u.XScale)
	s.writeBitDouble(u.YScale)
	s.writeBitDouble(u.ZScale)
	s.writeByte(byte(u.Flags))
	s.writeByte(u.Contrast)
	s.writeByte(u.Fade)
	s.handleReference(wRefHardPointer, w.handleOf(u.Definition))
	s.writeBitLong(int32(len(u.ClipBoundaryVertices)))
	for _, v := range u.ClipBoundaryVertices {
		s.write2RawDouble(v)
	}
}

func (w *wObjectWriter) writePolyfaceMesh(p *cadoc.PolyfaceMesh) {
	s := w.sw
	s.writeBitShort(int16(len(p.Vertices)))
	s.writeBitShort(int16(len(p.Faces)))
	if w.r2004 {
		s.writeBitLong(int32(len(p.Vertices) + len(p.Faces)))
		for _, v := range p.Vertices {
			s.handleReference(wRefSoftPointer, v.Handle)
		}
		for _, f := range p.Faces {
			s.handleReference(wRefSoftPointer, f.Handle)
		}
	}
	if w.r13_15 {
		all := p.VertexEntities()
		var first, last uint64
		if len(all) > 0 {
			first, last = all[0].Base().Handle, all[len(all)-1].Base().Handle
		}
		s.handleReference(wRefSoftPointer, first)
		s.handleReference(wRefSoftPointer, last)
	}
	s.handleReference(wRefSoftPointer, w.objHandle(p.Seqend))
}

func (w *wObjectWriter) firstLastVertex(vs []cadoc.Entity) (uint64, uint64) {
	if len(vs) == 0 {
		return 0, 0
	}
	return vs[0].Base().Handle, vs[len(vs)-1].Base().Handle
}

func (w *wObjectWriter) writePolygonMesh(p *cadoc.PolygonMesh) {
	s := w.sw
	s.writeBitShort(int16(p.Flags))
	s.writeBitShort(int16(p.SmoothSurface))
	s.writeBitShort(p.MVertexCount)
	s.writeBitShort(p.NVertexCount)
	s.writeBitShort(p.MSmoothSurfaceDensity)
	s.writeBitShort(p.NSmoothSurfaceDensity)
	if w.r13_15 {
		first, last := w.firstLastVertex(p.VertexEntities())
		s.handleReference(wRefSoftPointer, first)
		s.handleReference(wRefSoftPointer, last)
	}
	if w.r2004 {
		s.writeBitLong(int32(len(p.Vertices)))
		for _, v := range p.Vertices {
			s.handleReference(wRefHardOwnership, v.Handle)
		}
	}
	s.handleReference(wRefHardOwnership, w.objHandle(p.Seqend))
}

func (w *wObjectWriter) writePolyline2D(p *cadoc.Polyline2D) {
	s := w.sw
	s.writeBitShort(int16(p.Flags))
	s.writeBitShort(int16(p.SmoothSurface))
	s.writeBitDouble(p.StartWidth)
	s.writeBitDouble(p.EndWidth)
	s.writeBitThickness(p.Thickness)
	s.writeBitDouble(p.Elevation)
	s.writeBitExtrusion(p.Normal)
	w.writePolylineVertexHandles(p.VertexEntities(), p.Seqend)
}

func (w *wObjectWriter) writePolylineVertexHandles(vs []cadoc.Entity, seqend *cadoc.Seqend) {
	s := w.sw
	if w.r2004 {
		s.writeBitLong(int32(len(vs)))
		for _, v := range vs {
			s.handleReference(wRefHardOwnership, v.Base().Handle)
		}
	}
	if w.r13_15 {
		first, last := w.firstLastVertex(vs)
		s.handleReference(wRefSoftPointer, first)
		s.handleReference(wRefSoftPointer, last)
	}
	s.handleReference(wRefHardOwnership, w.objHandle(seqend))
}

func (w *wObjectWriter) writePolyline3D(p *cadoc.Polyline3D) {
	s := w.sw
	var smooth byte
	switch p.SmoothSurface {
	case cadoc.SmoothSurfaceTypeQuadratic:
		smooth = 0b1
	case cadoc.SmoothSurfaceTypeCubic:
		smooth = 0b10
	}
	s.writeByte(smooth)
	var closed byte
	if p.Flags&cadoc.PolylineFlagsClosedPolylineOrClosedPolygonMeshInM != 0 {
		closed = 1
	}
	s.writeByte(closed)
	w.writePolylineVertexHandles(p.VertexEntities(), p.Seqend)
}

func (w *wObjectWriter) writeProxyEntity(p *cadoc.ProxyEntity) {
	s := w.sw
	s.writeBitLong(p.ClassNumber)
	if w.r2000 {
		if w.version > cadoc.AC1015 {
			var cpp string
			if p.Class != nil {
				cpp = p.Class.CppClassName
			}
			s.writeVariableText(cpp)
		}
		if !w.r2018 {
			s.writeBitLong(int32(p.Version) | p.MaintenanceVersion<<16)
		} else {
			s.writeBitLong(int32(p.Version))
			s.writeBitLong(p.MaintenanceVersion)
		}
		s.writeBit(p.OriginalDataFormatDxf)
	} else {
		var id int32
		if p.Class != nil {
			id = int32(p.Class.ItemClassId())
		}
		s.writeBitLong(id)
	}
	// The retained data runs up to the handles (ACadSharp drops it).
	s.writeBytes(p.BinaryData)
}

func (w *wObjectWriter) writeShape(sh *cadoc.Shape) {
	s := w.sw
	s.write3BitDouble(sh.InsertionPoint)
	s.writeBitDouble(sh.Size)
	s.writeBitDouble(sh.Rotation)
	s.writeBitDouble(sh.RelativeXScale)
	s.writeBitDouble(sh.ObliqueAngle)
	s.writeBitDouble(sh.Thickness)
	s.writeBitShort(int16(sh.ShapeIndex))
	s.write3BitDouble(sh.Normal)
	// The shape file (ACadSharp writes null).
	s.handleReference(wRefHardPointer, w.handleOf(sh.ShapeStyle))
}

func (w *wObjectWriter) writeSolid(so *cadoc.Solid) {
	s := w.sw
	s.writeBitThickness(so.Thickness)
	s.writeBitDouble(so.FirstCorner.Z)
	for _, c := range []cadoc.Vec3{so.FirstCorner, so.SecondCorner, so.ThirdCorner, so.FourthCorner} {
		s.writeRawDouble(c.X)
		s.writeRawDouble(c.Y)
	}
	s.writeBitExtrusion(so.Normal)
}

func (w *wObjectWriter) writeSpline(sp *cadoc.Spline) {
	s := w.sw
	var scenario int32
	if w.r2013 {
		flags1 := sp.Flags1
		if sp.KnotParametrization == cadoc.KnotParametrizationCustom || len(sp.FitPoints) == 0 {
			scenario = 1
			// Scenario 1 must not have the knot parameter bit.
			flags1 &^= cadoc.SplineFlags1UseKnotParameter
		} else {
			scenario = 2
			flags1 |= cadoc.SplineFlags1MethodFitPoints | cadoc.SplineFlags1UseKnotParameter
		}
		s.writeBitLong(scenario)
		s.writeBitLong(int32(flags1))
		s.writeBitLong(int32(sp.KnotParametrization))
	} else {
		scenario = 1
		if len(sp.FitPoints) > 0 {
			scenario = 2
		}
		// Fit data with another than chord parametrization is written as
		// control points, unless there are none (ACadSharp writes an empty
		// spline then).
		if scenario == 2 && sp.KnotParametrization != 0 && len(sp.ControlPoints) > 0 {
			scenario = 1
		}
		s.writeBitLong(scenario)
	}
	s.writeBitLong(sp.Degree)
	weighted := len(sp.Weights) > 0
	switch scenario {
	case 1:
		s.writeBit(sp.Flags&cadoc.SplineFlagsRational != 0)
		s.writeBit(sp.Flags&cadoc.SplineFlagsClosed != 0)
		s.writeBit(sp.Flags&cadoc.SplineFlagsPeriodic != 0)
		s.writeBitDouble(sp.KnotTolerance)
		s.writeBitDouble(sp.ControlPointTolerance)
		s.writeBitLong(int32(len(sp.Knots)))
		s.writeBitLong(int32(len(sp.ControlPoints)))
		s.writeBit(weighted)
		for _, k := range sp.Knots {
			s.writeBitDouble(k)
		}
		for i, cp := range sp.ControlPoints {
			s.write3BitDouble(cp)
			if weighted {
				wt := 1.0
				if i < len(sp.Weights) {
					wt = sp.Weights[i]
				}
				s.writeBitDouble(wt)
			}
		}
	case 2:
		s.writeBitDouble(sp.FitTolerance)
		s.write3BitDouble(sp.StartTangent)
		s.write3BitDouble(sp.EndTangent)
		s.writeBitLong(int32(len(sp.FitPoints)))
		for _, fp := range sp.FitPoints {
			s.write3BitDouble(fp)
		}
	}
}

func (w *wObjectWriter) writeTextEntity(t *cadoc.TextEntity) {
	s := w.sw
	if w.r13_14 {
		s.writeBitDouble(t.InsertPoint.Z)
		s.writeRawDouble(t.InsertPoint.X)
		s.writeRawDouble(t.InsertPoint.Y)
		s.writeRawDouble(t.AlignmentPoint.X)
		s.writeRawDouble(t.AlignmentPoint.Y)
		s.write3BitDouble(t.Normal)
		s.writeBitDouble(t.Thickness)
		s.writeBitDouble(t.ObliqueAngle)
		s.writeBitDouble(t.Rotation)
		s.writeBitDouble(t.Height)
		s.writeBitDouble(t.WidthFactor)
		s.writeVariableText(t.Value)
		s.writeBitShort(int16(t.Mirror))
		s.writeBitShort(int16(t.HorizontalAlignment))
		s.writeBitShort(int16(t.VerticalAlignment))
	} else {
		var df byte
		if t.InsertPoint.Z == 0 {
			df |= 0x01
		}
		if t.AlignmentPoint == (cadoc.Vec3{}) {
			df |= 0x02
		}
		if t.ObliqueAngle == 0 {
			df |= 0x04
		}
		if t.Rotation == 0 {
			df |= 0x08
		}
		if t.WidthFactor == 1 {
			df |= 0x10
		}
		if t.Mirror == 0 {
			df |= 0x20
		}
		if t.HorizontalAlignment == 0 {
			df |= 0x40
		}
		if t.VerticalAlignment == 0 {
			df |= 0x80
		}
		s.writeByte(df)
		if df&0x01 == 0 {
			s.writeRawDouble(t.InsertPoint.Z)
		}
		s.writeRawDouble(t.InsertPoint.X)
		s.writeRawDouble(t.InsertPoint.Y)
		if df&0x02 == 0 {
			s.writeBitDoubleWithDefault(t.AlignmentPoint.X, t.InsertPoint.X)
			s.writeBitDoubleWithDefault(t.AlignmentPoint.Y, t.InsertPoint.Y)
		}
		s.writeBitExtrusion(t.Normal)
		s.writeBitThickness(t.Thickness)
		if df&0x04 == 0 {
			s.writeRawDouble(t.ObliqueAngle)
		}
		if df&0x08 == 0 {
			s.writeRawDouble(t.Rotation)
		}
		s.writeRawDouble(t.Height)
		if df&0x10 == 0 {
			s.writeRawDouble(t.WidthFactor)
		}
		s.writeVariableText(t.Value)
		if df&0x20 == 0 {
			s.writeBitShort(int16(t.Mirror))
		}
		if df&0x40 == 0 {
			s.writeBitShort(int16(t.HorizontalAlignment))
		}
		if df&0x80 == 0 {
			s.writeBitShort(int16(t.VerticalAlignment))
		}
	}
	s.handleReference(wRefHardPointer, w.textStyleOrStandard(t.Style))
}

func (w *wObjectWriter) writeTolerance(t *cadoc.Tolerance) {
	s := w.sw
	if w.r13_14 {
		s.writeBitShort(0)
		s.writeBitDouble(0)
		s.writeBitDouble(0)
	}
	s.write3BitDouble(t.InsertionPoint)
	s.write3BitDouble(t.Direction)
	s.write3BitDouble(t.Normal)
	s.writeVariableText(t.Text)
	s.handleReference(wRefHardPointer, w.handleOf(w.dimStyleOrStandard(t.Style)))
}

func (w *wObjectWriter) writeVertex(v *cadoc.VertexBase) {
	w.sw.writeByte(byte(v.Flags))
	w.sw.write3BitDouble(v.Location)
}

func (w *wObjectWriter) writeVertex2D(v *cadoc.Vertex2D) {
	s := w.sw
	s.writeByte(byte(v.Flags))
	// The Z is taken from the polyline's elevation.
	s.writeBitDouble(v.Location.X)
	s.writeBitDouble(v.Location.Y)
	s.writeBitDouble(0)
	if v.StartWidth != 0 && v.EndWidth == v.StartWidth {
		// A negative start width stands for equal start and end widths.
		s.writeBitDouble(-v.StartWidth)
	} else {
		s.writeBitDouble(v.StartWidth)
		s.writeBitDouble(v.EndWidth)
	}
	s.writeBitDouble(v.Bulge)
	if w.r2010 {
		s.writeBitLong(v.Id)
	}
	s.writeBitDouble(v.CurveTangent)
}

func (w *wObjectWriter) writeViewport(v *cadoc.Viewport) {
	s := w.sw
	s.write3BitDouble(v.Center)
	s.writeBitDouble(v.Width)
	s.writeBitDouble(v.Height)
	if w.r2000 {
		s.write3BitDouble(v.ViewTarget)
		s.write3BitDouble(v.ViewDirection)
		s.writeBitDouble(v.TwistAngle)
		s.writeBitDouble(v.ViewHeight)
		s.writeBitDouble(v.LensLength)
		s.writeBitDouble(v.FrontClipPlane)
		s.writeBitDouble(v.BackClipPlane)
		s.writeBitDouble(v.SnapAngle)
		s.write2RawDouble(v.ViewCenter)
		s.write2RawDouble(v.SnapBase)
		s.write2RawDouble(v.SnapSpacing)
		s.write2RawDouble(v.GridSpacing)
		s.writeBitShort(v.CircleZoomPercent)
	}
	if w.r2007 {
		s.writeBitShort(v.MajorGridLineFrequency)
	}
	var frozen []uint64
	for _, l := range v.FrozenLayers {
		if h := w.handleOf(l); h != 0 {
			frozen = append(frozen, h)
		}
	}
	if w.r2000 {
		s.writeBitLong(int32(len(frozen)))
		s.writeBitLong(int32(v.Status))
		// Style sheet (never used).
		s.writeVariableText("")
		s.writeByte(byte(v.RenderMode))
		s.writeBit(v.DisplayUcsIcon)
		s.writeBit(v.UcsPerViewport)
		s.write3BitDouble(v.UcsOrigin)
		s.write3BitDouble(v.UcsXAxis)
		s.write3BitDouble(v.UcsYAxis)
		s.writeBitDouble(v.Elevation)
		s.writeBitShort(int16(v.UcsOrthographicType))
	}
	if w.r2004 {
		s.writeBitShort(int16(v.ShadePlotMode))
	}
	if w.r2007 {
		s.writeBit(v.UseDefaultLighting)
		s.writeByte(byte(v.DefaultLightingType))
		s.writeBitDouble(v.Brightness)
		s.writeBitDouble(v.Contrast)
		s.writeCmColor(v.AmbientLightColor, false)
	}
	if w.r13_14 {
		s.handleReference(wRefHardPointer, 0)
	}
	if w.r2000 {
		for _, h := range frozen {
			if w.r2004 {
				s.handleReference(wRefSoftPointer, h)
			} else {
				s.handleReference(wRefHardPointer, h)
			}
		}
		s.handleReference(wRefHardPointer, w.handleOf(v.Boundary))
	}
	if w.version == cadoc.AC1015 {
		// Viewport entity header.
		s.handleReference(wRefHardPointer, 0)
	}
	if w.r2000 {
		s.handleReference(wRefHardPointer, w.handleOf(v.NamedUcs))
		s.handleReference(wRefHardPointer, w.handleOf(v.BaseUcs))
	}
	if w.r2007 {
		s.handleReference(wRefSoftPointer, 0)
		s.handleReference(wRefHardPointer, 0)
		s.handleReference(wRefSoftPointer, 0)
		s.handleReference(wRefHardOwnership, 0)
	}
}

func (w *wObjectWriter) writeCadImage(img *cadoc.WipeoutBase) {
	s := w.sw
	s.writeBitLong(img.ClassVersion)
	s.write3BitDouble(img.InsertPoint)
	s.write3BitDouble(img.UVector)
	s.write3BitDouble(img.VVector)
	s.write2RawDouble(img.Size)
	s.writeBitShort(int16(img.Flags))
	s.writeBit(img.ClippingState)
	s.writeByte(img.Brightness)
	s.writeByte(img.Contrast)
	s.writeByte(img.Fade)
	if w.r2010 {
		s.writeBit(img.ClipMode == cadoc.ClipModeInside)
	}
	s.writeBitShort(int16(img.ClipType))
	switch img.ClipType {
	case cadoc.ClipTypeRectangular:
		var a, b cadoc.Vec2
		if len(img.ClipBoundaryVertices) >= 2 {
			a, b = img.ClipBoundaryVertices[0], img.ClipBoundaryVertices[1]
		}
		s.write2RawDouble(a)
		s.write2RawDouble(b)
	case cadoc.ClipTypePolygonal:
		s.writeBitLong(int32(len(img.ClipBoundaryVertices)))
		for _, v := range img.ClipBoundaryVertices {
			s.write2RawDouble(v)
		}
	}
	s.handleReference(wRefHardPointer, w.handleOf(img.Definition))
	s.handleReference(wRefHardOwnership, w.handleOf(img.DefinitionReactor))
}
