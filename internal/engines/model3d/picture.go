package model3d

// The rendered preview goes through the image engine like other pictures:
// the preset's picture options (turn, mirror, resize, background, grey,
// quality, size limit) and every format it writes (C# ModelConverter.WriteImage
// with ImageEncoder.ApplyOptions and ImageEncoder.Writable).

import (
	"image"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

// PictureFormats are the picture formats of the preview: what the image engine writes.
var PictureFormats = images.Writable

// writePicture applies the options to the preview and writes it as format.
func writePicture(img *image.NRGBA, format string, out convert.OutputAllocator, o *presets.ImageOptions) ([]string, error) {
	if o == nil {
		d := presets.DefaultImageOptions()
		o = &d
	}
	f := images.NewFrame(img)
	images.ApplyOptions(f, o, format)
	return images.Write([]*images.Frame{f}, format, out, o, "", false, 0)
}
