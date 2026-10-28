package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"math/bits"
)

// Windows bitmaps (BMP files and the pictures inside ICO/CUR), read by Filee
// itself where golang.org/x/image/bmp stops: RLE4/RLE8, 16-bit and bit-field
// pixels, OS/2 headers and the AND mask of icons.

var errDIB = errors.New("the bitmap is damaged or of an unknown kind")

// decodeDIB reads a device-independent bitmap (from its info header on). In an
// icon the height counts the AND mask too (icon true), whose bits make pixels
// transparent when the colour bits carry no alpha. Pixels and data may start
// at pixelOffset (relative to dib; 0 = right after the palette).
func decodeDIB(dib []byte, icon bool, pixelOffset int) (*image.NRGBA, bool, error) {
	if len(dib) < 12 {
		return nil, false, errDIB
	}
	le := binary.LittleEndian
	hsize := int(le.Uint32(dib))
	var w, h, bpp, compression, colors int
	if hsize == 12 {
		w, h = int(le.Uint16(dib[4:])), int(int16(le.Uint16(dib[6:])))
		bpp = int(le.Uint16(dib[10:]))
	} else {
		if hsize < 40 || len(dib) < 40 {
			return nil, false, errDIB
		}
		w, h = int(int32(le.Uint32(dib[4:]))), int(int32(le.Uint32(dib[8:])))
		bpp = int(le.Uint16(dib[14:]))
		compression = int(le.Uint32(dib[16:]))
		colors = int(le.Uint32(dib[32:]))
	}
	topDown := h < 0
	if topDown {
		h = -h
	}
	if icon {
		h /= 2
	}
	if w <= 0 || h <= 0 || w > 1<<16 || h > 1<<16 {
		return nil, false, errDIB
	}
	if hsize > len(dib) {
		return nil, false, errDIB
	}
	p := hsize
	// bit-field masks
	masks := [4]uint32{}
	switch {
	case compression == 3 || compression == 6:
		n := 3
		if compression == 6 {
			n = 4
		}
		if hsize >= 52 {
			for i := 0; i < 4 && 40+i*4+4 <= hsize; i++ {
				masks[i] = le.Uint32(dib[40+i*4:])
			}
		} else {
			if p+n*4 > len(dib) {
				return nil, false, errDIB
			}
			for i := 0; i < n; i++ {
				masks[i] = le.Uint32(dib[p+i*4:])
			}
			p += n * 4
		}
	case bpp == 16:
		masks = [4]uint32{0x7C00, 0x03E0, 0x001F, 0}
	case bpp == 32:
		masks = [4]uint32{0xFF0000, 0xFF00, 0xFF, 0}
	}
	var palette [][4]uint8
	if bpp <= 8 {
		n := colors
		if n == 0 || n > 1<<bpp {
			n = 1 << bpp
		}
		entry := 4
		if hsize == 12 {
			entry = 3
		}
		for i := 0; i < n && p+entry <= len(dib); i++ {
			palette = append(palette, [4]uint8{dib[p+i*entry+2], dib[p+i*entry+1], dib[p+i*entry], 255})
		}
		p += n * entry
	}
	if pixelOffset > 0 {
		p = pixelOffset
	}
	if p > len(dib) {
		return nil, false, errDIB
	}
	data := dib[p:]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	set := func(x, y int, c [4]uint8) {
		if !topDown {
			y = h - 1 - y
		}
		o := y*img.Stride + x*4
		img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c[0], c[1], c[2], c[3]
	}
	pal := func(i int) [4]uint8 {
		if i < len(palette) {
			return palette[i]
		}
		return [4]uint8{0, 0, 0, 255}
	}
	alphaBits := false
	stride := ((w*bpp + 31) / 32) * 4
	switch {
	case compression == 1 || compression == 2:
		if err := decodeRLE(data, w, h, compression == 2, func(x, y, i int) { set(x, y, pal(i)) }); err != nil {
			return nil, false, err
		}
	case bpp == 1 || bpp == 2 || bpp == 4 || bpp == 8:
		for y := 0; y < h; y++ {
			if (y+1)*stride > len(data) {
				return nil, false, errDIB
			}
			row := data[y*stride:]
			for x := 0; x < w; x++ {
				bit := x * bpp
				v := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				set(x, y, pal(v))
			}
		}
	case bpp == 24:
		for y := 0; y < h; y++ {
			if (y+1)*stride > len(data) {
				return nil, false, errDIB
			}
			row := data[y*stride:]
			for x := 0; x < w; x++ {
				set(x, y, [4]uint8{row[x*3+2], row[x*3+1], row[x*3], 255})
			}
		}
	case bpp == 16 || bpp == 32:
		channel := func(v, mask uint32) (uint8, bool) {
			if mask == 0 {
				return 255, false
			}
			shift := bits.TrailingZeros32(mask)
			width := bits.OnesCount32(mask)
			c := (v & mask) >> shift
			if width >= 8 {
				return uint8(c >> (width - 8)), true
			}
			return uint8(c * 255 / (1<<width - 1)), true
		}
		bytesPer := bpp / 8
		if bpp == 32 && masks[3] == 0 && compression == 0 {
			masks[3] = 0xFF000000 // the "unused" byte, alpha in practice (icons, and kept when any pixel uses it)
		}
		for y := 0; y < h; y++ {
			if (y+1)*stride > len(data) {
				return nil, false, errDIB
			}
			row := data[y*stride:]
			for x := 0; x < w; x++ {
				var v uint32
				if bytesPer == 2 {
					v = uint32(le.Uint16(row[x*2:]))
				} else {
					v = le.Uint32(row[x*4:])
				}
				r, _ := channel(v, masks[0])
				g, _ := channel(v, masks[1])
				b, _ := channel(v, masks[2])
				a, has := channel(v, masks[3])
				if has && a != 0 {
					alphaBits = true
				}
				set(x, y, [4]uint8{r, g, b, a})
			}
		}
		if masks[3] != 0 && !alphaBits {
			// an alpha byte that is zero everywhere is unused: opaque
			for i := 3; i < len(img.Pix); i += 4 {
				img.Pix[i] = 255
			}
		}
	default:
		return nil, false, errDIB
	}
	alpha := alphaBits
	if icon && !alphaBits {
		// the AND mask: 1 bit per pixel, rows padded to 32 bits, after the colour bits
		colorBytes := stride * h
		if compression == 1 || compression == 2 {
			colorBytes = len(data)
		}
		maskStride := ((w + 31) / 32) * 4
		if mask := data[min(len(data), colorBytes):]; len(mask) >= maskStride*h {
			for y := 0; y < h; y++ {
				row := mask[y*maskStride:]
				yy := y
				if !topDown {
					yy = h - 1 - y
				}
				for x := 0; x < w; x++ {
					if row[x/8]>>(7-x%8)&1 == 1 {
						img.Pix[yy*img.Stride+x*4+3] = 0
						alpha = true
					}
				}
			}
		}
	}
	return img, alpha, nil
}

