package cad

import (
	"math"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/presets"
)

// The display list the CAD renderer draws. The scene builder resolves layers,
// colours, line weights, line types and block transforms once; the result is
// plain geometry in WCS, so one drawing routine serves PDF, SVG and PNG and
// the page is fitted to what is drawn (not to the header's EXTMIN/EXTMAX).

// RGB is an opaque colour.
type RGB struct{ R, G, B uint8 }

var black = RGB{}

// Primitive is something to draw.
type Primitive interface{ color() RGB }

// StrokePrimitive is stroked figures: lines, arcs, polylines, curves.
type StrokePrimitive struct {
	Color   RGB
	Figures []*Figure
	// WidthMm is the line weight on paper in millimetres.
	WidthMm float64
	// WorldWidth is the width in drawing units of polylines with a constant
	// width (drawn with butt caps); 0 for normal lines.
	WorldWidth float64
	// Dashes in drawing units (>0 dash, <0 gap, 0 dot); nil for continuous lines.
	Dashes []float64
}

// FillPrimitive is filled areas (even-odd): solid hatches, SOLID/TRACE, wide
// polyline segments.
type FillPrimitive struct {
	Color   RGB
	Figures []*Figure
	// GradientColor is the second colour of a gradient hatch along GradientAngle.
	GradientColor *RGB
	GradientAngle float64
	// IsArea: a filled hatch, which black-and-white and grayscale output shade
	// instead of filling it black.
	IsArea bool
}

// HatchLinesPrimitive is pattern hatch lines, clipped by the boundary.
type HatchLinesPrimitive struct {
	Color    RGB
	Boundary []*Figure
	Lines    [][2]Vec2
	WidthMm  float64
}

// TextPrimitive is one run of text in one typeface. Glyph coordinates at font
// size 1 (y down) map to WCS as Origin + gx·XAxis + gy·YAxis, which covers
// rotation, width factor, oblique angle and mirrored blocks.
type TextPrimitive struct {
	Color                RGB
	Text                 string
	Face                 Typeface
	Origin, XAxis, YAxis Vec2
}

// InfiniteLinePrimitive is XLINE and RAY, clipped to the page when drawn.
type InfiniteLinePrimitive struct {
	Color             RGB
	Origin, Direction Vec2
	IsRay             bool
	WidthMm           float64
	Dashes            []float64
}

// PointPrimitive is a POINT, drawn as a dot of its line weight.
type PointPrimitive struct {
	Color    RGB
	Location Vec2
	WidthMm  float64
}

func (p *StrokePrimitive) color() RGB       { return p.Color }
func (p *FillPrimitive) color() RGB         { return p.Color }
func (p *HatchLinesPrimitive) color() RGB   { return p.Color }
func (p *TextPrimitive) color() RGB         { return p.Color }
func (p *InfiniteLinePrimitive) color() RGB { return p.Color }
func (p *PointPrimitive) color() RGB        { return p.Color }

// Scene is everything to draw plus what was left out.
type Scene struct {
	Items   []Primitive
	Extents Box2
	// Skipped counts entity types not drawn (for the log).
	Skipped map[string]int
	// Truncated: a limit (primitives, nesting, array size) cut the drawing short.
	Truncated bool
	// Space is "model" or the layout drawn.
	Space string
}

func NewScene() *Scene { return &Scene{Extents: EmptyBox(), Skipped: map[string]int{}, Space: "model"} }

// LayoutSheet is a paper-space layout printed as one page.
type LayoutSheet struct {
	Name      string
	Paper     *Scene
	Viewports []ViewportView
}

// Extents is what the page has to show: the layout's drawing and every viewport frame.
func (s *LayoutSheet) Extents() Box2 {
	b := s.Paper.Extents
	for _, v := range s.Viewports {
		b.Add(Vec2{v.Frame.MinX, v.Frame.MinY})
		b.Add(Vec2{v.Frame.MaxX, v.Frame.MaxY})
	}
	return b
}

// ViewportView is a viewport on a layout: its frame in paper units, the model
// point at its centre and paper units per model unit.
type ViewportView struct {
	Frame      Box2
	ViewCenter Vec2
	Scale      float64
}

// Palette maps the drawing's colours to the colours printed (#411), like a
// plot style table: ink (lines, text, small fills) and areas (filled hatches).
type Palette struct{ Colors presets.CadColors }

// lightestInk is the lightest gray ink is printed in with grayscale.
const lightestInk = 160

func brightness(c RGB) float64 { return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B) }

func gray(level float64) RGB {
	v := uint8(math.Max(0, math.Min(255, math.Round(level))))
	return RGB{v, v, v}
}

// Ink: black and white prints all ink black (monochrome.ctb); grayscale the
// gray of its brightness, scaled so the lightest colour is still a mid gray.
func (p Palette) Ink(c RGB) RGB {
	switch p.Colors {
	case presets.CadBlackAndWhite:
		return black
	case presets.CadGrayscale:
		return gray(brightness(c) * lightestInk / 255)
	}
	return c
}

// Area: both choices print an area as the gray of its brightness, so a light
// background fill stays light instead of hiding what is drawn over it.
func (p Palette) Area(c RGB) RGB {
	if p.Colors == presets.CadBlackAndWhite || p.Colors == presets.CadGrayscale {
		return gray(brightness(c))
	}
	return c
}

func (c RGB) gfx() gfx.Color { return gfx.RGB8(c.R, c.G, c.B) }
