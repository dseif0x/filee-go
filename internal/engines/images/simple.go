package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"regexp"
	"strconv"
	"strings"
)

// Small raster formats read and written by Filee itself: TGA, QOI, PCX, WBMP,
// XBM, XPM, SGI and Sun Raster.

// ---- TGA ----

func decodeTGA(data []byte) (*Picture, error) {
	if len(data) < 18 {
		return nil, errors.New("the file is not a TGA image")
	}
	le := binary.LittleEndian
	idLen, cmapType, kind := int(data[0]), data[1], data[2]
	cmapFirst, cmapLen, cmapBits := int(le.Uint16(data[3:])), int(le.Uint16(data[5:])), int(data[7])
	w, h, bpp, desc := int(le.Uint16(data[12:])), int(le.Uint16(data[14:])), int(data[16]), data[17]
	if w == 0 || h == 0 {
		return nil, errors.New("the TGA image is empty")
	}
	base := kind & 7
	rle := kind&8 != 0
	if base < 1 || base > 3 {
		return nil, fmt.Errorf("TGA image type %d is not supported", kind)
	}
	p := 18 + idLen
	var palette [][4]uint8
	if cmapType == 1 {
		eb := (cmapBits + 7) / 8
		for i := 0; i < cmapLen; i++ {
			if p+eb > len(data) {
				return nil, errors.New("the TGA file is cut short")
			}
			palette = append(palette, tgaColor(data[p:], cmapBits))
			p += eb
		}
	}
	pb := (bpp + 7) / 8
	if pb == 0 || pb > 4 {
		return nil, errors.New("the TGA file is damaged (pixel size)")
	}
	n := w * h
	pixels := make([]byte, n*pb)
	if rle {
		for i := 0; i < n && p < len(data); {
			c := data[p]
			p++
			count := int(c&0x7F) + 1
			if c&0x80 != 0 {
				if p+pb > len(data) {
					break
				}
				for k := 0; k < count && i < n; k++ {
					copy(pixels[i*pb:], data[p:p+pb])
					i++
				}
				p += pb
			} else {
				for k := 0; k < count && i < n; k++ {
					if p+pb > len(data) {
						break
					}
					copy(pixels[i*pb:], data[p:p+pb])
					p += pb
					i++
				}
			}
		}
	} else {
		if p+len(pixels) > len(data) {
			return nil, errors.New("the TGA file is cut short")
		}
		copy(pixels, data[p:])
	}
	alphaBits := int(desc & 15)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	topDown := desc&0x20 != 0
	rightToLeft := desc&0x10 != 0
	alpha := false
	for i := 0; i < n; i++ {
		x, y := i%w, i/w
		if !topDown {
			y = h - 1 - y
		}
		if rightToLeft {
			x = w - 1 - x
		}
		s := pixels[i*pb:]
		var c [4]uint8
		switch base {
		case 1:
			idx := int(s[0])
			if pb == 2 {
				idx = int(le.Uint16(s))
			}
			idx -= cmapFirst
			if idx >= 0 && idx < len(palette) {
				c = palette[idx]
			} else {
				c = [4]uint8{0, 0, 0, 255}
			}
		case 2:
			c = tgaColor(s, bpp)
			if bpp == 32 && alphaBits == 0 {
				c[3] = 255
			}
			if bpp == 16 && alphaBits == 0 {
				c[3] = 255
			}
		case 3:
			c = [4]uint8{s[0], s[0], s[0], 255}
			if pb == 2 {
				c[3] = s[1]
			}
		}
		if c[3] != 255 {
			alpha = true
		}
		o := y*img.Stride + x*4
		img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c[0], c[1], c[2], c[3]
	}
	return one(&Frame{Img: img, Alpha: alpha, Gray: base == 3}), nil
}

func tgaColor(s []byte, bits int) [4]uint8 {
	switch bits {
	case 15, 16:
		v := uint16(s[0]) | uint16(s[1])<<8
		c := [4]uint8{uint8((v >> 10 & 31) * 255 / 31), uint8((v >> 5 & 31) * 255 / 31), uint8((v & 31) * 255 / 31), 255}
		if bits == 16 && v&0x8000 == 0 {
			c[3] = 0
		}
		return c
	case 24:
		return [4]uint8{s[2], s[1], s[0], 255}
	case 32:
		return [4]uint8{s[2], s[1], s[0], s[3]}
	}
	return [4]uint8{s[0], s[0], s[0], 255}
}

