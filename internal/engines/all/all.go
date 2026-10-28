// Package all lists every conversion engine. The order is the default engine
// priority: earlier engines win ties in route planning.
package all

import (
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/archive"
	"github.com/dseif0x/filee-go/internal/engines/calibre"
	"github.com/dseif0x/filee-go/internal/engines/data"
	"github.com/dseif0x/filee-go/internal/engines/docxwriter"
	"github.com/dseif0x/filee-go/internal/engines/ebook"
	"github.com/dseif0x/filee-go/internal/engines/email"
	"github.com/dseif0x/filee-go/internal/engines/fonts"
	"github.com/dseif0x/filee-go/internal/engines/geo"
	"github.com/dseif0x/filee-go/internal/engines/ghostscript"
	"github.com/dseif0x/filee-go/internal/engines/hwp/hwpx"
	"github.com/dseif0x/filee-go/internal/engines/hwp/rhwp"
	"github.com/dseif0x/filee-go/internal/engines/hwp/unhwp"
	"github.com/dseif0x/filee-go/internal/engines/icns"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/engines/libreoffice"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
	"github.com/dseif0x/filee-go/internal/engines/media"
	"github.com/dseif0x/filee-go/internal/engines/model3d"
	"github.com/dseif0x/filee-go/internal/engines/ocr"
	_ "github.com/dseif0x/filee-go/internal/engines/ofd" // the OFD document reader
	"github.com/dseif0x/filee-go/internal/engines/office"
	"github.com/dseif0x/filee-go/internal/engines/pandoc"
	"github.com/dseif0x/filee-go/internal/engines/pdf"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
	"github.com/dseif0x/filee-go/internal/engines/subtitles"
	"github.com/dseif0x/filee-go/internal/engines/vector"
	"github.com/dseif0x/filee-go/internal/engines/wic"
)

// Converters returns a fresh instance of every engine.
func Converters() []convert.Converter {
	return []convert.Converter{
		// Engines add themselves here, one line each (keep the groups).
		fonts.New(),
		data.New(),
		subtitles.New(),
		// Media
		media.New(),
		// 3D models and geospatial data (C# order: after FFmpeg and CAD)
		model3d.New(),
		geo.New(),
		// OCR: Apple Vision first, so a Mac needs no download for the languages it reads.
		ocr.NewVision(),
		ocr.NewTesseract(),
		// PostScript
		ghostscript.New(),
		// PDF (C# order: PdfSharp, Pdfium near the top; text, slides, tables after the DOCX writer).
		pdf.PdfSharp{},
		pdf.Pdfium{},
		pdf.PdfText{},
		pdf.PdfSlides{},
		pdf.PdfTables{},
		email.New(),
		// HWP (C# order: right after the PDF engines, the HWPX writer after OOXML)
		rhwp.New(),
		unhwp.New(),
		// office
		markdown.New(),
		spreadsheet.New(),
		office.New(),
		hwpx.New(),
		archive.New(),
		docxwriter.New(),
		// e-books before Pandoc (C# order); calibre ranks last
		ebook.New(),
		pandoc.New(),
		libreoffice.New(),
		images.New(),
		vector.New(),
		icns.New(),
		wic.New(),
		calibre.New(),
	}
}

// Catalog returns a catalog of all engines.
func Catalog() *convert.Catalog { return convert.NewCatalog(Converters()...) }
