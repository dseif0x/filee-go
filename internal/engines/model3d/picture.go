package model3d

// The rendered preview goes through the preset's picture options (turn,
// mirror, resize, grayscale, background, quality) like other pictures, then
// is encoded. The engine writes the pictures Go encodes in-process (PNG, JPG,
// GIF, BMP, TIFF); other picture formats are reached through PNG and the
// image engines, which then only re-encode (the options are applied here).

import (
	"bufio"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/presets"
)

// PictureFormats are the picture formats the engine writes itself.
var PictureFormats = []string{"png", "jpg", "gif", "bmp", "tiff"}

// noAlpha are formats written without transparency (flattened on the background).
var noAlpha = map[string]bool{"jpg": true}

// applyImageOptions turns, mirrors, resizes and greys the picture as the options say.
func applyImageOptions(img *image.NRGBA, o *presets.ImageOptions) *image.NRGBA {
	if o == nil {
		return img
	}
	switch o.Rotation {
	case presets.Right90:
		img = rotate(img, 90)
	case presets.Left90:
		img = rotate(img, 270)
	case presets.Rotate180:
		img = rotate(img, 180)
	}
	switch o.Flip {
	case presets.FlipHorizontal:
		img = mirror(img, true)
	case presets.FlipVertical:
		img = mirror(img, false)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if c := o.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		cw, ch := c.ClampedWidth(), c.ClampedHeight()
		if c.LockAspect {
			s := math.Min(float64(cw)/float64(w), float64(ch)/float64(h))
			if s != 1 {
				img = resize(img, max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s))))
			}
		} else if w != cw || h != ch {
			img = resize(img, cw, ch)
		}
	} else if m := o.ClampedMinShortEdge(); m > 0 {
		s := float64(m) / float64(min(w, h))
		if s > 1 {
			limit := math.Sqrt(float64(presets.MaxSide) * presets.MaxSide / float64(w*h))
			s = math.Max(1, math.Min(s, limit))
			img = resize(img, max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s))))
		}
	} else {
		switch o.Resize {
		case presets.ResizePercent:
			if o.ResizePercent > 0 && o.ResizePercent != 100 {
				s := float64(o.ResizePercent) / 100
				img = resize(img, max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s))))
			}
		case presets.ResizeLongEdge:
			if o.LongEdge > 0 && max(w, h) > o.LongEdge {
				s := float64(o.LongEdge) / float64(max(w, h))
				img = resize(img, max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s))))
			}
		case presets.ResizeWidth:
			if o.Width > 0 && w > o.Width {
				img = resize(img, o.Width, max(1, int(math.Round(float64(h)*float64(o.Width)/float64(w)))))
			}
		}
	}
	if o.Grayscale {
		for i := 0; i+3 < len(img.Pix); i += 4 {
			// Rec. 709 luma, as ImageMagick's Grayscale.
			y := uint8(math.Round(0.2126*float64(img.Pix[i]) + 0.7152*float64(img.Pix[i+1]) + 0.0722*float64(img.Pix[i+2])))
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = y, y, y
		}
	}
	return img
}

func rotate(src *image.NRGBA, degrees int) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if degrees != 180 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch degrees {
			case 90: // clockwise
				dx, dy = h-1-y, x
			case 270:
				dx, dy = y, w-1-x
			default:
				dx, dy = w-1-x, h-1-y
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):dst.PixOffset(dx, dy)+4], src.Pix[src.PixOffset(x, y):src.PixOffset(x, y)+4])
		}
	}
	return dst
}

func mirror(src *image.NRGBA, horizontal bool) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, h-1-y
			if horizontal {
				dx, dy = w-1-x, y
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):dst.PixOffset(dx, dy)+4], src.Pix[src.PixOffset(x, y):src.PixOffset(x, y)+4])
		}
	}
	return dst
}

func resize(src *image.NRGBA, w, h int) *image.NRGBA {
	// Scaled premultiplied, so transparent pixels don't bleed their colour into the edges.
	pre := image.NewRGBA(src.Bounds())
	draw.Draw(pre, pre.Bounds(), src, src.Bounds().Min, draw.Src)
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), pre, pre.Bounds(), xdraw.Src, nil)
	out := image.NewNRGBA(scaled.Bounds())
	draw.Draw(out, out.Bounds(), scaled, image.Point{}, draw.Src)
	return out
}

// parseBackground reads #RGB / #RRGGBB / #RRGGBBAA, white when it is not a colour.
func parseBackground(s string) color.NRGBA {
	s = strings.TrimSpace(s)
	if c, ok := parseHex(s); ok {
		return color.NRGBA{toByte(c.X), toByte(c.Y), toByte(c.Z), toByte(c.W)}
	}
	return color.NRGBA{255, 255, 255, 255}
}

func flatten(img *image.NRGBA, bg color.NRGBA) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), &image.Uniform{color.NRGBA{bg.R, bg.G, bg.B, 255}}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

// writePicture encodes the picture as format into path.
func writePicture(img *image.NRGBA, format, path string, o *presets.ImageOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	var src image.Image = img
	if noAlpha[format] {
		bg := "#FFFFFF"
		if o != nil {
			bg = o.Background
		}
		src = flatten(img, parseBackground(bg))
	}
	quality := 90
	if o != nil {
		quality = max(1, min(100, o.Quality))
	}
	switch format {
	case "jpg":
		err = jpeg.Encode(w, src, &jpeg.Options{Quality: quality})
	case "gif":
		err = gif.Encode(w, src, nil)
	case "bmp":
		err = bmp.Encode(w, src)
	case "tiff":
		compression := tiff.Deflate
		if o != nil && o.TiffCompression == presets.TiffNone {
			compression = tiff.Uncompressed
		}
		err = tiff.Encode(w, src, &tiff.Options{Compression: compression})
	default:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if o != nil && o.MaxCompression {
			enc.CompressionLevel = png.BestCompression
		}
		err = enc.Encode(w, src)
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
