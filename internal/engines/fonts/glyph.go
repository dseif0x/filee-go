package fonts

// TrueType glyph outlines (the 'glyf' and 'loca' tables): parse a glyph into
// points / components and write it back in the compact standard encoding. Used
// by the WOFF2 glyf transform and the CFF → TrueType conversion.

type glyphPoint struct {
	x, y    int
	onCurve bool
}

const (
	onCurveFlag       = 0x01
	xShortFlag        = 0x02
	yShortFlag        = 0x04
	repeatFlag        = 0x08
	xSameFlag         = 0x10
	ySameFlag         = 0x20
	overlapSimpleFlag = 0x40
	cubicFlag         = 0x80

	argsAreWords    = 0x0001
	hasScale        = 0x0008
	moreComponents  = 0x0020
	hasXYScale      = 0x0040
	hasTwoByTwo     = 0x0080
	hasInstructions = 0x0100
)

// ttGlyph is a simple glyph (contours of points) or a composite glyph (raw component records).
type ttGlyph struct {
	numberOfContours       int16 // -1 for a composite glyph
	xMin, yMin, xMax, yMax int16
	endPoints              []uint16
	points                 []glyphPoint
	instructions           []byte
	overlapSimple          bool
	hasCubicPoints         bool
	components             []byte // component records, without instructions
}

func (g *ttGlyph) isComposite() bool { return g.numberOfContours < 0 }

// parseGlyph parses the glyph at offset; nil for an empty glyph (no outline).
func parseGlyph(glyf []byte, offset, length int) *ttGlyph {
	if length == 0 {
		return nil
	}
	r := newWindow(glyf, offset, offset+length)
	g := &ttGlyph{numberOfContours: r.i16(), xMin: r.i16(), yMin: r.i16(), xMax: r.i16(), yMax: r.i16()}
	switch {
	case g.numberOfContours >= 0:
		g.parseSimple(r)
	case g.numberOfContours == -1:
		start := r.pos
		instr := skipComponents(r)
		g.components = append([]byte(nil), r.data[start:r.pos]...)
		if instr {
			g.instructions = r.bytes(int(r.u16()))
		}
	default:
		fail("Invalid TrueType glyph (negative contour count).")
	}
	return g
}

func (g *ttGlyph) parseSimple(r *reader) {
	contours := int(g.numberOfContours)
	g.endPoints = make([]uint16, contours)
	for i := range g.endPoints {
		g.endPoints[i] = r.u16()
		if i > 0 && g.endPoints[i] <= g.endPoints[i-1] {
			fail("Invalid TrueType glyph (contour end points out of order).")
		}
	}
	g.instructions = r.bytes(int(r.u16()))
	count := 0
	if contours > 0 {
		count = int(g.endPoints[contours-1]) + 1
	}
	flags := make([]byte, count)
	for i := 0; i < count; i++ {
		flag := r.u8()
		flags[i] = flag
		if flag&repeatFlag == 0 {
			continue
		}
		for repeat := int(r.u8()); repeat > 0; repeat-- {
			i++
			if i >= count {
				fail("Invalid TrueType glyph (too many point flags).")
			}
			flags[i] = flag
		}
	}
	xs := readCoordinates(r, flags, xShortFlag, xSameFlag)
	ys := readCoordinates(r, flags, yShortFlag, ySameFlag)
	g.points = make([]glyphPoint, count)
	for i := range g.points {
		g.points[i] = glyphPoint{xs[i], ys[i], flags[i]&onCurveFlag != 0}
		g.hasCubicPoints = g.hasCubicPoints || flags[i]&cubicFlag != 0
	}
	g.overlapSimple = count > 0 && flags[0]&overlapSimpleFlag != 0
}

func readCoordinates(r *reader, flags []byte, shortFlag, sameFlag byte) []int {
	values := make([]int, len(flags))
	v := 0
	for i, f := range flags {
		switch {
		case f&shortFlag != 0:
			if f&sameFlag != 0 {
				v += int(r.u8())
			} else {
				v -= int(r.u8())
			}
		case f&sameFlag == 0:
			v += int(r.i16())
		}
		values[i] = v
	}
	return values
}

// skipComponents moves past the component records; true if any component announces instructions.
func skipComponents(r *reader) bool {
	instr := false
	for {
		flags := r.u16()
		r.skip(2) // glyph index
		if flags&argsAreWords != 0 {
			r.skip(4)
		} else {
			r.skip(2)
		}
		switch {
		case flags&hasScale != 0:
			r.skip(2)
		case flags&hasXYScale != 0:
			r.skip(4)
		case flags&hasTwoByTwo != 0:
			r.skip(8)
		}
		instr = instr || flags&hasInstructions != 0
		if flags&moreComponents == 0 {
			return instr
		}
	}
}

// computeBounds is the bounding box of the points (all zero without points).
func (g *ttGlyph) computeBounds() (int16, int16, int16, int16) {
	if len(g.points) == 0 {
		return 0, 0, 0, 0
	}
	xMin, yMin, xMax, yMax := 1<<31-1, 1<<31-1, -1<<31, -1<<31
	for _, p := range g.points {
		xMin, yMin = min(xMin, p.x), min(yMin, p.y)
		xMax, yMax = max(xMax, p.x), max(yMax, p.y)
	}
	return int16(xMin), int16(yMin), int16(xMax), int16(yMax)
}

