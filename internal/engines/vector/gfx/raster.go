package gfx

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// Raster draws onto premultiplied RGBA pixels. Colours blend in sRGB, like
// browsers and Skia do by default.
type Raster struct {
	W, H int
	// Tolerance is the flatness of curves in pixels.
	Tolerance float64

	layers []*rasterLayer
	clips  []*clipMask
}

type rasterLayer struct {
	img     *image.RGBA
	opacity float64
	mask    bool // content of PushMask
	kind    MaskKind
	isMask  bool // the mask drawing of BeginMaskContent
	blend   BlendMode
}

// clipMask is the coverage of all clips pushed so far (nil pix: everything).
type clipMask struct {
	pix    []uint8
	bounds image.Rectangle
}

// NewRaster returns a transparent w × h raster.
func NewRaster(w, h int) *Raster {
	r := &Raster{W: w, H: h, Tolerance: 0.2}
	r.layers = []*rasterLayer{{img: image.NewRGBA(image.Rect(0, 0, w, h)), opacity: 1}}
	return r
}

// Image is the base layer.
func (r *Raster) Image() *image.RGBA { return r.layers[0].img }

func (r *Raster) top() *image.RGBA { return r.layers[len(r.layers)-1].img }

func (r *Raster) clip() *clipMask {
	if len(r.clips) == 0 {
		return nil
	}
	return r.clips[len(r.clips)-1]
}

func (r *Raster) bounds() image.Rectangle {
	b := image.Rect(0, 0, r.W, r.H)
	if c := r.clip(); c != nil {
		b = b.Intersect(c.bounds)
	}
	return b
}

// Clear fills the base layer with a colour.
func (r *Raster) Clear(c Color) {
	cr, cg, cb, ca := premul(c)
	pix := r.layers[0].img.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = cr, cg, cb, ca
	}
}

