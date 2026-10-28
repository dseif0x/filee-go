// Code generated from ACadSharp enums (tools/genenums.py); DO NOT EDIT.

package cadoc

// AngularDirection is ACadSharp's AngularDirection.
type AngularDirection int16

const (
	AngularDirectionCounterClockWise AngularDirection = 0
	AngularDirectionClockWise        AngularDirection = 1
)

// AngularUnitFormat is ACadSharp's AngularUnitFormat.
type AngularUnitFormat int16

const (
	AngularUnitFormatDecimalDegrees        AngularUnitFormat = 0
	AngularUnitFormatDegreesMinutesSeconds AngularUnitFormat = 1
	AngularUnitFormatGradians              AngularUnitFormat = 2
	AngularUnitFormatRadians               AngularUnitFormat = 3
	AngularUnitFormatSurveyorsUnits        AngularUnitFormat = 4
)

// AngularZeroHandling is ACadSharp's AngularZeroHandling.
type AngularZeroHandling uint8

const (
	AngularZeroHandlingDisplayAll            AngularZeroHandling = 0
	AngularZeroHandlingSuppressLeadingZeroes AngularZeroHandling = 1
	AngularZeroHandlingSupressTrailingZeroes AngularZeroHandling = 2
	AngularZeroHandlingSupressAll            AngularZeroHandling = 3
)

// ArcLengthSymbolPosition is ACadSharp's ArcLengthSymbolPosition.
type ArcLengthSymbolPosition int16

const (
	ArcLengthSymbolPositionBeforeDimensionText ArcLengthSymbolPosition = 0
	ArcLengthSymbolPositionAboveDimensionText  ArcLengthSymbolPosition = 1
	ArcLengthSymbolPositionNone                ArcLengthSymbolPosition = 2
)

// AssociativityFlags is ACadSharp's AssociativityFlags.
type AssociativityFlags int16

const (
	AssociativityFlagsNone                 AssociativityFlags = 0
	AssociativityFlagsFirstPointReference  AssociativityFlags = 1
	AssociativityFlagsSecondPointReference AssociativityFlags = 2
	AssociativityFlagsThirdPointReference  AssociativityFlags = 4
	AssociativityFlagsFourthPointReference AssociativityFlags = 8
)

// AttachmentPointType is ACadSharp's AttachmentPointType.
type AttachmentPointType int16

const (
	AttachmentPointTypeTopLeft      AttachmentPointType = 1
	AttachmentPointTypeTopCenter    AttachmentPointType = 2
	AttachmentPointTypeTopRight     AttachmentPointType = 3
	AttachmentPointTypeMiddleLeft   AttachmentPointType = 4
	AttachmentPointTypeMiddleCenter AttachmentPointType = 5
	AttachmentPointTypeMiddleRight  AttachmentPointType = 6
	AttachmentPointTypeBottomLeft   AttachmentPointType = 7
	AttachmentPointTypeBottomCenter AttachmentPointType = 8
	AttachmentPointTypeBottomRight  AttachmentPointType = 9
)

// AttributeFlags is ACadSharp's AttributeFlags.
type AttributeFlags int32

const (
	AttributeFlagsNone     AttributeFlags = 0
	AttributeFlagsHidden   AttributeFlags = 1
	AttributeFlagsConstant AttributeFlags = 2
	AttributeFlagsVerify   AttributeFlags = 4
	AttributeFlagsPreset   AttributeFlags = 8
)

// AttributeType is ACadSharp's AttributeType.
type AttributeType int32

const (
	AttributeTypeSingleLine        AttributeType = 1
	AttributeTypeMultiLine         AttributeType = 2
	AttributeTypeConstantMultiLine AttributeType = 4
)

// AttributeVisibilityMode is ACadSharp's AttributeVisibilityMode.
type AttributeVisibilityMode int32

const (
	AttributeVisibilityModeNone   AttributeVisibilityMode = 0
	AttributeVisibilityModeNormal AttributeVisibilityMode = 1
	AttributeVisibilityModeAll    AttributeVisibilityMode = 2
)

// AutoTransformMethodFlags is ACadSharp's AutoTransformMethodFlags.
type AutoTransformMethodFlags uint8

const (
	AutoTransformMethodFlagsNone                AutoTransformMethodFlags = 0
	AutoTransformMethodFlagsNoAutoTransform     AutoTransformMethodFlags = 1
	AutoTransformMethodFlagsScaleMapper         AutoTransformMethodFlags = 2
	AutoTransformMethodFlagsIncludeCurrentBlock AutoTransformMethodFlags = 4
)

// BackgroundFillFlags is ACadSharp's BackgroundFillFlags.
type BackgroundFillFlags uint8

const (
	BackgroundFillFlagsNone                   BackgroundFillFlags = 0
	BackgroundFillFlagsUseBackgroundFillColor BackgroundFillFlags = 1
	BackgroundFillFlagsUseDrawingWindowColor  BackgroundFillFlags = 2
	BackgroundFillFlagsTextFrame              BackgroundFillFlags = 16
)

// BlockContentConnectionType is ACadSharp's BlockContentConnectionType.
type BlockContentConnectionType int16

const (
	BlockContentConnectionTypeBlockExtents BlockContentConnectionType = 0
	BlockContentConnectionTypeBasePoint    BlockContentConnectionType = 1
)

// BlockTypeFlags is ACadSharp's BlockTypeFlags.
type BlockTypeFlags int32

const (
	BlockTypeFlagsNone                            BlockTypeFlags = 0
	BlockTypeFlagsAnonymous                       BlockTypeFlags = 1
	BlockTypeFlagsNonConstantAttributeDefinitions BlockTypeFlags = 2
	BlockTypeFlagsXRef                            BlockTypeFlags = 4
	BlockTypeFlagsXRefOverlay                     BlockTypeFlags = 8
	BlockTypeFlagsXRefDependent                   BlockTypeFlags = 16
	BlockTypeFlagsXRefResolved                    BlockTypeFlags = 32
	BlockTypeFlagsReferenced                      BlockTypeFlags = 64
)

// BoundaryPathFlags is ACadSharp's BoundaryPathFlags.
type BoundaryPathFlags int32

const (
	BoundaryPathFlagsDefault             BoundaryPathFlags = 0
	BoundaryPathFlagsExternal            BoundaryPathFlags = 1
	BoundaryPathFlagsPolyline            BoundaryPathFlags = 2
	BoundaryPathFlagsDerived             BoundaryPathFlags = 4
	BoundaryPathFlagsTextbox             BoundaryPathFlags = 8
	BoundaryPathFlagsOutermost           BoundaryPathFlags = 16
	BoundaryPathFlagsNotClosed           BoundaryPathFlags = 32
	BoundaryPathFlagsSelfIntersecting    BoundaryPathFlags = 64
	BoundaryPathFlagsTextIsland          BoundaryPathFlags = 128
	BoundaryPathFlagsDuplicate           BoundaryPathFlags = 256
	BoundaryPathFlagsIsAnnotative        BoundaryPathFlags = 512
	BoundaryPathFlagsDoesNotSupportScale BoundaryPathFlags = 1024
	BoundaryPathFlagsForceAnnoAllVisible BoundaryPathFlags = 2048
	BoundaryPathFlagsOrientToPaper       BoundaryPathFlags = 4096
	BoundaryPathFlagsIsAnnotativeBlock   BoundaryPathFlags = 8192
)

// CadValueType is ACadSharp's CadValueType.
type CadValueType int32

const (
	CadValueTypeUnknown      CadValueType = 0
	CadValueTypeLong         CadValueType = 1
	CadValueTypeDouble       CadValueType = 2
	CadValueTypeString       CadValueType = 4
	CadValueTypeDate         CadValueType = 8
	CadValueTypePoint2D      CadValueType = 0x10
	CadValueTypePoint3D      CadValueType = 0x20
	CadValueTypeHandle       CadValueType = 0x40
	CadValueTypeBuffer       CadValueType = 0x80
	CadValueTypeResultBuffer CadValueType = 0x100
	CadValueTypeGeneral      CadValueType = 0x200
)

// CadValueUnitType is ACadSharp's CadValueUnitType.
type CadValueUnitType int32

const (
	CadValueUnitTypeNoUnits    CadValueUnitType = 0
	CadValueUnitTypeDistance   CadValueUnitType = 1
	CadValueUnitTypeAngle      CadValueUnitType = 2
	CadValueUnitTypeArea       CadValueUnitType = 4
	CadValueUnitTypeVolume     CadValueUnitType = 8
	CadValueUnitTypeCurrency   CadValueUnitType = 0x10
	CadValueUnitTypePercentage CadValueUnitType = 0x20
)

// ClipMode is ACadSharp's ClipMode.
type ClipMode uint8

const (
	ClipModeOutside ClipMode = 0
	ClipModeInside  ClipMode = 1
)

// ClipType is ACadSharp's ClipType.
type ClipType int16

const (
	ClipTypeRectangular ClipType = 1
	ClipTypePolygonal   ClipType = 2
)

// ColorMethod is ACadSharp's ColorMethod.
type ColorMethod uint8

const (
	ColorMethodCurrent  ColorMethod = 0
	ColorMethodOverride ColorMethod = 1
)

// ColumnType is ACadSharp's ColumnType.
type ColumnType int16

const (
	ColumnTypeNoColumns      ColumnType = 0
	ColumnTypeStaticColumns  ColumnType = 1
	ColumnTypeDynamicColumns ColumnType = 2
)

// DefaultLightingType is ACadSharp's DefaultLightingType.
type DefaultLightingType int16

const (
	DefaultLightingTypeOneDistantLight  DefaultLightingType = 0
	DefaultLightingTypeTwoDistantLights DefaultLightingType = 1
)

// DesignCoordinatesType is ACadSharp's DesignCoordinatesType.
type DesignCoordinatesType int32

const (
	DesignCoordinatesTypeUnknown       DesignCoordinatesType = 0
	DesignCoordinatesTypeLocalGrid     DesignCoordinatesType = 1
	DesignCoordinatesTypeProjectedGrid DesignCoordinatesType = 2
	DesignCoordinatesTypeGeographic    DesignCoordinatesType = 3
)

// DictionaryCloningFlags is ACadSharp's DictionaryCloningFlags.
type DictionaryCloningFlags int16

const (
	DictionaryCloningFlagsNotApplicable DictionaryCloningFlags = 0
	DictionaryCloningFlagsKeepExisting  DictionaryCloningFlags = 1
	DictionaryCloningFlagsUseClone      DictionaryCloningFlags = 2
	DictionaryCloningFlagsXrefName      DictionaryCloningFlags = 3
	DictionaryCloningFlagsName          DictionaryCloningFlags = 4
	DictionaryCloningFlagsUnmangleName  DictionaryCloningFlags = 5
)

