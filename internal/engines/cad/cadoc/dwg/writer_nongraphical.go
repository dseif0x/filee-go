package dwg

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// writeObjects writes the queued non-graphical objects; writing one can
// queue more (dictionary entries, extension dictionaries, referenced
// objects).
func (w *wObjectWriter) writeObjects() {
	written := map[cadoc.Object]bool{}
	for i := 0; i < len(w.queue); i++ {
		o := w.queue[i]
		if written[o] {
			continue
		}
		written[o] = true
		w.writeObject(o)
	}
}

func (w *wObjectWriter) writeObject(o cadoc.Object) {
	if !w.writable(o) {
		if skip, report := w.skipObject(o); skip && report {
			w.note(writerLevelNotImplemented, "Object type not implemented %s", writerObjectLabel(o))
		}
		return
	}
	switch o.(type) {
	case *cadoc.AcdbPlaceHolder, *cadoc.BookColor, *cadoc.BlockReferenceObjectContextData, *cadoc.DictionaryWithDefault,
		*cadoc.Dictionary, *cadoc.DictionaryVariable, *cadoc.DynamicBlockPurgePreventer, *cadoc.GeoData, *cadoc.Group,
		*cadoc.ImageDefinitionReactor, *cadoc.ImageDefinition, *cadoc.Layout, *cadoc.Material, *cadoc.MLineStyle,
		*cadoc.MultiLeaderStyle, *cadoc.MultiLeaderObjectContextData, *cadoc.PdfUnderlayDefinition,
		*cadoc.PlotSettings, *cadoc.RasterVariables, *cadoc.Scale, *cadoc.SortEntitiesTable, *cadoc.SpatialFilter,
		*cadoc.WipeoutVariables, *cadoc.Field, *cadoc.FieldList, *cadoc.XRecord:
	default:
		w.note(writerLevelNotImplemented, "Object not implemented : %s", writerObjectLabel(o))
		return
	}
	if !w.writeCommonNonEntityData(o) {
		return
	}
	switch x := o.(type) {
	case *cadoc.AcdbPlaceHolder:
	case *cadoc.BookColor:
		w.writeBookColor(x)
	case *cadoc.BlockReferenceObjectContextData:
		w.writeAnnotScaleObjectContextData(&x.ObjectContextDataBase)
		w.sw.writeBitDouble(x.Rotation)
		w.sw.write3BitDouble(x.InsertionPoint)
		w.sw.writeBitDouble(x.XScale)
		w.sw.writeBitDouble(x.YScale)
		w.sw.writeBitDouble(x.ZScale)
	case *cadoc.DictionaryWithDefault:
		w.writeDictionary(&x.Dictionary)
		w.sw.handleReference(wRefHardPointer, w.handleOf(x.DefaultEntry))
	case *cadoc.Dictionary:
		w.writeDictionary(x)
	case *cadoc.DictionaryVariable:
		// Schema number RC (ACadSharp writes 0, the usual value).
		w.sw.writeByte(byte(x.ObjectSchemaNumber))
		w.sw.writeVariableText(x.Value)
	case *cadoc.DynamicBlockPurgePreventer:
		w.sw.writeBitShort(x.Version)
		w.sw.handleReference(wRefHardPointer, w.handleOf(x.Block))
	case *cadoc.GeoData:
		w.writeGeoData(x)
	case *cadoc.Group:
		w.writeGroup(x)
	case *cadoc.ImageDefinitionReactor:
		w.sw.writeBitLong(x.ClassVersion)
	case *cadoc.ImageDefinition:
		w.sw.writeBitLong(x.ClassVersion)
		w.sw.write2RawDouble(x.Size)
		w.sw.writeVariableText(x.FileName)
		w.sw.writeBit(x.IsLoaded)
		w.sw.writeByte(byte(x.Units))
		w.sw.write2RawDouble(x.DefaultSize)
	case *cadoc.Layout:
		w.writeLayout(x)
	case *cadoc.Material:
		w.writeMaterial(x)
	case *cadoc.MLineStyle:
		w.writeMLineStyle(x)
	case *cadoc.MultiLeaderStyle:
		w.writeMultiLeaderStyle(x)
	case *cadoc.MultiLeaderObjectContextData:
		w.writeAnnotScaleObjectContextData(&x.ObjectContextDataBase)
		w.writeMultiLeaderAnnotContextSubObject(x)
	case *cadoc.PdfUnderlayDefinition:
		w.sw.writeVariableText(x.File)
		w.sw.writeVariableText(x.Page)
	case *cadoc.PlotSettings:
		w.writePlotSettings(x)
	case *cadoc.RasterVariables:
		w.sw.writeBitLong(x.ClassVersion)
		w.sw.writeBitShort(wB2S(x.IsDisplayFrameShown))
		w.sw.writeBitShort(int16(x.DisplayQuality))
		w.sw.writeBitShort(int16(x.Units))
	case *cadoc.Scale:
		w.sw.writeBitShort(0)
		w.sw.writeVariableText(x.Name)
		w.sw.writeBitDouble(x.PaperUnits)
		w.sw.writeBitDouble(x.DrawingUnits)
		w.sw.writeBit(x.IsUnitScale)
	case *cadoc.SortEntitiesTable:
		w.writeSortEntitiesTable(x)
	case *cadoc.SpatialFilter:
		w.writeSpatialFilter(x)
	case *cadoc.WipeoutVariables:
		w.sw.writeBitShort(wB2S(x.DisplayImageFrame))
	case *cadoc.Field:
		w.writeField(x)
	case *cadoc.FieldList:
		var fields []uint64
		for _, f := range x.Fields {
			if h := w.handleOf(f); h != 0 {
				fields = append(fields, h)
			}
		}
		w.sw.writeBitLong(int32(len(fields)))
		w.sw.writeBit(true)
		for _, h := range fields {
			w.sw.handleReference(wRefSoftPointer, h)
		}
	case *cadoc.XRecord:
		w.writeXRecord(x)
	}
	w.registerObject(o)
}

