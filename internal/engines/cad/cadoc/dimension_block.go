package cadoc

import (
	"errors"
	"math"
	"strings"
)

// ActiveStyle is GetActiveDimensionStyle: the dimension's style (the
// document's Standard style when it has none) with the DSTYLE overrides of
// its ACAD xdata applied to a copy.
func (d *DimensionBase) ActiveStyle(doc *Document) *DimensionStyle {
	style := d.Style
	if style == nil && doc != nil {
		style, _ = doc.DimensionStyles.Get(DefaultDimStyleName)
	}
	if style == nil {
		style = NewDimensionStyle(DefaultDimStyleName)
	}
	if !d.HasStyleOverride() {
		return style
	}
	x, _ := d.GetExtendedData(DefaultAppIdName)
	clone := style.Clone()
	clone.Name = "override"
	var index map[uint64]Object
	recs := x.Records
	i := 1
	for i < len(recs) && !(recs[i].Code == 1002 && recs[i].String() == "{") {
		i++
	}
	for i++; i+1 < len(recs); i += 2 {
		if recs[i].Code == 1002 {
			break
		}
		code := recs[i]
		if code.Code != 1070 {
			break
		}
		v := recs[i+1]
		if v.Code == 1005 && index == nil && doc != nil {
			index = doc.Index()
		}
		applyDimStyleCode(clone, int(code.Int16()), v, index)
	}
	return clone
}

