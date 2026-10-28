package fonts

import (
	"context"
	"encoding/binary"
	"math"
	"slices"
)

// Converts an OpenType font with CFF (PostScript, cubic) outlines to TrueType
// (quadratic) outlines: builds glyf, loca, maxp 1.0 and post, updates head,
// hhea, hmtx and vmtx, and keeps every other table (cmap, name, OS/2, GSUB,
// GPOS, kern, ...) unchanged. Hints are not converted; a gasp/prep pair asks
// rasterizers to smooth instead.

// toleranceEm is the maximum distance between a converted curve and the
// original, in em units (1 unit at 1000 units per em), as fontTools' otf2ttf uses.
const toleranceEm = 0.001

var (
	// unhintedPrep switches on dropout control (SCANCTRL 511, SCANTYPE 4), as recommended for unhinted fonts.
	unhintedPrep = []byte{0xB8, 0x01, 0xFF, 0x85, 0xB0, 0x04, 0x8D}
	// smoothGasp is gasp version 1 with one range: grid-fit and smooth (symmetrically) at every size.
	smoothGasp = []byte{0, 1, 0, 1, 0xFF, 0xFF, 0, 0x0F}
)

// cffToTrueType returns a TrueType-outline copy of source.
func cffToTrueType(ctx context.Context, source *sfnt, progress func(float64)) *sfnt {
	if source.has("CFF2") {
		fail("This is a variable font with CFF2 outlines. Converting CFF2 to TrueType isn't supported; WOFF, WOFF2 and OTF work.")
	}
	cff := parseCff(source.require("CFF "))
	glyphCount := source.glyphCount()
	if cff.glyphCount() != glyphCount {
		fail("The font's CFF outlines and glyph count don't match.")
	}
	head := source.require("head")
	if len(head) < 54 {
		fail("The font file is truncated or damaged.")
	}
	upem := unitsPerEm(head)
	tolerance := float64(max(upem, 16)) * toleranceEm
	matrices := make([][]float64, len(cff.fontMatrices))
	for i, m := range cff.fontMatrices {
		matrices[i] = toFontUnits(m, upem)
	}
	interp := newType2(cff)
	glyphs := make([]*ttGlyph, glyphCount)
	for gid := 0; gid < glyphCount; gid++ {
		if gid%64 == 0 {
			checkCtx(ctx)
			if progress != nil {
				progress(float64(gid) / float64(glyphCount))
			}
		}
		contours := interp.run(gid, true)
		if m := matrices[min(cff.fontDictIndex(gid), len(matrices)-1)]; m != nil {
			for i, c := range contours {
				contours[i] = c.transformed(func(p vec2) vec2 { return p.apply(m) })
			}
		}
		glyphs[gid] = buildTTGlyph(contours, tolerance)
	}

	font := source.clone()
	for _, t := range []string{"CFF ", "VORG", "DSIG"} {
		delete(font.tables, t)
	}
	font.flavor = trueTypeFlavor
	serialized := make([][]byte, glyphCount)
	for i, g := range glyphs {
		if g != nil {
			serialized[i] = g.serialize()
		}
	}
	glyf, loca, longLoca := buildGlyf(serialized, false, 2)
	font.tables["glyf"] = glyf
	font.tables["loca"] = loca
	font.tables["head"] = updateHead(head, glyphs, longLoca)
	font.tables["maxp"] = buildMaxp(glyphs)
	names := make([]string, glyphCount)
	usable := true
	for i := range names {
		n, ok := cff.glyphName(i)
		names[i] = n
		usable = usable && ok
	}
	font.tables["post"] = buildPost(source.table("post"), names, usable)
	if hhea, hmtx := font.table("hhea"), font.table("hmtx"); len(hhea) >= 36 && font.has("hmtx") {
		font.tables["hhea"], font.tables["hmtx"] = updateHorizontalMetrics(hhea, hmtx, glyphs)
	}
	if vorg, vhea, vmtx := source.table("VORG"), font.table("vhea"), font.table("vmtx"); source.has("VORG") && len(vhea) >= 36 && font.has("vmtx") {
		font.tables["vhea"], font.tables["vmtx"] = updateVerticalMetrics(vhea, vmtx, vorg, glyphs)
	}
	if !font.has("gasp") && !font.has("prep") {
		font.tables["gasp"] = smoothGasp
		font.tables["prep"] = unhintedPrep
	}
	if progress != nil {
		progress(1)
	}
	return font
}

// toFontUnits is FontMatrix scaled to font units; nil when it is the identity (the usual 1/unitsPerEm matrix).
func toFontUnits(matrix []float64, upem int) []float64 {
	identity := []float64{1, 0, 0, 1, 0, 0}
	m := make([]float64, 6)
	same := true
	for i := range m {
		m[i] = matrix[i] * float64(upem)
		same = same && math.Abs(m[i]-identity[i]) < 1e-9
	}
	if same {
		return nil
	}
	return m
}

func roundHalfUp(v float64) int { return int(math.Floor(v + 0.5)) }

