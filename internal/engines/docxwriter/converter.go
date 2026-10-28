package docxwriter

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
)

// cost is below Pandoc's and LibreOffice's direct edges (10 and more), so the built-in writer wins.
const cost = 8

// wordFormats: DOCX → DOCX is no conversion; the variants are the ooxml engine's job.
var wordFormats = []string{"docx", "docm", "dotx", "dotm"}

// Converter is the "docx-writer" engine: anything a built-in reader reads
// (doc.ReaderFormats) → DOCX.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "docx-writer" }
func (*Converter) Name() string        { return "DOCX writer (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, f := range doc.ReaderFormats() {
		if slices.Contains(wordFormats, f) {
			continue
		}
		c := cost
		if f == "pdf" {
			c += doc.PdfPenalty
		}
		edges = append(edges, convert.EC(f, "docx", c))
	}
	return edges
}

func (c *Converter) Status() convert.Status {
	var from []string
	for _, e := range c.Edges() {
		from = append(from, strings.ToUpper(e.From))
	}
	return convert.Available(strings.Join(from, ", ")+" → DOCX", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	media, err := os.MkdirTemp(step.WorkDir, "media-")
	if err != nil {
		return nil, err
	}
	var d *doc.Document
	if step.From == "pdf" && step.Preset != nil && step.Preset.Pdf.Ocr && doc.PdfOcrLayer != nil {
		d, err = doc.PdfOcrLayer(step.InputPath, filepath.Join(media, "files"))
	} else {
		d, err = doc.Read(step.InputPath, step.From, filepath.Join(media, "files"))
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.6)
	output := step.Output.Allocate("docx", "")
	if output == "" {
		return nil, nil
	}
	if err := Write(d, output); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}
