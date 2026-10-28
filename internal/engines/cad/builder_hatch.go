package cad

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// HATCH for the scene: boundary loops (lines, arcs, ellipses, splines,
// polylines) become even-odd filled paths; pattern hatches draw their
// pattern lines clipped to the boundary, or a light fill when too dense.

const (
	maxPatternSegmentsPerHatch = 20_000
	maxPatternSegments         = 2_000_000
)

func (b *builder) drawHatch(h *cadoc.Hatch, layer *cadoc.Layer, c drawContext) {
	var loops []*Figure
	for _, p := range h.Paths {
		if l := hatchLoop(p); l != nil {
			loops = append(loops, l)
		}
	}
	if len(loops) == 0 {
		return
	}
	plane := c.transform.Multiply(Ocs(v3(h.Normal))).Multiply(Translation(Vec3{0, 0, h.Elevation}))
	boundary := make([]*Figure, len(loops))
	for i, l := range loops {
		boundary[i] = mapFigure(l, plane)
	}
	color := resolveColor(h.Color, layer, c)
	if h.IsSolid {
		g := h.GradientColor
		var second *RGB
		angle := 0.0
		if g != nil && g.Enabled && len(g.Colors) > 0 {
			color = resolveColor(g.Colors[0].Color, layer, c)
			s := blend(color, RGB{255, 255, 255}, 0.8)
			if len(g.Colors) > 1 {
				s = resolveColor(g.Colors[1].Color, layer, c)
			}
			second = &s
		}
		if g != nil {
			angle = g.Angle
		}
		b.addFill(&FillPrimitive{Color: color, GradientColor: second, GradientAngle: angle, IsArea: true, Figures: boundary})
		return
	}
	box := EmptyBox()
	for _, l := range loops {
		l.AddTo(&box)
	}
	var lines [][2]Vec2
	ok := false
	if h.Pattern != nil && len(h.Pattern.Lines) > 0 && b.patternSegments < maxPatternSegments {
		lines, ok = patternLines(h.Pattern, box)
	}
	if !ok {
		// Too dense (or no pattern data): a light tint keeps the area recognisable.
		b.addFill(&FillPrimitive{Color: blend(color, RGB{255, 255, 255}, 0.75), IsArea: true, Figures: boundary})
		return
	}
	b.patternSegments += len(lines)
	p := &HatchLinesPrimitive{Color: color, WidthMm: math.Min(resolveWidth(h, layer, c), defaultLineWeightMm), Boundary: boundary}
	for _, l := range lines {
		p.Lines = append(p.Lines, [2]Vec2{plane.PointXY(l[0].X, l[0].Y), plane.PointXY(l[1].X, l[1].Y)})
	}
	if b.reserve() {
		for _, f := range boundary {
			f.AddTo(&b.scene.Extents)
		}
		b.scene.Items = append(b.scene.Items, p)
	}
}

// hatchLoop is one boundary loop in the hatch's OCS plane.
func hatchLoop(path *cadoc.HatchBoundaryPath) *Figure {
	var f *Figure
	moveOrLine := func(p Vec2) {
		if f == nil {
			f = NewFigure(p)
		} else if f.End().Sub(p).Length() > 1e-9 {
			f.LineTo(p)
		}
	}
	// Clockwise edges store their angles mirrored at the X axis: the arc runs from -start to -end.
	edgeArc := func(center, u, v Vec2, start, end float64, ccw bool) {
		var t0, t1 float64
		if ccw {
			t0, t1 = start, end
			for t1 <= t0 {
				t1 += 2 * math.Pi
			}
		} else {
			t0, t1 = -start, -end
			for t1 >= t0 {
				t1 -= 2 * math.Pi
			}
		}
		moveOrLine(center.Add(u.Mul(math.Cos(t0))).Add(v.Mul(math.Sin(t0))))
		f.ArcTo(center, u, v, t0, t1)
	}
	for _, edge := range path.Edges {
		switch e := edge.(type) {
		case *cadoc.HatchLineEdge:
			moveOrLine(Vec2{e.Start.X, e.Start.Y})
			f.LineTo(Vec2{e.End.X, e.End.Y})
		case *cadoc.HatchArcEdge:
			r := e.Radius
			edgeArc(Vec2{e.Center.X, e.Center.Y}, Vec2{r, 0}, Vec2{0, r}, e.StartAngle, e.EndAngle, e.CounterClockWise)
		case *cadoc.HatchEllipseEdge:
			major := Vec2{e.MajorAxisEndPoint.X, e.MajorAxisEndPoint.Y}
			edgeArc(Vec2{e.Center.X, e.Center.Y}, major, major.Perp().Mul(e.RadiusRatio), e.StartAngle, e.EndAngle, e.CounterClockWise)
		case *cadoc.HatchSplineEdge:
			var pts []Vec3
			if len(e.ControlPoints) >= 2 {
				ctrl := make([]Vec3, len(e.ControlPoints))
				var w []float64
				for i, p := range e.ControlPoints {
					ctrl[i] = Vec3{p.X, p.Y, 0}
					if e.IsRational {
						// Rational edges keep each weight in the control point's Z.
						w = append(w, p.Z)
					}
				}
				pts = SampleNurbs(int(e.Degree), ctrl, e.Knots, w)
			} else {
				fit := make([]Vec3, len(e.FitPoints))
				for i, p := range e.FitPoints {
					fit[i] = Vec3{p.X, p.Y, 0}
				}
				pts = ThroughPoints(fit, false)
			}
			for _, p := range pts {
				moveOrLine(Vec2{p.X, p.Y})
			}
		case *cadoc.HatchPolylineEdge:
			vs := e.Vertices
			if len(vs) == 0 {
				break
			}
			moveOrLine(Vec2{vs[0].X, vs[0].Y})
			n := len(vs) - 1
			if e.IsClosed {
				n = len(vs)
			}
			for i := 0; i < n; i++ {
				a, bv := vs[i], vs[(i+1)%len(vs)]
				appendSegment(f, Identity3, polyVertex{loc: Vec2{a.X, a.Y}, bulge: a.Z}, polyVertex{loc: Vec2{bv.X, bv.Y}, bulge: bv.Z})
			}
		}
	}
	if f == nil || len(f.Segments) == 0 {
		return nil
	}
	f.Closed = true
	return f
}

