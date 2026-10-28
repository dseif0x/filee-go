package dxf

import (
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func init() {
	registerEntityWriter(&cadoc.Spline{}, func(w *writer, e cadoc.Entity) { w.writeSpline(e.(*cadoc.Spline)) })
	registerEntityWriter(&cadoc.Hatch{}, func(w *writer, e cadoc.Entity) { w.writeHatch(e.(*cadoc.Hatch)) })
	dim := func(w *writer, e cadoc.Entity) { w.writeDimension(e.(cadoc.Dimension)) }
	for _, d := range []cadoc.Entity{&cadoc.DimensionAligned{}, &cadoc.DimensionLinear{}, &cadoc.DimensionRadius{},
		&cadoc.DimensionDiameter{}, &cadoc.DimensionAngular2Line{}, &cadoc.DimensionAngular3Pt{},
		&cadoc.DimensionOrdinate{}, &cadoc.DimensionArc{}} {
		registerEntityWriter(d, dim)
	}
	registerEntityWriter(&cadoc.Leader{}, func(w *writer, e cadoc.Entity) { w.writeLeader(e.(*cadoc.Leader)) })
	registerEntityWriter(&cadoc.MultiLeader{}, func(w *writer, e cadoc.Entity) { w.writeMultiLeader(e.(*cadoc.MultiLeader)) })
	registerEntityWriter(&cadoc.MLine{}, func(w *writer, e cadoc.Entity) { w.writeMLine(e.(*cadoc.MLine)) })
	registerEntityWriter(&cadoc.Mesh{}, func(w *writer, e cadoc.Entity) { w.writeMesh(e.(*cadoc.Mesh)) })
	registerEntityWriter(&cadoc.Viewport{}, func(w *writer, e cadoc.Entity) { w.writeViewport(e.(*cadoc.Viewport)) })
	registerEntityWriter(&cadoc.RasterImage{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.RasterImage)
		w.writeWipeoutBase(&x.WipeoutBase, x.SubclassMarker())
	})
	registerEntityWriter(&cadoc.Wipeout{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Wipeout)
		w.writeWipeoutBase(&x.WipeoutBase, x.SubclassMarker())
	})
	registerEntityWriter(&cadoc.Ole2Frame{}, func(w *writer, e cadoc.Entity) { w.writeOle2Frame(e.(*cadoc.Ole2Frame)) })
	registerEntityWriter(&cadoc.PdfUnderlay{}, func(w *writer, e cadoc.Entity) { w.writePdfUnderlay(e.(*cadoc.PdfUnderlay)) })
	registerEntityWriter(&cadoc.Region{}, func(w *writer, e cadoc.Entity) { w.writeModeler(&e.(*cadoc.Region).ModelerGeometry) })
	registerEntityWriter(&cadoc.CadBody{}, func(w *writer, e cadoc.Entity) { w.writeModeler(&e.(*cadoc.CadBody).ModelerGeometry) })
	registerEntityWriter(&cadoc.Solid3D{}, func(w *writer, e cadoc.Entity) {
		x := e.(*cadoc.Solid3D)
		w.writeModeler(&x.ModelerGeometry)
		if w.version >= cadoc.AC1021 {
			w.o.str(100, cadoc.SubclassSolid3D)
			w.o.handle(350, 0)
		}
	})
	registerEntityWriter(&cadoc.ProxyEntity{}, func(w *writer, e cadoc.Entity) { w.writeProxyEntity(e.(*cadoc.ProxyEntity)) })
}

func (w *writer) writeSpline(s *cadoc.Spline) {
	o := w.o
	o.str(100, cadoc.SubclassSpline)
	if s.Flags&cadoc.SplineFlagsPlanar != 0 {
		o.vec3(210, s.Normal)
	}
	o.integer(70, int64(s.Flags))
	o.integer(71, int64(s.Degree))
	o.integer(72, int64(len(s.Knots)))
	o.integer(73, int64(len(s.ControlPoints)))
	if len(s.FitPoints) > 0 {
		o.integer(74, int64(len(s.FitPoints)))
	}
	o.dbl(42, s.KnotTolerance)
	o.dbl(43, s.ControlPointTolerance)
	o.dbl(44, s.FitTolerance)
	if !s.StartTangent.IsZero() {
		o.vec3(12, s.StartTangent)
	}
	if !s.EndTangent.IsZero() {
		o.vec3(13, s.EndTangent)
	}
	for _, k := range s.Knots {
		o.dbl(40, k)
	}
	for _, wt := range s.Weights {
		o.dbl(41, wt)
	}
	for _, p := range s.ControlPoints {
		o.vec3(10, p)
	}
	for _, p := range s.FitPoints {
		o.vec3(11, p)
	}
}

