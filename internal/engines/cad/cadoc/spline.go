package cadoc

// Spline is SPLINE (NURBS curve in WCS).
type Spline struct {
	EntityBase
	Normal                Vec3
	Flags                 SplineFlags
	Flags1                SplineFlags1
	KnotParametrization   KnotParametrization
	Degree                int32
	Knots                 []float64
	Weights               []float64
	ControlPoints         []Vec3
	FitPoints             []Vec3
	KnotTolerance         float64
	ControlPointTolerance float64
	FitTolerance          float64
	StartTangent          Vec3
	EndTangent            Vec3
}

func NewSpline() *Spline {
	s := &Spline{Normal: AxisZ, Degree: 3, KnotTolerance: 1e-7, ControlPointTolerance: 1e-7, FitTolerance: 1e-10}
	s.InitEntity()
	return s
}

func (*Spline) ObjectName() string     { return TokenEntitySpline }
func (*Spline) SubclassMarker() string { return SubclassSpline }
func (*Spline) ObjectType() ObjectType { return ObjectTypeSpline }

func (s *Spline) IsClosed() bool   { return s.Flags&SplineFlagsClosed != 0 }
func (s *Spline) IsPeriodic() bool { return s.Flags&SplineFlagsPeriodic != 0 }
func (s *Spline) IsRational() bool { return s.Flags&SplineFlagsRational != 0 }

// HasValidKnotCount checks len(Knots) against the control points and degree.
func (s *Spline) HasValidKnotCount() bool {
	m := 1
	if s.IsClosed() {
		m = 2
	}
	return len(s.Knots) == len(s.ControlPoints)+m*int(s.Degree)+1
}
