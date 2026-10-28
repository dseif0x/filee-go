package ofd

import (
	"image"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/images"
)

// JPEG 2000 and TIFF pictures are read by the image engine and kept as PNG (C# ImageMagick).
func TestJpeg2000AndTiffPictures(t *testing.T) {
	r := &reader{media: t.TempDir()}
	for _, f := range []string{"jp2", "tiff"} {
		b, err := images.EncodeImage(image.NewNRGBA(image.Rect(0, 0, 16, 8)), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		path := r.save(b)
		if !strings.HasSuffix(path, ".png") {
			t.Fatalf("%s: %q", f, path)
		}
		img, err := decodeFile(path)
		if err != nil || img.Bounds().Dx() != 16 {
			t.Fatalf("%s: %v", f, err)
		}
	}
}