// applyDimStyleCode sets the DIMSTYLE field of a group code from an xdata
// override value.
func applyDimStyleCode(s *DimensionStyle, code int, r ExtendedDataRecord, index map[uint64]Object) {
	f := func() float64 {
		switch v := r.Value.(type) {
		case float64:
			return v
		case int16:
			return float64(v)
		case int32:
			return float64(v)
		}
		return 0
	}
	n := func() int16 { return int16(math.Round(f())) }
	b := func() bool { return f() != 0 }
	obj := func() Object {
		if index == nil {
			return nil
		}
		return index[r.Handle()]
	}
	switch code {
	case 3:
		s.PostFix = r.String()
	case 4:
		s.AlternateDimensioningSuffix = r.String()
	case 40:
		s.ScaleFactor = f()
	case 41:
		s.ArrowSize = f()
	case 42:
		s.ExtensionLineOffset = f()
	case 43:
		s.DimensionLineIncrement = f()
	case 44:
		s.ExtensionLineExtension = f()
	case 45:
		s.Rounding = f()
	case 46:
		s.DimensionLineExtension = f()
	case 47:
		s.PlusTolerance = f()
	case 48:
		s.MinusTolerance = f()
	case 49:
		s.FixedExtensionLineLength = f()
	case 50:
		s.JoggedRadiusDimensionTransverseSegmentAngle = f()
	case 69:
		s.TextBackgroundFillMode = DimensionTextBackgroundFillMode(n())
	case 70:
		s.TextBackgroundColor = ColorIndex(n())
	case 71:
		s.GenerateTolerances = b()
	case 72:
		s.LimitsGeneration = b()
	case 73:
		s.TextInsideHorizontal = b()
	case 74:
		s.TextOutsideHorizontal = b()
	case 75:
		s.SuppressFirstExtensionLine = b()
	case 76:
		s.SuppressSecondExtensionLine = b()
	case 77:
		s.TextVerticalAlignment = DimensionTextVerticalAlignment(n())
	case 78:
		s.ZeroHandling = ZeroHandling(n())
	case 79:
		s.AngularZeroHandling = AngularZeroHandling(n())
	case 90:
		s.ArcLengthSymbolPosition = ArcLengthSymbolPosition(n())
	case 140:
		s.TextHeight = f()
	case 141:
		s.CenterMarkSize = f()
	case 142:
		s.TickSize = f()
	case 143:
		s.AlternateUnitScaleFactor = f()
	case 144:
		s.LinearScaleFactor = f()
	case 145:
		s.TextVerticalPosition = f()
	case 146:
		s.ToleranceScaleFactor = f()
	case 147:
		s.DimensionLineGap = f()
	case 148:
		s.AlternateUnitRounding = f()
	case 170:
		s.AlternateUnitDimensioning = b()
	case 171:
		s.AlternateUnitDecimalPlaces = n()
	case 172:
		s.TextOutsideExtensions = b()
	case 173:
		s.SeparateArrowBlocks = b()
	case 174:
		s.TextInsideExtensions = b()
	case 175:
		s.SuppressOutsideExtensions = b()
	case 176:
		s.DimensionLineColor = ColorIndex(n())
	case 177:
		s.ExtensionLineColor = ColorIndex(n())
	case 178:
		s.TextColor = ColorIndex(n())
	case 179:
		s.AngularDecimalPlaces = n()
	case 270:
		s.DimensionUnit = n()
	case 271:
		s.DecimalPlaces = n()
	case 272:
		s.ToleranceDecimalPlaces = n()
	case 273:
		s.AlternateUnitFormat = LinearUnitFormat(n())
	case 274:
		s.AlternateUnitToleranceDecimalPlaces = n()
	case 275:
		s.AngularUnit = AngularUnitFormat(n())
	case 276:
		s.FractionFormat = FractionFormat(n())
	case 277:
		s.LinearUnitFormat = LinearUnitFormat(n())
	case 278:
		s.DecimalSeparator = rune(n())
	case 279:
		s.TextMovement = TextMovement(n())
	case 280:
		s.TextHorizontalAlignment = DimensionTextHorizontalAlignment(n())
	case 281:
		s.SuppressFirstDimensionLine = b()
	case 282:
		s.SuppressSecondDimensionLine = b()
	case 283:
		s.ToleranceAlignment = ToleranceAlignment(n())
	case 284:
		s.ToleranceZeroHandling = ZeroHandling(n())
	case 285:
		s.AlternateUnitZeroHandling = ZeroHandling(n())
	case 286:
		s.AlternateUnitToleranceZeroHandling = ZeroHandling(n())
	case 287:
		s.DimensionFit = n()
	case 288:
		s.CursorUpdate = b()
	case 289:
		s.DimensionTextArrowFit = TextArrowFitType(n())
	case 290:
		s.IsExtensionLineLengthFixed = b()
	case 294:
		s.TextDirection = TextDirection(n())
	case 340:
		if t, ok := obj().(*TextStyle); ok {
			s.Style = t
		}
	case 341:
		s.LeaderArrow, _ = obj().(*BlockRecord)
	case 342:
		s.ArrowBlock, _ = obj().(*BlockRecord)
	case 343:
		s.DimArrow1, _ = obj().(*BlockRecord)
	case 344:
		s.DimArrow2, _ = obj().(*BlockRecord)
	case 345:
		s.LineType, _ = obj().(*LineType)
	case 346:
		s.LineTypeExt1, _ = obj().(*LineType)
	case 347:
		s.LineTypeExt2, _ = obj().(*LineType)
	case 371:
		s.DimensionLineWeight = LineWeight(n())
	case 372:
		s.ExtensionLineWeight = LineWeight(n())
	}
}

// MeasurementText is GetMeasurementText: the measured value formatted with
// the active style, with prefix/suffix and the Text override ("<>" stands
// for the value).
func MeasurementText(doc *Document, d Dimension) (string, error) {
	m, err := d.Measurement()
	if err != nil {
		return "", err
	}
	base := d.Dimension()
	return measurementText(base.ActiveStyle(doc), d, m), nil
}

