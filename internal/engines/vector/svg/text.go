package svg

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// textChar is one character of a <text> with what positions it.
type textChar struct {
	r            rune
	st           *style
	lc           lengthContext
	x, y, rot    *float64
	dx, dy       float64
	hasDX, hasDY bool
	path         *Node // textPath
	startOffset  string
	pathStart    bool // first character of a textPath
	preserve     bool
}

// textPiece is laid-out text in one style.
type textPiece struct {
	path *gfx.Path
	st   *style
	lc   lengthContext
}

// spaceState carries whitespace collapsing across the text's elements.
type spaceState struct{ lastSpace bool }

func (s *spaceState) add(chars *[]textChar, c textChar) {
	if c.r == '\n' || c.r == '\r' || c.r == '\t' {
		c.r = ' '
	}
	if !c.preserve {
		if c.r == ' ' && s.lastSpace {
			return
		}
	}
	s.lastSpace = c.r == ' '
	*chars = append(*chars, c)
}

func (r *renderer) collectText(n *Node, st *style, lc lengthContext, chars *[]textChar, path *Node, startOffset string, sp *spaceState) {
	start := len(*chars)
	for _, ch := range n.Children {
		if ch.Name == "" {
			for _, rn := range ch.Text {
				sp.add(chars, textChar{r: rn, st: st, lc: lc, path: path, startOffset: startOffset, preserve: st.preserveSpace})
			}
			continue
		}
		cst := computeStyle(ch, st, lc)
		if !cst.display {
			continue
		}
		clc := lc
		clc.fontSize = cst.fontSize
		switch ch.Name {
		case "tspan", "a":
			r.collectText(ch, cst, clc, chars, path, startOffset, sp)
		case "textPath":
			id, _, ok := urlRef(ch.Attr("href"))
			target := r.doc.ids[id]
			if !ok || target == nil {
				if d := ch.Attr("path"); d != "" {
					target = &Node{Name: "path", Attrs: map[string]string{"d": d}}
				}
			}
			if target == nil {
				continue
			}
			before := len(*chars)
			r.collectText(ch, cst, clc, chars, target, ch.Attr("startOffset"), sp)
			if len(*chars) > before {
				(*chars)[before].pathStart = true
			}
		case "tref":
			id, _, ok := urlRef(ch.Attr("href"))
			if t := r.doc.ids[id]; ok && t != nil {
				for _, rn := range textContent(t) {
					sp.add(chars, textChar{r: rn, st: cst, lc: clc, path: path, startOffset: startOffset, preserve: cst.preserveSpace})
				}
			}
		}
	}
	end := len(*chars)
	// Positions of this element apply from its first character on; those of
	// descendants (assigned before) win.
	assign := func(attr string, axis int, set func(c *textChar, v float64), has func(c *textChar) bool) {
		v, ok := n.Attrs[attr]
		if !ok {
			return
		}
		ls := parseLengthList(v)
		for i, l := range ls {
			if start+i >= end {
				break
			}
			c := &(*chars)[start+i]
			if !has(c) {
				set(c, lc.resolve(l, axis))
			}
		}
	}
	assign("x", axisX, func(c *textChar, v float64) { c.x = &v }, func(c *textChar) bool { return c.x != nil })
	assign("y", axisY, func(c *textChar, v float64) { c.y = &v }, func(c *textChar) bool { return c.y != nil })
	assign("dx", axisX, func(c *textChar, v float64) { c.dx, c.hasDX = v, true }, func(c *textChar) bool { return c.hasDX })
	assign("dy", axisY, func(c *textChar, v float64) { c.dy, c.hasDY = v, true }, func(c *textChar) bool { return c.hasDY })
	if v, ok := n.Attrs["rotate"]; ok {
		nums := parseNumbers(v)
		for i := start; i < end && len(nums) > 0; i++ {
			c := &(*chars)[i]
			if c.rot != nil {
				continue
			}
			k := i - start
			if k >= len(nums) {
				k = len(nums) - 1
			}
			a := nums[k]
			c.rot = &a
		}
	}
}

