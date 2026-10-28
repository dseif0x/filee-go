package cad

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// TEXT, ATTRIB and MTEXT for the scene: justification, rotation, width
// factor, oblique angle, mirroring, and MTEXT formatting codes with word wrap.

const (
	// linePitch is AutoCAD's MTEXT line pitch: 5/3 of the text height.
	linePitch = 5.0 / 3.0
	// ascentEm is the top of the text box above the baseline for extents (covers accents and CJK).
	ascentEm = 0.95
)

func (b *builder) drawText(t *cadoc.TextEntity, value string, e cadoc.Entity, layer *cadoc.Layer, c drawContext) {
	content := DecodeText(value)
	if strings.TrimSpace(content) == "" {
		return
	}
	style := t.Style
	height := t.Height
	if height <= 0 {
		if style != nil && style.Height > 0 {
			height = style.Height
		} else {
			height = 2.5
		}
	}
	face := typefaceOf(style, "", false, false)
	capHeight, descent := Metrics(face)
	em := height / capHeight
	widthFactor := t.WidthFactor
	if widthFactor <= 0 {
		widthFactor = 1
	}
	runs := Layout(content, face)
	width := 0.0
	for _, r := range runs {
		width += r.Advance
	}
	width *= em * widthFactor

	// Justified text is positioned by its second alignment point; aligned and fit stretch between both.
	h, v := t.HorizontalAlignment, t.VerticalAlignment
	justified := h != cadoc.TextHorizontalAlignmentLeft || v != cadoc.TextVerticalAlignmentTypeBaseline
	anchor := v3(t.InsertPoint)
	if justified {
		anchor = v3(t.AlignmentPoint)
	}
	rotation := t.Rotation
	if (h == cadoc.TextHorizontalAlignmentAligned || h == cadoc.TextHorizontalAlignmentFit) && width > 0 {
		first := v3(t.InsertPoint)
		span := v3(t.AlignmentPoint).Sub(first)
		length := math.Hypot(span.X, span.Y)
		if length > 1e-12 {
			anchor = first
			rotation = math.Atan2(span.Y, span.X)
			if h == cadoc.TextHorizontalAlignmentAligned {
				em *= length / width
				height *= length / width
			} else {
				widthFactor *= length / width
			}
			width = length
		}
	}
	x := 0.0
	switch h {
	case cadoc.TextHorizontalAlignmentCenter, cadoc.TextHorizontalAlignmentMiddle:
		x = -width / 2
	case cadoc.TextHorizontalAlignmentRight:
		x = -width
	}
	y := 0.0
	if h == cadoc.TextHorizontalAlignmentMiddle {
		y = -height / 2
	} else {
		switch v {
		case cadoc.TextVerticalAlignmentTypeBottom:
			y = descent * em
		case cadoc.TextVerticalAlignmentTypeMiddle:
			y = -height / 2
		case cadoc.TextVerticalAlignmentTypeTop:
			y = -height
		}
	}
	// Mirrored text (MIRRTEXT, backward / upside-down styles) flips around the anchor.
	flipX, flipY := 1.0, 1.0
	if t.Mirror&cadoc.TextMirrorFlagBackward != 0 {
		flipX = -1
	}
	if t.Mirror&cadoc.TextMirrorFlagUpsideDown != 0 {
		flipY = -1
	}
	frame := c.transform.Multiply(Ocs(v3(t.Normal))).Multiply(Translation(anchor)).Multiply(RotationZ(rotation)).Multiply(Scale3(flipX, flipY, 1))
	b.emitLine(runs, frame, x, y, em, widthFactor, t.ObliqueAngle, resolveColor(e.Entity().Color, layer, c))
}

func (b *builder) drawMText(m *cadoc.MText, layer *cadoc.Layer, c drawContext) {
	style := m.Style
	height := m.Height
	if height <= 0 {
		if style != nil && style.Height > 0 {
			height = style.Height
		} else {
			height = 2.5
		}
	}
	b.drawMTextCore(m.Value, v3(m.InsertPoint), v3(m.AlignmentPoint), v3(m.Normal), height, m.RectangleWidth,
		m.AttachmentPoint, m.LineSpacing, style, resolveColor(m.Color, layer, c), c)
}

// piece is one placed part of an MTEXT line.
type piece struct {
	text   string
	format MTextFormat
	width  float64
}

