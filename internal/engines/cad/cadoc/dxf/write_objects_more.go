package dxf

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func init() {
	registerObjectWriter(&cadoc.Group{}, func(w *writer, obj cadoc.Object) {
		g := obj.(*cadoc.Group)
		o := w.o
		o.str(100, cadoc.SubclassGroup)
		o.str(300, g.Description)
		o.boolean(70, g.IsUnnamed())
		o.boolean(71, g.Selectable)
		for _, e := range g.Entities {
			if w.known(e) {
				o.handle(340, e.Base().Handle)
			}
		}
	})
	registerObjectWriter(&cadoc.MLineStyle{}, func(w *writer, obj cadoc.Object) {
		m := obj.(*cadoc.MLineStyle)
		o := w.o
		o.str(100, cadoc.SubclassMLineStyle)
		o.str(2, m.Name)
		o.integer(70, int64(m.Flags))
		o.str(3, m.Description)
		o.integer(62, int64(m.FillColor.ApproxIndex()))
		if m.FillColor.IsTrueColor() && w.version >= cadoc.AC1018 {
			o.integer(420, int64(m.FillColor.TrueColor()))
		}
		o.angle(51, m.StartAngle)
		o.angle(52, m.EndAngle)
		o.integer(71, int64(len(m.Elements)))
		for _, e := range m.Elements {
			o.dbl(49, e.Offset)
			o.integer(62, int64(e.Color.ApproxIndex()))
			if e.Color.IsTrueColor() && w.version >= cadoc.AC1018 {
				o.integer(420, int64(e.Color.TrueColor()))
			}
			lt := cadoc.ByLayerName
			if e.LineType != nil {
				lt = e.LineType.Name
			}
			o.str(6, lt)
		}
	})
	registerObjectWriter(&cadoc.MultiLeaderStyle{}, func(w *writer, obj cadoc.Object) {
		w.writeMLeaderStyle(obj.(*cadoc.MultiLeaderStyle))
	})
	registerObjectWriter(&cadoc.ImageDefinition{}, func(w *writer, obj cadoc.Object) {
		d := obj.(*cadoc.ImageDefinition)
		o := w.o
		o.str(100, cadoc.SubclassRasterImageDef)
		o.integer(90, int64(d.ClassVersion))
		o.str(1, d.FileName)
		o.vec2(10, d.Size)
		o.vec2(11, d.DefaultSize)
		o.boolean(280, d.IsLoaded)
		o.integer(281, int64(d.Units))
	})
	registerObjectWriter(&cadoc.ImageDefinitionReactor{}, func(w *writer, obj cadoc.Object) {
		d := obj.(*cadoc.ImageDefinitionReactor)
		o := w.o
		o.str(100, cadoc.SubclassRasterImageDefReactor)
		o.integer(90, int64(d.ClassVersion))
		if d.Image != nil && w.known(d.Image) {
			o.handle(330, d.Image.Handle)
		} else {
			o.handle(330, 0)
		}
	})
	registerObjectWriter(&cadoc.RasterVariables{}, func(w *writer, obj cadoc.Object) {
		v := obj.(*cadoc.RasterVariables)
		o := w.o
		o.str(100, cadoc.SubclassRasterVariables)
		o.integer(90, int64(v.ClassVersion))
		o.boolean(70, v.IsDisplayFrameShown)
		o.integer(71, int64(v.DisplayQuality))
		o.integer(72, int64(v.Units))
	})
	registerObjectWriter(&cadoc.WipeoutVariables{}, func(w *writer, obj cadoc.Object) {
		w.o.str(100, cadoc.SubclassWipeoutVariables)
		w.o.boolean(70, obj.(*cadoc.WipeoutVariables).DisplayImageFrame)
	})
	registerObjectWriter(&cadoc.XRecord{}, func(w *writer, obj cadoc.Object) {
		x := obj.(*cadoc.XRecord)
		o := w.o
		o.str(100, cadoc.SubclassXRecord)
		o.integer(280, int64(x.CloningFlags))
		for _, e := range x.Entries {
			if kindOf(e.Code) == kindHandle {
				switch v := e.Value.(type) {
				case cadoc.Object:
					if w.known(v) {
						o.handle(e.Code, v.Base().Handle)
					}
				case uint64:
					if _, ok := w.index[v]; ok {
						o.handle(e.Code, v)
					}
				}
				continue
			}
			if kindOf(e.Code) == kindNone {
				continue
			}
			o.any(e.Code, e.Value)
		}
	})
	registerObjectWriter(&cadoc.Scale{}, func(w *writer, obj cadoc.Object) {
		sc := obj.(*cadoc.Scale)
		o := w.o
		o.str(100, cadoc.SubclassScale)
		o.integer(70, 0)
		o.str(300, sc.Name)
		o.dbl(140, sc.PaperUnits)
		o.dbl(141, sc.DrawingUnits)
		o.boolean(290, sc.IsUnitScale)
	})
	registerObjectWriter(&cadoc.SortEntitiesTable{}, func(w *writer, obj cadoc.Object) {
		t := obj.(*cadoc.SortEntitiesTable)
		o := w.o
		o.str(100, cadoc.SubclassSortentsTable)
		if t.BlockOwner != nil && w.known(t.BlockOwner) {
			o.handle(330, t.BlockOwner.Handle)
		} else {
			o.handle(330, 0)
		}
		for _, s := range t.Sorted() {
			if !w.known(s.Entity) {
				continue
			}
			o.handle(331, s.Entity.Base().Handle)
			o.handle(5, s.SortHandle)
		}
	})
	registerObjectWriter(&cadoc.BookColor{}, func(w *writer, obj cadoc.Object) {
		b := obj.(*cadoc.BookColor)
		o := w.o
		o.str(100, cadoc.SubclassDbColor)
		o.integer(62, int64(b.Color.ApproxIndex()))
		if b.Color.IsTrueColor() {
			o.integer(420, int64(b.Color.TrueColor()))
		}
		o.str(430, b.Name)
	})
	registerObjectWriter(&cadoc.PdfUnderlayDefinition{}, func(w *writer, obj cadoc.Object) {
		d := obj.(*cadoc.PdfUnderlayDefinition)
		w.o.str(100, cadoc.SubclassUnderlayDefinition)
		w.o.str(1, d.File)
		w.o.str(2, d.Page)
	})
	registerObjectWriter(&cadoc.GeoData{}, func(w *writer, obj cadoc.Object) { w.writeGeoData(obj.(*cadoc.GeoData)) })
	registerObjectWriter(&cadoc.SpatialFilter{}, func(w *writer, obj cadoc.Object) {
		f := obj.(*cadoc.SpatialFilter)
		o := w.o
		o.str(100, cadoc.SubclassFilter)
		o.str(100, cadoc.SubclassSpatialFilter)
		o.integer(70, int64(len(f.BoundaryPoints)))
		for _, p := range f.BoundaryPoints {
			o.vec2(10, p)
		}
		o.vec3(210, f.Normal)
		o.vec3(11, f.Origin)
		o.boolean(71, f.DisplayBoundary)
		o.boolean(72, f.ClipFrontPlane)
		if f.ClipFrontPlane {
			o.dbl(40, f.FrontDistance)
		}
		o.boolean(73, f.ClipBackPlane)
		if f.ClipBackPlane {
			o.dbl(41, f.BackDistance)
		}
		for _, v := range f.InverseInsertTransform {
			o.dbl(40, v)
		}
		for _, v := range f.InsertTransform {
			o.dbl(40, v)
		}
	})
	registerObjectWriter(&cadoc.AcdbPlaceHolder{}, func(w *writer, obj cadoc.Object) {})
	registerObjectWriter(&cadoc.BlockReferenceObjectContextData{}, func(w *writer, obj cadoc.Object) {
		c := obj.(*cadoc.BlockReferenceObjectContextData)
		o := w.o
		w.writeContextBase(&c.ObjectContextDataBase)
		o.angle(50, c.Rotation)
		o.vec3(10, c.InsertionPoint)
		o.dbl(41, c.XScale)
		o.dbl(42, c.YScale)
		o.dbl(43, c.ZScale)
	})
	registerObjectWriter(&cadoc.MTextAttributeObjectContextData{}, func(w *writer, obj cadoc.Object) {
		c := obj.(*cadoc.MTextAttributeObjectContextData)
		o := w.o
		w.writeContextBase(&c.ObjectContextDataBase)
		o.integer(70, int64(c.AttachmentPoint))
		o.angle(50, c.Rotation)
		o.vec3(10, c.AlignmentPoint)
		o.vec3(11, c.InsertPoint)
		o.boolean(290, c.Value290)
	})
	registerObjectWriter(&cadoc.DynamicBlockPurgePreventer{}, func(w *writer, obj cadoc.Object) {
		w.o.str(100, cadoc.SubclassAcDbDynamicBlockPurgePreventer)
		w.o.integer(70, int64(obj.(*cadoc.DynamicBlockPurgePreventer).Version))
	})
	registerObjectWriter(&cadoc.Field{}, func(w *writer, obj cadoc.Object) { w.writeField(obj.(*cadoc.Field)) })
	registerObjectWriter(&cadoc.FieldList{}, func(w *writer, obj cadoc.Object) {
		fl := obj.(*cadoc.FieldList)
		o := w.o
		o.str(100, cadoc.SubclassIdSet)
		o.integer(90, int64(len(fl.Fields)))
		for _, f := range fl.Fields {
			o.handle(330, f.Handle)
			f.Owner = fl
			w.enqueue(f)
		}
		o.str(100, cadoc.SubclassFieldList)
	})
}

