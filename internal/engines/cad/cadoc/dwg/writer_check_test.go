package dwg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// This file holds a minimal, independent DWG parser used by the writer
// tests to check the structure of written files: file header, section maps,
// page checksums, section CRCs, the handle map and every object's CRC,
// type and handle.

// wtBits is a small MSB-first bit reader.
type wtBits struct {
	b   []byte
	pos int // in bits
}

func (r *wtBits) bit() bool {
	if r.pos/8 >= len(r.b) {
		panic("wtBits: read past end")
	}
	v := r.b[r.pos/8]>>(7-uint(r.pos%8))&1 != 0
	r.pos++
	return v
}

func (r *wtBits) bits(n int) uint64 {
	var v uint64
	for i := 0; i < n; i++ {
		v <<= 1
		if r.bit() {
			v |= 1
		}
	}
	return v
}

func (r *wtBits) byte_() byte { return byte(r.bits(8)) }

func (r *wtBits) rawShort() int16 {
	lo := r.byte_()
	hi := r.byte_()
	return int16(uint16(lo) | uint16(hi)<<8)
}

func (r *wtBits) rawLong() int32 {
	var b [4]byte
	for i := range b {
		b[i] = r.byte_()
	}
	return int32(binary.LittleEndian.Uint32(b[:]))
}

func (r *wtBits) rawDouble() float64 {
	var b [8]byte
	for i := range b {
		b[i] = r.byte_()
	}
	return wtFloat(b[:])
}

func (r *wtBits) bs() int16 {
	switch r.bits(2) {
	case 0:
		return r.rawShort()
	case 1:
		return int16(r.byte_())
	case 2:
		return 0
	}
	return 256
}

func (r *wtBits) bl() int32 {
	switch r.bits(2) {
	case 0:
		return r.rawLong()
	case 1:
		return int32(r.byte_())
	}
	return 0
}

func (r *wtBits) bd() float64 {
	switch r.bits(2) {
	case 0:
		return r.rawDouble()
	case 1:
		return 1
	}
	return 0
}

func (r *wtBits) bll() int64 {
	n := int(r.bits(3))
	var v uint64
	for i := 0; i < n; i++ {
		v |= uint64(r.byte_()) << (8 * i)
	}
	return int64(v)
}

func (r *wtBits) dd(def float64) float64 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], wtBitsOf(def))
	switch r.bits(2) {
	case 0:
		return def
	case 1:
		for i := 0; i < 4; i++ {
			b[i] = r.byte_()
		}
	case 2:
		b[4] = r.byte_()
		b[5] = r.byte_()
		for i := 0; i < 4; i++ {
			b[i] = r.byte_()
		}
	default:
		for i := range b {
			b[i] = r.byte_()
		}
	}
	return wtFloat(b[:])
}

func (r *wtBits) handle() (code byte, h uint64) {
	b := r.byte_()
	code = b >> 4
	for i := 0; i < int(b&0xF); i++ {
		h = h<<8 | uint64(r.byte_())
	}
	return code, h
}

func (r *wtBits) objectType(v cadoc.Version) int16 {
	if v < cadoc.AC1024 {
		return r.bs()
	}
	switch r.bits(2) {
	case 0:
		return int16(r.byte_())
	case 1:
		return int16(r.byte_()) + 0x1F0
	}
	return r.rawShort()
}

