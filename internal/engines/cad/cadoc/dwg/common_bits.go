package dwg

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// RefType is DwgReferenceType: the 2 low bits of a handle reference code.
type RefType byte

const (
	RefUndefined     RefType = 0
	RefSoftOwnership RefType = 2
	RefHardOwnership RefType = 3
	RefSoftPointer   RefType = 4
	RefHardPointer   RefType = 5
)

// BitReader reads the DWG bit stream (ACadSharp's DwgStreamReaderBase with
// the per-version behaviour of DwgStreamReaderAC12 … AC24). Errors panic
// (like the C# exceptions); readers recover them per object or section.
//
// Pos is the position of the next byte in Buf (the C# Stream.Position) and
// Shift the bit offset inside the byte read last (BitShift).
type BitReader struct {
	Buf   []byte
	Pos   int
	Shift int
	last  byte
	// Version selects the version dependent encodings.
	Version cadoc.Version
	// text decodes code page strings (pre-2007); nil means UTF-8.
	text *textDecoder
	// Empty is IsEmpty: the string stream flag was 0 (SetPositionByFlag).
	Empty bool
}

// NewBitReader returns a reader over buf starting at pos.
func NewBitReader(v cadoc.Version, buf []byte, pos int, text *textDecoder) *BitReader {
	return &BitReader{Buf: buf, Pos: pos, Version: v, text: text}
}

// clone returns a reader over the same data (HugeMemoryStream.Clone), at the
// start, with the same encoding.
func (r *BitReader) clone() *BitReader {
	return &BitReader{Buf: r.Buf, Version: r.Version, text: r.text}
}

// SetPosition moves to a byte position and resets the bit shift (the C#
// Position setter).
func (r *BitReader) SetPosition(pos int) {
	r.Pos = pos
	r.Shift = 0
}

// Length is the stream length.
func (r *BitReader) Length() int { return len(r.Buf) }

func (r *BitReader) streamByte() byte {
	if r.Pos >= len(r.Buf) || r.Pos < 0 {
		panic(errEndOfStream)
	}
	b := r.Buf[r.Pos]
	r.Pos++
	return b
}

// AdvanceByte reads the next raw byte into the bit buffer.
func (r *BitReader) AdvanceByte() { r.last = r.streamByte() }

// ReadByte reads 8 bits.
func (r *BitReader) ReadRC() byte {
	if r.Shift == 0 {
		r.last = r.streamByte()
		return r.last
	}
	lastValues := r.last << r.Shift
	r.last = r.streamByte()
	return lastValues | r.last>>(8-r.Shift)
}

// count checks a count read from the stream against the bits left (every
// element takes at least minBits) and returns it as an int.
func (r *BitReader) count(n int32, minBits int) int {
	if n < 0 || int64(n)*int64(minBits) > int64(len(r.Buf)-r.Pos)*8+8 {
		panic(errorf("invalid count %d", n))
	}
	return int(n)
}

// ReadBytes reads n bytes (shifted by the current bit offset).
func (r *BitReader) ReadBytes(n int) []byte {
	if n < 0 || r.Pos < 0 || r.Pos+n > len(r.Buf) {
		panic(errEndOfStream)
	}
	out := make([]byte, n)
	copy(out, r.Buf[r.Pos:r.Pos+n])
	r.Pos += n
	if r.Shift == 0 {
		return out
	}
	shift := 8 - r.Shift
	for i := range out {
		lastValue := r.last << r.Shift
		r.last = out[i]
		out[i] = lastValue | r.last>>shift
	}
	return out
}

// ReadShort reads a raw little endian short (RS).
func (r *BitReader) ReadShort() int16 { return int16(binary.LittleEndian.Uint16(r.ReadBytes(2))) }

// ReadShortBE reads a raw big endian short.
func (r *BitReader) ReadShortBE() int16 { return int16(binary.BigEndian.Uint16(r.ReadBytes(2))) }

