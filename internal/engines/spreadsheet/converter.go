package spreadsheet

import (
	"context"
	"os"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
)

var (
	sources = []string{"xlsx", "xlsm", "xltx", "xls", "xlt", "ods", "csv", "tsv"}
	targets = []string{"xlsx", "ods", "csv", "tsv"}
)

// Converter is the "spreadsheet" engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "spreadsheet" }
func (*Converter) Name() string        { return "Spreadsheets (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, from := range sources {
		for _, to := range targets {
			// XLSM / XLTX → XLSX is the ooxml engine's, which keeps the whole package.
			if to != from && !(to == "xlsx" && (from == "xlsm" || from == "xltx")) {
				edges = append(edges, convert.E(from, to))
			}
		}
	}
	return edges
}

func (*Converter) Status() convert.Status {
	return convert.Available("XLSX, XLS, ODS, CSV, TSV → XLSX, ODS, CSV, TSV", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	book, err := Read(step.InputPath, step.From)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.6)
	out, err := Write(book, step)
	progress.Report(1)
	return out, err
}

// Read reads a spreadsheet of a format this engine accepts.
func Read(path, format string) (*Workbook, error) {
	switch strings.ToLower(format) {
	case "xls", "xlt":
		return ReadXLS(path)
	case "ods", "ots":
		return ReadODS(path)
	case "fods":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return ReadFODS(f)
	case "csv":
		return ReadCSV(path)
	case "tsv":
		return ReadTSV(path)
	}
	return ReadXLSX(path)
}

// Write writes a workbook as the step's XLSX, ODS, CSV or TSV. CSV and TSV
// hold one sheet: a workbook with several becomes one file per sheet.
func Write(book *Workbook, step *convert.Step) ([]string, error) {
	switch step.To {
	case "xlsx", "ods":
		path := step.Output.Allocate(step.To, "")
		if path == "" {
			return nil, nil
		}
		var err error
		if step.To == "xlsx" {
			err = WriteXLSX(book, path)
		} else {
			err = WriteODS(book, path)
		}
		if err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	var sheets []*Worksheet
	for _, s := range book.Sheets {
		if _, ok := s.UsedRange(); ok {
			sheets = append(sheets, s)
		}
	}
	if len(sheets) == 0 && len(book.Sheets) > 0 {
		sheets = book.Sheets[:1]
	}
	delimiter := ','
	if step.To == "tsv" {
		delimiter = '\t'
	}
	var outputs []string
	for _, s := range sheets {
		suffix := ""
		if len(sheets) > 1 {
			suffix = "_" + SafeName(s.Name)
		}
		file := step.Output.Allocate(step.To, suffix)
		if file == "" {
			continue
		}
		if err := WriteCSV(s, file, delimiter); err != nil {
			return outputs, err
		}
		outputs = append(outputs, file)
	}
	return outputs, nil
}

// SafeName makes a sheet name usable in a file name.
func SafeName(name string) string {
	clean := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name))
	if clean == "" {
		return "Sheet"
	}
	return clean
}

// SheetFormats are the formats read into documents through Document.
var SheetFormats = []string{"xlsx", "xlsm", "xltx", "csv", "tsv", "xls", "xlt", "ods"}

func init() {
	for _, f := range SheetFormats {
		doc.RegisterReader(f, func(path, _ string) (*doc.Document, error) {
			book, err := Read(path, f)
			if err != nil {
				return nil, err
			}
			return Document(book), nil
		})
	}
}
