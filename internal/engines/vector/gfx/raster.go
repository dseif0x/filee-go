package gfx

import (
	"image"
	"image/color"
	"math"

	xdraw "golang.org/x/image/draw"
)

// Raster draws onto premultiplied RGBA pixels. Colours blend in sRGB, like
// browsers and Skia do by default.
//
// Groups and masks draw into extra layers. Layers are reused and only their
// touched area (dirty) is composited and cleared, and clip masks only cover
// their bounds, so drawings with thousands of small groups or clips (pattern
// tiles, semi-transparent shapes) stay fast.
type Raster struct {
	W, H int
	// Tolerance is the flatness of curves in pixels.
	Tolerance float64

	layers []*rasterLayer
	clips  []*clipMask
	free   []*image.RGBA
}

type rasterLayer struct {
	img     *image.RGBA
	dirty   image.Rectangle
	opacity float64
	mask    bool // content of PushMask
	kind    MaskKind
	isMask  bool // the mask drawing of BeginMaskContent
	blend   BlendMode
}

// clipMask is the coverage of all clips pushed so far, over bounds only.
type clipMask struct {
	pix    []uint8
	bounds image.Rectangle
}

func (c *clipMask) at(x, y int) uint8 {
	if !(image.Point{x, y}).In(c.bounds) {
		return 0
	}
	return c.pix[(y-c.bounds.Min.Y)*c.bounds.Dx()+x-c.bounds.Min.X]
}

// NewRaster returns a transparent w × h raster.
func NewRaster(w, h int) *Raster {
	r := &Raster{W: w, H: h, Tolerance: 0.2}
	r.layers = []*rasterLayer{{img: image.NewRGBA(image.Rect(0, 0, w, h)), opacity: 1}}
	return r
}

// Image is the base layer.
func (r *Raster) Image() *image.RGBA { return r.layers[0].img }

func (r *Raster) topLayer() *rasterLayer { return r.layers[len(r.layers)-1] }

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
	layer := r.topLayer()
	dst := layer.img
	clip := r.clip()
	minX, minY, maxX, maxY := r.W, r.H, -1, -1
	coverageRows(polys, rule, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y, func(y, x0, x1 int, cov []float32) {
		row := dst.Pix[y*dst.Stride:]
		minX, maxX = min(minX, x0), max(maxX, x1)
		minY, maxY = min(minY, y), max(maxY, y)
		for x := x0; x < x1; x++ {
			c := cov[x-x0]
			if c <= 0 {
				continue
			}
			if clip != nil {
				c *= float32(clip.at(x, y)) / 255
				if c <= 0 {
					continue
				}
			}
			sr, sg, sb, sa := src.at(x, y)
			blend(row[x*4:x*4+4], sr*c, sg*c, sb*c, sa*c)
		}
	})
	if maxY >= minY {
		layer.dirty = layer.dirty.Union(image.Rect(minX, minY, maxX, maxY+1))
	}
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
	layer := r.topLayer()
	dst := layer.img
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
				k *= float32(clip.at(x, y)) / 255
			}
			if k <= 0 {
				continue
			}
			blend(row[x*4:x*4+4], cr*k, cg*k, cb*k, ca*k)
		}
	}
	layer.dirty = layer.dirty.Union(b)
}

// ---- clips ----

// newClip intersects coverage (over rect cov, row-major) with the current clip.
func (r *Raster) pushCoverage(cov []float32, area image.Rectangle) {
	parent := r.clip()
	minX, minY, maxX, maxY := r.W, r.H, -1, -1
	w := area.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			v := cov[(y-area.Min.Y)*w+x-area.Min.X]
			if parent != nil {
				v *= float32(parent.at(x, y)) / 255
			}
			if v <= 0 {
				continue
			}
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	c := &clipMask{}
	if maxX >= minX {
		c.bounds = image.Rect(minX, minY, maxX+1, maxY+1)
		c.pix = make([]uint8, c.bounds.Dx()*c.bounds.Dy())
		for y := minY; y <= maxY; y++ {
			for x := minX; x <= maxX; x++ {
				v := cov[(y-area.Min.Y)*w+x-area.Min.X]
				if parent != nil {
					v *= float32(parent.at(x, y)) / 255
				}
				if v > 0 {
					c.pix[(y-minY)*c.bounds.Dx()+x-minX] = clamp8(v * 255)
				}
			}
		}
	}
	r.clips = append(r.clips, c)
}

// shapesArea is where the shapes can draw: their bounds within the current clip.
func (r *Raster) shapesArea(shapes []ClipShape) image.Rectangle {
	box := EmptyRect()
	for _, s := range shapes {
		if s.M.Finite() {
			box = box.Union(s.Path.Transformed(s.M).Bounds())
		}
	}
	if box.Empty() {
		return image.Rectangle{}
	}
	return r.bounds().Intersect(image.Rect(int(math.Floor(box.X0)), int(math.Floor(box.Y0)), int(math.Ceil(box.X1))+1, int(math.Ceil(box.Y1))+1))
}

func (r *Raster) PushClip(p *Path, m Matrix, rule FillRule) {
	r.PushClipShapes([]ClipShape{{Path: p, M: m, Rule: rule}})
}

// PushClipShapes clips to the union of the shapes.
func (r *Raster) PushClipShapes(shapes []ClipShape) {
	area := r.shapesArea(shapes)
	if area.Empty() {
		r.clips = append(r.clips, &clipMask{})
		return
	}
	w := area.Dx()
	acc := make([]float32, w*area.Dy())
	for _, s := range shapes {
		if !s.M.Finite() {
			continue
		}
		polys := s.Path.Transformed(s.M).Flatten(r.Tolerance)
		coverageRows(polys, s.Rule, area.Min.X, area.Min.Y, area.Max.X, area.Max.Y, func(y, x0, x1 int, cov []float32) {
			for x := x0; x < x1; x++ {
				// Union of coverages: 1 - (1-a)(1-b).
				i := (y-area.Min.Y)*w + x - area.Min.X
				acc[i] = 1 - (1-acc[i])*(1-cov[x-x0])
			}
		})
	}
	r.pushCoverage(acc, area)
}

