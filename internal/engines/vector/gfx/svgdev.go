package gfx

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"strconv"
	"strings"
)

// SVGWriter is a Device that writes an SVG document (user units = device units).
type SVGWriter struct {
	W, H  float64
	defs  bytes.Buffer
	body  []*bytes.Buffer
	seq   int
	masks []svgMask
}

type svgMask struct {
	id      string
	opacity float64
	kind    MaskKind
	inMask  bool
}

// NewSVGWriter starts a w × h document.
func NewSVGWriter(w, h float64) *SVGWriter {
	return &SVGWriter{W: w, H: h, body: []*bytes.Buffer{{}}}
}

func (s *SVGWriter) out() *bytes.Buffer { return s.body[len(s.body)-1] }

func (s *SVGWriter) id(prefix string) string {
	s.seq++
	return prefix + strconv.Itoa(s.seq)
}

func svgPathData(p *Path, m Matrix) string {
	var b strings.Builder
	pt := func(q Point) {
		q = m.Apply(q)
		b.WriteString(num(q.X))
		b.WriteByte(' ')
		b.WriteString(num(q.Y))
	}
	p.Walk(func(op Op, pts []Point) {
		switch op {
		case MoveTo:
			b.WriteByte('M')
			pt(pts[0])
		case LineTo:
			b.WriteByte('L')
			pt(pts[0])
		case QuadTo:
			b.WriteByte('Q')
			pt(pts[0])
			b.WriteByte(' ')
			pt(pts[1])
		case CubeTo:
			b.WriteByte('C')
			pt(pts[0])
			b.WriteByte(' ')
			pt(pts[1])
			b.WriteByte(' ')
			pt(pts[2])
		case Close:
			b.WriteByte('Z')
		}
	})
	return b.String()
}

func svgColor(c Color) string {
	r, g, b, _ := c.RGBA8()
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func svgMatrix(m Matrix) string {
	return "matrix(" + num(m.A) + " " + num(m.B) + " " + num(m.C) + " " + num(m.D) + " " + num(m.E) + " " + num(m.F) + ")"
}

// paint returns the attribute value and the opacity attribute for a paint.
func (s *SVGWriter) paint(p Paint, m Matrix, what string) string {
	if p.Gradient == nil {
		attr := what + `="` + svgColor(p.Color) + `"`
		if p.Color.A < 1 {
			attr += " " + what + `-opacity="` + num(p.Color.A) + `"`
		}
		return attr
	}
	g := p.Gradient
	id := s.id("g")
	spread := ""
	switch g.Spread {
	case SpreadReflect:
		spread = ` spreadMethod="reflect"`
	case SpreadRepeat:
		spread = ` spreadMethod="repeat"`
	}
	tf := svgMatrix(m.Mul(g.Matrix))
	if g.Radial {
		fmt.Fprintf(&s.defs, `<radialGradient id="%s" gradientUnits="userSpaceOnUse" cx="%s" cy="%s" r="%s" fx="%s" fy="%s" fr="%s" gradientTransform="%s"%s>`,
			id, num(g.CX), num(g.CY), num(g.R), num(g.FX), num(g.FY), num(g.FR), tf, spread)
	} else {
		fmt.Fprintf(&s.defs, `<linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s" gradientTransform="%s"%s>`,
			id, num(g.X1), num(g.Y1), num(g.X2), num(g.Y2), tf, spread)
	}
	for _, st := range g.Stops {
		fmt.Fprintf(&s.defs, `<stop offset="%s" stop-color="%s"`, num(st.Offset), svgColor(st.Color))
		if st.Color.A < 1 {
			fmt.Fprintf(&s.defs, ` stop-opacity="%s"`, num(st.Color.A))
		}
		s.defs.WriteString("/>")
	}
	if g.Radial {
		s.defs.WriteString("</radialGradient>")
	} else {
		s.defs.WriteString("</linearGradient>")
	}
	return what + `="url(#` + id + `)"`
}

func (s *SVGWriter) Fill(p *Path, m Matrix, rule FillRule, paint Paint) {
	if !paint.Visible() || p.Empty() {
		return
	}
	attr := s.paint(paint, m, "fill")
	if rule == EvenOdd {
		attr += ` fill-rule="evenodd"`
	}
	fmt.Fprintf(s.out(), `<path d="%s" %s/>`+"\n", svgPathData(p, m), attr)
}

func (s *SVGWriter) Stroke(p *Path, m Matrix, st *StrokeStyle, paint Paint) {
	if !paint.Visible() || p.Empty() || !(st.Width > 0) {
		return
	}
	attr := `fill="none" ` + s.paint(paint, Identity, "stroke")
	attr += ` stroke-width="` + num(st.Width) + `"`
	switch st.Cap {
	case RoundCap:
		attr += ` stroke-linecap="round"`
	case SquareCap:
		attr += ` stroke-linecap="square"`
	}
	switch st.Join {
	case RoundJoin:
		attr += ` stroke-linejoin="round"`
	case BevelJoin:
		attr += ` stroke-linejoin="bevel"`
	}
	if st.MiterLimit >= 1 && st.MiterLimit != 4 {
		attr += ` stroke-miterlimit="` + num(st.MiterLimit) + `"`
	}
	if len(st.Dashes) > 0 {
		parts := make([]string, len(st.Dashes))
		for i, d := range st.Dashes {
			parts[i] = num(d)
		}
		attr += ` stroke-dasharray="` + strings.Join(parts, " ") + `"`
		if st.DashOffset != 0 {
			attr += ` stroke-dashoffset="` + num(st.DashOffset) + `"`
		}
	}
	if m.IsIdentity() {
		fmt.Fprintf(s.out(), `<path d="%s" %s/>`+"\n", svgPathData(p, Identity), attr)
		return
	}
	fmt.Fprintf(s.out(), `<path transform="%s" d="%s" %s/>`+"\n", svgMatrix(m), svgPathData(p, Identity), attr)
}

func (s *SVGWriter) DrawImage(img image.Image, m Matrix, opacity float64) {
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		return
	}
	bd := img.Bounds()
	op := ""
	if opacity < 1 {
		op = ` opacity="` + num(opacity) + `"`
	}
	fmt.Fprintf(s.out(), `<image transform="%s" width="%d" height="%d"%s href="data:image/png;base64,%s"/>`+"\n",
		svgMatrix(m), bd.Dx(), bd.Dy(), op, base64.StdEncoding.EncodeToString(b.Bytes()))
}

