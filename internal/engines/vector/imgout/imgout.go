// Package imgout writes pictures rendered by the vector and CAD engines in the
// image formats, and reads pictures for tracing, through the image engine
// (ImageMagick's role in the original).
package imgout

import (
	"image"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

// Writable are the formats rendered pictures can be written as (ImageEncoder.Writable).
var Writable = images.Writable

// Readable are the raster formats pictures are read from (ImageEncoder.Readable).
var Readable = images.Readable

// Edge is ImageEncoder.Edge.
func Edge(source, target string) convert.Edge { return images.Edge(source, target) }

// Write applies the preset's image options to a rendered picture and writes
// it as target. applyResize is false when the picture was rendered at its
// final size already: the resize and minimum short edge are then dropped
// (a custom size stays, as the exact stretch of the render). dpi is the
// picture's resolution.
func Write(img image.Image, dpi float64, target string, out convert.OutputAllocator, o *presets.ImageOptions, applyResize bool) ([]string, error) {
	opts := presets.DefaultImageOptions()
	if o != nil {
		opts = *o
	}
	if !applyResize {
		opts.Resize = presets.ResizeNone
		opts.MinShortEdge = nil
	}
	f := images.NewFrame(img)
	f.Meta.DpiX, f.Meta.DpiY = dpi, dpi
	images.ApplyOptions(f, &opts, target)
	return images.Write([]*images.Frame{f}, target, out, &opts, "", false, 0)
}

// Read decodes the first picture of a file, turned upright.
func Read(path, format string) (*image.NRGBA, error) { return images.DecodeImage(path, format) }
