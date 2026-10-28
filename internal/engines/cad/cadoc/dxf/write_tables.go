package dxf

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func (w *writer) writeTables() {
	d := w.doc
	w.section(tokTables, func() {
		writeTable(w, d.VPorts, "", true)
		writeTable(w, d.LineTypes, "", true)
		writeTable(w, d.Layers, "", true)
		writeTable(w, d.TextStyles, "", true)
		writeTable(w, d.Views, "", true)
		writeTable(w, d.UCSs, "", true)
		writeTable(w, d.AppIds, "", true)
		writeTable(w, d.DimensionStyles, cadoc.SubclassDimensionStyleTable, true)
		writeTable(w, d.BlockRecords, "", false)
	})
}

func writeTable[T cadoc.TableEntry](w *writer, t *cadoc.Table[T], subclass string, writeFlags bool) {
	o := w.o
	o.str(0, tokTable)
	o.str(2, t.ObjectName())
	w.commonObjectData(t)
	o.str(100, cadoc.SubclassTable)
	n := t.Len()
	if n > 32767 {
		n = 0
	}
	o.integer(70, int64(n))
	if subclass != "" {
		o.str(100, subclass)
		if subclass == cadoc.SubclassDimensionStyleTable {
			o.integer(71, int64(t.Len()))
			for _, e := range t.Entries() {
				o.handle(340, e.Base().Handle)
			}
		}
	}
	w.xdata(t)
	for _, e := range t.Entries() {
		w.writeEntry(e, writeFlags)
	}
	o.str(0, tokEndTable)
}

// invalidNameChars are refused in DXF symbol table names (ACadSharp's
// INamedCadObjectExtensions.InvalidCharacters, '*' allowed only first).
const invalidNameChars = "\\/:?\"<>|;,=`"

// validEntryName reports whether AutoCAD accepts a symbol table name.
func validEntryName(name string) bool {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 255 {
		return false
	}
	if strings.IndexByte(name, '*') > 0 || strings.ContainsAny(name, invalidNameChars) {
		return false
	}
	return !strings.ContainsAny(name, "\n\r\t")
}

// sanitizeName replaces the characters AutoCAD refuses with '_'.
func sanitizeName(name string) string {
	r := []rune(name)
	if len(r) > 255 {
		r = r[:255]
	}
	for i, c := range r {
		if (c == '*' && i > 0) || strings.ContainsRune(invalidNameChars, c) || c == '\n' || c == '\r' || c == '\t' {
			r[i] = '_'
		}
	}
	s := strings.TrimSpace(string(r))
	if s == "" {
		s = "_"
	}
	return s
}

// sanitizeTableNames renames entries whose names AutoCAD would refuse;
// references are pointers, so they follow the new name.
func sanitizeTableNames[T cadoc.TableEntry](w *writer, t *cadoc.Table[T]) {
	for _, e := range t.Entries() {
		name := e.EntryName()
		if validEntryName(name) {
			continue
		}
		if _, isStyle := any(e).(*cadoc.TextStyle); isStyle && name == "" {
			continue
		}
		n := sanitizeName(name)
		base := n
		for i := 1; t.Contains(n); i++ {
			n = base + "_" + itoa(i)
		}
		w.note(LevelWarning, "%s name %q is not valid in DXF, written as %q", e.ObjectName(), name, n)
		e.SetEntryName(n)
	}
}