func (w *writer) writeHatch(h *cadoc.Hatch) {
	o := w.o
	o.str(100, cadoc.SubclassHatch)
	o.dbl(10, 0)
	o.dbl(20, 0)
	o.dbl(30, h.Elevation)
	o.vec3(210, h.Normal)
	name := "SOLID"
	if h.Pattern != nil && h.Pattern.Name != "" {
		name = h.Pattern.Name
	}
	o.str(2, name)
	o.boolean(70, h.IsSolid)
	o.boolean(71, h.IsAssociative)
	o.integer(91, int64(len(h.Paths)))
	derived := false
	for _, p := range h.Paths {
		w.writeBoundaryPath(p)
		if p.Flags&cadoc.BoundaryPathFlagsDerived != 0 {
			derived = true
		}
	}
	o.integer(75, int64(h.Style))
	o.integer(76, int64(h.PatternType))
	if p := h.Pattern; p != nil && len(p.Lines) > 0 {
		if !h.IsSolid {
			o.angle(52, h.PatternAngle)
			o.dbl(41, h.PatternScale)
		}
		o.boolean(77, h.IsDouble)
		o.integer(78, int64(len(p.Lines)))
		for _, l := range p.Lines {
			o.angle(53, l.Angle)
			o.dbl(43, l.BasePoint.X)
			o.dbl(44, l.BasePoint.Y)
			o.dbl(45, l.Offset.X)
			o.dbl(46, l.Offset.Y)
			o.integer(79, int64(len(l.DashLengths)))
			for _, d := range l.DashLengths {
				o.dbl(49, d)
			}
		}
	}
	if h.PixelSize != 0 || derived {
		o.dbl(47, h.PixelSize)
	}
	o.integer(98, int64(len(h.SeedPoints)))
	for _, sp := range h.SeedPoints {
		o.vec2(10, sp)
	}
	g := h.GradientColor
	if w.version <= cadoc.AC1015 || g == nil || (h.Pattern != nil && len(h.Pattern.Lines) > 0) {
		return
	}
	o.boolean(450, g.Enabled)
	o.integer(451, int64(g.Reserved))
	o.dbl(460, g.Angle)
	o.dbl(461, g.Shift)
	o.boolean(452, g.IsSingleColorGradient)
	o.dbl(462, g.ColorTint)
	o.integer(453, int64(len(g.Colors)))
	for _, c := range g.Colors {
		o.dbl(463, c.Value)
		o.integer(63, int64(c.Color.ApproxIndex()))
		if c.Color.IsTrueColor() {
			o.integer(421, int64(c.Color.TrueColor()))
		}
	}
	o.str(470, g.Name)
}

func (w *writer) writeBoundaryPath(p *cadoc.HatchBoundaryPath) {
	o := w.o
	flags := p.EffectiveFlags()
	o.integer(92, int64(flags))
	if flags&cadoc.BoundaryPathFlagsPolyline == 0 {
		o.integer(93, int64(len(p.Edges)))
	}
	for _, e := range p.Edges {
		w.writeHatchEdge(e)
	}
	var refs []cadoc.Entity
	for _, e := range p.Entities {
		if w.known(e) {
			refs = append(refs, e)
		}
	}
	o.integer(97, int64(len(refs)))
	for _, e := range refs {
		o.handle(330, e.Base().Handle)
	}
}

