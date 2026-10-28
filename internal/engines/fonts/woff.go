package fonts

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"math"
)

// WOFF 1.0 (W3C Recommendation, 2012): an SFNT whose tables are
// zlib-compressed one by one. Lossless both ways.

const (
	woffSignature  = 0x774F4646 // 'wOFF'
	woffHeaderSize = 44
	woffEntrySize  = 20
)

// encodeWoff wraps the font: each table zlib-compressed, or stored as-is when
// compressing doesn't make it smaller (as the spec requires). Extended metadata
// and private data are not written.
func encodeWoff(ctx context.Context, font *sfnt) []byte {
	// Normalize through the SFNT writer so head.checkSumAdjustment matches the font a decoder rebuilds.
	f := readSfnt(font.write(), 0)
	tags := f.sortedTags()
	compressed := make([][]byte, len(tags))
	sfntSize := int64(12 + 16*len(tags))
	for i, t := range tags {
		checkCtx(ctx)
		data := f.tables[t]
		packed := zlibCompress(data)
		if len(packed) < len(data) {
			compressed[i] = packed
		} else {
			compressed[i] = data
		}
		sfntSize += int64((len(data) + 3) &^ 3)
	}
	w := newWriter(woffHeaderSize + woffEntrySize*len(tags))
	w.u32(woffSignature)
	w.u32(f.flavor)
	w.u32(0) // length, patched below
	w.u16(len(tags))
	w.u16(0) // reserved
	w.u32(uint32(sfntSize))
	major, minor := fontVersion(f)
	w.u16(major)
	w.u16(minor)
	w.zeros(20) // metaOffset, metaLength, metaOrigLength, privOffset, privLength
	off := woffHeaderSize + woffEntrySize*len(tags)
	for i, t := range tags {
		data := f.tables[t]
		w.u32(tagValue(t))
		w.u32(uint32(off))
		w.u32(uint32(len(compressed[i])))
		w.u32(uint32(len(data)))
		w.u32(tableChecksum(t, data))
		off += (len(compressed[i]) + 3) &^ 3
	}
	for _, c := range compressed {
		w.bytes(c)
		w.pad4()
	}
	w.setU32(8, uint32(w.len()))
	return w.buf
}

// decodeWoff unpacks a WOFF file into its font. Extended metadata and private data are dropped.
func decodeWoff(data []byte) *sfnt {
	r := newReader(data)
	if r.u32() != woffSignature {
		fail("This is not a WOFF file.")
	}
	font := newSfnt(r.u32())
	switch font.flavor {
	case trueTypeFlavor, cffFlavor:
	case appleTrueTypeFlavor:
		font.flavor = trueTypeFlavor
	default:
		fail("The WOFF file contains an unknown kind of font.")
	}
	r.skip(4) // length
	count := int(r.u16())
	r.pos = woffHeaderSize
	for i := 0; i < count; i++ {
		tag := tagName(r.u32())
		off, compLength, origLength := r.u32(), r.u32(), r.u32()
		r.skip(4) // origChecksum
		if uint64(off) > uint64(len(data)) || uint64(compLength) > uint64(len(data))-uint64(off) || compLength > origLength {
			fail("The WOFF table '" + trimTag(tag) + "' is damaged.")
		}
		stored := data[off : off+compLength]
		if compLength == origLength {
			font.tables[tag] = append([]byte(nil), stored...)
		} else {
			font.tables[tag] = zlibDecompress(stored, int(origLength), tag)
		}
	}
	return font
}

func zlibCompress(data []byte) []byte {
	var b bytes.Buffer
	z, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
	z.Write(data)
	z.Close()
	return b.Bytes()
}

func zlibDecompress(data []byte, length int, tag string) []byte {
	// Deflate packs at most about 1032:1; more claims a damaged header (and a huge allocation).
	if int64(length) > int64(len(data))*1100+1024 {
		fail("The WOFF table '" + trimTag(tag) + "' is damaged.")
	}
	z, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		fail("The WOFF table '" + trimTag(tag) + "' can't be decompressed.")
	}
	out := make([]byte, length)
	if _, err := io.ReadFull(z, out); err != nil {
		fail("The WOFF table '" + trimTag(tag) + "' can't be decompressed.")
	}
	return out
}

// fontVersion is WOFF's informational version: head.fontRevision as
// major.minor (e.g. 2.015 → 2, 15).
func fontVersion(f *sfnt) (int, int) {
	head := f.table("head")
	if len(head) < 8 {
		return 0, 0
	}
	revision := float64(int32(be32(head, 4))) / 65536.0
	major := int(math.Floor(revision))
	minor := int(math.RoundToEven((revision - float64(major)) * 1000))
	return min(max(major, 0), 0xFFFF), min(max(minor, 0), 0xFFFF)
}