func (w *writer) writeEntry(e cadoc.TableEntry, writeFlags bool) {
	o := w.o
	o.str(0, e.ObjectName())
	w.commonObjectData(e)
	o.str(100, cadoc.SubclassTableRecord)
	o.str(100, e.SubclassMarker())
	if ts, ok := e.(*cadoc.TextStyle); ok && ts.IsShapeFile() {
		o.str(2, "")
	} else {
		o.str(2, e.EntryName())
	}
	if writeFlags {
		o.integer(70, int64(e.EntryFlags()))
	}
	switch x := e.(type) {
	case *cadoc.AppId:
	case *cadoc.BlockRecord:
		o.ref(340, layoutOrNil(x.Layout))
		o.integer(70, int64(x.Units))
		o.boolean(280, x.IsExplodable)
		o.boolean(281, x.CanScale)
	case *cadoc.DimensionStyle:
		w.writeDimStyle(x)
	case *cadoc.Layer:
		w.writeLayer(x)
	case *cadoc.LineType:
		w.writeLineType(x)
	case *cadoc.TextStyle:
		w.writeTextStyle(x)
	case *cadoc.UCS:
		o.vec3(10, x.Origin)
		o.vec3(11, x.XAxis)
		o.vec3(12, x.YAxis)
		o.integer(79, int64(x.OrthographicViewType))
		o.dbl(146, x.Elevation)
		if x.OrthographicType != 0 {
			o.integer(71, int64(x.OrthographicType))
		}
	case *cadoc.View:
		o.dbl(40, x.Height)
		o.vec2(10, x.Center)
		o.dbl(41, x.Width)
		o.vec3(11, x.Direction)
		o.vec3(12, x.Target)
		o.dbl(42, x.LensLength)
		o.dbl(43, x.FrontClipping)
		o.dbl(44, x.BackClipping)
		o.angle(50, x.Angle)
		o.integer(71, int64(x.ViewMode))
		o.integer(281, int64(x.RenderMode))
		o.boolean(72, x.IsUcsAssociated)
		if x.IsUcsAssociated {
			o.vec3(110, x.UcsOrigin)
			o.vec3(111, x.UcsXAxis)
			o.vec3(112, x.UcsYAxis)
			o.integer(79, int64(x.UcsOrthographicType))
			o.dbl(146, x.UcsElevation)
		}
	case *cadoc.VPort:
		w.writeVPort(x)
	}
	w.xdata(e)
}

func layoutOrNil(l *cadoc.Layout) cadoc.Object {
	if l == nil {
		return nil
	}
	return l
}

func (w *writer) writeLayer(l *cadoc.Layer) {
	o := w.o
	index := l.Color.Index()
	if l.Color.IsTrueColor() {
		index = l.Color.ApproxIndex()
	}
	if index <= 0 || index > 255 {
		index = 7
	}
	if !l.IsOn {
		index = -index
	}
	o.integer(62, int64(index))
	if l.Color.IsTrueColor() && w.version >= cadoc.AC1018 {
		o.integer(420, int64(l.Color.TrueColor()))
	}
	o.str(6, l.LineTypeName())
	o.boolean(290, l.Plots())
	o.integer(370, int64(l.LineWeight))
	var plotStyle uint64
	if _, ok := w.index[l.PlotStyleName]; ok {
		plotStyle = l.PlotStyleName
	}
	o.handle(390, plotStyle)
	if w.known(l.Material) {
		o.handle(347, l.Material.Handle)
	}
}

func (w *writer) writeLineType(l *cadoc.LineType) {
	o := w.o
	o.str(3, l.Description)
	align := l.Alignment
	if align == 0 {
		align = 'A'
	}
	o.integer(72, int64(align))
	o.integer(73, int64(len(l.Segments)))
	o.dbl(40, l.PatternLength())
	for _, s := range l.Segments {
		o.dbl(49, s.Length)
		o.integer(74, int64(s.Flags))
		if s.Flags == 0 {
			continue
		}
		if s.Flags&cadoc.LineTypeShapeFlagsShape != 0 {
			o.integer(75, int64(s.ShapeNumber))
		}
		if s.Flags&cadoc.LineTypeShapeFlagsText != 0 {
			o.integer(75, 0)
		}
		if w.known(s.Style) {
			o.handle(340, s.Style.Handle)
		} else {
			o.handle(340, 0)
		}
		o.dbl(46, s.Scale)
		o.angle(50, s.Rotation)
		o.dbl(44, s.Offset.X)
		o.dbl(45, s.Offset.Y)
		if s.Flags&cadoc.LineTypeShapeFlagsText != 0 {
			o.str(9, s.Text)
		}
	}
}

