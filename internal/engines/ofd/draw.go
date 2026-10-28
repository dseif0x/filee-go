package ofd

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// Drawing a seal OFD (the red oval of 全国统一发票监制章, drawn with paths
// and rotated characters) on a transparent PNG at its own size: paths are
// filled and stroked with golang.org/x/image/vector, characters are drawn
// from a CJK font of the system (none found: the seal keeps its paths only).

var fileCounter atomic.Int64

func (r *reader) newMediaName(ext string) string {
	if err := os.MkdirAll(r.media, 0o755); err != nil {
		return ""
	}
	return filepath.Join(r.media, fmt.Sprintf("ofd-%d-%d.%s", os.Getpid(), fileCounter.Add(1), ext))
}

// toPNG decodes a picture of kind (TIFF, JPEG 2000) with the image engine
// and writes it as PNG; "" when it cannot.
func toPNG(b []byte, kind, path string) string {
	// The image engine registers its decoders (JPEG 2000 too) with package image.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > maxPicturePixels {
		return ""
	}
	pic, err := images.DecodeBytes(b, kind)
	if err != nil || len(pic.Frames) == 0 {
		return ""
	}
	img := pic.Frames[0].Img
	f, err := os.Create(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if png.Encode(f, img) != nil {
		os.Remove(path)
		return ""
	}
	return path
}