// DimensionTextBackgroundFillMode is ACadSharp's DimensionTextBackgroundFillMode.
type DimensionTextBackgroundFillMode int16

const (
	DimensionTextBackgroundFillModeNoBackground                 DimensionTextBackgroundFillMode = 0
	DimensionTextBackgroundFillModeDrawingBackgroundColor       DimensionTextBackgroundFillMode = 1
	DimensionTextBackgroundFillModeDimensionTextBackgroundColor DimensionTextBackgroundFillMode = 2
)

// DimensionTextHorizontalAlignment is ACadSharp's DimensionTextHorizontalAlignment.
type DimensionTextHorizontalAlignment uint8

const (
	DimensionTextHorizontalAlignmentCentered          DimensionTextHorizontalAlignment = 0
	DimensionTextHorizontalAlignmentLeft              DimensionTextHorizontalAlignment = 1
	DimensionTextHorizontalAlignmentRight             DimensionTextHorizontalAlignment = 2
	DimensionTextHorizontalAlignmentOverFirstExtLine  DimensionTextHorizontalAlignment = 3
	DimensionTextHorizontalAlignmentOverSecondExtLine DimensionTextHorizontalAlignment = 4
)

// DimensionTextVerticalAlignment is ACadSharp's DimensionTextVerticalAlignment.
type DimensionTextVerticalAlignment int32

const (
	DimensionTextVerticalAlignmentCentered DimensionTextVerticalAlignment = 0
	DimensionTextVerticalAlignmentAbove    DimensionTextVerticalAlignment = 1
	DimensionTextVerticalAlignmentOutside  DimensionTextVerticalAlignment = 2
	DimensionTextVerticalAlignmentJIS      DimensionTextVerticalAlignment = 3
	DimensionTextVerticalAlignmentBelow    DimensionTextVerticalAlignment = 4
)

// DimensionType is ACadSharp's DimensionType.
type DimensionType int32

const (
	DimensionTypeLinear                  DimensionType = 0
	DimensionTypeAligned                 DimensionType = 1
	DimensionTypeAngular                 DimensionType = 2
	DimensionTypeDiameter                DimensionType = 3
	DimensionTypeRadius                  DimensionType = 4
	DimensionTypeAngular3Point           DimensionType = 5
	DimensionTypeOrdinate                DimensionType = 6
	DimensionTypeBlockReference          DimensionType = 32
	DimensionTypeOrdinateTypeX           DimensionType = 64
	DimensionTypeTextUserDefinedLocation DimensionType = 128
)

// DrawingDirectionType is ACadSharp's DrawingDirectionType.
type DrawingDirectionType int16

const (
	DrawingDirectionTypeLeftToRight DrawingDirectionType = 1
	DrawingDirectionTypeRightToLeft DrawingDirectionType = 2
	DrawingDirectionTypeTopToBottom DrawingDirectionType = 3
	DrawingDirectionTypeBottomToTop DrawingDirectionType = 4
	DrawingDirectionTypeByStyle     DrawingDirectionType = 5
)

// DwgPreviewType is ACadSharp's DwgPreview.PreviewType.
type DwgPreviewType int32

const (
	DwgPreviewTypeUnknown DwgPreviewType = 0
	DwgPreviewTypeBmp     DwgPreviewType = 2
	DwgPreviewTypeWmf     DwgPreviewType = 3
	DwgPreviewTypePng     DwgPreviewType = 6
)

// EdgeStyleModel is ACadSharp's EdgeStyleModel.
type EdgeStyleModel int32

const (
	EdgeStyleModelNoEdges    EdgeStyleModel = 0
	EdgeStyleModelIsolines   EdgeStyleModel = 1
	EdgeStyleModelFacetEdges EdgeStyleModel = 2
)

// EntityPlotStyleType is ACadSharp's EntityPlotStyleType.
type EntityPlotStyleType int16

const (
	EntityPlotStyleTypeByLayer             EntityPlotStyleType = 0
	EntityPlotStyleTypeByBlock             EntityPlotStyleType = 1
	EntityPlotStyleTypeByDictionaryDefault EntityPlotStyleType = 2
	EntityPlotStyleTypeByObjectId          EntityPlotStyleType = 3
)

// EntryFlags is ACadSharp's EntryFlags.
type EntryFlags int32

const (
	EntryFlagsNone          EntryFlags = 0
	EntryFlagsXrefDependent EntryFlags = 16
	EntryFlagsXrefResolved  EntryFlags = 32
	EntryFlagsReferenced    EntryFlags = 64
)

// EvaluationOptionFlags is ACadSharp's EvaluationOptionFlags.
type EvaluationOptionFlags int32

const (
	EvaluationOptionFlagsNever             EvaluationOptionFlags = 0
	EvaluationOptionFlagsOnOpen            EvaluationOptionFlags = 1
	EvaluationOptionFlagsOnSave            EvaluationOptionFlags = 2
	EvaluationOptionFlagsOnPlot            EvaluationOptionFlags = 4
	EvaluationOptionFlagsOnPackedETransmit EvaluationOptionFlags = 8
	EvaluationOptionFlagsOnRegeneration    EvaluationOptionFlags = 0x10
	EvaluationOptionFlagsOnDemand          EvaluationOptionFlags = 0x20
)

// EvaluationStatusFlags is ACadSharp's EvaluationStatusFlags.
type EvaluationStatusFlags int32

const (
	EvaluationStatusFlagsNotEvaluated      EvaluationStatusFlags = 1
	EvaluationStatusFlagsSuccess           EvaluationStatusFlags = 2
	EvaluationStatusFlagsEvaluatorNotFound EvaluationStatusFlags = 4
	EvaluationStatusFlagsSyntaxError       EvaluationStatusFlags = 8
	EvaluationStatusFlagsInvalidCode       EvaluationStatusFlags = 0x10
	EvaluationStatusFlagsInvalidContext    EvaluationStatusFlags = 0x20
	EvaluationStatusFlagsOtherError        EvaluationStatusFlags = 0x40
)

// FaceColorMode is ACadSharp's FaceColorMode.
type FaceColorMode int16

const (
	FaceColorModeNoColor         FaceColorMode = 0
	FaceColorModeObjectColor     FaceColorMode = 1
	FaceColorModeBackgroundColor FaceColorMode = 2
	FaceColorModeCustomColor     FaceColorMode = 3
	FaceColorModeMonochrome      FaceColorMode = 4
	FaceColorModeTinted          FaceColorMode = 5
	FaceColorModeDesaturated     FaceColorMode = 6
)

// FaceLightingModelType is ACadSharp's FaceLightingModelType.
type FaceLightingModelType int16

const (
	FaceLightingModelTypeInvisible FaceLightingModelType = 0
	FaceLightingModelTypeVisible   FaceLightingModelType = 1
	FaceLightingModelTypePhong     FaceLightingModelType = 2
	FaceLightingModelTypeGooch     FaceLightingModelType = 3
)

// FaceLightingQualityType is ACadSharp's FaceLightingQualityType.
type FaceLightingQualityType int16

const (
	FaceLightingQualityTypeNoLighting        FaceLightingQualityType = 0
	FaceLightingQualityTypePerFaceLighting   FaceLightingQualityType = 1
	FaceLightingQualityTypePerVertexLighting FaceLightingQualityType = 2
)

// FaceModifierType is ACadSharp's FaceModifierType.
type FaceModifierType int16

const (
	FaceModifierTypeNone     FaceModifierType = 0
	FaceModifierTypeOpacity  FaceModifierType = 1
	FaceModifierTypeSpecular FaceModifierType = 2
)

// FieldStateFlags is ACadSharp's FieldStateFlags.
type FieldStateFlags int32

const (
	FieldStateFlagsUnknown     FieldStateFlags = 0
	FieldStateFlagsInitialized FieldStateFlags = 1
	FieldStateFlagsCompiled    FieldStateFlags = 2
	FieldStateFlagsModified    FieldStateFlags = 4
	FieldStateFlagsEvaluated   FieldStateFlags = 8
	FieldStateFlagsCached      FieldStateFlags = 16
)

// FilingOptionFlags is ACadSharp's FilingOptionFlags.
type FilingOptionFlags int32

const (
	FilingOptionFlagsNone          FilingOptionFlags = 0
	FilingOptionFlagsNoFieldResult FilingOptionFlags = 1
)

// FlowDirectionType is ACadSharp's FlowDirectionType.
type FlowDirectionType int16

const (
	FlowDirectionTypeHorizontal    FlowDirectionType = 1
	FlowDirectionTypeVertical      FlowDirectionType = 3
	FlowDirectionTypeByStyle       FlowDirectionType = 5
	FlowDirectionTypeByStyleBadDoc FlowDirectionType = 6
)

// FontFlags is ACadSharp's FontFlags.
type FontFlags int32

const (
	FontFlagsRegular FontFlags = 0
	FontFlagsItalic  FontFlags = 1
	FontFlagsBold    FontFlags = 2
)

// FractionFormat is ACadSharp's FractionFormat.
type FractionFormat int16

const (
	FractionFormatHorizontal FractionFormat = 0
	FractionFormatDiagonal   FractionFormat = 1
	FractionFormatNone       FractionFormat = 2
)

// GeoDataVersion is ACadSharp's GeoDataVersion.
type GeoDataVersion int32

const (
	GeoDataVersionR2009 GeoDataVersion = 1
	GeoDataVersionR2010 GeoDataVersion = 2
	GeoDataVersionR2013 GeoDataVersion = 3
)

// GridFlags is ACadSharp's GridFlags.
type GridFlags int16

const (
	GridFlags0 GridFlags = 0
	GridFlags1 GridFlags = 1
	GridFlags2 GridFlags = 2
	GridFlags3 GridFlags = 4
	GridFlags4 GridFlags = 8
)

// HatchEdgeType is ACadSharp's Hatch.BoundaryPath.EdgeType.
type HatchEdgeType int32

const (
	HatchEdgeTypePolyline    HatchEdgeType = 0
	HatchEdgeTypeLine        HatchEdgeType = 1
	HatchEdgeTypeCircularArc HatchEdgeType = 2
	HatchEdgeTypeEllipticArc HatchEdgeType = 3
	HatchEdgeTypeSpline      HatchEdgeType = 4
)

// HatchPatternType is ACadSharp's HatchPatternType.
type HatchPatternType int32

const (
	HatchPatternTypePatternFill HatchPatternType = 0
	HatchPatternTypeSolidFill   HatchPatternType = 1
	HatchPatternTypeCustom      HatchPatternType = 2
)

// HatchStyleType is ACadSharp's HatchStyleType.
type HatchStyleType int32

const (
	HatchStyleTypeNormal HatchStyleType = 0
	HatchStyleTypeOuter  HatchStyleType = 1
	HatchStyleTypeIgnore HatchStyleType = 2
)

