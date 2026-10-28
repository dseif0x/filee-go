// Package all lists every conversion engine. The order is the default engine
// priority: earlier engines win ties in route planning.
package all

import (
	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/docxwriter"
	"github.com/dseif0x/filee-go/internal/engines/libreoffice"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
	"github.com/dseif0x/filee-go/internal/engines/office"
	_ "github.com/dseif0x/filee-go/internal/engines/ofd" // the OFD document reader
	"github.com/dseif0x/filee-go/internal/engines/pandoc"
	"github.com/dseif0x/filee-go/internal/engines/spreadsheet"
)

// Converters returns a fresh instance of every engine.
func Converters() []convert.Converter {
	return []convert.Converter{
		// Engines add themselves here, one line each (keep the groups).
		// office
		markdown.New(),
		spreadsheet.New(),
		office.New(),
		docxwriter.New(),
		pandoc.New(),
		libreoffice.New(),
	}
}

// Catalog returns a catalog of all engines.
func Catalog() *convert.Catalog { return convert.NewCatalog(Converters()...) }