// hatchAngles writes arc edge angles in degrees, keeping a full sweep
// distinct without leaving -360..360 (writeHatchBoundaryAngles).
func (w *writer) hatchAngles(start, end float64) {
	norm := func(a float64) float64 {
		d := math.Mod(a*180/math.Pi, 360)
		return d
	}
	s, e := norm(start), norm(end)
	if math.Abs(math.Abs(end-start)-2*math.Pi) < 1e-9 {
		sweep := 360.0
		if end < start {
			sweep = -360
		}
		if math.Abs(s+sweep) > 360 {
			s -= sweep
		}
		e = s + sweep
	}
	w.o.dbl(50, s)
	w.o.dbl(51, e)
}

func (w *writer) writeHatchEdge(e cadoc.HatchEdge) {
	o := w.o
	if _, pl := e.(*cadoc.HatchPolylineEdge); !pl {
		o.integer(72, int64(e.EdgeType()))
	}
	switch x := e.(type) {
	case *cadoc.HatchArcEdge:
		o.vec2(10, x.Center)
		o.dbl(40, x.Radius)
		w.hatchAngles(x.StartAngle, x.EndAngle)
		o.boolean(73, x.CounterClockWise)
	case *cadoc.HatchEllipseEdge:
		o.vec2(10, x.Center)
		o.vec2(11, x.MajorAxisEndPoint)
		o.dbl(40, x.RadiusRatio)
		w.hatchAngles(x.StartAngle, x.EndAngle)
		o.boolean(73, x.CounterClockWise)
	case *cadoc.HatchLineEdge:
		o.vec2(10, x.Start)
		o.vec2(11, x.End)
	case *cadoc.HatchPolylineEdge:
		bulge := x.HasBulge()
		o.boolean(72, bulge)
		o.boolean(73, x.IsClosed)
		o.integer(93, int64(len(x.Vertices)))
		for _, v := range x.Vertices {
			o.dbl(10, v.X)
			o.dbl(20, v.Y)
			if bulge {
				o.dbl(42, v.Z)
			}
		}
	case *cadoc.HatchSplineEdge:
		o.integer(94, int64(x.Degree))
		o.boolean(73, x.IsRational)
		o.boolean(74, x.IsPeriodic)
		o.integer(95, int64(len(x.Knots)))
		o.integer(96, int64(len(x.ControlPoints)))
		for _, k := range x.Knots {
			o.dbl(40, k)
		}
		for _, p := range x.ControlPoints {
			o.dbl(10, p.X)
			o.dbl(20, p.Y)
			if x.IsRational {
				o.dbl(42, p.Z)
			}
		}
		o.integer(97, int64(len(x.FitPoints)))
		for _, p := range x.FitPoints {
			o.dbl(11, p.X)
			o.dbl(21, p.Y)
		}
		if len(x.FitPoints) > 0 {
			o.vec2(12, x.StartTangent)
			o.vec2(13, x.EndTangent)
		}
	}
}

