package cad

import "math"

// Curve math for the CAD renderer: NURBS evaluation (SPLINE entities, spline
// hatch edges, spline-fit polylines), smooth curves through fit points, and
// polyline bulge arcs.

// maxSamples bounds the points generated for one curve, so a broken knot
// vector cannot explode.
const maxSamples = 4000

// SampleNurbs samples a (rational) B-spline with de Boor's algorithm. Missing
// or inconsistent knots are replaced by a clamped uniform knot vector; missing
// weights mean 1. Empty for unusable input.
func SampleNurbs(degree int, ctrl []Vec3, knots, weights []float64) []Vec3 {
	n := len(ctrl)
	if n < 2 {
		return nil
	}
	p := max(1, min(degree, n-1))
	var t []float64
	if len(knots) == n+p+1 && nonDecreasing(knots) {
		t = knots
	} else {
		t = clampedUniform(n, p)
	}
	var w []float64
	if len(weights) == n {
		w = weights
		for _, v := range weights {
			if !(v > 0) || math.IsInf(v, 0) {
				w = nil
				break
			}
		}
	}
	var spans []int
	for k := p; k < n; k++ {
		if t[k+1] > t[k] {
			spans = append(spans, k)
		}
	}
	if len(spans) == 0 {
		return nil
	}
	// Straight segments need only their end points; curved spans get up to 32 points.
	perSpan := 1
	if p != 1 {
		perSpan = max(1, min(32, maxSamples/len(spans)))
	}
	h := make([][4]float64, p+1)
	eval := func(u float64, k int) Vec3 {
		for j := 0; j <= p; j++ {
			pt := ctrl[k-p+j]
			wt := 1.0
			if w != nil {
				wt = w[k-p+j]
			}
			h[j] = [4]float64{pt.X * wt, pt.Y * wt, pt.Z * wt, wt}
		}
		for r := 1; r <= p; r++ {
			for j := p; j >= r; j-- {
				den := t[j+1+k-r] - t[j+k-p]
				alpha := 0.0
				if den != 0 {
					alpha = (u - t[j+k-p]) / den
				}
				for c := 0; c < 4; c++ {
					h[j][c] = (1-alpha)*h[j-1][c] + alpha*h[j][c]
				}
			}
		}
		v := h[p]
		if v[3] == 0 {
			return Vec3{v[0], v[1], v[2]}
		}
		return Vec3{v[0] / v[3], v[1] / v[3], v[2] / v[3]}
	}
	var out []Vec3
	for _, k := range spans {
		for s := 0; s < perSpan; s++ {
			out = append(out, eval(t[k]+(t[k+1]-t[k])*float64(s)/float64(perSpan), k))
		}
	}
	last := spans[len(spans)-1]
	out = append(out, eval(t[last+1], last))
	return out
}

// ThroughPoints is a smooth curve through fit points (centripetal
// Catmull-Rom): close to what AutoCAD draws for fit-point splines and spline
// leaders, without solving the interpolation system.
func ThroughPoints(points []Vec3, closed bool) []Vec3 {
	count := len(points)
	if count < 3 {
		return append([]Vec3(nil), points...)
	}
	segments := count - 1
	if closed {
		segments = count
	}
	per := max(1, min(10, maxSamples/segments))
	get := func(i int) Vec3 {
		if closed {
			return points[((i%count)+count)%count]
		}
		if i < 0 {
			return points[0].Add(points[0].Sub(points[1]))
		}
		if i >= count {
			return points[count-1].Add(points[count-1].Sub(points[count-2]))
		}
		return points[i]
	}
	var out []Vec3
	for i := 0; i < segments; i++ {
		p0, p1, p2, p3 := get(i-1), get(i), get(i+1), get(i+2)
		for s := 0; s < per; s++ {
			out = append(out, catmullRom(p0, p1, p2, p3, float64(s)/float64(per)))
		}
	}
	if closed {
		out = append(out, points[0])
	} else {
		out = append(out, points[count-1])
	}
	return out
}

// BulgeArc is the arc of a polyline segment from p0 to p1 with bulge (tan of
// a quarter of the included angle, positive counter-clockwise).
func BulgeArc(p0, p1 Vec2, bulge float64) (center Vec2, radius, start, sweep float64) {
	chord := p1.Sub(p0)
	center = p0.Add(chord.Mul(0.5)).Add(chord.Perp().Mul((1 - bulge*bulge) / (4 * bulge)))
	radius = p0.Sub(center).Length()
	start = math.Atan2(p0.Y-center.Y, p0.X-center.X)
	return center, radius, start, 4 * math.Atan(bulge)
}

func catmullRom(p0, p1, p2, p3 Vec3, u float64) Vec3 {
	// Centripetal parameterisation (alpha = 0.5) avoids loops and cusps.
	t0 := 0.0
	t1 := t0 + math.Sqrt(math.Max(p1.Sub(p0).Length(), 1e-12))
	t2 := t1 + math.Sqrt(math.Max(p2.Sub(p1).Length(), 1e-12))
	t3 := t2 + math.Sqrt(math.Max(p3.Sub(p2).Length(), 1e-12))
	t := t1 + (t2-t1)*u
	lerp := func(a, b Vec3, ta, tb float64) Vec3 {
		return a.Mul((tb - t) / (tb - ta)).Add(b.Mul((t - ta) / (tb - ta)))
	}
	a1 := lerp(p0, p1, t0, t1)
	a2 := lerp(p1, p2, t1, t2)
	a3 := lerp(p2, p3, t2, t3)
	b1 := lerp(a1, a2, t0, t2)
	b2 := lerp(a2, a3, t1, t3)
	return lerp(b1, b2, t1, t2)
}

func nonDecreasing(v []float64) bool {
	for i := 1; i < len(v); i++ {
		if !(v[i] >= v[i-1]) || math.IsInf(v[i], 0) {
			return false
		}
	}
	return len(v) > 0 && v[len(v)-1] > v[0]
}

func clampedUniform(n, p int) []float64 {
	k := make([]float64, n+p+1)
	for i := range k {
		switch {
		case i <= p:
			k[i] = 0
		case i >= n:
			k[i] = 1
		default:
			k[i] = float64(i-p) / float64(n-p)
		}
	}
	return k
}
