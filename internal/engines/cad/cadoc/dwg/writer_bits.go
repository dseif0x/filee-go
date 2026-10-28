package dwg

import (
	"encoding/binary"
	"math"
	"time"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// wMemStream behaves like System.IO.MemoryStream: a growable buffer with a
// position; writes overwrite or extend, reads past the end return -1. The
// bit writers seek back into it to patch sizes, so it has to keep these
// semantics exactly.
type wMemStream struct {
	buf []byte
	pos int
}

func (m *wMemStream) writeByte(b byte) {
	if m.pos < len(m.buf) {
		m.buf[m.pos] = b
	} else {
		for len(m.buf) < m.pos {
			m.buf = append(m.buf, 0)
		}
		m.buf = append(m.buf, b)
	}
	m.pos++
}

func (m *wMemStream) write(p []byte) {
	if m.pos == len(m.buf) {
		m.buf = append(m.buf, p...)
		m.pos += len(p)
		return
	}
	for _, b := range p {
		m.writeByte(b)
	}
}

func (m *wMemStream) readByte() int {
	if m.pos >= len(m.buf) {
		return -1
	}
	b := m.buf[m.pos]
	m.pos++
	return int(b)
}

func (m *wMemStream) length() int { return len(m.buf) }

func (m *wMemStream) bytes() []byte { return m.buf }

func (m *wMemStream) reset() {
	m.buf = m.buf[:0]
	m.pos = 0
}

func (m *wMemStream) writeInt32(v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	m.write(b[:])
}

func (m *wMemStream) writeUint32(v uint32) { m.writeInt32(int32(v)) }

func (m *wMemStream) writeInt16(v int16) {
	m.writeByte(byte(v))
	m.writeByte(byte(uint16(v) >> 8))
}

func (m *wMemStream) writeInt64(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	m.write(b[:])
}

func (m *wMemStream) writeFloat64(v float64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	m.write(b[:])
}

// wRefType is DwgReferenceType: the code in the high nibble of a handle
// reference.
type wRefType byte

const (
	wRefUndefined     wRefType = 0
	wRefSoftOwnership wRefType = 2
	wRefHardOwnership wRefType = 3
	wRefSoftPointer   wRefType = 4
	wRefHardPointer   wRefType = 5
)

// wWriter is IDwgStreamWriter: the bit-level writer the section writers use,
// either a plain stream (wBits) or the merged main/text/handle streams of an
// object (wMerged).
type wWriter interface {
	main() *wBits
	writeBit(v bool)
	write2Bits(v byte)
	writeBitShort(v int16)
	writeBitLong(v int32)
	writeBitLongLong(v int64)
	writeBitDouble(v float64)
	writeBitDoubleWithDefault(value, def float64)
	write2BitDoubleWithDefault(value, def cadoc.Vec2)
	write3BitDoubleWithDefault(value, def cadoc.Vec3)
	write2BitDouble(v cadoc.Vec2)
	write3BitDouble(v cadoc.Vec3)
	write2RawDouble(v cadoc.Vec2)
	writeRawDouble(v float64)
	writeRawShort(v int16)
	writeRawLong(v int64)
	writeInt(v int32)
	writeByte(v byte)
	writeBytes(p []byte)
	writeVariableText(s string)
	writeTextUnicode(s string)
	writeCmColor(c cadoc.Color, useTextStream bool)
	writeEnColor(c cadoc.Color, t cadoc.Transparency, isBookColor bool)
	writeDateTime(t time.Time)
	write8BitJulianDate(t time.Time)
	writeTimeSpan(d time.Duration)
	writeObjectType(v int16)
	handleReference(t wRefType, handle uint64)
	writeSpearShift()
	writeBitThickness(v float64)
	writeBitExtrusion(v cadoc.Vec3)
	resetStream()
	savePositionForSize()
	savedPositionInBits() int64
	encoder() *wTextEncoder
}

// wBits is DwgStreamWriterBase with its version subclasses (AC12, AC15,
// AC18, AC21, AC24), selected by version.
type wBits struct {
	ms       *wMemStream
	shift    int
	last     byte
	version  cadoc.Version
	enc      *wTextEncoder
	unicode  bool // AC21+: TV and TU strings are UTF-16
	ac15Bits bool // AC15+: BT thickness and BE extrusion
	cmc      bool // AC18+: CMC colours
	ac24Type bool // AC24+: object types as bit pair + byte(s)
}

func newWBits(version cadoc.Version, ms *wMemStream, enc *wTextEncoder) *wBits {
	if ms == nil {
		ms = &wMemStream{}
	}
	return &wBits{ms: ms, version: version, enc: enc,
		unicode:  version >= cadoc.AC1021,
		ac15Bits: version >= cadoc.AC1015,
		cmc:      version >= cadoc.AC1018,
		ac24Type: version >= cadoc.AC1024}
}

func (w *wBits) main() *wBits                 { return w }
func (w *wBits) encoder() *wTextEncoder       { return w.enc }
func (w *wBits) positionInBits() int64        { return int64(w.ms.pos)*8 + int64(w.shift) }
func (w *wBits) savedPositionInBits() int64   { return 0 }
func (w *wBits) savePositionForSize()         { w.writeRawLong(0) }
func (w *wBits) writeInt(v int32)             { w.writeRawLong(int64(v)) }
func (w *wBits) writeRawDouble(v float64)     { w.writeRawBits64(math.Float64bits(v)) }
func (w *wBits) write2RawDouble(v cadoc.Vec2) { w.writeRawDouble(v.X); w.writeRawDouble(v.Y) }

func (w *wBits) writeRawBits64(u uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], u)
	w.writeBytes(b[:])
}