func (w *writer) writeTextStyle(t *cadoc.TextStyle) {
	o := w.o
	if t.FontFamily != "" {
		t.SetExtendedData(cadoc.DefaultAppIdName, trueTypeXData(t))
		if !w.doc.AppIds.Contains(cadoc.DefaultAppIdName) {
			_ = w.doc.AppIds.Add(cadoc.NewAppId(cadoc.DefaultAppIdName))
		}
	}
	o.dbl(40, t.Height)
	o.dbl(41, t.Width)
	o.angle(50, t.ObliqueAngle)
	o.integer(71, int64(t.MirrorFlag))
	o.dbl(42, t.LastHeight)
	o.str(3, t.Filename)
	o.str(4, t.BigFontFilename)
}

func (w *writer) writeVPort(v *cadoc.VPort) {
	o := w.o
	o.vec2(10, v.BottomLeft)
	o.vec2(11, v.TopRight)
	o.vec2(12, v.Center)
	o.vec2(13, v.SnapBasePoint)
	o.vec2(14, v.SnapSpacing)
	o.vec2(15, v.GridSpacing)
	o.vec3(16, v.Direction)
	o.vec3(17, v.Target)
	o.dbl(40, v.ViewHeight)
	o.dbl(41, v.AspectRatio)
	o.dbl(42, v.LensLength)
	o.dbl(43, v.FrontClippingPlane)
	o.dbl(44, v.BackClippingPlane)
	o.angle(50, v.SnapRotation)
	o.angle(51, v.TwistAngle)
	o.integer(71, int64(v.ViewMode))
	o.integer(72, int64(v.CircleZoomPercent))
	o.integer(73, 1)
	o.integer(74, int64(v.UcsIconDisplay))
	o.boolean(75, v.SnapOn)
	o.boolean(76, v.ShowGrid)
	o.boolean(77, v.IsometricSnap)
	o.integer(78, int64(v.SnapIsoPair))
	o.integer(281, int64(v.RenderMode))
	o.integer(65, 1)
	o.vec3(110, v.Origin)
	o.vec3(111, v.XAxis)
	o.vec3(112, v.YAxis)
	o.integer(79, int64(v.OrthographicType))
	o.dbl(146, v.Elevation)
	if w.version >= cadoc.AC1018 {
		o.integer(60, int64(v.GridFlags))
		o.integer(61, int64(v.MinorGridLinesPerMajorGridLine))
		o.boolean(292, v.UseDefaultLighting)
		o.integer(282, int64(v.DefaultLighting))
		o.dbl(141, v.Brightness)
		o.dbl(142, v.Contrast)
		o.integer(63, int64(v.AmbientColor.ApproxIndex()))
		if v.AmbientColor.IsTrueColor() {
			o.integer(421, int64(v.AmbientColor.TrueColor()))
		}
	}
}