// encodeTGA writes an RLE-compressed true-colour (or grey) TGA, bottom-up.
func encodeTGA(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	pb, kind, desc := 3, byte(10), byte(0)
	switch {
	case f.Gray && !f.Alpha:
		pb, kind = 1, 11
	case f.Alpha:
		pb, desc = 4, 8
	}
	var b bytes.Buffer
	head := make([]byte, 18)
	head[2] = kind
	binary.LittleEndian.PutUint16(head[12:], uint16(w))
	binary.LittleEndian.PutUint16(head[14:], uint16(h))
	head[16] = byte(pb * 8)
	head[17] = desc
	b.Write(head)
	px := func(x, y int) []byte {
		s := f.Img.Pix[y*f.Img.Stride+x*4:]
		switch pb {
		case 1:
			return []byte{s[0]}
		case 3:
			return []byte{s[2], s[1], s[0]}
		}
		return []byte{s[2], s[1], s[0], s[3]}
	}
	for y := h - 1; y >= 0; y-- {
		row := make([][]byte, w)
		for x := 0; x < w; x++ {
			row[x] = px(x, y)
		}
		// runs never cross rows (as the specification asks)
		for x := 0; x < w; {
			run := 1
			for x+run < w && run < 128 && bytes.Equal(row[x+run], row[x]) {
				run++
			}
			if run > 1 {
				b.WriteByte(0x80 | byte(run-1))
				b.Write(row[x])
				x += run
				continue
			}
			lit := 1
			for x+lit < w && lit < 128 && (x+lit+1 >= w || !bytes.Equal(row[x+lit], row[x+lit+1])) {
				lit++
			}
			b.WriteByte(byte(lit - 1))
			for k := 0; k < lit; k++ {
				b.Write(row[x+k])
			}
			x += lit
		}
	}
	b.WriteString("\x00\x00\x00\x00\x00\x00\x00\x00TRUEVISION-XFILE.\x00")
	return b.Bytes()
}

// ---- QOI ----

func qoiHash(c [4]uint8) int { return (int(c[0])*3 + int(c[1])*5 + int(c[2])*7 + int(c[3])*11) % 64 }

