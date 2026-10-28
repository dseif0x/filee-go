package cadoc

// MLine is MLINE (multiline).
type MLine struct {
	EntityBase
	Style         *MLineStyle
	ScaleFactor   float64
	Justification MLineJustification
	Flags         MLineFlags
	StartPoint    Vec3
	Normal        Vec3
	Vertices      []MLineVertex
}

// MLineVertex is MLine.Vertex.
type MLineVertex struct {
	Position  Vec3
	Direction Vec3
	Miter     Vec3
	Segments  []MLineSegment
}

// MLineSegment is MLine.Vertex.Segment: the parameters of one style element.
type MLineSegment struct {
	Parameters         []float64
	AreaFillParameters []float64
}

func NewMLine() *MLine { m := &MLine{ScaleFactor: 1, Normal: AxisZ}; m.InitEntity(); return m }

func (*MLine) ObjectName() string     { return TokenEntityMLine }
func (*MLine) SubclassMarker() string { return SubclassMLine }
func (*MLine) ObjectType() ObjectType { return ObjectTypeMLine }

// Mesh is MESH (subdivision mesh, R2010+).
type Mesh struct {
	EntityBase
	Version          int16
	BlendCrease      bool
	SubdivisionLevel int32
	Vertices         []Vec3
	// Faces are lists of vertex indexes (0-based).
	Faces [][]int32
	Edges []MeshEdge
}

// MeshEdge is Mesh.Edge; Crease nil means no crease value.
type MeshEdge struct {
	Start, End int32
	Crease     *float64
}

func NewMesh() *Mesh { m := &Mesh{Version: 2}; m.InitEntity(); return m }

func (*Mesh) ObjectName() string     { return TokenEntityMesh }
func (*Mesh) SubclassMarker() string { return SubclassMesh }
func (*Mesh) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*Mesh) DxfClass() DxfClass { return entClass(TokenEntityMesh, SubclassMesh, 0, 0) }

// Viewport is VIEWPORT (paper space view into model space).
type Viewport struct {
	EntityBase
	Center         Vec3
	Width          float64
	Height         float64
	ActiveStatus   int16
	Id             int16
	ViewCenter     Vec2
	SnapBase       Vec2
	SnapSpacing    Vec2
	GridSpacing    Vec2
	ViewDirection  Vec3
	ViewTarget     Vec3
	LensLength     float64
	FrontClipPlane float64
	BackClipPlane  float64
	ViewHeight     float64
	// SnapAngle and TwistAngle are in radians.
	SnapAngle              float64
	TwistAngle             float64
	CircleZoomPercent      int16
	FrozenLayers           []*Layer
	Status                 ViewportStatusFlags
	Boundary               Entity
	StyleSheetName         string
	RenderMode             RenderMode
	UcsPerViewport         bool
	DisplayUcsIcon         bool
	UcsOrigin              Vec3
	UcsXAxis               Vec3
	UcsYAxis               Vec3
	NamedUcs               *UCS
	BaseUcs                *UCS
	UcsOrthographicType    OrthographicType
	Elevation              float64
	ShadePlotMode          ShadePlotMode
	MajorGridLineFrequency int16
	VisualStyle            Object
	UseDefaultLighting     bool
	DefaultLightingType    LightingType
	Brightness             float64
	Contrast               float64
	AmbientLightColor      Color
}

func NewViewport() *Viewport {
	v := &Viewport{ActiveStatus: 1, ViewDirection: AxisZ, UcsXAxis: AxisX, UcsYAxis: AxisY, ViewHeight: 1,
		LensLength: 50, CircleZoomPercent: 1000}
	v.InitEntity()
	return v
}

func (*Viewport) ObjectName() string     { return TokenEntityViewport }
func (*Viewport) SubclassMarker() string { return SubclassViewport }
func (*Viewport) ObjectType() ObjectType { return ObjectTypeViewport }

// RepresentsPaper reports the overall paper space viewport (id 1).
func (v *Viewport) RepresentsPaper() bool { return v.Id == 1 }