func writerObjectLabel(o cadoc.Object) string {
	name := o.ObjectName()
	if name == "" {
		name = fmt.Sprintf("%T", o)
	}
	return fmt.Sprintf("%s (handle %X)", name, o.Base().Handle)
}

func (w *wObjectWriter) writeAnnotScaleObjectContextData(c *cadoc.ObjectContextDataBase) {
	w.sw.writeBitShort(c.Version)
	w.sw.writeBit(c.HasFileToExtensionDictionary)
	// Default flag (290): not kept by the model.
	w.sw.writeBit(false)
	w.sw.handleReference(wRefHardPointer, w.handleOf(c.Scale))
}

func (w *wObjectWriter) writeBookColor(b *cadoc.BookColor) {
	if !w.r2004 {
		// A CMC before R2004 is the colour index (ACadSharp writes 0).
		w.sw.writeBitShort(b.Color.ApproxIndex())
		return
	}
	w.sw.writeBitShort(0)
	c := b.Color
	w.sw.writeBitLong(int32(uint32(c.B()) | uint32(c.G())<<8 | uint32(c.R())<<16 | 0b1100_0010<<24))
	colorName, bookName := b.ColorName(), b.BookName()
	var flags byte
	if colorName != "" {
		flags |= 1
	}
	if bookName != "" {
		flags |= 2
	}
	w.sw.writeByte(flags)
	if colorName != "" {
		w.sw.writeVariableText(colorName)
	}
	if bookName != "" {
		w.sw.writeVariableText(bookName)
	}
}

func (w *wObjectWriter) writeDictionary(d *cadoc.Dictionary) {
	var entries []cadoc.NonGraphicalObject
	for _, e := range d.Entries() {
		if w.writable(e) {
			entries = append(entries, e)
		}
	}
	w.sw.writeBitLong(int32(len(entries)))
	if w.version == cadoc.AC1014 {
		w.sw.writeByte(0)
	}
	if w.r2000 {
		w.sw.writeBitShort(int16(d.ClonningFlags))
		w.sw.writeByte(byte(wB2S(d.HardOwnerFlag)))
	}
	for _, e := range entries {
		w.sw.writeVariableText(*e.ObjectNameKey())
		w.sw.handleReference(wRefSoftOwnership, e.Base().Handle)
	}
	for _, e := range entries {
		w.enqueue(e)
	}
}

func (w *wObjectWriter) writeGroup(g *cadoc.Group) {
	w.sw.writeVariableText(g.Description)
	w.sw.writeBitShort(wB2S(g.IsUnnamed()))
	w.sw.writeBitShort(wB2S(g.Selectable))
	var hs []uint64
	for _, e := range g.Entities {
		if h := w.handleOf(e); h != 0 {
			hs = append(hs, h)
		}
	}
	w.sw.writeBitLong(int32(len(hs)))
	for _, h := range hs {
		w.sw.handleReference(wRefHardPointer, h)
	}
}

