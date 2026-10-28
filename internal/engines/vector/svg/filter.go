package svg

import (
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Filter effects are drawn into a raster of the filter region (at the
// device's filter resolution), processed, and placed as a picture: PDF
// cannot express them, and Skia's PDF output rasterizes them the same way.

const maxFilterSide = 4096

// resolveFilters reads the filter property: url references and CSS filter
// functions, applied in order. ok is false when the element must not be drawn
// (a reference to something that is no filter).
func (r *renderer) resolveFilters(v string) ([]*Node, bool) {
	var out []*Node
	rest := strings.TrimSpace(v)
	for rest != "" {
		if strings.HasPrefix(strings.ToLower(rest), "url(") {
			end := strings.IndexByte(rest, ')')
			if end < 0 {
				return nil, false
			}
			id, _, _ := urlRef(rest[:end+1])
			f := r.doc.ids[id]
			if f == nil || f.Name != "filter" {
				return nil, false
			}
			out = append(out, f)
			rest = strings.TrimSpace(rest[end+1:])
			continue
		}
		open := strings.IndexByte(rest, '(')
		end := strings.IndexByte(rest, ')')
		if open < 0 || end < open {
			return nil, false
		}
		f := cssFilterFunction(strings.ToLower(strings.TrimSpace(rest[:open])), strings.TrimSpace(rest[open+1:end]))
		if f == nil {
			// An invalid value makes the declaration void: no filter at all.
			return nil, true
		}
		out = append(out, f)
		rest = strings.TrimSpace(rest[end+1:])
	}
	return out, len(out) > 0
}

// fimg is an intermediate result: premultiplied pixels, their colour space
// and the pixel subregion that holds them.
type fimg struct {
	img    *image.RGBA
	linear bool
	sub    image.Rectangle
}

type filterRun struct {
	r       *renderer
	src     *image.RGBA
	srcSub  image.Rectangle
	results map[string]*fimg
	last    *fimg
	w, h    int
	m       gfx.Matrix // user space → filter pixels
	inv     gfx.Matrix
	sx, sy  float64 // user units → pixels along x and y
	region  gfx.Rect
	bbox    gfx.Rect
	primBB  bool
	lc      lengthContext
	c       context
	linear  bool
	fillSrc gfx.Paint
	strkSrc gfx.Paint
}

func (r *renderer) drawFiltered(n, f *Node, c context, draw func(context)) {
	// feImage can draw elements that are filtered again; stop runaway nesting.
	if r.filterDepth > 8 {
		return
	}
	r.filterDepth++
	defer func() { r.filterDepth-- }()
	chain := r.gradientChain(f)
	var prims *Node
	for _, cn := range chain {
		if cn.Name == "filter" && len(cn.Elements()) > 0 {
			prims = cn
			break
		}
	}
	if prims == nil {
		// A filter without primitives makes the element disappear.
		return
	}
	pst := c.pst
	if pst == nil {
		pst = defaultStyle()
	}
	bbox := r.bbox(n, context{m: gfx.Identity, st: pst, lc: c.plc}, false)
	units, _ := chainAttr(chain, "filterUnits")
	var region gfx.Rect
	if units == "userSpaceOnUse" {
		get := func(name string, axis int, def Length) float64 {
			v, ok := chainAttr(chain, name)
			l, ok2 := parseLength(v)
			if !ok || !ok2 {
				l = def
			}
			return c.lc.resolve(l, axis)
		}
		x := get("x", axisX, Length{-10, "%"})
		y := get("y", axisY, Length{-10, "%"})
		region = gfx.Rect{X0: x, Y0: y, X1: x + get("width", axisX, Length{120, "%"}), Y1: y + get("height", axisY, Length{120, "%"})}
	} else {
		if bbox.Empty() || bbox.Width() <= 0 || bbox.Height() <= 0 {
			return
		}
		frac := func(name string, def float64) float64 {
			v, ok := chainAttr(chain, name)
			if !ok {
				return def
			}
			return parseFloat(v, def)
		}
		x := bbox.X0 + frac("x", -0.1)*bbox.Width()
		y := bbox.Y0 + frac("y", -0.1)*bbox.Height()
		region = gfx.Rect{X0: x, Y0: y, X1: x + frac("width", 1.2)*bbox.Width(), Y1: y + frac("height", 1.2)*bbox.Height()}
	}
	if region.Empty() || region.Width() <= 0 || region.Height() <= 0 {
		return
	}
	dev := region.Transform(c.m)
	if dev.Empty() || dev.Width() <= 0 || dev.Height() <= 0 {
		return
	}
	res := r.filterRes
	if k := maxFilterSide / math.Max(dev.Width()*res, dev.Height()*res); k < 1 {
		res *= k
	}
	w := int(math.Ceil(dev.Width() * res))
	h := int(math.Ceil(dev.Height() * res))
	if w <= 0 || h <= 0 {
		return
	}
	off := gfx.NewRaster(w, h)
	toOff := gfx.Scale(res, res).Mul(gfx.Translate(-dev.X0, -dev.Y0))
	saved := r.dev
	r.dev = off
	clip := gfx.NewPath()
	clip.Rect(region.X0, region.Y0, region.Width(), region.Height())
	cc := c
	cc.m = toOff.Mul(c.m)
	off.PushClip(clip, cc.m, gfx.NonZero)
	draw(cc)
	off.PopClip()
	r.dev = saved

	inv, ok := cc.m.Invert()
	if !ok {
		return
	}
	pu, _ := chainAttr(chain, "primitiveUnits")
	fr := &filterRun{
		r: r, src: off.Image(), results: map[string]*fimg{}, w: w, h: h, m: cc.m, inv: inv,
		sx: math.Hypot(cc.m.A, cc.m.B), sy: math.Hypot(cc.m.C, cc.m.D),
		region: region, bbox: bbox, primBB: pu == "objectBoundingBox", lc: c.lc, c: c,
	}
	fr.srcSub = fr.toPixels(region)
	if p, _, ok := r.resolvePaint(c.st.fill, c.st.fillOpacity, c, bbox); ok {
		fr.fillSrc = p
	}
	if p, _, ok := r.resolvePaint(c.st.stroke, c.st.strokeOpacity, c, bbox); ok {
		fr.strkSrc = p
	}
	fst := r.styleOf(prims, c.lc)
	for _, p := range prims.Elements() {
		pstyle := computeStyle(p, fst, c.lc)
		fr.linear = true
		if v, ok := spec(p, "color-interpolation-filters"); ok && v != "auto" && v != "inherit" {
			fr.linear = v == "linearRGB"
		} else if v := fst.colorInterp; v != "" {
			fr.linear = v == "linearRGB"
		}
		out := fr.primitive(p, pstyle)
		if out == nil {
			continue
		}
		fr.last = out
		if name := p.Attr("result"); name != "" {
			fr.results[name] = out
		}
	}
	if fr.last == nil {
		return
	}
	result := fr.convert(fr.last, false)
	r.dev.DrawImage(result.img, gfx.Translate(dev.X0, dev.Y0).Mul(gfx.Scale(1/res, 1/res)), 1)
}

// toPixels maps a user-space rectangle to the pixel rectangle covering it.
func (fr *filterRun) toPixels(rect gfx.Rect) image.Rectangle {
	b := rect.Transform(fr.m)
	if b.Empty() {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(b.X0+1e-6)), int(math.Floor(b.Y0+1e-6)), int(math.Ceil(b.X1-1e-6)), int(math.Ceil(b.Y1-1e-6))).
		Intersect(image.Rect(0, 0, fr.w, fr.h))
}

func (fr *filterRun) blank() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, fr.w, fr.h)) }

