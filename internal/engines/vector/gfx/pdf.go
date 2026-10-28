package gfx

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"fmt"
	"image"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// PDF writes vector PDF documents: pages are Devices whose coordinates are
// points with the origin at the top-left corner and y pointing down.
type PDF struct {
	objs      [][]byte // object n is objs[n-1]
	pages     []int
	pagesRef  int
	resRef    int
	extG      map[string]int // ExtGState dictionary → object
	extGNames map[int]string
	patterns  map[int]string
	xobjects  map[int]string
	images    map[image.Image]int
	shapes    map[shapeKey]int
	nameSeq   int
}

type shapeKey struct {
	path *Path
	rule FillRule
}

// PDFInfo is the document information.
type PDFInfo struct {
	Title, Creator, Producer string
	Created                  time.Time
	// PdfA writes PDF/A-2b metadata and an sRGB output intent.
	PdfA bool
}

// NewPDF starts a document.
func NewPDF() *PDF {
	d := &PDF{extG: map[string]int{}, extGNames: map[int]string{}, patterns: map[int]string{}, xobjects: map[int]string{}, images: map[image.Image]int{}, shapes: map[shapeKey]int{}}
	d.pagesRef = d.reserve()
	d.resRef = d.reserve()
	return d
}

func (d *PDF) reserve() int {
	d.objs = append(d.objs, nil)
	return len(d.objs)
}

func (d *PDF) set(ref int, body []byte) { d.objs[ref-1] = body }

func (d *PDF) add(body []byte) int {
	d.objs = append(d.objs, body)
	return len(d.objs)
}

