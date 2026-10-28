package cadoc

import (
	"math"
	"strings"
)

const (
	DefaultLayerName       = "0"
	DefpointsLayerName     = "defpoints"
	ByLayerName            = "ByLayer"
	ByBlockName            = "ByBlock"
	ContinuousName         = "Continuous"
	DefaultTextStyleName   = "Standard"
	DefaultDimStyleName    = "Standard"
	DefaultVPortName       = "*Active"
	DefaultAppIdName       = "ACAD"
	ModelSpaceName         = "*Model_Space"
	PaperSpaceName         = "*Paper_Space"
	AnonymousBlockPrefix   = "*A"
	StyleOverrideEntryName = "DSTYLE"
)

// AppId is a registered application (APPID).
type AppId struct {
	TableEntryBase
	Flags StandardFlags
}

// Well-known application names (AppId constants in ACadSharp).
const (
	AppIdBlockRepBTag   = "AcDbBlockRepBTag"
	AppIdBlockRepETag   = "AcDbBlockRepETag"
	AppIdDynamicBlock   = "AcDbDynamicBlockTrueName"
	AppIdAcadMLeaderVer = "ACAD_MLEADERVER"
	AppIdAcadPstyleMode = "ACAD_PSEXT"
)

func NewAppId(name string) *AppId { return &AppId{TableEntryBase: TableEntryBase{Name: name}} }

func (*AppId) ObjectName() string      { return TokenTableAppId }
func (*AppId) SubclassMarker() string  { return SubclassApplicationId }
func (*AppId) ObjectType() ObjectType  { return ObjectTypeAppId }
func (a *AppId) EntryFlags() int16     { return int16(a.Flags) }
func (a *AppId) SetEntryFlags(f int16) { a.Flags = StandardFlags(f) }

// Layer is a LAYER table entry.
type Layer struct {
	TableEntryBase
	Flags LayerFlags
	// Color is the layer colour (never ByLayer/ByBlock).
	Color Color
	// IsOn is false when the layer is off (negative colour in DXF).
	IsOn bool
	// LineType nil means Continuous.
	LineType   *LineType
	LineWeight LineWeight
	// PlotFlag false: the layer does not plot (always false for Defpoints,
	// see Plots).
	PlotFlag      bool
	Material      *Material
	PlotStyleName uint64
}

// NewLayer returns a layer with ACadSharp's defaults (colour 7, on, plots).
func NewLayer(name string) *Layer {
	return &Layer{TableEntryBase: TableEntryBase{Name: name}, Color: ColorIndex(7), IsOn: true,
		LineWeight: LineWeightDefault, PlotFlag: true}
}

func (*Layer) ObjectName() string      { return TokenTableLayer }
func (*Layer) SubclassMarker() string  { return SubclassLayer }
func (*Layer) ObjectType() ObjectType  { return ObjectTypeLayer }
func (l *Layer) EntryFlags() int16     { return int16(l.Flags) }
func (l *Layer) SetEntryFlags(f int16) { l.Flags = LayerFlags(f) }

// Plots is ACadSharp's PlotFlag getter: the Defpoints layer never plots.
func (l *Layer) Plots() bool {
	return l.PlotFlag && !strings.EqualFold(l.Name, DefpointsLayerName)
}

// IsFrozen reports the Frozen flag.
func (l *Layer) IsFrozen() bool { return l.Flags&LayerFlagsFrozen != 0 }

// LineTypeName is the layer's line type name, Continuous without one.
func (l *Layer) LineTypeName() string {
	if l.LineType == nil {
		return ContinuousName
	}
	return l.LineType.Name
}

// LineType is an LTYPE table entry.
type LineType struct {
	TableEntryBase
	Flags       StandardFlags
	Description string
	// Alignment is 'A' (code 72).
	Alignment rune
	Segments  []LineTypeSegment
}

// LineTypeSegment is LineType.Segment: a dash (positive), gap (negative) or
// dot (0), optionally with a shape or text.
type LineTypeSegment struct {
	Length      float64
	Flags       LineTypeShapeFlags
	ShapeNumber int16
	Offset      Vec2
	// Rotation is in radians.
	Rotation float64
	Scale    float64
	Text     string
	// Style is the text style (shape file) of a shape or text segment.
	Style *TextStyle
}

