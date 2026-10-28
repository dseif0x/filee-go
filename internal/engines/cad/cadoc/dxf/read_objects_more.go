package dxf

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// decodeObjCmColor decodes an AcCmColor stored as a 32-bit integer: the high
// byte says how (0xC0 ByLayer, 0xC1 ByBlock, 0xC2 RGB, 0xC3 ACI). ACadSharp
// writes indexed colours as 0xC1|index, which is read as an index too.
func decodeObjCmColor(v int32) cadoc.Color {
	u := uint32(v)
	switch u >> 24 {
	case 0xC0:
		return cadoc.ColorByLayer
	case 0xC1:
		if i := u & 0xFF; i != 0 {
			return cadoc.ColorIndex(int16(i))
		}
		return cadoc.ColorByBlock
	case 0xC2:
		return cadoc.ColorFromTrueColor(int32(u & 0xFFFFFF))
	case 0xC3:
		return cadoc.ColorIndex(int16(u & 0xFF))
	}
	if u <= 257 {
		return cadoc.ColorIndex(int16(u))
	}
	return cadoc.ColorFromTrueColor(int32(u & 0xFFFFFF))
}

// encodeObjCmColor is the inverse of decodeObjCmColor.
func encodeObjCmColor(c cadoc.Color) int32 {
	var u uint32
	switch {
	case c.IsTrueColor():
		u = 0xC2000000 | uint32(c.TrueColor())
	case c.IsByLayer():
		u = 0xC0000000
	case c.IsByBlock():
		u = 0xC1000000
	default:
		u = 0xC3000000 | uint32(uint8(c.Index()))
	}
	return int32(u)
}

