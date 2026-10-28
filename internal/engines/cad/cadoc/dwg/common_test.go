package dwg

import (
	"bytes"
	"math"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

func TestCommonCRC(t *testing.T) {
	check := []byte("123456789")
	// The "CRC8" of the DWG format is CRC-16/ARC.
	if got := CRC8(0, check); got != 0xBB3D {
		t.Errorf("CRC8 = %04X", got)
	}
	// Continuing a CRC over a split buffer gives the same value.
	if got := CRC8(CRC8(0, check[:4]), check[4:]); got != 0xBB3D {
		t.Errorf("CRC8 split = %04X", got)
	}
	if got := CRC32(0, check); got != 0xCBF43926 {
		t.Errorf("CRC32 = %08X", got)
	}
	// The section page checksum is Adler-32 with the seed as initial value.
	if got := Checksum(1, []byte("Wikipedia")); got != 0x11E60398 {
		t.Errorf("Checksum = %08X", got)
	}
	big := bytes.Repeat([]byte{0xFF}, 0x15B0*3+17)
	if got, want := Checksum(1, big), adler32(big); got != want {
		t.Errorf("Checksum chunked = %08X, want %08X", got, want)
	}
}

func adler32(p []byte) uint32 {
	a, b := uint32(1), uint32(0)
	for _, c := range p {
		a = (a + uint32(c)) % 65521
		b = (b + a) % 65521
	}
	return b<<16 | a
}

func TestCommonMagicSequence(t *testing.T) {
	// The first bytes of the documented R2004 header XOR sequence.
	want := []byte{0x29, 0x23, 0xBE, 0x84, 0xE1, 0x6C, 0xD6, 0xAE, 0x52, 0x90, 0x49, 0xF1, 0xF1, 0xBB, 0xE9, 0xEB}
	if !bytes.Equal(MagicSequence[:16], want) {
		t.Errorf("% X", MagicSequence[:16])
	}
	p := bytes.Clone(want)
	DecryptAC18Header(p)
	if !bytes.Equal(p, make([]byte, 16)) {
		t.Errorf("decrypt: % X", p)
	}
	if CompressionPadding(0x20) != 0 || CompressionPadding(1) != 0x1F {
		t.Error("CompressionPadding")
	}
}

func TestCommonReedSolomon(t *testing.T) {
	// Two blocks of 3 data bytes interleaved with factor 2: the data bytes
	// of block i are at i, i+2, i+4.
	encoded := []byte{'a', 'd', 'b', 'e', 'c', 'f', 'p', 'q'}
	out := make([]byte, 6)
	ReedSolomonDeinterleave(encoded, out, 2, 3)
	if string(out) != "abcdef" {
		t.Errorf("%q", out)
	}
}

func TestCommonLZ77AC18(t *testing.T) {
	// Three literals, a 6 byte copy from 3 back, the 0x11 end marker.
	src := []byte{0xFF, 0x14, 'a', 'b', 'c', 0x78, 0x00, 0x11}
	out, next, err := DecompressAC18(src, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "abcabcabc" || next != len(src) {
		t.Errorf("%q next %d", out, next)
	}
	// Output is appended to dst, back references reach into it.
	out, _, err = DecompressAC18([]byte{0x12, 'x', 0x11}, 0, []byte("pre"))
	if err != nil || string(out) != "prex" {
		t.Errorf("%q %v", out, err)
	}
	// A literal run given as a literal length (0x00 opcode: 0x0F plus the
	// next byte, plus 3).
	lit := bytes.Repeat([]byte{'z'}, 0x0F+2+3)
	src = append(append([]byte{0x00, 0x02}, lit...), 0x11)
	out, _, err = DecompressAC18(src, 0, nil)
	if err != nil || !bytes.Equal(out, lit) {
		t.Errorf("literal length: %d bytes %v", len(out), err)
	}
	// A back reference before the start of the output is an error.
	if _, _, err := DecompressAC18([]byte{0x14, 'a', 'b', 'c', 0x7C, 0x00, 0x11}, 0, nil); err == nil {
		t.Error("expected an error for a reference before the start")
	}
	// Truncated input.
	if _, _, err := DecompressAC18([]byte{0x20}, 0, nil); err == nil {
		t.Error("expected an error for truncated input")
	}
}

func TestCommonLZ77AC21(t *testing.T) {
	// A literal run of 8 bytes (opcode 0: length 0 + 8).
	src := append([]byte{0x00}, []byte("abcdefgh")...)
	out := make([]byte, 8)
	if err := DecompressAC21(src, 0, uint32(len(src)), out); err != nil || string(out) != "abcdefgh" {
		t.Errorf("%q %v", out, err)
	}
	// Then a 3 byte copy from 2 back (opcode 0x31, offset byte 0).
	src = append(src, 0x31, 0x00)
	out = make([]byte, 11)
	if err := DecompressAC21(src, 0, uint32(len(src)), out); err != nil || string(out) != "abcdefghghg" {
		t.Errorf("%q %v", out, err)
	}
	// Literal runs keep the AC21 byte order: 9 bytes are the 9th byte, then
	// the first 8.
	src = append([]byte{0x01}, []byte("123456789")...)
	out = make([]byte, 9)
	if err := DecompressAC21(src, 0, uint32(len(src)), out); err != nil || string(out) != "912345678" {
		t.Errorf("%q %v", out, err)
	}
	// Overruns are errors, not panics.
	if err := DecompressAC21([]byte{0x05, 1}, 0, 2, make([]byte, 2)); err == nil {
		t.Error("expected an error")
	}
}

// bitWriter builds bit streams for the reader tests.
type bitWriter struct {
	buf []byte
	n   int
}

func (w *bitWriter) bit(b bool) {
	if w.n%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if b {
		w.buf[len(w.buf)-1] |= 0x80 >> (w.n % 8)
	}
	w.n++
}

func (w *bitWriter) bits(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		w.bit(v>>i&1 == 1)
	}
}

func (w *bitWriter) bytes(p ...byte) {
	for _, b := range p {
		w.bits(uint64(b), 8)
	}
}

func TestCommonBitReader(t *testing.T) {
	w := &bitWriter{}
	w.bit(true)         // B
	w.bits(0b01, 2)     // BS: one byte follows
	w.bytes(200)        //
	w.bits(0b10, 2)     // BS 0
	w.bits(0b11, 2)     // BS 256
	w.bits(0b00, 2)     // BS: raw short
	w.bytes(0x34, 0x12) //
	w.bits(0b01, 2)     // BD 1.0
	w.bits(0b10, 2)     // BD 0.0
	w.bits(0b00, 2)     // BD raw
	f := math.Float64bits(-2.5)
	for i := 0; i < 8; i++ {
		w.bytes(byte(f >> (8 * i)))
	}
	w.bits(0b00, 2)                 // BL raw
	w.bytes(0x78, 0x56, 0x34, 0x12) //
	w.bits(0b010, 3)                // BLL: 2 bytes
	w.bytes(0x02, 0x01)             //
	w.bytes(0x5_1, 0x01)            // H: code 5, 1 byte, 0x01
	w.bytes(0x6_0)                  // H: code 6 (ref + 1)
	w.bytes(0xC_1, 0x03)            // H: code C (ref - 3)
	w.bits(0b01, 2)                 // DD: 4 bytes patched in
	w.bytes(0x00, 0x00, 0x00, 0x00) //
	w.bytes(0x81, 0x02)             // MC 0x101
	w.bytes(0xC1, 0x02)             // signed MC: 0x41 | 0x02<<7 = 0x141, negative? no: last byte 0x02
	w.bytes(0x45)                   // signed MC -5
	w.bytes(0x31, 0x82, 0x01, 0x00) // MS: 0x0231 | 0x01<<15
	r := NewBitReader(cadoc.AC1015, w.buf, 0, nil)
	if !r.ReadBit() {
		t.Error("B")
	}
	if v := r.ReadBitShort(); v != 200 {
		t.Errorf("BS %d", v)
	}
	if v := r.ReadBitShort(); v != 0 {
		t.Errorf("BS %d", v)
	}
	if v := r.ReadBitShort(); v != 256 {
		t.Errorf("BS %d", v)
	}
	if v := r.ReadBitShort(); v != 0x1234 {
		t.Errorf("BS %X", v)
	}
	if v := r.ReadBitDouble(); v != 1 {
		t.Errorf("BD %v", v)
	}
	if v := r.ReadBitDouble(); v != 0 {
		t.Errorf("BD %v", v)
	}
	if v := r.ReadBitDouble(); v != -2.5 {
		t.Errorf("BD %v", v)
	}
	if v := r.ReadBitLong(); v != 0x12345678 {
		t.Errorf("BL %X", v)
	}
	if v := r.ReadBitLongLong(); v != 0x0102 {
		t.Errorf("BLL %X", v)
	}
	if h := r.HandleReference(0); h != 1 {
		t.Errorf("H %X", h)
	}
	if h := r.HandleReference(0x10); h != 0x11 {
		t.Errorf("H %X", h)
	}
	if h := r.HandleReference(0x10); h != 0x0D {
		t.Errorf("H %X", h)
	}
	if v := r.ReadBitDoubleWithDefault(1.0); v != 1.0 {
		// 1.0 has zero low bytes: patching them with zeros keeps it.
		t.Errorf("DD %v", v)
	}
	if v := r.ReadModularChar(); v != 0x101 {
		t.Errorf("MC %X", v)
	}
	if v := r.ReadSignedModularChar(); v != 0x141 {
		t.Errorf("signed MC %X", v)
	}
	if v := r.ReadSignedModularChar(); v != -5 {
		t.Errorf("signed MC %d", v)
	}
	if v := r.ReadModularShort(); v != 0x0231|1<<15 {
		t.Errorf("MS %X", v)
	}
	// Reading past the end panics with errEndOfStream.
	func() {
		defer func() {
			if p := recover(); p != errEndOfStream {
				t.Errorf("end of stream: %v", p)
			}
		}()
		r.ReadBytes(10)
	}()
}

func TestCommonBitReaderPositions(t *testing.T) {
	w := &bitWriter{}
	w.bits(0b101, 3)
	w.bytes('A', 'B')
	r := NewBitReader(cadoc.AC1018, w.buf, 0, nil)
	r.ReadBit()
	r.ReadBit()
	r.ReadBit()
	if p := r.PositionInBits(); p != 3 {
		t.Errorf("position %d", p)
	}
	if s := string(r.ReadBytes(2)); s != "AB" {
		t.Errorf("shifted bytes %q", s)
	}
	r.SetPositionInBits(3)
	if b := r.ReadRC(); b != 'A' {
		t.Errorf("after seek %q", b)
	}
}

func TestCommonText(t *testing.T) {
	// R2007+ strings are UTF-16 with a 16 bit character count.
	w := &bitWriter{}
	w.bits(0b01, 2) // BS 3
	w.bytes(3)
	w.bytes('h', 0, 0xE9, 0, 0x34, 0x12)
	r := NewBitReader(cadoc.AC1021, w.buf, 0, nil)
	if s := r.ReadVariableText(); s != "héሴ" {
		t.Errorf("%q", s)
	}
	// Before R2007 they are in the drawing code page.
	w = &bitWriter{}
	w.bits(0b01, 2)
	w.bytes(2)
	w.bytes('a', 0xE4)
	r = NewBitReader(cadoc.AC1015, w.buf, 0, newTextDecoder(DwgCodePageAt(30).Encoding))
	if s := r.ReadVariableText(); s != "aä" {
		t.Errorf("%q", s)
	}
	if cp := DwgCodePageAt(30); cp.Name != "ansi_1252" || cp.Windows != 1252 {
		t.Errorf("%+v", cp)
	}
	if cp := DwgCodePageAt(999); cp.Windows != 0 {
		t.Errorf("%+v", cp)
	}
}

func TestCommonColors(t *testing.T) {
	// CMC true colour (0xC2 flag byte) and indexed (0xC3).
	if c := cmColorFromRGB(0xC2112233); c != cadoc.ColorRGB(0x11, 0x22, 0x33) {
		t.Errorf("%v", c)
	}
	if c := cmColorFromRGB(0xC3000005); c != cadoc.ColorIndex(5) {
		t.Errorf("%v", c)
	}
	if c := cmColorFromRGB(0xC0000000); !c.IsByLayer() {
		t.Errorf("%v", c)
	}
	if tr := transparencyFromAlpha(0x02000000 | 0xFF); tr.Value() != 0 {
		t.Errorf("%v", tr.Value())
	}
	if tr := transparencyFromAlpha(0x01000000); !tr.IsByBlock() {
		t.Errorf("%v", tr.Value())
	}
}