func decodeQOI(data []byte) (*Picture, error) {
	if len(data) < 14 || string(data[:4]) != "qoif" {
		return nil, errors.New("the file is not a QOI image")
	}
	w, h := int(binary.BigEndian.Uint32(data[4:])), int(binary.BigEndian.Uint32(data[8:]))
	channels := data[12]
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 400_000_000 {
		return nil, errors.New("the QOI image has an impossible size")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var index [64][4]uint8
	px := [4]uint8{0, 0, 0, 255}
	p, run := 14, 0
	for i := 0; i < w*h*4; i += 4 {
		if run > 0 {
			run--
		} else if p < len(data) {
			b := data[p]
			p++
			switch {
			case b == 0xFE && p+3 <= len(data):
				px[0], px[1], px[2] = data[p], data[p+1], data[p+2]
				p += 3
			case b == 0xFF && p+4 <= len(data):
				px = [4]uint8{data[p], data[p+1], data[p+2], data[p+3]}
				p += 4
			case b>>6 == 0:
				px = index[b]
			case b>>6 == 1:
				px[0] += (b>>4)&3 - 2
				px[1] += (b>>2)&3 - 2
				px[2] += b&3 - 2
			case b>>6 == 2 && p < len(data):
				dg := b&63 - 32
				b2 := data[p]
				p++
				px[0] += dg - 8 + (b2 >> 4)
				px[1] += dg
				px[2] += dg - 8 + (b2 & 15)
			case b>>6 == 3:
				run = int(b & 63)
			}
			index[qoiHash(px)] = px
		}
		copy(img.Pix[i:i+4], px[:])
	}
	return one(&Frame{Img: img, Alpha: channels == 4 && !opaque(img)}), nil
}

func encodeQOI(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	var b bytes.Buffer
	b.WriteString("qoif")
	head := make([]byte, 10)
	binary.BigEndian.PutUint32(head, uint32(w))
	binary.BigEndian.PutUint32(head[4:], uint32(h))
	head[8] = 3
	if f.Alpha {
		head[8] = 4
	}
	b.Write(head)
	var index [64][4]uint8
	prev := [4]uint8{0, 0, 0, 255}
	run := 0
	n := w * h
	for i := 0; i < n; i++ {
		x, y := i%w, i/w
		var px [4]uint8
		copy(px[:], f.Img.Pix[y*f.Img.Stride+x*4:])
		if !f.Alpha {
			px[3] = 255
		}
		if px == prev {
			run++
			if run == 62 || i == n-1 {
				b.WriteByte(0xC0 | byte(run-1))
				run = 0
			}
			continue
		}
		if run > 0 {
			b.WriteByte(0xC0 | byte(run-1))
			run = 0
		}
		hidx := qoiHash(px)
		switch {
		case index[hidx] == px:
			b.WriteByte(byte(hidx))
		case px[3] == prev[3]:
			dr, dg, db := int8(px[0]-prev[0]), int8(px[1]-prev[1]), int8(px[2]-prev[2])
			dgr, dgb := dr-dg, db-dg
			switch {
			case dr >= -2 && dr <= 1 && dg >= -2 && dg <= 1 && db >= -2 && db <= 1:
				b.WriteByte(0x40 | byte(dr+2)<<4 | byte(dg+2)<<2 | byte(db+2))
			case dg >= -32 && dg <= 31 && dgr >= -8 && dgr <= 7 && dgb >= -8 && dgb <= 7:
				b.WriteByte(0x80 | byte(dg+32))
				b.WriteByte(byte(dgr+8)<<4 | byte(dgb+8))
			default:
				b.Write([]byte{0xFE, px[0], px[1], px[2]})
			}
		default:
			b.Write([]byte{0xFF, px[0], px[1], px[2], px[3]})
		}
		index[hidx] = px
		prev = px
	}
	b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	return b.Bytes()
}

// ---- PCX ----

func decodePCX(data []byte) (*Picture, error) {
	if len(data) < 128 || data[0] != 0x0A {
		return nil, errors.New("the file is not a PCX image")
	}
	le := binary.LittleEndian
	bits := int(data[3])
	w := int(le.Uint16(data[8:])) - int(le.Uint16(data[4:])) + 1
	h := int(le.Uint16(data[10:])) - int(le.Uint16(data[6:])) + 1
	planes := int(data[65])
	bpl := int(le.Uint16(data[66:]))
	if w <= 0 || h <= 0 || planes < 1 || planes > 4 || bpl <= 0 {
		return nil, errors.New("the PCX file is damaged (header)")
	}
	lineBytes := bpl * planes
	raw := make([]byte, lineBytes*h)
	p := 128
	for i := 0; i < len(raw) && p < len(data); {
		c := data[p]
		p++
		if data[2] == 1 && c&0xC0 == 0xC0 {
			n := int(c & 0x3F)
			if p >= len(data) {
				break
			}
			v := data[p]
			p++
			for k := 0; k < n && i < len(raw); k++ {
				raw[i] = v
				i++
			}
		} else {
			raw[i] = c
			i++
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var palette [][3]uint8
	if bits == 8 && planes == 1 {
		if len(data) >= 769 && data[len(data)-769] == 0x0C {
			pal := data[len(data)-768:]
			for i := 0; i < 256; i++ {
				palette = append(palette, [3]uint8{pal[i*3], pal[i*3+1], pal[i*3+2]})
			}
		} else {
			for i := 0; i < 256; i++ {
				palette = append(palette, [3]uint8{uint8(i), uint8(i), uint8(i)})
			}
		}
	} else {
		for i := 0; i < 16; i++ {
			palette = append(palette, [3]uint8{data[16+i*3], data[17+i*3], data[18+i*3]})
		}
		if bits == 1 && planes == 1 {
			palette = [][3]uint8{{0, 0, 0}, {255, 255, 255}}
		}
	}
	gray := false
	for y := 0; y < h; y++ {
		line := raw[y*lineBytes:]
		for x := 0; x < w; x++ {
			var c [4]uint8
			switch {
			case planes >= 3 && bits == 8:
				c = [4]uint8{line[x], line[bpl+x], line[2*bpl+x], 255}
				if planes == 4 {
					c[3] = line[3*bpl+x]
				}
			case bits == 8:
				pc := palette[line[x]]
				c = [4]uint8{pc[0], pc[1], pc[2], 255}
			default:
				idx := 0
				for pl := 0; pl < planes; pl++ {
					bit := x * bits
					v := int(line[pl*bpl+bit/8]>>(8-bits-bit%8)) & (1<<bits - 1)
					idx |= v << (pl * bits)
				}
				pc := [3]uint8{}
				if idx < len(palette) {
					pc = palette[idx]
				}
				c = [4]uint8{pc[0], pc[1], pc[2], 255}
			}
			o := y*img.Stride + x*4
			copy(img.Pix[o:o+4], c[:])
		}
	}
	if bits == 1 && planes == 1 {
		gray = true
	}
	return one(&Frame{Img: img, Gray: gray, Alpha: planes == 4 && !opaque(img)}), nil
}

// encodePCX writes 24-bit PCX (three RLE planes) with 300 dpi in the header
// fields when no resolution is known.
func encodePCX(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	bpl := (w + 1) &^ 1
	head := make([]byte, 128)
	head[0], head[1], head[2], head[3] = 0x0A, 5, 1, 8
	le := binary.LittleEndian
	le.PutUint16(head[8:], uint16(w-1))
	le.PutUint16(head[10:], uint16(h-1))
	dpi := uint16(72)
	if f.Meta.DpiX > 0 {
		dpi = uint16(min(65535, f.Meta.DpiX+0.5))
	}
	le.PutUint16(head[12:], dpi)
	le.PutUint16(head[14:], dpi)
	head[65] = 3
	le.PutUint16(head[66:], uint16(bpl))
	le.PutUint16(head[68:], 1)
	var b bytes.Buffer
	b.Write(head)
	line := make([]byte, bpl)
	for y := 0; y < h; y++ {
		row := f.Img.Pix[y*f.Img.Stride:]
		for c := 0; c < 3; c++ {
			clear(line)
			for x := 0; x < w; x++ {
				line[x] = row[x*4+c]
			}
			for i := 0; i < bpl; {
				run := 1
				for i+run < bpl && run < 63 && line[i+run] == line[i] {
					run++
				}
				if run > 1 || line[i]&0xC0 == 0xC0 {
					b.WriteByte(0xC0 | byte(run))
				}
				b.WriteByte(line[i])
				i += run
			}
		}
	}
	return b.Bytes()
}

// ---- WBMP ----

func readMultiByte(data []byte, p *int) (int, bool) {
	v := 0
	for k := 0; k < 5 && *p < len(data); k++ {
		c := data[*p]
		*p++
		v = v<<7 | int(c&0x7F)
		if c&0x80 == 0 {
			return v, true
		}
	}
	return 0, false
}

func decodeWBMP(data []byte) (*Picture, error) {
	p := 0
	typ, ok := readMultiByte(data, &p)
	if !ok || typ != 0 || p >= len(data) {
		return nil, errors.New("the file is not a WBMP image")
	}
	p++ // fixed header
	w, ok1 := readMultiByte(data, &p)
	h, ok2 := readMultiByte(data, &p)
	if !ok1 || !ok2 || w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<30 {
		return nil, errors.New("the WBMP file is damaged")
	}
	stride := (w + 7) / 8
	if p+stride*h > len(data) {
		return nil, errors.New("the WBMP file is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := data[p+y*stride:]
		for x := 0; x < w; x++ {
			v := uint8(0)
			if row[x/8]>>(7-x%8)&1 == 1 {
				v = 255
			}
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = v, v, v, 255
		}
	}
	return one(&Frame{Img: img, Gray: true}), nil
}

func writeMultiByte(b *bytes.Buffer, v int) {
	var tmp []byte
	tmp = append(tmp, byte(v&0x7F))
	for v >>= 7; v > 0; v >>= 7 {
		tmp = append(tmp, byte(v&0x7F)|0x80)
	}
	for i := len(tmp) - 1; i >= 0; i-- {
		b.WriteByte(tmp[i])
	}
}

func encodeWBMP(f *Frame) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0})
	w, h := f.Width(), f.Height()
	writeMultiByte(&b, w)
	writeMultiByte(&b, h)
	stride := (w + 7) / 8
	row := make([]byte, stride)
	for y := 0; y < h; y++ {
		clear(row)
		for x := 0; x < w; x++ {
			s := f.Img.Pix[y*f.Img.Stride+x*4:]
			if luma(s[0], s[1], s[2]) >= 128 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(row)
	}
	return b.Bytes()
}

// ---- XBM ----

var (
	xbmDefine = regexp.MustCompile(`#define\s+\S*?(width|height)\s+(\d+)`)
	xbmBytes  = regexp.MustCompile(`0[xX]([0-9a-fA-F]{1,4})`)
)

func decodeXBM(data []byte) (*Picture, error) {
	w, h := 0, 0
	for _, m := range xbmDefine.FindAllSubmatch(data, -1) {
		v, _ := strconv.Atoi(string(m[2]))
		if string(m[1]) == "width" && w == 0 {
			w = v
		} else if string(m[1]) == "height" && h == 0 {
			h = v
		}
	}
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<28 {
		return nil, errors.New("the file is not an XBM image")
	}
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return nil, errors.New("the XBM file has no pixel data")
	}
	var vals []byte
	short := bytes.Contains(data[:start], []byte("short"))
	for _, m := range xbmBytes.FindAllSubmatch(data[start:], -1) {
		v, _ := strconv.ParseUint(string(m[1]), 16, 16)
		if short {
			vals = append(vals, byte(v), byte(v>>8))
		} else {
			vals = append(vals, byte(v))
		}
	}
	stride := (w + 7) / 8
	if short {
		stride = (w + 15) / 16 * 2
	}
	if len(vals) < stride*h {
		return nil, errors.New("the XBM file is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(255)
			if vals[y*stride+x/8]>>(x%8)&1 == 1 {
				v = 0
			}
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = v, v, v, 255
		}
	}
	return one(&Frame{Img: img, Gray: true}), nil
}

func encodeXBM(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	var b bytes.Buffer
	fmt.Fprintf(&b, "#define image_width %d\n#define image_height %d\nstatic char image_bits[] = {\n", w, h)
	stride := (w + 7) / 8
	n := 0
	for y := 0; y < h; y++ {
		for bx := 0; bx < stride; bx++ {
			var v byte
			for k := 0; k < 8; k++ {
				x := bx*8 + k
				if x >= w {
					break
				}
				s := f.Img.Pix[y*f.Img.Stride+x*4:]
				if luma(s[0], s[1], s[2]) < 128 {
					v |= 1 << k
				}
			}
			if n > 0 {
				b.WriteString(",")
				if n%12 == 0 {
					b.WriteString("\n")
				}
			}
			fmt.Fprintf(&b, " 0x%02X", v)
			n++
		}
	}
	b.WriteString("};\n")
	return b.Bytes()
}

// ---- XPM ----

var xpmNames = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255}, "green": {0, 255, 0, 255},
	"blue": {0, 0, 255, 255}, "yellow": {255, 255, 0, 255}, "cyan": {0, 255, 255, 255}, "magenta": {255, 0, 255, 255},
	"gray": {190, 190, 190, 255}, "grey": {190, 190, 190, 255}, "orange": {255, 165, 0, 255}, "purple": {160, 32, 240, 255},
	"brown": {165, 42, 42, 255}, "pink": {255, 192, 203, 255}, "gray50": {127, 127, 127, 255}, "navy": {0, 0, 128, 255},
	"darkgray": {169, 169, 169, 255}, "lightgray": {211, 211, 211, 255}, "maroon": {176, 48, 96, 255},
}

