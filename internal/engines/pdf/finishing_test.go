package pdf

import (
	"context"
	"image/color"
	"os"
	"testing"

	"github.com/dseif0x/filee-go/internal/presets"
)

func greyish(c color.RGBA) bool {
	return c.R < 245 && c.R > 150 && absDiff(c.R, c.G) < 8 && absDiff(c.G, c.B) < 8
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func countGrey(t *testing.T, path string, page int, x0, y0, x1, y1 int, dark bool) int {
	img := renderPage(t, path, page, 72)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if dark && c.R < 200 && absDiff(c.R, c.B) < 8 || !dark && greyish(c) {
				n++
			}
		}
	}
	return n
}

func TestFinishWatermarkNumbersPassword(t *testing.T) {
	for _, rotate := range []int{0, 90} {
		src := buildPDF(t,
			testPage{W: 595, H: 842, Rotate: rotate},
			testPage{W: 595, H: 842, Rotate: rotate})
		path := t.TempDir() + "/out.pdf"
		copyWritable(src, path)
		opts := presets.DefaultPdfOptions()
		opts.Watermark = "CONFIDENTIAL"
		opts.PageNumbers = true
		if err := FinishPdf(context.Background(), path, &opts); err != nil {
			t.Fatal(err)
		}
		s := pdfInfo(t, path)
		w, h := int(s[0][0]), int(s[0][1])
		// The watermark across the middle of the shown page.
		if n := countGrey(t, path, 1, w/2-60, h/2-30, w/2+60, h/2+30, false); n < 200 {
			t.Fatalf("rotate %d: watermark pixels %d", rotate, n)
		}
		// The page number "2 / 2" centred 10 mm above the bottom edge.
		if n := countGrey(t, path, 1, w/2-20, h-40, w/2+20, h-20, true); n < 5 {
			t.Fatalf("rotate %d: page number pixels %d", rotate, n)
		}
		if n := countGrey(t, path, 1, 0, 0, 40, 40, true); n != 0 {
			t.Fatalf("rotate %d: stray marks in the corner", rotate)
		}
	}

	src := buildPDF(t, testPage{W: 300, H: 300, Content: textLines(12, 20, 250, "Secret")})
	opts := presets.DefaultPdfOptions()
	opts.Password = "pässwörd"
	if err := FinishPdf(context.Background(), src, &opts); err != nil {
		t.Fatal(err)
	}
	if _, err := readContext(context.Background(), src); err != ErrPasswordProtected {
		t.Fatalf("expected a locked PDF, got %v", err)
	}
	data, _ := os.ReadFile(src)
	d, err := openPdfium(context.Background(), data)
	if err != ErrPasswordProtected {
		d.Close()
		t.Fatalf("pdfium opened it: %v", err)
	}
	opts.Password = string(make([]byte, 200))
	if err := FinishPdf(context.Background(), src, &opts); err == nil {
		t.Fatal("too long a password should fail")
	}
}

func TestCompressDownsamples(t *testing.T) {
	src := writePNG(t, noisy(3000, 2000, 3, false))
	step := newStep(t, src, "png", "pdf", func(p *presets.Preset) { p.Pdf.PageSize = presets.PageA4 })
	pdfs, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	var notice string
	step = newStep(t, pdfs[0], "pdf", "pdf", func(p *presets.Preset) { p.Pdf.Compression = presets.PdfCompressEbook })
	step.Notice = func(k string) { notice = k }
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(pdfs[0])
	after, _ := os.Stat(out[0])
	if after.Size() >= before.Size()/3 || notice != "" {
		t.Fatalf("%d → %d bytes, notice %q", before.Size(), after.Size(), notice)
	}
	if s := pdfInfo(t, out[0]); len(s) != 1 {
		t.Fatal(s)
	}
	// Never larger than the file was.
	step = newStep(t, out[0], "pdf", "pdf", func(p *presets.Preset) { p.Pdf.Compression = presets.PdfCompressScreen })
	again, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := os.Stat(again[0]); a.Size() > after.Size() {
		t.Fatalf("%d → %d bytes", after.Size(), a.Size())
	}
}

func TestBlankPagesAndPageTools(t *testing.T) {
	src := buildPDF(t,
		testPage{W: 200, H: 200, Content: textLines(12, 20, 150, "One")},
		testPage{W: 200, H: 200, Content: "q 1 0 0 1 0 0 cm Q"},
		testPage{W: 200, H: 200, Content: "0 0 m 100 100 l S"})
	path := t.TempDir() + "/blank.pdf"
	copyWritable(src, path)
	n, err := RemoveBlankPages(context.Background(), path)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if s := pdfInfo(t, path); len(s) != 2 {
		t.Fatal(s)
	}

	other := buildPDF(t, testPage{W: 400, H: 300})
	out := t.TempDir() + "/edited.pdf"
	err = SavePages(context.Background(), []PageReference{{Path: src, PageIndex: 2}, {Path: other, PageIndex: 0, Rotation: 90}, {Path: src, PageIndex: 0}}, out)
	if err != nil {
		t.Fatal(err)
	}
	s := pdfInfo(t, out)
	if len(s) != 3 || s[1][0] != 300 || s[0][0] != 200 {
		t.Fatal(s)
	}

	cropped := t.TempDir() + "/crop.pdf"
	if err := CropAll(context.Background(), src, cropped, 0.25, 0, 0.5, 0.5); err != nil {
		t.Fatal(err)
	}
	if s := pdfInfo(t, cropped); s[0][0] != 100 || s[0][1] != 100 {
		t.Fatal(s)
	}
}
