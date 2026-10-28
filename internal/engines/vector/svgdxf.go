package vector

import (
	"image"
	"math"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// SVG → DXF: the drawing's outlines as CAD geometry. The SVG is drawn onto a
// device that keeps outlines instead of pixels, so groups, transforms, <use>,
// nested <svg> and CSS are resolved exactly as for PDF output:
//   - circles and ellipses become CIRCLE and ELLIPSE entities (an ellipse also
//     when a transform skews a circle);
//   - everything else becomes LWPOLYLINEs, closed where the shape is closed, or
//     a LINE for a single straight segment; curves are divided into straight
//     pieces within 0.01 mm of the curve, which every CAD and CAM program reads;
//   - text becomes its glyph outlines.
// Colours are kept as true colours (the stroke's, else the fill's); fills,
// stroke widths, gradients, clipping, masks, images and filters are not
// represented.
//
// Units: SVG user units are CSS pixels, 96 per inch, so the DXF is in
// millimetres ($INSUNITS 4). SVG's y axis points down and DXF's up, so y is
// flipped, with the bottom-left corner of the SVG's viewport at (0, 0).

const (
	millimetresPerPixel = 25.4 / 96
	flatnessMillimetres = 0.01
	maxPiecesPerCurve   = 512
)

type dxfKind int

const (
	dxfLine dxfKind = iota
	dxfPolyline
	dxfCircle
	dxfEllipse
)

// dxfShape is one entity to write, in millimetres with y up.
type dxfShape struct {
	Kind    dxfKind
	Points  []gfx.Point // line, polyline
	Closed  bool
	Center  gfx.Point
	Radius  float64   // circle
	Major   gfx.Point // ellipse major axis end point (relative)
	Ratio   float64
	R, G, B uint8
}

// outlineCollector is a Device that keeps outlines.
type outlineCollector struct {
	height   float64 // viewport height in px (for the y flip)
	shapes   []dxfShape
	flatness float64
	lastPath *gfx.Path
	lastM    gfx.Matrix
	lastIdx  [2]int // shapes written for the last path
}

func newOutlineCollector(height float64) *outlineCollector {
	return &outlineCollector{height: height, flatness: flatnessMillimetres / millimetresPerPixel}
}

func colorOf(p gfx.Paint) (uint8, uint8, uint8) {
	if p.Gradient != nil {
		return 0, 0, 0
	}
	r, g, b, _ := p.Color.RGBA8()
	return r, g, b
}

func (c *outlineCollector) toDxf(p gfx.Point) gfx.Point {
	return gfx.Point{X: p.X * millimetresPerPixel, Y: (c.height - p.Y) * millimetresPerPixel}
}

func (c *outlineCollector) add(p *gfx.Path, m gfx.Matrix, paint gfx.Paint, stroke bool) {
	r, g, b := colorOf(paint)
	// A shape with fill and stroke is drawn twice: its outline is written once,
	// in the stroke's colour.
	if p == c.lastPath && m == c.lastM {
		if stroke {
			for i := c.lastIdx[0]; i < c.lastIdx[1]; i++ {
				c.shapes[i].R, c.shapes[i].G, c.shapes[i].B = r, g, b
			}
		}
		return
	}
	start := len(c.shapes)
	if p.Shape != nil {
		c.addEllipse(p.Shape, m, r, g, b)
	} else {
		c.addOutlines(p, m, r, g, b)
	}
	c.lastPath, c.lastM, c.lastIdx = p, m, [2]int{start, len(c.shapes)}
}

// addEllipse writes an axis-aligned ellipse seen through m: its image is an
// ellipse whose axes come from the singular values of the mapped radii.
func (c *outlineCollector) addEllipse(e *gfx.EllipseHint, m gfx.Matrix, r, g, b uint8) {
	if e.RX <= 0 || e.RY <= 0 {
		return
	}
	center := m.Apply(gfx.Point{X: e.CX, Y: e.CY})
	ux, uy := m.A*e.RX, m.B*e.RX
	vx, vy := m.C*e.RY, m.D*e.RY
	a, bb, cc := ux*ux+vx*vx, ux*uy+vx*vy, uy*uy+vy*vy
	root := math.Sqrt((a-cc)*(a-cc)/4 + bb*bb)
	major := math.Sqrt(math.Max(0, (a+cc)/2+root))
	minor := math.Sqrt(math.Max(0, (a+cc)/2-root))
	if major <= 1e-9 {
		return
	}
	centre := c.toDxf(center)
	if minor/major > 1-1e-6 {
		c.shapes = append(c.shapes, dxfShape{Kind: dxfCircle, Center: centre, Radius: major * millimetresPerPixel, R: r, G: g, B: b})
		return
	}
	angle := 0.5 * math.Atan2(2*bb, a-cc)
	// The y flip mirrors the axis direction.
	axis := gfx.Point{X: math.Cos(angle) * major * millimetresPerPixel, Y: -math.Sin(angle) * major * millimetresPerPixel}
	c.shapes = append(c.shapes, dxfShape{Kind: dxfEllipse, Center: centre, Major: axis, Ratio: minor / major, R: r, G: g, B: b})
}

func (c *outlineCollector) pieces(deviation float64) int {
	n := int(math.Ceil(math.Sqrt(deviation / c.flatness)))
	return max(1, min(maxPiecesPerCurve, n))
}

func dist(x, y float64) float64 { return math.Sqrt(x*x + y*y) }

func (c *outlineCollector) addOutlines(path *gfx.Path, m gfx.Matrix, r, g, b uint8) {
	p := path.Transformed(m)
	var cur []gfx.Point
	var pos, start gfx.Point
	p.Walk(func(op gfx.Op, pts []gfx.Point) {
		switch op {
		case gfx.MoveTo:
			c.flush(cur, false, r, g, b)
			cur = []gfx.Point{pts[0]}
			pos, start = pts[0], pts[0]
		case gfx.LineTo:
			cur = append(cur, pts[0])
			pos = pts[0]
		case gfx.QuadTo:
			p0, p1, p2 := pos, pts[0], pts[1]
			bend := dist(p0.X-2*p1.X+p2.X, p0.Y-2*p1.Y+p2.Y)
			n := c.pieces(bend / 4)
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				mt := 1 - t
				cur = append(cur, gfx.Point{X: mt*mt*p0.X + 2*mt*t*p1.X + t*t*p2.X, Y: mt*mt*p0.Y + 2*mt*t*p1.Y + t*t*p2.Y})
			}
			pos = p2
		case gfx.CubeTo:
			p0, p1, p2, p3 := pos, pts[0], pts[1], pts[2]
			// A chord of a curve in n equal parameter steps is off by at most
			// 1/8 · max|B''| / n², and max|B''| ≤ 6 · the largest second difference.
			bend := math.Max(dist(p0.X-2*p1.X+p2.X, p0.Y-2*p1.Y+p2.Y), dist(p1.X-2*p2.X+p3.X, p1.Y-2*p2.Y+p3.Y))
			n := c.pieces(bend * 0.75)
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				mt := 1 - t
				b0, b1, b2, b3 := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
				cur = append(cur, gfx.Point{X: b0*p0.X + b1*p1.X + b2*p2.X + b3*p3.X, Y: b0*p0.Y + b1*p1.Y + b2*p2.Y + b3*p3.Y})
			}
			pos = p3
		case gfx.Close:
			c.flush(cur, true, r, g, b)
			cur = []gfx.Point{start}
			pos = start
		}
	})
	c.flush(cur, false, r, g, b)
}

