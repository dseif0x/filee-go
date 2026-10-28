package svg

import (
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Document is a loaded SVG drawing.
type Document struct {
	root *Node
	ids  map[string]*Node
	// Width and Height are the viewport in CSS pixels.
	Width, Height float64
	viewBox       [4]float64
	hasViewBox    bool
	// BaseDir is the folder external images may be read from (and its subfolders).
	BaseDir string
	// Fonts shapes text; nil uses gfx.SharedFonts().
	Fonts *gfx.Fonts
	depth int
}

// Root is the <svg> element.
func (d *Document) Root() *Node { return d.root }

// Parse reads an SVG document. baseDir limits where referenced files may come from.
func Parse(data []byte, baseDir string) (*Document, error) {
	return parse(data, baseDir, 0)
}

func parse(data []byte, baseDir string, depth int) (*Document, error) {
	root, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	if root.Name != "svg" {
		return nil, ErrNotSVG
	}
	d := &Document{root: root, ids: map[string]*Node{}, BaseDir: baseDir, depth: depth}
	var index func(*Node)
	index = func(n *Node) {
		if n.Name == "" {
			return
		}
		if id := n.Attr("id"); id != "" {
			if _, dup := d.ids[id]; !dup {
				d.ids[id] = n
			}
		}
		for _, c := range n.Children {
			index(c)
		}
	}
	index(root)
	cascade(root)
	d.size()
	return d, nil
}

// size works out the viewport: width/height, else the viewBox, else the
// drawing's own extent.
func (d *Document) size() {
	vb, ok := parseViewBox(d.root.Attr("viewBox"))
	d.viewBox, d.hasViewBox = vb, ok
	lc := lengthContext{vw: 100, vh: 100, fontSize: 16}
	dim := func(attr string, axis int, fromVB float64) float64 {
		l, ok := parseLength(d.root.Attr(attr))
		if !ok {
			return fromVB
		}
		if l.V <= 0 {
			return math.NaN() // an explicit zero or negative size: nothing to draw
		}
		if l.Unit == "%" {
			if fromVB > 0 {
				return fromVB * l.V / 100
			}
			return 0
		}
		return lc.resolve(l, axis)
	}
	w := dim("width", axisX, vb[2])
	h := dim("height", axisY, vb[3])
	// Only one of width/height and a viewBox: keep its aspect ratio.
	if ok {
		if _, okW := parseLength(d.root.Attr("width")); okW && !d.root.Has("height") {
			h = w * vb[3] / vb[2]
		}
		if _, okH := parseLength(d.root.Attr("height")); okH && !d.root.Has("width") {
			w = h * vb[2] / vb[3]
		}
	}
	if math.IsNaN(w) || math.IsNaN(h) {
		d.Width, d.Height = 0, 0
		return
	}
	if w <= 0 || h <= 0 {
		// No size at all: the extent of what is drawn.
		d.Width, d.Height = 100, 100
		b := d.ContentBounds()
		if !b.Empty() && b.X1 > 0 && b.Y1 > 0 {
			if w <= 0 {
				w = b.X1
			}
			if h <= 0 {
				h = b.Y1
			}
		}
	}
	d.Width, d.Height = w, h
}

// ContentBounds is the bounding box of the drawing in viewport coordinates.
func (d *Document) ContentBounds() gfx.Rect {
	r := &renderer{doc: d, fonts: d.fonts(), active: map[*Node]int{}}
	c := r.rootContext(gfx.Identity)
	return r.bbox(d.root, c, true)
}

func (d *Document) fonts() *gfx.Fonts {
	if d.Fonts != nil {
		return d.Fonts
	}
	return gfx.SharedFonts()
}

// Options tune drawing.
type Options struct {
	// FilterResolution is the raster resolution of filter effects in pixels per
	// device unit (1 for rasters; PDF uses 300/72 for 300 dpi).
	FilterResolution float64
}

// Draw draws the drawing onto dev; m maps viewport coordinates (CSS pixels) to the device.
func (d *Document) Draw(dev gfx.Device, m gfx.Matrix, opt Options) {
	if opt.FilterResolution <= 0 {
		opt.FilterResolution = 1
	}
	r := &renderer{doc: d, dev: dev, fonts: d.fonts(), active: map[*Node]int{}, rootScale: m.MeanScale(), filterRes: opt.FilterResolution}
	c := r.rootContext(m)
	if !c.st.display {
		return
	}
	// Opacity, clipping, masks and filters on the root element apply to the whole drawing.
	r.withEffects(d.root, c, func(c context) { r.drawChildren(d.root, c) })
}

type renderer struct {
	styles      map[*Node]*style
	doc         *Document
	dev         gfx.Device
	fonts       *gfx.Fonts
	active      map[*Node]int
	rootScale   float64
	filterRes   float64
	budget      int
	filterDepth int
}

type context struct {
	m  gfx.Matrix
	st *style
	lc lengthContext
	// paints for context-fill / context-stroke inside markers and <use>
	ctxFill, ctxStroke *gfx.Paint
	// the parent's style and lengths, for re-deriving this element's style
	pst *style
	plc lengthContext
}

func (r *renderer) rootContext(m gfx.Matrix) context {
	d := r.doc
	lc := lengthContext{vw: d.Width, vh: d.Height, fontSize: 16}
	st := computeStyle(d.root, defaultStyle(), lc)
	m = m.Mul(r.transformOf(d.root, st, lc))
	if d.hasViewBox {
		m = m.Mul(viewBoxTransform(d.viewBox, d.root.Attr("preserveAspectRatio"), 0, 0, d.Width, d.Height))
		lc.vw, lc.vh = d.viewBox[2], d.viewBox[3]
	}
	lc.fontSize = st.fontSize
	return context{m: m, st: st, lc: lc, pst: defaultStyle(), plc: lc}
}

// nonRendered elements are only drawn when referenced.
var nonRendered = map[string]bool{
	"defs": true, "symbol": true, "clipPath": true, "mask": true, "marker": true, "pattern": true,
	"linearGradient": true, "radialGradient": true, "style": true, "script": true, "title": true, "desc": true,
	"metadata": true, "filter": true, "foreignObject": true, "font": true, "font-face": true, "cursor": true,
	"view": true, "animate": true, "animateTransform": true, "animateMotion": true, "set": true, "glyph": true,
	"missing-glyph": true, "hkern": true, "vkern": true, "solidcolor": true, "meshgradient": true, "hatch": true,
}

func (r *renderer) drawChildren(n *Node, c context) {
	for _, ch := range n.Children {
		if ch.Name != "" {
			r.drawNode(ch, c)
		}
	}
}

func (r *renderer) transformOf(n *Node, st *style, lc lengthContext) gfx.Matrix {
	t := n.Attr("transform")
	if n.Name == "linearGradient" || n.Name == "radialGradient" {
		t = n.Attr("gradientTransform")
	} else if n.Name == "pattern" {
		t = n.Attr("patternTransform")
	}
	if st.transformCSS != "" {
		t = st.transformCSS
	}
	if t == "" || t == "none" {
		return gfx.Identity
	}
	m := parseTransform(t)
	if !m.Finite() {
		return gfx.Identity
	}
	if o, ok := spec(n, "transform-origin"); ok {
		ox, oy := transformOrigin(o, lc)
		m = gfx.Translate(ox, oy).Mul(m).Mul(gfx.Translate(-ox, -oy))
	}
	return m
}

// transformOrigin reads "x y" of transform-origin (lengths, % of the viewport, keywords).
func transformOrigin(v string, lc lengthContext) (float64, float64) {
	f := strings.Fields(v)
	val := func(s string, axis int) (float64, bool) {
		switch s {
		case "left", "top":
			return 0, true
		case "center":
			return lc.resolve(Length{50, "%"}, axis), true
		case "right":
			return lc.resolve(Length{100, "%"}, axisX), true
		case "bottom":
			return lc.resolve(Length{100, "%"}, axisY), true
		}
		l, ok := parseLength(s)
		if !ok {
			return 0, false
		}
		return lc.resolve(l, axis), true
	}
	x, y := lc.resolve(Length{50, "%"}, axisX), lc.resolve(Length{50, "%"}, axisY)
	if len(f) >= 1 {
		if f[0] == "top" || f[0] == "bottom" {
			y, _ = val(f[0], axisY)
			if len(f) >= 2 {
				x, _ = val(f[1], axisX)
			}
			return x, y
		}
		x, _ = val(f[0], axisX)
	}
	if len(f) >= 2 {
		y, _ = val(f[1], axisY)
	}
	return x, y
}

func (r *renderer) drawNode(n *Node, c context) {
	if nonRendered[n.Name] || !conditionsHold(n) {
		return
	}
	// Guard against huge or self-referencing documents.
	r.budget++
	if r.budget > 5_000_000 {
		return
	}
	st := computeStyle(n, c.st, c.lc)
	if !st.display {
		return
	}
	lc := c.lc
	lc.fontSize = st.fontSize
	m := c.m.Mul(r.transformOf(n, st, c.lc))
	if !m.Finite() || m.Det() == 0 {
		return
	}
	cc := context{m: m, st: st, lc: lc, ctxFill: c.ctxFill, ctxStroke: c.ctxStroke, pst: c.st, plc: c.lc}
	r.withEffects(n, cc, func(c context) { r.drawContent(n, c) })
}

// withEffects applies filter, clip-path, mask and opacity around draw.
func (r *renderer) withEffects(n *Node, c context, draw func(context)) {
	st := c.st
	if st.filter != "" {
		filters, ok := r.resolveFilters(st.filter)
		if !ok {
			// A filter that doesn't exist makes the element disappear.
			return
		}
		for _, f := range filters {
			inner, f := draw, f
			draw = func(c context) { r.drawFiltered(n, f, c, inner) }
		}
	}
	var bbox *gfx.Rect
	bounds := func() gfx.Rect {
		if bbox == nil {
			pst := c.pst
			if pst == nil {
				pst = defaultStyle()
			}
			b := r.bbox(n, context{m: gfx.Identity, st: pst, lc: c.plc}, false)
			bbox = &b
		}
		return *bbox
	}
	clips := 0
	if st.clipPath != "" {
		if id, _, ok := urlRef(st.clipPath); ok {
			if cp := r.doc.ids[id]; cp != nil && cp.Name == "clipPath" {
				clips = r.pushClipPath(cp, c, bounds)
			}
		} else if shape := cssClipShape(st.clipPath, c.lc); shape != nil {
			r.dev.PushClip(shape, c.m, gfx.NonZero)
			clips = 1
		}
	}
	defer func() {
		for i := 0; i < clips; i++ {
			r.dev.PopClip()
		}
	}()
	if st.mask != "" {
		if id, _, ok := urlRef(st.mask); ok {
			if mk := r.doc.ids[id]; mk != nil && mk.Name == "mask" && r.enter(mk) {
				defer r.leave(mk)
				kind := gfx.MaskLuminance
				mst := r.styleOf(mk, c.lc)
				kind = mst.maskType
				r.dev.PushMask(kind, st.opacity)
				draw(c)
				r.dev.BeginMaskContent()
				r.drawMaskContent(mk, c, bounds)
				r.dev.PopMask()
				return
			}
		}
	}
	if st.blend != gfx.BlendNormal {
		if bd, ok := r.dev.(gfx.BlendDevice); ok {
			bd.PushBlendGroup(st.opacity, st.blend)
			draw(c)
			r.dev.PopGroup()
			return
		}
	}
	if st.opacity < 1 || st.isolate {
		if st.opacity <= 0 {
			return
		}
		r.dev.PushGroup(st.opacity)
		draw(c)
		r.dev.PopGroup()
		return
	}
	draw(c)
}

// styleOf is n's style inherited along its own ancestors: referenced elements
// (gradients, clip paths, masks, markers, patterns) inherit from where they
// are, not from what references them.
func (r *renderer) styleOf(n *Node, lc lengthContext) *style {
	if n == nil {
		return defaultStyle()
	}
	if r.styles == nil {
		r.styles = map[*Node]*style{}
	}
	if s, ok := r.styles[n]; ok {
		return s
	}
	s := computeStyle(n, r.styleOf(n.Parent, lc), lc)
	r.styles[n] = s
	return s
}

func (r *renderer) enter(n *Node) bool {
	if r.active[n] > 0 {
		return false
	}
	r.active[n]++
	return true
}

func (r *renderer) leave(n *Node) { r.active[n]-- }

func (r *renderer) drawContent(n *Node, c context) {
	switch n.Name {
	case "g", "a":
		r.drawChildren(n, c)
	case "switch":
		for _, ch := range n.Elements() {
			if !switchable[ch.Name] {
				continue
			}
			if conditionsHold(ch) {
				r.drawNode(ch, c)
				break
			}
		}
	case "svg":
		r.drawNestedSVG(n, c, n)
	case "use":
		r.drawUse(n, c)
	case "rect", "circle", "ellipse", "line", "polyline", "polygon", "path":
		if p := shapePath(n, c.lc); p != nil {
			r.paintShape(n, p, c)
		}
	case "text":
		r.drawText(n, c)
	case "image":
		r.drawImage(n, c)
	}
}

var switchable = map[string]bool{
	"g": true, "svg": true, "use": true, "rect": true, "circle": true, "ellipse": true, "line": true, "polyline": true,
	"polygon": true, "path": true, "text": true, "image": true, "switch": true, "a": true, "foreignObject": true,
}

// conditionsHold evaluates the conditional processing attributes of <switch>.
func conditionsHold(n *Node) bool {
	if n.Has("requiredExtensions") && strings.TrimSpace(n.Attr("requiredExtensions")) != "" {
		return false
	}
	if n.Has("systemLanguage") {
		ok := false
		for _, l := range strings.Split(n.Attr("systemLanguage"), ",") {
			l = strings.ToLower(strings.TrimSpace(l))
			if l == "en" || strings.HasPrefix(l, "en-") {
				ok = true
			}
		}
		return ok
	}
	return true
}

// drawNestedSVG draws an <svg> (or a <symbol> through <use>) as a new viewport.
func (r *renderer) drawNestedSVG(n *Node, c context, sizeFrom *Node) {
	x := c.lc.resolveAttr(sizeFrom, "x", axisX, 0)
	y := c.lc.resolveAttr(sizeFrom, "y", axisY, 0)
	w := c.lc.resolveAttr(sizeFrom, "width", axisX, -1)
	h := c.lc.resolveAttr(sizeFrom, "height", axisY, -1)
	if w < 0 {
		w = c.lc.resolve(Length{100, "%"}, axisX)
	}
	if h < 0 {
		h = c.lc.resolve(Length{100, "%"}, axisY)
	}
	if w <= 0 || h <= 0 {
		return
	}
	m := c.m
	if sizeFrom != n {
		// <use> places the viewport at its x/y.
		m = m.Mul(gfx.Translate(x, y))
		x, y = 0, 0
	}
	clip := !c.st.overflowVisible
	if clip {
		rect := gfx.NewPath()
		rect.Rect(x, y, w, h)
		r.dev.PushClip(rect, m, gfx.NonZero)
		defer r.dev.PopClip()
	}
	lc := c.lc
	if vb, ok := parseViewBox(n.Attr("viewBox")); ok {
		m = m.Mul(viewBoxTransform(vb, n.Attr("preserveAspectRatio"), x, y, w, h))
		lc.vw, lc.vh = vb[2], vb[3]
	} else {
		m = m.Mul(gfx.Translate(x, y))
		lc.vw, lc.vh = w, h
	}
	cc := c
	cc.m, cc.lc = m, lc
	r.drawChildren(n, cc)
}

func (lc lengthContext) resolveAttr(n *Node, attr string, axis int, def float64) float64 {
	// Geometry comes from attributes only (SVG 1.1), as Svg.Skia reads it.
	v, ok := n.Attrs[attr]
	if !ok {
		return def
	}
	l, ok := parseLength(v)
	if !ok {
		return def
	}
	return lc.resolve(l, axis)
}

func (r *renderer) drawUse(n *Node, c context) {
	id, _, ok := urlRef(n.Attr("href"))
	if !ok {
		return
	}
	target := r.doc.ids[id]
	if target == nil || !r.enter(target) {
		return
	}
	defer r.leave(target)
	// A <use> inside what it references would never end.
	for p := n; p != nil; p = p.Parent {
		if p == target {
			return
		}
	}
	if r.active[n] > 30 {
		return
	}
	// The use element is the context element of context-fill / context-stroke.
	none := gfx.EmptyRect()
	if p, _, ok := r.resolvePaint(c.st.fill, c.st.fillOpacity, c, none); ok {
		c.ctxFill = &p
	} else {
		c.ctxFill = nil
	}
	if p, _, ok := r.resolvePaint(c.st.stroke, c.st.strokeOpacity, c, none); ok {
		c.ctxStroke = &p
	} else {
		c.ctxStroke = nil
	}
	switch target.Name {
	case "symbol":
		st := computeStyle(target, c.st, c.lc)
		if !st.display {
			return
		}
		cc := c
		cc.st, cc.pst, cc.plc = st, c.st, c.lc
		r.withEffects(target, cc, func(c context) { r.drawNestedSVG(target, c, n) })
		return
	case "svg":
		st := computeStyle(target, c.st, c.lc)
		cc := c
		cc.st = st
		// The use's width/height override the svg's own.
		sizes := &Node{Name: "svg", Attrs: map[string]string{}}
		for _, a := range []string{"width", "height"} {
			if v, ok := n.Attrs[a]; ok {
				sizes.Attrs[a] = v
			} else if v, ok := target.Attrs[a]; ok {
				sizes.Attrs[a] = v
			}
		}
		sizes.Attrs["x"] = target.Attr("x")
		sizes.Attrs["y"] = target.Attr("y")
		cc.m = cc.m.Mul(gfx.Translate(c.lc.resolveAttr(n, "x", axisX, 0), c.lc.resolveAttr(n, "y", axisY, 0)))
		r.drawNestedSVG(target, cc, sizes)
		return
	}
	x := c.lc.resolveAttr(n, "x", axisX, 0)
	y := c.lc.resolveAttr(n, "y", axisY, 0)
	cc := c
	cc.m = c.m.Mul(gfx.Translate(x, y))
	r.drawNode(target, cc)
}

// shapePath is the geometry of a basic shape or path in its user space.
func shapePath(n *Node, lc lengthContext) *gfx.Path {
	attr := func(name string, axis int, def float64) float64 { return lc.resolveAttr(n, name, axis, def) }
	p := gfx.NewPath()
	switch n.Name {
	case "rect":
		x, y := attr("x", axisX, 0), attr("y", axisY, 0)
		w, h := attr("width", axisX, 0), attr("height", axisY, 0)
		if w <= 0 || h <= 0 {
			return nil
		}
		rx, ry := attr("rx", axisX, -1), attr("ry", axisY, -1)
		if rx < 0 && ry >= 0 {
			rx = ry
		} else if ry < 0 && rx >= 0 {
			ry = rx
		}
		if rx > 0 && ry > 0 {
			p.RoundRect(x, y, w, h, math.Min(rx, w/2), math.Min(ry, h/2))
		} else {
			p.Rect(x, y, w, h)
		}
	case "circle":
		cx, cy, rr := attr("cx", axisX, 0), attr("cy", axisY, 0), attr("r", axisOther, 0)
		if rr <= 0 {
			return nil
		}
		p.Ellipse(cx, cy, rr, rr)
		p.Shape = &gfx.EllipseHint{CX: cx, CY: cy, RX: rr, RY: rr}
	case "ellipse":
		cx, cy := attr("cx", axisX, 0), attr("cy", axisY, 0)
		rx, ry := attr("rx", axisX, -1), attr("ry", axisY, -1)
		if rx < 0 {
			rx = ry
		}
		if ry < 0 {
			ry = rx
		}
		if rx <= 0 || ry <= 0 {
			return nil
		}
		p.Ellipse(cx, cy, rx, ry)
		p.Shape = &gfx.EllipseHint{CX: cx, CY: cy, RX: rx, RY: ry}
	case "line":
		p.MoveTo(attr("x1", axisX, 0), attr("y1", axisY, 0))
		p.LineTo(attr("x2", axisX, 0), attr("y2", axisY, 0))
	case "polyline", "polygon":
		pts := parsePoints(n.Attr("points"))
		if len(pts) == 0 {
			return nil
		}
		p.MoveTo(pts[0].X, pts[0].Y)
		for _, q := range pts[1:] {
			p.LineTo(q.X, q.Y)
		}
		if n.Name == "polygon" {
			p.Close()
		}
	case "path":
		d, _ := spec(n, "d")
		if d == "" {
			return nil
		}
		if strings.HasPrefix(strings.TrimSpace(d), "path(") {
			d = strings.Trim(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(d), "path("), ")"), `"' `)
		}
		p = parsePathData(d)
	}
	if len(p.Ops) == 0 {
		return nil
	}
	return p
}

