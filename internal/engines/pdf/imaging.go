// Picture helpers of the PDF engines. Reading pictures into a PDF and
// writing rendered pages are the image engine's (package images); these are
// the small pieces the PDF writer, the compressor and the previews share.

package pdf

import (
	"image"
	"image/color"

	"golang.org/x/image/draw"
)

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// scaleTo resizes to w × h.
func scaleTo(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if w == b.Dx() && h == b.Dy() {
		return img
	}
	out := image.NewNRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

// hasAlpha tells whether any pixel is not opaque.
func hasAlpha(img image.Image) bool {
	switch m := img.(type) {
	case *image.Gray, *image.YCbCr, *image.CMYK, *image.Gray16:
		return false
	case *image.RGBA:
		for i := 3; i < len(m.Pix); i += 4 {
			if m.Pix[i] != 0xFF {
				return true
			}
		}
		return false
	case *image.NRGBA:
		for i := 3; i < len(m.Pix); i += 4 {
			if m.Pix[i] != 0xFF {
				return true
			}
		}
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return true
			}
		}
	}
	return false
}

// flatten draws the picture on a solid background.
func flatten(img image.Image, bg color.Color) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}

func isGray(img image.Image) bool {
	_, ok := img.(*image.Gray)
	return ok
}

func drawSrc(dst draw.Image, src image.Image) {
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
}
