package trace

import "math"

// Step 2: a pixel outline becomes a smooth closed path of lines and cubic
// Béziers.
//
//  1. Sampling: the staircase of pixel edges is replaced by points on it. Short
//     runs (the steps of a slanted or curved edge) contribute their midpoint;
//     long runs contribute points along themselves. A pixel corner between two
//     long runs (a rectangle), and the two corners of a short run between long
//     ones that both turn the same way (the end of a thin line), stay corners.
//  2. Corners: a point where the direction turns sharply within about two
//     pixels, or turns less but all at once, which a small circle doesn't.
//  3. Smoothing: points between corners are averaged with their neighbours.
//  4. Straight stretches: runs of points that stay within half a pixel of a
//     line and don't bend become lines.
//  5. Fitting: the rest becomes cubic Béziers fitted by least squares with
//     Newton–Raphson reparameterization, split where the error is largest
//     (Philip J. Schneider, "An Algorithm for Automatically Fitting Digitized
//     Curves", Graphics Gems, 1990).

// Vec is a point or vector in pixels.
type Vec struct{ X, Y float64 }

func (a Vec) add(b Vec) Vec        { return Vec{a.X + b.X, a.Y + b.Y} }
func (a Vec) sub(b Vec) Vec        { return Vec{a.X - b.X, a.Y - b.Y} }
func (a Vec) neg() Vec             { return Vec{-a.X, -a.Y} }
func (a Vec) mul(s float64) Vec    { return Vec{a.X * s, a.Y * s} }
func (a Vec) dot(b Vec) float64    { return a.X*b.X + a.Y*b.Y }
func (a Vec) length() float64      { return math.Sqrt(a.X*a.X + a.Y*a.Y) }
func (a Vec) lengthSquared() float64 { return a.X*a.X + a.Y*a.Y }

func (a Vec) normalized() Vec {
	l := a.length()
	if l < 1e-12 {
		return Vec{}
	}
	return Vec{a.X / l, a.Y / l}
}

// Segment is a line to End, or a cubic Bézier with two control points.
type Segment struct {
	IsLine             bool
	Control1, Control2 Vec
	End                Vec
}

func lineSeg(end Vec) Segment         { return Segment{true, end, end, end} }
func cubicSeg(c1, c2, end Vec) Segment { return Segment{false, c1, c2, end} }

// Path is a closed path starting at Start.
type Path struct {
	Start    Vec
	Segments []Segment
}

const (
	cornerReach           = 2.0
	sampleSpacing         = 2.0
	maxReparameterizations = 4
	minStretch            = 10.0
	stretchTolerance      = 0.5
	stretchBend           = 0.035
)

// Fit fits a smooth path to a pixel outline.
func Fit(outline *PixelOutline, s *Settings) Path {
	points, hard := sample(outline, s.CornerRun)
	corners := findCorners(points, hard, s.CornerAngle)
	smooth(points, corners, s.SmoothingPasses)

	tol := s.CurveTolerance
	var segs []Segment
	count := len(points)
	first := -1
	for i, c := range corners {
		if c {
			first = i
			break
		}
	}
	if first < 0 {
		loop := rotated(points, 0)
		stretches := findStretches(loop, 0, count)
		if len(stretches) > 0 {
			loop = rotated(points, stretches[0][0])
			dir := loop[stretches[0][1]-stretches[0][0]].sub(loop[0]).normalized()
			fitBetween(loop, 0, count, dir, dir.neg(), findStretches(loop, 0, count), tol, &segs)
			return Path{loop[0], segs}
		}
		half := count / 2
		atStart := centerTangent(loop, 0, 0, count-1, true)
		atHalf := centerTangent(loop, half, 0, count, false)
		fitCubic(loop, 0, half, atStart.neg(), atHalf, tol, &segs)
		fitCubic(loop, half, count, atHalf.neg(), atStart, tol, &segs)
		return Path{loop[0], segs}
	}
	path := rotated(points, first)
	from := 0
	for i := 1; i <= count; i++ {
		if i < count && !corners[(first+i)%count] {
			continue
		}
		fitPiece(path, from, i, tol, &segs)
		from = i
	}
	return Path{path[0], segs}
}

func rotated(points []Vec, start int) []Vec {
	n := len(points)
	out := make([]Vec, n+1)
	for i := 0; i <= n; i++ {
		out[i] = points[(start+i)%n]
	}
	return out
}