func init() {
	registerObject(cadoc.TokenObjectGroup, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		g := cadoc.NewGroup("")
		return g, func() bool {
			s := r.s
			switch s.code {
			case 300:
				g.Description = s.str
			case 70:
			case 71:
				g.Selectable = s.bool()
			case 340:
				h := s.handle()
				r.later(func() {
					if e, ok := r.object(h).(cadoc.Entity); ok {
						g.Entities = append(g.Entities, e)
					}
				})
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectMLineStyle, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		m := cadoc.NewMLineStyle("")
		return m, func() bool {
			s := r.s
			n := len(m.Elements)
			switch s.code {
			case 2:
				m.Name = s.str
			case 70:
				m.Flags = cadoc.MLineStyleFlags(s.i16())
			case 3:
				m.Description = s.str
			case 62:
				c := cadoc.ColorIndex(s.i16())
				if n > 0 {
					if !m.Elements[n-1].Color.IsTrueColor() {
						m.Elements[n-1].Color = c
					}
				} else if !m.FillColor.IsTrueColor() {
					m.FillColor = c
				}
			case 420:
				c := cadoc.ColorFromTrueColor(s.i32())
				if n > 0 {
					m.Elements[n-1].Color = c
				} else {
					m.FillColor = c
				}
			case 51:
				m.StartAngle = s.angle()
			case 52:
				m.EndAngle = s.angle()
			case 71:
			case 49:
				m.Elements = append(m.Elements, cadoc.MLineStyleElement{Offset: s.float(), Color: cadoc.ColorByLayer})
			case 6:
				if n == 0 {
					return true
				}
				name := s.str
				r.later(func() {
					if lt, ok := r.doc.LineTypes.Get(name); ok {
						m.Elements[n-1].LineType = lt
					}
				})
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectMLeaderStyle, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		m := cadoc.NewMultiLeaderStyle("")
		return m, func() bool { return r.parseMLeaderStyle(m) }
	})
	registerObject(cadoc.TokenObjectImageDefinition, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := cadoc.NewImageDefinition("")
		return d, func() bool {
			s := r.s
			switch s.code {
			case 90:
				d.ClassVersion = s.i32()
			case 1:
				d.FileName = s.str
			case 10, 20:
				setVec2(&d.Size, s.code, s.float())
			case 11, 21:
				setVec2(&d.DefaultSize, s.code, s.float())
			case 280:
				d.IsLoaded = s.bool()
			case 281:
				d.Units = cadoc.ResolutionUnit(s.int())
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectImageDefinitionReactor, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := &cadoc.ImageDefinitionReactor{ClassVersion: 2}
		return d, func() bool {
			s := r.s
			switch {
			case s.code == 90:
				d.ClassVersion = s.i32()
			case s.code == 330 && st.hasOwner:
				r.handleRef(s.handle(), func(o cadoc.Object) { d.Image, _ = o.(*cadoc.RasterImage) })
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectRasterVariables, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		v := &cadoc.RasterVariables{DisplayQuality: cadoc.ImageDisplayQualityHigh}
		return v, func() bool {
			s := r.s
			switch s.code {
			case 90:
				v.ClassVersion = s.i32()
			case 70:
				v.IsDisplayFrameShown = s.bool()
			case 71:
				v.DisplayQuality = cadoc.ImageDisplayQuality(s.i16())
			case 72:
				v.Units = cadoc.ImageUnits(s.i16())
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectWipeoutVariables, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		v := &cadoc.WipeoutVariables{}
		return v, func() bool {
			if r.s.code == 70 {
				v.DisplayImageFrame = r.s.bool()
				return true
			}
			return false
		}
	})
	registerObject(cadoc.TokenObjectXRecord, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		x := &cadoc.XRecord{}
		return x, func() bool { return r.parseXRecord(x) }
	})
	registerObject(cadoc.TokenObjectScale, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		sc := &cadoc.Scale{}
		return sc, func() bool {
			s := r.s
			switch s.code {
			case 70:
			case 300:
				sc.Name = s.str
			case 140:
				sc.PaperUnits = s.float()
			case 141:
				sc.DrawingUnits = s.float()
			case 290:
				sc.IsUnitScale = s.bool()
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectSortEntsTable, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		t := cadoc.NewSortEntitiesTable(nil)
		var ent, sort uint64
		var hasEnt, hasSort bool
		flush := func() {
			if !hasEnt || !hasSort {
				return
			}
			e, h := ent, sort
			hasEnt, hasSort = false, false
			r.later(func() {
				if en, ok := r.object(e).(cadoc.Entity); ok {
					t.Add(en, h)
				}
			})
		}
		return t, func() bool {
			s := r.s
			if r.subclass != cadoc.SubclassSortentsTable {
				return false
			}
			switch s.code {
			case 330:
				r.handleRef(s.handle(), func(o cadoc.Object) { t.BlockOwner, _ = o.(*cadoc.BlockRecord) })
			case 331:
				ent, hasEnt = s.handle(), true
				flush()
			case 5:
				sort, hasSort = s.handle(), true
				flush()
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectDBColor, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		b := &cadoc.BookColor{}
		return b, func() bool {
			s := r.s
			switch s.code {
			case 62:
				if !b.Color.IsTrueColor() {
					b.Color = cadoc.ColorIndex(s.i16())
				}
			case 420:
				b.Color = cadoc.ColorFromTrueColor(s.i32())
			case 430:
				b.Name = s.str
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectPdfDefinition, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := &cadoc.PdfUnderlayDefinition{}
		return d, func() bool {
			s := r.s
			switch s.code {
			case 1:
				d.File = s.str
			case 2:
				d.Page = s.str
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectGeoData, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		g := cadoc.NewGeoData()
		var csd string
		return g, func() bool { return r.parseGeoData(g, st, &csd) }
	})
	registerObject(cadoc.TokenObjectSpatialFilter, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		f := cadoc.NewSpatialFilter()
		f.Name = ""
		matrices, frontRead := 0, false
		return f, func() bool {
			s := r.s
			switch s.code {
			case 70:
			case 10:
				f.BoundaryPoints = append(f.BoundaryPoints, cadoc.Vec2{X: s.float()})
			case 20:
				if n := len(f.BoundaryPoints); n > 0 {
					f.BoundaryPoints[n-1].Y = s.float()
				}
			case 210, 220, 230:
				setVec3(&f.Normal, s.code, s.float())
			case 11, 21, 31:
				setVec3(&f.Origin, s.code, s.float())
			case 71:
				f.DisplayBoundary = s.bool()
			case 72:
				f.ClipFrontPlane = s.bool()
			case 73:
				f.ClipBackPlane = s.bool()
			case 41:
				if f.ClipBackPlane {
					f.BackDistance = s.float()
				}
			case 40:
				if f.ClipFrontPlane && !frontRead {
					f.FrontDistance = s.float()
					frontRead = true
					return true
				}
				var m [12]float64
				for i := 0; i < 12; i++ {
					if s.code != 40 {
						r.hold = true
						break
					}
					m[i] = s.float()
					if i < 11 {
						s.next()
					}
				}
				if matrices == 0 {
					f.InverseInsertTransform = m
				} else {
					f.InsertTransform = m
				}
				matrices++
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectMaterial, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		m := &cadoc.Material{}
		return m, func() bool {
			s := r.s
			switch s.code {
			case 1:
				m.Name = s.str
			case 2:
				m.Description = s.str
			case 5, 102, 1001, 100:
				return false
			case 330:
				if !st.hasOwner {
					return false
				}
				m.Raw = append(m.Raw, cadoc.GroupCode{Code: s.code, Value: s.value()})
			default:
				m.Raw = append(m.Raw, cadoc.GroupCode{Code: s.code, Value: s.value()})
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectVisualStyle, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		v := &cadoc.VisualStyle{}
		return v, func() bool {
			s := r.s
			switch s.code {
			case 2:
				v.Description = s.str
			case 70:
				v.Type = s.i32()
			case 5, 102, 1001, 100:
				return false
			case 330:
				if !st.hasOwner {
					return false
				}
				v.Raw = append(v.Raw, cadoc.GroupCode{Code: s.code, Value: s.value()})
			default:
				v.Raw = append(v.Raw, cadoc.GroupCode{Code: s.code, Value: s.value()})
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectPlaceholder, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		return &cadoc.AcdbPlaceHolder{}, func() bool { return false }
	})
	registerObject(cadoc.TokenBlkRefObjectContextData, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		c := &cadoc.BlockReferenceObjectContextData{XScale: 1, YScale: 1, ZScale: 1}
		c.Version = 3
		return c, func() bool {
			if r.parseContextBase(&c.ObjectContextDataBase) {
				return true
			}
			s := r.s
			switch s.code {
			case 50:
				c.Rotation = s.angle()
			case 10, 20, 30:
				setVec3(&c.InsertionPoint, s.code, s.float())
			case 41:
				c.XScale = s.float()
			case 42:
				c.YScale = s.float()
			case 43:
				c.ZScale = s.float()
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenMTextAttributeObjectContextData, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		c := &cadoc.MTextAttributeObjectContextData{AttachmentPoint: cadoc.AttachmentPointTypeTopLeft, AlignmentPoint: cadoc.AxisX}
		c.Version = 3
		return c, func() bool {
			if r.parseContextBase(&c.ObjectContextDataBase) {
				return true
			}
			s := r.s
			switch s.code {
			case 70:
				c.AttachmentPoint = cadoc.AttachmentPointType(s.i16())
			case 50:
				c.Rotation = s.angle()
			case 10, 20, 30:
				setVec3(&c.AlignmentPoint, s.code, s.float())
			case 11, 21, 31:
				setVec3(&c.InsertPoint, s.code, s.float())
			case 290:
				c.Value290 = s.bool()
			default:
				return false
			}
			return true
		}
	})
	registerObject(cadoc.TokenObjectDynamicBlockPurgePreventer, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		d := &cadoc.DynamicBlockPurgePreventer{}
		return d, func() bool {
			if r.s.code == 70 {
				d.Version = r.s.i16()
				return true
			}
			return false
		}
	})
	registerObject(cadoc.TokenObjectField, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		f := &cadoc.Field{}
		return f, r.fieldParser(f)
	})
	registerObject(cadoc.TokenObjectFieldList, func(r *reader, st *objectState) (cadoc.Object, func() bool) {
		fl := &cadoc.FieldList{}
		return fl, func() bool {
			s := r.s
			if r.subclass != cadoc.SubclassIdSet {
				return false
			}
			switch s.code {
			case 90:
			case 330:
				h := s.handle()
				r.later(func() {
					if f, ok := r.object(h).(*cadoc.Field); ok {
						fl.Fields = append(fl.Fields, f)
					}
				})
			default:
				return false
			}
			return true
		}
	})
}

// parseContextBase reads the AcDbObjectContextData and scale groups.
func (r *reader) parseContextBase(c *cadoc.ObjectContextDataBase) bool {
	s := r.s
	switch r.subclass {
	case cadoc.SubclassObjectContextData:
		switch s.code {
		case 70:
			c.Version = s.i16()
		case 290:
			c.HasFileToExtensionDictionary = s.bool()
		default:
			return false
		}
		return true
	case cadoc.SubclassAnnotScaleObjectContextData:
		if s.code == 340 {
			r.handleRef(s.handle(), func(o cadoc.Object) { c.Scale, _ = o.(*cadoc.Scale) })
			return true
		}
	}
	return false
}

func (r *reader) parseMLeaderStyle(m *cadoc.MultiLeaderStyle) bool {
	s := r.s
	switch s.code {
	case 179:
	case 170:
		m.ContentType = cadoc.LeaderContentType(s.i16())
	case 171:
		m.MultiLeaderDrawOrder = cadoc.MultiLeaderDrawOrderType(s.i16())
	case 172:
		m.LeaderDrawOrder = cadoc.LeaderDrawOrderType(s.i16())
	case 90:
		m.MaxLeaderSegmentsPoints = s.i32()
	case 40:
		m.FirstSegmentAngleConstraint = s.float()
	case 41:
		m.SecondSegmentAngleConstraint = s.float()
	case 173:
		m.PathType = cadoc.MultiLeaderPathType(s.i16())
	case 91:
		m.LineColor = decodeObjCmColor(s.i32())
	case 340:
		r.handleRef(s.handle(), func(o cadoc.Object) { m.LeaderLineType, _ = o.(*cadoc.LineType) })
	case 92:
		m.LeaderLineWeight = cadoc.LineWeight(s.int())
	case 290:
		m.EnableLanding = s.bool()
	case 42:
		m.LandingGap = s.float()
	case 291:
		m.EnableDogleg = s.bool()
	case 43:
		m.LandingDistance = s.float()
	case 3:
		m.Description = s.str
	case 341:
		r.handleRef(s.handle(), func(o cadoc.Object) { m.Arrowhead, _ = o.(*cadoc.BlockRecord) })
	case 44:
		m.ArrowheadSize = s.float()
	case 300:
		m.DefaultTextContents = s.str
	case 342:
		r.handleRef(s.handle(), func(o cadoc.Object) { m.TextStyle, _ = o.(*cadoc.TextStyle) })
	case 174:
		m.TextLeftAttachment = cadoc.TextAttachmentType(s.i16())
	case 178:
		m.TextRightAttachment = cadoc.TextAttachmentType(s.i16())
	case 175:
		m.TextAngle = cadoc.TextAngleType(s.i16())
	case 176:
		m.TextAlignment = cadoc.TextAlignmentType(s.i16())
	case 93:
		m.TextColor = decodeObjCmColor(s.i32())
	case 45:
		m.TextHeight = s.float()
	case 292:
		m.TextFrame = s.bool()
	case 297:
		m.TextAlignAlwaysLeft = s.bool()
	case 46:
		m.AlignSpace = s.float()
	case 343:
		r.handleRef(s.handle(), func(o cadoc.Object) { m.BlockContent, _ = o.(*cadoc.BlockRecord) })
	case 94:
		m.BlockContentColor = decodeObjCmColor(s.i32())
	case 47:
		m.BlockContentScale.X = s.float()
	case 49:
		m.BlockContentScale.Y = s.float()
	case 140:
		m.BlockContentScale.Z = s.float()
	case 293:
		m.EnableBlockContentScale = s.bool()
	case 141:
		m.BlockContentRotation = s.angle()
	case 294:
		m.EnableBlockContentRotation = s.bool()
	case 177:
		m.BlockContentConnection = cadoc.BlockContentConnectionType(s.i16())
	case 142:
		m.ScaleFactor = s.float()
	case 295:
		m.OverwritePropertyValue = s.bool()
	case 296:
		m.IsAnnotative = s.bool()
	case 143:
		m.BreakGapSize = s.float()
	case 271:
		m.TextAttachmentDirection = cadoc.TextAttachmentDirectionType(s.i16())
	case 272:
		m.TextBottomAttachment = cadoc.TextAttachmentType(s.i16())
	case 273:
		m.TextTopAttachment = cadoc.TextAttachmentType(s.i16())
	case 298:
		m.UnknownFlag298 = s.bool()
	default:
		return false
	}
	return true
}

func (r *reader) parseXRecord(x *cadoc.XRecord) bool {
	s := r.s
	if r.subclass != cadoc.SubclassXRecord {
		return false
	}
	switch {
	case s.code == 100 || s.code == 102 || s.code >= 1001:
		return false
	case s.code == 280 && len(x.Entries) == 0 && x.CloningFlags == 0:
		x.CloningFlags = cadoc.DictionaryCloningFlags(s.i16())
		return true
	}
	code := s.code
	switch kindOf(code) {
	case kindDouble:
		if code >= 10 && code <= 39 && (code/10)%10 == 1 {
			p := cadoc.Vec3{X: s.float()}
			s.next()
			if s.code == code+10 {
				p.Y = s.float()
				s.next()
				if s.code == code+20 {
					p.Z = s.float()
				} else {
					r.hold = true
				}
			} else {
				r.hold = true
			}
			x.Entries = append(x.Entries, cadoc.XRecordEntry{Code: code, Value: p})
			return true
		}
	case kindHandle:
		h := s.handle()
		idx := len(x.Entries)
		x.Entries = append(x.Entries, cadoc.XRecordEntry{Code: code})
		r.later(func() {
			if o := r.object(h); o != nil {
				x.Entries[idx].Value = o
			}
		})
		return true
	}
	x.Entries = append(x.Entries, cadoc.XRecordEntry{Code: code, Value: s.value()})
	return true
}

func (r *reader) parseGeoData(g *cadoc.GeoData, st *objectState, csd *string) bool {
	s := r.s
	old := g.Version == cadoc.GeoDataVersionR2009
	switch s.code {
	case 90:
		g.Version = cadoc.GeoDataVersion(s.i32())
	case 330:
		if !st.hasOwner {
			return false
		}
		r.handleRef(s.handle(), func(o cadoc.Object) { g.HostBlock, _ = o.(*cadoc.BlockRecord) })
	case 70:
		g.CoordinatesType = cadoc.DesignCoordinatesType(s.i16())
	case 10, 20, 30:
		setVec3(&g.DesignPoint, s.code, s.float())
	case 11, 21, 31:
		if !old {
			setVec3(&g.ReferencePoint, s.code, s.float())
		}
	case 40:
		if old {
			g.ReferencePoint.Y = s.float()
		} else {
			g.HorizontalUnitScale = s.float()
		}
	case 41:
		if old {
			g.ReferencePoint.X = s.float()
		} else {
			g.VerticalUnitScale = s.float()
		}
	case 42:
		if old {
			g.ReferencePoint.Z = s.float()
		}
	case 46:
		if old {
			g.HorizontalUnitScale = s.float()
		}
	case 52:
		if old {
			a := math.Pi/2 - s.angle()
			g.NorthDirection = cadoc.Vec2{X: math.Cos(a), Y: math.Sin(a)}
		}
	case 91:
		g.HorizontalUnits = cadoc.UnitsType(s.i32())
	case 92:
		g.VerticalUnits = cadoc.UnitsType(s.i32())
	case 210, 220, 230:
		setVec3(&g.UpDirection, s.code, s.float())
	case 12, 22:
		if !old {
			setVec2(&g.NorthDirection, s.code, s.float())
		}
	case 95:
		g.ScaleEstimationMethod = cadoc.ScaleEstimationType(s.i32())
	case 141:
		g.UserSpecifiedScaleFactor = s.float()
	case 294:
		g.EnableSeaLevelCorrection = s.bool()
	case 142:
		g.SeaLevelElevation = s.float()
	case 143:
		g.CoordinateProjectionRadius = s.float()
	case 303:
		*csd += s.str
	case 301:
		g.CoordinateSystemDefinition = *csd + s.str
		*csd = ""
	case 302:
		g.GeoRssTag = s.str
	case 305:
		g.ObservationFromTag = s.str
	case 306:
		g.ObservationToTag = s.str
	case 307:
		g.ObservationCoverageTag = s.str
	case 93:
		n := int(s.int())
		for i := 0; i < n && !s.eof; i++ {
			var p cadoc.GeoMeshPoint
			s.next()
			p.Source.X = s.float()
			s.next()
			p.Source.Y = s.float()
			s.next()
			p.Destination.X = s.float()
			s.next()
			p.Destination.Y = s.float()
			g.Points = append(g.Points, p)
		}
	case 96:
		n := int(s.int())
		for i := 0; i < n && !s.eof; i++ {
			var f cadoc.GeoMeshFace
			s.next()
			f.Index1 = s.i32()
			s.next()
			f.Index2 = s.i32()
			s.next()
			f.Index3 = s.i32()
			g.Faces = append(g.Faces, f)
		}
	case 3, 4, 14, 24, 15, 25, 43, 44, 45, 94, 293, 16, 26, 17, 27, 54, 140, 304, 292:
	default:
		return false
	}
	return true
}

// fieldParser reads a FIELD; long texts arrive as continuation chunks
// (3 for the field code, 9 for the format) before their final group.
func (r *reader) fieldParser(f *cadoc.Field) func() bool {
	var code, format string
	return func() bool {
		s := r.s
		if r.subclass != cadoc.SubclassField {
			return false
		}
		switch s.code {
		case 1:
			f.EvaluatorId = s.str
		case 3:
			code += s.str
		case 2:
			f.FieldCode = code + s.str
			code = ""
		case 9:
			format += s.str
		case 301:
			f.FormatString = format + s.str
			format = ""
		case 90, 93, 97, 98:
		case 91:
			f.EvaluationOptionFlags = cadoc.EvaluationOptionFlags(s.i32())
		case 92:
			f.FilingOptionFlags = cadoc.FilingOptionFlags(s.i32())
		case 94:
			f.FieldStateFlags = cadoc.FieldStateFlags(s.i32())
		case 95:
			f.EvaluationStatusFlags = cadoc.EvaluationStatusFlags(s.i32())
		case 96:
			f.EvaluationErrorCode = s.i32()
		case 300:
			f.EvaluationErrorMessage = s.str
		case 6:
			key := s.str
			idx := len(f.Values)
			f.Values = append(f.Values, cadoc.FieldValue{Key: key})
			r.readCadValue(func() *cadoc.CadValue { return &f.Values[idx].Value })
		case 7:
			r.readCadValue(func() *cadoc.CadValue { return &f.Value })
		case 331:
			h := s.handle()
			r.later(func() {
				if o := r.object(h); o != nil {
					f.CadObjects = append(f.CadObjects, o)
				}
			})
		case 360:
			h := s.handle()
			r.later(func() {
				if c, ok := r.object(h).(*cadoc.Field); ok {
					f.Children = append(f.Children, c)
				}
			})
		default:
			return false
		}
		return true
	}
}

// readCadValue reads a CadValue up to its 304 end group; target gives the
// value's address (re-evaluated because the slice holding it may grow).
func (r *reader) readCadValue(target func() *cadoc.CadValue) {
	s := r.s
	var text string
	s.next()
	for !r.at0() && s.code != 304 {
		v := target()
		switch s.code {
		case 2:
			text += s.str
		case 1:
			v.Value = text + s.str
			text = ""
		case 11:
			v.Value = cadoc.Vec3{X: s.float()}
		case 21:
			if p, ok := v.Value.(cadoc.Vec3); ok {
				p.Y = s.float()
				v.Value = p
			}
		case 31:
			if p, ok := v.Value.(cadoc.Vec3); ok {
				p.Z = s.float()
				v.Value = p
			}
		case 90:
			v.ValueType = cadoc.CadValueType(s.i32())
		case 91:
			v.Value = s.i32()
		case 93:
			v.Flags = s.i32()
		case 94:
			v.Units = cadoc.CadValueUnitType(s.i32())
		case 140:
			v.Value = s.float()
		case 300:
			v.Format = s.str
		case 302:
			v.FormattedValue = s.str
		case 330:
			h := s.handle()
			r.later(func() {
				if o := r.object(h); o != nil {
					target().Value = o
				}
			})
		}
		s.next()
	}
	if r.at0() {
		r.hold = true
	}
}