func (w *writer) writeDimStyle(s *cadoc.DimensionStyle) {
	o := w.o
	o.str(3, s.PostFix)
	o.str(4, s.AlternateDimensioningSuffix)
	o.dbl(40, s.ScaleFactor)
	o.dbl(41, s.ArrowSize)
	o.dbl(42, s.ExtensionLineOffset)
	o.dbl(43, s.DimensionLineIncrement)
	o.dbl(44, s.ExtensionLineExtension)
	o.dbl(45, s.Rounding)
	o.dbl(46, s.DimensionLineExtension)
	o.dbl(47, s.PlusTolerance)
	o.dbl(48, s.MinusTolerance)
	if w.version >= cadoc.AC1021 {
		o.dbl(49, s.FixedExtensionLineLength)
		o.angle(50, s.JoggedRadiusDimensionTransverseSegmentAngle)
		if s.TextBackgroundFillMode != 0 {
			o.integer(69, int64(s.TextBackgroundFillMode))
			o.integer(70, int64(s.TextBackgroundColor.ApproxIndex()))
		}
	}
	if w.version >= cadoc.AC1024 && s.ArcLengthSymbolPosition != cadoc.ArcLengthSymbolPositionAboveDimensionText {
		o.integer(90, int64(s.ArcLengthSymbolPosition))
	}
	o.dbl(140, s.TextHeight)
	o.dbl(141, s.CenterMarkSize)
	o.dbl(142, s.TickSize)
	o.dbl(143, s.AlternateUnitScaleFactor)
	o.dbl(144, s.LinearScaleFactor)
	o.dbl(145, s.TextVerticalPosition)
	o.dbl(146, s.ToleranceScaleFactor)
	o.dbl(147, s.DimensionLineGap)
	o.dbl(148, s.AlternateUnitRounding)
	o.boolean(71, s.GenerateTolerances)
	o.boolean(72, s.LimitsGeneration)
	o.boolean(73, s.TextInsideHorizontal)
	o.boolean(74, s.TextOutsideHorizontal)
	o.boolean(75, s.SuppressFirstExtensionLine)
	o.boolean(76, s.SuppressSecondExtensionLine)
	o.integer(77, int64(s.TextVerticalAlignment))
	o.integer(78, int64(s.ZeroHandling))
	o.integer(79, int64(s.AngularZeroHandling))
	o.boolean(170, s.AlternateUnitDimensioning)
	o.integer(171, int64(s.AlternateUnitDecimalPlaces))
	o.boolean(172, s.TextOutsideExtensions)
	o.boolean(173, s.SeparateArrowBlocks)
	o.boolean(174, s.TextInsideExtensions)
	o.boolean(175, s.SuppressOutsideExtensions)
	o.integer(176, int64(s.DimensionLineColor.ApproxIndex()))
	o.integer(177, int64(s.ExtensionLineColor.ApproxIndex()))
	o.integer(178, int64(s.TextColor.ApproxIndex()))
	o.integer(179, int64(s.AngularDecimalPlaces))
	o.integer(271, int64(s.DecimalPlaces))
	o.integer(272, int64(s.ToleranceDecimalPlaces))
	o.integer(273, int64(s.AlternateUnitFormat))
	o.integer(274, int64(s.AlternateUnitToleranceDecimalPlaces))
	o.integer(275, int64(s.AngularUnit))
	o.integer(276, int64(s.FractionFormat))
	o.integer(277, int64(s.LinearUnitFormat))
	sep := s.DecimalSeparator
	if sep == 0 {
		sep = '.'
	}
	o.integer(278, int64(sep))
	o.integer(279, int64(s.TextMovement))
	o.integer(280, int64(s.TextHorizontalAlignment))
	o.boolean(281, s.SuppressFirstDimensionLine)
	o.boolean(282, s.SuppressSecondDimensionLine)
	o.integer(283, int64(s.ToleranceAlignment))
	o.integer(284, int64(s.ToleranceZeroHandling))
	o.integer(285, int64(s.AlternateUnitZeroHandling))
	o.integer(286, int64(s.AlternateUnitToleranceZeroHandling))
	o.integer(287, int64(s.DimensionFit))
	o.boolean(288, s.CursorUpdate)
	o.integer(289, int64(s.DimensionTextArrowFit))
	if w.version >= cadoc.AC1021 {
		o.boolean(290, s.IsExtensionLineLengthFixed)
	}
	o.ref(340, textStyleOrNil(s.Style))
	w.refIfKnown(341, s.LeaderArrow)
	w.refIfKnown(342, s.ArrowBlock)
	w.refIfKnown(343, s.DimArrow1)
	w.refIfKnown(344, s.DimArrow2)
	if w.version >= cadoc.AC1021 {
		w.refIfKnown(345, s.LineType)
		w.refIfKnown(346, s.LineTypeExt1)
		w.refIfKnown(347, s.LineTypeExt2)
	}
	o.integer(371, int64(s.DimensionLineWeight))
	o.integer(372, int64(s.ExtensionLineWeight))
}

func textStyleOrNil(t *cadoc.TextStyle) cadoc.Object {
	if t == nil {
		return nil
	}
	return t
}

// refIfKnown writes a handle when the object is part of the document.
func (w *writer) refIfKnown(code int, obj cadoc.Object) {
	if w.known(obj) {
		w.o.handle(code, obj.Base().Handle)
	}
}
