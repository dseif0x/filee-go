package dwg

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// ----- dimensions -----

// readCommonDimensionData reads the AcDbDimension data.
func (or *objectReader) readCommonDimensionData(t *tmpl, d *cadoc.DimensionBase) {
	or.readCommonEntityData(t)
	if or.R2010Plus {
		d.Version = or.obj.ReadRC()
	}
	d.Normal = or.obj.Read3BitDouble()
	mid := or.obj.Read2RawDouble()
	elevation := or.obj.ReadBitDouble()
	d.TextMiddlePoint = cadoc.Vec3{X: mid.X, Y: mid.Y, Z: elevation}
	flags := or.obj.ReadRC()
	d.SetTextUserDefinedLocation(flags&0b01 == 0)
	d.Text = or.text.ReadVariableText()
	d.TextRotation = or.obj.ReadBitDouble()
	d.HorizontalDirection = or.obj.ReadBitDouble()
	// Insertion scale and rotation of the anonymous block (not kept).
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	or.obj.ReadBitDouble()
	if or.R2000Plus {
		d.AttachmentPoint = cadoc.AttachmentPointType(or.obj.ReadBitShort())
		d.LineSpacingStyle = cadoc.LineSpacingStyleType(or.obj.ReadBitShort())
		d.LineSpacingFactor = or.obj.ReadBitDouble()
		d.MeasurementValue = or.obj.ReadBitDouble()
	}
	if or.R2007Plus {
		or.obj.ReadBit()
		d.FlipArrow1 = or.obj.ReadBit()
		d.FlipArrow2 = or.obj.ReadBit()
	}
	pt := or.obj.Read2RawDouble()
	d.InsertionPoint = cadoc.Vec3{X: pt.X, Y: pt.Y, Z: elevation}
}

func (or *objectReader) readCommonDimensionAlignedData(d *cadoc.DimensionAligned) {
	d.FirstPoint = or.obj.Read3BitDouble()
	d.SecondPoint = or.obj.Read3BitDouble()
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.ExtLineRotation = or.obj.ReadBitDouble()
}

// readDimensionHandles reads the style and block handles and sets the
// build of the template.
func (or *objectReader) readDimensionHandles(t *tmpl, d *cadoc.DimensionBase) {
	style := or.handleRef()
	block := or.handleRef()
	t.build = func(b *builder) {
		if s, ok := tableRef(b, t, b.dimensionStyles, style, ""); ok {
			d.Style = s
		}
		if r, ok := tableRef(b, t, b.blockRecords, block, ""); ok {
			d.Block = r
		}
	}
}