func (d *PDF) stream(dict string, data []byte, compress bool) []byte {
	if compress && len(data) > 64 {
		var b bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
		zw.Write(data)
		zw.Close()
		if b.Len() < len(data) {
			data = b.Bytes()
			dict += "/Filter/FlateDecode"
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "<<%s/Length %d>>stream\n", dict, len(data))
	out.Write(data)
	out.WriteString("\nendstream")
	return out.Bytes()
}

func (d *PDF) name(prefix string) string {
	d.nameSeq++
	return prefix + strconv.Itoa(d.nameSeq)
}

func (d *PDF) gstate(dict string) string {
	ref, ok := d.extG[dict]
	if !ok {
		ref = d.add([]byte("<</Type/ExtGState" + dict + ">>"))
		d.extG[dict] = ref
		d.extGNames[ref] = d.name("G")
	}
	return d.extGNames[ref]
}

func (d *PDF) alphaState(fill, stroke float64) string {
	return d.gstate("/ca " + num(fill) + "/CA " + num(stroke))
}

// Page adds a page of w × h points and returns its drawing surface.
func (d *PDF) Page(w, h float64) *PDFPage {
	p := &PDFPage{doc: d, w: w, h: h, base: Matrix{A: 1, D: -1, F: h}}
	p.stack = []*pdfContent{{}}
	return p
}

// PDFPage draws one page. Call Finish when done.
type PDFPage struct {
	doc   *PDF
	w, h  float64
	base  Matrix // device → default space of the page
	stack []*pdfContent
}

type pdfContent struct {
	blend    BlendMode
	buf      bytes.Buffer
	isMask   bool
	mask     bool
	kind     MaskKind
	opacity  float64
	maskForm int
}

func (p *PDFPage) out() *bytes.Buffer { return &p.stack[len(p.stack)-1].buf }

// Finish writes the page.
func (p *PDFPage) Finish() {
	for len(p.stack) > 1 {
		p.PopGroup()
	}
	var content bytes.Buffer
	// Device space: y down from the top-left corner.
	fmt.Fprintf(&content, "1 0 0 -1 0 %s cm\n", num(p.h))
	content.Write(p.stack[0].buf.Bytes())
	cref := p.doc.add(p.doc.stream("", content.Bytes(), true))
	ref := p.doc.add([]byte(fmt.Sprintf("<</Type/Page/Parent %d 0 R/MediaBox[0 0 %s %s]/Resources %d 0 R/Contents %d 0 R/Group<</S/Transparency/CS/DeviceRGB>>>>",
		p.doc.pagesRef, num(p.w), num(p.h), p.doc.resRef, cref)))
	p.doc.pages = append(p.doc.pages, ref)
}

func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if math.Abs(v) < 1e-9 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func matrixOp(m Matrix) string {
	return num(m.A) + " " + num(m.B) + " " + num(m.C) + " " + num(m.D) + " " + num(m.E) + " " + num(m.F) + " cm\n"
}

// writePath appends path operators; points are mapped by m.
func writePath(b *bytes.Buffer, p *Path, m Matrix) {
	var cur Point
	pt := func(q Point) string {
		q = m.Apply(q)
		return num(q.X) + " " + num(q.Y)
	}
	p.Walk(func(op Op, pts []Point) {
		switch op {
		case MoveTo:
			b.WriteString(pt(pts[0]) + " m\n")
			cur = pts[0]
		case LineTo:
			b.WriteString(pt(pts[0]) + " l\n")
			cur = pts[0]
		case QuadTo:
			c1 := cur.Add(pts[0].Sub(cur).Mul(2.0 / 3))
			c2 := pts[1].Add(pts[0].Sub(pts[1]).Mul(2.0 / 3))
			b.WriteString(pt(c1) + " " + pt(c2) + " " + pt(pts[1]) + " c\n")
			cur = pts[1]
		case CubeTo:
			b.WriteString(pt(pts[0]) + " " + pt(pts[1]) + " " + pt(pts[2]) + " c\n")
			cur = pts[2]
		case Close:
			b.WriteString("h\n")
		}
	})
}

func colorOp(c Color, stroke bool) string {
	op := " rg\n"
	if stroke {
		op = " RG\n"
	}
	return num(clamp01(c.R)) + " " + num(clamp01(c.G)) + " " + num(clamp01(c.B)) + op
}

func (p *PDFPage) Fill(path *Path, m Matrix, rule FillRule, paint Paint) {
	if !paint.Visible() || path.Empty() || !m.Finite() {
		return
	}
	op := "f\n"
	if rule == EvenOdd {
		op = "f*\n"
	}
	if paint.Gradient != nil {
		p.gradient(path, m, rule, paint.Gradient)
		return
	}
	b := p.out()
	b.WriteString("q\n")
	if paint.Color.A < 1 {
		b.WriteString("/" + p.doc.alphaState(paint.Color.A, 1) + " gs\n")
	}
	b.WriteString(colorOp(paint.Color, false))
	if path.Reusable {
		// Stored once as a form that takes the colour set here.
		key := shapeKey{path, rule}
		ref, ok := p.doc.shapes[key]
		if !ok {
			var fb bytes.Buffer
			writePath(&fb, path, Identity)
			fb.WriteString(op)
			bb := path.Bounds()
			ref = p.doc.add(p.doc.stream(fmt.Sprintf("/Type/XObject/Subtype/Form/BBox[%s %s %s %s]/Resources %d 0 R",
				num(bb.X0-1), num(bb.Y0-1), num(bb.X1+1), num(bb.Y1+1), p.doc.resRef), fb.Bytes(), true))
			p.doc.xobjects[ref] = p.doc.name("S")
			p.doc.shapes[key] = ref
		}
		b.WriteString(matrixOp(m))
		b.WriteString("/" + p.doc.xobjects[ref] + " Do\nQ\n")
		return
	}
	writePath(b, path, m)
	b.WriteString(op + "Q\n")
}

func (p *PDFPage) Stroke(path *Path, m Matrix, s *StrokeStyle, paint Paint) {
	if !paint.Visible() || path.Empty() || !m.Finite() || !(s.Width > 0) {
		return
	}
	if paint.Gradient != nil {
		// The stroke outline filled with the gradient.
		outline := PathFromPolylines(StrokePath(path, m, s, 0.05))
		g := *paint.Gradient
		g.Matrix = m.Mul(g.Matrix)
		p.gradient(outline, Identity, NonZero, &g)
		return
	}
	b := p.out()
	b.WriteString("q\n")
	if paint.Color.A < 1 {
		b.WriteString("/" + p.doc.alphaState(1, paint.Color.A) + " gs\n")
	}
	b.WriteString(colorOp(paint.Color, true))
	b.WriteString(matrixOp(m))
	b.WriteString(num(s.Width) + " w " + strconv.Itoa(int(s.Cap)) + " J ")
	join := 0
	switch s.Join {
	case RoundJoin:
		join = 1
	case BevelJoin:
		join = 2
	}
	b.WriteString(strconv.Itoa(join) + " j ")
	if s.MiterLimit >= 1 {
		b.WriteString(num(s.MiterLimit) + " M ")
	}
	if len(s.Dashes) > 0 {
		b.WriteString("[")
		for i, v := range s.Dashes {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(num(math.Abs(v)))
		}
		b.WriteString("] " + num(s.DashOffset) + " d")
	}
	b.WriteString("\n")
	writePath(b, path, Identity)
	b.WriteString("S\nQ\n")
}

// gradient fills path (mapped by m) with g (gradient space mapped by m·g.Matrix).
func (p *PDFPage) gradient(path *Path, m Matrix, rule FillRule, g *Gradient) {
	gm := m.Mul(g.Matrix)
	inv, ok := gm.Invert()
	if !ok {
		return
	}
	// The range of t the shape needs, for repeating gradients.
	box := path.Transformed(m).Bounds().Transform(inv)
	t0, t1 := 0.0, 1.0
	if g.Spread != SpreadPad && !box.Empty() {
		lo, hi := math.Inf(1), math.Inf(-1)
		var rp radialPrep
		if g.Radial {
			rp = prepRadial(g)
		}
		corners := []Point{{box.X0, box.Y0}, {box.X1, box.Y0}, {box.X0, box.Y1}, {box.X1, box.Y1}}
		for _, c := range corners {
			if t, ok := g.param(c, &rp); ok {
				lo, hi = math.Min(lo, t), math.Max(hi, t)
			}
		}
		if g.Radial {
			lo = 0
			// The far corner may lie beyond what the corners' t shows for focal gradients.
			r := math.Max(g.R, 1e-9)
			for _, c := range corners {
				hi = math.Max(hi, math.Hypot(c.X-g.CX, c.Y-g.CY)/r+1)
			}
		}
		if lo < hi {
			t0, t1 = math.Floor(lo), math.Ceil(hi)
			if t1-t0 > 200 {
				t1 = t0 + 200
			}
		}
	}
	fn := p.doc.function(g, t0, t1, false)
	var coords string
	if g.Radial {
		f := func(t float64) (float64, float64, float64) {
			return g.FX + (g.CX-g.FX)*t, g.FY + (g.CY-g.FY)*t, math.Max(0, g.FR+(g.R-g.FR)*t)
		}
		x0, y0, r0 := f(t0)
		x1, y1, r1 := f(t1)
		coords = fmt.Sprintf("/ShadingType 3/Coords[%s %s %s %s %s %s]", num(x0), num(y0), num(r0), num(x1), num(y1), num(r1))
	} else {
		pa := Point{g.X1, g.Y1}.Lerp(Point{g.X2, g.Y2}, t0)
		pb := Point{g.X1, g.Y1}.Lerp(Point{g.X2, g.Y2}, t1)
		coords = fmt.Sprintf("/ShadingType 2/Coords[%s %s %s %s]", num(pa.X), num(pa.Y), num(pb.X), num(pb.Y))
	}
	shading := fmt.Sprintf("<<%s/ColorSpace/DeviceRGB/Domain[%s %s]/Function %d 0 R/Extend[true true]>>", coords, num(t0), num(t1), fn)
	pm := p.patternMatrix().Mul(gm)
	pat := p.doc.add([]byte(fmt.Sprintf("<</Type/Pattern/PatternType 2/Shading %s/Matrix[%s %s %s %s %s %s]>>",
		shading, num(pm.A), num(pm.B), num(pm.C), num(pm.D), num(pm.E), num(pm.F))))
	name := p.doc.name("P")
	p.doc.patterns[pat] = name

	op := "f\n"
	if rule == EvenOdd {
		op = "f*\n"
	}
	alphaVaries, alpha := false, 1.0
	for i, s := range g.Stops {
		if i == 0 {
			alpha = s.Color.A
		} else if s.Color.A != alpha {
			alphaVaries = true
		}
	}
	b := p.out()
	b.WriteString("q\n")
	if alphaVaries {
		// The stops' opacity as a luminosity soft mask: the same gradient in gray.
		gfn := p.doc.function(g, t0, t1, true)
		gshade := strings.Replace(strings.Replace(shading, "/DeviceRGB", "/DeviceGray", 1), fmt.Sprintf("/Function %d 0 R", fn), fmt.Sprintf("/Function %d 0 R", gfn), 1)
		gpat := p.doc.add([]byte(fmt.Sprintf("<</Type/Pattern/PatternType 2/Shading %s/Matrix[%s %s %s %s %s %s]>>",
			gshade, num(pm.A), num(pm.B), num(pm.C), num(pm.D), num(pm.E), num(pm.F))))
		gname := p.doc.name("P")
		p.doc.patterns[gpat] = gname
		var mb bytes.Buffer
		mb.WriteString("/Pattern cs /" + gname + " scn\n")
		writePath(&mb, path, m)
		mb.WriteString(op)
		form := p.doc.form(mb.Bytes(), p.w, p.h, true)
		b.WriteString("/" + p.doc.gstate(fmt.Sprintf("/SMask<</Type/Mask/S/Luminosity/G %d 0 R>>", form)) + " gs\n")
	} else if alpha < 1 {
		b.WriteString("/" + p.doc.alphaState(alpha, alpha) + " gs\n")
	}
	b.WriteString("/Pattern cs /" + name + " scn\n")
	writePath(b, path, m)
	b.WriteString(op + "Q\n")
}

// patternMatrix maps device space to the default space of the current content
// (page: flipped; forms are drawn in device space).
func (p *PDFPage) patternMatrix() Matrix {
	if len(p.stack) == 1 {
		return p.base
	}
	return Identity
}

// function builds the colour function of g over t0–t1 (repeated or reflected
// copies for those spreads); gray gives the stops' alpha as gray levels.
func (d *PDF) function(g *Gradient, t0, t1 float64, gray bool) int {
	stops := g.Stops
	if len(stops) == 0 {
		stops = []Stop{{0, Transparent}, {1, Transparent}}
	}
	if len(stops) == 1 {
		stops = []Stop{stops[0], {1, stops[0].Color}}
	}
	col := func(c Color) string {
		if gray {
			return num(clamp01(c.A))
		}
		return num(clamp01(c.R)) + " " + num(clamp01(c.G)) + " " + num(clamp01(c.B))
	}
	// One period: stitched linear pieces between stops.
	var fns, bounds, encode []string
	type piece struct{ a, b Stop }
	var pieces []piece
	if stops[0].Offset > 0 {
		pieces = append(pieces, piece{Stop{0, stops[0].Color}, stops[0]})
	}
	for i := 1; i < len(stops); i++ {
		if stops[i].Offset > stops[i-1].Offset {
			pieces = append(pieces, piece{stops[i-1], stops[i]})
		}
	}
	if last := stops[len(stops)-1]; last.Offset < 1 {
		pieces = append(pieces, piece{last, Stop{1, last.Color}})
	}
	if len(pieces) == 0 {
		pieces = append(pieces, piece{Stop{0, stops[len(stops)-1].Color}, Stop{1, stops[len(stops)-1].Color}})
	}
	pieceRef := make([]int, len(pieces))
	for i, pc := range pieces {
		pieceRef[i] = d.add([]byte(fmt.Sprintf("<</FunctionType 2/Domain[0 1]/C0[%s]/C1[%s]/N 1>>", col(pc.a.Color), col(pc.b.Color))))
	}
	periodFn := func() string {
		fns, bounds, encode = fns[:0], bounds[:0], encode[:0]
		for i, pc := range pieces {
			fns = append(fns, fmt.Sprintf("%d 0 R", pieceRef[i]))
			if i > 0 {
				bounds = append(bounds, num(pc.a.Offset))
			}
			encode = append(encode, "0 1")
		}
		return fmt.Sprintf("<</FunctionType 3/Domain[0 1]/Functions[%s]/Bounds[%s]/Encode[%s]>>",
			strings.Join(fns, " "), strings.Join(bounds, " "), strings.Join(encode, " "))
	}
	period := d.add([]byte(periodFn()))
	if t0 == 0 && t1 == 1 {
		return period
	}
	var outer, ob, oe []string
	for k := t0; k < t1; k++ {
		outer = append(outer, fmt.Sprintf("%d 0 R", period))
		if k > t0 {
			ob = append(ob, num(k))
		}
		if g.Spread == SpreadReflect && int(math.Abs(k))%2 == 1 {
			oe = append(oe, "1 0")
		} else {
			oe = append(oe, "0 1")
		}
	}
	return d.add([]byte(fmt.Sprintf("<</FunctionType 3/Domain[%s %s]/Functions[%s]/Bounds[%s]/Encode[%s]>>",
		num(t0), num(t1), strings.Join(outer, " "), strings.Join(ob, " "), strings.Join(oe, " "))))
}

// form adds a transparency group XObject in device space.
func (d *PDF) form(content []byte, w, h float64, isolated bool) int {
	dict := fmt.Sprintf("/Type/XObject/Subtype/Form/BBox[0 0 %s %s]/Resources %d 0 R/Group<</S/Transparency/CS/DeviceRGB", num(w), num(h), d.resRef)
	if isolated {
		dict += "/I true"
	}
	dict += ">>"
	ref := d.add(d.stream(dict, content, true))
	d.xobjects[ref] = d.name("X")
	return ref
}

func (p *PDFPage) DrawImage(img image.Image, m Matrix, opacity float64) {
	if img == nil || opacity <= 0 || !m.Finite() {
		return
	}
	b := img.Bounds()
	if b.Empty() {
		return
	}
	ref := p.doc.image(img)
	out := p.out()
	out.WriteString("q\n")
	if opacity < 1 {
		out.WriteString("/" + p.doc.alphaState(opacity, opacity) + " gs\n")
	}
	// The unit square, y up, onto the pixel rectangle, y down.
	out.WriteString(matrixOp(m.Mul(Matrix{A: float64(b.Dx()), D: -float64(b.Dy()), F: float64(b.Dy())})))
	out.WriteString("/" + p.doc.xobjects[ref] + " Do\nQ\n")
}

func (d *PDF) image(img image.Image) int {
	if ref, ok := d.images[img]; ok {
		return ref
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	opaque := true
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0 {
				rgb = append(rgb, 0, 0, 0)
			} else {
				rgb = append(rgb, uint8(r*0xffff/a>>8), uint8(g*0xffff/a>>8), uint8(bl*0xffff/a>>8))
			}
			alpha = append(alpha, uint8(a>>8))
			if a>>8 != 255 {
				opaque = false
			}
		}
	}
	dict := fmt.Sprintf("/Type/XObject/Subtype/Image/Width %d/Height %d/ColorSpace/DeviceRGB/BitsPerComponent 8", w, h)
	if !opaque {
		sm := d.add(d.stream(fmt.Sprintf("/Type/XObject/Subtype/Image/Width %d/Height %d/ColorSpace/DeviceGray/BitsPerComponent 8", w, h), alpha, true))
		dict += fmt.Sprintf("/SMask %d 0 R", sm)
	}
	ref := d.add(d.stream(dict, rgb, true))
	d.xobjects[ref] = d.name("I")
	d.images[img] = ref
	return ref
}