// paintShape fills, strokes and adds markers in paint-order.
func (r *renderer) paintShape(n *Node, p *gfx.Path, c context) {
	st := c.st
	if !st.visible {
		return
	}
	order := st.paintOrder
	if order == nil {
		order = []string{"fill", "stroke", "markers"}
	}
	markers := n.Name == "path" || n.Name == "line" || n.Name == "polyline" || n.Name == "polygon"
	for _, o := range order {
		switch o {
		case "fill":
			r.fillPath(p, c, st.fill, st.fillOpacity, st.fillRule, p.Bounds())
		case "stroke":
			r.strokePath(p, c, p.Bounds())
		case "markers":
			if markers {
				r.drawMarkers(p, c)
			}
		}
	}
}

// resolvePaint turns a paint spec into a device paint; pattern is set for pattern paints.
func (r *renderer) resolvePaint(ps paintSpec, opacity float64, c context, bbox gfx.Rect) (paint gfx.Paint, pattern *Node, ok bool) {
	switch ps.kind {
	case paintNone:
		return gfx.Paint{}, nil, false
	case paintColor:
		return gfx.Solid(ps.color.WithAlpha(opacity)), nil, true
	case paintCurrentColor:
		return gfx.Solid(c.st.color.WithAlpha(opacity)), nil, true
	case paintContextFill, paintContextStroke:
		src := c.ctxFill
		if ps.kind == paintContextStroke {
			src = c.ctxStroke
		}
		if src == nil {
			return gfx.Paint{}, nil, false
		}
		p := *src
		if p.Gradient == nil {
			p.Color = p.Color.WithAlpha(opacity)
		}
		return p, nil, true
	}
	target := r.doc.ids[ps.url]
	if target == nil {
		if ps.fallback != nil {
			return r.resolvePaint(*ps.fallback, opacity, c, bbox)
		}
		return gfx.Paint{}, nil, false
	}
	switch target.Name {
	case "linearGradient", "radialGradient":
		g, solid, ok := r.gradient(target, c, bbox, opacity)
		if !ok {
			if ps.fallback != nil {
				return r.resolvePaint(*ps.fallback, opacity, c, bbox)
			}
			return gfx.Paint{}, nil, false
		}
		if g == nil {
			return gfx.Solid(solid), nil, true
		}
		return gfx.Paint{Gradient: g}, nil, true
	case "pattern":
		return gfx.Paint{}, target, true
	case "solidcolor":
		sst := r.styleOf(target, c.lc)
		col := sst.color
		if v, ok := spec(target, "solid-color"); ok {
			if cc, ok := parseColor(v); ok {
				col = cc
			}
		}
		op := parseFloat(target.Attr("solid-opacity"), 1)
		return gfx.Solid(col.WithAlpha(op * opacity)), nil, true
	}
	if ps.fallback != nil {
		return r.resolvePaint(*ps.fallback, opacity, c, bbox)
	}
	return gfx.Paint{}, nil, false
}

func (r *renderer) fillPath(p *gfx.Path, c context, ps paintSpec, opacity float64, rule gfx.FillRule, bbox gfx.Rect) {
	paint, pattern, ok := r.resolvePaint(ps, opacity, c, bbox)
	if !ok {
		return
	}
	if pattern != nil {
		r.patternFill(p, c, rule, pattern, bbox, opacity)
		return
	}
	r.dev.Fill(p, c.m, rule, paint)
}

func (r *renderer) strokeStyle(c context) (*gfx.StrokeStyle, bool) {
	st := c.st
	w := c.lc.resolve(st.strokeWidth, axisOther)
	if !(w > 0) {
		return nil, false
	}
	s := &gfx.StrokeStyle{Width: w, Cap: st.lineCap, Join: st.lineJoin, MiterLimit: st.miterLimit}
	if len(st.dashArray) > 0 {
		sum := 0.0
		d := make([]float64, len(st.dashArray))
		for i, l := range st.dashArray {
			d[i] = c.lc.resolve(l, axisOther)
			sum += d[i]
		}
		if sum > 0 {
			s.Dashes = d
			s.DashOffset = c.lc.resolve(st.dashOffset, axisOther)
		}
	}
	return s, true
}

func (r *renderer) strokePath(p *gfx.Path, c context, bbox gfx.Rect) {
	st := c.st
	s, ok := r.strokeStyle(c)
	if !ok {
		return
	}
	paint, pattern, ok := r.resolvePaint(st.stroke, st.strokeOpacity, c, bbox)
	if !ok {
		return
	}
	if st.nonScalingStroke {
		// Widths in viewport units, whatever the element's transform.
		k := r.rootScale
		ns := *s
		ns.Width *= k
		for i := range ns.Dashes {
			ns.Dashes[i] *= k
		}
		ns.DashOffset *= k
		dp := p.Transformed(c.m)
		if pattern != nil || paint.Gradient != nil {
			outline := gfx.PathFromPolylines(gfx.StrokePolylines(dashed(dp.Flatten(0.1), &ns), &ns, 0.1))
			inv, ok := c.m.Invert()
			if !ok {
				return
			}
			r.fillPath(outline.Transformed(inv), c, st.stroke, st.strokeOpacity, gfx.NonZero, bbox)
			return
		}
		r.dev.Stroke(dp, gfx.Identity, &ns, paint)
		return
	}
	if pattern != nil {
		tol := 0.1 / math.Max(c.m.MeanScale(), 1e-9)
		outline := gfx.PathFromPolylines(gfx.StrokePolylines(dashed(p.Flatten(tol), s), s, tol))
		r.patternFill(outline, c, gfx.NonZero, pattern, bbox, st.strokeOpacity)
		return
	}
	r.dev.Stroke(p, c.m, s, paint)
}