// HookLineDirection is ACadSharp's HookLineDirection.
type HookLineDirection int16

const (
	HookLineDirectionOpposite HookLineDirection = 0
	HookLineDirectionSame     HookLineDirection = 1
)

// ImageDisplayFlags is ACadSharp's ImageDisplayFlags.
type ImageDisplayFlags int16

const (
	ImageDisplayFlagsNone                ImageDisplayFlags = 0
	ImageDisplayFlagsShowImage           ImageDisplayFlags = 1
	ImageDisplayFlagsShowNotAlignedImage ImageDisplayFlags = 2
	ImageDisplayFlagsUseClippingBoundary ImageDisplayFlags = 4
	ImageDisplayFlagsTransparencyIsOn    ImageDisplayFlags = 8
)

// ImageDisplayQuality is ACadSharp's ImageDisplayQuality.
type ImageDisplayQuality int32

const (
	ImageDisplayQualityDraft ImageDisplayQuality = 0
	ImageDisplayQualityHigh  ImageDisplayQuality = 1
)

// ImageUnits is ACadSharp's ImageUnits.
type ImageUnits int16

const (
	ImageUnitsUnitless    ImageUnits = 0
	ImageUnitsMillimeters ImageUnits = 1
	ImageUnitsCentimeters ImageUnits = 2
	ImageUnitsMeters      ImageUnits = 3
	ImageUnitsKilometers  ImageUnits = 4
	ImageUnitsInches      ImageUnits = 5
	ImageUnitsFeet        ImageUnits = 6
	ImageUnitsYards       ImageUnits = 7
	ImageUnitsMiles       ImageUnits = 8
)

// IndexCreationFlags is ACadSharp's IndexCreationFlags.
type IndexCreationFlags uint8

const (
	IndexCreationFlagsNoIndex              IndexCreationFlags = 0b0
	IndexCreationFlagsLayerIndex           IndexCreationFlags = 0b1
	IndexCreationFlagsSpatialIndex         IndexCreationFlags = 0b10
	IndexCreationFlagsLayerAndSpatialIndex IndexCreationFlags = 0b11
)

// InvisibleEdgeFlags is ACadSharp's InvisibleEdgeFlags.
type InvisibleEdgeFlags int32

const (
	InvisibleEdgeFlagsNone   InvisibleEdgeFlags = 0
	InvisibleEdgeFlagsFirst  InvisibleEdgeFlags = 1
	InvisibleEdgeFlagsSecond InvisibleEdgeFlags = 2
	InvisibleEdgeFlagsThird  InvisibleEdgeFlags = 4
	InvisibleEdgeFlagsFourth InvisibleEdgeFlags = 8
)

// KnotParametrization is ACadSharp's KnotParametrization.
type KnotParametrization uint16

const (
	KnotParametrizationChord      KnotParametrization = 0
	KnotParametrizationSquareRoot KnotParametrization = 1
	KnotParametrizationUniform    KnotParametrization = 2
	KnotParametrizationCustom     KnotParametrization = 15
)

// LayerFlags is ACadSharp's LayerFlags.
type LayerFlags int16

const (
	LayerFlagsNone               LayerFlags = 0
	LayerFlagsFrozen             LayerFlags = 1
	LayerFlagsFrozenNewViewports LayerFlags = 2
	LayerFlagsLocked             LayerFlags = 4
	LayerFlagsXrefDependent      LayerFlags = 16
	LayerFlagsXrefResolved       LayerFlags = 32
	LayerFlagsReferenced         LayerFlags = 64
)

// LayoutFlags is ACadSharp's LayoutFlags.
type LayoutFlags int16

const (
	LayoutFlagsNone                      LayoutFlags = 0
	LayoutFlagsPaperSpaceLinetypeScaling LayoutFlags = 1
	LayoutFlagsLimitsChecking            LayoutFlags = 2
)

// LeaderContentType is ACadSharp's LeaderContentType.
type LeaderContentType int16

const (
	LeaderContentTypeNone      LeaderContentType = 0
	LeaderContentTypeBlock     LeaderContentType = 1
	LeaderContentTypeMText     LeaderContentType = 2
	LeaderContentTypeTolerance LeaderContentType = 3
)

// LeaderCreationType is ACadSharp's LeaderCreationType.
type LeaderCreationType int16

const (
	LeaderCreationTypeCreatedWithTextAnnotation           LeaderCreationType = 0
	LeaderCreationTypeCreatedWithToleranceAnnotation      LeaderCreationType = 1
	LeaderCreationTypeCreatedWithBlockReferenceAnnotation LeaderCreationType = 2
	LeaderCreationTypeCreatedWithoutAnnotation            LeaderCreationType = 3
)

// LeaderDrawOrderType is ACadSharp's LeaderDrawOrderType.
type LeaderDrawOrderType int32

const (
	LeaderDrawOrderTypeLeaderHeadFirst LeaderDrawOrderType = 0
	LeaderDrawOrderTypeLeaderTailFirst LeaderDrawOrderType = 1
)

// LeaderLinePropertOverrideFlags is ACadSharp's LeaderLinePropertOverrideFlags.
type LeaderLinePropertOverrideFlags int32

const (
	LeaderLinePropertOverrideFlagsNone          LeaderLinePropertOverrideFlags = 0
	LeaderLinePropertOverrideFlagsPathType      LeaderLinePropertOverrideFlags = 1
	LeaderLinePropertOverrideFlagsLineColor     LeaderLinePropertOverrideFlags = 2
	LeaderLinePropertOverrideFlagsLineType      LeaderLinePropertOverrideFlags = 4
	LeaderLinePropertOverrideFlagsLineWeight    LeaderLinePropertOverrideFlags = 8
	LeaderLinePropertOverrideFlagsArrowheadSize LeaderLinePropertOverrideFlags = 16
	LeaderLinePropertOverrideFlagsArrowhead     LeaderLinePropertOverrideFlags = 32
)

// LeaderPathType is ACadSharp's LeaderPathType.
type LeaderPathType int32

const (
	LeaderPathTypeStraightLineSegments LeaderPathType = 0
	LeaderPathTypeSpline               LeaderPathType = 1
)

// LightingType is ACadSharp's LightingType.
type LightingType uint8

const (
	LightingTypeOneDistantLight  LightingType = 0
	LightingTypeTwoDistantLights LightingType = 1
)

// LineSpacingStyle is ACadSharp's LineSpacingStyle.
type LineSpacingStyle int16

const (
	LineSpacingStyleAtLeast LineSpacingStyle = 1
	LineSpacingStyleExactly LineSpacingStyle = 2
)

// LineSpacingStyleType is ACadSharp's LineSpacingStyleType.
type LineSpacingStyleType int16

const (
	LineSpacingStyleTypeNone    LineSpacingStyleType = 0
	LineSpacingStyleTypeAtLeast LineSpacingStyleType = 1
	LineSpacingStyleTypeExact   LineSpacingStyleType = 2
)

// LineTypeShapeFlags is ACadSharp's LineTypeShapeFlags.
type LineTypeShapeFlags int16

const (
	LineTypeShapeFlagsNone               LineTypeShapeFlags = 0
	LineTypeShapeFlagsRotationIsAbsolute LineTypeShapeFlags = 1
	LineTypeShapeFlagsText               LineTypeShapeFlags = 2
	LineTypeShapeFlagsShape              LineTypeShapeFlags = 4
)

// LinearUnitFormat is ACadSharp's LinearUnitFormat.
type LinearUnitFormat int16

const (
	LinearUnitFormatNone           LinearUnitFormat = 0
	LinearUnitFormatScientific     LinearUnitFormat = 1
	LinearUnitFormatDecimal        LinearUnitFormat = 2
	LinearUnitFormatEngineering    LinearUnitFormat = 3
	LinearUnitFormatArchitectural  LinearUnitFormat = 4
	LinearUnitFormatFractional     LinearUnitFormat = 5
	LinearUnitFormatWindowsDesktop LinearUnitFormat = 6
)

// LwPolylineFlags is ACadSharp's LwPolylineFlags.
type LwPolylineFlags int32

const (
	LwPolylineFlagsDefault  LwPolylineFlags = 0
	LwPolylineFlagsClosed   LwPolylineFlags = 1
	LwPolylineFlagsPlinegen LwPolylineFlags = 128
)

// MLineFlags is ACadSharp's MLineFlags.
type MLineFlags int32

const (
	MLineFlagsNone        MLineFlags = 0
	MLineFlagsHasVertices MLineFlags = 1
	MLineFlagsClosed      MLineFlags = 2
	MLineFlagsNoStartCaps MLineFlags = 4
	MLineFlagsNoEndCaps   MLineFlags = 8
)

// MLineJustification is ACadSharp's MLineJustification.
type MLineJustification int32

const (
	MLineJustificationTop    MLineJustification = 0
	MLineJustificationZero   MLineJustification = 1
	MLineJustificationBottom MLineJustification = 2
)

// MLineStyleFlags is ACadSharp's MLineStyleFlags.
type MLineStyleFlags int32

const (
	MLineStyleFlagsNone              MLineStyleFlags = 0
	MLineStyleFlagsFillOn            MLineStyleFlags = 1
	MLineStyleFlagsDisplayJoints     MLineStyleFlags = 2
	MLineStyleFlagsStartSquareCap    MLineStyleFlags = 16
	MLineStyleFlagsStartInnerArcsCap MLineStyleFlags = 32
	MLineStyleFlagsStartRoundCap     MLineStyleFlags = 64
	MLineStyleFlagsEndSquareCap      MLineStyleFlags = 256
	MLineStyleFlagsEndInnerArcsCap   MLineStyleFlags = 512
	MLineStyleFlagsEndRoundCap       MLineStyleFlags = 1024
)

// MapSource is ACadSharp's MapSource.
type MapSource uint8

const (
	MapSourceUseCurrentScene MapSource = 0
	MapSourceUseImageFile    MapSource = 1
	MapSourceProcedural      MapSource = 2
)

// MaterialChannelFlags is ACadSharp's MaterialChannelFlags.
type MaterialChannelFlags int32

const (
	MaterialChannelFlagsNone          MaterialChannelFlags = 0
	MaterialChannelFlagsUseDiffuse    MaterialChannelFlags = 0x01
	MaterialChannelFlagsUseSpecular   MaterialChannelFlags = 0x02
	MaterialChannelFlagsUseReflection MaterialChannelFlags = 0x04
	MaterialChannelFlagsUseOpacity    MaterialChannelFlags = 0x08
	MaterialChannelFlagsUseBump       MaterialChannelFlags = 0x10
	MaterialChannelFlagsUseRefraction MaterialChannelFlags = 0x20
	MaterialChannelFlagsUseNormalMap  MaterialChannelFlags = 0x40
	MaterialChannelFlagsUseAll        MaterialChannelFlags = MaterialChannelFlagsUseDiffuse | MaterialChannelFlagsUseSpecular | MaterialChannelFlagsUseReflection | MaterialChannelFlagsUseOpacity | MaterialChannelFlagsUseBump | MaterialChannelFlagsUseRefraction | MaterialChannelFlagsUseNormalMap
)