var xpmString = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

func decodeXPM(data []byte) (*Picture, error) {
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return nil, errors.New("the file is not an XPM image")
	}
	var lines []string
	for _, m := range xpmString.FindAllSubmatch(data[start:], -1) {
		lines = append(lines, string(m[1]))
	}
	if len(lines) == 0 {
		return nil, errors.New("the XPM file is empty")
	}
	var w, h, ncolors, cpp int
	if n, _ := fmt.Sscan(lines[0], &w, &h, &ncolors, &cpp); n < 4 || w <= 0 || h <= 0 || cpp <= 0 || cpp > 8 || int64(w)*int64(h) > 1<<28 {
		return nil, errors.New("the XPM file is damaged (values line)")
	}
	if len(lines) < 1+ncolors+h {
		return nil, errors.New("the XPM file is cut short")
	}
	colors := map[string]color.NRGBA{}
	alpha := false
	for _, l := range lines[1 : 1+ncolors] {
		if len(l) < cpp {
			continue
		}
		key := l[:cpp]
		fields := strings.Fields(l[cpp:])
		var spec string
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "c" {
				spec = strings.Join(fields[i+1:], " ")
				// up to the next key
				for _, k := range []string{" m ", " g ", " g4 ", " s "} {
					if j := strings.Index(" "+spec+" ", k); j >= 0 {
						spec = strings.TrimSpace(spec[:max(0, j-1)])
					}
				}
				break
			}
		}
		if spec == "" && len(fields) >= 2 {
			spec = fields[len(fields)-1]
		}
		c, ok := ParseColor(spec)
		if strings.EqualFold(spec, "none") {
			c, ok = color.NRGBA{}, true
			alpha = true
		}
		if !ok {
			if nc, found := xpmNames[strings.ToLower(strings.ReplaceAll(spec, " ", ""))]; found {
				c = nc
			} else if len(spec) == 13 && spec[0] == '#' { // #RRRRGGGGBBBB
				r, _ := strconv.ParseUint(spec[1:3], 16, 8)
				g, _ := strconv.ParseUint(spec[5:7], 16, 8)
				bb, _ := strconv.ParseUint(spec[9:11], 16, 8)
				c = color.NRGBA{uint8(r), uint8(g), uint8(bb), 255}
			} else {
				c = color.NRGBA{0, 0, 0, 255}
			}
		}
		colors[key] = c
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		l := lines[1+ncolors+y]
		for x := 0; x < w && (x+1)*cpp <= len(l); x++ {
			c := colors[l[x*cpp:(x+1)*cpp]]
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c.R, c.G, c.B, c.A
		}
	}
	return one(&Frame{Img: img, Alpha: alpha}), nil
}