func sign(v int64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sample(o *PixelOutline, cornerRun int) ([]Vec, []bool) {
	n := o.count()
	lengths := make([]int, n)
	turns := make([]int, n)
	for i := 0; i < n; i++ {
		next := (i + 1) % n
		lengths[i] = absInt(o.Xs[next]-o.Xs[i]) + absInt(o.Ys[next]-o.Ys[i])
	}
	for i := 0; i < n; i++ {
		prev := (i + n - 1) % n
		next := (i + 1) % n
		inX, inY := int64(o.Xs[i]-o.Xs[prev]), int64(o.Ys[i]-o.Ys[prev])
		outX, outY := int64(o.Xs[next]-o.Xs[i]), int64(o.Ys[next]-o.Ys[i])
		turns[i] = sign(inX*outY - inY*outX)
	}
	hardCorner := make([]bool, n)
	for i := 0; i < n; i++ {
		before := lengths[(i+n-1)%n]
		after := lengths[i]
		if before >= cornerRun && after >= cornerRun {
			hardCorner[i] = true
		}
		next := (i + 1) % n
		if after <= 2 && turns[i] == turns[next] && before >= cornerRun && lengths[next] >= cornerRun {
			hardCorner[i] = true
			hardCorner[next] = true
		}
	}
	points := make([]Vec, 0, n*2)
	hard := make([]bool, 0, n*2)
	for i := 0; i < n; i++ {
		start := Vec{float64(o.Xs[i]), float64(o.Ys[i])}
		next := (i + 1) % n
		end := Vec{float64(o.Xs[next]), float64(o.Ys[next])}
		if hardCorner[i] {
			points = append(points, start)
			hard = append(hard, true)
		}
		length := lengths[i]
		dir := end.sub(start).mul(1 / float64(length))
		if length <= 2 {
			points = append(points, start.add(dir.mul(float64(length)/2)))
			hard = append(hard, false)
			continue
		}
		samples := max(2, int(math.Ceil(float64(length-2)/sampleSpacing))+1)
		for s := 0; s < samples; s++ {
			off := 1 + float64(length-2)*float64(s)/float64(samples-1)
			points = append(points, start.add(dir.mul(off)))
			hard = append(hard, false)
		}
	}
	return points, hard
}

func findCorners(points []Vec, hard []bool, cornerAngle float64) []bool {
	count := len(points)
	corners := append([]bool(nil), hard...)
	if count < 6 {
		return corners
	}
	limit := count / 3
	angles := make([]float64, count)
	back := make([]int, count)
	fwd := make([]int, count)
	candidate := make([]bool, count)
	gentle := cornerAngle - 20
	for i := 0; i < count; i++ {
		back[i] = reach(points, i, -1, cornerReach, limit)
		fwd[i] = reach(points, i, 1, cornerReach, limit)
		angles[i] = turnAngle(points[back[i]], points[i], points[fwd[i]])
		if hard[i] || angles[i] <= gentle {
			continue
		}
		if angles[i] > cornerAngle {
			candidate[i] = true
			continue
		}
		wide := turnAngle(points[reach(points, i, -1, 2*cornerReach, limit)], points[i], points[reach(points, i, 1, 2*cornerReach, limit)])
		candidate[i] = angles[i] >= 0.65*wide
	}
	for i := 0; i < count; i++ {
		if !candidate[i] {
			continue
		}
		best := !hard[back[i]] && !hard[fwd[i]]
		for j := back[i]; j != fwd[i] && best; j = (j + 1) % count {
			if j == i {
				continue
			}
			if hard[j] || (candidate[j] && (angles[j] > angles[i] || (angles[j] == angles[i] && j < i))) {
				best = false
			}
		}
		if best {
			corners[i] = true
		}
	}
	return corners
}

func turnAngle(before, here, after Vec) float64 {
	in := here.sub(before)
	out := after.sub(here)
	den := in.length() * out.length()
	if den < 1e-12 {
		return 0
	}
	c := math.Max(-1, math.Min(1, in.dot(out)/den))
	return math.Acos(c) * 180 / math.Pi
}

func reach(points []Vec, i, step int, distance float64, limit int) int {
	count := len(points)
	j := i
	for s := 0; s < limit; s++ {
		j = (j + step + count) % count
		if points[j].sub(points[i]).lengthSquared() >= distance*distance {
			break
		}
	}
	return j
}

func smooth(points []Vec, corners []bool, passes int) {
	count := len(points)
	if count < 5 {
		return
	}
	next := make([]Vec, count)
	for p := 0; p < passes; p++ {
		for i := 0; i < count; i++ {
			before := points[(i+count-1)%count]
			after := points[(i+1)%count]
			here := points[i]
			if corners[i] || here.sub(before).lengthSquared() > 6.25 || after.sub(here).lengthSquared() > 6.25 {
				next[i] = here
			} else {
				next[i] = before.add(here.mul(2)).add(after).mul(0.25)
			}
		}
		copy(points, next)
	}
}

func fitPiece(d []Vec, first, last int, tol float64, out *[]Segment) {
	if last-first <= 1 || isStraight(d, first, last, math.Max(1, tol)) {
		*out = append(*out, lineSeg(d[last]))
		return
	}
	fitBetween(d, first, last, endTangent(d, first, last, 1), endTangent(d, last, first, -1), findStretches(d, first, last), tol, out)
}

func fitBetween(d []Vec, first, last int, startT, endT Vec, stretches [][2]int, tol float64, out *[]Segment) {
	cursor := first
	t := startT
	for _, st := range stretches {
		start, end := st[0], st[1]
		dir := d[end].sub(d[start]).normalized()
		if start > cursor {
			fitCubic(d, cursor, start, t, dir.neg(), tol, out)
		}
		*out = append(*out, lineSeg(d[end]))
		cursor = end
		t = dir
	}
	if cursor < last {
		fitCubic(d, cursor, last, t, endT, tol, out)
	}
}

func findStretches(d []Vec, first, last int) [][2]int {
	var out [][2]int
	i := first
	for i < last-1 {
		end := longestStretch(d, i, last)
		if d[end].sub(d[i]).length() >= minStretch && !bends(d, i, end) {
			out = append(out, [2]int{i, end})
			i = end
		} else {
			i++
		}
	}
	return out
}

func longestStretch(d []Vec, start, last int) int {
	end := start + 1
	for j := start + 2; j <= last; j++ {
		if !isStraight(d, start, j, stretchTolerance) {
			break
		}
		end = j
	}
	return end
}

func bends(d []Vec, start, end int) bool {
	chord := d[end].sub(d[start])
	length := chord.length()
	normal := Vec{-chord.Y / length, chord.X / length}
	num, den := 0.0, 0.0
	for i := start + 1; i < end; i++ {
		off := d[i].sub(d[start])
		t := math.Max(0, math.Min(1, off.dot(chord)/(length*length)))
		shape := 4 * t * (1 - t)
		num += off.dot(normal) * shape
		den += shape * shape
	}
	sag := 0.0
	if den > 0 {
		sag = num / den
	}
	return math.Abs(8*sag/length) > stretchBend
}

func isStraight(d []Vec, first, last int, tol float64) bool {
	a := d[first]
	chord := d[last].sub(a)
	length := chord.length()
	if length < 1e-9 {
		return false
	}
	normal := Vec{-chord.Y / length, chord.X / length}
	for i := first + 1; i < last; i++ {
		off := d[i].sub(a)
		if math.Abs(off.dot(normal)) > tol {
			return false
		}
		along := off.dot(chord) / (length * length)
		if along < -0.05 || along > 1.05 {
			return false
		}
	}
	return true
}

func endTangent(d []Vec, at, other, step int) Vec {
	i := at
	for i != other {
		i += step
		if d[i].sub(d[at]).lengthSquared() >= 6.25 {
			break
		}
	}
	return d[i].sub(d[at]).normalized()
}

func centerTangent(d []Vec, center, first, last int, closed bool) Vec {
	clampIdx := func(i int) int {
		if closed {
			n := last + 1
			return ((i % n) + n) % n
		}
		return max(first, min(last, i))
	}
	span := last - first + 1
	if closed {
		span = last + 1
	}
	back, fwd := center, center
	for s := 0; s < span/3+1; s++ {
		c := clampIdx(back - 1)
		if c == back {
			break
		}
		back = c
		if d[back].sub(d[center]).lengthSquared() >= 2.25 {
			break
		}
	}
	for s := 0; s < span/3+1; s++ {
		c := clampIdx(fwd + 1)
		if c == fwd {
			break
		}
		fwd = c
		if d[fwd].sub(d[center]).lengthSquared() >= 2.25 {
			break
		}
	}
	t := d[back].sub(d[fwd]).normalized()
	if t.lengthSquared() > 0 {
		return t
	}
	return d[clampIdx(center-1)].sub(d[clampIdx(center+1)]).normalized()
}

func fitCubic(d []Vec, first, last int, startT, endT Vec, tol float64, out *[]Segment) {
	errLimit := tol * tol
	type job struct {
		a, b   int
		t1, t2 Vec
	}
	pending := []job{{first, last, startT, endT}}
	for len(pending) > 0 {
		j := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		a, b, t1, t2 := j.a, j.b, j.t1, j.t2
		if b-a == 1 {
			dist := d[b].sub(d[a]).length() / 3
			*out = append(*out, cubicSeg(d[a].add(t1.mul(dist)), d[b].add(t2.mul(dist)), d[b]))
			continue
		}
		u := chordLengths(d, a, b)
		bez := generateBezier(d, a, b, u, t1, t2)
		maxErr, split := maxError(d, a, b, bez, u)
		if maxErr > errLimit && maxErr < errLimit*4 {
			for i := 0; i < maxReparameterizations && maxErr > errLimit; i++ {
				u = reparameterize(d, a, b, u, bez)
				bez = generateBezier(d, a, b, u, t1, t2)
				maxErr, split = maxError(d, a, b, bez, u)
			}
		}
		if maxErr <= errLimit {
			*out = append(*out, cubicSeg(bez[1], bez[2], bez[3]))
			continue
		}
		center := centerTangent(d, split, a, b, false)
		pending = append(pending, job{split, b, center.neg(), t2}, job{a, split, t1, center})
	}
}

func chordLengths(d []Vec, first, last int) []float64 {
	u := make([]float64, last-first+1)
	for i := first + 1; i <= last; i++ {
		u[i-first] = u[i-first-1] + d[i].sub(d[i-1]).length()
	}
	total := u[len(u)-1]
	for i := 1; i < len(u); i++ {
		if total > 0 {
			u[i] /= total
		} else {
			u[i] = float64(i) / float64(len(u)-1)
		}
	}
	return u
}

func generateBezier(d []Vec, first, last int, u []float64, t1, t2 Vec) [4]Vec {
	var c00, c01, c11, x0, x1 float64
	p0, p3 := d[first], d[last]
	for i, t := range u {
		mt := 1 - t
		b0 := mt * mt * mt
		b1 := 3 * t * mt * mt
		b2 := 3 * t * t * mt
		b3 := t * t * t
		a1 := t1.mul(b1)
		a2 := t2.mul(b2)
		c00 += a1.dot(a1)
		c01 += a1.dot(a2)
		c11 += a2.dot(a2)
		rest := d[first+i].sub(p0.mul(b0 + b1).add(p3.mul(b2 + b3)))
		x0 += a1.dot(rest)
		x1 += a2.dot(rest)
	}
	det := c00*c11 - c01*c01
	alpha1, alpha2 := 0.0, 0.0
	if math.Abs(det) > 1e-12 {
		alpha1 = (x0*c11 - x1*c01) / det
		alpha2 = (c00*x1 - c01*x0) / det
	}
	chord := p3.sub(p0).length()
	eps := 1e-6 * chord
	if alpha1 < eps || alpha2 < eps || alpha1 > chord*2 || alpha2 > chord*2 {
		alpha1, alpha2 = chord/3, chord/3
	}
	return [4]Vec{p0, p0.add(t1.mul(alpha1)), p3.add(t2.mul(alpha2)), p3}
}

func maxError(d []Vec, first, last int, bez [4]Vec, u []float64) (float64, int) {
	split := (first + last) / 2
	best := 0.0
	for i := first + 1; i < last; i++ {
		e := evaluate(bez, u[i-first]).sub(d[i]).lengthSquared()
		if e >= best {
			best = e
			split = i
		}
	}
	return best, split
}

func reparameterize(d []Vec, first, last int, u []float64, bez [4]Vec) []float64 {
	out := make([]float64, len(u))
	for i := range u {
		out[i] = newtonRaphson(bez, d[first+i], u[i])
	}
	return out
}

func newtonRaphson(q [4]Vec, p Vec, u float64) float64 {
	diff := evaluate(q, u).sub(p)
	mt := 1 - u
	first := q[1].sub(q[0]).mul(mt * mt).add(q[2].sub(q[1]).mul(2 * mt * u)).add(q[3].sub(q[2]).mul(u * u)).mul(3)
	second := q[2].sub(q[1].mul(2)).add(q[0]).mul(mt).add(q[3].sub(q[2].mul(2)).add(q[1]).mul(u)).mul(6)
	num := diff.dot(first)
	den := first.dot(first) + diff.dot(second)
	if math.Abs(den) < 1e-12 {
		return u
	}
	return math.Max(0, math.Min(1, u-num/den))
}

func evaluate(q [4]Vec, t float64) Vec {
	mt := 1 - t
	return q[0].mul(mt * mt * mt).add(q[1].mul(3 * t * mt * mt)).add(q[2].mul(3 * t * t * mt)).add(q[3].mul(t * t * t))
}