func premul(c Color) (uint8, uint8, uint8, uint8) {
	a := clamp01(c.A)
	return to8(clamp01(c.R) * a), to8(clamp01(c.G) * a), to8(clamp01(c.B) * a), to8(a)
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (r *Raster) Fill(p *Path, m Matrix, rule FillRule, paint Paint) {
	if !paint.Visible() || !m.Finite() {
		return
	}
	polys := p.Transformed(m).Flatten(r.Tolerance)
	r.paintPolys(polys, rule, paint, m)
}

func (r *Raster) Stroke(p *Path, m Matrix, s *StrokeStyle, paint Paint) {
	if !paint.Visible() || !m.Finite() || !(s.Width > 0) {
		return
	}
	polys := StrokePath(p, m, s, r.Tolerance)
	r.paintPolys(polys, NonZero, paint, m)
}

func (r *Raster) paintPolys(polys []Polyline, rule FillRule, paint Paint, m Matrix) {
	b := r.bounds()
	if b.Empty() || len(polys) == 0 {
		return
	}
	src := newSource(paint, m)
	if src == nil {
		return
	}
	dst := r.top()
	clip := r.clip()
	coverageRows(polys, rule, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y, func(y, x0, x1 int, cov []float32) {
		row := dst.Pix[y*dst.Stride:]
		for x := x0; x < x1; x++ {
			c := cov[x-x0]
			if c <= 0 {
				continue
			}
			if clip != nil {
				c *= float32(clip.pix[y*r.W+x]) / 255
				if c <= 0 {
					continue
				}
			}
			sr, sg, sb, sa := src.at(x, y)
			blend(row[x*4:x*4+4], sr*c, sg*c, sb*c, sa*c)
		}
	})
}

// blend composites a premultiplied source (0–1) over a pixel.
func blend(px []uint8, sr, sg, sb, sa float32) {
	if sa <= 0 {
		return
	}
	k := 1 - sa
	px[0] = clamp8(sr*255 + float32(px[0])*k)
	px[1] = clamp8(sg*255 + float32(px[1])*k)
	px[2] = clamp8(sb*255 + float32(px[2])*k)
	px[3] = clamp8(sa*255 + float32(px[3])*k)
}

func clamp8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// source gives the premultiplied colour of a paint at a pixel.
type source struct {
	solid      [4]float32
	grad       *Gradient
	inv        Matrix
	lut        [256][4]float32
	radialPrep radialPrep
}

func newSource(paint Paint, m Matrix) *source {
	if paint.Gradient == nil {
		c := paint.Color
		a := float32(clamp01(c.A))
		return &source{solid: [4]float32{float32(clamp01(c.R)) * a, float32(clamp01(c.G)) * a, float32(clamp01(c.B)) * a, a}}
	}
	g := paint.Gradient
	inv, ok := m.Mul(g.Matrix).Invert()
	if !ok {
		return nil
	}
	s := &source{grad: g, inv: inv}
	for i := range s.lut {
		c := g.colorAt(float64(i) / 255)
		a := float32(clamp01(c.A))
		s.lut[i] = [4]float32{float32(clamp01(c.R)) * a, float32(clamp01(c.G)) * a, float32(clamp01(c.B)) * a, a}
	}
	if g.Radial {
		s.radialPrep = prepRadial(g)
	}
	return s
}

func (s *source) at(x, y int) (float32, float32, float32, float32) {
	if s.grad == nil {
		return s.solid[0], s.solid[1], s.solid[2], s.solid[3]
	}
	p := s.inv.Apply(Point{float64(x) + 0.5, float64(y) + 0.5})
	t, ok := s.grad.param(p, &s.radialPrep)
	if !ok {
		return 0, 0, 0, 0
	}
	t = applySpread(t, s.grad.Spread)
	i := int(t*255 + 0.5)
	if i < 0 {
		i = 0
	} else if i > 255 {
		i = 255
	}
	c := s.lut[i]
	return c[0], c[1], c[2], c[3]
}

func applySpread(t float64, sp Spread) float64 {
	switch sp {
	case SpreadRepeat:
		t -= math.Floor(t)
	case SpreadReflect:
		t = math.Mod(math.Abs(t), 2)
		if t > 1 {
			t = 2 - t
		}
	}
	return clamp01(t)
}

// colorAt interpolates the stops (straight colour, sRGB).
func (g *Gradient) colorAt(t float64) Color {
	st := g.Stops
	if len(st) == 0 {
		return Transparent
	}
	if t <= st[0].Offset {
		return st[0].Color
	}
	for i := 1; i < len(st); i++ {
		if t <= st[i].Offset {
			a, b := st[i-1], st[i]
			span := b.Offset - a.Offset
			if span <= 0 {
				return b.Color
			}
			f := (t - a.Offset) / span
			return Color{a.Color.R + (b.Color.R-a.Color.R)*f, a.Color.G + (b.Color.G-a.Color.G)*f,
				a.Color.B + (b.Color.B-a.Color.B)*f, a.Color.A + (b.Color.A-a.Color.A)*f}
		}
	}
	return st[len(st)-1].Color
}

type radialPrep struct {
	cdx, cdy, dr, a float64
}

func prepRadial(g *Gradient) radialPrep {
	cdx, cdy := g.CX-g.FX, g.CY-g.FY
	dr := g.R - g.FR
	return radialPrep{cdx, cdy, dr, cdx*cdx + cdy*cdy - dr*dr}
}

// param is the gradient parameter t of point p (in gradient space).
func (g *Gradient) param(p Point, rp *radialPrep) (float64, bool) {
	if !g.Radial {
		dx, dy := g.X2-g.X1, g.Y2-g.Y1
		l2 := dx*dx + dy*dy
		if l2 == 0 {
			return 1, true
		}
		return ((p.X-g.X1)*dx + (p.Y-g.Y1)*dy) / l2, true
	}
	// Two-point conical gradient: the largest t whose circle passes through p
	// with a non-negative radius.
	pdx, pdy := p.X-g.FX, p.Y-g.FY
	b := pdx*rp.cdx + pdy*rp.cdy + g.FR*rp.dr
	c := pdx*pdx + pdy*pdy - g.FR*g.FR
	if math.Abs(rp.a) < 1e-12 {
		if b == 0 {
			return 0, false
		}
		t := c / (2 * b)
		return t, g.FR+t*rp.dr >= 0
	}
	d := b*b - rp.a*c
	if d < 0 {
		return 0, false
	}
	sq := math.Sqrt(d)
	t1, t2 := (b+sq)/rp.a, (b-sq)/rp.a
	if t1 < t2 {
		t1, t2 = t2, t1
	}
	if g.FR+t1*rp.dr >= 0 {
		return t1, true
	}
	if g.FR+t2*rp.dr >= 0 {
		return t2, true
	}
	return 0, false
}

// ---- images ----

func (r *Raster) DrawImage(img image.Image, m Matrix, opacity float64) {
	if img == nil || opacity <= 0 || !m.Finite() {
		return
	}
	sb := img.Bounds()
	if sb.Empty() {
		return
	}
	inv, ok := m.Invert()
	if !ok {
		return
	}
	// Strong reductions are pre-scaled so bilinear sampling doesn't alias.
	src := toRGBA(img)
	sx := math.Hypot(m.A, m.B)
	sy := math.Hypot(m.C, m.D)
	if sx < 0.5 || sy < 0.5 {
		nw := max(1, int(math.Ceil(float64(sb.Dx())*math.Min(1, sx*1.5))))
		nh := max(1, int(math.Ceil(float64(sb.Dy())*math.Min(1, sy*1.5))))
		small := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.CatmullRom.Scale(small, small.Bounds(), src, src.Bounds(), xdraw.Src, nil)
		inv = Scale(float64(nw)/float64(sb.Dx()), float64(nh)/float64(sb.Dy())).Mul(inv)
		src = small
	}
	box := Rect{0, 0, float64(sb.Dx()), float64(sb.Dy())}.Transform(m)
	b := r.bounds().Intersect(image.Rect(int(math.Floor(box.X0)), int(math.Floor(box.Y0)), int(math.Ceil(box.X1)), int(math.Ceil(box.Y1))))
	if b.Empty() {
		return
	}
	dst := r.top()
	clip := r.clip()
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	op := float32(opacity)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := dst.Pix[y*dst.Stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			p := inv.Apply(Point{float64(x) + 0.5, float64(y) + 0.5})
			if p.X < 0 || p.Y < 0 || p.X >= float64(w) || p.Y >= float64(h) {
				continue
			}
			cr, cg, cb, ca := bilinear(src, p.X-0.5, p.Y-0.5)
			k := op
			if clip != nil {
				k *= float32(clip.pix[y*r.W+x]) / 255
			}
			if k <= 0 {
				continue
			}
			blend(row[x*4:x*4+4], cr*k, cg*k, cb*k, ca*k)
		}
	}
}