func (w *wBits) writeRawLong(v int64) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	w.writeBytes(b[:])
}

func (w *wBits) writeRawShort(v int16) {
	w.writeBytes([]byte{byte(v), byte(uint16(v) >> 8)})
}

func (w *wBits) writeObjectType(v int16) {
	if !w.ac24Type {
		w.writeBitShort(v)
		return
	}
	switch {
	case v <= 255:
		w.write2Bits(0)
		w.writeByte(byte(v))
	case v >= 0x1F0 && v <= 0x2EF:
		w.write2Bits(1)
		w.writeByte(byte(v - 0x1F0))
	default:
		w.write2Bits(2)
		w.writeBytes([]byte{byte(v), byte(uint16(v) >> 8)})
	}
}

func (w *wBits) writeBytes(p []byte) {
	if w.shift == 0 {
		w.ms.write(p)
		return
	}
	num := 8 - w.shift
	for _, b := range p {
		w.ms.writeByte(w.last | b>>w.shift)
		w.last = b << num
	}
}

func (w *wBits) writeBitShort(v int16) {
	switch {
	case v == 0:
		w.write2Bits(2)
	case v > 0 && v < 256:
		w.write2Bits(1)
		w.writeByte(byte(v))
	case v == 256:
		w.write2Bits(3)
	default:
		w.write2Bits(0)
		w.writeByte(byte(v))
		w.writeByte(byte(v >> 8))
	}
}

func (w *wBits) writeBitDouble(v float64) {
	switch v {
	case 0:
		w.write2Bits(2)
	case 1:
		w.write2Bits(1)
	default:
		w.write2Bits(0)
		w.writeRawDouble(v)
	}
}

func (w *wBits) writeBitLong(v int32) {
	switch {
	case v == 0:
		w.write2Bits(2)
	case v > 0 && v < 256:
		w.write2Bits(1)
		w.writeByte(byte(v))
	default:
		w.write2Bits(0)
		w.writeByte(byte(v))
		w.writeByte(byte(v >> 8))
		w.writeByte(byte(v >> 16))
		w.writeByte(byte(v >> 24))
	}
}

func (w *wBits) writeBitLongLong(v int64) {
	u := uint64(v)
	var size byte
	for hold := u; hold != 0; hold >>= 8 {
		size++
	}
	// Like ACadSharp: the 3-bit length can't hold 8 (a full 64-bit value).
	w.writeBit(size&4 != 0)
	w.writeBit(size&2 != 0)
	w.writeBit(size&1 != 0)
	hold := u
	for i := 0; i < int(size); i++ {
		w.writeByte(byte(hold))
		hold >>= 8
	}
}

func (w *wBits) writeVariableText(s string) {
	if s == "" {
		w.writeBitShort(0)
		return
	}
	if w.unicode {
		u := utf16.Encode([]rune(s))
		w.writeBitShort(int16(len(u)))
		w.writeBytes(wUTF16Bytes(u))
		return
	}
	b := w.enc.encode(s)
	w.writeBitShort(int16(len(b)))
	w.writeBytes(b)
}