func (or *objectReader) readDimOrdinate() *tmpl {
	d := cadoc.NewDimensionOrdinate()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.FeatureLocation = or.obj.Read3BitDouble()
	d.LeaderEndpoint = or.obj.Read3BitDouble()
	if or.obj.ReadRC()&0b01 != 0 {
		d.Flags |= cadoc.DimensionTypeOrdinateTypeX
	} else {
		d.Flags &^= cadoc.DimensionTypeOrdinateTypeX
	}
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimLinear() *tmpl {
	d := cadoc.NewDimensionLinear()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	or.readCommonDimensionAlignedData(&d.DimensionAligned)
	d.Rotation = or.obj.ReadBitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimAligned() *tmpl {
	d := cadoc.NewDimensionAligned()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	or.readCommonDimensionAlignedData(d)
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimAngular3pt() *tmpl {
	d := cadoc.NewDimensionAngular3Pt()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.FirstPoint = or.obj.Read3BitDouble()
	d.SecondPoint = or.obj.Read3BitDouble()
	d.AngleVertex = or.obj.Read3BitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimLine2pt() *tmpl {
	d := cadoc.NewDimensionAngular2Line()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	xy := or.obj.Read2RawDouble()
	d.DimensionArc = cadoc.Vec3{X: xy.X, Y: xy.Y, Z: d.TextMiddlePoint.Z}
	d.FirstPoint = or.obj.Read3BitDouble()
	d.SecondPoint = or.obj.Read3BitDouble()
	d.AngleVertex = or.obj.Read3BitDouble()
	d.DefinitionPoint = or.obj.Read3BitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimRadius() *tmpl {
	d := cadoc.NewDimensionRadius()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.AngleVertex = or.obj.Read3BitDouble()
	d.LeaderLength = or.obj.ReadBitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimDiameter() *tmpl {
	d := cadoc.NewDimensionDiameter()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	d.AngleVertex = or.obj.Read3BitDouble()
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.LeaderLength = or.obj.ReadBitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

func (or *objectReader) readDimArc() *tmpl {
	d := cadoc.NewDimensionArc()
	t := or.newEntityTmpl(d)
	or.readCommonDimensionData(t, &d.DimensionBase)
	d.DefinitionPoint = or.obj.Read3BitDouble()
	d.FirstPoint = or.obj.Read3BitDouble()
	d.SecondPoint = or.obj.Read3BitDouble()
	d.Center = or.obj.Read3BitDouble()
	d.IsPartial = or.obj.ReadBit()
	d.StartAngle = or.obj.ReadBitDouble()
	d.EndAngle = or.obj.ReadBitDouble()
	d.HasLeader = or.obj.ReadBit()
	d.LeaderPoint1 = or.obj.Read3BitDouble()
	d.LeaderPoint2 = or.obj.Read3BitDouble()
	or.readDimensionHandles(t, &d.DimensionBase)
	return t
}

// ----- leader, multileader -----

func (or *objectReader) readLeader() *tmpl {
	l := cadoc.NewLeader()
	t := or.newEntityTmpl(l)
	or.readCommonEntityData(t)
	or.obj.ReadBit()
	l.CreationType = cadoc.LeaderCreationType(or.obj.ReadBitShort())
	l.PathType = cadoc.LeaderPathType(or.obj.ReadBitShort())
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		l.Vertices = append(l.Vertices, or.obj.Read3BitDouble())
	}
	or.obj.Read3BitDouble() // origin
	l.Normal = or.obj.Read3BitDouble()
	l.HorizontalDirection = or.obj.Read3BitDouble()
	l.BlockOffset = or.obj.Read3BitDouble()
	if or.ver >= cadoc.AC1014 {
		l.AnnotationOffset = or.obj.Read3BitDouble()
	}
	if or.R13_14Only {
		// DIMGAP at creation; ACadSharp stores it in the (default) style.
		or.obj.ReadBitDouble()
	}
	if or.ver <= cadoc.AC1021 {
		// For later versions these values are unreliable (ACadSharp).
		l.TextHeight = or.obj.ReadBitDouble()
		l.TextWidth = or.obj.ReadBitDouble()
	}
	if or.obj.ReadBit() {
		l.HookLineDirection = cadoc.HookLineDirectionSame
	} else {
		l.HookLineDirection = cadoc.HookLineDirectionOpposite
	}
	l.ArrowHeadEnabled = or.obj.ReadBit()
	if or.R13_14Only {
		or.obj.ReadBitShort()  // arrowhead type
		or.obj.ReadBitDouble() // DIMASZ
		or.obj.ReadBit()
		or.obj.ReadBit()
		or.obj.ReadBitShort()
		or.obj.ReadBitShort() // byblock colour
		or.obj.ReadBit()
		or.obj.ReadBit()
	}
	if or.R2000Plus {
		or.obj.ReadBitShort()
		or.obj.ReadBit()
		or.obj.ReadBit()
	}
	annotation := or.handleRef()
	style := or.handleRef()
	t.build = func(b *builder) {
		if s, ok := tableRef(b, t, b.dimensionStyles, style, ""); ok {
			l.Style = s
		}
		if e := b.entity(annotation); e != nil {
			l.AssociatedAnnotation = e
		}
	}
	return t
}

// mleaderContextTmpl is CadMLeaderAnnotContextTemplate.
type mleaderContextTmpl struct {
	textStyle, blockRecord uint64
	lines                  []mleaderLineTmpl
}

type mleaderLineTmpl struct {
	line            *cadoc.MultiLeaderLeaderLine
	lineType, arrow uint64
}

func (b *builder) buildMLeaderContext(c *cadoc.MultiLeaderObjectContextData, ct *mleaderContextTmpl) {
	if s, ok := b.object(ct.textStyle).(*cadoc.TextStyle); ok {
		c.TextStyle = s
	}
	if r, ok := b.object(ct.blockRecord).(*cadoc.BlockRecord); ok {
		c.BlockContent = r
	}
	for _, lt := range ct.lines {
		if l, ok := b.object(lt.lineType).(*cadoc.LineType); ok {
			lt.line.LineType = l
		}
		if r, ok := b.object(lt.arrow).(*cadoc.BlockRecord); ok {
			lt.line.Arrowhead = r
		}
	}
}

func (or *objectReader) readStartEndPointPair() cadoc.StartEndPointPair {
	s := or.obj.Read3BitDouble()
	return cadoc.StartEndPointPair{StartPoint: s, EndPoint: or.obj.Read3BitDouble()}
}

func (or *objectReader) readLeaderLine(ct *mleaderContextTmpl) *cadoc.MultiLeaderLeaderLine {
	l := &cadoc.MultiLeaderLeaderLine{}
	lt := mleaderLineTmpl{line: l}
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		l.Points = append(l.Points, or.obj.Read3BitDouble())
	}
	nBreaks := int(or.obj.ReadBitLong())
	l.BreakInfoCount = int32(nBreaks)
	for i := 0; i < nBreaks; i++ {
		// Break info: segment index and start/end pairs; the model keeps
		// the pairs of all of them in one list.
		seg := or.obj.ReadBitLong()
		if i == 0 {
			l.SegmentIndex = seg
		}
		np := int(or.obj.ReadBitLong())
		for j := 0; j < np; j++ {
			l.BreakInfo = append(l.BreakInfo, or.readStartEndPointPair())
		}
	}
	l.Index = or.obj.ReadBitLong()
	if or.R2010Plus {
		l.PathType = cadoc.MultiLeaderPathType(or.obj.ReadBitShort())
		l.LineColor = or.merged.ReadCmColor(false)
		lt.lineType = or.handleRef()
		l.LineWeight = cadoc.LineWeight(or.obj.ReadBitLong())
		l.ArrowheadSize = or.obj.ReadBitDouble()
		lt.arrow = or.handleRef()
		l.OverrideFlags = cadoc.LeaderLinePropertOverrideFlags(or.obj.ReadBitLong())
	}
	ct.lines = append(ct.lines, lt)
	return l
}

func (or *objectReader) readLeaderRoot(ct *mleaderContextTmpl) *cadoc.MultiLeaderLeaderRoot {
	r := &cadoc.MultiLeaderLeaderRoot{}
	r.ContentValid = or.obj.ReadBit()
	r.Unknown = or.obj.ReadBit()
	r.ConnectionPoint = or.obj.Read3BitDouble()
	r.Direction = or.obj.Read3BitDouble()
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		r.BreakStartEndPointsPairs = append(r.BreakStartEndPointsPairs, or.readStartEndPointPair())
	}
	r.LeaderIndex = or.obj.ReadBitLong()
	r.LandingDistance = or.obj.ReadBitDouble()
	n = int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		r.Lines = append(r.Lines, or.readLeaderLine(ct))
	}
	if or.R2010Plus {
		r.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(or.obj.ReadBitShort())
	}
	return r
}

// readMultiLeaderAnnotContextData reads the context data embedded in a
// MULTILEADER (and the body of ACDB_MLEADEROBJECTCONTEXTDATA_CLASS).
func (or *objectReader) readMultiLeaderAnnotContextData(c *cadoc.MultiLeaderObjectContextData, ct *mleaderContextTmpl) {
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		c.LeaderRoots = append(c.LeaderRoots, or.readLeaderRoot(ct))
	}
	c.ScaleFactor = or.obj.ReadBitDouble()
	c.ContentBasePoint = or.obj.Read3BitDouble()
	c.TextHeight = or.obj.ReadBitDouble()
	c.ArrowheadSize = or.obj.ReadBitDouble()
	c.LandingGap = or.obj.ReadBitDouble()
	c.TextLeftAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	c.TextRightAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	c.TextAlignment = cadoc.TextAlignmentType(or.obj.ReadBitShort())
	c.BlockContentConnection = cadoc.BlockContentConnectionType(or.obj.ReadBitShort())
	c.HasTextContents = or.obj.ReadBit()
	if c.HasTextContents {
		c.TextLabel = or.text.ReadVariableText()
		c.TextNormal = or.obj.Read3BitDouble()
		ct.textStyle = or.handleRef()
		c.TextLocation = or.obj.Read3BitDouble()
		c.Direction = or.obj.Read3BitDouble()
		c.TextRotation = or.obj.ReadBitDouble()
		c.BoundaryWidth = or.obj.ReadBitDouble()
		c.BoundaryHeight = or.obj.ReadBitDouble()
		c.LineSpacingFactor = or.obj.ReadBitDouble()
		c.LineSpacing = cadoc.LineSpacingStyle(or.obj.ReadBitShort())
		c.TextColor = or.merged.ReadCmColor(false)
		c.TextAttachmentPoint = cadoc.TextAttachmentPointType(or.obj.ReadBitShort())
		c.FlowDirection = cadoc.FlowDirectionType(or.obj.ReadBitShort())
		c.BackgroundFillColor = or.merged.ReadCmColor(false)
		c.BackgroundScaleFactor = or.obj.ReadBitDouble()
		c.BackgroundTransparency = or.obj.ReadBitLong()
		c.BackgroundFillEnabled = or.obj.ReadBit()
		c.BackgroundMaskFillOn = or.obj.ReadBit()
		c.ColumnType = or.obj.ReadBitShort()
		c.TextHeightAutomatic = or.obj.ReadBit()
		c.ColumnWidth = or.obj.ReadBitDouble()
		c.ColumnGutter = or.obj.ReadBitDouble()
		c.ColumnFlowReversed = or.obj.ReadBit()
		n := int(or.obj.ReadBitLong())
		for i := 0; i < n; i++ {
			c.ColumnSizes = append(c.ColumnSizes, or.obj.ReadBitDouble())
		}
		c.WordBreak = or.obj.ReadBit()
		or.obj.ReadBit()
	} else {
		c.HasContentsBlock = or.obj.ReadBit()
		if c.HasContentsBlock {
			ct.blockRecord = or.handleRef()
			c.BlockContentNormal = or.obj.Read3BitDouble()
			c.BlockContentLocation = or.obj.Read3BitDouble()
			c.BlockContentScale = or.obj.Read3BitDouble()
			c.BlockContentRotation = or.obj.ReadBitDouble()
			c.BlockContentColor = or.merged.ReadCmColor(false)
			for i := range c.TransformationMatrix {
				c.TransformationMatrix[i] = or.obj.ReadBitDouble()
			}
		}
	}
	c.BasePoint = or.obj.Read3BitDouble()
	c.BaseDirection = or.obj.Read3BitDouble()
	c.BaseVertical = or.obj.Read3BitDouble()
	c.NormalReversed = or.obj.ReadBit()
	if or.R2010Plus {
		c.TextTopAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
		c.TextBottomAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	}
}

func (or *objectReader) readMultiLeader() *tmpl {
	m := cadoc.NewMultiLeader()
	t := or.newEntityTmpl(m)
	or.readCommonEntityData(t)
	if or.R2010Plus {
		or.obj.ReadBitShort() // version 2
	}
	ct := &mleaderContextTmpl{}
	or.readMultiLeaderAnnotContextData(m.ContextData, ct)
	style := or.handleRef()
	m.PropertyOverrideFlags = cadoc.MultiLeaderPropertyOverrideFlags(or.obj.ReadBitLong())
	m.PathType = cadoc.MultiLeaderPathType(or.obj.ReadBitShort())
	m.LineColor = or.merged.ReadCmColor(false)
	lineType := or.handleRef()
	m.LeaderLineWeight = cadoc.LineWeight(or.obj.ReadBitLong())
	m.EnableLanding = or.obj.ReadBit()
	m.EnableDogleg = or.obj.ReadBit()
	m.LandingDistance = or.obj.ReadBitDouble()
	arrow := or.handleRef()
	m.ArrowheadSize = or.obj.ReadBitDouble()
	m.ContentType = cadoc.LeaderContentType(or.obj.ReadBitShort())
	textStyle := or.handleRef()
	m.TextLeftAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	m.TextRightAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	m.TextAngle = cadoc.TextAngleType(or.obj.ReadBitShort())
	m.TextAlignment = cadoc.TextAlignmentType(or.obj.ReadBitShort())
	m.TextColor = or.merged.ReadCmColor(false)
	m.TextFrame = or.obj.ReadBit()
	blockContent := or.handleRef()
	m.BlockContentColor = or.merged.ReadCmColor(false)
	m.BlockContentScale = or.obj.Read3BitDouble()
	m.BlockContentRotation = or.obj.ReadBitDouble()
	m.BlockContentConnection = cadoc.BlockContentConnectionType(or.obj.ReadBitShort())
	m.EnableAnnotationScale = or.obj.ReadBit()
	if or.R2007Pre {
		n := int(or.obj.ReadBitLong())
		for i := 0; i < n; i++ {
			or.obj.ReadBit() // is default
			or.handleRef()   // arrowhead
		}
	}
	n := or.obj.count(or.obj.ReadBitLong(), 2)
	attHandles := make([]uint64, n)
	for i := 0; i < n; i++ {
		attHandles[i] = or.handleRef()
		a := cadoc.MultiLeaderBlockAttribute{}
		a.Text = or.text.ReadVariableText()
		a.Index = or.obj.ReadBitShort()
		a.Width = or.obj.ReadBitDouble()
		m.BlockAttributes = append(m.BlockAttributes, a)
	}
	m.TextDirectionNegative = or.obj.ReadBit()
	m.TextAligninIPE = or.obj.ReadBitShort()
	m.TextAttachmentPoint = cadoc.TextAttachmentPointType(or.obj.ReadBitShort())
	m.ScaleFactor = or.obj.ReadBitDouble()
	if or.R2010Plus {
		m.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(or.obj.ReadBitShort())
		m.TextBottomAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
		m.TextTopAttachment = cadoc.TextAttachmentType(or.obj.ReadBitShort())
	}
	if or.R2013Plus {
		m.ExtendedToText = or.obj.ReadBit()
	}
	t.build = func(b *builder) {
		b.buildMLeaderContext(m.ContextData, ct)
		if s, ok := b.object(style).(*cadoc.MultiLeaderStyle); ok {
			m.Style = s
		}
		if l, ok := b.object(lineType).(*cadoc.LineType); ok {
			m.LeaderLineType = l
		}
		if s, ok := b.object(textStyle).(*cadoc.TextStyle); ok {
			m.TextStyle = s
		}
		if r, ok := b.object(blockContent).(*cadoc.BlockRecord); ok {
			m.BlockContent = r
		}
		if r, ok := b.object(arrow).(*cadoc.BlockRecord); ok {
			m.Arrowhead = r
		}
		for i := range m.BlockAttributes {
			if d, ok := b.object(attHandles[i]).(*cadoc.AttributeDefinition); ok {
				m.BlockAttributes[i].AttributeDefinition = d
			}
		}
	}
	return t
}

// ----- hatch -----

func (or *objectReader) readHatch() *tmpl {
	h := cadoc.NewHatch()
	t := or.newEntityTmpl(h)
	or.readCommonEntityData(t)
	if or.R2004Plus {
		g := h.GradientColor
		g.Enabled = or.obj.ReadBitLong() != 0
		g.Reserved = or.obj.ReadBitLong()
		g.Angle = or.obj.ReadBitDouble()
		g.Shift = or.obj.ReadBitDouble()
		g.IsSingleColorGradient = uint32(or.obj.ReadBitLong()) > 0
		g.ColorTint = or.obj.ReadBitDouble()
		n := int(or.obj.ReadBitLong())
		for i := 0; i < n; i++ {
			c := cadoc.GradientColor{}
			c.Value = or.obj.ReadBitDouble()
			c.Color = or.merged.ReadCmColor(false)
			g.Colors = append(g.Colors, c)
		}
		g.Name = or.text.ReadVariableText()
	}
	h.Elevation = or.obj.ReadBitDouble()
	h.Normal = or.obj.Read3BitDouble()
	h.Pattern = cadoc.NewHatchPattern(or.text.ReadVariableText())
	h.IsSolid = or.obj.ReadBit()
	h.IsAssociative = or.obj.ReadBit()
	nPaths := int(or.obj.ReadBitLong())
	derived := false
	var pathHandles [][]uint64
	for i := 0; i < nPaths; i++ {
		path := &cadoc.HatchBoundaryPath{}
		path.Flags = cadoc.BoundaryPathFlags(or.obj.ReadBitLong())
		if path.Flags&cadoc.BoundaryPathFlagsDerived != 0 {
			derived = true
		}
		if path.Flags&cadoc.BoundaryPathFlagsPolyline == 0 {
			n := int(or.obj.ReadBitLong())
			for j := 0; j < n; j++ {
				switch cadoc.HatchEdgeType(or.obj.ReadRC()) {
				case cadoc.HatchEdgeTypeLine:
					s := or.obj.Read2RawDouble()
					path.Edges = append(path.Edges, &cadoc.HatchLineEdge{Start: s, End: or.obj.Read2RawDouble()})
				case cadoc.HatchEdgeTypeCircularArc:
					e := &cadoc.HatchArcEdge{}
					e.Center = or.obj.Read2RawDouble()
					e.Radius = or.obj.ReadBitDouble()
					e.StartAngle = or.obj.ReadBitDouble()
					e.EndAngle = or.obj.ReadBitDouble()
					e.CounterClockWise = or.obj.ReadBit()
					path.Edges = append(path.Edges, e)
				case cadoc.HatchEdgeTypeEllipticArc:
					e := &cadoc.HatchEllipseEdge{}
					e.Center = or.obj.Read2RawDouble()
					e.MajorAxisEndPoint = or.obj.Read2RawDouble()
					e.RadiusRatio = or.obj.ReadBitDouble()
					e.StartAngle = or.obj.ReadBitDouble()
					e.EndAngle = or.obj.ReadBitDouble()
					e.CounterClockWise = or.obj.ReadBit()
					path.Edges = append(path.Edges, e)
				case cadoc.HatchEdgeTypeSpline:
					e := &cadoc.HatchSplineEdge{}
					e.Degree = or.obj.ReadBitLong()
					e.IsRational = or.obj.ReadBit()
					e.IsPeriodic = or.obj.ReadBit()
					nKnots := int(or.obj.ReadBitLong())
					nCtrl := int(or.obj.ReadBitLong())
					for k := 0; k < nKnots; k++ {
						e.Knots = append(e.Knots, or.obj.ReadBitDouble())
					}
					for p := 0; p < nCtrl; p++ {
						cp := or.obj.Read2RawDouble()
						w := 0.0
						if e.IsRational {
							w = or.obj.ReadBitDouble()
						}
						e.ControlPoints = append(e.ControlPoints, cadoc.Vec3{X: cp.X, Y: cp.Y, Z: w})
					}
					if or.R2010Plus {
						nFit := int(or.obj.ReadBitLong())
						if nFit > 0 {
							for f := 0; f < nFit; f++ {
								e.FitPoints = append(e.FitPoints, or.obj.Read2RawDouble())
							}
							e.StartTangent = or.obj.Read2RawDouble()
							e.EndTangent = or.obj.Read2RawDouble()
						}
					}
					path.Edges = append(path.Edges, e)
				}
			}
		} else {
			pl := &cadoc.HatchPolylineEdge{}
			bulges := or.obj.ReadBit()
			pl.IsClosed = or.obj.ReadBit()
			n := int(or.obj.ReadBitLong())
			for k := 0; k < n; k++ {
				v := or.obj.Read2RawDouble()
				bulge := 0.0
				if bulges {
					bulge = or.obj.ReadBitDouble()
				}
				pl.Vertices = append(pl.Vertices, cadoc.Vec3{X: v.X, Y: v.Y, Z: bulge})
			}
			path.Edges = append(path.Edges, pl)
		}
		n := int(or.obj.ReadBitLong())
		var handles []uint64
		for k := 0; k < n; k++ {
			handles = append(handles, or.handleRef())
		}
		pathHandles = append(pathHandles, handles)
		h.Paths = append(h.Paths, path)
	}
	h.Style = cadoc.HatchStyleType(or.obj.ReadBitShort())
	h.PatternType = cadoc.HatchPatternType(or.obj.ReadBitShort())
	if !h.IsSolid {
		h.PatternAngle = or.obj.ReadBitDouble()
		h.PatternScale = or.obj.ReadBitDouble()
		h.IsDouble = or.obj.ReadBit()
		n := int(or.obj.ReadBitShort())
		for i := 0; i < n; i++ {
			l := cadoc.HatchPatternLine{}
			l.Angle = or.obj.ReadBitDouble()
			l.BasePoint = or.obj.Read2BitDouble()
			l.Offset = or.obj.Read2BitDouble()
			nd := int(or.obj.ReadBitShort())
			for d := 0; d < nd; d++ {
				l.DashLengths = append(l.DashLengths, or.obj.ReadBitDouble())
			}
			h.Pattern.Lines = append(h.Pattern.Lines, l)
		}
	}
	if derived {
		h.PixelSize = or.obj.ReadBitDouble()
	}
	n := int(or.obj.ReadBitLong())
	for i := 0; i < n; i++ {
		h.SeedPoints = append(h.SeedPoints, or.obj.Read2RawDouble())
	}
	t.build = func(b *builder) {
		for i, path := range h.Paths {
			seen := map[uint64]bool{}
			for _, hd := range pathHandles[i] {
				if seen[hd] {
					continue
				}
				seen[hd] = true
				if e := b.entity(hd); e != nil {
					path.Entities = append(path.Entities, e)
				}
			}
		}
	}
	return t
}

// ----- images, underlays, OLE -----

func (or *objectReader) readCadImage(w *cadoc.WipeoutBase, e cadoc.Entity) *tmpl {
	t := or.newEntityTmpl(e)
	or.readCommonEntityData(t)
	w.ClassVersion = or.obj.ReadBitLong()
	w.InsertPoint = or.obj.Read3BitDouble()
	w.UVector = or.obj.Read3BitDouble()
	w.VVector = or.obj.Read3BitDouble()
	w.Size = or.obj.Read2RawDouble()
	w.Flags = cadoc.ImageDisplayFlags(or.obj.ReadBitShort())
	w.ClippingState = or.obj.ReadBit()
	w.Brightness = or.obj.ReadRC()
	w.Contrast = or.obj.ReadRC()
	w.Fade = or.obj.ReadRC()
	if or.R2010Plus {
		if or.obj.ReadBit() {
			w.ClipMode = cadoc.ClipModeInside
		} else {
			w.ClipMode = cadoc.ClipModeOutside
		}
	}
	clipType := cadoc.ClipType(or.obj.ReadBitShort())
	// ACadSharp does not keep the clip type.
	w.ClipType = clipType
	switch clipType {
	case cadoc.ClipTypeRectangular:
		w.ClipBoundaryVertices = append(w.ClipBoundaryVertices, or.obj.Read2RawDouble(), or.obj.Read2RawDouble())
	case cadoc.ClipTypePolygonal:
		n := int(or.obj.ReadBitLong())
		for i := 0; i < n; i++ {
			w.ClipBoundaryVertices = append(w.ClipBoundaryVertices, or.obj.Read2RawDouble())
		}
	}
	def := or.handleRef()
	reactor := or.handleRef()
	t.build = func(b *builder) {
		if d, ok := b.object(def).(*cadoc.ImageDefinition); ok {
			w.Definition = d
		}
		if r, ok := b.object(reactor).(*cadoc.ImageDefinitionReactor); ok {
			w.DefinitionReactor = r
		}
	}
	return t
}

func (or *objectReader) readPdfUnderlay() *tmpl {
	u := cadoc.NewPdfUnderlay()
	t := or.newEntityTmpl(u)
	or.readCommonEntityData(t)
	u.Normal = or.obj.Read3BitDouble()
	u.InsertPoint = or.obj.Read3BitDouble()
	u.Rotation = or.obj.ReadBitDouble()
	u.XScale = or.obj.ReadBitDouble()
	u.YScale = or.obj.ReadBitDouble()
	u.ZScale = or.obj.ReadBitDouble()
	u.Flags = cadoc.UnderlayDisplayFlags(or.obj.ReadRC())
	u.Contrast = or.obj.ReadRC()
	u.Fade = or.obj.ReadRC()
	def := or.handleRef()
	n := int(or.merged.ReadBitLong())
	for i := 0; i < n; i++ {
		u.ClipBoundaryVertices = append(u.ClipBoundaryVertices, or.merged.Read2RawDouble())
	}
	t.build = func(b *builder) {
		if d, ok := b.object(def).(*cadoc.PdfUnderlayDefinition); ok {
			u.Definition = d
		} else {
			b.notify(NotifyWarning, "UnderlayDefinition not found for %d", u.Handle)
		}
	}
	return t
}

func (or *objectReader) readOle2Frame() *tmpl {
	o := cadoc.NewOle2Frame()
	t := or.newEntityTmpl(o)
	or.readCommonEntityData(t)
	o.Version = or.merged.ReadBitShort()
	if or.R2000Plus {
		// Tile mode descriptor (DXF 72); ACadSharp drops it.
		o.IsPaperSpace = or.merged.ReadBitShort() == 1
	}
	n := int(or.merged.ReadBitLong())
	o.BinaryData = or.merged.ReadBytes(n)
	if or.R2000Plus {
		or.merged.ReadRC()
	}
	t.build = func(b *builder) { b.buildOle2Frame(o) }
	return t
}

// buildOle2Frame takes the frame corners from the OLE2 header data
// (CadOle2FrameTemplate).
func (b *builder) buildOle2Frame(o *cadoc.Ole2Frame) {
	d := o.BinaryData
	if len(d) < 2+4*24 {
		if len(d) > 0 {
			b.notify(NotifyWarning, "Error reading OLE2 Frame data for handle %d", o.Handle)
		}
		return
	}
	vec := func(off int) cadoc.Vec3 {
		f := func(i int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(d[off+8*i:])) }
		return cadoc.Vec3{X: f(0), Y: f(1), Z: f(2)}
	}
	o.UpperLeftCorner = vec(2)
	o.LowerRightCorner = vec(2 + 2*24)
}

// ----- proxy, modeler geometry -----

func (or *objectReader) readCommonProxyData() (classNumber int32, c *cadoc.DxfClass, version int16, maint int32, dxf bool, data []byte) {
	classNumber = or.merged.ReadBitLong()
	c = or.classes[int16(classNumber)]
	if or.R2000Plus {
		if or.ver > cadoc.AC1015 {
			or.merged.ReadVariableText()
		}
		if !or.R2018Plus {
			format := or.merged.ReadBitLong()
			version = int16(format & 0xFFFF)
			maint = format >> 16
		} else {
			version = int16(or.merged.ReadBitLong())
			maint = or.merged.ReadBitLong()
		}
		dxf = or.merged.ReadBit()
	} else {
		or.merged.ReadBitLong() // item class id
	}
	if or.handleSectionOffset > 0 {
		if bits := or.handleSectionOffset - or.obj.PositionInBits(); bits > 0 {
			data = or.obj.ReadBytes(int((bits + 7) / 8))
		}
	}
	return
}

func (or *objectReader) readProxyEntity() *tmpl {
	p := cadoc.NewProxyEntity()
	t := or.newEntityTmpl(p)
	or.readCommonEntityData(t)
	n, c, v, m, dxf, data := or.readCommonProxyData()
	p.ClassNumber, p.Class, p.Version, p.MaintenanceVersion, p.OriginalDataFormatDxf = n, c, v, m, dxf
	p.BinaryData = data
	return t
}

func (or *objectReader) readProxyObject() *tmpl {
	o := &cadoc.UnknownObject{DxfName: cadoc.TokenObjectProxyObject}
	t := or.newObjectTmpl(o)
	or.readCommonNonEntityData(t)
	_, c, _, _, _, _ := or.readCommonProxyData()
	o.Class = c
	return t
}

// readModelerGeometry reads the ACIS data of REGION, 3DSOLID, BODY and
// surfaces; wireframe and silhouette data are read and dropped.
func (or *objectReader) readModelerGeometry(t *tmpl, g *cadoc.ModelerGeometry) {
	or.readCommonEntityData(t)
	if !or.R2013Plus {
		// ACadSharp reads the data when this "empty" bit is 0 and stops.
		if !or.merged.ReadBit() {
			or.readModelerGeometryData(g)
			return
		}
	}
	if or.merged.ReadBit() {
		if or.merged.ReadBit() {
			or.obj.Read3BitDouble() // point
		}
		or.merged.ReadBitLong() // isolines
		if or.merged.ReadBit() {
			n := int(or.merged.ReadBitLong())
			for i := 0; i < n; i++ {
				or.readWire()
			}
		}
		n := int(or.merged.ReadBitLong())
		for i := 0; i < n; i++ {
			or.merged.ReadBitLongLong()
			or.merged.Read3BitDouble()
			or.merged.Read3BitDouble()
			or.merged.Read3BitDouble()
			or.merged.ReadBit()
			if or.merged.ReadBit() {
				nw := int(or.merged.ReadBitLong())
				for j := 0; j < nw; j++ {
					or.readWire()
				}
			}
		}
		if !or.merged.ReadBit() {
			or.readModelerGeometryData(g)
		}
	}
	if or.R2007Plus {
		n := int(or.merged.ReadBitLong())
		for i := 0; i < n; i++ {
			or.merged.ReadBitLong()
			or.merged.ReadBitLong()
			or.handleRef() // material
		}
	}
	if or.R2013Plus {
		or.merged.ReadBit()
		g.Guid = or.readGuid()
		or.merged.ReadBitLong()
	}
}

func (or *objectReader) readGuid() string {
	a := uint32(or.merged.ReadBitLong())
	b := uint16(or.merged.ReadBitShort())
	c := uint16(or.merged.ReadBitShort())
	var rest [8]byte
	for i := range rest {
		rest[i] = or.merged.ReadRC()
	}
	return fmt.Sprintf("{%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x}", a, b, c,
		rest[0], rest[1], rest[2], rest[3], rest[4], rest[5], rest[6], rest[7])
}

func (or *objectReader) readWire() {
	or.merged.ReadRC()
	or.merged.ReadBitLong()
	or.merged.ReadBitShort()
	or.merged.ReadBitLong()
	n := int(uint32(or.merged.ReadBitLong()))
	for i := 0; i < n; i++ {
		or.merged.Read3BitDouble()
	}
	if or.merged.ReadBit() {
		or.merged.Read3BitDouble()
		or.merged.Read3BitDouble()
		or.merged.Read3BitDouble()
		or.merged.Read3BitDouble()
		or.merged.ReadBitDouble()
		or.merged.ReadBit()
		or.merged.ReadBit()
		or.merged.ReadBit()
	}
}

// acisEndMarkers end the SAT/SAB payload (AcisTextCodec._endMarkers).
var acisEndMarkers = [][]byte{
	[]byte("End-of-ACIS-data"),
	[]byte("End-of-ASM-data"),
	{0x0E, 0x03, 'E', 'n', 'd', 0x0E, 0x02, 'o', 'f', 0x0E, 0x04, 'A', 'C', 'I', 'S', 0x0D, 0x04, 'd', 'a', 't', 'a'},
	{0x0E, 0x03, 'E', 'n', 'd', 0x0E, 0x02, 'o', 'f', 0x0E, 0x03, 'A', 'S', 'M', 0x0D, 0x04, 'd', 'a', 't', 'a'},
}

func trimAtAcisEnd(data []byte) []byte {
	for _, m := range acisEndMarkers {
		if i := indexBytes(data, m); i >= 0 {
			if end := i + len(m); end < len(data) {
				return data[:end]
			}
			return data
		}
	}
	return data
}

func indexBytes(h, n []byte) int {
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// readModelerGeometryData reads the SAT (version 1, character swapped) or
// SAB (version 2) payload.
func (or *objectReader) readModelerGeometryData(g *cadoc.ModelerGeometry) {
	or.merged.ReadBit()
	version := or.merged.ReadBitShort()
	switch version {
	case 1:
		var sat []byte
		for n := or.merged.ReadBitLong(); n > 0; n = or.merged.ReadBitLong() {
			for _, c := range or.merged.ReadBytes(int(n)) {
				if c > 0x20 && c < 0x9F {
					c = 0x9F - c
				}
				sat = append(sat, c)
			}
		}
		if len(sat) > 0 {
			g.AcisText = string(sat)
		}
	case 2:
		// No length is stored: take the rest of the object data and cut it
		// at the end marker.
		end := or.objectInitialPos + int64(or.size)*8
		if n := int((end - or.merged.PositionInBits()) / 8); n > 0 {
			data := trimAtAcisEnd(or.merged.ReadBytes(n))
			if isSAB(data) {
				g.AcisData = data
			} else {
				g.AcisText = string(data)
			}
		}
	default:
		or.notify(NotifyWarning, "Modeler geometry data version %d not recognized", version)
	}
}

func isSAB(d []byte) bool {
	return len(d) >= 15 && string(d[:15]) == "ACIS BinaryFile" || len(d) >= 14 && string(d[:14]) == "ASM BinaryFile"
}

func (or *objectReader) readRegion() *tmpl {
	e := cadoc.NewRegion()
	t := or.newEntityTmpl(e)
	or.readModelerGeometry(t, &e.ModelerGeometry)
	return t
}

func (or *objectReader) readBody() *tmpl {
	e := cadoc.NewCadBody()
	t := or.newEntityTmpl(e)
	or.readModelerGeometry(t, &e.ModelerGeometry)
	return t
}

func (or *objectReader) readSolid3D() *tmpl {
	e := cadoc.NewSolid3D()
	t := or.newEntityTmpl(e)
	or.readModelerGeometry(t, &e.ModelerGeometry)
	var history uint64
	if or.R2007Plus {
		history = or.merged.HandleReference(0)
	}
	t.build = func(b *builder) {
		// ACadSharp keeps the history handle in the template only.
		if h := b.object(history); h != nil {
			e.History = h
		}
	}
	return t
}

func (or *objectReader) readSurface(dxfName string) *tmpl {
	e := &cadoc.Surface{DxfName: dxfName}
	e.ModelerFormatVersion = 1
	e.InitEntity()
	t := or.newEntityTmpl(e)
	// The surface data after the ACIS data is not decoded (as in ACadSharp).
	or.readModelerGeometry(t, &e.ModelerGeometry)
	return t
}

// ----- table entity -----

// readTableEntity reads ACAD_TABLE: the block reference part and, before
// R2010, the row and column sizes and the cell texts.
func (or *objectReader) readTableEntity() *tmpl {
	te := cadoc.NewTableEntity()
	t := or.newEntityTmpl(te)
	it := &insertTmpl{}
	or.readInsertCommonData(t, &te.Insert, it)
	or.readInsertCommonHandles(it)
	var style uint64
	t.build = func(b *builder) {
		b.buildInsert(t, &te.Insert, it)
		te.TableBlock = te.Block
		if s := b.object(style); s != nil {
			te.Style = s
		}
	}
	if or.R2010Plus {
		m := or.merged
		m.ReadRC()
		or.handleRef()
		m.ReadBitLong()
		if or.R2013Plus {
			m.ReadBitLong()
		} else {
			m.ReadBit()
		}
		// The table content; what follows (direction, break data) is not
		// needed once the content is read, and a content the reader can't
		// decode leaves the rows and cells empty: the anonymous block still
		// draws the table.
		func() {
			defer func() {
				if p := recover(); p != nil {
					or.notify(NotifyWarning, "Table content of %d could not be read: %v", te.Handle, p)
				}
			}()
			style = or.readTableContent(te)
			m.ReadBitShort()
			te.HorizontalDirection = m.Read3BitDouble()
		}()
		return t
	}
	te.ValueFlag = int32(or.merged.ReadBitShort())
	te.HorizontalDirection = or.merged.Read3BitDouble()
	ncols := int(or.merged.ReadBitLong())
	nrows := int(or.merged.ReadBitLong())
	for i := 0; i < ncols; i++ {
		te.Columns = append(te.Columns, cadoc.TableColumn{Width: or.merged.ReadBitDouble()})
	}
	for i := 0; i < nrows; i++ {
		te.Rows = append(te.Rows, cadoc.TableRow{Height: or.merged.ReadBitDouble()})
	}
	style = or.handleRef()
	func() {
		// Keep what was read when a cell can't be decoded.
		defer func() {
			if p := recover(); p != nil {
				or.notify(NotifyWarning, "Table cells of %d could not be read: %v", te.Handle, p)
			}
		}()
		for n := 0; n < nrows; n++ {
			for m := 0; m < ncols; m++ {
				te.Cells = append(te.Cells, or.readTableCellData())
			}
		}
	}()
	return t
}

// readTableCellData reads one pre-R2010 table cell (readTableCellData); the
// model keeps the type, flags, rotation and text.
func (or *objectReader) readTableCellData() cadoc.TableCell {
	m := or.merged
	c := cadoc.TableCell{}
	c.Type = cadoc.TableCellType(m.ReadBitShort())
	c.EdgeFlags = int16(m.ReadRC())
	if m.ReadBit() {
		c.Merged = 1
	}
	c.AutoFit = m.ReadBit()
	m.ReadBitLong() // merged width
	m.ReadBitLong() // merged height
	c.Rotation = m.ReadBitDouble()
	value := or.handleRef()
	switch {
	case c.Type == cadoc.TableCellTypeText && value == 0 && or.ver < cadoc.AC1021:
		c.Text = m.ReadVariableText()
	case c.Type == cadoc.TableCellTypeBlock:
		m.ReadBitDouble() // block scale
		if m.ReadBit() {
			n := int(m.ReadBitShort())
			for i := 0; i < n; i++ {
				or.handleRef()
				m.ReadBitShort()
				m.ReadVariableText()
			}
		}
	}
	if m.ReadBit() {
		flags := cadoc.TableCellOverrideFlags(m.ReadBitLong())
		m.ReadRC() // virtual edge flags
		has := func(f cadoc.TableCellOverrideFlags) bool { return flags&f != 0 }
		if has(cadoc.TableCellOverrideFlagsCellAlignment) {
			m.ReadBitShort()
		}
		if has(cadoc.TableCellOverrideFlagsBackgroundFillNone) {
			m.ReadBit()
		}
		if has(cadoc.TableCellOverrideFlagsBackgroundColor) {
			m.ReadCmColor(or.R2004Pre)
		}
		if has(cadoc.TableCellOverrideFlagsContentColor) {
			m.ReadCmColor(or.R2004Pre)
		}
		if has(cadoc.TableCellOverrideFlagsTextStyle) {
			or.handleRef()
		}
		if has(cadoc.TableCellOverrideFlagsTextHeight) {
			m.ReadBitDouble()
		}
		// The visibility values follow their own flags (ACadSharp tests the
		// line weight flags for them).
		for _, g := range [][3]cadoc.TableCellOverrideFlags{
			{cadoc.TableCellOverrideFlagsTopGridColor, cadoc.TableCellOverrideFlagsTopGridLineWeight, cadoc.TableCellOverrideFlagsTopVisibility},
			{cadoc.TableCellOverrideFlagsRightGridColor, cadoc.TableCellOverrideFlagsRightGridLineWeight, cadoc.TableCellOverrideFlagsRightVisibility},
			{cadoc.TableCellOverrideFlagsBottomGridColor, cadoc.TableCellOverrideFlagsBottomGridLineWeight, cadoc.TableCellOverrideFlagsBottomVisibility},
			{cadoc.TableCellOverrideFlagsLeftGridColor, cadoc.TableCellOverrideFlagsLeftGridLineWeight, cadoc.TableCellOverrideFlagsLeftVisibility},
		} {
			if has(g[0]) {
				m.ReadCmColor(or.R2004Pre)
			}
			if has(g[1]) {
				m.ReadBitShort()
			}
			if has(g[2]) {
				m.ReadBitShort()
			}
		}
	}
	if or.R2007Plus {
		m.ReadBitLong()
		v, _ := or.readCadValue()
		if s, ok := v.Value.(string); ok && c.Text == "" {
			c.Text = s
		}
	}
	return c
}

// ----- R2010+ table content -----

// readTableContent reads the TABLECONTENT data of an R2010+ table into
// rows, columns and cell texts; it returns the table style handle.
func (or *objectReader) readTableContent(te *cadoc.TableEntity) uint64 {
	m := or.merged
	m.ReadVariableText() // name
	m.ReadVariableText() // description
	cols := int(m.ReadBitLong())
	for i := 0; i < cols; i++ {
		m.ReadVariableText()
		m.ReadBitLong()
		n := int(m.ReadBitLong())
		for j := 0; j < n; j++ {
			or.readCustomTableData()
		}
		or.readCellStyle()
		m.ReadBitLong() // style id
		te.Columns = append(te.Columns, cadoc.TableColumn{Width: m.ReadBitDouble()})
	}
	rows := int(m.ReadBitLong())
	var cells []cadoc.TableCell
	for i := 0; i < rows; i++ {
		n := int(m.ReadBitLong())
		for j := 0; j < n; j++ {
			cells = append(cells, or.readTableCell())
		}
		m.ReadBitLong()
		n = int(m.ReadBitLong())
		for j := 0; j < n; j++ {
			or.readCustomTableData()
		}
		or.readCellStyle()
		m.ReadBitLong() // style id
		te.Rows = append(te.Rows, cadoc.TableRow{Height: m.ReadBitDouble()})
	}
	te.Cells = cells
	n := int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		m.HandleReference(0) // field
	}
	or.readCellStyle()
	n = int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		// Merged cell range: top, left, bottom, right.
		m.ReadBitLong()
		m.ReadBitLong()
		m.ReadBitLong()
		m.ReadBitLong()
	}
	return or.handleRef()
}