func (r *reader) drawSeal(zipBytes []byte) string {
	p, err := memoryPackage(zipBytes, maxSealPackageBytes)
	if err != nil {
		return ""
	}
	sr := &reader{pkg: p, media: r.media, extracted: map[string]string{}, templates: map[string]*template{}}
	ofd := sr.xml("OFD.xml")
	docPath, ok := resolve("", child(child(ofd, "DocBody"), "DocRoot").Value())
	if ofd == nil || !ok {
		return ""
	}
	document := sr.xml(docPath)
	if document == nil {
		return ""
	}
	docDir := dirName(docPath)
	common := child(document, "CommonData")
	res := newResources(nil)
	for _, n := range append(children(common, "PublicRes"), children(common, "DocumentRes")...) {
		sr.loadRes(res, resolveOr(docDir, n.Value()), docDir)
	}
	pagePath := pagePath(docDir, attr(child(child(document, "Pages"), "Page"), "BaseLoc"))
	page := sr.xml(pagePath)
	if page == nil {
		return ""
	}
	pageRes := sr.pageResources(page, pagePath, docDir, res)
	area, ok := pageArea(child(page, "Area"))
	if !ok {
		area, _ = pageArea(child(common, "PageArea"))
	}
	if area[0] < 1 || area[0] > maxSealSize || area[1] < 1 || area[1] > maxSealSize {
		return ""
	}
	c := &canvas{img: image.NewRGBA(image.Rect(0, 0, int(math.Ceil(area[0]*sealScale)), int(math.Ceil(area[1]*sealScale))))}
	objects := 0
	sr.drawContent(c, child(page, "Content"), pageRes, "", 0, &objects)
	file := r.newMediaName("png")
	f, err := os.Create(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	if png.Encode(f, c.img) != nil {
		return ""
	}
	return file
}

type canvas struct{ img *image.RGBA }

// objectMatrix maps an object's space to pixels: its boundary's corner, then its CTM.
func objectMatrix(el *xdom.Node) matrix {
	b, _ := box(attr(el, "Boundary"))
	m := parseMatrix(attr(el, "CTM"))
	// pixel = scale · (box + ctm(p))
	return matrix{m.a * sealScale, m.b * sealScale, m.c * sealScale, m.d * sealScale, (m.e + b[0]) * sealScale, (m.f + b[1]) * sealScale}
}

func (r *reader) drawContent(c *canvas, container *xdom.Node, res *resources, layerDraw string, depth int, objects *int) {
	if container == nil || depth > maxDepth {
		return
	}
	for _, el := range container.Elements() {
		*objects++
		if attr(el, "Visible") == "false" || *objects > maxSealObjects {
			continue
		}
		switch el.Name.Local {
		case "Layer":
			ld := attr(el, "DrawParam")
			if ld == "" {
				ld = layerDraw
			}
			r.drawContent(c, el, res, ld, depth+1, objects)
		case "PageBlock":
			r.drawContent(c, el, res, layerDraw, depth+1, objects)
		case "TextObject":
			drawText(c, el, res, layerDraw)
		case "PathObject":
			drawPath(c, el, res, layerDraw)
		case "ImageObject":
			r.drawImage(c, el, res)
		}
	}
}

func rgba(hex string, def color.RGBA) color.RGBA {
	if len(hex) != 7 || hex[0] != '#' {
		return def
	}
	var v uint32
	if _, err := fmt.Sscanf(hex[1:], "%06x", &v); err != nil {
		return def
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// path is a list of subpaths of points (curves flattened), in pixels.
type path [][]point

type pathBuilder struct {
	m        matrix
	p        path
	cur, beg point
}

func (b *pathBuilder) at(x, y float64) point {
	px, py := b.m.apply(x, y)
	return point{px, py}
}

func (b *pathBuilder) moveTo(x, y float64) {
	b.cur = point{x, y}
	b.beg = b.cur
	b.p = append(b.p, []point{b.at(x, y)})
}

func (b *pathBuilder) lineTo(x, y float64) {
	if len(b.p) == 0 {
		b.moveTo(b.cur.x, b.cur.y)
	}
	b.cur = point{x, y}
	b.p[len(b.p)-1] = append(b.p[len(b.p)-1], b.at(x, y))
}

func (b *pathBuilder) cubicTo(x1, y1, x2, y2, x, y float64) {
	p0 := b.cur
	for i := 1; i <= 16; i++ {
		t := float64(i) / 16
		u := 1 - t
		b.lineTo(u*u*u*p0.x+3*u*u*t*x1+3*u*t*t*x2+t*t*t*x, u*u*u*p0.y+3*u*u*t*y1+3*u*t*t*y2+t*t*t*y)
	}
}

func (b *pathBuilder) quadTo(x1, y1, x, y float64) {
	p0 := b.cur
	b.cubicTo(p0.x+2.0/3*(x1-p0.x), p0.y+2.0/3*(y1-p0.y), x+2.0/3*(x1-x), y+2.0/3*(y1-y), x, y)
}

// arcTo is an SVG-style elliptical arc (endpoint parameterization).
func (b *pathBuilder) arcTo(rx, ry, rotation float64, large, sweep bool, x, y float64) {
	x0, y0 := b.cur.x, b.cur.y
	if rx == 0 || ry == 0 || x0 == x && y0 == y {
		b.lineTo(x, y)
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	phi := rotation * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (x0-x)/2, (y0-y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		rx, ry = rx*math.Sqrt(l), ry*math.Sqrt(l)
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cx1, cy1 := co*rx*y1/ry, -co*ry*x1/rx
	cx := cos*cx1 - sin*cy1 + (x0+x)/2
	cy := sin*cx1 + cos*cy1 + (y0+y)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := angle(1, 0, (x1-cx1)/rx, (y1-cy1)/ry)
	dt := angle((x1-cx1)/rx, (y1-cy1)/ry, (-x1-cx1)/rx, (-y1-cy1)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := max(4, int(math.Ceil(math.Abs(dt)/(math.Pi/32))))
	for i := 1; i <= n; i++ {
		t := t1 + dt*float64(i)/float64(n)
		px := cx + rx*math.Cos(t)*cos - ry*math.Sin(t)*sin
		py := cy + rx*math.Cos(t)*sin + ry*math.Sin(t)*cos
		b.lineTo(px, py)
	}
	b.cur = point{x, y}
}

func (b *pathBuilder) close() {
	if len(b.p) > 0 && len(b.p[len(b.p)-1]) > 0 {
		b.p[len(b.p)-1] = append(b.p[len(b.p)-1], b.p[len(b.p)-1][0])
	}
	b.cur = b.beg
}

func (c *canvas) mask() *vector.Rasterizer {
	s := c.img.Bounds().Size()
	return vector.NewRasterizer(s.X, s.Y)
}

func (c *canvas) paint(r *vector.Rasterizer, col color.RGBA) {
	r.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{})
}

func fillPath(c *canvas, p path, col color.RGBA) {
	r := c.mask()
	for _, sp := range p {
		if len(sp) < 3 {
			continue
		}
		r.MoveTo(float32(sp[0].x), float32(sp[0].y))
		for _, q := range sp[1:] {
			r.LineTo(float32(q.x), float32(q.y))
		}
		r.ClosePath()
	}
	c.paint(r, col)
}

// strokePath draws each segment as a quad and each joint as a disc, all wound alike so they unite.
func strokePath(c *canvas, p path, width float64, col color.RGBA) {
	r := c.mask()
	h := width / 2
	poly := func(pts []point) {
		area := 0.0
		for i := range pts {
			j := (i + 1) % len(pts)
			area += pts[i].x*pts[j].y - pts[j].x*pts[i].y
		}
		if area < 0 {
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
		r.MoveTo(float32(pts[0].x), float32(pts[0].y))
		for _, q := range pts[1:] {
			r.LineTo(float32(q.x), float32(q.y))
		}
		r.ClosePath()
	}
	disc := func(q point) {
		var pts []point
		for i := 0; i < 12; i++ {
			a := float64(i) * math.Pi / 6
			pts = append(pts, point{q.x + h*math.Cos(a), q.y + h*math.Sin(a)})
		}
		poly(pts)
	}
	for _, sp := range p {
		for i := 1; i < len(sp); i++ {
			a, b := sp[i-1], sp[i]
			dx, dy := b.x-a.x, b.y-a.y
			l := math.Hypot(dx, dy)
			if l == 0 {
				continue
			}
			nx, ny := -dy/l*h, dx/l*h
			poly([]point{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}})
			disc(b)
		}
		if len(sp) > 0 {
			disc(sp[0])
		}
	}
	c.paint(r, col)
}

func drawPath(c *canvas, el *xdom.Node, res *resources, layerDraw string) {
	data := child(el, "AbbreviatedData").Value()
	if data == "" || len(data) >= 1_000_000 {
		return
	}
	own, layer := res.draw(attr(el, "DrawParam")), res.draw(layerDraw)
	black := color.RGBA{0, 0, 0, 255}
	lw := 0.353
	if v, ok := number(attr(el, "LineWidth")); ok {
		lw = v
	} else if own.lineWidth != nil {
		lw = *own.lineWidth
	} else if layer.lineWidth != nil {
		lw = *layer.lineWidth
	}
	m := objectMatrix(el)
	b := &pathBuilder{m: m}
	tokens := strings.FieldsFunc(data, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ',' })
	at := func(i int) float64 {
		if i < len(tokens) {
			v, _ := number(tokens[i])
			return v
		}
		return 0
	}
	for i := 0; i < len(tokens); {
		switch tokens[i] {
		case "S", "M":
			b.moveTo(at(i+1), at(i+2))
			i += 3
		case "L":
			b.lineTo(at(i+1), at(i+2))
			i += 3
		case "Q":
			b.quadTo(at(i+1), at(i+2), at(i+3), at(i+4))
			i += 5
		case "B":
			b.cubicTo(at(i+1), at(i+2), at(i+3), at(i+4), at(i+5), at(i+6))
			i += 7
		case "A":
			b.arcTo(at(i+1), at(i+2), at(i+3), at(i+4) != 0, at(i+5) != 0, at(i+6), at(i+7))
			i += 8
		case "C":
			b.close()
			i++
		default:
			i++
		}
	}
	if attr(el, "Fill") == "true" {
		fillPath(c, b.p, rgba(first(colorOf(child(el, "FillColor")), own.fill, layer.fill), black))
	}
	if attr(el, "Stroke") != "false" {
		scale := (m.scaleX() + m.scaleY()) / 2
		strokePath(c, b.p, math.Max(0.05, math.Min(20, lw))*scale, rgba(first(colorOf(child(el, "StrokeColor")), own.stroke, layer.stroke), black))
	}
}

func drawText(c *canvas, el *xdom.Node, res *resources, layerDraw string) {
	f := cjkFont()
	if f == nil {
		return
	}
	size := defaultSize
	if s, ok := number(attr(el, "Size")); ok && s > 0 && s < 1000 {
		size = s
	}
	own, layer := res.draw(attr(el, "DrawParam")), res.draw(layerDraw)
	col := rgba(first(colorValue(attr(el, "FillColor")), colorOf(child(el, "FillColor")), own.fill, layer.fill), color.RGBA{0, 0, 0, 255})
	m := objectMatrix(el)
	var buf sfnt.Buffer
	const ppem = 1000
	var penX, penY float64
	for _, code := range children(el, "TextCode") {
		text, xs, ys, _, ok := glyphs(code, size, &penX, &penY)
		if !ok {
			continue
		}
		for i, ch := range text {
			if ch == ' ' || ch == '\t' {
				continue
			}
			gi, err := f.GlyphIndex(&buf, ch)
			if err != nil || gi == 0 {
				continue
			}
			segs, err := f.LoadGlyph(&buf, gi, fixed.I(ppem), nil)
			if err != nil {
				continue
			}
			// Glyph units (y down, ppem per em) → object space (mm) at the character's origin.
			k := size / ppem
			b := &pathBuilder{m: m}
			pt := func(p fixed.Point26_6) (float64, float64) {
				return xs[i] + float64(p.X)/64*k, ys[i] + float64(p.Y)/64*k
			}
			for _, s := range segs {
				switch s.Op {
				case sfnt.SegmentOpMoveTo:
					x, y := pt(s.Args[0])
					b.moveTo(x, y)
				case sfnt.SegmentOpLineTo:
					x, y := pt(s.Args[0])
					b.lineTo(x, y)
				case sfnt.SegmentOpQuadTo:
					x1, y1 := pt(s.Args[0])
					x, y := pt(s.Args[1])
					b.quadTo(x1, y1, x, y)
				case sfnt.SegmentOpCubeTo:
					x1, y1 := pt(s.Args[0])
					x2, y2 := pt(s.Args[1])
					x, y := pt(s.Args[2])
					b.cubicTo(x1, y1, x2, y2, x, y)
				}
			}
			fillPath(c, b.p, col)
		}
	}

}

func (r *reader) drawImage(c *canvas, el *xdom.Node, res *resources) {
	part := res.media(attr(el, "ResourceID"))
	if part == "" {
		return
	}
	b, err := r.pkg.readPart(part, maxPictureBytes)
	if err != nil || b == nil {
		return
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > maxPicturePixels {
		return
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return
	}
	// The picture fills the unit square of the object's space: sample it through the inverse matrix.
	m := objectMatrix(el)
	det := m.a*m.d - m.b*m.c
	if math.Abs(det) < 1e-12 {
		return
	}
	l, t, w, h := m.bounds(0, 0)
	sb := src.Bounds()
	bounds := c.img.Bounds()
	for py := max(bounds.Min.Y, int(t)); py < min(bounds.Max.Y, int(math.Ceil(t+h))); py++ {
		for px := max(bounds.Min.X, int(l)); px < min(bounds.Max.X, int(math.Ceil(l+w))); px++ {
			x, y := float64(px)+0.5-m.e, float64(py)+0.5-m.f
			u := (m.d*x - m.c*y) / det
			v := (-m.b*x + m.a*y) / det
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			sc := src.At(sb.Min.X+int(u*float64(sb.Dx())), sb.Min.Y+int(v*float64(sb.Dy())))
			draw.Draw(c.img, image.Rect(px, py, px+1, py+1), image.NewUniform(sc), image.Point{}, draw.Over)
		}
	}
}

var (
	cjkOnce sync.Once
	cjk     *sfnt.Font
)

// cjkFont is a font of the system with Chinese characters, or nil.
func cjkFont() *sfnt.Font {
	cjkOnce.Do(func() {
		var candidates []string
		switch runtime.GOOS {
		case "windows":
			dir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
			for _, n := range []string{"simsun.ttc", "simhei.ttf", "msyh.ttc", "simkai.ttf", "simfang.ttf"} {
				candidates = append(candidates, filepath.Join(dir, n))
			}
		case "darwin":
			candidates = []string{"/System/Library/Fonts/Supplemental/Songti.ttc", "/System/Library/Fonts/STHeiti Medium.ttc",
				"/System/Library/Fonts/STHeiti Light.ttc", "/System/Library/Fonts/PingFang.ttc", "/Library/Fonts/Arial Unicode.ttf"}
		default:
			for _, pattern := range []string{"/usr/share/fonts/*/*/*CJK*", "/usr/share/fonts/*/*CJK*", "/usr/share/fonts/*/*/wqy*", "/usr/share/fonts/*/wqy*", "/usr/share/fonts/*/*/*Droid*Fallback*"} {
				m, _ := filepath.Glob(pattern)
				candidates = append(candidates, m...)
			}
		}
		for _, p := range candidates {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var fonts []*sfnt.Font
			if c, err := sfnt.ParseCollection(b); err == nil {
				for i := 0; i < c.NumFonts(); i++ {
					if f, err := c.Font(i); err == nil {
						fonts = append(fonts, f)
					}
				}
			} else if f, err := sfnt.Parse(b); err == nil {
				fonts = append(fonts, f)
			}
			var buf sfnt.Buffer
			for _, f := range fonts {
				if gi, err := f.GlyphIndex(&buf, '章'); err == nil && gi != 0 {
					cjk = f
					return
				}
			}
		}
	})
	return cjk
}
