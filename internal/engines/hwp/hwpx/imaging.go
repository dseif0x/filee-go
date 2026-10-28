package hwpx

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/images"
)

// imageSize is the pixel size of a picture file; ok is false when the image engine can't read it.
func imageSize(path string) (w, h int, ok bool) { return images.PictureSize(path) }

// readable tells whether a picture can be decoded (so it can be converted to PNG).
func readable(path string) bool {
	_, _, ok := imageSize(path)
	return ok
}

// writePNG writes the picture at path as PNG (WebP, TIFF, AVIF, PSD, … as the C# did with ImageMagick).
func writePNG(path string, w io.Writer) error { return images.WritePNG(path, w) }

// copyFile copies a file into w.
func copyFile(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// ext is the lower-case extension without the dot, "jpeg"/"jpe" as "jpg".
func ext(path string) string {
	e := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if e == "jpeg" || e == "jpe" {
		return "jpg"
	}
	return e
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
