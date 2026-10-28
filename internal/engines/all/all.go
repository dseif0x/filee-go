// Package all lists every conversion engine. The order is the default engine
// priority: earlier engines win ties in route planning.
package all

import (
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/data"
	"github.com/dseif0x/filee-go/internal/engines/fonts"
	"github.com/dseif0x/filee-go/internal/engines/ghostscript"
	"github.com/dseif0x/filee-go/internal/engines/media"
	"github.com/dseif0x/filee-go/internal/engines/ocr"
	"github.com/dseif0x/filee-go/internal/engines/subtitles"
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
		// OCR: Apple Vision first, so a Mac needs no download for the languages it reads.
		ocr.NewVision(),
		ocr.NewTesseract(),
		// PostScript
		ghostscript.New(),
	}
}

// Catalog returns a catalog of all engines.
func Catalog() *convert.Catalog { return convert.NewCatalog(Converters()...) }
