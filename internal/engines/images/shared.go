package images

import (
	"image"
	"image/png"
	"io"
	"os"
)

// PictureSize is the pixel size of a picture file, for writers that place
// pictures (DOCX, HWPX, EPUB, …; C# MagickImageInfo): from its header where
// Go's decoders read one, else by decoding it. ok is false when the image
// engine cannot read the file.
func PictureSize(path string) (w, h int, ok bool) {
	if f, err := os.Open(path); err == nil {
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err == nil && cfg.Width > 0 && cfg.Height > 0 {
			return cfg.Width, cfg.Height, true
		}
	}
	img, err := DecodeImage(path, "")
	if err != nil {
		return 0, 0, false
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), b.Dx() > 0 && b.Dy() > 0
}

// WritePNG writes the first frame of a picture file as PNG, for formats a
// document's reader may not show (WebP, TIFF, AVIF, HEIC, PSD, …).
func WritePNG(path string, w io.Writer) error {
	img, err := DecodeImage(path, "")
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}
