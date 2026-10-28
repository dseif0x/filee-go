package cadoc

// DimensionBase is the AcDbDimension data shared by all dimension kinds.
type DimensionBase struct {
	EntityBase
	// Block is the anonymous block (*Dn) with the dimension's picture.
	Block *BlockRecord
	// Style nil means the document's Standard style.
	Style           *DimensionStyle
	DefinitionPoint Vec3
	TextMiddlePoint Vec3
	InsertionPoint  Vec3
	Normal          Vec3
	// Flags holds the dimension type in its low bits plus BlockReference,
	// OrdinateTypeX and TextUserDefinedLocation.
	Flags             DimensionType
	AttachmentPoint   AttachmentPointType
	LineSpacingStyle  LineSpacingStyleType
	LineSpacingFactor float64
	// Text overrides the measurement text; "<>" stands for the measurement.
	// Empty means the measured value.
	Text string
	// TextRotation and HorizontalDirection are in radians.
	TextRotation        float64
	HorizontalDirection float64
	FlipArrow1          bool
	FlipArrow2          bool
	Version             uint8
	// MeasurementValue is code 42 as read (writers recompute it).
	MeasurementValue float64
}

func (d *DimensionBase) initDimension(t DimensionType) {
	d.InitEntity()
	d.Normal = AxisZ
	d.Flags = t | DimensionTypeBlockReference
	d.AttachmentPoint = AttachmentPointTypeMiddleCenter
	d.LineSpacingStyle = LineSpacingStyleTypeAtLeast
	d.LineSpacingFactor = 1
}

// Dimension returns the shared dimension data.
func (d *DimensionBase) Dimension() *DimensionBase { return d }

func (*DimensionBase) ObjectName() string { return TokenEntityDimension }

// IsTextUserDefinedLocation reports the user-positioned text flag.
func (d *DimensionBase) IsTextUserDefinedLocation() bool {
	return d.Flags&DimensionTypeTextUserDefinedLocation != 0
}

// SetTextUserDefinedLocation sets or clears the user-positioned text flag.
func (d *DimensionBase) SetTextUserDefinedLocation(v bool) {
	if v {
		d.Flags |= DimensionTypeTextUserDefinedLocation
	} else {
		d.Flags &^= DimensionTypeTextUserDefinedLocation
	}
}

// Kind is the dimension type without the flag bits.
func (d *DimensionBase) Kind() DimensionType { return d.Flags & 0x0F }

// IsAngular reports angular dimensions.
func (d *DimensionBase) IsAngular() bool {
	k := d.Kind()
	return k == DimensionTypeAngular || k == DimensionTypeAngular3Point
}

// HasStyleOverride reports a DSTYLE override in the ACAD xdata.
func (d *DimensionBase) HasStyleOverride() bool {
	x, ok := d.GetExtendedData(DefaultAppIdName)
	if !ok || len(x.Records) == 0 {
		return false
	}
	return x.Records[0].Code == 1000 && x.Records[0].String() == StyleOverrideEntryName
}

// Dimension is implemented by every dimension kind.
type Dimension interface {
	Entity
	Dimension() *DimensionBase
	// Measurement is the measured value (length, or angle in radians); it
	// fails for degenerate definition points.
	Measurement() (float64, error)
}

// DimensionAligned is an aligned dimension.
type DimensionAligned struct {
	DimensionBase
	FirstPoint      Vec3
	SecondPoint     Vec3
	ExtLineRotation float64
}

func NewDimensionAligned() *DimensionAligned {
	d := &DimensionAligned{}
	d.initDimension(DimensionTypeAligned)
	return d
}

func (*DimensionAligned) SubclassMarker() string { return SubclassAlignedDimension }
func (*DimensionAligned) ObjectType() ObjectType { return ObjectTypeDimensionAligned }

// DimensionLinear is a rotated (linear) dimension.
type DimensionLinear struct {
	DimensionAligned
	// Rotation is in radians.
	Rotation float64
}

func NewDimensionLinear() *DimensionLinear {
	d := &DimensionLinear{}
	d.initDimension(DimensionTypeLinear)
	return d
}

func (*DimensionLinear) SubclassMarker() string { return SubclassLinearDimension }
func (*DimensionLinear) ObjectType() ObjectType { return ObjectTypeDimensionLinear }

