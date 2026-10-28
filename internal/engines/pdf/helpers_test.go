package pdf

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// newStep is a conversion of input to `to` with outputs in a temp folder.
func newStep(t testing.TB, input, from, to string, edit func(p *presets.Preset)) *convert.Step {
	t.Helper()
	p := presets.New("test", to)
	if edit != nil {
		edit(p)
	}
	out := t.TempDir()
	base := filepath.Base(input)
	base = base[:len(base)-len(filepath.Ext(base))]
	return &convert.Step{
		InputPath: input, From: from, To: to, Preset: p, WorkDir: t.TempDir(),
		Output: convert.AllocatorFunc(func(ext, suffix string) string {
			return filepath.Join(out, base+suffix+"."+ext)
		}),
	}
}

func writePNG(t testing.TB, img image.Image) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "picture.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// pdfInfo opens a PDF with PDFium: page count and shown sizes.
func pdfInfo(t testing.TB, path string) [][2]float64 {
	t.Helper()
	d, err := openPdfiumFile(context.Background(), path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer d.Close()
	s, err := d.PageSizes()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func renderPage(t testing.TB, path string, page, dpi int) *image.RGBA {
	t.Helper()
	d, err := openPdfiumFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	img, err := d.Render(page, dpi)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(a, b, tol float64) bool { return a-b <= tol && b-a <= tol }