// decodeRLE expands RLE8 / RLE4 bitmap data (bottom-up rows as stored).
func decodeRLE(data []byte, w, h int, four bool, set func(x, y, index int)) error {
	x, y := 0, 0
	put := func(i int) {
		if x < w && y < h {
			set(x, y, i)
		}
		x++
	}
	for p := 0; p+1 < len(data); {
		n, v := int(data[p]), data[p+1]
		p += 2
		if n > 0 {
			for k := 0; k < n; k++ {
				if four {
					if k%2 == 0 {
						put(int(v >> 4))
					} else {
						put(int(v & 15))
					}
				} else {
					put(int(v))
				}
			}
			continue
		}
		switch v {
		case 0:
			x, y = 0, y+1
		case 1:
			return nil
		case 2:
			if p+1 >= len(data) {
				return nil
			}
			x, y = x+int(data[p]), y+int(data[p+1])
			p += 2
		default:
			count := int(v)
			nbytes := count
			if four {
				nbytes = (count + 1) / 2
			}
			if p+nbytes > len(data) {
				return errDIB
			}
			for k := 0; k < count; k++ {
				if four {
					b := data[p+k/2]
					if k%2 == 0 {
						put(int(b >> 4))
					} else {
						put(int(b & 15))
					}
				} else {
					put(int(data[p+k]))
				}
			}
			p += nbytes
			if nbytes%2 == 1 {
				p++
			}
		}
	}
	return nil
}

// decodeBMPFallback reads the BMP files golang.org/x/image/bmp does not.
func decodeBMPFallback(data []byte) (*Picture, error) {
	if len(data) < 14+12 || string(data[:2]) != "BM" {
		return nil, errors.New("the file is not a BMP image")
	}
	off := int(binary.LittleEndian.Uint32(data[10:]))
	if off < 14 || off > len(data) {
		return nil, errDIB
	}
	img, alpha, err := decodeDIB(data[14:], false, off-14)
	if err != nil {
		return nil, err
	}
	return one(&Frame{Img: img, Alpha: alpha}), nil
}

