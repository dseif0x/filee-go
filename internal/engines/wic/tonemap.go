package wic

import (
	"math"
	"slices"
)

// HDR → SDR for linear scRGB pictures, the float JPEG XR files Windows saves
// for screenshots on an HDR display. In scRGB 1.0 is SDR white at 80 nits,
// but Windows draws SDR content at the display's "SDR content brightness"
// (1.0 to 6.0) and HDR highlights go well beyond that. So:
//
//  1. Reference white: the luminance 99 % of the pixels stay below, between
//     1.0 and 4.0 (320 nits). Pixels are divided by it.
//  2. Up to 80 % of that white nothing changes; above it an extended Reinhard
//     shoulder takes the range up to the brightest pixel into the last 20 %,
//     on the largest of R, G and B, so colours keep their hue.
//  3. Negative values (out of the sRGB gamut) are clipped to 0, then the
//     sRGB transfer curve and 8 bits.

const (
	whitePercentile = 0.99
	maxWhite        = 4
	knee            = 0.8
	maxSamples      = 1 << 16
)

type toneMap struct {
	white, rng float64
}

func newToneMap(white, peak float64) *toneMap {
	return &toneMap{white: white, rng: math.Max(1, (peak/white-knee)/(1-knee))}
}

// referenceWhite of a picture from the luminance of a sample of its pixels.
func referenceWhite(lum []float32) float64 {
	if len(lum) == 0 {
		return 1
	}
	s := slices.Clone(lum)
	slices.Sort(s)
	w := float64(s[int(math.Round(whitePercentile*float64(len(s)-1)))])
	if math.IsNaN(w) {
		return 1
	}
	return math.Max(1, math.Min(maxWhite, w))
}

// sampleStep is how many pixels to skip between two luminance samples.
func sampleStep(pixels int64) int64 { return max(1, (pixels+maxSamples-1)/maxSamples) }

func positive(v float32) float64 {
	if v > 0 {
		return math.Min(float64(v), 65504)
	}
	return 0
}

func luminance(r, g, b float32) float32 {
	return float32(0.2126*positive(r) + 0.7152*positive(g) + 0.0722*positive(b))
}

func maxChannel(r, g, b float32) float64 {
	return math.Max(positive(r), math.Max(positive(g), positive(b)))
}

// curve is the identity up to the knee, then the shoulder.
func (t *toneMap) curve(rel float64) float64 {
	if rel <= knee {
		return rel
	}
	x := (rel - knee) / (1 - knee)
	shoulder := x * (1 + x/(t.rng*t.rng)) / (1 + x)
	return math.Min(1, knee+(1-knee)*shoulder)
}

// mapPixel maps one linear RGBA pixel to 8-bit sRGB RGBA.
func (t *toneMap) mapPixel(r, g, b, a float32, out []byte) {
	rr, gg, bb := positive(r)/t.white, positive(g)/t.white, positive(b)/t.white
	m := math.Max(rr, math.Max(gg, bb))
	scale := 1.0
	if m > knee {
		scale = t.curve(m) / m
	}
	out[0], out[1], out[2] = encodeSRGB(rr*scale), encodeSRGB(gg*scale), encodeSRGB(bb*scale)
	av := float64(a)
	if math.IsNaN(av) {
		av = 1
	}
	out[3] = uint8(math.Round(math.Max(0, math.Min(1, av)) * 255))
}

var srgbTable = func() []byte {
	t := make([]byte, 65536)
	for i := range t {
		l := float64(i) / 65535
		e := 12.92 * l
		if l > 0.0031308 {
			e = 1.055*math.Pow(l, 1/2.4) - 0.055
		}
		t[i] = uint8(math.Round(e * 255))
	}
	return t
}()

func encodeSRGB(linear float64) byte {
	return srgbTable[int(math.Max(0, math.Min(1, linear))*65535+0.5)]
}
