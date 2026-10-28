package cad

import (
	"image/png"
	"io"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

var testFonts = gfx.NewFonts(false)

func init() { fonts = func() *gfx.Fonts { return testFonts } }

func writePNG(w io.Writer, r *gfx.Raster) { png.Encode(w, r.Straight()) }
