package fonts

import (
	"fmt"
	"math"
)

// Runs Type 2 charstrings (Adobe Technical Note #5177) and records the outline
// as cubic Bézier contours. Hints are parsed only as far as needed to skip
// them; subroutines, flex, the arithmetic operators and seac accents work.

type vec2 struct{ x, y float64 }

func (a vec2) add(b vec2) vec2    { return vec2{a.x + b.x, a.y + b.y} }
func (a vec2) sub(b vec2) vec2    { return vec2{a.x - b.x, a.y - b.y} }
func (a vec2) mul(s float64) vec2 { return vec2{a.x * s, a.y * s} }
func (a vec2) length() float64    { return math.Sqrt(a.x*a.x + a.y*a.y) }
func dot(a, b vec2) float64       { return a.x*b.x + a.y*b.y }
func (a vec2) apply(m []float64) vec2 {
	return vec2{m[0]*a.x + m[2]*a.y + m[4], m[1]*a.x + m[3]*a.y + m[5]}
}

// pathSegment is a line (curve false) or cubic Bézier segment ending at end.
type pathSegment struct {
	curve       bool
	c1, c2, end vec2
}

// pathContour is a closed contour: a start point and the segments that follow it (the closing line is implied).
type pathContour struct {
	start    vec2
	segments []pathSegment
}

func (c *pathContour) transformed(f func(vec2) vec2) *pathContour {
	out := &pathContour{start: f(c.start)}
	for _, s := range c.segments {
		out.segments = append(out.segments, pathSegment{s.curve, f(s.c1), f(s.c2), f(s.end)})
	}
	return out
}

const maxSubrDepth = 10

type seacArgs struct {
	adx, ady     float64
	base, accent int
}

// type2 interprets the charstrings of one CFF font (not for concurrent use).
type type2 struct {
	font        *cffFont
	stack       [513]float64
	transient   [32]float64
	globalBias  int
	contours    []*pathContour
	current     *pathContour
	priv        cffPrivate
	localBias   int
	count       int
	stems       int
	widthParsed bool
	ended       bool
	x, y        float64
	seac        *seacArgs
}

func newType2(f *cffFont) *type2 { return &type2{font: f, globalBias: subrBias(len(f.globalSubrs))} }

// subrBias is the subroutine number bias, which depends on how many subroutines there are.
func subrBias(count int) int {
	switch {
	case count < 1240:
		return 107
	case count < 33900:
		return 1131
	}
	return 32768
}

// run runs a glyph's charstring and returns its contours (seac accents are drawn in place).
func (t *type2) run(glyph int, allowSeac bool) []*pathContour {
	if glyph < 0 || glyph >= t.font.glyphCount() {
		fail("A CFF accent refers to a missing glyph.")
	}
	t.contours = nil
	t.current = nil
	t.priv = t.font.privates[min(t.font.fontDictIndex(glyph), len(t.font.privates)-1)]
	t.localBias = subrBias(len(t.priv.subrs))
	t.count, t.stems = 0, 0
	t.widthParsed, t.ended = false, false
	t.x, t.y = 0, 0
	t.seac = nil

	t.execute(t.font.charStrings[glyph], 0)
	t.closePath()
	contours := t.contours
	if s := t.seac; s != nil {
		if !allowSeac {
			fail("A CFF accent is itself an accented glyph.")
		}
		base := t.font.glyphForSid(int(cffStandardEncoding[s.base&0xFF]))
		accent := t.font.glyphForSid(int(cffStandardEncoding[s.accent&0xFF]))
		contours = append(contours, t.run(base, false)...)
		offset := vec2{s.adx, s.ady}
		for _, c := range t.run(accent, false) {
			contours = append(contours, c.transformed(func(p vec2) vec2 { return p.add(offset) }))
		}
	}
	return contours
}

