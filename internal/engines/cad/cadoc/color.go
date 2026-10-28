package cadoc

import "fmt"

// Color is ACadSharp's Color: an AutoCAD Color Index (0 = ByBlock, 1..255,
// 256 = ByLayer, 257 = ByEntity) or a 24-bit true colour. The zero value is
// ByLayer.
type Color struct{ v uint32 }

const trueColorFlag = 1 << 30

var (
	ColorByBlock  = ColorIndex(0)
	ColorByLayer  = ColorIndex(256)
	ColorByEntity = ColorIndex(257)
	ColorRed      = ColorIndex(1)
	ColorYellow   = ColorIndex(2)
	ColorGreen    = ColorIndex(3)
	ColorCyan     = ColorIndex(4)
	ColorBlue     = ColorIndex(5)
	ColorMagenta  = ColorIndex(6)
	ColorDefault  = ColorIndex(7)
	ColorDarkGray = ColorIndex(8)
	ColorLight    = ColorIndex(9)
	ColorBlack    = ColorIndex(250)
)

// ColorIndex returns an indexed colour; indexes outside 0..257 are clamped
// into that range by taking the absolute value modulo 258.
func ColorIndex(index int16) Color {
	i := int32(index)
	if i < 0 {
		i = -i
	}
	if i > 257 {
		i %= 258
	}
	return Color{uint32(i) ^ 256}
}

// ColorRGB returns a true colour.
func ColorRGB(r, g, b uint8) Color {
	return Color{trueColorFlag | uint32(r) | uint32(g)<<8 | uint32(b)<<16}
}

// ColorFromTrueColor builds a colour from the 0x00RRGGBB value DXF codes 420
// and 421 store.
func ColorFromTrueColor(rgb int32) Color {
	u := uint32(rgb)
	return ColorRGB(uint8(u>>16), uint8(u>>8), uint8(u))
}

// IsTrueColor reports whether the colour is an RGB value.
func (c Color) IsTrueColor() bool { return c.v&trueColorFlag != 0 }

// Index is the ACI index, -1 for a true colour.
func (c Color) Index() int16 {
	if c.IsTrueColor() {
		return -1
	}
	return int16(c.v ^ 256)
}

func (c Color) IsByLayer() bool { return !c.IsTrueColor() && c.Index() == 256 }
func (c Color) IsByBlock() bool { return !c.IsTrueColor() && c.Index() == 0 }

// TrueColor is the 0x00RRGGBB value of a true colour, -1 otherwise.
func (c Color) TrueColor() int32 {
	if !c.IsTrueColor() {
		return -1
	}
	r, g, b := c.RGB()
	return int32(r)<<16 | int32(g)<<8 | int32(b)
}

// RGB returns the colour's components; indexed colours use the AutoCAD palette
// (ByLayer, ByBlock and ByEntity give black).
func (c Color) RGB() (r, g, b uint8) {
	if c.IsTrueColor() {
		return uint8(c.v), uint8(c.v >> 8), uint8(c.v >> 16)
	}
	i := c.Index()
	if i < 0 || i > 255 {
		return 0, 0, 0
	}
	p := aciPalette[i]
	return p[0], p[1], p[2]
}

// GetRgb is RGB as an array, like ACadSharp's GetRgb.
func (c Color) GetRgb() [3]uint8 {
	r, g, b := c.RGB()
	return [3]uint8{r, g, b}
}

func (c Color) R() uint8 { r, _, _ := c.RGB(); return r }
func (c Color) G() uint8 { _, g, _ := c.RGB(); return g }
func (c Color) B() uint8 { _, _, b := c.RGB(); return b }

// ApproxIndex is the index of the palette colour nearest to a true colour, or
// the index itself for an indexed colour.
func (c Color) ApproxIndex() int16 {
	if !c.IsTrueColor() {
		return c.Index()
	}
	r, g, b := c.RGB()
	return NearestIndex(r, g, b)
}

// NearestIndex returns the palette index (1..255) closest to an RGB value.
func NearestIndex(r, g, b uint8) int16 {
	best, bestDist := int16(7), int32(1<<31-1)
	for i := 1; i < 256; i++ {
		p := aciPalette[i]
		dr, dg, db := int32(r)-int32(p[0]), int32(g)-int32(p[1]), int32(b)-int32(p[2])
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			best, bestDist = int16(i), d
			if d == 0 {
				break
			}
		}
	}
	return best
}

// IndexRGB returns the palette RGB of an ACI index.
func IndexRGB(index uint8) [3]uint8 { return aciPalette[index] }

