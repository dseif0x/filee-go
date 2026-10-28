package images

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrjoshuak/go-openexr/exr"
	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/presets"
)

// fitsFile builds a FITS file: 16-bit data (unsigned through BZERO) of w × h, with extra header cards.
func fitsFile(w, h int, value func(x, y int) int, cards ...string) []byte {
	all := append([]string{"SIMPLE  =                    T", "BITPIX  =                   16", "NAXIS   =                    2",
		fmt.Sprintf("NAXIS1  = %20d", w), fmt.Sprintf("NAXIS2  = %20d", h), "BZERO   =                32768"}, cards...)
	all = append(all, "END")
	var b bytes.Buffer
	for _, c := range all {
		b.WriteString(c + strings.Repeat(" ", 80-len(c)))
	}
	for b.Len()%2880 != 0 {
		b.WriteByte(' ')
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var v [2]byte
			binary.BigEndian.PutUint16(v[:], uint16(int16(value(x, y)-32768)))
			b.Write(v[:])
		}
	}
	for b.Len()%2880 != 0 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

func TestFITS(t *testing.T) {
	data := fitsFile(16, 8, func(x, y int) int { return 1000 + x*100 + y*7 }, "BAYERPAT= 'RGGB'", "ROWORDER= 'BOTTOM-UP'")
	in := write(t, "sky.fits", data)
	// stretched and debayered for the eye
	out := run(t, step(t, in, "fits", "png", nil))
	f := decodeFile(t, out[0]).Frames[0]
	if f.Width() != 16 || f.Height() != 8 || f.Gray {
		t.Errorf("FITS → PNG %dx%d grey=%v, want 16x8 colour (debayered)", f.Width(), f.Height(), f.Gray)
	}
	out = run(t, step(t, in, "fits", "png", func(p *presets.Preset) { p.Image.FitsDebayer = false }))
	if !decodeFile(t, out[0]).Frames[0].Gray {
		t.Error("without debayering the picture should stay grey")
	}
	// TIFF: the data as it is, 16 bits, upright (bottom-up rows turned)
	out = run(t, step(t, in, "fits", "tiff", nil))
	raw, _ := os.ReadFile(out[0])
	img, err := tiff.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := img.(*image.Gray16)
	if !ok {
		t.Fatalf("FITS → TIFF is %T, want 16-bit grey", img)
	}
	// row 0 of the TIFF is the last stored row (y = 7)
	if v := g.Gray16At(3, 0).Y; v != uint16(1000+300+7*7) {
		t.Errorf("sample %d, want %d", v, 1000+300+49)
	}
}

func dicomElement(b *bytes.Buffer, group, elem uint16, value []byte) {
	binary.Write(b, binary.LittleEndian, group)
	binary.Write(b, binary.LittleEndian, elem)
	binary.Write(b, binary.LittleEndian, uint32(len(value)))
	b.Write(value)
}