// buildTTGlyph builds a TrueType glyph: curves become quadratic splines, points
// are rounded to integers, and every contour is reversed because TrueType fills
// clockwise outer contours while PostScript draws them counter-clockwise.
func buildTTGlyph(contours []*pathContour, tolerance float64) *ttGlyph {
	var endPoints []uint16
	var points []glyphPoint
	for _, c := range contours {
		type rawPoint struct {
			p  vec2
			on bool
		}
		raw := []rawPoint{{c.start, true}}
		current := c.start
		for _, s := range c.segments {
			if s.curve {
				for _, control := range cubicToQuadratic(current, s.c1, s.c2, s.end, tolerance) {
					raw = append(raw, rawPoint{control, false})
				}
			}
			raw = append(raw, rawPoint{s.end, true})
			current = s.end
		}
		rounded := make([]glyphPoint, len(raw))
		for i, p := range raw {
			rounded[i] = glyphPoint{roundHalfUp(p.p.x), roundHalfUp(p.p.y), p.on}
		}
		rounded = cleanPoints(rounded)
		if len(rounded) < 3 {
			continue // nothing left to fill
		}
		points = append(points, rounded[0])
		for i := len(rounded) - 1; i > 0; i-- {
			points = append(points, rounded[i])
		}
		if len(points) > 0xFFFF {
			fail("A glyph has more points than TrueType allows.")
		}
		endPoints = append(endPoints, uint16(len(points)-1))
	}
	if len(endPoints) == 0 {
		return nil
	}
	g := &ttGlyph{numberOfContours: int16(len(endPoints)), endPoints: endPoints, points: points}
	g.xMin, g.yMin, g.xMax, g.yMax = g.computeBounds()
	return g
}

// cleanPoints drops the closing point when it repeats the start (the contour
// closes by itself) and on-curve points that repeat their predecessor after
// rounding (zero-length lines).
func cleanPoints(points []glyphPoint) []glyphPoint {
	out := make([]glyphPoint, 0, len(points))
	for _, p := range points {
		if n := len(out); n > 0 && p.onCurve && out[n-1].onCurve && out[n-1].x == p.x && out[n-1].y == p.y {
			continue
		}
		out = append(out, p)
	}
	for len(out) > 1 && out[len(out)-1].onCurve && out[len(out)-1].x == out[0].x && out[len(out)-1].y == out[0].y {
		out = out[:len(out)-1]
	}
	return out
}

func putI16(b []byte, off int, v int) { binary.BigEndian.PutUint16(b[off:], uint16(int16(v))) }

func updateHead(head []byte, glyphs []*ttGlyph, longLoca bool) []byte {
	out := withIndexToLocFormat(head, longLoca)
	first := true
	var xMin, yMin, xMax, yMax int16
	for _, g := range glyphs {
		if g == nil {
			continue
		}
		if first {
			xMin, yMin, xMax, yMax = g.xMin, g.yMin, g.xMax, g.yMax
			first = false
			continue
		}
		xMin, yMin = min(xMin, g.xMin), min(yMin, g.yMin)
		xMax, yMax = max(xMax, g.xMax), max(yMax, g.yMax)
	}
	if !first {
		putI16(out, 36, int(xMin))
		putI16(out, 38, int(yMin))
		putI16(out, 40, int(xMax))
		putI16(out, 42, int(yMax))
	}
	putI16(out, 52, 0) // glyphDataFormat
	return out
}

// buildMaxp writes maxp version 1.0: outline maxima; no hinting instructions
// beyond the dropout-control prep.
func buildMaxp(glyphs []*ttGlyph) []byte {
	maxPoints, maxContours := 0, 0
	for _, g := range glyphs {
		if g != nil {
			maxPoints = max(maxPoints, len(g.points))
			maxContours = max(maxContours, int(g.numberOfContours))
		}
	}
	w := newWriter(32)
	w.u32(0x00010000)
	w.u16(len(glyphs))
	w.u16(maxPoints)
	w.u16(maxContours)
	w.u16(0) // maxCompositePoints
	w.u16(0) // maxCompositeContours
	w.u16(1) // maxZones: no twilight zone
	w.u16(0) // maxTwilightPoints
	w.u16(0) // maxStorage
	w.u16(0) // maxFunctionDefs
	w.u16(0) // maxInstructionDefs
	w.u16(1) // maxStackElements (the prep pushes one value at a time)
	w.u16(0) // maxSizeOfInstructions
	w.u16(0) // maxComponentElements
	w.u16(0) // maxComponentDepth
	return w.buf
}