// writeTextUnicode is TU: a raw short length and the text with its
// terminator, byte aligned.
func (w *wBits) writeTextUnicode(s string) {
	if w.unicode {
		u := utf16.Encode([]rune(s))
		w.writeRawShort(int16(len(u) + 1))
		w.writeBytes(wUTF16Bytes(u))
		w.ms.writeByte(0)
		w.ms.writeByte(0)
		return
	}
	b := w.enc.encode(s)
	w.writeRawShort(int16(len(b) + 1))
	w.ms.write(b)
	w.ms.writeByte(0)
}

func (w *wBits) write2Bits(v byte) {
	switch {
	case w.shift < 6:
		w.last |= v << (6 - w.shift)
		w.shift += 2
	case w.shift == 6:
		w.last |= v
		w.ms.writeByte(w.last)
		w.resetShift()
	default:
		w.last |= v >> 1
		w.ms.writeByte(w.last)
		w.last = v << 7
		w.shift = 1
	}
}

func (w *wBits) writeBit(v bool) {
	if w.shift < 7 {
		if v {
			w.last |= 1 << (7 - w.shift)
		}
		w.shift++
		return
	}
	if v {
		w.last |= 1
	}
	w.ms.writeByte(w.last)
	w.resetShift()
}

func (w *wBits) writeByte(v byte) {
	if w.shift == 0 {
		w.ms.writeByte(v)
		return
	}
	w.ms.writeByte(w.last | v>>w.shift)
	w.last = v << (8 - w.shift)
}

func (w *wBits) resetShift() {
	w.shift = 0
	w.last = 0
}

func (w *wBits) writeDateTime(t time.Time) {
	jd, ms := wDateToJulian(t)
	w.writeBitLong(jd)
	w.writeBitLong(ms)
}

// writeTimeSpan writes days and milliseconds into the day, as the DWG spec
// describes TDINDWG (ACadSharp writes TimeSpan.Days and the millisecond
// component only, which loses the hours).
func (w *wBits) writeTimeSpan(d time.Duration) {
	if d < 0 {
		d = 0
	}
	days := d / (24 * time.Hour)
	w.writeBitLong(int32(days))
	w.writeBitLong(int32((d - days*24*time.Hour) / time.Millisecond))
}

func (w *wBits) write8BitJulianDate(t time.Time) {
	jd, ms := wDateToJulian(t)
	w.writeRawLong(int64(jd))
	w.writeRawLong(int64(ms))
}

func (w *wBits) writeCmColor(c cadoc.Color, _ bool) {
	if !w.cmc {
		// R15 and earlier: BS colour index.
		w.writeBitShort(c.ApproxIndex())
		return
	}
	wWriteCMC(w, c)
}

// wWriteCMC writes the R2004+ CMC colour: BS 0, BL RGB with the colour
// method in the high byte, RC 0 (no colour or book name).
func wWriteCMC(w wWriter, c cadoc.Color) {
	w.writeBitShort(0)
	var arr [4]byte
	switch {
	case c.IsTrueColor():
		arr[2], arr[1], arr[0] = c.R(), c.G(), c.B()
		arr[3] = 0b1100_0010
	case c.IsByLayer():
		arr[3] = 0b1100_0000
	default:
		arr[3] = 0b1100_0011
		arr[0] = byte(c.Index())
	}
	w.writeBitLong(int32(binary.LittleEndian.Uint32(arr[:])))
	w.writeByte(0)
}

func (w *wBits) writeEnColor(c cadoc.Color, t cadoc.Transparency, isBookColor bool) {
	if !w.cmc {
		w.writeCmColor(c, false)
		return
	}
	if c.IsByBlock() && t.IsByLayer() && !isBookColor {
		w.writeBitShort(0)
		return
	}
	var size uint16
	// 0x2000: a transparency BL follows.
	if !t.IsByLayer() {
		size |= 0x2000
	}
	switch {
	case isBookColor:
		// 0x4000: AcDbColor reference (0x8000 is set too). AutoCAD stores the
		// nearest index and no RGB value: the colour comes from the DBCOLOR
		// object (ACadSharp writes the RGB value, which its reader doesn't
		// expect).
		size |= 0x4000 | 0x8000 | uint16(c.ApproxIndex())&0xFF
	case c.IsTrueColor():
		size |= 0x8000
	default:
		size |= uint16(c.Index())
	}
	w.writeBitShort(int16(size))
	if c.IsTrueColor() && !isBookColor {
		w.writeBitLong(int32(binary.LittleEndian.Uint32([]byte{c.B(), c.G(), c.R(), 0b1100_0010})))
	}
	if !t.IsByLayer() {
		w.writeBitLong(t.AlphaValue())
	}
}