func dashed(pls []gfx.Polyline, s *gfx.StrokeStyle) []gfx.Polyline {
	if len(s.Dashes) == 0 {
		return pls
	}
	return gfx.DashPolylines(pls, s.Dashes, s.DashOffset)
}

// ---- gradients ----

// gradientAttr finds an attribute along the href chain.
func (r *renderer) gradientChain(n *Node) []*Node {
	chain := []*Node{n}
	seen := map[*Node]bool{n: true}
	for len(chain) < 32 {
		id, _, ok := urlRef(chain[len(chain)-1].Attr("href"))
		if !ok {
			break
		}
		next := r.doc.ids[id]
		if next == nil || seen[next] {
			break
		}
		seen[next] = true
		chain = append(chain, next)
	}
	return chain
}

func chainAttr(chain []*Node, name string) (string, bool) {
	for _, n := range chain {
		if v, ok := n.Attrs[name]; ok {
			return v, true
		}
	}
	return "", false
}

// gradient builds a gradient paint; with fewer than two stops it is a solid colour.
func (r *renderer) gradient(n *Node, c context, bbox gfx.Rect, opacity float64) (*gfx.Gradient, gfx.Color, bool) {
	chain := r.gradientChain(n)
	var stops []gfx.Stop
	for _, g := range chain {
		if g.Name != "linearGradient" && g.Name != "radialGradient" {
			continue
		}
		gst := r.styleOf(g, c.lc)
		last := 0.0
		for _, s := range g.Elements() {
			if s.Name != "stop" {
				continue
			}
			sst := computeStyle(s, gst, c.lc)
			off := 0.0
			if v, ok := spec(s, "offset"); ok {
				// Only numbers and percentages; anything else is 0.
				if l, ok := parseLength(v); ok && (l.Unit == "" || l.Unit == "%") {
					off = l.V
					if l.Unit == "%" {
						off /= 100
					}
				}
			}
			off = clamp01(off)
			if off < last {
				off = last
			}
			last = off
			col := sst.stopColor
			col.A *= sst.stopOpacity * opacity
			stops = append(stops, gfx.Stop{Offset: off, Color: col})
		}
		if len(stops) > 0 {
			break
		}
	}
	if len(stops) == 0 {
		return nil, gfx.Color{}, false
	}
	if len(stops) == 1 {
		return nil, stops[0].Color, true
	}
	units, _ := chainAttr(chain, "gradientUnits")
	obb := units != "userSpaceOnUse"
	g := &gfx.Gradient{Stops: stops, Radial: n.Name == "radialGradient", Matrix: gfx.Identity}
	switch v, _ := chainAttr(chain, "spreadMethod"); v {
	case "reflect":
		g.Spread = gfx.SpreadReflect
	case "repeat":
		g.Spread = gfx.SpreadRepeat
	}
	lc := c.lc
	if obb {
		lc = lengthContext{vw: 1, vh: 1, fontSize: c.lc.fontSize}
	}
	get := func(name string, axis int, def string) float64 {
		// Geometry is only taken from gradients of the same kind.
		v, ok := "", false
		for _, cn := range chain {
			if cn.Name != n.Name {
				continue
			}
			if v, ok = cn.Attrs[name]; ok {
				break
			}
		}
		if !ok {
			v = def
		}
		l, ok := parseLength(v)
		if !ok {
			l, _ = parseLength(def)
		}
		if obb && l.Unit == "" {
			return l.V
		}
		return lc.resolve(l, axis)
	}
	if g.Radial {
		g.CX = get("cx", axisX, "50%")
		g.CY = get("cy", axisY, "50%")
		g.R = get("r", axisOther, "50%")
		g.FX, g.FY = g.CX, g.CY
		for _, cn := range chain {
			if cn.Name == n.Name && cn.Has("fx") {
				g.FX = get("fx", axisX, "50%")
				break
			}
		}
		for _, cn := range chain {
			if cn.Name == n.Name && cn.Has("fy") {
				g.FY = get("fy", axisY, "50%")
				break
			}
		}
		g.FR = get("fr", axisOther, "0")
		if g.R <= 0 {
			return nil, stops[len(stops)-1].Color, true
		}
	} else {
		g.X1 = get("x1", axisX, "0%")
		g.Y1 = get("y1", axisY, "0%")
		g.X2 = get("x2", axisX, "100%")
		g.Y2 = get("y2", axisY, "0%")
		if g.X1 == g.X2 && g.Y1 == g.Y2 {
			return nil, stops[len(stops)-1].Color, true
		}
	}
	if obb {
		if bbox.Empty() || bbox.Width() <= 0 || bbox.Height() <= 0 {
			return nil, gfx.Color{}, false
		}
		g.Matrix = gfx.Translate(bbox.X0, bbox.Y0).Mul(gfx.Scale(bbox.Width(), bbox.Height()))
	}
	if t, ok := chainAttr(chain, "gradientTransform"); ok {
		gt := parseTransform(t)
		if o, ok := spec(n, "transform-origin"); ok {
			ox, oy := transformOrigin(o, lc)
			gt = gfx.Translate(ox, oy).Mul(gt).Mul(gfx.Translate(-ox, -oy))
		}
		g.Matrix = g.Matrix.Mul(gt)
	}
	return g, gfx.Color{}, true
}

