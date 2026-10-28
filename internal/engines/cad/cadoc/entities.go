package cadoc

import "math"

// Line is LINE.
type Line struct {
	EntityBase
	StartPoint Vec3
	EndPoint   Vec3
	Thickness  float64
	Normal     Vec3
}

func NewLine() *Line { e := &Line{Normal: AxisZ}; e.InitEntity(); return e }

func (*Line) ObjectName() string     { return TokenEntityLine }
func (*Line) SubclassMarker() string { return SubclassLine }
func (*Line) ObjectType() ObjectType { return ObjectTypeLine }

// Circle is CIRCLE.
type Circle struct {
	EntityBase
	Center    Vec3
	Radius    float64
	Thickness float64
	Normal    Vec3
}

func NewCircle() *Circle { e := &Circle{Normal: AxisZ, Radius: 1}; e.InitEntity(); return e }

func (*Circle) ObjectName() string     { return TokenEntityCircle }
func (*Circle) SubclassMarker() string { return SubclassCircle }
func (*Circle) ObjectType() ObjectType { return ObjectTypeCircle }

// Arc is ARC. Angles are in radians, counter-clockwise in the OCS.
type Arc struct {
	Circle
	StartAngle float64
	EndAngle   float64
}

func NewArc() *Arc {
	e := &Arc{Circle: Circle{Normal: AxisZ, Radius: 1}, EndAngle: math.Pi}
	e.InitEntity()
	return e
}

func (*Arc) ObjectName() string     { return TokenEntityArc }
func (*Arc) SubclassMarker() string { return SubclassArc }
func (*Arc) ObjectType() ObjectType { return ObjectTypeArc }

// Sweep is the counter-clockwise angle from StartAngle to EndAngle.
func (a *Arc) Sweep() float64 {
	s := a.EndAngle - a.StartAngle
	for s < 0 {
		s += 2 * math.Pi
	}
	return s
}

// Ellipse is ELLIPSE (WCS); parameters are in radians.
type Ellipse struct {
	EntityBase
	Center            Vec3
	MajorAxisEndPoint Vec3
	Normal            Vec3
	RadiusRatio       float64
	StartParameter    float64
	EndParameter      float64
	Thickness         float64
}

func NewEllipse() *Ellipse {
	e := &Ellipse{MajorAxisEndPoint: AxisX, Normal: AxisZ, RadiusRatio: 1, EndParameter: 2 * math.Pi}
	e.InitEntity()
	return e
}

func (*Ellipse) ObjectName() string     { return TokenEntityEllipse }
func (*Ellipse) SubclassMarker() string { return SubclassEllipse }
func (*Ellipse) ObjectType() ObjectType { return ObjectTypeEllipse }

// IsFullEllipse reports a closed ellipse.
func (e *Ellipse) IsFullEllipse() bool {
	return math.Abs(e.EndParameter-e.StartParameter-2*math.Pi) < 1e-9 ||
		(e.StartParameter == 0 && e.EndParameter == 2*math.Pi)
}

// MajorAxis is the length of the major axis (twice the radius).
func (e *Ellipse) MajorAxis() float64 { return 2 * e.MajorAxisEndPoint.Length() }

// MinorAxis is MajorAxis × RadiusRatio.
func (e *Ellipse) MinorAxis() float64 { return e.MajorAxis() * e.RadiusRatio }

// Point is POINT.
type Point struct {
	EntityBase
	Location  Vec3
	Thickness float64
	Normal    Vec3
	// Rotation is the X axis angle (radians) for PDMODE display.
	Rotation float64
}

func NewPoint() *Point { e := &Point{Normal: AxisZ}; e.InitEntity(); return e }

func (*Point) ObjectName() string     { return TokenEntityPoint }
func (*Point) SubclassMarker() string { return SubclassPoint }
func (*Point) ObjectType() ObjectType { return ObjectTypePoint }

// XLine is XLINE (infinite construction line).
type XLine struct {
	EntityBase
	FirstPoint Vec3
	Direction  Vec3
}

func NewXLine() *XLine { e := &XLine{Direction: AxisX}; e.InitEntity(); return e }

func (*XLine) ObjectName() string     { return TokenEntityXline }
func (*XLine) SubclassMarker() string { return SubclassXLine }
func (*XLine) ObjectType() ObjectType { return ObjectTypeXLine }

// Ray is RAY (semi-infinite line).
type Ray struct {
	EntityBase
	StartPoint Vec3
	Direction  Vec3
}