// ViewWidth is ViewHeight scaled by the viewport's aspect ratio.
func (v *Viewport) ViewWidth() float64 {
	if v.Height == 0 {
		return 0
	}
	return v.ViewHeight / v.Height * v.Width
}

// WipeoutBase is CadWipeoutBase, shared by IMAGE and WIPEOUT.
type WipeoutBase struct {
	EntityBase
	ClassVersion         int32
	InsertPoint          Vec3
	UVector              Vec3
	VVector              Vec3
	Size                 Vec2
	Definition           *ImageDefinition
	DefinitionReactor    *ImageDefinitionReactor
	Flags                ImageDisplayFlags
	ClippingState        bool
	Brightness           uint8
	Contrast             uint8
	Fade                 uint8
	ClipType             ClipType
	ClipMode             ClipMode
	ClipBoundaryVertices []Vec2
}

func (w *WipeoutBase) initWipeout() {
	w.InitEntity()
	w.UVector, w.VVector = AxisX, AxisY
	w.Brightness, w.Contrast = 50, 50
	w.Flags = ImageDisplayFlagsShowImage | ImageDisplayFlagsShowNotAlignedImage | ImageDisplayFlagsUseClippingBoundary
	w.ClipType = ClipTypeRectangular
}

// Wipeout returns the shared image data.
func (w *WipeoutBase) Wipeout() *WipeoutBase { return w }

// RasterImage is IMAGE.
type RasterImage struct{ WipeoutBase }

func NewRasterImage(def *ImageDefinition) *RasterImage {
	r := &RasterImage{}
	r.initWipeout()
	r.Definition = def
	return r
}

func (*RasterImage) ObjectName() string     { return TokenEntityImage }
func (*RasterImage) SubclassMarker() string { return SubclassRasterImage }
func (*RasterImage) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*RasterImage) DxfClass() DxfClass {
	c := entClass(TokenEntityImage, SubclassRasterImage, 20, proxyImage)
	c.ApplicationName = "ISM"
	return c
}

// Wipeout is WIPEOUT.
type Wipeout struct{ WipeoutBase }

func NewWipeout() *Wipeout { w := &Wipeout{}; w.initWipeout(); return w }

func (*Wipeout) ObjectName() string     { return TokenEntityWipeout }
func (*Wipeout) SubclassMarker() string { return SubclassWipeout }
func (*Wipeout) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*Wipeout) DxfClass() DxfClass {
	c := entClass(TokenEntityWipeout, SubclassWipeout, AC1015, proxyImage)
	c.ApplicationName = "WipeOut"
	return c
}

// Ole2Frame is OLE2FRAME.
type Ole2Frame struct {
	EntityBase
	Version           int16
	SourceApplication string
	UpperLeftCorner   Vec3
	LowerRightCorner  Vec3
	OleObjectType     OleObjectType
	IsPaperSpace      bool
	BinaryData        []byte
}

func NewOle2Frame() *Ole2Frame {
	o := &Ole2Frame{Version: 2, UpperLeftCorner: Vec3{1, 1, 0}, OleObjectType: OleObjectTypeEmbedded}
	o.InitEntity()
	return o
}

func (*Ole2Frame) ObjectName() string     { return TokenEntityOle2Frame }
func (*Ole2Frame) SubclassMarker() string { return SubclassOle2Frame }
func (*Ole2Frame) ObjectType() ObjectType { return ObjectTypeOle2Frame }

// PdfUnderlay is PDFUNDERLAY.
type PdfUnderlay struct {
	EntityBase
	Definition           *PdfUnderlayDefinition
	InsertPoint          Vec3
	XScale               float64
	YScale               float64
	ZScale               float64
	Rotation             float64 // radians
	Normal               Vec3
	Flags                UnderlayDisplayFlags
	Contrast             uint8
	Fade                 uint8
	ClipBoundaryVertices []Vec2
}

func NewPdfUnderlay() *PdfUnderlay {
	p := &PdfUnderlay{XScale: 1, YScale: 1, ZScale: 1, Normal: AxisZ, Flags: UnderlayDisplayFlagsDefault, Contrast: 100}
	p.InitEntity()
	return p
}

