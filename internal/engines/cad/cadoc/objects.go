package cadoc

import (
	"math"
	"sort"
	"strings"
)

// DictionaryVariable is DICTIONARYVAR: a named string variable stored in
// AcDbVariableDictionary.
type DictionaryVariable struct {
	NonGraphicalBase
	Value              string
	ObjectSchemaNumber int32
}

// Well-known dictionary variable names.
const (
	DictionaryVariableCurrentAnnotationScale  = "CANNOSCALE"
	DictionaryVariableCurrentMultiLeaderStyle = "CMLEADERSTYLE"
	DictionaryVariableCurrentTableStyle       = "CTABLESTYLE"
	DictionaryVariableWipeoutFrame            = "WIPEOUTFRAME"
)

func NewDictionaryVariable(name, value string) *DictionaryVariable {
	v := &DictionaryVariable{Value: value}
	v.Name = name
	return v
}

func (*DictionaryVariable) ObjectName() string     { return TokenObjectDictionaryVar }
func (*DictionaryVariable) SubclassMarker() string { return SubclassDictionaryVariables }
func (*DictionaryVariable) ObjectType() ObjectType { return ObjectTypeUnlisted }

// PaperMargin is the unprintable margin of a plot setting, in millimetres.
type PaperMargin struct{ Left, Bottom, Right, Top float64 }

// PlotSettings is PLOTSETTINGS (also the base of Layout).
type PlotSettings struct {
	NonGraphicalBase
	PageName                string
	SystemPrinterName       string
	PaperSize               string
	PlotViewName            string
	StyleSheet              string
	UnprintableMargin       PaperMargin
	PaperWidth              float64
	PaperHeight             float64
	PlotOriginX             float64
	PlotOriginY             float64
	WindowLowerLeftX        float64
	WindowLowerLeftY        float64
	WindowUpperLeftX        float64
	WindowUpperLeftY        float64
	NumeratorScale          float64
	DenominatorScale        float64
	Flags                   PlotFlags
	PaperUnits              PlotPaperUnits
	PaperRotation           PlotRotation
	PlotType                PlotType
	ScaledFit               ScaledType
	ShadePlotMode           ShadePlotMode
	ShadePlotResolutionMode ShadePlotResolutionMode
	ShadePlotDPI            int16
	StandardScale           float64
	PaperImageOrigin        Vec2
	ShadePlotIDHandle       uint64
}

// InitPlotSettings sets ACadSharp's defaults (ISO A4, metric).
func (p *PlotSettings) InitPlotSettings() {
	p.PageName = "none_device"
	p.PaperSize = "ISO_A4_(210.00_x_297.00_MM)"
	p.PaperWidth, p.PaperHeight = 297, 210
	p.NumeratorScale, p.DenominatorScale = 1, 1
	p.Flags = PlotFlagsDrawViewportsFirst | PlotFlagsPrintLineweights | PlotFlagsPlotPlotStyles | PlotFlagsUseStandardScale
	p.PaperUnits = PlotPaperUnitsMillimeters
	p.PlotType = PlotTypeDrawingExtents
	p.ShadePlotMode = ShadePlotModeAsDisplayed
	p.ShadePlotResolutionMode = ShadePlotResolutionModeDraft
	p.ShadePlotDPI = 300
	p.StandardScale = 1
}

func NewPlotSettings(name string) *PlotSettings {
	p := &PlotSettings{}
	p.Name = name
	p.InitPlotSettings()
	return p
}

func (*PlotSettings) ObjectName() string     { return TokenObjectPlotSettings }
func (*PlotSettings) SubclassMarker() string { return SubclassPlotSettings }
func (*PlotSettings) ObjectType() ObjectType { return ObjectTypeUnlisted }

// PrintScale is NumeratorScale / DenominatorScale.
func (p *PlotSettings) PrintScale() float64 {
	if p.DenominatorScale == 0 {
		return 1
	}
	return p.NumeratorScale / p.DenominatorScale
}