// ReadUShort reads a raw little endian unsigned short.
func (r *BitReader) ReadUShort() uint16 { return binary.LittleEndian.Uint16(r.ReadBytes(2)) }

// ReadInt reads a raw little endian int (RL).
func (r *BitReader) ReadInt() int32 { return int32(binary.LittleEndian.Uint32(r.ReadBytes(4))) }

// ReadUInt reads a raw little endian unsigned int.
func (r *BitReader) ReadUInt() uint32 { return binary.LittleEndian.Uint32(r.ReadBytes(4)) }

// ReadRawLong is ReadRawLong: an RL widened to 64 bits.
func (r *BitReader) ReadRawLong() int64 { return int64(r.ReadInt()) }

// ReadRawULong reads a raw little endian unsigned 64 bit value.
func (r *BitReader) ReadRawULong() uint64 { return binary.LittleEndian.Uint64(r.ReadBytes(8)) }

// ReadDouble reads a raw double (RD).
func (r *BitReader) ReadDouble() float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(r.ReadBytes(8)))
}

// ReadRawChar reads an RC.
func (r *BitReader) ReadRawChar() byte { return r.ReadRC() }

// ReadString reads n bytes as text in the given decoder.
func (r *BitReader) ReadString(n int, d *textDecoder) string { return d.decode(r.ReadBytes(n)) }

// ReadBit reads one bit (B).
func (r *BitReader) ReadBit() bool {
	if r.Shift == 0 {
		r.AdvanceByte()
		r.Shift = 1
		return r.last&128 == 128
	}
	v := (r.last<<r.Shift)&128 == 128
	r.Shift++
	r.Shift &= 7
	return v
}

// ReadBitAsShort reads a bit as 0 or 1.
func (r *BitReader) ReadBitAsShort() int16 {
	if r.ReadBit() {
		return 1
	}
	return 0
}

// Read2Bits reads a bit pair (BB).
func (r *BitReader) Read2Bits() byte {
	var v byte
	switch r.Shift {
	case 0:
		r.AdvanceByte()
		v = r.last >> 6
		r.Shift = 2
	case 7:
		lastValue := r.last << 1 & 2
		r.AdvanceByte()
		v = lastValue | r.last>>7
		r.Shift = 1
	default:
		v = r.last >> (6 - r.Shift) & 3
		r.Shift += 2
		r.Shift &= 7
	}
	return v
}

// ReadBitShort reads a BS.
func (r *BitReader) ReadBitShort() int16 {
	switch r.Read2Bits() {
	case 0:
		return r.ReadShort()
	case 1:
		return int16(r.ReadRC())
	case 2:
		return 0
	default:
		return 256
	}
}

// ReadBitShortAsBool reads a BS that is non-zero for true.
func (r *BitReader) ReadBitShortAsBool() bool { return r.ReadBitShort() != 0 }

// ReadBitLong reads a BL.
func (r *BitReader) ReadBitLong() int32 {
	switch r.Read2Bits() {
	case 0:
		return r.ReadInt()
	case 1:
		return int32(r.ReadRC())
	case 2:
		return 0
	}
	panic(errorf("Failed to read ReadBitLong"))
}

// ReadBitLongLong reads a BLL (3 bit byte count, then the bytes).
func (r *BitReader) ReadBitLongLong() int64 {
	var v uint64
	size := r.read3Bits()
	for i := 0; i < int(size); i++ {
		v += uint64(r.ReadRC()) << (i << 3)
	}
	return int64(v)
}

func (r *BitReader) read3Bits() byte {
	var b byte
	for i := 0; i < 3; i++ {
		b <<= 1
		if r.ReadBit() {
			b |= 1
		}
	}
	return b
}

// ReadBitDouble reads a BD.
func (r *BitReader) ReadBitDouble() float64 {
	switch r.Read2Bits() {
	case 0:
		return r.ReadDouble()
	case 1:
		return 1
	case 2:
		return 0
	}
	panic(errorf("Failed to read ReadBitDouble"))
}

