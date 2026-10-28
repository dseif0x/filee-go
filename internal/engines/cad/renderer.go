package cad

import (
	"io"
	"math"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/presets"
)

// PageLayout is where the drawing lands on the page: a uniform scale that
// fits the extents into the printable area, centred, y down.
type PageLayout struct {
	Width, Height float64 // output units (PDF points, SVG pixels, bitmap pixels)
	UnitsPerMm    float64 // output units per millimetre of paper
	Scale         float64 // output units per drawing unit
	offX, offY    float64
}

func (p *PageLayout) ToPage(v Vec2) Vec2 { return Vec2{v.X*p.Scale + p.offX, -v.Y*p.Scale + p.offY} }
func (p *PageLayout) VectorToPage(v Vec2) Vec2 {
	return Vec2{v.X * p.Scale, -v.Y * p.Scale}
}

// FromPage is the WCS point shown at page position (x, y).
func (p *PageLayout) FromPage(x, y float64) Vec2 {
	return Vec2{(x - p.offX) / p.Scale, -(y - p.offY) / p.Scale}
}

// Through is the same page seen through a viewport: model space at scale
// paper units per model unit, viewCenter landing on paperCenter.
func (p *PageLayout) Through(scale float64, viewCenter, paperCenter Vec2) *PageLayout {
	c := p.ToPage(paperCenter)
	ms := p.Scale * scale
	return &PageLayout{p.Width, p.Height, p.UnitsPerMm, ms, c.X - viewCenter.X*ms, c.Y + viewCenter.Y*ms}
}

// ForPdf: an ISO sheet (A3 unless the preset asks for A4 or Letter),
// landscape when the drawing is wider than tall, 10 mm margins unless set.
func ForPdf(extents Box2, o *presets.PdfOptions) *PageLayout {
	shortMm, longMm := 297.0, 420.0
	switch o.PageSize {
	case presets.PageA4:
		shortMm, longMm = 210, 297
	case presets.PageLetter:
		shortMm, longMm = 215.9, 279.4
	}
	w, h := extentSize(extents)
	landscape := w >= h
	margin := 10.0
	if o.MarginMm > 0 {
		margin = math.Min(o.MarginMm, shortMm/4)
	}
	if landscape {
		return fit(extents, longMm, shortMm, margin, 72/25.4)
	}
	return fit(extents, shortMm, longMm, margin, 72/25.4)
}

// ForImage: SVG and images get a page with the drawing's aspect ratio whose
// long edge is an A3 sheet (420 mm) with a 6 mm margin; unitsPerMm sets the resolution.
func ForImage(extents Box2, unitsPerMm float64) *PageLayout {
	const longMm, marginMm = 420.0, 6.0
	w, h := extentSize(extents)
	// Very thin drawings (one long line) still get a usable page: at most 20:1.
	aspect := math.Max(1.0/20, math.Min(20, h/w))
	content := longMm - 2*marginMm
	cw, ch := content, content*aspect
	if aspect > 1 {
		cw, ch = content/aspect, content
	}
	return fit(extents, cw+2*marginMm, ch+2*marginMm, marginMm, unitsPerMm)
}

func fit(extents Box2, pageWmm, pageHmm, marginMm, unitsPerMm float64) *PageLayout {
	w, h := extentSize(extents)
	aw := (pageWmm - 2*marginMm) * unitsPerMm
	ah := (pageHmm - 2*marginMm) * unitsPerMm
	scale := math.Min(aw/w, ah/h)
	cx, cy := 0.0, 0.0
	if !extents.Empty {
		cx, cy = (extents.MinX+extents.MaxX)/2, (extents.MinY+extents.MaxY)/2
	}
	pw, ph := pageWmm*unitsPerMm, pageHmm*unitsPerMm
	return &PageLayout{pw, ph, unitsPerMm, scale, pw/2 - cx*scale, ph/2 + cy*scale}
}

// extentSize gives degenerate extents (a point, a horizontal line) some room.
func extentSize(e Box2) (float64, float64) {
	w, h := e.Width(), e.Height()
	s := math.Max(w, h)
	if s <= 0 {
		return 1, 1
	}
	return math.Max(w, s*1e-3), math.Max(h, s*1e-3)
}