// drawMTextCore lays out MTEXT: paragraphs word-wrapped at wrapWidth (CJK
// text breaks between characters), lines stacked with AutoCAD's pitch and the
// block placed by its attachment point.
func (b *builder) drawMTextCore(value string, insert, direction, normal Vec3, height, wrapWidth float64,
	attachment cadoc.AttachmentPointType, lineSpacing float64, style *cadoc.TextStyle, color RGB, c drawContext) {
	if value == "" {
		return
	}
	var flags cadoc.FontFlags
	if style != nil {
		flags = style.TrueType
	}
	initial := MTextFormat{Height: height, ColorIndex: -1, TrueColor: -1,
		Bold: flags&cadoc.FontFlagsBold != 0, Italic: flags&cadoc.FontFlagsItalic != 0, WidthFactor: 1}
	if style != nil {
		if style.Width > 0 {
			initial.WidthFactor = style.Width
		}
		initial.ObliqueDegrees = style.ObliqueAngle * 180 / math.Pi
	}
	paragraphs := ParseMText(value, initial)
	limit := math.Inf(1)
	if wrapWidth > 0 {
		limit = wrapWidth
	}
	var lines [][]piece
	for _, p := range paragraphs {
		lines = wrapParagraph(p, limit, style, lines)
	}
	empty := true
	for _, l := range lines {
		for _, p := range l {
			if strings.TrimSpace(p.text) != "" {
				empty = false
			}
		}
	}
	if empty {
		return
	}
	// Baselines from the top: the first line's caps touch the top, then one pitch per line.
	baselines := make([]float64, len(lines))
	y := 0.0
	spacing := lineSpacing
	if spacing <= 0 {
		spacing = 1
	}
	for i, l := range lines {
		lh := height
		if len(l) > 0 {
			lh = 0
			for _, p := range l {
				lh = math.Max(lh, p.format.Height)
			}
		}
		if i == 0 {
			y -= lh
		} else {
			y -= lh * linePitch * spacing
		}
		baselines[i] = y
	}
	total := -y
	shiftY := 0.0
	switch attachment {
	case cadoc.AttachmentPointTypeMiddleLeft, cadoc.AttachmentPointTypeMiddleCenter, cadoc.AttachmentPointTypeMiddleRight:
		shiftY = total / 2
	case cadoc.AttachmentPointTypeBottomLeft, cadoc.AttachmentPointTypeBottomCenter, cadoc.AttachmentPointTypeBottomRight:
		shiftY = total
	}
	// MTEXT's direction is in WCS; a zero vector means the OCS X axis.
	ocs := Ocs(normal)
	xAxis := ocs.X
	if direction.Length() > 1e-12 {
		xAxis = direction.Normalized()
	}
	zAxis := ocs.Z
	yAxis := Cross(zAxis, xAxis).Normalized()
	frame := c.transform.Multiply(Affine3{X: xAxis, Y: yAxis, Z: zAxis, T: insert})
	for i, raw := range lines {
		line := trimEnd(raw)
		lw := 0.0
		for _, p := range line {
			lw += p.width
		}
		x := 0.0
		switch attachment {
		case cadoc.AttachmentPointTypeTopCenter, cadoc.AttachmentPointTypeMiddleCenter, cadoc.AttachmentPointTypeBottomCenter:
			x = -lw / 2
		case cadoc.AttachmentPointTypeTopRight, cadoc.AttachmentPointTypeMiddleRight, cadoc.AttachmentPointTypeBottomRight:
			x = -lw
		}
		for _, p := range line {
			f := p.format
			face := typefaceOf(style, f.FontFamily, f.Bold, f.Italic)
			capHeight, _ := Metrics(face)
			em := f.Height / capHeight
			pc := color
			switch {
			case f.TrueColor >= 0:
				pc = RGB{uint8(f.TrueColor >> 16), uint8(f.TrueColor >> 8), uint8(f.TrueColor)}
			case f.ColorIndex >= 0 && f.ColorIndex != 256:
				if f.ColorIndex == 0 {
					pc = c.blockColor
				} else {
					pc = resolveColor(cadoc.ColorIndex(int16(f.ColorIndex)), defaultLayer, c)
				}
			}
			b.emitLine(Layout(p.text, face), frame, x, baselines[i]+shiftY, em, f.WidthFactor, f.ObliqueDegrees*math.Pi/180, pc)
			x += p.width
		}
	}
}