func (r *renderer) layoutText(n *Node, c context) []textPiece {
	var chars []textChar
	// Leading spaces are dropped (lastSpace starts true), trailing ones below.
	r.collectText(n, c.st, c.lc, &chars, nil, "", &spaceState{lastSpace: true})
	for len(chars) > 0 && chars[len(chars)-1].r == ' ' && !chars[len(chars)-1].preserve {
		chars = chars[:len(chars)-1]
	}
	if len(chars) == 0 {
		return nil
	}
	type glyphOut struct {
		g    gfx.ShapedGlyph
		rot  float64
		x, y float64 // pen position of the glyph's span start
		st   *style
		lc   lengthContext
		path *Node
	}
	var pieces []textPiece
	var chunk []glyphOut
	var chunkPath *Node
	var chunkOffset string
	penX, penY := 0.0, 0.0
	var decos []deco
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		// text-anchor of the chunk's first character.
		anchor := chunk[0].st.textAnchor
		minX, maxX := math.Inf(1), math.Inf(-1)
		for _, g := range chunk {
			minX = math.Min(minX, g.g.X)
			maxX = math.Max(maxX, g.g.X+g.g.Advance)
		}
		shift := 0.0
		rtl := chunk[0].st.direction == "rtl"
		switch {
		case anchor == "middle":
			shift = -(maxX - minX) / 2
		case anchor == "end" && !rtl, anchor == "start" && rtl:
			shift = -(maxX - minX)
		}
		shift += chunk[0].g.X - minX
		var measure *pathMeasure
		pathOffset := 0.0
		if chunkPath != nil {
			if p := shapePath(chunkPath, chunk[0].lc); p != nil {
				m := r.transformOf(chunkPath, defaultStyle(), chunk[0].lc)
				measure = newPathMeasure(p.Transformed(m))
				if l, ok := parseLength(chunkOffset); ok {
					if l.Unit == "%" {
						pathOffset = l.V / 100 * measure.length
					} else {
						pathOffset = chunk[0].lc.resolve(l, axisX)
					}
				}
			}
		}
		for _, g := range chunk {
			gp := r.fonts.GlyphPath(gfx.ShapedGlyph{Face: g.g.Face, GID: g.g.GID, Size: g.g.Size})
			if gp == nil {
				continue
			}
			var t gfx.Matrix
			if measure != nil {
				mid := g.g.X + shift + g.g.Advance/2 - chunk[0].g.X + pathOffset
				pt, ang, ok := measure.at(mid)
				if !ok {
					continue
				}
				t = gfx.Translate(pt.X, pt.Y).Mul(gfx.Rotate(ang + g.rot*math.Pi/180)).Mul(gfx.Translate(-g.g.Advance/2, g.g.Y))
			} else {
				t = gfx.Translate(g.g.X+shift, g.g.Y)
				if g.rot != 0 {
					t = t.Mul(gfx.Rotate(g.rot * math.Pi / 180))
				}
			}
			p := gp.Transformed(t)
			if n := len(pieces); n > 0 && pieces[n-1].st == g.st {
				pieces[n-1].path.Append(p)
			} else {
				pieces = append(pieces, textPiece{path: p, st: g.st, lc: g.lc})
			}
		}
		for i := range decos {
			decos[i].x0 += shift
			decos[i].x1 += shift
		}
		chunk = chunk[:0]
	}
	i := 0
	for i < len(chars) {
		ch := chars[i]
		if ch.x != nil || ch.y != nil || ch.pathStart || ch.path != chunkPath {
			flush()
			r.emitDecorations(&pieces, decos)
			decos = decos[:0]
			if ch.x != nil {
				penX = *ch.x
			}
			if ch.y != nil {
				penY = *ch.y
			}
			if ch.pathStart || ch.path != chunkPath {
				chunkPath = ch.path
				chunkOffset = ch.startOffset
				if ch.path != nil {
					penX, penY = 0, 0
					if ch.x != nil {
						penX = *ch.x
					}
				}
			}
		}
		penX += ch.dx
		penY += ch.dy
		// A span: characters in one style with no positioning of their own.
		j := i + 1
		for j < len(chars) {
			nx := chars[j]
			if nx.st != ch.st || nx.x != nil || nx.y != nil || nx.hasDX || nx.hasDY || nx.path != ch.path || nx.pathStart {
				break
			}
			j++
		}
		text := make([]rune, 0, j-i)
		for k := i; k < j; k++ {
			text = append(text, chars[k].r)
		}
		st := ch.st
		q := gfx.FontQuery{Families: st.fontFamily, Weight: st.fontWeight, Italic: st.italic}
		shaped := r.fonts.Shape(string(text), q, st.fontSize, st.lang)
		shiftY := baselineShift(st, ch.lc, r.fonts, q)
		spanStart := penX
		lastCluster := -1
		shapedPen := 0.0
		for _, g := range shaped.Glyphs {
			if g.Cluster != lastCluster && lastCluster >= 0 {
				// letter-spacing between characters, word-spacing after spaces.
				penX += st.letterSpacing
				if lastCluster < len(text) && text[lastCluster] == ' ' {
					penX += st.wordSpacing
				}
			}
			lastCluster = g.Cluster
			offset := g.X - shapedPen
			shapedPen += g.Advance
			g.X = penX + offset
			g.Y += penY + shiftY
			rot := 0.0
			if k := i + g.Cluster; k < j && chars[k].rot != nil {
				rot = *chars[k].rot
			}
			chunk = append(chunk, glyphOut{g: g, rot: rot, st: st, lc: ch.lc, path: ch.path})
			penX += g.Advance
		}
		if lastCluster >= 0 {
			penX += st.letterSpacing
		}
		if len(st.textDecorationInherited) > 0 && ch.path == nil {
			for _, d := range st.textDecorationInherited {
				var y float64
				switch d {
				case "underline":
					y = penY + st.fontSize*0.1
				case "line-through":
					y = penY - st.fontSize*0.3
				case "overline":
					y = penY - st.fontSize*0.85
				default:
					continue
				}
				decos = append(decos, deco{spanStart, penX, y, st})
			}
		}
		i = j
	}
	flush()
	r.emitDecorations(&pieces, decos)
	return pieces
}