func measurementText(style *DimensionStyle, d Dimension, measurement float64) string {
	base := d.Dimension()
	value := style.ApplyRounding(measurement, false)
	f := style.UnitStyleFormat()
	_, isArc := d.(*DimensionArc)
	var text string
	if base.IsAngular() && !isArc {
		switch style.AngularUnit {
		case AngularUnitFormatDecimalDegrees:
			text = f.ToDegrees(value)
		case AngularUnitFormatDegreesMinutesSeconds:
			text = f.ToDegreesMinutesSeconds(value)
		case AngularUnitFormatGradians:
			text = f.ToGradians(value)
		case AngularUnitFormatRadians:
			text = f.ToRadians(value)
		default:
			text = f.ToDecimal(value, true)
		}
	} else {
		v := value * style.LinearScaleFactor
		switch style.LinearUnitFormat {
		case LinearUnitFormatScientific:
			text = f.ToScientific(v)
		case LinearUnitFormatEngineering:
			text = f.ToEngineering(v)
		case LinearUnitFormatArchitectural:
			text = f.ToArchitectural(v)
		case LinearUnitFormatFractional:
			text = f.ToFractional(v)
		default:
			text = f.ToDecimal(v, false)
		}
	}
	prefix := style.Prefix()
	if prefix == "" {
		switch base.Kind() {
		case DimensionTypeDiameter:
			prefix = "Ø"
		case DimensionTypeRadius:
			prefix = "R"
		}
	}
	valueText := prefix + text + style.Suffix()
	if base.Text != "" {
		return strings.ReplaceAll(base.Text, "<>", valueText)
	}
	return valueText
}

// UpdateDimensionBlock regenerates the anonymous block (*Dn) that draws a
// dimension (Dimension.UpdateBlock). Angular two-line dimensions are left
// alone, as in ACadSharp; angular three-point and arc dimensions get an empty
// block.
func UpdateDimensionBlock(doc *Document, d Dimension) error {
	if doc == nil {
		return errors.New("cadoc: UpdateDimensionBlock needs a document")
	}
	if _, ok := d.(*DimensionAngular2Line); ok {
		return nil
	}
	if _, err := d.Measurement(); err != nil {
		return err
	}
	b := newDimBuilder(doc, d)
	switch x := d.(type) {
	case *DimensionLinear:
		return b.linear(x)
	case *DimensionAligned:
		return b.aligned(x)
	case *DimensionRadius:
		return b.radius(x)
	case *DimensionDiameter:
		return b.diameter(x)
	case *DimensionOrdinate:
		return b.ordinate(x)
	}
	return nil
}

func (d *DimensionAligned) UpdateBlock(doc *Document) error      { return UpdateDimensionBlock(doc, d) }
func (d *DimensionLinear) UpdateBlock(doc *Document) error       { return UpdateDimensionBlock(doc, d) }
func (d *DimensionRadius) UpdateBlock(doc *Document) error       { return UpdateDimensionBlock(doc, d) }
func (d *DimensionDiameter) UpdateBlock(doc *Document) error     { return UpdateDimensionBlock(doc, d) }
func (d *DimensionOrdinate) UpdateBlock(doc *Document) error     { return UpdateDimensionBlock(doc, d) }
func (d *DimensionAngular3Pt) UpdateBlock(doc *Document) error   { return UpdateDimensionBlock(doc, d) }
func (d *DimensionAngular2Line) UpdateBlock(doc *Document) error { return UpdateDimensionBlock(doc, d) }
func (d *DimensionArc) UpdateBlock(doc *Document) error          { return UpdateDimensionBlock(doc, d) }

type dimBuilder struct {
	doc       *Document
	dim       Dimension
	d         *DimensionBase
	style     *DimensionStyle
	block     *BlockRecord
	layer0    *Layer
	defpoints *Layer
	byLayer   *LineType
}

func newDimBuilder(doc *Document, dim Dimension) *dimBuilder {
	d := dim.Dimension()
	b := &dimBuilder{doc: doc, dim: dim, d: d, style: d.ActiveStyle(doc)}
	b.layer0, _ = doc.Layers.Get(DefaultLayerName)
	if b.layer0 == nil {
		b.layer0 = NewLayer(DefaultLayerName)
		_ = doc.Layers.Add(b.layer0)
	}
	b.defpoints, _ = doc.Layers.Get(DefpointsLayerName)
	if b.defpoints == nil {
		b.defpoints = NewLayer(DefpointsLayerName)
		b.defpoints.PlotFlag = false
		b.defpoints.LineType, _ = doc.LineTypes.Get(ContinuousName)
		_ = doc.Layers.Add(b.defpoints)
	}
	b.byLayer, _ = doc.LineTypes.Get(ByLayerName)
	if b.byLayer == nil {
		b.byLayer = NewLineType(ByLayerName)
		_ = doc.LineTypes.Add(b.byLayer)
	}
	b.createBlock()
	return b
}

