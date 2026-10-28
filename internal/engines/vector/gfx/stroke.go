package gfx

import "math"

type LineCap uint8

const (
	ButtCap LineCap = iota
	RoundCap
	SquareCap
)

type LineJoin uint8

const (
	MiterJoin LineJoin = iota
	RoundJoin
	BevelJoin
	// MiterClipJoin and ArcsJoin of SVG 2 are drawn as miters.
)

// StrokeStyle describes how a path is stroked, in the path's own units.
type StrokeStyle struct {
	Width      float64
	Cap        LineCap
	Join       LineJoin
	MiterLimit float64
	Dashes     []float64
	DashOffset float64
}

// DashPolylines cuts polylines into the dashes of pattern (lengths of
// alternating dashes and gaps), starting offset into the pattern. A zero dash
// gives a zero-length piece (a dot with round or square caps).
func DashPolylines(pls []Polyline, pattern []float64, offset float64) []Polyline {
	period := 0.0
	for _, d := range pattern {
		period += math.Abs(d)
	}
	if len(pattern) == 0 || !(period > 0) || math.IsInf(period, 0) {
		return pls
	}
	// An odd count repeats (SVG and PDF both say so).
	if len(pattern)%2 == 1 {
		pattern = append(append([]float64(nil), pattern...), pattern...)
		period *= 2
	}
	var out []Polyline
	for _, pl := range pls {
		pts := pl.Pts
		if pl.Closed && len(pts) > 1 {
			pts = append(append([]Point(nil), pts...), pts[0])
		}
		if len(pts) < 2 {
			continue
		}
		// Find the dash at the offset.
		off := math.Mod(offset, period)
		if off < 0 {
			off += period
		}
		idx := 0
		for off > 0 {
			l := math.Abs(pattern[idx])
			if off < l {
				break
			}
			off -= l
			idx = (idx + 1) % len(pattern)
		}
		remain := math.Abs(pattern[idx]) - off
		on := idx%2 == 0
		var cur []Point
		if on {
			cur = []Point{pts[0]}
		}
		// Bound the work for absurd patterns.
		budget := 1_000_000
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			seg := b.Sub(a).Len()
			pos := 0.0
			for seg-pos > remain {
				pos += remain
				q := a.Lerp(b, pos/seg)
				if on {
					cur = append(cur, q)
					out = append(out, Polyline{Pts: cur})
					cur = nil
				} else {
					cur = []Point{q}
				}
				on = !on
				idx = (idx + 1) % len(pattern)
				remain = math.Abs(pattern[idx])
				if budget--; budget <= 0 {
					return out
				}
			}
			remain -= seg - pos
			if on {
				cur = append(cur, b)
			}
		}
		if on && len(cur) > 1 {
			out = append(out, Polyline{Pts: cur})
		}
	}
	return out
}

// StrokePolylines returns closed polygons, all wound the same way, whose
// non-zero union is the stroke outline. tol is the flatness of round parts.
func StrokePolylines(pls []Polyline, s *StrokeStyle, tol float64) []Polyline {
	hw := s.Width / 2
	if !(hw > 0) {
		return nil
	}
	if tol <= 0 {
		tol = 0.1
	}
	limit := s.MiterLimit
	if limit < 1 {
		limit = 4
	}
	var out []Polyline
	add := func(poly []Point) {
		if len(poly) < 3 {
			return
		}
		if signedArea(poly) < 0 {
			for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
				poly[i], poly[j] = poly[j], poly[i]
			}
		}
		out = append(out, Polyline{Pts: poly, Closed: true})
	}
	circle := func(c Point) {
		n := circleSegments(hw, tol)
		poly := make([]Point, n)
		for i := range poly {
			a := 2 * math.Pi * float64(i) / float64(n)
			poly[i] = Point{c.X + hw*math.Cos(a), c.Y + hw*math.Sin(a)}
		}
		add(poly)
	}
	for _, pl := range pls {
		pts := dedupe(pl.Pts)
		closed := pl.Closed && len(pts) > 2
		if closed && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		if len(pts) == 1 || (len(pts) == 2 && !closed && pts[0] == pts[1]) {
			// Zero length: only caps show (round: a dot, square: a square).
			switch s.Cap {
			case RoundCap:
				circle(pts[0])
			case SquareCap:
				c := pts[0]
				add([]Point{{c.X - hw, c.Y - hw}, {c.X + hw, c.Y - hw}, {c.X + hw, c.Y + hw}, {c.X - hw, c.Y + hw}})
			}
			continue
		}
		n := len(pts)
		segs := n - 1
		if closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			a, b := pts[i], pts[(i+1)%n]
			d := b.Sub(a).Norm()
			nrm := d.Perp().Mul(hw)
			a0, b0 := a, b
			if !closed {
				if i == 0 && s.Cap == SquareCap {
					a0 = a.Sub(d.Mul(hw))
				}
				if i == segs-1 && s.Cap == SquareCap {
					b0 = b.Add(d.Mul(hw))
				}
			}
			add([]Point{a0.Add(nrm), b0.Add(nrm), b0.Sub(nrm), a0.Sub(nrm)})
		}
		// Joins at inner vertices (all vertices of a closed line).
		first, last := 1, n-1
		if closed {
			first, last = 0, n
		}
		for i := first; i < last; i++ {
			prev, at, next := pts[(i-1+n)%n], pts[i%n], pts[(i+1)%n]
			join(prev, at, next, hw, s.Join, limit, tol, add, circle)
		}
		if !closed && s.Cap == RoundCap {
			circle(pts[0])
			circle(pts[n-1])
		}
	}
	return out
}