// Layout is LAYOUT: a model or paper space layout tab.
type Layout struct {
	PlotSettings
	// AssociatedBlock is the layout's block record (*Model_Space,
	// *Paper_Space, *Paper_SpaceN).
	AssociatedBlock     *BlockRecord
	LastActiveViewport  *Viewport
	LayoutFlags         LayoutFlags
	TabOrder            int32
	MinLimits           Vec2
	MaxLimits           Vec2
	InsertionBasePoint  Vec3
	Origin              Vec3
	MinExtents          Vec3
	MaxExtents          Vec3
	XAxis               Vec3
	YAxis               Vec3
	Elevation           float64
	UcsOrthographicType OrthographicType
	UCS                 *UCS
	BaseUCS             *UCS
}

const (
	ModelLayoutName = "Model"
	PaperLayoutName = "Layout1"
)

// NewLayout returns a layout with ACadSharp's defaults (not linked to a block).
func NewLayout(name string) *Layout {
	l := &Layout{LayoutFlags: LayoutFlagsPaperSpaceLinetypeScaling, MinLimits: Vec2{-20, -7.5},
		MaxLimits: Vec2{277, 202.5}, MinExtents: Vec3{25.7, 19.5, 0}, MaxExtents: Vec3{231.3, 175.5, 0},
		XAxis: AxisX, YAxis: AxisY}
	l.Name = name
	l.InitPlotSettings()
	return l
}

func (*Layout) ObjectName() string     { return TokenObjectLayout }
func (*Layout) SubclassMarker() string { return SubclassLayout }
func (*Layout) ObjectType() ObjectType { return ObjectTypeLayout }

// IsPaperSpace is true for every layout but Model.
func (l *Layout) IsPaperSpace() bool { return !strings.EqualFold(l.Name, ModelLayoutName) }

// SetBlock links the layout and its block record both ways.
func (l *Layout) SetBlock(r *BlockRecord) {
	l.AssociatedBlock = r
	if r != nil {
		r.Layout = l
	}
}

// Viewports returns the viewports of the layout's block.
func (l *Layout) Viewports() []*Viewport {
	if l.AssociatedBlock == nil {
		return nil
	}
	return l.AssociatedBlock.Viewports()
}

// UpdatePaperViewport adds (or resizes) the overall paper space viewport.
func (l *Layout) UpdatePaperViewport() {
	if !l.IsPaperSpace() || l.AssociatedBlock == nil {
		return
	}
	vps := l.Viewports()
	var vp *Viewport
	if len(vps) == 0 {
		vp = NewViewport()
		l.AssociatedBlock.AddEntity(vp)
	} else {
		vp = vps[0]
	}
	vp.Width, vp.Height = l.PaperWidth, l.PaperHeight
	vp.Center = Vec3{vp.Width / 2, vp.Height / 2, 0}
}

// Group is GROUP: a named selection of entities.
type Group struct {
	NonGraphicalBase
	Description string
	Selectable  bool
	Entities    []Entity
}

func NewGroup(name string) *Group { g := &Group{Selectable: true}; g.Name = name; return g }

func (*Group) ObjectName() string     { return TokenObjectGroup }
func (*Group) SubclassMarker() string { return SubclassGroup }
func (*Group) ObjectType() ObjectType { return ObjectTypeGroup }

// IsUnnamed reports an anonymous group (empty name or "*" prefix).
func (g *Group) IsUnnamed() bool {
	return strings.TrimSpace(g.Name) == "" || strings.HasPrefix(g.Name, "*")
}

// Add appends an entity and registers the group as its reactor.
func (g *Group) Add(e Entity) {
	g.Entities = append(g.Entities, e)
	e.Base().AddReactor(g)
}

// MLineStyle is MLINESTYLE.
type MLineStyle struct {
	NonGraphicalBase
	Flags       MLineStyleFlags
	Description string
	FillColor   Color
	// StartAngle and EndAngle are in radians.
	StartAngle float64
	EndAngle   float64
	Elements   []MLineStyleElement
}