func (s LineTypeSegment) IsLine() bool  { return s.Length > 0 }
func (s LineTypeSegment) IsPoint() bool { return s.Length == 0 }
func (s LineTypeSegment) IsSpace() bool { return s.Length < 0 }
func (s LineTypeSegment) IsShape() bool { return s.Flags&LineTypeShapeFlagsShape != 0 }
func (s LineTypeSegment) IsText() bool  { return s.Flags&LineTypeShapeFlagsText != 0 }

func NewLineType(name string) *LineType {
	return &LineType{TableEntryBase: TableEntryBase{Name: name}, Alignment: 'A'}
}

func (*LineType) ObjectName() string      { return TokenTableLinetype }
func (*LineType) SubclassMarker() string  { return SubclassLinetype }
func (*LineType) ObjectType() ObjectType  { return ObjectTypeLType }
func (l *LineType) EntryFlags() int16     { return int16(l.Flags) }
func (l *LineType) SetEntryFlags(f int16) { l.Flags = StandardFlags(f) }

// IsComplex is true when the line type has segments (a dash pattern), as in
// ACadSharp.
func (l *LineType) IsComplex() bool { return len(l.Segments) > 0 }

// HasShapes reports a shape segment.
func (l *LineType) HasShapes() bool {
	for _, s := range l.Segments {
		if s.IsShape() {
			return true
		}
	}
	return false
}

// PatternLength is the sum of the absolute segment lengths.
func (l *LineType) PatternLength() float64 {
	var sum float64
	for _, s := range l.Segments {
		sum += math.Abs(s.Length)
	}
	return sum
}

// AddSegment appends a segment.
func (l *LineType) AddSegment(s LineTypeSegment) { l.Segments = append(l.Segments, s) }

// TextStyle is a STYLE table entry (text styles and shape files).
type TextStyle struct {
	TableEntryBase
	Flags           StyleFlags
	Filename        string
	BigFontFilename string
	Height          float64
	Width           float64
	LastHeight      float64
	// ObliqueAngle is in radians.
	ObliqueAngle float64
	MirrorFlag   TextMirrorFlag
	// TrueType holds the bold/italic flags of a TrueType font (xdata ACAD
	// 1071), FontFamily its family name (xdata ACAD 1000). Readers fill both
	// from the xdata, writers write them back into it.
	TrueType   FontFlags
	FontFamily string
}

func NewTextStyle(name string) *TextStyle {
	return &TextStyle{TableEntryBase: TableEntryBase{Name: name}, Width: 1}
}

func (*TextStyle) ObjectName() string      { return TokenTableStyle }
func (*TextStyle) SubclassMarker() string  { return SubclassTextStyle }
func (*TextStyle) ObjectType() ObjectType  { return ObjectTypeStyle }
func (t *TextStyle) EntryFlags() int16     { return int16(t.Flags) }
func (t *TextStyle) SetEntryFlags(f int16) { t.Flags = StyleFlags(f) }

// IsShapeFile reports a shape file style (flag 1).
func (t *TextStyle) IsShapeFile() bool { return t.Flags&StyleFlagsIsShape != 0 }

// UCS is a UCS table entry.
type UCS struct {
	TableEntryBase
	Flags                StandardFlags
	Origin               Vec3
	XAxis                Vec3
	YAxis                Vec3
	OrthographicType     OrthographicType
	OrthographicViewType OrthographicType
	Elevation            float64
	BaseUCS              *UCS
}

func NewUCS(name string) *UCS {
	return &UCS{TableEntryBase: TableEntryBase{Name: name}, XAxis: AxisX, YAxis: AxisY}
}

func (*UCS) ObjectName() string      { return TokenTableUcs }
func (*UCS) SubclassMarker() string  { return SubclassUcs }
func (*UCS) ObjectType() ObjectType  { return ObjectTypeUCS }
func (u *UCS) EntryFlags() int16     { return int16(u.Flags) }
func (u *UCS) SetEntryFlags(f int16) { u.Flags = StandardFlags(f) }