// convert returns the image in the wanted colour space.
func (fr *filterRun) convert(f *fimg, linear bool) *fimg {
	if f.linear == linear {
		return f
	}
	out := fr.blank()
	lut := srgbToLinear8
	if !linear {
		lut = linearToSRGB8
	}
	for i := 0; i < len(f.img.Pix); i += 4 {
		a := f.img.Pix[i+3]
		if a == 0 {
			continue
		}
		af := float64(a) / 255
		for k := 0; k < 3; k++ {
			v := float64(f.img.Pix[i+k]) / 255 / af
			out.Pix[i+k] = uint8(lut[min(255, int(v*255+0.5))]*af + 0.5)
		}
		out.Pix[i+3] = a
	}
	return &fimg{out, linear, f.sub}
}

var srgbToLinear8, linearToSRGB8 [256]float64

func init() {
	for i := range srgbToLinear8 {
		v := float64(i) / 255
		srgbToLinear8[i] = toLinear(v) * 255
		linearToSRGB8[i] = toSRGB(v) * 255
	}
}

func (fr *filterRun) input(name string) *fimg {
	switch name {
	case "SourceGraphic":
		return &fimg{fr.src, false, fr.srcSub}
	case "SourceAlpha":
		a := fr.blank()
		for i := 3; i < len(a.Pix); i += 4 {
			a.Pix[i] = fr.src.Pix[i]
		}
		return &fimg{a, false, fr.srcSub}
	case "BackgroundImage", "BackgroundAlpha":
		return &fimg{fr.blank(), false, fr.srcSub}
	case "FillPaint", "StrokePaint":
		paint := fr.fillSrc
		if name == "StrokePaint" {
			paint = fr.strkSrc
		}
		ras := gfx.NewRaster(fr.w, fr.h)
		p := gfx.NewPath()
		p.Rect(fr.region.X0, fr.region.Y0, fr.region.Width(), fr.region.Height())
		ras.Fill(p, fr.m, gfx.NonZero, paint)
		return &fimg{ras.Image(), false, fr.srcSub}
	case "":
		if fr.last != nil {
			return fr.last
		}
		return &fimg{fr.src, false, fr.srcSub}
	}
	if r, ok := fr.results[name]; ok {
		return r
	}
	if fr.last != nil {
		return fr.last
	}
	return &fimg{fr.src, false, fr.srcSub}
}

// subregion is the pixel rectangle of a primitive: its x/y/width/height, by
// default the union of its inputs' subregions (or the filter region).
func (fr *filterRun) subregion(p *Node, inputs []*fimg) image.Rectangle {
	def := fr.srcSub
	if len(inputs) > 0 {
		def = image.Rectangle{}
		for _, in := range inputs {
			def = def.Union(in.sub)
		}
	}
	if !p.Has("x") && !p.Has("y") && !p.Has("width") && !p.Has("height") {
		return def
	}
	// The default in user space.
	d := gfx.Rect{X0: float64(def.Min.X), Y0: float64(def.Min.Y), X1: float64(def.Max.X), Y1: float64(def.Max.Y)}.Transform(fr.inv)
	val := func(name string, axis int, dflt float64, isSize bool) float64 {
		v, ok := p.Attrs[name]
		if !ok {
			return dflt
		}
		l, ok := parseLength(v)
		if !ok {
			return dflt
		}
		if fr.primBB {
			f := l.V
			if l.Unit == "%" {
				f /= 100
			}
			if axis == axisX {
				if isSize {
					return f * fr.bbox.Width()
				}
				return fr.bbox.X0 + f*fr.bbox.Width()
			}
			if isSize {
				return f * fr.bbox.Height()
			}
			return fr.bbox.Y0 + f*fr.bbox.Height()
		}
		return fr.lc.resolve(l, axis)
	}
	x := val("x", axisX, d.X0, false)
	y := val("y", axisY, d.Y0, false)
	w := val("width", axisX, d.Width(), true)
	h := val("height", axisY, d.Height(), true)
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	return fr.toPixels(gfx.Rect{X0: x, Y0: y, X1: x + w, Y1: y + h})
}

// clipTo zeroes the pixels outside sub.
func clipTo(img *image.RGBA, sub image.Rectangle) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+b.Dx()*4]
		if y < sub.Min.Y || y >= sub.Max.Y {
			clear(row)
			continue
		}
		clear(row[:max(0, sub.Min.X)*4])
		if sub.Max.X < b.Dx() {
			clear(row[max(0, sub.Max.X)*4:])
		}
	}
}