const xpmChars = " .XoO+@#$%&*=-;:>,<1234567890qwertyuipasdfghjklzxcvbnmMNBVCZASDFGHJKLPIUYTREWQ!~^/()_`'][{}|"

func encodeXPM(f *Frame) []byte {
	pal := quantize(f.Img, 256)
	n := len(pal.Palette)
	cpp := 1
	if n > len(xpmChars) {
		cpp = 2
	}
	key := func(i int) string {
		if cpp == 1 {
			return string(xpmChars[i])
		}
		return string(xpmChars[i/len(xpmChars)]) + string(xpmChars[i%len(xpmChars)])
	}
	var b bytes.Buffer
	w, h := f.Width(), f.Height()
	fmt.Fprintf(&b, "/* XPM */\nstatic char *image[] = {\n/* columns rows colors chars-per-pixel */\n\"%d %d %d %d \",\n", w, h, n, cpp)
	for i, c := range pal.Palette {
		nc := c.(color.NRGBA)
		if nc.A == 0 {
			fmt.Fprintf(&b, "\"%s c None\",\n", key(i))
		} else {
			fmt.Fprintf(&b, "\"%s c #%02X%02X%02X\",\n", key(i), nc.R, nc.G, nc.B)
		}
	}
	b.WriteString("/* pixels */\n")
	for y := 0; y < h; y++ {
		b.WriteByte('"')
		for x := 0; x < w; x++ {
			b.WriteString(key(int(pal.Pix[y*pal.Stride+x])))
		}
		if y < h-1 {
			b.WriteString("\",\n")
		} else {
			b.WriteString("\"\n")
		}
	}
	b.WriteString("};\n")
	return b.Bytes()
}

