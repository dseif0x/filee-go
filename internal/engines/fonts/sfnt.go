package fonts

import (
	"encoding/binary"
	"math/bits"
	"slices"
	"sort"
)

// The OpenType / TrueType container (SFNT): a flavour plus a set of tables.
// Reads TTF / OTF (and the first font of a TTC collection) and writes them back
// with correct checksums, head.checkSumAdjustment and search fields.

const (
	trueTypeFlavor      = 0x00010000
	cffFlavor           = 0x4F54544F // 'OTTO'
	appleTrueTypeFlavor = 0x74727565 // 'true'
	collectionTag       = 0x74746366 // 'ttcf'
	checksumMagic       = 0xB1B0AFBA
)

// Data order recommended by the OpenType spec for TrueType and CFF fonts; other tables follow by tag.
var (
	trueTypeOrder = []string{"head", "hhea", "maxp", "OS/2", "hmtx", "LTSH", "VDMX", "hdmx", "cmap", "fpgm", "prep", "cvt ", "loca", "glyf", "kern", "name", "post", "gasp", "PCLT"}
	cffOrder      = []string{"head", "hhea", "maxp", "OS/2", "name", "cmap", "post", "CFF "}
)

// sfnt is an OpenType font as a set of raw tables, independent of the container it came in.
type sfnt struct {
	flavor uint32
	tables map[string][]byte
}

func newSfnt(flavor uint32) *sfnt { return &sfnt{flavor: flavor, tables: map[string][]byte{}} }

func (f *sfnt) table(tag string) []byte { return f.tables[tag] }

func (f *sfnt) has(tag string) bool { _, ok := f.tables[tag]; return ok }

// hasCffOutlines is true when the glyphs are CFF (PostScript) outlines.
func (f *sfnt) hasCffOutlines() bool { return f.has("CFF ") && !f.has("glyf") }

// require returns a table or fails with a readable error when it is missing.
func (f *sfnt) require(tag string) []byte {
	t, ok := f.tables[tag]
	if !ok {
		fail("The font has no '" + trimTag(tag) + "' table.")
	}
	return t
}

func trimTag(tag string) string {
	for len(tag) > 0 && tag[len(tag)-1] == ' ' {
		tag = tag[:len(tag)-1]
	}
	return tag
}

func (f *sfnt) glyphCount() int {
	m := f.require("maxp")
	if len(m) < 6 {
		fail("The font file is truncated or damaged.")
	}
	return int(be16(m, 4))
}

