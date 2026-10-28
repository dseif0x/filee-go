package donut

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/vector"
)

// Matrix is a 2-D affine transform: x' = A·x + C·y + E, y' = B·x + D·y + F.
type Matrix struct{ A, B, C, D, E, F float64 }

// Identity leaves points alone.
var Identity = Matrix{A: 1, D: 1}

// Translate, Scale and Rotate (clockwise on screen, y down) make transforms.
func Translate(x, y float64) Matrix { return Matrix{A: 1, D: 1, E: x, F: y} }
func Scale(sx, sy float64) Matrix   { return Matrix{A: sx, D: sy} }
func Rotate(a float64) Matrix {
	s, c := math.Sincos(a)
	return Matrix{A: c, B: s, C: -s, D: c}
}

// Then is m followed by n.
func (m Matrix) Then(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.B*n.C, B: m.A*n.B + m.B*n.D,
		C: m.C*n.A + m.D*n.C, D: m.C*n.B + m.D*n.D,
		E: m.E*n.A + m.F*n.C + n.E, F: m.E*n.B + m.F*n.D + n.F,
	}
}

// Apply transforms a point.
func (m Matrix) Apply(p Point) Point {
	return Point{m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F}
}

// about is a transform applied about the point c (translate c to the origin, t, back).
func about(c Point, t Matrix) Matrix { return Translate(-c.X, -c.Y).Then(t).Then(Translate(c.X, c.Y)) }

type opKind uint8

const (
	opMove opKind = iota
	opLine
	opQuad
	opCube
	opClose
)

type op struct {
	kind opKind
	p    [3]Point
}

// Path is a figure in logical coordinates. Filled with the non-zero rule:
// a hole is a sub-path going the other way round.
type Path struct {
	ops     []op
	start   Point
	current Point
	open    bool
}

func (p *Path) MoveTo(q Point) {
	if p.open {
		p.Close()
	}
	p.ops = append(p.ops, op{kind: opMove, p: [3]Point{q}})
	p.start, p.current, p.open = q, q, true
}

func (p *Path) LineTo(q Point) {
	if !p.open {
		p.MoveTo(p.current)
	}
	p.ops = append(p.ops, op{kind: opLine, p: [3]Point{q}})
	p.current = q
}

func (p *Path) QuadTo(c, q Point) {
	p.ops = append(p.ops, op{kind: opQuad, p: [3]Point{c, q}})
	p.current = q
}

func (p *Path) CubeTo(c1, c2, q Point) {
	p.ops = append(p.ops, op{kind: opCube, p: [3]Point{c1, c2, q}})
	p.current = q
}

// Close ends the current sub-path back at its start.
func (p *Path) Close() {
	if p.open {
		p.ops = append(p.ops, op{kind: opClose})
		p.current, p.open = p.start, false
	}
}

// Arc adds a circular arc around c from angle a0 sweeping by sweep (angles
// clockwise from 12 o'clock, like PointAt), as line segments fine enough for
// any scale up to 3.
func (p *Path) Arc(c Point, r, a0, sweep float64) {
	n := int(math.Ceil(math.Abs(sweep) * math.Max(r, 1) * 3 / 2))
	n = max(2, min(n, 720))
	for i := 0; i <= n; i++ {
		q := PointAt(c, r, a0+sweep*float64(i)/float64(n))
		if i == 0 && !p.open {
			p.MoveTo(q)
		} else {
			p.LineTo(q)
		}
	}
}

// ArcTo adds an SVG-style arc from the current point to q with radius r.
// clockwise is on screen (y down).
func (p *Path) ArcTo(q Point, r float64, large, clockwise bool) {
	from := p.current
	d := distance(from, q)
	if d < 1e-9 || r <= 0 {
		p.LineTo(q)
		return
	}
	r = math.Max(r, d/2)
	// The centre lies on the perpendicular bisector of the chord.
	mid := from.Add(q).Mul(0.5)
	h := math.Sqrt(math.Max(0, r*r-d*d/4))
	dir := Point{(q.X - from.X) / d, (q.Y - from.Y) / d}
	normal := Point{-dir.Y, dir.X} // to the right of the chord on screen
	// For a clockwise short arc the centre is to the right of the travel direction.
	sign := 1.0
	if clockwise == large {
		sign = -1
	}
	c := mid.Add(normal.Mul(h * sign))
	a0 := AngleOf(c, from)
	a1 := AngleOf(c, q)
	sweep := a1 - a0
	if clockwise {
		for sweep < 0 {
			sweep += 2 * math.Pi
		}
	} else {
		for sweep > 0 {
			sweep -= 2 * math.Pi
		}
	}
	n := int(math.Ceil(math.Abs(sweep) * math.Max(r, 1) * 3 / 2))
	n = max(2, min(n, 720))
	for i := 1; i < n; i++ {
		p.LineTo(PointAt(c, r, a0+sweep*float64(i)/float64(n)))
	}
	p.LineTo(q)
}

