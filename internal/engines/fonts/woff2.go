package fonts

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"slices"

	"github.com/andybalholm/brotli"
)

// WOFF 2.0 (W3C Recommendation): all tables in one Brotli stream, glyf/loca
// and hmtx transformed for better compression. Writes single fonts; reads
// single fonts and the first font of a WOFF2 collection.

const (
	woff2Signature  = 0x774F4632 // 'wOF2'
	woff2HeaderSize = 48
	brotliQuality   = 11
	brotliWindow    = 22
	arbitraryTag    = 63
)

// knownTags have a 6-bit code in the table directory (section 4.1 of the spec); others are spelled out.
var knownTags = []string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ", "fpgm", "glyf", "loca", "prep", "CFF ",
	"VORG", "EBDT", "EBLC", "gasp", "hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea", "vmtx", "BASE", "GDEF", "GPOS",
	"GSUB", "EBSC", "JSTF", "MATH", "CBDT", "CBLC", "COLR", "CPAL", "SVG ", "sbix", "acnt", "avar", "bdat", "bloc",
	"bsln", "cvar", "fdsc", "feat", "fmtx", "fvar", "gvar", "hsty", "just", "lcar", "mort", "morx", "opbd", "prop",
	"trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

type woff2Entry struct {
	tag          string
	version      int
	origLength   uint32
	storedLength uint32
	transformed  bool
	offset       int
}

// encodeWoff2 encodes the font. TrueType glyphs get the glyf/loca transform
// (and hmtx when possible), which is what makes WOFF2 smaller than WOFF; like
// other encoders, the DSIG signature is dropped because the rebuilt tables no
// longer match it, and head.flags bit 11 marks the font as losslessly
// transformed.
func encodeWoff2(ctx context.Context, source *sfnt, progress func(float64)) []byte {
	font := source.clone()
	delete(font.tables, "DSIG")

	var glyf *transformedGlyf
	var hmtx []byte
	if !font.hasCffOutlines() && font.has("glyf") && font.has("loca") && font.has("head") {
		glyf = tryTransformGlyf(ctx, font)
	}
	if glyf != nil {
		head := withIndexToLocFormat(font.require("head"), glyf.longLoca)
		binary.BigEndian.PutUint16(head[16:], be16(head, 16)|1<<11)
		font.tables["head"] = head
		if metrics, ok := font.tables["hmtx"]; ok {
			if hhea := font.table("hhea"); len(hhea) >= 36 {
				hmtx = tryTransformHmtx(metrics, font.glyphCount(), numberOfMetrics(hhea), glyf.xMins)
			}
		}
	}

	var entries []woff2Entry
	stream := newWriter(0)
	sfntSize := int64(12 + 16*len(font.tables))
	for _, tag := range font.sortedTags() {
		data := font.tables[tag]
		var e woff2Entry
		switch {
		case tag == "glyf" && glyf != nil:
			e = woff2Entry{tag: tag, origLength: uint32(glyf.glyfLength), storedLength: uint32(len(glyf.data)), transformed: true}
			data = glyf.data
		case tag == "loca" && glyf != nil:
			e = woff2Entry{tag: tag, origLength: uint32(glyf.locaLength), transformed: true}
			data = nil
		case tag == "glyf" || tag == "loca":
			e = woff2Entry{tag: tag, version: 3, origLength: uint32(len(data)), storedLength: uint32(len(data))}
		case tag == "hmtx" && hmtx != nil:
			e = woff2Entry{tag: tag, version: 1, origLength: uint32(len(data)), storedLength: uint32(len(hmtx)), transformed: true}
			data = hmtx
		default:
			e = woff2Entry{tag: tag, origLength: uint32(len(data)), storedLength: uint32(len(data))}
		}
		entries = append(entries, e)
		stream.bytes(data)
		sfntSize += int64((e.origLength + 3) &^ 3)
	}
	checkCtx(ctx)
	compressed := brotliCompress(ctx, stream.buf, progress)

	w := newWriter(woff2HeaderSize + len(entries)*10 + len(compressed) + 4)
	w.u32(woff2Signature)
	w.u32(font.flavor)
	w.u32(0) // length, patched below
	w.u16(len(entries))
	w.u16(0) // reserved
	w.u32(uint32(min(sfntSize, 0xFFFFFFFF)))
	w.u32(uint32(len(compressed)))
	major, minor := fontVersion(font)
	w.u16(major)
	w.u16(minor)
	w.zeros(20) // no extended metadata, no private data
	for _, e := range entries {
		index := slices.Index(knownTags, e.tag)
		code := index
		if index < 0 {
			code = arbitraryTag
		}
		w.u8(code | e.version<<6)
		if index < 0 {
			w.u32(tagValue(e.tag))
		}
		w.uintBase128(e.origLength)
		if e.transformed {
			w.uintBase128(e.storedLength)
		}
	}
	w.bytes(compressed)
	w.pad4()
	w.setU32(8, uint32(w.len()))
	return w.buf
}

