package fonts

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
)

// Turns a validated monochrome FNT strike into an unhinted TrueType outline font.

// fonScale scales the strike's pixel height into TrueType font units.
func fonScale(height int) int {
	if height <= 1024 {
		return 1024 / height
	}
	return 1
}

type corner struct{ x, y int }
type unitEdge struct{ from, to corner }

func signed16(v int) int {
	if v < math.MinInt16 || v > math.MaxInt16 {
		refuse("error.fon_too_large", "32767")
	}
	return v
}

type outlineKey struct {
	offset int64
	width  int
}

// buildFon builds a TrueType font of a strike; resourceCount and ordinal name
// strikes of a file with several.
func buildFon(ctx context.Context, strike *fonStrike, resourceCount, ordinal int) *sfnt {
	if resourceCount < 1 || ordinal < 1 || len(strike.glyphs) < 1 {
		refuse("error.fon_damaged", "")
	}
	if len(strike.glyphs) > 65534 {
		refuse("error.fon_too_large", "65535")
	}
	height := strike.pixelHeight
	scale := fonScale(height)
	upem := height * scale
	if upem < 16 || upem > 16384 {
		refuse("error.fon_too_large", "16384")
	}
	ascent := signed16(strike.ascent * scale)
	descent := signed16(-(height - strike.ascent) * scale)
	lineGap := signed16(strike.externalLeading * scale)

	glyphs := []*ttGlyph{nil}
	widths := []int{0}
	cmap := map[int]int{}
	outlines := map[outlineKey]*ttGlyph{}
	enc, codePage := charSetEncoding(strike.charSet)
	for i := range strike.glyphs {
		src := &strike.glyphs[i]
		checkCtx(ctx)
		advance := src.width * scale
		if advance > 0xFFFF {
			refuse("error.fon_too_large", "65535")
		}
		key := outlineKey{src.bitmapOffset, src.width}
		outline, ok := outlines[key]
		if !ok {
			outline = fonOutline(ctx, src, strike.ascent, scale)
			outlines[key] = outline
		}
		glyphs = append(glyphs, outline)
		widths = append(widths, advance)
		if scalar, ok := fonCodePoint(enc, src.byteValue); ok {
			if _, seen := cmap[scalar]; !seen {
				cmap[scalar] = len(glyphs) - 1
			}
		}
	}
	glyphs[0] = glyphs[strike.defaultChar+1]
	widths[0] = widths[strike.defaultChar+1]
	serialized := map[*ttGlyph][]byte{}
	parts := make([][]byte, len(glyphs))
	for i, g := range glyphs {
		checkCtx(ctx)
		if g == nil {
			continue
		}
		b, ok := serialized[g]
		if !ok {
			b = g.serialize()
			serialized[g] = b
		}
		parts[i] = b
	}
	glyf, loca, longLoca := buildGlyf(parts, false, 2)
	checkCtx(ctx)

	leftBearings := make([]int, len(glyphs))
	hasAny := false
	var xMin, xMax, yMin, yMax, minRight, maxPoints, maxContours int
	for i, g := range glyphs {
		if g == nil {
			continue
		}
		leftBearings[i] = int(g.xMin)
		gx0, gx1, gy0, gy1 := int(g.xMin), int(g.xMax), int(g.yMin), int(g.yMax)
		right := widths[i] - gx1
		if !hasAny {
			xMin, xMax, yMin, yMax, minRight, maxPoints, maxContours = gx0, gx1, gy0, gy1, right, len(g.points), int(g.numberOfContours)
			hasAny = true
			continue
		}
		xMin, xMax, yMin, yMax = min(xMin, gx0), max(xMax, gx1), min(yMin, gy0), max(yMax, gy1)
		minRight, maxPoints, maxContours = min(minRight, right), max(maxPoints, len(g.points)), max(maxContours, int(g.numberOfContours))
	}

	weight := strike.weight
	bold := weight >= 700
	baseName := strike.face
	if resourceCount != 1 {
		baseName = fmt.Sprintf("%s %dpx", strike.face, height)
		if ordinal > 1 {
			baseName += fmt.Sprintf("-%d", ordinal)
		}
	}
	family := baseName
	if weight != 400 && weight != 700 {
		family = fmt.Sprintf("%s Weight%d", baseName, weight)
	}
	style := "Regular"
	switch {
	case weight == 700 && strike.italic:
		style = "Bold Italic"
	case weight == 700:
		style = "Bold"
	case strike.italic:
		style = "Italic"
	}
	full := family + " " + style
	ps := postScriptName(full)
	styleBits := 0
	if bold {
		styleBits |= 1
	}
	if strike.italic {
		styleBits |= 2
	}
	maxAdvance := 0
	for _, w := range widths {
		maxAdvance = max(maxAdvance, w)
	}
	font := newSfnt(trueTypeFlavor)
	font.tables["glyf"] = glyf
	font.tables["loca"] = loca
	font.tables["hmtx"] = fonMetrics(widths, leftBearings)
	font.tables["head"] = fonHead(upem, xMin, yMin, xMax, yMax, longLoca, styleBits)
	font.tables["hhea"] = fonHhea(ascent, descent, lineGap, maxAdvance, xMin, minRight, xMax, len(glyphs))
	font.tables["maxp"] = fonMaxp(len(glyphs), maxPoints, maxContours)
	font.tables["OS/2"] = fonOs2(strike, widths, cmap, codePage, weight, bold, ascent, descent, lineGap, yMin, yMax)
	font.tables["name"] = fonName(family, style, full, ps)
	font.tables["cmap"] = fonCmap(cmap)
	post := make([]byte, 32)
	binary.BigEndian.PutUint32(post, 0x00030000)
	fixed := true
	for _, g := range strike.glyphs {
		fixed = fixed && g.width == strike.glyphs[0].width
	}
	if fixed {
		binary.BigEndian.PutUint32(post[12:], 1)
	}
	font.tables["post"] = post
	return font
}