func (t *type2) execute(code []byte, depth int) {
	if depth > maxSubrDepth {
		fail("CFF subroutines are nested too deeply.")
	}
	r := newReader(code)
	for r.remaining() > 0 && !t.ended {
		b0 := int(r.u8())
		if b0 >= 32 || b0 == 28 {
			switch {
			case b0 == 28:
				t.push(float64(r.i16()))
			case b0 <= 246:
				t.push(float64(b0 - 139))
			case b0 <= 250:
				t.push(float64((b0-247)*256 + int(r.u8()) + 108))
			case b0 <= 254:
				t.push(float64(-(b0-251)*256 - int(r.u8()) - 108))
			default: // 255: 16.16 fixed
				t.push(float64(r.i32()) / 65536.0)
			}
			continue
		}
		s := &t.stack
		switch b0 {
		case 1, 3, 18, 23: // hstem, vstem, hstemhm, vstemhm
			t.stemHints()
		case 19, 20: // hintmask, cntrmask: pending arguments are an implicit vstemhm
			t.stemHints()
			r.skip((t.stems + 7) / 8)
		case 21: // rmoveto
			t.parseWidth(2)
			t.moveTo(t.arg(0), t.arg(1))
		case 22: // hmoveto
			t.parseWidth(1)
			t.moveTo(t.arg(0), 0)
		case 4: // vmoveto
			t.parseWidth(1)
			t.moveTo(0, t.arg(0))
		case 5: // rlineto
			for i := 0; i+1 < t.count; i += 2 {
				t.lineTo(s[i], s[i+1])
			}
			t.count = 0
		case 6, 7: // hlineto, vlineto: alternating horizontal and vertical lines
			for i := 0; i < t.count; i++ {
				if (i%2 == 0) == (b0 == 6) {
					t.lineTo(s[i], 0)
				} else {
					t.lineTo(0, s[i])
				}
			}
			t.count = 0
		case 8: // rrcurveto
			for i := 0; i+5 < t.count; i += 6 {
				t.curveAt(i)
			}
			t.count = 0
		case 24: // rcurveline
			i := 0
			for ; i+7 < t.count; i += 6 {
				t.curveAt(i)
			}
			if i+1 < t.count {
				t.lineTo(s[i], s[i+1])
			}
			t.count = 0
		case 25: // rlinecurve
			i := 0
			for ; i+7 < t.count; i += 2 {
				t.lineTo(s[i], s[i+1])
			}
			if i+5 < t.count {
				t.curveAt(i)
			}
			t.count = 0
		case 26: // vvcurveto
			i := t.count % 2
			dx1 := 0.0
			if i == 1 {
				dx1 = s[0]
			}
			for ; i+3 < t.count; i, dx1 = i+4, 0 {
				t.curveTo(dx1, s[i], s[i+1], s[i+2], 0, s[i+3])
			}
			t.count = 0
		case 27: // hhcurveto
			i := t.count % 2
			dy1 := 0.0
			if i == 1 {
				dy1 = s[0]
			}
			for ; i+3 < t.count; i, dy1 = i+4, 0 {
				t.curveTo(s[i], dy1, s[i+1], s[i+2], s[i+3], 0)
			}
			t.count = 0
		case 30, 31: // vhcurveto, hvcurveto: curves alternating between vertical and horizontal tangents
			horizontal := b0 == 31
			for i := 0; i+3 < t.count; i, horizontal = i+4, !horizontal {
				last := 0.0
				if t.count-i == 5 {
					last = s[i+4]
				}
				if horizontal {
					t.curveTo(s[i], 0, s[i+1], s[i+2], last, s[i+3])
				} else {
					t.curveTo(0, s[i], s[i+1], s[i+2], s[i+3], last)
				}
			}
			t.count = 0
		case 10: // callsubr
			t.execute(t.subroutine(t.priv.subrs, t.localBias), depth+1)
		case 29: // callgsubr
			t.execute(t.subroutine(t.font.globalSubrs, t.globalBias), depth+1)
		case 11: // return
			return
		case 14: // endchar, optionally with the four seac arguments
			if !t.widthParsed && (t.count == 1 || t.count == 5) {
				t.takeWidth()
			}
			t.widthParsed = true
			if t.count >= 4 {
				t.seac = &seacArgs{s[t.count-4], s[t.count-3], int(s[t.count-2]), int(s[t.count-1])}
			}
			t.closePath()
			t.ended = true
			return
		case 12:
			t.escape(int(r.u8()))
		case 15, 16:
			fail("The font uses CFF2 charstring operators (variable fonts), which aren't supported.")
		default:
			fail(fmt.Sprintf("The CFF charstring uses an unknown operator (%d).", b0))
		}
	}
}

