// Pictures for the PDF engines: decoding the images that go into a PDF and
// encoding rendered pages, with the preset's picture options (turn, resize,
// grayscale, …). The image engine has its own, fuller version; this one is
// kept local so the PDF engines stand on their own.

package pdf

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/gen2brain/avif"
	_ "github.com/gen2brain/heic"
	_ "github.com/gen2brain/jpegxl"
	"github.com/gen2brain/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/presets"
)

// imageSources are the pictures this package decodes itself (image → PDF).
var imageSources = []string{"png", "jpg", "webp", "tiff", "bmp", "gif", "avif", "heic", "jxl"}

// imageTargets are the pictures rendered pages are written as; the image
// engine takes them on to the rest.
var imageTargets = []string{"png", "jpg", "webp", "tiff", "bmp", "gif", "avif", "ppm", "tga"}

// multiPageTargets hold several pages in one file.
var multiPageTargets = map[string]bool{"tiff": true, "gif": true}

// noAlpha formats are written without transparency.
var noAlpha = map[string]bool{"jpg": true, "ppm": true}

// maxSide is the longest side a format stores.
func maxSide(format string) float64 {
	switch format {
	case "jpg":
		return 65_500
	case "webp":
		return 16_383
	case "avif":
		return 16_384
	}
	return math.Inf(1)
}

// picture is a decoded frame with what the file said about it.
type picture struct {
	Image image.Image
	// Dpi is the resolution the file gives (0 when it gives none).
	Dpi float64
	// Jpeg is the file's own JPEG data when the picture is a plain JPEG.
	Jpeg []byte
}

// readPictures decodes every page of a picture file: TIFF pages, the first
// frame of an animation (a PDF gets one page per picture, not per frame).
func readPictures(path, format string) ([]picture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch format {
	case "tiff":
		return readTiffPages(data)
	case "gif":
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(g.Image) == 0 {
			return nil, fmt.Errorf("the GIF has no pictures")
		}
		// The first frame drawn on the logical screen, as a viewer shows it.
		w, h := g.Config.Width, g.Config.Height
		if w <= 0 || h <= 0 {
			w, h = g.Image[0].Bounds().Max.X, g.Image[0].Bounds().Max.Y
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(canvas, g.Image[0].Bounds(), g.Image[0], g.Image[0].Bounds().Min, draw.Over)
		return []picture{{Image: canvas}}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not read the picture: %w", err)
	}
	p := picture{Image: img}
	switch format {
	case "jpg":
		p.Dpi = jpegDpi(data)
		if o := jpegOrientation(data); o > 1 {
			p.Image = orient(img, o)
		} else {
			p.Jpeg = data
		}
	case "png":
		p.Dpi = pngDpi(data)
	case "bmp":
		p.Dpi = bmpDpi(data)
	}
	return []picture{p}, nil
}

// readTiffPages decodes every page (IFD) of a TIFF: each is decoded as if it
// were the first, by pointing the header at it.
func readTiffPages(data []byte) ([]picture, error) {
	offsets, order, err := tiffIFDs(data)
	if err != nil || len(offsets) <= 1 {
		img, err := tiff.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("could not read the TIFF: %w", err)
		}
		return []picture{{Image: img, Dpi: tiffDpi(data, 0)}}, nil
	}
	var out []picture
	patched := make([]byte, len(data))
	for i, off := range offsets {
		copy(patched, data)
		order.PutUint32(patched[4:8], off)
		img, err := tiff.Decode(bytes.NewReader(patched))
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("could not read the TIFF: %w", err)
			}
			continue // a thumbnail or page in a form the decoder doesn't know
		}
		out = append(out, picture{Image: img, Dpi: tiffDpi(data, i)})
	}
	return out, nil
}

// tiffIFDs lists the offsets of a TIFF's IFDs (pages).
func tiffIFDs(data []byte) ([]uint32, binary.ByteOrder, error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("short TIFF")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, nil, fmt.Errorf("not a TIFF")
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, nil, fmt.Errorf("BigTIFF or not a TIFF")
	}
	var offsets []uint32
	seen := map[uint32]bool{}
	for off := order.Uint32(data[4:8]); off != 0 && !seen[off] && int(off)+2 <= len(data) && len(offsets) < 10000; {
		seen[off] = true
		offsets = append(offsets, off)
		n := int(order.Uint16(data[off:]))
		next := int(off) + 2 + 12*n
		if next+4 > len(data) {
			break
		}
		off = order.Uint32(data[next:])
	}
	return offsets, order, nil
}

