package gfx

import "math"

// Op is a path command.
type Op uint8

const (
	MoveTo Op = iota
	LineTo
	QuadTo
	CubeTo
	Close
)

// Path is a sequence of subpaths made of lines, quadratic and cubic Béziers.
// Arcs are converted to cubics when added.
type Path struct {
	Ops []Op
	// Pts holds the points of the commands: 1 for MoveTo/LineTo, 2 for QuadTo,
	// 3 for CubeTo, 0 for Close.
	Pts []Point

	start, cur Point
	open       bool

	// Shape is set when the path is exactly this ellipse (in path
	// coordinates), so CAD output can keep it a circle or ellipse.
	Shape *EllipseHint
}

// EllipseHint describes an axis-aligned ellipse.
type EllipseHint struct{ CX, CY, RX, RY float64 }

// NewPath returns an empty path.
func NewPath() *Path { return &Path{} }

// Empty reports a path without drawing commands.
func (p *Path) Empty() bool {
	for _, op := range p.Ops {
		if op != MoveTo {
			return false
		}
	}
	return true
}

// Current is the current point.
func (p *Path) Current() Point { return p.cur }

func (p *Path) MoveTo(x, y float64) {
	pt := Point{x, y}
	// Two moves in a row: the second wins.
	if n := len(p.Ops); n > 0 && p.Ops[n-1] == MoveTo {
		p.Pts[len(p.Pts)-1] = pt
	} else {
		p.Ops = append(p.Ops, MoveTo)
		p.Pts = append(p.Pts, pt)
	}
	p.start, p.cur, p.open = pt, pt, true
}

func (p *Path) ensureOpen() {
	if !p.open {
		p.MoveTo(p.cur.X, p.cur.Y)
	}
}

func (p *Path) LineTo(x, y float64) {
	p.ensureOpen()
	pt := Point{x, y}
	p.Ops = append(p.Ops, LineTo)
	p.Pts = append(p.Pts, pt)
	p.cur = pt
}

func (p *Path) QuadTo(cx, cy, x, y float64) {
	p.ensureOpen()
	p.Ops = append(p.Ops, QuadTo)
	p.Pts = append(p.Pts, Point{cx, cy}, Point{x, y})
	p.cur = Point{x, y}
}

func (p *Path) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	p.ensureOpen()
	p.Ops = append(p.Ops, CubeTo)
	p.Pts = append(p.Pts, Point{c1x, c1y}, Point{c2x, c2y}, Point{x, y})
	p.cur = Point{x, y}
}

func (p *Path) Close() {
	if !p.open {
		return
	}
	p.Ops = append(p.Ops, Close)
	p.cur = p.start
	p.open = false
}

// Append adds every command of q.
func (p *Path) Append(q *Path) {
	if q == nil {
		return
	}
	j := 0
	for _, op := range q.Ops {
		switch op {
		case MoveTo:
			p.MoveTo(q.Pts[j].X, q.Pts[j].Y)
			j++
		case LineTo:
			p.LineTo(q.Pts[j].X, q.Pts[j].Y)
			j++
		case QuadTo:
			p.QuadTo(q.Pts[j].X, q.Pts[j].Y, q.Pts[j+1].X, q.Pts[j+1].Y)
			j += 2
		case CubeTo:
			p.CubeTo(q.Pts[j].X, q.Pts[j].Y, q.Pts[j+1].X, q.Pts[j+1].Y, q.Pts[j+2].X, q.Pts[j+2].Y)
			j += 3
		case Close:
			p.Close()
		}
	}
}

// Transformed returns a copy mapped by m.
func (p *Path) Transformed(m Matrix) *Path {
	q := &Path{Ops: append([]Op(nil), p.Ops...), Pts: make([]Point, len(p.Pts))}
	for i, pt := range p.Pts {
		q.Pts[i] = m.Apply(pt)
	}
	q.start, q.cur, q.open = m.Apply(p.start), m.Apply(p.cur), p.open
	return q
}

// Clone returns a copy.
func (p *Path) Clone() *Path { return p.Transformed(Identity) }

// Rect adds a closed rectangle.
func (p *Path) Rect(x, y, w, h float64) {
	p.MoveTo(x, y)
	p.LineTo(x+w, y)
	p.LineTo(x+w, y+h)
	p.LineTo(x, y+h)
	p.Close()
}

// kappa is the cubic control distance of a quarter circle.
const kappa = 0.5522847498307936

// Ellipse adds a closed axis-aligned ellipse (clockwise on screen, like SVG).
func (p *Path) Ellipse(cx, cy, rx, ry float64) {
	kx, ky := rx*kappa, ry*kappa
	p.MoveTo(cx+rx, cy)
	p.CubeTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.CubeTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.CubeTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.CubeTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.Close()
}