// Read2BitDouble reads a 2BD.
func (r *BitReader) Read2BitDouble() cadoc.Vec2 {
	x := r.ReadBitDouble()
	return cadoc.Vec2{X: x, Y: r.ReadBitDouble()}
}

// Read3BitDouble reads a 3BD.
func (r *BitReader) Read3BitDouble() cadoc.Vec3 {
	x := r.ReadBitDouble()
	y := r.ReadBitDouble()
	return cadoc.Vec3{X: x, Y: y, Z: r.ReadBitDouble()}
}

// Read2RawDouble reads a 2RD.
func (r *BitReader) Read2RawDouble() cadoc.Vec2 {
	x := r.ReadDouble()
	return cadoc.Vec2{X: x, Y: r.ReadDouble()}
}

// Read3RawDouble reads a 3RD.
func (r *BitReader) Read3RawDouble() cadoc.Vec3 {
	x := r.ReadDouble()
	y := r.ReadDouble()
	return cadoc.Vec3{X: x, Y: y, Z: r.ReadDouble()}
}

// ReadModularChar reads an unsigned modular char (MC).
func (r *BitReader) ReadModularChar() uint64 {
	shift := 0
	b := r.ReadRC()
	v := uint64(b & 0b01111111)
	if b&0b10000000 != 0 {
		for {
			shift += 7
			last := r.ReadRC()
			v |= uint64(last&0b01111111) << (shift & 63)
			if last&0b10000000 == 0 {
				break
			}
		}
	}
	return v
}

// ReadSignedModularChar reads a signed modular char. The shifted branch of
// the C# code sums in 32 bits; that is kept.
func (r *BitReader) ReadSignedModularChar() int64 {
	if r.Shift == 0 {
		r.AdvanceByte()
		if r.last&0b10000000 == 0 {
			v := int64(r.last & 0b00111111)
			if r.last&0b01000000 > 0 {
				v = -v
			}
			return v
		}
		totalShift := 0
		sum := int64(r.last & 0x7F)
		for {
			totalShift += 7
			r.AdvanceByte()
			if r.last&0b10000000 != 0 {
				sum |= int64(r.last&0x7F) << (totalShift & 63)
			} else {
				break
			}
		}
		v := sum | int64(r.last&0b00111111)<<(totalShift&63)
		if r.last&0b01000000 > 0 {
			v = -v
		}
		return v
	}
	last := r.ReadRC()
	if last&0b10000000 == 0 {
		v := int64(last & 0b00111111)
		if last&0b01000000 > 0 {
			v = -v
		}
		return v
	}
	totalShift := 0
	sum := int32(last & 0x7F)
	var cur byte
	for {
		totalShift += 7
		cur = r.ReadRC()
		if cur&0b10000000 != 0 {
			sum |= int32(cur&0x7F) << (totalShift & 31)
		} else {
			break
		}
	}
	v := int64(sum | int32(cur&0b00111111)<<(totalShift&31))
	if cur&0b01000000 > 0 {
		v = -v
	}
	return v
}

// ReadModularShort reads a modular short (MS).
func (r *BitReader) ReadModularShort() int32 {
	shift := 0b1111
	b1 := r.ReadRC()
	b2 := r.ReadRC()
	flag := b2&0b10000000 == 0
	v := int32(b1) | int32(b2&0b1111111)<<8
	for !flag {
		b1 = r.ReadRC()
		b2 = r.ReadRC()
		flag = b2&0b10000000 == 0
		v |= int32(b1) << (shift & 31)
		shift += 8
		v |= int32(b2&0b1111111) << (shift & 31)
		shift += 7
	}
	return v
}

// HandleReference reads a handle (H); offset codes are relative to ref.
func (r *BitReader) HandleReference(ref uint64) uint64 {
	h, _ := r.HandleReferenceType(ref)
	return h
}

