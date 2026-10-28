package images

import (
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/dseif0x/filee-go/internal/presets"
)

// AutoOrient turns the frame upright as its Exif orientation says and marks it upright.
func (f *Frame) AutoOrient() {
	if f.Meta.Orientation > 1 {
		f.Img = orient(f.Img, f.Meta.Orientation)
	}
	if f.Meta.Orientation != 0 {
		f.Meta.Orientation = 1
		if f.Meta.Exif != nil {
			f.Meta.Exif = setExifOrientation(f.Meta.Exif, 1)
		}
	}
}

// Turn turns and mirrors the frame as o says (part of ApplyOptions).
func Turn(f *Frame, o *presets.ImageOptions) {
	switch o.Rotation {
	case presets.Right90:
		f.Img = rotate90(f.Img)
	case presets.Left90:
		f.Img = rotate270(f.Img)
	case presets.Rotate180:
		f.Img = rotate180(f.Img)
	}
	switch o.Flip {
	case presets.FlipHorizontal:
		f.Img = flipH(f.Img)
	case presets.FlipVertical:
		f.Img = flipV(f.Img)
	}
}

// ResizeScale is how much ApplyOptions resizes a width × height picture before
// it is fitted to its format (1 when it does not). Custom sizing with the
// aspect kept takes one scale for both sides; stretched, the height's factor.
func ResizeScale(width, height float64, o *presets.ImageOptions) float64 {
	if c := o.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		if c.LockAspect {
			return math.Min(float64(c.ClampedWidth())/width, float64(c.ClampedHeight())/height)
		}
		return float64(c.ClampedHeight()) / height
	}
	if m := o.ClampedMinShortEdge(); m > 0 {
		return MinShortEdgeScale(width, height, m)
	}
	switch o.Resize {
	case presets.ResizePercent:
		if o.ResizePercent > 0 {
			return float64(o.ResizePercent) / 100
		}
	case presets.ResizeLongEdge:
		if o.LongEdge > 0 {
			return math.Min(1, float64(o.LongEdge)/math.Max(width, height))
		}
	case presets.ResizeWidth:
		if o.Width > 0 {
			return math.Min(1, float64(o.Width)/width)
		}
	}
	return 1
}

// MaxEnlargedPixels is the most pixels a minimum short edge enlarges a picture
// to: as many as the largest custom size holds.
const MaxEnlargedPixels = float64(presets.MaxSide) * presets.MaxSide

// MinShortEdgeScale brings the shorter edge up to minShortEdge: never below 1,
// and at most what keeps the result within MaxEnlargedPixels.
func MinShortEdgeScale(width, height float64, minShortEdge int) float64 {
	scale := float64(minShortEdge) / math.Min(width, height)
	if scale <= 1 {
		return 1
	}
	return math.Max(1, math.Min(scale, math.Sqrt(MaxEnlargedPixels/(width*height))))
}

func roundSide(v float64) int { return max(1, int(math.Round(v))) }

// fitBox is ImageMagick's geometry "WxH": the largest size of the picture's
// shape inside the box (up or down).
func fitBox(w, h, bw, bh int) (int, int) {
	scale := math.Min(float64(bw)/float64(w), float64(bh)/float64(h))
	return roundSide(float64(w) * scale), roundSide(float64(h) * scale)
}

// TrimTransparentBorders crops the frame to the box around its pixels that
// aren't fully transparent. Only transparency counts: a picture whose content
// touches a corner keeps its solid border.
func TrimTransparentBorders(f *Frame) {
	if !f.Alpha {
		return
	}
	img := f.Img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	left, top, right, bottom := w, h, -1, -1
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			if row[x*4+3] == 0 {
				continue
			}
			left, right = min(left, x), max(right, x)
			top, bottom = min(top, y), max(bottom, y)
		}
	}
	if right < 0 || (left == 0 && top == 0 && right == w-1 && bottom == h-1) {
		return
	}
	f.Img = crop(img, image.Rect(left, top, right+1, bottom+1))
}