// Circle adds a whole circle, clockwise (or counter-clockwise for a hole).
func (p *Path) Circle(c Point, r float64, clockwise bool) {
	sweep := 2 * math.Pi
	if !clockwise {
		sweep = -sweep
	}
	p.Close()
	p.Arc(c, r, 0, sweep)
	p.Close()
}

// AddOutline adds a slice outline.
func (p *Path) AddOutline(o Outline) {
	p.MoveTo(o.Start)
	for _, s := range o.Steps {
		if s.IsArc() {
			p.ArcTo(s.To, s.ArcRadius, s.LargeArc, s.Clockwise)
		} else {
			p.LineTo(s.To)
		}
	}
	p.Close()
}

// Canvas draws paths into an RGBA image (premultiplied) at Scale device
// pixels per logical point.
type Canvas struct {
	Img   *image.RGBA
	Scale float64
	ras   vector.Rasterizer
}

// NewCanvas makes a canvas of w×h device pixels.
func NewCanvas(w, h int, scale float64) *Canvas {
	return &Canvas{Img: image.NewRGBA(image.Rect(0, 0, w, h)), Scale: scale}
}

// Clear makes every pixel transparent.
func (c *Canvas) Clear() { clear(c.Img.Pix) }

func withAlpha(col color.NRGBA, opacity float64) color.NRGBA {
	col.A = uint8(math.Round(float64(col.A) * clamp(opacity, 0, 1)))
	return col
}

// Fill fills p, transformed by m, with col at opacity.
func (c *Canvas) Fill(p *Path, m Matrix, col color.NRGBA, opacity float64) {
	col = withAlpha(col, opacity)
	if col.A == 0 || len(p.ops) == 0 {
		return
	}
	dev := m.Then(Scale(c.Scale, c.Scale))
	// Bounding box in device pixels, so only that part is rasterised.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, o := range p.ops {
		n := 0
		switch o.kind {
		case opMove, opLine:
			n = 1
		case opQuad:
			n = 2
		case opCube:
			n = 3
		}
		for i := range n {
			q := dev.Apply(o.p[i])
			minX, minY = math.Min(minX, q.X), math.Min(minY, q.Y)
			maxX, maxY = math.Max(maxX, q.X), math.Max(maxY, q.Y)
		}
	}
	r := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(c.Img.Rect)
	if r.Empty() {
		return
	}
	ox, oy := float64(r.Min.X), float64(r.Min.Y)
	c.ras.Reset(r.Dx(), r.Dy())
	pt := func(q Point) (float32, float32) {
		d := dev.Apply(q)
		return float32(d.X - ox), float32(d.Y - oy)
	}
	for _, o := range p.ops {
		switch o.kind {
		case opMove:
			x, y := pt(o.p[0])
			c.ras.MoveTo(x, y)
		case opLine:
			x, y := pt(o.p[0])
			c.ras.LineTo(x, y)
		case opQuad:
			x1, y1 := pt(o.p[0])
			x, y := pt(o.p[1])
			c.ras.QuadTo(x1, y1, x, y)
		case opCube:
			x1, y1 := pt(o.p[0])
			x2, y2 := pt(o.p[1])
			x, y := pt(o.p[2])
			c.ras.CubeTo(x1, y1, x2, y2, x, y)
		case opClose:
			c.ras.ClosePath()
		}
	}
	c.ras.ClosePath()
	c.ras.Draw(c.Img, r, image.NewUniform(col), image.Point{})
}

// StrokeStyle is how a line is drawn.
type StrokeStyle struct {
	Width float64
	// Round caps and joins (else flat caps, and joins filled by round dots anyway).
	Round bool
}

// Stroke draws the polyline points (closed when closed) with the style.
func (c *Canvas) Stroke(points []Point, closed bool, m Matrix, st StrokeStyle, col color.NRGBA, opacity float64) {
	if len(points) < 2 {
		return
	}
	var p Path
	half := st.Width / 2
	segs := len(points) - 1
	if closed {
		segs = len(points)
	}
	for i := range segs {
		a, b := points[i], points[(i+1)%len(points)]
		d := distance(a, b)
		if d < 1e-9 {
			continue
		}
		n := Point{-(b.Y - a.Y) / d * half, (b.X - a.X) / d * half}
		quad := []Point{a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)}
		addPositive(&p, quad)
	}
	// Joins (and round caps): dots at the vertices so segments meet without notches.
	for i, q := range points {
		end := !closed && (i == 0 || i == len(points)-1)
		if end {
			if st.Round {
				p.Circle(q, half, true)
			}
			continue
		}
		// A join where the line turns enough to leave a notch.
		prev := points[(i-1+len(points))%len(points)]
		next := points[(i+1)%len(points)]
		a, b := q.Sub(prev), next.Sub(q)
		la, lb := a.Len(), b.Len()
		if la < 1e-9 || lb < 1e-9 || (a.X*b.X+a.Y*b.Y)/(la*lb) < 0.97 {
			p.Circle(q, half, true)
		}
	}
	c.Fill(&p, m, col, opacity)
}

