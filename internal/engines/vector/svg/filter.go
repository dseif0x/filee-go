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
// cannot express them, Skia's PDF output rasterizes them the same way.

const maxFilterSide = 4096

func (r *renderer) drawFiltered(n, f *Node, c context, draw func(context)) {
	if !r.enter(f) {
		draw(c)
		return
	}
	defer r.leave(f)
	pst := c.pst
	if pst == nil {
		pst = defaultStyle()
	}
	bbox := r.bbox(n, context{m: gfx.Identity, st: pst, lc: c.plc}, false)
	var region gfx.Rect
	if f.Attr("filterUnits") == "userSpaceOnUse" {
		x := c.lc.resolveAttr(f, "x", axisX, c.lc.resolve(Length{-10, "%"}, axisX))
		y := c.lc.resolveAttr(f, "y", axisY, c.lc.resolve(Length{-10, "%"}, axisY))
		w := c.lc.resolveAttr(f, "width", axisX, c.lc.resolve(Length{120, "%"}, axisX))
		h := c.lc.resolveAttr(f, "height", axisY, c.lc.resolve(Length{120, "%"}, axisY))
		region = gfx.Rect{X0: x, Y0: y, X1: x + w, Y1: y + h}
	} else {
		if bbox.Empty() || bbox.Width() <= 0 && bbox.Height() <= 0 {
			return
		}
		frac := func(name string, def float64) float64 {
			v, ok := f.Attrs[name]
			if !ok {
				return def
			}
			return parseFloat(v, def)
		}
		x := bbox.X0 + frac("x", -0.1)*bbox.Width()
		y := bbox.Y0 + frac("y", -0.1)*bbox.Height()
		region = gfx.Rect{X0: x, Y0: y, X1: x + frac("width", 1.2)*bbox.Width(), Y1: y + frac("height", 1.2)*bbox.Height()}
	}
	if region.Empty() {
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

	// User units to filter pixels.
	scaleX := math.Hypot(cc.m.A, cc.m.B)
	scaleY := math.Hypot(cc.m.C, cc.m.D)
	primUnitsBB := f.Attr("primitiveUnits") == "objectBoundingBox"
	if primUnitsBB {
		scaleX *= bbox.Width()
		scaleY *= bbox.Height()
	}
	fs := &filterRun{src: off.Image(), results: map[string]*image.RGBA{}, sx: scaleX, sy: scaleY, w: w, h: h, linear: true}
	fst := computeStyle(f, c.st, c.lc)
	out := fs.src
	for _, p := range f.Elements() {
		pst := computeStyle(p, fst, c.lc)
		fs.linear = true
		if v, ok := spec(p, "color-interpolation-filters"); ok {
			fs.linear = v != "sRGB" && v != "srgb"
		} else if v, ok := spec(f, "color-interpolation-filters"); ok {
			fs.linear = v != "sRGB" && v != "srgb"
		}
		res := fs.primitive(p, pst)
		if res == nil {
			continue
		}
		out = res
		fs.last = res
		if name := p.Attr("result"); name != "" {
			fs.results[name] = res
		}
	}
	r.dev.DrawImage(out, gfx.Translate(dev.X0, dev.Y0).Mul(gfx.Scale(1/res, 1/res)), 1)
}

type filterRun struct {
	src     *image.RGBA
	last    *image.RGBA
	results map[string]*image.RGBA
	sx, sy  float64
	w, h    int
	linear  bool
}

func (fs *filterRun) blank() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, fs.w, fs.h)) }

func (fs *filterRun) input(name string) *image.RGBA {
	switch name {
	case "SourceGraphic":
		return fs.src
	case "SourceAlpha":
		a := fs.blank()
		for i := 3; i < len(a.Pix); i += 4 {
			a.Pix[i] = fs.src.Pix[i]
		}
		return a
	case "BackgroundImage", "BackgroundAlpha", "FillPaint", "StrokePaint":
		return fs.blank()
	case "":
		if fs.last != nil {
			return fs.last
		}
		return fs.src
	}
	if r, ok := fs.results[name]; ok {
		return r
	}
	if fs.last != nil {
		return fs.last
	}
	return fs.src
}