func TestDICOM(t *testing.T) {
	var b bytes.Buffer
	b.Write(make([]byte, 128))
	b.WriteString("DICM")
	// meta: explicit VR little endian
	uid := []byte("1.2.840.10008.1.2\x00")
	b.Write([]byte{2, 0, 0x10, 0, 'U', 'I', byte(len(uid)), 0})
	b.Write(uid)
	u16 := func(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
	dicomElement(&b, 0x28, 0x02, u16(1))
	dicomElement(&b, 0x28, 0x04, []byte("MONOCHROME2 "))
	dicomElement(&b, 0x28, 0x10, u16(4))
	dicomElement(&b, 0x28, 0x11, u16(8))
	dicomElement(&b, 0x28, 0x100, u16(16))
	dicomElement(&b, 0x28, 0x101, u16(12))
	dicomElement(&b, 0x28, 0x102, u16(11))
	dicomElement(&b, 0x28, 0x103, u16(0))
	dicomElement(&b, 0x28, 0x1050, []byte("2000"))
	dicomElement(&b, 0x28, 0x1051, []byte("4000"))
	var px []byte
	for i := 0; i < 32; i++ {
		px = binary.LittleEndian.AppendUint16(px, uint16(i*128))
	}
	dicomElement(&b, 0x7FE0, 0x10, px)
	in := write(t, "scan.dcm", b.Bytes())
	out := run(t, step(t, in, "dcm", "png", nil))
	f := decodeFile(t, out[0]).Frames[0]
	if f.Width() != 8 || f.Height() != 4 {
		t.Fatalf("size %dx%d", f.Width(), f.Height())
	}
	// window 2000 ± 2000: value 0 is black, 3968 nearly white, 2048 mid grey
	if v := f.Img.NRGBAAt(0, 0).R; v != 0 {
		t.Errorf("first pixel %d, want 0", v)
	}
	if v := f.Img.NRGBAAt(0, 2).R; v < 120 || v > 140 {
		t.Errorf("mid pixel %d, want about 130", v)
	}
	if v := f.Img.NRGBAAt(7, 3).R; v < 245 {
		t.Errorf("last pixel %d, want nearly white", v)
	}
}

func TestEXRAndHDR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "light.exr")
	out, err := exr.NewRGBAOutputFile(path, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	img := exr.NewRGBAImage(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, float32(x)/7, 0.2140, 4, 1) // 0.214 linear ≈ 128 sRGB; 4 clips
		}
	}
	if err := out.WriteRGBA(img); err != nil {
		t.Fatal(err)
	}
	pic, err := Decode(path, "exr", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := pic.Frames[0].Img.NRGBAAt(7, 0)
	if c.R != 255 || c.G < 124 || c.G > 132 || c.B != 255 {
		t.Errorf("EXR pixel %v", c)
	}

	var h bytes.Buffer
	h.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 2 +X 3\n")
	for i := 0; i < 6; i++ {
		h.Write([]byte{128, 64, 0, 129}) // about 1, 0.5 and 0 (+ half a step)
	}
	pic, err = DecodeBytes(h.Bytes(), "hdr")
	if err != nil {
		t.Fatal(err)
	}
	c = pic.Frames[0].Img.NRGBAAt(1, 1)
	if pic.Frames[0].Width() != 3 || c.R != 255 || c.G < 185 || c.G > 190 || c.B > 15 {
		t.Errorf("HDR pixel %v", c)
	}
}

// fakeRaw is a TIFF-like camera file: a header saying orientation 6 and a
// 64 × 48 JPEG preview inside, which dcraw does not take for a camera.
func fakeRaw(t *testing.T) []byte {
	var j bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	fill(img, color.NRGBA{200, 100, 50, 255})
	jpeg.Encode(&j, img, nil)
	var b bytes.Buffer
	b.WriteString("II*\x00\x08\x00\x00\x00")
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, []uint16{tagOrientation, 3})
	binary.Write(&b, binary.LittleEndian, uint32(1))
	binary.Write(&b, binary.LittleEndian, uint16(6))
	binary.Write(&b, binary.LittleEndian, uint16(0))
	binary.Write(&b, binary.LittleEndian, uint32(0))
	b.Write(make([]byte, 100))
	b.Write(j.Bytes())
	b.Write(make([]byte, 100))
	return b.Bytes()
}

func TestRawPreviewFallback(t *testing.T) {
	in := write(t, "IMG_1.CR3", fakeRaw(t))
	out := run(t, step(t, in, "raw", "png", nil))
	f := decodeFile(t, out[0]).Frames[0]
	// the preview is turned upright by the RAW file's orientation
	if f.Width() != 48 || f.Height() != 64 {
		t.Errorf("RAW preview %dx%d, want 48x64", f.Width(), f.Height())
	}
}