// wrapParagraph is a greedy word wrap of one paragraph.
func wrapParagraph(paragraph []MTextRun, limit float64, style *cadoc.TextStyle, lines [][]piece) [][]piece {
	var line []piece
	width := 0.0
	for _, run := range paragraph {
		face := typefaceOf(style, run.Format.FontFamily, run.Format.Bold, run.Format.Italic)
		capHeight, _ := Metrics(face)
		em := run.Format.Height / capHeight * run.Format.WidthFactor
		for _, word := range words(run.Text) {
			ww := advance(word, face) * em
			ink := advance(strings.TrimRight(word, " "), face) * em
			if len(line) > 0 && width+ink > limit {
				lines = append(lines, line)
				line = nil
				width = 0
				if strings.TrimSpace(word) == "" {
					continue // spaces at a line break disappear
				}
			}
			line = append(line, piece{word, run.Format, ww})
			width += ww
		}
	}
	return append(lines, merge(line))
}

func advance(text string, face Typeface) float64 {
	a := 0.0
	for _, r := range Layout(text, face) {
		a += r.Advance
	}
	return a
}

// words splits text into wrap units: words with their trailing spaces, and single CJK characters.
func words(text string) []string {
	var out []string
	rs := []rune(text)
	start := 0
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		if isCJK(ch) {
			if i > start {
				out = append(out, string(rs[start:i]))
			}
			end := i + 1
			for end < len(rs) && rs[end] == ' ' {
				end++
			}
			out = append(out, string(rs[i:end]))
			start = end
			i = end - 1
		} else if ch == ' ' && (i+1 >= len(rs) || rs[i+1] != ' ') {
			out = append(out, string(rs[start:i+1]))
			start = i + 1
		}
	}
	if start < len(rs) {
		out = append(out, string(rs[start:]))
	}
	return out
}

func isCJK(ch rune) bool {
	return ch >= 0x1100 && ch <= 0x11FF || ch >= 0x2E80 && ch <= 0x9FFF || ch >= 0xAC00 && ch <= 0xD7AF ||
		ch >= 0xF900 && ch <= 0xFAFF || ch >= 0xFF00 && ch <= 0xFFEF
}

// merge joins neighbouring pieces with the same format.
func merge(line []piece) []piece {
	var out []piece
	for _, p := range line {
		if n := len(out); n > 0 && out[n-1].format == p.format {
			out[n-1].text += p.text
			out[n-1].width += p.width
		} else {
			out = append(out, p)
		}
	}
	return out
}

// trimEnd drops trailing spaces so right and centre alignment use the visible width.
func trimEnd(line []piece) []piece {
	out := merge(line)
	if n := len(out); n > 0 {
		last := out[n-1]
		trimmed := strings.TrimRight(last.text, " \t")
		if len(trimmed) != len(last.text) {
			ratio := 0.0
			if c := utf8.RuneCountInString(last.text); c > 0 {
				ratio = float64(utf8.RuneCountInString(trimmed)) / float64(c)
			}
			out[n-1] = piece{trimmed, last.format, last.width * ratio}
		}
	}
	return out
}

// emitLine emits the runs of one line with its baseline starting at (x, y) in frame's XY plane.
func (b *builder) emitLine(runs []GlyphRun, frame Affine3, x, y, em, widthFactor, oblique float64, color RGB) {
	// Oblique angles are stored in [0, 2π): 345° leans left like -15°; steeper than ±85° is not allowed.
	shear := math.Tan(math.Max(-1.48, math.Min(1.48, math.Remainder(oblique, 2*math.Pi))))
	xAxis := frame.VectorXY(em*widthFactor, 0)
	yAxis := frame.VectorXY(-em*shear, -em)
	for _, r := range runs {
		adv := r.Advance * em * widthFactor
		if strings.TrimSpace(r.Text) != "" && b.reserve() {
			b.scene.Items = append(b.scene.Items, &TextPrimitive{
				Text: r.Text, Face: r.Face, Origin: frame.PointXY(x, y), XAxis: xAxis, YAxis: yAxis, Color: color,
			})
			for _, rise := range []float64{-0.2 * em, ascentEm * em} {
				b.scene.Extents.Add(frame.PointXY(x+rise*shear, y+rise))
				b.scene.Extents.Add(frame.PointXY(x+adv+rise*shear, y+rise))
			}
		}
		x += adv
	}
}

// typefaceOf is the typeface of an MTEXT font code, else of the text style.
func typefaceOf(style *cadoc.TextStyle, family string, bold, italic bool) Typeface {
	if family != "" {
		return ResolveFace(family, bold, italic)
	}
	var flags cadoc.FontFlags
	name := ""
	if style != nil {
		flags = style.TrueType
		name = style.Filename
		if name == "" || strings.HasSuffix(strings.ToLower(name), ".shx") {
			if style.FontFamily != "" {
				name = style.FontFamily
			}
		}
	}
	return ResolveFace(name, bold || flags&cadoc.FontFlagsBold != 0, italic || flags&cadoc.FontFlagsItalic != 0)
}
