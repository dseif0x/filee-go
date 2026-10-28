package metafile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
)

// decodeDIB reads a device-independent bitmap: a BITMAPINFO (core, info,
// V4 or V5 header with colour table) and its pixels. keepAlpha keeps the
// alpha channel of 32-bit pixels (AlphaBlend); otherwise they are opaque.
func decodeDIB(info, pixels []byte, keepAlpha bool) (image.Image, error) {
	le := binary.LittleEndian
	if len(info) < 12 {
		return nil, errors.New("short bitmap header")
	}
	hsize := int(le.Uint32(info))
	var width, height int
	var bpp, compression uint32
	var clrUsed int
	palEntry := 4
	switch {
	case hsize == 12:
		width = int(le.Uint16(info[4:]))
		height = int(int16(le.Uint16(info[6:])))
		bpp = uint32(le.Uint16(info[10:]))
		palEntry = 3
	case hsize >= 40 && len(info) >= 40:
		width = int(int32(le.Uint32(info[4:])))
		height = int(int32(le.Uint32(info[8:])))
		bpp = uint32(le.Uint16(info[14:]))
		compression = le.Uint32(info[16:])
		clrUsed = int(le.Uint32(info[32:]))
	default:
		return nil, errors.New("unknown bitmap header")
	}
	if compression == 4 || compression == 5 { // BI_JPEG, BI_PNG
		img, _, err := image.Decode(bytes.NewReader(pixels))
		return img, err
	}
	if width <= 0 || height == 0 || width > 1<<15 || abs(height) > 1<<15 {
		return nil, errors.New("bad bitmap size")
	}
	topDown := height < 0
	h := abs(height)
	// Bit masks of BI_BITFIELDS follow the 40-byte header (or are part of V4/V5).
	masks := [4]uint32{}
	palOff := hsize
	if compression == 3 || compression == 6 {
		if hsize == 40 {
			if len(info) >= 52 {
				masks[0], masks[1], masks[2] = le.Uint32(info[40:]), le.Uint32(info[44:]), le.Uint32(info[48:])
			}
			palOff = 52
			if compression == 6 && len(info) >= 56 {
				masks[3] = le.Uint32(info[52:])
				palOff = 56
			}
		} else if len(info) >= 56 {
			masks[0], masks[1], masks[2], masks[3] = le.Uint32(info[40:]), le.Uint32(info[44:]), le.Uint32(info[48:]), le.Uint32(info[52:])
		}
	}
	var palette []color.NRGBA
	if bpp <= 8 {
		n := clrUsed
		if n == 0 {
			n = 1 << bpp
		}
		for i := 0; i < n && palOff+i*palEntry+3 <= len(info); i++ {
			o := palOff + i*palEntry
			palette = append(palette, color.NRGBA{info[o+2], info[o+1], info[o], 255})
		}
		if len(palette) == 0 {
			for i := 0; i < 1<<bpp; i++ {
				v := uint8(i * 255 / ((1 << bpp) - 1))
				palette = append(palette, color.NRGBA{v, v, v, 255})
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, h))
	if compression == 1 || compression == 2 {
		decodeRLE(img, pixels, palette, compression == 2, topDown)
		return img, nil
	}
	stride := ((width*int(bpp) + 31) / 32) * 4
	if stride*h > len(pixels) {
		// Truncated pixel data: draw what is there.
		h = len(pixels) / max(1, stride)
		if h == 0 {
			return nil, errors.New("no bitmap pixels")
		}
	}
	if bpp == 16 && masks[0] == 0 {
		masks = [4]uint32{0x7C00, 0x03E0, 0x001F, 0}
	}
	if bpp == 32 && masks[0] == 0 {
		masks = [4]uint32{0x00FF0000, 0x0000FF00, 0x000000FF, 0xFF000000}
	}
	anyAlpha := false
	for y := 0; y < h; y++ {
		row := pixels[y*stride:]
		dy := h - 1 - y
		if topDown {
			dy = y
		}
		for x := 0; x < width; x++ {
			var c color.NRGBA
			switch bpp {
			case 1, 2, 4, 8:
				bitPos := x * int(bpp)
				v := (row[bitPos/8] >> (8 - int(bpp) - bitPos%8)) & byte(1<<bpp-1)
				if int(v) < len(palette) {
					c = palette[v]
				} else {
					c = color.NRGBA{A: 255}
				}
			case 16:
				v := uint32(le.Uint16(row[x*2:]))
				c = color.NRGBA{field(v, masks[0]), field(v, masks[1]), field(v, masks[2]), 255}
			case 24:
				c = color.NRGBA{row[x*3+2], row[x*3+1], row[x*3], 255}
			case 32:
				v := le.Uint32(row[x*4:])
				c = color.NRGBA{field(v, masks[0]), field(v, masks[1]), field(v, masks[2]), 255}
				if masks[3] != 0 {
					c.A = field(v, masks[3])
					if c.A != 0 {
						anyAlpha = true
					}
				}
			default:
				return nil, errors.New("unsupported bitmap depth")
			}
			img.SetNRGBA(x, dy, c)
		}
	}
	if bpp == 32 {
		// An all-zero alpha channel means none (BI_RGB); with AlphaBlend the
		// pixels are premultiplied.
		for i := 3; i < len(img.Pix); i += 4 {
			switch {
			case !keepAlpha || !anyAlpha:
				img.Pix[i] = 255
			case img.Pix[i] > 0 && img.Pix[i] < 255:
				a := int(img.Pix[i])
				for k := i - 3; k < i; k++ {
					img.Pix[k] = uint8(min(255, int(img.Pix[k])*255/a))
				}
			}
		}
	}
	return img, nil
}

func field(v, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	width := bits.OnesCount32(mask)
	x := (v & mask) >> shift
	if width >= 8 {
		return uint8(x >> (width - 8))
	}
	return uint8(x * 255 / (1<<width - 1))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// decodeRLE reads BI_RLE8 / BI_RLE4 pixels.
func decodeRLE(img *image.NRGBA, data []byte, pal []color.NRGBA, four bool, topDown bool) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	x, y := 0, 0
	set := func(v int) {
		if x < w && y < h && v < len(pal) {
			dy := h - 1 - y
			if topDown {
				dy = y
			}
			img.SetNRGBA(x, dy, pal[v])
		}
		x++
	}
	for i := 0; i+1 < len(data); {
		n, v := int(data[i]), int(data[i+1])
		i += 2
		if n > 0 {
			for k := 0; k < n; k++ {
				if four {
					if k%2 == 0 {
						set(v >> 4)
					} else {
						set(v & 15)
					}
				} else {
					set(v)
				}
			}
			continue
		}
		switch v {
		case 0:
			x, y = 0, y+1
		case 1:
			return
		case 2:
			if i+1 < len(data) {
				x += int(data[i])
				y += int(data[i+1])
				i += 2
			}
		default:
			if four {
				nb := (v + 1) / 2
				for k := 0; k < v && i+k/2 < len(data); k++ {
					b := int(data[i+k/2])
					if k%2 == 0 {
						set(b >> 4)
					} else {
						set(b & 15)
					}
				}
				i += nb
				if nb%2 == 1 {
					i++
				}
			} else {
				for k := 0; k < v && i+k < len(data); k++ {
					set(int(data[i+k]))
				}
				i += v
				if v%2 == 1 {
					i++
				}
			}
		}
	}
}

// crop returns the part of img inside the source rectangle (DIB rows are bottom-up in coordinates).
func crop(img image.Image, x, y, w, h int) image.Image {
	b := img.Bounds()
	r := image.Rect(x, y, x+w, y+h).Intersect(b)
	if r.Empty() || r == b {
		return img
	}
	if s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return s.SubImage(r)
	}
	return img
}
