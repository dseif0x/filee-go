// Package ebook is the built-in "ebook" engine: EPUB, MOBI / AZW / AZW3 /
// PRC, FB2, HTMLZ and TXTZ are read into the document model and written as
// EPUB, TXT, HTML, Markdown, FB2, HTMLZ or TXTZ (and HWPX when the HWPX
// writer is linked in); HTML, Markdown, TXT and DOCX become e-books too.
// Comics (CBZ / CBR / CB7 / CBT / CBC) become PDF, fixed-layout EPUB or CBZ,
// PDF becomes CBZ, and AZW4 (Print Replica) gives back its PDF. EPUB ↔ KEPUB
// (Kobo) adds or removes Kobo's sentence markup and keeps the rest of the
// book as it is. Formats only calibre reads or writes are the calibre engine's.
package ebook

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/engines/markdown"
	"github.com/dseif0x/filee-go/internal/formats"
)

// bookReaders are the e-book formats read into the document model.
var bookReaders = []string{"epub", "kepub", "mobi", "azw3", "azw", "prc", "fb2", "htmlz", "txtz"}

// bookWriters are the formats written from the document model ("hwpx" when HwpxWriter is set).
var bookWriters = []string{"epub", "hwpx", "txt", "html", "md", "fb2", "htmlz", "txtz"}

// documentReaders are documents that become e-books (their other conversions belong to the document engines).
var documentReaders = []string{"html", "md", "txt", "docx"}

var ebookWriters = []string{"epub", "fb2", "htmlz", "txtz"}

var comics = []string{"cbz", "cbr", "cb7", "cbt", "cbc"}

// HwpxWriter writes the document model as HWPX; the HWPX engine sets it.
// Without it the e-book → HWPX edges are left out (the HWPX writer engine
// reads e-books through the document readers registered here as well).
var HwpxWriter func(d *doc.Document, output string) error

// Converter is the e-book engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "ebook" }
func (*Converter) Name() string        { return "E-books (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range bookReaders {
		for _, to := range bookWriters {
			// KEPUB → EPUB is below: the book is copied without Kobo's markup instead of being written anew.
			if to == from || from == "kepub" && to == "epub" || to == "hwpx" && HwpxWriter == nil {
				continue
			}
			cost := 10
			if to == "md" {
				cost = 12
			}
			e = append(e, convert.EC(from, to, cost))
		}
	}
	// Every other format reaches KEPUB through EPUB.
	e = append(e, convert.E("epub", "kepub"), convert.E("kepub", "epub"))
	for _, from := range documentReaders {
		if from == "docx" && !doc.HasReader("docx") {
			continue // DOCX is read by the DOCX reader of the document engines when it is linked in
		}
		for _, to := range ebookWriters {
			e = append(e, convert.E(from, to))
		}
	}
	// HTML → TXT has no other built-in route; HTML → Markdown is a fallback for when Pandoc is not installed.
	e = append(e, convert.E("html", "txt"), convert.EC("html", "md", 14))
	for _, from := range comics {
		e = append(e, convert.E(from, "pdf"), convert.E(from, "epub"))
		if from != "cbz" {
			e = append(e, convert.E(from, "cbz"))
		}
	}
	// Pages as pictures lose the text: other routes to EPUB (through DOCX, TXT, …) should win when they exist.
	return append(e, convert.EC("pdf", "cbz", 15), convert.E("azw4", "pdf"))
}

func (*Converter) Status() convert.Status {
	return convert.Available("EPUB, KEPUB, MOBI / AZW3, FB2, HTMLZ, TXTZ, CBZ / CBR / CB7 / CBT / CBC, AZW4", "built-in")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.05)
	work, err := newFolder(step.WorkDir, "ebook")
	if err != nil {
		return nil, err
	}
	if slices.Contains(comics, step.From) || step.From == "pdf" {
		return comic(ctx, step, work, progress)
	}
	if step.From == "azw4" {
		data, err := printReplicaPdf(step.InputPath)
		if err != nil {
			return nil, err
		}
		target := step.Output.Allocate("pdf", "")
		if target == "" {
			return nil, nil
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return nil, err
		}
		progress.Report(1)
		return []string{target}, nil
	}
	if step.To == "kepub" || step.From == "kepub" && step.To == "epub" {
		target := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
		if target == "" {
			return nil, nil
		}
		if step.To == "kepub" {
			err = kepubFromEpub(ctx, step.InputPath, target)
		} else {
			err = kepubToEpub(ctx, step.InputPath, target)
		}
		if err != nil {
			return nil, err
		}
		progress.Report(1)
		return []string{target}, nil
	}
	book, err := readBook(ctx, step.InputPath, step.From, work)
	if err != nil {
		return nil, err
	}
	progress.Report(0.5)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	if err := writeBook(book, step, output, work); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

