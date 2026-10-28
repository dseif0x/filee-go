package donut

import (
	"image/color"
	"strconv"
	"strings"
)

// Palette is the donut's colours (the Soft theme's resources it binds).
type Palette struct {
	Slice, SliceBorder, Center, Accent, OnAccent, Foreground, Secondary, Danger, Success color.NRGBA
	Dark                                                                                 bool
}

// ParseColor reads #RRGGBB or #AARRGGBB.
func ParseColor(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	switch len(s) {
	case 6:
		return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
	case 8:
		return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), uint8(v >> 24)}, true
	}
	return color.NRGBA{}, false
}

func mustColor(s string) color.NRGBA { c, _ := ParseColor(s); return c }

// DefaultAccent is the accent of a new install.
const DefaultAccent = "#7F77DD"

// NewPalette is the Soft theme for light or dark mode with the accent colour
// (#RRGGBB) and the user's theme.json colours of that variant (may be nil).
func NewPalette(dark bool, accent string, custom map[string]string) Palette {
	p := Palette{Dark: dark}
	if dark {
		p.Slice, p.SliceBorder = mustColor("#262624"), mustColor("#1CFFFFFF")
		p.Foreground, p.Secondary = mustColor("#F1EFE8"), mustColor("#AEACA4")
		p.Danger, p.Success = mustColor("#F09595"), mustColor("#5DCAA5")
	} else {
		p.Slice, p.SliceBorder = mustColor("#FFFFFF"), mustColor("#16000000")
		p.Foreground, p.Secondary = mustColor("#1F1E1D"), mustColor("#6B6A66")
		p.Danger, p.Success = mustColor("#C73A39"), mustColor("#0F7A5A")
	}
	for name, value := range custom {
		c, ok := ParseColor(value)
		if !ok {
			continue
		}
		switch name {
		case "surface":
			p.Slice = c
		case "border":
			p.SliceBorder = c
		case "text":
			p.Foreground = c
		case "textSecondary":
			p.Secondary = c
		case "danger":
			p.Danger = c
		case "success":
			p.Success = c
		}
	}
	p.Center = p.Slice
	a, ok := ParseColor(accent)
	if !ok {
		a = mustColor(DefaultAccent)
	}
	a.A = 255
	p.Accent = a
	// As SoftPalette.ApplyAccent: dark text on a light accent.
	if luminance(a) > 0.6 {
		p.OnAccent = mustColor("#1F1E1D")
	} else {
		p.OnAccent = color.NRGBA{255, 255, 255, 255}
	}
	return p
}

func luminance(c color.NRGBA) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}