func (w *wBits) write2BitDouble(v cadoc.Vec2) { w.writeBitDouble(v.X); w.writeBitDouble(v.Y) }

func (w *wBits) write3BitDouble(v cadoc.Vec3) {
	w.writeBitDouble(v.X)
	w.writeBitDouble(v.Y)
	w.writeBitDouble(v.Z)
}

func (w *wBits) handleReference(t wRefType, handle uint64) {
	b := byte(t) << 4
	n := 0
	for h := handle; h != 0; h >>= 8 {
		n++
	}
	w.writeByte(b | byte(n))
	for i := n - 1; i >= 0; i-- {
		w.writeByte(byte(handle >> (8 * i)))
	}
}

func (w *wBits) writeSpearShift() {
	if w.shift > 0 {
		for i := w.shift; i < 8; i++ {
			w.writeBit(false)
		}
	}
}

func (w *wBits) writeBitThickness(v float64) {
	if !w.ac15Bits {
		// R13-R14: BD.
		w.writeBitDouble(v)
		return
	}
	// R2000+: a bit (1 = 0.0) optionally followed by a BD.
	if v == 0 {
		w.writeBit(true)
		return
	}
	w.writeBit(false)
	w.writeBitDouble(v)
}

func (w *wBits) writeBitExtrusion(v cadoc.Vec3) {
	if !w.ac15Bits {
		w.write3BitDouble(v)
		return
	}
	// R2000+: a bit (1 = 0,0,1) optionally followed by 3BD.
	if v == cadoc.AxisZ {
		w.writeBit(true)
		return
	}
	w.writeBit(false)
	w.write3BitDouble(v)
}

func (w *wBits) write2BitDoubleWithDefault(value, def cadoc.Vec2) {
	w.writeBitDoubleWithDefault(value.X, def.X)
	w.writeBitDoubleWithDefault(value.Y, def.Y)
}

func (w *wBits) write3BitDoubleWithDefault(value, def cadoc.Vec3) {
	w.writeBitDoubleWithDefault(value.X, def.X)
	w.writeBitDoubleWithDefault(value.Y, def.Y)
	w.writeBitDoubleWithDefault(value.Z, def.Z)
}

// writeBitDoubleWithDefault is DD: the value patched into the default
// double's bytes. ACadSharp names the parameters the other way round, but
// every caller passes (value, default), as here.
func (w *wBits) writeBitDoubleWithDefault(value, def float64) {
	if value == def {
		w.write2Bits(0)
		return
	}
	var vb, db [8]byte
	binary.LittleEndian.PutUint64(vb[:], math.Float64bits(value))
	binary.LittleEndian.PutUint64(db[:], math.Float64bits(def))
	first := 0
	for last := 7; last >= 0 && vb[last] == db[last]; last-- {
		first++
	}
	switch {
	case first >= 4:
		w.write2Bits(1)
		w.writeBytes(vb[:4])
	case first >= 2:
		w.write2Bits(2)
		w.writeBytes([]byte{vb[4], vb[5], vb[0], vb[1], vb[2], vb[3]})
	default:
		w.write2Bits(3)
		w.writeBytes(vb[:])
	}
}

func (w *wBits) resetStream() {
	w.ms.reset()
	w.resetShift()
}

// setPositionByFlag writes the string stream size (RS, with 0x8000 marking
// a second word) as the merged writer stores it.
func (w *wBits) setPositionByFlag(pos int64) {
	put := func(v uint16) { w.writeBytes([]byte{byte(v), byte(v >> 8)}) }
	if pos >= 0x8000 {
		if pos >= 0x40000000 {
			put(uint16((pos >> 30) & 0xFFFF))
			put(uint16(((pos >> 15) & 0x7FFF) | 0x8000))
		} else {
			put(uint16((pos >> 15) & 0xFFFF))
		}
		put(uint16((pos & 0x7FFF) | 0x8000))
		return
	}
	put(uint16(pos))
}

