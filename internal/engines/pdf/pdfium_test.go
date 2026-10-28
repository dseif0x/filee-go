package pdf

import (
	"context"
	"testing"
	"time"
)

func TestPdfiumRenderTiming(t *testing.T) {
	path := buildPDF(t, testPage{W: 595, H: 842, Content: textLines(12, 72, 770, "Hello PDFium", "Second line of text") + "0 0 1 rg 100 100 200 150 re f"})
	start := time.Now()
	d, err := openPdfiumFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Since(start)
	n, _ := d.PageCount()
	if n != 1 {
		t.Fatalf("pages %d", n)
	}
	r0 := time.Now()
	img, err := d.Render(0, 150)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(r0)
	r1 := time.Now()
	const runs = 5
	for i := 0; i < runs; i++ {
		if _, err := d.Render(0, 150); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(r1) / runs
	d.Close()
	b := img.Bounds()
	if b.Dx() < 1230 || b.Dx() > 1242 {
		t.Fatalf("width %d", b.Dx())
	}
	// The blue rectangle at (150, 125) pt from the bottom left.
	c := img.RGBAAt(312, b.Dy()-364)
	if c.B < 200 || c.R > 50 {
		t.Fatalf("colour %v", c)
	}
	t.Logf("pool init %v (open incl. init %v), first render %v, per page at 150 dpi %v", PdfiumInitTime, opened, first, per)
}