func (p *PDFPage) PushClip(path *Path, m Matrix, rule FillRule) {
	b := p.out()
	b.WriteString("q\n")
	if !m.Finite() || path.Empty() {
		// Nothing is inside an empty clip.
		b.WriteString("0 0 m h W n\n")
		return
	}
	writePath(b, path, m)
	if rule == EvenOdd {
		b.WriteString("W* n\n")
	} else {
		b.WriteString("W n\n")
	}
}

// PushClipShapes clips to the union of shapes: one clip path when the rules
// agree, else a soft mask of the shapes filled white (exact union).
func (p *PDFPage) PushClipShapes(shapes []ClipShape) {
	if len(shapes) == 0 {
		p.PushClip(NewPath(), Identity, NonZero)
		return
	}
	if len(shapes) == 1 {
		p.PushClip(shapes[0].Path, shapes[0].M, shapes[0].Rule)
		return
	}
	var mb bytes.Buffer
	mb.WriteString("1 g\n")
	for _, s := range shapes {
		if !s.M.Finite() || s.Path.Empty() {
			continue
		}
		writePath(&mb, s.Path, s.M)
		if s.Rule == EvenOdd {
			mb.WriteString("f*\n")
		} else {
			mb.WriteString("f\n")
		}
	}
	form := p.doc.form(mb.Bytes(), p.w, p.h, true)
	b := p.out()
	b.WriteString("q\n/" + p.doc.gstate(fmt.Sprintf("/SMask<</Type/Mask/S/Luminosity/G %d 0 R>>", form)) + " gs\n")
}