// wtDecompressAC18 is the R2004 LZ77 decompressor.
func wtDecompressAC18(src []byte) []byte {
	var dst []byte
	pos := 0
	rd := func() int {
		if pos >= len(src) {
			panic("lz77: input exhausted")
		}
		b := src[pos]
		pos++
		return int(b)
	}
	copyLit := func(n int) int {
		dst = append(dst, src[pos:pos+n]...)
		pos += n
		return rd()
	}
	literalCount := func(code int) int {
		low := code & 0x0F
		if low == 0 {
			b := rd()
			for ; b == 0; b = rd() {
				low += 0xFF
			}
			low += 0x0F + b
		}
		return low
	}
	compressedBytes := func(op, valid int) int {
		n := op & valid
		if n == 0 {
			b := rd()
			for ; b == 0; b = rd() {
				n += 0xFF
			}
			n += b + valid
		}
		return n + 2
	}
	op := rd()
	if op > 0x11 {
		op = copyLit(op - 17)
	}
	if op&0xF0 == 0 {
		op = copyLit(literalCount(op) + 3)
	}
	for op != 0x11 {
		var off, n int
		switch {
		case op < 0x10 || op >= 0x40:
			n = (op >> 4) - 1
			op2 := rd()
			off = (op>>2&3 | op2<<2) + 1
		case op < 0x20:
			n = compressedBytes(op, 0b0111)
			off = (op & 8) << 11
			first := rd()
			off |= first >> 2
			off |= rd() << 6
			off += 0x4000
			op = first
		default:
			n = compressedBytes(op, 0b11111)
			first := rd()
			off |= first >> 2
			off |= rd() << 6
			off++
			op = first
		}
		start := len(dst) - off
		if start < 0 {
			panic("lz77: offset before start")
		}
		for i := 0; i < n; i++ {
			dst = append(dst, dst[start+i])
		}
		lit := op & 3
		if lit == 0 {
			op = rd()
			if op&0xF0 == 0 {
				lit = literalCount(op) + 3
			}
		}
		if lit > 0 {
			op = copyLit(lit)
		}
	}
	return dst
}

type wtObject struct {
	offset int64
	typ    int16
	handle uint64
	size   int
}

type wtFile struct {
	version  cadoc.Version
	sections map[string][]byte
	handles  map[uint64]int64
	objects  map[uint64]wtObject
	// objectsData is what object offsets are relative to.
	objectsData []byte
}

func wtLE32(b []byte) int32 { return int32(binary.LittleEndian.Uint32(b)) }

