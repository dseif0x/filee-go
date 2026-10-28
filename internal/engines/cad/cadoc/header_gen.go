// Code generated from ACadSharp CadHeader (tools/genheader3.py); DO NOT EDIT.

package cadoc

import (
	"math"
	"time"
)

// Header holds the drawing variables (ACadSharp's CadHeader). Field names are
// ACadSharp's property names; HeaderVariables maps them to $NAME and DXF codes.
type Header struct {
	// Version is $ACADVER.
	Version                                              Version
	AngleBase                                            float64                          // $ANGBASE
	AngularDirection                                     AngularDirection                 // $ANGDIR
	AngularUnit                                          AngularUnitFormat                // $AUNITS
	AngularUnitPrecision                                 int16                            // $AUPREC
	ArrowBlockName                                       string                           // $DIMLDRBLK
	AssociatedDimensions                                 bool                             // $DIMASO
	AttributeVisibility                                  AttributeVisibilityMode          // $ATTMODE
	BlipMode                                             bool                             // $BLIPMODE
	CameraDisplayObjects                                 bool                             // $CAMERADISPLAY
	CameraHeight                                         float64                          // $CAMERAHEIGHT
	ChamferAngle                                         float64                          // $CHAMFERD
	ChamferDistance1                                     float64                          // $CHAMFERA
	ChamferDistance2                                     float64                          // $CHAMFERB
	ChamferLength                                        float64                          // $CHAMFERC
	CodePage                                             string                           // $DWGCODEPAGE
	CreateDateTime                                       time.Time                        // $TDCREATE
	CreateEllipseAsPolyline                              bool                             // $PELLIPSE
	CurrentDimensionStyleName                            string                           // $DIMSTYLE
	CurrentEntityColor                                   Color                            // $CECOLOR
	CurrentEntityLinetypeScale                           float64                          // $CELTSCALE
	CurrentEntityLineWeight                              LineWeight                       // $CELWEIGHT
	CurrentEntityPlotStyle                               EntityPlotStyleType              // $CEPSNTYPE
	CurrentLayerName                                     string                           // $CLAYER
	CurrentLineTypeName                                  string                           // $CELTYPE
	CurrentMLineStyleName                                string                           // $CMLSTYLE
	CurrentMultiLineJustification                        VerticalAlignmentType            // $CMLJUST
	CurrentMultilineScale                                float64                          // $CMLSCALE
	CurrentTextStyleName                                 string                           // $TEXTSTYLE
	DgnUnderlayFramesVisibility                          uint8                            // $DGNFRAME
	DimensionAlternateDimensioningSuffix                 string                           // $DIMAPOST
	DimensionAlternateUnitDecimalPlaces                  int16                            // $DIMALTD
	DimensionAlternateUnitDimensioning                   bool                             // $DIMALT
	DimensionAlternateUnitFormat                         LinearUnitFormat                 // $DIMALTU
	DimensionAlternateUnitRounding                       float64                          // $DIMALTRND
	DimensionAlternateUnitScaleFactor                    float64                          // $DIMALTF
	DimensionAlternateUnitToleranceDecimalPlaces         int16                            // $DIMALTTD
	DimensionAlternateUnitToleranceZeroHandling          ZeroHandling                     // $DIMALTTZ
	DimensionAlternateUnitZeroHandling                   ZeroHandling                     // $DIMALTZ
	DimensionAltMzf                                      float64                          // $DIMALTMZF
	DimensionAltMzs                                      string                           // $DIMALTMZS
	DimensionAngularDimensionDecimalPlaces               int16                            // $DIMADEC
	DimensionAngularUnit                                 AngularUnitFormat                // $DIMAUNIT
	DimensionAngularZeroHandling                         AngularZeroHandling              // $DIMAZIN
	DimensionArcLengthSymbolPosition                     ArcLengthSymbolPosition          // $DIMARCSYM
	DimensionArrowSize                                   float64                          // $DIMASZ
	DimensionAssociativity                               HeaderDimensionAssociation       // $DIMASSOC
	DimensionBlockName                                   string                           // $DIMBLK
	DimensionBlockNameFirst                              string                           // $DIMBLK1
	DimensionBlockNameSecond                             string                           // $DIMBLK2
	DimensionCenterMarkSize                              float64                          // $DIMCEN
	DimensionCursorUpdate                                bool                             // $DIMUPT
	DimensionDecimalPlaces                               int16                            // $DIMDEC
	DimensionDecimalSeparator                            rune                             // $DIMDSEP
	DimensionDimensionTextArrowFit                       TextArrowFitType                 // $DIMATFIT
	DimensionExtensionLineColor                          Color                            // $DIMCLRE
	DimensionExtensionLineExtension                      float64                          // $DIMEXE
	DimensionExtensionLineOffset                         float64                          // $DIMEXO
	DimensionFit                                         int16                            // $DIMFIT
	DimensionFixedExtensionLineLength                    float64                          // $DIMFXL
	DimensionFractionFormat                              FractionFormat                   // $DIMFRAC
	DimensionGenerateTolerances                          bool                             // $DIMTOL
	DimensionIsExtensionLineLengthFixed                  bool                             // $DIMFXLON
	DimensionJoggedRadiusDimensionTransverseSegmentAngle float64                          // $DIMJOGANG
	DimensionLimitsGeneration                            bool                             // $DIMLIM
	DimensionLinearScaleFactor                           float64                          // $DIMLFAC
	DimensionLinearUnitFormat                            LinearUnitFormat                 // $DIMLUNIT
	DimensionLineColor                                   Color                            // $DIMCLRD
	DimensionLineExtension                               float64                          // $DIMDLE
	DimensionLineGap                                     float64                          // $DIMGAP
	DimensionLineIncrement                               float64                          // $DIMDLI
	DimensionLineType                                    string                           // $DIMLTYPE
	DimensionLineWeight                                  LineWeight                       // $DIMLWD
	DimensionMinusTolerance                              float64                          // $DIMTM
	DimensionMzf                                         float64                          // $DIMMZF
	DimensionMzs                                         string                           // $DIMMZS
	DimensionPlusTolerance                               float64                          // $DIMTP
	DimensionPostFix                                     string                           // $DIMPOST
	DimensionRounding                                    float64                          // $DIMRND
	DimensionScaleFactor                                 float64                          // $DIMSCALE
	DimensionSeparateArrowBlocks                         bool                             // $DIMSAH
	DimensionSuppressFirstDimensionLine                  bool                             // $DIMSD1
	DimensionSuppressFirstExtensionLine                  bool                             // $DIMSE1
	DimensionSuppressOutsideExtensions                   bool                             // $DIMSOXD
	DimensionSuppressSecondDimensionLine                 bool                             // $DIMSD2
	DimensionSuppressSecondExtensionLine                 bool                             // $DIMSE2
	DimensionTex1                                        string                           // $DIMLTEX1
	DimensionTex2                                        string                           // $DIMLTEX2
	DimensionTextBackgroundColor                         Color                            // $DIMTFILLCLR
	DimensionTextBackgroundFillMode                      DimensionTextBackgroundFillMode  // $DIMTFILL
	DimensionTextColor                                   Color                            // $DIMCLRT
	DimensionTextDirection                               TextDirection                    // $DIMTXTDIRECTION
	DimensionTextHeight                                  float64                          // $DIMTXT
	DimensionTextHorizontalAlignment                     DimensionTextHorizontalAlignment // $DIMJUST
	DimensionTextInsideExtensions                        bool                             // $DIMTIX
	DimensionTextInsideHorizontal                        bool                             // $DIMTIH
	DimensionTextMovement                                TextMovement                     // $DIMTMOVE
	DimensionTextOutsideExtensions                       bool                             // $DIMTOFL
	DimensionTextOutsideHorizontal                       bool                             // $DIMTOH
	DimensionTextStyleName                               string                           // $DIMTXSTY
	DimensionTextVerticalAlignment                       DimensionTextVerticalAlignment   // $DIMTAD
	DimensionTextVerticalPosition                        float64                          // $DIMTVP
	DimensionTickSize                                    float64                          // $DIMTSZ
	DimensionToleranceAlignment                          ToleranceAlignment               // $DIMTOLJ
	DimensionToleranceDecimalPlaces                      int16                            // $DIMTDEC
	DimensionToleranceScaleFactor                        float64                          // $DIMTFAC
	DimensionToleranceZeroHandling                       ZeroHandling                     // $DIMTZIN
	DimensionUnit                                        int16                            // $DIMUNIT
	DimensionZeroHandling                                ZeroHandling                     // $DIMZIN
	DisplayLightGlyphs                                   uint8                            // $LIGHTGLYPHDISPLAY
	DisplayLineWeight                                    bool                             // $LWDISPLAY
	DisplaySilhouetteCurves                              bool                             // $DISPSILH
	DraftAngleFirstCrossSection                          float64                          // $LOFTANG1
	DraftAngleSecondCrossSection                         float64                          // $LOFTANG2
	DraftMagnitudeFirstCrossSection                      float64                          // $LOFTMAG1
	DraftMagnitudeSecondCrossSection                     float64                          // $LOFTMAG2
	Dw3DPrecision                                        float64                          // $3DDWFPREC
	DwgUnderlayFramesVisibility                          uint8                            // $DWFFRAME
	Elevation                                            float64                          // $ELEVATION
	EndCaps                                              int16                            // $ENDCAPS
	EntitySortingFlags                                   ObjectSortingFlags               // $SORTENTS
	ExtendedNames                                        bool                             // $EXTNAMES
	ExtensionLineWeight                                  LineWeight                       // $DIMLWE
	ExternalReferenceClippingBoundaryType                XClipFrameType                   // $XCLIPFRAME
	FacetResolution                                      float64                          // $FACETRES
	FilletRadius                                         float64                          // $FILLETRAD
	FillMode                                             bool                             // $FILLMODE
	FingerPrintGuid                                      string                           // $FINGERPRINTGUID
	HaloGapPercentage                                    uint8                            // $HALOGAP
	HandleSeed                                           uint64                           // $HANDSEED
	HideText                                             uint8                            // $HIDETEXT
	HyperLinkBase                                        string                           // $HYPERLINKBASE
	IndexCreationFlags                                   IndexCreationFlags               // $INDEXCTL
	InsUnits                                             UnitsType                        // $INSUNITS
	InterfereColor                                       Color                            // $INTERFERECOLOR
	IntersectionDisplay                                  uint8
	JoinStyle                                            int16            // $JOINSTYLE
	LastSavedBy                                          string           // $LASTSAVEDBY
	Latitude                                             float64          // $LATITUDE
	LensLength                                           float64          // $LENSLENGTH
	LimitCheckingOn                                      bool             // $LIMCHECK
	LinearUnitFormat                                     LinearUnitFormat // $LUNITS
	LinearUnitPrecision                                  int16            // $LUPREC
	LineTypeScale                                        float64          // $LTSCALE
	LoadOLEObject                                        bool
	LoftedObjectNormals                                  uint8            // $LOFTNORMALS
	Longitude                                            float64          // $LONGITUDE
	MaintenanceVersion                                   int16            // $ACADMAINTVER
	MaxViewportCount                                     int16            // $MAXACTVP
	MeasurementUnits                                     MeasurementUnits // $MEASUREMENT
	MenuFileName                                         string           // $MENU
	MirrorText                                           bool             // $MIRRTEXT
	ModelSpaceExtMax                                     Vec3             // $EXTMAX
	ModelSpaceExtMin                                     Vec3             // $EXTMIN
	ModelSpaceInsertionBase                              Vec3             // $INSBASE
	ModelSpaceLimitsMax                                  Vec2             // $LIMMAX
	ModelSpaceLimitsMin                                  Vec2             // $LIMMIN
	ModelSpaceOrigin                                     Vec3             // $UCSORG
	ModelSpaceOrthographicBackDOrigin                    Vec3             // $UCSORGBACK
	ModelSpaceOrthographicBottomDOrigin                  Vec3             // $UCSORGBOTTOM
	ModelSpaceOrthographicFrontDOrigin                   Vec3             // $UCSORGFRONT
	ModelSpaceOrthographicLeftDOrigin                    Vec3             // $UCSORGLEFT
	ModelSpaceOrthographicRightDOrigin                   Vec3             // $UCSORGRIGHT
	ModelSpaceOrthographicTopDOrigin                     Vec3             // $UCSORGTOP
	ModelSpaceXAxis                                      Vec3             // $UCSXDIR
	ModelSpaceYAxis                                      Vec3             // $UCSYDIR
	NorthDirection                                       float64          // $NORTHDIRECTION
	NumberOfSplineSegments                               int16            // $SPLINESEGS
	ObjectSnapMode                                       ObjectSnapMode   // $OSMODE
	ObscuredColor                                        Color
	ObscuredType                                         uint8
	OrthoMode                                            bool                 // $ORTHOMODE
	PaperSpaceBaseName                                   string               // $PUCSBASE
	PaperSpaceElevation                                  float64              // $PELEVATION
	PaperSpaceExtMax                                     Vec3                 // $PEXTMAX
	PaperSpaceExtMin                                     Vec3                 // $PEXTMIN
	PaperSpaceInsertionBase                              Vec3                 // $PINSBASE
	PaperSpaceLimitsChecking                             bool                 // $PLIMCHECK
	PaperSpaceLimitsMax                                  Vec2                 // $PLIMMAX
	PaperSpaceLimitsMin                                  Vec2                 // $PLIMMIN
	PaperSpaceLineTypeScaling                            SpaceLineTypeScaling // $PSLTSCALE
	PaperSpaceName                                       string               // $PUCSNAME
	PaperSpaceOrthographicBackDOrigin                    Vec3                 // $PUCSORGBACK
	PaperSpaceOrthographicBottomDOrigin                  Vec3                 // $PUCSORGBOTTOM
	PaperSpaceOrthographicFrontDOrigin                   Vec3                 // $PUCSORGFRONT
	PaperSpaceOrthographicLeftDOrigin                    Vec3                 // $PUCSORGLEFT
	PaperSpaceOrthographicRightDOrigin                   Vec3                 // $PUCSORGRIGHT
	PaperSpaceOrthographicTopDOrigin                     Vec3                 // $PUCSORGTOP
	PaperSpaceUcsOrigin                                  Vec3                 // $PUCSORG
	PaperSpaceUcsXAxis                                   Vec3                 // $PUCSXDIR
	PaperSpaceUcsYAxis                                   Vec3                 // $PUCSYDIR
	PlotStyleMode                                        int16                // $PSTYLEMODE
	PointDisplayMode                                     int16                // $PDMODE
	PointDisplaySize                                     float64              // $PDSIZE
	PolylineLineTypeGeneration                           bool                 // $PLINEGEN
	PolylineWidthDefault                                 float64              // $PLINEWID
	ProjectName                                          string               // $PROJECTNAME
	ProxyGraphics                                        bool                 // $PROXYGRAPHICS
	QuickTextMode                                        bool                 // $QTEXTMODE
	RegenerationMode                                     bool                 // $REGENMODE
	RequiredVersions                                     int64                // $REQUIREDVERSIONS
	RetainXRefDependentVisibilitySettings                bool                 // $VISRETAIN
	ShadeDiffuseToAmbientPercentage                      int16                // $SHADEDIF
	ShadeEdge                                            ShadeEdgeType        // $SHADEDGE
	ShadowMode                                           ShadowMode           // $CSHADOW
	ShadowPlaneLocation                                  float64              // $SHADOWPLANELOCATION
	ShowModelSpace                                       bool                 // $TILEMODE
	ShowSolidsHistory                                    uint8                // $SHOWHIST
	ShowSplineControlPoints                              bool                 // $SPLFRAME
	SketchIncrement                                      float64              // $SKETCHINC
	SketchPolylines                                      bool                 // $SKPOLY
	SolidLoftedShape                                     int16                // $LOFTPARAM
	SolidsRetainHistory                                  uint8                // $SOLIDHIST
	SpatialIndexMaxTreeDepth                             int16                // $TREEDEPTH
	SplineType                                           SplineType           // $SPLINETYPE
	StackedTextAlignment                                 int16
	StackedTextSizePercentage                            int16
	StepSize                                             float64 // $STEPSIZE
	StepsPerSecond                                       float64 // $STEPSPERSEC
	StyleSheetName                                       string  // $STYLESHEET
	SurfaceDensityU                                      int16   // $SURFU
	SurfaceDensityV                                      int16   // $SURFV
	SurfaceIsolineCount                                  int16
	SurfaceMeshTabulationCount1                          int16   // $SURFTAB1
	SurfaceMeshTabulationCount2                          int16   // $SURFTAB2
	SurfaceType                                          int16   // $SURFTYPE
	SweptSolidHeight                                     float64 // $PSOLHEIGHT
	SweptSolidWidth                                      float64 // $PSOLWIDTH
	TextHeightDefault                                    float64 // $TEXTSIZE
	TextQuality                                          int16
	ThicknessDefault                                     float64       // $THICKNESS
	TimeZone                                             int32         // $TIMEZONE
	TotalEditingTime                                     time.Duration // $TDINDWG
	TraceWidthDefault                                    float64       // $TRACEWID
	UcsBaseName                                          string        // $UCSBASE
	UcsName                                              string        // $UCSNAME
	UnitMode                                             int16         // $UNITMODE
	UniversalCreateDateTime                              time.Time     // $TDUCREATE
	UniversalUpdateDateTime                              time.Time     // $TDUUPDATE
	UpdateDateTime                                       time.Time     // $TDUPDATE
	UpdateDimensionsWhileDragging                        bool          // $DIMSHO
	UserDouble1                                          float64       // $USERR1
	UserDouble2                                          float64       // $USERR2
	UserDouble3                                          float64       // $USERR3
	UserDouble4                                          float64       // $USERR4
	UserDouble5                                          float64       // $USERR5
	UserElapsedTimeSpan                                  time.Duration // $TDUSRTIMER
	UserShort1                                           int16         // $USERI1
	UserShort2                                           int16         // $USERI2
	UserShort3                                           int16         // $USERI3
	UserShort4                                           int16         // $USERI4
	UserShort5                                           int16         // $USERI5
	UserTimer                                            bool          // $USRTIMER
	VersionGuid                                          string        // $VERSIONGUID
	ViewportDefaultViewScaleFactor                       float64       // $PSVPSCALE
	WorldView                                            bool          // $WORLDVIEW
	XEdit                                                bool          // $XEDIT
}

