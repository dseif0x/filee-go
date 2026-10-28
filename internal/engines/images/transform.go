package images

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

// Pixel operations on 8-bit straight-alpha pictures.

// Resize scales img to w × h (aspect not kept) with Catmull-Rom, or bilinear
// for very large results, in premultiplied alpha so transparent pixels don't
// bleed their colour into the edges.
func Resize(img *image.NRGBA, w, h int) *image.NRGBA {
	w, h = max(1, w), max(1, h)
	sb := img.Rect
	if sb.Dx() == w && sb.Dy() == h {
		return img
	}
	var interp draw.Interpolator = draw.CatmullRom
	if int64(w)*int64(h) > 80_000_000 || int64(sb.Dx())*int64(sb.Dy()) > 200_000_000 {
		interp = draw.ApproxBiLinear
	}
	src := premultiplied(img)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	interp.Scale(dst, dst.Rect, src, src.Rect, draw.Src, nil)
	return unpremultiplied(dst)
}

// scaleBox shrinks img to w × h by averaging boxes of pixels (ImageMagick's
// Scale, used by the blur): every source pixel counts once.
func scaleBox(img *image.NRGBA, w, h int) *image.NRGBA {
	w, h = max(1, min(w, img.Rect.Dx())), max(1, min(h, img.Rect.Dy()))
	return unpremultiplied(boxDown(premultiplied(img), w, h))
}

func boxDown(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max(y*sh/h+1, (y+1)*sh/h)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max(x*sw/w+1, (x+1)*sw/w)
			var s [4]uint64
			for yy := y0; yy < y1; yy++ {
				row := src.Pix[yy*src.Stride:]
				for xx := x0; xx < x1; xx++ {
					for c := 0; c < 4; c++ {
						s[c] += uint64(row[xx*4+c])
					}
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			o := dst.Pix[y*dst.Stride+x*4:]
			for c := 0; c < 4; c++ {
				o[c] = uint8((s[c] + n/2) / n)
			}
		}
	}
	return dst
}

func premultiplied(img *image.NRGBA) *image.RGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if opaque(img) {
		return &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: image.Rect(0, 0, w, h)}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		s := img.Pix[y*img.Stride : y*img.Stride+w*4]
		d := out.Pix[y*out.Stride:]
		for i := 0; i < len(s); i += 4 {
			a := uint32(s[i+3])
			d[i] = uint8((uint32(s[i])*a + 127) / 255)
			d[i+1] = uint8((uint32(s[i+1])*a + 127) / 255)
			d[i+2] = uint8((uint32(s[i+2])*a + 127) / 255)
			d[i+3] = uint8(a)
		}
	}
	return out
}

func unpremultiplied(img *image.RGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		s := img.Pix[y*img.Stride : y*img.Stride+w*4]
		d := out.Pix[y*out.Stride:]
		for i := 0; i < len(s); i += 4 {
			a := uint32(s[i+3])
			switch a {
			case 255:
				d[i], d[i+1], d[i+2], d[i+3] = s[i], s[i+1], s[i+2], 255
			case 0:
			default:
				d[i] = uint8(min(255, (uint32(s[i])*255+a/2)/a))
				d[i+1] = uint8(min(255, (uint32(s[i+1])*255+a/2)/a))
				d[i+2] = uint8(min(255, (uint32(s[i+2])*255+a/2)/a))
				d[i+3] = uint8(a)
			}
		}
	}
	return out
}

// orient returns img turned and mirrored as Exif orientation o says, upright.
func orient(img *image.NRGBA, o int) *image.NRGBA {
	switch o {
	case 2:
		return flipH(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipV(img)
	case 5:
		return transpose(img)
	case 6:
		return rotate90(img)
	case 7:
		return rotate180(transpose(img))
	case 8:
		return rotate270(img)
	}
	return img
}

// transform builds a w × h picture whose pixel (x, y) is src's pixel at at(x, y).
func transformPixels(src *image.NRGBA, w, h int, at func(x, y int) (int, int)) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			sx, sy := at(x, y)
			copy(d[x*4:x*4+4], src.Pix[sy*src.Stride+sx*4:])
		}
	}
	return dst
}

func flipH(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	return transformPixels(img, w, h, func(x, y int) (int, int) { return w - 1 - x, y })
}

func flipV(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+w*4], img.Pix[(h-1-y)*img.Stride:])
	}
	return dst
}

func rotate180(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	return transformPixels(img, w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y })
}

// rotate90 turns clockwise.
func rotate90(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	return transformPixels(img, h, w, func(x, y int) (int, int) { return y, h - 1 - x })
}

