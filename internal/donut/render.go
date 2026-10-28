package donut

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

// Renderer draws a Menu into an RGBA image of Side×Scale pixels. It keeps
// the image and the text layouts between frames. Not safe for concurrent use.
type Renderer struct {
	Fonts *Fonts
	// Scale is device pixels per point (2 on Retina screens).
	Scale float64
	// HitBackdrop fills the whole square with alpha 1/255: macOS passes the
	// mouse and drags through fully transparent pixels, and a drag waiting
	// for files must reach the window.
	HitBackdrop bool
	canvas      *Canvas
	layouts     map[layoutKey]*Text
	sizes       map[layoutKey]float64
}

type layoutKey struct {
	text  string
	bold  bool
	size  float64
	width float64
	lines int
}

// NewRenderer returns a renderer with the given fonts (nil for DefaultFonts).
func NewRenderer(fonts *Fonts, scale float64) *Renderer {
	if fonts == nil {
		fonts = DefaultFonts()
	}
	return &Renderer{Fonts: fonts, Scale: scale}
}

func (r *Renderer) layout(s string, bold bool, size, width float64, lines int) *Text {
	k := layoutKey{s, bold, math.Round(size*100) / 100, math.Round(width*4) / 4, lines}
	if t, ok := r.layouts[k]; ok {
		return t
	}
	if r.layouts == nil || len(r.layouts) > 512 {
		r.layouts = map[layoutKey]*Text{}
	}
	t := r.Fonts.Layout(s, bold, k.size, k.width, lines)
	r.layouts[k] = t
	return t
}

const (
	labelFontSize    = 13
	minLabelFontSize = 10
)

// labelSize is 13, or smaller (down to 10) when a word is wider than the
// slice (Chinese and Japanese words are left out: they break anywhere).
func (r *Renderer) labelSize(text string, width float64) float64 {
	line := math.Round(width)
	k := layoutKey{text: text, width: line}
	if s, ok := r.sizes[k]; ok {
		return s
	}
	widest := 0.0
	for _, w := range strings.Fields(text) {
		if strings.IndexFunc(w, isHanOrKana) >= 0 {
			continue
		}
		widest = math.Max(widest, r.Fonts.Measure(w, true, labelFontSize))
	}
	size := float64(labelFontSize)
	if widest >= line {
		size = math.Max(minLabelFontSize, labelFontSize*(line-1)/widest)
	}
	if r.sizes == nil || len(r.sizes) > 512 {
		r.sizes = map[layoutKey]float64{}
	}
	r.sizes[k] = size
	return size
}

// Render draws the menu with the palette and returns the image (reused by the next call).
func (r *Renderer) Render(m *Menu, pal Palette) *image.RGBA {
	side := m.Side()
	px := int(math.Ceil(side * r.Scale))
	if r.canvas == nil || r.canvas.Img.Rect.Dx() != px || r.canvas.Scale != r.Scale {
		r.canvas = NewCanvas(px, px, r.Scale)
	}
	c := r.canvas
	c.Clear()
	if r.HitBackdrop {
		for i := 3; i < len(c.Img.Pix); i += 4 {
			c.Img.Pix[i] = 1
		}
	}
	op := clamp(m.Opacity, 0, 1)
	if op <= 0.001 {
		return c.Img
	}
	d := &drawer{r: r, c: c, m: m, pal: pal, op: op, center: m.CenterPoint(), r2: m.OuterRadius, r1: m.innerRadius()}
	if m.progressMode {
		d.progress()
		return c.Img
	}
	d.ring()
	reaching := m.expanded
	if reaching < 0 {
		reaching = m.pendingGroup
	}
	if reaching >= 0 && reaching < m.Count() && len(m.items[reaching].Children) > 0 {
		d.hitBackdrop(reaching, len(m.items[reaching].Children))
	}
	if m.shownExpanded >= 0 && m.shownExpanded < m.Count() && m.expand.v > 0.01 && len(m.items[m.shownExpanded].Children) > 0 {
		g := m.shownExpanded
		ch := m.items[g].Children
		d.groupRing(g, ch, MidAngle(g, m.Count()), m.subSpanOf(len(ch)), m.expand.v, m.subHover, g == m.expanded, m.averageOpen(), false)
	}
	if og := m.oldGroup; og != nil && m.sliceFlipWidth(true, og.index) > 0 {
		d.groupRing(og.index, og.children, m.oldFace[og.index].mid, og.span, og.expansion, og.subHover, og.expanded, og.opening, true)
	}
	d.hub()
	return c.Img
}