func (w *writer) writeContextBase(c *cadoc.ObjectContextDataBase) {
	o := w.o
	o.str(100, cadoc.SubclassObjectContextData)
	o.integer(70, int64(c.Version))
	o.boolean(290, c.HasFileToExtensionDictionary)
	o.str(100, cadoc.SubclassAnnotScaleObjectContextData)
	if c.Scale != nil && w.known(c.Scale) {
		o.handle(340, c.Scale.Handle)
	} else {
		o.handle(340, 0)
	}
}

func (w *writer) writeMLeaderStyle(s *cadoc.MultiLeaderStyle) {
	o := w.o
	o.str(100, cadoc.SubclassMLeaderStyle)
	o.integer(179, 2)
	o.integer(170, int64(s.ContentType))
	o.integer(171, int64(s.MultiLeaderDrawOrder))
	o.integer(172, int64(s.LeaderDrawOrder))
	o.integer(90, int64(s.MaxLeaderSegmentsPoints))
	o.dbl(40, s.FirstSegmentAngleConstraint)
	o.dbl(41, s.SecondSegmentAngleConstraint)
	o.integer(173, int64(s.PathType))
	o.integer(91, int64(encodeObjCmColor(s.LineColor)))
	w.refIfKnown(340, lineTypeObj(s.LeaderLineType))
	o.integer(92, int64(s.LeaderLineWeight))
	o.boolean(290, s.EnableLanding)
	o.dbl(42, s.LandingGap)
	o.boolean(291, s.EnableDogleg)
	o.dbl(43, s.LandingDistance)
	o.str(3, s.Description)
	w.refIfKnown(341, blockObj(s.Arrowhead))
	o.dbl(44, s.ArrowheadSize)
	o.str(300, s.DefaultTextContents)
	w.refIfKnown(342, textStyleObj(s.TextStyle))
	o.integer(174, int64(s.TextLeftAttachment))
	o.integer(178, int64(s.TextRightAttachment))
	o.integer(175, int64(s.TextAngle))
	o.integer(176, int64(s.TextAlignment))
	o.integer(93, int64(encodeObjCmColor(s.TextColor)))
	o.dbl(45, s.TextHeight)
	o.boolean(292, s.TextFrame)
	o.boolean(297, s.TextAlignAlwaysLeft)
	o.dbl(46, s.AlignSpace)
	w.refIfKnown(343, blockObj(s.BlockContent))
	o.integer(94, int64(encodeObjCmColor(s.BlockContentColor)))
	o.dbl(47, s.BlockContentScale.X)
	o.dbl(49, s.BlockContentScale.Y)
	o.dbl(140, s.BlockContentScale.Z)
	o.boolean(293, s.EnableBlockContentScale)
	o.angle(141, s.BlockContentRotation)
	o.boolean(294, s.EnableBlockContentRotation)
	o.integer(177, int64(s.BlockContentConnection))
	o.dbl(142, s.ScaleFactor)
	o.boolean(295, s.OverwritePropertyValue)
	o.boolean(296, s.IsAnnotative)
	o.dbl(143, s.BreakGapSize)
	o.integer(271, int64(s.TextAttachmentDirection))
	o.integer(272, int64(s.TextBottomAttachment))
	o.integer(273, int64(s.TextTopAttachment))
	o.boolean(298, s.UnknownFlag298)
}