func (*PdfUnderlay) ObjectName() string     { return TokenEntityPdfUnderlay }
func (*PdfUnderlay) SubclassMarker() string { return SubclassUnderlay }
func (*PdfUnderlay) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*PdfUnderlay) DxfClass() DxfClass {
	return entClass(TokenEntityPdfUnderlay, SubclassPdfReference, 26, proxyAll)
}

// ModelerGeometry is the ACIS data of REGION, 3DSOLID, BODY and surfaces.
type ModelerGeometry struct {
	EntityBase
	ModelerFormatVersion int16
	// AcisText is the decoded SAT text (one line per record); AcisData the
	// SAB bytes of AC1027+ files from the ACDSDATA section.
	AcisText string
	AcisData []byte
	// Guid is the ACDSDATA record id of AC1027+ files ({...} form).
	Guid string
}

func (m *ModelerGeometry) initModeler() { m.InitEntity(); m.ModelerFormatVersion = 1 }

// Modeler returns the ACIS data.
func (m *ModelerGeometry) Modeler() *ModelerGeometry { return m }

// IsBinaryAcisData reports SAB data ("ACIS BinaryFile", or "ASM BinaryFile"
// as AutoCAD 2013+ writes it).
func (m *ModelerGeometry) IsBinaryAcisData() bool {
	d := m.AcisData
	return (len(d) >= 15 && string(d[:15]) == "ACIS BinaryFile") || (len(d) >= 14 && string(d[:14]) == "ASM BinaryFile")
}

// Region is REGION.
type Region struct{ ModelerGeometry }

func NewRegion() *Region { r := &Region{}; r.initModeler(); return r }

func (*Region) ObjectName() string     { return TokenEntityRegion }
func (*Region) SubclassMarker() string { return SubclassModelerGeometry }
func (*Region) ObjectType() ObjectType { return ObjectTypeRegion }

// Solid3D is 3DSOLID.
type Solid3D struct {
	ModelerGeometry
	History Object
}

func NewSolid3D() *Solid3D { s := &Solid3D{}; s.initModeler(); return s }

func (*Solid3D) ObjectName() string     { return TokenEntity3DSolid }
func (*Solid3D) SubclassMarker() string { return SubclassSolid3D }
func (*Solid3D) ObjectType() ObjectType { return ObjectTypeSolid3D }

// CadBody is BODY.
type CadBody struct{ ModelerGeometry }

func NewCadBody() *CadBody { b := &CadBody{}; b.initModeler(); return b }

func (*CadBody) ObjectName() string     { return TokenEntityBody }
func (*CadBody) SubclassMarker() string { return SubclassModelerGeometry }
func (*CadBody) ObjectType() ObjectType { return ObjectTypeBody }

// Surface is SURFACE and its kinds (EXTRUDEDSURFACE, LOFTEDSURFACE, …): the
// DXF name is kept; the kind-specific data is kept as group codes.
type Surface struct {
	ModelerGeometry
	DxfName   string
	UIsolines int16
	VIsolines int16
	Groups    []GroupCode
}

func (s *Surface) ObjectName() string {
	if s.DxfName == "" {
		return TokenEntitySurface
	}
	return s.DxfName
}
func (*Surface) SubclassMarker() string { return SubclassSurface }
func (*Surface) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (s *Surface) DxfClass() DxfClass {
	return entClass(s.ObjectName(), SubclassSurface, 26, proxyAll)
}

// ProxyEntity is ACAD_PROXY_ENTITY.
type ProxyEntity struct {
	EntityBase
	ProxyClassId          int32
	ClassNumber           int32
	Class                 *DxfClass
	Version               int16
	MaintenanceVersion    int32
	OriginalDataFormatDxf bool
	BinaryData            []byte
	StringData            []byte
}

func NewProxyEntity() *ProxyEntity { p := &ProxyEntity{ProxyClassId: 498}; p.InitEntity(); return p }

func (*ProxyEntity) ObjectName() string     { return TokenEntityProxyEntity }
func (*ProxyEntity) SubclassMarker() string { return SubclassProxyEntity }
func (*ProxyEntity) ObjectType() ObjectType { return ObjectTypeAcadProxyEntity }