// HandleReferenceType reads a handle and returns its reference type.
func (r *BitReader) HandleReferenceType(ref uint64) (uint64, RefType) {
	form := r.ReadRC()
	code := form >> 4
	counter := int(form & 0b00001111)
	t := RefType(code & 0b0011)
	switch {
	case code <= 0x5:
		return r.readHandle(counter), t
	case code == 0x6:
		return ref + 1, t
	case code == 0x8:
		return ref - 1, t
	case code == 0xA:
		return ref + r.readHandle(counter), t
	case code == 0xC:
		return ref - r.readHandle(counter), t
	}
	panic(errorf("[HandleReference] invalid reference code with value: %d", code))
}

func (r *BitReader) readHandle(length int) uint64 {
	if length > 8 {
		panic(errorf("[HandleReference] invalid handle length %d", length))
	}
	var v uint64
	for _, b := range r.ReadBytes(length) {
		v = v<<8 | uint64(b)
	}
	return v
}

// ReadTextUnicode reads a TU string (RS length, codepage RC and bytes before
// R2007; RS length and UTF-16 from R2007 on).
func (r *BitReader) ReadTextUnicode() string {
	if r.Version >= cadoc.AC1021 {
		n := r.ReadShort()
		if n == 0 {
			return ""
		}
		length := int16(n << 1)
		return removeNUL(decodeUTF16LE(r.ReadBytes(int(length))))
	}
	n := int(r.ReadShort())
	key := int(r.ReadRC())
	if n == 0 {
		return ""
	}
	cp := DwgCodePageAt(key)
	return r.ReadString(n, newTextDecoder(cp.Encoding))
}

// ReadVariableText reads a TV string.
func (r *BitReader) ReadVariableText() string {
	if r.Version >= cadoc.AC1021 {
		n := r.ReadBitShort()
		if n == 0 {
			return ""
		}
		length := int16(n << 1)
		return removeNUL(decodeUTF16LE(r.ReadBytes(int(length))))
	}
	n := r.ReadBitShort()
	if n <= 0 {
		return ""
	}
	return removeNUL(r.ReadString(int(n), r.text))
}

// ReadSentinel reads 16 bytes.
func (r *BitReader) ReadSentinel() [16]byte {
	var s [16]byte
	copy(s[:], r.ReadBytes(16))
	return s
}

// ReadBitDoubleWithDefault reads a DD.
func (r *BitReader) ReadBitDoubleWithDefault(def float64) float64 {
	var arr [8]byte
	binary.LittleEndian.PutUint64(arr[:], math.Float64bits(def))
	switch r.Read2Bits() {
	case 0:
		return def
	case 1:
		copy(arr[0:4], r.ReadBytes(4))
	case 2:
		b := r.ReadBytes(6)
		arr[4], arr[5] = b[0], b[1]
		copy(arr[0:4], b[2:6])
	default:
		return r.ReadDouble()
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(arr[:]))
}

// Read2BitDoubleWithDefault reads a 2DD.
func (r *BitReader) Read2BitDoubleWithDefault(def cadoc.Vec2) cadoc.Vec2 {
	x := r.ReadBitDoubleWithDefault(def.X)
	return cadoc.Vec2{X: x, Y: r.ReadBitDoubleWithDefault(def.Y)}
}

// Read3BitDoubleWithDefault reads a 3DD.
func (r *BitReader) Read3BitDoubleWithDefault(def cadoc.Vec3) cadoc.Vec3 {
	x := r.ReadBitDoubleWithDefault(def.X)
	y := r.ReadBitDoubleWithDefault(def.Y)
	return cadoc.Vec3{X: x, Y: y, Z: r.ReadBitDoubleWithDefault(def.Z)}
}

// colorIndex is new Color(short): out of range indexes are an error.
func colorIndex(i int16) cadoc.Color {
	if i < 0 || i > 257 {
		panic(errorf("True index must be a value between 0 and 257, was %d", i))
	}
	return cadoc.ColorIndex(i)
}