func (f *sfnt) sortedTags() []string {
	tags := make([]string, 0, len(f.tables))
	for t := range f.tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

func (f *sfnt) clone() *sfnt {
	c := newSfnt(f.flavor)
	for k, v := range f.tables {
		c.tables[k] = v
	}
	return c
}

func tagValue(tag string) uint32 {
	return uint32(tag[0])<<24 | uint32(tag[1])<<16 | uint32(tag[2])<<8 | uint32(tag[3])
}

func tagName(t uint32) string {
	return string([]byte{byte(t >> 24), byte(t >> 16), byte(t >> 8), byte(t)})
}

// checksum is the OpenType table checksum: the sum of big-endian uint32s, the last one zero-padded.
func checksum(data []byte) uint32 {
	var sum uint32
	whole := len(data) &^ 3
	for i := 0; i < whole; i += 4 {
		sum += binary.BigEndian.Uint32(data[i:])
	}
	if whole < len(data) {
		var last [4]byte
		copy(last[:], data[whole:])
		sum += binary.BigEndian.Uint32(last[:])
	}
	return sum
}

// tableChecksum is the checksum stored in the table directory (head counts with checkSumAdjustment = 0).
func tableChecksum(tag string, data []byte) uint32 {
	if tag != "head" || len(data) < 12 {
		return checksum(data)
	}
	c := slices.Clone(data)
	clear(c[8:12])
	return checksum(c)
}

func isSfnt(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	switch binary.BigEndian.Uint32(data) {
	case trueTypeFlavor, cffFlavor, appleTrueTypeFlavor, collectionTag:
		return true
	}
	return false
}

// readSfnt reads a TTF/OTF, or the first font of a TTC collection.
func readSfnt(data []byte, offset int) *sfnt {
	r := newReader(data)
	r.pos = offset
	flavor := r.u32()
	if flavor == collectionTag {
		if offset != 0 {
			fail("Nested font collection.")
		}
		r.skip(4) // version
		if r.u32() == 0 {
			fail("The font collection is empty.")
		}
		off := r.u32()
		if off > 1<<31-1 {
			fail("The font file is truncated or damaged.")
		}
		return readSfnt(data, int(off))
	}
	if flavor != trueTypeFlavor && flavor != cffFlavor && flavor != appleTrueTypeFlavor {
		fail("This is not a TrueType or OpenType font.")
	}
	if flavor == appleTrueTypeFlavor {
		flavor = trueTypeFlavor
	}
	font := newSfnt(flavor)
	count := int(r.u16())
	r.skip(6) // searchRange, entrySelector, rangeShift
	for i := 0; i < count; i++ {
		tag := tagName(r.u32())
		r.skip(4) // checksum
		off := r.u32()
		length := r.u32()
		if uint64(off) > uint64(len(data)) || uint64(length) > uint64(len(data))-uint64(off) {
			fail("The font's '" + trimTag(tag) + "' table lies outside the file.")
		}
		font.tables[tag] = slices.Clone(data[off : off+length])
	}
	return font
}

// write writes the font: directory sorted by tag, table data in the order the
// OpenType spec recommends, every table 4-byte aligned, checksums and
// head.checkSumAdjustment computed.
func (f *sfnt) write() []byte {
	tags := f.sortedTags()
	order := trueTypeOrder
	if f.flavor == cffFlavor {
		order = cffOrder
	}
	rank := func(t string) int {
		if t == "DSIG" {
			return 1 << 30
		}
		if i := slices.Index(order, t); i >= 0 {
			return i
		}
		return len(order)
	}
	dataOrder := slices.Clone(tags)
	sort.SliceStable(dataOrder, func(i, j int) bool { return rank(dataOrder[i]) < rank(dataOrder[j]) })

	headerSize := 12 + 16*len(tags)
	offsets := map[string]int{}
	pos := headerSize
	for _, t := range dataOrder {
		offsets[t] = pos
		pos += (len(f.tables[t]) + 3) &^ 3
	}
	out := make([]byte, pos)
	w := newWriter(headerSize)
	writeOffsetTable(w, f.flavor, len(tags))
	for _, t := range tags {
		data := f.tables[t]
		w.u32(tagValue(t))
		w.u32(tableChecksum(t, data))
		w.u32(uint32(offsets[t]))
		w.u32(uint32(len(data)))
		copy(out[offsets[t]:], data)
	}
	copy(out, w.buf)
	if head, ok := offsets["head"]; ok && len(f.tables["head"]) >= 12 {
		clear(out[head+8 : head+12])
		binary.BigEndian.PutUint32(out[head+8:], checksumMagic-checksum(out))
	}
	return out
}

// writeOffsetTable writes sfntVersion, numTables and the binary-search fields derived from it.
func writeOffsetTable(w *writer, flavor uint32, count int) {
	entrySelector := 0
	if count > 0 {
		entrySelector = bits.Len(uint(count)) - 1
	}
	searchRange := (1 << entrySelector) * 16
	w.u32(flavor)
	w.u16(count)
	w.u16(searchRange)
	w.u16(entrySelector)
	w.u16(count*16 - searchRange)
}

// ---- small fixed-layout tables ----

func unitsPerEm(head []byte) int { return int(be16(head, 18)) }

func indexToLocFormat(head []byte) int {
	if len(head) < 52 {
		fail("The font file is truncated or damaged.")
	}
	return int(int16(be16(head, 50)))
}

// withIndexToLocFormat returns a copy of head with indexToLocFormat set.
func withIndexToLocFormat(head []byte, long bool) []byte {
	if len(head) < 52 {
		fail("The font file is truncated or damaged.")
	}
	c := slices.Clone(head)
	v := uint16(0)
	if long {
		v = 1
	}
	binary.BigEndian.PutUint16(c[50:], v)
	return c
}

// numberOfMetrics is hhea.numberOfHMetrics (also vhea.numOfLongVerMetrics, same layout).
func numberOfMetrics(hhea []byte) int {
	if len(hhea) < 36 {
		fail("The font file is truncated or damaged.")
	}
	return int(be16(hhea, 34))
}
