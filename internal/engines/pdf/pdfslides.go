// PDF → PPTX: one slide per page showing the page as a picture, so the slides
// look exactly like the PDF (not editable). Pages are rendered with PDFium at
// the preset's resolution (150–600 dpi; a page over 25 megapixels is rendered
// smaller) and saved as JPEG at the preset's quality; the text of each page
// goes into the slide's speaker notes. The slide size is the first page's
// own size; every page is fitted onto it, centred on white.

package pdf

import (
	"context"
	"fmt"
	"math"

	"github.com/dseif0x/filee-go/internal/convert"
)

const (
	slidesMinDpi   = 150
	slidesMaxDpi   = 600
	maxSlidePixels = 25_000_000
)

// PdfSlides makes picture slides of a PDF's pages.
type PdfSlides struct{}

func (PdfSlides) ID() string   { return "pdf-slides" }
func (PdfSlides) Name() string { return "PDF slides (built-in)" }

// MaxParallelism: one deck at a time keeps memory predictable.
func (PdfSlides) MaxParallelism() int { return 1 }

func (PdfSlides) Edges() []convert.Edge {
	return []convert.Edge{{From: "pdf", To: "pptx", Cost: 10, DirectOnly: true}}
}

func (PdfSlides) Status() convert.Status { return convert.Available("PDF → PPTX", PdfiumVersion) }

// slidePageDpi lowers dpi for a page that would exceed maxSlidePixels.
func slidePageDpi(width, height float64, dpi int) int {
	px := width / 72 * float64(dpi) * (height / 72 * float64(dpi))
	if px <= maxSlidePixels || px <= 0 {
		return dpi
	}
	return max(1, int(math.Floor(float64(dpi)*math.Sqrt(maxSlidePixels/px))))
}

func (PdfSlides) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	d, err := openPdfiumFile(ctx, step.InputPath)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	sizes, err := d.PageSizes()
	if err != nil {
		return nil, err
	}
	pages := convert.ParsePageRange(step.Preset.Pdf.PageRange, len(sizes))
	if len(pages) == 0 {
		return nil, fmt.Errorf("The PDF has no pages.")
	}
	out := step.Output.Allocate("pptx", "")
	if out == "" {
		return nil, nil
	}
	// The notes are an extra that must not make the conversion fail.
	notes, err := PageTexts(ctx, step.InputPath, pages)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		notes = nil
	}
	progress.Report(0.1)
	dpi := max(slidesMinDpi, min(step.Preset.Pdf.RenderDpi, slidesMaxDpi))
	quality := max(1, min(step.Preset.Image.Quality, 100))
	var deck *pictureDeck
	defer func() {
		if deck != nil {
			deck.Close()
		}
	}()
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		w, h := sizes[page][0], sizes[page][1]
		img, err := d.Render(page, slidePageDpi(w, h, dpi))
		if err != nil {
			return nil, err
		}
		// The page as it is shown: when the rendering is turned against the size, swap.
		b := img.Bounds()
		if w != h && b.Dx() != b.Dy() && (w > h) != (b.Dx() > b.Dy()) {
			w, h = h, w
		}
		if deck == nil {
			sw, sh := slideSize(w, h)
			if deck, err = newPictureDeck(out, sw, sh); err != nil {
				return nil, err
			}
		}
		note := ""
		if notes != nil {
			note = notes[i]
		}
		deck.addSlide(encodeJpegBytes(img, quality), w, h, fmt.Sprintf("Page %d", page+1), note)
		progress.Report(0.1 + 0.85*float64(i+1)/float64(len(pages)))
	}
	if err := deck.Save(); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}