func (fs *filterRun) primitive(p *Node, st *style) *image.RGBA {
	in := fs.input(p.Attr("in"))
	switch p.Name {
	case "feGaussianBlur":
		sd := parseNumbers(p.Attr("stdDeviation"))
		sx, sy := 0.0, 0.0
		if len(sd) > 0 {
			sx, sy = sd[0], sd[0]
		}
		if len(sd) > 1 {
			sy = sd[1]
		}
		return blur(in, sx*fs.sx, sy*fs.sy)
	case "feOffset":
		return offset(in, parseFloat(p.Attr("dx"), 0)*fs.sx, parseFloat(p.Attr("dy"), 0)*fs.sy)
	case "feFlood":
		out := fs.blank()
		col := st.floodColor
		col.A *= st.floodOpacity
		fillImage(out, col)
		return out
	case "feMerge":
		out := fs.blank()
		for _, mn := range p.Elements() {
			if mn.Name == "feMergeNode" {
				over(out, fs.input(mn.Attr("in")))
			}
		}
		return out
	case "feComposite":
		in2 := fs.input(p.Attr("in2"))
		k := [4]float64{parseFloat(p.Attr("k1"), 0), parseFloat(p.Attr("k2"), 0), parseFloat(p.Attr("k3"), 0), parseFloat(p.Attr("k4"), 0)}
		return composite(in, in2, p.Attr("operator"), k)
	case "feBlend":
		return blendImages(in, fs.input(p.Attr("in2")), p.Attr("mode"))
	case "feColorMatrix":
		return fs.colorMatrix(in, p)
	case "feComponentTransfer":
		return fs.componentTransfer(in, p)
	case "feDropShadow":
		sd := parseNumbers(p.Attr("stdDeviation"))
		s := 2.0
		if len(sd) > 0 {
			s = sd[0]
		}
		dx, dy := parseFloat(p.Attr("dx"), 2), parseFloat(p.Attr("dy"), 2)
		shadow := blur(offset(in, dx*fs.sx, dy*fs.sy), s*fs.sx, s*fs.sy)
		col := st.floodColor
		col.A *= st.floodOpacity
		cr, cg, cb, ca := premul8(col)
		for i := 0; i < len(shadow.Pix); i += 4 {
			a := float64(shadow.Pix[i+3]) / 255
			shadow.Pix[i], shadow.Pix[i+1], shadow.Pix[i+2], shadow.Pix[i+3] = uint8(cr*a), uint8(cg*a), uint8(cb*a), uint8(ca*a)
		}
		over(shadow, in)
		return shadow
	case "feMorphology":
		rad := parseNumbers(p.Attr("radius"))
		rx, ry := 0.0, 0.0
		if len(rad) > 0 {
			rx, ry = rad[0], rad[0]
		}
		if len(rad) > 1 {
			ry = rad[1]
		}
		return morphology(in, int(math.Round(rx*fs.sx)), int(math.Round(ry*fs.sy)), p.Attr("operator") == "dilate")
	case "feTile", "feImage", "feTurbulence", "feDiffuseLighting", "feSpecularLighting", "feDisplacementMap", "feConvolveMatrix":
		// Not drawn: the input passes through unchanged.
		return in
	}
	return nil
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

func blendImages(a, b *image.RGBA, mode string) *image.RGBA {
	out := image.NewRGBA(a.Rect)
	for i := 0; i < len(a.Pix); i += 4 {
		qa := float64(a.Pix[i+3]) / 255
		qb := float64(b.Pix[i+3]) / 255
		for c := 0; c < 3; c++ {
			ca := float64(a.Pix[i+c]) / 255
			cb := float64(b.Pix[i+c]) / 255
			var v float64
			switch mode {
			case "multiply":
				v = (1-qa)*cb + (1-qb)*ca + ca*cb
			case "screen":
				v = cb + ca - ca*cb
			case "darken":
				v = math.Min((1-qa)*cb+ca, (1-qb)*ca+cb)
			case "lighten":
				v = math.Max((1-qa)*cb+ca, (1-qb)*ca+cb)
			default:
				v = (1-qa)*cb + ca
			}
			out.Pix[i+c] = uint8(clamp01(v)*255 + 0.5)
		}
		out.Pix[i+3] = uint8(clamp01(1-(1-qa)*(1-qb))*255 + 0.5)
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

// eachStraight applies fn to unpremultiplied colours (in linearRGB when asked).
func (fs *filterRun) eachStraight(src *image.RGBA, fn func(c *[4]float64)) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := float64(src.Pix[i+3]) / 255
		var c [4]float64
		if a > 0 {
			for j := 0; j < 3; j++ {
				c[j] = float64(src.Pix[i+j]) / 255 / a
				if fs.linear {
					c[j] = toLinear(clamp01(c[j]))
				}
			}
		}
		c[3] = a
		fn(&c)
		na := clamp01(c[3])
		for j := 0; j < 3; j++ {
			v := clamp01(c[j])
			if fs.linear {
				v = toSRGB(v)
			}
			out.Pix[i+j] = uint8(v*na*255 + 0.5)
		}
		out.Pix[i+3] = uint8(na*255 + 0.5)
	}
	return out
}

func (fs *filterRun) colorMatrix(in *image.RGBA, p *Node) *image.RGBA {
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
	return fs.eachStraight(in, func(c *[4]float64) {
		r, g, b, a := c[0], c[1], c[2], c[3]
		for row := 0; row < 4; row++ {
			c[row] = m[row*5]*r + m[row*5+1]*g + m[row*5+2]*b + m[row*5+3]*a + m[row*5+4]
		}
	})
}

func (fs *filterRun) componentTransfer(in *image.RGBA, p *Node) *image.RGBA {
	var funcs [4]func(float64) float64
	for _, f := range p.Elements() {
		idx := strings.Index("feFuncR feFuncG feFuncB feFuncA", f.Name)
		if idx < 0 || len(f.Name) != 7 {
			continue
		}
		ch := idx / 8
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
	return fs.eachStraight(in, func(c *[4]float64) {
		for i, f := range funcs {
			if f != nil {
				c[i] = f(clamp01(c[i]))
			}
		}
	})
}

func morphology(src *image.RGBA, rx, ry int, dilate bool) *image.RGBA {
	if rx <= 0 && ry <= 0 {
		return src
	}
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

// cssFilterNode turns CSS filter functions (blur, drop-shadow) into a filter element.
func cssFilterNode(v string) *Node {
	f := &Node{Name: "filter", Attrs: map[string]string{}}
	rest := strings.TrimSpace(v)
	for rest != "" {
		open := strings.IndexByte(rest, '(')
		end := strings.IndexByte(rest, ')')
		if open < 0 || end < open {
			break
		}
		name := strings.TrimSpace(rest[:open])
		args := rest[open+1 : end]
		rest = strings.TrimSpace(rest[end+1:])
		switch name {
		case "blur":
			l, _ := parseLength(args)
			f.Children = append(f.Children, &Node{Name: "feGaussianBlur", Attrs: map[string]string{"stdDeviation": strconv.FormatFloat(l.V, 'g', -1, 64)}, Parent: f})
		case "drop-shadow":
			fields := strings.Fields(args)
			var nums []string
			color := "black"
			for _, fl := range fields {
				if _, ok := parseLength(fl); ok {
					nums = append(nums, strings.TrimSuffix(fl, "px"))
				} else {
					color = fl
				}
			}
			a := map[string]string{"flood-color": color}
			if len(nums) > 0 {
				a["dx"] = nums[0]
			}
			if len(nums) > 1 {
				a["dy"] = nums[1]
			}
			if len(nums) > 2 {
				sd, _ := strconv.ParseFloat(nums[2], 64)
				a["stdDeviation"] = strconv.FormatFloat(sd/2, 'g', -1, 64)
			} else {
				a["stdDeviation"] = "0"
			}
			f.Children = append(f.Children, &Node{Name: "feDropShadow", Attrs: a, Parent: f})
		}
	}
	if len(f.Children) == 0 {
		return nil
	}
	return f
}

// cssClipShape would read CSS basic shapes; they are left unclipped.
func cssClipShape(v string, lc lengthContext) *gfx.Path { return nil }
