package fonts

import "math"

// Approximates cubic Bézier curves (CFF) with quadratic splines (TrueType)
// within a distance tolerance. Follows the approach of cu2qu (Google,
// Apache-2.0; part of fontTools): split the cubic into n equal pieces, give
// each piece one quadratic control point, let TrueType's implied on-curve
// midpoints join them, and take the smallest n that fits.

const (
	maxPieces     = 100
	maxCheckDepth = 24
)

// cubicToQuadratic returns the off-curve control points of a quadratic spline
// from p0 to p3 that stays within tolerance of the cubic. Consecutive off-curve
// points have an implied on-curve point halfway between them. An empty result
// means a straight line.
func cubicToQuadratic(p0, p1, p2, p3 vec2, tolerance float64) []vec2 {
	if isLine(p0, p1, p2, p3, tolerance) {
		return nil
	}
	for n := 1; n <= maxPieces; n++ {
		var s []vec2
		var ok bool
		if n == 1 {
			s, ok = singleQuadratic(p0, p1, p2, p3, tolerance)
		} else {
			s, ok = quadraticSpline(p0, p1, p2, p3, n, tolerance)
		}
		if ok {
			return s
		}
	}
	// Not reachable for real outlines (100 pieces fit anything); keep the finest split rather than failing.
	s, _ := quadraticSpline(p0, p1, p2, p3, maxPieces, math.Inf(1))
	return s
}

// isLine is true when the control points lie on the chord within a small
// fraction of the tolerance and between its ends: the curve is the chord.
func isLine(p0, p1, p2, p3 vec2, tolerance float64) bool {
	chord := p3.sub(p0)
	length := chord.length()
	if length < 1e-9 {
		return p1.sub(p0).length() < 1e-9 && p2.sub(p0).length() < 1e-9
	}
	for _, p := range []vec2{p1, p2} {
		v := p.sub(p0)
		along := dot(v, chord) / (length * length)
		distance := math.Abs(v.x*chord.y-v.y*chord.x) / length
		if along < 0 || along > 1 || distance > tolerance*0.01 {
			return false
		}
	}
	return true
}

// singleQuadratic is one quadratic whose control point is where the cubic's end tangents meet.
func singleQuadratic(p0, p1, p2, p3 vec2, tolerance float64) ([]vec2, bool) {
	q, ok := intersect(p0, p1, p2, p3)
	if !ok {
		return nil, false
	}
	// Degree-elevated quadratic vs. the cubic: compare the differences of their control points.
	c1 := p0.add(q.sub(p0).mul(2.0 / 3))
	c2 := p3.add(q.sub(p3).mul(2.0 / 3))
	if fitsInside(vec2{}, c1.sub(p1), c2.sub(p2), vec2{}, tolerance, 0) {
		return []vec2{q}, true
	}
	return nil, false
}

// quadraticSpline is n quadratics chained through implied on-curve points;
// false when the error exceeds the tolerance.
func quadraticSpline(p0, p1, p2, p3 vec2, n int, tolerance float64) ([]vec2, bool) {
	pieces := splitCubic(p0, p1, p2, p3, n)
	controls := make([]vec2, n)
	for i := range controls {
		controls[i] = pieceControl(float64(i)/float64(n-1), pieces[i])
	}
	var d1 vec2
	q2 := p0
	for i := 0; i < n; i++ {
		c1, c2, c3 := pieces[i][1], pieces[i][2], pieces[i][3]
		q0 := q2
		q1 := controls[i]
		if i+1 < n {
			q2 = q1.add(controls[i+1]).mul(0.5)
		} else {
			q2 = c3
		}
		d0 := d1
		d1 = q2.sub(c3)
		if d1.length() > tolerance ||
			!fitsInside(d0, q0.add(q1.sub(q0).mul(2.0/3)).sub(c1), q2.add(q1.sub(q2).mul(2.0/3)).sub(c2), d1, tolerance, 0) {
			return nil, false
		}
	}
	return controls, true
}

// pieceControl is the control point for piece t (0 = first, 1 = last):
// interpolates between the points where the piece's start and end tangents,
// extended by 1.5, land.
func pieceControl(t float64, p [4]vec2) vec2 {
	start := p[0].add(p[1].sub(p[0]).mul(1.5))
	end := p[3].add(p[2].sub(p[3]).mul(1.5))
	return start.add(end.sub(start).mul(t))
}

// splitCubic splits the cubic into n pieces of equal parameter length.
func splitCubic(p0, p1, p2, p3 vec2, n int) [][4]vec2 {
	// Power basis: B(t) = a t³ + b t² + c t + d.
	c := p1.sub(p0).mul(3)
	b := p2.sub(p1).mul(3).sub(c)
	d := p0
	a := p3.sub(d).sub(c).sub(b)
	pieces := make([][4]vec2, n)
	dt := 1.0 / float64(n)
	for i := range pieces {
		// Substitute t = t0 + dt·u and convert the piece back to Bézier control points.
		t0 := float64(i) * dt
		a1 := a.mul(dt * dt * dt)
		b1 := a.mul(3 * t0).add(b).mul(dt * dt)
		c1 := b.mul(2 * t0).add(c).add(a.mul(3 * t0 * t0)).mul(dt)
		d1 := a.mul(t0 * t0 * t0).add(b.mul(t0 * t0)).add(c.mul(t0)).add(d)
		q1 := c1.mul(1.0 / 3).add(d1)
		q2 := b1.add(c1).mul(1.0 / 3).add(q1)
		q3 := a1.add(d1).add(c1).add(b1)
		pieces[i] = [4]vec2{d1, q1, q2, q3}
	}
	return pieces
}

// intersect is where line a→b meets line c→d; false when they are parallel.
func intersect(a, b, c, d vec2) (vec2, bool) {
	ab := b.sub(a)
	cd := d.sub(c)
	normal := vec2{-ab.y, ab.x}
	den := dot(normal, cd)
	if math.Abs(den) < 1e-12 {
		return vec2{}, false
	}
	h := dot(normal, a.sub(c)) / den
	return c.add(cd.mul(h)), true
}

// fitsInside is true when the cubic with these control points (a difference
// curve) stays within the tolerance of the origin everywhere, checked by
// subdividing until its control points are all close enough.
func fitsInside(p0, p1, p2, p3 vec2, tolerance float64, depth int) bool {
	if p2.length() <= tolerance && p1.length() <= tolerance {
		return true
	}
	mid := p0.add(p1.add(p2).mul(3)).add(p3).mul(0.125)
	if mid.length() > tolerance {
		return false
	}
	if depth >= maxCheckDepth {
		return true
	}
	deriv := p3.add(p2).sub(p1).sub(p0).mul(0.125)
	return fitsInside(p0, p0.add(p1).mul(0.5), mid.sub(deriv), mid, tolerance, depth+1) &&
		fitsInside(mid, mid.add(deriv), p2.add(p3).mul(0.5), p3, tolerance, depth+1)
}
