package cadoc

// Hatch is HATCH: a filled or patterned area bounded by paths (in the OCS of
// Normal at Elevation).
type Hatch struct {
	EntityBase
	Elevation     float64
	Normal        Vec3
	Pattern       *HatchPattern
	IsSolid       bool
	IsAssociative bool
	IsDouble      bool
	Style         HatchStyleType
	PatternType   HatchPatternType
	// PatternAngle is in radians.
	PatternAngle  float64
	PatternScale  float64
	PixelSize     float64
	Paths         []*HatchBoundaryPath
	SeedPoints    []Vec2
	GradientColor *HatchGradientPattern
}

func NewHatch() *Hatch {
	h := &Hatch{Normal: AxisZ, Pattern: NewHatchPattern("SOLID"), Style: HatchStyleTypeNormal,
		PatternScale: 1, GradientColor: &HatchGradientPattern{}}
	h.InitEntity()
	return h
}

func (*Hatch) ObjectName() string     { return TokenEntityHatch }
func (*Hatch) SubclassMarker() string { return SubclassHatch }
func (*Hatch) ObjectType() ObjectType { return ObjectTypeHatch }

// HatchBoundaryPath is Hatch.BoundaryPath: a loop of edges (or one polyline).
type HatchBoundaryPath struct {
	Flags BoundaryPathFlags
	Edges []HatchEdge
	// Entities are the associated boundary objects (code 330).
	Entities []Entity
}

// IsPolyline reports a path made of one polyline edge.
func (p *HatchBoundaryPath) IsPolyline() bool {
	if len(p.Edges) != 1 {
		return false
	}
	_, ok := p.Edges[0].(*HatchPolylineEdge)
	return ok
}

// EffectiveFlags is Flags with the polyline bit matching the edges, as
// ACadSharp's getter computes it.
func (p *HatchBoundaryPath) EffectiveFlags() BoundaryPathFlags {
	if p.IsPolyline() {
		return p.Flags | BoundaryPathFlagsPolyline
	}
	return p.Flags &^ BoundaryPathFlagsPolyline
}

// HatchEdge is Hatch.BoundaryPath.Edge: *HatchLineEdge, *HatchArcEdge,
// *HatchEllipseEdge, *HatchSplineEdge or *HatchPolylineEdge.
type HatchEdge interface {
	EdgeType() HatchEdgeType
}

// HatchLineEdge is a line edge.
type HatchLineEdge struct{ Start, End Vec2 }

// HatchArcEdge is an arc edge; angles in radians. Clockwise edges store the
// angles mirrored at the X axis.
type HatchArcEdge struct {
	Center           Vec2
	Radius           float64
	StartAngle       float64
	EndAngle         float64
	CounterClockWise bool
}

// HatchEllipseEdge is an elliptic arc edge; MajorAxisEndPoint is relative to
// Center, angles in radians.
type HatchEllipseEdge struct {
	Center            Vec2
	MajorAxisEndPoint Vec2
	RadiusRatio       float64
	StartAngle        float64
	EndAngle          float64
	CounterClockWise  bool
}

// HatchSplineEdge is a spline edge; a rational spline keeps each control
// point's weight in its Z.
type HatchSplineEdge struct {
	Degree        int32
	IsRational    bool
	IsPeriodic    bool
	Knots         []float64
	ControlPoints []Vec3
	FitPoints     []Vec2
	StartTangent  Vec2
	EndTangent    Vec2
}

// HatchPolylineEdge is a polyline edge; each vertex keeps its bulge in Z.
type HatchPolylineEdge struct {
	IsClosed bool
	Vertices []Vec3
}

func (*HatchLineEdge) EdgeType() HatchEdgeType     { return HatchEdgeTypeLine }
func (*HatchArcEdge) EdgeType() HatchEdgeType      { return HatchEdgeTypeCircularArc }
func (*HatchEllipseEdge) EdgeType() HatchEdgeType  { return HatchEdgeTypeEllipticArc }
func (*HatchSplineEdge) EdgeType() HatchEdgeType   { return HatchEdgeTypeSpline }
func (*HatchPolylineEdge) EdgeType() HatchEdgeType { return HatchEdgeTypePolyline }

// HasBulge reports a non-zero bulge.
func (p *HatchPolylineEdge) HasBulge() bool {
	for _, v := range p.Vertices {
		if v.Z != 0 {
			return true
		}
	}
	return false
}

// Weights returns the control point weights of a rational spline edge.
func (s *HatchSplineEdge) Weights() []float64 {
	w := make([]float64, len(s.ControlPoints))
	for i, p := range s.ControlPoints {
		w[i] = p.Z
	}
	return w
}

// HatchPattern is the pattern of a hatch: line families in the hatch plane,
// already rotated and scaled as stored in the file.
type HatchPattern struct {
	Name        string
	Description string
	Lines       []HatchPatternLine
}

func NewHatchPattern(name string) *HatchPattern { return &HatchPattern{Name: name} }

// HatchPatternLine is HatchPattern.Line; Angle in radians.
type HatchPatternLine struct {
	Angle       float64
	BasePoint   Vec2
	Offset      Vec2
	DashLengths []float64
}

// HatchGradientPattern is the gradient fill of a hatch.
type HatchGradientPattern struct {
	Enabled               bool
	Reserved              int32
	Angle                 float64
	Shift                 float64
	IsSingleColorGradient bool
	ColorTint             float64
	Colors                []GradientColor
	Name                  string
}

// GradientColor is one gradient colour stop.
type GradientColor struct {
	Value float64
	Color Color
}
