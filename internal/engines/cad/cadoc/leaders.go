package cadoc

// Leader is LEADER.
type Leader struct {
	EntityBase
	// Style is the dimension style (arrow size and scale come from it).
	Style                *DimensionStyle
	ArrowHeadEnabled     bool
	PathType             LeaderPathType
	CreationType         LeaderCreationType
	HookLineDirection    HookLineDirection
	HasHookline          bool
	TextHeight           float64
	TextWidth            float64
	Vertices             []Vec3
	AssociatedAnnotation Entity
	Normal               Vec3
	HorizontalDirection  Vec3
	BlockOffset          Vec3
	AnnotationOffset     Vec3
}

func NewLeader() *Leader {
	l := &Leader{CreationType: LeaderCreationTypeCreatedWithoutAnnotation, Normal: AxisZ, HorizontalDirection: AxisX,
		ArrowHeadEnabled: true}
	l.InitEntity()
	return l
}

func (*Leader) ObjectName() string     { return TokenEntityLeader }
func (*Leader) SubclassMarker() string { return SubclassLeader }
func (*Leader) ObjectType() ObjectType { return ObjectTypeLeader }

// MultiLeader is MULTILEADER.
type MultiLeader struct {
	EntityBase
	Style                   *MultiLeaderStyle
	ContextData             *MultiLeaderObjectContextData
	PropertyOverrideFlags   MultiLeaderPropertyOverrideFlags
	PathType                MultiLeaderPathType
	LineColor               Color
	LeaderLineType          *LineType
	LeaderLineWeight        LineWeight
	EnableLanding           bool
	EnableDogleg            bool
	LandingDistance         float64
	Arrowhead               *BlockRecord
	ArrowheadSize           float64
	ContentType             LeaderContentType
	TextStyle               *TextStyle
	TextLeftAttachment      TextAttachmentType
	TextRightAttachment     TextAttachmentType
	TextAngle               TextAngleType
	TextAlignment           TextAlignmentType
	TextColor               Color
	TextFrame               bool
	BlockContent            *BlockRecord
	BlockContentColor       Color
	BlockContentScale       Vec3
	BlockContentRotation    float64 // radians
	BlockContentConnection  BlockContentConnectionType
	EnableAnnotationScale   bool
	TextDirectionNegative   bool
	TextAligninIPE          int16
	TextAttachmentPoint     TextAttachmentPointType
	ScaleFactor             float64
	TextAttachmentDirection TextAttachmentDirectionType
	TextBottomAttachment    TextAttachmentType
	TextTopAttachment       TextAttachmentType
	ExtendedToText          bool
	BlockAttributes         []MultiLeaderBlockAttribute
}

// MultiLeaderBlockAttribute is MultiLeader.BlockAttribute.
type MultiLeaderBlockAttribute struct {
	AttributeDefinition *AttributeDefinition
	Index               int16
	Width               float64
	Text                string
}

func NewMultiLeader() *MultiLeader {
	m := &MultiLeader{LineColor: ColorByLayer, LeaderLineWeight: LineWeightByLayer, TextAlignment: TextAlignmentTypeLeft,
		TextAngle: TextAngleTypeHorizontal, BlockContentScale: Vec3{1, 1, 1}, ScaleFactor: 1,
		TextColor: ColorByBlock, BlockContentColor: ColorByBlock, ContextData: NewMultiLeaderObjectContextData()}
	m.InitEntity()
	return m
}

func (*MultiLeader) ObjectName() string     { return TokenEntityMultiLeader }
func (*MultiLeader) SubclassMarker() string { return SubclassMultiLeader }
func (*MultiLeader) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*MultiLeader) DxfClass() DxfClass {
	return entClass(TokenEntityMultiLeader, SubclassMultiLeader, MC0_0, proxyEraseDis)
}