func (w *wBits) setPositionInBits(posInBits int64) {
	position := int(posInBits / 8)
	w.shift = int(posInBits % 8)
	w.ms.pos = position
	if w.shift > 0 {
		v := w.ms.readByte()
		if v < 0 {
			panic("dwg: bit position past the end of the stream")
		}
		w.last = byte(v)
	} else {
		w.last = 0
	}
	w.ms.pos = position
}

// writeShiftValue flushes the pending partial byte, keeping the bits of the
// byte already in the stream that follow it.
func (w *wBits) writeShiftValue() {
	if w.shift == 0 {
		return
	}
	position := w.ms.pos
	v := w.ms.readByte()
	cur := w.last | byte(v)&(0xFF>>w.shift)
	w.ms.pos = position
	w.ms.writeByte(cur)
}

// wMerged is DwgMergedStreamWriter (R2007+: main, string and handle streams)
// and DwgMergedStreamWriterAC14 (up to R2004: strings in the main stream).
type wMerged struct {
	m, t, h      *wBits
	ac14         bool
	saved        bool
	sizePosition int64
	savedPos     int64
}

func newWMerged(version cadoc.Version, enc *wTextEncoder) *wMerged {
	m := newWBits(version, &wMemStream{}, enc)
	mw := &wMerged{m: m, h: newWBits(version, &wMemStream{}, enc)}
	if version <= cadoc.AC1018 {
		mw.ac14 = true
		mw.t = m
	} else {
		mw.t = newWBits(version, &wMemStream{}, enc)
	}
	return mw
}

func (w *wMerged) main() *wBits                              { return w.m }
func (w *wMerged) encoder() *wTextEncoder                    { return w.m.enc }
func (w *wMerged) savedPositionInBits() int64                { return w.savedPos }
func (w *wMerged) writeBit(v bool)                           { w.m.writeBit(v) }
func (w *wMerged) write2Bits(v byte)                         { w.m.write2Bits(v) }
func (w *wMerged) writeBitShort(v int16)                     { w.m.writeBitShort(v) }
func (w *wMerged) writeBitLong(v int32)                      { w.m.writeBitLong(v) }
func (w *wMerged) writeBitLongLong(v int64)                  { w.m.writeBitLongLong(v) }
func (w *wMerged) writeBitDouble(v float64)                  { w.m.writeBitDouble(v) }
func (w *wMerged) write2BitDouble(v cadoc.Vec2)              { w.m.write2BitDouble(v) }
func (w *wMerged) write3BitDouble(v cadoc.Vec3)              { w.m.write3BitDouble(v) }
func (w *wMerged) write2RawDouble(v cadoc.Vec2)              { w.m.write2RawDouble(v) }
func (w *wMerged) writeRawDouble(v float64)                  { w.m.writeRawDouble(v) }
func (w *wMerged) writeRawShort(v int16)                     { w.m.writeRawShort(v) }
func (w *wMerged) writeRawLong(v int64)                      { w.m.writeRawLong(v) }
func (w *wMerged) writeInt(v int32)                          { w.m.writeInt(v) }
func (w *wMerged) writeByte(v byte)                          { w.m.writeByte(v) }
func (w *wMerged) writeBytes(p []byte)                       { w.m.writeBytes(p) }
func (w *wMerged) writeVariableText(s string)                { w.t.writeVariableText(s) }
func (w *wMerged) writeTextUnicode(s string)                 { w.t.writeTextUnicode(s) }
func (w *wMerged) writeDateTime(t time.Time)                 { w.m.writeDateTime(t) }
func (w *wMerged) write8BitJulianDate(t time.Time)           { w.m.write8BitJulianDate(t) }
func (w *wMerged) writeTimeSpan(d time.Duration)             { w.m.writeTimeSpan(d) }
func (w *wMerged) writeObjectType(v int16)                   { w.m.writeObjectType(v) }
func (w *wMerged) writeBitThickness(v float64)               { w.m.writeBitThickness(v) }
func (w *wMerged) writeBitExtrusion(v cadoc.Vec3)            { w.m.writeBitExtrusion(v) }
func (w *wMerged) handleReference(t wRefType, handle uint64) { w.h.handleReference(t, handle) }