func (fr *filterRun) primitive(p *Node, st *style) *fimg {
	in := func(attr string) *fimg { return fr.convert(fr.input(p.Attr(attr)), fr.linear) }
	var inputs []*fimg
	var out *image.RGBA
	switch p.Name {
	case "feGaussianBlur":
		a := in("in")
		inputs = []*fimg{a}
		sd := parseNumbers(p.Attr("stdDeviation"))
		sx, sy := 0.0, 0.0
		if len(sd) > 0 {
			sx, sy = sd[0], sd[0]
		}
		if len(sd) > 1 {
			sy = sd[1]
		}
		if len(sd) > 2 || sx < 0 || sy < 0 {
			// A negative deviation disables the primitive: the input passes through.
			out = a.img
			break
		}
		if fr.primBB {
			sx *= fr.bbox.Width()
			sy *= fr.bbox.Height()
		}
		out = blur(a.img, sx*fr.sx, sy*fr.sy)
	case "feOffset":
		a := in("in")
		inputs = []*fimg{a}
		dx, dy := parseFloat(p.Attr("dx"), 0), parseFloat(p.Attr("dy"), 0)
		if fr.primBB {
			dx *= fr.bbox.Width()
			dy *= fr.bbox.Height()
		}
		v := fr.m.ApplyVector(gfx.Point{X: dx, Y: dy})
		out = offset(a.img, v.X, v.Y)
	case "feFlood":
		out = fr.blank()
		col := st.floodColor
		col.A *= st.floodOpacity
		if fr.linear {
			col.R, col.G, col.B = toLinear(col.R), toLinear(col.G), toLinear(col.B)
		}
		fillImage(out, col)
	case "feMerge":
		out = fr.blank()
		for _, mn := range p.Elements() {
			if mn.Name == "feMergeNode" {
				a := fr.convert(fr.input(mn.Attr("in")), fr.linear)
				inputs = append(inputs, a)
				over(out, a.img)
			}
		}
	case "feComposite":
		a, b := in("in"), in("in2")
		inputs = []*fimg{a, b}
		k := [4]float64{parseFloat(p.Attr("k1"), 0), parseFloat(p.Attr("k2"), 0), parseFloat(p.Attr("k3"), 0), parseFloat(p.Attr("k4"), 0)}
		out = composite(a.img, b.img, p.Attr("operator"), k)
	case "feBlend":
		a, b := in("in"), in("in2")
		inputs = []*fimg{a, b}
		out = blendImages(a.img, b.img, gfx.ParseBlendMode(p.Attr("mode")))
	case "feColorMatrix":
		a := in("in")
		inputs = []*fimg{a}
		out = colorMatrix(a.img, p)
	case "feComponentTransfer":
		a := in("in")
		inputs = []*fimg{a}
		out = componentTransfer(a.img, p)
	case "feDropShadow":
		a := in("in")
		inputs = []*fimg{a}
		sd := parseNumbers(p.Attr("stdDeviation"))
		sx, sy := 2.0, 2.0
		if len(sd) > 0 {
			sx, sy = sd[0], sd[0]
		}
		if len(sd) > 1 {
			sy = sd[1]
		}
		dx, dy := parseFloat(p.Attr("dx"), 2), parseFloat(p.Attr("dy"), 2)
		if fr.primBB {
			sx, dx = sx*fr.bbox.Width(), dx*fr.bbox.Width()
			sy, dy = sy*fr.bbox.Height(), dy*fr.bbox.Height()
		}
		v := fr.m.ApplyVector(gfx.Point{X: dx, Y: dy})
		shadow := blur(offset(a.img, v.X, v.Y), sx*fr.sx, sy*fr.sy)
		col := st.floodColor
		col.A *= st.floodOpacity
		if fr.linear {
			col.R, col.G, col.B = toLinear(col.R), toLinear(col.G), toLinear(col.B)
		}
		cr, cg, cb, ca := premul8(col)
		for i := 0; i < len(shadow.Pix); i += 4 {
			k := float64(shadow.Pix[i+3]) / 255
			shadow.Pix[i], shadow.Pix[i+1], shadow.Pix[i+2], shadow.Pix[i+3] = uint8(cr*k+0.5), uint8(cg*k+0.5), uint8(cb*k+0.5), uint8(ca*k+0.5)
		}
		over(shadow, a.img)
		out = shadow
	case "feMorphology":
		a := in("in")
		inputs = []*fimg{a}
		rad := parseNumbers(p.Attr("radius"))
		rx, ry := 0.0, 0.0
		if len(rad) > 0 {
			rx, ry = rad[0], rad[0]
		}
		if len(rad) > 1 {
			ry = rad[1]
		}
		if len(rad) > 2 || rx < 0 || ry < 0 || (p.Has("radius") && len(rad) == 0) {
			out = fr.blank()
			break
		}
		if rx == 0 && ry == 0 {
			out = a.img
			break
		}
		if fr.primBB {
			rx *= fr.bbox.Width()
			ry *= fr.bbox.Height()
		}
		out = morphology(a.img, int(math.Round(rx*fr.sx)), int(math.Round(ry*fr.sy)), p.Attr("operator") == "dilate")
	case "feTile":
		a := in("in")
		inputs = []*fimg{a}
		out = tile(a.img, a.sub, fr.subregion(p, inputs))
	case "feImage":
		out = fr.feImage(p)
	case "feTurbulence":
		out = fr.turbulence(p)
	case "feConvolveMatrix":
		a := in("in")
		inputs = []*fimg{a}
		out = convolve(a.img, p)
	case "feDiffuseLighting", "feSpecularLighting":
		a := in("in")
		inputs = []*fimg{a}
		out = fr.lighting(a.img, p, st)
	case "feDisplacementMap":
		a, b := in("in"), in("in2")
		inputs = []*fimg{a, b}
		scale := parseFloat(p.Attr("scale"), 0)
		if fr.primBB {
			scale *= fr.bbox.Width()
		}
		out = displace(a.img, b.img, scale*fr.sx, p.Attr("xChannelSelector"), p.Attr("yChannelSelector"))
	default:
		return nil
	}
	if out == nil {
		return nil
	}
	sub := fr.subregion(p, inputs)
	if p.Name == "feFlood" || p.Name == "feImage" || p.Name == "feTurbulence" {
		sub = fr.subregion(p, nil)
	}
	if len(inputs) > 0 && (out == inputs[0].img) {
		cp := fr.blank()
		copy(cp.Pix, out.Pix)
		out = cp
	}
	clipTo(out, sub)
	return &fimg{out, fr.linear, sub}
}

func premul8(c gfx.Color) (float64, float64, float64, float64) {
	a := clamp01(c.A)
	return clamp01(c.R) * a * 255, clamp01(c.G) * a * 255, clamp01(c.B) * a * 255, a * 255
}

func fillImage(img *image.RGBA, c gfx.Color) {
	r, g, b, a := premul8(c)
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(r+0.5), uint8(g+0.5), uint8(b+0.5), uint8(a+0.5)
	}
}

// over composites src over dst in place.
func over(dst, src *image.RGBA) {
	for i := 0; i < len(dst.Pix) && i < len(src.Pix); i += 4 {
		sa := float64(src.Pix[i+3]) / 255
		if sa == 0 {
			continue
		}
		k := 1 - sa
		for j := 0; j < 4; j++ {
			dst.Pix[i+j] = uint8(math.Min(255, float64(src.Pix[i+j])+float64(dst.Pix[i+j])*k+0.5))
		}
	}
}

func offset(src *image.RGBA, dx, dy float64) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	ix, iy := int(math.Round(dx)), int(math.Round(dy))
	w, h := src.Rect.Dx(), src.Rect.Dy()
	for y := 0; y < h; y++ {
		sy := y - iy
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < w; x++ {
			sx := x - ix
			if sx < 0 || sx >= w {
				continue
			}
			copy(out.Pix[y*out.Stride+x*4:y*out.Stride+x*4+4], src.Pix[sy*src.Stride+sx*4:sy*src.Stride+sx*4+4])
		}
	}
	return out
}