type drawer struct {
	r      *Renderer
	c      *Canvas
	m      *Menu
	pal    Palette
	op     float64
	center Point
	r1, r2 float64
}

func (d *drawer) wedge(inner, outer, mid, span float64) *Path {
	var p Path
	if span >= 2*math.Pi-0.001 {
		p.Circle(d.center, outer, true)
		p.Circle(d.center, inner, false)
		return &p
	}
	p.AddOutline(Wedge(d.center, inner, outer, mid, span, GapPixels, d.m.sliceCornerRadius()))
	return &p
}

func (d *drawer) wedgeSharp(inner, outer, mid, span float64) *Path {
	var p Path
	p.AddOutline(Wedge(d.center, inner, outer, mid, span, 0, 0))
	return &p
}

func (d *drawer) border(p *Path, m Matrix, opacity float64) {
	if d.pal.SliceBorder.A == 0 {
		return
	}
	for _, sub := range p.subpaths() {
		d.c.Stroke(sub, true, m, StrokeStyle{Width: 1}, d.pal.SliceBorder, opacity*d.op)
	}
}

func (d *drawer) fill(p *Path, m Matrix, col color.NRGBA, opacity float64) {
	d.c.Fill(p, m, col, opacity*d.op)
}

func (d *drawer) shadow(r2 float64, opacity float64) {
	sr := r2 + 14
	d.c.FillRadial(Point{d.center.X, d.center.Y + 4}, sr, []GradientStop{
		{0, color.NRGBA{}}, {(r2 - 18) / sr, color.NRGBA{}}, {(r2 - 2) / sr, color.NRGBA{A: 46}}, {1, color.NRGBA{}},
	}, opacity*d.op)
}

// slice draws one slice with its label, transformed by t.
func (d *drawer) slice(it *Item, mid, span, opacity, emphasis float64, hot bool, t Matrix) {
	w := d.wedge(d.r1, d.r2, mid, span)
	alpha := d.m.SliceOpacity
	if !it.Enabled {
		alpha *= 0.55
	}
	d.fill(w, t, d.pal.Slice, alpha*opacity)
	d.border(w, t, opacity)
	if emphasis > 0.01 && it.Enabled {
		d.fill(w, t, d.pal.Accent, 0.82*clamp(emphasis, 0, 1)*opacity)
	}
	d.label(it, (d.r1+d.r2)/2, mid, span*(d.r1+d.r2)/2-10, d.r2-d.r1-16, hot && it.Enabled, t, opacity)
}

func (d *drawer) ring() {
	m := d.m
	d.shadow(d.r2, m.averageOpen()*m.shadowCoverage())
	n := m.Count()
	if n == 0 {
		var p Path
		p.Circle(d.center, d.r2, true)
		p.Circle(d.center, d.r1, false)
		d.fill(&p, Identity, d.pal.Slice, m.SliceOpacity)
		d.border(&p, Identity, 1)
	}
	for i := range n {
		if m.flipping && m.sliceFlipWidth(false, i) == 0 {
			continue
		}
		open := 1.0
		if i < len(m.open) {
			open = m.open[i]
		}
		if open <= 0.001 {
			continue
		}
		hover := 0.0
		if i < len(m.hover) {
			hover = m.hover[i].v
		}
		mid, span := MidAngle(i, n), Span(n)
		d.slice(&m.items[i], mid, span, clamp(open, 0, 1), hover, i == m.ActiveIndex(), d.sliceTransform(mid, open, hover).Then(m.sliceFlipTransform(false, i)))
	}
	for i := range m.oldFace {
		s := &m.oldFace[i]
		if m.sliceFlipWidth(true, i) == 0 || s.open <= 0.001 {
			continue
		}
		d.slice(&s.item, s.mid, s.span, clamp(s.open, 0, 1), s.emphasis, s.hot, d.sliceTransform(s.mid, s.open, s.hover).Then(m.sliceFlipTransform(true, i)))
	}
}

// sliceTransform pops a hovered slice out and scales an opening one up from the centre with a small twist.
func (d *drawer) sliceTransform(mid, open, hover float64) Matrix {
	offset := PointAt(Point{}, hover*PopOut, mid)
	scale := 0.55 + 0.45*open
	rot := (1 - math.Min(open, 1)) * -0.35
	s := scale * (1 + hover*0.025)
	return Translate(-d.center.X, -d.center.Y).Then(Rotate(rot)).Then(Scale(s, s)).Then(Translate(d.center.X+offset.X, d.center.Y+offset.Y))
}