func (w *writer) writeDimension(d cadoc.Dimension) {
	o := w.o
	b := d.Dimension()
	o.str(100, cadoc.SubclassDimension)
	if w.version >= cadoc.AC1027 {
		o.integer(280, int64(b.Version))
	}
	if b.Block != nil {
		o.str(2, b.Block.Name)
	}
	o.vec3(10, b.DefinitionPoint)
	o.vec3(11, b.TextMiddlePoint)
	o.vec3(12, b.InsertionPoint)
	o.angle(53, b.TextRotation)
	o.integer(70, int64(b.Flags))
	o.integer(71, int64(b.AttachmentPoint))
	o.integer(72, int64(b.LineSpacingStyle))
	o.dbl(41, b.LineSpacingFactor)
	if w.version >= cadoc.AC1021 {
		o.boolean(74, b.FlipArrow1)
		o.boolean(75, b.FlipArrow2)
	}
	if b.Text != "" {
		o.str(1, b.Text)
	}
	if b.HorizontalDirection != 0 {
		o.angle(51, b.HorizontalDirection)
	}
	o.vec3(210, b.Normal)
	o.str(3, dimStyleName(b.Style))
	switch x := d.(type) {
	case *cadoc.DimensionLinear:
		o.str(100, cadoc.SubclassAlignedDimension)
		o.vec3(13, x.FirstPoint)
		o.vec3(14, x.SecondPoint)
		o.angle(50, x.Rotation)
		o.str(100, cadoc.SubclassLinearDimension)
	case *cadoc.DimensionAligned:
		o.str(100, cadoc.SubclassAlignedDimension)
		o.vec3(13, x.FirstPoint)
		o.vec3(14, x.SecondPoint)
	case *cadoc.DimensionRadius:
		o.str(100, cadoc.SubclassRadialDimension)
		o.vec3(15, x.AngleVertex)
		o.dbl(40, x.LeaderLength)
	case *cadoc.DimensionDiameter:
		o.str(100, cadoc.SubclassDiametricDimension)
		o.vec3(15, x.AngleVertex)
		o.dbl(40, x.LeaderLength)
	case *cadoc.DimensionAngular2Line:
		o.str(100, cadoc.SubclassAngular2LineDimension)
		o.vec3(13, x.FirstPoint)
		o.vec3(14, x.SecondPoint)
		o.vec3(15, x.AngleVertex)
		o.vec3(16, x.DimensionArc)
	case *cadoc.DimensionAngular3Pt:
		o.str(100, cadoc.SubclassAngular3PointDimension)
		o.vec3(13, x.FirstPoint)
		o.vec3(14, x.SecondPoint)
		o.vec3(15, x.AngleVertex)
	case *cadoc.DimensionArc:
		o.str(100, cadoc.SubclassArcDimension)
		o.vec3(13, x.FirstPoint)
		o.vec3(14, x.SecondPoint)
		o.vec3(15, x.Center)
		o.boolean(70, x.IsPartial)
		o.dbl(40, x.StartAngle)
		o.dbl(41, x.EndAngle)
		o.boolean(71, x.HasLeader)
		if x.HasLeader {
			o.vec3(16, x.LeaderPoint1)
			o.vec3(17, x.LeaderPoint2)
		}
	case *cadoc.DimensionOrdinate:
		o.str(100, cadoc.SubclassOrdinateDimension)
		o.vec3(13, x.FeatureLocation)
		o.vec3(14, x.LeaderEndpoint)
	}
}

func (w *writer) writeLeader(l *cadoc.Leader) {
	o := w.o
	o.str(100, cadoc.SubclassLeader)
	o.str(3, dimStyleName(l.Style))
	o.boolean(71, l.ArrowHeadEnabled)
	o.integer(72, int64(l.PathType))
	o.integer(73, int64(l.CreationType))
	o.integer(74, int64(l.HookLineDirection))
	o.boolean(75, l.HasHookline)
	o.dbl(40, l.TextHeight)
	o.dbl(41, l.TextWidth)
	o.integer(76, int64(len(l.Vertices)))
	for _, v := range l.Vertices {
		o.vec3(10, v)
	}
	if w.known(l.AssociatedAnnotation) {
		o.handle(340, l.AssociatedAnnotation.Base().Handle)
	}
	o.vec3(210, l.Normal)
	o.vec3(211, l.HorizontalDirection)
	o.vec3(212, l.BlockOffset)
	o.vec3(213, l.AnnotationOffset)
}