// View is a VIEW table entry.
type View struct {
	TableEntryBase
	Flags         StandardFlags
	Height        float64
	Width         float64
	LensLength    float64
	FrontClipping float64
	BackClipping  float64
	// Angle (twist) is in radians.
	Angle               float64
	ViewMode            ViewModeType
	IsUcsAssociated     bool
	IsPlottable         bool
	RenderMode          RenderMode
	Center              Vec2
	Direction           Vec3
	Target              Vec3
	VisualStyle         Object
	UcsOrigin           Vec3
	UcsXAxis            Vec3
	UcsYAxis            Vec3
	UcsElevation        float64
	UcsOrthographicType OrthographicType
	NamedUcs            *UCS
	BaseUcs             *UCS
}

func NewView(name string) *View { return &View{TableEntryBase: TableEntryBase{Name: name}} }

func (*View) ObjectName() string      { return TokenTableView }
func (*View) SubclassMarker() string  { return SubclassView }
func (*View) ObjectType() ObjectType  { return ObjectTypeView }
func (v *View) EntryFlags() int16     { return int16(v.Flags) }
func (v *View) SetEntryFlags(f int16) { v.Flags = StandardFlags(f) }

// VPort is a VPORT table entry (a model space viewport configuration).
type VPort struct {
	TableEntryBase
	Flags              StandardFlags
	BottomLeft         Vec2
	TopRight           Vec2
	Center             Vec2
	SnapBasePoint      Vec2
	SnapSpacing        Vec2
	GridSpacing        Vec2
	Direction          Vec3
	Target             Vec3
	ViewHeight         float64
	AspectRatio        float64
	LensLength         float64
	FrontClippingPlane float64
	BackClippingPlane  float64
	// SnapRotation and TwistAngle are in radians.
	SnapRotation                   float64
	TwistAngle                     float64
	CircleZoomPercent              int16
	RenderMode                     RenderMode
	ViewMode                       ViewModeType
	UcsIconDisplay                 UscIconType
	SnapOn                         bool
	ShowGrid                       bool
	IsometricSnap                  bool
	SnapIsoPair                    int16
	Origin                         Vec3
	XAxis                          Vec3
	YAxis                          Vec3
	NamedUcs                       *UCS
	BaseUcs                        *UCS
	OrthographicType               OrthographicType
	Elevation                      float64
	GridFlags                      GridFlags
	MinorGridLinesPerMajorGridLine int16
	VisualStyle                    Object
	UseDefaultLighting             bool
	DefaultLighting                DefaultLightingType
	Brightness                     float64
	Contrast                       float64
	AmbientColor                   Color
}

func NewVPort(name string) *VPort {
	return &VPort{TableEntryBase: TableEntryBase{Name: name}, TopRight: Vec2{1, 1},
		SnapSpacing: Vec2{0.5, 0.5}, GridSpacing: Vec2{10, 10}, Direction: AxisZ, ViewHeight: 10,
		AspectRatio: 1, LensLength: 50, CircleZoomPercent: 1000, RenderMode: RenderModeOptimized2D,
		ViewMode: ViewModeTypeFrontClippingZ, UcsIconDisplay: UscIconTypeOnOrigin, ShowGrid: true,
		XAxis: AxisX, YAxis: AxisY, GridFlags: GridFlags1 | GridFlags2, MinorGridLinesPerMajorGridLine: 5,
		UseDefaultLighting: true, DefaultLighting: DefaultLightingTypeTwoDistantLights, AmbientColor: ColorBlack}
}

func (*VPort) ObjectName() string      { return TokenTableVport }
func (*VPort) SubclassMarker() string  { return SubclassVPort }
func (*VPort) ObjectType() ObjectType  { return ObjectTypeVPort }
func (v *VPort) EntryFlags() int16     { return int16(v.Flags) }
func (v *VPort) SetEntryFlags(f int16) { v.Flags = StandardFlags(f) }