func (p *PDFPage) PopClip() { p.out().WriteString("Q\n") }

func (p *PDFPage) PushGroup(opacity float64) {
	p.stack = append(p.stack, &pdfContent{opacity: opacity})
}

// PushBlendGroup starts a group composited with a blend mode.
func (p *PDFPage) PushBlendGroup(opacity float64, mode BlendMode) {
	p.stack = append(p.stack, &pdfContent{opacity: opacity, blend: mode})
}

func (p *PDFPage) PopGroup() {
	if len(p.stack) < 2 {
		return
	}
	c := p.stack[len(p.stack)-1]
	if c.isMask {
		p.PopMask()
		return
	}
	p.stack = p.stack[:len(p.stack)-1]
	if c.mask {
		p.finishMasked(c)
		return
	}
	if c.buf.Len() == 0 {
		return
	}
	form := p.doc.form(c.buf.Bytes(), p.w, p.h, true)
	b := p.out()
	b.WriteString("q\n")
	if c.opacity < 1 {
		b.WriteString("/" + p.doc.alphaState(c.opacity, c.opacity) + " gs\n")
	}
	if c.blend != BlendNormal {
		b.WriteString("/" + p.doc.gstate("/BM/"+pdfBlendNames[c.blend]) + " gs\n")
	}
	b.WriteString("/" + p.doc.xobjects[form] + " Do\nQ\n")
}