// blur approximates a Gaussian with three box blurs per axis.
func blur(src *image.RGBA, sx, sy float64) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	copy(out.Pix, src.Pix)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	buf := make([]float64, max(w, h)*4)
	tmp := make([]float64, max(w, h)*4)
	pass := func(sigma float64, horizontal bool) {
		if sigma <= 0.1 {
			return
		}
		sizes := boxSizes(sigma)
		lines, length := h, w
		if !horizontal {
			lines, length = w, h
		}
		for l := 0; l < lines; l++ {
			for i := 0; i < length; i++ {
				o := l*out.Stride + i*4
				if !horizontal {
					o = i*out.Stride + l*4
				}
				for c := 0; c < 4; c++ {
					buf[i*4+c] = float64(out.Pix[o+c])
				}
			}
			for _, s := range sizes {
				boxBlur(buf[:length*4], tmp[:length*4], length, (s-1)/2)
			}
			for i := 0; i < length; i++ {
				o := l*out.Stride + i*4
				if !horizontal {
					o = i*out.Stride + l*4
				}
				for c := 0; c < 4; c++ {
					out.Pix[o+c] = uint8(math.Max(0, math.Min(255, buf[i*4+c]+0.5)))
				}
			}
		}
	}
	pass(sx, true)
	pass(sy, false)
	return out
}

func boxSizes(sigma float64) [3]int {
	wIdeal := math.Sqrt(12*sigma*sigma/3 + 1)
	wl := int(math.Floor(wIdeal))
	if wl%2 == 0 {
		wl--
	}
	wu := wl + 2
	mIdeal := (12*sigma*sigma - 3*float64(wl*wl) - 12*float64(wl) - 9) / (-4*float64(wl) - 4)
	m := int(math.Round(mIdeal))
	var s [3]int
	for i := range s {
		if i < m {
			s[i] = wl
		} else {
			s[i] = wu
		}
	}
	return s
}

// boxBlur blurs 4-channel values with radius r (edges count as transparent).
func boxBlur(v, tmp []float64, n, r int) {
	if r <= 0 {
		return
	}
	k := 1 / float64(2*r+1)
	for c := 0; c < 4; c++ {
		sum := 0.0
		for i := -r; i <= r; i++ {
			if i >= 0 && i < n {
				sum += v[i*4+c]
			}
		}
		for i := 0; i < n; i++ {
			tmp[i*4+c] = sum * k
			if j := i + r + 1; j < n {
				sum += v[j*4+c]
			}
			if j := i - r; j >= 0 {
				sum -= v[j*4+c]
			}
		}
	}
	copy(v, tmp[:n*4])
}

func composite(a, b *image.RGBA, op string, k [4]float64) *image.RGBA {
	out := image.NewRGBA(a.Rect)
	for i := 0; i < len(a.Pix); i += 4 {
		aa := float64(a.Pix[i+3]) / 255
		ba := float64(b.Pix[i+3]) / 255
		for c := 0; c < 4; c++ {
			ac := float64(a.Pix[i+c]) / 255
			bc := float64(b.Pix[i+c]) / 255
			var v float64
			switch op {
			case "in":
				v = ac * ba
			case "out":
				v = ac * (1 - ba)
			case "atop":
				v = ac*ba + bc*(1-aa)
			case "xor":
				v = ac*(1-ba) + bc*(1-aa)
			case "arithmetic":
				v = k[0]*ac*bc + k[1]*ac + k[2]*bc + k[3]
			case "lighter":
				v = ac + bc
			default:
				v = ac + bc*(1-aa)
			}
			out.Pix[i+c] = uint8(clamp01(v)*255 + 0.5)
		}
		// Premultiplied colour never exceeds alpha.
		for c := 0; c < 3; c++ {
			if out.Pix[i+c] > out.Pix[i+3] {
				out.Pix[i+c] = out.Pix[i+3]
			}
		}
	}
	return out
}

func blendImages(a, b *image.RGBA, mode gfx.BlendMode) *image.RGBA {
	out := image.NewRGBA(a.Rect)
	for i := 0; i < len(a.Pix); i += 4 {
		d := [4]float64{float64(b.Pix[i]) / 255, float64(b.Pix[i+1]) / 255, float64(b.Pix[i+2]) / 255, float64(b.Pix[i+3]) / 255}
		s := [4]float64{float64(a.Pix[i]) / 255, float64(a.Pix[i+1]) / 255, float64(a.Pix[i+2]) / 255, float64(a.Pix[i+3]) / 255}
		gfx.BlendPixel(mode, &d, s)
		for k := 0; k < 4; k++ {
			out.Pix[i+k] = uint8(clamp01(d[k])*255 + 0.5)
		}
	}
	return out
}

func toLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func toSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// eachStraight applies fn to unpremultiplied colours.
func eachStraight(src *image.RGBA, fn func(c *[4]float64)) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := float64(src.Pix[i+3]) / 255
		var c [4]float64
		if a > 0 {
			for j := 0; j < 3; j++ {
				c[j] = float64(src.Pix[i+j]) / 255 / a
			}
		}
		c[3] = a
		fn(&c)
		na := clamp01(c[3])
		for j := 0; j < 3; j++ {
			out.Pix[i+j] = uint8(clamp01(c[j])*na*255 + 0.5)
		}
		out.Pix[i+3] = uint8(na*255 + 0.5)
	}
	return out
}

func colorMatrix(in *image.RGBA, p *Node) *image.RGBA {
	var m [20]float64
	vals := parseNumbers(p.Attr("values"))
	switch p.Attr("type") {
	case "saturate":
		s := 1.0
		if len(vals) > 0 {
			s = vals[0]
		}
		m = [20]float64{
			0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
			0, 0, 0, 1, 0}
	case "hueRotate":
		a := 0.0
		if len(vals) > 0 {
			a = vals[0] * math.Pi / 180
		}
		cs, sn := math.Cos(a), math.Sin(a)
		m = [20]float64{
			0.213 + cs*0.787 - sn*0.213, 0.715 - cs*0.715 - sn*0.715, 0.072 - cs*0.072 + sn*0.928, 0, 0,
			0.213 - cs*0.213 + sn*0.143, 0.715 + cs*0.285 + sn*0.140, 0.072 - cs*0.072 - sn*0.283, 0, 0,
			0.213 - cs*0.213 - sn*0.787, 0.715 - cs*0.715 + sn*0.715, 0.072 + cs*0.928 + sn*0.072, 0, 0,
			0, 0, 0, 1, 0}
	case "luminanceToAlpha":
		m = [20]float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.2125, 0.7154, 0.0721, 0, 0}
	default:
		if len(vals) != 20 {
			return in
		}
		copy(m[:], vals)
	}
	return eachStraight(in, func(c *[4]float64) {
		r, g, b, a := c[0], c[1], c[2], c[3]
		for row := 0; row < 4; row++ {
			c[row] = m[row*5]*r + m[row*5+1]*g + m[row*5+2]*b + m[row*5+3]*a + m[row*5+4]
		}
	})
}