func lineTypeObj(l *cadoc.LineType) cadoc.Object {
	if l == nil {
		return nil
	}
	return l
}

func blockObj(b *cadoc.BlockRecord) cadoc.Object {
	if b == nil {
		return nil
	}
	return b
}

func textStyleObj(t *cadoc.TextStyle) cadoc.Object {
	if t == nil {
		return nil
	}
	return t
}

func (w *writer) writeGeoData(g *cadoc.GeoData) {
	o := w.o
	o.str(100, cadoc.SubclassGeoData)
	old := w.version <= cadoc.AC1021
	switch {
	case old:
		o.integer(90, 1)
	case w.version == cadoc.AC1024:
		o.integer(90, 2)
	default:
		o.integer(90, 3)
	}
	if g.HostBlock != nil && w.known(g.HostBlock) {
		o.handle(330, g.HostBlock.Handle)
	}
	o.integer(70, int64(g.CoordinatesType))
	north := math.Atan2(g.NorthDirection.Y, g.NorthDirection.X)
	if old {
		o.dbl(40, g.ReferencePoint.Y)
		o.dbl(41, g.ReferencePoint.X)
		o.dbl(42, g.ReferencePoint.Z)
		o.integer(91, int64(g.HorizontalUnits))
		o.vec3(10, g.DesignPoint)
		o.vec3(11, cadoc.Vec3{})
		o.vec3(210, g.UpDirection)
		o.angle(52, math.Pi/2-north)
		o.dbl(43, 1)
		o.dbl(44, 1)
		o.dbl(45, 1)
		o.str(301, g.CoordinateSystemDefinition)
		o.str(302, g.GeoRssTag)
		o.dbl(46, g.HorizontalUnitScale)
		o.str(303, "")
		o.str(304, "")
		o.str(305, g.ObservationFromTag)
		o.str(306, g.ObservationToTag)
		o.str(307, g.ObservationCoverageTag)
		o.integer(93, int64(len(g.Points)))
		for _, p := range g.Points {
			o.vec2(12, p.Source)
			o.vec2(13, p.Destination)
		}
		w.writeGeoFaces(g)
		o.str(3, "CIVIL3D_DATA_BEGIN")
		o.boolean(292, false)
		o.vec2(14, g.ReferencePoint.XY())
		o.vec2(15, g.ReferencePoint.XY())
		o.integer(93, 0)
		o.integer(94, 0)
		o.boolean(293, false)
		o.vec2(16, cadoc.Vec2{})
		o.vec2(17, cadoc.Vec2{})
		o.angle(54, math.Pi/2-north)
		o.dbl(140, math.Pi/2-north)
		o.integer(95, int64(g.ScaleEstimationMethod))
		o.dbl(141, g.UserSpecifiedScaleFactor)
		o.boolean(294, g.EnableSeaLevelCorrection)
		o.dbl(142, g.SeaLevelElevation)
		o.dbl(143, g.CoordinateProjectionRadius)
		o.str(4, "CIVIL3D_DATA_END")
		return
	}
	o.vec3(10, g.DesignPoint)
	o.vec3(11, g.ReferencePoint)
	o.dbl(40, g.HorizontalUnitScale)
	o.integer(91, int64(g.HorizontalUnits))
	o.dbl(41, g.VerticalUnitScale)
	o.integer(92, int64(g.VerticalUnits))
	o.vec3(210, g.UpDirection)
	o.vec2(12, g.NorthDirection)
	o.integer(95, int64(g.ScaleEstimationMethod))
	o.dbl(141, g.UserSpecifiedScaleFactor)
	o.boolean(294, g.EnableSeaLevelCorrection)
	o.dbl(142, g.SeaLevelElevation)
	o.dbl(143, g.CoordinateProjectionRadius)
	w.longText(301, 303, g.CoordinateSystemDefinition)
	o.str(302, g.GeoRssTag)
	o.str(305, g.ObservationFromTag)
	o.str(306, g.ObservationToTag)
	o.str(307, g.ObservationCoverageTag)
	o.integer(93, int64(len(g.Points)))
	for _, p := range g.Points {
		o.vec2(13, p.Source)
		o.vec2(14, p.Destination)
	}
	w.writeGeoFaces(g)
}