func (p *PDFPage) PushMask(kind MaskKind, opacity float64) {
	p.stack = append(p.stack, &pdfContent{mask: true, kind: kind, opacity: opacity})
}

func (p *PDFPage) BeginMaskContent() {
	p.stack = append(p.stack, &pdfContent{isMask: true})
}

func (p *PDFPage) PopMask() {
	if len(p.stack) < 2 {
		return
	}
	var maskForm int
	if top := p.stack[len(p.stack)-1]; top.isMask {
		p.stack = p.stack[:len(p.stack)-1]
		maskForm = p.doc.form(top.buf.Bytes(), p.w, p.h, true)
	}
	if len(p.stack) < 2 {
		return
	}
	c := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	c.maskForm = maskForm
	p.finishMasked(c)
}

func (p *PDFPage) finishMasked(c *pdfContent) {
	if c.maskForm == 0 || c.buf.Len() == 0 {
		return
	}
	form := p.doc.form(c.buf.Bytes(), p.w, p.h, true)
	s := "Luminosity"
	if c.kind == MaskAlpha {
		s = "Alpha"
	}
	b := p.out()
	b.WriteString("q\n/" + p.doc.gstate(fmt.Sprintf("/SMask<</Type/Mask/S/%s/G %d 0 R>>", s, c.maskForm)) + " gs\n")
	if c.opacity < 1 {
		b.WriteString("/" + p.doc.alphaState(c.opacity, c.opacity) + " gs\n")
	}
	b.WriteString("/" + p.doc.xobjects[form] + " Do\nQ\n")
}