func (w *writer) writeMultiLeader(m *cadoc.MultiLeader) {
	o := w.o
	o.str(100, cadoc.SubclassMultiLeader)
	o.integer(270, 2)
	c := m.ContextData
	if c == nil {
		c = cadoc.NewMultiLeaderObjectContextData()
	}
	w.writeMLeaderContext(c)
	if w.writable(m.Style) {
		o.handle(340, m.Style.Handle)
	}
	o.integer(90, int64(m.PropertyOverrideFlags))
	o.integer(170, int64(m.PathType))
	o.integer(91, cmColorValue(m.LineColor))
	w.refIfKnown(341, m.LeaderLineType)
	o.integer(171, int64(m.LeaderLineWeight))
	o.boolean(290, m.EnableLanding)
	o.boolean(291, m.EnableDogleg)
	o.dbl(41, m.LandingDistance)
	o.dbl(42, m.ArrowheadSize)
	w.refIfKnown(342, m.Arrowhead)
	o.integer(172, int64(m.ContentType))
	w.refIfKnown(343, m.TextStyle)
	o.integer(173, int64(m.TextLeftAttachment))
	o.integer(95, int64(m.TextRightAttachment))
	o.integer(174, int64(m.TextAngle))
	o.integer(175, int64(m.TextAlignment))
	o.integer(92, cmColorValue(m.TextColor))
	o.boolean(292, m.TextFrame)
	w.refIfKnown(344, m.BlockContent)
	o.integer(93, cmColorValue(m.BlockContentColor))
	o.vec3(10, m.BlockContentScale)
	o.dbl(43, m.BlockContentRotation)
	o.integer(176, int64(m.BlockContentConnection))
	o.boolean(293, m.EnableAnnotationScale)
	for _, a := range m.BlockAttributes {
		if w.known(a.AttributeDefinition) {
			o.handle(330, a.AttributeDefinition.Handle)
			o.integer(177, int64(a.Index))
			o.dbl(44, a.Width)
			o.str(302, a.Text)
		}
	}
	o.boolean(294, m.TextDirectionNegative)
	o.integer(178, int64(m.TextAligninIPE))
	o.integer(179, int64(m.TextAttachmentPoint))
	o.dbl(45, m.ScaleFactor)
	o.integer(271, int64(m.TextAttachmentDirection))
	o.integer(272, int64(m.TextBottomAttachment))
	o.integer(273, int64(m.TextTopAttachment))
	o.boolean(295, m.ExtendedToText)
}

func (w *writer) writeMLeaderContext(c *cadoc.MultiLeaderObjectContextData) {
	o := w.o
	o.str(300, "CONTEXT_DATA{")
	o.dbl(40, c.ScaleFactor)
	o.vec3(10, c.ContentBasePoint)
	o.dbl(41, c.TextHeight)
	o.dbl(140, c.ArrowheadSize)
	o.dbl(145, c.LandingGap)
	o.integer(174, int64(c.TextLeftAttachment))
	o.integer(175, int64(c.TextRightAttachment))
	o.integer(176, int64(c.TextAlignment))
	o.integer(177, int64(c.BlockContentConnection))
	o.boolean(290, c.HasTextContents)
	if c.HasTextContents {
		o.str(304, c.TextLabel)
		o.vec3(11, c.TextNormal)
		w.refIfKnown(340, c.TextStyle)
		o.vec3(12, c.TextLocation)
		o.vec3(13, c.Direction)
		o.dbl(42, c.TextRotation)
		o.dbl(43, c.BoundaryWidth)
		o.dbl(44, c.BoundaryHeight)
		o.dbl(45, c.LineSpacingFactor)
		o.integer(170, int64(c.LineSpacing))
		o.integer(90, cmColorValue(c.TextColor))
		o.integer(171, int64(c.TextAttachmentPoint))
		o.integer(172, int64(c.FlowDirection))
		o.integer(91, cmColorValue(c.BackgroundFillColor))
		o.dbl(141, c.BackgroundScaleFactor)
		o.integer(92, int64(c.BackgroundTransparency))
		o.boolean(291, c.BackgroundFillEnabled)
		o.boolean(292, c.BackgroundMaskFillOn)
		o.integer(173, int64(c.ColumnType))
		o.boolean(293, c.TextHeightAutomatic)
		o.dbl(142, c.ColumnWidth)
		o.dbl(143, c.ColumnGutter)
		o.boolean(294, c.ColumnFlowReversed)
		for _, sz := range c.ColumnSizes {
			o.dbl(144, sz)
		}
		o.boolean(295, c.WordBreak)
	}
	o.boolean(296, c.HasContentsBlock)
	if c.HasContentsBlock {
		w.refIfKnown(341, c.BlockContent)
		o.vec3(14, c.BlockContentNormal)
		o.vec3(15, c.BlockContentLocation)
		o.vec3(16, c.BlockContentScale)
		o.dbl(46, c.BlockContentRotation)
		o.integer(93, cmColorValue(c.BlockContentColor))
		for _, v := range c.TransformationMatrix {
			o.dbl(47, v)
		}
	}
	o.vec3(110, c.BasePoint)
	o.vec3(111, c.BaseDirection)
	o.vec3(112, c.BaseVertical)
	o.boolean(297, c.NormalReversed)
	for _, root := range c.LeaderRoots {
		o.str(302, "LEADER{")
		o.integer(290, 1)
		o.integer(291, 1)
		o.vec3(10, root.ConnectionPoint)
		o.vec3(11, root.Direction)
		for _, p := range root.BreakStartEndPointsPairs {
			o.vec3(12, p.StartPoint)
			o.vec3(13, p.EndPoint)
		}
		o.integer(90, int64(root.LeaderIndex))
		o.dbl(40, root.LandingDistance)
		for _, line := range root.Lines {
			o.str(304, "LEADER_LINE{")
			for _, p := range line.Points {
				o.vec3(10, p)
			}
			o.integer(91, int64(line.Index))
			if line.OverrideFlags != 0 {
				o.integer(170, int64(line.PathType))
				o.integer(92, cmColorValue(line.LineColor))
				w.refIfKnown(340, line.LineType)
				o.integer(171, int64(line.LineWeight))
				o.dbl(40, line.ArrowheadSize)
				w.refIfKnown(341, line.Arrowhead)
				o.integer(93, int64(line.OverrideFlags))
			}
			o.str(305, "}")
		}
		o.integer(271, int64(root.TextAttachmentDirection))
		o.str(303, "}")
	}
	o.integer(272, int64(c.TextBottomAttachment))
	o.integer(273, int64(c.TextTopAttachment))
	o.str(301, "}")
}

