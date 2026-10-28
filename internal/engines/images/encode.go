package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"slices"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/jpegxl"
	"github.com/gen2brain/webp"
	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"golang.org/x/image/bmp"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// encodeFrame encodes one frame as format. ext is the extension written
// (ppm/pgm/pbm tell the Netpbm sub-format apart).
func encodeFrame(f *Frame, format, ext string) ([]byte, error) {
	var buf bytes.Buffer
	quality := f.enc.quality
	if quality <= 0 {
		quality = 90
	}
	switch format {
	case "png":
		data, err := encodePNG(f)
		return data, err
	case "jpg":
		var img image.Image
		if f.Gray {
			img = grayImage(f.Img)
		} else {
			img = rgbaView(flatten(f.Img, colorWhite))
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		return jpegWithMeta(buf.Bytes(), &f.Meta), nil
	case "webp":
		err := webp.Encode(&buf, f.Img, webp.Options{Quality: quality, Lossless: f.enc.webpLossless, Method: webp.DefaultMethod, Exact: f.enc.webpLossless})
		if err != nil {
			return nil, fmt.Errorf("WebP: %w", err)
		}
		return webpWithMeta(buf.Bytes(), &f.Meta, f.Width(), f.Height()), nil
	case "tiff":
		return encodeTIFF([]*Frame{f})
	case "bmp":
		var img image.Image = f.Img
		switch {
		case f.Gray && !f.Alpha:
			img = grayImage(f.Img)
		case !f.Alpha || opaque(f.Img):
			img = rgbaView(flatten(f.Img, colorWhite))
		}
		if err := bmp.Encode(&buf, img); err != nil {
			return nil, err
		}
		b := buf.Bytes()
		if f.Meta.DpiX > 0 && len(b) >= 46 {
			putLE32(b[38:], uint32(math.Round(f.Meta.DpiX/0.0254)))
			putLE32(b[42:], uint32(math.Round(f.Meta.DpiY/0.0254)))
		}
		return b, nil
	case "gif":
		if err := gif.EncodeAll(&buf, encodeGIF([]*Frame{f}, 0)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "ico", "cur":
		return encodeICO(f, format == "cur")
	case "avif":
		img := image.Image(f.Img)
		if !f.Alpha {
			img = rgbaView(flatten(f.Img, colorWhite))
		}
		if err := avif.Encode(&buf, img, avif.Options{Quality: quality, QualityAlpha: quality, Speed: 8, Lossless: quality == 100}); err != nil {
			return nil, fmt.Errorf("AVIF: %w", err)
		}
		return buf.Bytes(), nil
	case "jxl":
		// 100 means lossless (JPEG XL distance 0).
		if err := jpegxl.Encode(&buf, f.Img, jpegxl.Options{Quality: quality, Lossless: quality == 100, Effort: jpegxl.DefaultEffort}); err != nil {
			return nil, fmt.Errorf("JPEG XL: %w", err)
		}
		return buf.Bytes(), nil
	case "jp2":
		return encodeJP2(f, quality)
	case "psd", "psb":
		return encodePSD(f, format == "psb")
	case "tga":
		return encodeTGA(f), nil
	case "ppm", "pam", "pfm":
		return encodeNetpbm(f, ext), nil
	case "apng":
		return encodeAPNG([]*Frame{f}, 0)
	case "mng":
		return encodeMNG([]*Frame{f}, 0)
	case "qoi":
		return encodeQOI(f), nil
	case "dds":
		return encodeDDS(f)
	case "pcx":
		return encodePCX(f), nil
	case "xpm":
		return encodeXPM(f), nil
	case "xbm":
		return encodeXBM(f), nil
	case "wbmp":
		return encodeWBMP(f), nil
	case "sgi":
		return encodeSGI(f), nil
	case "ras":
		return encodeSunRaster(f), nil
	case "miff":
		return encodeMIFF([]*Frame{f}), nil
	}
	return nil, fmt.Errorf("the image engine cannot write %q", format)
}

// encodeMulti encodes several frames into one file of format (pages or an animation).
func encodeMulti(frames []*Frame, format string, loops int) ([]byte, error) {
	switch format {
	case "tiff":
		return encodeTIFF(frames)
	case "gif":
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, encodeGIF(frames, loops)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "apng":
		return encodeAPNG(frames, loops)
	case "mng":
		return encodeMNG(frames, loops)
	case "miff":
		return encodeMIFF(frames), nil
	case "webp":
		anim := &webp.WEBP{LoopCount: loops}
		w, h := 0, 0
		for _, f := range frames {
			w, h = max(w, f.Width()), max(h, f.Height())
		}
		for _, f := range frames {
			img := f.Img
			if f.Width() != w || f.Height() != h {
				img = extent(img, w, h, 0, 0, colorTransparent)
			}
			anim.Image = append(anim.Image, img)
			anim.Delay = append(anim.Delay, int(math.Round(f.DelayMs())))
		}
		q := frames[0].enc.quality
		if q <= 0 {
			q = 90
		}
		var buf bytes.Buffer
		if err := webp.EncodeAll(&buf, anim, webp.Options{Quality: q, Lossless: frames[0].enc.webpLossless, Method: webp.DefaultMethod}); err != nil {
			return nil, fmt.Errorf("WebP: %w", err)
		}
		return buf.Bytes(), nil
	}
	return encodeFrame(frames[0], format, format)
}

func encodePNG(f *Frame) ([]byte, error) {
	var img image.Image = f.Img
	switch {
	case f.Gray && !f.Alpha:
		img = grayImage(f.Img)
	case !f.Alpha:
		img = rgbaView(flatten(f.Img, colorWhite))
	default:
		if p := paletteOf(f.Img); p != nil {
			img = p
		}
	}
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if f.enc.pngMax {
		enc.CompressionLevel = png.BestCompression
	}
	var buf bytes.Buffer
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return pngWithMeta(buf.Bytes(), &f.Meta), nil
}

// paletteOf is img as a palette picture when it has 256 colours or fewer
// (smaller files, as ImageMagick writes them), or nil.
func paletteOf(img *image.NRGBA) *image.Paletted {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w*h < 1024 {
		return nil
	}
	index := map[[4]uint8]uint8{}
	var pal [][4]uint8
	out := image.NewPaletted(image.Rect(0, 0, w, h), nil)
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			var k [4]uint8
			copy(k[:], row[x*4:x*4+4])
			if k[3] == 0 {
				k = [4]uint8{}
			}
			i, ok := index[k]
			if !ok {
				if len(index) == 256 {
					return nil
				}
				i = uint8(len(index))
				index[k] = i
				pal = append(pal, k)
			}
			out.Pix[y*out.Stride+x] = i
		}
	}
	for _, c := range pal {
		out.Palette = append(out.Palette, color.NRGBA{c[0], c[1], c[2], c[3]})
	}
	return out
}