// Write finishes the document.
func (d *PDF) Write(w io.Writer, info PDFInfo) error {
	// Resources shared by every page and form.
	var res bytes.Buffer
	res.WriteString("<</ProcSet[/PDF/ImageC/ImageB]")
	writeNames := func(key string, m map[int]string) {
		if len(m) == 0 {
			return
		}
		res.WriteString("/" + key + "<<")
		refs := make([]int, 0, len(m))
		for ref := range m {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			fmt.Fprintf(&res, "/%s %d 0 R", m[ref], ref)
		}
		res.WriteString(">>")
	}
	writeNames("ExtGState", d.extGNames)
	writeNames("Pattern", d.patterns)
	writeNames("XObject", d.xobjects)
	res.WriteString(">>")
	d.set(d.resRef, res.Bytes())

	kids := make([]string, len(d.pages))
	for i, p := range d.pages {
		kids[i] = fmt.Sprintf("%d 0 R", p)
	}
	d.set(d.pagesRef, []byte(fmt.Sprintf("<</Type/Pages/Kids[%s]/Count %d>>", strings.Join(kids, " "), len(d.pages))))

	created := info.Created
	if created.IsZero() {
		created = time.Now()
	}
	pdfDate := created.Format("D:20060102150405") + tzSuffix(created)
	infoRef := d.add([]byte(fmt.Sprintf("<</Title %s/Creator %s/Producer %s/CreationDate (%s)/ModDate (%s)>>",
		pdfString(info.Title), pdfString(info.Creator), pdfString(info.Producer), pdfDate, pdfDate)))
	catalog := fmt.Sprintf("<</Type/Catalog/Pages %d 0 R", d.pagesRef)
	if info.PdfA {
		xmp := xmpMetadata(info, created)
		meta := d.add(d.stream("/Type/Metadata/Subtype/XML", []byte(xmp), false))
		icc := d.add(d.stream("/N 3", SRGBProfile(), true))
		catalog += fmt.Sprintf("/Metadata %d 0 R/OutputIntents[<</Type/OutputIntent/S/GTS_PDFA1/OutputConditionIdentifier(sRGB IEC61966-2.1)/Info(sRGB IEC61966-2.1)/DestOutputProfile %d 0 R>>]", meta, icc)
	}
	catalog += ">>"
	root := d.add([]byte(catalog))

	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(d.objs))
	for i, body := range d.objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n", i+1)
		out.Write(body)
		out.WriteString("\nendobj\n")
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(d.objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	id := md5.Sum(out.Bytes()[:min(out.Len(), 1<<20)])
	fmt.Fprintf(&out, "trailer\n<</Size %d/Root %d 0 R/Info %d 0 R/ID[<%x><%x>]>>\nstartxref\n%d\n%%%%EOF\n",
		len(d.objs)+1, root, infoRef, id, id, xref)
	_, err := w.Write(out.Bytes())
	return err
}