func (w *wObjectWriter) writePlotSettings(p *cadoc.PlotSettings) {
	s := w.sw
	s.writeVariableText(p.PageName)
	s.writeVariableText(p.SystemPrinterName)
	s.writeBitShort(int16(p.Flags))
	s.writeBitDouble(p.UnprintableMargin.Left)
	s.writeBitDouble(p.UnprintableMargin.Bottom)
	s.writeBitDouble(p.UnprintableMargin.Right)
	s.writeBitDouble(p.UnprintableMargin.Top)
	s.writeBitDouble(p.PaperWidth)
	s.writeBitDouble(p.PaperHeight)
	s.writeVariableText(p.PaperSize)
	s.writeBitDouble(p.PlotOriginX)
	s.writeBitDouble(p.PlotOriginY)
	s.writeBitShort(int16(p.PaperUnits))
	s.writeBitShort(int16(p.PaperRotation))
	s.writeBitShort(int16(p.PlotType))
	s.writeBitDouble(p.WindowLowerLeftX)
	s.writeBitDouble(p.WindowLowerLeftY)
	s.writeBitDouble(p.WindowUpperLeftX)
	s.writeBitDouble(p.WindowUpperLeftY)
	if w.r13_15 {
		s.writeVariableText(p.PlotViewName)
	}
	s.writeBitDouble(p.NumeratorScale)
	s.writeBitDouble(p.DenominatorScale)
	s.writeVariableText(p.StyleSheet)
	s.writeBitShort(int16(p.ScaledFit))
	s.writeBitDouble(p.StandardScale)
	s.write2BitDouble(p.PaperImageOrigin)
	if w.r2004 {
		s.writeBitShort(int16(p.ShadePlotMode))
		s.writeBitShort(int16(p.ShadePlotResolutionMode))
		s.writeBitShort(p.ShadePlotDPI)
		s.handleReference(wRefHardPointer, 0)
	}
	if w.r2007 {
		s.handleReference(wRefSoftPointer, 0)
	}
}

func (w *wObjectWriter) writeLayout(l *cadoc.Layout) {
	w.writePlotSettings(&l.PlotSettings)
	s := w.sw
	s.writeVariableText(l.Name)
	s.writeBitLong(l.TabOrder)
	s.writeBitShort(int16(l.LayoutFlags))
	s.write3BitDouble(l.Origin)
	s.write2RawDouble(l.MinLimits)
	// ACadSharp writes MinLimits twice.
	s.write2RawDouble(l.MaxLimits)
	s.write3BitDouble(l.InsertionBasePoint)
	s.write3BitDouble(l.XAxis)
	s.write3BitDouble(l.YAxis)
	s.writeBitDouble(l.Elevation)
	s.writeBitShort(int16(l.UcsOrthographicType))
	s.write3BitDouble(l.MinExtents)
	s.write3BitDouble(l.MaxExtents)
	var viewports []uint64
	for _, v := range l.Viewports() {
		if h := w.handleOf(v); h != 0 {
			viewports = append(viewports, h)
		}
	}
	if w.r2004 {
		s.writeBitLong(int32(len(viewports)))
	}
	s.handleReference(wRefSoftPointer, w.handleOf(l.AssociatedBlock))
	s.handleReference(wRefSoftPointer, w.handleOf(l.LastActiveViewport))
	if l.UcsOrthographicType == cadoc.OrthographicTypeNone {
		s.handleReference(wRefHardPointer, 0)
		s.handleReference(wRefHardPointer, w.handleOf(l.UCS))
	} else {
		s.handleReference(wRefHardPointer, w.handleOf(l.BaseUCS))
		s.handleReference(wRefHardPointer, 0)
	}
	if w.r2004 {
		for _, h := range viewports {
			s.handleReference(wRefSoftPointer, h)
		}
	}
}