func componentTransfer(in *image.RGBA, p *Node) *image.RGBA {
	var funcs [4]func(float64) float64
	for _, f := range p.Elements() {
		var ch int
		switch f.Name {
		case "feFuncR":
			ch = 0
		case "feFuncG":
			ch = 1
		case "feFuncB":
			ch = 2
		case "feFuncA":
			ch = 3
		default:
			continue
		}
		table := parseNumbers(f.Attr("tableValues"))
		slope := parseFloat(f.Attr("slope"), 1)
		intercept := parseFloat(f.Attr("intercept"), 0)
		amp := parseFloat(f.Attr("amplitude"), 1)
		exp := parseFloat(f.Attr("exponent"), 1)
		off := parseFloat(f.Attr("offset"), 0)
		switch f.Attr("type") {
		case "table":
			if len(table) == 0 {
				continue
			}
			funcs[ch] = func(v float64) float64 {
				n := len(table) - 1
				if n == 0 {
					return table[0]
				}
				k := int(math.Min(float64(n-1), math.Floor(v*float64(n))))
				return table[k] + (v*float64(n)-float64(k))*(table[k+1]-table[k])
			}
		case "discrete":
			if len(table) == 0 {
				continue
			}
			funcs[ch] = func(v float64) float64 {
				k := int(math.Min(float64(len(table)-1), math.Floor(v*float64(len(table)))))
				return table[k]
			}
		case "linear":
			funcs[ch] = func(v float64) float64 { return slope*v + intercept }
		case "gamma":
			funcs[ch] = func(v float64) float64 { return amp*math.Pow(v, exp) + off }
		}
	}
	return eachStraight(in, func(c *[4]float64) {
		for i, f := range funcs {
			if f != nil {
				c[i] = f(clamp01(c[i]))
			}
		}
	})
}

func morphology(src *image.RGBA, rx, ry int, dilate bool) *image.RGBA {
	pass := func(in *image.RGBA, r int, horizontal bool) *image.RGBA {
		out := image.NewRGBA(in.Rect)
		w, h := in.Rect.Dx(), in.Rect.Dy()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				for c := 0; c < 4; c++ {
					best := 0
					if !dilate {
						best = 255
					}
					for k := -r; k <= r; k++ {
						xx, yy := x, y
						if horizontal {
							xx += k
						} else {
							yy += k
						}
						v := 0
						if xx >= 0 && xx < w && yy >= 0 && yy < h {
							v = int(in.Pix[yy*in.Stride+xx*4+c])
						}
						if dilate && v > best || !dilate && v < best {
							best = v
						}
					}
					out.Pix[y*out.Stride+x*4+c] = uint8(best)
				}
			}
		}
		return out
	}
	out := src
	if rx > 0 {
		out = pass(out, rx, true)
	}
	if ry > 0 {
		out = pass(out, ry, false)
	}
	return out
}

// tile repeats the input's subregion across the result's subregion.
func tile(src *image.RGBA, from, to image.Rectangle) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	if from.Empty() {
		return out
	}
	tw, th := from.Dx(), from.Dy()
	for y := to.Min.Y; y < to.Max.Y; y++ {
		sy := from.Min.Y + ((y-from.Min.Y)%th+th)%th
		for x := to.Min.X; x < to.Max.X; x++ {
			sx := from.Min.X + ((x-from.Min.X)%tw+tw)%tw
			copy(out.Pix[y*out.Stride+x*4:y*out.Stride+x*4+4], src.Pix[sy*src.Stride+sx*4:sy*src.Stride+sx*4+4])
		}
	}
	return out
}

// feImage draws a referenced element, or a picture into the subregion.
func (fr *filterRun) feImage(p *Node) *image.RGBA {
	href := p.Attr("href")
	ras := gfx.NewRaster(fr.w, fr.h)
	if id, _, ok := urlRef(href); ok && strings.HasPrefix(strings.TrimSpace(href), "#") {
		target := fr.r.doc.ids[id]
		if target == nil {
			return ras.Image()
		}
		saved := fr.r.dev
		fr.r.dev = ras
		c := fr.c
		c.m = fr.m
		if fr.primBB {
			c.m = c.m.Mul(gfx.Translate(fr.bbox.X0, fr.bbox.Y0))
		}
		fr.r.drawNode(target, c)
		fr.r.dev = saved
		return ras.Image()
	}
	sub := fr.subregion(p, nil)
	user := gfx.Rect{X0: float64(sub.Min.X), Y0: float64(sub.Min.Y), X1: float64(sub.Max.X), Y1: float64(sub.Max.Y)}.Transform(fr.inv)
	img := &Node{Name: "image", Attrs: map[string]string{
		"href": href, "preserveAspectRatio": p.Attr("preserveAspectRatio"),
		"x": ftoa(user.X0), "y": ftoa(user.Y0), "width": ftoa(user.Width()), "height": ftoa(user.Height()),
	}}
	saved := fr.r.dev
	fr.r.dev = ras
	c := fr.c
	c.m = fr.m
	fr.r.drawImage(img, c)
	fr.r.dev = saved
	return ras.Image()
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func convolve(src *image.RGBA, p *Node) *image.RGBA {
	order := parseNumbers(p.Attr("order"))
	ox, oy := 3, 3
	if len(order) > 0 {
		ox, oy = int(order[0]), int(order[0])
	}
	if len(order) > 1 {
		oy = int(order[1])
	}
	kernel := parseNumbers(p.Attr("kernelMatrix"))
	if ox <= 0 || oy <= 0 || len(kernel) != ox*oy {
		return image.NewRGBA(src.Rect)
	}
	div := 0.0
	for _, k := range kernel {
		div += k
	}
	if p.Has("divisor") {
		div = parseFloat(p.Attr("divisor"), div)
	}
	if div == 0 {
		div = 1
	}
	bias := parseFloat(p.Attr("bias"), 0)
	tx := ox / 2
	if p.Has("targetX") {
		tx = int(parseFloat(p.Attr("targetX"), float64(tx)))
	}
	ty := oy / 2
	if p.Has("targetY") {
		ty = int(parseFloat(p.Attr("targetY"), float64(ty)))
	}
	if tx < 0 || tx >= ox || ty < 0 || ty >= oy {
		return image.NewRGBA(src.Rect)
	}
	edge := p.Attr("edgeMode")
	if edge == "" {
		edge = "duplicate"
	}
	preserve := p.Attr("preserveAlpha") == "true"
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(src.Rect)
	at := func(x, y, c int) float64 {
		switch edge {
		case "wrap":
			x = (x%w + w) % w
			y = (y%h + h) % h
		case "none":
			if x < 0 || y < 0 || x >= w || y >= h {
				return 0
			}
		default:
			x = min(max(x, 0), w-1)
			y = min(max(y, 0), h-1)
		}
		o := y*src.Stride + x*4
		v := float64(src.Pix[o+c]) / 255
		if preserve && c < 3 {
			if a := float64(src.Pix[o+3]) / 255; a > 0 {
				v /= a
			}
		}
		return v
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc [4]float64
			for j := 0; j < oy; j++ {
				for i := 0; i < ox; i++ {
					k := kernel[(oy-1-j)*ox+(ox-1-i)]
					for c := 0; c < 4; c++ {
						if c == 3 && preserve {
							continue
						}
						acc[c] += at(x-tx+i, y-ty+j, c) * k
					}
				}
			}
			o := y*out.Stride + x*4
			a := 0.0
			if preserve {
				a = float64(src.Pix[o+3]) / 255
			} else {
				a = clamp01(acc[3]/div + bias)
			}
			for c := 0; c < 3; c++ {
				v := acc[c]/div + bias
				if preserve {
					v = clamp01(v) * a
				} else {
					v = math.Min(clamp01(v), a)
				}
				out.Pix[o+c] = uint8(v*255 + 0.5)
			}
			out.Pix[o+3] = uint8(a*255 + 0.5)
		}
	}
	return out
}

