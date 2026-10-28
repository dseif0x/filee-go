package hwpx

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// imageSize is the pixel size of a picture file; ok is false when it is no picture Go can read.
func imageSize(path string) (w, h int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// readable tells whether a picture can be decoded (so it can be converted to PNG).
func readable(path string) bool {
	_, _, ok := imageSize(path)
	return ok
}

// writePNG writes the picture at path as PNG.
func writePNG(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

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