func (w *wObjectWriter) writeMLineStyle(m *cadoc.MLineStyle) {
	s := w.sw
	s.writeVariableText(m.Name)
	s.writeVariableText(m.Description)
	// The DWG flag bits differ from the DXF ones.
	var flags int16
	for _, f := range []struct {
		dxf cadoc.MLineStyleFlags
		dwg int16
	}{{cadoc.MLineStyleFlagsDisplayJoints, 1}, {cadoc.MLineStyleFlagsFillOn, 2},
		{cadoc.MLineStyleFlagsStartSquareCap, 16}, {cadoc.MLineStyleFlagsStartRoundCap, 0x20},
		{cadoc.MLineStyleFlagsStartInnerArcsCap, 0x40}, {cadoc.MLineStyleFlagsEndSquareCap, 0x100},
		{cadoc.MLineStyleFlagsEndRoundCap, 0x200}, {cadoc.MLineStyleFlagsEndInnerArcsCap, 0x400}} {
		if m.Flags&f.dxf != 0 {
			flags |= f.dwg
		}
	}
	s.writeBitShort(flags)
	s.writeCmColor(m.FillColor, false)
	s.writeBitDouble(m.StartAngle)
	s.writeBitDouble(m.EndAngle)
	s.writeByte(byte(len(m.Elements)))
	for _, e := range m.Elements {
		s.writeBitDouble(e.Offset)
		s.writeCmColor(e.Color, false)
		if w.r2018 {
			s.handleReference(wRefHardPointer, w.handleOf(e.LineType))
		} else {
			// Line type index (ACadSharp writes 0 as well).
			s.writeBitShort(0)
		}
	}
}

func (w *wObjectWriter) writeMultiLeaderStyle(m *cadoc.MultiLeaderStyle) {
	s := w.sw
	if w.r2010 {
		s.writeBitShort(2)
	}
	s.writeBitShort(int16(m.ContentType))
	s.writeBitShort(int16(m.MultiLeaderDrawOrder))
	s.writeBitShort(int16(m.LeaderDrawOrder))
	s.writeBitLong(int32(int16(m.MaxLeaderSegmentsPoints)))
	s.writeBitDouble(m.FirstSegmentAngleConstraint)
	s.writeBitDouble(m.SecondSegmentAngleConstraint)
	s.writeBitShort(int16(m.PathType))
	s.writeCmColor(m.LineColor, false)
	s.handleReference(wRefHardPointer, w.handleOf(m.LeaderLineType))
	s.writeBitLong(int32(int16(m.LeaderLineWeight)))
	s.writeBit(m.EnableLanding)
	s.writeBitDouble(m.LandingGap)
	s.writeBit(m.EnableDogleg)
	s.writeBitDouble(m.LandingDistance)
	s.writeVariableText(m.Description)
	s.handleReference(wRefHardPointer, w.handleOf(m.Arrowhead))
	s.writeBitDouble(m.ArrowheadSize)
	s.writeVariableText(m.DefaultTextContents)
	s.handleReference(wRefHardPointer, w.handleOf(m.TextStyle))
	s.writeBitShort(int16(m.TextLeftAttachment))
	s.writeBitShort(int16(m.TextRightAttachment))
	s.writeBitShort(int16(m.TextAngle))
	s.writeBitShort(int16(m.TextAlignment))
	s.writeCmColor(m.TextColor, false)
	s.writeBitDouble(m.TextHeight)
	s.writeBit(m.TextFrame)
	s.writeBit(m.TextAlignAlwaysLeft)
	s.writeBitDouble(m.AlignSpace)
	s.handleReference(wRefHardPointer, w.handleOf(m.BlockContent))
	s.writeCmColor(m.BlockContentColor, false)
	s.write3BitDouble(m.BlockContentScale)
	s.writeBit(m.EnableBlockContentScale)
	s.writeBitDouble(m.BlockContentRotation)
	s.writeBit(m.EnableBlockContentRotation)
	s.writeBitShort(int16(m.BlockContentConnection))
	s.writeBitDouble(m.ScaleFactor)
	s.writeBit(m.OverwritePropertyValue)
	s.writeBit(m.IsAnnotative)
	s.writeBitDouble(m.BreakGapSize)
	if w.r2010 {
		s.writeBitShort(int16(m.TextAttachmentDirection))
		// Bottom then top, as ACadSharp writes (and reads) them.
		s.writeBitShort(int16(m.TextBottomAttachment))
		s.writeBitShort(int16(m.TextTopAttachment))
	}
	if w.r2013 {
		s.writeBit(m.UnknownFlag298)
	}
}