func displace(src, mapImg *image.RGBA, scale float64, xc, yc string) *image.RGBA {
	ch := func(s string) int {
		switch s {
		case "R":
			return 0
		case "G":
			return 1
		case "B":
			return 2
		}
		return 3
	}
	cx, cy := ch(xc), ch(yc)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(src.Rect)
	val := func(o, c int) float64 {
		a := float64(mapImg.Pix[o+3]) / 255
		v := float64(mapImg.Pix[o+c]) / 255
		if c < 3 && a > 0 {
			v /= a
		}
		return v
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := y*mapImg.Stride + x*4
			sx := x + int(math.Round(scale*(val(o, cx)-0.5)))
			sy := y + int(math.Round(scale*(val(o, cy)-0.5)))
			if sx < 0 || sy < 0 || sx >= w || sy >= h {
				continue
			}
			copy(out.Pix[o:o+4], src.Pix[sy*src.Stride+sx*4:sy*src.Stride+sx*4+4])
		}
	}
	return out
}

// ---- turbulence (the reference algorithm of the SVG specification) ----

const (
	turbBSize   = 0x100
	turbBM      = 0xff
	turbPerlinN = 0x1000
	randM       = 2147483647
	randA       = 16807
	randQ       = 127773
	randR       = 2836
)

type turbulenceGen struct {
	lattice  [turbBSize + turbBSize + 2]int
	gradient [4][turbBSize + turbBSize + 2][2]float64
}

func turbRandom(seed int64) int64 {
	r := randA*(seed%randQ) - randR*(seed/randQ)
	if r <= 0 {
		r += randM
	}
	return r
}

func newTurbulence(seed int64) *turbulenceGen {
	t := &turbulenceGen{}
	if seed <= 0 {
		seed = -(seed % (randM - 1)) + 1
	}
	if seed > randM-1 {
		seed = randM - 1
	}
	var i int
	for k := 0; k < 4; k++ {
		for i = 0; i < turbBSize; i++ {
			t.lattice[i] = i
			for j := 0; j < 2; j++ {
				seed = turbRandom(seed)
				t.gradient[k][i][j] = float64((seed%(turbBSize+turbBSize))-turbBSize) / turbBSize
			}
			s := math.Sqrt(t.gradient[k][i][0]*t.gradient[k][i][0] + t.gradient[k][i][1]*t.gradient[k][i][1])
			if s != 0 {
				t.gradient[k][i][0] /= s
				t.gradient[k][i][1] /= s
			}
		}
	}
	for i--; i > 0; i-- {
		k := t.lattice[i]
		seed = turbRandom(seed)
		j := int(seed % turbBSize)
		t.lattice[i] = t.lattice[j]
		t.lattice[j] = k
	}
	for i = 0; i < turbBSize+2; i++ {
		t.lattice[turbBSize+i] = t.lattice[i]
		for k := 0; k < 4; k++ {
			for j := 0; j < 2; j++ {
				t.gradient[k][turbBSize+i][j] = t.gradient[k][i][j]
			}
		}
	}
	return t
}

type stitchInfo struct {
	width, height int
	wrapX, wrapY  int
}

func sCurve(t float64) float64 { return t * t * (3 - 2*t) }

func (t *turbulenceGen) noise2(ch int, vx, vy float64, st *stitchInfo) float64 {
	tt := vx + turbPerlinN
	bx0 := int(tt) & turbBM
	bx1 := (bx0 + 1) & turbBM
	rx0 := tt - float64(int(tt))
	rx1 := rx0 - 1
	tt = vy + turbPerlinN
	by0 := int(tt) & turbBM
	by1 := (by0 + 1) & turbBM
	ry0 := tt - float64(int(tt))
	ry1 := ry0 - 1
	if st != nil {
		if bx0 >= st.wrapX {
			bx0 -= st.width
		}
		if bx1 >= st.wrapX {
			bx1 -= st.width
		}
		if by0 >= st.wrapY {
			by0 -= st.height
		}
		if by1 >= st.wrapY {
			by1 -= st.height
		}
	}
	bx0 &= turbBM
	bx1 &= turbBM
	by0 &= turbBM
	by1 &= turbBM
	i := t.lattice[bx0]
	j := t.lattice[bx1]
	b00 := t.lattice[i+by0]
	b10 := t.lattice[j+by0]
	b01 := t.lattice[i+by1]
	b11 := t.lattice[j+by1]
	sx, sy := sCurve(rx0), sCurve(ry0)
	q := t.gradient[ch][b00]
	u := rx0*q[0] + ry0*q[1]
	q = t.gradient[ch][b10]
	v := rx1*q[0] + ry0*q[1]
	a := u + sx*(v-u)
	q = t.gradient[ch][b01]
	u = rx0*q[0] + ry1*q[1]
	q = t.gradient[ch][b11]
	v = rx1*q[0] + ry1*q[1]
	b := u + sx*(v-u)
	return a + sy*(b-a)
}

func (t *turbulenceGen) turbulence(ch int, px, py, fx, fy float64, octaves int, fractal, stitch bool, tileX, tileY, tileW, tileH float64) float64 {
	var st *stitchInfo
	if stitch {
		if fx != 0 {
			lo := math.Floor(tileW*fx) / tileW
			hi := math.Ceil(tileW*fx) / tileW
			if lo > 0 && fx/lo < hi/fx {
				fx = lo
			} else {
				fx = hi
			}
		}
		if fy != 0 {
			lo := math.Floor(tileH*fy) / tileH
			hi := math.Ceil(tileH*fy) / tileH
			if lo > 0 && fy/lo < hi/fy {
				fy = lo
			} else {
				fy = hi
			}
		}
		st = &stitchInfo{}
		st.width = int(tileW*fx + 0.5)
		st.wrapX = int(tileX*fx + turbPerlinN + float64(st.width))
		st.height = int(tileH*fy + 0.5)
		st.wrapY = int(tileY*fy + turbPerlinN + float64(st.height))
	}
	sum := 0.0
	vx, vy := px*fx, py*fy
	ratio := 1.0
	for o := 0; o < octaves; o++ {
		n := t.noise2(ch, vx, vy, st)
		if fractal {
			sum += n / ratio
		} else {
			sum += math.Abs(n) / ratio
		}
		vx *= 2
		vy *= 2
		ratio *= 2
		if st != nil {
			st.width *= 2
			st.wrapX = 2*st.wrapX - turbPerlinN
			st.height *= 2
			st.wrapY = 2*st.wrapY - turbPerlinN
		}
	}
	return sum
}

