package pdf

import (
	"bytes"
	"context"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

func threePages(t *testing.T) string {
	return buildPDF(t,
		testPage{W: 595, H: 842, Content: textLines(14, 72, 770, "Page one")},
		testPage{W: 595, H: 842, Content: textLines(14, 72, 770, "Page two")},
		testPage{W: 842, H: 595, Content: textLines(14, 72, 500, "Page three")})
}

func TestImageToPdf(t *testing.T) {
	src := writePNG(t, solid(200, 100, color.NRGBA{255, 0, 0, 255}))
	step := newStep(t, src, "png", "pdf", nil)
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	s := pdfInfo(t, out[0])
	// 96 dpi: 200 px = 150 pt.
	if len(s) != 1 || !near(s[0][0], 150, 0.5) || !near(s[0][1], 75, 0.5) {
		t.Fatalf("sizes %v", s)
	}
	img := renderPage(t, out[0], 0, 72)
	if c := img.RGBAAt(75, 37); c.R < 240 || c.G > 20 {
		t.Fatalf("colour %v", c)
	}

	// A4 with a 20 mm margin, landscape for a wide picture, rotated right (tall then).
	step = newStep(t, src, "png", "pdf", func(p *presets.Preset) {
		p.Pdf.PageSize = presets.PageA4
		p.Pdf.MarginMm = 20
		p.Image.Rotation = presets.Right90
	})
	out, err = PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = pdfInfo(t, out[0])
	if !near(s[0][0], 595.28, 0.5) || !near(s[0][1], 841.89, 0.5) {
		t.Fatalf("A4 portrait expected, got %v", s)
	}
	img = renderPage(t, out[0], 0, 72)
	// The margin stays white, the centre is red.
	if c := img.RGBAAt(10, 420); c.G < 240 {
		t.Fatalf("margin %v", c)
	}
	if c := img.RGBAAt(297, 420); c.G > 20 {
		t.Fatalf("centre %v", c)
	}
}

func TestJpegKeptAsItIs(t *testing.T) {
	data := encodeJpegBytes(noisy(64, 48, 1, true), 90)
	src := t.TempDir() + "/photo.jpg"
	os.WriteFile(src, data, 0o644)
	step := newStep(t, src, "jpg", "pdf", nil)
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := os.ReadFile(out[0])
	if !bytes.Contains(pdf, data) || !bytes.Contains(pdf, []byte("/DCTDecode")) {
		t.Fatal("the JPEG should be embedded unchanged")
	}
}

func TestSplitExtractRotate(t *testing.T) {
	src := threePages(t)
	step := newStep(t, src, "pdf", "pdf", func(p *presets.Preset) { p.Pdf.SplitPages = true })
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 3 || !strings.HasSuffix(out[2], "_p3.pdf") {
		t.Fatal(out, err)
	}
	if s := pdfInfo(t, out[2]); len(s) != 1 || s[0][0] != 842 {
		t.Fatalf("page 3 %v", s)
	}

	step = newStep(t, src, "pdf", "pdf", func(p *presets.Preset) {
		p.Pdf.PageRange = "2-3"
		p.Pdf.Rotation = presets.PdfRight90
	})
	out, err = PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	s := pdfInfo(t, out[0])
	if len(s) != 2 || s[0][0] != 842 || s[1][0] != 595 {
		t.Fatalf("turned range %v", s)
	}

	// The whole document turned keeps its information.
	step = newStep(t, src, "pdf", "pdf", func(p *presets.Preset) { p.Pdf.Rotation = presets.PdfLeft90 })
	out, err = PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := pdfInfo(t, out[0]); len(s) != 3 || s[0][0] != 842 {
		t.Fatalf("turned %v", s)
	}
	if data, _ := os.ReadFile(out[0]); !bytes.Contains(data, []byte("Test title")) {
		t.Fatal("the document information was lost")
	}
}

func TestRemoveMetadata(t *testing.T) {
	src := threePages(t)
	step := newStep(t, src, "pdf", "pdf", func(p *presets.Preset) { p.Pdf.RemoveMetadata = true })
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out[0])
	if bytes.Contains(data, []byte("Test title")) || bytes.Contains(data, []byte("Someone")) {
		t.Fatal("metadata still there")
	}
	if s := pdfInfo(t, out[0]); len(s) != 3 {
		t.Fatal(s)
	}
}

func TestMerge(t *testing.T) {
	a, b := threePages(t), buildPDF(t, testPage{W: 300, H: 300, Content: textLines(10, 20, 250, "Small")})
	out := t.TempDir() + "/merged.pdf"
	if err := (PdfSharp{}).MergePdfs(context.Background(), []string{a, b}, out); err != nil {
		t.Fatal(err)
	}
	s := pdfInfo(t, out)
	if len(s) != 4 || s[3][0] != 300 {
		t.Fatal(s)
	}
}

// "Compress with Ghostscript": a stand-in Ghostscript whose PDF is smaller wins.
func TestCompressWithGhostscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in Ghostscript is a shell script")
	}
	dir := t.TempDir()
	gs := filepath.Join(dir, "gs")
	script := "#!/bin/sh\nfor a; do case \"$a\" in -sOutputFile=*) printf '%%PDF-1.4 gs' > \"${a#-sOutputFile=}\";; esac; done\n"
	if err := os.WriteFile(gs, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILEE_GHOSTSCRIPT", gs)
	tools.Forget()
	t.Cleanup(tools.Forget)
	step := newStep(t, threePages(t), "pdf", "pdf", func(p *presets.Preset) {
		p.Pdf.Compression = presets.PdfCompressEbook
		p.Pdf.CompressWithGhostscript = true
	})
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	if data, _ := os.ReadFile(out[0]); string(data) != "%PDF-1.4 gs" {
		t.Fatalf("Ghostscript's smaller PDF should win: %q", data[:min(len(data), 40)])
	}
}

// Any picture the image engine reads goes into a PDF (C# ImageEncoder.Sources → pdf).
func TestImageEngineSourcesToPdf(t *testing.T) {
	for _, from := range []string{"qoi", "psd", "tga"} {
		src := filepath.Join(t.TempDir(), "in."+from)
		if err := images.Encode(src, solid(200, 100, color.NRGBA{0, 0, 255, 255}), from, nil); err != nil {
			t.Fatal(err)
		}
		out, err := PdfSharp{}.Convert(context.Background(), newStep(t, src, from, "pdf", nil), nil)
		if err != nil || len(out) != 1 {
			t.Fatal(from, out, err)
		}
		if s := pdfInfo(t, out[0]); len(s) != 1 || !near(s[0][0], 150, 0.5) {
			t.Fatal(from, s)
		}
	}
}