// decodeWoff2 decodes a WOFF2 file (the first font of a collection) into an SFNT font.
func decodeWoff2(ctx context.Context, data []byte) *sfnt {
	r := newReader(data)
	if r.u32() != woff2Signature {
		fail("This is not a WOFF2 file.")
	}
	flavor := r.u32()
	r.skip(4) // length
	tableCount := int(r.u16())
	r.skip(6) // reserved, totalSfntSize
	compressedLength := r.u32()
	r.pos = woff2HeaderSize

	entries := make([]woff2Entry, 0, tableCount)
	var streamLength int64
	for i := 0; i < tableCount; i++ {
		flags := r.u8()
		var tag string
		if int(flags&0x3F) == arbitraryTag {
			tag = tagName(r.u32())
		} else {
			if int(flags&0x3F) >= len(knownTags) {
				fail("The WOFF2 table directory is damaged.")
			}
			tag = knownTags[flags&0x3F]
		}
		version := int(flags >> 6)
		origLength := r.uintBase128()
		// glyf and loca use version 3 for "not transformed"; every other table uses version 0.
		glyfOrLoca := tag == "glyf" || tag == "loca"
		transformed := version != 0
		if glyfOrLoca {
			transformed = version != 3
		}
		if transformed && !(glyfOrLoca && version == 0) && !(tag == "hmtx" && version == 1) {
			fail("The WOFF2 table '" + trimTag(tag) + "' uses an unknown transform.")
		}
		stored := origLength
		if transformed {
			stored = r.uintBase128()
		}
		if tag == "loca" && transformed && stored != 0 {
			fail("The WOFF2 'loca' table is damaged.")
		}
		entries = append(entries, woff2Entry{tag, version, origLength, stored, transformed, int(min(streamLength, 1<<31-1))})
		streamLength += int64(stored)
	}
	if streamLength > 1<<31-1 {
		fail("The WOFF2 file is too large.")
	}

	// A collection lists which tables belong to each font; only the first font is converted.
	selected := entries
	if flavor == collectionTag {
		r.u32() // collection version
		fonts := int(r.read255UInt16())
		if fonts == 0 {
			fail("The WOFF2 font collection is empty.")
		}
		for member := 0; member < fonts; member++ {
			count := int(r.read255UInt16())
			fontFlavor := r.u32()
			indices := make([]int, count)
			for i := range indices {
				indices[i] = int(r.read255UInt16())
				if indices[i] >= len(entries) {
					fail("The WOFF2 font collection is damaged.")
				}
			}
			if member == 0 {
				flavor = fontFlavor
				selected = nil
				for _, i := range indices {
					selected = append(selected, entries[i])
				}
			}
		}
	}
	if flavor != trueTypeFlavor && flavor != cffFlavor && flavor != appleTrueTypeFlavor {
		fail("The WOFF2 file contains an unknown kind of font.")
	}
	if int64(compressedLength) > int64(len(data)-r.pos) {
		fail("The WOFF2 file is truncated.")
	}
	stream := brotliDecompress(data[r.pos:r.pos+int(compressedLength)], int(streamLength))
	checkCtx(ctx)

	if flavor == appleTrueTypeFlavor {
		flavor = trueTypeFlavor
	}
	font := newSfnt(flavor)
	var glyfEntry, locaEntry, hmtxEntry *woff2Entry
	for i := range selected {
		e := &selected[i]
		if !e.transformed {
			font.tables[e.tag] = slices.Clone(stream[e.offset : e.offset+int(e.storedLength)])
		}
		switch {
		case e.tag == "glyf" && glyfEntry == nil:
			glyfEntry = e
		case e.tag == "loca" && locaEntry == nil:
			locaEntry = e
		case e.tag == "hmtx" && e.transformed && hmtxEntry == nil:
			hmtxEntry = e
		}
	}
	if (glyfEntry == nil) != (locaEntry == nil) || glyfEntry != nil && glyfEntry.transformed != locaEntry.transformed {
		fail("The WOFF2 'glyf' and 'loca' tables don't match.")
	}
	var xMins []int16
	if glyfEntry != nil && glyfEntry.transformed {
		g, loca, long, mins := reconstructGlyf(ctx, stream, glyfEntry.offset, int(glyfEntry.storedLength))
		font.tables["glyf"] = g
		font.tables["loca"] = loca
		font.tables["head"] = withIndexToLocFormat(font.require("head"), long)
		xMins = mins
	}
	if hmtxEntry != nil {
		if xMins == nil {
			fail("The WOFF2 'hmtx' table is transformed without 'glyf'.")
		}
		font.tables["hmtx"] = reconstructHmtx(stream, hmtxEntry.offset, int(hmtxEntry.storedLength),
			font.glyphCount(), numberOfMetrics(font.require("hhea")), xMins)
	}
	return font
}

// brotliCompress compresses at the highest quality, like Google's encoder. That
// takes a while per megabyte, so the data is fed in chunks to report progress
// and honour cancellation.
func brotliCompress(ctx context.Context, data []byte, progress func(float64)) []byte {
	const chunk = 256 * 1024
	var out bytes.Buffer
	w := brotli.NewWriterOptions(&out, brotli.WriterOptions{Quality: brotliQuality, LGWin: brotliWindow})
	for rest := data; ; {
		take := min(chunk, len(rest))
		w.Write(rest[:take])
		rest = rest[take:]
		if len(rest) == 0 {
			break
		}
		checkCtx(ctx)
		if progress != nil {
			progress(1 - float64(len(rest))/float64(len(data)))
		}
	}
	w.Close()
	return out.Bytes()
}

func brotliDecompress(data []byte, length int) []byte {
	out := make([]byte, length)
	r := brotli.NewReader(bytes.NewReader(data))
	if _, err := io.ReadFull(r, out); err != nil {
		fail("The WOFF2 font data can't be decompressed (the file is damaged).")
	}
	var extra [1]byte
	if n, _ := r.Read(extra[:]); n != 0 {
		fail("The WOFF2 font data can't be decompressed (the file is damaged).")
	}
	return out
}