// RoundRect adds a rectangle with elliptical corners (SVG rect rx/ry).
func (p *Path) RoundRect(x, y, w, h, rx, ry float64) {
	if rx <= 0 || ry <= 0 {
		p.Rect(x, y, w, h)
		return
	}
	rx = math.Min(rx, w/2)
	ry = math.Min(ry, h/2)
	kx, ky := rx*(1-kappa), ry*(1-kappa)
	p.MoveTo(x+rx, y)
	p.LineTo(x+w-rx, y)
	p.CubeTo(x+w-kx, y, x+w, y+ky, x+w, y+ry)
	p.LineTo(x+w, y+h-ry)
	p.CubeTo(x+w, y+h-ky, x+w-kx, y+h, x+w-rx, y+h)
	p.LineTo(x+rx, y+h)
	p.CubeTo(x+kx, y+h, x, y+h-ky, x, y+h-ry)
	p.LineTo(x, y+ry)
	p.CubeTo(x, y+ky, x+kx, y, x+rx, y)
	p.Close()
}

// ArcTo adds an SVG elliptical arc from the current point to (x, y)
// (radii rx, ry, x-axis rotation phi in radians, flags as in SVG).
func (p *Path) ArcTo(rx, ry, phi float64, large, sweep bool, x, y float64) {
	p.ensureOpen()
	x0, y0 := p.cur.X, p.cur.Y
	if x0 == x && y0 == y {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.LineTo(x, y)
		return
	}
	sinPhi, cosPhi := math.Sincos(phi)
	dx2, dy2 := (x0-x)/2, (y0-y)/2
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2
	// Radii too small for the chord are scaled up (SVG implementation notes F.6.6).
	lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	coef := 0.0
	if den > 0 && num > 0 {
		coef = math.Sqrt(num / den)
	}
	if large == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1p / ry
	cyp := -coef * ry * x1p / rx
	cx := cosPhi*cxp - sinPhi*cyp + (x0+x)/2
	cy := sinPhi*cxp + cosPhi*cyp + (y0+y)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	theta1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dtheta := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dtheta > 0 {
		dtheta -= 2 * math.Pi
	} else if sweep && dtheta < 0 {
		dtheta += 2 * math.Pi
	}
	p.arcSegments(cx, cy, rx, ry, phi, theta1, dtheta)
	// Land exactly on the end point.
	p.Pts[len(p.Pts)-1] = Point{x, y}
	p.cur = Point{x, y}
}

// Arc adds an elliptical arc around (cx, cy) from angle a0 sweeping da
// radians, drawing a line to its start first when the path is open.
func (p *Path) Arc(cx, cy, rx, ry, phi, a0, da float64) {
	sinPhi, cosPhi := math.Sincos(phi)
	s, c := math.Sincos(a0)
	start := Point{cx + rx*c*cosPhi - ry*s*sinPhi, cy + rx*c*sinPhi + ry*s*cosPhi}
	if p.open {
		p.LineTo(start.X, start.Y)
	} else {
		p.MoveTo(start.X, start.Y)
	}
	p.arcSegments(cx, cy, rx, ry, phi, a0, da)
}

func (p *Path) arcSegments(cx, cy, rx, ry, phi, theta, dtheta float64) {
	n := int(math.Ceil(math.Abs(dtheta)/(math.Pi/2) - 1e-9))
	if n < 1 {
		n = 1
	}
	step := dtheta / float64(n)
	k := 4.0 / 3.0 * math.Tan(step/4)
	sinPhi, cosPhi := math.Sincos(phi)
	pt := func(a float64) (Point, Point) {
		s, c := math.Sincos(a)
		px := Point{cx + rx*c*cosPhi - ry*s*sinPhi, cy + rx*c*sinPhi + ry*s*cosPhi}
		d := Point{-rx*s*cosPhi - ry*c*sinPhi, -rx*s*sinPhi + ry*c*cosPhi}
		return px, d
	}
	for i := 0; i < n; i++ {
		a := theta + step*float64(i)
		p0, d0 := pt(a)
		p3, d3 := pt(a + step)
		c1 := p0.Add(d0.Mul(k))
		c2 := p3.Sub(d3.Mul(k))
		p.CubeTo(c1.X, c1.Y, c2.X, c2.Y, p3.X, p3.Y)
	}
}

// Bounds returns the exact bounding box (curve extremes included).
func (p *Path) Bounds() Rect {
	r := EmptyRect()
	var cur Point
	j := 0
	for _, op := range p.Ops {
		switch op {
		case MoveTo, LineTo:
			cur = p.Pts[j]
			r.Add(cur)
			j++
		case QuadTo:
			c, e := p.Pts[j], p.Pts[j+1]
			// Elevate to cubic for the extremes.
			c1 := cur.Add(c.Sub(cur).Mul(2.0 / 3))
			c2 := e.Add(c.Sub(e).Mul(2.0 / 3))
			cubicBounds(&r, cur, c1, c2, e)
			cur = e
			j += 2
		case CubeTo:
			cubicBounds(&r, cur, p.Pts[j], p.Pts[j+1], p.Pts[j+2])
			cur = p.Pts[j+2]
			j += 3
		}
	}
	return r
}