// createBlock creates or clears the dimension's block record.
func (b *dimBuilder) createBlock() {
	d, doc := b.d, b.doc
	r := d.Block
	if r == nil {
		r = NewBlockRecord(doc.UniqueBlockName("*D"))
		r.SetFlags(r.Flags() | BlockTypeFlagsAnonymous)
	}
	if existing, ok := doc.BlockRecords.Get(r.Name); !ok {
		_ = doc.BlockRecords.Add(r)
	} else if existing != r {
		r.Name = doc.UniqueBlockName("*D")
		_ = doc.BlockRecords.Add(r)
	}
	r.RemoveEntities(func(Entity) bool { return true })
	d.Block = r
	b.block = r
}

func (b *dimBuilder) add(e Entity) {
	eb := e.Entity()
	if eb.Layer == nil {
		eb.Layer = b.layer0
	}
	if eb.LineType == nil {
		eb.LineType = b.byLayer
	}
	b.block.AddEntity(e)
}

func (b *dimBuilder) lineType(lt *LineType) *LineType {
	if lt == nil {
		return b.byLayer
	}
	return lt
}

func (b *dimBuilder) point(p Vec3) {
	e := NewPoint()
	e.Location = p
	e.Layer = b.defpoints
	b.add(e)
}

func (b *dimBuilder) dimensionLine(start, end Vec3) {
	l := NewLine()
	l.StartPoint, l.EndPoint = start, end
	l.Color, l.LineType, l.LineWeight = b.style.DimensionLineColor, b.lineType(b.style.LineType), b.style.DimensionLineWeight
	b.add(l)
}

func (b *dimBuilder) extensionLine(start, end Vec3, lt *LineType) {
	l := NewLine()
	l.StartPoint, l.EndPoint = start, end
	l.Color, l.LineType, l.LineWeight = b.style.ExtensionLineColor, b.lineType(lt), b.style.ExtensionLineWeight
	b.add(l)
}

func (b *dimBuilder) plainLine(start, end Vec3) {
	l := NewLine()
	l.StartPoint, l.EndPoint = start, end
	b.add(l)
}

// arrow is dimensionArrow: a filled triangle, or an insert of the arrow block.
func (b *dimBuilder) arrow(at, dir Vec3, record *BlockRecord) {
	s := b.style
	scale := s.ArrowSize * s.ScaleFactor
	if record == nil {
		p := b.d.Normal.Cross(dir).Normalized()
		a := NewSolid()
		a.FirstCorner = at
		a.SecondCorner = at.Sub(dir.Scale(scale)).Sub(p.Scale(scale / 6))
		a.ThirdCorner = at.Sub(dir.Scale(scale)).Add(p.Scale(scale / 6))
		a.FourthCorner = a.ThirdCorner
		b.add(a)
		return
	}
	in := NewInsert(record)
	in.InsertPoint = at
	in.Color = s.DimensionLineColor
	in.XScale, in.YScale, in.ZScale = scale, scale, scale
	in.Rotation = math.Atan2(dir.Y, dir.X)
	in.LineWeight = s.DimensionLineWeight
	in.Normal = b.d.Normal
	b.add(in)
}

func (b *dimBuilder) text(at Vec3, value string, attach AttachmentPointType) {
	m := NewMText()
	m.Value = value
	m.AttachmentPoint = attach
	m.InsertPoint = at
	m.Height = b.style.TextHeight
	m.Style = b.style.Style
	if m.Style == nil {
		m.Style, _ = b.doc.TextStyles.Get(DefaultTextStyleName)
	}
	b.add(m)
}

func (b *dimBuilder) measurementText() string {
	m, _ := b.dim.Measurement()
	return measurementText(b.style, b.dim, m)
}