func tzSuffix(t time.Time) string {
	_, off := t.Zone()
	if off == 0 {
		return "Z"
	}
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("%s%02d'%02d'", sign, off/3600, off%3600/60)
}

// pdfString encodes text as a PDF string (UTF-16BE when not ASCII).
func pdfString(s string) string {
	ascii := true
	for _, r := range s {
		if r > 126 || r < 32 {
			ascii = false
			break
		}
	}
	if ascii {
		r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
		return "(" + r.Replace(s) + ")"
	}
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			fmt.Fprintf(&b, "%04X%04X", 0xD800+(r>>10), 0xDC00+(r&0x3FF))
		} else {
			fmt.Fprintf(&b, "%04X", r)
		}
	}
	b.WriteString(">")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func xmpMetadata(info PDFInfo, t time.Time) string {
	date := t.Format("2006-01-02T15:04:05-07:00")
	return `<?xpacket begin="` + "\ufeff" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + xmlEscape(info.Title) + `</rdf:li></rdf:Alt></dc:title>
<xmp:CreatorTool>` + xmlEscape(info.Creator) + `</xmp:CreatorTool>
<xmp:CreateDate>` + date + `</xmp:CreateDate><xmp:ModifyDate>` + date + `</xmp:ModifyDate>
<pdf:Producer>` + xmlEscape(info.Producer) + `</pdf:Producer>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}
