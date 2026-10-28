package images

import (
	"image"
	"image/color"
	"image/draw"
)

var (
	colorWhite       = color.NRGBA{255, 255, 255, 255}
	colorTransparent = color.NRGBA{}
)

// Meta is what a picture carries besides its pixels.
type Meta struct {
	// ICC is the colour profile, Exif the Exif data from its TIFF header on
	// ("II*\0" / "MM\0*", without the "Exif\0\0" of JPEG), XMP the XMP packet.
	ICC, Exif, XMP []byte
	// DpiX and DpiY are the resolution in pixels per inch (0 = unknown).
	DpiX, DpiY float64
	// Orientation is the Exif orientation (1–8; 0 = none).
	Orientation int
	// Quality is the JPEG quality the file was saved with (estimated from its
	// quantization tables), 0 when it is not a JPEG.
	Quality int
	// Comment is a text comment (GIF, JPEG, PNG tEXt "Comment").
	Comment string
}

// Frame is one picture of a file (a page, an animation frame, an icon size) at
// 8 bits per channel with straight alpha, as ImageMagick's Q8 build holds it.
type Frame struct {
	Img *image.NRGBA
	// Alpha: the picture has an alpha channel (it may still be opaque).
	Alpha bool
	// Gray: the picture is grey (written as grey where the format has it).
	Gray bool
	// Delay is the time the frame is shown, in Ticks per second (0 means 100).
	Delay, Ticks int
	Meta         Meta

	// set by ApplyOptions for the encoder
	enc encodeSettings
}

// Picture is a decoded file: its frames and, for animations, the loop count.
type Picture struct {
	Frames []*Frame
	// Loops is how often an animation plays (0 = forever).
	Loops int
}

// encodeSettings are the per-format choices ApplyOptions makes (ImageMagick's
// image settings).
type encodeSettings struct {
	quality      int
	webpLossless bool
	pngMax       bool
	tiff         string // "None", "Lzw", "Zip"
	icoSizes     []int
	subfile2     bool // TIFF SubfileType 2 (a page of a document)
}

// NewFrame wraps img (any image) as a frame. Grey comes from its type; it has
// alpha when any pixel is not opaque.
func NewFrame(img image.Image) *Frame {
	f := &Frame{Img: ToNRGBA(img)}
	switch img.(type) {
	case *image.Gray, *image.Gray16:
		f.Gray = true
	case *image.YCbCr, *image.CMYK:
	default:
		f.Alpha = !opaque(f.Img)
	}
	return f
}

func hasTransparentEntry(p color.Palette) bool {
	for _, c := range p {
		if _, _, _, a := c.RGBA(); a != 0xffff {
			return true
		}
	}
	return false
}

// ToNRGBA converts any image to *image.NRGBA with its origin at 0,0.
func ToNRGBA(img image.Image) *image.NRGBA {
	b := img.Bounds()
	if n, ok := img.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return n
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	switch src := img.(type) {
	case *image.NRGBA:
		for y := 0; y < b.Dy(); y++ {
			copy(out.Pix[y*out.Stride:y*out.Stride+b.Dx()*4], src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):])
		}
		return out
	case *image.Gray:
		for y := 0; y < b.Dy(); y++ {
			row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
			o := out.Pix[y*out.Stride:]
			for x := 0; x < b.Dx(); x++ {
				v := row[x]
				o[x*4], o[x*4+1], o[x*4+2], o[x*4+3] = v, v, v, 255
			}
		}
		return out
	case *image.YCbCr, *image.RGBA:
		// image/draw has fast paths into *image.RGBA; opaque RGBA is the same bytes as NRGBA
		rgba := image.NewRGBA(out.Rect)
		draw.Draw(rgba, rgba.Rect, img, b.Min, draw.Src)
		if _, ok := img.(*image.YCbCr); ok {
			out.Pix = rgba.Pix
			return out
		}
		return unpremultiplied(rgba)
	case *image.CMYK:
		for y := 0; y < b.Dy(); y++ {
			row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
			o := out.Pix[y*out.Stride:]
			for x := 0; x < b.Dx(); x++ {
				r, g, bb := color.CMYKToRGB(row[x*4], row[x*4+1], row[x*4+2], row[x*4+3])
				o[x*4], o[x*4+1], o[x*4+2], o[x*4+3] = r, g, bb, 255
			}
		}
		return out
	}
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// Clone returns a deep copy of the frame.
func (f *Frame) Clone() *Frame {
	c := *f
	c.Img = image.NewNRGBA(f.Img.Rect)
	copy(c.Img.Pix, f.Img.Pix)
	c.enc.icoSizes = append([]int(nil), f.enc.icoSizes...)
	return &c
}

// Width and Height of the frame.
func (f *Frame) Width() int  { return f.Img.Rect.Dx() }
func (f *Frame) Height() int { return f.Img.Rect.Dy() }

// DelayMs is the frame's delay in milliseconds.
func (f *Frame) DelayMs() float64 {
	t := f.Ticks
	if t <= 0 {
		t = 100
	}
	return float64(f.Delay) * 1000 / float64(t)
}

// opaque reports whether every pixel is fully opaque.
func opaque(img *image.NRGBA) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for i := 3; i < len(row); i += 4 {
			if row[i] != 255 {
				return false
			}
		}
	}
	return true
}

// isGray reports whether every pixel is grey.
func isGray(img *image.NRGBA) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for i := 0; i < len(row); i += 4 {
			if row[i] != row[i+1] || row[i] != row[i+2] {
				return false
			}
		}
	}
	return true
}

// hasAlpha: the frame has an alpha channel that is not opaque everywhere.
func (f *Frame) hasVisibleAlpha() bool { return f.Alpha && !opaque(f.Img) }

// grayImage returns the frame's luminance as *image.Gray (the red channel of a grey frame).
func grayImage(img *image.NRGBA) *image.Gray {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	g := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		o := g.Pix[y*g.Stride:]
		for x := 0; x < w; x++ {
			o[x] = row[x*4]
		}
	}
	return g
}

// rgbaImage returns img premultiplied (for the encoders that take it).
func flatten(img *image.NRGBA, bg color.NRGBA) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		s := img.Pix[y*img.Stride : y*img.Stride+w*4]
		d := out.Pix[y*out.Stride:]
		for i := 0; i < len(s); i += 4 {
			a := uint32(s[i+3])
			if a == 255 {
				d[i], d[i+1], d[i+2], d[i+3] = s[i], s[i+1], s[i+2], 255
				continue
			}
			d[i] = uint8((uint32(s[i])*a + uint32(bg.R)*(255-a) + 127) / 255)
			d[i+1] = uint8((uint32(s[i+1])*a + uint32(bg.G)*(255-a) + 127) / 255)
			d[i+2] = uint8((uint32(s[i+2])*a + uint32(bg.B)*(255-a) + 127) / 255)
			d[i+3] = 255
		}
	}
	return out
}