func sign(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func (b *dimBuilder) aligned(x *DimensionAligned) error {
	s := b.style
	dir := x.SecondPoint.Sub(x.FirstPoint).Normalized()
	vec := x.Normal.Cross(dir).Normalized()
	// Signed, so that a definition point on either side works.
	offset := x.DefinitionPoint.Sub(x.SecondPoint).Dot(vec)
	dimRef1 := x.FirstPoint.Add(vec.Scale(offset))
	dimRef2 := x.DefinitionPoint
	b.point(x.FirstPoint)
	b.point(x.SecondPoint)
	b.point(dimRef1)
	b.point(dimRef2)
	if !s.SuppressFirstDimensionLine && !s.SuppressSecondDimensionLine {
		b.dimensionLine(dimRef1, dimRef2)
		b.arrow(dimRef1, dir.Scale(-1), s.DimArrow1)
		b.arrow(dimRef2, dir, s.DimArrow2)
	}
	exo := sign(offset) * s.ExtensionLineOffset * s.ScaleFactor
	exe := sign(offset) * s.ExtensionLineExtension * s.ScaleFactor
	if !s.SuppressFirstExtensionLine {
		b.extensionLine(x.FirstPoint.Add(vec.Scale(exo)), dimRef1.Add(vec.Scale(exe)), s.LineTypeExt1)
	}
	if !s.SuppressSecondExtensionLine {
		b.extensionLine(x.SecondPoint.Add(vec.Scale(exo)), dimRef2.Add(vec.Scale(exe)), s.LineTypeExt2)
	}
	textRef := dimRef1.Add(dimRef2).Scale(0.5)
	gap := s.DimensionLineGap * s.ScaleFactor
	if r := x.ExtLineRotation; r > math.Pi/2 && r <= 1.5*math.Pi {
		gap = -gap
	}
	text := b.measurementText()
	if !x.IsTextUserDefinedLocation() {
		x.TextMiddlePoint = textRef.Add(vec.Scale(gap))
	}
	b.text(x.TextMiddlePoint, text, AttachmentPointTypeMiddleCenter)
	return nil
}

func (b *dimBuilder) linear(x *DimensionLinear) error {
	s := b.style
	yVec := rotateAround(AxisY, x.Normal, x.Rotation).Normalized()
	xVec := rotateAround(AxisX, x.Normal, x.Rotation).Normalized()
	dimRef1, ok := lineIntersection(x.FirstPoint, yVec, x.DefinitionPoint, xVec)
	if !ok {
		return ErrDegenerateDimension
	}
	dimRef2 := x.DefinitionPoint
	dir := dimRef2.Sub(dimRef1).Normalized()
	b.point(x.FirstPoint)
	b.point(x.SecondPoint)
	b.point(dimRef1)
	b.point(dimRef2)
	if !s.SuppressFirstDimensionLine && !s.SuppressSecondDimensionLine {
		b.dimensionLine(dimRef1, dimRef2)
		b.arrow(dimRef1, dir.Scale(-1), s.DimArrow1)
		b.arrow(dimRef2, dir, s.DimArrow2)
	}
	dirRef1 := dimRef1.Sub(x.FirstPoint).Normalized()
	dirRef2 := dimRef2.Sub(x.SecondPoint).Normalized()
	exo := s.ExtensionLineOffset * s.ScaleFactor
	exe := s.ExtensionLineExtension * s.ScaleFactor
	if !s.SuppressFirstExtensionLine {
		b.extensionLine(x.FirstPoint.Add(dirRef1.Scale(exo)), dimRef1.Add(dirRef1.Scale(exe)), s.LineTypeExt1)
	}
	if !s.SuppressSecondExtensionLine {
		b.extensionLine(x.SecondPoint.Add(dirRef2.Scale(exo)), dimRef2.Add(dirRef2.Scale(exe)), s.LineTypeExt2)
	}
	textRef := dimRef1.Add(dimRef2).Scale(0.5)
	gap := s.DimensionLineGap * s.ScaleFactor
	if r := x.Rotation; r > math.Pi/2 && r <= 1.5*math.Pi {
		gap = -gap
	}
	text := b.measurementText()
	if !x.IsTextUserDefinedLocation() {
		x.TextMiddlePoint = textRef.Add(yVec.Scale(gap))
	}
	b.text(x.TextMiddlePoint, text, AttachmentPointTypeMiddleCenter)
	return nil
}

func polar(p Vec2, dist, angle float64) Vec2 {
	return Vec2{p.X + dist*math.Cos(angle), p.Y + dist*math.Sin(angle)}
}

// angleTo is XY.GetAngle: the direction from p to q in [0, 2π).
func angleTo(p, q Vec2) float64 {
	a := math.Atan2(q.Y-p.Y, q.X-p.X)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

func rotate2(v Vec2, angle float64) Vec2 {
	s, c := math.Sin(angle), math.Cos(angle)
	return Vec2{v.X*c - v.Y*s, v.X*s + v.Y*c}
}

func v3(p Vec2) Vec3 { return Vec3{p.X, p.Y, 0} }

// radialLine is dimensionRadialLine: the line shortened by the arrow size.
func (b *dimBuilder) radialLine(start, end Vec2, rotation float64, reversed float64) {
	ext := -b.style.ArrowSize * b.style.ScaleFactor
	end = polar(end, reversed*ext, rotation)
	b.dimensionLine(v3(start), v3(end))
}

func (b *dimBuilder) centerCross(center Vec3, radius float64) {
	s := b.style
	if s.CenterMarkSize == 0 {
		return
	}
	dist := math.Abs(s.CenterMarkSize * s.ScaleFactor)
	line := func(p, q Vec3) {
		l := NewLine()
		l.StartPoint, l.EndPoint = center.Add(p), center.Add(q)
		l.Color, l.LineWeight = s.ExtensionLineColor, s.ExtensionLineWeight
		b.add(l)
	}
	line(Vec3{0, -dist, 0}, Vec3{0, dist, 0})
	line(Vec3{-dist, 0, 0}, Vec3{dist, 0, 0})
	if s.CenterMarkSize < 0 {
		line(Vec3{2 * dist, 0, 0}, Vec3{radius + dist, 0, 0})
		line(Vec3{-2 * dist, 0, 0}, Vec3{-radius - dist, 0, 0})
		line(Vec3{0, 2 * dist, 0}, Vec3{0, radius + dist, 0})
		line(Vec3{0, -2 * dist, 0}, Vec3{0, -radius - dist, 0})
	}
}

// insideOffset classifies the text distance against the circle (shared by
// the radial kinds).
func insideOffset(offset, radius, minOffset float64) (float64, float64) {
	switch {
	case offset >= radius && offset <= radius+minOffset:
		return radius + minOffset, -1
	case offset >= radius-minOffset && offset <= radius:
		return radius - minOffset, 1
	case offset > radius:
		return offset, -1
	}
	return offset, 1
}

func (b *dimBuilder) radialText(ref Vec2, angleRef, inside float64) {
	s := b.style
	textRot := angleRef
	reverse := 1.0
	if textRot > math.Pi/2 && textRot <= 1.5*math.Pi {
		textRot += math.Pi
		reverse = -1
	}
	if !b.d.IsTextUserDefinedLocation() {
		b.d.TextMiddlePoint = v3(polar(ref, -reverse*inside*s.DimensionLineGap*s.ScaleFactor, textRot))
	}
	attach := AttachmentPointTypeMiddleRight
	if reverse*inside < 0 {
		attach = AttachmentPointTypeMiddleLeft
	}
	b.text(b.d.TextMiddlePoint, b.measurementText(), attach)
}

// angularBlock is the shared picture of radius dimensions.
func (b *dimBuilder) angularBlock(radius float64, centerRef, ref1 Vec2, minOffset float64, drawRef2 bool) {
	s := b.style
	d := b.d
	offset := d.DefinitionPoint.DistanceTo(d.TextMiddlePoint)
	defPoint := d.DefinitionPoint.XY()
	angleRef := angleTo(centerRef, ref1)
	offset, inside := insideOffset(offset, radius, minOffset)
	dimRef := polar(centerRef, offset-s.DimensionLineGap*s.ScaleFactor, angleRef)
	b.point(v3(ref1))
	if !s.SuppressFirstDimensionLine && !s.SuppressSecondDimensionLine {
		if inside > 0 {
			b.radialLine(dimRef, ref1, angleRef, inside)
		} else {
			b.dimensionLine(v3(defPoint), v3(ref1))
			b.radialLine(dimRef, ref1, angleRef, inside)
			if drawRef2 {
				dimRef2 := polar(centerRef, radius+minOffset-s.DimensionLineGap*s.ScaleFactor, math.Pi+angleRef)
				b.radialLine(dimRef2, defPoint, math.Pi+angleRef, inside)
			}
		}
	}
	b.centerCross(v3(centerRef), radius)
	b.radialText(dimRef, angleRef, inside)
}

func (b *dimBuilder) radius(x *DimensionRadius) error {
	m, _ := x.Measurement()
	minOffset := 2 * b.style.ArrowSize * b.style.ScaleFactor
	b.angularBlock(m, x.DefinitionPoint.XY(), x.AngleVertex.XY(), minOffset, false)
	return nil
}

func (b *dimBuilder) diameter(x *DimensionDiameter) error {
	s := b.style
	m, _ := x.Measurement()
	offset := x.DefinitionPoint.DistanceTo(x.TextMiddlePoint)
	radius := m / 2
	ref1 := x.AngleVertex.XY()
	defPoint := x.DefinitionPoint.XY()
	minOffset := (2*s.ArrowSize + s.DimensionLineGap) * s.ScaleFactor
	angleRef := angleTo(defPoint, ref1)
	offset, inside := insideOffset(offset, radius, minOffset)
	dimRef := polar(defPoint, offset-s.DimensionLineGap*s.ScaleFactor, angleRef)
	b.point(v3(ref1))
	if !s.SuppressFirstDimensionLine && !s.SuppressSecondDimensionLine {
		b.radialLine(dimRef, ref1, angleRef, inside)
	}
	b.centerCross(x.Center(), radius)
	b.radialText(ref1, angleRef, inside)
	return nil
}

func (b *dimBuilder) ordinate(x *DimensionOrdinate) error {
	s := b.style
	minOffset := 2 * s.ArrowSize
	ref1 := x.FeatureLocation.XY()
	ref2 := x.LeaderEndpoint.XY()
	refDim := ref2.Sub(ref1)
	rotation := x.HorizontalDirection
	if x.IsOrdinateTypeX() {
		rotation += math.Pi / 2
	}
	side := 1.0
	ocs := rotate2(refDim, -rotation)
	var p1, p2 Vec2
	if ocs.X >= 0 {
		if ocs.X >= 2*minOffset {
			p1 = Vec2{ocs.X - minOffset, 0}
		} else {
			p1 = Vec2{minOffset, 0}
		}
		p2 = Vec2{ocs.X - minOffset, ocs.Y}
	} else {
		if ocs.X <= -2*minOffset {
			p1 = Vec2{ocs.X + minOffset, 0}
		} else {
			p1 = Vec2{-minOffset, 0}
		}
		p2 = Vec2{ocs.X + minOffset, ocs.Y}
		side = -1
	}
	p1 = ref1.Add(rotate2(p1, rotation))
	p2 = ref1.Add(rotate2(p2, rotation))
	b.point(x.DefinitionPoint)
	b.point(x.FeatureLocation)
	b.plainLine(v3(polar(ref1, s.ExtensionLineOffset*s.ScaleFactor, rotation)), v3(p1))
	b.plainLine(v3(p1), v3(p2))
	b.plainLine(v3(p2), v3(ref2))
	mid := polar(ref2, side*s.DimensionLineGap*s.ScaleFactor, rotation)
	if !x.IsTextUserDefinedLocation() {
		x.TextMiddlePoint = v3(mid)
	}
	attach := AttachmentPointTypeMiddleLeft
	if side < 0 {
		attach = AttachmentPointTypeMiddleRight
	}
	b.text(x.TextMiddlePoint, b.measurementText(), attach)
	return nil
}