func (g *ttGlyph) componentsHaveInstructions() bool {
	return len(g.components) > 0 && skipComponents(newReader(g.components))
}

// serialize writes the glyph in the standard compact form: repeated flags,
// one-byte deltas where they fit, "same as before" flags for zero deltas.
func (g *ttGlyph) serialize() []byte {
	w := newWriter(16 + len(g.points)*5 + len(g.components) + len(g.instructions))
	w.i16(int(g.numberOfContours))
	w.i16(int(g.xMin))
	w.i16(int(g.yMin))
	w.i16(int(g.xMax))
	w.i16(int(g.yMax))
	if g.isComposite() {
		w.bytes(g.components)
		if g.componentsHaveInstructions() {
			w.u16(len(g.instructions))
			w.bytes(g.instructions)
		}
		return w.buf
	}
	for _, e := range g.endPoints {
		w.u16(int(e))
	}
	w.u16(len(g.instructions))
	w.bytes(g.instructions)

	flags := newWriter(len(g.points))
	xs := newWriter(len(g.points) * 2)
	ys := newWriter(len(g.points) * 2)
	lastX, lastY, lastFlag, repeat := 0, 0, -1, 0
	for i, p := range g.points {
		flag := 0
		if p.onCurve {
			flag = onCurveFlag
		}
		if i == 0 && g.overlapSimple {
			flag |= overlapSimpleFlag
		}
		flag |= delta(xs, p.x-lastX, xShortFlag, xSameFlag)
		flag |= delta(ys, p.y-lastY, yShortFlag, ySameFlag)
		lastX, lastY = p.x, p.y
		if flag == lastFlag && repeat < 255 {
			// Mark the previous flag byte as repeated; its count is written when the run ends.
			if repeat == 0 {
				flags.buf[len(flags.buf)-1] |= repeatFlag
			}
			repeat++
		} else {
			if repeat > 0 {
				flags.u8(repeat)
			}
			flags.u8(flag)
			repeat = 0
		}
		lastFlag = flag
	}
	if repeat > 0 {
		flags.u8(repeat)
	}
	w.bytes(flags.buf)
	w.bytes(xs.buf)
	w.bytes(ys.buf)
	return w.buf
}

func delta(w *writer, d int, shortFlag, sameFlag int) int {
	if d == 0 {
		return sameFlag
	}
	if d > -256 && d < 256 {
		if d > 0 {
			w.u8(d)
			return shortFlag | sameFlag
		}
		w.u8(-d)
		return shortFlag
	}
	w.i16(d)
	return 0
}

// maxShortLocaSize is the largest glyf size a short (offset / 2) loca can address.
const maxShortLocaSize = 0xFFFF * 2

type locaEntry struct{ offset, length int }

// readLoca is the offset and length of every glyph in glyf according to loca.
func readLoca(loca []byte, glyphCount int, long bool, glyfLength int) []locaEntry {
	r := newReader(loca)
	offsets := make([]int64, glyphCount+1)
	for i := range offsets {
		if long {
			offsets[i] = int64(r.u32())
		} else {
			offsets[i] = int64(r.u16()) * 2
		}
	}
	out := make([]locaEntry, glyphCount)
	for i := range out {
		start := min(offsets[i], int64(glyfLength))
		end := min(offsets[i+1], int64(glyfLength))
		out[i] = locaEntry{int(start), int(max(0, end-start))}
	}
	return out
}

// readAllGlyphs parses every glyph of a TrueType font (nil entries are empty glyphs).
func readAllGlyphs(f *sfnt) []*ttGlyph {
	glyf := f.require("glyf")
	long := indexToLocFormat(f.require("head")) != 0
	loca := readLoca(f.require("loca"), f.glyphCount(), long, len(glyf))
	out := make([]*ttGlyph, len(loca))
	for i, l := range loca {
		out[i] = parseGlyph(glyf, l.offset, l.length)
	}
	return out
}

// buildGlyf concatenates serialized glyphs, each padded to alignment bytes, and
// builds loca. A short loca is used when asked for and when every offset fits;
// the returned flag says which format was written.
func buildGlyf(glyphs [][]byte, long bool, alignment int) ([]byte, []byte, bool) {
	align := func(n int) int { return (n + alignment - 1) / alignment * alignment }
	var total int64
	for _, g := range glyphs {
		total += int64(align(len(g)))
	}
	long = long || total > maxShortLocaSize
	if total > 1<<31-1 {
		fail("The glyph outlines are too large for a TrueType font.")
	}
	glyf := make([]byte, total)
	n := 2
	if long {
		n = 4
	}
	loca := newWriter((len(glyphs) + 1) * n)
	entry := func(off int) {
		if long {
			loca.u32(uint32(off))
		} else {
			loca.u16(off / 2)
		}
	}
	off := 0
	for _, g := range glyphs {
		entry(off)
		copy(glyf[off:], g)
		off += align(len(g))
	}
	entry(off)
	return glyf, loca.buf, long
}
