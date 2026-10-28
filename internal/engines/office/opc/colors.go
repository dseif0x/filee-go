package opc

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// DrawingML is the DrawingML main namespace.
const DrawingML = "http://schemas.openxmlformats.org/drawingml/2006/main"

var slots = []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"}

// Office 2013+ default theme, used when a file has none.
var defaultTheme = map[string]string{
	"dk1": "#000000", "lt1": "#FFFFFF", "dk2": "#44546A", "lt2": "#E7E6E6",
	"accent1": "#4472C4", "accent2": "#ED7D31", "accent3": "#A5A5A5", "accent4": "#FFC000",
	"accent5": "#5B9BD5", "accent6": "#70AD47", "hlink": "#0563C1", "folHlink": "#954F72",
}

// Excel's legacy indexed colours 0-63.
var indexed = [64]string{
	"000000", "FFFFFF", "FF0000", "00FF00", "0000FF", "FFFF00", "FF00FF", "00FFFF",
	"000000", "FFFFFF", "FF0000", "00FF00", "0000FF", "FFFF00", "FF00FF", "00FFFF",
	"800000", "008000", "000080", "808000", "800080", "008080", "C0C0C0", "808080",
	"9999FF", "993366", "FFFFCC", "CCFFFF", "660066", "FF8080", "0066CC", "CCCCFF",
	"000080", "FF00FF", "FFFF00", "00FFFF", "800080", "800000", "008080", "0000FF",
	"00CCFF", "CCFFFF", "CCFFCC", "FFFF99", "99CCFF", "FF99CC", "CC99FF", "FFCC99",
	"3366FF", "33CCCC", "99CC00", "FFCC00", "FF9900", "FF6600", "666699", "969696",
	"003366", "339966", "003300", "333300", "993300", "993366", "333399", "333333",
}

// Colors resolves Office colours to "#RRGGBB": theme colours, Excel's tint
// and indexed palette, DrawingML colour elements with their modifiers.
type Colors struct {
	theme map[string]string
	cmap  map[string]string
}

// WithMap returns the colours with a PresentationML colour map (bg1="lt1" …).
func (c *Colors) WithMap(m map[string]string) *Colors { return &Colors{theme: c.theme, cmap: m} }

// FromTheme reads the colour scheme of a theme part (nil = Office defaults).
func FromTheme(theme *xdom.Node) *Colors {
	colors := map[string]string{}
	for k, v := range defaultTheme {
		colors[k] = v
	}
	var scheme *xdom.Node
	if theme != nil {
		if d := theme.Descendants(DrawingML, "clrScheme"); len(d) > 0 {
			scheme = d[0]
		} else if theme.Is(DrawingML, "clrScheme") {
			scheme = theme
		}
	}
	for _, slot := range slots {
		el := scheme.Child(DrawingML, slot)
		v := el.Child(DrawingML, "srgbClr").A("val")
		if v == "" {
			v = el.Child(DrawingML, "sysClr").A("lastClr")
		}
		if len(v) == 6 {
			colors[slot] = "#" + strings.ToUpper(v)
		}
	}
	return &Colors{theme: colors}
}

// Theme returns a slot or alias (tx1, bg1, …).
func (c *Colors) Theme(name string) string {
	slot := name
	if m, ok := c.cmap[name]; ok {
		slot = m
	} else {
		switch name {
		case "tx1":
			slot = "dk1"
		case "bg1":
			slot = "lt1"
		case "tx2":
			slot = "dk2"
		case "bg2":
			slot = "lt2"
		}
	}
	if v, ok := c.theme[slot]; ok {
		return v
	}
	return "#000000"
}

// Spreadsheet resolves a SpreadsheetML colour element; "" for automatic.
func (c *Colors) Spreadsheet(color *xdom.Node) string {
	if color == nil {
		return ""
	}
	if a := color.A("auto"); a == "1" || a == "true" {
		return ""
	}
	hex := ""
	if argb := color.A("rgb"); len(argb) >= 6 {
		hex = "#" + strings.ToUpper(argb[len(argb)-6:])
	} else if t, err := strconv.Atoi(color.A("theme")); err == nil && t >= 0 && t < 12 {
		// SpreadsheetML swaps the first two pairs: 0 = lt1, 1 = dk1, 2 = lt2, 3 = dk2.
		name := slots[t]
		switch t {
		case 0:
			name = "lt1"
		case 1:
			name = "dk1"
		case 2:
			name = "lt2"
		case 3:
			name = "dk2"
		}
		hex = c.Theme(name)
	} else if i, err := strconv.Atoi(color.A("indexed")); err == nil && i >= 0 && i < 64 {
		hex = "#" + indexed[i]
	}
	if hex == "" {
		return ""
	}
	if tint, err := strconv.ParseFloat(color.A("tint"), 64); err == nil && tint != 0 {
		return Tint(hex, tint)
	}
	return hex
}