// ReadCmColor reads a CMC (BS index before R2004; index, RGB and the color
// and book name flags from R2004 on, the names read from this stream).
func (r *BitReader) ReadCmColor() cadoc.Color {
	if r.Version < cadoc.AC1018 {
		return colorIndex(r.ReadBitShort())
	}
	r.ReadBitShort()
	rgb := uint32(r.ReadBitLong())
	c := cmColorFromRGB(rgb)
	id := r.ReadRC()
	if id&1 == 1 {
		r.ReadVariableText()
	}
	if id&2 == 2 {
		r.ReadVariableText()
	}
	return c
}

func cmColorFromRGB(rgb uint32) cadoc.Color {
	switch {
	case rgb == 0xC0000000:
		return cadoc.ColorByLayer
	case rgb&0x01000000 != 0:
		return colorIndex(int16(byte(rgb)))
	}
	return cadoc.ColorRGB(byte(rgb>>16), byte(rgb>>8), byte(rgb))
}

// transparencyFromAlpha is Transparency.FromAlphaValue.
func transparencyFromAlpha(value int32) cadoc.Transparency {
	alpha := int16(100 - float64(byte(value))/255.0*100)
	switch {
	case alpha == -1:
		return cadoc.TransparencyByLayer
	case alpha == 100:
		return cadoc.TransparencyByBlock
	case alpha < 0:
		return cadoc.NewTransparency(0)
	case alpha > 90:
		return cadoc.NewTransparency(90)
	}
	return cadoc.NewTransparency(alpha)
}

// newTransparency is new Transparency(short): out of range values fail.
func newTransparency(v int16) cadoc.Transparency {
	if v != -1 && v != 100 && (v < 0 || v > 90) {
		panic(errorf("Transparency must be in range from 0 to 90, was %d", v))
	}
	return cadoc.NewTransparency(v)
}

// ReadEnColor reads an entity color (ENC); isBookColor reports that a color
// book handle follows in the handle stream.
func (r *BitReader) ReadEnColor() (c cadoc.Color, t cadoc.Transparency, isBookColor bool) {
	if r.Version < cadoc.AC1018 {
		return colorIndex(r.ReadBitShort()), cadoc.TransparencyByLayer, false
	}
	size := r.ReadBitShort()
	if size == 0 {
		// ACadSharp gives these opaque; without a transparency value it is
		// ByLayer, as the DXF of the same drawing has it.
		return cadoc.ColorByBlock, cadoc.TransparencyByLayer, false
	}
	flags := uint16(size) & 0b1111111100000000
	switch {
	case flags&0x4000 > 0:
		c = cadoc.ColorByBlock
		isBookColor = true
	case flags&0x8000 > 0:
		rgb := uint32(r.ReadBitLong())
		c = cadoc.ColorRGB(byte(rgb>>16), byte(rgb>>8), byte(rgb))
	default:
		c = colorIndex(size & 0b111111111111)
	}
	if flags&0x2000 > 0 {
		t = transparencyFromAlpha(r.ReadBitLong())
	} else {
		t = cadoc.TransparencyByLayer
	}
	return c, t, isBookColor
}

// ReadColorByIndex reads a BS color index.
func (r *BitReader) ReadColorByIndex() cadoc.Color { return colorIndex(r.ReadBitShort()) }

// ReadObjectType reads the object type (BS before R2010, OT after).
func (r *BitReader) ReadObjectType() cadoc.ObjectType {
	if r.Version < cadoc.AC1024 {
		return cadoc.ObjectType(r.ReadBitShort())
	}
	switch r.Read2Bits() {
	case 0:
		return cadoc.ObjectType(r.ReadRC())
	case 1:
		return cadoc.ObjectType(0x1F0 + int16(r.ReadRC()))
	default:
		return cadoc.ObjectType(r.ReadShort())
	}
}