// buildPost writes post format 2.0 with the CFF glyph names when the font has
// them (name-keyed CFF), else format 3.0 (CID-keyed fonts have no names). The
// header fields (italic angle, underline) are kept.
func buildPost(old []byte, names []string, haveNames bool) []byte {
	w := newWriter(64 + len(names)*10)
	header := make([]byte, 32)
	copy(header, old[:min(32, len(old))])
	usable := haveNames
	for _, n := range names {
		if len(n) == 0 || len(n) >= 256 {
			usable = false
		}
		for _, c := range n {
			if c <= ' ' || c >= 127 {
				usable = false
			}
		}
	}
	if usable {
		binary.BigEndian.PutUint32(header, 0x00020000)
	} else {
		binary.BigEndian.PutUint32(header, 0x00030000)
	}
	w.bytes(header)
	if !usable {
		return w.buf
	}
	standard := map[string]int{}
	for i, n := range macGlyphNames {
		if _, ok := standard[n]; !ok {
			standard[n] = i
		}
	}
	custom := map[string]int{}
	var extra []string
	w.u16(len(names))
	for _, n := range names {
		if i, ok := standard[n]; ok {
			w.u16(i)
			continue
		}
		i, ok := custom[n]
		if !ok {
			i = len(standard) + len(extra)
			custom[n] = i
			extra = append(extra, n)
		}
		w.u16(i)
	}
	for _, n := range extra {
		w.u8(len(n))
		w.bytes([]byte(n))
	}
	return w.buf
}

// updateHorizontalMetrics sets each left side bearing to the new xMin and recomputes hhea's extremes.
func updateHorizontalMetrics(hhea, hmtx []byte, glyphs []*ttGlyph) ([]byte, []byte) {
	metrics := numberOfMetrics(hhea)
	advances, _ := readMetrics(hmtx, metrics, len(glyphs))
	lsbs := make([]int, len(glyphs))
	for i, g := range glyphs {
		if g != nil {
			lsbs[i] = int(g.xMin)
		}
	}
	result := writeMetrics(advances, lsbs, metrics)
	header := slices.Clone(hhea)
	maxAdvance := 0
	for _, a := range advances {
		maxAdvance = max(maxAdvance, a)
	}
	binary.BigEndian.PutUint16(header[10:], uint16(maxAdvance))
	first := true
	var minLsb, minRsb, maxExtent int
	for i, g := range glyphs {
		if g == nil {
			continue
		}
		lsb, rsb, ext := int(g.xMin), advances[i]-int(g.xMax), int(g.xMax)
		if first {
			minLsb, minRsb, maxExtent, first = lsb, rsb, ext, false
			continue
		}
		minLsb, minRsb, maxExtent = min(minLsb, lsb), min(minRsb, rsb), max(maxExtent, ext)
	}
	if !first {
		putI16(header, 12, minLsb)
		putI16(header, 14, minRsb)
		putI16(header, 16, maxExtent)
	}
	return header, result
}

// updateVerticalMetrics keeps VORG's vertical origin: TrueType has no VORG, the
// origin is yMax + top side bearing. Sets each top side bearing so the origin
// stays put, and recomputes vhea's extremes.
func updateVerticalMetrics(vhea, vmtx, vorg []byte, glyphs []*ttGlyph) ([]byte, []byte) {
	metrics := numberOfMetrics(vhea)
	heights, tsbs := readMetrics(vmtx, metrics, len(glyphs))
	r := newReader(vorg)
	r.skip(4) // version
	defaultOrigin := int(r.i16())
	count := int(r.u16())
	origins := map[int]int{}
	for i := 0; i < count; i++ {
		g := int(r.u16())
		origins[g] = int(r.i16())
	}
	for i, g := range glyphs {
		if g != nil {
			o, ok := origins[i]
			if !ok {
				o = defaultOrigin
			}
			tsbs[i] = o - int(g.yMax)
		}
	}
	result := writeMetrics(heights, tsbs, metrics)
	header := slices.Clone(vhea)
	first := true
	var minTsb, minBsb, maxExtent int
	for i, g := range glyphs {
		if g == nil {
			continue
		}
		extent := tsbs[i] + int(g.yMax) - int(g.yMin)
		if first {
			minTsb, minBsb, maxExtent, first = tsbs[i], heights[i]-extent, extent, false
			continue
		}
		minTsb, minBsb, maxExtent = min(minTsb, tsbs[i]), min(minBsb, heights[i]-extent), max(maxExtent, extent)
	}
	if !first {
		putI16(header, 12, minTsb)
		putI16(header, 14, minBsb)
		putI16(header, 16, maxExtent)
	}
	return header, result
}

// readMetrics reads hmtx/vmtx: the advance of every glyph (the last one repeats) and the side bearings.
func readMetrics(table []byte, metrics, glyphCount int) ([]int, []int) {
	r := newReader(table)
	advances := make([]int, glyphCount)
	bearings := make([]int, glyphCount)
	last := 0
	for i := 0; i < glyphCount; i++ {
		if i < metrics {
			last = int(r.u16())
		}
		advances[i] = last
		if r.remaining() >= 2 {
			bearings[i] = int(r.i16())
		}
	}
	return advances, bearings
}

func writeMetrics(advances, bearings []int, metrics int) []byte {
	w := newWriter(len(advances) * 4)
	for i := range advances {
		if i < metrics {
			w.u16(advances[i])
		}
		w.i16(bearings[i])
	}
	return w.buf
}
