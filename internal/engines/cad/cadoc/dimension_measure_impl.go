package cadoc

import (
	"errors"
	"math"
)

// ErrDegenerateDimension is returned when the definition points don't define
// a measurement (coincident points, zero-length directions).
var ErrDegenerateDimension = errors.New("cadoc: degenerate dimension definition points")

func finite(v float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, ErrDegenerateDimension
	}
	return v, nil
}

func validVec(v Vec3) bool {
	return !math.IsNaN(v.X) && !math.IsNaN(v.Y) && !math.IsNaN(v.Z) &&
		!math.IsInf(v.X, 0) && !math.IsInf(v.Y, 0) && !math.IsInf(v.Z, 0)
}

// angleBetween is CSMath's AngleBetweenVectors; it fails for zero vectors.
func angleBetween(u, v Vec3) (float64, error) {
	lu, lv := u.Length(), v.Length()
	if lu == 0 || lv == 0 || !validVec(u) || !validVec(v) {
		return 0, ErrDegenerateDimension
	}
	c := u.Dot(v) / (lu * lv)
	return math.Acos(math.Max(-1, math.Min(1, c))), nil
}

func isParallel(u, v Vec3) bool {
	c := u.Normalized().Cross(v.Normalized())
	return math.Abs(c.X) < 1e-12 && math.Abs(c.Y) < 1e-12 && math.Abs(c.Z) < 1e-12
}

// rotateAround rotates v around a unit axis (Rodrigues).
func rotateAround(v, axis Vec3, angle float64) Vec3 {
	k := axis.Normalized()
	if k.IsZero() {
		k = AxisZ
	}
	c, s := math.Cos(angle), math.Sin(angle)
	return v.Scale(c).Add(k.Cross(v).Scale(s)).Add(k.Scale(k.Dot(v) * (1 - c)))
}

// lineIntersection intersects the lines p1+t·d1 and p2+s·d2 (closest point
// on the first line); false for parallel or degenerate lines.
func lineIntersection(p1, d1, p2, d2 Vec3) (Vec3, bool) {
	a, b, c := d1.Dot(d1), d1.Dot(d2), d2.Dot(d2)
	w := p1.Sub(p2)
	d, e := d1.Dot(w), d2.Dot(w)
	den := a*c - b*b
	if a == 0 || c == 0 || math.Abs(den) < 1e-18*a*c {
		return Vec3{}, false
	}
	t := (b*e - c*d) / den
	return p1.Add(d1.Scale(t)), true
}

func (d *DimensionAligned) Measurement() (float64, error) {
	return finite(d.FirstPoint.DistanceTo(d.SecondPoint))
}

func (d *DimensionLinear) Measurement() (float64, error) {
	v := d.SecondPoint.Sub(d.FirstPoint)
	if v.IsZero() {
		return 0, ErrDegenerateDimension
	}
	dir := Vec3{math.Cos(d.Rotation), math.Sin(d.Rotation), 0}
	return finite(v.Length() * math.Abs(dir.Dot(v.Normalized())))
}

func (d *DimensionRadius) Measurement() (float64, error) {
	return finite(d.DefinitionPoint.DistanceTo(d.AngleVertex))
}

func (d *DimensionDiameter) Measurement() (float64, error) {
	return finite(d.DefinitionPoint.DistanceTo(d.AngleVertex))
}

// Center is the intersection of the two lines (NaN-free: ok is false when
// they are parallel).
func (d *DimensionAngular2Line) Center() (Vec3, bool) {
	return lineIntersection(d.DefinitionPoint, d.AngleVertex.Sub(d.DefinitionPoint),
		d.FirstPoint, d.SecondPoint.Sub(d.FirstPoint))
}

func (d *DimensionAngular2Line) Measurement() (float64, error) {
	v1 := d.SecondPoint.Sub(d.FirstPoint)
	v2 := d.DefinitionPoint.Sub(d.AngleVertex)
	angle, err := angleBetween(v1, v2)
	if err != nil {
		return 0, err
	}
	center, ok := d.Center()
	if !ok {
		return angle, nil
	}
	vArc := d.DimensionArc.Sub(center)
	if vArc.IsZero() {
		return angle, nil
	}
	dot1 := v1.Cross(vArc).Dot(d.Normal)
	dot2 := vArc.Cross(v2).Dot(d.Normal)
	if (dot1 < 0 && dot2 > 0) || (dot1 > 0 && dot2 < 0) {
		angle = math.Pi - angle
	}
	return finite(angle)
}

func (d *DimensionAngular3Pt) Measurement() (float64, error) {
	v1 := d.FirstPoint.Sub(d.AngleVertex)
	v2 := d.SecondPoint.Sub(d.AngleVertex)
	if v1 == v2 && validVec(v1) {
		return 0, nil
	}
	if v1.IsZero() || v2.IsZero() {
		return 0, ErrDegenerateDimension
	}
	if isParallel(v1, v2) {
		return math.Pi, nil
	}
	return angleBetween(v1, v2)
}

func (d *DimensionOrdinate) Measurement() (float64, error) {
	dir := Vec2{1, 0}
	if d.IsOrdinateTypeX() {
		dir = Vec2{0, 1}
	}
	s, c := math.Sin(d.HorizontalDirection), math.Cos(d.HorizontalDirection)
	dir = Vec2{dir.X*c - dir.Y*s, dir.X*s + dir.Y*c}
	def, feat := d.DefinitionPoint.XY(), d.FeatureLocation.XY()
	t := dir.Dot(def.Sub(feat))
	v := def.Sub(feat.Add(dir.Scale(t)))
	return finite(math.Sqrt(v.Dot(v)))
}

func (d *DimensionArc) Measurement() (float64, error) {
	v1 := d.FirstPoint.Sub(d.Center)
	v2 := d.SecondPoint.Sub(d.Center)
	r := v1.Length()
	if v1 == v2 && validVec(v1) {
		return 0, nil
	}
	if v1.IsZero() || v2.IsZero() {
		return 0, ErrDegenerateDimension
	}
	angle := math.Pi
	if !isParallel(v1, v2) {
		a, err := angleBetween(v1, v2)
		if err != nil {
			return 0, err
		}
		angle = a
	}
	return finite(r * angle)
}