func (w *wObjectWriter) writeSortEntitiesTable(t *cadoc.SortEntitiesTable) {
	s := w.sw
	s.handleReference(wRefSoftPointer, w.handleOf(t.BlockOwner))
	var sorters []cadoc.SortEntitiesSorter
	for _, so := range t.Sorters {
		if w.handleOf(so.Entity) != 0 {
			sorters = append(sorters, so)
		}
	}
	s.writeBitLong(int32(len(sorters)))
	for _, so := range sorters {
		// The sort handle is part of the main stream (code 0).
		s.main().handleReference(wRefUndefined, so.SortHandle)
		s.handleReference(wRefSoftPointer, so.Entity.Base().Handle)
	}
}

func (w *wObjectWriter) writeSpatialFilter(f *cadoc.SpatialFilter) {
	s := w.sw
	s.writeBitShort(int16(len(f.BoundaryPoints)))
	for _, p := range f.BoundaryPoints {
		s.write2RawDouble(p)
	}
	s.write3BitDouble(f.Normal)
	s.write3BitDouble(f.Origin)
	s.writeBitShort(wB2S(f.DisplayBoundary))
	s.writeBitShort(wB2S(f.ClipFrontPlane))
	if f.ClipFrontPlane {
		s.writeBitDouble(f.FrontDistance)
	}
	s.writeBitShort(wB2S(f.ClipBackPlane))
	if f.ClipBackPlane {
		s.writeBitDouble(f.BackDistance)
	}
	for _, v := range f.InverseInsertTransform {
		s.writeBitDouble(v)
	}
	for _, v := range f.InsertTransform {
		s.writeBitDouble(v)
	}
}

func (w *wObjectWriter) writeGeoData(g *cadoc.GeoData) {
	s := w.sw
	s.writeBitLong(int32(g.Version))
	s.handleReference(wRefSoftPointer, w.handleOf(g.HostBlock))
	s.writeBitShort(int16(g.CoordinatesType))
	switch g.Version {
	case cadoc.GeoDataVersionR2009:
		s.write3BitDouble(g.ReferencePoint)
		s.writeBitLong(int32(g.HorizontalUnits))
		s.write3BitDouble(g.DesignPoint)
		s.write3BitDouble(cadoc.Vec3{})
		s.write3BitDouble(g.UpDirection)
		s.writeBitDouble(math.Pi/2 - math.Atan2(g.NorthDirection.Y, g.NorthDirection.X))
		s.write3BitDouble(cadoc.Vec3{X: 1, Y: 1, Z: 1})
		s.writeVariableText(g.CoordinateSystemDefinition)
		s.writeVariableText(g.GeoRssTag)
		s.writeBitDouble(g.HorizontalUnitScale)
		s.writeVariableText("")
		s.writeVariableText("")
	case cadoc.GeoDataVersionR2010, cadoc.GeoDataVersionR2013:
		s.write3BitDouble(g.DesignPoint)
		s.write3BitDouble(g.ReferencePoint)
		s.writeBitDouble(g.HorizontalUnitScale)
		s.writeBitLong(int32(g.HorizontalUnits))
		s.writeBitDouble(g.VerticalUnitScale)
		// Vertical units (ACadSharp repeats the horizontal ones).
		s.writeBitLong(int32(g.VerticalUnits))
		s.write3BitDouble(g.UpDirection)
		s.write2RawDouble(g.NorthDirection)
		s.writeBitLong(int32(g.ScaleEstimationMethod))
		s.writeBitDouble(g.UserSpecifiedScaleFactor)
		s.writeBit(g.EnableSeaLevelCorrection)
		s.writeBitDouble(g.SeaLevelElevation)
		s.writeBitDouble(g.CoordinateProjectionRadius)
		s.writeVariableText(g.CoordinateSystemDefinition)
		s.writeVariableText(g.GeoRssTag)
	}
	s.writeVariableText(g.ObservationFromTag)
	s.writeVariableText(g.ObservationToTag)
	s.writeVariableText(g.ObservationCoverageTag)
	s.writeBitLong(int32(len(g.Points)))
	for _, p := range g.Points {
		s.write2RawDouble(p.Source)
		s.write2RawDouble(p.Destination)
	}
	s.writeBitLong(int32(len(g.Faces)))
	for _, f := range g.Faces {
		s.writeBitLong(f.Index1)
		s.writeBitLong(f.Index2)
		s.writeBitLong(f.Index3)
	}
}

