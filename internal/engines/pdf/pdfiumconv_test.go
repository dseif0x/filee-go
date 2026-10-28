package pdf

import (
	"bytes"
	"context"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

func decodeFile(t *testing.T, path string) image.Image {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img
}

func TestPdfiumPagesToPictures(t *testing.T) {
	src := threePages(t)
	for _, to := range []string{"png", "jpg", "webp", "tiff", "bmp", "gif", "avif", "ppm", "tga", "psd", "qoi", "jxl", "ico"} {
		step := newStep(t, src, "pdf", to, func(p *presets.Preset) { p.Pdf.RenderDpi = 72; p.Pdf.PageRange = "1,3" })
		out, err := Pdfium{}.Convert(context.Background(), step, nil)
		if err != nil {
			t.Fatalf("%s: %v", to, err)
		}
		want := 2
		if multiPageTargets[to] {
			want = 1
		}
		if len(out) != want {
			t.Fatalf("%s: %v", to, out)
		}
		pic, err := images.Decode(out[0], to, nil)
		if err != nil {
			t.Fatalf("%s: %v", to, err)
		}
		if to == "ico" || to == "gif" {
			continue // icons are at most 256 × 256; a GIF's frames share the largest page's screen
		}
		img := pic.Frames[0].Img
		if b := img.Bounds(); b.Dx() != 595 || b.Dy() != 842 {
			t.Fatalf("%s: %v", to, b)
		}
		if !multiPageTargets[to] && !strings.HasSuffix(out[1], "_p3."+to) {
			t.Fatalf("%s: %v", to, out)
		}
	}
	tiffOut := newStep(t, src, "pdf", "tiff", func(p *presets.Preset) { p.Pdf.RenderDpi = 72 })
	out, _ := Pdfium{}.Convert(context.Background(), tiffOut, nil)
	pic, err := images.Decode(out[0], "tiff", nil)
	if err != nil || len(pic.Frames) != 3 || pic.Frames[2].Width() != 842 {
		t.Fatalf("multi-page TIFF: %v", err)
	}
}

func TestPdfiumCombinePages(t *testing.T) {
	src := threePages(t)
	step := newStep(t, src, "pdf", "png", func(p *presets.Preset) {
		p.Pdf.RenderDpi = 72
		p.Pdf.CombinePages = true
	})
	out, err := Pdfium{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	b := decodeFile(t, out[0]).Bounds()
	if b.Dx() != 842 || b.Dy() != 842+842+595 {
		t.Fatal(b)
	}
	step = newStep(t, src, "pdf", "png", func(p *presets.Preset) {
		p.Pdf.RenderDpi = 72
		p.Pdf.CombinePages = true
		p.Pdf.TrimPageMargins = true
	})
	out, _ = Pdfium{}.Convert(context.Background(), step, nil)
	if b := decodeFile(t, out[0]).Bounds(); b.Dy() > 200 {
		t.Fatalf("trimmed height %d", b.Dy())
	}
	// Split for JPG: 10 tall pages at 7200 px each are more than 65,500 px.
	tall := make([]testPage, 10)
	for i := range tall {
		tall[i] = testPage{W: 100, H: 7200}
	}
	step = newStep(t, buildPDF(t, tall...), "pdf", "jpg", func(p *presets.Preset) {
		p.Pdf.RenderDpi = 72
		p.Pdf.CombinePages = true
		p.Pdf.SplitTallImage = true
	})
	out, err = Pdfium{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 2 || !strings.HasSuffix(out[1], "_2.jpg") {
		t.Fatal(out, err)
	}
}

func TestPreviews(t *testing.T) {
	doc, err := OpenPreview(threePages(t))
	if err != nil || doc.PageCount() != 3 {
		t.Fatal(err)
	}
	p, err := doc.Preview(context.Background(), 2, 400)
	// At least 36 dpi: 421 px for the 842 pt wide page.
	if err != nil || p.Width != 421 {
		t.Fatal(err)
	}
	thumbs, err := doc.RenderThumbnails(context.Background(), 100)
	if err != nil || len(thumbs) != 3 || thumbs[2].Width != 100 || thumbs[2].PageNumber != 3 {
		t.Fatal(err)
	}
}