// tiffDpi reads XResolution / ResolutionUnit of page n.
func tiffDpi(data []byte, n int) float64 {
	offsets, order, err := tiffIFDs(data)
	if err != nil || n >= len(offsets) {
		return 0
	}
	off := int(offsets[n])
	count := int(order.Uint16(data[off:]))
	var res float64
	unit := 2
	for i := 0; i < count; i++ {
		e := off + 2 + 12*i
		if e+12 > len(data) {
			break
		}
		tag := order.Uint16(data[e:])
		switch tag {
		case 282: // XResolution, a RATIONAL
			p := int(order.Uint32(data[e+8:]))
			if p+8 <= len(data) {
				num, den := order.Uint32(data[p:]), order.Uint32(data[p+4:])
				if den != 0 {
					res = float64(num) / float64(den)
				}
			}
		case 296:
			unit = int(order.Uint16(data[e+8:]))
		}
	}
	switch unit {
	case 2:
		return res
	case 3:
		return res * 2.54
	}
	return 0
}

func pngDpi(data []byte) float64 {
	for i := 8; i+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[i:]))
		kind := string(data[i+4 : i+8])
		if kind == "pHYs" && n >= 9 && i+8+9 <= len(data) {
			x := binary.BigEndian.Uint32(data[i+8:])
			if data[i+16] == 1 { // per metre
				return float64(x) * 0.0254
			}
			return 0
		}
		if kind == "IDAT" || n < 0 {
			return 0
		}
		i += 12 + n
	}
	return 0
}

func bmpDpi(data []byte) float64 {
	if len(data) < 46 {
		return 0
	}
	ppm := int32(binary.LittleEndian.Uint32(data[38:]))
	if ppm <= 0 {
		return 0
	}
	return float64(ppm) * 0.0254
}

// jpegSegments calls f for every marker segment before the image data.
func jpegSegments(data []byte, f func(marker byte, body []byte) bool) {
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return
		}
		marker := data[i+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 || marker == 0xFF {
			i++
			continue
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return
		}
		if !f(marker, data[i+4:i+2+n]) || marker == 0xDA {
			return
		}
		i += 2 + n
	}
}

func jpegDpi(data []byte) float64 {
	var dpi float64
	jpegSegments(data, func(marker byte, body []byte) bool {
		if marker == 0xE0 && len(body) >= 12 && string(body[:5]) == "JFIF\x00" {
			x := float64(binary.BigEndian.Uint16(body[8:]))
			switch body[7] {
			case 1:
				dpi = x
			case 2:
				dpi = x * 2.54
			}
			return false
		}
		return true
	})
	return dpi
}