func (c Color) String() string {
	switch {
	case c.IsTrueColor():
		r, g, b := c.RGB()
		return fmt.Sprintf("True Color RGB:%d,%d,%d", r, g, b)
	case c.IsByBlock():
		return "ByBlock"
	case c.IsByLayer():
		return "ByLayer"
	}
	return fmt.Sprintf("Indexed Color:%d", c.Index())
}

// Transparency is ACadSharp's Transparency: -1 = ByLayer, 100 = ByBlock, 0..90
// = percent transparent. The zero value is ByLayer.
type Transparency struct{ v int16 }

var (
	TransparencyByLayer = Transparency{0}
	TransparencyByBlock = Transparency{101}
	TransparencyOpaque  = Transparency{1}
)

// NewTransparency returns a transparency value (-1, 100 or 0..90, clamped).
func NewTransparency(value int16) Transparency {
	switch {
	case value == -1 || value == 100:
	case value < 0:
		value = 0
	case value > 90:
		value = 90
	}
	return Transparency{value + 1}
}

func (t Transparency) Value() int16    { return t.v - 1 }
func (t Transparency) IsByLayer() bool { return t.v == 0 }
func (t Transparency) IsByBlock() bool { return t.v == 101 }

// TransparencyFromAlpha decodes the DXF 440 value.
func TransparencyFromAlpha(value int32) Transparency {
	if uint32(value)&0xFF000000 == 0x01000000 {
		return TransparencyByBlock
	}
	alpha := int16(100 - float64(uint8(value))/255.0*100)
	return NewTransparency(alpha)
}

// AlphaValue encodes the transparency as DXF 440 stores it.
func (t Transparency) AlphaValue() int32 {
	if t.IsByBlock() {
		return 0x01000000
	}
	alpha := uint8(255 * float64(100-t.Value()) / 100.0)
	return 0x02000000 | int32(alpha)
}

// LineWeight is LineWeightType: hundredths of a millimetre or one of the
// special values.
type LineWeight int16

const (
	LineWeightByDIPs  LineWeight = -4
	LineWeightDefault LineWeight = -3
	LineWeightByBlock LineWeight = -2
	LineWeightByLayer LineWeight = -1
	LineWeightW0      LineWeight = 0
	LineWeightW5      LineWeight = 5
	LineWeightW9      LineWeight = 9
	LineWeightW13     LineWeight = 13
	LineWeightW15     LineWeight = 15
	LineWeightW18     LineWeight = 18
	LineWeightW20     LineWeight = 20
	LineWeightW25     LineWeight = 25
	LineWeightW30     LineWeight = 30
	LineWeightW35     LineWeight = 35
	LineWeightW40     LineWeight = 40
	LineWeightW50     LineWeight = 50
	LineWeightW53     LineWeight = 53
	LineWeightW60     LineWeight = 60
	LineWeightW70     LineWeight = 70
	LineWeightW80     LineWeight = 80
	LineWeightW90     LineWeight = 90
	LineWeightW100    LineWeight = 100
	LineWeightW106    LineWeight = 106
	LineWeightW120    LineWeight = 120
	LineWeightW140    LineWeight = 140
	LineWeightW158    LineWeight = 158
	LineWeightW200    LineWeight = 200
	LineWeightW211    LineWeight = 211
)

// IndexedLineWeights are the weights in DWG index order (CadUtils).
var IndexedLineWeights = []LineWeight{0, 5, 9, 13, 15, 18, 20, 25, 30, 35, 40, 50, 53, 60, 70, 80, 90, 100, 106, 120, 140, 158, 200, 211}

// LineWeightFromIndex decodes the DWG line weight index (CadUtils.ToValue).
func LineWeightFromIndex(b uint8) LineWeight {
	switch b {
	case 28, 29:
		return LineWeightByLayer
	case 30:
		return LineWeightByBlock
	case 31:
		return LineWeightDefault
	}
	if int(b) >= len(IndexedLineWeights) {
		return LineWeightDefault
	}
	return IndexedLineWeights[b]
}

// LineWeightIndex encodes a line weight as a DWG index (CadUtils.ToIndex).
func LineWeightIndex(w LineWeight) uint8 {
	switch w {
	case LineWeightDefault:
		return 31
	case LineWeightByBlock:
		return 30
	case LineWeightByLayer:
		return 29
	}
	for i, v := range IndexedLineWeights {
		if v == w {
			return uint8(i)
		}
	}
	return 31
}