// fonCodePoint is the Unicode scalar a byte stands for, when it round-trips
// through the code page and is no control or private use character (Windows
// best-fit tables decode undefined bytes of some code pages to private use).
func fonCodePoint(e encoding.Encoding, b byte) (int, bool) {
	text, ok := strictDecode(e, []byte{b})
	if !ok || utf8.RuneCountInString(text) != 1 {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(text)
	if r > 0xFFFF || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Co, r) {
		return 0, false
	}
	back, err := e.NewEncoder().Bytes([]byte(text))
	if err != nil || len(back) != 1 || back[0] != b {
		return 0, false
	}
	return int(r), true
}

// orderedRuns is a dictionary with .NET's Dictionary enumeration order: entries
// in slot order, freed slots reused last-freed first. The outline's rectangles
// come out in the order of the original.
type orderedRuns struct {
	keys  [][2]int
	vals  []int
	used  []bool
	free  []int
	index map[[2]int]int
}

func newOrderedRuns() *orderedRuns { return &orderedRuns{index: map[[2]int]int{}} }

func (d *orderedRuns) has(k [2]int) bool { _, ok := d.index[k]; return ok }

func (d *orderedRuns) set(k [2]int, v int) {
	if i, ok := d.index[k]; ok {
		d.vals[i] = v
		return
	}
	var i int
	if n := len(d.free); n > 0 {
		i = d.free[n-1]
		d.free = d.free[:n-1]
		d.keys[i], d.vals[i], d.used[i] = k, v, true
	} else {
		i = len(d.keys)
		d.keys = append(d.keys, k)
		d.vals = append(d.vals, v)
		d.used = append(d.used, true)
	}
	d.index[k] = i
}

func (d *orderedRuns) remove(k [2]int) {
	i := d.index[k]
	delete(d.index, k)
	d.used[i] = false
	d.free = append(d.free, i)
}

