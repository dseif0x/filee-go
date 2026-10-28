package gfx

import "math"

// BlendMode is a CSS / PDF blend mode.
type BlendMode uint8

const (
	BlendNormal BlendMode = iota
	BlendMultiply
	BlendScreen
	BlendOverlay
	BlendDarken
	BlendLighten
	BlendColorDodge
	BlendColorBurn
	BlendHardLight
	BlendSoftLight
	BlendDifference
	BlendExclusion
	BlendHue
	BlendSaturation
	BlendColor
	BlendLuminosity
)

var blendNames = []string{"normal", "multiply", "screen", "overlay", "darken", "lighten", "color-dodge", "color-burn",
	"hard-light", "soft-light", "difference", "exclusion", "hue", "saturation", "color", "luminosity"}

var pdfBlendNames = []string{"Normal", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn",
	"HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity"}

// ParseBlendMode reads a CSS name ("multiply"); unknown names are normal.
func ParseBlendMode(s string) BlendMode {
	for i, n := range blendNames {
		if n == s {
			return BlendMode(i)
		}
	}
	return BlendNormal
}

func (b BlendMode) String() string { return blendNames[b] }

// BlendDevice is a Device that can composite a group with a blend mode.
type BlendDevice interface {
	PushBlendGroup(opacity float64, mode BlendMode)
}

func blendChannel(mode BlendMode, cb, cs float64) float64 {
	switch mode {
	case BlendMultiply:
		return cb * cs
	case BlendScreen:
		return cb + cs - cb*cs
	case BlendOverlay:
		return blendChannel(BlendHardLight, cs, cb)
	case BlendDarken:
		return math.Min(cb, cs)
	case BlendLighten:
		return math.Max(cb, cs)
	case BlendColorDodge:
		if cb == 0 {
			return 0
		}
		if cs >= 1 {
			return 1
		}
		return math.Min(1, cb/(1-cs))
	case BlendColorBurn:
		if cb >= 1 {
			return 1
		}
		if cs <= 0 {
			return 0
		}
		return 1 - math.Min(1, (1-cb)/cs)
	case BlendHardLight:
		if cs <= 0.5 {
			return cb * 2 * cs
		}
		return blendChannel(BlendScreen, cb, 2*cs-1)
	case BlendSoftLight:
		if cs <= 0.5 {
			return cb - (1-2*cs)*cb*(1-cb)
		}
		var d float64
		if cb <= 0.25 {
			d = ((16*cb-12)*cb + 4) * cb
		} else {
			d = math.Sqrt(cb)
		}
		return cb + (2*cs-1)*(d-cb)
	case BlendDifference:
		return math.Abs(cb - cs)
	case BlendExclusion:
		return cb + cs - 2*cb*cs
	}
	return cs
}

func lum(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

func clipColor(c [3]float64) [3]float64 {
	l := lum(c)
	n := math.Min(c[0], math.Min(c[1], c[2]))
	x := math.Max(c[0], math.Max(c[1], c[2]))
	for i := range c {
		if n < 0 {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
		if x > 1 {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

func setLum(c [3]float64, l float64) [3]float64 {
	d := l - lum(c)
	return clipColor([3]float64{c[0] + d, c[1] + d, c[2] + d})
}

func sat(c [3]float64) float64 {
	return math.Max(c[0], math.Max(c[1], c[2])) - math.Min(c[0], math.Min(c[1], c[2]))
}

func setSat(c [3]float64, s float64) [3]float64 {
	imax, imin := 0, 0
	for i := 1; i < 3; i++ {
		if c[i] > c[imax] {
			imax = i
		}
		if c[i] < c[imin] {
			imin = i
		}
	}
	if imax == imin {
		return [3]float64{}
	}
	imid := 3 - imax - imin
	var out [3]float64
	if c[imax] > c[imin] {
		out[imid] = (c[imid] - c[imin]) * s / (c[imax] - c[imin])
		out[imax] = s
	}
	out[imin] = 0
	return out
}

// BlendColors blends straight colours cs (source) onto cb (backdrop).
func BlendColors(mode BlendMode, cb, cs [3]float64) [3]float64 {
	switch mode {
	case BlendHue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case BlendSaturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case BlendColor:
		return setLum(cs, lum(cb))
	case BlendLuminosity:
		return setLum(cb, lum(cs))
	}
	return [3]float64{blendChannel(mode, cb[0], cs[0]), blendChannel(mode, cb[1], cs[1]), blendChannel(mode, cb[2], cs[2])}
}

// BlendPixel composites a premultiplied source pixel (0–1, already scaled by
// opacity) onto a premultiplied backdrop with a blend mode.
func BlendPixel(mode BlendMode, dst *[4]float64, src [4]float64) {
	as, ab := src[3], dst[3]
	if as <= 0 {
		return
	}
	if mode == BlendNormal || ab <= 0 {
		for i := 0; i < 4; i++ {
			dst[i] = src[i] + dst[i]*(1-as)
		}
		return
	}
	var cs, cb [3]float64
	for i := 0; i < 3; i++ {
		cs[i] = src[i] / as
		cb[i] = dst[i] / ab
	}
	b := BlendColors(mode, cb, cs)
	for i := 0; i < 3; i++ {
		// Cs' = (1 - αb)·Cs + αb·B(Cb, Cs), then source-over.
		mixed := (1-ab)*cs[i] + ab*clamp01(b[i])
		dst[i] = as*mixed + dst[i]*(1-as)
	}
	dst[3] = as + ab*(1-as)
}