func cubicBounds(r *Rect, p0, p1, p2, p3 Point) {
	r.Add(p0)
	r.Add(p3)
	var roots [4]float64
	n := derivRoots(p0.X, p1.X, p2.X, p3.X, roots[:0])
	n = append(n, derivRoots(p0.Y, p1.Y, p2.Y, p3.Y, nil)...)
	for _, t := range n {
		r.Add(cubicAt(p0, p1, p2, p3, t))
	}
}

func derivRoots(p0, p1, p2, p3 float64, out []float64) []float64 {
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
	d := b*b - 4*a*c
	if d < 0 {
		return out
	}
	s := math.Sqrt(d)
	keep((-b + s) / (2 * a))
	keep((-b - s) / (2 * a))
	return out
}

func cubicAt(p0, p1, p2, p3 Point, t float64) Point {
	mt := 1 - t
	a, b, c, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
	return Point{a*p0.X + b*p1.X + c*p2.X + d*p3.X, a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y}
}

// Polyline is one flattened subpath.
type Polyline struct {
	Pts    []Point
	Closed bool
}

// Flatten converts the path into polylines whose chords stay within tol of
// the curves.
func (p *Path) Flatten(tol float64) []Polyline {
	if tol <= 0 {
		tol = 0.1
	}
	var out []Polyline
	var cur Polyline
	var pos, start Point
	flush := func() {
		if len(cur.Pts) > 0 {
			out = append(out, cur)
		}
		cur = Polyline{}
	}
	j := 0
	for _, op := range p.Ops {
		switch op {
		case MoveTo:
			flush()
			pos = p.Pts[j]
			start = pos
			cur.Pts = append(cur.Pts, pos)
			j++
		case LineTo:
			pos = p.Pts[j]
			cur.Pts = append(cur.Pts, pos)
			j++
		case QuadTo:
			c, e := p.Pts[j], p.Pts[j+1]
			dd := pos.Sub(c.Mul(2)).Add(e).Len()
			n := segmentsFor(dd/4, tol)
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				mt := 1 - t
				cur.Pts = append(cur.Pts, Point{mt*mt*pos.X + 2*mt*t*c.X + t*t*e.X, mt*mt*pos.Y + 2*mt*t*c.Y + t*t*e.Y})
			}
			pos = e
			j += 2
		case CubeTo:
			c1, c2, e := p.Pts[j], p.Pts[j+1], p.Pts[j+2]
			dd := math.Max(pos.Sub(c1.Mul(2)).Add(c2).Len(), c1.Sub(c2.Mul(2)).Add(e).Len())
			n := segmentsFor(dd*0.75, tol)
			for i := 1; i <= n; i++ {
				cur.Pts = append(cur.Pts, cubicAt(pos, c1, c2, e, float64(i)/float64(n)))
			}
			pos = e
			j += 3
		case Close:
			// Paths built with the methods always move after a close.
			cur.Closed = true
			pos = start
			flush()
		}
	}
	flush()
	return out
}

func segmentsFor(deviation, tol float64) int {
	n := int(math.Ceil(math.Sqrt(deviation / tol)))
	if n < 1 {
		return 1
	}
	if n > 1000 {
		return 1000
	}
	return n
}

// Length of a polyline.
func (pl Polyline) Length() float64 {
	l := 0.0
	for i := 1; i < len(pl.Pts); i++ {
		l += pl.Pts[i].Sub(pl.Pts[i-1]).Len()
	}
	if pl.Closed && len(pl.Pts) > 1 {
		l += pl.Pts[0].Sub(pl.Pts[len(pl.Pts)-1]).Len()
	}
	return l
}

// PathFromPolylines builds a path of straight segments.
func PathFromPolylines(pls []Polyline) *Path {
	p := &Path{}
	for _, pl := range pls {
		if len(pl.Pts) == 0 {
			continue
		}
		p.MoveTo(pl.Pts[0].X, pl.Pts[0].Y)
		for _, q := range pl.Pts[1:] {
			p.LineTo(q.X, q.Y)
		}
		if pl.Closed {
			p.Close()
		}
	}
	return p
}

// Walk calls fn for every command with its points (in absolute coordinates).
func (p *Path) Walk(fn func(op Op, pts []Point)) {
	j := 0
	for _, op := range p.Ops {
		n := 0
		switch op {
		case MoveTo, LineTo:
			n = 1
		case QuadTo:
			n = 2
		case CubeTo:
			n = 3
		}
		fn(op, p.Pts[j:j+n])
		j += n
	}
}