type deco struct {
	x0, x1, y float64
	st        *style
}

// emitDecorations adds underlines, overlines and strike-throughs as rectangles.
func (r *renderer) emitDecorations(pieces *[]textPiece, decos []deco) {
	for _, d := range decos {
		if d.x1 <= d.x0 {
			continue
		}
		t := d.st.fontSize / 15
		p := gfx.NewPath()
		p.Rect(d.x0, d.y-t/2, d.x1-d.x0, t)
		*pieces = append(*pieces, textPiece{path: p, st: d.st, lc: lengthContext{fontSize: d.st.fontSize}})
	}
}

// baselineShift is the vertical offset of dominant-baseline and baseline-shift.
func baselineShift(st *style, lc lengthContext, fonts *gfx.Fonts, q gfx.FontQuery) float64 {
	shift := 0.0
	if st.baseline != "" && st.baseline != "auto" && st.baseline != "alphabetic" && st.baseline != "baseline" {
		m := fonts.Metrics(q, 'x')
		size := st.fontSize
		switch st.baseline {
		case "middle":
			shift = m.XHeight / 2 * size
		case "central":
			shift = (m.Ascent - m.Descent) / 2 * size
		case "hanging":
			shift = m.Ascent * 0.8 * size
		case "text-top", "text-before-edge", "before-edge":
			shift = m.Ascent * size
		case "text-bottom", "text-after-edge", "after-edge", "ideographic":
			shift = -m.Descent * size
		case "mathematical":
			shift = m.Ascent / 2 * size
		}
	}
	switch v := st.baselineShift; v {
	case "", "baseline":
	case "super":
		shift -= st.fontSize / 3
	case "sub":
		shift += st.fontSize / 5
	default:
		if l, ok := parseLength(v); ok {
			if l.Unit == "%" {
				shift -= l.V / 100 * st.fontSize
			} else {
				shift -= lc.resolve(l, axisY)
			}
		}
	}
	return shift
}

// textOutline is the union of the glyph outlines of a <text> in its user space.
func (r *renderer) textOutline(n *Node, c context) *gfx.Path {
	pieces := r.layoutText(n, c)
	if len(pieces) == 0 {
		return nil
	}
	p := gfx.NewPath()
	for _, pc := range pieces {
		p.Append(pc.path)
	}
	return p
}

func (r *renderer) drawText(n *Node, c context) {
	pieces := r.layoutText(n, c)
	// textLength / lengthAdjust on the text element stretch the whole line.
	if v, ok := n.Attrs["textLength"]; ok && len(pieces) > 0 {
		if l, ok := parseLength(v); ok && l.V > 0 {
			want := c.lc.resolve(l, axisX)
			b := gfx.EmptyRect()
			for _, pc := range pieces {
				b = b.Union(pc.path.Bounds())
			}
			if have := b.Width(); have > 0 && math.Abs(have-want) > 1e-6 {
				k := want / have
				t := gfx.Translate(b.X0, 0).Mul(gfx.Scale(k, 1)).Mul(gfx.Translate(-b.X0, 0))
				for i := range pieces {
					pieces[i].path = pieces[i].path.Transformed(t)
				}
			}
		}
	}
	for _, pc := range pieces {
		st := pc.st
		if !st.visible {
			continue
		}
		cc := c
		cc.st = st
		cc.lc = pc.lc
		bbox := pc.path.Bounds()
		order := st.paintOrder
		if order == nil {
			order = []string{"fill", "stroke"}
		}
		for _, o := range order {
			switch o {
			case "fill":
				r.fillPath(pc.path, cc, st.fill, st.fillOpacity, gfx.NonZero, bbox)
			case "stroke":
				r.strokePath(pc.path, cc, bbox)
			}
		}
	}
}

// pathMeasure finds points along a path by length.
type pathMeasure struct {
	pts    []gfx.Point
	cum    []float64
	length float64
}

func newPathMeasure(p *gfx.Path) *pathMeasure {
	m := &pathMeasure{}
	for _, pl := range p.Flatten(0.05) {
		pts := pl.Pts
		if pl.Closed && len(pts) > 1 {
			pts = append(pts, pts[0])
		}
		for i, q := range pts {
			if len(m.pts) > 0 && i > 0 {
				m.length += q.Sub(m.pts[len(m.pts)-1]).Len()
			}
			m.pts = append(m.pts, q)
			m.cum = append(m.cum, m.length)
		}
	}
	return m
}

func (m *pathMeasure) at(d float64) (gfx.Point, float64, bool) {
	if len(m.pts) < 2 || d < 0 || d > m.length {
		return gfx.Point{}, 0, false
	}
	lo, hi := 0, len(m.cum)-1
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if m.cum[mid] < d {
			lo = mid
		} else {
			hi = mid
		}
	}
	a, b := m.pts[lo], m.pts[hi]
	seg := m.cum[hi] - m.cum[lo]
	t := 0.0
	if seg > 0 {
		t = (d - m.cum[lo]) / seg
	}
	dir := b.Sub(a)
	return a.Lerp(b, t), math.Atan2(dir.Y, dir.X), true
}