func (d *drawer) label(it *Item, radius, angle, maxWidth, maxHeight float64, hot bool, t Matrix, opacity float64) {
	fg := d.pal.Secondary
	switch {
	case hot:
		fg = d.pal.OnAccent
	case it.Enabled:
		fg = d.pal.Foreground
	}
	width := clamp(maxWidth, 30, 120)
	label := d.r.layout(it.Label, true, d.r.labelSize(it.Label, width), width, 2)
	var caption *Text
	if it.Caption != "" && !strings.EqualFold(it.Caption, it.Label) && label.Height+14 < maxHeight {
		lines := 1
		if label.Height+28 < maxHeight {
			lines = 2
		}
		caption = d.r.layout(it.Caption, false, 10.5, width, lines)
	}
	total := label.Height
	if caption != nil {
		total += caption.Height
	}
	anchor := PointAt(d.center, radius, angle)
	y := anchor.Y - total/2
	d.c.DrawText(d.r.Fonts, label, anchor.X, y, t, fg, opacity*d.op)
	if caption != nil {
		cc := d.pal.Secondary
		if hot {
			cc = d.pal.OnAccent
		}
		d.c.DrawText(d.r.Fonts, caption, anchor.X, y+label.Height, t, cc, opacity*d.op)
	}
}

func (d *drawer) hub() {
	m := d.m
	radius := (d.r1 - 6) * (1 + m.centerPulse.v*0.06)
	d.c.FillCircle(d.center, radius, Identity, d.pal.Center, d.op)
	if d.pal.SliceBorder.A > 0 {
		d.c.StrokeCircle(d.center, radius, 1, Identity, d.pal.SliceBorder, d.op)
	}
	if m.extCenterHot {
		d.c.FillCircle(d.center, radius, Identity, d.pal.Danger, 0.18*d.op)
	}
	maxWidth := radius * 1.6
	title := d.r.layout(m.CenterTitle, true, 14, maxWidth, 2)
	sub := d.r.layout(m.CenterSubtitle, false, 11.5, maxWidth, 2)
	const arrow, mouseW, iconGap, mouseGap = 9.0, 7.0, 4.0, 6.0
	icons := arrow + iconGap
	if m.CenterHintWheel {
		icons += mouseW + mouseGap
	}
	var hint *Text
	if m.CenterHint != "" {
		hint = d.r.layout(m.CenterHint, false, 10.5, maxWidth*0.85-icons, 1)
	}
	total := title.Height
	if m.CenterSubtitle != "" {
		total += sub.Height + 2
	}
	if hint != nil {
		total += hint.Height + 5
	}
	y := d.center.Y - total/2
	d.c.DrawText(d.r.Fonts, title, d.center.X, y, Identity, d.pal.Foreground, d.op)
	y += title.Height
	if m.CenterSubtitle != "" {
		col := d.pal.Secondary
		if m.extCenterHot {
			col = d.pal.Danger
		}
		d.c.DrawText(d.r.Fonts, sub, d.center.X, y+2, Identity, col, d.op)
		y += sub.Height + 2
	}
	if hint != nil {
		op := 0.85 * d.op
		left := d.center.X - (icons+hint.Width)/2
		middle := y + 5 + hint.Height/2
		x := left
		if m.CenterHintWheel {
			d.mouse(x, middle-5, mouseW, 10, op)
			x += mouseW + mouseGap
		}
		d.flipArrow(Point{x + arrow/2, middle}, arrow/2, op)
		d.c.DrawText(d.r.Fonts, hint, left+icons+hint.Width/2, y+5, Identity, d.pal.Secondary, op)
	}
}

// flipArrow is a small arrow turning clockwise, the "flip" in the hub hint.
func (d *drawer) flipArrow(c Point, r, op float64) {
	at := func(a float64) Point { return Point{c.X + r*math.Cos(a), c.Y + r*math.Sin(a)} }
	start, sweep := -math.Pi/3, math.Pi*5/3
	var pts []Point
	for i := 0; i <= 24; i++ {
		pts = append(pts, at(start+sweep*float64(i)/24))
	}
	st := StrokeStyle{Width: 1.2, Round: true}
	d.c.Stroke(pts, false, Identity, st, d.pal.Secondary, op)
	end := start + sweep
	tip := at(end)
	along := Point{-math.Sin(end), math.Cos(end)}
	out := Point{math.Cos(end), math.Sin(end)}
	head := r * 0.75
	back := tip.Sub(along.Mul(head))
	d.c.Stroke([]Point{back.Add(out.Mul(head * 0.6)), tip, back.Sub(out.Mul(head * 0.6))}, false, Identity, st, d.pal.Secondary, op)
}

