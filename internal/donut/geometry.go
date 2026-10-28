// Package donut is the donut toolbar without a window: its geometry and hit
// testing, its state and animations (Menu), and a renderer that draws it into
// an RGBA image in pure Go. The macOS GUI shows that image in a transparent
// panel; tests render it anywhere.
//
// Angles are measured clockwise from 12 o'clock, in radians. Slice i is
// centred on i·span, so the first preset sits at the top and the rest follow
// clockwise. Coordinates are logical points with y pointing down.
package donut

import "math"

// Point is a point in logical coordinates (y down).
type Point struct{ X, Y float64 }

func (p Point) Add(q Point) Point         { return Point{p.X + q.X, p.Y + q.Y} }
func (p Point) Sub(q Point) Point         { return Point{p.X - q.X, p.Y - q.Y} }
func (p Point) Mul(k float64) Point       { return Point{p.X * k, p.Y * k} }
func (p Point) Len() float64              { return math.Hypot(p.X, p.Y) }
func distance(a, b Point) float64         { return math.Hypot(a.X-b.X, a.Y-b.Y) }
func clamp(v, lo, hi float64) float64     { return math.Max(lo, math.Min(hi, v)) }
func lerp(a, b, t float64) float64        { return a + (b-a)*t }
func remainder(x, y float64) float64      { return math.Remainder(x, y) }
func pointAt(c Point, r, a float64) Point { return PointAt(c, r, a) }

// HitKind is what lies under a point of the donut.
type HitKind int

const (
	// Outside the donut.
	Outside HitKind = iota
	// Center is the hole in the middle (cancel / close).
	Center
	// Segment is a slice; Hit.Index is valid.
	Segment
	// EmptyRing is the ring of a donut without slices.
	EmptyRing
	// ProgressRing is the progress ring shown after a drop.
	ProgressRing
	// ProgressCancel is the × (cancel) button on the progress ring.
	ProgressCancel
	// SubSegment is a slice of the ring a group fans out: Index is the group, Sub the slice.
	SubSegment
)

func (k HitKind) String() string {
	return [...]string{"Outside", "Center", "Segment", "EmptyRing", "ProgressRing", "ProgressCancel", "SubSegment"}[k]
}

// Hit is the result of a hit test.
type Hit struct {
	Kind  HitKind
	Index int
	Sub   int
}

func hit(k HitKind) Hit { return Hit{Kind: k, Index: -1, Sub: -1} }

// Span is the angle covered by one of count slices.
func Span(count int) float64 {
	if count <= 0 {
		return 2 * math.Pi
	}
	return 2 * math.Pi / float64(count)
}

// MidAngle is the centre angle of slice index.
func MidAngle(index, count int) float64 { return float64(index) * Span(count) }

// PointAt converts a clockwise-from-top angle and radius into a point around c.
func PointAt(c Point, radius, angle float64) Point {
	return Point{c.X + radius*math.Sin(angle), c.Y - radius*math.Cos(angle)}
}

