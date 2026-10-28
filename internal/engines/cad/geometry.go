package cad

import "math"

// Geometry for the CAD renderer: 3D affine transforms (block inserts, object
// coordinate systems), 2D figures whose arcs are cubic Béziers so they stay
// exact under any affine transform, and bounding boxes.

// Vec2 is a point or vector in the drawing plane (WCS X/Y after the top view).
type Vec2 struct{ X, Y float64 }

func (a Vec2) Add(b Vec2) Vec2    { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2    { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Neg() Vec2          { return Vec2{-a.X, -a.Y} }
func (a Vec2) Mul(k float64) Vec2 { return Vec2{a.X * k, a.Y * k} }
func (a Vec2) Length() float64    { return math.Sqrt(a.X*a.X + a.Y*a.Y) }
func (a Vec2) Perp() Vec2         { return Vec2{-a.Y, a.X} }
func (a Vec2) Finite() bool {
	return !math.IsNaN(a.X) && !math.IsNaN(a.Y) && !math.IsInf(a.X, 0) && !math.IsInf(a.Y, 0)
}
func Dot2(a, b Vec2) float64 { return a.X*b.X + a.Y*b.Y }

// Normalized returns the unit vector ((1, 0) for zero).
func (a Vec2) Normalized() Vec2 {
	l := a.Length()
	if l > 0 {
		return Vec2{a.X / l, a.Y / l}
	}
	return Vec2{1, 0}
}

// Vec3 is a 3D point or vector.
type Vec3 struct{ X, Y, Z float64 }

func (a Vec3) Add(b Vec3) Vec3    { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3    { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Mul(k float64) Vec3 { return Vec3{a.X * k, a.Y * k, a.Z * k} }
func (a Vec3) Length() float64    { return math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z) }

func Cross(a, b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// Normalized returns the unit vector ((0, 0, 1) for zero).
func (a Vec3) Normalized() Vec3 {
	l := a.Length()
	if l > 0 {
		return Vec3{a.X / l, a.Y / l, a.Z / l}
	}
	return Vec3{0, 0, 1}
}

// Affine3 maps an entity's own coordinates to WCS: the images of the X, Y and
// Z axes plus a translation.
type Affine3 struct{ X, Y, Z, T Vec3 }

var Identity3 = Affine3{X: Vec3{1, 0, 0}, Y: Vec3{0, 1, 0}, Z: Vec3{0, 0, 1}}

func Translation(t Vec3) Affine3 {
	a := Identity3
	a.T = t
	return a
}

func Scale3(sx, sy, sz float64) Affine3 {
	return Affine3{X: Vec3{sx, 0, 0}, Y: Vec3{0, sy, 0}, Z: Vec3{0, 0, sz}}
}

func RotationZ(angle float64) Affine3 {
	s, c := math.Sincos(angle)
	return Affine3{X: Vec3{c, s, 0}, Y: Vec3{-s, c, 0}, Z: Vec3{0, 0, 1}}
}

// Ocs is the object coordinate system of an extrusion direction (AutoCAD's
// arbitrary axis algorithm). Mirrored 2D entities often carry (0, 0, -1).
func Ocs(normal Vec3) Affine3 {
	n := normal.Normalized()
	if math.Abs(n.X) < 1e-12 && math.Abs(n.Y) < 1e-12 && n.Z > 0 {
		return Identity3
	}
	const limit = 1.0 / 64
	var ax Vec3
	if math.Abs(n.X) < limit && math.Abs(n.Y) < limit {
		ax = Cross(Vec3{0, 1, 0}, n)
	} else {
		ax = Cross(Vec3{0, 0, 1}, n)
	}
	ax = ax.Normalized()
	ay := Cross(n, ax).Normalized()
	return Affine3{X: ax, Y: ay, Z: n}
}

func (m Affine3) Apply(p Vec3) Vec3 {
	return Vec3{
		m.T.X + m.X.X*p.X + m.Y.X*p.Y + m.Z.X*p.Z,
		m.T.Y + m.X.Y*p.X + m.Y.Y*p.Y + m.Z.Y*p.Z,
		m.T.Z + m.X.Z*p.X + m.Y.Z*p.Y + m.Z.Z*p.Z,
	}
}

func (m Affine3) ApplyVector(v Vec3) Vec3 {
	return Vec3{
		m.X.X*v.X + m.Y.X*v.Y + m.Z.X*v.Z,
		m.X.Y*v.X + m.Y.Y*v.Y + m.Z.Y*v.Z,
		m.X.Z*v.X + m.Y.Z*v.Y + m.Z.Z*v.Z,
	}
}

// Point maps a point and projects it onto the drawing plane.
func (m Affine3) Point(p Vec3) Vec2 {
	w := m.Apply(p)
	return Vec2{w.X, w.Y}
}

// PointXY maps a point of the entity's XY plane.
func (m Affine3) PointXY(x, y float64) Vec2 { return m.Point(Vec3{x, y, 0}) }

// Vector maps a direction and projects it.
func (m Affine3) Vector(v Vec3) Vec2 {
	w := m.ApplyVector(v)
	return Vec2{w.X, w.Y}
}

func (m Affine3) VectorXY(x, y float64) Vec2 { return m.Vector(Vec3{x, y, 0}) }

// Multiply composes: inner first, then m.
func (m Affine3) Multiply(inner Affine3) Affine3 {
	return Affine3{m.ApplyVector(inner.X), m.ApplyVector(inner.Y), m.ApplyVector(inner.Z), m.Apply(inner.T)}
}

// PlanScale is the average scale of the projected XY plane, for widths and
// dash lengths given in the entity's units.
func (m Affine3) PlanScale() float64 {
	det := math.Abs(m.X.X*m.Y.Y - m.X.Y*m.Y.X)
	if det > 1e-300 {
		return math.Sqrt(det)
	}
	// A plane seen edge-on: the longest axis image, so widths do not vanish.
	return math.Max(Vec2{m.X.X, m.X.Y}.Length(), Vec2{m.Y.X, m.Y.Y}.Length())
}

// Segment is a line or a cubic Bézier ending in P.
type Segment struct {
	C1, C2, P Vec2
	Cubic     bool
}

// Figure is a connected run of segments in WCS.
type Figure struct {
	Start    Vec2
	Segments []Segment
	Closed   bool
}

func NewFigure(start Vec2) *Figure { return &Figure{Start: start} }

func (f *Figure) End() Vec2 {
	if len(f.Segments) == 0 {
		return f.Start
	}
	return f.Segments[len(f.Segments)-1].P
}

func (f *Figure) LineTo(p Vec2) { f.Segments = append(f.Segments, Segment{P: p}) }

// ArcTo appends the elliptical arc C + U·cos t + V·sin t from t0 to t1 as
// cubics of at most 90° each; exact for circles seen through any affine
// transform because U and V are the images of the original axes.
func (f *Figure) ArcTo(center, u, v Vec2, t0, t1 float64) {
	sweep := t1 - t0
	count := max(1, int(math.Ceil(math.Abs(sweep)/(math.Pi/2)-1e-9)))
	step := sweep / float64(count)
	k := 4.0 / 3.0 * math.Tan(step/4)
	for i := 0; i < count; i++ {
		a := t0 + step*float64(i)
		b := a + step
		sa, ca := math.Sincos(a)
		sb, cb := math.Sincos(b)
		p0 := center.Add(u.Mul(ca)).Add(v.Mul(sa))
		p3 := center.Add(u.Mul(cb)).Add(v.Mul(sb))
		d0 := u.Mul(-sa).Add(v.Mul(ca))
		d3 := u.Mul(-sb).Add(v.Mul(cb))
		f.Segments = append(f.Segments, Segment{C1: p0.Add(d0.Mul(k)), C2: p3.Sub(d3.Mul(k)), P: p3, Cubic: true})
	}
}

// AddTo includes every point of the figure (curve extremes, not control points) in box.
func (f *Figure) AddTo(box *Box2) {
	box.Add(f.Start)
	prev := f.Start
	for _, s := range f.Segments {
		if s.Cubic {
			addCubic(box, prev, s.C1, s.C2, s.P)
		} else {
			box.Add(s.P)
		}
		prev = s.P
	}
}

func addCubic(box *Box2, p0, p1, p2, p3 Vec2) {
	box.Add(p3)
	var roots []float64
	roots = derivativeRoots(p0.X, p1.X, p2.X, p3.X, roots)
	roots = derivativeRoots(p0.Y, p1.Y, p2.Y, p3.Y, roots)
	for _, t := range roots {
		mt := 1 - t
		a, b, c, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
		box.Add(Vec2{a*p0.X + b*p1.X + c*p2.X + d*p3.X, a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y})
	}
}

func derivativeRoots(p0, p1, p2, p3 float64, out []float64) []float64 {
	a := -p0 + 3*p1 - 3*p2 + p3
	b := 2 * (p0 - 2*p1 + p2)
	c := p1 - p0
	keep := func(t float64) {
		if t > 0 && t < 1 {
			out = append(out, t)
		}
	}
	if math.Abs(a) < 1e-12 {
		if math.Abs(b) > 1e-12 {
			keep(-c / b)
		}
		return out
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return out
	}
	sq := math.Sqrt(disc)
	keep((-b + sq) / (2 * a))
	keep((-b - sq) / (2 * a))
	return out
}

// Box2 is an axis-aligned box in WCS; empty until the first point is added.
type Box2 struct {
	MinX, MinY, MaxX, MaxY float64
	Empty                  bool
}

func EmptyBox() Box2 {
	return Box2{MinX: math.MaxFloat64, MinY: math.MaxFloat64, MaxX: -math.MaxFloat64, MaxY: -math.MaxFloat64, Empty: true}
}

func (b Box2) Width() float64 {
	if b.Empty {
		return 0
	}
	return b.MaxX - b.MinX
}

func (b Box2) Height() float64 {
	if b.Empty {
		return 0
	}
	return b.MaxY - b.MinY
}

func (b *Box2) Add(p Vec2) {
	// Garbage coordinates (NaN, 1e300 from corrupt files) must not blow up the page fit.
	if !p.Finite() || math.Abs(p.X) > 1e15 || math.Abs(p.Y) > 1e15 {
		return
	}
	b.MinX = math.Min(b.MinX, p.X)
	b.MinY = math.Min(b.MinY, p.Y)
	b.MaxX = math.Max(b.MaxX, p.X)
	b.MaxY = math.Max(b.MaxY, p.Y)
	b.Empty = false
}