func NewRay() *Ray { e := &Ray{Direction: AxisX}; e.InitEntity(); return e }

func (*Ray) ObjectName() string     { return TokenEntityRay }
func (*Ray) SubclassMarker() string { return SubclassRay }
func (*Ray) ObjectType() ObjectType { return ObjectTypeRay }

// Solid is SOLID (and TRACE, see IsTrace): a filled triangle or quad whose
// corners run 1-2-4-3 around the outline.
type Solid struct {
	EntityBase
	FirstCorner  Vec3
	SecondCorner Vec3
	ThirdCorner  Vec3
	FourthCorner Vec3
	Thickness    float64
	Normal       Vec3
	// IsTrace is set for TRACE entities, which have the same data; ACadSharp
	// reads them as SOLID, and writers write them as SOLID too.
	IsTrace bool
}

func NewSolid() *Solid { e := &Solid{Normal: AxisZ}; e.InitEntity(); return e }

func (*Solid) ObjectName() string     { return TokenEntitySolid }
func (*Solid) SubclassMarker() string { return SubclassSolid }
func (*Solid) ObjectType() ObjectType { return ObjectTypeSolid }

// Face3D is 3DFACE.
type Face3D struct {
	EntityBase
	FirstCorner  Vec3
	SecondCorner Vec3
	ThirdCorner  Vec3
	FourthCorner Vec3
	Flags        InvisibleEdgeFlags
}

func NewFace3D() *Face3D { e := &Face3D{}; e.InitEntity(); return e }

func (*Face3D) ObjectName() string     { return TokenEntity3DFace }
func (*Face3D) SubclassMarker() string { return SubclassFace3d }
func (*Face3D) ObjectType() ObjectType { return ObjectTypeFace3D }

// Shape is SHAPE: a shape from a shape file (text style with the shape flag).
type Shape struct {
	EntityBase
	Thickness      float64
	InsertionPoint Vec3
	Size           float64
	// ShapeStyle is the shape file; ShapeName the shape (code 2).
	ShapeStyle *TextStyle
	ShapeName  string
	ShapeIndex uint16
	// Rotation and ObliqueAngle are in radians.
	Rotation       float64
	RelativeXScale float64
	ObliqueAngle   float64
	Normal         Vec3
}

func NewShape() *Shape {
	e := &Shape{Size: 1, RelativeXScale: 1, Normal: AxisZ}
	e.InitEntity()
	return e
}

func (*Shape) ObjectName() string     { return TokenEntityShape }
func (*Shape) SubclassMarker() string { return SubclassShape }
func (*Shape) ObjectType() ObjectType { return ObjectTypeShape }

// Seqend is SEQEND, the end marker of a polyline's vertices or an insert's
// attributes. A stray one can be a block entity of its own in old files.
type Seqend struct{ EntityBase }

func NewSeqend() *Seqend { e := &Seqend{}; e.InitEntity(); return e }

func (*Seqend) ObjectName() string     { return TokenEntitySeqend }
func (*Seqend) SubclassMarker() string { return SubclassEntity }
func (*Seqend) ObjectType() ObjectType { return ObjectTypeSeqend }

// Tolerance is TOLERANCE (feature control frame).
type Tolerance struct {
	EntityBase
	Style          *DimensionStyle
	InsertionPoint Vec3
	Direction      Vec3
	Normal         Vec3
	Text           string
}

func NewTolerance() *Tolerance {
	e := &Tolerance{Direction: AxisX, Normal: AxisZ}
	e.InitEntity()
	return e
}

func (*Tolerance) ObjectName() string     { return TokenEntityTolerance }
func (*Tolerance) SubclassMarker() string { return SubclassTolerance }
func (*Tolerance) ObjectType() ObjectType { return ObjectTypeTolerance }

// UnknownEntity is an entity type without a model. Its groups after the
// common entity data are kept as read (ACadSharp's UnknownEntity keeps only
// the common data). Writers write it back only when WriteConfig asks.
type UnknownEntity struct {
	EntityBase
	DxfName string
	Class   *DxfClass
	Groups  []GroupCode
}

func (u *UnknownEntity) ObjectName() string { return u.DxfName }
func (u *UnknownEntity) SubclassMarker() string {
	for i := len(u.Groups) - 1; i >= 0; i-- {
		if u.Groups[i].Code == 100 {
			s, _ := u.Groups[i].Value.(string)
			return s
		}
	}
	return SubclassEntity
}
func (*UnknownEntity) ObjectType() ObjectType { return ObjectTypeUnlisted }