// tinyDNG is an uncompressed 16-bit RGGB DNG of 32 × 32 that dcraw develops.
func tinyDNG() []byte {
	const w, h = 32, 32
	type entry struct {
		tag, typ uint16
		count    uint32
		value    []byte
	}
	le := binary.LittleEndian
	u16 := func(v ...uint16) []byte {
		var b []byte
		for _, x := range v {
			b = le.AppendUint16(b, x)
		}
		return b
	}
	u32 := func(v ...uint32) []byte {
		var b []byte
		for _, x := range v {
			b = le.AppendUint32(b, x)
		}
		return b
	}
	srat := func(v ...int32) []byte {
		var b []byte
		for _, x := range v {
			b = le.AppendUint32(b, uint32(x))
			b = le.AppendUint32(b, 10000)
		}
		return b
	}
	pixels := make([]byte, 0, w*h*2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16(1000 + 40*x + 20*y)
			pixels = le.AppendUint16(pixels, v)
		}
	}
	model := []byte("Filee Test\x00")
	entries := []entry{
		{254, 4, 1, u32(0)},
		{256, 4, 1, u32(w)},
		{257, 4, 1, u32(h)},
		{258, 3, 1, u16(16)},
		{259, 3, 1, u16(1)},
		{262, 3, 1, u16(32803)},
		{271, 2, uint32(len("Filee\x00")), []byte("Filee\x00")},
		{272, 2, uint32(len(model)), model},
		{273, 4, 1, nil}, // strip offset, filled below
		{277, 3, 1, u16(1)},
		{278, 4, 1, u32(h)},
		{279, 4, 1, u32(uint32(len(pixels)))},
		{284, 3, 1, u16(1)},
		{33421, 3, 2, u16(2, 2)},
		{33422, 1, 4, []byte{0, 1, 1, 2}},
		{50706, 1, 4, []byte{1, 4, 0, 0}},
		{50708, 2, uint32(len(model)), model},
		{50717, 4, 1, u32(65535)},
		{50721, 10, 9, srat(10000, 0, 0, 0, 10000, 0, 0, 0, 10000)},
		{50728, 5, 3, u32(1, 1, 1, 1, 1, 1)},
	}
	var data bytes.Buffer
	ifdSize := 2 + len(entries)*12 + 4
	extraStart := 8 + ifdSize
	var extra bytes.Buffer
	offsets := make([][]byte, len(entries))
	for i, e := range entries {
		if e.tag == 273 {
			continue
		}
		if len(e.value) > 4 {
			offsets[i] = u32(uint32(extraStart + extra.Len()))
			extra.Write(e.value)
			if extra.Len()%2 == 1 {
				extra.WriteByte(0)
			}
		}
	}
	pixelStart := extraStart + extra.Len()
	data.WriteString("II*\x00")
	data.Write(u32(8))
	data.Write(u16(uint16(len(entries))))
	for i, e := range entries {
		data.Write(u16(e.tag, e.typ))
		data.Write(u32(e.count))
		v := e.value
		switch {
		case e.tag == 273:
			v = u32(uint32(pixelStart))
		case offsets[i] != nil:
			v = offsets[i]
		}
		v = append(append([]byte{}, v...), 0, 0, 0, 0)
		data.Write(v[:4])
	}
	data.Write(u32(0))
	data.Write(extra.Bytes())
	data.Write(pixels)
	return data.Bytes()
}

func TestRawDevelopedByDcraw(t *testing.T) {
	ppm, err := developRaw(t.Context(), tinyDNG())
	if err != nil {
		t.Fatalf("dcraw: %v", err)
	}
	p, err := decodeNetpbm(ppm)
	if err != nil {
		t.Fatal(err)
	}
	if f := p.Frames[0]; f.Width() != 32 || f.Height() != 32 {
		t.Errorf("developed %dx%d, want 32x32", f.Width(), f.Height())
	}
}