func (w *writer) writeGeoFaces(g *cadoc.GeoData) {
	o := w.o
	o.integer(96, int64(len(g.Faces)))
	for _, f := range g.Faces {
		o.integer(97, int64(f.Index1))
		o.integer(98, int64(f.Index2))
		o.integer(99, int64(f.Index3))
	}
}

func (w *writer) writeField(f *cadoc.Field) {
	o := w.o
	o.str(100, cadoc.SubclassField)
	o.str(1, f.EvaluatorId)
	w.longText(2, 3, f.FieldCode)
	o.integer(90, int64(len(f.Children)))
	for _, c := range f.Children {
		o.handle(360, c.Handle)
		c.Owner = f
		w.enqueue(c)
	}
	var objs []cadoc.Object
	for _, c := range f.CadObjects {
		if w.known(c) {
			objs = append(objs, c)
		}
	}
	o.integer(97, int64(len(objs)))
	for _, c := range objs {
		o.handle(331, c.Base().Handle)
	}
	o.integer(91, int64(f.EvaluationOptionFlags))
	o.integer(92, int64(f.FilingOptionFlags))
	o.integer(94, int64(f.FieldStateFlags))
	o.integer(95, int64(f.EvaluationStatusFlags))
	o.integer(96, int64(f.EvaluationErrorCode))
	o.str(300, f.EvaluationErrorMessage)
	o.integer(93, int64(len(f.Values)))
	for _, v := range f.Values {
		o.str(6, v.Key)
		w.writeCadValue(&v.Value)
	}
	o.str(7, "ACFD_FIELD_VALUE")
	w.writeCadValue(&f.Value)
	w.longText(301, 9, f.FormatString)
	o.integer(98, int64(len([]rune(f.FormatString))))
}

func (w *writer) writeCadValue(v *cadoc.CadValue) {
	o := w.o
	o.integer(93, int64(v.Flags))
	o.integer(90, int64(v.ValueType))
	switch v.ValueType {
	case cadoc.CadValueTypeDouble:
		f, _ := v.Value.(float64)
		o.dbl(140, f)
	case cadoc.CadValueTypeLong:
		i, _ := v.Value.(int32)
		o.integer(91, int64(i))
	case cadoc.CadValueTypeString, cadoc.CadValueTypeGeneral:
		s, _ := v.Value.(string)
		w.longText(1, 2, s)
	case cadoc.CadValueTypePoint2D, cadoc.CadValueTypePoint3D:
		switch p := v.Value.(type) {
		case cadoc.Vec3:
			o.vec3(11, p)
		case cadoc.Vec2:
			o.vec3(11, p.Vec3(0))
		default:
			o.vec3(11, cadoc.Vec3{})
		}
	case cadoc.CadValueTypeHandle:
		if obj, ok := v.Value.(cadoc.Object); ok && w.known(obj) {
			o.handle(330, obj.Base().Handle)
		}
	default:
		o.integer(91, 0)
	}
	o.str(304, "ACVALUE_END")
}
