package ebook

import (
	"encoding/binary"
	"errors"
	"math/bits"
	"strings"
)

// The Palm database (PDB) container of MOBI, AZW, AZW3, AZW4 and PRC books:
// a 78-byte header with the type and creator ("BOOKMOBI", "TEXtREAd"), then
// a table of record offsets. All numbers are big-endian.

type palmDatabase struct {
	data        []byte
	offsets     []int
	name        string
	typeCreator string
}

func openPalm(data []byte) (*palmDatabase, error) {
	if len(data) < 78 {
		return nil, errors.New("the file is too short to be an e-book")
	}
	count := int(binary.BigEndian.Uint16(data[76:]))
	if count == 0 || 78+count*8 > len(data) {
		return nil, errors.New("the e-book's record table is damaged")
	}
	offsets := make([]int, count)
	for i := range offsets {
		off := binary.BigEndian.Uint32(data[78+i*8:])
		if int64(off) > int64(len(data)) || i > 0 && int(off) < offsets[i-1] {
			return nil, errors.New("the e-book's record table is damaged")
		}
		offsets[i] = int(off)
	}
	name := strings.TrimRight(latin1(data[:32]), "\x00")
	if i := strings.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return &palmDatabase{data, offsets, name, string(data[60:68])}, nil
}

func (p *palmDatabase) count() int { return len(p.offsets) }

// record is record i (empty when out of range).
func (p *palmDatabase) record(i int) []byte {
	if i < 0 || i >= len(p.offsets) {
		return nil
	}
	end := len(p.data)
	if i+1 < len(p.offsets) {
		end = p.offsets[i+1]
	}
	return p.data[p.offsets[i]:max(p.offsets[i], end)]
}

// u16 and u32 are big-endian fields with bounds checks: damaged books fail
// with a clear error, not a panic deep inside. u32 maps 0xFFFFFFFF ("none") to -1.
func u16(d []byte, off int) int {
	if off < 0 || off+2 > len(d) {
		return 0
	}
	return int(binary.BigEndian.Uint16(d[off:]))
}