// MaterialIlluminationModel is ACadSharp's MaterialIlluminationModel.
type MaterialIlluminationModel int32

const (
	MaterialIlluminationModelBlinnShader MaterialIlluminationModel = 0
	MaterialIlluminationModelMetalShader MaterialIlluminationModel = 1
)

// MaterialMode is ACadSharp's MaterialMode.
type MaterialMode int32

const (
	MaterialModeRealistic MaterialMode = 0
	MaterialModeAdvanced  MaterialMode = 1
)

// MeasurementUnits is ACadSharp's MeasurementUnits.
type MeasurementUnits int16

const (
	MeasurementUnitsEnglish MeasurementUnits = 0
	MeasurementUnitsMetric  MeasurementUnits = 1
)

// MultiLeaderDrawOrderType is ACadSharp's MultiLeaderDrawOrderType.
type MultiLeaderDrawOrderType int32

const (
	MultiLeaderDrawOrderTypeContentFirst MultiLeaderDrawOrderType = 0
	MultiLeaderDrawOrderTypeLeaderFirst  MultiLeaderDrawOrderType = 1
)

// MultiLeaderPathType is ACadSharp's MultiLeaderPathType.
type MultiLeaderPathType int16

const (
	MultiLeaderPathTypeInvisible            MultiLeaderPathType = 0
	MultiLeaderPathTypeStraightLineSegments MultiLeaderPathType = 1
	MultiLeaderPathTypeSpline               MultiLeaderPathType = 2
)

// MultiLeaderPropertyOverrideFlags is ACadSharp's MultiLeaderPropertyOverrideFlags.
type MultiLeaderPropertyOverrideFlags int32

const (
	MultiLeaderPropertyOverrideFlagsNone                    MultiLeaderPropertyOverrideFlags = 0
	MultiLeaderPropertyOverrideFlagsPathType                MultiLeaderPropertyOverrideFlags = 0x1
	MultiLeaderPropertyOverrideFlagsLineColor               MultiLeaderPropertyOverrideFlags = 0x2
	MultiLeaderPropertyOverrideFlagsLeaderLineType          MultiLeaderPropertyOverrideFlags = 0x4
	MultiLeaderPropertyOverrideFlagsLeaderLineWeight        MultiLeaderPropertyOverrideFlags = 0x8
	MultiLeaderPropertyOverrideFlagsEnableLanding           MultiLeaderPropertyOverrideFlags = 0x10
	MultiLeaderPropertyOverrideFlagsLandingGap              MultiLeaderPropertyOverrideFlags = 0x20
	MultiLeaderPropertyOverrideFlagsEnableDogleg            MultiLeaderPropertyOverrideFlags = 0x40
	MultiLeaderPropertyOverrideFlagsLandingDistance         MultiLeaderPropertyOverrideFlags = 0x80
	MultiLeaderPropertyOverrideFlagsArrowhead               MultiLeaderPropertyOverrideFlags = 0x100
	MultiLeaderPropertyOverrideFlagsArrowheadSize           MultiLeaderPropertyOverrideFlags = 0x200
	MultiLeaderPropertyOverrideFlagsContentType             MultiLeaderPropertyOverrideFlags = 0x400
	MultiLeaderPropertyOverrideFlagsTextStyle               MultiLeaderPropertyOverrideFlags = 0x800
	MultiLeaderPropertyOverrideFlagsTextLeftAttachment      MultiLeaderPropertyOverrideFlags = 0x1000
	MultiLeaderPropertyOverrideFlagsTextAngle               MultiLeaderPropertyOverrideFlags = 0x2000
	MultiLeaderPropertyOverrideFlagsTextAlignment           MultiLeaderPropertyOverrideFlags = 0x4000
	MultiLeaderPropertyOverrideFlagsTextColor               MultiLeaderPropertyOverrideFlags = 0x8000
	MultiLeaderPropertyOverrideFlagsTextHeight              MultiLeaderPropertyOverrideFlags = 0x10000
	MultiLeaderPropertyOverrideFlagsTextFrame               MultiLeaderPropertyOverrideFlags = 0x20000
	MultiLeaderPropertyOverrideFlagsEnableUseDefaultMText   MultiLeaderPropertyOverrideFlags = 0x40000
	MultiLeaderPropertyOverrideFlagsBlockContent            MultiLeaderPropertyOverrideFlags = 0x80000
	MultiLeaderPropertyOverrideFlagsBlockContentColor       MultiLeaderPropertyOverrideFlags = 0x100000
	MultiLeaderPropertyOverrideFlagsBlockContentScale       MultiLeaderPropertyOverrideFlags = 0x200000
	MultiLeaderPropertyOverrideFlagsBlockContentRotation    MultiLeaderPropertyOverrideFlags = 0x400000
	MultiLeaderPropertyOverrideFlagsBlockContentConnection  MultiLeaderPropertyOverrideFlags = 0x800000
	MultiLeaderPropertyOverrideFlagsScaleFactor             MultiLeaderPropertyOverrideFlags = 0x1000000
	MultiLeaderPropertyOverrideFlagsTextRightAttachment     MultiLeaderPropertyOverrideFlags = 0x2000000
	MultiLeaderPropertyOverrideFlagsTextSwitchAlignmentType MultiLeaderPropertyOverrideFlags = 0x4000000
	MultiLeaderPropertyOverrideFlagsTextAttachmentDirection MultiLeaderPropertyOverrideFlags = 0x8000000
	MultiLeaderPropertyOverrideFlagsTextTopAttachment       MultiLeaderPropertyOverrideFlags = 0x10000000
	MultiLeaderPropertyOverrideFlagsTextBottomAttachment    MultiLeaderPropertyOverrideFlags = 0x20000000
)

// ObjectOsnapType is ACadSharp's ObjectOsnapType.
type ObjectOsnapType int16

const (
	ObjectOsnapTypeNone                 ObjectOsnapType = 0
	ObjectOsnapTypeEndpoint             ObjectOsnapType = 1
	ObjectOsnapTypeMidpoint             ObjectOsnapType = 2
	ObjectOsnapTypeCenter               ObjectOsnapType = 3
	ObjectOsnapTypeNode                 ObjectOsnapType = 4
	ObjectOsnapTypeQuadrant             ObjectOsnapType = 5
	ObjectOsnapTypeIntersection         ObjectOsnapType = 6
	ObjectOsnapTypeInsertion            ObjectOsnapType = 7
	ObjectOsnapTypePerpendicular        ObjectOsnapType = 8
	ObjectOsnapTypeTangent              ObjectOsnapType = 9
	ObjectOsnapTypeNearest              ObjectOsnapType = 10
	ObjectOsnapTypeApparentIntersection ObjectOsnapType = 11
	ObjectOsnapTypeParallel             ObjectOsnapType = 12
	ObjectOsnapTypeStartPoint           ObjectOsnapType = 13
)

// ObjectSnapMode is ACadSharp's ObjectSnapMode.
type ObjectSnapMode uint16

const (
	ObjectSnapModeNone                 ObjectSnapMode = 0
	ObjectSnapModeEndPoint             ObjectSnapMode = 1
	ObjectSnapModeMidPoint             ObjectSnapMode = 2
	ObjectSnapModeCenter               ObjectSnapMode = 4
	ObjectSnapModeNode                 ObjectSnapMode = 8
	ObjectSnapModeQuadrant             ObjectSnapMode = 16
	ObjectSnapModeIntersection         ObjectSnapMode = 32
	ObjectSnapModeInsertion            ObjectSnapMode = 64
	ObjectSnapModePerpendicular        ObjectSnapMode = 128
	ObjectSnapModeTangent              ObjectSnapMode = 256
	ObjectSnapModeNearest              ObjectSnapMode = 512
	ObjectSnapModeClearsAllObjectSnaps ObjectSnapMode = 1024
	ObjectSnapModeApparentIntersection ObjectSnapMode = 2048
	ObjectSnapModeExtension            ObjectSnapMode = 4096
	ObjectSnapModeParallel             ObjectSnapMode = 8192
	ObjectSnapModeAllModes             ObjectSnapMode = ObjectSnapModeParallel | ObjectSnapModeExtension | ObjectSnapModeApparentIntersection | ObjectSnapModeClearsAllObjectSnaps | ObjectSnapModeNearest | ObjectSnapModeTangent | ObjectSnapModePerpendicular | ObjectSnapModeInsertion | ObjectSnapModeIntersection | ObjectSnapModeQuadrant | ObjectSnapModeNode | ObjectSnapModeCenter | ObjectSnapModeMidPoint | ObjectSnapModeEndPoint
	ObjectSnapModeSwitchedOff          ObjectSnapMode = 16384
)

// ObjectSortingFlags is ACadSharp's ObjectSortingFlags.
type ObjectSortingFlags uint8

const (
	ObjectSortingFlagsDisabled   ObjectSortingFlags = 0
	ObjectSortingFlagsSelection  ObjectSortingFlags = 1
	ObjectSortingFlagsSnap       ObjectSortingFlags = 2
	ObjectSortingFlagsRedraw     ObjectSortingFlags = 4
	ObjectSortingFlagsSlide      ObjectSortingFlags = 8
	ObjectSortingFlagsRegen      ObjectSortingFlags = 16
	ObjectSortingFlagsPlotting   ObjectSortingFlags = 32
	ObjectSortingFlagsPostscript ObjectSortingFlags = 64
	ObjectSortingFlagsAll        ObjectSortingFlags = ObjectSortingFlagsPostscript | ObjectSortingFlagsPlotting | ObjectSortingFlagsRegen | ObjectSortingFlagsSlide | ObjectSortingFlagsRedraw | ObjectSortingFlagsSnap | ObjectSortingFlagsSelection
)

// OleObjectType is ACadSharp's OleObjectType.
type OleObjectType int32

const (
	OleObjectTypeLink     OleObjectType = 1
	OleObjectTypeEmbedded OleObjectType = 2
	OleObjectTypeStatic   OleObjectType = 3
)

// OrthographicType is ACadSharp's OrthographicType.
type OrthographicType int32

const (
	OrthographicTypeNone   OrthographicType = 0
	OrthographicTypeTop    OrthographicType = 1
	OrthographicTypeBottom OrthographicType = 2
	OrthographicTypeFront  OrthographicType = 3
	OrthographicTypeBack   OrthographicType = 4
	OrthographicTypeLeft   OrthographicType = 5
	OrthographicTypeRight  OrthographicType = 6
)

