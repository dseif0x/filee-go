package fonts

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser for the Compact Font Format version 1 (Adobe Technical Note #5176),
// the 'CFF ' table of OpenType fonts with PostScript outlines: charstrings,
// subroutines, private dictionaries, charsets and CID font dictionaries.

// DICT operators (two-byte operators are 1200 + second byte).
const (
	opCharset        = 15
	opCharStrings    = 17
	opPrivate        = 18
	opSubrs          = 19
	opCharstringType = 1206
	opFontMatrix     = 1207
	opRos            = 1230
	opFdArray        = 1236
	opFdSelect       = 1237
)

var defaultFontMatrix = []float64{0.001, 0, 0, 0.001, 0, 0}

// cffPrivate holds the values of a Private DICT that charstrings need (widths are not: hmtx has them).
type cffPrivate struct{ subrs [][]byte }

// cffFont is a parsed CFF font (the first font of the table's FontSet).
type cffFont struct {
	charStrings  [][]byte
	globalSubrs  [][]byte
	privates     []cffPrivate
	fontMatrices [][]float64 // the Top DICT's FontMatrix combined with each font dictionary's own, per FD
	isCid        bool
	charset      []uint16 // SID or CID of each glyph; nil for the predefined expert charsets
	strings      []string // the String INDEX (SID 391 and up)
	fdSelect     []byte
	glyphBySid   map[int]int
}

func (f *cffFont) glyphCount() int { return len(f.charStrings) }

func (f *cffFont) fontDictIndex(glyph int) int {
	if glyph < len(f.fdSelect) {
		return int(f.fdSelect[glyph])
	}
	return 0
}

// glyphName is the glyph name from the charset; false for CID fonts or unknown charsets.
func (f *cffFont) glyphName(glyph int) (string, bool) {
	if f.isCid || f.charset == nil || glyph >= len(f.charset) {
		return "", false
	}
	sid := int(f.charset[glyph])
	if sid < len(cffStandardStrings) {
		return cffStandardStrings[sid], true
	}
	if sid-len(cffStandardStrings) < len(f.strings) {
		return f.strings[sid-len(cffStandardStrings)], true
	}
	return "", false
}

// glyphForSid is the glyph whose charset entry is sid, or -1.
func (f *cffFont) glyphForSid(sid int) int {
	if f.charset == nil || f.isCid {
		return -1
	}
	if f.glyphBySid == nil {
		f.glyphBySid = map[int]int{}
		for gid, s := range f.charset {
			if _, ok := f.glyphBySid[int(s)]; !ok {
				f.glyphBySid[int(s)] = gid
			}
		}
	}
	if g, ok := f.glyphBySid[sid]; ok {
		return g
	}
	return -1
}

func parseCff(data []byte) *cffFont {
	if len(data) < 4 {
		fail("The CFF table is damaged.")
	}
	if data[0] != 1 {
		fail(fmt.Sprintf("CFF version %d outlines can't be converted.", data[0]))
	}
	pos := int(data[2])   // hdrSize
	readIndex(data, &pos) // Name INDEX
	topDicts := readIndex(data, &pos)
	strs := readIndex(data, &pos)
	globalSubrs := readIndex(data, &pos)
	if len(topDicts) == 0 {
		fail("The CFF table has no font.")
	}
	top := parseDict(topDicts[0])
	if dictNumber(top, opCharstringType, 2) != 2 {
		fail("The CFF font uses Type 1 charstrings, which aren't supported.")
	}
	cs, ok := top[opCharStrings]
	if !ok || len(cs) == 0 {
		fail("The CFF font has no glyphs.")
	}
	at := cffOffset(cs[0], data)
	charStrings := readIndex(data, &at)
	topMatrix := defaultFontMatrix
	if m, ok := top[opFontMatrix]; ok && len(m) == 6 {
		topMatrix = m
	}
	_, isCid := top[opRos]

	f := &cffFont{charStrings: charStrings, globalSubrs: globalSubrs, isCid: isCid}
	if isCid {
		fdArray, ok := top[opFdArray]
		if !ok || len(fdArray) == 0 {
			fail("The CID-keyed CFF font has no font dictionaries.")
		}
		fdAt := cffOffset(fdArray[0], data)
		for _, raw := range readIndex(data, &fdAt) {
			fd := parseDict(raw)
			f.privates = append(f.privates, readPrivate(data, fd))
			// A font dictionary's FontMatrix applies first, then the Top DICT's.
			if fm, ok := fd[opFontMatrix]; ok && len(fm) == 6 {
				f.fontMatrices = append(f.fontMatrices, multiplyMatrix(fm, topMatrix))
			} else {
				f.fontMatrices = append(f.fontMatrices, topMatrix)
			}
		}
		if sel, ok := top[opFdSelect]; ok && len(sel) > 0 {
			f.fdSelect = readFdSelect(data, cffOffset(sel[0], data), len(charStrings), len(f.privates))
		}
	} else {
		f.privates = []cffPrivate{readPrivate(data, top)}
		f.fontMatrices = [][]float64{topMatrix}
	}
	if len(f.privates) == 0 {
		fail("The CID-keyed CFF font has no font dictionaries.")
	}
	f.charset = readCharset(data, int(dictNumber(top, opCharset, 0)), len(charStrings))
	for _, s := range strs {
		rs := make([]rune, len(s))
		for i, c := range s {
			rs[i] = rune(c) // Latin-1
		}
		f.strings = append(f.strings, string(rs))
	}
	return f
}

// multiplyMatrix combines two FontMatrix transforms: first is applied, then then.
func multiplyMatrix(first, then []float64) []float64 {
	return []float64{
		then[0]*first[0] + then[2]*first[1],
		then[1]*first[0] + then[3]*first[1],
		then[0]*first[2] + then[2]*first[3],
		then[1]*first[2] + then[3]*first[3],
		then[0]*first[4] + then[2]*first[5] + then[4],
		then[1]*first[4] + then[3]*first[5] + then[5],
	}
}

func readPrivate(data []byte, dict map[int][]float64) cffPrivate {
	entry, ok := dict[opPrivate]
	if !ok || len(entry) < 2 {
		return cffPrivate{}
	}
	size := int(entry[0])
	offset := cffOffset(entry[1], data)
	if size < 0 || size > len(data)-offset {
		fail("The CFF private dictionary lies outside the table.")
	}
	priv := parseDict(data[offset : offset+size])
	var p cffPrivate
	if subrs, ok := priv[opSubrs]; ok && len(subrs) > 0 {
		at := cffOffset(float64(offset)+subrs[0], data)
		p.subrs = readIndex(data, &at)
	}
	return p
}

func readCharset(data []byte, offset, glyphCount int) []uint16 {
	charset := make([]uint16, glyphCount)
	switch offset {
	case 0:
		// ISOAdobe: glyph i is SID i.
		for i := range charset {
			charset[i] = uint16(i)
		}
		return charset
	case 1, 2:
		return nil // Expert charsets: only used by old expert fonts; names are not needed to convert them.
	}
	r := newReader(data)
	r.pos = cffOffset(float64(offset), data)
	format := r.u8()
	for glyph := 1; glyph < glyphCount; {
		switch format {
		case 0:
			charset[glyph] = r.u16()
			glyph++
		case 1, 2:
			first := int(r.u16())
			var left int
			if format == 1 {
				left = int(r.u8())
			} else {
				left = int(r.u16())
			}
			for i := 0; i <= left && glyph < glyphCount; i++ {
				charset[glyph] = uint16(first + i)
				glyph++
			}
		default:
			fail(fmt.Sprintf("Unknown CFF charset format %d.", format))
		}
	}
	return charset
}

func readFdSelect(data []byte, offset, glyphCount, fdCount int) []byte {
	fds := make([]byte, glyphCount)
	r := newReader(data)
	r.pos = offset
	switch format := r.u8(); format {
	case 0:
		for i := range fds {
			fds[i] = r.u8()
		}
	case 3:
		ranges := int(r.u16())
		first := int(r.u16())
		for i := 0; i < ranges; i++ {
			fd := r.u8()
			next := int(r.u16())
			for g := first; g < next && g < glyphCount; g++ {
				fds[g] = fd
			}
			first = next
		}
	default:
		fail(fmt.Sprintf("Unknown CFF FDSelect format %d.", format))
	}
	for _, fd := range fds {
		if int(fd) >= fdCount {
			fail("The CFF FDSelect refers to a missing font dictionary.")
		}
	}
	return fds
}

// readIndex reads an INDEX (count, offset size, 1-based offsets, data) and advances past it.
func readIndex(data []byte, pos *int) [][]byte {
	r := newReader(data)
	r.pos = *pos
	count := int(r.u16())
	if count == 0 {
		*pos = r.pos
		return nil
	}
	offSize := int(r.u8())
	if offSize < 1 || offSize > 4 {
		fail("The CFF table is damaged (invalid INDEX).")
	}
	offsets := make([]int64, count+1)
	for i := range offsets {
		var v int64
		for b := 0; b < offSize; b++ {
			v = v<<8 | int64(r.u8())
		}
		offsets[i] = v
	}
	dataStart := int64(r.pos) - 1 // offsets are 1-based
	items := make([][]byte, count)
	for i := range items {
		start, end := dataStart+offsets[i], dataStart+offsets[i+1]
		if start < int64(r.pos) || end < start || end > int64(len(data)) {
			fail("The CFF table is damaged (INDEX data outside the table).")
		}
		items[i] = data[start:end]
	}
	*pos = int(dataStart + offsets[count])
	return items
}

// parseDict parses a DICT into operator → operands.
func parseDict(data []byte) map[int][]float64 {
	dict := map[int][]float64{}
	var operands []float64
	r := newReader(data)
	for r.remaining() > 0 {
		b0 := int(r.u8())
		switch {
		case b0 <= 21:
			op := b0
			if b0 == 12 {
				op = 1200 + int(r.u8())
			}
			dict[op] = operands
			operands = nil
		case b0 == 28:
			operands = append(operands, float64(r.i16()))
		case b0 == 29:
			operands = append(operands, float64(r.i32()))
		case b0 == 30:
			operands = append(operands, readReal(r))
		case b0 >= 32 && b0 <= 246:
			operands = append(operands, float64(b0-139))
		case b0 >= 247 && b0 <= 250:
			operands = append(operands, float64((b0-247)*256+int(r.u8())+108))
		case b0 >= 251 && b0 <= 254:
			operands = append(operands, float64(-(b0-251)*256-int(r.u8())-108))
		default:
			fail("The CFF table is damaged (invalid DICT data).")
		}
	}
	return dict
}

// readReal reads a real number: nibbles for digits, '.', 'E', 'E-' and '-', ended by 0xF.
func readReal(r *reader) float64 {
	var b strings.Builder
	for {
		v := r.u8()
		for _, n := range []byte{v >> 4, v & 0xF} {
			switch {
			case n <= 9:
				b.WriteByte('0' + n)
			case n == 0xA:
				b.WriteByte('.')
			case n == 0xB:
				b.WriteByte('E')
			case n == 0xC:
				b.WriteString("E-")
			case n == 0xE:
				b.WriteByte('-')
			case n == 0xF:
				f, err := strconv.ParseFloat(b.String(), 64)
				if err != nil {
					return 0
				}
				return f
			}
		}
	}
}

func dictNumber(dict map[int][]float64, op int, fallback float64) float64 {
	if v, ok := dict[op]; ok && len(v) > 0 {
		return v[0]
	}
	return fallback
}

func cffOffset(v float64, data []byte) int {
	if v < 0 || v >= float64(len(data)) {
		fail("The CFF table is damaged (offset outside the table).")
	}
	return int(v)
}