func fonOutline(ctx context.Context, src *fonGlyph, ascent, scale int) *ttGlyph {
	height := len(src.pixels)
	if src.width == 0 {
		return nil
	}
	if countFonPoints(ctx, src.pixels, src.width, 0xFFFF) > 0xFFFF {
		refuse("error.fon_too_large", "65535")
	}
	// Extend identical horizontal runs into maximal rectangles, then cancel each shared unit edge.
	active := newOrderedRuns()
	type rect struct{ x0, x1, y0, y1 int }
	var rectangles []rect
	for y := 0; y <= height; y++ {
		checkCtx(ctx)
		var runs [][2]int
		runSet := map[[2]int]bool{}
		if y < height {
			for x := 0; x < src.width; {
				if !src.pixels[y][x] {
					x++
					continue
				}
				start := x
				for x < src.width && src.pixels[y][x] {
					x++
				}
				runs = append(runs, [2]int{start, x})
				runSet[[2]int{start, x}] = true
			}
		}
		for i := range active.keys {
			if !active.used[i] {
				continue
			}
			if run := active.keys[i]; !runSet[run] {
				rectangles = append(rectangles, rect{run[0], run[1], active.vals[i], y})
				active.remove(run)
			}
		}
		for _, run := range runs {
			if !active.has(run) {
				active.set(run, y)
			}
		}
	}
	if len(rectangles) == 0 {
		return nil
	}
	edges := map[unitEdge]bool{}
	var order []unitEdge
	add := func(from, to corner) {
		if rev := (unitEdge{to, from}); edges[rev] {
			delete(edges, rev)
			return
		}
		e := unitEdge{from, to}
		edges[e] = true
		order = append(order, e)
	}
	work := 0
	tick := func() {
		work++
		if work&4095 == 0 {
			checkCtx(ctx)
		}
	}
	for _, r := range rectangles {
		checkCtx(ctx)
		for x := r.x0; x < r.x1; x++ {
			tick()
			add(corner{x, r.y0}, corner{x + 1, r.y0})
			add(corner{x + 1, r.y1}, corner{x, r.y1})
		}
		for y := r.y0; y < r.y1; y++ {
			tick()
			add(corner{r.x1, y}, corner{r.x1, y + 1})
			add(corner{r.x0, y + 1}, corner{r.x0, y})
		}
	}
	outgoing := map[corner][]unitEdge{}
	for e := range edges {
		tick()
		outgoing[e.from] = append(outgoing[e.from], e)
	}
	var points []glyphPoint
	var ends []uint16
	cursor := 0
	for len(edges) != 0 {
		checkCtx(ctx)
		for !edges[order[cursor]] {
			cursor++
		}
		start := order[cursor]
		cursor++
		e := start
		var loop []corner
		for {
			tick()
			if !edges[e] {
				refuse("error.fon_damaged", "")
			}
			delete(edges, e)
			loop = append(loop, e.from)
			if e.to == start.from {
				break
			}
			choices, ok := outgoing[e.to]
			if !ok {
				refuse("error.fon_damaged", "")
			}
			dx, dy := e.to.x-e.from.x, e.to.y-e.from.y
			best, found, bestTurn := unitEdge{}, false, 0
			for _, c := range choices {
				if !edges[c] {
					continue
				}
				turn := dx*(c.to.y-c.from.y) - dy*(c.to.x-c.from.x)
				if !found || turn > bestTurn {
					best, found, bestTurn = c, true, turn
				}
			}
			if !found {
				refuse("error.fon_damaged", "")
			}
			e = best
		}
		for i, curr := range loop {
			tick()
			prev := loop[(i+len(loop)-1)%len(loop)]
			next := loop[(i+1)%len(loop)]
			if (curr.x-prev.x)*(next.y-curr.y) == (curr.y-prev.y)*(next.x-curr.x) {
				continue
			}
			points = append(points, glyphPoint{signed16(curr.x * scale), signed16((ascent - curr.y) * scale), true})
		}
		if len(points) > 0xFFFF || len(ends) == math.MaxInt16 {
			if len(points) > 0xFFFF {
				refuse("error.fon_too_large", "65535")
			}
			refuse("error.fon_too_large", "32767")
		}
		ends = append(ends, uint16(len(points)-1))
	}
	g := &ttGlyph{numberOfContours: int16(len(ends)), points: points, endPoints: ends}
	g.xMin, g.yMin, g.xMax, g.yMax = g.computeBounds()
	return g
}