// NewHeader returns a header with ACadSharp's defaults for AC1032.
func NewHeader() *Header {
	h := &Header{Version: AC1032}
	h.AngularUnit = AngularUnitFormatDecimalDegrees
	h.ArrowBlockName = ""
	h.AssociatedDimensions = true
	h.AttributeVisibility = AttributeVisibilityModeNormal
	h.CodePage = "ANSI_1252"
	h.CreateDateTime = time.Now()
	h.CreateEllipseAsPolyline = false
	h.CurrentDimensionStyleName = "Standard"
	h.CurrentEntityColor = ColorByLayer
	h.CurrentEntityLinetypeScale = 1.0
	h.CurrentEntityLineWeight = LineWeightByLayer
	h.CurrentLayerName = "0"
	h.CurrentLineTypeName = "ByLayer"
	h.CurrentMLineStyleName = "Standard"
	h.CurrentMultiLineJustification = VerticalAlignmentTypeTop
	h.CurrentMultilineScale = 20.0
	h.CurrentTextStyleName = "Standard"
	h.DimensionAlternateUnitDecimalPlaces = 3
	h.DimensionAlternateUnitScaleFactor = 25.4
	h.DimensionAlternateUnitToleranceDecimalPlaces = 3
	h.DimensionAlternateUnitToleranceZeroHandling = ZeroHandlingSuppressZeroFeetAndInches
	h.DimensionAlternateUnitZeroHandling = ZeroHandlingSuppressZeroFeetAndInches
	h.DimensionAngularZeroHandling = AngularZeroHandlingDisplayAll
	h.DimensionArcLengthSymbolPosition = ArcLengthSymbolPositionBeforeDimensionText
	h.DimensionArrowSize = 0.18
	h.DimensionAssociativity = HeaderDimensionAssociationCreateAssociativeDimensions
	h.DimensionBlockName = ""
	h.DimensionCenterMarkSize = 0.09
	h.DimensionDecimalPlaces = 2
	h.DimensionDecimalSeparator = '.'
	h.DimensionDimensionTextArrowFit = TextArrowFitTypeBestFit
	h.DimensionExtensionLineColor = ColorByBlock
	h.DimensionExtensionLineExtension = 1.25
	h.DimensionExtensionLineOffset = 0.625
	h.DimensionFixedExtensionLineLength = 1
	h.DimensionJoggedRadiusDimensionTransverseSegmentAngle = math.Pi / 4
	h.DimensionLinearScaleFactor = 1
	h.DimensionLineColor = ColorByBlock
	h.DimensionLineGap = 0.625
	h.DimensionLineIncrement = 3.75
	h.DimensionLineType = "ByBlock"
	h.DimensionLineWeight = LineWeightByBlock
	h.DimensionScaleFactor = 1
	h.DimensionTex1 = "ByBlock"
	h.DimensionTex2 = "ByBlock"
	h.DimensionTextBackgroundColor = ColorByBlock
	h.DimensionTextColor = ColorByBlock
	h.DimensionTextHeight = 0.18
	h.DimensionTextHorizontalAlignment = DimensionTextHorizontalAlignmentCentered
	h.DimensionTextMovement = TextMovementMoveLineWithText
	h.DimensionTextStyleName = "Standard"
	h.DimensionTextVerticalAlignment = DimensionTextVerticalAlignmentAbove
	h.DimensionToleranceAlignment = ToleranceAlignmentBottom
	h.DimensionToleranceDecimalPlaces = 2
	h.DimensionToleranceScaleFactor = 1
	h.DimensionToleranceZeroHandling = ZeroHandlingSuppressDecimalTrailingZeroes
	h.DimensionUnit = 2
	h.DimensionZeroHandling = ZeroHandlingSuppressDecimalTrailingZeroes
	h.DisplayLineWeight = false
	h.DisplaySilhouetteCurves = false
	h.ExtendedNames = true
	h.ExtensionLineWeight = LineWeightByBlock
	h.ExternalReferenceClippingBoundaryType = XClipFrameTypeDisplayNotPlot
	h.FacetResolution = 0.5
	h.FillMode = true
	h.FingerPrintGuid = newGUID()
	h.HandleSeed = 0x01
	h.InsUnits = UnitsTypeUnitless
	h.InterfereColor = ColorIndex(1)
	h.LastSavedBy = "ACadSharp"
	h.Latitude = 37.7950
	h.LimitCheckingOn = false
	h.LinearUnitFormat = LinearUnitFormatDecimal
	h.LinearUnitPrecision = 4
	h.LineTypeScale = 1.0
	h.Longitude = -122.394
	h.MaxViewportCount = 64
	h.MeasurementUnits = MeasurementUnitsMetric
	h.MenuFileName = "."
	h.MirrorText = false
	h.ModelSpaceXAxis = AxisX
	h.ModelSpaceYAxis = AxisY
	h.NumberOfSplineSegments = 8
	h.ObjectSnapMode = ObjectSnapMode(4133)
	h.OrthoMode = false
	h.PaperSpaceLimitsChecking = false
	h.PaperSpaceLineTypeScaling = SpaceLineTypeScalingNormal
	h.PaperSpaceUcsXAxis = AxisX
	h.PaperSpaceUcsYAxis = AxisY
	h.PolylineLineTypeGeneration = false
	h.ProxyGraphics = true
	h.QuickTextMode = false
	h.RegenerationMode = true
	h.RetainXRefDependentVisibilitySettings = true
	h.ShadeDiffuseToAmbientPercentage = 70
	h.ShadeEdge = ShadeEdgeTypeFacesInEntityColorEdgesInBlack
	h.ShowModelSpace = true
	h.ShowSplineControlPoints = false
	h.SketchIncrement = 1.0
	h.SketchPolylines = false
	h.SpatialIndexMaxTreeDepth = 3020
	h.SplineType = SplineTypeCubicBSpline
	h.StackedTextAlignment = 1
	h.StackedTextSizePercentage = 70
	h.StepSize = 6.0
	h.StepsPerSecond = 2.0
	h.SurfaceDensityU = 6
	h.SurfaceDensityV = 6
	h.SurfaceIsolineCount = 4
	h.SurfaceMeshTabulationCount1 = 6
	h.SurfaceMeshTabulationCount2 = 6
	h.SurfaceType = 6
	h.TextHeightDefault = 2.5
	h.TextQuality = 50
	h.UniversalCreateDateTime = time.Now().UTC()
	h.UniversalUpdateDateTime = time.Now().UTC()
	h.UpdateDateTime = time.Now()
	h.UpdateDimensionsWhileDragging = true
	h.UserTimer = false
	h.VersionGuid = newGUID()
	h.WorldView = true
	h.MaintenanceVersion = AC1032.MaintenanceVersion()
	h.FingerPrintGuid = newGUID()
	h.VersionGuid = newGUID()
	return h
}