// patternLines are the pattern's line families across the boundary's box
// (in the hatch plane); false when the pattern is too dense.
func patternLines(pattern *cadoc.HatchPattern, box Box2) ([][2]Vec2, bool) {
	var out [][2]Vec2
	corners := []Vec2{{box.MinX, box.MinY}, {box.MaxX, box.MinY}, {box.MinX, box.MaxY}, {box.MaxX, box.MaxY}}
	size := math.Max(math.Max(box.Width(), box.Height()), 1e-9)
	minMax := func(f func(Vec2) float64) (float64, float64) {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, c := range corners {
			v := f(c)
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		return lo, hi
	}
	for _, line := range pattern.Lines {
		d := Vec2{math.Cos(line.Angle), math.Sin(line.Angle)}
		n := d.Perp()
		base := Vec2{line.BasePoint.X, line.BasePoint.Y}
		offset := Vec2{line.Offset.X, line.Offset.Y}
		spacing := Dot2(offset, n)
		c0 := Dot2(base, n)
		low, high := minMax(func(c Vec2) float64 { return Dot2(c, n) })
		var first, last int64
		if math.Abs(spacing) < size*1e-9 {
			if c0 < low || c0 > high {
				continue
			}
		} else {
			k1, k2 := (low-c0)/spacing, (high-c0)/spacing
			first = int64(math.Floor(math.Min(k1, k2)))
			last = int64(math.Ceil(math.Max(k1, k2)))
		}
		if last-first > maxPatternSegmentsPerHatch {
			return nil, false
		}
		var dashes []float64
		period := 0.0
		allPositive := true
		for _, x := range line.DashLengths {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				continue
			}
			dashes = append(dashes, x)
			period += math.Abs(x)
			if x <= 0 {
				allPositive = false
			}
		}
		for k := first; k <= last; k++ {
			origin := base.Add(offset.Mul(float64(k)))
			tMin, tMax := minMax(func(c Vec2) float64 { return Dot2(c.Sub(origin), d) })
			if len(dashes) == 0 || period <= 0 || allPositive {
				out = append(out, [2]Vec2{origin.Add(d.Mul(tMin)), origin.Add(d.Mul(tMax))})
			} else {
				if (tMax-tMin)/period*float64(len(dashes))+float64(len(out)) > maxPatternSegmentsPerHatch {
					return nil, false
				}
				for t := math.Floor(tMin/period) * period; t < tMax; {
					for _, dash := range dashes {
						if dash > 0 && t+dash > tMin && t < tMax {
							out = append(out, [2]Vec2{origin.Add(d.Mul(math.Max(t, tMin))), origin.Add(d.Mul(math.Min(t+dash, tMax)))})
						} else if dash == 0 && t >= tMin && t <= tMax {
							out = append(out, [2]Vec2{origin.Add(d.Mul(t)), origin.Add(d.Mul(t))})
						}
						t += math.Abs(dash)
					}
				}
			}
			if len(out) > maxPatternSegmentsPerHatch {
				return nil, false
			}
		}
	}
	return out, true
}

// mapFigure maps a figure built in a plane's own coordinates to WCS (exact for Bézier arcs).
func mapFigure(local *Figure, plane Affine3) *Figure {
	m := NewFigure(plane.PointXY(local.Start.X, local.Start.Y))
	m.Closed = local.Closed
	for _, s := range local.Segments {
		p := plane.PointXY(s.P.X, s.P.Y)
		if s.Cubic {
			m.Segments = append(m.Segments, Segment{C1: plane.PointXY(s.C1.X, s.C1.Y), C2: plane.PointXY(s.C2.X, s.C2.Y), P: p, Cubic: true})
		} else {
			m.Segments = append(m.Segments, Segment{P: p})
		}
	}
	return m
}

func blend(c, toward RGB, amount float64) RGB {
	mix := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*amount) }
	return RGB{mix(c.R, toward.R), mix(c.G, toward.G), mix(c.B, toward.B)}
}