func (or *objectReader) readCustomTableData() {
	or.merged.ReadVariableText()
	or.readCadValue()
}

func (or *objectReader) readTableCell() cadoc.TableCell {
	m := or.merged
	c := cadoc.TableCell{Type: cadoc.TableCellTypeText}
	m.ReadBitLong()      // state flags
	m.ReadVariableText() // tool tip
	m.ReadBitLong()      // custom data
	n := int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		or.readCustomTableData()
	}
	if m.ReadBitLong() == 1 {
		// Linked data.
		m.HandleReference(0)
		m.ReadBitLong()
		m.ReadBitLong()
		m.ReadBitLong()
	}
	n = int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		typ, text := or.readTableCellContent()
		if typ == cadoc.TableTableCellContentTypeBlock {
			c.Type = cadoc.TableCellTypeBlock
		}
		if c.Text == "" {
			c.Text = text
		}
	}
	or.readCellStyle()
	m.ReadBitLong() // style id
	if m.ReadBitLong() != 0 {
		m.ReadBitLong()
		m.ReadBitDouble()
		m.ReadBitDouble()
		geomFlags := m.ReadBitLong()
		or.handleRef()
		if geomFlags != 0 {
			m.Read3BitDouble()
			m.Read3BitDouble()
			m.ReadBitDouble()
			m.ReadBitDouble()
			m.ReadBitDouble()
			m.ReadBitDouble()
			m.ReadBitLong()
		}
	}
	return c
}