// ----- FIELD -----

func (w *wObjectWriter) writeField(f *cadoc.Field) {
	s := w.sw
	s.writeVariableText(f.EvaluatorId)
	s.writeVariableText(f.FieldCode)
	var children []*cadoc.Field
	for _, c := range f.Children {
		if w.writable(c) {
			children = append(children, c)
		}
	}
	s.writeBitLong(int32(len(children)))
	for _, c := range children {
		s.handleReference(wRefHardOwnership, c.Handle)
	}
	var objs []uint64
	for _, o := range f.CadObjects {
		if h := w.handleOf(o); h != 0 {
			objs = append(objs, h)
		}
	}
	s.writeBitLong(int32(len(objs)))
	for _, h := range objs {
		s.handleReference(wRefSoftPointer, h)
	}
	if w.version < cadoc.AC1021 {
		s.writeVariableText(f.FormatString)
	}
	s.writeBitLong(int32(f.EvaluationOptionFlags))
	s.writeBitLong(int32(f.FilingOptionFlags))
	s.writeBitLong(int32(f.FieldStateFlags))
	s.writeBitLong(int32(f.EvaluationStatusFlags))
	s.writeBitLong(f.EvaluationErrorCode)
	s.writeVariableText(f.EvaluationErrorMessage)
	w.writeCadValue(&f.Value)
	// Value string and its length (ACadSharp writes the format string).
	s.writeVariableText(f.FormatString)
	s.writeBitLong(int32(len(utf16.Encode([]rune(f.FormatString)))))
	s.writeBitLong(int32(len(f.Values)))
	for i := range f.Values {
		s.writeVariableText(f.Values[i].Key)
		w.writeCadValue(&f.Values[i].Value)
	}
	for _, c := range children {
		w.enqueue(c)
	}
}

func (w *wObjectWriter) writeCadValue(v *cadoc.CadValue) {
	s := w.sw
	if w.r2007 {
		s.writeBitLong(v.Flags)
	}
	s.writeBitLong(int32(v.ValueType))
	if !w.r2007 || v.Flags&1 == 0 {
		switch v.ValueType {
		case cadoc.CadValueTypeLong:
			s.writeBitLong(int32(wAsInt(v.Value)))
		case cadoc.CadValueTypeDouble:
			s.writeBitDouble(wAsFloat(v.Value))
		case cadoc.CadValueTypeGeneral, cadoc.CadValueTypeString:
			w.writeStringCadValue(wAsString(v.Value))
		case cadoc.CadValueTypePoint2D:
			switch p := v.Value.(type) {
			case cadoc.Vec2:
				s.writeBitLong(16)
				s.write2RawDouble(p)
			case cadoc.Vec3:
				s.writeBitLong(16)
				s.write2RawDouble(p.XY())
			default:
				s.writeBitLong(0)
			}
		case cadoc.CadValueTypePoint3D:
			if p, ok := v.Value.(cadoc.Vec3); ok {
				s.writeBitLong(24)
				s.writeRawDouble(p.X)
				s.writeRawDouble(p.Y)
				s.writeRawDouble(p.Z)
			} else {
				s.writeBitLong(0)
			}
		case cadoc.CadValueTypeHandle:
			var h uint64
			if o, ok := v.Value.(cadoc.Object); ok {
				h = w.handleOf(o)
			}
			s.handleReference(wRefSoftPointer, h)
		default:
			// Unknown, dates (not kept by the model) and buffers (ACadSharp
			// can't write them either): no data.
			s.writeBitLong(0)
		}
	}
	if w.r2007 {
		s.writeBitLong(int32(v.Units))
		s.writeVariableText(v.Format)
		s.writeVariableText(v.FormattedValue)
	}
}

func (w *wObjectWriter) writeStringCadValue(v string) {
	s := w.sw
	if v == "" {
		// ACadSharp writes the length 0 and then the empty string anyway.
		s.writeBitLong(0)
		return
	}
	if w.r2007 {
		b := wUTF16(v)
		s.writeBitLong(int32(len(b) + 2))
		s.writeBytes(b)
		s.writeByte(0)
		s.writeByte(0)
		return
	}
	b := w.enc.encode(v)
	s.writeBitLong(int32(len(b) + 1))
	s.writeBytes(b)
	s.writeByte(0)
}

// ----- MATERIAL -----

