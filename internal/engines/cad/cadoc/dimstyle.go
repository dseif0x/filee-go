package cadoc

import "math"

// DimensionStyle is a DIMSTYLE table entry. Field names follow ACadSharp;
// the DIM* variable is noted where the name doesn't make it obvious.
type DimensionStyle struct {
	TableEntryBase
	Flags StandardFlags

	PostFix                                     string                           // DIMPOST
	AlternateDimensioningSuffix                 string                           // DIMAPOST
	ScaleFactor                                 float64                          // DIMSCALE
	ArrowSize                                   float64                          // DIMASZ
	ExtensionLineOffset                         float64                          // DIMEXO
	DimensionLineIncrement                      float64                          // DIMDLI
	ExtensionLineExtension                      float64                          // DIMEXE
	Rounding                                    float64                          // DIMRND
	DimensionLineExtension                      float64                          // DIMDLE
	PlusTolerance                               float64                          // DIMTP
	MinusTolerance                              float64                          // DIMTM
	FixedExtensionLineLength                    float64                          // DIMFXL
	JoggedRadiusDimensionTransverseSegmentAngle float64                          // DIMJOGANG, radians
	TextBackgroundFillMode                      DimensionTextBackgroundFillMode  // DIMTFILL
	TextBackgroundColor                         Color                            // DIMTFILLCLR
	GenerateTolerances                          bool                             // DIMTOL
	LimitsGeneration                            bool                             // DIMLIM
	TextInsideHorizontal                        bool                             // DIMTIH
	TextOutsideHorizontal                       bool                             // DIMTOH
	SuppressFirstExtensionLine                  bool                             // DIMSE1
	SuppressSecondExtensionLine                 bool                             // DIMSE2
	TextVerticalAlignment                       DimensionTextVerticalAlignment   // DIMTAD
	ZeroHandling                                ZeroHandling                     // DIMZIN
	AngularZeroHandling                         AngularZeroHandling              // DIMAZIN
	ArcLengthSymbolPosition                     ArcLengthSymbolPosition          // DIMARCSYM
	TextHeight                                  float64                          // DIMTXT
	CenterMarkSize                              float64                          // DIMCEN
	TickSize                                    float64                          // DIMTSZ
	AlternateUnitScaleFactor                    float64                          // DIMALTF
	LinearScaleFactor                           float64                          // DIMLFAC
	TextVerticalPosition                        float64                          // DIMTVP
	ToleranceScaleFactor                        float64                          // DIMTFAC
	DimensionLineGap                            float64                          // DIMGAP
	AlternateUnitRounding                       float64                          // DIMALTRND
	AlternateUnitDimensioning                   bool                             // DIMALT
	AlternateUnitDecimalPlaces                  int16                            // DIMALTD
	TextOutsideExtensions                       bool                             // DIMTOFL
	SeparateArrowBlocks                         bool                             // DIMSAH
	TextInsideExtensions                        bool                             // DIMTIX
	SuppressOutsideExtensions                   bool                             // DIMSOXD
	DimensionLineColor                          Color                            // DIMCLRD
	ExtensionLineColor                          Color                            // DIMCLRE
	TextColor                                   Color                            // DIMCLRT
	AngularDecimalPlaces                        int16                            // DIMADEC
	DimensionUnit                               int16                            // DIMUNIT (obsolete)
	DecimalPlaces                               int16                            // DIMDEC
	ToleranceDecimalPlaces                      int16                            // DIMTDEC
	AlternateUnitFormat                         LinearUnitFormat                 // DIMALTU
	AlternateUnitToleranceDecimalPlaces         int16                            // DIMALTTD
	AngularUnit                                 AngularUnitFormat                // DIMAUNIT
	FractionFormat                              FractionFormat                   // DIMFRAC
	LinearUnitFormat                            LinearUnitFormat                 // DIMLUNIT
	DecimalSeparator                            rune                             // DIMDSEP
	TextMovement                                TextMovement                     // DIMTMOVE
	TextHorizontalAlignment                     DimensionTextHorizontalAlignment // DIMJUST
	SuppressFirstDimensionLine                  bool                             // DIMSD1
	SuppressSecondDimensionLine                 bool                             // DIMSD2
	ToleranceAlignment                          ToleranceAlignment               // DIMTOLJ
	ToleranceZeroHandling                       ZeroHandling                     // DIMTZIN
	AlternateUnitZeroHandling                   ZeroHandling                     // DIMALTZ
	AlternateUnitToleranceZeroHandling          ZeroHandling                     // DIMALTTZ
	DimensionFit                                int16                            // DIMFIT (obsolete)
	CursorUpdate                                bool                             // DIMUPT
	DimensionTextArrowFit                       TextArrowFitType                 // DIMATFIT
	IsExtensionLineLengthFixed                  bool                             // DIMFXLON
	TextDirection                               TextDirection                    // DIMTXTDIRECTION
	AltMzf                                      float64                          // DIMALTMZF
	AltMzs                                      string                           // DIMALTMZS
	Mzf                                         float64                          // DIMMZF
	Mzs                                         string                           // DIMMZS
	DimensionLineWeight                         LineWeight                       // DIMLWD
	ExtensionLineWeight                         LineWeight                       // DIMLWE

	Style        *TextStyle   // DIMTXSTY
	LeaderArrow  *BlockRecord // DIMLDRBLK
	ArrowBlock   *BlockRecord // DIMBLK
	DimArrow1    *BlockRecord // DIMBLK1
	DimArrow2    *BlockRecord // DIMBLK2
	LineType     *LineType    // DIMLTYPE
	LineTypeExt1 *LineType    // DIMLTEX1
	LineTypeExt2 *LineType    // DIMLTEX2
}