func (w *writer) writeMLine(m *cadoc.MLine) {
	o := w.o
	o.str(100, cadoc.SubclassMLine)
	name := cadoc.DefaultMLineStyleName
	if m.Style != nil {
		name = m.Style.Name
	}
	o.str(2, name)
	if w.writable(m.Style) {
		o.handle(340, m.Style.Handle)
	}
	o.dbl(40, m.ScaleFactor)
	o.integer(70, int64(m.Justification))
	o.integer(71, int64(m.Flags))
	o.integer(72, int64(len(m.Vertices)))
	if m.Style != nil {
		o.integer(73, int64(len(m.Style.Elements)))
	} else if len(m.Vertices) > 0 {
		o.integer(73, int64(len(m.Vertices[0].Segments)))
	}
	o.vec3(10, m.StartPoint)
	o.vec3(210, m.Normal)
	for _, v := range m.Vertices {
		o.vec3(11, v.Position)
		o.vec3(12, v.Direction)
		o.vec3(13, v.Miter)
		for _, s := range v.Segments {
			o.integer(74, int64(len(s.Parameters)))
			for _, p := range s.Parameters {
				o.dbl(41, p)
			}
			o.integer(75, int64(len(s.AreaFillParameters)))
			for _, p := range s.AreaFillParameters {
				o.dbl(42, p)
			}
		}
	}
}

func (w *writer) writeMesh(m *cadoc.Mesh) {
	o := w.o
	o.str(100, cadoc.SubclassMesh)
	o.integer(71, int64(m.Version))
	o.boolean(72, m.BlendCrease)
	o.integer(91, int64(m.SubdivisionLevel))
	o.integer(92, int64(len(m.Vertices)))
	for _, v := range m.Vertices {
		o.vec3(10, v)
	}
	n := len(m.Faces)
	for _, f := range m.Faces {
		n += len(f)
	}
	o.integer(93, int64(n))
	for _, f := range m.Faces {
		o.integer(90, int64(len(f)))
		for _, i := range f {
			o.integer(90, int64(i))
		}
	}
	o.integer(94, int64(len(m.Edges)))
	for _, e := range m.Edges {
		o.integer(90, int64(e.Start))
		o.integer(90, int64(e.End))
	}
	o.integer(95, int64(len(m.Edges)))
	for _, e := range m.Edges {
		c := 0.0
		if e.Crease != nil {
			c = *e.Crease
		}
		o.dbl(140, c)
	}
	o.integer(90, 0)
}