func (fr *filterRun) turbulence(p *Node) *image.RGBA {
	freq := parseNumbers(p.Attr("baseFrequency"))
	fx, fy := 0.0, 0.0
	if len(freq) > 0 {
		fx, fy = freq[0], freq[0]
	}
	if len(freq) > 1 {
		fy = freq[1]
	}
	out := fr.blank()
	if fx < 0 || fy < 0 || len(freq) > 2 {
		return out
	}
	octaves := int(parseFloat(p.Attr("numOctaves"), 1))
	seed := int64(math.Round(parseFloat(p.Attr("seed"), 0)))
	fractal := p.Attr("type") == "fractalNoise"
	stitch := p.Attr("stitchTiles") == "stitch"
	gen := newTurbulence(seed)
	sub := fr.subregion(p, nil)
	tile := gfx.Rect{X0: float64(sub.Min.X), Y0: float64(sub.Min.Y), X1: float64(sub.Max.X), Y1: float64(sub.Max.Y)}.Transform(fr.inv)
	for y := sub.Min.Y; y < sub.Max.Y; y++ {
		for x := sub.Min.X; x < sub.Max.X; x++ {
			u := fr.inv.Apply(gfx.Point{X: float64(x), Y: float64(y)})
			var c [4]float64
			for ch := 0; ch < 4; ch++ {
				v := gen.turbulence(ch, u.X, u.Y, fx, fy, octaves, fractal, stitch, tile.X0, tile.Y0, tile.Width(), tile.Height())
				if fractal {
					v = (v*255 + 255) / 2
				} else {
					v *= 255
				}
				c[ch] = math.Max(0, math.Min(255, v))
			}
			o := y*out.Stride + x*4
			a := c[3] / 255
			out.Pix[o] = uint8(c[0]*a + 0.5)
			out.Pix[o+1] = uint8(c[1]*a + 0.5)
			out.Pix[o+2] = uint8(c[2]*a + 0.5)
			out.Pix[o+3] = uint8(c[3] + 0.5)
		}
	}
	return out
}

// ---- lighting ----

func (fr *filterRun) lighting(src *image.RGBA, p *Node, st *style) *image.RGBA {
	var light *Node
	for _, l := range p.Elements() {
		if l.Name == "feDistantLight" || l.Name == "fePointLight" || l.Name == "feSpotLight" {
			light = l
			break
		}
	}
	out := fr.blank()
	if light == nil {
		return out
	}
	diffuse := p.Name == "feDiffuseLighting"
	surface := parseFloat(p.Attr("surfaceScale"), 1)
	kd := parseFloat(p.Attr("diffuseConstant"), 1)
	ks := parseFloat(p.Attr("specularConstant"), 1)
	exp := parseFloat(p.Attr("specularExponent"), 1)
	if !diffuse && (exp < 1 || exp > 128) {
		return out
	}
	col := st.lightingColor
	if fr.linear {
		col.R, col.G, col.B = toLinear(col.R), toLinear(col.G), toLinear(col.B)
	}
	w, h := fr.w, fr.h
	alpha := func(x, y int) float64 {
		x = min(max(x, 0), w-1)
		y = min(max(y, 0), h-1)
		return float64(src.Pix[y*src.Stride+x*4+3]) / 255
	}
	scale := (fr.sx + fr.sy) / 2
	// Light positions in user space → pixels; z scales like x.
	lp := func(prefix string) (float64, float64, float64) {
		pt := fr.m.Apply(gfx.Point{X: parseFloat(light.Attr(prefix+"x"), 0), Y: parseFloat(light.Attr(prefix+"y"), 0)})
		return pt.X, pt.Y, parseFloat(light.Attr(prefix+"z"), 0) * scale
	}
	var lx, ly, lz, sx, sy, sz float64
	az := parseFloat(light.Attr("azimuth"), 0) * math.Pi / 180
	el := parseFloat(light.Attr("elevation"), 0) * math.Pi / 180
	if light.Name != "feDistantLight" {
		lx, ly, lz = lp("")
	}
	var spotDir [3]float64
	cone := -1.0
	spotExp := parseFloat(light.Attr("specularExponent"), 1)
	if light.Name == "feSpotLight" {
		pt := fr.m.Apply(gfx.Point{X: parseFloat(light.Attr("pointsAtX"), 0), Y: parseFloat(light.Attr("pointsAtY"), 0)})
		sx, sy, sz = pt.X, pt.Y, parseFloat(light.Attr("pointsAtZ"), 0)*scale
		d := [3]float64{sx - lx, sy - ly, sz - lz}
		l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
		if l > 0 {
			spotDir = [3]float64{d[0] / l, d[1] / l, d[2] / l}
		}
		if light.Has("limitingConeAngle") {
			cone = math.Cos(math.Abs(parseFloat(light.Attr("limitingConeAngle"), 90)) * math.Pi / 180)
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx := -surface / 4 * ((alpha(x+1, y-1) + 2*alpha(x+1, y) + alpha(x+1, y+1)) - (alpha(x-1, y-1) + 2*alpha(x-1, y) + alpha(x-1, y+1)))
			ny := -surface / 4 * ((alpha(x-1, y+1) + 2*alpha(x, y+1) + alpha(x+1, y+1)) - (alpha(x-1, y-1) + 2*alpha(x, y-1) + alpha(x+1, y-1)))
			nl := math.Sqrt(nx*nx + ny*ny + 1)
			n := [3]float64{nx / nl, ny / nl, 1 / nl}
			z := surface * alpha(x, y)
			var L [3]float64
			if light.Name == "feDistantLight" {
				L = [3]float64{math.Cos(az) * math.Cos(el), math.Sin(az) * math.Cos(el), math.Sin(el)}
			} else {
				d := [3]float64{lx - float64(x), ly - float64(y), lz - z}
				l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
				if l > 0 {
					L = [3]float64{d[0] / l, d[1] / l, d[2] / l}
				}
			}
			lc := [3]float64{col.R, col.G, col.B}
			if light.Name == "feSpotLight" {
				minusLS := -(L[0]*spotDir[0] + L[1]*spotDir[1] + L[2]*spotDir[2])
				if minusLS <= 0 || (cone >= 0 && minusLS < cone) {
					lc = [3]float64{}
				} else {
					f := math.Pow(minusLS, spotExp)
					lc = [3]float64{lc[0] * f, lc[1] * f, lc[2] * f}
				}
			}
			o := y*out.Stride + x*4
			if diffuse {
				nDotL := n[0]*L[0] + n[1]*L[1] + n[2]*L[2]
				k := kd * nDotL
				out.Pix[o] = uint8(clamp01(k*lc[0])*255 + 0.5)
				out.Pix[o+1] = uint8(clamp01(k*lc[1])*255 + 0.5)
				out.Pix[o+2] = uint8(clamp01(k*lc[2])*255 + 0.5)
				out.Pix[o+3] = 255
			} else {
				hv := [3]float64{L[0], L[1], L[2] + 1}
				hl := math.Sqrt(hv[0]*hv[0] + hv[1]*hv[1] + hv[2]*hv[2])
				nDotH := 0.0
				if hl > 0 {
					nDotH = (n[0]*hv[0] + n[1]*hv[1] + n[2]*hv[2]) / hl
				}
				k := ks * math.Pow(math.Max(0, nDotH), exp)
				r, g, b := clamp01(k*lc[0]), clamp01(k*lc[1]), clamp01(k*lc[2])
				a := math.Max(r, math.Max(g, b))
				out.Pix[o] = uint8(r*255 + 0.5)
				out.Pix[o+1] = uint8(g*255 + 0.5)
				out.Pix[o+2] = uint8(b*255 + 0.5)
				out.Pix[o+3] = uint8(a*255 + 0.5)
			}
		}
	}
	return out
}

