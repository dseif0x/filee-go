package fonts

import "context"

// The WOFF2 table transforms (W3C WOFF 2.0, section 5): glyf/loca split into
// streams with a compact triplet encoding for points (version 0), and hmtx
// without the left side bearings that equal the glyph's xMin (version 1).

type transformedGlyf struct {
	data       []byte
	glyfLength int     // length of the glyf table a decoder reconstructs
	locaLength int     // length of the loca table a decoder reconstructs
	longLoca   bool    // loca format of the reconstructed font
	xMins      []int16 // xMin of every glyph (0 for empty glyphs), for the hmtx transform
}

const overlapOption = 1

// tryTransformGlyf transforms glyf/loca; nil when the glyphs can't be carried
// losslessly (damaged data, cubic glyf extension), and glyf and loca are then
// stored untransformed.
func tryTransformGlyf(ctx context.Context, font *sfnt) (result *transformedGlyf) {
	var glyphs []*ttGlyph
	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(dataError); !ok {
					panic(r)
				}
				failed = true
			}
		}()
		glyphs = readAllGlyphs(font)
	}()
	if failed {
		return nil
	}
	for _, g := range glyphs {
		if g != nil && g.hasCubicPoints {
			return nil
		}
	}
	count := len(glyphs)
	nContours := newWriter(count * 2)
	nPoints := newWriter(count * 4)
	flags := newWriter(count * 32)
	glyphStream := newWriter(count * 64)
	composites := newWriter(0)
	bboxBitmap := make([]byte, 4*((count+31)/32))
	bboxes := newWriter(0)
	instructions := newWriter(0)
	overlap := make([]byte, (count+7)/8)
	hasOverlap := false
	xMins := make([]int16, count)
	var glyfLength int64
	writeBbox := func(g *ttGlyph, i int) {
		bboxBitmap[i>>3] |= byte(0x80 >> (i & 7))
		bboxes.i16(int(g.xMin))
		bboxes.i16(int(g.yMin))
		bboxes.i16(int(g.xMax))
		bboxes.i16(int(g.yMax))
	}
	for i, g := range glyphs {
		checkCtx(ctx)
		if g == nil || g.numberOfContours == 0 {
			nContours.i16(0)
			continue
		}
		xMins[i] = g.xMin
		glyfLength += int64((len(g.serialize()) + 3) &^ 3)
		if g.isComposite() {
			nContours.i16(-1)
			composites.bytes(g.components)
			if g.componentsHaveInstructions() {
				glyphStream.write255UInt16(len(g.instructions))
				instructions.bytes(g.instructions)
			}
			writeBbox(g, i) // composites always carry their box
			continue
		}
		nContours.i16(int(g.numberOfContours))
		previous := -1
		for _, end := range g.endPoints {
			nPoints.write255UInt16(int(end) - previous)
			previous = int(end)
		}
		lastX, lastY := 0, 0
		for _, p := range g.points {
			writeTriplet(flags, glyphStream, p.onCurve, p.x-lastX, p.y-lastY)
			lastX, lastY = p.x, p.y
		}
		glyphStream.write255UInt16(len(g.instructions))
		instructions.bytes(g.instructions)
		if x0, y0, x1, y1 := g.computeBounds(); x0 != g.xMin || y0 != g.yMin || x1 != g.xMax || y1 != g.yMax {
			writeBbox(g, i)
		}
		if g.overlapSimple {
			overlap[i>>3] |= byte(0x80 >> (i & 7))
			hasOverlap = true
		}
	}
	longLoca := indexToLocFormat(font.require("head")) != 0 || glyfLength > maxShortLocaSize
	w := newWriter(0)
	w.u16(0) // reserved
	if hasOverlap {
		w.u16(overlapOption)
	} else {
		w.u16(0)
	}
	w.u16(count)
	if longLoca {
		w.u16(1)
	} else {
		w.u16(0)
	}
	for _, s := range []int{nContours.len(), nPoints.len(), flags.len(), glyphStream.len(), composites.len(), len(bboxBitmap) + bboxes.len(), instructions.len()} {
		w.u32(uint32(s))
	}
	w.bytes(nContours.buf)
	w.bytes(nPoints.buf)
	w.bytes(flags.buf)
	w.bytes(glyphStream.buf)
	w.bytes(composites.buf)
	w.bytes(bboxBitmap)
	w.bytes(bboxes.buf)
	w.bytes(instructions.buf)
	if hasOverlap {
		w.bytes(overlap)
	}
	locaEntrySize := 2
	if longLoca {
		locaEntrySize = 4
	}
	return &transformedGlyf{w.buf, int(glyfLength), (count + 1) * locaEntrySize, longLoca, xMins}
}