func (w *writer) writeViewport(v *cadoc.Viewport) {
	o := w.o
	o.str(100, cadoc.SubclassViewport)
	o.vec3(10, v.Center)
	o.dbl(40, v.Width)
	o.dbl(41, v.Height)
	o.integer(68, int64(v.ActiveStatus))
	o.integer(69, int64(v.Id))
	o.vec2(12, v.ViewCenter)
	o.vec2(13, v.SnapBase)
	o.vec2(14, v.SnapSpacing)
	o.vec2(15, v.GridSpacing)
	o.vec3(16, v.ViewDirection)
	o.vec3(17, v.ViewTarget)
	o.dbl(42, v.LensLength)
	o.dbl(43, v.FrontClipPlane)
	o.dbl(44, v.BackClipPlane)
	o.dbl(45, v.ViewHeight)
	o.angle(50, v.SnapAngle)
	o.angle(51, v.TwistAngle)
	o.integer(72, int64(v.CircleZoomPercent))
	for _, l := range v.FrozenLayers {
		if w.known(l) {
			o.handle(331, l.Handle)
		}
	}
	o.integer(90, int64(v.Status))
	if w.known(v.Boundary) {
		o.handle(340, v.Boundary.Base().Handle)
	}
	o.str(1, v.StyleSheetName)
	o.integer(281, int64(v.RenderMode))
	o.boolean(71, v.UcsPerViewport)
	o.boolean(74, v.DisplayUcsIcon)
	o.vec3(110, v.UcsOrigin)
	o.vec3(111, v.UcsXAxis)
	o.vec3(112, v.UcsYAxis)
	w.refIfKnown(345, v.NamedUcs)
	w.refIfKnown(346, v.BaseUcs)
	o.integer(79, int64(v.UcsOrthographicType))
	o.dbl(146, v.Elevation)
	if w.version >= cadoc.AC1018 {
		o.integer(170, int64(v.ShadePlotMode))
	}
	if w.version >= cadoc.AC1021 {
		o.integer(61, int64(v.MajorGridLineFrequency))
		if w.writable(v.VisualStyle) {
			o.handle(348, v.VisualStyle.Base().Handle)
		}
		o.boolean(292, v.UseDefaultLighting)
		o.integer(282, int64(v.DefaultLightingType))
		o.dbl(141, v.Brightness)
		o.dbl(142, v.Contrast)
		o.integer(63, int64(v.AmbientLightColor.ApproxIndex()))
		if v.AmbientLightColor.IsTrueColor() {
			o.integer(421, int64(v.AmbientLightColor.TrueColor()))
		}
	}
}

func (w *writer) writeWipeoutBase(x *cadoc.WipeoutBase, subclass string) {
	o := w.o
	o.str(100, subclass)
	o.integer(90, int64(x.ClassVersion))
	o.vec3(10, x.InsertPoint)
	o.vec3(11, x.UVector)
	o.vec3(12, x.VVector)
	o.vec2(13, x.Size)
	if w.writable(x.Definition) {
		o.handle(340, x.Definition.Handle)
		w.enqueue(x.Definition)
	}
	o.integer(70, int64(x.Flags))
	o.boolean(280, x.ClippingState)
	o.integer(281, int64(x.Brightness))
	o.integer(282, int64(x.Contrast))
	o.integer(283, int64(x.Fade))
	if w.writable(x.DefinitionReactor) {
		o.handle(360, x.DefinitionReactor.Handle)
		w.enqueue(x.DefinitionReactor)
	}
	clip := x.ClipType
	if clip == 0 {
		clip = cadoc.ClipTypeRectangular
	}
	o.integer(71, int64(clip))
	if clip == cadoc.ClipTypePolygonal && len(x.ClipBoundaryVertices) > 0 {
		o.integer(91, int64(len(x.ClipBoundaryVertices)+1))
		for _, v := range x.ClipBoundaryVertices {
			o.vec2(14, v)
		}
		o.vec2(14, x.ClipBoundaryVertices[0])
	} else {
		o.integer(91, int64(len(x.ClipBoundaryVertices)))
		for _, v := range x.ClipBoundaryVertices {
			o.vec2(14, v)
		}
	}
	if w.version >= cadoc.AC1024 {
		o.integer(290, int64(x.ClipMode))
	}
}

