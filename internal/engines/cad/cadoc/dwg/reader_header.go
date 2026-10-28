package dwg

import (
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// headerHandles is DwgHeaderHandlesCollection: the object handles of the
// header section, in the order they were read (that order seeds the object
// reading queue).
type headerHandles struct {
	names  []string
	values map[string]uint64
}

func newHeaderHandles() *headerHandles { return &headerHandles{values: map[string]uint64{}} }

func (h *headerHandles) set(name string, v uint64) {
	if _, ok := h.values[name]; !ok {
		h.names = append(h.names, name)
	}
	h.values[name] = v
}

func (h *headerHandles) get(name string) (uint64, bool) {
	v, ok := h.values[name]
	return v, ok
}

// handles returns the values in first-set order.
func (h *headerHandles) handles() []uint64 {
	out := make([]uint64, 0, len(h.names))
	for _, n := range h.names {
		out = append(out, h.values[n])
	}
	return out
}

// readHeader reads AcDb:Header into a new header (DwgReader.ReadHeader).
func (r *reader) readHeader() *cadoc.Header {
	h := cadoc.NewHeader()
	if name := DwgCodePageAt(r.fh.codePage).Name; name != "" {
		h.CodePage = strings.ToUpper(name)
	}
	s := r.sectionStream(SectionHeader)
	if s == nil {
		panic(errorf("header section not found"))
	}
	r.b.headerHandles = r.readHeaderSection(h, s)
	return h
}

// readHeaderSection is DwgHeaderReader.Read.
func (r *reader) readHeaderSection(h *cadoc.Header, mainReader *BitReader) *headerHandles {
	v := r.fh.version
	f := newVersionFeatures(v)
	h.Version = v
	ptr := newHeaderHandles()

	if mainReader.ReadSentinel() != SentinelHeaderStart {
		r.note(NotifyWarning, "Invalid section sentinel found in %s", SectionHeader)
	}
	size := mainReader.ReadRawLong()
	if f.R2010Plus && r.fh.maintenance > 3 || f.R2018Plus {
		mainReader.ReadRawLong()
	}
	initialPos := mainReader.PositionInBits()

	rd := newMerged(mainReader, mainReader, mainReader)
	if f.R2007Plus {
		sizeInBits := mainReader.ReadRawLong()
		lastPositionInBits := initialPos + sizeInBits - 1
		text := mainReader.clone()
		text.SetPositionByFlag(lastPositionInBits)
		ref := mainReader.clone()
		ref.SetPositionInBits(lastPositionInBits + 1)
		rd = newMerged(mainReader, text, ref)
	}

	if f.R2013Plus {
		h.RequiredVersions = rd.ReadBitLongLong()
	}
	rd.ReadBitDouble()
	rd.ReadBitDouble()
	rd.ReadBitDouble()
	rd.ReadBitDouble()
	rd.ReadVariableText()
	rd.ReadVariableText()
	rd.ReadVariableText()
	rd.ReadVariableText()
	rd.ReadBitLong()
	rd.ReadBitLong()
	if f.R13_14Only {
		rd.ReadBitShort()
	}
	if f.R2004Pre {
		// Current viewport entity header.
		rd.HandleReference(0)
	}
	h.AssociatedDimensions = rd.ReadBit()
	h.UpdateDimensionsWhileDragging = rd.ReadBit()
	if f.R13_14Only {
		rd.ReadBit() // DIMSAV
	}
	h.PolylineLineTypeGeneration = rd.ReadBit()
	h.OrthoMode = rd.ReadBit()
	h.RegenerationMode = rd.ReadBit()
	h.FillMode = rd.ReadBit()
	h.QuickTextMode = rd.ReadBit()
	if rd.ReadBit() {
		h.PaperSpaceLineTypeScaling = cadoc.SpaceLineTypeScalingNormal
	} else {
		h.PaperSpaceLineTypeScaling = cadoc.SpaceLineTypeScalingViewport
	}
	h.LimitCheckingOn = rd.ReadBit()
	if f.R13_14Only {
		h.BlipMode = rd.ReadBit()
	}
	if f.R2004Plus {
		rd.ReadBit()
	}
	h.UserTimer = rd.ReadBit()
	h.SketchPolylines = rd.ReadBit()
	h.AngularDirection = cadoc.AngularDirection(rd.ReadBitAsShort())
	h.ShowSplineControlPoints = rd.ReadBit()
	if f.R13_14Only {
		rd.ReadBit() // ATTREQ
		rd.ReadBit() // ATTDIA
	}
	h.MirrorText = rd.ReadBit()
	h.WorldView = rd.ReadBit()
	if f.R13_14Only {
		rd.ReadBit() // WIREFRAME
	}
	h.ShowModelSpace = rd.ReadBit()
	h.PaperSpaceLimitsChecking = rd.ReadBit()
	h.RetainXRefDependentVisibilitySettings = rd.ReadBit()
	if f.R13_14Only {
		rd.ReadBit() // DELOBJ
	}
	h.DisplaySilhouetteCurves = rd.ReadBit()
	h.CreateEllipseAsPolyline = rd.ReadBit()
	h.ProxyGraphics = rd.ReadBitShortAsBool()
	if f.R13_14Only {
		rd.ReadBitShort() // DRAGMODE
	}
	h.SpatialIndexMaxTreeDepth = rd.ReadBitShort()
	h.LinearUnitFormat = cadoc.LinearUnitFormat(rd.ReadBitShort())
	if p := rd.ReadBitShort(); p >= 0 && p <= 8 {
		h.LinearUnitPrecision = p
	}
	h.AngularUnit = cadoc.AngularUnitFormat(rd.ReadBitShort())
	if p := rd.ReadBitShort(); p >= 0 && p <= 8 {
		h.AngularUnitPrecision = p
	}
	if f.R13_14Only {
		h.ObjectSnapMode = cadoc.ObjectSnapMode(rd.ReadBitShort())
	}
	h.AttributeVisibility = cadoc.AttributeVisibilityMode(rd.ReadBitShort())
	if f.R13_14Only {
		rd.ReadBitShort() // COORDS
	}
	h.PointDisplayMode = rd.ReadBitShort()
	if f.R13_14Only {
		rd.ReadBitShort() // PICKSTYLE
	}
	if f.R2004Plus {
		rd.ReadBitLong()
		rd.ReadBitLong()
		rd.ReadBitLong()
	}
	h.UserShort1 = rd.ReadBitShort()
	h.UserShort2 = rd.ReadBitShort()
	h.UserShort3 = rd.ReadBitShort()
	h.UserShort4 = rd.ReadBitShort()
	h.UserShort5 = rd.ReadBitShort()
	h.NumberOfSplineSegments = rd.ReadBitShort()
	h.SurfaceDensityU = rd.ReadBitShort()
	h.SurfaceDensityV = rd.ReadBitShort()
	h.SurfaceType = rd.ReadBitShort()
	h.SurfaceMeshTabulationCount1 = rd.ReadBitShort()
	h.SurfaceMeshTabulationCount2 = rd.ReadBitShort()
	h.SplineType = cadoc.SplineType(rd.ReadBitShort())
	h.ShadeEdge = cadoc.ShadeEdgeType(rd.ReadBitShort())
	h.ShadeDiffuseToAmbientPercentage = rd.ReadBitShort()
	h.UnitMode = rd.ReadBitShort()
	h.MaxViewportCount = rd.ReadBitShort()
	if n := rd.ReadBitShort(); n >= 0 && n <= 2047 {
		h.SurfaceIsolineCount = n
	}
	h.CurrentMultiLineJustification = cadoc.VerticalAlignmentType(rd.ReadBitShort())
	if q := rd.ReadBitShort(); q >= 0 && q <= 100 {
		h.TextQuality = q
	}
	h.LineTypeScale = rd.ReadBitDouble()
	h.TextHeightDefault = rd.ReadBitDouble()
	h.TraceWidthDefault = rd.ReadBitDouble()
	h.SketchIncrement = rd.ReadBitDouble()
	h.FilletRadius = rd.ReadBitDouble()
	h.ThicknessDefault = rd.ReadBitDouble()
	h.AngleBase = rd.ReadBitDouble()
	h.PointDisplaySize = rd.ReadBitDouble()
	h.PolylineWidthDefault = rd.ReadBitDouble()
	h.UserDouble1 = rd.ReadBitDouble()
	h.UserDouble2 = rd.ReadBitDouble()
	h.UserDouble3 = rd.ReadBitDouble()
	h.UserDouble4 = rd.ReadBitDouble()
	h.UserDouble5 = rd.ReadBitDouble()
	h.ChamferDistance1 = rd.ReadBitDouble()
	h.ChamferDistance2 = rd.ReadBitDouble()
	h.ChamferLength = rd.ReadBitDouble()
	h.ChamferAngle = rd.ReadBitDouble()
	if fr := rd.ReadBitDouble(); fr > 0 && fr <= 10 {
		h.FacetResolution = fr
	}
	h.CurrentMultilineScale = rd.ReadBitDouble()
	h.CurrentEntityLinetypeScale = rd.ReadBitDouble()
	h.MenuFileName = rd.ReadVariableText()
	h.CreateDateTime = rd.ReadDateTime()
	h.UpdateDateTime = rd.ReadDateTime()
	if f.R2004Plus {
		rd.ReadBitLong()
		rd.ReadBitLong()
		rd.ReadBitLong()
	}
	h.TotalEditingTime = rd.ReadTimeSpan()
	h.UserElapsedTimeSpan = rd.ReadTimeSpan()
	h.CurrentEntityColor = rd.ReadCmColor(false)
	// HANDSEED is part of the main data stream, not the handle stream.
	h.HandleSeed = mainReader.HandleReference(0)
	ptr.set("CLAYER", rd.HandleReference(0))
	ptr.set("TEXTSTYLE", rd.HandleReference(0))
	ptr.set("CELTYPE", rd.HandleReference(0))
	if f.R2007Plus {
		ptr.set("CMATERIAL", rd.HandleReference(0))
	}
	ptr.set("DIMSTYLE", rd.HandleReference(0))
	ptr.set("CMLSTYLE", rd.HandleReference(0))
	if f.R2000Plus {
		h.ViewportDefaultViewScaleFactor = rd.ReadBitDouble()
	}
	h.PaperSpaceInsertionBase = rd.Read3BitDouble()
	h.PaperSpaceExtMin = rd.Read3BitDouble()
	h.PaperSpaceExtMax = rd.Read3BitDouble()
	h.PaperSpaceLimitsMin = rd.Read2RawDouble()
	h.PaperSpaceLimitsMax = rd.Read2RawDouble()
	h.PaperSpaceElevation = rd.ReadBitDouble()
	h.PaperSpaceUcsOrigin = rd.Read3BitDouble()
	h.PaperSpaceUcsXAxis = rd.Read3BitDouble()
	h.PaperSpaceUcsYAxis = rd.Read3BitDouble()
	ptr.set("UCSNAME_PSPACE", rd.HandleReference(0))
	if f.R2000Plus {
		ptr.set("PUCSORTHOREF", rd.HandleReference(0))
		rd.ReadBitShort() // PUCSORTHOVIEW
		ptr.set("PUCSBASE", rd.HandleReference(0))
		h.PaperSpaceOrthographicTopDOrigin = rd.Read3BitDouble()
		h.PaperSpaceOrthographicBottomDOrigin = rd.Read3BitDouble()
		h.PaperSpaceOrthographicLeftDOrigin = rd.Read3BitDouble()
		h.PaperSpaceOrthographicRightDOrigin = rd.Read3BitDouble()
		h.PaperSpaceOrthographicFrontDOrigin = rd.Read3BitDouble()
		h.PaperSpaceOrthographicBackDOrigin = rd.Read3BitDouble()
	}
	h.ModelSpaceInsertionBase = rd.Read3BitDouble()
	h.ModelSpaceExtMin = rd.Read3BitDouble()
	h.ModelSpaceExtMax = rd.Read3BitDouble()
	h.ModelSpaceLimitsMin = rd.Read2RawDouble()
	h.ModelSpaceLimitsMax = rd.Read2RawDouble()
	h.Elevation = rd.ReadBitDouble()
	h.ModelSpaceOrigin = rd.Read3BitDouble()
	h.ModelSpaceXAxis = rd.Read3BitDouble()
	h.ModelSpaceYAxis = rd.Read3BitDouble()
	ptr.set("UCSNAME_MSPACE", rd.HandleReference(0))
	if f.R2000Plus {
		ptr.set("UCSORTHOREF", rd.HandleReference(0))
		rd.ReadBitShort() // UCSORTHOVIEW
		ptr.set("UCSBASE", rd.HandleReference(0))
		h.ModelSpaceOrthographicTopDOrigin = rd.Read3BitDouble()
		h.ModelSpaceOrthographicBottomDOrigin = rd.Read3BitDouble()
		h.ModelSpaceOrthographicLeftDOrigin = rd.Read3BitDouble()
		h.ModelSpaceOrthographicRightDOrigin = rd.Read3BitDouble()
		h.ModelSpaceOrthographicFrontDOrigin = rd.Read3BitDouble()
		h.ModelSpaceOrthographicBackDOrigin = rd.Read3BitDouble()
		h.DimensionPostFix = rd.ReadVariableText()
		h.DimensionAlternateDimensioningSuffix = rd.ReadVariableText()
	}
	if f.R13_14Only {
		h.DimensionGenerateTolerances = rd.ReadBit()
		h.DimensionLimitsGeneration = rd.ReadBit()
		h.DimensionTextInsideHorizontal = rd.ReadBit()
		h.DimensionTextOutsideHorizontal = rd.ReadBit()
		h.DimensionSuppressFirstExtensionLine = rd.ReadBit()
		h.DimensionSuppressSecondExtensionLine = rd.ReadBit()
		h.DimensionAlternateUnitDimensioning = rd.ReadBit()
		h.DimensionTextOutsideExtensions = rd.ReadBit()
		h.DimensionSeparateArrowBlocks = rd.ReadBit()
		h.DimensionTextInsideExtensions = rd.ReadBit()
		h.DimensionSuppressOutsideExtensions = rd.ReadBit()
		h.DimensionAlternateUnitDecimalPlaces = int16(rd.ReadRawChar())
		h.DimensionZeroHandling = cadoc.ZeroHandling(rd.ReadRawChar())
		h.DimensionSuppressFirstDimensionLine = rd.ReadBit()
		h.DimensionSuppressSecondDimensionLine = rd.ReadBit()
		h.DimensionToleranceAlignment = cadoc.ToleranceAlignment(rd.ReadRawChar())
		h.DimensionTextHorizontalAlignment = cadoc.DimensionTextHorizontalAlignment(rd.ReadRawChar())
		h.DimensionFit = int16(rd.ReadRawChar())
		h.DimensionCursorUpdate = rd.ReadBit()
		h.DimensionToleranceZeroHandling = cadoc.ZeroHandling(rd.ReadRawChar())
		h.DimensionAlternateUnitZeroHandling = cadoc.ZeroHandling(rd.ReadRawChar())
		h.DimensionAlternateUnitToleranceZeroHandling = cadoc.ZeroHandling(rd.ReadRawChar())
		h.DimensionTextVerticalAlignment = cadoc.DimensionTextVerticalAlignment(rd.ReadRawChar())
		h.DimensionUnit = rd.ReadBitShort()
		h.DimensionAngularDimensionDecimalPlaces = rd.ReadBitShort()
		h.DimensionDecimalPlaces = rd.ReadBitShort()
		h.DimensionToleranceDecimalPlaces = rd.ReadBitShort()
		h.DimensionAlternateUnitFormat = cadoc.LinearUnitFormat(rd.ReadBitShort())
		h.DimensionAlternateUnitToleranceDecimalPlaces = rd.ReadBitShort()
		ptr.set("DIMTXSTY", rd.HandleReference(0))
	}
	h.DimensionScaleFactor = rd.ReadBitDouble()
	h.DimensionArrowSize = rd.ReadBitDouble()
	h.DimensionExtensionLineOffset = rd.ReadBitDouble()
	h.DimensionLineIncrement = rd.ReadBitDouble()
	h.DimensionExtensionLineExtension = rd.ReadBitDouble()
	h.DimensionRounding = rd.ReadBitDouble()
	h.DimensionLineExtension = rd.ReadBitDouble()
	h.DimensionPlusTolerance = rd.ReadBitDouble()
	h.DimensionMinusTolerance = rd.ReadBitDouble()
	if f.R2007Plus {
		h.DimensionFixedExtensionLineLength = rd.ReadBitDouble()
		jog := rd.ReadBitDouble()
		if rounded := math.Round(jog*1e6) / 1e6; rounded > 5*math.Pi/180 && rounded < math.Pi/2 {
			h.DimensionJoggedRadiusDimensionTransverseSegmentAngle = jog
		}
		h.DimensionTextBackgroundFillMode = cadoc.DimensionTextBackgroundFillMode(rd.ReadBitShort())
		h.DimensionTextBackgroundColor = rd.ReadCmColor(false)
	}
	if f.R2000Plus {
		h.DimensionGenerateTolerances = rd.ReadBit()
		h.DimensionLimitsGeneration = rd.ReadBit()
		h.DimensionTextInsideHorizontal = rd.ReadBit()
		h.DimensionTextOutsideHorizontal = rd.ReadBit()
		h.DimensionSuppressFirstExtensionLine = rd.ReadBit()
		h.DimensionSuppressSecondExtensionLine = rd.ReadBit()
		h.DimensionTextVerticalAlignment = cadoc.DimensionTextVerticalAlignment(uint16(rd.ReadBitShort()))
		h.DimensionZeroHandling = cadoc.ZeroHandling(uint16(rd.ReadBitShort()))
		h.DimensionAngularZeroHandling = cadoc.AngularZeroHandling(rd.ReadBitShort())
	}
	if f.R2007Plus {
		h.DimensionArcLengthSymbolPosition = cadoc.ArcLengthSymbolPosition(rd.ReadBitShort())
	}
	h.DimensionTextHeight = rd.ReadBitDouble()
	h.DimensionCenterMarkSize = rd.ReadBitDouble()
	h.DimensionTickSize = rd.ReadBitDouble()
	h.DimensionAlternateUnitScaleFactor = rd.ReadBitDouble()
	h.DimensionLinearScaleFactor = rd.ReadBitDouble()
	h.DimensionTextVerticalPosition = rd.ReadBitDouble()
	h.DimensionToleranceScaleFactor = rd.ReadBitDouble()
	h.DimensionLineGap = rd.ReadBitDouble()
	if f.R13_14Only {
		h.DimensionPostFix = rd.ReadVariableText()
		h.DimensionAlternateDimensioningSuffix = rd.ReadVariableText()
		h.DimensionBlockName = rd.ReadVariableText()
		h.DimensionBlockNameFirst = rd.ReadVariableText()
		h.DimensionBlockNameSecond = rd.ReadVariableText()
	}
	if f.R2000Plus {
		h.DimensionAlternateUnitRounding = rd.ReadBitDouble()
		h.DimensionAlternateUnitDimensioning = rd.ReadBit()
		h.DimensionAlternateUnitDecimalPlaces = int16(uint16(rd.ReadBitShort()))
		h.DimensionTextOutsideExtensions = rd.ReadBit()
		h.DimensionSeparateArrowBlocks = rd.ReadBit()
		h.DimensionTextInsideExtensions = rd.ReadBit()
		h.DimensionSuppressOutsideExtensions = rd.ReadBit()
	}
	h.DimensionLineColor = rd.ReadCmColor(false)
	h.DimensionExtensionLineColor = rd.ReadCmColor(false)
	h.DimensionTextColor = rd.ReadCmColor(false)
	if f.R2000Plus {
		h.DimensionAngularDimensionDecimalPlaces = rd.ReadBitShort()
		h.DimensionDecimalPlaces = rd.ReadBitShort()
		h.DimensionToleranceDecimalPlaces = rd.ReadBitShort()
		h.DimensionAlternateUnitFormat = cadoc.LinearUnitFormat(rd.ReadBitShort())
		h.DimensionAlternateUnitToleranceDecimalPlaces = rd.ReadBitShort()
		h.DimensionAngularUnit = cadoc.AngularUnitFormat(rd.ReadBitShort())
		h.DimensionFractionFormat = cadoc.FractionFormat(rd.ReadBitShort())
		h.DimensionLinearUnitFormat = cadoc.LinearUnitFormat(rd.ReadBitShort())
		h.DimensionDecimalSeparator = rune(uint16(rd.ReadBitShort()))
		h.DimensionTextMovement = cadoc.TextMovement(rd.ReadBitShort())
		h.DimensionTextHorizontalAlignment = cadoc.DimensionTextHorizontalAlignment(uint16(rd.ReadBitShort()))
		// DIMSD1/DIMSD2 (ACadSharp stores them in DIMSE1/DIMSE2 by mistake).
		h.DimensionSuppressFirstDimensionLine = rd.ReadBit()
		h.DimensionSuppressSecondDimensionLine = rd.ReadBit()
		h.DimensionToleranceAlignment = cadoc.ToleranceAlignment(uint16(rd.ReadBitShort()))
		h.DimensionToleranceZeroHandling = cadoc.ZeroHandling(uint16(rd.ReadBitShort()))
		h.DimensionAlternateUnitZeroHandling = cadoc.ZeroHandling(uint16(rd.ReadBitShort()))
		h.DimensionAlternateUnitToleranceZeroHandling = cadoc.ZeroHandling(uint16(rd.ReadBitShort()))
		h.DimensionCursorUpdate = rd.ReadBit()
		h.DimensionDimensionTextArrowFit = cadoc.TextArrowFitType(rd.ReadBitShort())
	}
	if f.R2007Plus {
		h.DimensionIsExtensionLineLengthFixed = rd.ReadBit()
	}
	if f.R2010Plus {
		if rd.ReadBit() {
			h.DimensionTextDirection = cadoc.TextDirectionRightToLeft
		} else {
			h.DimensionTextDirection = cadoc.TextDirectionLeftToRight
		}
		h.DimensionAltMzf = rd.ReadBitDouble()
		h.DimensionAltMzs = rd.ReadVariableText()
		h.DimensionMzf = rd.ReadBitDouble()
		h.DimensionMzs = rd.ReadVariableText()
	}
	if f.R2000Plus {
		ptr.set("DIMTXSTY", rd.HandleReference(0))
		ptr.set("DIMLDRBLK", rd.HandleReference(0))
		ptr.set("DIMBLK", rd.HandleReference(0))
		ptr.set("DIMBLK1", rd.HandleReference(0))
		ptr.set("DIMBLK2", rd.HandleReference(0))
	}
	if f.R2007Plus {
		ptr.set("DIMLTYPE", rd.HandleReference(0))
		ptr.set("DIMLTEX1", rd.HandleReference(0))
		ptr.set("DIMLTEX2", rd.HandleReference(0))
	}
	if f.R2000Plus {
		h.DimensionLineWeight = cadoc.LineWeight(rd.ReadBitShort())
		h.ExtensionLineWeight = cadoc.LineWeight(rd.ReadBitShort())
	}
	ptr.set("BLOCK_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("LAYER_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("STYLE_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("LINETYPE_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("VIEW_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("UCS_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("VPORT_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("APPID_CONTROL_OBJECT", rd.HandleReference(0))
	ptr.set("DIMSTYLE_CONTROL_OBJECT", rd.HandleReference(0))
	if f.R13_15Only {
		ptr.set("VIEWPORT_ENTITY_HEADER_CONTROL_OBJECT", rd.HandleReference(0))
	}
	ptr.set("DICTIONARY_ACAD_GROUP", rd.HandleReference(0))
	ptr.set("DICTIONARY_ACAD_MLINESTYLE", rd.HandleReference(0))
	ptr.set("DICTIONARY_NAMED_OBJECTS", rd.HandleReference(0))
	if f.R2000Plus {
		h.StackedTextAlignment = rd.ReadBitShort()
		h.StackedTextSizePercentage = rd.ReadBitShort()
		h.HyperLinkBase = rd.ReadVariableText()
		h.StyleSheetName = rd.ReadVariableText()
		ptr.set("DICTIONARY_LAYOUTS", rd.HandleReference(0))
		ptr.set("DICTIONARY_PLOTSETTINGS", rd.HandleReference(0))
		ptr.set("DICTIONARY_PLOTSTYLES", rd.HandleReference(0))
	}
	if f.R2004Plus {
		ptr.set("DICTIONARY_MATERIALS", rd.HandleReference(0))
		ptr.set("DICTIONARY_COLORS", rd.HandleReference(0))
	}
	if f.R2007Plus {
		ptr.set("DICTIONARY_VISUALSTYLE", rd.HandleReference(0))
		if f.R2013Plus {
			// Unknown handle, stored where ACadSharp stores it.
			ptr.set("DICTIONARY_VISUALSTYLE", rd.HandleReference(0))
		}
	}
	if f.R2000Plus {
		flags := rd.ReadBitLong()
		// ACadSharp casts and compares these flags without shifting; they are
		// decoded here the way AutoCAD defines them (CELWEIGHT is a line
		// weight index, LWDISPLAY and XEDIT are negated bits).
		h.CurrentEntityLineWeight = cadoc.LineWeightFromIndex(byte(flags & 0x1F))
		h.EndCaps = int16(flags & 0x60 >> 5)
		h.JoinStyle = int16(flags & 0x180 >> 7)
		h.DisplayLineWeight = flags&0x200 == 0
		h.XEdit = flags&0x400 == 0
		h.ExtendedNames = flags&0x800 != 0
		h.PlotStyleMode = int16(flags & 0x2000 >> 13)
		h.LoadOLEObject = flags&0x4000 != 0
		h.InsUnits = cadoc.UnitsType(rd.ReadBitShort())
		h.CurrentEntityPlotStyle = cadoc.EntityPlotStyleType(rd.ReadBitShort())
		if h.CurrentEntityPlotStyle == cadoc.EntityPlotStyleTypeByObjectId {
			ptr.set("CPSNID", rd.HandleReference(0))
		}
		h.FingerPrintGuid = rd.ReadVariableText()
		h.VersionGuid = rd.ReadVariableText()
	}
	if f.R2004Plus {
		h.EntitySortingFlags = cadoc.ObjectSortingFlags(rd.ReadRC())
		h.IndexCreationFlags = cadoc.IndexCreationFlags(rd.ReadRC())
		h.HideText = rd.ReadRC()
		h.ExternalReferenceClippingBoundaryType = cadoc.XClipFrameType(rd.ReadRC())
		h.DimensionAssociativity = cadoc.HeaderDimensionAssociation(rd.ReadRC())
		h.HaloGapPercentage = rd.ReadRC()
		h.ObscuredColor = colorIndex(rd.ReadBitShort())
		h.InterfereColor = colorIndex(rd.ReadBitShort())
		h.ObscuredType = rd.ReadRC()
		h.IntersectionDisplay = rd.ReadRC()
		h.ProjectName = rd.ReadVariableText()
	}
	ptr.set("PAPER_SPACE", rd.HandleReference(0))
	ptr.set("MODEL_SPACE", rd.HandleReference(0))
	ptr.set("BYLAYER", rd.HandleReference(0))
	ptr.set("BYBLOCK", rd.HandleReference(0))
	ptr.set("CONTINUOUS", rd.HandleReference(0))
	if f.R2007Plus {
		h.CameraDisplayObjects = rd.ReadBit()
		rd.ReadBitLong()
		rd.ReadBitLong()
		rd.ReadBitDouble()
		if sps := rd.ReadBitDouble(); sps >= 1 && sps <= 30 {
			h.StepsPerSecond = sps
		}
		h.StepSize = rd.ReadBitDouble()
		h.Dw3DPrecision = rd.ReadBitDouble()
		h.LensLength = rd.ReadBitDouble()
		h.CameraHeight = rd.ReadBitDouble()
		h.SolidsRetainHistory = rd.ReadRawChar()
		h.ShowSolidsHistory = rd.ReadRawChar()
		h.SweptSolidWidth = rd.ReadBitDouble()
		h.SweptSolidHeight = rd.ReadBitDouble()
		h.DraftAngleFirstCrossSection = rd.ReadBitDouble()
		h.DraftAngleSecondCrossSection = rd.ReadBitDouble()
		h.DraftMagnitudeFirstCrossSection = rd.ReadBitDouble()
		h.DraftMagnitudeSecondCrossSection = rd.ReadBitDouble()
		h.SolidLoftedShape = rd.ReadBitShort()
		h.LoftedObjectNormals = rd.ReadRawChar()
		h.Latitude = rd.ReadBitDouble()
		h.Longitude = rd.ReadBitDouble()
		h.NorthDirection = rd.ReadBitDouble()
		h.TimeZone = rd.ReadBitLong()
		h.DisplayLightGlyphs = rd.ReadRawChar()
		rd.ReadRawChar() // TILEMODELIGHTSYNCH
		h.DwgUnderlayFramesVisibility = rd.ReadRawChar()
		h.DgnUnderlayFramesVisibility = rd.ReadRawChar()
		rd.ReadBit()
		h.InterfereColor = rd.ReadCmColor(false)
		ptr.set("INTERFEREOBJVS", rd.HandleReference(0))
		ptr.set("INTERFEREVPVS", rd.HandleReference(0))
		ptr.set("DRAGVS", rd.HandleReference(0))
		h.ShadowMode = cadoc.ShadowMode(rd.ReadRC())
		h.ShadowPlaneLocation = rd.ReadBitDouble()
	}

	func() {
		// The end sentinel is not needed for the data.
		defer func() {
			if p := recover(); p != nil {
				r.note(NotifyError, "An error ocurred at the end of the Header reading: %v", p)
			}
		}()
		mainReader.SetPositionInBits(initialPos + size*8)
		mainReader.ResetShift()
		if mainReader.ReadSentinel() != SentinelHeaderEnd {
			r.note(NotifyWarning, "Invalid section sentinel found in %s", SectionHeader)
		}
	}()
	h.MaintenanceVersion = v.MaintenanceVersion()
	return ptr
}