// countFonPoints counts the corners an outline of the pixels has, stopping past limit.
func countFonPoints(ctx context.Context, px [][]bool, width, limit int) int {
	height := len(px)
	points := 0
	at := func(x, y int) bool { return x >= 0 && y >= 0 && x < width && y < height && px[y][x] }
	for y := 0; y <= height; y++ {
		checkCtx(ctx)
		for x := 0; x <= width; x++ {
			a, b, c, d := at(x-1, y-1), at(x, y-1), at(x-1, y), at(x, y)
			on := 0
			for _, v := range []bool{a, b, c, d} {
				if v {
					on++
				}
			}
			if on == 1 || on == 3 {
				points++
			} else if on == 2 && (a && d || b && c) {
				points += 2
			}
			if points > limit {
				return points
			}
		}
	}
	return points
}

func put16(b []byte, off, v int)        { binary.BigEndian.PutUint16(b[off:], uint16(v&0xFFFF)) }
func put32(b []byte, off int, v uint32) { binary.BigEndian.PutUint32(b[off:], v) }

func fonHead(upem, xMin, yMin, xMax, yMax int, longLoca bool, style int) []byte {
	b := make([]byte, 54)
	put32(b, 0, 0x00010000)
	put32(b, 4, 0x00010000)
	put32(b, 12, 0x5F0F3CF5)
	put16(b, 16, 3)
	put16(b, 18, upem)
	put16(b, 36, xMin)
	put16(b, 38, yMin)
	put16(b, 40, xMax)
	put16(b, 42, yMax)
	put16(b, 44, style)
	put16(b, 46, 8)
	put16(b, 48, 2)
	if longLoca {
		put16(b, 50, 1)
	}
	return b
}

func fonHhea(asc, desc, gap, maxAdvance, minLsb, minRsb, extent, count int) []byte {
	b := make([]byte, 36)
	put32(b, 0, 0x00010000)
	put16(b, 4, asc)
	put16(b, 6, desc)
	put16(b, 8, gap)
	put16(b, 10, maxAdvance)
	put16(b, 12, signed16(minLsb))
	put16(b, 14, signed16(minRsb))
	put16(b, 16, signed16(extent))
	put16(b, 18, 1)
	put16(b, 34, count)
	return b
}

func fonMaxp(count, points, contours int) []byte {
	b := make([]byte, 32)
	put32(b, 0, 0x00010000)
	put16(b, 4, count)
	put16(b, 6, points)
	put16(b, 8, contours)
	put16(b, 14, 1) // maxZones: no twilight zone
	return b
}

func fonMetrics(widths, bearings []int) []byte {
	b := make([]byte, len(widths)*4)
	for i := range widths {
		put16(b, i*4, widths[i])
		put16(b, i*4+2, bearings[i])
	}
	return b
}