const (
	// minDashPatternMm: shorter dash patterns on paper are drawn continuous.
	minDashPatternMm = 0.8
	// maxDashesPerFigure: beyond it the line is drawn continuous.
	maxDashesPerFigure = 20_000
)

func pdfInfo(title string) gfx.PDFInfo {
	return gfx.PDFInfo{Title: title, Creator: "Filee", Producer: "Filee", Created: time.Now()}
}

// WritePdf writes a one-page vector PDF.
func WritePdf(scene *Scene, o *presets.PdfOptions, palette Palette, title string, w io.Writer) error {
	page := ForPdf(scene.Extents, o)
	doc := gfx.NewPDF()
	pg := doc.Page(page.Width, page.Height)
	Draw(pg, scene, page, palette, false)
	pg.Finish()
	return doc.Write(w, pdfInfo(title))
}

// WritePdfSheets writes one page per layout (#172): each page fitted to the
// layout's drawing and viewport frames; inside each frame model space at the
// viewport's scale, clipped to it, under the layout's own drawing.
func WritePdfSheets(sheets []*LayoutSheet, model *Scene, o *presets.PdfOptions, palette Palette, title string, w io.Writer) error {
	doc := gfx.NewPDF()
	for _, sheet := range sheets {
		page := ForPdf(sheet.Extents(), o)
		pg := doc.Page(page.Width, page.Height)
		for _, v := range sheet.Viewports {
			c1 := page.ToPage(Vec2{v.Frame.MinX, v.Frame.MinY})
			c2 := page.ToPage(Vec2{v.Frame.MaxX, v.Frame.MaxY})
			clip := gfx.NewPath()
			clip.Rect(math.Min(c1.X, c2.X), math.Min(c1.Y, c2.Y), math.Abs(c2.X-c1.X), math.Abs(c2.Y-c1.Y))
			pg.PushClip(clip, gfx.Identity, gfx.NonZero)
			center := Vec2{(v.Frame.MinX + v.Frame.MaxX) / 2, (v.Frame.MinY + v.Frame.MaxY) / 2}
			Draw(pg, model, page.Through(v.Scale, v.ViewCenter, center), palette, false)
			pg.PopClip()
		}
		Draw(pg, sheet.Paper, page, palette, false)
		pg.Finish()
	}
	return doc.Write(w, pdfInfo(title))
}

// WriteSvg writes an SVG (pixels at 96 dpi) with a white background.
func WriteSvg(scene *Scene, palette Palette, w io.Writer) error {
	page := ForImage(scene.Extents, 96/25.4)
	s := gfx.NewSVGWriter(page.Width, page.Height)
	Draw(s, scene, page, palette, true)
	return s.Write(w)
}

// RenderBitmap renders a white-background bitmap at dpi (for the virtual A3
// page), limited to a sensible pixel count; it returns the dpi used.
func RenderBitmap(scene *Scene, dpi float64, palette Palette) (*gfx.Raster, float64) {
	const maxEdge, maxPixels = 12_000.0, 80_000_000.0
	page := ForImage(scene.Extents, dpi/25.4)
	reduce := math.Min(1, math.Min(maxEdge/math.Max(page.Width, page.Height), math.Sqrt(maxPixels/(page.Width*page.Height))))
	if reduce < 1 {
		dpi *= reduce
		page = ForImage(scene.Extents, dpi/25.4)
	}
	r := gfx.NewRaster(max(1, int(math.Round(page.Width))), max(1, int(math.Round(page.Height))))
	Draw(r, scene, page, palette, true)
	return r, dpi
}

// Draw draws every primitive in order, in the palette's colours; consecutive
// strokes with the same pen are merged into one path.
func Draw(dev gfx.Device, scene *Scene, page *PageLayout, palette Palette, background bool) {
	if background {
		bg := gfx.NewPath()
		bg.Rect(0, 0, page.Width, page.Height)
		dev.Fill(bg, gfx.Identity, gfx.NonZero, gfx.Solid(gfx.White))
	}
	batch := &strokeBatch{dev: dev}
	for _, item := range scene.Items {
		switch p := item.(type) {
		case *StrokePrimitive:
			drawStroke(batch, p, page, palette.Ink(p.Color))
		case *InfiniteLinePrimitive:
			drawInfinite(batch, p, page, palette.Ink(p.Color))
		case *FillPrimitive:
			batch.flush()
			drawFill(dev, p, page, palette)
		case *HatchLinesPrimitive:
			batch.flush()
			drawHatchLines(dev, p, page, palette.Ink(p.Color))
		case *TextPrimitive:
			batch.flush()
			drawText(dev, p, page, palette.Ink(p.Color))
		case *PointPrimitive:
			batch.flush()
			drawPoint(dev, p, page, palette.Ink(p.Color))
		}
	}
	batch.flush()
}