// writeTriplet encodes one point delta as a flag byte (on-curve bit + one of
// 128 shapes) and 1–4 data bytes, choosing the smallest shape that fits (the
// table in section 5.2 of the spec).
func writeTriplet(flags, data *writer, onCurve bool, dx, dy int) {
	absX, absY := abs(dx), abs(dy)
	onCurveBit := 128
	if onCurve {
		onCurveBit = 0
	}
	xSign, ySign := 1, 1
	if dx < 0 {
		xSign = 0
	}
	if dy < 0 {
		ySign = 0
	}
	signs := xSign + 2*ySign
	switch {
	case dx == 0 && absY < 1280:
		flags.u8(onCurveBit + ((absY & 0xF00) >> 7) + ySign)
		data.u8(absY & 0xFF)
	case dy == 0 && absX < 1280:
		flags.u8(onCurveBit + 10 + ((absX & 0xF00) >> 7) + xSign)
		data.u8(absX & 0xFF)
	case absX < 65 && absY < 65:
		flags.u8(onCurveBit + 20 + ((absX - 1) & 0x30) + (((absY - 1) & 0x30) >> 2) + signs)
		data.u8(((absX-1)&0xF)<<4 | ((absY - 1) & 0xF))
	case absX < 769 && absY < 769:
		flags.u8(onCurveBit + 84 + 12*(((absX-1)&0x300)>>8) + (((absY - 1) & 0x300) >> 6) + signs)
		data.u8((absX - 1) & 0xFF)
		data.u8((absY - 1) & 0xFF)
	case absX < 4096 && absY < 4096:
		flags.u8(onCurveBit + 120 + signs)
		data.u8(absX >> 4)
		data.u8((absX&0xF)<<4 | absY>>8)
		data.u8(absY & 0xFF)
	default:
		flags.u8(onCurveBit + 124 + signs)
		data.u16(absX)
		data.u16(absY)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// reconstructGlyf rebuilds glyf and loca from a transformed glyf table (section 5.1 of the spec).
func reconstructGlyf(ctx context.Context, data []byte, offset, length int) ([]byte, []byte, bool, []int16) {
	end := offset + length
	header := newWindow(data, offset, end)
	header.u16() // reserved
	options := header.u16()
	count := int(header.u16())
	longLoca := header.u16() != 0
	var sizes [7]uint32
	for i := range sizes {
		sizes[i] = header.u32()
	}
	pos := int64(header.pos)
	next := func(size int64) *reader {
		if size > int64(end)-pos {
			fail("The WOFF2 glyph data is damaged.")
		}
		s := newWindow(data, int(pos), int(pos+size))
		pos += size
		return s
	}
	nContours := next(int64(sizes[0]))
	nPoints := next(int64(sizes[1]))
	flagStream := next(int64(sizes[2]))
	glyphStream := next(int64(sizes[3]))
	composites := next(int64(sizes[4]))
	bboxStream := next(int64(sizes[5]))
	instructions := next(int64(sizes[6]))
	bboxBitmap := bboxStream.bytes(4 * ((count + 31) / 32))
	var overlap []byte
	if options&overlapOption != 0 {
		overlap = next(int64((count + 7) / 8)).bytes((count + 7) / 8)
	}

	glyphs := make([][]byte, count)
	xMins := make([]int16, count)
	for i := 0; i < count; i++ {
		checkCtx(ctx)
		contours := int(nContours.i16())
		hasBbox := bboxBitmap[i>>3]&(0x80>>(i&7)) != 0
		var g *ttGlyph
		switch {
		case contours == 0:
			if hasBbox {
				fail("The WOFF2 glyph data is damaged (bounding box on an empty glyph).")
			}
			glyphs[i] = nil
			continue
		case contours == -1:
			if !hasBbox {
				fail("The WOFF2 glyph data is damaged (composite glyph without bounding box).")
			}
			start := composites.pos
			instr := skipComponents(composites)
			g = &ttGlyph{numberOfContours: -1, components: append([]byte(nil), data[start:composites.pos]...)}
			if instr {
				g.instructions = instructions.bytes(int(glyphStream.read255UInt16()))
			}
		case contours > 0:
			g = &ttGlyph{numberOfContours: int16(contours), endPoints: make([]uint16, contours)}
			total := 0
			for c := 0; c < contours; c++ {
				total += int(nPoints.read255UInt16())
				if total > 0xFFFF {
					fail("The WOFF2 glyph data is damaged (too many points).")
				}
				g.endPoints[c] = uint16(total - 1)
			}
			g.points = readTriplets(flagStream, glyphStream, total)
			g.instructions = instructions.bytes(int(glyphStream.read255UInt16()))
			g.overlapSimple = overlap != nil && overlap[i>>3]&(0x80>>(i&7)) != 0
		default:
			fail("The WOFF2 glyph data is damaged (invalid contour count).")
		}
		if hasBbox {
			g.xMin, g.yMin, g.xMax, g.yMax = bboxStream.i16(), bboxStream.i16(), bboxStream.i16(), bboxStream.i16()
		} else {
			g.xMin, g.yMin, g.xMax, g.yMax = g.computeBounds()
		}
		xMins[i] = g.xMin
		glyphs[i] = g.serialize()
	}
	glyf, loca, actualLong := buildGlyf(glyphs, longLoca, 4)
	return glyf, loca, actualLong, xMins
}

func readTriplets(flags, data *reader, count int) []glyphPoint {
	points := make([]glyphPoint, count)
	x, y := 0, 0
	withSign := func(flag, v int) int {
		if flag&1 != 0 {
			return v
		}
		return -v
	}
	for i := range points {
		flag := int(flags.u8())
		onCurve := flag&0x80 == 0
		flag &= 0x7F
		var dx, dy int
		switch {
		case flag < 10:
			dy = withSign(flag, (flag&14)<<7+int(data.u8()))
		case flag < 20:
			dx = withSign(flag, ((flag-10)&14)<<7+int(data.u8()))
		case flag < 84:
			b0, b1 := flag-20, int(data.u8())
			dx = withSign(flag, 1+(b0&0x30)+b1>>4)
			dy = withSign(flag>>1, 1+(b0&0x0C)<<2+b1&0x0F)
		case flag < 120:
			b0, b1, b2 := flag-84, int(data.u8()), int(data.u8())
			dx = withSign(flag, 1+(b0/12)<<8+b1)
			dy = withSign(flag>>1, 1+((b0%12)>>2)<<8+b2)
		case flag < 124:
			b1, b2, b3 := int(data.u8()), int(data.u8()), int(data.u8())
			dx = withSign(flag, b1<<4+b2>>4)
			dy = withSign(flag>>1, (b2&0x0F)<<8+b3)
		default:
			dx = withSign(flag, int(data.u16()))
			dy = withSign(flag>>1, int(data.u16()))
		}
		x += dx
		y += dy
		points[i] = glyphPoint{x, y, onCurve}
	}
	return points
}

// tryTransformHmtx drops the left side bearings that equal the glyph's xMin;
// nil when neither array can be dropped (the table is then stored as-is).
func tryTransformHmtx(hmtx []byte, glyphCount, metricsCount int, xMins []int16) []byte {
	if metricsCount < 1 || metricsCount > glyphCount || len(hmtx) != 4*metricsCount+2*(glyphCount-metricsCount) || len(xMins) < glyphCount {
		return nil
	}
	r := newReader(hmtx)
	advances := make([]uint16, metricsCount)
	lsbs := make([]int16, glyphCount)
	for i := 0; i < metricsCount; i++ {
		advances[i] = r.u16()
		lsbs[i] = r.i16()
	}
	for i := metricsCount; i < glyphCount; i++ {
		lsbs[i] = r.i16()
	}
	proportional, monospaced := true, true
	for i := 0; i < glyphCount; i++ {
		if lsbs[i] != xMins[i] {
			if i < metricsCount {
				proportional = false
			} else {
				monospaced = false
			}
		}
	}
	if !proportional && !monospaced {
		return nil
	}
	w := newWriter(len(hmtx))
	f := 0
	if proportional {
		f |= 1
	}
	if monospaced {
		f |= 2
	}
	w.u8(f)
	for _, a := range advances {
		w.u16(int(a))
	}
	if !proportional {
		for i := 0; i < metricsCount; i++ {
			w.i16(int(lsbs[i]))
		}
	}
	if !monospaced {
		for i := metricsCount; i < glyphCount; i++ {
			w.i16(int(lsbs[i]))
		}
	}
	return w.buf
}

// reconstructHmtx rebuilds hmtx from its transformed form, filling missing side
// bearings with each glyph's xMin.
func reconstructHmtx(data []byte, offset, length, glyphCount, metricsCount int, xMins []int16) []byte {
	if metricsCount < 1 || metricsCount > glyphCount || len(xMins) < glyphCount {
		fail("The WOFF2 metrics are damaged.")
	}
	r := newWindow(data, offset, offset+length)
	flags := r.u8()
	if flags&0xFC != 0 {
		fail("The WOFF2 metrics use unknown flags.")
	}
	advances := make([]uint16, metricsCount)
	for i := range advances {
		advances[i] = r.u16()
	}
	lsbs := append([]int16(nil), xMins[:glyphCount]...)
	if flags&1 == 0 {
		for i := 0; i < metricsCount; i++ {
			lsbs[i] = r.i16()
		}
	}
	if flags&2 == 0 {
		for i := metricsCount; i < glyphCount; i++ {
			lsbs[i] = r.i16()
		}
	}
	w := newWriter(4*metricsCount + 2*(glyphCount-metricsCount))
	for i := 0; i < glyphCount; i++ {
		if i < metricsCount {
			w.u16(int(advances[i]))
		}
		w.i16(int(lsbs[i]))
	}
	return w.buf
}
