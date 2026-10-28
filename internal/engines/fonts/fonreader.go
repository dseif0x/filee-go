package fonts

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// Windows bitmap fonts (.fon): the monochrome FNT v2/v3 resources of a 16-bit
// NE font library, each turned into an unhinted TrueType outline font.

// fonGlyph is a validated raster glyph; pixels are indexed by row, then column.
type fonGlyph struct {
	byteValue    byte
	width        int
	pixels       [][]bool
	bitmapOffset int64
}

// fonStrike is one RT_FONT resource, independent of its NE container.
type fonStrike struct {
	version, pixelWidth, pixelHeight, ascent, externalLeading int
	italic                                                    bool
	weight                                                    int
	charSet                                                   byte
	face                                                      string
	defaultChar, breakChar                                    int
	glyphs                                                    []fonGlyph
}

// maxGlyphPixels is the maximum sum of max(width, 1) * height for every glyph
// entry across all RT_FONT resources; zero-width rows and entries aliasing the
// same bitmap bytes each count separately.
const maxGlyphPixels = 4_194_304

func fits(limit, start, length int64) bool {
	return start >= 0 && length >= 0 && start <= limit && length <= limit-start
}

func le16(b []byte, off int64) int { return int(binary.LittleEndian.Uint16(b[off:])) }
func le32(b []byte, off int64) int64 {
	return int64(binary.LittleEndian.Uint32(b[off:]))
}

// readFon validates every glyph entry across all RT_FONT resources before decoding.
func readFon(ctx context.Context, data []byte) []fonStrike {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		refuse("error.fon_damaged", "")
	}
	size := int64(len(data))
	ne := le32(data, 0x3c)
	if !fits(size, ne, 0x28) {
		refuse("error.fon_damaged", "")
	}
	if data[ne] == 'P' && data[ne+1] == 'E' {
		refuse("error.fon_unsupported", "PE")
	}
	if data[ne] != 'N' || data[ne+1] != 'E' {
		refuse("error.fon_damaged", "")
	}
	table := ne + int64(le16(data, ne+0x24))
	end := ne + int64(le16(data, ne+0x26))
	if end <= table || !fits(size, table, 2) || end > size {
		refuse("error.fon_damaged", "")
	}
	shift := le16(data, table)
	if shift > 15 {
		refuse("error.fon_damaged", "")
	}
	type span struct{ start, end int64 }
	var ranges []span
	cursor := table + 2
	terminated := false
	for fits(end, cursor, 2) {
		typ := le16(data, cursor)
		if typ == 0 {
			terminated = true
			break
		}
		if !fits(end, cursor, 8) {
			refuse("error.fon_damaged", "")
		}
		count := int64(le16(data, cursor+2))
		cursor += 8
		if !fits(end, cursor, count*12) {
			refuse("error.fon_damaged", "")
		}
		if typ == 0x8008 {
			for i := int64(0); i < count; i++ {
				entry := cursor + i*12
				start := int64(le16(data, entry)) << shift
				length := int64(le16(data, entry+2)) << shift
				if length == 0 || start < end || !fits(size, start, length) {
					refuse("error.fon_damaged", "")
				}
				ranges = append(ranges, span{start, start + length})
			}
		}
		cursor += count * 12
	}
	if !terminated || len(ranges) == 0 {
		refuse("error.fon_damaged", "")
	}
	sorted := append([]span(nil), ranges...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start < sorted[i-1].end {
			refuse("error.fon_damaged", "")
		}
	}
	var pixels int64
	for _, r := range ranges {
		readFnt(ctx, data[r.start:r.end], false, &pixels)
	}
	strikes := make([]fonStrike, 0, len(ranges))
	for _, r := range ranges {
		strikes = append(strikes, *readFnt(ctx, data[r.start:r.end], true, &pixels))
	}
	return strikes
}

