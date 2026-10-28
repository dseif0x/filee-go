package fonts

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"

	"github.com/dseif0x/filee-go/internal/convert"
)

// Big-endian readers and writers for font data (SFNT, WOFF, WOFF2, CFF all
// store numbers big-endian), including the variable-length integers of WOFF2
// (UIntBase128 and 255UInt16).
//
// Damaged data is reported by panicking with a dataError, which the package's
// entry points turn back into an error: the parsers read like the format
// specifications instead of checking an error after every number.

type dataError string

func (e dataError) Error() string { return string(e) }

func fail(msg string) { panic(dataError(msg)) }

type canceled struct{ err error }

// refusal is a conversion refused for a reason the user can understand.
type refusal struct{ key, detail string }

func refuse(key, detail string) { panic(refusal{key, detail}) }

func checkCtx(ctx context.Context) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			panic(canceled{err})
		}
	}
}

// recoverData turns a dataError or cancellation panic into *err.
func recoverData(err *error) {
	switch r := recover().(type) {
	case nil:
	case dataError:
		*err = errors.New(string(r))
	case canceled:
		*err = r.err
	case refusal:
		*err = convert.Refuse(r.key, r.detail)
	case runtime.Error:
		// A damaged file that slipped past the checks: an error for this file, not a crash of the app.
		*err = errors.New("The font file is truncated or damaged.")
	default:
		panic(r)
	}
}

// reader reads big-endian values from a window of data; every read is bounds-checked.
type reader struct {
	data []byte
	pos  int
	end  int
}

func newReader(data []byte) *reader { return &reader{data: data, end: len(data)} }

func newWindow(data []byte, offset, end int) *reader {
	return &reader{data: data, pos: offset, end: end}
}

func (r *reader) need(n int) {
	if n < 0 || r.pos < 0 || r.pos > r.end-n {
		fail("The font file is truncated or damaged.")
	}
}

func (r *reader) remaining() int { return r.end - r.pos }

func (r *reader) u8() byte {
	r.need(1)
	r.pos++
	return r.data[r.pos-1]
}

func (r *reader) u16() uint16 {
	r.need(2)
	v := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v
}

func (r *reader) i16() int16 { return int16(r.u16()) }

func (r *reader) u32() uint32 {
	r.need(4)
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) i32() int32 { return int32(r.u32()) }

// span returns the next n bytes without copying.
func (r *reader) span(n int) []byte {
	r.need(n)
	s := r.data[r.pos : r.pos+n]
	r.pos += n
	return s
}

func (r *reader) bytes(n int) []byte { return append([]byte(nil), r.span(n)...) }

func (r *reader) skip(n int) {
	r.need(n)
	r.pos += n
}

// uintBase128 is WOFF2's UIntBase128: up to five bytes of 7 bits each, most
// significant first, no leading zeros.
func (r *reader) uintBase128() uint32 {
	var v uint32
	for i := 0; i < 5; i++ {
		b := r.u8()
		if i == 0 && b == 0x80 {
			fail("Invalid WOFF2 number (leading zeros).")
		}
		if v&0xFE000000 != 0 {
			fail("Invalid WOFF2 number (overflow).")
		}
		v = v<<7 | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v
		}
	}
	fail("Invalid WOFF2 number (longer than five bytes).")
	return 0
}

// read255UInt16 is WOFF2's 255UInt16: one byte below 253, else a marker byte
// followed by one or two bytes.
func (r *reader) read255UInt16() uint16 {
	switch code := r.u8(); code {
	case 253:
		return r.u16()
	case 254:
		return uint16(r.u8()) + 253*2
	case 255:
		return uint16(r.u8()) + 253
	default:
		return uint16(code)
	}
}

// writer is a growable buffer that writes big-endian values.
type writer struct{ buf []byte }

func newWriter(capacity int) *writer { return &writer{buf: make([]byte, 0, max(16, capacity))} }

func (w *writer) len() int { return len(w.buf) }

func (w *writer) u8(v int) { w.buf = append(w.buf, byte(v)) }

func (w *writer) u16(v int) { w.buf = binary.BigEndian.AppendUint16(w.buf, uint16(v)) }

func (w *writer) i16(v int) { w.u16(int(uint16(int16(v)))) }

func (w *writer) u32(v uint32) { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }

func (w *writer) bytes(b []byte) { w.buf = append(w.buf, b...) }

func (w *writer) zeros(n int) { w.buf = append(w.buf, make([]byte, n)...) }

// pad4 pads with zeros to the next multiple of four.
func (w *writer) pad4() { w.zeros((4 - len(w.buf)&3) & 3) }

func (w *writer) setU32(pos int, v uint32) { binary.BigEndian.PutUint32(w.buf[pos:], v) }

func (w *writer) uintBase128(v uint32) {
	groups := 1
	for x := v >> 7; x != 0; x >>= 7 {
		groups++
	}
	for i := groups - 1; i >= 0; i-- {
		bits := int(v>>(7*i)) & 0x7F
		if i > 0 {
			bits |= 0x80
		}
		w.u8(bits)
	}
}

// write255UInt16 writes a WOFF2 255UInt16 in its shortest form.
func (w *writer) write255UInt16(v int) {
	switch {
	case v < 253:
		w.u8(v)
	case v < 506:
		w.u8(255)
		w.u8(v - 253)
	case v < 762:
		w.u8(254)
		w.u8(v - 506)
	default:
		w.u8(253)
		w.u16(v)
	}
}

func be16(b []byte, off int) uint16 { return binary.BigEndian.Uint16(b[off:]) }
func be32(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }
