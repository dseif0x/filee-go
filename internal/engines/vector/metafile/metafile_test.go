package metafile

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

type rec struct{ b bytes.Buffer }

func (r *rec) u32(v ...uint32) *rec {
	for _, x := range v {
		binary.Write(&r.b, binary.LittleEndian, x)
	}
	return r
}
func (r *rec) i32(v ...int32) *rec {
	for _, x := range v {
		binary.Write(&r.b, binary.LittleEndian, x)
	}
	return r
}
func (r *rec) u16(v ...uint16) *rec {
	for _, x := range v {
		binary.Write(&r.b, binary.LittleEndian, x)
	}
	return r
}
func (r *rec) i16(v ...int16) *rec {
	for _, x := range v {
		binary.Write(&r.b, binary.LittleEndian, x)
	}
	return r
}

// emfRecord wraps parameters into an EMF record.
func emfRecord(typ uint32, params []byte) []byte {
	for len(params)%4 != 0 {
		params = append(params, 0)
	}
	var b rec
	b.u32(typ, uint32(8+len(params)))
	b.b.Write(params)
	return b.b.Bytes()
}

func testEMF() []byte {
	// Header: bounds, frame 0..10000 (100 mm), device 1000 px = 100 mm.
	var h rec
	h.i32(0, 0, 999, 999)
	h.i32(0, 0, 10000, 10000)
	h.u32(0x464D4520, 0x10000, 0, 0)
	h.u16(4, 0)
	h.u32(0, 0, 0)
	h.i32(1000, 1000, 100, 100)
	var body []byte
	body = append(body, emfRecord(1, h.b.Bytes())...)
	var br rec
	br.u32(1, 0, 0x000000FF, 0) // red brush in slot 1
	body = append(body, emfRecord(39, br.b.Bytes())...)
	var sel rec
	sel.u32(1)
	body = append(body, emfRecord(37, sel.b.Bytes())...)
	var np rec
	np.u32(0x80000008) // NULL_PEN
	body = append(body, emfRecord(37, np.b.Bytes())...)
	var r rec
	r.i32(100, 100, 500, 500)
	body = append(body, emfRecord(43, r.b.Bytes())...)
	// Text "Hi" at (600, 600).
	var t rec
	t.i32(0, 0, 0, 0) // bounds
	t.u32(1)          // graphics mode
	t.u32(0, 0)       // scales
	t.i32(600, 600)   // reference
	t.u32(2, 76, 0)   // chars, offString, options
	t.i32(0, 0, 0, 0) // rect
	t.u32(0)          // offDx
	t.u16('H', 'i')
	body = append(body, emfRecord(84, t.b.Bytes())...)
	var eof rec
	eof.u32(0, 16, 20)
	body = append(body, emfRecord(14, eof.b.Bytes())...)
	return body
}

func TestEMFRectangle(t *testing.T) {
	pic, err := Parse(testEMF())
	if err != nil {
		t.Fatal(err)
	}
	if int(pic.Width) != 377 { // 100 mm at 96 dpi
		t.Fatalf("width %v", pic.Width)
	}
	r := gfx.NewRaster(int(pic.Width), int(pic.Height))
	pic.Draw(r, gfx.Identity, gfx.NewFonts(false))
	img := r.Straight()
	if c := img.NRGBAAt(113, 113); c.R != 255 || c.G != 0 || c.A != 255 {
		t.Fatalf("rectangle pixel %v", c)
	}
	if c := img.NRGBAAt(10, 10); c.A != 0 {
		t.Fatalf("outside pixel %v", c)
	}
	// Text was drawn somewhere right of 600 px device = 226 output px.
	ink := false
	for y := 200; y < 240; y++ {
		for x := 226; x < 260; x++ {
			if img.NRGBAAt(x, y).A > 0 {
				ink = true
			}
		}
	}
	if !ink {
		t.Fatal("no text")
	}
}

func testWMF() []byte {
	var b rec
	// Placeable header: bbox 0,0,1000,1000 at 1000 units per inch.
	b.u32(0x9AC6CDD7).u16(0).i16(0, 0, 1000, 1000).u16(1000).u32(0)
	b.u16(0) // checksum (unchecked)
	b.u16(1, 9, 0x300).u32(0).u16(2).u32(0).u16(0)
	add := func(fn uint16, params ...int16) {
		b.u32(uint32(3 + len(params))).u16(fn)
		b.i16(params...)
	}
	add(0x02FC, 0, 0x00FF, 0x0000, 0) // blue brush: style, color (lo, hi), hatch
	add(0x012D, 0)
	add(0x041B, 600, 600, 100, 100) // rectangle bottom, right, top, left
	add(0x0000)
	return b.b.Bytes()
}

func TestWMFRectangle(t *testing.T) {
	pic, err := Parse(testWMF())
	if err != nil {
		t.Fatal(err)
	}
	if pic.Width != 96 || pic.Height != 96 {
		t.Fatalf("size %v x %v", pic.Width, pic.Height)
	}
	r := gfx.NewRaster(96, 96)
	pic.Draw(r, gfx.Identity, gfx.NewFonts(false))
	if c := r.Straight().NRGBAAt(30, 30); c.R != 255 || c.A != 255 {
		// COLORREF 0x000000FF is red: the low byte is red.
		t.Fatalf("pixel %v", c)
	}
}

func TestDIB(t *testing.T) {
	// 2×2 24-bit bottom-up: blue, green / red, white.
	var info rec
	info.u32(40).i32(2, 2).u16(1, 24).u32(0, 0, 0, 0, 0, 0)
	pixels := []byte{255, 0, 0, 0, 255, 0, 0, 0, 0, 0, 255, 255, 255, 255, 0, 0}
	img, err := decodeDIB(info.b.Bytes(), pixels, false)
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := img.At(0, 0).RGBA()
	if r>>8 != 255 {
		t.Fatalf("top-left should be red (rows are bottom-up), got %v", img.At(0, 0))
	}
}

func TestNotMetafile(t *testing.T) {
	if _, err := Parse([]byte("hello world, not a metafile at all")); err == nil {
		t.Fatal("expected an error")
	}
}