// ---- patterns ----

func (r *renderer) patternFill(p *gfx.Path, c context, rule gfx.FillRule, pat *Node, bbox gfx.Rect, opacity float64) {
	if !r.enter(pat) {
		return
	}
	defer r.leave(pat)
	chain := r.gradientChain(pat)
	var content *Node
	for _, n := range chain {
		if n.Name == "pattern" && len(n.Elements()) > 0 {
			content = n
			break
		}
	}
	if content == nil {
		return
	}
	units, _ := chainAttr(chain, "patternUnits")
	cunits, _ := chainAttr(chain, "patternContentUnits")
	obb := units != "userSpaceOnUse"
	get := func(name string, axis int, def float64) float64 {
		v, ok := chainAttr(chain, name)
		if !ok {
			return def
		}
		l, ok := parseLength(v)
		if !ok {
			return def
		}
		if obb {
			if l.Unit == "%" {
				l.V /= 100
			}
			if axis == axisX {
				return l.V * bbox.Width()
			}
			return l.V * bbox.Height()
		}
		return c.lc.resolve(l, axis)
	}
	if obb && (bbox.Empty() || bbox.Width() <= 0 || bbox.Height() <= 0) {
		return
	}
	x, y := get("x", axisX, 0), get("y", axisY, 0)
	w, h := get("width", axisX, 0), get("height", axisY, 0)
	if obb {
		x += bbox.X0
		y += bbox.Y0
	}
	if w <= 0 || h <= 0 {
		return
	}
	pt := gfx.Identity
	if t, ok := chainAttr(chain, "patternTransform"); ok {
		pt = parseTransform(t)
		if o, ok := spec(pat, "transform-origin"); ok {
			ox, oy := transformOrigin(o, c.lc)
			pt = gfx.Translate(ox, oy).Mul(pt).Mul(gfx.Translate(-ox, -oy))
		}
	}
	inv, ok := pt.Invert()
	if !ok {
		return
	}
	area := p.Bounds().Transform(inv)
	if area.Empty() {
		return
	}
	i0, i1 := math.Floor((area.X0-x)/w), math.Ceil((area.X1-x)/w)
	j0, j1 := math.Floor((area.Y0-y)/h), math.Ceil((area.Y1-y)/h)
	tiles := (i1 - i0) * (j1 - j0)
	if tiles > 200_000 || math.IsNaN(tiles) {
		return
	}
	var inner gfx.Matrix
	if vb, ok := parseViewBox(firstAttr(chain, "viewBox")); ok {
		par, _ := chainAttr(chain, "preserveAspectRatio")
		inner = viewBoxTransform(vb, par, 0, 0, w, h)
	} else if cunits == "objectBoundingBox" {
		inner = gfx.Scale(bbox.Width(), bbox.Height())
	} else {
		inner = gfx.Identity
	}
	r.dev.PushClip(p, c.m, rule)
	defer r.dev.PopClip()
	if opacity < 1 {
		r.dev.PushGroup(opacity)
		defer r.dev.PopGroup()
	}
	pst := r.styleOf(content, c.lc)
	base := c.m.Mul(pt)
	for j := j0; j < j1; j++ {
		for i := i0; i < i1; i++ {
			tx, ty := x+i*w, y+j*h
			tm := base.Mul(gfx.Translate(tx, ty))
			cell := gfx.NewPath()
			cell.Rect(0, 0, w, h)
			r.dev.PushClip(cell, tm, gfx.NonZero)
			// The tile's content is drawn from the tile's origin.
			cc := context{m: tm.Mul(inner), st: pst, lc: c.lc}
			r.drawChildren(content, cc)
			r.dev.PopClip()
		}
	}
}

