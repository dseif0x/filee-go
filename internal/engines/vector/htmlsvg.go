package vector

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

// SVG pictures inside HTML, e-books and documents become PNGs for the
// document model, rendered like SVG → PNG with the default settings.
func init() { htmldoc.SVGRasterizer = RasterizeToFolder }

// RasterizeToFolder renders an SVG / SVGZ file to a new PNG in folder.
func RasterizeToFolder(svgPath, folder string) (string, error) {
	doc, err := load(svgPath)
	if err != nil {
		return "", err
	}
	img, pdf := presets.DefaultImageOptions(), presets.DefaultPdfOptions()
	r := Rasterize(doc, RasterScale(doc.Width, doc.Height, &img, &pdf))
	base := strings.TrimSuffix(filepath.Base(svgPath), filepath.Ext(svgPath))
	f, err := os.CreateTemp(folder, base+"-*.png")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	if err := images.Encode(path, r.Straight(), "png", nil); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
