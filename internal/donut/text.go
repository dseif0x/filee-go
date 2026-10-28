package donut

import (
	"image/color"
	"math"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Fonts are the faces the donut writes with: an embedded Latin font (the Go
// fonts) and fallbacks for the scripts it lacks (on macOS the system's CJK
// fonts, added with AddFallback). Safe for concurrent use.
type Fonts struct {
	mu      sync.Mutex
	bold    []*sfnt.Font
	regular []*sfnt.Font
	buf     sfnt.Buffer
	glyphs  map[glyphKey][]op
}

// lineFactor is a line's height per point of font size (Noto Sans, the
// original's font, has 1.362).
const lineFactor = 1.362

var (
	defaultOnce  sync.Once
	defaultFonts *Fonts
)

// DefaultFonts are the embedded fonts (shared; fallbacks added to it are seen by every user).
func DefaultFonts() *Fonts {
	defaultOnce.Do(func() {
		b, err1 := opentype.Parse(gomedium.TTF)
		r, err2 := opentype.Parse(goregular.TTF)
		if err1 != nil || err2 != nil {
			panic("donut: embedded fonts don't parse")
		}
		defaultFonts = &Fonts{bold: []*sfnt.Font{b}, regular: []*sfnt.Font{r}}
	})
	return defaultFonts
}

// AddFallback adds a font file (TrueType, OpenType or a collection) behind
// the fonts there are, for characters they lack.
func (f *Fonts) AddFallback(data []byte) error {
	var faces []*sfnt.Font
	if c, err := opentype.ParseCollection(data); err == nil {
		for i := range c.NumFonts() {
			if face, err := c.Font(i); err == nil {
				faces = append(faces, face)
			}
		}
	} else {
		face, err := opentype.Parse(data)
		if err != nil {
			return err
		}
		faces = append(faces, face)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bold = append(f.bold, faces...)
	f.regular = append(f.regular, faces...)
	return nil
}

type glyph struct {
	font *sfnt.Font
	idx  sfnt.GlyphIndex
	x    float64
}

type line struct {
	glyphs []glyph
	width  float64
}

// Text is laid-out text: lines centred in a box MaxWidth wide.
type Text struct {
	lines    []line
	size     float64
	MaxWidth float64
	Height   float64
	Width    float64 // the widest line
}

func ppem(size float64) fixed.Int26_6 { return fixed.Int26_6(math.Round(size * 64)) }

// glyphFor finds the first face with a glyph for r (the primary's .notdef when none has).
func (f *Fonts) glyphFor(faces []*sfnt.Font, r rune) (*sfnt.Font, sfnt.GlyphIndex) {
	for _, face := range faces {
		if i, err := face.GlyphIndex(&f.buf, r); err == nil && i != 0 {
			return face, i
		}
	}
	return faces[0], 0
}

func (f *Fonts) advance(face *sfnt.Font, i sfnt.GlyphIndex, size float64) float64 {
	a, err := face.GlyphAdvance(&f.buf, i, ppem(size), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(a) / 64
}

// Measure is the width of s on one line.
func (f *Fonts) Measure(s string, bold bool, size float64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shape([]rune(s), f.faces(bold), size).width
}

func (f *Fonts) faces(bold bool) []*sfnt.Font {
	if bold {
		return f.bold
	}
	return f.regular
}

// shape places the runes on one line (with kerning within a face).
func (f *Fonts) shape(rs []rune, faces []*sfnt.Font, size float64) line {
	var l line
	x := 0.0
	var prev glyph
	for k, r := range rs {
		if r == '⁠' || r == '\n' || r == '\r' {
			continue
		}
		face, i := f.glyphFor(faces, r)
		if k > 0 && prev.font == face {
			if kern, err := face.Kern(&f.buf, prev.idx, i, ppem(size), font.HintingNone); err == nil {
				x += float64(kern) / 64
			}
		}
		g := glyph{font: face, idx: i, x: x}
		l.glyphs = append(l.glyphs, g)
		x += f.advance(face, i, size)
		prev = g
	}
	l.width = x
	return l
}

// breaksAfter: a line may break after rs[i] (a space, or between two
// Chinese or Japanese characters). Korean words are kept together.
func breaksAfter(rs []rune, i int) bool {
	if i+1 >= len(rs) {
		return true
	}
	if unicode.IsSpace(rs[i]) {
		return true
	}
	return isHanOrKana(rs[i]) || isHanOrKana(rs[i+1])
}

func isHanOrKana(c rune) bool {
	return c >= 0x2E80 && c <= 0x312F || c >= 0x3190 && c <= 0x9FFF || c >= 0xF900 && c <= 0xFAFF || c >= 0xFF00 && c <= 0xFFEF
}

// Layout wraps s into at most maxLines lines of maxWidth, centred; the last
// line ends in "…" when the text doesn't fit (as Avalonia's CharacterEllipsis).
func (f *Fonts) Layout(s string, bold bool, size, maxWidth float64, maxLines int) *Text {
	f.mu.Lock()
	defer f.mu.Unlock()
	maxWidth = math.Max(10, maxWidth)
	maxLines = max(1, maxLines)
	faces := f.faces(bold)
	t := &Text{size: size, MaxWidth: maxWidth}
	rs := []rune(strings.TrimSpace(s))
	if len(rs) == 0 {
		t.Height = size * lineFactor
		return t
	}
	width := func(a, b int) float64 { return f.shape(trimSpaces(rs[a:b]), faces, size).width }
	start := 0
	for start < len(rs) && len(t.lines) < maxLines {
		last := len(t.lines) == maxLines-1
		// The longest run from start that fits, ending at a break opportunity.
		end := -1
		for i := start; i < len(rs); i++ {
			if breaksAfter(rs, i) {
				if width(start, i+1) <= maxWidth {
					end = i + 1
				} else {
					break
				}
			}
		}
		if end < 0 {
			// One word wider than the line: as many characters as fit.
			end = start + 1
			for end < len(rs) && width(start, end+1) <= maxWidth {
				end++
			}
		}
		if last && end < len(rs) {
			t.lines = append(t.lines, f.ellipsized(rs[start:], faces, size, maxWidth))
			start = len(rs)
			break
		}
		t.lines = append(t.lines, f.shape(trimSpaces(rs[start:end]), faces, size))
		start = end
		for start < len(rs) && unicode.IsSpace(rs[start]) {
			start++
		}
	}
	for _, l := range t.lines {
		t.Width = math.Max(t.Width, l.width)
	}
	t.Height = float64(len(t.lines)) * size * lineFactor
	return t
}

func trimSpaces(rs []rune) []rune {
	for len(rs) > 0 && unicode.IsSpace(rs[len(rs)-1]) {
		rs = rs[:len(rs)-1]
	}
	for len(rs) > 0 && unicode.IsSpace(rs[0]) {
		rs = rs[1:]
	}
	return rs
}

// ellipsized is as much of rs as fits with "…" after it.
func (f *Fonts) ellipsized(rs []rune, faces []*sfnt.Font, size, maxWidth float64) line {
	rs = trimSpaces(rs)
	if l := f.shape(rs, faces, size); l.width <= maxWidth {
		return l
	}
	for n := len(rs) - 1; n > 0; n-- {
		cut := append(trimSpaces(append([]rune(nil), rs[:n]...)), '…')
		if l := f.shape(cut, faces, size); l.width <= maxWidth {
			return l
		}
	}
	return f.shape([]rune{'…'}, faces, size)
}

type glyphKey struct {
	font *sfnt.Font
	idx  sfnt.GlyphIndex
	ppem fixed.Int26_6
}

// outline is a glyph's figure relative to its origin on the baseline (cached:
// every frame of an animation draws the same labels).
func (f *Fonts) outline(face *sfnt.Font, idx sfnt.GlyphIndex, size float64) []op {
	key := glyphKey{face, idx, ppem(size)}
	if ops, ok := f.glyphs[key]; ok {
		return ops
	}
	segs, err := face.LoadGlyph(&f.buf, idx, key.ppem, nil)
	var ops []op
	if err == nil {
		pt := func(q fixed.Point26_6) Point { return Point{float64(q.X) / 64, float64(q.Y) / 64} }
		open := false
		for _, s := range segs {
			switch s.Op {
			case sfnt.SegmentOpMoveTo:
				if open {
					ops = append(ops, op{kind: opClose})
				}
				ops = append(ops, op{kind: opMove, p: [3]Point{pt(s.Args[0])}})
				open = true
			case sfnt.SegmentOpLineTo:
				ops = append(ops, op{kind: opLine, p: [3]Point{pt(s.Args[0])}})
			case sfnt.SegmentOpQuadTo:
				ops = append(ops, op{kind: opQuad, p: [3]Point{pt(s.Args[0]), pt(s.Args[1])}})
			case sfnt.SegmentOpCubeTo:
				ops = append(ops, op{kind: opCube, p: [3]Point{pt(s.Args[0]), pt(s.Args[1]), pt(s.Args[2])}})
			}
		}
		if open {
			ops = append(ops, op{kind: opClose})
		}
	}
	if f.glyphs == nil || len(f.glyphs) > 4096 {
		f.glyphs = map[glyphKey][]op{}
	}
	f.glyphs[key] = ops
	return ops
}

// DrawText draws t centred on centerX with its top at top, transformed by m.
func (c *Canvas) DrawText(fonts *Fonts, t *Text, centerX, top float64, m Matrix, col color.NRGBA, opacity float64) {
	if t == nil || len(t.lines) == 0 {
		return
	}
	fonts.mu.Lock()
	var p Path
	lh := t.size * lineFactor
	for k, l := range t.lines {
		left := centerX - l.width/2
		// The baseline sits where Noto Sans puts it in its line (ascender 1.069 of 1.362).
		baseline := top + float64(k)*lh + t.size*1.069
		for _, g := range l.glyphs {
			o := Point{left + g.x, baseline}
			for _, gop := range fonts.outline(g.font, g.idx, t.size) {
				gop.p[0], gop.p[1], gop.p[2] = gop.p[0].Add(o), gop.p[1].Add(o), gop.p[2].Add(o)
				p.ops = append(p.ops, gop)
			}
		}
	}
	fonts.mu.Unlock()
	c.Fill(&p, m, col, opacity)
}
