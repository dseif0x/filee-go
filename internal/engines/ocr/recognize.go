// Package ocr is text recognition: scans and pictures → searchable PDF (the
// page as it looks, the recognized words as an invisible layer on top), with
// Tesseract (an external program, id "tesseract") or Apple Vision on macOS
// (id "vision", through purego, no cgo). The route planner puts this step
// first (Edge.Ocr) and goes on from the PDF to DOCX, TXT, Markdown, HWPX.
//
// A PDF keeps its pages that already have text (born-digital pages are never
// turned into pictures); only pages without any are rendered at 300 dpi and
// recognized, a few at a time.
package ocr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/dseif0x/filee-go/internal/convert"
)

// Dpi is the resolution scans are rendered at, which Tesseract reads best.
const Dpi = 300

// ImageSources are the pictures the OCR engines open themselves; others become a PNG first.
var ImageSources = []string{"png", "jpg", "tiff", "bmp", "webp", "gif"}

func ocrEdges() []convert.Edge {
	var e []convert.Edge
	for _, f := range ImageSources {
		e = append(e, convert.Edge{From: f, To: "pdf", Cost: 10, Ocr: true})
	}
	return append(e, convert.Edge{From: "pdf", To: "pdf", Cost: 10, Ocr: true})
}

// recognizePage reads picture p<n>.png in scratch and writes p<n>.pdf there,
// both names relative to scratch.
type recognizePage func(ctx context.Context, picture, base string) error

// recognizePdf: the pages without text become recognized scans, the others
// are copied. A PDF whose pages all have text is copied as it is, with a note.
func recognizePdf(ctx context.Context, step *convert.Step, scratch, output string, parallel int, recognize recognizePage, progress convert.Progress) error {
	hasText, err := pagesWithText(step.InputPath)
	if err != nil {
		return err
	}
	pages := convert.ParsePageRange(step.Preset.Pdf.PageRange, len(hasText))
	var scans []int
	for _, p := range pages {
		if !hasText[p] {
			scans = append(scans, p)
		}
	}
	if len(scans) == 0 && len(pages) == len(hasText) {
		if step.Notice != nil {
			step.Notice("notice.ocr_has_text")
		}
		return copyFile(step.InputPath, output)
	}

	// Page by page: a page is drawn only when a run is free, and its picture is
	// deleted as soon as its PDF exists, so a long scan never has more than
	// parallel pictures in the work folder.
	doc, err := openPdf(step.InputPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	free := make(chan struct{}, max(1, parallel))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	done := 0
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}
	for _, page := range scans {
		select {
		case free <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		picture := filepath.Join(scratch, fmt.Sprintf("p%d.png", page))
		img, cleanup, err := doc.render(page, Dpi)
		if err == nil {
			err = writePng(img, picture)
			cleanup()
		}
		if err != nil {
			<-free
			fail(err)
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-free }()
			if err := recognize(ctx, filepath.Base(picture), fmt.Sprintf("p%d", page)); err != nil {
				fail(err)
				return
			}
			os.Remove(picture)
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			progress.Report(0.05 + 0.9*float64(n)/float64(len(scans)))
		}()
	}
	wg.Wait()
	doc.Close()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A new document of the pages in order; bookmarks and form fields don't come along.
	return assemble(ctx, step.InputPath, pages, func(p int) bool { return hasText[p] },
		func(p int) string { return filepath.Join(scratch, fmt.Sprintf("p%d.pdf", p)) }, scratch, output)
}

func newScratch(workDir, prefix string) (string, error) {
	s := filepath.Join(workDir, prefix+randomName())
	return s, os.MkdirAll(s, 0o755)
}

var errNoOutput = errors.New("no output")

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}