// ApplyOptions applies the preset's picture options to one frame: upright,
// hidden areas, turning, resizing, fitting the format, background removal,
// grey, resolution, metadata, flattening for formats without alpha, and the
// encoder settings (quality, compression, icon sizes).
func ApplyOptions(f *Frame, o *presets.ImageOptions, target string) {
	turned := o.TurnsOrMirrors() || f.Meta.Orientation > 1
	f.AutoOrient()

	// The parts were drawn on the picture as it is seen, before it is turned or resized.
	HideAreas(f, o)

	Turn(f, o)
	// The small preview kept in the Exif data still shows the picture as it was.
	if turned && f.Meta.Exif != nil && exifHasThumbnail(f.Meta.Exif) {
		f.Meta.Exif = removeExifThumbnail(f.Meta.Exif)
	}

	w, h := f.Width(), f.Height()
	switch c := o.CustomSize; {
	case c != nil && c.Width > 0 && c.Height > 0:
		cw, ch := c.ClampedWidth(), c.ClampedHeight()
		if c.LockAspect {
			scale := math.Min(float64(cw)/float64(w), float64(ch)/float64(h))
			if scale != 1 {
				f.Img = Resize(f.Img, roundSide(float64(w)*scale), roundSide(float64(h)*scale))
			}
		} else if w != cw || h != ch {
			// exactly the asked size, stretching on purpose
			f.Img = Resize(f.Img, cw, ch)
		}
	case o.ClampedMinShortEdge() > 0:
		if scale := MinShortEdgeScale(float64(w), float64(h), o.ClampedMinShortEdge()); scale > 1 {
			f.Img = Resize(f.Img, roundSide(float64(w)*scale), roundSide(float64(h)*scale))
		}
	default:
		switch o.Resize {
		case presets.ResizePercent:
			if o.ResizePercent > 0 && o.ResizePercent != 100 {
				p := float64(o.ResizePercent) / 100
				f.Img = Resize(f.Img, roundSide(float64(w)*p), roundSide(float64(h)*p))
			}
		case presets.ResizeLongEdge:
			// only shrink pictures larger than the box, never enlarge
			if l := o.LongEdge; l > 0 && (w > l || h > l) {
				nw, nh := fitBox(w, h, l, l)
				f.Img = Resize(f.Img, nw, nh)
			}
		case presets.ResizeWidth:
			if o.Width > 0 && w > o.Width {
				f.Img = Resize(f.Img, o.Width, roundSide(float64(h)*float64(o.Width)/float64(w)))
			}
		}
	}
	fitFormat(f, target)

	// Before grey (a blue signature stays blue until then) and before alpha is flattened.
	if o.RemoveBackground {
		RemoveBackground(f, o.BackgroundTolerance, o.KeepEnclosedBackground)
	}
	if o.Grayscale {
		toGray(f.Img)
		f.Gray = true
	}
	if o.Dpi != nil && *o.Dpi > 0 {
		f.Meta.DpiX, f.Meta.DpiY = float64(*o.Dpi), float64(*o.Dpi)
	}
	if !o.KeepMetadata {
		icc := f.Meta.ICC
		f.Meta = Meta{DpiX: f.Meta.DpiX, DpiY: f.Meta.DpiY, Quality: f.Meta.Quality}
		if o.KeepColorProfile {
			f.Meta.ICC = icc
		}
	}
	if !HoldsAlpha(target) && f.Alpha {
		f.Img = flatten(f.Img, backgroundColor(o.Background))
		f.Alpha = false
	}

	quality := max(1, min(100, o.Quality))
	// Turning a JPEG re-encodes it at the photo's own quality, not above it;
	// "Compress image" never raises it either, and "keep source quality" keeps it.
	source := 0
	if target == "jpg" && f.Meta.Quality > 0 && f.Meta.Quality <= 100 {
		source = f.Meta.Quality
	}
	if (o.TurnsOrMirrors() || o.MaxCompression) && source > 0 {
		quality = min(quality, source)
	}
	if o.KeepSourceQuality && source > 0 {
		quality = source
	}
	f.enc.quality = quality
	f.enc.webpLossless = target == "webp" && o.WebpLossless
	f.enc.pngMax = target == "png" && o.MaxCompression
	f.enc.tiff = string(o.TiffCompression)
	switch target {
	case "ico":
		prepareIcon(f, o)
	case "cur":
		// A cursor stores at most 256 × 256 pixels.
		if f.Width() > 256 || f.Height() > 256 {
			nw, nh := fitBox(f.Width(), f.Height(), 256, 256)
			f.Img = Resize(f.Img, nw, nh)
		}
	}
}

// fitFormat scales a picture down until both sides fit MaxSide of target.
func fitFormat(f *Frame, target string) {
	scale := math.Min(1, MaxSide(target)/float64(max(f.Width(), f.Height())))
	if scale < 1 {
		f.Img = Resize(f.Img, max(1, int(math.Floor(float64(f.Width())*scale))), max(1, int(math.Floor(float64(f.Height())*scale))))
	}
}

// prepareIcon makes the picture square (padded with transparency), at most 256
// px, and chooses the sizes the icon holds.
func prepareIcon(f *Frame, o *presets.ImageOptions) {
	side := max(f.Width(), f.Height())
	if f.Width() != f.Height() {
		f.Img = extent(f.Img, side, side, (side-f.Width())/2, (side-f.Height())/2, color.NRGBA{})
		f.Alpha = true
	}
	if side > 256 {
		f.Img = Resize(f.Img, 256, 256)
		side = 256
	}
	var sizes []int
	for _, s := range o.IcoSizes {
		if s > 0 && s <= 256 && !slices.Contains(sizes, s) {
			sizes = append(sizes, s)
		}
	}
	slices.SortFunc(sizes, func(a, b int) int { return b - a })
	if len(sizes) == 0 {
		sizes = []int{256, 48, 32, 16}
	}
	// Resized on purpose to less than an icon holds: that size and the smaller ones asked for.
	if o.Resize != presets.ResizeNone && o.Resize != "" && side < 256 {
		smaller := []int{side}
		for _, s := range sizes {
			if s < side {
				smaller = append(smaller, s)
			}
		}
		sizes = smaller
	}
	f.enc.icoSizes = sizes
}