// PlotFlags is ACadSharp's PlotFlags.
type PlotFlags int32

const (
	PlotFlagsNone                PlotFlags = 0
	PlotFlagsPlotViewportBorders PlotFlags = 1
	PlotFlagsShowPlotStyles      PlotFlags = 2
	PlotFlagsPlotCentered        PlotFlags = 4
	PlotFlagsPlotHidden          PlotFlags = 8
	PlotFlagsUseStandardScale    PlotFlags = 16
	PlotFlagsPlotPlotStyles      PlotFlags = 32
	PlotFlagsScaleLineweights    PlotFlags = 64
	PlotFlagsPrintLineweights    PlotFlags = 128
	PlotFlagsDrawViewportsFirst  PlotFlags = 512
	PlotFlagsModelType           PlotFlags = 1024
	PlotFlagsUpdatePaper         PlotFlags = 2048
	PlotFlagsZoomToPaperOnUpdate PlotFlags = 4096
	PlotFlagsInitializing        PlotFlags = 8192
	PlotFlagsPrevPlotInit        PlotFlags = 16384
)

// PlotPaperUnits is ACadSharp's PlotPaperUnits.
type PlotPaperUnits int32

const (
	PlotPaperUnitsInches      PlotPaperUnits = 0
	PlotPaperUnitsMillimeters PlotPaperUnits = 1
	PlotPaperUnitsPixels      PlotPaperUnits = 2
)

// PlotRotation is ACadSharp's PlotRotation.
type PlotRotation int32

const (
	PlotRotationNoRotation PlotRotation = 0
	PlotRotationDegrees90  PlotRotation = 1
	PlotRotationDegrees180 PlotRotation = 2
	PlotRotationDegrees270 PlotRotation = 3
)

// PlotType is ACadSharp's PlotType.
type PlotType int32

const (
	PlotTypeLastScreenDisplay PlotType = 0
	PlotTypeDrawingExtents    PlotType = 1
	PlotTypeDrawingLimits     PlotType = 2
	PlotTypeView              PlotType = 3
	PlotTypeWindow            PlotType = 4
	PlotTypeLayoutInformation PlotType = 5
)

// PolylineFlags is ACadSharp's PolylineFlags.
type PolylineFlags int32

const (
	PolylineFlagsDefault                              PolylineFlags = 0
	PolylineFlagsClosedPolylineOrClosedPolygonMeshInM PolylineFlags = 1
	PolylineFlagsCurveFit                             PolylineFlags = 2
	PolylineFlagsSplineFit                            PolylineFlags = 4
	PolylineFlagsPolyline3D                           PolylineFlags = 8
	PolylineFlagsPolygonMesh                          PolylineFlags = 16
	PolylineFlagsClosedPolygonMeshInN                 PolylineFlags = 32
	PolylineFlagsPolyfaceMesh                         PolylineFlags = 64
	PolylineFlagsContinuousLinetypePattern            PolylineFlags = 128
)

// ProjectionMethod is ACadSharp's ProjectionMethod.
type ProjectionMethod uint8

const (
	ProjectionMethodNone     ProjectionMethod = 0
	ProjectionMethodPlanar   ProjectionMethod = 1
	ProjectionMethodBox      ProjectionMethod = 2
	ProjectionMethodCylinder ProjectionMethod = 3
	ProjectionMethodSphere   ProjectionMethod = 4
)

// ProxyFlags is ACadSharp's ProxyFlags.
type ProxyFlags uint16

const (
	ProxyFlagsNone                              ProxyFlags = 0
	ProxyFlagsEraseAllowed                      ProxyFlags = 1
	ProxyFlagsTransformAllowed                  ProxyFlags = 2
	ProxyFlagsColorChangeAllowed                ProxyFlags = 4
	ProxyFlagsLayerChangeAllowed                ProxyFlags = 8
	ProxyFlagsLinetypeChangeAllowed             ProxyFlags = 16
	ProxyFlagsLinetypeScaleChangeAllowed        ProxyFlags = 32
	ProxyFlagsVisibilityChangeAllowed           ProxyFlags = 64
	ProxyFlagsCloningAllowed                    ProxyFlags = 128
	ProxyFlagsLineweightChangeAllowed           ProxyFlags = 256
	ProxyFlagsPlotStyleNameChangeAllowed        ProxyFlags = 512
	ProxyFlagsAllOperationsExceptCloningAllowed ProxyFlags = 895
	ProxyFlagsAllOperationsAllowed              ProxyFlags = 1023
	ProxyFlagsDisablesProxyWarningDialog        ProxyFlags = 1024
	ProxyFlagsR13FormatProxy                    ProxyFlags = 32768
)

// RenderMode is ACadSharp's RenderMode.
type RenderMode int32

const (
	RenderModeOptimized2D                RenderMode = 0
	RenderModeWireframe                  RenderMode = 1
	RenderModeHiddenLine                 RenderMode = 2
	RenderModeFlatShaded                 RenderMode = 3
	RenderModeGouraudShaded              RenderMode = 4
	RenderModeFlatShadedWithWireframe    RenderMode = 5
	RenderModeGouraudShadedWithWireframe RenderMode = 6
)

// ResolutionUnit is ACadSharp's ResolutionUnit.
type ResolutionUnit uint8

const (
	ResolutionUnitNone        ResolutionUnit = 0
	ResolutionUnitCentimeters ResolutionUnit = 2
	ResolutionUnitInches      ResolutionUnit = 5
)

// RotatedDimensionType is ACadSharp's RotatedDimensionType.
type RotatedDimensionType int16

const (
	RotatedDimensionTypeUnknown       RotatedDimensionType = 0
	RotatedDimensionTypeParallel      RotatedDimensionType = 1
	RotatedDimensionTypePerpendicular RotatedDimensionType = 2
)

// ScaleEstimationType is ACadSharp's ScaleEstimationType.
type ScaleEstimationType int32

const (
	ScaleEstimationTypeNone                      ScaleEstimationType = 1
	ScaleEstimationTypeUserSpecifiedScaleFactor  ScaleEstimationType = 2
	ScaleEstimationTypeGridScaleAtReferencePoint ScaleEstimationType = 3
	ScaleEstimationTypePrismodial                ScaleEstimationType = 4
)

// ScaledType is ACadSharp's ScaledType.
type ScaledType int32

const (
	ScaledTypeScaledToFit ScaledType = 0
	ScaledType1           ScaledType = 1
	ScaledType2           ScaledType = 2
	ScaledType3           ScaledType = 3
	ScaledType4           ScaledType = 4
	ScaledType5           ScaledType = 5
	ScaledType6           ScaledType = 6
	ScaledType7           ScaledType = 7
	ScaledType8           ScaledType = 8
	ScaledType9           ScaledType = 9
	ScaledType10          ScaledType = 10
	ScaledType11          ScaledType = 11
	ScaledType12          ScaledType = 12
	ScaledType13          ScaledType = 13
	ScaledType14          ScaledType = 14
	ScaledType15          ScaledType = 15
	ScaledType16          ScaledType = 16
	ScaledType17          ScaledType = 17
	ScaledType18          ScaledType = 18
	ScaledType19          ScaledType = 19
	ScaledType20          ScaledType = 20
	ScaledType21          ScaledType = 21
	ScaledType22          ScaledType = 22
	ScaledType23          ScaledType = 23
	ScaledType24          ScaledType = 24
	ScaledType25          ScaledType = 25
	ScaledType26          ScaledType = 26
	ScaledType27          ScaledType = 27
	ScaledType28          ScaledType = 28
	ScaledType29          ScaledType = 29
	ScaledType30          ScaledType = 30
	ScaledType31          ScaledType = 31
	ScaledType32          ScaledType = 32
)

// ShadeEdgeType is ACadSharp's ShadeEdgeType.
type ShadeEdgeType int16

const (
	ShadeEdgeTypeFacesShadedEdgesNotHighlighted     ShadeEdgeType = 0
	ShadeEdgeTypeFacesShadedEdgesHighlightedInBlack ShadeEdgeType = 1
	ShadeEdgeTypeFacesNotFilledEdgesInEntityColor   ShadeEdgeType = 2
	ShadeEdgeTypeFacesInEntityColorEdgesInBlack     ShadeEdgeType = 3
)

// ShadePlotMode is ACadSharp's ShadePlotMode.
type ShadePlotMode int32

const (
	ShadePlotModeAsDisplayed ShadePlotMode = 0
	ShadePlotModeWireframe   ShadePlotMode = 1
	ShadePlotModeHidden      ShadePlotMode = 2
	ShadePlotModeRendered    ShadePlotMode = 3
)

// ShadePlotResolutionMode is ACadSharp's ShadePlotResolutionMode.
type ShadePlotResolutionMode uint16

const (
	ShadePlotResolutionModeDraft        ShadePlotResolutionMode = 0
	ShadePlotResolutionModePreview      ShadePlotResolutionMode = 1
	ShadePlotResolutionModeNormal       ShadePlotResolutionMode = 2
	ShadePlotResolutionModePresentation ShadePlotResolutionMode = 3
	ShadePlotResolutionModeMaximum      ShadePlotResolutionMode = 4
	ShadePlotResolutionModeCustom       ShadePlotResolutionMode = 5
)

// ShadowMode is ACadSharp's ShadowMode.
type ShadowMode uint8

const (
	ShadowModeCastsAndReceivesShadows ShadowMode = 0
	ShadowModeCastsShadows            ShadowMode = 1
	ShadowModeReceivesShadows         ShadowMode = 2
	ShadowModeIgnoresShadows          ShadowMode = 3
	ShadowModeCastsAndReceives        ShadowMode = 0
	ShadowModeCasts                   ShadowMode = 1
	ShadowModeReceives                ShadowMode = 2
	ShadowModeIgnores                 ShadowMode = 3
)

// SmoothSurfaceType is ACadSharp's SmoothSurfaceType.
type SmoothSurfaceType int16

const (
	SmoothSurfaceTypeNoSmooth      SmoothSurfaceType = 0
	SmoothSurfaceTypeQuadratic     SmoothSurfaceType = 5
	SmoothSurfaceTypeCubic         SmoothSurfaceType = 6
	SmoothSurfaceTypeBezierSurface SmoothSurfaceType = 8
)

// SpaceLineTypeScaling is ACadSharp's SpaceLineTypeScaling.
type SpaceLineTypeScaling int16

const (
	SpaceLineTypeScalingViewport SpaceLineTypeScaling = 0
	SpaceLineTypeScalingNormal   SpaceLineTypeScaling = 1
)

// SplineFlags is ACadSharp's SplineFlags.
type SplineFlags uint16