func encodeJP2(f *Frame, quality int) ([]byte, error) {
	o := jpeg2000.DefaultOptions()
	o.Format = jpeg2000.FormatJP2
	if quality >= 100 {
		// reversible 5/3 wavelet
		o.Lossless = true
	} else {
		o.Lossless = false
		o.Quality = quality
	}
	if len(f.Meta.ICC) > 0 {
		o.ICCProfile = f.Meta.ICC
	}
	var img image.Image = f.Img
	switch {
	case f.Gray && !f.Alpha:
		img = grayImage(f.Img)
	case !f.Alpha:
		img = rgbaView(flatten(f.Img, colorWhite))
	}
	var buf bytes.Buffer
	if err := jpeg2000.Encode(&buf, img, o); err != nil {
		return nil, fmt.Errorf("JPEG 2000: %w", err)
	}
	return buf.Bytes(), nil
}

// Write writes frames through the allocator. Multi-frame targets (and
// animations) get one file; other targets get one file per frame with a _p{n}
// suffix when there is more than one. Single pictures are fitted to the
// preset's size limit.
func Write(frames []*Frame, target string, out convert.OutputAllocator, o *presets.ImageOptions, sourcePath string, animation bool, loops int) ([]string, error) {
	if len(frames) == 0 {
		return nil, errors.New("the picture has no image in it")
	}
	ext := OutputExtension(target, o, sourcePath)
	var written []string
	if target == "apng" || (len(frames) > 1 && (slices.Contains(multiFrame, target) || animation)) {
		path := out.Allocate(ext, "")
		if path == "" {
			return nil, nil
		}
		var data []byte
		var err error
		if target == "apng" {
			data, err = encodeAPNG(frames, loops)
		} else {
			data, err = encodeMulti(frames, target, loops)
		}
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	for i, f := range frames {
		suffix := ""
		if len(frames) > 1 {
			suffix = fmt.Sprintf("_p%d", i+1)
		}
		path := out.Allocate(ext, suffix)
		if path == "" {
			continue
		}
		data, err := encodeFrame(f, target, ext)
		if err != nil {
			return written, err
		}
		if o != nil && o.MaxSizeMb > 0 {
			data, err = FitToSize(f, data, target, ext, sizeLimit(o.MaxSizeMb))
			if err != nil {
				return written, err
			}
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}

// sizeLimit is megabytes of 1,000,000 bytes.
func sizeLimit(mb float64) int64 { return int64(mb * 1_000_000) }

// describeSize is "12.3 MB > 10 MB".
func describeSize(size, limit int64) string {
	return fmt.Sprintf("%s MB > %s MB", trimNumber(float64(size)/1e6), trimNumber(float64(limit)/1e6))
}

func trimNumber(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	for s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// MinFittedLongEdge: a fitted picture is never made smaller than this long
// edge (nor than its own): below it, it's a thumbnail.
const MinFittedLongEdge = 640

// FitToSize encodes the frame again until it is at most limit bytes: a lower
// quality first (down to 50), then fewer pixels, but never below a long edge
// of MinFittedLongEdge. It refuses when it still doesn't fit.
func FitToSize(f *Frame, data []byte, format, ext string, limit int64) ([]byte, error) {
	if int64(len(data)) <= limit {
		return data, nil
	}
	c := f.Clone()
	if c.enc.quality <= 0 {
		c.enc.quality = 90
	}
	lossy := format == "jpg" || format == "webp" && !c.enc.webpLossless || format == "avif" || format == "jxl" || format == "jp2"
	floor := min(MinFittedLongEdge, max(c.Width(), c.Height()))
	size := int64(len(data))
	for size > limit {
		if lossy && c.enc.quality > 50 {
			c.enc.quality = max(50, c.enc.quality-10)
		} else {
			long := max(c.Width(), c.Height())
			if long <= floor {
				break
			}
			scale := math.Max(math.Max(0.3, math.Min(0.9, math.Sqrt(float64(limit)/float64(size))*0.95)), float64(floor)/float64(long))
			c.Img = Resize(c.Img, roundSide(float64(c.Width())*scale), roundSide(float64(c.Height())*scale))
			if c.enc.icoSizes != nil {
				c.enc.icoSizes = nil
			}
		}
		var err error
		data, err = encodeFrame(c, format, ext)
		if err != nil {
			return nil, err
		}
		size = int64(len(data))
	}
	if size > limit {
		return nil, convert.Refuse("error.over_size_limit", describeSize(size, limit))
	}
	return data, nil
}

// EncodeImage encodes img as format with the encoder settings of o (quality,
// WebP lossless, PNG compression, TIFF compression, icon sizes; nil for the
// defaults). Nothing else of o is applied: see ApplyOptions for that.
func EncodeImage(img image.Image, format string, o *presets.ImageOptions) ([]byte, error) {
	f := NewFrame(img)
	if o == nil {
		d := presets.DefaultImageOptions()
		o = &d
	}
	f.enc = encodeSettings{quality: max(1, min(100, o.Quality)), webpLossless: o.WebpLossless, pngMax: o.MaxCompression, tiff: string(o.TiffCompression)}
	if format == "ico" {
		prepareIcon(f, o)
	}
	if !HoldsAlpha(format) && f.Alpha {
		f.Img = flatten(f.Img, backgroundColor(o.Background))
		f.Alpha = false
	}
	if format == "apng" {
		return encodeAPNG([]*Frame{f}, 0)
	}
	return encodeFrame(f, format, format)
}

// Encode writes img to path as format (see EncodeImage).
func Encode(path string, img image.Image, format string, o *presets.ImageOptions) error {
	data, err := EncodeImage(img, format, o)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func rgbaView(img *image.NRGBA) *image.RGBA {
	return &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
}

func putLE32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}