func (w *writer) writeOle2Frame(x *cadoc.Ole2Frame) {
	o := w.o
	o.str(100, cadoc.SubclassOle2Frame)
	o.integer(70, int64(x.Version))
	o.str(3, x.SourceApplication)
	o.vec3(10, x.UpperLeftCorner)
	o.vec3(11, x.LowerRightCorner)
	o.integer(71, int64(x.OleObjectType))
	o.boolean(72, x.IsPaperSpace)
	o.integer(73, 3)
	o.integer(90, int64(len(x.BinaryData)))
	if len(x.BinaryData) > 0 {
		o.chunk(310, x.BinaryData)
	}
	o.str(1, "OLE")
}

func (w *writer) writePdfUnderlay(x *cadoc.PdfUnderlay) {
	o := w.o
	o.str(100, cadoc.SubclassUnderlay)
	if w.writable(x.Definition) {
		o.handle(340, x.Definition.Handle)
		w.enqueue(x.Definition)
	}
	o.vec3(10, x.InsertPoint)
	o.dbl(41, x.XScale)
	o.dbl(42, x.YScale)
	o.dbl(43, x.ZScale)
	o.angle(50, x.Rotation)
	o.vec3(210, x.Normal)
	o.integer(280, int64(x.Flags))
	o.integer(281, int64(x.Contrast))
	o.integer(282, int64(x.Fade))
	for _, v := range x.ClipBoundaryVertices {
		o.vec2(11, v)
	}
}

func (w *writer) writeModeler(m *cadoc.ModelerGeometry) {
	o := w.o
	o.str(100, cadoc.SubclassModelerGeometry)
	if w.version >= cadoc.AC1027 {
		guid := m.Guid
		has := guid != "" && strings.Trim(guid, "{}0-") != ""
		if guid == "" {
			guid = "{00000000-0000-0000-0000-000000000000}"
		}
		o.boolean(290, has)
		o.str(2, guid)
		return
	}
	o.integer(70, int64(m.ModelerFormatVersion))
	if m.IsBinaryAcisData() {
		w.note(LevelWarning, "Encrypted SAB data is not supported in DXF version %s.", w.version)
		return
	}
	text := m.AcisText
	if text == "" && len(m.AcisData) > 0 {
		text = string(m.AcisData)
	}
	if text == "" {
		w.note(LevelWarning, "Encrypted SAT not found for modeler geometry %X.", m.Handle)
		return
	}
	const chunk = 255
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		enc := []rune(acisCode(line))
		for off := 0; off < len(enc); {
			n := min(chunk, len(enc)-off)
			if off+n < len(enc) {
				for i := off + n - 1; i > off; i-- {
					if enc[i] == ' ' {
						n = i - off + 1
						break
					}
				}
			}
			code := 3
			if off == 0 {
				code = 1
			}
			o.str(code, string(enc[off:off+n]))
			off += n
		}
	}
}

func (w *writer) writeProxyEntity(p *cadoc.ProxyEntity) {
	o := w.o
	o.str(100, cadoc.SubclassProxyEntity)
	o.integer(90, int64(p.ProxyClassId))
	o.integer(91, int64(p.ClassNumber))
	o.integer(71, int64(p.Version))
	o.integer(97, int64(p.MaintenanceVersion))
	o.boolean(70, p.OriginalDataFormatDxf)
	if len(p.ProxyGraphics) > 0 {
		o.integer(160, int64(len(p.ProxyGraphics)))
		o.chunk(310, p.ProxyGraphics)
	}
	o.integer(162, int64(len(p.StringData)))
	if len(p.StringData) > 0 {
		o.chunk(311, p.StringData)
	}
	o.integer(161, int64(len(p.BinaryData)))
	if len(p.BinaryData) > 0 {
		o.chunk(310, p.BinaryData)
	}
}

// writable reports a document object that the OBJECTS section will contain,
// so a reference to it doesn't dangle.
func (w *writer) writable(obj cadoc.Object) bool {
	return w.known(obj) && w.objectSupported(obj)
}