func addFigure(path *gfx.Path, f *Figure, page *PageLayout, close bool) {
	s := page.ToPage(f.Start)
	path.MoveTo(s.X, s.Y)
	if len(f.Segments) == 0 {
		path.LineTo(s.X, s.Y) // zero length: a dot with round caps
		return
	}
	for _, seg := range f.Segments {
		p := page.ToPage(seg.P)
		if seg.Cubic {
			c1, c2 := page.ToPage(seg.C1), page.ToPage(seg.C2)
			path.CubeTo(c1.X, c1.Y, c2.X, c2.Y, p.X, p.Y)
		} else {
			path.LineTo(p.X, p.Y)
		}
	}
	if f.Closed || close {
		path.Close()
	}
}

func drawStroke(b *strokeBatch, s *StrokePrimitive, page *PageLayout, color RGB) {
	pen := s.WidthMm * page.UnitsPerMm
	wide := s.WorldWidth > 0 && s.WorldWidth*page.Scale > pen
	width := pen
	if wide {
		width = s.WorldWidth * page.Scale
	}
	path := gfx.NewPath()
	for _, f := range s.Figures {
		addFigure(path, f, page, false)
	}
	dashes := pageDashes(s.Dashes, page)
	if dashes != nil {
		// Too many pieces: drawn continuous.
		period := 0.0
		for _, d := range dashes {
			period += math.Abs(d)
		}
		length := 0.0
		for _, pl := range path.Flatten(1) {
			length += pl.Length()
		}
		if length/period*float64(len(dashes)) > maxDashesPerFigure {
			dashes = nil
		}
	}
	b.add(path, color, width, wide, dashes)
}

// pageDashes is the dash pattern in page units, or nil for a continuous line.
func pageDashes(dashes []float64, page *PageLayout) []float64 {
	if dashes == nil {
		return nil
	}
	out := make([]float64, len(dashes))
	period := 0.0
	for i, d := range dashes {
		out[i] = math.Abs(d * page.Scale)
		period += out[i]
	}
	if math.IsNaN(period) || math.IsInf(period, 0) || period < minDashPatternMm*page.UnitsPerMm {
		return nil
	}
	return out
}

func drawInfinite(b *strokeBatch, l *InfiniteLinePrimitive, page *PageLayout, color RGB) {
	// Clip to the page: the line only needs to reach its corners.
	corners := []Vec2{page.FromPage(0, 0), page.FromPage(page.Width, 0), page.FromPage(0, page.Height), page.FromPage(page.Width, page.Height)}
	tMax, tMin := math.Inf(-1), math.Inf(1)
	for _, c := range corners {
		t := Dot2(c.Sub(l.Origin), l.Direction)
		tMax = math.Max(tMax, t)
		tMin = math.Min(tMin, t)
	}
	if l.IsRay {
		tMin = 0
	}
	if tMax <= tMin {
		return
	}
	f := NewFigure(l.Origin.Add(l.Direction.Mul(tMin)))
	f.LineTo(l.Origin.Add(l.Direction.Mul(tMax)))
	drawStroke(b, &StrokePrimitive{WidthMm: l.WidthMm, Dashes: l.Dashes, Figures: []*Figure{f}}, page, color)
}

func drawFill(dev gfx.Device, f *FillPrimitive, page *PageLayout, palette Palette) {
	color := palette.Ink(f.Color)
	if f.IsArea {
		color = palette.Area(f.Color)
	}
	path := gfx.NewPath()
	for _, fig := range f.Figures {
		addFigure(path, fig, page, true)
	}
	paint := gfx.Solid(color.gfx())
	if f.GradientColor != nil {
		second := palette.Ink(*f.GradientColor)
		if f.IsArea {
			second = palette.Area(*f.GradientColor)
		}
		b := path.Bounds()
		s, c := math.Sincos(f.GradientAngle)
		half := (math.Abs(c)*b.Width() + math.Abs(s)*b.Height()) / 2
		mx, my := (b.X0+b.X1)/2, (b.Y0+b.Y1)/2
		dx, dy := c*half, -s*half
		paint = gfx.Paint{Gradient: &gfx.Gradient{
			X1: mx - dx, Y1: my - dy, X2: mx + dx, Y2: my + dy,
			Stops:  []gfx.Stop{{Offset: 0, Color: color.gfx()}, {Offset: 1, Color: second.gfx()}},
			Matrix: gfx.Identity,
		}}
	}
	dev.Fill(path, gfx.Identity, gfx.EvenOdd, paint)
}

