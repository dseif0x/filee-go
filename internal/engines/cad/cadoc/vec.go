package cadoc

import "math"

// Vec2 is CSMath.XY.
type Vec2 struct{ X, Y float64 }

// Vec3 is CSMath.XYZ.
type Vec3 struct{ X, Y, Z float64 }

var (
	AxisX = Vec3{1, 0, 0}
	AxisY = Vec3{0, 1, 0}
	AxisZ = Vec3{0, 0, 1}
)

func (a Vec2) Add(b Vec2) Vec2           { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2           { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Scale(s float64) Vec2      { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float64        { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Length() float64           { return math.Hypot(a.X, a.Y) }
func (a Vec2) IsZero() bool              { return a.X == 0 && a.Y == 0 }
func (a Vec2) Vec3(z float64) Vec3       { return Vec3{a.X, a.Y, z} }
func (a Vec2) DistanceTo(b Vec2) float64 { return a.Sub(b).Length() }

// Normalized returns the unit vector; the zero vector stays zero.
func (a Vec2) Normalized() Vec2 {
	l := a.Length()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Dot(b Vec3) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a Vec3) Length() float64           { return math.Sqrt(a.Dot(a)) }
func (a Vec3) IsZero() bool              { return a.X == 0 && a.Y == 0 && a.Z == 0 }
func (a Vec3) XY() Vec2                  { return Vec2{a.X, a.Y} }
func (a Vec3) DistanceTo(b Vec3) float64 { return a.Sub(b).Length() }

// Normalized returns the unit vector; the zero vector stays zero.
func (a Vec3) Normalized() Vec3 {
	l := a.Length()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

// ArbitraryAxis returns the OCS axes (AutoCAD's arbitrary axis algorithm) for
// an extrusion direction: OCS x, y and the normalized normal.
func ArbitraryAxis(normal Vec3) (x, y, z Vec3) {
	z = normal.Normalized()
	if z.IsZero() {
		z = AxisZ
	}
	const limit = 1.0 / 64
	if math.Abs(z.X) < limit && math.Abs(z.Y) < limit {
		x = AxisY.Cross(z).Normalized()
	} else {
		x = AxisZ.Cross(z).Normalized()
	}
	y = z.Cross(x).Normalized()
	return x, y, z
}

// OcsToWcs converts a point in the OCS of normal to world coordinates.
func OcsToWcs(p, normal Vec3) Vec3 {
	if normal == AxisZ {
		return p
	}
	x, y, z := ArbitraryAxis(normal)
	return x.Scale(p.X).Add(y.Scale(p.Y)).Add(z.Scale(p.Z))
}

// WcsToOcs converts a world point to the OCS of normal.
func WcsToOcs(p, normal Vec3) Vec3 {
	if normal == AxisZ {
		return p
	}
	x, y, z := ArbitraryAxis(normal)
	return Vec3{p.Dot(x), p.Dot(y), p.Dot(z)}
}