func join(prev, at, next Point, hw float64, kind LineJoin, limit, tol float64, add func([]Point), circle func(Point)) {
	d0 := at.Sub(prev).Norm()
	d1 := next.Sub(at).Norm()
	cross := d0.Cross(d1)
	dot := d0.Dot(d1)
	if math.Abs(cross) < 1e-12 && dot > 0 {
		return // straight on
	}
	if kind == RoundJoin {
		circle(at)
		return
	}
	// The outer side is where the line turns away from.
	side := 1.0
	if cross > 0 {
		side = -1
	}
	n0 := d0.Perp().Mul(hw * side)
	n1 := d1.Perp().Mul(hw * side)
	p0 := at.Add(n0)
	p1 := at.Add(n1)
	if kind == MiterJoin {
		// Miter length ratio is 1/sin(theta/2), theta the angle between segments.
		cosTheta := -dot
		sinHalf := math.Sqrt(math.Max(0, (1-cosTheta)/2))
		if sinHalf > 1e-9 && 1/sinHalf <= limit {
			bis := n0.Add(n1).Norm()
			tip := at.Add(bis.Mul(hw / sinHalf))
			add([]Point{at, p0, tip, p1})
			return
		}
	}
	add([]Point{at, p0, p1})
}

func circleSegments(r, tol float64) int {
	if r <= tol {
		return 8
	}
	n := int(math.Ceil(math.Pi / math.Acos(1-tol/r)))
	if n < 8 {
		n = 8
	}
	if n > 256 {
		n = 256
	}
	return n
}

func dedupe(pts []Point) []Point {
	out := make([]Point, 0, len(pts))
	for _, p := range pts {
		if len(out) == 0 || out[len(out)-1].Sub(p).Len() > 1e-9 {
			out = append(out, p)
		}
	}
	if len(out) == 0 && len(pts) > 0 {
		out = append(out, pts[0])
	}
	return out
}

func signedArea(pts []Point) float64 {
	a := 0.0
	for i := range pts {
		j := (i + 1) % len(pts)
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return a / 2
}

// StrokePath strokes p in device space: the path is mapped by m, the stroke is
// widened in the path's own units (so a non-uniform m distorts it like SVG
// and PDF do) and the outline is returned in device coordinates.
func StrokePath(p *Path, m Matrix, s *StrokeStyle, tol float64) []Polyline {
	inv, ok := m.Invert()
	if !ok {
		return nil
	}
	// Flatten in device space for the right precision, stroke in user space.
	scale := m.MeanScale()
	if scale <= 0 {
		return nil
	}
	userTol := tol / scale
	pls := p.Flatten(userTol)
	_ = inv
	if len(s.Dashes) > 0 {
		pls = DashPolylines(pls, s.Dashes, s.DashOffset)
	}
	polys := StrokePolylines(pls, s, userTol)
	for i := range polys {
		for j, q := range polys[i].Pts {
			polys[i].Pts[j] = m.Apply(q)
		}
	}
	return polys
}