// ReadBitExtrusion reads a BE (3BD in R13-R14).
func (r *BitReader) ReadBitExtrusion() cadoc.Vec3 {
	if r.Version >= cadoc.AC1015 && r.ReadBit() {
		return cadoc.AxisZ
	}
	return r.Read3BitDouble()
}

// ReadBitThickness reads a BT (BD in R13-R14).
func (r *BitReader) ReadBitThickness() float64 {
	if r.Version >= cadoc.AC1015 && r.ReadBit() {
		return 0
	}
	return r.ReadBitDouble()
}

// Read8BitJulianDate reads a TD of two raw longs.
func (r *BitReader) Read8BitJulianDate() time.Time {
	day := r.ReadInt()
	return julianToDate(day, r.ReadInt())
}

// ReadDateTime reads a TD of two bit longs.
func (r *BitReader) ReadDateTime() time.Time {
	day := r.ReadBitLong()
	return julianToDate(day, r.ReadBitLong())
}

// ReadTimeSpan reads a TIMEBLL (ACadSharp reads the day part as hours).
func (r *BitReader) ReadTimeSpan() time.Duration {
	hours := int64(r.ReadBitLong())
	ms := int64(r.ReadBitLong())
	if hours < 0 || ms < 0 {
		return 0
	}
	return time.Duration(hours)*time.Hour + time.Duration(ms)*time.Millisecond
}

// julianToDate is DwgStreamReaderBase.julianToDate: the local wall clock
// 1970-01-01 plus the seconds since the Unix epoch's Julian day.
func julianToDate(jdate, ms int32) time.Time {
	seconds := (float64(jdate) - 2440587.5) * 86400
	whole := math.Floor(seconds)
	t := time.Unix(int64(whole), int64((seconds-whole)*1e9)).UTC()
	if t.Year() < 1 || t.Year() > 9999 {
		return time.Time{}
	}
	if ms >= 0 {
		t = t.Add(time.Duration(ms) * time.Millisecond)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
}

// PositionInBits is the bit position.
func (r *BitReader) PositionInBits() int64 {
	p := int64(r.Pos) * 8
	if r.Shift > 0 {
		p += int64(r.Shift) - 8
	}
	return p
}

// SetPositionInBits moves to a bit position.
func (r *BitReader) SetPositionInBits(p int64) {
	r.SetPosition(int(p >> 3))
	r.Shift = int(p & 7)
	if r.Shift > 0 {
		r.AdvanceByte()
	}
}

// Advance skips offset bytes.
func (r *BitReader) Advance(offset int) {
	if offset > 1 {
		r.Pos += offset - 1
	}
	r.ReadRC()
}

// ResetShift aligns to the next byte and reads a raw short (the CRC after a
// bit coded section).
func (r *BitReader) ResetShift() uint16 {
	r.Shift = 0
	r.AdvanceByte()
	n := uint16(r.last)
	r.AdvanceByte()
	return n | uint16(r.last)<<8
}

// SetPositionByFlag positions a string stream reader: position is the
// "endbit" of the object data; when the string stream flag bit there is set
// the reader moves to the start of the strings, otherwise it is marked empty
// and moved to the end.
func (r *BitReader) SetPositionByFlag(position int64) int64 {
	r.SetPositionInBits(position)
	start := position
	if r.ReadBit() {
		length, size := r.applyFlagToPosition(position)
		start = length - size
		r.SetPositionInBits(start)
	} else {
		r.Empty = true
		r.SetPosition(len(r.Buf))
	}
	return start
}

func (r *BitReader) applyFlagToPosition(lastPos int64) (length, size int64) {
	length = lastPos - 16
	r.SetPositionInBits(length)
	size = int64(r.ReadUShort())
	if size&0x8000 == 0 {
		return length, size
	}
	length -= 16
	r.SetPositionInBits(length)
	size &= 0x7FFF
	hi := int64(r.ReadUShort())
	size += (hi & 0xFFFF) << 15
	return length, size
}
