package images

import (
	"context"
	"encoding/binary"
	"image"
	"math"
	"os"
	"runtime"
	"sync"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// What a FITS picture becomes: a picture for the eye or the data for further processing.
//
//   - PNG, JPG and every other picture: stretched automatically, the way
//     capture programs show a preview. Values are scaled to 0–1 over their
//     range; per channel the shadows are clipped 2.8 normalized MADs below the
//     median, and a midtones transfer function takes the median to 0.2. A raw
//     frame of a colour camera is debayered first (bilinear), unless the
//     preset keeps it mono; its channels are stretched one by one.
//   - TIFF: the data as it is, or stretched, at the file's own depth (8- and
//     16-bit stay so, 32-bit integer and floating point become 32-bit float).
//     Never debayered.
//
// Files that say ROWORDER = 'BOTTOM-UP' are turned upright.

const (
	fitsTargetBackground = 0.2
	fitsShadowClipping   = -2.8
	fitsBins             = 65536
)

// fitsToFrame is the picture for the eye: stretched, debayered if asked, 8 bits, upright.
func fitsToFrame(img *fitsImage, debayer bool) *Frame {
	planes := fitsStretch(img, debayer)
	if img.bottomUp {
		for i := range planes {
			planes[i] = flipPlane(planes[i], img.w, img.h)
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, img.w, img.h))
	n := img.w * img.h
	forChunks(n, func(start, end int) {
		for i := start; i < end; i++ {
			for c := 0; c < 3; c++ {
				p := planes[0]
				if len(planes) == 3 {
					p = planes[c]
				}
				out.Pix[i*4+c] = uint8(p[i]*255 + 0.5)
			}
			out.Pix[i*4+3] = 255
		}
	})
	return &Frame{Img: out, Gray: len(planes) == 1}
}

// writeFitsTiff writes FITS → TIFF at the file's own depth, turned and
// mirrored as the preset asks, compressed as it asks.
func writeFitsTiff(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	img, err := readFits(step.InputPath)
	if err != nil {
		return nil, err
	}
	progress.Report(0.3)
	o := &step.Preset.Image
	var planes [][]float32
	var kind tiffKind
	if o.FitsTiff == presets.FitsStretched {
		switch img.bitpix {
		case 8:
			kind = tiffU8
		case 16:
			kind = tiffU16
		default:
			kind = tiffF32
		}
		planes = fitsStretch(img, false)
		full := map[tiffKind]float32{tiffU8: 255, tiffU16: 65535, tiffF32: 1}[kind]
		if full != 1 {
			for _, p := range planes {
				for i := range p {
					p[i] *= full
				}
			}
		}
	} else {
		kind = fitsLinearKind(img)
		planes = img.planes
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.6)
	w, h := img.w, img.h
	if img.bottomUp {
		for i := range planes {
			planes[i] = flipPlane(planes[i], w, h)
		}
	}
	tw, th := w, h
	for i := range planes {
		planes[i], tw, th = turnPlane(planes[i], w, h, o.Rotation, o.Flip)
	}
	out := step.Output.Allocate("tiff", "")
	if out == "" {
		return nil, nil
	}
	dpi := 72.0
	if o.Dpi != nil && *o.Dpi > 0 {
		dpi = float64(*o.Dpi)
	}
	page := &tiffPage{w: tw, h: th, samples: len(planes), kind: kind, compression: string(o.TiffCompression), dpiX: dpi, dpiY: dpi}
	page.row = func(y int, dst []byte) {
		for x := 0; x < tw; x++ {
			for c, p := range planes {
				v := p[y*tw+x]
				i := x*len(planes) + c
				fin := float64(v)
				if math.IsNaN(fin) || math.IsInf(fin, 0) {
					fin = 0
				}
				switch kind {
				case tiffU8:
					dst[i] = uint8(max(0, min(255, math.Round(fin))))
				case tiffU16:
					binary.LittleEndian.PutUint16(dst[i*2:], uint16(max(0, min(65535, math.Round(fin)))))
				default:
					binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(v))
				}
			}
		}
	}
	data, err := writeTIFF([]*tiffPage{page})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

// fitsLinearKind is the TIFF sample type that holds the data as it is.
func fitsLinearKind(img *fitsImage) tiffKind {
	switch {
	case img.bitpix == 8 && img.integral && img.min >= 0 && img.max <= 255:
		return tiffU8
	case img.bitpix == 16 && img.integral && img.min >= 0 && img.max <= 65535:
		return tiffU16
	}
	return tiffF32
}

// fitsStretch scales the planes to 0–1 over the picture's range and stretches
// them channel by channel; a colour camera's mosaic is debayered first when asked.
func fitsStretch(img *fitsImage, debayer bool) [][]float32 {
	lo, hi := float64(img.min), float64(img.max)
	scale := 0.0
	if hi > lo {
		scale = 1 / (hi - lo)
	}
	planes := make([][]float32, len(img.planes))
	for p, src := range img.planes {
		plane := make([]float32, len(src))
		forChunks(len(plane), func(start, end int) {
			for i := start; i < end; i++ {
				v := float64(src[i])
				if math.IsNaN(v) || math.IsInf(v, 0) {
					plane[i] = 0
				} else {
					plane[i] = float32(math.Max(0, math.Min(1, (v-lo)*scale)))
				}
			}
		})
		planes[p] = plane
	}
	if debayer && len(planes) == 1 && img.pattern != "" && img.w >= 2 && img.h >= 2 {
		planes = fitsDebayer(planes[0], img.w, img.h, img.pattern, img.offX, img.offY)
	}
	var wg sync.WaitGroup
	for _, plane := range planes {
		wg.Add(1)
		go func(plane []float32) {
			defer wg.Done()
			shadows, midtones := fitsStretchParameters(plane)
			rng := 1 - shadows
			forChunks(len(plane), func(start, end int) {
				for i := start; i < end; i++ {
					plane[i] = float32(fitsMidtones(midtones, math.Max(0, math.Min(1, (float64(plane[i])-shadows)/rng))))
				}
			})
		}(plane)
	}
	wg.Wait()
	return planes
}

func forChunks(n int, body func(start, end int)) {
	const chunk = 1 << 16
	workers := runtime.GOMAXPROCS(0)
	if n <= chunk || workers <= 1 {
		body(0, n)
		return
	}
	var wg sync.WaitGroup
	next := make(chan int, (n+chunk-1)/chunk)
	for c := 0; c < n; c += chunk {
		next <- c
	}
	close(next)
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range next {
				body(c, min(n, c+chunk))
			}
		}()
	}
	wg.Wait()
}