// wMaterial holds the MATERIAL values the DWG object needs, with
// ACadSharp's defaults, filled from the group codes the model keeps.
type wMaterial struct {
	ints    map[int]int64
	doubles map[int]float64
	strs    map[int]string
	mats    map[int][]float64
}

func newWMaterial(m *cadoc.Material) *wMaterial {
	x := &wMaterial{ints: map[int]int64{}, doubles: map[int]float64{}, strs: map[int]string{},
		mats: map[int][]float64{}}
	for _, g := range m.Raw {
		switch c := g.Code; {
		case c == 43 || c == 47 || c == 49 || c == 142 || c == 144 || c == 147:
			x.mats[c] = append(x.mats[c], wAsFloat(g.Value))
		case c >= 1 && c <= 9:
			x.strs[c] = wAsString(g.Value)
		case c >= 40 && c <= 49 || c >= 140 && c <= 149 || c == 468:
			x.doubles[c] = wAsFloat(g.Value)
		default:
			x.ints[c] = wAsInt(g.Value)
		}
	}
	return x
}

func (x *wMaterial) i(code int, def int64) int64 {
	if v, ok := x.ints[code]; ok {
		return v
	}
	return def
}

func (x *wMaterial) d(code int, def float64) float64 {
	if v, ok := x.doubles[code]; ok {
		return v
	}
	return def
}

