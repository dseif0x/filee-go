package hwpx

// Anything → HWPX without Hancom Office: a reader turns the source into the document model (internal/doc's registry:
// DOCX, Markdown, text, spreadsheets, PPTX, PDF, OFD, HTML, HWPX, e-books ... whatever is registered) and the writer
// writes the OWPML package. ODT, RTF, reStructuredText, LaTeX and the other markups are parsed by Pandoc (optional
// engine) into its JSON AST.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/ebook"
	_ "github.com/dseif0x/filee-go/internal/engines/htmldoc" // the HTML document reader
	"github.com/dseif0x/filee-go/internal/engines/pandoc"
	"github.com/dseif0x/filee-go/internal/tools"
)

const pandocTimeout = 2 * time.Minute

// E-books → HWPX go through the e-book engine's readers and this writer.
func init() {
	ebook.HwpxWriter = func(d *doc.Document, output string) error { return Write(d, output, nil) }
}

// throughPandoc are the formats read through Pandoc's JSON AST when Pandoc is installed: format id → Pandoc reader.
// A built-in reader for the same format wins.
var throughPandoc = []struct{ format, reader string }{
	{"odt", "odt"}, {"rtf", "rtf"}, {"html", "html"}, {"rst", "rst"}, {"tex", "latex"}, {"org", "org"},
	{"adoc", "asciidoc"}, {"typ", "typst"}, {"mediawiki", "mediawiki"}, {"dbk", "docbook"}, {"ipynb", "ipynb"},
	{"textile", "textile"}, {"man", "man"}, {"opml", "opml"},
}

// PandocFormats are the formats that need Pandoc (those without a built-in reader).
func PandocFormats() []string {
	var out []string
	for _, p := range throughPandoc {
		if !doc.HasReader(p.format) {
			out = append(out, p.format)
		}
	}
	return out
}

// ReaderFormats are every readable format; the Pandoc ones only when Pandoc is available.
func ReaderFormats(withPandoc bool) []string {
	formats := doc.ReaderFormats()
	if withPandoc {
		formats = append(formats, PandocFormats()...)
	}
	return formats
}

// NeedsPandoc tells whether format can only be read with Pandoc.
func NeedsPandoc(format string) bool {
	return !doc.HasReader(format) && slices.Contains(PandocFormats(), format)
}

// OcrReader, when set, reads a PDF through its OCR text layer (the PDF engine registers it); used when the preset
// asks for OCR.
var OcrReader doc.ReadFunc

// ReadDocument reads path (of format) into the document model. Extracted pictures go into a new folder inside
// workDir.
func ReadDocument(ctx context.Context, path, format, workDir string, ocrLayer bool) (*doc.Document, error) {
	media, err := os.MkdirTemp(workDir, "media-")
	if err != nil {
		return nil, err
	}
	if ocrLayer && format == "pdf" && OcrReader != nil {
		return OcrReader(path, media)
	}
	if doc.HasReader(format) {
		return doc.Read(path, format, media)
	}
	for _, p := range throughPandoc {
		if p.format == format {
			return readWithPandoc(ctx, path, p.reader, workDir, media)
		}
	}
	return nil, fmt.Errorf("there is no document reader for %s files", strings.ToUpper(format))
}

func readWithPandoc(ctx context.Context, path, reader, workDir, media string) (*doc.Document, error) {
	exe := pandoc.Locate()
	if exe == "" {
		return nil, errors.New("pandoc was not found")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	inputFolder := filepath.Dir(abs)
	out, err := os.CreateTemp(workDir, "ast-*.json")
	if err != nil {
		return nil, err
	}
	out.Close()
	// Working directory = the input's folder, so relative image paths in HTML and LaTeX resolve.
	_, runErr := tools.Run(ctx, tools.Cmd{Path: exe, Dir: inputFolder, Timeout: pandocTimeout,
		Args: []string{abs, "-f", reader, "-t", "json", "--extract-media=" + media, "-o", out.Name()}})
	data, err := os.ReadFile(out.Name())
	if err != nil || len(data) == 0 {
		if runErr != nil {
			return nil, fmt.Errorf("pandoc failed: %w", runErr)
		}
		return nil, errors.New("pandoc produced an empty document")
	}
	return ReadPandocAST(data, func(target string) string { return resolvePandocImage(target, inputFolder) })
}

// resolvePandocImage: images extracted by Pandoc have absolute paths; HTML may use paths relative to the input.
func resolvePandocImage(target, inputFolder string) string {
	if strings.TrimSpace(target) == "" || strings.Contains(target, "://") {
		return ""
	}
	p, err := url.PathUnescape(target)
	if err != nil {
		p = target
	}
	for _, candidate := range []string{p, filepath.Join(inputFolder, p)} {
		if fileExists(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
		}
	}
	return ""
}

// Converter is the "hwpx-writer" engine: HWPX files from every format a document reader reads.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "hwpx-writer" }
func (*Converter) Name() string        { return "HWPX writer (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var edges []convert.Edge
	for _, from := range ReaderFormats(pandoc.Locate() != "") {
		if from == "hwpx" {
			continue // HWPX is read only to write other formats (DOCX, HTML, EPUB, ...)
		}
		cost := 10
		if from == "pdf" {
			cost += doc.PdfPenalty
		}
		edges = append(edges, convert.EC(from, "hwpx", cost))
	}
	return edges
}

func (*Converter) Status() convert.Status {
	upper := func(list []string) string {
		out := make([]string, len(list))
		for i, f := range list {
			out[i] = strings.ToUpper(f)
		}
		return strings.Join(out, ", ")
	}
	builtIn, withPandoc := upper(doc.ReaderFormats()), upper(PandocFormats())
	if p := pandoc.Locate(); p != "" {
		return convert.Available(fmt.Sprintf("%s; %s with Pandoc (%s)", builtIn, withPandoc, p), "built-in")
	}
	return convert.Available(fmt.Sprintf("%s → HWPX. %s need Pandoc.", builtIn, withPandoc), "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	ocr := step.Preset != nil && step.Preset.Pdf.Ocr
	d, err := ReadDocument(ctx, step.InputPath, step.From, step.WorkDir, ocr)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.6)
	output := step.Output.Allocate("hwpx", "")
	if output == "" {
		return nil, nil
	}
	if err := Write(d, output, nil); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}