// NewDimensionStyle returns a style with ACadSharp's (metric-agnostic) defaults.
func NewDimensionStyle(name string) *DimensionStyle {
	return &DimensionStyle{
		TableEntryBase:           TableEntryBase{Name: name},
		ScaleFactor:              1,
		ArrowSize:                0.18,
		ExtensionLineOffset:      0.625,
		DimensionLineIncrement:   3.75,
		ExtensionLineExtension:   1.25,
		FixedExtensionLineLength: 1,
		JoggedRadiusDimensionTransverseSegmentAngle: math.Pi / 4,
		TextBackgroundColor:                         ColorByBlock,
		TextVerticalAlignment:                       DimensionTextVerticalAlignmentAbove,
		ZeroHandling:                                ZeroHandlingSuppressDecimalTrailingZeroes,
		AngularZeroHandling:                         AngularZeroHandlingDisplayAll,
		ArcLengthSymbolPosition:                     ArcLengthSymbolPositionBeforeDimensionText,
		TextHeight:                                  0.18,
		CenterMarkSize:                              0.09,
		AlternateUnitScaleFactor:                    25.4,
		LinearScaleFactor:                           1,
		ToleranceScaleFactor:                        1,
		DimensionLineGap:                            0.625,
		AlternateUnitDecimalPlaces:                  3,
		DimensionLineColor:                          ColorByBlock,
		ExtensionLineColor:                          ColorByBlock,
		TextColor:                                   ColorByBlock,
		DimensionUnit:                               2,
		DecimalPlaces:                               2,
		ToleranceDecimalPlaces:                      2,
		AlternateUnitFormat:                         LinearUnitFormatDecimal,
		AlternateUnitToleranceDecimalPlaces:         3,
		AngularUnit:                                 AngularUnitFormatDecimalDegrees,
		FractionFormat:                              FractionFormatHorizontal,
		LinearUnitFormat:                            LinearUnitFormatDecimal,
		DecimalSeparator:                            '.',
		TextMovement:                                TextMovementMoveLineWithText,
		TextHorizontalAlignment:                     DimensionTextHorizontalAlignmentCentered,
		ToleranceAlignment:                          ToleranceAlignmentBottom,
		ToleranceZeroHandling:                       ZeroHandlingSuppressDecimalTrailingZeroes,
		AlternateUnitZeroHandling:                   ZeroHandlingSuppressZeroFeetAndInches,
		AlternateUnitToleranceZeroHandling:          ZeroHandlingSuppressZeroFeetAndInches,
		DimensionTextArrowFit:                       TextArrowFitTypeBestFit,
		TextDirection:                               TextDirectionLeftToRight,
		DimensionLineWeight:                         LineWeightByBlock,
		ExtensionLineWeight:                         LineWeightByBlock,
	}
}

func (*DimensionStyle) ObjectName() string      { return TokenTableDimstyle }
func (*DimensionStyle) SubclassMarker() string  { return SubclassDimensionStyle }
func (*DimensionStyle) ObjectType() ObjectType  { return ObjectTypeDimStyle }
func (d *DimensionStyle) EntryFlags() int16     { return int16(d.Flags) }
func (d *DimensionStyle) SetEntryFlags(f int16) { d.Flags = StandardFlags(f) }

// Clone returns a copy of the style (references are shared).
func (d *DimensionStyle) Clone() *DimensionStyle {
	c := *d
	c.ObjectBase = ObjectBase{}
	return &c
}

// Prefix is the text before "<>" in PostFix.
func (d *DimensionStyle) Prefix() string { p, _ := splitPostFix(d.PostFix); return p }

// Suffix is the text after "<>" in PostFix (all of it without "<>").
func (d *DimensionStyle) Suffix() string { _, s := splitPostFix(d.PostFix); return s }

func splitPostFix(text string) (prefix, suffix string) {
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '<' && text[i+1] == '>' {
			return text[:i], text[i+2:]
		}
	}
	return "", text
}

// ApplyRounding rounds a measurement with DIMRND (or DIMALTRND).
func (d *DimensionStyle) ApplyRounding(value float64, alternate bool) float64 {
	r := d.Rounding
	if alternate {
		r = d.AlternateUnitRounding
	}
	if r != 0 {
		value = r * math.Round(value/r)
	}
	return value
}
