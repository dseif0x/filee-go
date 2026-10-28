package pdf

import (
	"context"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"github.com/dseif0x/filee-go/internal/presets"
)

// A PDF locked only against changes (it opens without a password) can still be split, turned and finished.
func TestOwnerPasswordOnly(t *testing.T) {
	src := threePages(t)
	locked := t.TempDir() + "/locked.pdf"
	conf := pdfConf()
	conf.OwnerPW = "owner"
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256
	if err := api.EncryptFile(context.Background(), src, locked, conf); err != nil {
		t.Fatal(err)
	}
	step := newStep(t, locked, "pdf", "pdf", func(p *presets.Preset) { p.Pdf.PageRange = "2"; p.Pdf.Rotation = presets.PdfRotate180 })
	out, err := PdfSharp{}.Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := pdfInfo(t, out[0]); len(s) != 1 {
		t.Fatal(s)
	}
	o := presets.DefaultPdfOptions()
	o.PageNumbers = true
	if err := FinishPdf(context.Background(), out[0], &o); err != nil {
		t.Fatal(err)
	}
	if n, err := PageCount(locked); err != nil || n != 3 {
		t.Fatal(n, err)
	}
}