func (d *drawer) mouse(x, y, w, h, op float64) {
	r := w / 2
	var pts []Point
	for i := 0; i <= 12; i++ {
		a := math.Pi * float64(i) / 12
		pts = append(pts, Point{x + r - r*math.Cos(a), y + r - r*math.Sin(a)})
	}
	for i := 0; i <= 12; i++ {
		a := math.Pi * float64(i) / 12
		pts = append(pts, Point{x + r + r*math.Cos(a), y + h - r + r*math.Sin(a)})
	}
	d.c.Stroke(pts, true, Identity, StrokeStyle{Width: 1.1}, d.pal.Secondary, op)
	d.c.Stroke([]Point{{x + r, y + 2}, {x + r, y + 4.5}}, false, Identity, StrokeStyle{Width: 1.1}, d.pal.Secondary, op)
}

// hitBackdrop is nearly transparent fill under a group's fan and the gap to
// it, so the pointer and drags aren't let through on their way to the ring.
func (d *drawer) hitBackdrop(group, count int) {
	m := d.m
	reach := m.subOuter() + backdropMargin
	span := math.Min(2*math.Pi, m.subSpanOf(count)*float64(count)+2*backdropMargin/reach)
	black := color.NRGBA{A: 1}
	d.c.FillCircle(d.center, m.OuterRadius+PopOut, Identity, black, 1)
	if span >= 2*math.Pi-0.001 {
		var p Path
		p.Circle(d.center, reach, true)
		d.c.Fill(&p, Identity, black, 1)
		return
	}
	d.c.Fill(d.wedgeSharp(m.OuterRadius, reach, MidAngle(group, m.Count()), span), Identity, black, 1)
}

func (d *drawer) groupRing(group int, children []Item, parentMid, subSpan, expansion float64, subHover int, expanded bool, opening float64, old bool) {
	m := d.m
	inner, outer := m.subRadii()
	t := math.Max(0, expansion)
	anchor := PointAt(d.center, inner, parentMid)
	s := 0.8 + 0.2*t
	tr := about(anchor, Scale(s, s)).Then(m.sliceFlipTransform(old, group))
	op := clamp(t, 0, 1) * opening
	for k := range children {
		it := &children[k]
		mid := SubMidAngle(parentMid, k, len(children), subSpan)
		w := d.wedge(inner, outer, mid, subSpan)
		alpha := m.SliceOpacity
		if !it.Enabled {
			alpha *= 0.55
		}
		d.fill(w, tr, d.pal.Slice, alpha*op)
		d.border(w, tr, op)
		hot := k == subHover && expanded && it.Enabled
		if hot {
			d.fill(w, tr, d.pal.Accent, 0.82*op)
		}
		d.label(it, (inner+outer)/2, mid, subSpan*(inner+outer)/2-8, subBand-8, hot, tr, op)
	}
}