// wtParse parses and checks a written DWG.
func wtParse(t *testing.T, data []byte) *wtFile {
	t.Helper()
	if len(data) < 0x100 {
		t.Fatalf("file too short: %d bytes", len(data))
	}
	v := cadoc.ParseVersion(string(data[:6]))
	f := &wtFile{version: v, sections: map[string][]byte{}}
	if v >= cadoc.AC1018 {
		wtParseAC18(t, f, data)
	} else {
		wtParseAC15(t, f, data)
	}
	required := []string{writerSecHeader, writerSecClasses, writerSecAuxHeader,
		writerSecAcDbObjects, writerSecObjFreeSpace, writerSecTemplate, writerSecHandles}
	if v >= cadoc.AC1018 {
		required = append(required, writerSecPreview, writerSecSummaryInfo, writerSecAppInfo, writerSecFileDepList, writerSecRevHistory)
	}
	for _, s := range required {
		if _, ok := f.sections[s]; !ok {
			t.Errorf("section %s missing", s)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	wtCheckHeaderSection(t, f)
	wtCheckClassesSection(t, f)
	f.handles = wtParseHandles(t, f.sections[writerSecHandles])
	wtCheckObjects(t, f)
	return f
}

func wtParseAC15(t *testing.T, f *wtFile, data []byte) {
	n := int(wtLE32(data[0x15:]))
	if n < 5 || n > 6 {
		t.Fatalf("AC15: %d section records", n)
	}
	crcAt := 0x19 + 9*n
	if got, want := binary.LittleEndian.Uint16(data[crcAt:]), wCRC8(0xC0C1, data[:crcAt]); got != want {
		t.Errorf("AC15 file header CRC %04X, want %04X", got, want)
	}
	if !bytes.Equal(data[crcAt+2:crcAt+18], writerAC15EndSentinel) {
		t.Errorf("AC15 end sentinel missing")
	}
	names := map[byte]string{0: writerSecHeader, 1: writerSecClasses, 2: writerSecHandles, 3: writerSecObjFreeSpace,
		4: writerSecTemplate, 5: writerSecAuxHeader}
	var classesEnd, handlesStart int
	for i := 0; i < n; i++ {
		r := data[0x19+9*i:]
		seeker, size := int(wtLE32(r[1:])), int(wtLE32(r[5:]))
		if seeker+size > len(data) {
			t.Fatalf("section %d out of the file", r[0])
		}
		f.sections[names[r[0]]] = data[seeker : seeker+size]
		switch r[0] {
		case 1:
			classesEnd = seeker + size
		case 2:
			handlesStart = seeker
		}
	}
	preview := int(wtLE32(data[0x0D:]))
	if !bytes.Equal(data[preview:preview+16], writerPreviewStartSentinel) {
		t.Errorf("preview sentinel not at the preview seeker")
	}
	f.sections[writerSecAcDbObjects] = data[classesEnd:handlesStart]
	// AC15 object offsets are absolute.
	f.objectsData = data
}

func wtParseAC18(t *testing.T, f *wtFile, data []byte) {
	hdr := make([]byte, 0x6C)
	copy(hdr, data[0x80:0xEC])
	for i := range hdr {
		hdr[i] ^= writerMagicSequence[i]
	}
	if string(hdr[:11]) != "AcFssFcAJMB" {
		t.Fatalf("AC18 file id %q", hdr[:11])
	}
	crc := binary.LittleEndian.Uint32(hdr[0x68:])
	zero := append([]byte(nil), hdr...)
	binary.LittleEndian.PutUint32(zero[0x68:], 0)
	if want := wCRC32(0, zero); crc != want {
		t.Errorf("AC18 header CRC32 %08X, want %08X", crc, want)
	}
	second := binary.LittleEndian.Uint64(hdr[0x34:])
	if int(second)+0x6C > len(data) {
		t.Fatalf("second header out of the file")
	}
	end := make([]byte, 0x6C)
	copy(end, data[second:])
	for i := range end {
		end[i] ^= writerMagicSequence[i]
	}
	if !bytes.Equal(end, hdr) {
		t.Errorf("second file header differs")
	}
	if !bytes.Equal(data[0xEC:0x100], writerMagicSequence[236:256]) {
		t.Errorf("magic bytes at 0xEC missing")
	}
	pageMapID := binary.LittleEndian.Uint32(hdr[0x50:])
	pageMapAddr := binary.LittleEndian.Uint64(hdr[0x54:]) + 0x100
	sectionMapID := binary.LittleEndian.Uint32(hdr[0x5C:])

	systemPage := func(at int, typ uint32) []byte {
		h := data[at:]
		if got := binary.LittleEndian.Uint32(h); got != typ {
			t.Fatalf("system page at %X: type %08X, want %08X", at, got, typ)
		}
		decomp := int(wtLE32(h[4:]))
		comp := int(wtLE32(h[8:]))
		sum := binary.LittleEndian.Uint32(h[16:])
		page := data[at+20 : at+20+comp]
		zh := append([]byte(nil), h[:20]...)
		binary.LittleEndian.PutUint32(zh[16:], 0)
		if want := wChecksum(wChecksum(0, zh), page); want != sum {
			t.Errorf("system page %X checksum %08X, want %08X", typ, sum, want)
		}
		out := wtDecompressAC18(page)
		if len(out) != decomp {
			t.Errorf("system page %X: %d bytes, want %d", typ, len(out), decomp)
		}
		return out
	}
	pm := systemPage(int(pageMapAddr), 0x41630E3B)
	pages := map[int32]int{}
	pos := 0x100
	for i := 0; i+8 <= len(pm); i += 8 {
		n, size := wtLE32(pm[i:]), int(wtLE32(pm[i+4:]))
		pages[n] = pos
		pos += size
	}
	if pages[int32(pageMapID)] != int(pageMapAddr) {
		t.Errorf("page map id %d at %X, header says %X", pageMapID, pages[int32(pageMapID)], pageMapAddr)
	}
	smPos, ok := pages[int32(sectionMapID)]
	if !ok {
		t.Fatalf("section map page %d missing", sectionMapID)
	}
	sm := systemPage(smPos, 0x4163003B)
	n := int(wtLE32(sm))
	p := 20
	for i := 0; i < n; i++ {
		d := sm[p:]
		size := int(binary.LittleEndian.Uint64(d))
		pageCount := int(wtLE32(d[8:]))
		maxDecomp := int(wtLE32(d[12:]))
		compressed := wtLE32(d[20:]) == 2
		name := string(bytes.TrimRight(d[32:96], "\x00"))
		p += 96
		buf := make([]byte, size)
		for j := 0; j < pageCount; j++ {
			e := sm[p:]
			num := wtLE32(e)
			compSize := int(wtLE32(e[4:]))
			offset := int(binary.LittleEndian.Uint64(e[8:]))
			p += 16
			at, ok := pages[num]
			if !ok {
				t.Fatalf("%s: page %d not in the page map", name, num)
			}
			h := append([]byte(nil), data[at:at+32]...)
			mask := uint32(0x4164536B ^ int32(at))
			for k := 0; k < 32; k += 4 {
				binary.LittleEndian.PutUint32(h[k:], binary.LittleEndian.Uint32(h[k:])^mask)
			}
			if binary.LittleEndian.Uint32(h) != writerDataPageType {
				t.Fatalf("%s page %d: type %08X", name, num, binary.LittleEndian.Uint32(h))
			}
			if int(wtLE32(h[8:])) != compSize || int(binary.LittleEndian.Uint64(h[16:])) != offset {
				t.Errorf("%s page %d: header and section map disagree", name, num)
			}
			page := data[at+32 : at+32+compSize]
			oda := binary.LittleEndian.Uint32(h[28:])
			if want := wChecksum(0, page); oda != want {
				t.Errorf("%s page %d: data checksum %08X, want %08X", name, num, oda, want)
			}
			sum := binary.LittleEndian.Uint32(h[24:])
			binary.LittleEndian.PutUint32(h[24:], 0)
			if want := wChecksum(oda, h); sum != want {
				t.Errorf("%s page %d: header checksum %08X, want %08X", name, num, sum, want)
			}
			var raw []byte
			if compressed {
				raw = wtDecompressAC18(page)
			} else {
				raw = page
			}
			if len(raw) != maxDecomp {
				t.Errorf("%s page %d: %d bytes, want %d", name, num, len(raw), maxDecomp)
			}
			copy(buf[offset:], raw)
		}
		f.sections[name] = buf
	}
	obj := f.sections[writerSecAcDbObjects]
	if wtLE32(obj) != 0xDCA {
		t.Errorf("objects section starts with %X", wtLE32(obj))
	}
	f.objectsData = obj
}

func wtCheckCRCSection(t *testing.T, name string, s, start, end []byte, extra bool) []byte {
	t.Helper()
	if !bytes.Equal(s[:16], start) {
		t.Errorf("%s: start sentinel missing", name)
		return nil
	}
	size := int(wtLE32(s[16:]))
	body := 20
	if extra {
		body += 4
	}
	crcAt := body + size
	if got, want := binary.LittleEndian.Uint16(s[crcAt:]), wCRC8(0xC0C1, s[16:crcAt]); got != want {
		t.Errorf("%s: CRC %04X, want %04X", name, got, want)
	}
	if !bytes.Equal(s[crcAt+2:crcAt+18], end) {
		t.Errorf("%s: end sentinel missing", name)
	}
	return s[body:crcAt]
}

func wtExtraSize(f *wtFile, maint int16) bool {
	return f.version >= cadoc.AC1024 && maint > 3 || f.version >= cadoc.AC1032
}

func wtCheckHeaderSection(t *testing.T, f *wtFile) {
	wtCheckCRCSection(t, "header", f.sections[writerSecHeader], writerHeaderStartSentinel, writerHeaderEndSentinel,
		wtExtraSize(f, f.version.MaintenanceVersion()))
}

func wtCheckClassesSection(t *testing.T, f *wtFile) {
	wtCheckCRCSection(t, "classes", f.sections[writerSecClasses], writerClassesStartSentinel, writerClassesEndSentinel,
		wtExtraSize(f, f.version.MaintenanceVersion()))
}

func wtParseHandles(t *testing.T, s []byte) map[uint64]int64 {
	out := map[uint64]int64{}
	p := 0
	for {
		size := int(binary.BigEndian.Uint16(s[p:]))
		if got, want := binary.BigEndian.Uint16(s[p+size:]), wCRC8(0xC0C1, s[p:p+size]); got != want {
			t.Errorf("handles page at %d: CRC %04X, want %04X", p, got, want)
		}
		if size == 2 {
			break
		}
		var handle uint64
		var loc int64
		q := p + 2
		for q < p+size {
			var dh uint64
			shift := 0
			for {
				b := s[q]
				q++
				dh |= uint64(b&0x7F) << shift
				shift += 7
				if b&0x80 == 0 {
					break
				}
			}
			var dl int64
			shift = 0
			for {
				b := s[q]
				q++
				if b&0x80 != 0 {
					dl |= int64(b&0x7F) << shift
					shift += 7
					continue
				}
				dl |= int64(b&0x3F) << shift
				if b&0x40 != 0 {
					dl = -dl
				}
				break
			}
			handle += dh
			loc += dl
			if _, dup := out[handle]; dup {
				t.Errorf("handle %X twice in the map", handle)
			}
			out[handle] = loc
		}
		p += size + 2
	}
	return out
}

// wtCheckObjects checks every object of the handle map: its size, CRC, type
// and handle.
func wtCheckObjects(t *testing.T, f *wtFile) {
	f.objects = map[uint64]wtObject{}
	for h, off := range f.handles {
		o, err := wtReadObject(f, off)
		if err != nil {
			t.Errorf("object %X at %d: %v", h, off, err)
			continue
		}
		if o.handle != h {
			t.Errorf("object at %d has handle %X, the map says %X", off, o.handle, h)
		}
		f.objects[h] = o
	}
}

func wtReadObject(f *wtFile, off int64) (o wtObject, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	d := f.objectsData
	p := int(off)
	w1 := int(binary.LittleEndian.Uint16(d[p:]))
	p += 2
	size := w1 & 0x7FFF
	if w1&0x8000 != 0 {
		size |= int(binary.LittleEndian.Uint16(d[p:])) << 15
		p += 2
	}
	var handleBits int
	if f.version >= cadoc.AC1024 {
		shift := 0
		for {
			b := d[p]
			p++
			handleBits |= int(b&0x7F) << shift
			shift += 7
			if b&0x80 == 0 {
				break
			}
		}
	}
	data := d[p : p+size]
	if got, want := binary.LittleEndian.Uint16(d[p+size:]), wCRC8(0xC0C1, d[off:p+size]); got != want {
		return o, fmt.Errorf("CRC %04X, want %04X", got, want)
	}
	r := &wtBits{b: data}
	o.offset, o.size = off, size
	o.typ = r.objectType(f.version)
	handleStart := -1
	if f.version >= cadoc.AC1015 && f.version < cadoc.AC1024 {
		bits := r.rawLong()
		if int(bits) > size*8 || bits <= 0 {
			return o, fmt.Errorf("size in bits %d for %d bytes", bits, size)
		}
		handleStart = int(bits)
	}
	if f.version >= cadoc.AC1024 {
		handleStart = size*8 - handleBits
		if handleStart <= 0 {
			return o, fmt.Errorf("handle stream of %d bits for %d bytes", handleBits, size)
		}
		// R2007+: the string stream flag is the last bit before the handles.
		flag := &wtBits{b: data, pos: handleStart - 1}
		if flag.bit() {
			sz := &wtBits{b: data, pos: handleStart - 17}
			textBits := int(uint16(sz.rawShort()))
			if textBits&0x8000 != 0 {
				hi := &wtBits{b: data, pos: handleStart - 33}
				textBits = textBits&0x7FFF | int(uint16(hi.rawShort())&0x7FFF)<<15
			}
			if textBits > handleStart-17 {
				return o, fmt.Errorf("string stream size %d", textBits)
			}
		}
	}
	code, h := r.handle()
	if code != 0 {
		return o, fmt.Errorf("handle code %d", code)
	}
	o.handle = h
	if handleStart > 0 {
		// The handle stream must be a run of well formed references that
		// ends in the padding of the last byte.
		hr := &wtBits{b: data, pos: handleStart}
		for size*8-hr.pos >= 8 {
			b := hr.b[hr.pos/8]
			if hr.pos%8 != 0 {
				b = byte(hr.bits(8))
				hr.pos -= 8
			}
			n := int(b & 0xF)
			if b>>4 > 0xC || n > 8 || hr.pos+8*(n+1) > size*8 {
				return o, fmt.Errorf("malformed handle reference %02X at bit %d of %d", b, hr.pos, size*8)
			}
			hr.pos += 8 * (n + 1)
		}
	}
	return o, nil
}

func wtFloat(b []byte) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(b)) }

func wtBitsOf(f float64) uint64 { return math.Float64bits(f) }