// ---- ICO / CUR ----

// decodeICO reads every picture of an icon or cursor (PNG or bitmap entries).
func decodeICO(data []byte) (*Picture, error) {
	le := binary.LittleEndian
	if len(data) < 6 || le.Uint16(data) != 0 || (le.Uint16(data[2:]) != 1 && le.Uint16(data[2:]) != 2) {
		return nil, errors.New("the file is not an ICO or CUR file")
	}
	n := int(le.Uint16(data[4:]))
	p := &Picture{}
	var firstErr error
	for i := 0; i < n; i++ {
		e := 6 + i*16
		if e+16 > len(data) {
			break
		}
		size, off := int(le.Uint32(data[e+8:])), int(le.Uint32(data[e+12:]))
		if off <= 0 || off >= len(data) {
			continue
		}
		entry := data[off:min(len(data), off+size)]
		var f *Frame
		if bytes.HasPrefix(entry, pngSignature) {
			img, err := png.Decode(bytes.NewReader(entry))
			if err != nil {
				firstErr = err
				continue
			}
			f = NewFrame(img)
			f.Alpha = true
		} else {
			img, alpha, err := decodeDIB(entry, true, 0)
			if err != nil {
				firstErr = err
				continue
			}
			_ = alpha
			f = &Frame{Img: img, Alpha: true} // an icon is a picture with alpha
		}
		p.Frames = append(p.Frames, f)
	}
	if len(p.Frames) == 0 {
		if firstErr == nil {
			firstErr = errors.New("the icon holds no picture")
		}
		return nil, firstErr
	}
	return p, nil
}

// encodeICO writes an icon with every size the frame asks for (PNG for 256 px,
// 32-bit bitmaps with an AND mask below), or a cursor of the frame itself.
func encodeICO(f *Frame, cursor bool) ([]byte, error) {
	le := binary.LittleEndian
	type entry struct {
		w, h int
		data []byte
	}
	var entries []entry
	sizes := f.enc.icoSizes
	if cursor || len(sizes) == 0 {
		sizes = nil
		entries = append(entries, entry{f.Width(), f.Height(), nil})
	}
	for _, s := range sizes {
		entries = append(entries, entry{s, s, nil})
	}
	for i := range entries {
		e := &entries[i]
		img := f.Img
		if img.Rect.Dx() != e.w || img.Rect.Dy() != e.h {
			img = Resize(img, e.w, e.h)
		}
		if e.w >= 256 || e.h >= 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			e.data = buf.Bytes()
		} else {
			e.data = iconBitmap(img)
		}
	}
	var out bytes.Buffer
	head := make([]byte, 6)
	le.PutUint16(head[2:], 1)
	if cursor {
		le.PutUint16(head[2:], 2)
	}
	le.PutUint16(head[4:], uint16(len(entries)))
	out.Write(head)
	off := 6 + 16*len(entries)
	for _, e := range entries {
		d := make([]byte, 16)
		d[0], d[1] = byte(e.w%256), byte(e.h%256)
		if cursor {
			// hot spot at the top left
		} else {
			le.PutUint16(d[4:], 1)
			le.PutUint16(d[6:], 32)
		}
		le.PutUint32(d[8:], uint32(len(e.data)))
		le.PutUint32(d[12:], uint32(off))
		off += len(e.data)
		out.Write(d)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return out.Bytes(), nil
}

// iconBitmap is a 32-bit BGRA bitmap with its AND mask, as icons store them.
func iconBitmap(img *image.NRGBA) []byte {
	le := binary.LittleEndian
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maskStride := ((w + 31) / 32) * 4
	out := make([]byte, 40+w*h*4+maskStride*h)
	le.PutUint32(out, 40)
	le.PutUint32(out[4:], uint32(w))
	le.PutUint32(out[8:], uint32(h*2))
	le.PutUint16(out[12:], 1)
	le.PutUint16(out[14:], 32)
	le.PutUint32(out[20:], uint32(w*h*4+maskStride*h))
	px := out[40:]
	mask := out[40+w*h*4:]
	for y := 0; y < h; y++ {
		src := img.Pix[(h-1-y)*img.Stride:]
		for x := 0; x < w; x++ {
			s := src[x*4:]
			d := px[(y*w+x)*4:]
			d[0], d[1], d[2], d[3] = s[2], s[1], s[0], s[3]
			if s[3] == 0 {
				mask[y*maskStride+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return out
}
