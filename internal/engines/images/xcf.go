package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"strconv"
)

// GIMP's XCF, read by Filee itself: the visible layers of an 8-bit document
// composed bottom-up onto a canvas of the document's size (normal blending
// with the layer's opacity, as ImageMagick flattens them). Tiles may be
// uncompressed, RLE or zlib compressed.

type xcfReader struct {
	b       []byte
	ptr64   bool
	version int
}

func (r *xcfReader) u32(at int) (int, error) {
	if at < 0 || at+4 > len(r.b) {
		return 0, errors.New("the XCF file is cut short")
	}
	return int(binary.BigEndian.Uint32(r.b[at:])), nil
}

func (r *xcfReader) ptr(at int) (int, int, error) {
	if r.ptr64 {
		if at < 0 || at+8 > len(r.b) {
			return 0, 0, errors.New("the XCF file is cut short")
		}
		return int(binary.BigEndian.Uint64(r.b[at:])), at + 8, nil
	}
	v, err := r.u32(at)
	return v, at + 4, err
}

// props walks a property list from at, calling fn for each, and returns the offset after it.
func (r *xcfReader) props(at int, fn func(typ int, data []byte)) (int, error) {
	for i := 0; i < 10000; i++ {
		typ, err := r.u32(at)
		if err != nil {
			return 0, err
		}
		n, err := r.u32(at + 4)
		if err != nil {
			return 0, err
		}
		at += 8
		if typ == 0 {
			return at, nil
		}
		if at+n > len(r.b) {
			return 0, errors.New("the XCF file is damaged (property)")
		}
		fn(typ, r.b[at:at+n])
		at += n
	}
	return 0, errors.New("the XCF file is damaged (properties)")
}