// jpegOrientation is the EXIF orientation (1–8; 0 when there is none).
func jpegOrientation(data []byte) int {
	orientation := 0
	jpegSegments(data, func(marker byte, body []byte) bool {
		if marker != 0xE1 || len(body) < 14 || string(body[:6]) != "Exif\x00\x00" {
			return true
		}
		t := body[6:]
		var order binary.ByteOrder
		switch string(t[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return false
		}
		off := int(order.Uint32(t[4:]))
		if off+2 > len(t) {
			return false
		}
		n := int(order.Uint16(t[off:]))
		for i := 0; i < n; i++ {
			e := off + 2 + 12*i
			if e+12 > len(t) {
				break
			}
			if order.Uint16(t[e:]) == 0x0112 {
				orientation = int(order.Uint16(t[e+8:]))
			}
		}
		return false
	})
	return orientation
}

// orient turns a picture upright by its EXIF orientation.
func orient(img image.Image, o int) image.Image {
	switch o {
	case 2:
		return flipH(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipV(img)
	case 5:
		return flipH(rotate90(img))
	case 6:
		return rotate90(img)
	case 7:
		return flipH(rotate270(img))
	case 8:
		return rotate270(img)
	}
	return img
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// rotate90 turns clockwise.
func rotate90(img image.Image) image.Image {
	src := toNRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			copy(out.Pix[out.PixOffset(h-1-y, x):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return out
}

func rotate270(img image.Image) image.Image {
	src := toNRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			copy(out.Pix[out.PixOffset(y, w-1-x):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return out
}

func rotate180(img image.Image) image.Image { return flipV(flipH(img)) }

func flipH(img image.Image) image.Image {
	src := toNRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewNRGBA(src.Rect)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			copy(out.Pix[out.PixOffset(w-1-x, y):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return out
}

func flipV(img image.Image) image.Image {
	src := toNRGBA(img)
	h := src.Rect.Dy()
	out := image.NewNRGBA(src.Rect)
	for y := 0; y < h; y++ {
		copy(out.Pix[out.PixOffset(0, h-1-y):][:src.Stride], src.Pix[src.PixOffset(0, y):][:src.Stride])
	}
	return out
}

// turn applies the preset's rotation and mirroring.
func turn(img image.Image, o *presets.ImageOptions) image.Image {
	switch o.Rotation {
	case presets.Right90:
		img = rotate90(img)
	case presets.Left90:
		img = rotate270(img)
	case presets.Rotate180:
		img = rotate180(img)
	}
	switch o.Flip {
	case presets.FlipHorizontal:
		img = flipH(img)
	case presets.FlipVertical:
		img = flipV(img)
	}
	return img
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

// resizeScale is how much applyOptions resizes a width × height picture
// (before the format's own size limit).
func resizeScale(width, height float64, o *presets.ImageOptions) float64 {
	if c := o.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		if c.LockAspect {
			return min(float64(c.ClampedWidth())/width, float64(c.ClampedHeight())/height)
		}
		return float64(c.ClampedHeight()) / height
	}
	if m := o.ClampedMinShortEdge(); m > 0 {
		return minShortEdgeScale(width, height, m)
	}
	switch o.Resize {
	case presets.ResizePercent:
		if o.ResizePercent > 0 {
			return float64(o.ResizePercent) / 100
		}
	case presets.ResizeLongEdge:
		if o.LongEdge > 0 {
			return min(1, float64(o.LongEdge)/max(width, height))
		}
	case presets.ResizeWidth:
		if o.Width > 0 {
			return min(1, float64(o.Width)/width)
		}
	}
	return 1
}

const maxEnlargedPixels = float64(presets.MaxSide) * presets.MaxSide

func minShortEdgeScale(width, height float64, minShortEdge int) float64 {
	scale := float64(minShortEdge) / min(width, height)
	if scale <= 1 {
		return 1
	}
	return max(1, min(scale, math.Sqrt(maxEnlargedPixels/(width*height))))
}

// resize applies the preset's size options.
func resize(img image.Image, o *presets.ImageOptions) image.Image {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	if c := o.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		if c.LockAspect {
			s := min(float64(c.ClampedWidth())/w, float64(c.ClampedHeight())/h)
			return scaleTo(img, int(math.Max(1, math.Round(w*s))), int(math.Max(1, math.Round(h*s))))
		}
		return scaleTo(img, c.ClampedWidth(), c.ClampedHeight())
	}
	if m := o.ClampedMinShortEdge(); m > 0 {
		if s := minShortEdgeScale(w, h, m); s > 1 {
			return scaleTo(img, int(math.Round(w*s)), int(math.Round(h*s)))
		}
		return img
	}
	switch o.Resize {
	case presets.ResizePercent:
		if o.ResizePercent > 0 && o.ResizePercent != 100 {
			s := float64(o.ResizePercent) / 100
			return scaleTo(img, int(math.Max(1, math.Round(w*s))), int(math.Max(1, math.Round(h*s))))
		}
	case presets.ResizeLongEdge:
		if l := float64(o.LongEdge); l > 0 && max(w, h) > l {
			s := l / max(w, h)
			return scaleTo(img, int(math.Max(1, math.Round(w*s))), int(math.Max(1, math.Round(h*s))))
		}
	case presets.ResizeWidth:
		if o.Width > 0 && b.Dx() > o.Width {
			return scaleTo(img, o.Width, int(math.Max(1, math.Round(h*float64(o.Width)/w))))
		}
	}
	return img
}

// fitFormat scales down until both sides fit what the format stores.
func fitFormat(img image.Image, format string) image.Image {
	b := img.Bounds()
	s := min(1, maxSide(format)/float64(max(b.Dx(), b.Dy())))
	if s >= 1 {
		return img
	}
	return scaleTo(img, int(math.Max(1, math.Floor(float64(b.Dx())*s))), int(math.Max(1, math.Floor(float64(b.Dy())*s))))
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

// parseColor reads "#RRGGBB" (white when it can't).
func parseColor(s string) color.NRGBA {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 6 {
		if v, err := strconv.ParseUint(s, 16, 32); err == nil {
			return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}
		}
	}
	return color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
}

// flatten draws the picture on a solid background.
func flatten(img image.Image, bg color.Color) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}

func grayscale(img image.Image) image.Image {
	b := img.Bounds()
	if !hasAlpha(img) {
		out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	src := toNRGBA(img)
	out := image.NewNRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		y := uint8((19595*uint32(src.Pix[i]) + 38470*uint32(src.Pix[i+1]) + 7471*uint32(src.Pix[i+2]) + 1<<15) >> 16)
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = y, y, y, src.Pix[i+3]
	}
	return out
}

// applyOptions applies the preset's picture options to one frame for target
// (turn, resize, the format's size limit, grayscale, no transparency where
// the format has none). Pixelating areas and removing backgrounds are the
// image engine's.
func applyOptions(img image.Image, o *presets.ImageOptions, target string) image.Image {
	img = turn(img, o)
	img = resize(img, o)
	img = fitFormat(img, target)
	if o.Grayscale {
		img = grayscale(img)
	}
	if noAlpha[target] && hasAlpha(img) {
		img = flatten(img, parseColor(o.Background))
	}
	return img
}

// encodeImage writes one picture in format.
func encodeImage(w io.Writer, img image.Image, format string, o *presets.ImageOptions, dpi int) error {
	quality := max(1, min(100, o.Quality))
	switch format {
	case "png":
		e := png.Encoder{CompressionLevel: png.DefaultCompression}
		if o.MaxCompression {
			e.CompressionLevel = png.BestCompression
		}
		return e.Encode(w, img)
	case "jpg":
		if hasAlpha(img) {
			img = flatten(img, parseColor(o.Background))
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	case "webp":
		return webp.Encode(w, img, webp.Options{Quality: quality, Lossless: o.WebpLossless, Method: 4})
	case "avif":
		return avif.Encode(w, img, avif.Options{Quality: quality, QualityAlpha: quality, Speed: 8})
	case "bmp":
		return bmp.Encode(w, img)
	case "gif":
		return gif.Encode(w, img, &gif.Options{NumColors: 256, Drawer: draw.FloydSteinberg})
	case "tiff":
		return writeTiff(w, []image.Image{img}, o.TiffCompression, dpi)
	case "ppm":
		return writePnm(w, img)
	case "tga":
		return writeTga(w, img)
	}
	return fmt.Errorf("cannot write %s pictures", format)
}

// encodeMultiPage writes several pages into one TIFF or GIF.
func encodeMultiPage(w io.Writer, frames []image.Image, format string, o *presets.ImageOptions, dpi int) error {
	if format == "tiff" {
		return writeTiff(w, frames, o.TiffCompression, dpi)
	}
	g := &gif.GIF{}
	for _, f := range frames {
		g.Config.Width = max(g.Config.Width, f.Bounds().Dx())
		g.Config.Height = max(g.Config.Height, f.Bounds().Dy())
	}
	for _, f := range frames {
		b := f.Bounds()
		p := image.NewPaletted(image.Rect(0, 0, b.Dx(), b.Dy()), webSafeWithWhite())
		draw.FloydSteinberg.Draw(p, p.Bounds(), f, b.Min)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 100)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	return gif.EncodeAll(w, g)
}

func webSafeWithWhite() color.Palette {
	var p color.Palette
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				p = append(p, color.RGBA{uint8(r * 51), uint8(g * 51), uint8(b * 51), 0xFF})
			}
		}
	}
	for i := 1; len(p) < 256; i++ {
		v := uint8(i * 255 / 41)
		p = append(p, color.RGBA{v, v, v, 0xFF})
	}
	return p
}

func isGray(img image.Image) bool {
	_, ok := img.(*image.Gray)
	return ok
}

// writePnm writes binary PPM (or PGM for a grayscale picture).
func writePnm(w io.Writer, img image.Image) error {
	bw := bufio.NewWriter(w)
	b := img.Bounds()
	if g, ok := img.(*image.Gray); ok {
		fmt.Fprintf(bw, "P5\n%d %d\n255\n", b.Dx(), b.Dy())
		for y := 0; y < b.Dy(); y++ {
			bw.Write(g.Pix[y*g.Stride : y*g.Stride+b.Dx()])
		}
		return bw.Flush()
	}
	rgb := flatten(img, color.White)
	fmt.Fprintf(bw, "P6\n%d %d\n255\n", b.Dx(), b.Dy())
	for i := 0; i < len(rgb.Pix); i += 4 {
		bw.Write(rgb.Pix[i : i+3])
	}
	return bw.Flush()
}

// writeTga writes an uncompressed 32-bit TGA, top-left origin.
func writeTga(w io.Writer, img image.Image) error {
	src := toNRGBA(img)
	b := src.Rect
	h := make([]byte, 18)
	h[2] = 2
	binary.LittleEndian.PutUint16(h[12:], uint16(b.Dx()))
	binary.LittleEndian.PutUint16(h[14:], uint16(b.Dy()))
	h[16] = 32
	h[17] = 0x28 // 8 alpha bits, top-left origin
	if b.Dx() > 0xFFFF || b.Dy() > 0xFFFF {
		return fmt.Errorf("a TGA holds at most 65,535 × 65,535 pixels")
	}
	bw := bufio.NewWriter(w)
	bw.Write(h)
	px := make([]byte, 4)
	for i := 0; i < len(src.Pix); i += 4 {
		px[0], px[1], px[2], px[3] = src.Pix[i+2], src.Pix[i+1], src.Pix[i], src.Pix[i+3]
		bw.Write(px)
	}
	return bw.Flush()
}

func drawSrc(dst draw.Image, src image.Image) {
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
}