const (
	SplineFlagsNone     SplineFlags = 0
	SplineFlagsClosed   SplineFlags = 1
	SplineFlagsPeriodic SplineFlags = 2
	SplineFlagsRational SplineFlags = 4
	SplineFlagsPlanar   SplineFlags = 8
	SplineFlagsLinear   SplineFlags = 16
)

// SplineFlags1 is ACadSharp's SplineFlags1.
type SplineFlags1 uint16

const (
	SplineFlags1None             SplineFlags1 = 0
	SplineFlags1MethodFitPoints  SplineFlags1 = 1
	SplineFlags1CVFrameShow      SplineFlags1 = 2
	SplineFlags1Closed           SplineFlags1 = 4
	SplineFlags1UseKnotParameter SplineFlags1 = 8
)

// SplineType is ACadSharp's SplineType.
type SplineType int16

const (
	SplineTypeNone             SplineType = 0
	SplineTypeQuadraticBSpline SplineType = 5
	SplineTypeCubicBSpline     SplineType = 6
	SplineTypeBezier           SplineType = 8
)

// StandardFlags is ACadSharp's StandardFlags.
type StandardFlags int16

const (
	StandardFlagsNone          StandardFlags = 0
	StandardFlagsXrefDependent StandardFlags = 16
	StandardFlagsXrefResolved  StandardFlags = 32
	StandardFlagsReferenced    StandardFlags = 64
)

// StyleFlags is ACadSharp's StyleFlags.
type StyleFlags int32

const (
	StyleFlagsNone          StyleFlags = 0
	StyleFlagsIsShape       StyleFlags = 1
	StyleFlagsVerticalText  StyleFlags = 4
	StyleFlagsXrefDependent StyleFlags = 16
	StyleFlagsXrefResolved  StyleFlags = 32
	StyleFlagsReferenced    StyleFlags = 64
)

// SubentType is ACadSharp's SubentType.
type SubentType int16

const (
	SubentTypeUnknown SubentType = 0
	SubentTypeEdge    SubentType = 1
	SubentTypeFace    SubentType = 2
)

// TableBorderOverrideFlags is ACadSharp's TableEntity.BorderOverrideFlags.
type TableBorderOverrideFlags int32

const (
	TableBorderOverrideFlagsNone                   TableBorderOverrideFlags = 0
	TableBorderOverrideFlagsTitleHorizontalTop     TableBorderOverrideFlags = 0x01
	TableBorderOverrideFlagsTitleHorizontalInsert  TableBorderOverrideFlags = 0x02
	TableBorderOverrideFlagsTitleHorizontalBottom  TableBorderOverrideFlags = 0x04
	TableBorderOverrideFlagsTitleVerticalLeft      TableBorderOverrideFlags = 0x8
	TableBorderOverrideFlagsTitleVerticalInsert    TableBorderOverrideFlags = 0x10
	TableBorderOverrideFlagsTitleVerticalRight     TableBorderOverrideFlags = 0x20
	TableBorderOverrideFlagsHeaderHorizontalTop    TableBorderOverrideFlags = 0x40
	TableBorderOverrideFlagsHeaderHorizontalInsert TableBorderOverrideFlags = 0x80
	TableBorderOverrideFlagsHeaderHorizontalBottom TableBorderOverrideFlags = 0x100
	TableBorderOverrideFlagsHeaderVerticalLeft     TableBorderOverrideFlags = 0x200
	TableBorderOverrideFlagsHeaderVerticalInsert   TableBorderOverrideFlags = 0x400
	TableBorderOverrideFlagsHeaderVerticalRight    TableBorderOverrideFlags = 0x800
	TableBorderOverrideFlagsDataHorizontalTop      TableBorderOverrideFlags = 0x1000
	TableBorderOverrideFlagsDataHorizontalInsert   TableBorderOverrideFlags = 0x2000
	TableBorderOverrideFlagsDataHorizontalBottom   TableBorderOverrideFlags = 0x4000
	TableBorderOverrideFlagsDataVerticalLeft       TableBorderOverrideFlags = 0x8000
	TableBorderOverrideFlagsDataVerticalInsert     TableBorderOverrideFlags = 0x10000
	TableBorderOverrideFlagsDataVerticalRight      TableBorderOverrideFlags = 0x20000
)

// TableBreakFlowDirection is ACadSharp's TableEntity.BreakFlowDirection.
type TableBreakFlowDirection int32

const (
	TableBreakFlowDirectionRight    TableBreakFlowDirection = 1
	TableBreakFlowDirectionVertical TableBreakFlowDirection = 2
	TableBreakFlowDirectionLeft     TableBreakFlowDirection = 4
)

// TableBreakOptionFlags is ACadSharp's TableEntity.BreakOptionFlags.
type TableBreakOptionFlags int32

const (
	TableBreakOptionFlagsNone                 TableBreakOptionFlags = 0
	TableBreakOptionFlagsEnableBreaks         TableBreakOptionFlags = 1
	TableBreakOptionFlagsRepeatTopLabels      TableBreakOptionFlags = 2
	TableBreakOptionFlagsRepeatBottomLabels   TableBreakOptionFlags = 4
	TableBreakOptionFlagsAllowManualPositions TableBreakOptionFlags = 8
	TableBreakOptionFlagsAllowManualHeights   TableBreakOptionFlags = 16
)

// TableCellOverrideFlags is ACadSharp's TableEntity.Cell.OverrideFlags.
type TableCellOverrideFlags int32

const (
	TableCellOverrideFlagsNone                 TableCellOverrideFlags = 0
	TableCellOverrideFlagsCellAlignment        TableCellOverrideFlags = 0x01
	TableCellOverrideFlagsBackgroundFillNone   TableCellOverrideFlags = 0x02
	TableCellOverrideFlagsBackgroundColor      TableCellOverrideFlags = 0x04
	TableCellOverrideFlagsContentColor         TableCellOverrideFlags = 0x08
	TableCellOverrideFlagsTextStyle            TableCellOverrideFlags = 0x10
	TableCellOverrideFlagsTextHeight           TableCellOverrideFlags = 0x20
	TableCellOverrideFlagsTopGridColor         TableCellOverrideFlags = 0x00040
	TableCellOverrideFlagsTopGridLineWeight    TableCellOverrideFlags = 0x00400
	TableCellOverrideFlagsTopVisibility        TableCellOverrideFlags = 0x04000
	TableCellOverrideFlagsRightGridColor       TableCellOverrideFlags = 0x00080
	TableCellOverrideFlagsRightGridLineWeight  TableCellOverrideFlags = 0x00800
	TableCellOverrideFlagsRightVisibility      TableCellOverrideFlags = 0x08000
	TableCellOverrideFlagsBottomGridColor      TableCellOverrideFlags = 0x00100
	TableCellOverrideFlagsBottomGridLineWeight TableCellOverrideFlags = 0x01000
	TableCellOverrideFlagsBottomVisibility     TableCellOverrideFlags = 0x10000
	TableCellOverrideFlagsLeftGridColor        TableCellOverrideFlags = 0x00200
	TableCellOverrideFlagsLeftGridLineWeight   TableCellOverrideFlags = 0x02000
	TableCellOverrideFlagsLeftVisibility       TableCellOverrideFlags = 0x20000
)

// TableCellType is ACadSharp's TableEntity.CellType.
type TableCellType int32

const (
	TableCellTypeText  TableCellType = 1
	TableCellTypeBlock TableCellType = 2
)

// TableTableCellContentType is ACadSharp's TableEntity.TableCellContentType.
type TableTableCellContentType int32

const (
	TableTableCellContentTypeUnknown TableTableCellContentType = 0
	TableTableCellContentTypeValue   TableTableCellContentType = 0x1
	TableTableCellContentTypeField   TableTableCellContentType = 0x2
	TableTableCellContentTypeBlock   TableTableCellContentType = 0x4
)

// TableTableCellStateFlags is ACadSharp's TableEntity.TableCellStateFlags.
type TableTableCellStateFlags int32

const (
	TableTableCellStateFlagsNone                       TableTableCellStateFlags = 0x0
	TableTableCellStateFlagsContentLocked              TableTableCellStateFlags = 0x1
	TableTableCellStateFlagsContentReadOnly            TableTableCellStateFlags = 0x2
	TableTableCellStateFlagsLinked                     TableTableCellStateFlags = 0x4
	TableTableCellStateFlagsContentModifiedAfterUpdate TableTableCellStateFlags = 0x8
	TableTableCellStateFlagsFormatLocked               TableTableCellStateFlags = 0x10
	TableTableCellStateFlagsFormatReadOnly             TableTableCellStateFlags = 0x20
	TableTableCellStateFlagsFormatModifiedAfterUpdate  TableTableCellStateFlags = 0x40
)

// TableTableOverrideFlags is ACadSharp's TableEntity.TableOverrideFlags.
type TableTableOverrideFlags int32

const (
	TableTableOverrideFlagsNone                 TableTableOverrideFlags = 0
	TableTableOverrideFlagsTitleSuppressed      TableTableOverrideFlags = 0x0001
	TableTableOverrideFlagsHeaderSuppressed     TableTableOverrideFlags = 0x02
	TableTableOverrideFlagsFlowDirection        TableTableOverrideFlags = 0x0004
	TableTableOverrideFlagsHorizontalCellMargin TableTableOverrideFlags = 0x0008
	TableTableOverrideFlagsVerticalCellMargin   TableTableOverrideFlags = 0x0010
	TableTableOverrideFlagsTitleRowColor        TableTableOverrideFlags = 0x0020
	TableTableOverrideFlagsHeaderRowColor       TableTableOverrideFlags = 0x00040
	TableTableOverrideFlagsDataRowColor         TableTableOverrideFlags = 0x0080
	TableTableOverrideFlagsTitleRowFillNone     TableTableOverrideFlags = 0x0100
	TableTableOverrideFlagsHeaderRowFillNone    TableTableOverrideFlags = 0x0200
	TableTableOverrideFlagsDataRowFillNone      TableTableOverrideFlags = 0x0400
	TableTableOverrideFlagsTitleRowFillColor    TableTableOverrideFlags = 0x0800
	TableTableOverrideFlagsHeaderRowFillColor   TableTableOverrideFlags = 0x1000
	TableTableOverrideFlagsDataRowFillColor     TableTableOverrideFlags = 0x2000
	TableTableOverrideFlagsTitleRowAlign        TableTableOverrideFlags = 0x4000
	TableTableOverrideFlagsHeaderRowAlign       TableTableOverrideFlags = 0x8000
	TableTableOverrideFlagsDataRowAlign         TableTableOverrideFlags = 0x10000
	TableTableOverrideFlagsTitleTextStyle       TableTableOverrideFlags = 0x20000
	TableTableOverrideFlagsHeaderTextStyle      TableTableOverrideFlags = 0x40000
	TableTableOverrideFlagsDataTextStyle        TableTableOverrideFlags = 0x80000
	TableTableOverrideFlagsTitleRowHeight       TableTableOverrideFlags = 0x100000
	TableTableOverrideFlagsHeaderRowHeight      TableTableOverrideFlags = 0x200000
	TableTableOverrideFlagsDataRowHeight        TableTableOverrideFlags = 0x400000
)

