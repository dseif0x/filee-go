// Package all lists every conversion engine. The order is the default engine
// priority: earlier engines win ties in route planning.
package all

import (
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/pdf"
)

// Converters returns a fresh instance of every engine.
func Converters() []convert.Converter {
	return []convert.Converter{
		// Engines add themselves here, one line each (keep the groups).
		// PDF (C# order: PdfSharp, Pdfium near the top; text, slides, tables after the DOCX writer).
		pdf.PdfSharp{},
		pdf.Pdfium{},
		pdf.PdfText{},
		pdf.PdfSlides{},
		pdf.PdfTables{},
	}
}

// Catalog returns a catalog of all engines.
func Catalog() *convert.Catalog { return convert.NewCatalog(Converters()...) }
