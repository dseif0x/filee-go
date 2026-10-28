package svg

import (
	"bytes"
	"compress/gzip"
	"io"
	"math"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Gunzip decompresses gzip data (SVGZ).
func Gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	// A gzip bomb is not a drawing.
	return io.ReadAll(io.LimitReader(zr, 512<<20))
}

// IsGzip reports the gzip magic number.
func IsGzip(data []byte) bool { return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b }

// ReadFile reads an SVG or SVGZ file as SVG text.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsGzip(data) {
		return Gunzip(data)
	}
	return data, nil
}

type vertex struct {
	p         gfx.Point
	in, out   gfx.Point // directions (zero when none)
	subStart  bool
	subEnd    bool
}

// vertices lists the path's vertices with their incoming and outgoing directions.
func vertices(p *gfx.Path) []vertex {
	var vs []vertex
	var cur, start gfx.Point
	startIdx := -1
	p.Walk(func(op gfx.Op, pts []gfx.Point) {
		switch op {
		case gfx.MoveTo:
			cur, start = pts[0], pts[0]
			vs = append(vs, vertex{p: cur, subStart: true})
			startIdx = len(vs) - 1
		case gfx.LineTo, gfx.QuadTo, gfx.CubeTo:
			end := pts[len(pts)-1]
			first := pts[0]
			last := cur
			if len(pts) > 1 {
				last = pts[len(pts)-2]
			}
			outDir := first.Sub(cur)
			if outDir.Len() < 1e-12 && len(pts) > 1 {
				outDir = pts[1].Sub(cur)
			}
			if outDir.Len() < 1e-12 {
				outDir = end.Sub(cur)
			}
			inDir := end.Sub(last)
			if inDir.Len() < 1e-12 {
				inDir = end.Sub(cur)
			}
			if n := len(vs); n > 0 {
				vs[n-1].out = outDir.Norm()
			}
			vs = append(vs, vertex{p: end, in: inDir.Norm()})
			cur = end
		case gfx.Close:
			// The closing segment, then the start vertex joins both ends.
			if cur.Sub(start).Len() > 1e-12 {
				d := start.Sub(cur).Norm()
				vs[len(vs)-1].out = d
				vs = append(vs, vertex{p: start, in: d})
			}
			if startIdx >= 0 && startIdx < len(vs) {
				last := &vs[len(vs)-1]
				last.out = vs[startIdx].out
				vs[startIdx].in = last.in
			}
			vs[len(vs)-1].subEnd = true
			cur = start
		}
	})
	return vs
}

func angleOf(d gfx.Point) float64 { return math.Atan2(d.Y, d.X) }

func bisector(in, out gfx.Point) float64 {
	switch {
	case in == gfx.Point{} && out == gfx.Point{}:
		return 0
	case in == gfx.Point{}:
		return angleOf(out)
	case out == gfx.Point{}:
		return angleOf(in)
	}
	a, b := angleOf(in), angleOf(out)
	d := b - a
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d < -math.Pi {
		d += 2 * math.Pi
	}
	return a + d/2
}

func (r *renderer) drawMarkers(p *gfx.Path, c context) {
	st := c.st
	if st.markerStart == "" && st.markerMid == "" && st.markerEnd == "" {
		return
	}
	vs := vertices(p)
	if len(vs) == 0 {
		return
	}
	sw := c.lc.resolve(st.strokeWidth, axisOther)
	fill, _, okF := r.resolvePaint(st.fill, st.fillOpacity, c, p.Bounds())
	stroke, _, okS := r.resolvePaint(st.stroke, st.strokeOpacity, c, p.Bounds())
	var fp, sp *gfx.Paint
	if okF {
		fp = &fill
	}
	if okS {
		sp = &stroke
	}
	for i, v := range vs {
		id := st.markerMid
		start := i == 0
		end := i == len(vs)-1
		if start {
			id = st.markerStart
		} else if end {
			id = st.markerEnd
		}
		if id == "" {
			continue
		}
		mk := r.doc.ids[id]
		if mk == nil || mk.Name != "marker" {
			continue
		}
		angle := bisector(v.in, v.out)
		orient := strings.TrimSpace(mk.Attr("orient"))
		switch orient {
		case "auto":
		case "auto-start-reverse":
			if start {
				angle += math.Pi
			}
		default:
			a := parseFloat(strings.TrimSuffix(orient, "deg"), 0)
			if strings.HasSuffix(orient, "rad") {
				a = parseFloat(strings.TrimSuffix(orient, "rad"), 0) * 180 / math.Pi
			} else if strings.HasSuffix(orient, "turn") {
				a = parseFloat(strings.TrimSuffix(orient, "turn"), 0) * 360
			}
			angle = a * math.Pi / 180
		}
		r.drawMarker(mk, v.p, angle, sw, c, fp, sp)
	}
}

func (r *renderer) drawMarker(mk *Node, at gfx.Point, angle, strokeWidth float64, c context, fill, stroke *gfx.Paint) {
	if !r.enter(mk) {
		return
	}
	defer r.leave(mk)
	mst := computeStyle(mk, defaultStyle(), c.lc)
	lc := c.lc
	mw := lc.resolveAttr(mk, "markerWidth", axisX, 3)
	mh := lc.resolveAttr(mk, "markerHeight", axisY, 3)
	if mw <= 0 || mh <= 0 {
		return
	}
	m := c.m.Mul(gfx.Translate(at.X, at.Y)).Mul(gfx.Rotate(angle))
	if mk.Attr("markerUnits") != "userSpaceOnUse" {
		m = m.Mul(gfx.Scale(strokeWidth, strokeWidth))
	}
	refX := parseFloat(mk.Attr("refX"), 0)
	refY := parseFloat(mk.Attr("refY"), 0)
	var content gfx.Matrix
	vb, hasVB := parseViewBox(mk.Attr("viewBox"))
	if hasVB {
		content = viewBoxTransform(vb, mk.Attr("preserveAspectRatio"), 0, 0, mw, mh)
		// refX/refY are in the viewBox's coordinates.
		ref := content.Apply(gfx.Point{X: refX, Y: refY})
		m = m.Mul(gfx.Translate(-ref.X, -ref.Y))
	} else {
		content = gfx.Identity
		m = m.Mul(gfx.Translate(-refX, -refY))
	}
	if !mst.overflowVisible {
		clip := gfx.NewPath()
		if hasVB {
			clip.Rect(vb[0], vb[1], vb[2], vb[3])
			r.dev.PushClip(clip, m.Mul(content), gfx.NonZero)
		} else {
			clip.Rect(0, 0, mw, mh)
			r.dev.PushClip(clip, m, gfx.NonZero)
		}
		defer r.dev.PopClip()
	}
	cc := context{m: m.Mul(content), st: mst, lc: lc, ctxFill: fill, ctxStroke: stroke}
	if hasVB {
		cc.lc.vw, cc.lc.vh = vb[2], vb[3]
	}
	r.drawChildren(mk, cc)
}