// MLineStyleElement is MLineStyle.Element.
type MLineStyleElement struct {
	Offset   float64
	Color    Color
	LineType *LineType
}

const DefaultMLineStyleName = "Standard"

func NewMLineStyle(name string) *MLineStyle {
	s := &MLineStyle{FillColor: ColorByLayer, StartAngle: math.Pi / 2, EndAngle: math.Pi / 2}
	s.Name = name
	return s
}

func (*MLineStyle) ObjectName() string     { return TokenObjectMLineStyle }
func (*MLineStyle) SubclassMarker() string { return SubclassMLineStyle }
func (*MLineStyle) ObjectType() ObjectType { return ObjectTypeMLineStyle }

// MultiLeaderStyle is MLEADERSTYLE.
type MultiLeaderStyle struct {
	NonGraphicalBase
	ContentType                  LeaderContentType
	MultiLeaderDrawOrder         MultiLeaderDrawOrderType
	LeaderDrawOrder              LeaderDrawOrderType
	MaxLeaderSegmentsPoints      int32
	FirstSegmentAngleConstraint  float64
	SecondSegmentAngleConstraint float64
	PathType                     MultiLeaderPathType
	LineColor                    Color
	LeaderLineType               *LineType
	LeaderLineWeight             LineWeight
	EnableLanding                bool
	LandingGap                   float64
	EnableDogleg                 bool
	LandingDistance              float64
	Description                  string
	Arrowhead                    *BlockRecord
	ArrowheadSize                float64
	DefaultTextContents          string
	TextStyle                    *TextStyle
	TextLeftAttachment           TextAttachmentType
	TextRightAttachment          TextAttachmentType
	TextAngle                    TextAngleType
	TextAlignment                TextAlignmentType
	TextColor                    Color
	TextHeight                   float64
	TextFrame                    bool
	TextAlignAlwaysLeft          bool
	AlignSpace                   float64
	BlockContent                 *BlockRecord
	BlockContentColor            Color
	BlockContentScale            Vec3
	EnableBlockContentScale      bool
	// BlockContentRotation is in radians.
	BlockContentRotation       float64
	EnableBlockContentRotation bool
	BlockContentConnection     BlockContentConnectionType
	ScaleFactor                float64
	OverwritePropertyValue     bool
	IsAnnotative               bool
	BreakGapSize               float64
	TextAttachmentDirection    TextAttachmentDirectionType
	TextBottomAttachment       TextAttachmentType
	TextTopAttachment          TextAttachmentType
	UnknownFlag298             bool
}

const DefaultMultiLeaderStyleName = "Standard"

func NewMultiLeaderStyle(name string) *MultiLeaderStyle {
	s := &MultiLeaderStyle{ContentType: LeaderContentTypeMText, MaxLeaderSegmentsPoints: 2,
		LineColor: ColorByBlock, LeaderLineWeight: LineWeightByBlock, EnableLanding: true, LandingGap: 0.09,
		EnableDogleg: true, LandingDistance: 0.36, ArrowheadSize: 0.18, TextAngle: TextAngleTypeHorizontal,
		TextColor: ColorByBlock, TextHeight: 0.18, BlockContentColor: ColorByBlock,
		BlockContentScale: Vec3{1, 1, 1}, ScaleFactor: 1, BreakGapSize: 0.125}
	s.Name = name
	return s
}

func (*MultiLeaderStyle) ObjectName() string     { return TokenObjectMLeaderStyle }
func (*MultiLeaderStyle) SubclassMarker() string { return SubclassMLeaderStyle }
func (*MultiLeaderStyle) ObjectType() ObjectType { return ObjectTypeUnlisted }

// ImageDefinition is IMAGEDEF: a raster image file referenced by IMAGE.
type ImageDefinition struct {
	NonGraphicalBase
	ClassVersion int32
	FileName     string
	Size         Vec2
	DefaultSize  Vec2
	IsLoaded     bool
	Units        ResolutionUnit
}