func u32(d []byte, off int) int64 {
	if off < 0 || off+4 > len(d) {
		return -1
	}
	v := binary.BigEndian.Uint32(d[off:])
	if v == 0xFFFFFFFF {
		return -1
	}
	return int64(v)
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// palmDocDecompress decompresses one PalmDOC (LZ77) text record.
func palmDocDecompress(in []byte) []byte {
	out := make([]byte, 0, len(in)*2)
	for i := 0; i < len(in); {
		c := in[i]
		i++
		switch {
		case c >= 1 && c <= 8:
			// 1..8: that many literal bytes follow.
			for n := 0; n < int(c) && i < len(in); n++ {
				out = append(out, in[i])
				i++
			}
		case c < 0x80:
			out = append(out, c) // 0 and 9..0x7F: the byte itself
		case c >= 0xC0:
			out = append(out, ' ', c^0x80) // a space followed by an ASCII character
		case i < len(in):
			// 0x80..0xBF: two bytes = 11-bit distance back and a length of 3..10.
			pair := int(c)<<8 | int(in[i])
			i++
			distance := (pair >> 3) & 0x7FF
			length := pair&7 + 3
			if distance == 0 || distance > len(out) {
				continue // damaged: skip rather than fail the whole book
			}
			start := len(out) - distance
			for n := 0; n < length; n++ {
				out = append(out, out[start+n])
			}
		}
	}
	return out
}

// ---- HUFF/CDIC ----

// huffCdic decodes Mobipocket's "Huffman dictionary" compression: a
// canonical Huffman code (HUFF record) selects phrases from the dictionary
// (CDIC records); a phrase may itself be compressed and is unpacked on first use.
type huffCdic struct {
	table1 [256]struct {
		length   int
		terminal bool
		maxCode  uint64
	}
	minCode, maxCode [33]uint64
	phrases          []*huffPhrase
}

type huffPhrase struct {
	data           []byte
	unpacked, busy bool
}

var errHuff = errors.New("the book's HUFF compression table is damaged")

func newHuffCdic(huff []byte, cdics [][]byte) (*huffCdic, error) {
	if len(huff) < 24 || string(huff[:4]) != "HUFF" {
		return nil, errHuff
	}
	t1, t2 := int(binary.BigEndian.Uint32(huff[8:])), int(binary.BigEndian.Uint32(huff[12:]))
	if t1+256*4 > len(huff) || t2+64*4 > len(huff) {
		return nil, errHuff
	}
	h := &huffCdic{}
	for i := 0; i < 256; i++ {
		v := binary.BigEndian.Uint32(huff[t1+i*4:])
		length := int(v & 0x1F)
		if length == 0 {
			return nil, errHuff
		}
		h.table1[i].length = length
		h.table1[i].terminal = v&0x80 != 0
		h.table1[i].maxCode = ((uint64(v>>8) + 1) << (32 - length)) - 1
	}
	for length := 1; length <= 32; length++ {
		mn := binary.BigEndian.Uint32(huff[t2+(length-1)*8:])
		mx := binary.BigEndian.Uint32(huff[t2+(length-1)*8+4:])
		h.minCode[length] = uint64(mn) << (32 - length)
		h.maxCode[length] = ((uint64(mx) + 1) << (32 - length)) - 1
	}
	for _, c := range cdics {
		if len(c) < 16 || string(c[:4]) != "CDIC" {
			return nil, errors.New("the book's CDIC dictionary is damaged")
		}
		total := int(binary.BigEndian.Uint32(c[8:]))
		b := int(binary.BigEndian.Uint32(c[12:]))
		count := min(1<<max(0, min(20, b)), total-len(h.phrases))
		for i := 0; i < count; i++ {
			off := 16 + u16(c, 16+i*2)
			header := u16(c, off)
			length := min(header&0x7FFF, max(0, len(c)-off-2))
			if off+2 > len(c) {
				length = 0
			}
			data := append([]byte(nil), c[min(len(c), off+2):min(len(c), off+2+length)]...)
			h.phrases = append(h.phrases, &huffPhrase{data: data, unpacked: header&0x8000 != 0})
		}
	}
	return h, nil
}

func (h *huffCdic) decompress(data []byte) ([]byte, error) {
	var out []byte
	err := h.unpack(data, &out, 0)
	return out, err
}

func (h *huffCdic) unpack(data []byte, out *[]byte, depth int) error {
	if depth > 32 {
		return errors.New("the book's CDIC dictionary refers to itself")
	}
	padded := make([]byte, len(data)+8)
	copy(padded, data)
	bitsLeft := int64(len(data)) * 8
	pos := 0
	window := binary.BigEndian.Uint64(padded)
	n := 32
	for {
		if n <= 0 {
			pos += 4
			if pos+8 <= len(padded) {
				window = binary.BigEndian.Uint64(padded[pos:])
			} else {
				window = 0
			}
			n += 32
		}
		code := (window >> n) & 0xFFFFFFFF
		e := h.table1[code>>24]
		length, maxCode := e.length, e.maxCode
		if !e.terminal {
			for length < 32 && code < h.minCode[length] {
				length++
			}
			maxCode = h.maxCode[length]
		}
		n -= length
		bitsLeft -= int64(length)
		if bitsLeft < 0 {
			return nil
		}
		index := int((maxCode - code) >> (32 - length))
		if index < 0 || index >= len(h.phrases) {
			return errors.New("the book's compressed text is damaged")
		}
		p := h.phrases[index]
		if !p.unpacked {
			if p.busy {
				return errors.New("the book's CDIC dictionary refers to itself")
			}
			p.busy = true
			var u []byte
			if err := h.unpack(p.data, &u, depth+1); err != nil {
				return err
			}
			p.data, p.unpacked, p.busy = u, true, false
		}
		*out = append(*out, p.data...)
	}
}

// ---- INDX ----

// indexEntry is one entry of a Kindle index: its key and tag values.
type indexEntry struct {
	key  string
	tags map[int][]int64
}

func (e indexEntry) tag(tag, position int) int64 {
	if v, ok := e.tags[tag]; ok && position < len(v) {
		return v[position]
	}
	return -1
}

type tagDefinition struct {
	tag, valuesPerEntry, mask int
	endFlag                   bool
}

// readIndex reads the index starting at record first (KF8 skeleton and
// fragment tables): a primary INDX record with the tag table (TAGX), then
// INDX records whose entries are a key followed by control bytes and
// variable-width tag values; strings live in CNCX records.
func readIndex(db *palmDatabase, first int, strs map[int64]string) ([]indexEntry, error) {
	primary := db.record(first)
	if len(primary) < 56 || string(primary[:4]) != "INDX" {
		return nil, errors.New("the book's index is damaged")
	}
	headerLength := int(u32(primary, 4))
	recordCount := int(u32(primary, 24))
	cncxCount := int(max(0, u32(primary, 52)))
	controlBytes, tags, err := tagTable(primary, headerLength)
	if err != nil {
		return nil, err
	}
	// CNCX records follow the index records; tag values address them as record * 0x10000 + offset.
	for c := 0; c < cncxCount; c++ {
		cncx := db.record(first + recordCount + 1 + c)
		off := 0
		for off < len(cncx) && cncx[off] != 0 {
			start := off
			length, used := forwardVarint(cncx, off)
			off += used
			length = min(length, int64(len(cncx)-off))
			strs[int64(c)*0x10000+int64(start)] = string(cncx[off : off+int(length)])
			off += int(length)
		}
	}
	var entries []indexEntry
	for r := 1; r <= recordCount; r++ {
		rec := db.record(first + r)
		if len(rec) < 28 || string(rec[:4]) != "INDX" {
			continue
		}
		idxt := int(u32(rec, 20))
		count := int(u32(rec, 24))
		if idxt < 0 || count < 0 || idxt+4+count*2 > len(rec) {
			continue
		}
		offsets := make([]int, count+1)
		for i := 0; i < count; i++ {
			offsets[i] = u16(rec, idxt+4+i*2)
		}
		offsets[count] = idxt
		for i := 0; i < count; i++ {
			start := offsets[i]
			if start >= len(rec) {
				continue
			}
			keyLen := int(rec[start])
			key := latin1(rec[start+1 : min(len(rec), start+1+keyLen)])
			values := tagValues(rec, start+1+keyLen, offsets[i+1], controlBytes, tags)
			entries = append(entries, indexEntry{key, values})
		}
	}
	return entries, nil
}

func tagTable(rec []byte, start int) (int, []tagDefinition, error) {
	if start < 0 || start+12 > len(rec) || string(rec[start:start+4]) != "TAGX" {
		return 0, nil, errors.New("the book's index has no tag table")
	}
	length := int(u32(rec, start+4))
	control := int(u32(rec, start+8))
	var tags []tagDefinition
	for i := 12; i+4 <= length && start+i+4 <= len(rec); i += 4 {
		p := start + i
		tags = append(tags, tagDefinition{int(rec[p]), int(rec[p+1]), int(rec[p+2]), rec[p+3] == 1})
	}
	return control, tags, nil
}

// tagValues are the tag values of one entry. Each tag's mask selects bits of
// a control byte: a partial value is the number of value groups; all bits set
// with a multi-bit mask means a byte count follows; a one-bit mask means one group.
func tagValues(rec []byte, start, end, controlBytes int, tags []tagDefinition) map[int][]int64 {
	result := map[int][]int64{}
	ci := 0
	pos := start + controlBytes
	type header struct{ tag, count, bytes, perEntry int }
	var headers []header
	for _, t := range tags {
		if t.endFlag {
			ci++
			continue
		}
		if start+ci >= len(rec) || start+ci < 0 {
			break
		}
		v := int(rec[start+ci]) & t.mask
		if v == 0 {
			continue
		}
		if v == t.mask {
			if bits.OnesCount(uint(t.mask)) > 1 {
				b, used := forwardVarint(rec, pos)
				pos += used
				headers = append(headers, header{t.tag, -1, int(b), t.valuesPerEntry})
			} else {
				headers = append(headers, header{t.tag, 1, 0, t.valuesPerEntry})
			}
			continue
		}
		mask := t.mask
		for mask&1 == 0 {
			mask >>= 1
			v >>= 1
		}
		headers = append(headers, header{t.tag, v, 0, t.valuesPerEntry})
	}
	for _, h := range headers {
		var values []int64
		if h.count >= 0 {
			for i := 0; i < h.count*h.perEntry && pos < end; i++ {
				v, used := forwardVarint(rec, pos)
				pos += used
				values = append(values, v)
			}
		} else {
			read := 0
			for read < h.bytes && pos < end {
				v, used := forwardVarint(rec, pos)
				pos += used
				read += used
				values = append(values, v)
			}
		}
		result[h.tag] = values
	}
	return result
}

// forwardVarint is a forward variable-width integer: 7 bits per byte, the last byte has the high bit set.
func forwardVarint(d []byte, off int) (int64, int) {
	var v int64
	used := 0
	for off+used < len(d) && used < 9 && off >= 0 {
		b := d[off+used]
		used++
		v = v<<7 | int64(b&0x7F)
		if b&0x80 != 0 {
			break
		}
	}
	return v, max(1, used)
}
