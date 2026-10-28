// Package imgout writes pictures rendered by the vector and CAD engines in the
// image formats. The image engine (ImageMagick's role in the original) owns the
// encoders; it plugs in through Hook. Without it, the formats the standard
// library and golang.org/x/image encode are written here, and only those get
// direct edges.
package imgout

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"slices"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Writable are the image formats ImageMagick writes in the original
// (ImageEncoder.Writable).
var Writable = []string{
	"png", "jpg", "webp", "tiff", "bmp", "gif", "ico", "avif",
	"jxl", "jp2", "psd", "psb", "tga", "ppm",
	"apng", "mng", "qoi", "dds", "pcx", "cur", "xpm", "xbm", "wbmp", "pam", "pfm", "sgi", "ras", "miff",
}

// builtin are the formats written without the image engine.
var builtin = []string{"png", "jpg", "tiff", "bmp", "gif"}

// Writer applies the preset's image options to a rendered picture (resize,
// rotation, grayscale, background, DPI, …; applyResize false when the picture
// was already rendered at its final size) and writes it as target. dpi is the
// picture's resolution.
type Writer func(img image.Image, dpi float64, target string, out convert.OutputAllocator, o *presets.ImageOptions, applyResize bool) ([]string, error)

// Hook is the image engine's writer; nil uses the built-in encoders.
var Hook Writer

// Targets are the formats rendered pictures can be written as.
func Targets() []string {
	if Hook != nil {
		return Writable
	}
	return builtin
}

// CanWrite reports whether target is one of Targets.
func CanWrite(target string) bool { return slices.Contains(Targets(), target) }

var (
	multiFrame = []string{"tiff", "gif", "apng", "mng"}
	animated   = []string{"gif", "webp", "apng", "mng"}
)

// Edge is ImageEncoder.Edge: a still picture costs more as GIF or APNG.
func Edge(source, target string) convert.Edge {
	cost := 10
	if (target == "gif" || target == "apng") && !slices.Contains(multiFrame, source) && !slices.Contains(animated, source) {
		cost = 12
	}
	return convert.EC(source, target, cost)
}