func boolNum(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func (t *type2) escape(op int) {
	s := &t.stack
	switch op {
	case 0: // dotsection (deprecated hint)
		t.count = 0
	case 3: // and
		t.binary(func(a, b float64) float64 { return boolNum(a != 0 && b != 0) })
	case 4: // or
		t.binary(func(a, b float64) float64 { return boolNum(a != 0 || b != 0) })
	case 5: // not
		t.push(boolNum(t.pop() == 0))
	case 9: // abs
		t.push(math.Abs(t.pop()))
	case 10: // add
		t.binary(func(a, b float64) float64 { return a + b })
	case 11: // sub
		t.binary(func(a, b float64) float64 { return a - b })
	case 12: // div
		t.binary(func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		})
	case 14: // neg
		t.push(-t.pop())
	case 15: // eq
		t.binary(func(a, b float64) float64 { return boolNum(a == b) })
	case 18: // drop
		t.pop()
	case 20: // put
		index := int(t.pop())
		v := t.pop()
		if index >= 0 && index < 32 {
			t.transient[index] = v
		}
	case 21: // get
		index := int(t.pop())
		if index >= 0 && index < 32 {
			t.push(t.transient[index])
		} else {
			t.push(0)
		}
	case 22: // ifelse
		v2, v1, s2, s1 := t.pop(), t.pop(), t.pop(), t.pop()
		if v1 <= v2 {
			t.push(s1)
		} else {
			t.push(s2)
		}
	case 23: // random: any value in (0, 1]; a fixed one keeps conversions reproducible
		t.push(0.5)
	case 24: // mul
		t.binary(func(a, b float64) float64 { return a * b })
	case 26: // sqrt
		t.push(math.Sqrt(math.Max(0, t.pop())))
	case 27: // dup
		v := t.pop()
		t.push(v)
		t.push(v)
	case 28: // exch
		b, a := t.pop(), t.pop()
		t.push(b)
		t.push(a)
	case 29: // index
		i := max(int(t.pop()), 0)
		if i < t.count {
			t.push(s[t.count-1-i])
		} else {
			t.push(0)
		}
	case 30: // roll
		j := int(t.pop())
		n := int(t.pop())
		if n > 0 && n <= t.count {
			items := append([]float64(nil), s[t.count-n:t.count]...)
			j = ((j % n) + n) % n
			for i := 0; i < n; i++ {
				s[t.count-n+(i+j)%n] = items[i]
			}
		}
	case 34: // hflex
		t.need(7)
		t.curveTo(s[0], 0, s[1], s[2], s[3], 0)
		t.curveTo(s[4], 0, s[5], -s[2], s[6], 0)
		t.count = 0
	case 35: // flex
		t.need(12)
		t.curveAt(0)
		t.curveAt(6)
		t.count = 0
	case 36: // hflex1
		t.need(9)
		t.curveTo(s[0], s[1], s[2], s[3], s[4], 0)
		t.curveTo(s[5], 0, s[6], s[7], s[8], -(s[1] + s[3] + s[7]))
		t.count = 0
	case 37: // flex1: the last point moves along the dominant direction only
		t.need(11)
		dx, dy := 0.0, 0.0
		for i := 0; i < 10; i += 2 {
			dx += s[i]
			dy += s[i+1]
		}
		horizontal := math.Abs(dx) > math.Abs(dy)
		t.curveAt(0)
		if horizontal {
			t.curveTo(s[6], s[7], s[8], s[9], s[10], -dy)
		} else {
			t.curveTo(s[6], s[7], s[8], s[9], -dx, s[10])
		}
		t.count = 0
	default:
		fail(fmt.Sprintf("The CFF charstring uses an unknown operator (12 %d).", op))
	}
}

func (t *type2) subroutine(subrs [][]byte, bias int) []byte {
	index := int(t.pop()) + bias
	if index < 0 || index >= len(subrs) {
		fail("A CFF charstring calls a missing subroutine.")
	}
	return subrs[index]
}

// stemHints: an odd argument count on the first hint means the width comes first.
func (t *type2) stemHints() {
	if !t.widthParsed && t.count%2 == 1 {
		t.takeWidth()
	}
	t.widthParsed = true
	t.stems += t.count / 2
	t.count = 0
}

// parseWidth drops the advance width the first stack-clearing operator may
// carry as an extra first argument: hmtx holds the same widths.
func (t *type2) parseWidth(expected int) {
	if !t.widthParsed && t.count > expected {
		t.takeWidth()
	}
	t.widthParsed = true
}

func (t *type2) takeWidth() {
	copy(t.stack[:], t.stack[1:t.count])
	t.count--
}

func (t *type2) moveTo(dx, dy float64) {
	t.closePath()
	t.x += dx
	t.y += dy
	t.current = &pathContour{start: vec2{t.x, t.y}}
	t.count = 0
}

func (t *type2) lineTo(dx, dy float64) {
	c := t.ensureContour()
	t.x += dx
	t.y += dy
	c.segments = append(c.segments, pathSegment{end: vec2{t.x, t.y}})
}

func (t *type2) curveAt(i int) {
	s := &t.stack
	t.curveTo(s[i], s[i+1], s[i+2], s[i+3], s[i+4], s[i+5])
}

func (t *type2) curveTo(dxa, dya, dxb, dyb, dxc, dyc float64) {
	c := t.ensureContour()
	c1 := vec2{t.x + dxa, t.y + dya}
	c2 := vec2{c1.x + dxb, c1.y + dyb}
	t.x = c2.x + dxc
	t.y = c2.y + dyc
	c.segments = append(c.segments, pathSegment{true, c1, c2, vec2{t.x, t.y}})
}

// ensureContour starts a contour at the current point when drawing comes
// without a moveto (sloppy but seen in the wild).
func (t *type2) ensureContour() *pathContour {
	if t.current == nil {
		t.current = &pathContour{start: vec2{t.x, t.y}}
	}
	return t.current
}

func (t *type2) closePath() {
	if t.current != nil && len(t.current.segments) > 0 {
		t.contours = append(t.contours, t.current)
	}
	t.current = nil
}

func (t *type2) arg(i int) float64 {
	if i < t.count {
		return t.stack[i]
	}
	return 0
}

func (t *type2) need(n int) {
	if t.count < n {
		fail("A CFF charstring has too few arguments.")
	}
}

func (t *type2) push(v float64) {
	if t.count >= len(t.stack) {
		fail("A CFF charstring overflows the argument stack.")
	}
	t.stack[t.count] = v
	t.count++
}

func (t *type2) pop() float64 {
	if t.count == 0 {
		fail("A CFF charstring has too few arguments.")
	}
	t.count--
	return t.stack[t.count]
}

func (t *type2) binary(op func(a, b float64) float64) {
	b, a := t.pop(), t.pop()
	t.push(op(a, b))
}