// cssFilterFunction turns one CSS filter function into a filter element.
func cssFilterFunction(name, args string) *Node {
	f := &Node{Name: "filter", Attrs: map[string]string{}}
	add := func(n *Node) {
		n.Parent = f
		f.Children = append(f.Children, n)
	}
	amount := func(def float64) float64 {
		a := strings.TrimSpace(args)
		if a == "" {
			return def
		}
		if strings.HasSuffix(a, "%") {
			return parseFloat(strings.TrimSuffix(a, "%"), def*100) / 100
		}
		return parseFloat(a, def)
	}
	srgb := map[string]string{"color-interpolation-filters": "sRGB"}
	withSRGB := func(a map[string]string) map[string]string {
		for k, v := range srgb {
			a[k] = v
		}
		return a
	}
	switch name {
	case "blur":
		l, ok := parseLength(args)
		if args != "" && (!ok || l.Unit == "%" || l.V < 0) {
			return nil
		}
		sd := (lengthContext{fontSize: 16}).resolve(l, axisOther)
		add(&Node{Name: "feGaussianBlur", Attrs: map[string]string{"stdDeviation": ftoa(sd)}})
	case "drop-shadow":
		fields := strings.Fields(strings.ReplaceAll(args, ",", " "))
		var lens []float64
		color := "currentColor"
		for _, fl := range fields {
			if l, ok := parseLength(fl); ok {
				lens = append(lens, (lengthContext{fontSize: 16}).resolve(l, axisOther))
			} else {
				color = fl
			}
		}
		if len(lens) < 2 || len(lens) > 3 {
			return nil
		}
		a := map[string]string{"flood-color": color, "dx": ftoa(lens[0]), "dy": ftoa(lens[1]), "stdDeviation": "0"}
		if len(lens) > 2 {
			a["stdDeviation"] = ftoa(lens[2] / 2)
		}
		add(&Node{Name: "feDropShadow", Attrs: withSRGB(a)})
	case "grayscale":
		add(&Node{Name: "feColorMatrix", Attrs: withSRGB(map[string]string{"type": "saturate", "values": ftoa(1 - clamp01(amount(1)))})})
	case "sepia":
		a := clamp01(amount(1))
		m := []float64{
			0.393 + 0.607*(1-a), 0.769 - 0.769*(1-a), 0.189 - 0.189*(1-a), 0, 0,
			0.349 - 0.349*(1-a), 0.686 + 0.314*(1-a), 0.168 - 0.168*(1-a), 0, 0,
			0.272 - 0.272*(1-a), 0.534 - 0.534*(1-a), 0.131 + 0.869*(1-a), 0, 0,
			0, 0, 0, 1, 0}
		add(&Node{Name: "feColorMatrix", Attrs: withSRGB(map[string]string{"type": "matrix", "values": joinNums(m)})})
	case "saturate":
		add(&Node{Name: "feColorMatrix", Attrs: withSRGB(map[string]string{"type": "saturate", "values": ftoa(math.Max(0, amount(1)))})})
	case "hue-rotate":
		a := strings.TrimSpace(args)
		deg := 0.0
		switch {
		case a == "" || a == "0":
		case strings.HasSuffix(a, "deg"):
			deg = parseFloat(strings.TrimSuffix(a, "deg"), 0)
		case strings.HasSuffix(a, "grad"):
			deg = parseFloat(strings.TrimSuffix(a, "grad"), 0) * 0.9
		case strings.HasSuffix(a, "rad"):
			deg = parseFloat(strings.TrimSuffix(a, "rad"), 0) * 180 / math.Pi
		case strings.HasSuffix(a, "turn"):
			deg = parseFloat(strings.TrimSuffix(a, "turn"), 0) * 360
		default:
			return nil
		}
		add(&Node{Name: "feColorMatrix", Attrs: withSRGB(map[string]string{"type": "hueRotate", "values": ftoa(deg)})})
	case "invert":
		a := clamp01(amount(1))
		ft := ftoa(a) + " " + ftoa(1-a)
		ct := &Node{Name: "feComponentTransfer", Attrs: withSRGB(map[string]string{})}
		for _, c := range []string{"feFuncR", "feFuncG", "feFuncB"} {
			fn := &Node{Name: c, Attrs: map[string]string{"type": "table", "tableValues": ft}, Parent: ct}
			ct.Children = append(ct.Children, fn)
		}
		add(ct)
	case "opacity":
		ct := &Node{Name: "feComponentTransfer", Attrs: withSRGB(map[string]string{})}
		ct.Children = append(ct.Children, &Node{Name: "feFuncA", Attrs: map[string]string{"type": "table", "tableValues": "0 " + ftoa(clamp01(amount(1)))}, Parent: ct})
		add(ct)
	case "brightness", "contrast":
		a := math.Max(0, amount(1))
		ct := &Node{Name: "feComponentTransfer", Attrs: withSRGB(map[string]string{})}
		for _, c := range []string{"feFuncR", "feFuncG", "feFuncB"} {
			attrs := map[string]string{"type": "linear", "slope": ftoa(a)}
			if name == "contrast" {
				attrs["intercept"] = ftoa(-(0.5 * a) + 0.5)
			}
			ct.Children = append(ct.Children, &Node{Name: c, Attrs: attrs, Parent: ct})
		}
		add(ct)
	default:
		return nil
	}
	return f
}

func joinNums(v []float64) string {
	s := make([]string, len(v))
	for i, x := range v {
		s[i] = ftoa(x)
	}
	return strings.Join(s, " ")
}

// cssClipShape would read CSS basic shapes; they are left unclipped.
func cssClipShape(v string, lc lengthContext) *gfx.Path { return nil }