func fonOs2(strike *fonStrike, widths []int, cmap map[int]int, cp, weight int, bold bool, asc, desc, gap, yMin, yMax int) []byte {
	b := make([]byte, 86)
	put16(b, 0, 1)
	sum := 0
	for _, w := range widths[1:] {
		sum += w
	}
	put16(b, 2, signed16(int(math.RoundToEven(float64(sum)/float64(len(widths)-1)))))
	put16(b, 4, weight)
	put16(b, 6, 5)
	// The PANOSE field is left zero (no invented classification).
	keys := make([]int, 0, len(cmap))
	for k := range cmap {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, s := range keys {
		bit := -1
		switch {
		case s <= 0x007F:
			bit = 0
		case s <= 0x00FF:
			bit = 1
		case s <= 0x017F:
			bit = 2
		case s <= 0x024F:
			bit = 3
		case s <= 0x02AF:
			bit = 4
		case s <= 0x02FF:
			bit = 5
		case s >= 0x0300 && s <= 0x036F:
			bit = 6
		case s >= 0x0370 && s <= 0x03FF:
			bit = 7
		case s >= 0x0400 && s <= 0x052F:
			bit = 9
		case s >= 0x0590 && s <= 0x05FF:
			bit = 11
		case s >= 0x0600 && s <= 0x06FF:
			bit = 13
		case s >= 0x0E00 && s <= 0x0E7F:
			bit = 24
		case s >= 0x1E00 && s <= 0x1EFF:
			bit = 29
		case s >= 0x2000 && s <= 0x206F:
			bit = 31
		case s >= 0x20A0 && s <= 0x20CF:
			bit = 33
		case s >= 0x2100 && s <= 0x214F:
			bit = 35
		}
		if bit >= 0 {
			off := 42 + bit/32*4
			put32(b, off, be32(b, off)|1<<(bit%32))
		}
	}
	copy(b[58:], "FILE")
	fs := 0
	if strike.italic {
		fs |= 1
	}
	if bold {
		fs |= 0x20
	}
	if !strike.italic && !bold {
		fs |= 0x40
	}
	put16(b, 62, fs)
	if len(keys) > 0 {
		put16(b, 64, keys[0])
		put16(b, 66, keys[len(keys)-1])
	}
	put16(b, 68, asc)
	put16(b, 70, desc)
	put16(b, 72, gap)
	put16(b, 74, max(0, max(asc, yMax)))
	put16(b, 76, max(0, -min(desc, yMin)))
	cpBit := map[int]int{874: 16, 1252: 0, 1250: 1, 1251: 2, 1253: 3, 1254: 4, 1255: 5, 1256: 6, 1257: 7, 1258: 8}
	bit, ok := cpBit[cp]
	if !ok {
		refuse("error.fon_damaged", "")
	}
	put32(b, 78, 1<<bit)
	return b
}

func utf16BE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.BigEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func fonName(family, style, full, ps string) []byte {
	names := []string{1: family, 2: style, 3: ps, 4: full, 5: "Version 1.0", 6: ps}
	type rec struct {
		platform, encoding, language, id int
		bytes                            []byte
	}
	var records []rec
	for _, platform := range []int{0, 3} {
		for id := 1; id <= 6; id++ {
			if platform == 0 {
				records = append(records, rec{0, 3, 0, id, utf16BE(names[id])})
			} else {
				records = append(records, rec{3, 1, 0x409, id, utf16BE(names[id])})
			}
		}
	}
	w := newWriter(0)
	w.u16(0)
	w.u16(len(records))
	w.u16(6 + len(records)*12)
	offset := 0
	for _, r := range records {
		if len(r.bytes) > 0xFFFF || offset > 0xFFFF-len(r.bytes) {
			refuse("error.fon_too_large", "65535")
		}
		w.u16(r.platform)
		w.u16(r.encoding)
		w.u16(r.language)
		w.u16(r.id)
		w.u16(len(r.bytes))
		w.u16(offset)
		offset += len(r.bytes)
	}
	for _, r := range records {
		w.bytes(r.bytes)
	}
	return w.buf
}

func postScriptName(full string) string {
	b := []byte(full)
	tokens := make([]string, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			tokens[i] = string(rune(c))
		} else {
			tokens[i] = fmt.Sprintf("_%02X", c)
		}
	}
	name := strings.Join(tokens, "")
	if len(name) <= 63 {
		return name
	}
	var prefix strings.Builder
	for _, t := range tokens {
		if prefix.Len()+len(t) > 50 {
			break
		}
		prefix.WriteString(t)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%s-%X", prefix.String(), sum[:6])
}

func fonCmap(m map[int]int) []byte {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	count := len(m) + 1
	length := 16 + 8*count
	sub := make([]byte, length)
	put16(sub, 0, 4)
	put16(sub, 2, length)
	put16(sub, 6, count*2)
	pow, selector := 1, 0
	for pow*2 <= count {
		pow *= 2
		selector++
	}
	put16(sub, 8, pow*2)
	put16(sub, 10, selector)
	put16(sub, 12, count*2-pow*2)
	i := 0
	for _, scalar := range keys {
		put16(sub, 14+i*2, scalar)
		put16(sub, 16+count*2+i*2, scalar)
		put16(sub, 16+count*4+i*2, (m[scalar]-scalar)&0xFFFF)
		i++
	}
	put16(sub, 14+i*2, 0xFFFF)
	put16(sub, 16+count*2+i*2, 0xFFFF)
	put16(sub, 16+count*4+i*2, 1)
	w := newWriter(0)
	w.u16(0)
	w.u16(2)
	w.u16(0)
	w.u16(3)
	w.u32(20)
	w.u16(3)
	w.u16(1)
	w.u32(20)
	w.bytes(sub)
	return w.buf
}