func NewImageDefinition(name string) *ImageDefinition {
	d := &ImageDefinition{DefaultSize: Vec2{1, 1}, IsLoaded: true}
	d.Name = name
	return d
}

func (*ImageDefinition) ObjectName() string     { return TokenObjectImageDefinition }
func (*ImageDefinition) SubclassMarker() string { return SubclassRasterImageDef }
func (*ImageDefinition) ObjectType() ObjectType { return ObjectTypeUnlisted }

// ImageDefinitionReactor is IMAGEDEF_REACTOR, the link from an image
// definition back to an IMAGE.
type ImageDefinitionReactor struct {
	NonGraphicalBase
	ClassVersion int32
	Image        *RasterImage
}

func (*ImageDefinitionReactor) ObjectName() string     { return TokenObjectImageDefinitionReactor }
func (*ImageDefinitionReactor) SubclassMarker() string { return SubclassRasterImageDefReactor }
func (*ImageDefinitionReactor) ObjectType() ObjectType { return ObjectTypeUnlisted }

// RasterVariables is RASTERVARIABLES (ACAD_IMAGE_VARS).
type RasterVariables struct {
	NonGraphicalBase
	ClassVersion        int32
	IsDisplayFrameShown bool
	DisplayQuality      ImageDisplayQuality
	Units               ImageUnits
}

func (*RasterVariables) ObjectName() string     { return TokenObjectRasterVariables }
func (*RasterVariables) SubclassMarker() string { return SubclassRasterVariables }
func (*RasterVariables) ObjectType() ObjectType { return ObjectTypeUnlisted }

// WipeoutVariables is WIPEOUTVARIABLES (ACAD_WIPEOUT_VARS).
type WipeoutVariables struct {
	NonGraphicalBase
	DisplayImageFrame bool
}

func (*WipeoutVariables) ObjectName() string     { return TokenObjectWipeoutVariables }
func (*WipeoutVariables) SubclassMarker() string { return SubclassWipeoutVariables }
func (*WipeoutVariables) ObjectType() ObjectType { return ObjectTypeUnlisted }

// XRecord is XRECORD: arbitrary group-coded data.
type XRecord struct {
	NonGraphicalBase
	CloningFlags DictionaryCloningFlags
	Entries      []XRecordEntry
}

// XRecordEntry is XRecord.Entry. Value is string, float64, int16, int32,
// int64, bool, []byte, Vec3 (point codes) or, for handle codes, the
// referenced Object (nil when it could not be resolved).
type XRecordEntry struct {
	Code  int
	Value any
}

func (*XRecord) ObjectName() string     { return TokenObjectXRecord }
func (*XRecord) SubclassMarker() string { return SubclassXRecord }
func (*XRecord) ObjectType() ObjectType { return ObjectTypeXRecord }

// Scale is SCALE (annotation scales in ACAD_SCALELIST).
type Scale struct {
	NonGraphicalBase
	PaperUnits   float64
	DrawingUnits float64
	IsUnitScale  bool
}

const DefaultScaleName = "1:1"

func (*Scale) ObjectName() string     { return TokenObjectScale }
func (*Scale) SubclassMarker() string { return SubclassScale }
func (*Scale) ObjectType() ObjectType { return ObjectTypeUnlisted }

// ScaleFactor is PaperUnits / DrawingUnits.
func (s *Scale) ScaleFactor() float64 {
	if s.DrawingUnits == 0 {
		return 1
	}
	return s.PaperUnits / s.DrawingUnits
}

// SortEntitiesTable is SORTENTSTABLE: the draw order of a block's entities.
type SortEntitiesTable struct {
	NonGraphicalBase
	BlockOwner *BlockRecord
	Sorters    []SortEntitiesSorter
}

// SortEntitiesSorter is SortEntitiesTable.Sorter.
type SortEntitiesSorter struct {
	Entity     Entity
	SortHandle uint64
}