func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

func bilinear(img *image.RGBA, fx, fy float64) (float32, float32, float32, float32) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := float32(fx-float64(x0)), float32(fy-float64(y0))
	var acc [4]float32
	for j := 0; j < 2; j++ {
		yy := min(max(y0+j, 0), h-1)
		wy := ty
		if j == 0 {
			wy = 1 - ty
		}
		for i := 0; i < 2; i++ {
			xx := min(max(x0+i, 0), w-1)
			wx := tx
			if i == 0 {
				wx = 1 - tx
			}
			k := wx * wy / 255
			o := yy*img.Stride + xx*4
			acc[0] += float32(img.Pix[o]) * k
			acc[1] += float32(img.Pix[o+1]) * k
			acc[2] += float32(img.Pix[o+2]) * k
			acc[3] += float32(img.Pix[o+3]) * k
		}
	}
	return acc[0], acc[1], acc[2], acc[3]
}

// ---- clips ----

func (r *Raster) PushClip(p *Path, m Matrix, rule FillRule) {
	parent := r.clip()
	c := &clipMask{pix: make([]uint8, r.W*r.H)}
	minX, minY, maxX, maxY := r.W, r.H, -1, -1
	b := r.bounds()
	if m.Finite() {
		polys := p.Transformed(m).Flatten(r.Tolerance)
		coverageRows(polys, rule, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y, func(y, x0, x1 int, cov []float32) {
			for x := x0; x < x1; x++ {
				v := cov[x-x0]
				if parent != nil {
					v *= float32(parent.pix[y*r.W+x]) / 255
				}
				if v <= 0 {
					continue
				}
				c.pix[y*r.W+x] = clamp8(v * 255)
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		})
	}
	if maxX >= minX {
		c.bounds = image.Rect(minX, minY, maxX+1, maxY+1)
	}
	r.clips = append(r.clips, c)
}

// PushClipShapes clips to the union of the shapes.
func (r *Raster) PushClipShapes(shapes []ClipShape) {
	if len(shapes) == 1 {
		r.PushClip(shapes[0].Path, shapes[0].M, shapes[0].Rule)
		return
	}
	parent := r.clip()
	acc := make([]float32, r.W*r.H)
	b := r.bounds()
	for _, s := range shapes {
		if !s.M.Finite() {
			continue
		}
		polys := s.Path.Transformed(s.M).Flatten(r.Tolerance)
		coverageRows(polys, s.Rule, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y, func(y, x0, x1 int, cov []float32) {
			for x := x0; x < x1; x++ {
				// Union of coverages: 1 - (1-a)(1-b).
				i := y*r.W + x
				acc[i] = 1 - (1-acc[i])*(1-cov[x-x0])
			}
		})
	}
	c := &clipMask{pix: make([]uint8, r.W*r.H)}
	minX, minY, maxX, maxY := r.W, r.H, -1, -1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := acc[y*r.W+x]
			if parent != nil {
				v *= float32(parent.pix[y*r.W+x]) / 255
			}
			if v <= 0 {
				continue
			}
			c.pix[y*r.W+x] = clamp8(v * 255)
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	if maxX >= minX {
		c.bounds = image.Rect(minX, minY, maxX+1, maxY+1)
	}
	r.clips = append(r.clips, c)
}

func (r *Raster) PopClip() {
	if len(r.clips) > 0 {
		r.clips = r.clips[:len(r.clips)-1]
	}
}

// ---- groups and masks ----

func (r *Raster) PushGroup(opacity float64) {
	r.layers = append(r.layers, &rasterLayer{img: image.NewRGBA(image.Rect(0, 0, r.W, r.H)), opacity: opacity})
}

// PushBlendGroup starts a group composited with a blend mode.
func (r *Raster) PushBlendGroup(opacity float64, mode BlendMode) {
	r.layers = append(r.layers, &rasterLayer{img: image.NewRGBA(image.Rect(0, 0, r.W, r.H)), opacity: opacity, blend: mode})
}

func (r *Raster) PopGroup() {
	if len(r.layers) < 2 {
		return
	}
	l := r.layers[len(r.layers)-1]
	r.layers = r.layers[:len(r.layers)-1]
	if l.blend != BlendNormal {
		compositeBlend(r.top(), l.img, l.opacity, l.blend)
		return
	}
	composite(r.top(), l.img, nil, MaskLuminance, l.opacity)
}

func compositeBlend(dst, src *image.RGBA, opacity float64, mode BlendMode) {
	op := clamp01(opacity)
	for i := 0; i < len(src.Pix); i += 4 {
		if src.Pix[i+3] == 0 {
			continue
		}
		s := [4]float64{float64(src.Pix[i]) / 255 * op, float64(src.Pix[i+1]) / 255 * op, float64(src.Pix[i+2]) / 255 * op, float64(src.Pix[i+3]) / 255 * op}
		d := [4]float64{float64(dst.Pix[i]) / 255, float64(dst.Pix[i+1]) / 255, float64(dst.Pix[i+2]) / 255, float64(dst.Pix[i+3]) / 255}
		BlendPixel(mode, &d, s)
		for k := 0; k < 4; k++ {
			dst.Pix[i+k] = to8(d[k])
		}
	}
}

func (r *Raster) PushMask(kind MaskKind, opacity float64) {
	r.layers = append(r.layers, &rasterLayer{img: image.NewRGBA(image.Rect(0, 0, r.W, r.H)), opacity: opacity, mask: true, kind: kind})
}

func (r *Raster) BeginMaskContent() {
	// The mask is drawn without the clips of its content.
	r.layers = append(r.layers, &rasterLayer{img: image.NewRGBA(image.Rect(0, 0, r.W, r.H)), opacity: 1, isMask: true})
}

func (r *Raster) PopMask() {
	if len(r.layers) < 2 {
		return
	}
	var maskImg *image.RGBA
	if top := r.layers[len(r.layers)-1]; top.isMask {
		maskImg = top.img
		r.layers = r.layers[:len(r.layers)-1]
	}
	if len(r.layers) < 2 {
		return
	}
	l := r.layers[len(r.layers)-1]
	r.layers = r.layers[:len(r.layers)-1]
	if maskImg == nil {
		// A mask with no drawing hides everything.
		return
	}
	composite(r.top(), l.img, maskImg, l.kind, l.opacity)
}

// composite draws src over dst with opacity, masked by mask's luminance or alpha.
func composite(dst, src, mask *image.RGBA, kind MaskKind, opacity float64) {
	op := float32(clamp01(opacity))
	if op <= 0 {
		return
	}
	for i := 0; i < len(src.Pix); i += 4 {
		sa := src.Pix[i+3]
		if sa == 0 {
			continue
		}
		k := op / 255
		if mask != nil {
			ma := mask.Pix[i+3]
			if ma == 0 {
				continue
			}
			if kind == MaskAlpha {
				k *= float32(ma) / 255
			} else {
				// Luminance of the premultiplied colour = luminance × alpha (linearRGB
				// coefficients on sRGB values, as browsers do).
				lum := 0.2125*float32(mask.Pix[i]) + 0.7154*float32(mask.Pix[i+1]) + 0.0721*float32(mask.Pix[i+2])
				k *= lum / 255
			}
		}
		blend(dst.Pix[i:i+4], float32(src.Pix[i])*k, float32(src.Pix[i+1])*k, float32(src.Pix[i+2])*k, float32(sa)*k)
	}
}

// Straight returns the base layer as straight-alpha NRGBA.
func (r *Raster) Straight() *image.NRGBA {
	src := r.layers[0].img
	out := image.NewNRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		a := src.Pix[i+3]
		switch a {
		case 0:
		case 255:
			copy(out.Pix[i:i+4], src.Pix[i:i+4])
		default:
			c := color.NRGBAModel.Convert(color.RGBA{src.Pix[i], src.Pix[i+1], src.Pix[i+2], a}).(color.NRGBA)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
	return out
}