func (w *wMerged) writeBitDoubleWithDefault(value, def float64) {
	w.m.writeBitDoubleWithDefault(value, def)
}
func (w *wMerged) write2BitDoubleWithDefault(value, def cadoc.Vec2) {
	w.m.write2BitDoubleWithDefault(value, def)
}
func (w *wMerged) write3BitDoubleWithDefault(value, def cadoc.Vec3) {
	w.m.write3BitDoubleWithDefault(value, def)
}
func (w *wMerged) writeEnColor(c cadoc.Color, t cadoc.Transparency, isBookColor bool) {
	w.m.writeEnColor(c, t, isBookColor)
}

// writeCmColor: before R2004 the merged writer writes a BS index unless the
// colour belongs to the "text stream" (table style colours), which get the
// CMC form, as in ACadSharp.
func (w *wMerged) writeCmColor(c cadoc.Color, useTextStream bool) {
	if !w.m.cmc && !useTextStream {
		w.m.writeCmColor(c, false)
		return
	}
	wWriteCMC(w, c)
}

func (w *wMerged) resetStream() {
	w.m.resetStream()
	w.t.resetStream()
	w.h.resetStream()
}

func (w *wMerged) savePositionForSize() {
	w.saved = true
	w.sizePosition = w.m.positionInBits()
	w.m.writeRawLong(0)
}

func (w *wMerged) writeSpearShift() {
	if w.ac14 {
		w.writeSpearShiftAC14()
		return
	}
	mainSizeBits := w.m.positionInBits()
	textSizeBits := w.t.positionInBits()
	w.m.writeSpearShift()
	if w.saved {
		total := mainSizeBits + textSizeBits + 1
		if textSizeBits > 0 {
			total += 16
			if textSizeBits >= 0x8000 {
				total += 16
				if textSizeBits >= 0x40000000 {
					total += 16
				}
			}
		}
		w.m.setPositionInBits(w.sizePosition)
		w.m.writeRawLong(int64(int32(total)))
		w.m.writeShiftValue()
	}
	w.m.setPositionInBits(mainSizeBits)
	if textSizeBits > 0 {
		w.t.writeSpearShift()
		w.m.writeBytes(w.t.ms.bytes())
		w.m.writeSpearShift()
		w.m.setPositionInBits(mainSizeBits + textSizeBits)
		w.m.setPositionByFlag(textSizeBits)
		w.m.writeBit(true)
	} else {
		w.m.writeBit(false)
	}
	w.h.writeSpearShift()
	w.savedPos = w.m.positionInBits()
	w.m.writeBytes(w.h.ms.bytes())
	w.m.writeSpearShift()
}

func (w *wMerged) writeSpearShiftAC14() {
	pos := w.m.positionInBits()
	if w.saved {
		w.m.writeSpearShift()
		w.m.setPositionInBits(w.sizePosition)
		w.m.writeRawLong(int64(int32(pos)))
		w.m.writeShiftValue()
		w.m.setPositionInBits(pos)
	}
	w.h.writeSpearShift()
	w.m.writeBytes(w.h.ms.bytes())
	w.m.writeSpearShift()
}

func wUTF16Bytes(u []uint16) []byte {
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

// wUTF16 encodes a string as UTF-16LE without terminator.
func wUTF16(s string) []byte { return wUTF16Bytes(utf16.Encode([]rune(s))) }

// wDateToJulian is CadUtils.DateToJulian: the Julian day and the
// milliseconds of a date shifted by -12 hours; dates before 0001-01-01 12:00
// (and the zero time) give 0, 0.
func wDateToJulian(t time.Time) (jdate, ms int32) {
	if t.IsZero() || t.Before(time.Date(1, 1, 1, 12, 0, 0, 0, t.Location())) {
		return 0, 0
	}
	t = t.Add(-12 * time.Hour)
	month := int(t.Month())
	day := int(math.Floor((14.0 - float64(month)) / 12.0))
	year := t.Year() + 4800 - day
	jd := t.Day() + int(math.Floor((153.0*float64(month+12*day-3)+2.0)/5.0)) + 365*year +
		int(math.Floor(float64(year)/4.0)) - int(math.Floor(float64(year)/100.0)) +
		int(math.Floor(float64(year)/400.0)) - 32045
	msec := t.Nanosecond()/1e6 + t.Second()*1000 + t.Minute()*60000 + t.Hour()*3600000
	return int32(jd), int32(msec)
}