func NewSortEntitiesTable(owner *BlockRecord) *SortEntitiesTable {
	t := &SortEntitiesTable{BlockOwner: owner}
	t.Name = SortEntsDictionaryName
	return t
}

func (*SortEntitiesTable) ObjectName() string     { return TokenObjectSortEntsTable }
func (*SortEntitiesTable) SubclassMarker() string { return SubclassSortentsTable }
func (*SortEntitiesTable) ObjectType() ObjectType { return ObjectTypeUnlisted }

// Add records a sort handle for an entity.
func (t *SortEntitiesTable) Add(e Entity, sortHandle uint64) {
	t.Sorters = append(t.Sorters, SortEntitiesSorter{Entity: e, SortHandle: sortHandle})
}

// SortHandle returns the entity's sort handle (its own handle if not listed).
func (t *SortEntitiesTable) SortHandle(e Entity) uint64 {
	for _, s := range t.Sorters {
		if s.Entity == e {
			return s.SortHandle
		}
	}
	return e.Base().Handle
}

// Sorted returns the sorters ordered by sort handle.
func (t *SortEntitiesTable) Sorted() []SortEntitiesSorter {
	s := append([]SortEntitiesSorter(nil), t.Sorters...)
	sort.SliceStable(s, func(i, j int) bool { return s[i].SortHandle < s[j].SortHandle })
	return s
}

// BookColor is DBCOLOR: a colour-book colour ("BOOK$NAME").
type BookColor struct {
	NonGraphicalBase
	Color Color
}

func (*BookColor) ObjectName() string     { return TokenObjectDBColor }
func (*BookColor) SubclassMarker() string { return SubclassDbColor }
func (*BookColor) ObjectType() ObjectType { return ObjectTypeUnlisted }

// BookName is the part of Name before "$".
func (b *BookColor) BookName() string {
	if i := strings.IndexByte(b.Name, '$'); i >= 0 {
		return b.Name[:i]
	}
	return ""
}

// ColorName is the part of Name after "$".
func (b *BookColor) ColorName() string {
	if i := strings.IndexByte(b.Name, '$'); i >= 0 {
		return b.Name[i+1:]
	}
	return b.Name
}

// PdfUnderlayDefinition is PDFDEFINITION.
type PdfUnderlayDefinition struct {
	NonGraphicalBase
	File string
	Page string
}

func (*PdfUnderlayDefinition) ObjectName() string     { return TokenObjectPdfDefinition }
func (*PdfUnderlayDefinition) SubclassMarker() string { return SubclassUnderlayDefinition }
func (*PdfUnderlayDefinition) ObjectType() ObjectType { return ObjectTypeUnlisted }

// GeoData is GEODATA: the geographic location of a drawing.
type GeoData struct {
	NonGraphicalBase
	Version                    GeoDataVersion
	HostBlock                  *BlockRecord
	CoordinatesType            DesignCoordinatesType
	DesignPoint                Vec3
	ReferencePoint             Vec3
	HorizontalUnitScale        float64
	HorizontalUnits            UnitsType
	VerticalUnitScale          float64
	VerticalUnits              UnitsType
	UpDirection                Vec3
	NorthDirection             Vec2
	ScaleEstimationMethod      ScaleEstimationType
	UserSpecifiedScaleFactor   float64
	EnableSeaLevelCorrection   bool
	SeaLevelElevation          float64
	CoordinateProjectionRadius float64
	CoordinateSystemDefinition string
	GeoRssTag                  string
	ObservationFromTag         string
	ObservationToTag           string
	ObservationCoverageTag     string
	Points                     []GeoMeshPoint
	Faces                      []GeoMeshFace
}

// GeoMeshPoint is GeoData.GeoMeshPoint.
type GeoMeshPoint struct{ Source, Destination Vec2 }

// GeoMeshFace is GeoData.GeoMeshFace.
type GeoMeshFace struct{ Index1, Index2, Index3 int32 }

