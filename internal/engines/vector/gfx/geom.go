// Package gfx is the 2D drawing layer shared by the vector and CAD engines:
// paths with stroking and dashing, an anti-aliased rasterizer with clipping,
// groups and masks, a PDF writer and an SVG writer behind one Device
// interface, and text as glyph outlines.
package gfx

import "math"

// Point is a point or vector.
type Point struct{ X, Y float64 }

func (p Point) Add(q Point) Point       { return Point{p.X + q.X, p.Y + q.Y} }
func (p Point) Sub(q Point) Point       { return Point{p.X - q.X, p.Y - q.Y} }
func (p Point) Mul(k float64) Point     { return Point{p.X * k, p.Y * k} }
func (p Point) Dot(q Point) float64     { return p.X*q.X + p.Y*q.Y }
func (p Point) Cross(q Point) float64   { return p.X*q.Y - p.Y*q.X }
func (p Point) Len() float64            { return math.Hypot(p.X, p.Y) }
func (p Point) Perp() Point             { return Point{-p.Y, p.X} }
func (p Point) Lerp(q Point, t float64) Point {
	return Point{p.X + (q.X-p.X)*t, p.Y + (q.Y-p.Y)*t}
}

// Norm returns the unit vector (zero stays zero).
func (p Point) Norm() Point {
	l := p.Len()
	if l < 1e-300 {
		return Point{}
	}
	return Point{p.X / l, p.Y / l}
}

// Matrix is an affine transform: x' = A*x + C*y + E, y' = B*x + D*y + F
// (the SVG / PDF a b c d e f order).
type Matrix struct{ A, B, C, D, E, F float64 }

// Identity is the identity transform.
var Identity = Matrix{A: 1, D: 1}

func Translate(x, y float64) Matrix { return Matrix{A: 1, D: 1, E: x, F: y} }
func Scale(x, y float64) Matrix     { return Matrix{A: x, D: y} }

// Rotate rotates by angle radians (counter-clockwise in y-up, clockwise on screen).
func Rotate(angle float64) Matrix {
	s, c := math.Sincos(angle)
	return Matrix{A: c, B: s, C: -s, D: c}
}

func SkewX(angle float64) Matrix { return Matrix{A: 1, C: math.Tan(angle), D: 1} }
func SkewY(angle float64) Matrix { return Matrix{A: 1, B: math.Tan(angle), D: 1} }

// Mul returns m·n: n is applied first, then m.
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.C*n.B,
		B: m.B*n.A + m.D*n.B,
		C: m.A*n.C + m.C*n.D,
		D: m.B*n.C + m.D*n.D,
		E: m.A*n.E + m.C*n.F + m.E,
		F: m.B*n.E + m.D*n.F + m.F,
	}
}

// Apply maps a point.
func (m Matrix) Apply(p Point) Point {
	return Point{m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F}
}

// ApplyVector maps a vector (no translation).
func (m Matrix) ApplyVector(p Point) Point {
	return Point{m.A*p.X + m.C*p.Y, m.B*p.X + m.D*p.Y}
}

func (m Matrix) Det() float64 { return m.A*m.D - m.B*m.C }

// Invert returns the inverse; ok is false for a singular matrix.
func (m Matrix) Invert() (Matrix, bool) {
	det := m.Det()
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Matrix{}, false
	}
	inv := 1 / det
	return Matrix{
		A: m.D * inv, B: -m.B * inv, C: -m.C * inv, D: m.A * inv,
		E: (m.C*m.F - m.D*m.E) * inv,
		F: (m.B*m.E - m.A*m.F) * inv,
	}, true
}

// MeanScale is the square root of the area scale: how much a line width grows.
func (m Matrix) MeanScale() float64 { return math.Sqrt(math.Abs(m.Det())) }

// IsIdentity reports an exact identity.
func (m Matrix) IsIdentity() bool { return m == Identity }

// Finite reports whether every entry is a finite number.
func (m Matrix) Finite() bool {
	for _, v := range [...]float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Rect is an axis-aligned rectangle; Empty until a point is added.
type Rect struct {
	X0, Y0, X1, Y1 float64
}

// EmptyRect has no area and absorbs the first point added.
func EmptyRect() Rect { return Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)} }

func (r Rect) Empty() bool      { return !(r.X1 >= r.X0 && r.Y1 >= r.Y0) }
func (r Rect) Width() float64   { return r.X1 - r.X0 }
func (r Rect) Height() float64  { return r.Y1 - r.Y0 }
func (r *Rect) Add(p Point) {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return
	}
	r.X0 = math.Min(r.X0, p.X)
	r.Y0 = math.Min(r.Y0, p.Y)
	r.X1 = math.Max(r.X1, p.X)
	r.Y1 = math.Max(r.Y1, p.Y)
}

// Union returns the smallest rectangle holding both.
func (r Rect) Union(o Rect) Rect {
	if o.Empty() {
		return r
	}
	if r.Empty() {
		return o
	}
	return Rect{math.Min(r.X0, o.X0), math.Min(r.Y0, o.Y0), math.Max(r.X1, o.X1), math.Max(r.Y1, o.Y1)}
}

// Intersect returns the overlap (possibly empty).
func (r Rect) Intersect(o Rect) Rect {
	return Rect{math.Max(r.X0, o.X0), math.Max(r.Y0, o.Y0), math.Min(r.X1, o.X1), math.Min(r.Y1, o.Y1)}
}

// Transform returns the bounds of the transformed rectangle.
func (r Rect) Transform(m Matrix) Rect {
	if r.Empty() {
		return r
	}
	out := EmptyRect()
	for _, p := range [...]Point{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X0, r.Y1}, {r.X1, r.Y1}} {
		out.Add(m.Apply(p))
	}
	return out
}

// Outset grows the rectangle by d on every side.
func (r Rect) Outset(d float64) Rect { return Rect{r.X0 - d, r.Y0 - d, r.X1 + d, r.Y1 + d} }