// ---- SGI ----

func decodeSGI(data []byte) (*Picture, error) {
	if len(data) < 512 || binary.BigEndian.Uint16(data) != 474 {
		return nil, errors.New("the file is not an SGI image")
	}
	be := binary.BigEndian
	storage, bpc := data[2], int(data[3])
	w, h, z := int(be.Uint16(data[6:])), int(be.Uint16(data[8:])), int(be.Uint16(data[10:]))
	if be.Uint16(data[4:]) == 1 {
		h, z = 1, 1
	} else if be.Uint16(data[4:]) == 2 {
		z = 1
	}
	if w <= 0 || h <= 0 || z <= 0 || (bpc != 1 && bpc != 2) {
		return nil, errors.New("the SGI file is damaged (header)")
	}
	planes := make([][]uint16, z)
	for c := range planes {
		planes[c] = make([]uint16, w*h)
	}
	if storage == 1 {
		tab := 512
		if tab+h*z*8 > len(data) {
			return nil, errors.New("the SGI file is cut short")
		}
		for c := 0; c < z; c++ {
			for y := 0; y < h; y++ {
				off := int(be.Uint32(data[tab+(c*h+y)*4:]))
				row := planes[c][y*w : (y+1)*w]
				x := 0
				for off+bpc <= len(data) && x < w {
					var v int
					if bpc == 1 {
						v = int(data[off])
					} else {
						v = int(be.Uint16(data[off:]))
					}
					off += bpc
					count := v & 0x7F
					if count == 0 {
						break
					}
					if v&0x80 != 0 {
						for k := 0; k < count && x < w && off+bpc <= len(data); k++ {
							row[x] = sgiSample(data[off:], bpc)
							off += bpc
							x++
						}
					} else {
						if off+bpc > len(data) {
							break
						}
						s := sgiSample(data[off:], bpc)
						off += bpc
						for k := 0; k < count && x < w; k++ {
							row[x] = s
							x++
						}
					}
				}
			}
		}
	} else {
		if 512+w*h*z*bpc > len(data) {
			return nil, errors.New("the SGI file is cut short")
		}
		for c := 0; c < z; c++ {
			for i := 0; i < w*h; i++ {
				planes[c][i] = sgiSample(data[512+(c*w*h+i)*bpc:], bpc)
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	shift := uint(0)
	if bpc == 2 {
		shift = 8
	}
	for y := 0; y < h; y++ {
		o := (h - 1 - y) * img.Stride
		for x := 0; x < w; x++ {
			i := y*w + x
			v := func(c int) uint8 { return uint8(planes[c][i] >> shift) }
			var p [4]uint8
			switch z {
			case 1:
				p = [4]uint8{v(0), v(0), v(0), 255}
			case 2:
				p = [4]uint8{v(0), v(0), v(0), v(1)}
			case 3:
				p = [4]uint8{v(0), v(1), v(2), 255}
			default:
				p = [4]uint8{v(0), v(1), v(2), v(3)}
			}
			copy(img.Pix[o+x*4:], p[:])
		}
	}
	return one(&Frame{Img: img, Gray: z <= 2, Alpha: z == 2 || z >= 4}), nil
}

func sgiSample(b []byte, bpc int) uint16 {
	if bpc == 1 {
		return uint16(b[0])
	}
	return binary.BigEndian.Uint16(b)
}

// encodeSGI writes an uncompressed 8-bit SGI image.
func encodeSGI(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	z := 3
	switch {
	case f.Gray && f.Alpha:
		z = 2
	case f.Gray:
		z = 1
	case f.Alpha:
		z = 4
	}
	head := make([]byte, 512)
	be := binary.BigEndian
	be.PutUint16(head, 474)
	head[3] = 1
	dim := uint16(3)
	if z == 1 {
		dim = 2
	}
	be.PutUint16(head[4:], dim)
	be.PutUint16(head[6:], uint16(w))
	be.PutUint16(head[8:], uint16(h))
	be.PutUint16(head[10:], uint16(z))
	be.PutUint32(head[16:], 255)
	out := make([]byte, 512+w*h*z)
	copy(out, head)
	for c := 0; c < z; c++ {
		src := c
		if z <= 2 && c == 1 {
			src = 3
		}
		for y := 0; y < h; y++ {
			row := f.Img.Pix[(h-1-y)*f.Img.Stride:]
			for x := 0; x < w; x++ {
				out[512+c*w*h+y*w+x] = row[x*4+src]
			}
		}
	}
	return out
}

// ---- Sun Raster ----

func decodeSunRaster(data []byte) (*Picture, error) {
	be := binary.BigEndian
	if len(data) < 32 || be.Uint32(data) != 0x59a66a95 {
		return nil, errors.New("the file is not a Sun Raster image")
	}
	w, h, depth := int(be.Uint32(data[4:])), int(be.Uint32(data[8:])), int(be.Uint32(data[12:]))
	kind, mapType, mapLen := be.Uint32(data[20:]), be.Uint32(data[24:]), int(be.Uint32(data[28:]))
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<30 || 32+mapLen > len(data) {
		return nil, errors.New("the Sun Raster file is damaged")
	}
	var palette [][3]uint8
	if mapType == 1 && mapLen > 0 {
		n := mapLen / 3
		m := data[32 : 32+mapLen]
		for i := 0; i < n; i++ {
			palette = append(palette, [3]uint8{m[i], m[n+i], m[2*n+i]})
		}
	}
	px := data[32+mapLen:]
	if kind == 2 {
		var out []byte
		for i := 0; i < len(px); {
			if px[i] != 0x80 {
				out = append(out, px[i])
				i++
				continue
			}
			if i+1 >= len(px) {
				break
			}
			n := int(px[i+1])
			if n == 0 {
				out = append(out, 0x80)
				i += 2
				continue
			}
			if i+2 >= len(px) {
				break
			}
			for k := 0; k <= n; k++ {
				out = append(out, px[i+2])
			}
			i += 3
		}
		px = out
	}
	stride := ((w*depth + 15) / 16) * 2
	if len(px) < stride*h {
		return nil, errors.New("the Sun Raster file is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha := false
	for y := 0; y < h; y++ {
		row := px[y*stride:]
		for x := 0; x < w; x++ {
			var c [4]uint8
			switch depth {
			case 1:
				v := uint8(255)
				if row[x/8]>>(7-x%8)&1 == 1 {
					v = 0
				}
				c = [4]uint8{v, v, v, 255}
				if len(palette) >= 2 {
					pc := palette[row[x/8]>>(7-x%8)&1]
					c = [4]uint8{pc[0], pc[1], pc[2], 255}
				}
			case 8:
				v := row[x]
				c = [4]uint8{v, v, v, 255}
				if int(v) < len(palette) {
					pc := palette[v]
					c = [4]uint8{pc[0], pc[1], pc[2], 255}
				}
			case 24:
				s := row[x*3:]
				c = [4]uint8{s[2], s[1], s[0], 255}
				if kind == 3 {
					c = [4]uint8{s[0], s[1], s[2], 255}
				}
			case 32:
				s := row[x*4:]
				c = [4]uint8{s[3], s[2], s[1], s[0]}
				if kind == 3 {
					c = [4]uint8{s[1], s[2], s[3], s[0]}
				}
				if c[3] != 255 {
					alpha = true
				}
			default:
				return nil, fmt.Errorf("Sun Raster depth %d is not supported", depth)
			}
			copy(img.Pix[y*img.Stride+x*4:], c[:])
		}
	}
	if depth == 32 && alpha && opaqueAllZero(img) {
		// an unused alpha byte
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
		alpha = false
	}
	return one(&Frame{Img: img, Alpha: alpha, Gray: depth == 1 || (depth == 8 && palette == nil)}), nil
}

func opaqueAllZero(img *image.NRGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			return false
		}
	}
	return true
}

// encodeSunRaster writes a standard 24-bit (BGR) Sun Raster image.
func encodeSunRaster(f *Frame) []byte {
	w, h := f.Width(), f.Height()
	stride := ((w*24 + 15) / 16) * 2
	out := make([]byte, 32+stride*h)
	be := binary.BigEndian
	be.PutUint32(out, 0x59a66a95)
	be.PutUint32(out[4:], uint32(w))
	be.PutUint32(out[8:], uint32(h))
	be.PutUint32(out[12:], 24)
	be.PutUint32(out[16:], uint32(stride*h))
	be.PutUint32(out[20:], 1)
	for y := 0; y < h; y++ {
		row := f.Img.Pix[y*f.Img.Stride:]
		d := out[32+y*stride:]
		for x := 0; x < w; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = row[x*4+2], row[x*4+1], row[x*4]
		}
	}
	return out
}