func (or *objectReader) readTableCellContent() (cadoc.TableTableCellContentType, string) {
	m := or.merged
	typ := cadoc.TableTableCellContentType(m.ReadBitLong())
	text := ""
	switch typ {
	case cadoc.TableTableCellContentTypeValue:
		v, _ := or.readCadValue()
		if s, ok := v.Value.(string); ok {
			text = s
		} else {
			text = v.FormattedValue
		}
	case cadoc.TableTableCellContentTypeField, cadoc.TableTableCellContentTypeBlock:
		or.handleRef()
	}
	n := int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		or.handleRef()
		m.ReadVariableText()
		m.ReadBitLong()
	}
	if m.ReadBitShortAsBool() {
		or.readCellContentFormat()
	}
	return typ, text
}

func (or *objectReader) readCellContentFormat() {
	m := or.merged
	m.ReadBitLong()
	m.ReadBitLong()
	m.ReadBitLong()
	m.ReadBitLong()
	m.ReadVariableText()
	m.ReadBitDouble()
	m.ReadBitDouble()
	m.ReadBitLong()
	m.ReadCmColor(or.R2004Pre)
	or.handleRef() // text style
	m.ReadBitDouble()
}

// readCellStyle reads (and drops) a table cell style.
func (or *objectReader) readCellStyle() {
	m := or.merged
	m.ReadBitLong() // type
	if !m.ReadBitShortAsBool() {
		return
	}
	m.ReadBitLong()
	m.ReadBitLong()
	m.ReadCmColor(or.R2004Pre)
	m.ReadBitLong()
	or.readCellContentFormat()
	if cadoc.TableStyleMarginFlags(m.ReadBitShort())&cadoc.TableStyleMarginFlagsOverride != 0 {
		for i := 0; i < 6; i++ {
			m.ReadBitDouble()
		}
	}
	n := int(m.ReadBitLong())
	for i := 0; i < n; i++ {
		switch cadoc.TableStyleCellEdgeFlags(m.ReadBitLong()) {
		case cadoc.TableStyleCellEdgeFlagsTop, cadoc.TableStyleCellEdgeFlagsRight, cadoc.TableStyleCellEdgeFlagsBottom,
			cadoc.TableStyleCellEdgeFlagsLeft, cadoc.TableStyleCellEdgeFlagsInsideVertical, cadoc.TableStyleCellEdgeFlagsInsideHorizontal:
			m.ReadBitLong()
			m.ReadBitLong()
			m.ReadCmColor(or.R2004Pre)
			m.ReadBitLong()
			or.handleRef() // line type
			m.ReadBitLong()
			m.ReadBitDouble()
		}
	}
}
