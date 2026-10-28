package media

// Pictures read in Go for HEIC and Live Photo writing: the image formats Go
// decodes in-process (others reach HEIC through PNG from the image engines),
// turned upright by their Exif orientation and edited as the preset says
// (rotate, flip, resize, grayscale), transparency flattened on the background.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/dseif0x/filee-go/internal/presets"
)

// HeicSources are the pictures the FFmpeg engine writes as HEIC itself.
var HeicSources = []string{"png", "jpg", "gif", "bmp", "tiff", "webp"}

type preparedPicture struct {
	png           string
	width, height int
	icc, exif     []byte
}

func decodePicture(data []byte, format string) (image.Image, error) {
	r := bytes.NewReader(data)
	switch format {
	case "png":
		return png.Decode(r)
	case "jpg":
		return jpeg.Decode(r)
	case "gif":
		return gif.Decode(r) // the first frame of an animation
	case "bmp":
		return bmp.Decode(r)
	case "tiff":
		return tiff.Decode(r)
	case "webp":
		return webp.Decode(r)
	}
	img, _, err := image.Decode(r)
	return img, err
}

func preparePictures(path, format string, o *presets.ImageOptions, workDir string) ([]preparedPicture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := decodePicture(data, format)
	if err != nil {
		return nil, fmt.Errorf("cannot read the picture: %w", err)
	}
	var icc, exif []byte
	switch format {
	case "jpg":
		icc = JpegIcc(data)
		if t := JpegExif(data); t != nil {
			img = orient(img, TiffOrientation(t))
			if o.KeepMetadata {
				exif = UprightExif(t)
			}
		}
	case "png":
		icc = PngIcc(data)
	}
	if !o.KeepColorProfile {
		icc = nil
	}
	img = EditPicture(img, o)
	// HEIC is written without alpha: transparency on the preset's background.
	rgba := flatten(img, parseColor(o.Background))
	out := filepath.Join(workDir, "heic-"+randomName()+".png")
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, rgba); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	b := rgba.Bounds()
	return []preparedPicture{{out, b.Dx(), b.Dy(), icc, exif}}, nil
}

// EditPicture applies the preset's rotation, flip, size and grayscale.
func EditPicture(img image.Image, o *presets.ImageOptions) image.Image {
	switch o.Rotation {
	case presets.Right90:
		img = orient(img, 6)
	case presets.Left90:
		img = orient(img, 8)
	case presets.Rotate180:
		img = orient(img, 3)
	}
	switch o.Flip {
	case presets.FlipHorizontal:
		img = orient(img, 2)
	case presets.FlipVertical:
		img = orient(img, 4)
	}
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	nw, nh := w, h
	switch o.Resize {
	case presets.ResizePercent:
		f := float64(min(max(o.ResizePercent, 1), 1000)) / 100
		nw, nh = w*f, h*f
	case presets.ResizeLongEdge:
		if l := float64(o.LongEdge); l > 0 && max(w, h) > l {
			f := l / max(w, h)
			nw, nh = w*f, h*f
		}
	case presets.ResizeWidth:
		if l := float64(o.Width); l > 0 && w > l {
			nw, nh = l, h*l/w
		}
	case presets.ResizeCustom:
		if c := o.CustomSize; c != nil {
			cw, ch := float64(c.ClampedWidth()), float64(c.ClampedHeight())
			if c.LockAspect {
				f := min(cw/w, ch/h)
				nw, nh = w*f, h*f
			} else {
				nw, nh = cw, ch
			}
		}
	case presets.ResizeMinShortEdge:
		if s := float64(o.ClampedMinShortEdge()); s > 0 && min(w, h) < s {
			f := s / min(w, h)
			nw, nh = w*f, h*f
		}
	}
	if iw, ih := max(1, int(math.Round(nw))), max(1, int(math.Round(nh))); iw != b.Dx() || ih != b.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, iw, ih))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	if o.Grayscale {
		b := img.Bounds()
		g := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				l := color.GrayModel.Convert(color.NRGBA{c.R, c.G, c.B, 255}).(color.Gray).Y
				g.Set(x, y, color.NRGBA{l, l, l, c.A})
			}
		}
		img = g
	}
	return img
}

// orient turns or mirrors img by an Exif orientation (1–8).
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flatten(img image.Image, bg color.Color) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}

// parseColor reads "#RRGGBB" (white when unreadable).
func parseColor(s string) color.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 6 {
		if v, err := strconv.ParseUint(s, 16, 32); err == nil {
			return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
		}
	}
	return color.White
}