func near(a, b gfx.Point) bool { return math.Abs(a.X-b.X) < 1e-6 && math.Abs(a.Y-b.Y) < 1e-6 }

func (c *outlineCollector) flush(points []gfx.Point, closed bool, r, g, b uint8) {
	// Repeated points (zero-length pieces) mean nothing in CAD.
	clean := make([]gfx.Point, 0, len(points))
	for _, p := range points {
		q := c.toDxf(p)
		if len(clean) == 0 || !near(clean[len(clean)-1], q) {
			clean = append(clean, q)
		}
	}
	if closed && len(clean) > 2 && near(clean[0], clean[len(clean)-1]) {
		clean = clean[:len(clean)-1]
	}
	if len(clean) < 2 {
		return
	}
	if len(clean) == 2 && !closed {
		c.shapes = append(c.shapes, dxfShape{Kind: dxfLine, Points: clean, R: r, G: g, B: b})
		return
	}
	c.shapes = append(c.shapes, dxfShape{Kind: dxfPolyline, Points: clean, Closed: closed, R: r, G: g, B: b})
}

func (c *outlineCollector) Fill(p *gfx.Path, m gfx.Matrix, rule gfx.FillRule, paint gfx.Paint) {
	c.add(p, m, paint, false)
}

func (c *outlineCollector) Stroke(p *gfx.Path, m gfx.Matrix, s *gfx.StrokeStyle, paint gfx.Paint) {
	c.add(p, m, paint, true)
}

// Images, clips, groups and masks have no outline to write.
func (c *outlineCollector) DrawImage(img image.Image, m gfx.Matrix, opacity float64) {}
func (c *outlineCollector) PushClip(p *gfx.Path, m gfx.Matrix, rule gfx.FillRule) {}
func (c *outlineCollector) PopClip()                                             {}
func (c *outlineCollector) PushGroup(opacity float64)                            {}
func (c *outlineCollector) PopGroup()                                            {}
func (c *outlineCollector) PushMask(kind gfx.MaskKind, opacity float64)          {}
func (c *outlineCollector) BeginMaskContent()                                    {}
func (c *outlineCollector) PopMask()                                             {}