func (s *SVGWriter) PushClip(p *Path, m Matrix, rule FillRule) {
	id := s.id("c")
	r := ""
	if rule == EvenOdd {
		r = ` clip-rule="evenodd"`
	}
	fmt.Fprintf(&s.defs, `<clipPath id="%s"><path d="%s"%s/></clipPath>`, id, svgPathData(p, m), r)
	fmt.Fprintf(s.out(), `<g clip-path="url(#%s)">`+"\n", id)
}

// PushClipShapes clips to the union of shapes.
func (s *SVGWriter) PushClipShapes(shapes []ClipShape) {
	id := s.id("c")
	fmt.Fprintf(&s.defs, `<clipPath id="%s">`, id)
	for _, sh := range shapes {
		r := ""
		if sh.Rule == EvenOdd {
			r = ` clip-rule="evenodd"`
		}
		fmt.Fprintf(&s.defs, `<path d="%s"%s/>`, svgPathData(sh.Path, sh.M), r)
	}
	s.defs.WriteString("</clipPath>")
	fmt.Fprintf(s.out(), `<g clip-path="url(#%s)">`+"\n", id)
}

func (s *SVGWriter) PopClip() { s.out().WriteString("</g>\n") }

func (s *SVGWriter) PushGroup(opacity float64) {
	if opacity < 1 {
		fmt.Fprintf(s.out(), `<g opacity="%s">`+"\n", num(opacity))
	} else {
		s.out().WriteString("<g>\n")
	}
}

// PushBlendGroup starts a group composited with a blend mode.
func (s *SVGWriter) PushBlendGroup(opacity float64, mode BlendMode) {
	fmt.Fprintf(s.out(), `<g opacity="%s" style="mix-blend-mode:%s">`+"\n", num(opacity), mode)
}

func (s *SVGWriter) PopGroup() { s.out().WriteString("</g>\n") }

func (s *SVGWriter) PushMask(kind MaskKind, opacity float64) {
	s.masks = append(s.masks, svgMask{id: s.id("m"), opacity: opacity, kind: kind})
	s.body = append(s.body, &bytes.Buffer{})
}

func (s *SVGWriter) BeginMaskContent() {
	if len(s.masks) == 0 {
		return
	}
	s.masks[len(s.masks)-1].inMask = true
	s.body = append(s.body, &bytes.Buffer{})
}

func (s *SVGWriter) PopMask() {
	if len(s.masks) == 0 {
		return
	}
	mk := s.masks[len(s.masks)-1]
	s.masks = s.masks[:len(s.masks)-1]
	var mask *bytes.Buffer
	if mk.inMask {
		mask = s.body[len(s.body)-1]
		s.body = s.body[:len(s.body)-1]
	}
	content := s.body[len(s.body)-1]
	s.body = s.body[:len(s.body)-1]
	if mask == nil {
		return
	}
	mt := ""
	if mk.kind == MaskAlpha {
		mt = ` mask-type="alpha"`
	}
	fmt.Fprintf(&s.defs, `<mask id="%s" maskUnits="userSpaceOnUse" x="0" y="0" width="%s" height="%s"%s>%s</mask>`,
		mk.id, num(s.W), num(s.H), mt, mask.String())
	op := ""
	if mk.opacity < 1 {
		op = ` opacity="` + num(mk.opacity) + `"`
	}
	fmt.Fprintf(s.out(), `<g mask="url(#%s)"%s>%s</g>`+"\n", mk.id, op, content.String())
}

// Write writes the document.
func (s *SVGWriter) Write(w io.Writer) error {
	for len(s.masks) > 0 {
		s.PopMask()
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" version="1.1" width="%s" height="%s" viewBox="0 0 %s %s">`+"\n",
		num(s.W), num(s.H), num(s.W), num(s.H))
	if s.defs.Len() > 0 {
		b.WriteString("<defs>")
		b.Write(s.defs.Bytes())
		b.WriteString("</defs>\n")
	}
	b.Write(s.body[0].Bytes())
	b.WriteString("</svg>\n")
	_, err := w.Write(b.Bytes())
	return err
}