func firstAttr(chain []*Node, name string) string {
	v, _ := chainAttr(chain, name)
	return v
}

// ---- clipping and masking ----

// pushClipPath pushes the clip of a <clipPath>; it returns how many clips were pushed.
func (r *renderer) pushClipPath(cp *Node, c context, bounds func() gfx.Rect) int {
	if !r.enter(cp) {
		return 0
	}
	defer r.leave(cp)
	pushed := 0
	cst := r.styleOf(cp, c.lc)
	// A clip path can itself be clipped.
	if cst.clipPath != "" {
		if id, _, ok := urlRef(cst.clipPath); ok {
			if inner := r.doc.ids[id]; inner != nil && inner.Name == "clipPath" {
				pushed += r.pushClipPath(inner, c, bounds)
			}
		}
	}
	m := c.m.Mul(r.transformOf(cp, cst, c.lc))
	lc := c.lc
	if cp.Attr("clipPathUnits") == "objectBoundingBox" {
		b := bounds()
		if b.Empty() || b.Width() <= 0 || b.Height() <= 0 {
			r.dev.PushClip(gfx.NewPath(), m, gfx.NonZero)
			return pushed + 1
		}
		m = m.Mul(gfx.Translate(b.X0, b.Y0)).Mul(gfx.Scale(b.Width(), b.Height()))
		lc = lengthContext{vw: 1, vh: 1, fontSize: c.lc.fontSize}
	}
	var shapes []gfx.ClipShape
	var add func(n *Node, st *style, t gfx.Matrix)
	add = func(n *Node, st *style, t gfx.Matrix) {
		s := computeStyle(n, st, lc)
		if !s.display || !conditionsHold(n) {
			return
		}
		t = t.Mul(r.transformOf(n, s, lc))
		var geom *gfx.Path
		switch n.Name {
		case "use":
			id, _, ok := urlRef(n.Attr("href"))
			if target := r.doc.ids[id]; ok && target != nil && r.enter(target) {
				add(target, s, t.Mul(gfx.Translate(lc.resolveAttr(n, "x", axisX, 0), lc.resolveAttr(n, "y", axisY, 0))))
				r.leave(target)
			}
			return
		case "text":
			geom = r.textOutline(n, context{m: gfx.Identity, st: s, lc: lc})
		case "rect", "circle", "ellipse", "line", "polyline", "polygon", "path":
			geom = shapePath(n, lc)
		}
		if geom == nil || !s.visible {
			return
		}
		// A child whose own clip-path points at an empty clip contributes nothing.
		if s.clipPath != "" {
			if id, _, ok := urlRef(s.clipPath); ok {
				if inner := r.doc.ids[id]; inner == nil || inner.Name != "clipPath" || len(inner.Elements()) == 0 {
					return
				}
			}
		}
		shapes = append(shapes, gfx.ClipShape{Path: geom, M: m.Mul(t), Rule: s.clipRule})
	}
	for _, ch := range cp.Elements() {
		add(ch, cst, gfx.Identity)
	}
	if len(shapes) == 0 {
		r.dev.PushClip(gfx.NewPath(), m, gfx.NonZero)
		return pushed + 1
	}
	r.dev.PushClipShapes(shapes)
	return pushed + 1
}