// Write writes img as target.
func Write(img image.Image, dpi float64, target string, out convert.OutputAllocator, o *presets.ImageOptions, applyResize bool) ([]string, error) {
	if Hook != nil {
		return Hook(img, dpi, target, out, o, applyResize)
	}
	if !slices.Contains(builtin, target) {
		return nil, fmt.Errorf("writing %s needs the image engine", target)
	}
	if o == nil {
		d := presets.DefaultImageOptions()
		o = &d
	}
	img = Apply(img, o, target, applyResize)
	ext := target
	path := out.Allocate(ext, "")
	if path == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriter(f)
	switch target {
	case "png":
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if o.MaxCompression {
			enc.CompressionLevel = png.BestCompression
		}
		err = enc.Encode(w, img)
	case "jpg":
		q := o.Quality
		if q <= 0 || q > 100 {
			q = 90
		}
		err = jpeg.Encode(w, img, &jpeg.Options{Quality: q})
	case "tiff":
		comp := tiff.Deflate
		switch o.TiffCompression {
		case presets.TiffNone:
			comp = tiff.Uncompressed
		case presets.TiffLzw:
			// x/image/tiff writes no LZW; Deflate is lossless too.
			comp = tiff.Deflate
		}
		err = tiff.Encode(w, img, &tiff.Options{Compression: comp, Predictor: true})
	case "bmp":
		err = bmp.Encode(w, img)
	case "gif":
		err = gif.Encode(w, img, &gif.Options{NumColors: 256})
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return []string{path}, nil
}

// Apply is the built-in subset of the image options: resize (when asked),
// rotation and mirroring, grayscale, and flattening for formats without alpha.
func Apply(img image.Image, o *presets.ImageOptions, target string, applyResize bool) image.Image {
	src := toNRGBA(img)
	if applyResize {
		src = resize(src, o)
	}
	src = turn(src, o)
	if o.Grayscale {
		for i := 0; i < len(src.Pix); i += 4 {
			y := uint8(math.Round(0.299*float64(src.Pix[i]) + 0.587*float64(src.Pix[i+1]) + 0.114*float64(src.Pix[i+2])))
			src.Pix[i], src.Pix[i+1], src.Pix[i+2] = y, y, y
		}
	}
	if target == "jpg" || target == "bmp" {
		bg := parseHex(o.Background, color.NRGBA{255, 255, 255, 255})
		flat := image.NewNRGBA(src.Bounds())
		draw.Draw(flat, flat.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), src, src.Bounds().Min, draw.Over)
		src = flat
	}
	return src
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

func resize(src *image.NRGBA, o *presets.ImageOptions) *image.NRGBA {
	w, h := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
	nw, nh := w, h
	if c := o.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		bw, bh := float64(c.ClampedWidth()), float64(c.ClampedHeight())
		if c.LockAspect {
			k := math.Min(bw/w, bh/h)
			nw, nh = w*k, h*k
		} else {
			nw, nh = bw, bh
		}
	} else if m := o.ClampedMinShortEdge(); m > 0 {
		if k := float64(m) / math.Min(w, h); k > 1 {
			nw, nh = w*k, h*k
		}
	} else {
		switch o.Resize {
		case presets.ResizePercent:
			if o.ResizePercent > 0 {
				nw, nh = w*float64(o.ResizePercent)/100, h*float64(o.ResizePercent)/100
			}
		case presets.ResizeLongEdge:
			if o.LongEdge > 0 {
				k := float64(o.LongEdge) / math.Max(w, h)
				nw, nh = w*k, h*k
			}
		case presets.ResizeWidth:
			if o.Width > 0 {
				k := float64(o.Width) / w
				nw, nh = w*k, h*k
			}
		}
	}
	iw, ih := max(1, int(math.Round(nw))), max(1, int(math.Round(nh)))
	if iw == int(w) && ih == int(h) {
		return src
	}
	out := image.NewNRGBA(image.Rect(0, 0, iw, ih))
	xdraw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return out
}

func turn(src *image.NRGBA, o *presets.ImageOptions) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	at := func(x, y int) []uint8 { return src.Pix[y*src.Stride+x*4 : y*src.Stride+x*4+4] }
	var out *image.NRGBA
	switch o.Rotation {
	case presets.Right90, presets.Left90:
		out = image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				nx, ny := h-1-y, x
				if o.Rotation == presets.Left90 {
					nx, ny = y, w-1-x
				}
				copy(out.Pix[ny*out.Stride+nx*4:], at(x, y))
			}
		}
	case presets.Rotate180:
		out = image.NewNRGBA(b)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				copy(out.Pix[(h-1-y)*out.Stride+(w-1-x)*4:], at(x, y))
			}
		}
	default:
		out = src
	}
	if o.Flip == presets.FlipHorizontal || o.Flip == presets.FlipVertical {
		ob := out.Bounds()
		f := image.NewNRGBA(ob)
		for y := 0; y < ob.Dy(); y++ {
			for x := 0; x < ob.Dx(); x++ {
				nx, ny := ob.Dx()-1-x, y
				if o.Flip == presets.FlipVertical {
					nx, ny = x, ob.Dy()-1-y
				}
				copy(f.Pix[ny*f.Stride+nx*4:ny*f.Stride+nx*4+4], out.Pix[y*out.Stride+x*4:y*out.Stride+x*4+4])
			}
		}
		out = f
	}
	return out
}

func parseHex(s string, def color.NRGBA) color.NRGBA {
	if len(s) == 7 && s[0] == '#' {
		var r, g, b uint8
		if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return color.NRGBA{r, g, b, 255}
		}
	}
	return def
}