// addPositive adds a polygon going clockwise on screen, so overlapping
// pieces of one stroke add up instead of cancelling.
func addPositive(p *Path, poly []Point) {
	area := 0.0
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		area += a.X*b.Y - b.X*a.Y
	}
	if area < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
	p.MoveTo(poly[0])
	for _, q := range poly[1:] {
		p.LineTo(q)
	}
	p.Close()
}

// FillCircle fills a circle.
func (c *Canvas) FillCircle(center Point, r float64, m Matrix, col color.NRGBA, opacity float64) {
	var p Path
	p.Circle(center, r, true)
	c.Fill(&p, m, col, opacity)
}

// StrokeCircle draws a circle's outline as a ring of the pen's width.
func (c *Canvas) StrokeCircle(center Point, r, width float64, m Matrix, col color.NRGBA, opacity float64) {
	var p Path
	p.Circle(center, r+width/2, true)
	p.Circle(center, math.Max(0, r-width/2), false)
	c.Fill(&p, m, col, opacity)
}

// GradientStop is a colour at an offset (0 centre, 1 edge) of a radial gradient.
type GradientStop struct {
	Offset float64
	Color  color.NRGBA
}

// FillRadial fills a circle with a radial gradient (no transform: the
// shadows only fade and move).
func (c *Canvas) FillRadial(center Point, radius float64, stops []GradientStop, opacity float64) {
	if opacity <= 0.001 || radius <= 0 {
		return
	}
	cx, cy, rr := center.X*c.Scale, center.Y*c.Scale, radius*c.Scale
	r := image.Rect(int(cx-rr)-1, int(cy-rr)-1, int(cx+rr)+2, int(cy+rr)+2).Intersect(c.Img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			t := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) / rr
			if t >= 1 {
				continue
			}
			col := gradientAt(stops, t)
			a := float64(col.A) / 255 * opacity
			if a <= 0 {
				continue
			}
			i := c.Img.PixOffset(x, y)
			blend(c.Img.Pix[i:i+4], col, a)
		}
	}
}

func gradientAt(stops []GradientStop, t float64) color.NRGBA {
	if t <= stops[0].Offset {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Offset {
			a, b := stops[i-1], stops[i]
			k := 0.0
			if b.Offset > a.Offset {
				k = (t - a.Offset) / (b.Offset - a.Offset)
			}
			mix := func(x, y uint8) uint8 { return uint8(math.Round(lerp(float64(x), float64(y), k))) }
			return color.NRGBA{mix(a.Color.R, b.Color.R), mix(a.Color.G, b.Color.G), mix(a.Color.B, b.Color.B), mix(a.Color.A, b.Color.A)}
		}
	}
	return stops[len(stops)-1].Color
}

// blend paints col at alpha a over a premultiplied pixel.
func blend(px []byte, col color.NRGBA, a float64) {
	inv := 1 - a
	px[0] = uint8(math.Min(255, float64(col.R)*a+float64(px[0])*inv))
	px[1] = uint8(math.Min(255, float64(col.G)*a+float64(px[1])*inv))
	px[2] = uint8(math.Min(255, float64(col.B)*a+float64(px[2])*inv))
	px[3] = uint8(math.Min(255, 255*a+float64(px[3])*inv))
}

// subpaths are the path's figures as polylines (curves by their end points),
// for drawing outlines.
func (p *Path) subpaths() [][]Point {
	var out [][]Point
	var cur []Point
	for _, o := range p.ops {
		switch o.kind {
		case opMove:
			if len(cur) > 1 {
				out = append(out, cur)
			}
			cur = []Point{o.p[0]}
		case opLine:
			cur = append(cur, o.p[0])
		case opQuad:
			cur = append(cur, o.p[1])
		case opCube:
			cur = append(cur, o.p[2])
		case opClose:
			if len(cur) > 1 {
				out = append(out, cur)
			}
			cur = nil
		}
	}
	if len(cur) > 1 {
		out = append(out, cur)
	}
	return out
}