func (r *renderer) drawMaskContent(mk *Node, c context, bounds func() gfx.Rect) {
	units := mk.Attr("maskUnits")
	var x, y, w, h float64
	if units == "userSpaceOnUse" {
		x = c.lc.resolveAttr(mk, "x", axisX, c.lc.resolve(Length{-10, "%"}, axisX))
		y = c.lc.resolveAttr(mk, "y", axisY, c.lc.resolve(Length{-10, "%"}, axisY))
		w = c.lc.resolveAttr(mk, "width", axisX, c.lc.resolve(Length{120, "%"}, axisX))
		h = c.lc.resolveAttr(mk, "height", axisY, c.lc.resolve(Length{120, "%"}, axisY))
	} else {
		b := bounds()
		if b.Empty() {
			return
		}
		frac := func(name string, def float64) float64 {
			v, ok := mk.Attrs[name]
			if !ok {
				return def
			}
			return parseFloat(v, def)
		}
		x = b.X0 + frac("x", -0.1)*b.Width()
		y = b.Y0 + frac("y", -0.1)*b.Height()
		w = frac("width", 1.2) * b.Width()
		h = frac("height", 1.2) * b.Height()
	}
	if w <= 0 || h <= 0 {
		return
	}
	region := gfx.NewPath()
	region.Rect(x, y, w, h)
	r.dev.PushClip(region, c.m, gfx.NonZero)
	defer r.dev.PopClip()
	mst := r.styleOf(mk, c.lc)
	cc := context{m: c.m, st: mst, lc: c.lc}
	if mk.Attr("maskContentUnits") == "objectBoundingBox" {
		b := bounds()
		cc.m = c.m.Mul(gfx.Translate(b.X0, b.Y0)).Mul(gfx.Scale(b.Width(), b.Height()))
	}
	// A mask can itself be masked.
	if mst.mask != "" {
		if id, _, ok := urlRef(mst.mask); ok {
			if inner := r.doc.ids[id]; inner != nil && inner.Name == "mask" && r.enter(inner) {
				defer r.leave(inner)
				r.dev.PushMask(r.styleOf(inner, c.lc).maskType, 1)
				r.drawChildren(mk, cc)
				r.dev.BeginMaskContent()
				r.drawMaskContent(inner, c, bounds)
				r.dev.PopMask()
				return
			}
		}
	}
	r.drawChildren(mk, cc)
}