func readFnt(ctx context.Context, resource []byte, decode bool, glyphPixels *int64) *fonStrike {
	if len(resource) < 6 {
		refuse("error.fon_damaged", "")
	}
	version := le16(resource, 0)
	if version != 0x0200 && version != 0x0300 {
		refuse("error.fon_unsupported", fmt.Sprintf("FNT 0x%04X", version))
	}
	header := int64(0x94)
	if version == 0x0200 {
		header = 0x76
	}
	size := le32(resource, 2)
	if size < header || size > int64(len(resource)) {
		refuse("error.fon_damaged", "")
	}
	fnt := resource[:size]
	typ := le16(fnt, 0x42)
	if typ&1 != 0 {
		refuse("error.fon_vector", "")
	}
	if typ&4 != 0 {
		refuse("error.fon_unsupported", "")
	}
	if version == 0x0300 {
		if flags := le32(fnt, 0x76); flags != 1 && flags != 2 {
			refuse("error.fon_unsupported", fmt.Sprintf("dfFlags 0x%08X", flags))
		}
	}
	height := le16(fnt, 0x58)
	ascent := le16(fnt, 0x4a)
	first, last := int(fnt[0x5f]), int(fnt[0x60])
	count := last - first + 1
	if height > 16384 {
		refuse("error.fon_too_large", "16384")
	}
	if height == 0 || ascent > height || count <= 0 {
		refuse("error.fon_damaged", "")
	}
	defaultChar, breakChar := int(fnt[0x61]), int(fnt[0x62])
	if defaultChar >= count || breakChar >= count {
		refuse("error.fon_damaged", "")
	}
	weight := le16(fnt, 0x53)
	if weight > 1000 {
		refuse("error.fon_damaged", "")
	}
	charset := fnt[0x55]
	enc, _ := charSetEncoding(charset)
	faceOffset := le32(fnt, 0x69)
	if !fits(size, faceOffset, 1) {
		refuse("error.fon_damaged", "")
	}
	faceEnd := strings.IndexByte(string(fnt[faceOffset:]), 0)
	if faceEnd < 0 {
		refuse("error.fon_damaged", "")
	}
	face, ok := strictDecode(enc, fnt[faceOffset:faceOffset+int64(faceEnd)])
	face = strings.TrimSpace(face)
	if !ok || face == "" {
		refuse("error.fon_damaged", "")
	}
	entrySize := int64(6)
	if version == 0x0200 {
		entrySize = 4
	}
	tableEnd := header + int64(count+1)*entrySize
	if tableEnd > size {
		refuse("error.fon_damaged", "")
	}
	bitsOffset := le32(fnt, 0x71)
	if bitsOffset < tableEnd || bitsOffset > size {
		refuse("error.fon_damaged", "")
	}
	scale := fonScale(height)
	var glyphs []fonGlyph
	for i := 0; i < count; i++ {
		checkCtx(ctx)
		entry := header + int64(i)*entrySize
		width := le16(fnt, entry)
		var offset int64
		if version == 0x0200 {
			offset = int64(le16(fnt, entry+2))
		} else {
			offset = le32(fnt, entry+2)
		}
		nbytes := (int64(width) + 7) / 8 * int64(height)
		if int64(width)*int64(scale) > 0xFFFF {
			refuse("error.fon_too_large", "65535")
		}
		if offset < bitsOffset || !fits(size, offset, nbytes) {
			refuse("error.fon_damaged", "")
		}
		if !decode {
			*glyphPixels += int64(max(width, 1)) * int64(height)
			if *glyphPixels > maxGlyphPixels {
				refuse("error.fon_too_large", "4,194,304")
			}
			continue
		}
		px := make([][]bool, height)
		for y := range px {
			px[y] = make([]bool, width)
			for x := 0; x < width; x++ {
				px[y][x] = fnt[offset+int64(x/8)*int64(height)+int64(y)]&(0x80>>(x%8)) != 0
			}
		}
		glyphs = append(glyphs, fonGlyph{byte(first + i), width, px, offset})
	}
	if !decode {
		return nil
	}
	if weight == 0 {
		weight = 400
	}
	return &fonStrike{version, le16(fnt, 0x56), height, ascent, le16(fnt, 0x4e), fnt[0x50] != 0, weight, charset, face, defaultChar, breakChar, glyphs}
}

// charSetEncoding is the Windows code page of a supported single-byte FNT charset.
func charSetEncoding(charset byte) (encoding.Encoding, int) {
	switch charset {
	case 0:
		return charmap.Windows1252, 1252
	case 238:
		return charmap.Windows1250, 1250
	case 204:
		return charmap.Windows1251, 1251
	case 161:
		return charmap.Windows1253, 1253
	case 162:
		return charmap.Windows1254, 1254
	case 177:
		return charmap.Windows1255, 1255
	case 178:
		return charmap.Windows1256, 1256
	case 186:
		return charmap.Windows1257, 1257
	case 163:
		return charmap.Windows1258, 1258
	case 222:
		return charmap.Windows874, 874
	}
	refuse("error.fon_unsupported", fmt.Sprintf("charset %d", charset))
	return nil, 0
}

// strictDecode decodes single-byte text; false when a byte is undefined in the code page.
func strictDecode(e encoding.Encoding, b []byte) (string, bool) {
	out, err := e.NewDecoder().Bytes(b)
	if err != nil || strings.ContainsRune(string(out), utf8.RuneError) {
		return "", false
	}
	return string(out), true
}
