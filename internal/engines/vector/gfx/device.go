package gfx

import (
	"image"
	"math"
)

// Color is a straight-alpha sRGB colour with components in 0–1.
type Color struct{ R, G, B, A float64 }

var (
	Black       = Color{0, 0, 0, 1}
	White       = Color{1, 1, 1, 1}
	Transparent = Color{}
)

// RGB8 builds an opaque colour from bytes.
func RGB8(r, g, b uint8) Color { return Color{float64(r) / 255, float64(g) / 255, float64(b) / 255, 1} }

// RGBA8 returns the colour as bytes.
func (c Color) RGBA8() (r, g, b, a uint8) {
	return to8(c.R), to8(c.G), to8(c.B), to8(c.A)
}

func to8(v float64) uint8 {
	v = math.Round(v * 255)
	if v <= 0 || math.IsNaN(v) {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// WithAlpha multiplies the alpha.
func (c Color) WithAlpha(k float64) Color { c.A *= k; return c }

// FillRule decides what is inside a path.
type FillRule uint8

const (
	NonZero FillRule = iota
	EvenOdd
)

// Spread is what a gradient does outside 0–1.
type Spread uint8

const (
	SpreadPad Spread = iota
	SpreadReflect
	SpreadRepeat
)

// Stop is one gradient colour.
type Stop struct {
	Offset float64
	Color  Color
}

// Gradient is a linear (X1,Y1 → X2,Y2) or radial (centre CX,CY radius R,
// focal FX,FY radius FR) gradient in gradient space; Matrix maps gradient
// space to the user space of the shape it paints.
type Gradient struct {
	Radial         bool
	X1, Y1, X2, Y2 float64
	CX, CY, R      float64
	FX, FY, FR     float64
	Stops          []Stop
	Spread         Spread
	Matrix         Matrix
}

// Paint is a solid colour or a gradient, with an opacity on top.
type Paint struct {
	Color    Color
	Gradient *Gradient
}

// Solid returns a colour paint.
func Solid(c Color) Paint { return Paint{Color: c} }

// Visible reports whether the paint can show anything.
func (p Paint) Visible() bool {
	if p.Gradient != nil {
		for _, s := range p.Gradient.Stops {
			if s.Color.A > 0 {
				return true
			}
		}
		return false
	}
	return p.Color.A > 0
}

// ClipShape is one shape of a clip union.
type ClipShape struct {
	Path *Path
	M    Matrix
	Rule FillRule
}

// MaskKind is how a mask's drawing becomes coverage.
type MaskKind uint8

const (
	MaskLuminance MaskKind = iota
	MaskAlpha
)

// Device is a drawing surface. Coordinates are in the device's own units
// (pixels for rasters, points for PDF); m maps path coordinates to them.
type Device interface {
	Fill(p *Path, m Matrix, rule FillRule, paint Paint)
	Stroke(p *Path, m Matrix, s *StrokeStyle, paint Paint)
	// DrawImage draws img with its pixel rectangle (0,0)–(w,h) mapped by m.
	DrawImage(img image.Image, m Matrix, opacity float64)
	PushClip(p *Path, m Matrix, rule FillRule)
	// PushClipShapes clips to the union of shapes, each with its own rule;
	// PopClip ends it.
	PushClipShapes(shapes []ClipShape)
	PopClip()
	// PushGroup starts an isolated group composited with opacity on PopGroup.
	PushGroup(opacity float64)
	PopGroup()
	// PushMask starts content that BeginMaskContent's drawing masks; PopMask
	// composites the masked content with opacity.
	PushMask(kind MaskKind, opacity float64)
	BeginMaskContent()
	PopMask()
}
