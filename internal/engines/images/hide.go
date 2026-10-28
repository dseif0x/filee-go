package images

import (
	"bytes"
	"image"
	"math"

	"golang.org/x/image/draw"

	"github.com/dseif0x/filee-go/internal/presets"
)

// "Pixelate / blur": covers parts of a picture (ImageOptions.HiddenAreas) so
// what was there can't be read back. Each part is changed on its own; the rest
// keeps its pixels. The arithmetic is Filee.Core's HideTool.

const (
	minHideStrength = 1
	maxHideStrength = 10
	// minEffectPixels is the smallest square (or blur) in pixels.
	minEffectPixels = 8
)

func clampStrength(s int) int { return max(minHideStrength, min(maxHideStrength, s)) }

// EffectSize is the side of a square (pixelate) or the blur's sigma for a
// width × height picture: 2 % of its shorter side at the lowest strength to
// 6.5 % at the highest, never under 8 px.
func EffectSize(strength, width, height int) float64 {
	return math.Max(minEffectPixels, float64(min(max(1, width), max(1, height)))*(0.015+0.005*float64(clampStrength(strength))))
}

// clampArea is area within the picture, ok false when nothing of it is left.
func clampArea(a presets.HiddenArea) (presets.HiddenArea, bool) {
	for _, v := range []float64{a.X, a.Y, a.Width, a.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return a, false
		}
	}
	c := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	left, right := c(math.Min(a.X, a.X+a.Width)), c(math.Max(a.X, a.X+a.Width))
	top, bottom := c(math.Min(a.Y, a.Y+a.Height)), c(math.Max(a.Y, a.Y+a.Height))
	if right <= left || bottom <= top {
		return a, false
	}
	return presets.HiddenArea{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

// PixelBounds is area in whole pixels of a width × height picture, rounded
// outwards, at least one pixel; ok false when nothing of it is on the picture.
func PixelBounds(area presets.HiddenArea, width, height int) (image.Rectangle, bool) {
	inside, ok := clampArea(area)
	if width <= 0 || height <= 0 || !ok {
		return image.Rectangle{}, false
	}
	clampI := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	left := clampI(int(math.Floor(inside.X*float64(width)+1e-9)), 0, width-1)
	top := clampI(int(math.Floor(inside.Y*float64(height)+1e-9)), 0, height-1)
	right := clampI(int(math.Ceil((inside.X+inside.Width)*float64(width)-1e-9)), left+1, width)
	bottom := clampI(int(math.Ceil((inside.Y+inside.Height)*float64(height)-1e-9)), top+1, height)
	return image.Rect(left, top, right, bottom), true
}

// cellOf is the square a pixel column (or row) falls in, by the pixel's centre.
func cellOf(pixel int, cell float64) int { return int(math.Floor((float64(pixel) + 0.5) / cell)) }

// cellStart is the first pixel column (or row) of square index.
func cellStart(index int, cell float64) int { return int(math.Ceil(float64(index)*cell - 0.5)) }

// HideAreas covers the hidden areas of the frame (upright, as they were drawn)
// and drops the previews the file carries of the uncovered picture.
func HideAreas(f *Frame, o *presets.ImageOptions) {
	if !o.HidesAreas() {
		return
	}
	w, h := f.Width(), f.Height()
	size := EffectSize(o.HideStrength, w, h)
	for _, a := range o.HiddenAreas {
		r, ok := PixelBounds(a, w, h)
		if !ok {
			continue
		}
		if o.HideStyle == presets.HideBlur {
			Blur(f.Img, r, size)
		} else {
			Pixelate(f.Img, r, math.Round(size))
		}
	}
	dropPreviews(f)
}

// Pixelate makes every pixel of r the average colour of its square on a grid
// of cell-pixel squares laid from the picture's top-left corner. Squares on the
// edge of a part are averaged over all their pixels, so the grid stays the same
// wherever a part is moved.
func Pixelate(img *image.NRGBA, r image.Rectangle, cell float64) {
	cell = math.Max(1, cell)
	width, height := img.Rect.Dx(), img.Rect.Dy()
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	firstCol, lastCol := cellOf(r.Min.X, cell), cellOf(r.Max.X-1, cell)
	firstRow, lastRow := cellOf(r.Min.Y, cell), cellOf(r.Max.Y-1, cell)
	left, right := max(0, cellStart(firstCol, cell)), min(width, cellStart(lastCol+1, cell))
	top, bottom := max(0, cellStart(firstRow, cell)), min(height, cellStart(lastRow+1, cell))
	cols, rows := lastCol-firstCol+1, lastRow-firstRow+1
	colOf := make([]int, right-left)
	for i := range colOf {
		colOf[i] = max(0, min(cols-1, cellOf(left+i, cell)-firstCol))
	}
	rowOf := make([]int, bottom-top)
	for j := range rowOf {
		rowOf[j] = max(0, min(rows-1, cellOf(top+j, cell)-firstRow))
	}
	// premultiplied sums, so transparent pixels don't lend their colour
	sums := make([]uint64, cols*rows*4)
	counts := make([]uint64, cols*rows)
	for j := 0; j < bottom-top; j++ {
		row := img.Pix[img.PixOffset(left, top+j):]
		for i := 0; i < right-left; i++ {
			sq := rowOf[j]*cols + colOf[i]
			counts[sq]++
			p := row[i*4 : i*4+4]
			a := uint64(p[3])
			sums[sq*4] += uint64(p[0]) * a
			sums[sq*4+1] += uint64(p[1]) * a
			sums[sq*4+2] += uint64(p[2]) * a
			sums[sq*4+3] += a
		}
	}
	avg := make([]uint8, cols*rows*4)
	for sq, n := range counts {
		if n == 0 {
			continue
		}
		a := sums[sq*4+3]
		if a > 0 {
			for c := 0; c < 3; c++ {
				avg[sq*4+c] = uint8((sums[sq*4+c] + a/2) / a)
			}
		}
		avg[sq*4+3] = uint8((a + n/2) / n)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[img.PixOffset(0, y):]
		for x := r.Min.X; x < r.Max.X; x++ {
			sq := rowOf[y-top]*cols + colOf[x-left]
			copy(row[x*4:x*4+4], avg[sq*4:sq*4+4])
		}
	}
}

// Blur blurs r with a Gaussian of sigma pixels after its pixels are averaged
// into squares of about a third of it: averaging throws pixels away for good,
// and the wide blur leaves letters far below what a pixel can hold, so it
// cannot be deconvolved. Pixels beside the part are blurred with it (no seam),
// but only the part is written back.
func Blur(img *image.NRGBA, r image.Rectangle, sigma float64) {
	sigma = math.Max(1, sigma)
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	margin := int(math.Ceil(2 * sigma))
	region := image.Rect(r.Min.X-margin, r.Min.Y-margin, r.Max.X+margin, r.Max.Y+margin).Intersect(img.Rect)
	part := crop(img, region)
	rw, rh := region.Dx(), region.Dy()
	step := max(1, int(math.Floor(sigma/3)))
	if step > 1 {
		part = scaleBox(part, max(1, (rw+step-1)/step), max(1, (rh+step-1)/step))
	}
	part = gaussianBlur(part, sigma/float64(step))
	if step > 1 {
		src := premultiplied(part)
		dst := image.NewRGBA(image.Rect(0, 0, rw, rh))
		draw.BiLinear.Scale(dst, dst.Rect, src, src.Rect, draw.Src, nil)
		part = unpremultiplied(dst)
	}
	inner := crop(part, r.Sub(region.Min))
	paste(img, inner, r.Min.X, r.Min.Y)
}

// gaussianBlur blurs in premultiplied alpha, separably, edges clamped.
func gaussianBlur(img *image.NRGBA, sigma float64) *image.NRGBA {
	radius := int(math.Ceil(3 * sigma))
	if radius < 1 {
		return img
	}
	kernel := make([]float64, 2*radius+1)
	var total float64
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = math.Exp(-d * d / (2 * sigma * sigma))
		total += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= total
	}
	src := premultiplied(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	tmp := make([]float64, w*h*4)
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride:]
		for x := 0; x < w; x++ {
			var s [4]float64
			for k, kv := range kernel {
				xx := max(0, min(w-1, x+k-radius))
				for c := 0; c < 4; c++ {
					s[c] += kv * float64(row[xx*4+c])
				}
			}
			copy(tmp[(y*w+x)*4:], s[:])
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s [4]float64
			for k, kv := range kernel {
				yy := max(0, min(h-1, y+k-radius))
				for c := 0; c < 4; c++ {
					s[c] += kv * tmp[(yy*w+x)*4+c]
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			for c := 0; c < 4; c++ {
				o[c] = uint8(max(0, min(255, math.Round(s[c]))))
			}
			o[0], o[1], o[2] = min(o[0], o[3]), min(o[1], o[3]), min(o[2], o[3])
		}
	}
	return unpremultiplied(out)
}

// dropPreviews drops the small previews a file may carry of the picture as it
// was: Exif's thumbnail and an XMP packet holding thumbnails.
func dropPreviews(f *Frame) {
	if f.Meta.Exif != nil && exifHasThumbnail(f.Meta.Exif) {
		f.Meta.Exif = removeExifThumbnail(f.Meta.Exif)
	}
	if f.Meta.XMP != nil && bytes.Contains(f.Meta.XMP, []byte("Thumbnails")) {
		f.Meta.XMP = nil
	}
}

// Cover covers the whole picture with an effect size pixels large (a square's
// side or the blur's sigma): the hide window's preview, of which it shows the
// parts inside the frames.
func Cover(img *image.NRGBA, style presets.HideStyle, size float64) {
	if style == presets.HideBlur {
		Blur(img, img.Rect, size)
	} else {
		Pixelate(img, img.Rect, size)
	}
}