func NewGeoData() *GeoData {
	return &GeoData{Version: GeoDataVersionR2013, CoordinatesType: DesignCoordinatesTypeLocalGrid,
		HorizontalUnitScale: 1, HorizontalUnits: UnitsTypeMeters, VerticalUnitScale: 1,
		VerticalUnits: UnitsTypeMeters, UpDirection: AxisZ, NorthDirection: Vec2{0, 1}}
}

func (*GeoData) ObjectName() string     { return TokenObjectGeoData }
func (*GeoData) SubclassMarker() string { return SubclassGeoData }
func (*GeoData) ObjectType() ObjectType { return ObjectTypeUnlisted }

// SpatialFilter is SPATIAL_FILTER (the XCLIP boundary of an INSERT, stored as
// ACAD_FILTER/SPATIAL in its extension dictionary).
type SpatialFilter struct {
	NonGraphicalBase
	BoundaryPoints         []Vec2
	Normal                 Vec3
	Origin                 Vec3
	DisplayBoundary        bool
	ClipFrontPlane         bool
	FrontDistance          float64
	ClipBackPlane          bool
	BackDistance           float64
	InverseInsertTransform [12]float64
	InsertTransform        [12]float64
}

func NewSpatialFilter() *SpatialFilter {
	f := &SpatialFilter{Normal: AxisZ}
	identity := [12]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}
	f.InsertTransform, f.InverseInsertTransform = identity, identity
	f.Name = "SPATIAL"
	return f
}

func (*SpatialFilter) ObjectName() string     { return TokenObjectSpatialFilter }
func (*SpatialFilter) SubclassMarker() string { return SubclassSpatialFilter }
func (*SpatialFilter) ObjectType() ObjectType { return ObjectTypeUnlisted }

// Material is MATERIAL. Only the identifying data is typed; the full
// definition is kept as group codes in Raw so that it survives a round trip.
type Material struct {
	NonGraphicalBase
	Description string
	Raw         []GroupCode
}

func (*Material) ObjectName() string     { return TokenObjectMaterial }
func (*Material) SubclassMarker() string { return SubclassMaterial }
func (*Material) ObjectType() ObjectType { return ObjectTypeUnlisted }

// VisualStyle is VISUALSTYLE, kept as group codes.
type VisualStyle struct {
	NonGraphicalBase
	Description string
	Type        int32
	Raw         []GroupCode
}

func (*VisualStyle) ObjectName() string     { return TokenObjectVisualStyle }
func (*VisualStyle) SubclassMarker() string { return SubclassVisualStyle }
func (*VisualStyle) ObjectType() ObjectType { return ObjectTypeUnlisted }

// AcdbPlaceHolder is ACDBPLACEHOLDER (default plot style name entry).
type AcdbPlaceHolder struct{ NonGraphicalBase }

func (*AcdbPlaceHolder) ObjectName() string     { return TokenObjectPlaceholder }
func (*AcdbPlaceHolder) SubclassMarker() string { return SubclassAcDbPlaceHolder }
func (*AcdbPlaceHolder) ObjectType() ObjectType { return ObjectTypeAcDbPlaceholder }

// ObjectContextDataBase is ObjectContextData + AnnotScaleObjectContextData.
type ObjectContextDataBase struct {
	NonGraphicalBase
	Version                      int16
	HasFileToExtensionDictionary bool
	Scale                        *Scale
}

// BlockReferenceObjectContextData is ACDB_BLKREFOBJECTCONTEXTDATA_CLASS.
type BlockReferenceObjectContextData struct {
	ObjectContextDataBase
	InsertionPoint Vec3
	// Rotation is in radians.
	Rotation float64
	XScale   float64
	YScale   float64
	ZScale   float64
}

func (*BlockReferenceObjectContextData) ObjectName() string { return TokenBlkRefObjectContextData }
func (*BlockReferenceObjectContextData) SubclassMarker() string {
	return SubclassBlkRefObjectContextData
}
func (*BlockReferenceObjectContextData) ObjectType() ObjectType { return ObjectTypeUnlisted }

