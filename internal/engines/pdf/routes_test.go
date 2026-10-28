package pdf

import (
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

func TestRoutes(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{PdfSharp{}, Pdfium{}, PdfText{}, PdfSlides{}, PdfTables{}}, nil)
	for _, c := range []struct{ from, to, first string }{
		{"png", "pdf", "pdfsharp"}, {"heic", "pdf", "pdfsharp"}, {"pdf", "pdf", "pdfsharp"},
		{"pdf", "webp", "pdfium"}, {"pdf", "txt", "pdf-text"}, {"pdf", "md", "pdf-text"}, {"pdf", "docx", "pdf-text"},
		{"pdf", "pptx", "pdf-slides"}, {"pdf", "xlsx", "pdf-tables"}, {"jpg", "png", "pdfsharp"},
	} {
		r := p.Plan(c.from, c.to, convert.Options{})
		if r == nil || r.Steps[0].Converter.ID() != c.first {
			t.Errorf("%s → %s: %+v", c.from, c.to, r)
		}
	}
	// Tables and slides only straight from a PDF.
	if r := p.Plan("png", "xlsx", convert.Options{}); r != nil {
		t.Errorf("png → xlsx should not route: %+v", r)
	}
}