var _ = math.Pi

// HeaderVariables lists every header variable with its DXF group codes, in
// ACadSharp's order. Get returns one DXF value per code (string, float64,
// int16, int32, int64, bool or uint64 handle); Set takes the values read.
var HeaderVariables = []HeaderVariable{
	{Name: "$ACADVER", Codes: []int{1}, Field: "Version", Get: func(h *Header) []any { return []any{h.Version.String()} }, Set: func(h *Header, v []any) { h.Version = ParseVersion(asString(v[0])) }},
	{Name: "$ANGBASE", Codes: []int{50}, Field: "AngleBase", Get: func(h *Header) []any { return []any{h.AngleBase} }, Set: func(h *Header, v []any) { h.AngleBase = asFloat(v[0]) }},
	{Name: "$ANGDIR", Codes: []int{70}, Field: "AngularDirection", Get: func(h *Header) []any { return []any{int16(h.AngularDirection)} }, Set: func(h *Header, v []any) { h.AngularDirection = AngularDirection(asInt(v[0])) }},
	{Name: "$AUNITS", Codes: []int{70}, Field: "AngularUnit", Get: func(h *Header) []any { return []any{int16(h.AngularUnit)} }, Set: func(h *Header, v []any) { h.AngularUnit = AngularUnitFormat(asInt(v[0])) }},
	{Name: "$AUPREC", Codes: []int{70}, Field: "AngularUnitPrecision", Get: func(h *Header) []any { return []any{int16(h.AngularUnitPrecision)} }, Set: func(h *Header, v []any) { h.AngularUnitPrecision = int16(asInt(v[0])) }},
	{Name: "$DIMLDRBLK", Codes: []int{1}, Field: "ArrowBlockName", Get: func(h *Header) []any { return []any{h.ArrowBlockName} }, Set: func(h *Header, v []any) { h.ArrowBlockName = asString(v[0]) }},
	{Name: "$DIMASO", Codes: []int{70}, Field: "AssociatedDimensions", Get: func(h *Header) []any { return []any{b2i16(h.AssociatedDimensions)} }, Set: func(h *Header, v []any) { h.AssociatedDimensions = asBool(v[0]) }},
	{Name: "$ATTMODE", Codes: []int{70}, Field: "AttributeVisibility", Get: func(h *Header) []any { return []any{int16(h.AttributeVisibility)} }, Set: func(h *Header, v []any) { h.AttributeVisibility = AttributeVisibilityMode(asInt(v[0])) }},
	{Name: "$BLIPMODE", Codes: []int{70}, Field: "BlipMode", Get: func(h *Header) []any { return []any{b2i16(h.BlipMode)} }, Set: func(h *Header, v []any) { h.BlipMode = asBool(v[0]) }},
	{Name: "$CAMERADISPLAY", Codes: []int{290}, Field: "CameraDisplayObjects", Get: func(h *Header) []any { return []any{h.CameraDisplayObjects} }, Set: func(h *Header, v []any) { h.CameraDisplayObjects = asBool(v[0]) }},
	{Name: "$CAMERAHEIGHT", Codes: []int{40}, Field: "CameraHeight", Get: func(h *Header) []any { return []any{h.CameraHeight} }, Set: func(h *Header, v []any) { h.CameraHeight = asFloat(v[0]) }},
	{Name: "$CHAMFERD", Codes: []int{40}, Field: "ChamferAngle", Get: func(h *Header) []any { return []any{h.ChamferAngle} }, Set: func(h *Header, v []any) { h.ChamferAngle = asFloat(v[0]) }},
	{Name: "$CHAMFERA", Codes: []int{40}, Field: "ChamferDistance1", Get: func(h *Header) []any { return []any{h.ChamferDistance1} }, Set: func(h *Header, v []any) { h.ChamferDistance1 = asFloat(v[0]) }},
	{Name: "$CHAMFERB", Codes: []int{40}, Field: "ChamferDistance2", Get: func(h *Header) []any { return []any{h.ChamferDistance2} }, Set: func(h *Header, v []any) { h.ChamferDistance2 = asFloat(v[0]) }},
	{Name: "$CHAMFERC", Codes: []int{40}, Field: "ChamferLength", Get: func(h *Header) []any { return []any{h.ChamferLength} }, Set: func(h *Header, v []any) { h.ChamferLength = asFloat(v[0]) }},
	{Name: "$DWGCODEPAGE", Codes: []int{3}, Field: "CodePage", Get: func(h *Header) []any { return []any{h.CodePage} }, Set: func(h *Header, v []any) { h.CodePage = asString(v[0]) }},
	{Name: "$TDCREATE", Codes: []int{40}, Field: "CreateDateTime", Get: func(h *Header) []any { return []any{ToJulianDate(h.CreateDateTime)} }, Set: func(h *Header, v []any) { h.CreateDateTime = FromJulianDate(asFloat(v[0])) }},
	{Name: "$PELLIPSE", Codes: []int{70}, Field: "CreateEllipseAsPolyline", Get: func(h *Header) []any { return []any{b2i16(h.CreateEllipseAsPolyline)} }, Set: func(h *Header, v []any) { h.CreateEllipseAsPolyline = asBool(v[0]) }},
	{Name: "$DIMSTYLE", Codes: []int{2}, Field: "CurrentDimensionStyleName", IsName: true, Get: func(h *Header) []any { return []any{h.CurrentDimensionStyleName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.CurrentDimensionStyleName = s
		}
	}},
	{Name: "$CECOLOR", Codes: []int{62}, Field: "CurrentEntityColor", Get: func(h *Header) []any { return []any{h.CurrentEntityColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.CurrentEntityColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$CELTSCALE", Codes: []int{40}, Field: "CurrentEntityLinetypeScale", Get: func(h *Header) []any { return []any{h.CurrentEntityLinetypeScale} }, Set: func(h *Header, v []any) { h.CurrentEntityLinetypeScale = asFloat(v[0]) }},
	{Name: "$CELWEIGHT", Codes: []int{370}, Field: "CurrentEntityLineWeight", Get: func(h *Header) []any { return []any{int16(h.CurrentEntityLineWeight)} }, Set: func(h *Header, v []any) { h.CurrentEntityLineWeight = LineWeight(asInt(v[0])) }},
	{Name: "$CEPSNTYPE", Codes: []int{380}, Field: "CurrentEntityPlotStyle", Get: func(h *Header) []any { return []any{int16(h.CurrentEntityPlotStyle)} }, Set: func(h *Header, v []any) { h.CurrentEntityPlotStyle = EntityPlotStyleType(asInt(v[0])) }},
	{Name: "$CLAYER", Codes: []int{8}, Field: "CurrentLayerName", IsName: true, Get: func(h *Header) []any { return []any{h.CurrentLayerName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.CurrentLayerName = s
		}
	}},
	{Name: "$CELTYPE", Codes: []int{6}, Field: "CurrentLineTypeName", IsName: true, Get: func(h *Header) []any { return []any{h.CurrentLineTypeName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.CurrentLineTypeName = s
		}
	}},
	{Name: "$CMLSTYLE", Codes: []int{2}, Field: "CurrentMLineStyleName", IsName: true, Get: func(h *Header) []any { return []any{h.CurrentMLineStyleName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.CurrentMLineStyleName = s
		}
	}},
	{Name: "$CMLJUST", Codes: []int{70}, Field: "CurrentMultiLineJustification", Get: func(h *Header) []any { return []any{int16(h.CurrentMultiLineJustification)} }, Set: func(h *Header, v []any) { h.CurrentMultiLineJustification = VerticalAlignmentType(asInt(v[0])) }},
	{Name: "$CMLSCALE", Codes: []int{40}, Field: "CurrentMultilineScale", Get: func(h *Header) []any { return []any{h.CurrentMultilineScale} }, Set: func(h *Header, v []any) { h.CurrentMultilineScale = asFloat(v[0]) }},
	{Name: "$TEXTSTYLE", Codes: []int{7}, Field: "CurrentTextStyleName", IsName: true, Get: func(h *Header) []any { return []any{h.CurrentTextStyleName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.CurrentTextStyleName = s
		}
	}},
	{Name: "$DGNFRAME", Codes: []int{280}, Field: "DgnUnderlayFramesVisibility", Get: func(h *Header) []any { return []any{int16(h.DgnUnderlayFramesVisibility)} }, Set: func(h *Header, v []any) { h.DgnUnderlayFramesVisibility = uint8(asInt(v[0])) }},
	{Name: "$DIMAPOST", Codes: []int{1}, Field: "DimensionAlternateDimensioningSuffix", Get: func(h *Header) []any { return []any{h.DimensionAlternateDimensioningSuffix} }, Set: func(h *Header, v []any) { h.DimensionAlternateDimensioningSuffix = asString(v[0]) }},
	{Name: "$DIMALTD", Codes: []int{70}, Field: "DimensionAlternateUnitDecimalPlaces", Get: func(h *Header) []any { return []any{int16(h.DimensionAlternateUnitDecimalPlaces)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitDecimalPlaces = int16(asInt(v[0])) }},
	{Name: "$DIMALT", Codes: []int{70}, Field: "DimensionAlternateUnitDimensioning", Get: func(h *Header) []any { return []any{b2i16(h.DimensionAlternateUnitDimensioning)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitDimensioning = asBool(v[0]) }},
	{Name: "$DIMALTU", Codes: []int{70}, Field: "DimensionAlternateUnitFormat", Get: func(h *Header) []any { return []any{int16(h.DimensionAlternateUnitFormat)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitFormat = LinearUnitFormat(asInt(v[0])) }},
	{Name: "$DIMALTRND", Codes: []int{40}, Field: "DimensionAlternateUnitRounding", Get: func(h *Header) []any { return []any{h.DimensionAlternateUnitRounding} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitRounding = asFloat(v[0]) }},
	{Name: "$DIMALTF", Codes: []int{40}, Field: "DimensionAlternateUnitScaleFactor", Get: func(h *Header) []any { return []any{h.DimensionAlternateUnitScaleFactor} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitScaleFactor = asFloat(v[0]) }},
	{Name: "$DIMALTTD", Codes: []int{70}, Field: "DimensionAlternateUnitToleranceDecimalPlaces", Get: func(h *Header) []any { return []any{int16(h.DimensionAlternateUnitToleranceDecimalPlaces)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitToleranceDecimalPlaces = int16(asInt(v[0])) }},
	{Name: "$DIMALTTZ", Codes: []int{70}, Field: "DimensionAlternateUnitToleranceZeroHandling", Get: func(h *Header) []any { return []any{int16(h.DimensionAlternateUnitToleranceZeroHandling)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitToleranceZeroHandling = ZeroHandling(asInt(v[0])) }},
	{Name: "$DIMALTZ", Codes: []int{70}, Field: "DimensionAlternateUnitZeroHandling", Get: func(h *Header) []any { return []any{int16(h.DimensionAlternateUnitZeroHandling)} }, Set: func(h *Header, v []any) { h.DimensionAlternateUnitZeroHandling = ZeroHandling(asInt(v[0])) }},
	{Name: "$DIMALTMZF", Codes: []int{40}, Field: "DimensionAltMzf", Get: func(h *Header) []any { return []any{h.DimensionAltMzf} }, Set: func(h *Header, v []any) { h.DimensionAltMzf = asFloat(v[0]) }},
	{Name: "$DIMALTMZS", Codes: []int{6}, Field: "DimensionAltMzs", Get: func(h *Header) []any { return []any{h.DimensionAltMzs} }, Set: func(h *Header, v []any) { h.DimensionAltMzs = asString(v[0]) }},
	{Name: "$DIMADEC", Codes: []int{70}, Field: "DimensionAngularDimensionDecimalPlaces", Get: func(h *Header) []any { return []any{int16(h.DimensionAngularDimensionDecimalPlaces)} }, Set: func(h *Header, v []any) { h.DimensionAngularDimensionDecimalPlaces = int16(asInt(v[0])) }},
	{Name: "$DIMAUNIT", Codes: []int{70}, Field: "DimensionAngularUnit", Get: func(h *Header) []any { return []any{int16(h.DimensionAngularUnit)} }, Set: func(h *Header, v []any) { h.DimensionAngularUnit = AngularUnitFormat(asInt(v[0])) }},
	{Name: "$DIMAZIN", Codes: []int{70}, Field: "DimensionAngularZeroHandling", Get: func(h *Header) []any { return []any{int16(h.DimensionAngularZeroHandling)} }, Set: func(h *Header, v []any) { h.DimensionAngularZeroHandling = AngularZeroHandling(asInt(v[0])) }},
	{Name: "$DIMARCSYM", Codes: []int{70}, Field: "DimensionArcLengthSymbolPosition", Get: func(h *Header) []any { return []any{int16(h.DimensionArcLengthSymbolPosition)} }, Set: func(h *Header, v []any) { h.DimensionArcLengthSymbolPosition = ArcLengthSymbolPosition(asInt(v[0])) }},
	{Name: "$DIMASZ", Codes: []int{40}, Field: "DimensionArrowSize", Get: func(h *Header) []any { return []any{h.DimensionArrowSize} }, Set: func(h *Header, v []any) { h.DimensionArrowSize = asFloat(v[0]) }},
	{Name: "$DIMASSOC", Codes: []int{280}, Field: "DimensionAssociativity", Get: func(h *Header) []any { return []any{int16(h.DimensionAssociativity)} }, Set: func(h *Header, v []any) { h.DimensionAssociativity = HeaderDimensionAssociation(asInt(v[0])) }},
	{Name: "$DIMBLK", Codes: []int{1}, Field: "DimensionBlockName", Get: func(h *Header) []any { return []any{h.DimensionBlockName} }, Set: func(h *Header, v []any) { h.DimensionBlockName = asString(v[0]) }},
	{Name: "$DIMBLK1", Codes: []int{1}, Field: "DimensionBlockNameFirst", Get: func(h *Header) []any { return []any{h.DimensionBlockNameFirst} }, Set: func(h *Header, v []any) { h.DimensionBlockNameFirst = asString(v[0]) }},
	{Name: "$DIMBLK2", Codes: []int{1}, Field: "DimensionBlockNameSecond", Get: func(h *Header) []any { return []any{h.DimensionBlockNameSecond} }, Set: func(h *Header, v []any) { h.DimensionBlockNameSecond = asString(v[0]) }},
	{Name: "$DIMCEN", Codes: []int{40}, Field: "DimensionCenterMarkSize", Get: func(h *Header) []any { return []any{h.DimensionCenterMarkSize} }, Set: func(h *Header, v []any) { h.DimensionCenterMarkSize = asFloat(v[0]) }},
	{Name: "$DIMUPT", Codes: []int{70}, Field: "DimensionCursorUpdate", Get: func(h *Header) []any { return []any{b2i16(h.DimensionCursorUpdate)} }, Set: func(h *Header, v []any) { h.DimensionCursorUpdate = asBool(v[0]) }},
	{Name: "$DIMDEC", Codes: []int{70}, Field: "DimensionDecimalPlaces", Get: func(h *Header) []any { return []any{int16(h.DimensionDecimalPlaces)} }, Set: func(h *Header, v []any) { h.DimensionDecimalPlaces = int16(asInt(v[0])) }},
	{Name: "$DIMDSEP", Codes: []int{70}, Field: "DimensionDecimalSeparator", Get: func(h *Header) []any { return []any{int16(h.DimensionDecimalSeparator)} }, Set: func(h *Header, v []any) { h.DimensionDecimalSeparator = rune(asInt(v[0])) }},
	{Name: "$DIMATFIT", Codes: []int{70}, Field: "DimensionDimensionTextArrowFit", Get: func(h *Header) []any { return []any{int16(h.DimensionDimensionTextArrowFit)} }, Set: func(h *Header, v []any) { h.DimensionDimensionTextArrowFit = TextArrowFitType(asInt(v[0])) }},
	{Name: "$DIMCLRE", Codes: []int{70}, Field: "DimensionExtensionLineColor", Get: func(h *Header) []any { return []any{h.DimensionExtensionLineColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.DimensionExtensionLineColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$DIMEXE", Codes: []int{40}, Field: "DimensionExtensionLineExtension", Get: func(h *Header) []any { return []any{h.DimensionExtensionLineExtension} }, Set: func(h *Header, v []any) { h.DimensionExtensionLineExtension = asFloat(v[0]) }},
	{Name: "$DIMEXO", Codes: []int{40}, Field: "DimensionExtensionLineOffset", Get: func(h *Header) []any { return []any{h.DimensionExtensionLineOffset} }, Set: func(h *Header, v []any) { h.DimensionExtensionLineOffset = asFloat(v[0]) }},
	{Name: "$DIMFIT", Codes: []int{70}, Field: "DimensionFit", Get: func(h *Header) []any { return []any{int16(h.DimensionFit)} }, Set: func(h *Header, v []any) { h.DimensionFit = int16(asInt(v[0])) }},
	{Name: "$DIMFXL", Codes: []int{40}, Field: "DimensionFixedExtensionLineLength", Get: func(h *Header) []any { return []any{h.DimensionFixedExtensionLineLength} }, Set: func(h *Header, v []any) { h.DimensionFixedExtensionLineLength = asFloat(v[0]) }},
	{Name: "$DIMFRAC", Codes: []int{70}, Field: "DimensionFractionFormat", Get: func(h *Header) []any { return []any{int16(h.DimensionFractionFormat)} }, Set: func(h *Header, v []any) { h.DimensionFractionFormat = FractionFormat(asInt(v[0])) }},
	{Name: "$DIMTOL", Codes: []int{70}, Field: "DimensionGenerateTolerances", Get: func(h *Header) []any { return []any{b2i16(h.DimensionGenerateTolerances)} }, Set: func(h *Header, v []any) { h.DimensionGenerateTolerances = asBool(v[0]) }},
	{Name: "$DIMFXLON", Codes: []int{70}, Field: "DimensionIsExtensionLineLengthFixed", Get: func(h *Header) []any { return []any{b2i16(h.DimensionIsExtensionLineLengthFixed)} }, Set: func(h *Header, v []any) { h.DimensionIsExtensionLineLengthFixed = asBool(v[0]) }},
	{Name: "$DIMJOGANG", Codes: []int{40}, Field: "DimensionJoggedRadiusDimensionTransverseSegmentAngle", Get: func(h *Header) []any { return []any{h.DimensionJoggedRadiusDimensionTransverseSegmentAngle} }, Set: func(h *Header, v []any) { h.DimensionJoggedRadiusDimensionTransverseSegmentAngle = asFloat(v[0]) }},
	{Name: "$DIMLIM", Codes: []int{70}, Field: "DimensionLimitsGeneration", Get: func(h *Header) []any { return []any{b2i16(h.DimensionLimitsGeneration)} }, Set: func(h *Header, v []any) { h.DimensionLimitsGeneration = asBool(v[0]) }},
	{Name: "$DIMLFAC", Codes: []int{40}, Field: "DimensionLinearScaleFactor", Get: func(h *Header) []any { return []any{h.DimensionLinearScaleFactor} }, Set: func(h *Header, v []any) { h.DimensionLinearScaleFactor = asFloat(v[0]) }},
	{Name: "$DIMLUNIT", Codes: []int{70}, Field: "DimensionLinearUnitFormat", Get: func(h *Header) []any { return []any{int16(h.DimensionLinearUnitFormat)} }, Set: func(h *Header, v []any) { h.DimensionLinearUnitFormat = LinearUnitFormat(asInt(v[0])) }},
	{Name: "$DIMCLRD", Codes: []int{70}, Field: "DimensionLineColor", Get: func(h *Header) []any { return []any{h.DimensionLineColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.DimensionLineColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$DIMDLE", Codes: []int{40}, Field: "DimensionLineExtension", Get: func(h *Header) []any { return []any{h.DimensionLineExtension} }, Set: func(h *Header, v []any) { h.DimensionLineExtension = asFloat(v[0]) }},
	{Name: "$DIMGAP", Codes: []int{40}, Field: "DimensionLineGap", Get: func(h *Header) []any { return []any{h.DimensionLineGap} }, Set: func(h *Header, v []any) { h.DimensionLineGap = asFloat(v[0]) }},
	{Name: "$DIMDLI", Codes: []int{40}, Field: "DimensionLineIncrement", Get: func(h *Header) []any { return []any{h.DimensionLineIncrement} }, Set: func(h *Header, v []any) { h.DimensionLineIncrement = asFloat(v[0]) }},
	{Name: "$DIMLTYPE", Codes: []int{6}, Field: "DimensionLineType", Get: func(h *Header) []any { return []any{h.DimensionLineType} }, Set: func(h *Header, v []any) { h.DimensionLineType = asString(v[0]) }},
	{Name: "$DIMLWD", Codes: []int{70}, Field: "DimensionLineWeight", Get: func(h *Header) []any { return []any{int16(h.DimensionLineWeight)} }, Set: func(h *Header, v []any) { h.DimensionLineWeight = LineWeight(asInt(v[0])) }},
	{Name: "$DIMTM", Codes: []int{40}, Field: "DimensionMinusTolerance", Get: func(h *Header) []any { return []any{h.DimensionMinusTolerance} }, Set: func(h *Header, v []any) { h.DimensionMinusTolerance = asFloat(v[0]) }},
	{Name: "$DIMMZF", Codes: []int{40}, Field: "DimensionMzf", Get: func(h *Header) []any { return []any{h.DimensionMzf} }, Set: func(h *Header, v []any) { h.DimensionMzf = asFloat(v[0]) }},
	{Name: "$DIMMZS", Codes: []int{6}, Field: "DimensionMzs", Get: func(h *Header) []any { return []any{h.DimensionMzs} }, Set: func(h *Header, v []any) { h.DimensionMzs = asString(v[0]) }},
	{Name: "$DIMTP", Codes: []int{40}, Field: "DimensionPlusTolerance", Get: func(h *Header) []any { return []any{h.DimensionPlusTolerance} }, Set: func(h *Header, v []any) { h.DimensionPlusTolerance = asFloat(v[0]) }},
	{Name: "$DIMPOST", Codes: []int{1}, Field: "DimensionPostFix", Get: func(h *Header) []any { return []any{h.DimensionPostFix} }, Set: func(h *Header, v []any) { h.DimensionPostFix = asString(v[0]) }},
	{Name: "$DIMRND", Codes: []int{40}, Field: "DimensionRounding", Get: func(h *Header) []any { return []any{h.DimensionRounding} }, Set: func(h *Header, v []any) { h.DimensionRounding = asFloat(v[0]) }},
	{Name: "$DIMSCALE", Codes: []int{40}, Field: "DimensionScaleFactor", Get: func(h *Header) []any { return []any{h.DimensionScaleFactor} }, Set: func(h *Header, v []any) { h.DimensionScaleFactor = asFloat(v[0]) }},
	{Name: "$DIMSAH", Codes: []int{70}, Field: "DimensionSeparateArrowBlocks", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSeparateArrowBlocks)} }, Set: func(h *Header, v []any) { h.DimensionSeparateArrowBlocks = asBool(v[0]) }},
	{Name: "$DIMSD1", Codes: []int{70}, Field: "DimensionSuppressFirstDimensionLine", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSuppressFirstDimensionLine)} }, Set: func(h *Header, v []any) { h.DimensionSuppressFirstDimensionLine = asBool(v[0]) }},
	{Name: "$DIMSE1", Codes: []int{70}, Field: "DimensionSuppressFirstExtensionLine", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSuppressFirstExtensionLine)} }, Set: func(h *Header, v []any) { h.DimensionSuppressFirstExtensionLine = asBool(v[0]) }},
	{Name: "$DIMSOXD", Codes: []int{70}, Field: "DimensionSuppressOutsideExtensions", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSuppressOutsideExtensions)} }, Set: func(h *Header, v []any) { h.DimensionSuppressOutsideExtensions = asBool(v[0]) }},
	{Name: "$DIMSD2", Codes: []int{70}, Field: "DimensionSuppressSecondDimensionLine", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSuppressSecondDimensionLine)} }, Set: func(h *Header, v []any) { h.DimensionSuppressSecondDimensionLine = asBool(v[0]) }},
	{Name: "$DIMSE2", Codes: []int{70}, Field: "DimensionSuppressSecondExtensionLine", Get: func(h *Header) []any { return []any{b2i16(h.DimensionSuppressSecondExtensionLine)} }, Set: func(h *Header, v []any) { h.DimensionSuppressSecondExtensionLine = asBool(v[0]) }},
	{Name: "$DIMLTEX1", Codes: []int{6}, Field: "DimensionTex1", Get: func(h *Header) []any { return []any{h.DimensionTex1} }, Set: func(h *Header, v []any) { h.DimensionTex1 = asString(v[0]) }},
	{Name: "$DIMLTEX2", Codes: []int{6}, Field: "DimensionTex2", Get: func(h *Header) []any { return []any{h.DimensionTex2} }, Set: func(h *Header, v []any) { h.DimensionTex2 = asString(v[0]) }},
	{Name: "$DIMTFILLCLR", Codes: []int{62}, Field: "DimensionTextBackgroundColor", Ignored: true, Get: func(h *Header) []any { return []any{h.DimensionTextBackgroundColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.DimensionTextBackgroundColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$DIMTFILL", Codes: []int{70}, Field: "DimensionTextBackgroundFillMode", Get: func(h *Header) []any { return []any{int16(h.DimensionTextBackgroundFillMode)} }, Set: func(h *Header, v []any) {
		h.DimensionTextBackgroundFillMode = DimensionTextBackgroundFillMode(asInt(v[0]))
	}},
	{Name: "$DIMCLRT", Codes: []int{70}, Field: "DimensionTextColor", Get: func(h *Header) []any { return []any{h.DimensionTextColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.DimensionTextColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$DIMTXTDIRECTION", Codes: []int{70}, Field: "DimensionTextDirection", Get: func(h *Header) []any { return []any{int16(h.DimensionTextDirection)} }, Set: func(h *Header, v []any) { h.DimensionTextDirection = TextDirection(asInt(v[0])) }},
	{Name: "$DIMTXT", Codes: []int{40}, Field: "DimensionTextHeight", Get: func(h *Header) []any { return []any{h.DimensionTextHeight} }, Set: func(h *Header, v []any) { h.DimensionTextHeight = asFloat(v[0]) }},
	{Name: "$DIMJUST", Codes: []int{70}, Field: "DimensionTextHorizontalAlignment", Get: func(h *Header) []any { return []any{int16(h.DimensionTextHorizontalAlignment)} }, Set: func(h *Header, v []any) {
		h.DimensionTextHorizontalAlignment = DimensionTextHorizontalAlignment(asInt(v[0]))
	}},
	{Name: "$DIMTIX", Codes: []int{70}, Field: "DimensionTextInsideExtensions", Get: func(h *Header) []any { return []any{b2i16(h.DimensionTextInsideExtensions)} }, Set: func(h *Header, v []any) { h.DimensionTextInsideExtensions = asBool(v[0]) }},
	{Name: "$DIMTIH", Codes: []int{70}, Field: "DimensionTextInsideHorizontal", Get: func(h *Header) []any { return []any{b2i16(h.DimensionTextInsideHorizontal)} }, Set: func(h *Header, v []any) { h.DimensionTextInsideHorizontal = asBool(v[0]) }},
	{Name: "$DIMTMOVE", Codes: []int{70}, Field: "DimensionTextMovement", Get: func(h *Header) []any { return []any{int16(h.DimensionTextMovement)} }, Set: func(h *Header, v []any) { h.DimensionTextMovement = TextMovement(asInt(v[0])) }},
	{Name: "$DIMTOFL", Codes: []int{70}, Field: "DimensionTextOutsideExtensions", Get: func(h *Header) []any { return []any{b2i16(h.DimensionTextOutsideExtensions)} }, Set: func(h *Header, v []any) { h.DimensionTextOutsideExtensions = asBool(v[0]) }},
	{Name: "$DIMTOH", Codes: []int{70}, Field: "DimensionTextOutsideHorizontal", Get: func(h *Header) []any { return []any{b2i16(h.DimensionTextOutsideHorizontal)} }, Set: func(h *Header, v []any) { h.DimensionTextOutsideHorizontal = asBool(v[0]) }},
	{Name: "$DIMTXSTY", Codes: []int{7}, Field: "DimensionTextStyleName", IsName: true, Get: func(h *Header) []any { return []any{h.DimensionTextStyleName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.DimensionTextStyleName = s
		}
	}},
	{Name: "$DIMTAD", Codes: []int{70}, Field: "DimensionTextVerticalAlignment", Get: func(h *Header) []any { return []any{int16(h.DimensionTextVerticalAlignment)} }, Set: func(h *Header, v []any) {
		h.DimensionTextVerticalAlignment = DimensionTextVerticalAlignment(asInt(v[0]))
	}},
	{Name: "$DIMTVP", Codes: []int{40}, Field: "DimensionTextVerticalPosition", Get: func(h *Header) []any { return []any{h.DimensionTextVerticalPosition} }, Set: func(h *Header, v []any) { h.DimensionTextVerticalPosition = asFloat(v[0]) }},
	{Name: "$DIMTSZ", Codes: []int{40}, Field: "DimensionTickSize", Get: func(h *Header) []any { return []any{h.DimensionTickSize} }, Set: func(h *Header, v []any) { h.DimensionTickSize = asFloat(v[0]) }},
	{Name: "$DIMTOLJ", Codes: []int{70}, Field: "DimensionToleranceAlignment", Get: func(h *Header) []any { return []any{int16(h.DimensionToleranceAlignment)} }, Set: func(h *Header, v []any) { h.DimensionToleranceAlignment = ToleranceAlignment(asInt(v[0])) }},
	{Name: "$DIMTDEC", Codes: []int{70}, Field: "DimensionToleranceDecimalPlaces", Get: func(h *Header) []any { return []any{int16(h.DimensionToleranceDecimalPlaces)} }, Set: func(h *Header, v []any) { h.DimensionToleranceDecimalPlaces = int16(asInt(v[0])) }},
	{Name: "$DIMTFAC", Codes: []int{40}, Field: "DimensionToleranceScaleFactor", Get: func(h *Header) []any { return []any{h.DimensionToleranceScaleFactor} }, Set: func(h *Header, v []any) { h.DimensionToleranceScaleFactor = asFloat(v[0]) }},
	{Name: "$DIMTZIN", Codes: []int{70}, Field: "DimensionToleranceZeroHandling", Get: func(h *Header) []any { return []any{int16(h.DimensionToleranceZeroHandling)} }, Set: func(h *Header, v []any) { h.DimensionToleranceZeroHandling = ZeroHandling(asInt(v[0])) }},
	{Name: "$DIMUNIT", Codes: []int{70}, Field: "DimensionUnit", Get: func(h *Header) []any { return []any{int16(h.DimensionUnit)} }, Set: func(h *Header, v []any) { h.DimensionUnit = int16(asInt(v[0])) }},
	{Name: "$DIMZIN", Codes: []int{70}, Field: "DimensionZeroHandling", Get: func(h *Header) []any { return []any{int16(h.DimensionZeroHandling)} }, Set: func(h *Header, v []any) { h.DimensionZeroHandling = ZeroHandling(asInt(v[0])) }},
	{Name: "$LIGHTGLYPHDISPLAY", Codes: []int{280}, Field: "DisplayLightGlyphs", Get: func(h *Header) []any { return []any{int16(h.DisplayLightGlyphs)} }, Set: func(h *Header, v []any) { h.DisplayLightGlyphs = uint8(asInt(v[0])) }},
	{Name: "$LWDISPLAY", Codes: []int{290}, Field: "DisplayLineWeight", Get: func(h *Header) []any { return []any{h.DisplayLineWeight} }, Set: func(h *Header, v []any) { h.DisplayLineWeight = asBool(v[0]) }},
	{Name: "$DISPSILH", Codes: []int{70}, Field: "DisplaySilhouetteCurves", Get: func(h *Header) []any { return []any{b2i16(h.DisplaySilhouetteCurves)} }, Set: func(h *Header, v []any) { h.DisplaySilhouetteCurves = asBool(v[0]) }},
	{Name: "$LOFTANG1", Codes: []int{40}, Field: "DraftAngleFirstCrossSection", Get: func(h *Header) []any { return []any{h.DraftAngleFirstCrossSection} }, Set: func(h *Header, v []any) { h.DraftAngleFirstCrossSection = asFloat(v[0]) }},
	{Name: "$LOFTANG2", Codes: []int{40}, Field: "DraftAngleSecondCrossSection", Get: func(h *Header) []any { return []any{h.DraftAngleSecondCrossSection} }, Set: func(h *Header, v []any) { h.DraftAngleSecondCrossSection = asFloat(v[0]) }},
	{Name: "$LOFTMAG1", Codes: []int{40}, Field: "DraftMagnitudeFirstCrossSection", Get: func(h *Header) []any { return []any{h.DraftMagnitudeFirstCrossSection} }, Set: func(h *Header, v []any) { h.DraftMagnitudeFirstCrossSection = asFloat(v[0]) }},
	{Name: "$LOFTMAG2", Codes: []int{40}, Field: "DraftMagnitudeSecondCrossSection", Get: func(h *Header) []any { return []any{h.DraftMagnitudeSecondCrossSection} }, Set: func(h *Header, v []any) { h.DraftMagnitudeSecondCrossSection = asFloat(v[0]) }},
	{Name: "$3DDWFPREC", Codes: []int{40}, Field: "Dw3DPrecision", Get: func(h *Header) []any { return []any{h.Dw3DPrecision} }, Set: func(h *Header, v []any) { h.Dw3DPrecision = asFloat(v[0]) }},
	{Name: "$DWFFRAME", Codes: []int{280}, Field: "DwgUnderlayFramesVisibility", Get: func(h *Header) []any { return []any{int16(h.DwgUnderlayFramesVisibility)} }, Set: func(h *Header, v []any) { h.DwgUnderlayFramesVisibility = uint8(asInt(v[0])) }},
	{Name: "$ELEVATION", Codes: []int{40}, Field: "Elevation", Get: func(h *Header) []any { return []any{h.Elevation} }, Set: func(h *Header, v []any) { h.Elevation = asFloat(v[0]) }},
	{Name: "$ENDCAPS", Codes: []int{280}, Field: "EndCaps", Get: func(h *Header) []any { return []any{int16(h.EndCaps)} }, Set: func(h *Header, v []any) { h.EndCaps = int16(asInt(v[0])) }},
	{Name: "$SORTENTS", Codes: []int{280}, Field: "EntitySortingFlags", Get: func(h *Header) []any { return []any{int16(h.EntitySortingFlags)} }, Set: func(h *Header, v []any) { h.EntitySortingFlags = ObjectSortingFlags(asInt(v[0])) }},
	{Name: "$EXTNAMES", Codes: []int{290}, Field: "ExtendedNames", Get: func(h *Header) []any { return []any{h.ExtendedNames} }, Set: func(h *Header, v []any) { h.ExtendedNames = asBool(v[0]) }},
	{Name: "$DIMLWE", Codes: []int{70}, Field: "ExtensionLineWeight", Get: func(h *Header) []any { return []any{int16(h.ExtensionLineWeight)} }, Set: func(h *Header, v []any) { h.ExtensionLineWeight = LineWeight(asInt(v[0])) }},
	{Name: "$XCLIPFRAME", Codes: []int{280}, Field: "ExternalReferenceClippingBoundaryType", Get: func(h *Header) []any { return []any{int16(h.ExternalReferenceClippingBoundaryType)} }, Set: func(h *Header, v []any) { h.ExternalReferenceClippingBoundaryType = XClipFrameType(asInt(v[0])) }},
	{Name: "$FACETRES", Codes: []int{40}, Field: "FacetResolution", Get: func(h *Header) []any { return []any{h.FacetResolution} }, Set: func(h *Header, v []any) { h.FacetResolution = asFloat(v[0]) }},
	{Name: "$FILLETRAD", Codes: []int{40}, Field: "FilletRadius", Get: func(h *Header) []any { return []any{h.FilletRadius} }, Set: func(h *Header, v []any) { h.FilletRadius = asFloat(v[0]) }},
	{Name: "$FILLMODE", Codes: []int{70}, Field: "FillMode", Get: func(h *Header) []any { return []any{b2i16(h.FillMode)} }, Set: func(h *Header, v []any) { h.FillMode = asBool(v[0]) }},
	{Name: "$FINGERPRINTGUID", Codes: []int{2}, Field: "FingerPrintGuid", Get: func(h *Header) []any { return []any{h.FingerPrintGuid} }, Set: func(h *Header, v []any) { h.FingerPrintGuid = asString(v[0]) }},
	{Name: "$HALOGAP", Codes: []int{280}, Field: "HaloGapPercentage", Get: func(h *Header) []any { return []any{int16(h.HaloGapPercentage)} }, Set: func(h *Header, v []any) { h.HaloGapPercentage = uint8(asInt(v[0])) }},
	{Name: "$HANDSEED", Codes: []int{5}, Field: "HandleSeed", Get: func(h *Header) []any { return []any{h.HandleSeed} }, Set: func(h *Header, v []any) { h.HandleSeed = asHandle(v[0]) }},
	{Name: "$HIDETEXT", Codes: []int{280}, Field: "HideText", Get: func(h *Header) []any { return []any{int16(h.HideText)} }, Set: func(h *Header, v []any) { h.HideText = uint8(asInt(v[0])) }},
	{Name: "$HYPERLINKBASE", Codes: []int{1}, Field: "HyperLinkBase", Get: func(h *Header) []any { return []any{h.HyperLinkBase} }, Set: func(h *Header, v []any) { h.HyperLinkBase = asString(v[0]) }},
	{Name: "$INDEXCTL", Codes: []int{280}, Field: "IndexCreationFlags", Get: func(h *Header) []any { return []any{int16(h.IndexCreationFlags)} }, Set: func(h *Header, v []any) { h.IndexCreationFlags = IndexCreationFlags(asInt(v[0])) }},
	{Name: "$INSUNITS", Codes: []int{70}, Field: "InsUnits", Get: func(h *Header) []any { return []any{int16(h.InsUnits)} }, Set: func(h *Header, v []any) { h.InsUnits = UnitsType(asInt(v[0])) }},
	{Name: "$INTERFERECOLOR", Codes: []int{62}, Field: "InterfereColor", Get: func(h *Header) []any { return []any{h.InterfereColor.ApproxIndex()} }, Set: func(h *Header, v []any) { h.InterfereColor = ColorIndex(int16(asInt(v[0]))) }},
	{Name: "$JOINSTYLE", Codes: []int{280}, Field: "JoinStyle", Get: func(h *Header) []any { return []any{int16(h.JoinStyle)} }, Set: func(h *Header, v []any) { h.JoinStyle = int16(asInt(v[0])) }},
	{Name: "$LASTSAVEDBY", Codes: []int{3}, Field: "LastSavedBy", Ignored: true, Get: func(h *Header) []any { return []any{h.LastSavedBy} }, Set: func(h *Header, v []any) { h.LastSavedBy = asString(v[0]) }},
	{Name: "$LATITUDE", Codes: []int{40}, Field: "Latitude", Get: func(h *Header) []any { return []any{h.Latitude} }, Set: func(h *Header, v []any) { h.Latitude = asFloat(v[0]) }},
	{Name: "$LENSLENGTH", Codes: []int{40}, Field: "LensLength", Get: func(h *Header) []any { return []any{h.LensLength} }, Set: func(h *Header, v []any) { h.LensLength = asFloat(v[0]) }},
	{Name: "$LIMCHECK", Codes: []int{70}, Field: "LimitCheckingOn", Get: func(h *Header) []any { return []any{b2i16(h.LimitCheckingOn)} }, Set: func(h *Header, v []any) { h.LimitCheckingOn = asBool(v[0]) }},
	{Name: "$LUNITS", Codes: []int{70}, Field: "LinearUnitFormat", Get: func(h *Header) []any { return []any{int16(h.LinearUnitFormat)} }, Set: func(h *Header, v []any) { h.LinearUnitFormat = LinearUnitFormat(asInt(v[0])) }},
	{Name: "$LUPREC", Codes: []int{70}, Field: "LinearUnitPrecision", Get: func(h *Header) []any { return []any{int16(h.LinearUnitPrecision)} }, Set: func(h *Header, v []any) { h.LinearUnitPrecision = int16(asInt(v[0])) }},
	{Name: "$LTSCALE", Codes: []int{40}, Field: "LineTypeScale", Get: func(h *Header) []any { return []any{h.LineTypeScale} }, Set: func(h *Header, v []any) { h.LineTypeScale = asFloat(v[0]) }},
	{Name: "$LOFTNORMALS", Codes: []int{280}, Field: "LoftedObjectNormals", Get: func(h *Header) []any { return []any{int16(h.LoftedObjectNormals)} }, Set: func(h *Header, v []any) { h.LoftedObjectNormals = uint8(asInt(v[0])) }},
	{Name: "$LONGITUDE", Codes: []int{40}, Field: "Longitude", Get: func(h *Header) []any { return []any{h.Longitude} }, Set: func(h *Header, v []any) { h.Longitude = asFloat(v[0]) }},
	{Name: "$ACADMAINTVER", Codes: []int{70}, Field: "MaintenanceVersion", Ignored: true, Get: func(h *Header) []any { return []any{int16(h.MaintenanceVersion)} }, Set: func(h *Header, v []any) { h.MaintenanceVersion = int16(asInt(v[0])) }},
	{Name: "$MAXACTVP", Codes: []int{70}, Field: "MaxViewportCount", Get: func(h *Header) []any { return []any{int16(h.MaxViewportCount)} }, Set: func(h *Header, v []any) { h.MaxViewportCount = int16(asInt(v[0])) }},
	{Name: "$MEASUREMENT", Codes: []int{70}, Field: "MeasurementUnits", Get: func(h *Header) []any { return []any{int16(h.MeasurementUnits)} }, Set: func(h *Header, v []any) { h.MeasurementUnits = MeasurementUnits(asInt(v[0])) }},
	{Name: "$MENU", Codes: []int{1}, Field: "MenuFileName", Get: func(h *Header) []any { return []any{h.MenuFileName} }, Set: func(h *Header, v []any) { h.MenuFileName = asString(v[0]) }},
	{Name: "$MIRRTEXT", Codes: []int{70}, Field: "MirrorText", Get: func(h *Header) []any { return []any{b2i16(h.MirrorText)} }, Set: func(h *Header, v []any) { h.MirrorText = asBool(v[0]) }},
	{Name: "$EXTMAX", Codes: []int{10, 20, 30}, Field: "ModelSpaceExtMax", Get: func(h *Header) []any { return []any{h.ModelSpaceExtMax.X, h.ModelSpaceExtMax.Y, h.ModelSpaceExtMax.Z} }, Set: func(h *Header, v []any) { h.ModelSpaceExtMax = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$EXTMIN", Codes: []int{10, 20, 30}, Field: "ModelSpaceExtMin", Get: func(h *Header) []any { return []any{h.ModelSpaceExtMin.X, h.ModelSpaceExtMin.Y, h.ModelSpaceExtMin.Z} }, Set: func(h *Header, v []any) { h.ModelSpaceExtMin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$INSBASE", Codes: []int{10, 20, 30}, Field: "ModelSpaceInsertionBase", Get: func(h *Header) []any {
		return []any{h.ModelSpaceInsertionBase.X, h.ModelSpaceInsertionBase.Y, h.ModelSpaceInsertionBase.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceInsertionBase = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$LIMMAX", Codes: []int{10, 20}, Field: "ModelSpaceLimitsMax", Get: func(h *Header) []any { return []any{h.ModelSpaceLimitsMax.X, h.ModelSpaceLimitsMax.Y} }, Set: func(h *Header, v []any) { h.ModelSpaceLimitsMax = Vec2{asFloat(v[0]), asFloat(v[1])} }},
	{Name: "$LIMMIN", Codes: []int{10, 20}, Field: "ModelSpaceLimitsMin", Get: func(h *Header) []any { return []any{h.ModelSpaceLimitsMin.X, h.ModelSpaceLimitsMin.Y} }, Set: func(h *Header, v []any) { h.ModelSpaceLimitsMin = Vec2{asFloat(v[0]), asFloat(v[1])} }},
	{Name: "$UCSORG", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrigin", Get: func(h *Header) []any { return []any{h.ModelSpaceOrigin.X, h.ModelSpaceOrigin.Y, h.ModelSpaceOrigin.Z} }, Set: func(h *Header, v []any) { h.ModelSpaceOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$UCSORGBACK", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicBackDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicBackDOrigin.X, h.ModelSpaceOrthographicBackDOrigin.Y, h.ModelSpaceOrthographicBackDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicBackDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSORGBOTTOM", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicBottomDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicBottomDOrigin.X, h.ModelSpaceOrthographicBottomDOrigin.Y, h.ModelSpaceOrthographicBottomDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicBottomDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSORGFRONT", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicFrontDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicFrontDOrigin.X, h.ModelSpaceOrthographicFrontDOrigin.Y, h.ModelSpaceOrthographicFrontDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicFrontDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSORGLEFT", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicLeftDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicLeftDOrigin.X, h.ModelSpaceOrthographicLeftDOrigin.Y, h.ModelSpaceOrthographicLeftDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicLeftDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSORGRIGHT", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicRightDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicRightDOrigin.X, h.ModelSpaceOrthographicRightDOrigin.Y, h.ModelSpaceOrthographicRightDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicRightDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSORGTOP", Codes: []int{10, 20, 30}, Field: "ModelSpaceOrthographicTopDOrigin", Get: func(h *Header) []any {
		return []any{h.ModelSpaceOrthographicTopDOrigin.X, h.ModelSpaceOrthographicTopDOrigin.Y, h.ModelSpaceOrthographicTopDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.ModelSpaceOrthographicTopDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$UCSXDIR", Codes: []int{10, 20, 30}, Field: "ModelSpaceXAxis", Get: func(h *Header) []any { return []any{h.ModelSpaceXAxis.X, h.ModelSpaceXAxis.Y, h.ModelSpaceXAxis.Z} }, Set: func(h *Header, v []any) { h.ModelSpaceXAxis = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$UCSYDIR", Codes: []int{10, 20, 30}, Field: "ModelSpaceYAxis", Get: func(h *Header) []any { return []any{h.ModelSpaceYAxis.X, h.ModelSpaceYAxis.Y, h.ModelSpaceYAxis.Z} }, Set: func(h *Header, v []any) { h.ModelSpaceYAxis = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$NORTHDIRECTION", Codes: []int{40}, Field: "NorthDirection", Get: func(h *Header) []any { return []any{h.NorthDirection} }, Set: func(h *Header, v []any) { h.NorthDirection = asFloat(v[0]) }},
	{Name: "$SPLINESEGS", Codes: []int{70}, Field: "NumberOfSplineSegments", Get: func(h *Header) []any { return []any{int16(h.NumberOfSplineSegments)} }, Set: func(h *Header, v []any) { h.NumberOfSplineSegments = int16(asInt(v[0])) }},
	{Name: "$OSMODE", Codes: []int{70}, Field: "ObjectSnapMode", Get: func(h *Header) []any { return []any{int16(h.ObjectSnapMode)} }, Set: func(h *Header, v []any) { h.ObjectSnapMode = ObjectSnapMode(asInt(v[0])) }},
	{Name: "$ORTHOMODE", Codes: []int{70}, Field: "OrthoMode", Get: func(h *Header) []any { return []any{b2i16(h.OrthoMode)} }, Set: func(h *Header, v []any) { h.OrthoMode = asBool(v[0]) }},
	{Name: "$PUCSBASE", Codes: []int{2}, Field: "PaperSpaceBaseName", IsName: true, Get: func(h *Header) []any { return []any{h.PaperSpaceBaseName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.PaperSpaceBaseName = s
		}
	}},
	{Name: "$PELEVATION", Codes: []int{40}, Field: "PaperSpaceElevation", Get: func(h *Header) []any { return []any{h.PaperSpaceElevation} }, Set: func(h *Header, v []any) { h.PaperSpaceElevation = asFloat(v[0]) }},
	{Name: "$PEXTMAX", Codes: []int{10, 20, 30}, Field: "PaperSpaceExtMax", Get: func(h *Header) []any { return []any{h.PaperSpaceExtMax.X, h.PaperSpaceExtMax.Y, h.PaperSpaceExtMax.Z} }, Set: func(h *Header, v []any) { h.PaperSpaceExtMax = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$PEXTMIN", Codes: []int{10, 20, 30}, Field: "PaperSpaceExtMin", Get: func(h *Header) []any { return []any{h.PaperSpaceExtMin.X, h.PaperSpaceExtMin.Y, h.PaperSpaceExtMin.Z} }, Set: func(h *Header, v []any) { h.PaperSpaceExtMin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$PINSBASE", Codes: []int{10, 20, 30}, Field: "PaperSpaceInsertionBase", Get: func(h *Header) []any {
		return []any{h.PaperSpaceInsertionBase.X, h.PaperSpaceInsertionBase.Y, h.PaperSpaceInsertionBase.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceInsertionBase = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PLIMCHECK", Codes: []int{70}, Field: "PaperSpaceLimitsChecking", Get: func(h *Header) []any { return []any{b2i16(h.PaperSpaceLimitsChecking)} }, Set: func(h *Header, v []any) { h.PaperSpaceLimitsChecking = asBool(v[0]) }},
	{Name: "$PLIMMAX", Codes: []int{10, 20}, Field: "PaperSpaceLimitsMax", Get: func(h *Header) []any { return []any{h.PaperSpaceLimitsMax.X, h.PaperSpaceLimitsMax.Y} }, Set: func(h *Header, v []any) { h.PaperSpaceLimitsMax = Vec2{asFloat(v[0]), asFloat(v[1])} }},
	{Name: "$PLIMMIN", Codes: []int{10, 20}, Field: "PaperSpaceLimitsMin", Get: func(h *Header) []any { return []any{h.PaperSpaceLimitsMin.X, h.PaperSpaceLimitsMin.Y} }, Set: func(h *Header, v []any) { h.PaperSpaceLimitsMin = Vec2{asFloat(v[0]), asFloat(v[1])} }},
	{Name: "$PSLTSCALE", Codes: []int{70}, Field: "PaperSpaceLineTypeScaling", Get: func(h *Header) []any { return []any{int16(h.PaperSpaceLineTypeScaling)} }, Set: func(h *Header, v []any) { h.PaperSpaceLineTypeScaling = SpaceLineTypeScaling(asInt(v[0])) }},
	{Name: "$PUCSNAME", Codes: []int{2}, Field: "PaperSpaceName", IsName: true, Get: func(h *Header) []any { return []any{h.PaperSpaceName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.PaperSpaceName = s
		}
	}},
	{Name: "$PUCSORGBACK", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicBackDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicBackDOrigin.X, h.PaperSpaceOrthographicBackDOrigin.Y, h.PaperSpaceOrthographicBackDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicBackDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORGBOTTOM", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicBottomDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicBottomDOrigin.X, h.PaperSpaceOrthographicBottomDOrigin.Y, h.PaperSpaceOrthographicBottomDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicBottomDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORGFRONT", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicFrontDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicFrontDOrigin.X, h.PaperSpaceOrthographicFrontDOrigin.Y, h.PaperSpaceOrthographicFrontDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicFrontDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORGLEFT", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicLeftDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicLeftDOrigin.X, h.PaperSpaceOrthographicLeftDOrigin.Y, h.PaperSpaceOrthographicLeftDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicLeftDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORGRIGHT", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicRightDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicRightDOrigin.X, h.PaperSpaceOrthographicRightDOrigin.Y, h.PaperSpaceOrthographicRightDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicRightDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORGTOP", Codes: []int{10, 20, 30}, Field: "PaperSpaceOrthographicTopDOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceOrthographicTopDOrigin.X, h.PaperSpaceOrthographicTopDOrigin.Y, h.PaperSpaceOrthographicTopDOrigin.Z}
	}, Set: func(h *Header, v []any) {
		h.PaperSpaceOrthographicTopDOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])}
	}},
	{Name: "$PUCSORG", Codes: []int{10, 20, 30}, Field: "PaperSpaceUcsOrigin", Get: func(h *Header) []any {
		return []any{h.PaperSpaceUcsOrigin.X, h.PaperSpaceUcsOrigin.Y, h.PaperSpaceUcsOrigin.Z}
	}, Set: func(h *Header, v []any) { h.PaperSpaceUcsOrigin = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$PUCSXDIR", Codes: []int{10, 20, 30}, Field: "PaperSpaceUcsXAxis", Get: func(h *Header) []any {
		return []any{h.PaperSpaceUcsXAxis.X, h.PaperSpaceUcsXAxis.Y, h.PaperSpaceUcsXAxis.Z}
	}, Set: func(h *Header, v []any) { h.PaperSpaceUcsXAxis = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$PUCSYDIR", Codes: []int{10, 20, 30}, Field: "PaperSpaceUcsYAxis", Get: func(h *Header) []any {
		return []any{h.PaperSpaceUcsYAxis.X, h.PaperSpaceUcsYAxis.Y, h.PaperSpaceUcsYAxis.Z}
	}, Set: func(h *Header, v []any) { h.PaperSpaceUcsYAxis = Vec3{asFloat(v[0]), asFloat(v[1]), asFloat(v[2])} }},
	{Name: "$PSTYLEMODE", Codes: []int{290}, Field: "PlotStyleMode", Get: func(h *Header) []any { return []any{h.PlotStyleMode != 0} }, Set: func(h *Header, v []any) { h.PlotStyleMode = int16(asInt(v[0])) }},
	{Name: "$PDMODE", Codes: []int{70}, Field: "PointDisplayMode", Get: func(h *Header) []any { return []any{int16(h.PointDisplayMode)} }, Set: func(h *Header, v []any) { h.PointDisplayMode = int16(asInt(v[0])) }},
	{Name: "$PDSIZE", Codes: []int{40}, Field: "PointDisplaySize", Get: func(h *Header) []any { return []any{h.PointDisplaySize} }, Set: func(h *Header, v []any) { h.PointDisplaySize = asFloat(v[0]) }},
	{Name: "$PLINEGEN", Codes: []int{70}, Field: "PolylineLineTypeGeneration", Get: func(h *Header) []any { return []any{b2i16(h.PolylineLineTypeGeneration)} }, Set: func(h *Header, v []any) { h.PolylineLineTypeGeneration = asBool(v[0]) }},
	{Name: "$PLINEWID", Codes: []int{40}, Field: "PolylineWidthDefault", Get: func(h *Header) []any { return []any{h.PolylineWidthDefault} }, Set: func(h *Header, v []any) { h.PolylineWidthDefault = asFloat(v[0]) }},
	{Name: "$PROJECTNAME", Codes: []int{1}, Field: "ProjectName", Get: func(h *Header) []any { return []any{h.ProjectName} }, Set: func(h *Header, v []any) { h.ProjectName = asString(v[0]) }},
	{Name: "$PROXYGRAPHICS", Codes: []int{70}, Field: "ProxyGraphics", Get: func(h *Header) []any { return []any{b2i16(h.ProxyGraphics)} }, Set: func(h *Header, v []any) { h.ProxyGraphics = asBool(v[0]) }},
	{Name: "$QTEXTMODE", Codes: []int{70}, Field: "QuickTextMode", Get: func(h *Header) []any { return []any{b2i16(h.QuickTextMode)} }, Set: func(h *Header, v []any) { h.QuickTextMode = asBool(v[0]) }},
	{Name: "$REGENMODE", Codes: []int{70}, Field: "RegenerationMode", Get: func(h *Header) []any { return []any{b2i16(h.RegenerationMode)} }, Set: func(h *Header, v []any) { h.RegenerationMode = asBool(v[0]) }},
	{Name: "$REQUIREDVERSIONS", Codes: []int{70}, Field: "RequiredVersions", Ignored: true, Get: func(h *Header) []any { return []any{int16(h.RequiredVersions)} }, Set: func(h *Header, v []any) { h.RequiredVersions = int64(asInt(v[0])) }},
	{Name: "$VISRETAIN", Codes: []int{70}, Field: "RetainXRefDependentVisibilitySettings", Get: func(h *Header) []any { return []any{b2i16(h.RetainXRefDependentVisibilitySettings)} }, Set: func(h *Header, v []any) { h.RetainXRefDependentVisibilitySettings = asBool(v[0]) }},
	{Name: "$SHADEDIF", Codes: []int{70}, Field: "ShadeDiffuseToAmbientPercentage", Get: func(h *Header) []any { return []any{int16(h.ShadeDiffuseToAmbientPercentage)} }, Set: func(h *Header, v []any) { h.ShadeDiffuseToAmbientPercentage = int16(asInt(v[0])) }},
	{Name: "$SHADEDGE", Codes: []int{70}, Field: "ShadeEdge", Get: func(h *Header) []any { return []any{int16(h.ShadeEdge)} }, Set: func(h *Header, v []any) { h.ShadeEdge = ShadeEdgeType(asInt(v[0])) }},
	{Name: "$CSHADOW", Codes: []int{280}, Field: "ShadowMode", Get: func(h *Header) []any { return []any{int16(h.ShadowMode)} }, Set: func(h *Header, v []any) { h.ShadowMode = ShadowMode(asInt(v[0])) }},
	{Name: "$SHADOWPLANELOCATION", Codes: []int{40}, Field: "ShadowPlaneLocation", Get: func(h *Header) []any { return []any{h.ShadowPlaneLocation} }, Set: func(h *Header, v []any) { h.ShadowPlaneLocation = asFloat(v[0]) }},
	{Name: "$TILEMODE", Codes: []int{70}, Field: "ShowModelSpace", Get: func(h *Header) []any { return []any{b2i16(h.ShowModelSpace)} }, Set: func(h *Header, v []any) { h.ShowModelSpace = asBool(v[0]) }},
	{Name: "$SHOWHIST", Codes: []int{280}, Field: "ShowSolidsHistory", Get: func(h *Header) []any { return []any{int16(h.ShowSolidsHistory)} }, Set: func(h *Header, v []any) { h.ShowSolidsHistory = uint8(asInt(v[0])) }},
	{Name: "$SPLFRAME", Codes: []int{70}, Field: "ShowSplineControlPoints", Get: func(h *Header) []any { return []any{b2i16(h.ShowSplineControlPoints)} }, Set: func(h *Header, v []any) { h.ShowSplineControlPoints = asBool(v[0]) }},
	{Name: "$SKETCHINC", Codes: []int{40}, Field: "SketchIncrement", Get: func(h *Header) []any { return []any{h.SketchIncrement} }, Set: func(h *Header, v []any) { h.SketchIncrement = asFloat(v[0]) }},
	{Name: "$SKPOLY", Codes: []int{70}, Field: "SketchPolylines", Get: func(h *Header) []any { return []any{b2i16(h.SketchPolylines)} }, Set: func(h *Header, v []any) { h.SketchPolylines = asBool(v[0]) }},
	{Name: "$LOFTPARAM", Codes: []int{70}, Field: "SolidLoftedShape", Get: func(h *Header) []any { return []any{int16(h.SolidLoftedShape)} }, Set: func(h *Header, v []any) { h.SolidLoftedShape = int16(asInt(v[0])) }},
	{Name: "$SOLIDHIST", Codes: []int{280}, Field: "SolidsRetainHistory", Get: func(h *Header) []any { return []any{int16(h.SolidsRetainHistory)} }, Set: func(h *Header, v []any) { h.SolidsRetainHistory = uint8(asInt(v[0])) }},
	{Name: "$TREEDEPTH", Codes: []int{70}, Field: "SpatialIndexMaxTreeDepth", Get: func(h *Header) []any { return []any{int16(h.SpatialIndexMaxTreeDepth)} }, Set: func(h *Header, v []any) { h.SpatialIndexMaxTreeDepth = int16(asInt(v[0])) }},
	{Name: "$SPLINETYPE", Codes: []int{70}, Field: "SplineType", Get: func(h *Header) []any { return []any{int16(h.SplineType)} }, Set: func(h *Header, v []any) { h.SplineType = SplineType(asInt(v[0])) }},
	{Name: "$STEPSIZE", Codes: []int{40}, Field: "StepSize", Get: func(h *Header) []any { return []any{h.StepSize} }, Set: func(h *Header, v []any) { h.StepSize = asFloat(v[0]) }},
	{Name: "$STEPSPERSEC", Codes: []int{40}, Field: "StepsPerSecond", Get: func(h *Header) []any { return []any{h.StepsPerSecond} }, Set: func(h *Header, v []any) { h.StepsPerSecond = asFloat(v[0]) }},
	{Name: "$STYLESHEET", Codes: []int{1}, Field: "StyleSheetName", Get: func(h *Header) []any { return []any{h.StyleSheetName} }, Set: func(h *Header, v []any) { h.StyleSheetName = asString(v[0]) }},
	{Name: "$SURFU", Codes: []int{70}, Field: "SurfaceDensityU", Get: func(h *Header) []any { return []any{int16(h.SurfaceDensityU)} }, Set: func(h *Header, v []any) { h.SurfaceDensityU = int16(asInt(v[0])) }},
	{Name: "$SURFV", Codes: []int{70}, Field: "SurfaceDensityV", Get: func(h *Header) []any { return []any{int16(h.SurfaceDensityV)} }, Set: func(h *Header, v []any) { h.SurfaceDensityV = int16(asInt(v[0])) }},
	{Name: "$SURFTAB1", Codes: []int{70}, Field: "SurfaceMeshTabulationCount1", Get: func(h *Header) []any { return []any{int16(h.SurfaceMeshTabulationCount1)} }, Set: func(h *Header, v []any) { h.SurfaceMeshTabulationCount1 = int16(asInt(v[0])) }},
	{Name: "$SURFTAB2", Codes: []int{70}, Field: "SurfaceMeshTabulationCount2", Get: func(h *Header) []any { return []any{int16(h.SurfaceMeshTabulationCount2)} }, Set: func(h *Header, v []any) { h.SurfaceMeshTabulationCount2 = int16(asInt(v[0])) }},
	{Name: "$SURFTYPE", Codes: []int{70}, Field: "SurfaceType", Get: func(h *Header) []any { return []any{int16(h.SurfaceType)} }, Set: func(h *Header, v []any) { h.SurfaceType = int16(asInt(v[0])) }},
	{Name: "$PSOLHEIGHT", Codes: []int{40}, Field: "SweptSolidHeight", Get: func(h *Header) []any { return []any{h.SweptSolidHeight} }, Set: func(h *Header, v []any) { h.SweptSolidHeight = asFloat(v[0]) }},
	{Name: "$PSOLWIDTH", Codes: []int{40}, Field: "SweptSolidWidth", Get: func(h *Header) []any { return []any{h.SweptSolidWidth} }, Set: func(h *Header, v []any) { h.SweptSolidWidth = asFloat(v[0]) }},
	{Name: "$TEXTSIZE", Codes: []int{40}, Field: "TextHeightDefault", Get: func(h *Header) []any { return []any{h.TextHeightDefault} }, Set: func(h *Header, v []any) { h.TextHeightDefault = asFloat(v[0]) }},
	{Name: "$THICKNESS", Codes: []int{40}, Field: "ThicknessDefault", Get: func(h *Header) []any { return []any{h.ThicknessDefault} }, Set: func(h *Header, v []any) { h.ThicknessDefault = asFloat(v[0]) }},
	{Name: "$TIMEZONE", Codes: []int{70}, Field: "TimeZone", Get: func(h *Header) []any { return []any{int16(h.TimeZone)} }, Set: func(h *Header, v []any) { h.TimeZone = int32(asInt(v[0])) }},
	{Name: "$TDINDWG", Codes: []int{40}, Field: "TotalEditingTime", Get: func(h *Header) []any { return []any{durationDays(h.TotalEditingTime)} }, Set: func(h *Header, v []any) { h.TotalEditingTime = daysDuration(asFloat(v[0])) }},
	{Name: "$TRACEWID", Codes: []int{40}, Field: "TraceWidthDefault", Get: func(h *Header) []any { return []any{h.TraceWidthDefault} }, Set: func(h *Header, v []any) { h.TraceWidthDefault = asFloat(v[0]) }},
	{Name: "$UCSBASE", Codes: []int{2}, Field: "UcsBaseName", IsName: true, Get: func(h *Header) []any { return []any{h.UcsBaseName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.UcsBaseName = s
		}
	}},
	{Name: "$UCSNAME", Codes: []int{2}, Field: "UcsName", IsName: true, Get: func(h *Header) []any { return []any{h.UcsName} }, Set: func(h *Header, v []any) {
		if s := asString(v[0]); s != "" {
			h.UcsName = s
		}
	}},
	{Name: "$UNITMODE", Codes: []int{70}, Field: "UnitMode", Get: func(h *Header) []any { return []any{int16(h.UnitMode)} }, Set: func(h *Header, v []any) { h.UnitMode = int16(asInt(v[0])) }},
	{Name: "$TDUCREATE", Codes: []int{40}, Field: "UniversalCreateDateTime", Get: func(h *Header) []any { return []any{ToJulianDate(h.UniversalCreateDateTime)} }, Set: func(h *Header, v []any) { h.UniversalCreateDateTime = FromJulianDate(asFloat(v[0])) }},
	{Name: "$TDUUPDATE", Codes: []int{40}, Field: "UniversalUpdateDateTime", Get: func(h *Header) []any { return []any{ToJulianDate(h.UniversalUpdateDateTime)} }, Set: func(h *Header, v []any) { h.UniversalUpdateDateTime = FromJulianDate(asFloat(v[0])) }},
	{Name: "$TDUPDATE", Codes: []int{40}, Field: "UpdateDateTime", Get: func(h *Header) []any { return []any{ToJulianDate(h.UpdateDateTime)} }, Set: func(h *Header, v []any) { h.UpdateDateTime = FromJulianDate(asFloat(v[0])) }},
	{Name: "$DIMSHO", Codes: []int{70}, Field: "UpdateDimensionsWhileDragging", Get: func(h *Header) []any { return []any{b2i16(h.UpdateDimensionsWhileDragging)} }, Set: func(h *Header, v []any) { h.UpdateDimensionsWhileDragging = asBool(v[0]) }},
	{Name: "$USERR1", Codes: []int{40}, Field: "UserDouble1", Get: func(h *Header) []any { return []any{h.UserDouble1} }, Set: func(h *Header, v []any) { h.UserDouble1 = asFloat(v[0]) }},
	{Name: "$USERR2", Codes: []int{40}, Field: "UserDouble2", Get: func(h *Header) []any { return []any{h.UserDouble2} }, Set: func(h *Header, v []any) { h.UserDouble2 = asFloat(v[0]) }},
	{Name: "$USERR3", Codes: []int{40}, Field: "UserDouble3", Get: func(h *Header) []any { return []any{h.UserDouble3} }, Set: func(h *Header, v []any) { h.UserDouble3 = asFloat(v[0]) }},
	{Name: "$USERR4", Codes: []int{40}, Field: "UserDouble4", Get: func(h *Header) []any { return []any{h.UserDouble4} }, Set: func(h *Header, v []any) { h.UserDouble4 = asFloat(v[0]) }},
	{Name: "$USERR5", Codes: []int{40}, Field: "UserDouble5", Get: func(h *Header) []any { return []any{h.UserDouble5} }, Set: func(h *Header, v []any) { h.UserDouble5 = asFloat(v[0]) }},
	{Name: "$TDUSRTIMER", Codes: []int{40}, Field: "UserElapsedTimeSpan", Get: func(h *Header) []any { return []any{durationDays(h.UserElapsedTimeSpan)} }, Set: func(h *Header, v []any) { h.UserElapsedTimeSpan = daysDuration(asFloat(v[0])) }},
	{Name: "$USERI1", Codes: []int{70}, Field: "UserShort1", Get: func(h *Header) []any { return []any{int16(h.UserShort1)} }, Set: func(h *Header, v []any) { h.UserShort1 = int16(asInt(v[0])) }},
	{Name: "$USERI2", Codes: []int{70}, Field: "UserShort2", Get: func(h *Header) []any { return []any{int16(h.UserShort2)} }, Set: func(h *Header, v []any) { h.UserShort2 = int16(asInt(v[0])) }},
	{Name: "$USERI3", Codes: []int{70}, Field: "UserShort3", Get: func(h *Header) []any { return []any{int16(h.UserShort3)} }, Set: func(h *Header, v []any) { h.UserShort3 = int16(asInt(v[0])) }},
	{Name: "$USERI4", Codes: []int{70}, Field: "UserShort4", Get: func(h *Header) []any { return []any{int16(h.UserShort4)} }, Set: func(h *Header, v []any) { h.UserShort4 = int16(asInt(v[0])) }},
	{Name: "$USERI5", Codes: []int{70}, Field: "UserShort5", Get: func(h *Header) []any { return []any{int16(h.UserShort5)} }, Set: func(h *Header, v []any) { h.UserShort5 = int16(asInt(v[0])) }},
	{Name: "$USRTIMER", Codes: []int{70}, Field: "UserTimer", Get: func(h *Header) []any { return []any{b2i16(h.UserTimer)} }, Set: func(h *Header, v []any) { h.UserTimer = asBool(v[0]) }},
	{Name: "$VERSIONGUID", Codes: []int{2}, Field: "VersionGuid", Get: func(h *Header) []any { return []any{h.VersionGuid} }, Set: func(h *Header, v []any) { h.VersionGuid = asString(v[0]) }},
	{Name: "$PSVPSCALE", Codes: []int{40}, Field: "ViewportDefaultViewScaleFactor", Get: func(h *Header) []any { return []any{h.ViewportDefaultViewScaleFactor} }, Set: func(h *Header, v []any) { h.ViewportDefaultViewScaleFactor = asFloat(v[0]) }},
	{Name: "$WORLDVIEW", Codes: []int{70}, Field: "WorldView", Get: func(h *Header) []any { return []any{b2i16(h.WorldView)} }, Set: func(h *Header, v []any) { h.WorldView = asBool(v[0]) }},
	{Name: "$XEDIT", Codes: []int{290}, Field: "XEdit", Get: func(h *Header) []any { return []any{h.XEdit} }, Set: func(h *Header, v []any) { h.XEdit = asBool(v[0]) }},
}
