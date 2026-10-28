package dxf

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func (r *reader) readTables() {
	s := r.s
	s.next()
	for !s.eof && !(s.code == 0 && s.str == tokEndSection) {
		if s.code == 0 && s.str == tokTable {
			r.readTable()
			continue
		}
		r.warn("Unexpected token at the beginning of a table: %s", s.str)
		s.next()
	}
}

// readTable reads one TABLE … ENDTAB; afterwards the reader is past ENDTAB.
func (r *reader) readTable() {
	s := r.s
	s.next()
	name := ""
	if s.code == 2 {
		name = strings.ToUpper(s.str)
		s.next()
	}
	var table cadoc.Object
	switch name {
	case "APPID":
		table = r.doc.AppIds
	case "BLOCK_RECORD":
		table = r.doc.BlockRecords
	case "VPORT":
		table = r.doc.VPorts
	case "LTYPE":
		table = r.doc.LineTypes
	case "LAYER":
		table = r.doc.Layers
	case "STYLE":
		table = r.doc.TextStyles
	case "VIEW":
		table = r.doc.Views
	case "UCS":
		table = r.doc.UCSs
	case "DIMSTYLE":
		table = r.doc.DimensionStyles
	default:
		r.warn("Unknown table name %s", name)
	}
	st := &objectState{}
	if table != nil {
		st.obj = table
	} else {
		st.obj = &cadoc.UnknownObject{}
	}
	// Table control data up to the first entry.
	r.subclass = ""
	for !r.at0() {
		if r.hold {
			r.hold = false
		}
		switch {
		case s.code == 100 && s.str == cadoc.SubclassDimensionStyleTable:
			// The DIMSTYLE table lists its entries' handles (340) here.
			for !r.at0() {
				s.next()
			}
			continue
		case s.code == 70, s.code == 71, s.code == 340:
		default:
			r.commonObjectCode(st)
		}
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
	if table != nil {
		r.finishObject(st)
	}
	for !s.eof && !(s.code == 0 && (s.str == tokEndTable || s.str == tokEndSection || s.str == tokEOF)) {
		if s.code != 0 {
			s.next()
			continue
		}
		var entry cadoc.TableEntry
		ok := r.guard("a table entry", func() { entry = r.readTableEntry(name) })
		if !ok || entry == nil {
			continue
		}
		r.addEntry(table, entry)
	}
	if s.code == 0 && s.str == tokEndTable {
		s.next()
	}
}

func (r *reader) addEntry(table cadoc.Object, entry cadoc.TableEntry) {
	var err error
	switch t := table.(type) {
	case *cadoc.AppIdsTable:
		err = t.Add(entry.(*cadoc.AppId))
	case *cadoc.BlockRecordsTable:
		err = t.Add(entry.(*cadoc.BlockRecord))
	case *cadoc.VPortsTable:
		err = t.Add(entry.(*cadoc.VPort))
	case *cadoc.LineTypesTable:
		err = t.Add(entry.(*cadoc.LineType))
	case *cadoc.LayersTable:
		err = t.Add(entry.(*cadoc.Layer))
	case *cadoc.TextStylesTable:
		err = t.Add(entry.(*cadoc.TextStyle))
	case *cadoc.ViewsTable:
		err = t.Add(entry.(*cadoc.View))
	case *cadoc.UCSTable:
		err = t.Add(entry.(*cadoc.UCS))
	case *cadoc.DimensionStylesTable:
		err = t.Add(entry.(*cadoc.DimensionStyle))
	}
	if err != nil {
		if !r.cfg.Failsafe {
			panic(err)
		}
		r.warn("Duplicated entry with name %s found in %s", entry.EntryName(), table.ObjectName())
		// Keep references by handle working: they point at the first entry.
		if prev, ok := r.objects[entry.Base().Handle]; ok && prev == entry {
			delete(r.objects, entry.Base().Handle)
		}
	}
}

// readTableEntry reads one entry; the reader is on its code 0.
func (r *reader) readTableEntry(table string) cadoc.TableEntry {
	s := r.s
	var entry cadoc.TableEntry
	var parse func() bool
	st := &objectState{}
	switch table {
	case "APPID":
		e := cadoc.NewAppId("")
		entry, parse = e, func() bool { return false }
	case "BLOCK_RECORD":
		e := cadoc.NewBlockRecord("")
		entry, parse = e, func() bool { return r.parseBlockRecord(e) }
	case "VPORT":
		e := cadoc.NewVPort("")
		entry, parse = e, func() bool { return r.parseVPort(e) }
	case "LTYPE":
		e := cadoc.NewLineType("")
		entry, parse = e, func() bool { return r.parseLineType(e) }
	case "LAYER":
		e := cadoc.NewLayer("")
		entry, parse = e, func() bool { return r.parseLayer(e) }
	case "STYLE":
		e := cadoc.NewTextStyle("")
		entry, parse = e, func() bool { return r.parseTextStyle(e) }
	case "VIEW":
		e := cadoc.NewView("")
		entry, parse = e, func() bool { return r.parseView(e) }
	case "UCS":
		e := cadoc.NewUCS("")
		entry, parse = e, func() bool { return r.parseUCS(e) }
	case "DIMSTYLE":
		e := cadoc.NewDimensionStyle("")
		ds := &dimStyleState{}
		entry, parse = e, func() bool { return r.parseDimStyle(e, ds) }
	default:
		s.next()
		for !r.at0() {
			s.next()
		}
		return nil
	}
	st.obj = entry
	r.subclass = ""
	s.next()
	for !r.at0() {
		if !parse() {
			switch s.code {
			case 2:
				if s.str != "" {
					entry.SetEntryName(s.str)
				}
			case 70:
				entry.SetEntryFlags(int16(uint16(s.int())))
			case 105:
				entry.Base().Handle = s.handle()
			default:
				r.commonObjectCode(st)
			}
		}
		if r.hold {
			r.hold = false
			continue
		}
		s.next()
	}
	if entry.EntryName() == "" {
		if ts, ok := entry.(*cadoc.TextStyle); ok && ts.IsShapeFile() {
			// Shape file styles are written without a name.
			ts.Name = r.uniqueShapeStyleName(ts.Filename)
		} else {
			r.warn("Table entry without a name in %s, discarded", table)
			return nil
		}
	}
	r.finishObject(st)
	return entry
}

func (r *reader) uniqueShapeStyleName(file string) string {
	base := strings.TrimSuffix(file, ".shx")
	if base == "" {
		base = "SHAPE"
	}
	name := base
	for i := 1; r.doc.TextStyles.Contains(name); i++ {
		name = base + "_" + itoa(i)
	}
	return name
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (r *reader) parseBlockRecord(e *cadoc.BlockRecord) bool {
	s := r.s
	switch s.code {
	case 70:
		e.Units = cadoc.UnitsType(s.i16())
	case 340:
		h := s.handle()
		r.later(func() {
			if l, ok := r.object(h).(*cadoc.Layout); ok {
				e.Layout = l
				if l.AssociatedBlock == nil {
					l.AssociatedBlock = e
				}
			}
		})
	case 280:
		e.IsExplodable = s.bool()
	case 281:
		e.CanScale = s.bool()
	case 310:
		e.Preview = append(e.Preview, s.chunk...)
	default:
		return false
	}
	return true
}

func (r *reader) parseLayer(e *cadoc.Layer) bool {
	s := r.s
	switch s.code {
	case 6:
		name := s.str
		r.later(func() {
			if lt, ok := r.doc.LineTypes.Get(name); ok {
				e.LineType = lt
			} else if name != "" {
				r.warn("Line type %s not found for layer %s", name, e.Name)
			}
		})
	case 62:
		index := s.i16()
		if index < 0 {
			e.IsOn = false
			index = -index
		}
		c := cadoc.ColorIndex(index)
		if c.IsByBlock() || c.IsByLayer() {
			r.warn("Wrong index %d for layer %s", index, e.Name)
		} else if !e.Color.IsTrueColor() {
			e.Color = c
		}
	case 420:
		e.Color = cadoc.ColorFromTrueColor(s.i32())
	case 290:
		e.PlotFlag = s.bool()
	case 370:
		e.LineWeight = cadoc.LineWeight(s.i16())
	case 347:
		h := s.handle()
		r.later(func() {
			if m, ok := r.object(h).(*cadoc.Material); ok {
				e.Material = m
			}
		})
	case 348, 430:
	case 390:
		e.PlotStyleName = s.handle()
	default:
		return false
	}
	return true
}

func (r *reader) parseLineType(e *cadoc.LineType) bool {
	s := r.s
	switch s.code {
	case 3:
		e.Description = s.str
	case 72:
		e.Alignment = rune(s.int())
	case 73, 40:
	case 49:
		for s.code == 49 && !s.eof {
			seg := cadoc.LineTypeSegment{Length: s.float(), Scale: 1}
			s.next()
			for !r.at0() && s.code != 49 {
				switch s.code {
				case 9:
					seg.Text = s.str
				case 44:
					seg.Offset.X = s.float()
				case 45:
					seg.Offset.Y = s.float()
				case 46:
					seg.Scale = s.float()
				case 50:
					seg.Rotation = s.angle()
				case 74:
					seg.Flags = cadoc.LineTypeShapeFlags(uint16(s.int()))
				case 75:
					seg.ShapeNumber = s.i16()
				case 340:
					h := s.handle()
					idx := len(e.Segments)
					r.later(func() {
						if ts, ok := r.object(h).(*cadoc.TextStyle); ok && idx < len(e.Segments) {
							e.Segments[idx].Style = ts
						}
					})
				default:
					if s.code >= 1000 || s.code == 100 || s.code == 102 || s.code == 330 {
						// Not a segment group: hand it back.
						e.Segments = append(e.Segments, seg)
						r.hold = true
						return true
					}
				}
				s.next()
			}
			e.Segments = append(e.Segments, seg)
		}
		r.hold = true
	default:
		return false
	}
	return true
}

func (r *reader) parseTextStyle(e *cadoc.TextStyle) bool {
	s := r.s
	switch s.code {
	case 2:
		if s.str != "" {
			e.Name = s.str
		}
	case 3:
		e.Filename = s.str
	case 4:
		e.BigFontFilename = s.str
	case 40:
		e.Height = s.float()
	case 41:
		e.Width = s.float()
	case 42:
		e.LastHeight = s.float()
	case 50:
		e.ObliqueAngle = s.angle()
	case 71:
		e.MirrorFlag = cadoc.TextMirrorFlag(s.i16())
	case 1071:
		e.TrueType = cadoc.FontFlags(s.i32())
	default:
		return false
	}
	return true
}

func (r *reader) parseUCS(e *cadoc.UCS) bool {
	s := r.s
	switch s.code {
	case 10, 20, 30:
		setVec3(&e.Origin, s.code, s.float())
	case 11, 21, 31:
		setVec3(&e.XAxis, s.code, s.float())
	case 12, 22, 32:
		setVec3(&e.YAxis, s.code, s.float())
	case 71:
		e.OrthographicType = cadoc.OrthographicType(s.i16())
	case 79:
		e.OrthographicViewType = cadoc.OrthographicType(s.i16())
	case 146:
		e.Elevation = s.float()
	case 13, 23, 33:
	case 346:
		h := s.handle()
		r.later(func() { e.BaseUCS, _ = r.object(h).(*cadoc.UCS) })
	default:
		return false
	}
	return true
}

func (r *reader) parseView(e *cadoc.View) bool {
	s := r.s
	switch s.code {
	case 40:
		e.Height = s.float()
	case 41:
		e.Width = s.float()
	case 42:
		e.LensLength = s.float()
	case 43:
		e.FrontClipping = s.float()
	case 44:
		e.BackClipping = s.float()
	case 50:
		e.Angle = s.angle()
	case 71:
		e.ViewMode = cadoc.ViewModeType(s.i16())
	case 72:
		e.IsUcsAssociated = s.bool()
	case 73:
		e.IsPlottable = s.bool()
	case 281:
		e.RenderMode = cadoc.RenderMode(s.int())
	case 10, 20:
		setVec2(&e.Center, s.code, s.float())
	case 11, 21, 31:
		setVec3(&e.Direction, s.code, s.float())
	case 12, 22, 32:
		setVec3(&e.Target, s.code, s.float())
	case 110, 120, 130:
		setVec3(&e.UcsOrigin, s.code, s.float())
	case 111, 121, 131:
		setVec3(&e.UcsXAxis, s.code, s.float())
	case 112, 122, 132:
		setVec3(&e.UcsYAxis, s.code, s.float())
	case 146:
		e.UcsElevation = s.float()
	case 79:
		e.UcsOrthographicType = cadoc.OrthographicType(s.i16())
	case 345, 346, 348, 332, 334, 361:
		code, h := s.code, s.handle()
		r.later(func() {
			switch code {
			case 345:
				e.NamedUcs, _ = r.object(h).(*cadoc.UCS)
			case 346:
				e.BaseUcs, _ = r.object(h).(*cadoc.UCS)
			case 348:
				e.VisualStyle = r.object(h)
			}
		})
	default:
		return false
	}
	return true
}

func (r *reader) parseVPort(e *cadoc.VPort) bool {
	s := r.s
	switch s.code {
	case 10, 20:
		setVec2(&e.BottomLeft, s.code, s.float())
	case 11, 21:
		setVec2(&e.TopRight, s.code, s.float())
	case 12, 22:
		setVec2(&e.Center, s.code, s.float())
	case 13, 23:
		setVec2(&e.SnapBasePoint, s.code, s.float())
	case 14, 24:
		setVec2(&e.SnapSpacing, s.code, s.float())
	case 15, 25:
		setVec2(&e.GridSpacing, s.code, s.float())
	case 16, 26, 36:
		setVec3(&e.Direction, s.code, s.float())
	case 17, 27, 37:
		setVec3(&e.Target, s.code, s.float())
	case 40:
		e.ViewHeight = s.float()
	case 41:
		e.AspectRatio = s.float()
	case 42:
		e.LensLength = s.float()
	case 43:
		e.FrontClippingPlane = s.float()
	case 44:
		e.BackClippingPlane = s.float()
	case 50:
		e.SnapRotation = s.angle()
	case 51:
		e.TwistAngle = s.angle()
	case 72:
		e.CircleZoomPercent = s.i16()
	case 281:
		e.RenderMode = cadoc.RenderMode(s.int())
	case 71:
		e.ViewMode = cadoc.ViewModeType(s.i16())
	case 74:
		e.UcsIconDisplay = cadoc.UscIconType(s.i16())
	case 75:
		e.SnapOn = s.bool()
	case 76:
		e.ShowGrid = s.bool()
	case 77:
		e.IsometricSnap = s.bool()
	case 78:
		e.SnapIsoPair = s.i16()
	case 110, 120, 130:
		setVec3(&e.Origin, s.code, s.float())
	case 111, 121, 131:
		setVec3(&e.XAxis, s.code, s.float())
	case 112, 122, 132:
		setVec3(&e.YAxis, s.code, s.float())
	case 79:
		e.OrthographicType = cadoc.OrthographicType(s.i16())
	case 146:
		e.Elevation = s.float()
	case 60:
		e.GridFlags = cadoc.GridFlags(s.i16())
	case 61:
		e.MinorGridLinesPerMajorGridLine = s.i16()
	case 292:
		e.UseDefaultLighting = s.bool()
	case 282:
		e.DefaultLighting = cadoc.DefaultLightingType(s.i16())
	case 141:
		e.Brightness = s.float()
	case 142:
		e.Contrast = s.float()
	case 63:
		if !e.AmbientColor.IsTrueColor() {
			e.AmbientColor = cadoc.ColorIndex(s.i16())
		}
	case 421:
		e.AmbientColor = cadoc.ColorFromTrueColor(s.i32())
	case 65, 73, 431, 332, 333:
	case 345, 346, 348:
		code, h := s.code, s.handle()
		r.later(func() {
			switch code {
			case 345:
				e.NamedUcs, _ = r.object(h).(*cadoc.UCS)
			case 346:
				e.BaseUcs, _ = r.object(h).(*cadoc.UCS)
			case 348:
				e.VisualStyle = r.object(h)
			}
		})
	default:
		return false
	}
	return true
}

type dimStyleState struct {
	flagsSeen bool
}

func (r *reader) parseDimStyle(e *cadoc.DimensionStyle, ds *dimStyleState) bool {
	s := r.s
	switch s.code {
	case 3:
		e.PostFix = s.str
	case 4:
		e.AlternateDimensioningSuffix = s.str
	case 5, 6, 7:
		// R12 names of DIMBLK, DIMBLK1, DIMBLK2.
		code, name := s.code, s.str
		r.later(func() {
			b, ok := r.doc.BlockRecords.Get(name)
			if !ok {
				return
			}
			switch code {
			case 5:
				e.ArrowBlock = b
			case 6:
				e.DimArrow1 = b
			case 7:
				e.DimArrow2 = b
			}
		})
	case 40:
		e.ScaleFactor = s.float()
	case 41:
		e.ArrowSize = s.float()
	case 42:
		e.ExtensionLineOffset = s.float()
	case 43:
		e.DimensionLineIncrement = s.float()
	case 44:
		e.ExtensionLineExtension = s.float()
	case 45:
		e.Rounding = s.float()
	case 46:
		e.DimensionLineExtension = s.float()
	case 47:
		e.PlusTolerance = s.float()
	case 48:
		e.MinusTolerance = s.float()
	case 49:
		e.FixedExtensionLineLength = s.float()
	case 50:
		e.JoggedRadiusDimensionTransverseSegmentAngle = s.angle()
	case 69:
		e.TextBackgroundFillMode = cadoc.DimensionTextBackgroundFillMode(s.i16())
	case 70:
		if !ds.flagsSeen {
			ds.flagsSeen = true
			e.Flags = cadoc.StandardFlags(s.i16())
		} else if s.i16() >= 0 {
			e.TextBackgroundColor = cadoc.ColorIndex(s.i16())
		}
	case 71:
		e.GenerateTolerances = s.bool()
	case 72:
		e.LimitsGeneration = s.bool()
	case 73:
		e.TextInsideHorizontal = s.bool()
	case 74:
		e.TextOutsideHorizontal = s.bool()
	case 75:
		e.SuppressFirstExtensionLine = s.bool()
	case 76:
		e.SuppressSecondExtensionLine = s.bool()
	case 77:
		e.TextVerticalAlignment = cadoc.DimensionTextVerticalAlignment(s.i16())
	case 78:
		e.ZeroHandling = cadoc.ZeroHandling(s.i16())
	case 79:
		e.AngularZeroHandling = cadoc.AngularZeroHandling(s.i16())
	case 90:
		e.ArcLengthSymbolPosition = cadoc.ArcLengthSymbolPosition(s.i16())
	case 105:
		e.Handle = s.handle()
	case 140:
		e.TextHeight = s.float()
	case 141:
		e.CenterMarkSize = s.float()
	case 142:
		e.TickSize = s.float()
	case 143:
		e.AlternateUnitScaleFactor = s.float()
	case 144:
		e.LinearScaleFactor = s.float()
	case 145:
		e.TextVerticalPosition = s.float()
	case 146:
		e.ToleranceScaleFactor = s.float()
	case 147:
		e.DimensionLineGap = s.float()
	case 148:
		e.AlternateUnitRounding = s.float()
	case 170:
		e.AlternateUnitDimensioning = s.bool()
	case 171:
		e.AlternateUnitDecimalPlaces = s.i16()
	case 172:
		e.TextOutsideExtensions = s.bool()
	case 173:
		e.SeparateArrowBlocks = s.bool()
	case 174:
		e.TextInsideExtensions = s.bool()
	case 175:
		e.SuppressOutsideExtensions = s.bool()
	case 176:
		e.DimensionLineColor = cadoc.ColorIndex(s.i16())
	case 177:
		e.ExtensionLineColor = cadoc.ColorIndex(s.i16())
	case 178:
		e.TextColor = cadoc.ColorIndex(s.i16())
	case 179:
		e.AngularDecimalPlaces = s.i16()
	case 270:
		e.LinearUnitFormat = cadoc.LinearUnitFormat(s.i16())
	case 271:
		e.DecimalPlaces = s.i16()
	case 272:
		e.ToleranceDecimalPlaces = s.i16()
	case 273:
		e.AlternateUnitFormat = cadoc.LinearUnitFormat(s.i16())
	case 274:
		e.AlternateUnitToleranceDecimalPlaces = s.i16()
	case 275:
		e.AngularUnit = cadoc.AngularUnitFormat(s.i16())
	case 276:
		e.FractionFormat = cadoc.FractionFormat(s.i16())
	case 277:
		e.LinearUnitFormat = cadoc.LinearUnitFormat(s.i16())
	case 278:
		e.DecimalSeparator = rune(s.i16())
	case 279:
		e.TextMovement = cadoc.TextMovement(s.i16())
	case 280:
		e.TextHorizontalAlignment = cadoc.DimensionTextHorizontalAlignment(s.i16())
	case 281:
		e.SuppressFirstDimensionLine = s.bool()
	case 282:
		e.SuppressSecondDimensionLine = s.bool()
	case 283:
		e.ToleranceAlignment = cadoc.ToleranceAlignment(s.i16())
	case 284:
		e.ToleranceZeroHandling = cadoc.ZeroHandling(s.i16())
	case 285:
		e.AlternateUnitZeroHandling = cadoc.ZeroHandling(s.i16())
	case 286:
		e.AlternateUnitToleranceZeroHandling = cadoc.ZeroHandling(uint8(s.i16()))
	case 287:
		e.DimensionFit = s.i16()
	case 288:
		e.CursorUpdate = s.bool()
	case 289:
		e.DimensionTextArrowFit = cadoc.TextArrowFitType(s.i16())
	case 290:
		e.IsExtensionLineLengthFixed = s.bool()
	case 294:
		e.TextDirection = cadoc.TextDirection(s.i16())
	case 340, 341, 342, 343, 344, 345, 346, 347:
		code, h := s.code, s.handle()
		r.later(func() {
			o := r.object(h)
			switch code {
			case 340:
				if ts, ok := o.(*cadoc.TextStyle); ok {
					e.Style = ts
				}
			case 341:
				e.LeaderArrow, _ = o.(*cadoc.BlockRecord)
			case 342:
				e.ArrowBlock, _ = o.(*cadoc.BlockRecord)
			case 343:
				e.DimArrow1, _ = o.(*cadoc.BlockRecord)
			case 344:
				e.DimArrow2, _ = o.(*cadoc.BlockRecord)
			case 345:
				e.LineType, _ = o.(*cadoc.LineType)
			case 346:
				e.LineTypeExt1, _ = o.(*cadoc.LineType)
			case 347:
				e.LineTypeExt2, _ = o.(*cadoc.LineType)
			}
		})
	case 371:
		e.DimensionLineWeight = cadoc.LineWeight(s.i16())
	case 372:
		e.ExtensionLineWeight = cadoc.LineWeight(s.i16())
	default:
		return false
	}
	return true
}

func setVec3(v *cadoc.Vec3, code int, f float64) {
	switch (code / 10) % 10 {
	case 1:
		v.X = f
	case 2:
		v.Y = f
	case 3:
		v.Z = f
	}
}

func setVec2(v *cadoc.Vec2, code int, f float64) {
	switch (code / 10) % 10 {
	case 1:
		v.X = f
	case 2:
		v.Y = f
	}
}