func readBook(ctx context.Context, input, from, work string) (*Book, error) {
	media := filepath.Join(work, "media")
	switch from {
	case "epub", "kepub":
		return readEpubBook(ctx, input, work)
	case "mobi", "azw3", "azw", "prc":
		return readMobiBook(ctx, input, work)
	case "fb2":
		return readFb2Book(input, work)
	case "htmlz":
		return readHtmlzBook(ctx, input, work)
	case "txtz":
		return readTxtzBook(input, work)
	case "html":
		d, err := htmldoc.Read(input, media)
		if err != nil {
			return nil, err
		}
		return &Book{d, &Metadata{Title: d.Title}}, nil
	case "md":
		b, err := os.ReadFile(input)
		if err != nil {
			return nil, err
		}
		abs, _ := filepath.Abs(input)
		d := markdown.Read(doc.DecodeText(b), filepath.Dir(abs), "")
		return &Book{d, &Metadata{Title: d.Title}}, nil
	case "txt":
		b, err := os.ReadFile(input)
		if err != nil {
			return nil, err
		}
		return &Book{textToDocument(doc.DecodeText(b)), &Metadata{}}, nil
	case "docx":
		d, err := doc.Read(input, "docx", media)
		if err != nil {
			return nil, err
		}
		return &Book{d, &Metadata{Title: d.Title}}, nil
	}
	return nil, fmt.Errorf("%s → e-book is not supported", from)
}

func newline() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}

func writeBook(b *Book, step *convert.Step, output, work string) error {
	switch step.To {
	case "epub":
		return writeEpub(b, output, step.InputPath, work)
	case "hwpx":
		if HwpxWriter == nil {
			return errors.New("the HWPX writer is not available")
		}
		b.Doc.Title = b.DisplayTitle(step.InputPath)
		return HwpxWriter(b.Doc, output)
	case "txt":
		return os.WriteFile(output, append([]byte{0xEF, 0xBB, 0xBF}, htmldoc.PlainText(b.Doc, newline())...), 0o644)
	case "html":
		// One self-contained page: pictures are embedded as data: URIs.
		images, err := newFolder(work, "html-images")
		if err != nil {
			return err
		}
		page := htmldoc.WriteXhtml(b.Doc, htmldoc.XhtmlOptions{ImageSource: func(p string) string { return dataURI(p, images) }})[0]
		return os.WriteFile(output, []byte(htmldoc.Page(b.DisplayTitle(step.InputPath), page.Body, b.DisplayLanguage(), "", false, "")), 0o644)
	case "md":
		pictures := func(string) string { return "" }
		if step.Preset == nil || !step.Preset.Document.LeaveOutPictures {
			pictures = markdown.NewPictures(output, step.Output).Add
		}
		return os.WriteFile(output, []byte(htmldoc.Markdown(b.Doc, pictures)), 0o644)
	case "fb2":
		return writeFb2(b, output, step.InputPath, work)
	case "htmlz":
		return writeHtmlz(b, output, step.InputPath, work)
	case "txtz":
		return writeTxtz(b, output, step.InputPath, work)
	}
	return fmt.Errorf("%s → %s is not supported", step.From, step.To)
}

func dataURI(path, convertFolder string) string {
	file, mt, ok := commonImage(path, convertFolder, true)
	if !ok {
		return ""
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func comic(ctx context.Context, step *convert.Step, work string, progress convert.Progress) ([]string, error) {
	var pages []string
	var err error
	if step.From == "pdf" {
		pages, err = renderPdfPages(ctx, step.InputPath, step, work, progress)
	} else {
		pages, err = comicPages(ctx, step.InputPath, step.From, work)
	}
	if err != nil {
		return nil, err
	}
	progress.Report(0.4)
	output := step.Output.Allocate(formats.ByID(step.To).PrimaryExtension(), "")
	if output == "" {
		return nil, nil
	}
	switch step.To {
	case "pdf":
		err = comicToPdf(ctx, pages, output, work, progress)
	case "epub":
		err = writeFixedLayoutEpub(ctx, formats.NameWithoutExtension(step.InputPath), pages, output, work)
	case "cbz":
		err = comicToCbz(ctx, pages, output)
	default:
		err = fmt.Errorf("%s → %s is not supported", step.From, step.To)
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{output}, nil
}

// The e-book readers are document readers too: the DOCX and HWPX writers read them directly.
func init() {
	read := func(f func(ctx context.Context, path, work string) (*Book, error)) doc.ReadFunc {
		return func(path, media string) (*doc.Document, error) {
			b, err := f(context.Background(), path, media)
			if err != nil {
				return nil, err
			}
			return b.Doc, nil
		}
	}
	plain := func(f func(path, work string) (*Book, error)) func(ctx context.Context, path, work string) (*Book, error) {
		return func(_ context.Context, path, work string) (*Book, error) { return f(path, work) }
	}
	for _, f := range []string{"epub", "kepub"} {
		doc.RegisterReader(f, read(readEpubBook))
	}
	for _, f := range []string{"mobi", "azw3", "azw", "prc"} {
		doc.RegisterReader(f, read(readMobiBook))
	}
	doc.RegisterReader("fb2", read(plain(readFb2Book)))
	doc.RegisterReader("htmlz", read(readHtmlzBook))
	doc.RegisterReader("txtz", read(plain(readTxtzBook)))
}