func (d *drawer) progress() {
	m := d.m
	n := m.Count()
	end := m.collapseEnd()
	all := 1.0
	if end > 0 {
		all = clamp(m.collapseClock/end, 0, 1)
	}
	if all < 1 {
		d.shadow(d.r2, 1-all)
		for i := range n {
			t := clamp((m.collapseClock-float64(i)*collapseStagger)/collapseSeconds, 0, 1)
			e := easeInCubic(t)
			if e >= 0.999 {
				continue
			}
			s := 1 - 0.8*e
			tr := about(d.center, Rotate(e*0.25).Then(Scale(s, s)))
			mid, span := MidAngle(i, n), Span(n)
			hot := i == m.ActiveIndex()
			em := 0.0
			if hot {
				em = 0.8 / 0.82
			}
			d.slice(&m.items[i], mid, span, 1-e, em, hot, tr)
		}
		hole := lerp(d.r1-6, plateRadius, easeInCubic(all))
		d.c.FillCircle(d.center, hole, Identity, d.pal.Center, d.op)
		if d.pal.SliceBorder.A > 0 {
			d.c.StrokeCircle(d.center, hole, 1, Identity, d.pal.SliceBorder, d.op)
		}
		return
	}
	fade, hand := 0.0, 0.0
	if m.fadeClock >= 0 {
		fade = clamp(m.fadeClock/fadeSeconds, 0, 1)
	}
	if m.handOffClock >= 0 {
		hand = clamp(m.handOffClock/handOffSeconds, 0, 1)
	}
	scale := math.Max(0, m.ring.v) * (1 - 0.15*fade) * (1 - 0.7*easeInCubic(hand))
	op := (1 - fade) * (1 - hand)
	if scale <= 0.01 || op <= 0.01 {
		return
	}
	tr := about(d.center, Scale(scale, scale))
	o := op * d.op
	// The plate's shadow (scaled with the ring).
	sr := (plateRadius + 10) * scale
	d.c.FillRadial(tr.Apply(Point{d.center.X, d.center.Y + 3}), sr, []GradientStop{
		{0, color.NRGBA{}}, {(plateRadius - 8) / (plateRadius + 10), color.NRGBA{}}, {plateRadius / (plateRadius + 10), color.NRGBA{A: 52}}, {1, color.NRGBA{}},
	}, o)
	d.c.FillCircle(d.center, plateRadius, tr, d.pal.Center, o)
	if d.pal.SliceBorder.A > 0 {
		d.c.StrokeCircle(d.center, plateRadius, 1, tr, d.pal.SliceBorder, o)
	}
	d.c.StrokeCircle(d.center, RingRadius, ringStroke, tr, d.pal.Secondary, 0.22*o)
	arc := d.pal.Accent
	switch m.outcome {
	case Succeeded:
		arc = d.pal.Success
	case Failed:
		arc = d.pal.Danger
	}
	sweep := m.shownProgress
	if m.outcome == Failed {
		sweep = math.Max(sweep, 0.02)
	}
	if sweep >= 0.999 {
		d.c.StrokeCircle(d.center, RingRadius, ringStroke, tr, arc, o)
	} else if sweep > 0.001 {
		var pts []Point
		steps := max(2, int(sweep*96))
		for i := 0; i <= steps; i++ {
			pts = append(pts, PointAt(d.center, RingRadius, sweep*2*math.Pi*float64(i)/float64(steps)))
		}
		d.c.Stroke(pts, false, tr, StrokeStyle{Width: ringStroke, Round: true}, arc, o)
	}
	badge := 0.0
	if m.outcome != NoOutcome {
		badge = math.Max(0, m.badge.v)
	}
	if badge < 0.98 {
		pct := d.r.layout(fmt.Sprintf("%.0f%%", math.Round(m.shownProgress*100)), true, 15, RingRadius*1.6, 1)
		d.c.DrawText(d.r.Fonts, pct, d.center.X, d.center.Y-pct.Height/2, tr, d.pal.Foreground, o*(1-clamp(badge, 0, 1)))
	}
	if m.outcome != NoOutcome && m.outcome != Cancelled && badge > 0.01 {
		fill := d.pal.Success
		if m.outcome == Failed {
			fill = d.pal.Danger
		}
		d.c.FillCircle(d.center, 22*badge, tr, fill, o)
		bt := about(d.center, Scale(badge, badge)).Then(tr)
		if m.outcome == Succeeded {
			cx, cy := d.center.X, d.center.Y
			d.c.Stroke([]Point{{cx - 9, cy + 0.5}, {cx - 2.5, cy + 7}, {cx + 10, cy - 6.5}}, false, bt, StrokeStyle{Width: 3.6, Round: true}, d.pal.OnAccent, o)
		} else {
			mark := d.r.layout("!", true, 22, 30, 1)
			d.c.DrawText(d.r.Fonts, mark, d.center.X, d.center.Y-mark.Height/2, bt, d.pal.OnAccent, o)
		}
	}
	if m.ringHover && m.outcome == NoOutcome && m.handOffClock < 0 {
		at := m.cancelButtonCenter()
		d.c.FillCircle(at, cancelButtonSize+1, tr, d.pal.Center, o)
		d.c.FillCircle(at, cancelButtonSize, tr, d.pal.Foreground, o)
		// The × of Material's "close" icon, 10 points wide.
		h := 3.0
		st := StrokeStyle{Width: 1.6, Round: true}
		d.c.Stroke([]Point{{at.X - h, at.Y - h}, {at.X + h, at.Y + h}}, false, tr, st, d.pal.Center, o)
		d.c.Stroke([]Point{{at.X - h, at.Y + h}, {at.X + h, at.Y - h}}, false, tr, st, d.pal.Center, o)
	}
}