func pen(width float64, wide bool) *gfx.StrokeStyle {
	// Line weights plot with round ends; wide polylines have square ends and mitred corners.
	if wide {
		return &gfx.StrokeStyle{Width: width, Cap: gfx.ButtCap, Join: gfx.MiterJoin, MiterLimit: 4}
	}
	return &gfx.StrokeStyle{Width: width, Cap: gfx.RoundCap, Join: gfx.RoundJoin, MiterLimit: 4}
}

func drawHatchLines(dev gfx.Device, h *HatchLinesPrimitive, page *PageLayout, color RGB) {
	clip := gfx.NewPath()
	for _, f := range h.Boundary {
		addFigure(clip, f, page, true)
	}
	lines := gfx.NewPath()
	for _, l := range h.Lines {
		a, b := page.ToPage(l[0]), page.ToPage(l[1])
		lines.MoveTo(a.X, a.Y)
		lines.LineTo(b.X, b.Y)
	}
	dev.PushClip(clip, gfx.Identity, gfx.EvenOdd)
	dev.Stroke(lines, gfx.Identity, pen(h.WidthMm*page.UnitsPerMm, false), gfx.Solid(color.gfx()))
	dev.PopClip()
}

func drawText(dev gfx.Device, t *TextPrimitive, page *PageLayout, color RGB) {
	o := page.ToPage(t.Origin)
	x := page.VectorToPage(t.XAxis)
	y := page.VectorToPage(t.YAxis)
	size := y.Length()
	if size < 0.01 || math.IsNaN(size) || math.IsInf(size, 0) || size > 1e6 {
		return
	}
	shaped := fonts().Shape(t.Text, t.Face.query(), 1, "")
	outline := fonts().TextPath(shaped, 0, 0)
	if outline.Empty() {
		return
	}
	m := gfx.Matrix{A: x.X, B: x.Y, C: y.X, D: y.Y, E: o.X, F: o.Y}
	dev.Fill(outline, m, gfx.NonZero, gfx.Solid(color.gfx()))
}

func drawPoint(dev gfx.Device, p *PointPrimitive, page *PageLayout, color RGB) {
	r := math.Max(p.WidthMm*page.UnitsPerMm, 0.3*page.UnitsPerMm) / 2
	c := page.ToPage(p.Location)
	path := gfx.NewPath()
	path.Ellipse(c.X, c.Y, r, r)
	dev.Fill(path, gfx.Identity, gfx.NonZero, gfx.Solid(color.gfx()))
}

// strokeBatch merges consecutive strokes with the same pen into one path
// (smaller PDF / SVG, faster), up to a size viewers handle well.
type strokeBatch struct {
	dev    gfx.Device
	path   *gfx.Path
	color  RGB
	width  float64
	wide   bool
	dashes []float64
	points int
}

const maxBatchPoints = 4096

func sameDashes(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *strokeBatch) add(p *gfx.Path, color RGB, width float64, wide bool, dashes []float64) {
	if b.path != nil && (color != b.color || width != b.width || wide != b.wide || !sameDashes(dashes, b.dashes) || b.points > maxBatchPoints) {
		b.flush()
	}
	if b.path == nil {
		b.path = gfx.NewPath()
	}
	b.color, b.width, b.wide, b.dashes = color, width, wide, dashes
	b.path.Append(p)
	b.points += len(p.Pts)
}

func (b *strokeBatch) flush() {
	if b.path == nil {
		return
	}
	s := pen(b.width, b.wide)
	s.Dashes = b.dashes
	b.dev.Stroke(b.path, gfx.Identity, s, gfx.Solid(b.color.gfx()))
	b.path = nil
	b.points = 0
}