func (x *wMaterial) matrix(code int) []float64 {
	if m := x.mats[code]; len(m) == 16 {
		return m
	}
	return []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

func (w *wObjectWriter) writeMaterial(m *cadoc.Material) {
	s := w.sw
	x := newWMaterial(m)
	s.writeVariableText(m.Name)
	s.writeVariableText(m.Description)

	color := func(methodCode, factorCode, colorCode int) {
		method := byte(x.i(methodCode, int64(cadoc.ColorMethodCurrent)))
		s.writeByte(method)
		s.writeBitDouble(x.d(factorCode, 1))
		if method == byte(cadoc.ColorMethodOverride) {
			rgb := uint32(x.i(colorCode, 0)) & 0xFFFFFF
			// True colour tag 0xC2 in the high byte, then B, G, R.
			s.writeBitLong(int32(rgb | 0xC2<<24))
		}
	}
	mapData := func(blendCode, projCode, tilingCode, autoCode, matrixCode, sourceCode, fileCode int) {
		s.writeBitDouble(x.d(blendCode, 1))
		s.writeByte(byte(x.i(projCode, 1)))
		s.writeByte(byte(x.i(tilingCode, 1)))
		s.writeByte(byte(x.i(autoCode, 1)))
		for _, v := range x.matrix(matrixCode) {
			s.writeBitDouble(v)
		}
		source := byte(x.i(sourceCode, int64(cadoc.MapSourceUseImageFile)))
		s.writeByte(source)
		if source == byte(cadoc.MapSourceUseImageFile) {
			s.writeVariableText(x.strs[fileCode])
		}
	}
	color(70, 40, 90)
	color(71, 41, 91)
	mapData(42, 73, 74, 75, 43, 72, 3)
	color(76, 45, 92)
	mapData(46, 78, 79, 170, 47, 77, 4)
	s.writeBitDouble(x.d(44, 0.5))
	mapData(48, 172, 173, 174, 49, 171, 6)
	s.writeBitDouble(x.d(140, 1))
	mapData(141, 176, 177, 178, 142, 175, 7)
	mapData(143, 270, 271, 272, 144, 179, 8)
	s.writeBitDouble(x.d(145, 1))
	mapData(146, 274, 275, 276, 147, 273, 9)
	if w.r2007 {
		s.writeBitDouble(x.d(148, 0))
		s.writeBitDouble(x.d(149, 0))
		s.writeBitDouble(x.d(468, 0))
		s.writeBitLong(int32(x.i(93, int64(cadoc.MaterialIlluminationModelBlinnShader))))
		s.writeBitLong(int32(x.i(94, int64(cadoc.MaterialChannelFlagsUseDiffuse))))
		s.writeBitLong(int32(x.i(282, int64(cadoc.MaterialModeRealistic))))
	}
}

// ----- XRECORD -----

func (w *wObjectWriter) writeXRecord(x *cadoc.XRecord) {
	ms := &wMemStream{}
	str := func(text string) {
		if w.r2007 {
			u := utf16.Encode([]rune(text))
			ms.writeInt16(int16(len(u)))
			ms.write(wUTF16Bytes(u))
			return
		}
		b := w.enc.encode(text)
		ms.writeInt16(int16(len(b)))
		ms.writeByte(w.enc.codeIndex)
		ms.write(b)
	}
	refHandle := func(v any) uint64 {
		switch r := v.(type) {
		case cadoc.Object:
			return w.handleOf(r)
		case uint64:
			if _, ok := w.index[r]; ok {
				return r
			}
		}
		return 0
	}
	for _, e := range x.Entries {
		t := writerGroupValueType(e.Code)
		if t == wGroupNone {
			continue
		}
		if e.Value == nil && t != wGroupHandle && t != wGroupObjectID {
			continue
		}
		ms.writeInt16(int16(e.Code))
		switch t {
		case wGroupByte:
			ms.writeByte(byte(wAsInt(e.Value)))
		case wGroupInt16:
			ms.writeInt16(int16(wAsInt(e.Value)))
		case wGroupInt32:
			ms.writeInt32(int32(wAsInt(e.Value)))
		case wGroupInt64:
			ms.writeInt64(wAsInt(e.Value))
		case wGroupDouble:
			ms.writeFloat64(wAsFloat(e.Value))
		case wGroupPoint3D:
			p, _ := e.Value.(cadoc.Vec3)
			ms.writeFloat64(p.X)
			ms.writeFloat64(p.Y)
			ms.writeFloat64(p.Z)
		case wGroupChunk:
			b, _ := e.Value.([]byte)
			ms.writeByte(byte(len(b)))
			ms.write(b)
		case wGroupHandle:
			if h := refHandle(e.Value); h != 0 {
				str(fmt.Sprintf("%X", h))
			} else {
				str("")
			}
		case wGroupObjectID:
			ms.writeInt64(int64(refHandle(e.Value)))
		case wGroupString:
			str(wXRecordString(e.Value))
		}
	}
	w.sw.writeBitLong(int32(ms.length()))
	w.sw.writeBytes(ms.bytes())
	if w.r2000 {
		w.sw.writeBitShort(int16(x.CloningFlags))
	}
}

func wXRecordString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", x), "0"), ".")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

type wGroupType int

const (
	wGroupNone wGroupType = iota
	wGroupString
	wGroupHandle
	wGroupObjectID
	wGroupPoint3D
	wGroupDouble
	wGroupInt16
	wGroupInt32
	wGroupInt64
	wGroupByte
	wGroupChunk
)

// writerGroupValueType is GroupCodeValue.TransformValue reduced to how
// XRECORD data stores the value.
func writerGroupValueType(code int) wGroupType {
	switch {
	case code >= 0 && code <= 4, code >= 6 && code <= 9, code >= 100 && code <= 102:
		return wGroupString
	case code == 5, code == 105, code >= 320 && code <= 329, code >= 480 && code <= 481:
		return wGroupHandle
	case code >= 10 && code <= 39:
		return wGroupPoint3D
	case code >= 40 && code <= 59, code >= 110 && code <= 149, code >= 210 && code <= 239, code >= 460 && code <= 469:
		return wGroupDouble
	case code >= 60 && code <= 79, code >= 170 && code <= 179, code >= 270 && code <= 279,
		code >= 370 && code <= 389, code >= 400 && code <= 409:
		return wGroupInt16
	case code >= 90 && code <= 99, code >= 420 && code <= 429, code >= 440 && code <= 459:
		return wGroupInt32
	case code >= 160 && code <= 169:
		return wGroupInt64
	case code >= 280 && code <= 299:
		return wGroupByte
	case code >= 300 && code <= 309, code >= 410 && code <= 419, code >= 430 && code <= 439, code >= 470 && code <= 479:
		return wGroupString
	case code >= 310 && code <= 319:
		return wGroupChunk
	case code >= 330 && code <= 369, code >= 390 && code <= 399:
		return wGroupObjectID
	case code >= 1000 && code <= 1003:
		return wGroupString
	case code == 1004:
		return wGroupChunk
	case code >= 1005 && code <= 1009:
		return wGroupObjectID
	case code >= 1010 && code <= 1059:
		return wGroupDouble
	case code >= 1060 && code <= 1070:
		return wGroupInt16
	case code == 1071:
		return wGroupInt32
	}
	return wGroupNone
}