// MTextAttributeObjectContextData is ACDB_MTEXTATTRIBUTEOBJECTCONTEXTDATA_CLASS.
type MTextAttributeObjectContextData struct {
	ObjectContextDataBase
	AttachmentPoint AttachmentPointType
	InsertPoint     Vec3
	AlignmentPoint  Vec3
	// Rotation is in radians.
	Rotation float64
	Value290 bool
}

func (*MTextAttributeObjectContextData) ObjectName() string {
	return TokenMTextAttributeObjectContextData
}
func (*MTextAttributeObjectContextData) SubclassMarker() string {
	return SubclassMTextAttributeObjectContextData
}
func (*MTextAttributeObjectContextData) ObjectType() ObjectType { return ObjectTypeUnlisted }

// DynamicBlockPurgePreventer is ACDB_DYNAMICBLOCKPURGEPREVENTER_VERSION.
type DynamicBlockPurgePreventer struct {
	NonGraphicalBase
	Version int16
	Block   *BlockRecord
}

func (*DynamicBlockPurgePreventer) ObjectName() string {
	return TokenObjectDynamicBlockPurgePreventer
}
func (*DynamicBlockPurgePreventer) SubclassMarker() string {
	return SubclassAcDbDynamicBlockPurgePreventer
}
func (*DynamicBlockPurgePreventer) ObjectType() ObjectType { return ObjectTypeUnlisted }

// CadValue is the value of a FIELD (and of table cells).
type CadValue struct {
	Flags          int32
	ValueType      CadValueType
	Units          CadValueUnitType
	Format         string
	FormattedValue string
	// Value is float64, int32, string, Vec3 or an Object (handle values).
	Value any
}

// Field is FIELD: a field expression and its cached values.
type Field struct {
	NonGraphicalBase
	EvaluatorId            string
	FieldCode              string
	Children               []*Field
	CadObjects             []Object
	EvaluationOptionFlags  EvaluationOptionFlags
	FilingOptionFlags      FilingOptionFlags
	FieldStateFlags        FieldStateFlags
	EvaluationStatusFlags  EvaluationStatusFlags
	EvaluationErrorCode    int32
	EvaluationErrorMessage string
	FormatString           string
	Value                  CadValue
	// Values are the named child values (code 6 keys), in file order.
	Values []FieldValue
}

// FieldValue is one named value of a FIELD.
type FieldValue struct {
	Key   string
	Value CadValue
}

func (*Field) ObjectName() string     { return TokenObjectField }
func (*Field) SubclassMarker() string { return SubclassField }
func (*Field) ObjectType() ObjectType { return ObjectTypeUnlisted }

// FieldList is FIELDLIST.
type FieldList struct {
	NonGraphicalBase
	Fields []*Field
}

func (*FieldList) ObjectName() string     { return TokenObjectFieldList }
func (*FieldList) SubclassMarker() string { return SubclassFieldList }
func (*FieldList) ObjectType() ObjectType { return ObjectTypeUnlisted }

// GroupCode is one raw DXF group: code and value as read (string, float64,
// int16, int32, int64, bool, []byte or uint64 for handles).
type GroupCode struct {
	Code  int
	Value any
}

// UnknownObject is an object without a typed model (ACadSharp's
// UnknownNonGraphicalObject, ProxyObject and the dynamic block, table style
// and association objects). The groups after the common object data are kept
// as read; writers skip unknown objects like ACadSharp does.
type UnknownObject struct {
	NonGraphicalBase
	DxfName string
	Class   *DxfClass
	Groups  []GroupCode
}

func (u *UnknownObject) ObjectName() string { return u.DxfName }
func (u *UnknownObject) SubclassMarker() string {
	for i := len(u.Groups) - 1; i >= 0; i-- {
		if u.Groups[i].Code == 100 {
			s, _ := u.Groups[i].Value.(string)
			return s
		}
	}
	return ""
}
func (*UnknownObject) ObjectType() ObjectType { return ObjectTypeUnlisted }
