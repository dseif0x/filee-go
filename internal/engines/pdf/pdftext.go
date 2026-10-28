// PDF → TXT, Markdown and DOCX, read by ReadDocument: the text in reading
// order (also on two-column pages), without running headers, footers and
// page numbers.
//
// TXT has one paragraph per line and an empty line between paragraphs; the
// invisible text layer of OCR-processed scans is included. Markdown keeps
// headings, bold / italic and links; the pictures go into a "<name>_files"
// folder next to it, and a page without text (a scan) becomes the picture of
// the page. With "Leave out pictures" the Markdown is read like TXT. DOCX
// keeps the layout as far as the document model holds it (page size and
// margins, indents, tabs, tables, pictures, a scan behind its page).

package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/presets"
)

// PdfPenalty is the extra cost of reading a PDF: its text is rebuilt from
// positioned words, so a route keeping the source's own structure wins.
const PdfPenalty = 6

// ErrNoText: a PDF without text (a scan).
var ErrNoText = errors.New("The PDF contains no text (it may be scanned; Filee does not recognise text in images).")

// PdfText extracts the text of PDF files.
type PdfText struct{}

func (PdfText) ID() string          { return "pdf-text" }
func (PdfText) Name() string        { return "PDF text (built-in)" }
func (PdfText) MaxParallelism() int { return pdfiumWorkers }

func (PdfText) Edges() []convert.Edge {
	return []convert.Edge{
		convert.E("pdf", "txt"),
		// Read once, written once; direct only, so no other route starts going
		// through a Markdown file and its picture folder.
		{From: "pdf", To: "md", Cost: 10 + PdfPenalty, DirectOnly: true},
		// The DOCX writer's edge for PDFs (cost 8 plus the PDF penalty).
		convert.EC("pdf", "docx", 8+PdfPenalty),
	}
}

func (PdfText) Status() convert.Status {
	return convert.Available("PDF → TXT, Markdown, DOCX", PdfiumVersion)
}

func (PdfText) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	switch step.To {
	case "md":
		return pdfToMarkdown(ctx, step, progress)
	case "docx":
		return pdfToDocx(ctx, step, progress)
	}
	return pdfToText(ctx, step, progress)
}

func mediaDir(step *convert.Step) string {
	dir, err := os.MkdirTemp(workDir(step), "media-")
	if err != nil {
		return filepath.Join(workDir(step), "media")
	}
	return dir
}

func pdfToText(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	doc, err := ReadDocument(ctx, step.InputPath, "", ReadTextOnly)
	if err != nil {
		return nil, err
	}
	text := PlainText(doc)
	if text == "" {
		return nil, ErrNoText
	}
	out := step.Output.Allocate("txt", "")
	if out == "" {
		return nil, nil
	}
	// UTF-8 with BOM, so Notepad and older tools pick the right encoding for Korean and Chinese text.
	if err := os.WriteFile(out, append([]byte{0xEF, 0xBB, 0xBF}, text...), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

func readMode(p *presets.Preset) readerMode {
	if p.Pdf.Ocr {
		return ReadOcrLayer
	}
	return ReadFull
}

func pdfToMarkdown(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	textOnly := step.Preset.Document.LeaveOutPictures
	mode := readMode(step.Preset)
	if textOnly {
		mode = ReadTextOnly
	}
	doc, err := ReadDocument(ctx, step.InputPath, mediaDir(step), mode)
	if err != nil {
		return nil, err
	}
	if textOnly && PlainText(doc) == "" {
		return nil, ErrNoText
	}
	progress.Report(0.6)
	// The PDF's information title is often a file name: no front matter. Indents
	// are the PDF's layout, not quotes, and headings are bold by nature.
	doc.Title = ""
	for _, s := range doc.Sections {
		for _, b := range s.Blocks {
			if p, ok := b.(*Paragraph); ok {
				p.Format.Left = nil
				if p.HeadingLevel > 0 {
					notBold(p.Inlines)
				}
			}
		}
	}
	out := step.Output.Allocate("md", "")
	if out == "" {
		return nil, nil
	}
	var pictures func(string) string
	if !textOnly {
		mp := &markdownPictures{markdown: out, allocate: step.Output.Allocate, links: map[string]string{}}
		pictures = mp.add
	}
	if err := os.WriteFile(out, []byte(Markdown(doc, pictures)), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

func notBold(inlines []Inline) {
	for _, in := range inlines {
		switch v := in.(type) {
		case *Text:
			v.Format.Bold = false
		case *Link:
			notBold(v.Content)
		}
	}
}

func pdfToDocx(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	doc, err := ReadDocument(ctx, step.InputPath, mediaDir(step), readMode(step.Preset))
	if err != nil {
		return nil, err
	}
	progress.Report(0.6)
	out := step.Output.Allocate("docx", "")
	if out == "" {
		return nil, nil
	}
	if err := WriteDocx(doc, out); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}