// DimensionRadius is a radial dimension.
type DimensionRadius struct {
	DimensionBase
	AngleVertex  Vec3
	LeaderLength float64
}

func NewDimensionRadius() *DimensionRadius {
	d := &DimensionRadius{}
	d.initDimension(DimensionTypeRadius)
	return d
}

func (*DimensionRadius) SubclassMarker() string { return SubclassRadialDimension }
func (*DimensionRadius) ObjectType() ObjectType { return ObjectTypeDimensionRadius }

// DimensionDiameter is a diameter dimension.
type DimensionDiameter struct {
	DimensionBase
	AngleVertex  Vec3
	LeaderLength float64
}

func NewDimensionDiameter() *DimensionDiameter {
	d := &DimensionDiameter{}
	d.initDimension(DimensionTypeDiameter)
	return d
}

func (*DimensionDiameter) SubclassMarker() string { return SubclassDiametricDimension }
func (*DimensionDiameter) ObjectType() ObjectType { return ObjectTypeDimensionDiameter }

// Center is the circle centre (midpoint of the two definition points).
func (d *DimensionDiameter) Center() Vec3 {
	return d.DefinitionPoint.Add(d.AngleVertex).Scale(0.5)
}

// DimensionAngular2Line is an angular dimension between two lines.
type DimensionAngular2Line struct {
	DimensionBase
	FirstPoint   Vec3
	SecondPoint  Vec3
	AngleVertex  Vec3
	DimensionArc Vec3
}

func NewDimensionAngular2Line() *DimensionAngular2Line {
	d := &DimensionAngular2Line{}
	d.initDimension(DimensionTypeAngular)
	return d
}

func (*DimensionAngular2Line) SubclassMarker() string { return SubclassAngular2LineDimension }
func (*DimensionAngular2Line) ObjectType() ObjectType { return ObjectTypeDimensionAng2Ln }

// DimensionAngular3Pt is an angular dimension from a vertex and two points.
type DimensionAngular3Pt struct {
	DimensionBase
	FirstPoint  Vec3
	SecondPoint Vec3
	AngleVertex Vec3
}

func NewDimensionAngular3Pt() *DimensionAngular3Pt {
	d := &DimensionAngular3Pt{}
	d.initDimension(DimensionTypeAngular3Point)
	return d
}

func (*DimensionAngular3Pt) SubclassMarker() string { return SubclassAngular3PointDimension }
func (*DimensionAngular3Pt) ObjectType() ObjectType { return ObjectTypeDimensionAng3Pt }

// DimensionOrdinate is an ordinate dimension.
type DimensionOrdinate struct {
	DimensionBase
	FeatureLocation Vec3
	LeaderEndpoint  Vec3
}

func NewDimensionOrdinate() *DimensionOrdinate {
	d := &DimensionOrdinate{}
	d.initDimension(DimensionTypeOrdinate)
	return d
}

func (*DimensionOrdinate) SubclassMarker() string { return SubclassOrdinateDimension }
func (*DimensionOrdinate) ObjectType() ObjectType { return ObjectTypeDimensionOrdinate }

// IsOrdinateTypeX reports an X-datum ordinate.
func (d *DimensionOrdinate) IsOrdinateTypeX() bool { return d.Flags&DimensionTypeOrdinateTypeX != 0 }

// DimensionArc is ARC_DIMENSION (arc length).
type DimensionArc struct {
	DimensionBase
	FirstPoint   Vec3
	SecondPoint  Vec3
	Center       Vec3
	IsPartial    bool
	StartAngle   float64
	EndAngle     float64
	HasLeader    bool
	LeaderPoint1 Vec3
	LeaderPoint2 Vec3
}

func NewDimensionArc() *DimensionArc {
	d := &DimensionArc{}
	d.initDimension(DimensionTypeAngular3Point)
	return d
}

func (*DimensionArc) ObjectName() string     { return TokenEntityArcDimension }
func (*DimensionArc) SubclassMarker() string { return SubclassArcDimension }
func (*DimensionArc) ObjectType() ObjectType { return ObjectTypeUnlisted }

func (*DimensionArc) DxfClass() DxfClass {
	return entClass(TokenEntityArcDimension, SubclassArcDimension, AC1018, proxyEraseDis)
}