// TableValueUnitType is ACadSharp's TableEntity.ValueUnitType.
type TableValueUnitType int32

const (
	TableValueUnitTypeNoUnits    TableValueUnitType = 0
	TableValueUnitTypeDistance   TableValueUnitType = 1
	TableValueUnitTypeAngle      TableValueUnitType = 2
	TableValueUnitTypeArea       TableValueUnitType = 4
	TableValueUnitTypeVolume     TableValueUnitType = 8
	TableValueUnitTypeCurrency   TableValueUnitType = 0x10
	TableValueUnitTypePercentage TableValueUnitType = 0x20
)

// TableFlowDirectionType is ACadSharp's TableFlowDirectionType.
type TableFlowDirectionType int32

const (
	TableFlowDirectionTypeDown TableFlowDirectionType = 0
	TableFlowDirectionTypeUp   TableFlowDirectionType = 1
)

// TableStyleBorderPropertyFlags is ACadSharp's TableStyle.BorderPropertyFlags.
type TableStyleBorderPropertyFlags int32

const (
	TableStyleBorderPropertyFlagsNone              TableStyleBorderPropertyFlags = 0x0
	TableStyleBorderPropertyFlagsBorderType        TableStyleBorderPropertyFlags = 0x1
	TableStyleBorderPropertyFlagsLineWeight        TableStyleBorderPropertyFlags = 0x2
	TableStyleBorderPropertyFlagsLineType          TableStyleBorderPropertyFlags = 0x4
	TableStyleBorderPropertyFlagsColor             TableStyleBorderPropertyFlags = 0x8
	TableStyleBorderPropertyFlagsInvisibility      TableStyleBorderPropertyFlags = 0x10
	TableStyleBorderPropertyFlagsDoubleLineSpacing TableStyleBorderPropertyFlags = 0x20
	TableStyleBorderPropertyFlagsAll               TableStyleBorderPropertyFlags = 0x3F
)

// TableStyleBorderType is ACadSharp's TableStyle.BorderType.
type TableStyleBorderType int16

const (
	TableStyleBorderTypeSingle TableStyleBorderType = 1
	TableStyleBorderTypeDouble TableStyleBorderType = 2
)

// TableStyleCellAlignmentType is ACadSharp's TableStyle.CellAlignmentType.
type TableStyleCellAlignmentType int32

const (
	TableStyleCellAlignmentTypeNone         TableStyleCellAlignmentType = 0
	TableStyleCellAlignmentTypeTopLeft      TableStyleCellAlignmentType = 1
	TableStyleCellAlignmentTypeTopCenter    TableStyleCellAlignmentType = 2
	TableStyleCellAlignmentTypeTopRight     TableStyleCellAlignmentType = 3
	TableStyleCellAlignmentTypeMiddleLeft   TableStyleCellAlignmentType = 4
	TableStyleCellAlignmentTypeMiddleCenter TableStyleCellAlignmentType = 5
	TableStyleCellAlignmentTypeMiddleRight  TableStyleCellAlignmentType = 6
	TableStyleCellAlignmentTypeBottomLeft   TableStyleCellAlignmentType = 7
	TableStyleCellAlignmentTypeBottomCenter TableStyleCellAlignmentType = 8
	TableStyleCellAlignmentTypeBottomRight  TableStyleCellAlignmentType = 9
)

// TableStyleCellContentLayoutFlags is ACadSharp's TableStyle.CellContentLayoutFlags.
type TableStyleCellContentLayoutFlags int32

const (
	TableStyleCellContentLayoutFlagsNone              TableStyleCellContentLayoutFlags = 0
	TableStyleCellContentLayoutFlagsFlow              TableStyleCellContentLayoutFlags = 1
	TableStyleCellContentLayoutFlagsStackedHorizontal TableStyleCellContentLayoutFlags = 2
	TableStyleCellContentLayoutFlagsStackedVertical   TableStyleCellContentLayoutFlags = 4
)

// TableStyleCellEdgeFlags is ACadSharp's TableStyle.CellEdgeFlags.
type TableStyleCellEdgeFlags int32

const (
	TableStyleCellEdgeFlagsUnknown          TableStyleCellEdgeFlags = 0
	TableStyleCellEdgeFlagsTop              TableStyleCellEdgeFlags = 1
	TableStyleCellEdgeFlagsRight            TableStyleCellEdgeFlags = 2
	TableStyleCellEdgeFlagsBottom           TableStyleCellEdgeFlags = 4
	TableStyleCellEdgeFlagsLeft             TableStyleCellEdgeFlags = 8
	TableStyleCellEdgeFlagsInsideVertical   TableStyleCellEdgeFlags = 16
	TableStyleCellEdgeFlagsInsideHorizontal TableStyleCellEdgeFlags = 32
)

// TableStyleCellStyleClass is ACadSharp's TableStyle.CellStyleClass.
type TableStyleCellStyleClass int32

const (
	TableStyleCellStyleClassData  TableStyleCellStyleClass = 1
	TableStyleCellStyleClassLabel TableStyleCellStyleClass = 2
)

// TableStyleCellStylePropertyFlags is ACadSharp's TableStyle.CellStylePropertyFlags.
type TableStyleCellStylePropertyFlags int32

const (
	TableStyleCellStylePropertyFlagsNone                     TableStyleCellStylePropertyFlags = 0x0
	TableStyleCellStylePropertyFlagsDataType                 TableStyleCellStylePropertyFlags = 0x1
	TableStyleCellStylePropertyFlagsDataFormat               TableStyleCellStylePropertyFlags = 0x2
	TableStyleCellStylePropertyFlagsRotation                 TableStyleCellStylePropertyFlags = 0x4
	TableStyleCellStylePropertyFlagsBlockScale               TableStyleCellStylePropertyFlags = 0x8
	TableStyleCellStylePropertyFlagsAlignment                TableStyleCellStylePropertyFlags = 0x10
	TableStyleCellStylePropertyFlagsContentColor             TableStyleCellStylePropertyFlags = 0x20
	TableStyleCellStylePropertyFlagsTextStyle                TableStyleCellStylePropertyFlags = 0x40
	TableStyleCellStylePropertyFlagsTextHeight               TableStyleCellStylePropertyFlags = 0x80
	TableStyleCellStylePropertyFlagsAutoScale                TableStyleCellStylePropertyFlags = 0x100
	TableStyleCellStylePropertyFlagsBackgroundColor          TableStyleCellStylePropertyFlags = 0x200
	TableStyleCellStylePropertyFlagsMarginLeft               TableStyleCellStylePropertyFlags = 0x400
	TableStyleCellStylePropertyFlagsMarginTop                TableStyleCellStylePropertyFlags = 0x800
	TableStyleCellStylePropertyFlagsMarginRight              TableStyleCellStylePropertyFlags = 0x1000
	TableStyleCellStylePropertyFlagsMarginBottom             TableStyleCellStylePropertyFlags = 0x2000
	TableStyleCellStylePropertyFlagsContentLayout            TableStyleCellStylePropertyFlags = 0x4000
	TableStyleCellStylePropertyFlagsMarginHorizontalSpacing  TableStyleCellStylePropertyFlags = 0x20000
	TableStyleCellStylePropertyFlagsMarginVerticalSpacing    TableStyleCellStylePropertyFlags = 0x40000
	TableStyleCellStylePropertyFlagsMergeAll                 TableStyleCellStylePropertyFlags = 0x8000
	TableStyleCellStylePropertyFlagsFlowDirectionBottomToTop TableStyleCellStylePropertyFlags = 0x10000
)

// TableStyleCellStyleType is ACadSharp's TableStyle.CellStyleType.
type TableStyleCellStyleType int32

const (
	TableStyleCellStyleTypeUnknown            TableStyleCellStyleType = 0
	TableStyleCellStyleTypeCell               TableStyleCellStyleType = 1
	TableStyleCellStyleTypeRow                TableStyleCellStyleType = 2
	TableStyleCellStyleTypeColumn             TableStyleCellStyleType = 3
	TableStyleCellStyleTypeFormattedTableData TableStyleCellStyleType = 4
	TableStyleCellStyleTypeTable              TableStyleCellStyleType = 5
)

// TableStyleMarginFlags is ACadSharp's TableStyle.MarginFlags.
type TableStyleMarginFlags int32

const (
	TableStyleMarginFlagsNone     TableStyleMarginFlags = 0
	TableStyleMarginFlagsOverride TableStyleMarginFlags = 1
)

// TextAlignmentType is ACadSharp's TextAlignmentType.
type TextAlignmentType int16

const (
	TextAlignmentTypeLeft   TextAlignmentType = 0
	TextAlignmentTypeCenter TextAlignmentType = 1
	TextAlignmentTypeRight  TextAlignmentType = 2
)

// TextAngleType is ACadSharp's TextAngleType.
type TextAngleType int16

const (
	TextAngleTypeParllelToLastLeaderLine TextAngleType = 0
	TextAngleTypeHorizontal              TextAngleType = 1
	TextAngleTypeOptimized               TextAngleType = 2
)

// TextArrowFitType is ACadSharp's TextArrowFitType.
type TextArrowFitType uint8

const (
	TextArrowFitTypeBoth        TextArrowFitType = 0
	TextArrowFitTypeArrowsFirst TextArrowFitType = 1
	TextArrowFitTypeTextFirst   TextArrowFitType = 2
	TextArrowFitTypeBestFit     TextArrowFitType = 3
)

// TextAttachmentDirectionType is ACadSharp's TextAttachmentDirectionType.
type TextAttachmentDirectionType int16

const (
	TextAttachmentDirectionTypeHorizontal TextAttachmentDirectionType = 0
	TextAttachmentDirectionTypeVertical   TextAttachmentDirectionType = 1
)

// TextAttachmentPointType is ACadSharp's TextAttachmentPointType.
type TextAttachmentPointType int16

const (
	TextAttachmentPointTypeLeft   TextAttachmentPointType = 1
	TextAttachmentPointTypeCenter TextAttachmentPointType = 2
	TextAttachmentPointTypeRight  TextAttachmentPointType = 3
)

// TextAttachmentType is ACadSharp's TextAttachmentType.
type TextAttachmentType int16