// fitsStretchParameters are the shadows clipping point and the midtones balance of one channel.
func fitsStretchParameters(values []float32) (float64, float64) {
	hist := make([]int, fitsBins)
	bin := func(v float64) int { return int(math.Max(0, math.Min(1, v))*(fitsBins-1) + 0.5) }
	for _, v := range values {
		hist[bin(float64(v))]++
	}
	median := histMedian(hist, len(values))
	clear(hist)
	for _, v := range values {
		hist[bin(math.Abs(float64(v)-median))]++
	}
	dev := 1.4826 * histMedian(hist, len(values))
	shadows := 0.0
	if dev > 0 {
		shadows = math.Max(0, math.Min(0.99, median+fitsShadowClipping*dev))
	}
	background := (median - shadows) / (1 - shadows)
	mid := 0.5
	if background > 0 {
		mid = fitsMidtones(fitsTargetBackground, background)
	}
	if !(mid > 0 && mid < 1) {
		mid = 0.5
	}
	return shadows, mid
}

// fitsMidtones is the midtones transfer function: 0 → 0, 1 → 1, balance → 0.5.
func fitsMidtones(balance, x float64) float64 {
	switch {
	case x <= 0:
		return 0
	case x >= 1:
		return 1
	}
	return (balance - 1) * x / ((2*balance-1)*x - balance)
}

func histMedian(hist []int, count int) float64 {
	half := (count + 1) / 2
	seen := 0
	for b, n := range hist {
		seen += n
		if seen >= half {
			return float64(b) / (fitsBins - 1)
		}
	}
	return 0
}

// fitsDebayer is bilinear debayering: each pixel keeps its own colour and
// takes the others from the average of its neighbours of that colour.
func fitsDebayer(mosaic []float32, w, h int, pattern string, offX, offY int) [][]float32 {
	colours := make([]int, 4)
	for i, c := range pattern {
		switch c {
		case 'R':
			colours[i] = 0
		case 'G':
			colours[i] = 1
		default:
			colours[i] = 2
		}
	}
	planes := [][]float32{make([]float32, len(mosaic)), make([]float32, len(mosaic)), make([]float32, len(mosaic))}
	forChunks(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var sum [3]float64
				var count [3]int
				for dy := -1; dy <= 1; dy++ {
					ny := y + dy
					if ny < 0 || ny >= h {
						continue
					}
					for dx := -1; dx <= 1; dx++ {
						nx := x + dx
						if (dx == 0 && dy == 0) || nx < 0 || nx >= w {
							continue
						}
						c := colours[((ny+offY)&1)*2+((nx+offX)&1)]
						sum[c] += float64(mosaic[ny*w+nx])
						count[c]++
					}
				}
				i := y*w + x
				own := colours[((y+offY)&1)*2+((x+offX)&1)]
				for c := 0; c < 3; c++ {
					switch {
					case c == own:
						planes[c][i] = mosaic[i]
					case count[c] > 0:
						planes[c][i] = float32(sum[c] / float64(count[c]))
					}
				}
			}
		}
	})
	return planes
}

func flipPlane(p []float32, w, h int) []float32 {
	out := make([]float32, len(p))
	for y := 0; y < h; y++ {
		copy(out[(h-1-y)*w:(h-y)*w], p[y*w:(y+1)*w])
	}
	return out
}

// turnPlane turns, then mirrors, one plane.
func turnPlane(p []float32, w, h int, rot presets.ImageRotation, flip presets.ImageFlip) ([]float32, int, int) {
	if (rot == presets.RotateNone || rot == "") && (flip == presets.FlipNone || flip == "") {
		return p, w, h
	}
	tw, th := w, h
	if rot == presets.Right90 || rot == presets.Left90 {
		tw, th = h, w
	}
	out := make([]float32, len(p))
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			fx, fy := x, y
			if flip == presets.FlipHorizontal {
				fx = tw - 1 - x
			}
			if flip == presets.FlipVertical {
				fy = th - 1 - y
			}
			sx, sy := fx, fy
			switch rot {
			case presets.Right90:
				sx, sy = fy, h-1-fx
			case presets.Left90:
				sx, sy = w-1-fy, fx
			case presets.Rotate180:
				sx, sy = w-1-fx, h-1-fy
			}
			out[y*tw+x] = p[sy*w+sx]
		}
	}
	return out, tw, th
}
