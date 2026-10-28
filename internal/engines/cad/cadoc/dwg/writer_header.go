package dwg

import (
	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// writeHeaderSection is DwgHeaderWriter: the header variables.
func (w *writer) writeHeaderSection() []byte {
	doc := w.doc
	h := doc.Header
	v := w.version
	r13_14 := v <= cadoc.AC1014
	r13_15 := v <= cadoc.AC1015
	r2000 := v >= cadoc.AC1015
	r2004pre := v < cadoc.AC1018
	r2004 := v >= cadoc.AC1018
	r2007 := v >= cadoc.AC1021
	r2010 := v >= cadoc.AC1024
	r2013 := v >= cadoc.AC1027
	r2018 := v >= cadoc.AC1032

	var sw wWriter
	if r2007 {
		m := newWMerged(v, w.enc)
		m.savePositionForSize()
		sw = m
	} else {
		sw = newWBits(v, &wMemStream{}, w.enc)
	}
	ref := func(o cadoc.Object) uint64 { return w.handleOf(o) }
	hard := func(o cadoc.Object) { sw.handleReference(wRefHardPointer, ref(o)) }
	b2i := func(b bool) int16 {
		if b {
			return 1
		}
		return 0
	}

	if r2013 {
		// REQUIREDVERSIONS, read only.
		sw.writeBitLongLong(0)
	}
	sw.writeBitDouble(412148564080.0)
	sw.writeBitDouble(1.0)
	sw.writeBitDouble(1.0)
	sw.writeBitDouble(1.0)
	sw.writeVariableText("m")
	sw.writeVariableText("")
	sw.writeVariableText("")
	sw.writeVariableText("")
	sw.writeBitLong(24)
	sw.writeBitLong(0)
	if r13_14 {
		sw.writeBitShort(0)
	}
	if r2004pre {
		// Current viewport entity header.
		sw.handleReference(wRefHardPointer, 0)
	}
	sw.writeBit(h.AssociatedDimensions)
	sw.writeBit(h.UpdateDimensionsWhileDragging)
	if r13_14 {
		sw.writeBit(false) // DIMSAV
	}
	sw.writeBit(h.PolylineLineTypeGeneration)
	sw.writeBit(h.OrthoMode)
	sw.writeBit(h.RegenerationMode)
	sw.writeBit(h.FillMode)
	sw.writeBit(h.QuickTextMode)
	sw.writeBit(h.PaperSpaceLineTypeScaling == cadoc.SpaceLineTypeScalingNormal)
	sw.writeBit(h.LimitCheckingOn)
	if r13_14 {
		sw.writeBit(h.BlipMode)
	}
	if r2004 {
		sw.writeBit(false)
	}
	sw.writeBit(h.UserTimer)
	sw.writeBit(h.SketchPolylines)
	sw.writeBit(h.AngularDirection != cadoc.AngularDirectionCounterClockWise)
	sw.writeBit(h.ShowSplineControlPoints)
	if r13_14 {
		sw.writeBit(false) // ATTREQ
		sw.writeBit(false) // ATTDIA
	}
	sw.writeBit(h.MirrorText)
	sw.writeBit(h.WorldView)
	if r13_14 {
		sw.writeBit(false) // WIREFRAME
	}
	sw.writeBit(h.ShowModelSpace)
	sw.writeBit(h.PaperSpaceLimitsChecking)
	sw.writeBit(h.RetainXRefDependentVisibilitySettings)
	if r13_14 {
		sw.writeBit(false) // DELOBJ
	}
	sw.writeBit(h.DisplaySilhouetteCurves)
	sw.writeBit(h.CreateEllipseAsPolyline)
	sw.writeBitShort(b2i(h.ProxyGraphics))
	if r13_14 {
		sw.writeBitShort(0) // DRAGMODE
	}
	sw.writeBitShort(h.SpatialIndexMaxTreeDepth)
	sw.writeBitShort(int16(h.LinearUnitFormat))
	sw.writeBitShort(h.LinearUnitPrecision)
	sw.writeBitShort(int16(h.AngularUnit))
	sw.writeBitShort(h.AngularUnitPrecision)
	if r13_14 {
		sw.writeBitShort(int16(h.ObjectSnapMode))
	}
	sw.writeBitShort(int16(h.AttributeVisibility))
	if r13_14 {
		sw.writeBitShort(0) // COORDS
	}
	sw.writeBitShort(h.PointDisplayMode)
	if r13_14 {
		sw.writeBitShort(0) // PICKSTYLE
	}
	if r2004 {
		sw.writeBitLong(0)
		sw.writeBitLong(0)
		sw.writeBitLong(0)
	}
	for _, s := range []int16{h.UserShort1, h.UserShort2, h.UserShort3, h.UserShort4, h.UserShort5,
		h.NumberOfSplineSegments, h.SurfaceDensityU, h.SurfaceDensityV, h.SurfaceType,
		h.SurfaceMeshTabulationCount1, h.SurfaceMeshTabulationCount2, int16(h.SplineType), int16(h.ShadeEdge),
		h.ShadeDiffuseToAmbientPercentage, h.UnitMode, h.MaxViewportCount, h.SurfaceIsolineCount,
		int16(h.CurrentMultiLineJustification), h.TextQuality} {
		sw.writeBitShort(s)
	}
	for _, d := range []float64{h.LineTypeScale, h.TextHeightDefault, h.TraceWidthDefault, h.SketchIncrement,
		h.FilletRadius, h.ThicknessDefault, h.AngleBase, h.PointDisplaySize, h.PolylineWidthDefault,
		h.UserDouble1, h.UserDouble2, h.UserDouble3, h.UserDouble4, h.UserDouble5, h.ChamferDistance1,
		h.ChamferDistance2, h.ChamferLength, h.ChamferAngle, h.FacetResolution, h.CurrentMultilineScale,
		h.CurrentEntityLinetypeScale} {
		sw.writeBitDouble(d)
	}
	sw.writeVariableText(h.MenuFileName)
	sw.writeDateTime(h.CreateDateTime)
	sw.writeDateTime(h.UpdateDateTime)
	if r2004 {
		sw.writeBitLong(0)
		sw.writeBitLong(0)
		sw.writeBitLong(0)
	}
	sw.writeTimeSpan(h.TotalEditingTime)
	sw.writeTimeSpan(h.UserElapsedTimeSpan)
	sw.writeCmColor(h.CurrentEntityColor, false)
	// HANDSEED is part of the main data stream, not of the handle stream.
	sw.main().handleReference(wRefUndefined, h.HandleSeed)
	hard(doc.CurrentLayer())
	hard(doc.CurrentTextStyle())
	hard(w.currentLineType())
	if r2007 {
		hard(nil) // CMATERIAL
	}
	hard(doc.CurrentDimensionStyle())
	hard(w.currentMLineStyle())
	if r2000 {
		sw.writeBitDouble(h.ViewportDefaultViewScaleFactor)
	}
	sw.write3BitDouble(h.PaperSpaceInsertionBase)
	sw.write3BitDouble(h.PaperSpaceExtMin)
	sw.write3BitDouble(h.PaperSpaceExtMax)
	sw.write2RawDouble(h.PaperSpaceLimitsMin)
	sw.write2RawDouble(h.PaperSpaceLimitsMax)
	sw.writeBitDouble(h.PaperSpaceElevation)
	sw.write3BitDouble(h.PaperSpaceUcsOrigin)
	sw.write3BitDouble(h.PaperSpaceUcsXAxis)
	sw.write3BitDouble(h.PaperSpaceUcsYAxis)
	hard(w.ucsByName(h.PaperSpaceName))
	if r2000 {
		hard(nil) // PUCSORTHOREF
		sw.writeBitShort(0)
		hard(nil) // PUCSBASE
		sw.write3BitDouble(h.PaperSpaceOrthographicTopDOrigin)
		sw.write3BitDouble(h.PaperSpaceOrthographicBottomDOrigin)
		sw.write3BitDouble(h.PaperSpaceOrthographicLeftDOrigin)
		sw.write3BitDouble(h.PaperSpaceOrthographicRightDOrigin)
		sw.write3BitDouble(h.PaperSpaceOrthographicFrontDOrigin)
		sw.write3BitDouble(h.PaperSpaceOrthographicBackDOrigin)
	}
	sw.write3BitDouble(h.ModelSpaceInsertionBase)
	sw.write3BitDouble(h.ModelSpaceExtMin)
	sw.write3BitDouble(h.ModelSpaceExtMax)
	sw.write2RawDouble(h.ModelSpaceLimitsMin)
	sw.write2RawDouble(h.ModelSpaceLimitsMax)
	sw.writeBitDouble(h.Elevation)
	sw.write3BitDouble(h.ModelSpaceOrigin)
	sw.write3BitDouble(h.ModelSpaceXAxis)
	sw.write3BitDouble(h.ModelSpaceYAxis)
	hard(w.ucsByName(h.UcsName))
	if r2000 {
		hard(nil) // UCSORTHOREF
		sw.writeBitShort(0)
		hard(nil) // UCSBASE
		sw.write3BitDouble(h.ModelSpaceOrthographicTopDOrigin)
		sw.write3BitDouble(h.ModelSpaceOrthographicBottomDOrigin)
		sw.write3BitDouble(h.ModelSpaceOrthographicLeftDOrigin)
		sw.write3BitDouble(h.ModelSpaceOrthographicRightDOrigin)
		sw.write3BitDouble(h.ModelSpaceOrthographicFrontDOrigin)
		sw.write3BitDouble(h.ModelSpaceOrthographicBackDOrigin)
		sw.writeVariableText(h.DimensionPostFix)
		sw.writeVariableText(h.DimensionAlternateDimensioningSuffix)
	}
	dimTextStyle := w.textStyleByName(h.DimensionTextStyleName)
	if r13_14 {
		sw.writeBit(h.DimensionGenerateTolerances)
		sw.writeBit(h.DimensionLimitsGeneration)
		sw.writeBit(h.DimensionTextInsideHorizontal)
		sw.writeBit(h.DimensionTextOutsideHorizontal)
		sw.writeBit(h.DimensionSuppressFirstExtensionLine)
		sw.writeBit(h.DimensionSuppressSecondExtensionLine)
		sw.writeBit(h.DimensionAlternateUnitDimensioning)
		sw.writeBit(h.DimensionTextOutsideExtensions)
		sw.writeBit(h.DimensionSeparateArrowBlocks)
		sw.writeBit(h.DimensionTextInsideExtensions)
		sw.writeBit(h.DimensionSuppressOutsideExtensions)
		sw.writeByte(byte(h.DimensionAlternateUnitDecimalPlaces))
		sw.writeByte(byte(h.DimensionZeroHandling))
		sw.writeBit(h.DimensionSuppressFirstDimensionLine)
		sw.writeBit(h.DimensionSuppressSecondDimensionLine)
		sw.writeByte(byte(h.DimensionToleranceAlignment))
		sw.writeByte(byte(h.DimensionTextHorizontalAlignment))
		sw.writeByte(byte(h.DimensionFit))
		sw.writeBit(h.DimensionCursorUpdate)
		sw.writeByte(byte(h.DimensionToleranceZeroHandling))
		sw.writeByte(byte(h.DimensionAlternateUnitZeroHandling))
		sw.writeByte(byte(h.DimensionAlternateUnitToleranceZeroHandling))
		sw.writeByte(byte(h.DimensionTextVerticalAlignment))
		sw.writeBitShort(h.DimensionUnit)
		// DIMAUNIT (ACadSharp writes DIMADEC here).
		sw.writeBitShort(int16(h.DimensionAngularUnit))
		sw.writeBitShort(h.DimensionDecimalPlaces)
		sw.writeBitShort(h.DimensionToleranceDecimalPlaces)
		sw.writeBitShort(int16(h.DimensionAlternateUnitFormat))
		sw.writeBitShort(h.DimensionAlternateUnitToleranceDecimalPlaces)
		hard(dimTextStyle)
	}
	for _, d := range []float64{h.DimensionScaleFactor, h.DimensionArrowSize, h.DimensionExtensionLineOffset,
		h.DimensionLineIncrement, h.DimensionExtensionLineExtension, h.DimensionRounding,
		h.DimensionLineExtension, h.DimensionPlusTolerance, h.DimensionMinusTolerance} {
		sw.writeBitDouble(d)
	}
	if r2007 {
		sw.writeBitDouble(h.DimensionFixedExtensionLineLength)
		sw.writeBitDouble(h.DimensionJoggedRadiusDimensionTransverseSegmentAngle)
		sw.writeBitShort(int16(h.DimensionTextBackgroundFillMode))
		sw.writeCmColor(h.DimensionTextBackgroundColor, false)
	}
	if r2000 {
		sw.writeBit(h.DimensionGenerateTolerances)
		sw.writeBit(h.DimensionLimitsGeneration)
		sw.writeBit(h.DimensionTextInsideHorizontal)
		sw.writeBit(h.DimensionTextOutsideHorizontal)
		sw.writeBit(h.DimensionSuppressFirstExtensionLine)
		sw.writeBit(h.DimensionSuppressSecondExtensionLine)
		sw.writeBitShort(int16(h.DimensionTextVerticalAlignment))
		sw.writeBitShort(int16(h.DimensionZeroHandling))
		sw.writeBitShort(int16(h.DimensionAngularZeroHandling))
	}
	if r2007 {
		sw.writeBitShort(int16(h.DimensionArcLengthSymbolPosition))
	}
	for _, d := range []float64{h.DimensionTextHeight, h.DimensionCenterMarkSize, h.DimensionTickSize,
		h.DimensionAlternateUnitScaleFactor, h.DimensionLinearScaleFactor, h.DimensionTextVerticalPosition,
		h.DimensionToleranceScaleFactor, h.DimensionLineGap} {
		sw.writeBitDouble(d)
	}
	if r13_14 {
		sw.writeVariableText(h.DimensionPostFix)
		sw.writeVariableText(h.DimensionAlternateDimensioningSuffix)
		sw.writeVariableText(h.DimensionBlockName)
		sw.writeVariableText(h.DimensionBlockNameFirst)
		sw.writeVariableText(h.DimensionBlockNameSecond)
	}
	if r2000 {
		sw.writeBitDouble(h.DimensionAlternateUnitRounding)
		sw.writeBit(h.DimensionAlternateUnitDimensioning)
		sw.writeBitShort(h.DimensionAlternateUnitDecimalPlaces)
		sw.writeBit(h.DimensionTextOutsideExtensions)
		sw.writeBit(h.DimensionSeparateArrowBlocks)
		sw.writeBit(h.DimensionTextInsideExtensions)
		sw.writeBit(h.DimensionSuppressOutsideExtensions)
	}
	sw.writeCmColor(h.DimensionLineColor, false)
	sw.writeCmColor(h.DimensionExtensionLineColor, false)
	sw.writeCmColor(h.DimensionTextColor, false)
	if r2000 {
		sw.writeBitShort(h.DimensionAngularDimensionDecimalPlaces)
		sw.writeBitShort(h.DimensionDecimalPlaces)
		sw.writeBitShort(h.DimensionToleranceDecimalPlaces)
		sw.writeBitShort(int16(h.DimensionAlternateUnitFormat))
		sw.writeBitShort(h.DimensionAlternateUnitToleranceDecimalPlaces)
		sw.writeBitShort(int16(h.DimensionAngularUnit))
		sw.writeBitShort(int16(h.DimensionFractionFormat))
		sw.writeBitShort(int16(h.DimensionLinearUnitFormat))
		sw.writeBitShort(int16(h.DimensionDecimalSeparator))
		sw.writeBitShort(int16(h.DimensionTextMovement))
		sw.writeBitShort(int16(h.DimensionTextHorizontalAlignment))
		// DIMSD1/DIMSD2 (ACadSharp writes DIMSE1/DIMSE2 here).
		sw.writeBit(h.DimensionSuppressFirstDimensionLine)
		sw.writeBit(h.DimensionSuppressSecondDimensionLine)
		sw.writeBitShort(int16(h.DimensionToleranceAlignment))
		sw.writeBitShort(int16(h.DimensionToleranceZeroHandling))
		sw.writeBitShort(int16(h.DimensionAlternateUnitZeroHandling))
		sw.writeBitShort(int16(h.DimensionAlternateUnitToleranceZeroHandling))
		sw.writeBit(h.DimensionCursorUpdate)
		sw.writeBitShort(int16(h.DimensionDimensionTextArrowFit))
	}
	if r2007 {
		sw.writeBit(h.DimensionIsExtensionLineLengthFixed)
	}
	if r2010 {
		sw.writeBit(h.DimensionTextDirection == cadoc.TextDirectionRightToLeft)
		sw.writeBitDouble(h.DimensionAltMzf)
		sw.writeVariableText(h.DimensionAltMzs)
		sw.writeBitDouble(h.DimensionMzf)
		sw.writeVariableText(h.DimensionMzs)
	}
	// ACadSharp writes null for the arrow blocks and dimension line types;
	// the header keeps their names, so they are resolved here.
	if r2000 {
		hard(dimTextStyle)
		hard(w.arrowBlockByName(h.ArrowBlockName))
		hard(w.arrowBlockByName(h.DimensionBlockName))
		hard(w.arrowBlockByName(h.DimensionBlockNameFirst))
		hard(w.arrowBlockByName(h.DimensionBlockNameSecond))
	}
	if r2007 {
		hard(w.lineTypeByName(h.DimensionLineType))
		hard(w.lineTypeByName(h.DimensionTex1))
		hard(w.lineTypeByName(h.DimensionTex2))
	}
	if r2000 {
		sw.writeBitShort(int16(h.DimensionLineWeight))
		sw.writeBitShort(int16(h.ExtensionLineWeight))
	}
	for _, t := range []cadoc.Object{doc.BlockRecords, doc.Layers, doc.TextStyles, doc.LineTypes, doc.Views,
		doc.UCSs, doc.VPorts, doc.AppIds, doc.DimensionStyles} {
		sw.handleReference(wRefHardOwnership, ref(t))
	}
	if r13_15 {
		sw.handleReference(wRefHardOwnership, w.vEntityControlHandle)
	}
	hard(w.namedDictionary(cadoc.AcadGroupDictionary))
	hard(w.namedDictionary(cadoc.AcadMLineStyleDictionary))
	sw.handleReference(wRefHardOwnership, ref(doc.RootDictionary))
	if r2000 {
		sw.writeBitShort(h.StackedTextAlignment)
		sw.writeBitShort(h.StackedTextSizePercentage)
		sw.writeVariableText(h.HyperLinkBase)
		sw.writeVariableText(h.StyleSheetName)
		hard(w.namedDictionary(cadoc.AcadLayoutDictionary))
		hard(nil) // PLOTSETTINGS
		hard(nil) // PLOTSTYLES
	}
	if r2004 {
		hard(w.namedDictionary(cadoc.AcadMaterialDictionary))
		hard(w.namedDictionary(cadoc.AcadColorDictionary))
	}
	if r2007 {
		hard(nil) // VISUALSTYLE
		if r2013 {
			hard(nil)
		}
	}
	if r2000 {
		// CELWEIGHT as a line weight index (ACadSharp masks the raw value,
		// which turns ByLayer into Default).
		flags := int32(cadoc.LineWeightIndex(h.CurrentEntityLineWeight))&0x1F | int32(h.EndCaps)<<5 | int32(h.JoinStyle)<<7
		if !h.DisplayLineWeight {
			flags |= 0x200
		}
		if !h.XEdit {
			flags |= 0x400
		}
		if h.ExtendedNames {
			flags |= 0x800
		}
		if h.PlotStyleMode == 1 {
			flags |= 0x2000
		}
		if h.LoadOLEObject {
			flags |= 0x4000
		}
		sw.writeBitLong(flags)
		sw.writeBitShort(int16(h.InsUnits))
		sw.writeBitShort(int16(h.CurrentEntityPlotStyle))
		if h.CurrentEntityPlotStyle == cadoc.EntityPlotStyleTypeByObjectId {
			hard(nil) // CPSNID
		}
		sw.writeVariableText(h.FingerPrintGuid)
		sw.writeVariableText(h.VersionGuid)
	}
	if r2004 {
		sw.writeByte(byte(h.EntitySortingFlags))
		sw.writeByte(byte(h.IndexCreationFlags))
		sw.writeByte(h.HideText)
		sw.writeByte(byte(h.ExternalReferenceClippingBoundaryType))
		sw.writeByte(byte(h.DimensionAssociativity))
		sw.writeByte(h.HaloGapPercentage)
		sw.writeBitShort(h.ObscuredColor.Index())
		sw.writeBitShort(h.InterfereColor.Index())
		sw.writeByte(h.ObscuredType)
		sw.writeByte(h.IntersectionDisplay)
		sw.writeVariableText(h.ProjectName)
	}
	hard(doc.PaperSpace())
	hard(doc.ModelSpace())
	hard(w.lineTypeByName(cadoc.ByLayerName))
	hard(w.lineTypeByName(cadoc.ByBlockName))
	hard(w.lineTypeByName(cadoc.ContinuousName))
	if r2007 {
		sw.writeBit(h.CameraDisplayObjects)
		sw.writeBitLong(0)
		sw.writeBitLong(0)
		sw.writeBitDouble(0)
		sw.writeBitDouble(h.StepsPerSecond)
		sw.writeBitDouble(h.StepSize)
		sw.writeBitDouble(h.Dw3DPrecision)
		sw.writeBitDouble(h.LensLength)
		sw.writeBitDouble(h.CameraHeight)
		sw.writeByte(h.SolidsRetainHistory)
		sw.writeByte(h.ShowSolidsHistory)
		sw.writeBitDouble(h.SweptSolidWidth)
		sw.writeBitDouble(h.SweptSolidHeight)
		sw.writeBitDouble(h.DraftAngleFirstCrossSection)
		sw.writeBitDouble(h.DraftAngleSecondCrossSection)
		sw.writeBitDouble(h.DraftMagnitudeFirstCrossSection)
		sw.writeBitDouble(h.DraftMagnitudeSecondCrossSection)
		sw.writeBitShort(h.SolidLoftedShape)
		sw.writeByte(h.LoftedObjectNormals)
		sw.writeBitDouble(h.Latitude)
		sw.writeBitDouble(h.Longitude)
		sw.writeBitDouble(h.NorthDirection)
		sw.writeBitLong(h.TimeZone)
		sw.writeByte(h.DisplayLightGlyphs)
		sw.writeByte('0') // TILEMODELIGHTSYNCH
		sw.writeByte(h.DwgUnderlayFramesVisibility)
		sw.writeByte(h.DgnUnderlayFramesVisibility)
		sw.writeBit(false)
		sw.writeCmColor(h.InterfereColor, false)
		hard(nil) // INTERFEREOBJVS
		hard(nil) // INTERFEREVPVS
		hard(nil) // DRAGVS
		sw.writeByte(byte(h.ShadowMode))
		sw.writeBitDouble(h.ShadowPlaneLocation)
	}
	if v >= cadoc.AC1014 {
		sw.writeBitShort(-1)
		sw.writeBitShort(-1)
		sw.writeBitShort(-1)
		sw.writeBitShort(-1)
		if r2004 {
			sw.writeBitLong(0)
			sw.writeBitLong(0)
			sw.writeBit(false)
		}
	}
	sw.writeSpearShift()

	extra := r2010 && h.MaintenanceVersion > 3 || r2018
	return writerSectionWithCRC(writerHeaderStartSentinel, writerHeaderEndSentinel, sw.main().ms.bytes(), extra).bytes()
}