const (
	TextAttachmentTypeTopOfTopLine                       TextAttachmentType = 0
	TextAttachmentTypeMiddleOfTopLine                    TextAttachmentType = 1
	TextAttachmentTypeMiddleOfText                       TextAttachmentType = 2
	TextAttachmentTypeMiddleOfBottomLine                 TextAttachmentType = 3
	TextAttachmentTypeBottomOfBottomLine                 TextAttachmentType = 4
	TextAttachmentTypeBottomLine                         TextAttachmentType = 5
	TextAttachmentTypeBottomOfTopLineUnderlineBottomLine TextAttachmentType = 6
	TextAttachmentTypeBottomOfTopLineUnderlineTopLine    TextAttachmentType = 7
	TextAttachmentTypeBottomofTopLineUnderlineAll        TextAttachmentType = 8
	TextAttachmentTypeCenterOfText                       TextAttachmentType = 9
	TextAttachmentTypeCenterOfTextOverline               TextAttachmentType = 10
)

// TextDirection is ACadSharp's TextDirection.
type TextDirection uint8

const (
	TextDirectionLeftToRight TextDirection = 0
	TextDirectionRightToLeft TextDirection = 1
)

// TextHorizontalAlignment is ACadSharp's TextHorizontalAlignment.
type TextHorizontalAlignment int16

const (
	TextHorizontalAlignmentLeft    TextHorizontalAlignment = 0
	TextHorizontalAlignmentCenter  TextHorizontalAlignment = 1
	TextHorizontalAlignmentRight   TextHorizontalAlignment = 2
	TextHorizontalAlignmentAligned TextHorizontalAlignment = 3
	TextHorizontalAlignmentMiddle  TextHorizontalAlignment = 4
	TextHorizontalAlignmentFit     TextHorizontalAlignment = 5
)

// TextMirrorFlag is ACadSharp's TextMirrorFlag.
type TextMirrorFlag int16

const (
	TextMirrorFlagNone       TextMirrorFlag = 0
	TextMirrorFlagBackward   TextMirrorFlag = 2
	TextMirrorFlagUpsideDown TextMirrorFlag = 4
)

// TextMovement is ACadSharp's TextMovement.
type TextMovement int16

const (
	TextMovementMoveLineWithText       TextMovement = 0
	TextMovementAddLeaderWhenTextMoved TextMovement = 1
	TextMovementFreeTextPosition       TextMovement = 2
)

// TextVerticalAlignmentType is ACadSharp's TextVerticalAlignmentType.
type TextVerticalAlignmentType int16

const (
	TextVerticalAlignmentTypeBaseline TextVerticalAlignmentType = 0
	TextVerticalAlignmentTypeBottom   TextVerticalAlignmentType = 1
	TextVerticalAlignmentTypeMiddle   TextVerticalAlignmentType = 2
	TextVerticalAlignmentTypeTop      TextVerticalAlignmentType = 3
)

// TilingMethod is ACadSharp's TilingMethod.
type TilingMethod uint8

const (
	TilingMethodNone  TilingMethod = 0
	TilingMethodTile  TilingMethod = 1
	TilingMethodCrop  TilingMethod = 2
	TilingMethodClamp TilingMethod = 3
)

// ToleranceAlignment is ACadSharp's ToleranceAlignment.
type ToleranceAlignment uint8

const (
	ToleranceAlignmentBottom ToleranceAlignment = 0
	ToleranceAlignmentMiddle ToleranceAlignment = 1
	ToleranceAlignmentTop    ToleranceAlignment = 2
)

// UnderlayDisplayFlags is ACadSharp's UnderlayDisplayFlags.
type UnderlayDisplayFlags uint8

const (
	UnderlayDisplayFlagsClippingOn          UnderlayDisplayFlags = 1
	UnderlayDisplayFlagsShowUnderlay        UnderlayDisplayFlags = 2
	UnderlayDisplayFlagsMonochrome          UnderlayDisplayFlags = 4
	UnderlayDisplayFlagsAdjustForBackground UnderlayDisplayFlags = 8
	UnderlayDisplayFlagsClipInsideMode      UnderlayDisplayFlags = 16
	UnderlayDisplayFlagsDefault             UnderlayDisplayFlags = 0xB
)

// UnitsType is ACadSharp's UnitsType.
type UnitsType int16

const (
	UnitsTypeUnitless          UnitsType = 0
	UnitsTypeInches            UnitsType = 1
	UnitsTypeFeet              UnitsType = 2
	UnitsTypeMiles             UnitsType = 3
	UnitsTypeMillimeters       UnitsType = 4
	UnitsTypeCentimeters       UnitsType = 5
	UnitsTypeMeters            UnitsType = 6
	UnitsTypeKilometers        UnitsType = 7
	UnitsTypeMicroinches       UnitsType = 8
	UnitsTypeMils              UnitsType = 9
	UnitsTypeYards             UnitsType = 10
	UnitsTypeAngstroms         UnitsType = 11
	UnitsTypeNanometers        UnitsType = 12
	UnitsTypeMicrons           UnitsType = 13
	UnitsTypeDecimeters        UnitsType = 14
	UnitsTypeDecameters        UnitsType = 15
	UnitsTypeHectometers       UnitsType = 16
	UnitsTypeGigameters        UnitsType = 17
	UnitsTypeAstronomicalUnits UnitsType = 18
	UnitsTypeLightYears        UnitsType = 19
	UnitsTypeParsecs           UnitsType = 20
	UnitsTypeUSSurveyFeet      UnitsType = 21
	UnitsTypeUSSurveyInches    UnitsType = 22
	UnitsTypeUSSurveyYards     UnitsType = 23
	UnitsTypeUSSurveyMiles     UnitsType = 24
)

// UscIconType is ACadSharp's UscIconType.
type UscIconType int16

const (
	UscIconTypeOff       UscIconType = 0
	UscIconTypeOnLower   UscIconType = 1
	UscIconTypeOffOrigin UscIconType = 2
	UscIconTypeOnOrigin  UscIconType = 3
)

// VertexFlags is ACadSharp's VertexFlags.
type VertexFlags int32

const (
	VertexFlagsDefault                       VertexFlags = 0
	VertexFlagsCurveFittingExtraVertex       VertexFlags = 1
	VertexFlagsCurveFitTangent               VertexFlags = 2
	VertexFlagsNotUsed                       VertexFlags = 4
	VertexFlagsSplineVertexFromSplineFitting VertexFlags = 8
	VertexFlagsSplineFrameControlPoint       VertexFlags = 16
	VertexFlagsPolylineVertex3D              VertexFlags = 32
	VertexFlagsPolygonMesh3D                 VertexFlags = 64
	VertexFlagsPolyFaceMeshVertex            VertexFlags = 128
)

// VerticalAlignmentType is ACadSharp's VerticalAlignmentType.
type VerticalAlignmentType int16

const (
	VerticalAlignmentTypeTop    VerticalAlignmentType = 0
	VerticalAlignmentTypeMiddle VerticalAlignmentType = 1
	VerticalAlignmentTypeBottom VerticalAlignmentType = 2
)

// ViewModeType is ACadSharp's ViewModeType.
type ViewModeType int32

const (
	ViewModeTypeOff             ViewModeType = 0
	ViewModeTypePerspectiveView ViewModeType = 1
	ViewModeTypeFrontClipping   ViewModeType = 2
	ViewModeTypeBackClipping    ViewModeType = 4
	ViewModeTypeFollow          ViewModeType = 8
	ViewModeTypeFrontClippingZ  ViewModeType = 16
)

// ViewportStatusFlags is ACadSharp's ViewportStatusFlags.
type ViewportStatusFlags int32

const (
	ViewportStatusFlagsPerspectiveMode                ViewportStatusFlags = 1
	ViewportStatusFlagsFrontClipping                  ViewportStatusFlags = 2
	ViewportStatusFlagsBackClipping                   ViewportStatusFlags = 4
	ViewportStatusFlagsUcsFollow                      ViewportStatusFlags = 8
	ViewportStatusFlagsFrontClipNotAtEye              ViewportStatusFlags = 16
	ViewportStatusFlagsUcsIconVisibility              ViewportStatusFlags = 32
	ViewportStatusFlagsUcsIconAtOrigin                ViewportStatusFlags = 64
	ViewportStatusFlagsFastZoom                       ViewportStatusFlags = 128
	ViewportStatusFlagsSnapMode                       ViewportStatusFlags = 256
	ViewportStatusFlagsGridMode                       ViewportStatusFlags = 512
	ViewportStatusFlagsIsometricSnapStyle             ViewportStatusFlags = 1024
	ViewportStatusFlagsHidePlotMode                   ViewportStatusFlags = 2048
	ViewportStatusFlagsIsoPairTop                     ViewportStatusFlags = 4096
	ViewportStatusFlagsIsoPairRight                   ViewportStatusFlags = 8192
	ViewportStatusFlagsViewportZoomLocking            ViewportStatusFlags = 16384
	ViewportStatusFlagsCurrentlyAlwaysEnabled         ViewportStatusFlags = 32768
	ViewportStatusFlagsNonRectangularClipping         ViewportStatusFlags = 65536
	ViewportStatusFlagsViewportOff                    ViewportStatusFlags = 131072
	ViewportStatusFlagsDisplayGridBeyondDrawingLimits ViewportStatusFlags = 262144
	ViewportStatusFlagsAdaptiveGridDisplay            ViewportStatusFlags = 524288
	ViewportStatusFlagsSubdivisionGridBelowSpacing    ViewportStatusFlags = 1048576
)

// WipeoutFrameType is ACadSharp's WipeoutFrameType.
type WipeoutFrameType int32

const (
	WipeoutFrameTypeNoDisplayOrPlotted WipeoutFrameType = 0
	WipeoutFrameTypeDisplayAndPlotted  WipeoutFrameType = 1
	WipeoutFrameTypeDisplayNoPlotted   WipeoutFrameType = 2
)

// XClipFrameType is ACadSharp's XClipFrameType.
type XClipFrameType int32

const (
	XClipFrameTypeNone           XClipFrameType = 0
	XClipFrameTypeDisplayAndPlot XClipFrameType = 1
	XClipFrameTypeDisplayNotPlot XClipFrameType = 2
)

// ZeroHandling is ACadSharp's ZeroHandling.
type ZeroHandling uint8

const (
	ZeroHandlingSuppressZeroFeetAndInches               ZeroHandling = 0
	ZeroHandlingShowZeroFeetAndInches                   ZeroHandling = 1
	ZeroHandlingShowZeroFeetSuppressZeroInches          ZeroHandling = 2
	ZeroHandlingSuppressZeroFeetShowZeroInches          ZeroHandling = 3
	ZeroHandlingSuppressDecimalLeadingZeroes            ZeroHandling = 4
	ZeroHandlingSuppressDecimalTrailingZeroes           ZeroHandling = 8
	ZeroHandlingSuppressDecimalLeadingAndTrailingZeroes ZeroHandling = 12
)