// MultiLeaderObjectContextData is the CONTEXT_DATA{ block of a MULTILEADER
// (ACDB_MLEADEROBJECTCONTEXTDATA_CLASS).
type MultiLeaderObjectContextData struct {
	ObjectContextDataBase
	ScaleFactor            float64
	ContentBasePoint       Vec3
	TextHeight             float64
	ArrowheadSize          float64
	LandingGap             float64
	TextLeftAttachment     TextAttachmentType
	TextRightAttachment    TextAttachmentType
	TextAlignment          TextAlignmentType
	BlockContentConnection BlockContentConnectionType
	HasTextContents        bool
	TextLabel              string
	TextNormal             Vec3
	TextStyle              *TextStyle
	TextLocation           Vec3
	Direction              Vec3
	TextRotation           float64 // radians
	BoundaryWidth          float64
	BoundaryHeight         float64
	LineSpacingFactor      float64
	LineSpacing            LineSpacingStyle
	TextColor              Color
	TextAttachmentPoint    TextAttachmentPointType
	FlowDirection          FlowDirectionType
	BackgroundFillColor    Color
	BackgroundScaleFactor  float64
	BackgroundTransparency int32
	BackgroundFillEnabled  bool
	BackgroundMaskFillOn   bool
	ColumnType             int16
	TextHeightAutomatic    bool
	ColumnWidth            float64
	ColumnGutter           float64
	ColumnFlowReversed     bool
	ColumnSizes            []float64
	WordBreak              bool
	HasContentsBlock       bool
	BlockContent           *BlockRecord
	BlockContentNormal     Vec3
	BlockContentLocation   Vec3
	BlockContentScale      Vec3
	BlockContentRotation   float64 // radians
	BlockContentColor      Color
	TransformationMatrix   [16]float64
	BasePoint              Vec3
	BaseDirection          Vec3
	BaseVertical           Vec3
	NormalReversed         bool
	TextTopAttachment      TextAttachmentType
	TextBottomAttachment   TextAttachmentType
	LeaderRoots            []*MultiLeaderLeaderRoot
}

func NewMultiLeaderObjectContextData() *MultiLeaderObjectContextData {
	return &MultiLeaderObjectContextData{ArrowheadSize: 0.18, BaseDirection: AxisX, BaseVertical: AxisY,
		ScaleFactor: 1, TextNormal: AxisZ, Direction: AxisX, LineSpacingFactor: 1, BlockContentNormal: AxisZ,
		BlockContentScale: Vec3{1, 1, 1}, TextColor: ColorByBlock, BlockContentColor: ColorByBlock}
}

func (*MultiLeaderObjectContextData) ObjectName() string { return TokenObjectMLeaderContextData }
func (*MultiLeaderObjectContextData) SubclassMarker() string {
	return SubclassMultiLeaderObjectContextData
}
func (*MultiLeaderObjectContextData) ObjectType() ObjectType { return ObjectTypeUnlisted }

// MultiLeaderLeaderRoot is MultiLeaderObjectContextData.LeaderRoot.
type MultiLeaderLeaderRoot struct {
	ContentValid             bool
	Unknown                  bool
	ConnectionPoint          Vec3
	Direction                Vec3
	BreakStartEndPointsPairs []StartEndPointPair
	LeaderIndex              int32
	LandingDistance          float64
	Lines                    []*MultiLeaderLeaderLine
	TextAttachmentDirection  TextAttachmentDirectionType
}

// StartEndPointPair is MultiLeaderObjectContextData.StartEndPointPair.
type StartEndPointPair struct{ StartPoint, EndPoint Vec3 }

// MultiLeaderLeaderLine is MultiLeaderObjectContextData.LeaderLine.
type MultiLeaderLeaderLine struct {
	Points         []Vec3
	BreakInfoCount int32
	SegmentIndex   int32
	BreakInfo      []StartEndPointPair
	Index          int32
	PathType       MultiLeaderPathType
	LineColor      Color
	LineType       *LineType
	LineWeight     LineWeight
	ArrowheadSize  float64
	Arrowhead      *BlockRecord
	OverrideFlags  LeaderLinePropertOverrideFlags
}
