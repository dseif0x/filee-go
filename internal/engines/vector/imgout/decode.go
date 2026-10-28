package imgout

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"slices"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
)

// Readable are the image formats ImageMagick reads in the original
// (ImageEncoder.Readable).
var Readable = []string{
	"png", "jpg", "webp", "tiff", "bmp", "gif", "ico", "avif", "heic",
	"jxl", "jp2", "psd", "psb", "tga", "ppm", "xcf", "raw",
	"apng", "mng", "qoi", "dds", "pcx", "cur", "xpm", "xbm", "wbmp", "pam", "pfm", "sgi", "ras", "miff",
	"dcm", "pcd", "exr", "hdr", "fits",
}

var builtinRead = []string{"png", "jpg", "gif", "bmp", "tiff", "webp", "avif", "heic"}

// Reader decodes the first picture of a file (turned upright).
type Reader func(path, format string) (image.Image, error)

// ReadHook is the image engine's decoder; nil uses the built-in decoders.
var ReadHook Reader

// Sources are the formats pictures can be read from.
func Sources() []string {
	if ReadHook != nil {
		return Readable
	}
	return builtinRead
}

// Read decodes a picture.
func Read(path, format string) (image.Image, error) {
	if ReadHook != nil {
		return ReadHook(path, format)
	}
	if !slices.Contains(builtinRead, format) {
		return nil, fmt.Errorf("reading %s needs the image engine", format)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	switch format {
	case "webp":
		return webp.Decode(r)
	case "avif":
		return avif.Decode(r)
	case "heic":
		return heic.Decode(r)
	}
	img, _, err := image.Decode(r)
	return img, err
}