func (r *Raster) PopClip() {
	if len(r.clips) > 0 {
		r.clips = r.clips[:len(r.clips)-1]
	}
}

// ---- groups and masks ----

func (r *Raster) newLayer() *image.RGBA {
	if n := len(r.free); n > 0 {
		img := r.free[n-1]
		r.free = r.free[:n-1]
		return img
	}
	return image.NewRGBA(image.Rect(0, 0, r.W, r.H))
}

// release clears what a layer touched and keeps it for reuse.
func (r *Raster) release(l *rasterLayer) {
	d := l.dirty.Intersect(l.img.Rect)
	for y := d.Min.Y; y < d.Max.Y; y++ {
		clear(l.img.Pix[y*l.img.Stride+d.Min.X*4 : y*l.img.Stride+d.Max.X*4])
	}
	if len(r.free) < 4 {
		r.free = append(r.free, l.img)
	}
}

func (r *Raster) PushGroup(opacity float64) {
	r.layers = append(r.layers, &rasterLayer{img: r.newLayer(), opacity: opacity})
}

// PushBlendGroup starts a group composited with a blend mode.
func (r *Raster) PushBlendGroup(opacity float64, mode BlendMode) {
	r.layers = append(r.layers, &rasterLayer{img: r.newLayer(), opacity: opacity, blend: mode})
}

func (r *Raster) PopGroup() {
	if len(r.layers) < 2 {
		return
	}
	l := r.layers[len(r.layers)-1]
	r.layers = r.layers[:len(r.layers)-1]
	dst := r.topLayer()
	if l.blend != BlendNormal {
		compositeBlend(dst, l, l.opacity, l.blend)
	} else {
		composite(dst, l, nil, MaskLuminance, l.opacity)
	}
	r.release(l)
}

func compositeBlend(dst, src *rasterLayer, opacity float64, mode BlendMode) {
	op := clamp01(opacity)
	d := src.dirty.Intersect(src.img.Rect)
	for y := d.Min.Y; y < d.Max.Y; y++ {
		for x := d.Min.X; x < d.Max.X; x++ {
			i := y*src.img.Stride + x*4
			sp := src.img.Pix[i : i+4]
			if sp[3] == 0 {
				continue
			}
			s := [4]float64{float64(sp[0]) / 255 * op, float64(sp[1]) / 255 * op, float64(sp[2]) / 255 * op, float64(sp[3]) / 255 * op}
			dp := dst.img.Pix[i : i+4]
			dd := [4]float64{float64(dp[0]) / 255, float64(dp[1]) / 255, float64(dp[2]) / 255, float64(dp[3]) / 255}
			BlendPixel(mode, &dd, s)
			for k := 0; k < 4; k++ {
				dp[k] = to8(dd[k])
			}
		}
	}
	dst.dirty = dst.dirty.Union(d)
}

func (r *Raster) PushMask(kind MaskKind, opacity float64) {
	r.layers = append(r.layers, &rasterLayer{img: r.newLayer(), opacity: opacity, mask: true, kind: kind})
}

func (r *Raster) BeginMaskContent() {
	// The mask is drawn without the clips of its content.
	r.layers = append(r.layers, &rasterLayer{img: r.newLayer(), opacity: 1, isMask: true})
}

func (r *Raster) PopMask() {
	if len(r.layers) < 2 {
		return
	}
	var maskLayer *rasterLayer
	if top := r.topLayer(); top.isMask {
		maskLayer = top
		r.layers = r.layers[:len(r.layers)-1]
	}
	if len(r.layers) < 2 {
		return
	}
	l := r.layers[len(r.layers)-1]
	r.layers = r.layers[:len(r.layers)-1]
	if maskLayer != nil {
		// A mask with no drawing hides everything.
		composite(r.topLayer(), l, maskLayer, l.kind, l.opacity)
		r.release(maskLayer)
	}
	r.release(l)
}

// composite draws src over dst with opacity, masked by mask's luminance or alpha.
func composite(dst, src, mask *rasterLayer, kind MaskKind, opacity float64) {
	op := float32(clamp01(opacity))
	if op <= 0 {
		return
	}
	d := src.dirty.Intersect(src.img.Rect)
	if mask != nil {
		d = d.Intersect(mask.dirty)
	}
	for y := d.Min.Y; y < d.Max.Y; y++ {
		for x := d.Min.X; x < d.Max.X; x++ {
			i := y*src.img.Stride + x*4
			sa := src.img.Pix[i+3]
			if sa == 0 {
				continue
			}
			k := op / 255
			if mask != nil {
				ma := mask.img.Pix[i+3]
				if ma == 0 {
					continue
				}
				if kind == MaskAlpha {
					k *= float32(ma) / 255
				} else {
					// Luminance of the premultiplied colour = luminance × alpha
					// (linearRGB coefficients on sRGB values, as browsers do).
					lum := 0.2125*float32(mask.img.Pix[i]) + 0.7154*float32(mask.img.Pix[i+1]) + 0.0721*float32(mask.img.Pix[i+2])
					k *= lum / 255
				}
			}
			p := src.img.Pix[i : i+4]
			blend(dst.img.Pix[i:i+4], float32(p[0])*k, float32(p[1])*k, float32(p[2])*k, float32(sa)*k)
		}
	}
	dst.dirty = dst.dirty.Union(d)
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