func TestXCF(t *testing.T) {
	// GIMP "file" version, RGB, no compression, one 2 × 2 RGBA layer at (1, 1)
	be := binary.BigEndian
	var b bytes.Buffer
	b.WriteString("gimp xcf file\x00")
	b.Write(be.AppendUint32(nil, 4))
	b.Write(be.AppendUint32(nil, 3))
	b.Write(be.AppendUint32(nil, 0))
	b.Write(be.AppendUint32(nil, 17)) // compression: none
	b.Write(be.AppendUint32(nil, 1))
	b.WriteByte(0)
	b.Write(make([]byte, 8)) // end of properties
	layerPtr := b.Len()
	b.Write(make([]byte, 4)) // layer pointer, set below
	b.Write(make([]byte, 4)) // end of layers
	b.Write(make([]byte, 4)) // end of channels
	layer := b.Len()
	be.PutUint32(b.Bytes()[layerPtr:], uint32(layer))
	b.Write(be.AppendUint32(nil, 2))
	b.Write(be.AppendUint32(nil, 2))
	b.Write(be.AppendUint32(nil, 1)) // RGBA
	b.Write(be.AppendUint32(nil, 2))
	b.WriteString("L\x00")
	b.Write(be.AppendUint32(nil, 15)) // offsets
	b.Write(be.AppendUint32(nil, 8))
	b.Write(be.AppendUint32(nil, 1))
	b.Write(be.AppendUint32(nil, 1))
	b.Write(make([]byte, 8))
	hier := b.Len() + 8
	b.Write(be.AppendUint32(nil, uint32(hier)))
	b.Write(be.AppendUint32(nil, 0)) // no mask
	b.Write(be.AppendUint32(nil, 2))
	b.Write(be.AppendUint32(nil, 2))
	b.Write(be.AppendUint32(nil, 4))
	level := b.Len() + 8
	b.Write(be.AppendUint32(nil, uint32(level)))
	b.Write(be.AppendUint32(nil, 0))
	b.Write(be.AppendUint32(nil, 2))
	b.Write(be.AppendUint32(nil, 2))
	tile := b.Len() + 8
	b.Write(be.AppendUint32(nil, uint32(tile)))
	b.Write(be.AppendUint32(nil, 0))
	for i := 0; i < 4; i++ {
		b.Write([]byte{255, 0, 0, 255})
	}
	pic, err := DecodeBytes(b.Bytes(), "xcf")
	if err != nil {
		t.Fatal(err)
	}
	img := pic.Frames[0].Img
	if img.Rect.Dx() != 4 || img.Rect.Dy() != 3 {
		t.Fatalf("canvas %v", img.Rect)
	}
	if c := img.NRGBAAt(1, 1); c != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("layer pixel %v", c)
	}
	if c := img.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("outside the layer %v, want transparent", c)
	}
}

func TestPhotoCD(t *testing.T) {
	data := make([]byte, pcdBaseOffset+pcdW*pcdH*3/2)
	copy(data[0x800:], "PCD_IPI")
	for i := pcdBaseOffset; i < len(data); i++ {
		data[i] = 128
	}
	pic, err := DecodeBytes(data, "pcd")
	if err != nil {
		t.Fatal(err)
	}
	if f := pic.Frames[0]; f.Width() != 768 || f.Height() != 512 {
		t.Errorf("Photo CD %dx%d", f.Width(), f.Height())
	}
}

func TestPSDFromLayers(t *testing.T) {
	// a PSD saved without its composite ("maximize compatibility" off) is composed from its layers
	f := NewFrame(testPicture(true))
	data, err := encodePSD(f, false)
	if err != nil {
		t.Fatal(err)
	}
	// version info resource saying there is no real composite, and a white composite
	i := bytes.Index(data, []byte("8BIM"))
	_ = i
	res := []byte("8BIM\x04\x21\x00\x00\x00\x00\x00\x05\x00\x00\x00\x01\x00\x00")
	head := 26 + 4
	resLen := int(binary.BigEndian.Uint32(data[head:]))
	patched := append([]byte{}, data[:head]...)
	patched = binary.BigEndian.AppendUint32(patched, uint32(resLen+len(res)))
	patched = append(patched, data[head+4:head+4+resLen]...)
	patched = append(patched, res...)
	patched = append(patched, data[head+4+resLen:]...)
	pic, err := DecodeBytes(patched, "psd")
	if err != nil {
		t.Fatal(err)
	}
	if d := meanDiff(testPicture(true), pic.Frames[0].Img); d > 0.5 {
		t.Errorf("composed layers differ by %v", d)
	}
}

func TestBMPKinds(t *testing.T) {
	// 4-bit RLE bitmap: two rows of 4 pixels
	pal := []byte{0, 0, 0, 0, 255, 255, 255, 0}
	rle := []byte{4, 0x10, 0, 0, 4, 0x01, 0, 1}
	var b bytes.Buffer
	b.WriteString("BM")
	b.Write(binary.LittleEndian.AppendUint32(nil, uint32(14+40+len(pal)+len(rle))))
	b.Write(make([]byte, 4))
	b.Write(binary.LittleEndian.AppendUint32(nil, uint32(14+40+len(pal))))
	h := make([]byte, 40)
	binary.LittleEndian.PutUint32(h, 40)
	binary.LittleEndian.PutUint32(h[4:], 4)
	binary.LittleEndian.PutUint32(h[8:], 2)
	binary.LittleEndian.PutUint16(h[12:], 1)
	binary.LittleEndian.PutUint16(h[14:], 4)
	binary.LittleEndian.PutUint32(h[16:], 2)
	binary.LittleEndian.PutUint32(h[32:], 2)
	b.Write(h)
	b.Write(pal)
	b.Write(rle)
	pic, err := DecodeBytes(b.Bytes(), "bmp")
	if err != nil {
		t.Fatal(err)
	}
	img := pic.Frames[0].Img
	// bottom row (stored first): white, black, white, black
	if img.NRGBAAt(0, 1).R != 255 || img.NRGBAAt(1, 1).R != 0 || img.NRGBAAt(0, 0).R != 0 || img.NRGBAAt(1, 0).R != 255 {
		t.Errorf("RLE4 pixels %v", img.Pix)
	}
}

func TestPCXPalette(t *testing.T) {
	head := make([]byte, 128)
	head[0], head[1], head[2], head[3] = 10, 5, 1, 8
	binary.LittleEndian.PutUint16(head[8:], 1)
	binary.LittleEndian.PutUint16(head[10:], 0)
	head[65] = 1
	binary.LittleEndian.PutUint16(head[66:], 2)
	data := append(head, 0xC2, 7) // two pixels of colour 7
	pal := make([]byte, 769)
	pal[0] = 0x0C
	pal[1+7*3], pal[2+7*3], pal[3+7*3] = 10, 20, 30
	data = append(data, pal...)
	pic, err := DecodeBytes(data, "pcx")
	if err != nil {
		t.Fatal(err)
	}
	if c := pic.Frames[0].Img.NRGBAAt(1, 0); c != (color.NRGBA{10, 20, 30, 255}) {
		t.Errorf("PCX pixel %v", c)
	}
}

func TestMIFFFromImageMagick(t *testing.T) {
	data := []byte("id=ImageMagick  version=1.0\nclass=PseudoClass  colors=2  alpha-trait=Undefined\ncompression=RLE  columns=3  rows=1  depth=8\n\f\n:\x1a" +
		"\x00\x00\x00\xFF\xFF\xFF" + "\x01\x02" + "\x00\x00")
	pic, err := DecodeBytes(data, "miff")
	if err != nil {
		t.Fatal(err)
	}
	img := pic.Frames[0].Img
	if img.NRGBAAt(0, 0).R != 255 || img.NRGBAAt(2, 0).R != 255 || img.NRGBAAt(2, 0).A != 255 {
		t.Errorf("MIFF pixels %v", img.Pix)
	}
}

func TestLinearToSRGB(t *testing.T) {
	if v := linearToSRGB8(0.214); math.Abs(float64(v)-128) > 1 {
		t.Errorf("0.214 → %d", v)
	}
}