func decodeXCF(data []byte) (*Picture, error) {
	if len(data) < 26 || !bytes.HasPrefix(data, []byte("gimp xcf ")) {
		return nil, errors.New("the file is not a GIMP image")
	}
	r := &xcfReader{b: data}
	ver := string(data[9:13])
	if ver != "file" {
		if len(ver) == 4 && ver[0] == 'v' {
			r.version, _ = strconv.Atoi(ver[1:])
		}
	}
	r.ptr64 = r.version >= 11
	w, _ := r.u32(14)
	h, _ := r.u32(18)
	base, _ := r.u32(22)
	at := 26
	if r.version >= 4 {
		precision, _ := r.u32(26)
		at = 30
		eight := (r.version < 7 && precision == 0) || precision == 100 || precision == 150
		if !eight {
			return nil, errors.New(xcfUnreadable("it is not an 8-bit image"))
		}
	}
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<30 {
		return nil, errors.New("the XCF file is damaged (size)")
	}
	compression := 0
	var colormap []byte
	at, err := r.props(at, func(typ int, d []byte) {
		switch typ {
		case 17:
			if len(d) >= 1 {
				compression = int(d[0])
			}
		case 1:
			if len(d) >= 4 {
				n := int(binary.BigEndian.Uint32(d))
				if 4+n*3 <= len(d) {
					colormap = d[4 : 4+n*3]
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if compression > 2 {
		return nil, errors.New(xcfUnreadable(fmt.Sprintf("compression %d is not supported", compression)))
	}
	var layers []int
	for {
		p, next, err := r.ptr(at)
		if err != nil {
			return nil, err
		}
		at = next
		if p == 0 {
			break
		}
		layers = append(layers, p)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	// layers are stored top first
	for i := len(layers) - 1; i >= 0; i-- {
		img, x, y, opacity, visible, err := r.layer(layers[i], compression, colormap)
		if err != nil {
			return nil, err
		}
		if !visible || img == nil {
			continue
		}
		rr := image.Rect(x, y, x+img.Rect.Dx(), y+img.Rect.Dy()).Intersect(canvas.Rect)
		for yy := rr.Min.Y; yy < rr.Max.Y; yy++ {
			for xx := rr.Min.X; xx < rr.Max.X; xx++ {
				s := img.Pix[(yy-y)*img.Stride+(xx-x)*4:]
				d := canvas.Pix[yy*canvas.Stride+xx*4:]
				blendOver(d[:4], s[:4], uint32(opacity))
			}
		}
	}
	return one(&Frame{Img: canvas, Alpha: true, Gray: base == 1}), nil
}

func xcfUnreadable(why string) string {
	return "This GIMP file can't be read (" + why + "). Filee reads 8-bit XCF files; in GIMP, export the picture as PNG or PSD instead."
}

func (r *xcfReader) layer(at, compression int, colormap []byte) (*image.NRGBA, int, int, int, bool, error) {
	w, err := r.u32(at)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	h, _ := r.u32(at + 4)
	typ, _ := r.u32(at + 8)
	nameLen, err := r.u32(at + 12)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	at += 16 + nameLen
	opacity, visible := 255, true
	x, y := 0, 0
	at, err = r.props(at, func(t int, d []byte) {
		switch t {
		case 6:
			if len(d) >= 4 {
				opacity = int(binary.BigEndian.Uint32(d))
			}
		case 33:
			if len(d) >= 4 {
				opacity = int(float32frombits(binary.BigEndian.Uint32(d))*255 + 0.5)
			}
		case 8:
			if len(d) >= 4 {
				visible = binary.BigEndian.Uint32(d) != 0
			}
		case 15:
			if len(d) >= 8 {
				x, y = int(int32(binary.BigEndian.Uint32(d))), int(int32(binary.BigEndian.Uint32(d[4:])))
			}
		}
	})
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	if !visible || w <= 0 || h <= 0 || int64(w)*int64(h) > 1<<30 {
		return nil, x, y, opacity, false, nil
	}
	hier, _, err := r.ptr(at)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	bpp, err := r.u32(hier + 8)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	level, _, err := r.ptr(hier + 12)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	if bpp < 1 || bpp > 4 {
		return nil, 0, 0, 0, false, errors.New(xcfUnreadable("it is not an 8-bit image"))
	}
	lat := level + 8
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cols, rows := (w+63)/64, (h+63)/64
	var tiles []int
	for i := 0; i < cols*rows; i++ {
		p, next, err := r.ptr(lat)
		if err != nil || p == 0 {
			return nil, 0, 0, 0, false, errors.New("the XCF file is damaged (tiles)")
		}
		tiles = append(tiles, p)
		lat = next
	}
	end, _, _ := r.ptr(lat) // a zero, then possibly nothing
	_ = end
	for t, p := range tiles {
		tx, ty := t%cols*64, t/cols*64
		tw, th := min(64, w-tx), min(64, h-ty)
		n := tw * th
		var px []byte // interleaved bpp bytes per pixel
		limit := len(r.b)
		if t+1 < len(tiles) && tiles[t+1] > p {
			limit = tiles[t+1]
		}
		src := r.b[p:min(len(r.b), max(p, limit))]
		switch compression {
		case 0:
			if len(src) < n*bpp {
				return nil, 0, 0, 0, false, errors.New("the XCF file is cut short (tile)")
			}
			px = src[:n*bpp]
		case 1:
			px = make([]byte, n*bpp)
			q := 0
			for c := 0; c < bpp; c++ {
				for i := 0; i < n && q < len(src); {
					op := int(src[q])
					q++
					switch {
					case op <= 126:
						if q >= len(src) {
							break
						}
						v := src[q]
						q++
						for k := 0; k <= op && i < n; k++ {
							px[i*bpp+c] = v
							i++
						}
					case op == 127:
						if q+3 > len(src) {
							q = len(src)
							break
						}
						cnt := int(src[q])<<8 | int(src[q+1])
						v := src[q+2]
						q += 3
						for k := 0; k < cnt && i < n; k++ {
							px[i*bpp+c] = v
							i++
						}
					case op == 128:
						if q+2 > len(src) {
							q = len(src)
							break
						}
						cnt := int(src[q])<<8 | int(src[q+1])
						q += 2
						for k := 0; k < cnt && i < n && q < len(src); k++ {
							px[i*bpp+c] = src[q]
							q++
							i++
						}
					default:
						cnt := 256 - op
						for k := 0; k < cnt && i < n && q < len(src); k++ {
							px[i*bpp+c] = src[q]
							q++
							i++
						}
					}
				}
			}
		case 2:
			out, err := inflatePartial(src, n*bpp)
			if err != nil || len(out) < n*bpp {
				return nil, 0, 0, 0, false, errors.New("the XCF file is damaged (zlib tile)")
			}
			px = out
		}
		for i := 0; i < n; i++ {
			s := px[i*bpp:]
			var c [4]uint8
			switch typ {
			case 0, 1:
				c = [4]uint8{s[0], s[min(1, bpp-1)], s[min(2, bpp-1)], 255}
				if typ == 1 && bpp >= 4 {
					c[3] = s[3]
				}
			case 2, 3:
				c = [4]uint8{s[0], s[0], s[0], 255}
				if typ == 3 && bpp >= 2 {
					c[3] = s[1]
				}
			case 4, 5:
				k := int(s[0])
				if k*3+2 < len(colormap) {
					c = [4]uint8{colormap[k*3], colormap[k*3+1], colormap[k*3+2], 255}
				} else {
					c[3] = 255
				}
				if typ == 5 && bpp >= 2 {
					c[3] = s[1]
				}
			}
			x0, y0 := tx+i%tw, ty+i/tw
			copy(img.Pix[y0*img.Stride+x0*4:], c[:])
		}
	}
	return img, x, y, opacity, true, nil
}