// ---- bounding boxes ----

// bbox is the bounding box of n's geometry in the coordinates c.m maps from
// (c.m includes n's own transform when withTransform is set).
func (r *renderer) bbox(n *Node, c context, withTransform bool) gfx.Rect {
	if r.active[n] > 0 && n.Name == "use" {
		return gfx.EmptyRect()
	}
	st := computeStyle(n, c.st, c.lc)
	if !st.display || nonRendered[n.Name] && n != r.doc.root {
		return gfx.EmptyRect()
	}
	m := c.m
	if withTransform {
		m = m.Mul(r.transformOf(n, st, c.lc))
	}
	lc := c.lc
	lc.fontSize = st.fontSize
	cc := context{m: m, st: st, lc: lc}
	switch n.Name {
	case "rect", "circle", "ellipse", "line", "polyline", "polygon", "path":
		p := shapePath(n, lc)
		if p == nil {
			return gfx.EmptyRect()
		}
		return p.Transformed(m).Bounds()
	case "text":
		p := r.textOutline(n, cc)
		if p == nil {
			return gfx.EmptyRect()
		}
		return p.Transformed(m).Bounds()
	case "image":
		x, y := lc.resolveAttr(n, "x", axisX, 0), lc.resolveAttr(n, "y", axisY, 0)
		w, h := lc.resolveAttr(n, "width", axisX, 0), lc.resolveAttr(n, "height", axisY, 0)
		return gfx.Rect{X0: x, Y0: y, X1: x + w, Y1: y + h}.Transform(m)
	case "use":
		id, _, ok := urlRef(n.Attr("href"))
		target := r.doc.ids[id]
		if !ok || target == nil || !r.enter(target) {
			return gfx.EmptyRect()
		}
		defer r.leave(target)
		x, y := lc.resolveAttr(n, "x", axisX, 0), lc.resolveAttr(n, "y", axisY, 0)
		cc.m = m.Mul(gfx.Translate(x, y))
		if target.Name == "symbol" {
			out := gfx.EmptyRect()
			for _, ch := range target.Elements() {
				out = out.Union(r.bbox(ch, cc, true))
			}
			return out
		}
		return r.bbox(target, cc, true)
	}
	out := gfx.EmptyRect()
	for _, ch := range n.Elements() {
		if nonRendered[ch.Name] {
			continue
		}
		out = out.Union(r.bbox(ch, cc, true))
	}
	return out
}