// Drawing resolves the first DrawingML colour child of parent with its
// modifiers; placeholder replaces schemeClr "phClr".
func (c *Colors) Drawing(parent *xdom.Node, placeholder string) string {
	for _, el := range parent.Elements() {
		hex := ""
		switch el.Name.Local {
		case "srgbClr":
			if v := el.A("val"); len(v) == 6 {
				hex = "#" + strings.ToUpper(v)
			}
		case "schemeClr":
			if v := el.A("val"); v == "phClr" {
				hex = placeholder
			} else {
				hex = c.Theme(v)
			}
		case "sysClr":
			hex = "#000000"
			if v := el.A("lastClr"); len(v) == 6 {
				hex = "#" + strings.ToUpper(v)
			}
		case "prstClr":
			hex = presetColor(el.A("val"))
		case "scrgbClr", "hslClr":
		default:
			continue
		}
		if hex == "" {
			return ""
		}
		return modify(hex, el)
	}
	return ""
}

func modify(hex string, color *xdom.Node) string {
	value := func(name string) float64 {
		v, err := strconv.Atoi(color.Child(DrawingML, name).A("val"))
		if err != nil {
			return math.NaN()
		}
		return float64(v) / 100000
	}
	h, s, l := toHsl(hex)
	lumMod, lumOff := value("lumMod"), value("lumOff")
	if !math.IsNaN(lumMod) || !math.IsNaN(lumOff) {
		if math.IsNaN(lumMod) {
			lumMod = 1
		}
		if math.IsNaN(lumOff) {
			lumOff = 0
		}
		l = clamp01(l*lumMod + lumOff)
	}
	r, g, b := rgb(fromHsl(h, s, l))
	if tint := value("tint"); !math.IsNaN(tint) {
		r, g, b = r+(1-r)*(1-tint), g+(1-g)*(1-tint), b+(1-b)*(1-tint)
	}
	if shade := value("shade"); !math.IsNaN(shade) {
		r, g, b = r*shade, g*shade, b*shade
	}
	return hexOf(r, g, b)
}

// Tint is Excel's tint: lightens (> 0) or darkens (< 0) the luminance.
func Tint(hex string, tint float64) string {
	h, s, l := toHsl(hex)
	if tint < 0 {
		l = l * (1 + tint)
	} else {
		l = l*(1-tint) + tint
	}
	return fromHsl(h, s, clamp01(l))
}

func presetColor(name string) string {
	switch name {
	case "black":
		return "#000000"
	case "white":
		return "#FFFFFF"
	case "red":
		return "#FF0000"
	case "green":
		return "#008000"
	case "blue":
		return "#0000FF"
	case "yellow":
		return "#FFFF00"
	case "gray", "grey":
		return "#808080"
	case "lightGray":
		return "#D3D3D3"
	case "darkGray":
		return "#A9A9A9"
	case "orange":
		return "#FFA500"
	}
	return ""
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func rgb(hex string) (float64, float64, float64) {
	p := func(s string) float64 { v, _ := strconv.ParseUint(s, 16, 8); return float64(v) / 255 }
	if len(hex) < 7 {
		return 0, 0, 0
	}
	return p(hex[1:3]), p(hex[3:5]), p(hex[5:7])
}

func hexOf(r, g, b float64) string {
	c := func(v float64) int { return int(math.Round(clamp01(v) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X", c(r), c(g), c(b))
}

func toHsl(hex string) (float64, float64, float64) {
	r, g, b := rgb(hex)
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	s := d / (mx + mn)
	if l > 0.5 {
		s = d / (2 - mx - mn)
	}
	var h float64
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func fromHsl(h, s, l float64) string {
	if s == 0 {
		return hexOf(l, l, l)
	}
	channel := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		} else if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	q := l + s - l*s
	if l < 0.5 {
		q = l * (1 + s)
	}
	p := 2*l - q
	return hexOf(channel(p, q, h+1.0/3), channel(p, q, h), channel(p, q, h-1.0/3))
}
