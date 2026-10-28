package pdf

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
)

// FILEE_PDF=some.pdf runs every PDF engine on a real file.
func TestRealPdf(t *testing.T) {
	src := os.Getenv("FILEE_PDF")
	if src == "" {
		t.Skip("FILEE_PDF not set")
	}
	for _, c := range []struct {
		to   string
		edit func(*presets.Preset)
	}{
		{"txt", nil}, {"md", nil}, {"docx", nil}, {"xlsx", nil}, {"pptx", nil}, {"png", nil},
		{"pdf", func(p *presets.Preset) { p.Pdf.Compression = presets.PdfCompressEbook }},
		{"pdf", func(p *presets.Preset) { p.Pdf.RemoveMetadata = true }},
		{"pdf", func(p *presets.Preset) { p.Pdf.SplitPages = true; p.Pdf.Rotation = presets.PdfRight90 }},
	} {
		step := newStep(t, src, "pdf", c.to, c.edit)
		start := time.Now()
		var out []string
		var err error
		switch c.to {
		case "txt", "md":
			out, err = PdfText{}.Convert(context.Background(), step, nil)
		case "docx":
			out = []string{pdfToDocx(t, src)}
		case "xlsx":
			out, err = PdfTables{}.Convert(context.Background(), step, nil)
		case "pptx":
			out, err = PdfSlides{}.Convert(context.Background(), step, nil)
		case "png":
			out, err = Pdfium{}.Convert(context.Background(), step, nil)
		default:
			out, err = PdfSharp{}.Convert(context.Background(), step, nil)
		}
		t.Logf("%s: %v %v (%v)", c.to, out, err, time.Since(start))
		if c.to == "txt" && err == nil {
			data, _ := os.ReadFile(out[0])
			t.Logf("%s", data[:min(len(data), 1500)])
		}
	}
	path := t.TempDir() + "/f.pdf"
	copyWritable(src, path)
	o := presets.DefaultPdfOptions()
	o.Watermark, o.PageNumbers, o.Password = "DRAFT", true, "x"
	t.Log("finish:", FinishPdf(context.Background(), path, &o))
}