// AngleOf is the clockwise-from-top angle (0..2π) of p around c.
func AngleOf(c, p Point) float64 {
	a := math.Atan2(p.X-c.X, c.Y-p.Y)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

// IndexAt is the slice containing the angle.
func IndexAt(angle float64, count int) int {
	if count <= 0 {
		return -1
	}
	span := Span(count)
	i := int(math.Floor((angle+span/2)/span)) % count
	if i < 0 {
		i += count
	}
	return i
}

const (
	// SubOverhang is how far a group's fan reaches into each neighbouring slice, as a share of a slice.
	SubOverhang = 0.25
	// MinSubWidth is the narrowest a slice of a group's ring gets (along its middle), so its label still fits.
	MinSubWidth = 44
	// MinCornerRadius: rounding below this is not visible, the slice stays sharp.
	MinCornerRadius = 0.25
)

// SubSpan is the width of one slice of a group's outer ring: the fan covers
// the group's slice and a quarter of each neighbour, split evenly; on a
// crowded donut it widens to keep each slice MinSubWidth wide at midRadius,
// and it never goes round more than once.
func SubSpan(parentSpan float64, count int, midRadius float64) float64 {
	if count <= 0 {
		return 0
	}
	span := parentSpan * (1 + 2*SubOverhang) / float64(count)
	if midRadius > 0 {
		span = math.Max(span, MinSubWidth/midRadius)
	}
	return math.Min(span, 2*math.Pi/float64(count))
}

// SubMidAngle is the centre angle of slice sub of a fan of count centred on parentMid.
func SubMidAngle(parentMid float64, sub, count int, subSpan float64) float64 {
	return parentMid + (float64(sub)-float64(count-1)/2)*subSpan
}

// SubIndexAt is the slice of a group's fan (between inner and outer) that
// holds p, or -1.
func SubIndexAt(c, p Point, inner, outer, parentMid float64, count int, subSpan float64) int {
	d := distance(c, p)
	if count <= 0 || d < inner || d > outer {
		return -1
	}
	// Signed angle from the fan's centre, wrapped to -π..π so a fan across 12 o'clock works.
	delta := remainder(AngleOf(c, p)-parentMid, 2*math.Pi)
	sub := int(math.Floor(delta/subSpan + float64(count)/2))
	if sub >= 0 && sub < count {
		return sub
	}
	return -1
}

// HitTest classifies a point; slack extends the outer radius to include popped-out slices.
func HitTest(c, p Point, inner, outer float64, count int, slack float64) Hit {
	d := distance(c, p)
	switch {
	case d < inner:
		return hit(Center)
	case d > outer+slack:
		return hit(Outside)
	case count == 0:
		return hit(EmptyRing)
	}
	return Hit{Kind: Segment, Index: IndexAt(AngleOf(c, p), count), Sub: -1}
}

// Step is one step of a slice outline: a line to To, or (ArcRadius > 0) an arc.
type Step struct {
	To        Point
	ArcRadius float64
	LargeArc  bool
	Clockwise bool
}

// IsArc reports whether the step is an arc.
func (s Step) IsArc() bool { return s.ArcRadius > 0 }

// Outline is a closed slice outline starting at Start; CornerRadius is the rounding used after clamping.
type Outline struct {
	Start        Point
	Steps        []Step
	CornerRadius float64
}

// Wedge is the outline of a slice of span radians centred on mid between
// inner and outer, with a constant gap to its neighbours. cornerRadius rounds
// its four corners; it shrinks to what fits a thin or short slice, and 0
// gives the sharp slice. The rounding only cuts into the corners.
func Wedge(c Point, inner, outer, mid, span, gap, cornerRadius float64) Outline {
	outerHalf := math.Max(0.001, span/2-gap/2/outer)
	innerHalf := math.Max(0.001, span/2-gap/2/inner)
	outerStart := PointAt(c, outer, mid-outerHalf)
	innerStart := PointAt(c, inner, mid-innerHalf)

	radius := math.Min(cornerRadius, (outer-inner)/2)
	if radius > MinCornerRadius {
		if o, ok := rounded(c, inner, outer, mid, outerHalf, innerHalf, radius); ok {
			return o
		}
		// Too thin or short for it: the largest rounding that fits, found by halving.
		var best Outline
		found := false
		low, high := 0.0, radius
		for range 14 {
			r := (low + high) / 2
			if o, ok := rounded(c, inner, outer, mid, outerHalf, innerHalf, r); ok {
				best, found, low = o, true, r
			} else {
				high = r
			}
		}
		if found && low > MinCornerRadius {
			return best
		}
	}
	return Outline{Start: outerStart, Steps: []Step{
		{To: PointAt(c, outer, mid+outerHalf), ArcRadius: outer, LargeArc: outerHalf*2 > math.Pi, Clockwise: true},
		{To: PointAt(c, inner, mid+innerHalf)},
		{To: innerStart, ArcRadius: inner, LargeArc: innerHalf*2 > math.Pi, Clockwise: false},
	}}
}

type corner struct {
	center, onArc, onLine Point
	along                 float64
}

func rounded(c Point, r1, r2, mid, outerHalf, innerHalf, radius float64) (Outline, bool) {
	// The slice is symmetric about its middle: work out the corners on its start side and mirror them.
	outerSide := PointAt(c, r2, mid-outerHalf)
	innerSide := PointAt(c, r1, mid-innerHalf)
	o, ok1 := cornerOf(c, outerSide, innerSide, r2, radius, true)
	i, ok2 := cornerOf(c, outerSide, innerSide, r1, radius, false)
	if !ok1 || !ok2 {
		return Outline{}, false
	}
	offset := func(p Point) float64 { return remainder(AngleOf(c, p)-mid, 2*math.Pi) }
	if o.along >= i.along || offset(o.center) >= 0 || offset(i.center) >= 0 {
		return Outline{}, false
	}
	mirror := func(p Point) Point { return PointAt(c, distance(c, p), 2*mid-AngleOf(c, p)) }
	outerArc := -offset(o.center)
	innerArc := -offset(i.center)
	return Outline{Start: o.onArc, CornerRadius: radius, Steps: []Step{
		{To: mirror(o.onArc), ArcRadius: r2, LargeArc: outerArc > math.Pi/2, Clockwise: true},
		{To: mirror(o.onLine), ArcRadius: radius, Clockwise: true},
		{To: mirror(i.onLine)},
		{To: mirror(i.onArc), ArcRadius: radius, Clockwise: true},
		{To: i.onArc, ArcRadius: r1, LargeArc: innerArc > math.Pi/2, Clockwise: false},
		{To: i.onLine, ArcRadius: radius, Clockwise: true},
		{To: o.onLine},
		{To: o.onArc, ArcRadius: radius, Clockwise: true},
	}}, true
}

// cornerOf is the rounded corner where the slice's start side meets the circle of radius circle.
func cornerOf(c, outerSide, innerSide Point, circle, radius float64, outer bool) (corner, bool) {
	length := distance(outerSide, innerSide)
	if length <= 0 {
		return corner{}, false
	}
	u := Point{(innerSide.X - outerSide.X) / length, (innerSide.Y - outerSide.Y) / length}
	a := AngleOf(c, outerSide)
	tangent := Point{math.Cos(a), math.Sin(a)}
	n := Point{-u.Y, u.X}
	if n.X*tangent.X+n.Y*tangent.Y < 0 {
		n = n.Mul(-1)
	}
	dist := circle + radius
	if outer {
		dist = circle - radius
	}
	w := outerSide.Add(n.Mul(radius)).Sub(c)
	b := w.X*u.X + w.Y*u.Y
	disc := b*b - (w.X*w.X + w.Y*w.Y - dist*dist)
	if disc < 0 {
		return corner{}, false
	}
	t := -b - math.Sqrt(disc)
	if t < 0 || t > length {
		return corner{}, false
	}
	cc := outerSide.Add(n.Mul(radius)).Add(u.Mul(t))
	to := cc.Sub(c)
	onArc := c.Add(to.Mul(circle / to.Len()))
	return corner{center: cc, onArc: onArc, onLine: outerSide.Add(u.Mul(t)), along: t}, true
}