// rotate270 turns counter-clockwise.
func rotate270(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	return transformPixels(img, h, w, func(x, y int) (int, int) { return w - 1 - y, x })
}

func transpose(img *image.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	return transformPixels(img, h, w, func(x, y int) (int, int) { return y, x })
}

// crop returns the r part of img as its own picture.
func crop(img *image.NRGBA, r image.Rectangle) *image.NRGBA {
	r = r.Intersect(img.Rect)
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):])
	}
	return out
}

// extent places img on a w × h canvas of bg at x, y.
func extent(img *image.NRGBA, w, h, x, y int, bg color.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill(out, bg)
	paste(out, img, x, y)
	return out
}

func fill(img *image.NRGBA, c color.NRGBA) {
	if c == (color.NRGBA{}) {
		clear(img.Pix)
		return
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
}

// paste copies src into dst at x, y (replacing, not blending).
func paste(dst, src *image.NRGBA, x, y int) {
	r := image.Rect(x, y, x+src.Rect.Dx(), y+src.Rect.Dy()).Intersect(dst.Rect)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		copy(dst.Pix[dst.PixOffset(r.Min.X, yy):dst.PixOffset(r.Max.X, yy)], src.Pix[src.PixOffset(r.Min.X-x, yy-y):])
	}
}

// over composites src over dst at x, y (straight alpha, Porter-Duff over).
func over(dst, src *image.NRGBA, x, y int) {
	r := image.Rect(x, y, x+src.Rect.Dx(), y+src.Rect.Dy()).Intersect(dst.Rect)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		d := dst.Pix[dst.PixOffset(r.Min.X, yy):dst.PixOffset(r.Max.X, yy)]
		s := src.Pix[src.PixOffset(r.Min.X-x, yy-y):]
		for i := 0; i < len(d); i += 4 {
			blendOver(d[i:i+4], s[i:i+4], 255)
		}
	}
}

// blendOver lays s (with extra opacity 0–255) over d in place.
func blendOver(d, s []uint8, opacity uint32) {
	sa := uint32(s[3]) * opacity / 255
	if sa == 0 {
		return
	}
	if sa == 255 {
		d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 255
		return
	}
	da := uint32(d[3])
	oa := sa + da*(255-sa)/255
	if oa == 0 {
		d[0], d[1], d[2], d[3] = 0, 0, 0, 0
		return
	}
	for c := 0; c < 3; c++ {
		v := (uint32(s[c])*sa + uint32(d[c])*da*(255-sa)/255 + oa/2) / oa
		d[c] = uint8(min(255, v))
	}
	d[3] = uint8(oa)
}

// toGray turns every pixel into its Rec. 709 luma (ImageMagick's Grayscale).
func toGray(img *image.NRGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		v := uint8(min(255, 0.212656*float64(img.Pix[i])+0.715158*float64(img.Pix[i+1])+0.072186*float64(img.Pix[i+2])+0.5))
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = v, v, v
	}
}

// ParseColor reads "#RGB", "#RRGGBB", "#RRGGBBAA" or a few names; ok is false otherwise.
func ParseColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "white":
		return color.NRGBA{255, 255, 255, 255}, true
	case "black":
		return color.NRGBA{0, 0, 0, 255}, true
	case "transparent", "none":
		return color.NRGBA{}, true
	}
	if !strings.HasPrefix(s, "#") {
		return color.NRGBA{}, false
	}
	h := s[1:]
	hex := func(c byte) (uint8, bool) {
		switch {
		case c >= '0' && c <= '9':
			return c - '0', true
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10, true
		}
		return 0, false
	}
	var v []uint8
	for i := 0; i < len(h); i++ {
		d, ok := hex(h[i])
		if !ok {
			return color.NRGBA{}, false
		}
		v = append(v, d)
	}
	switch len(v) {
	case 3:
		return color.NRGBA{v[0] * 17, v[1] * 17, v[2] * 17, 255}, true
	case 4:
		return color.NRGBA{v[0] * 17, v[1] * 17, v[2] * 17, v[3] * 17}, true
	case 6:
		return color.NRGBA{v[0]<<4 | v[1], v[2]<<4 | v[3], v[4]<<4 | v[5], 255}, true
	case 8:
		return color.NRGBA{v[0]<<4 | v[1], v[2]<<4 | v[3], v[4]<<4 | v[5], v[6]<<4 | v[7]}, true
	}
	return color.NRGBA{}, false
}

// backgroundColor is the preset's background, white when unset or unreadable.
func backgroundColor(s string) color.NRGBA {
	if c, ok := ParseColor(s); ok && strings.TrimSpace(s) != "" {
		return c
	}
	return color.NRGBA{255, 255, 255, 255}
}
