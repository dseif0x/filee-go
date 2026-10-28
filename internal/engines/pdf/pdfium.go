// PDFium (BSD-3) as WebAssembly inside Go (go-pdfium's webassembly mode, run
// by wazero): renders pages and reads their text, paths and pictures. The
// module is compiled once per process (the slow part, see the PR notes) and
// its compiled code cached on disk, so later starts are quicker.

package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	pdfiumerrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// pdfiumWorkers is how many PDFium instances run at once. Each instance is a
// separate WebAssembly module (its own memory), so they don't share state.
var pdfiumWorkers = max(1, min(runtime.NumCPU(), 4))

var (
	poolOnce sync.Once
	poolErr  error
	thePool  pdfium.Pool
	// PdfiumInitTime is how long compiling and starting the module took.
	PdfiumInitTime time.Duration
)

func pdfiumPool() (pdfium.Pool, error) {
	poolOnce.Do(func() {
		start := time.Now()
		rc := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
		if dir, err := os.UserCacheDir(); err == nil {
			if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(dir, "filee", "wazero")); err == nil {
				rc = rc.WithCompilationCache(cache)
			}
		}
		thePool, poolErr = webassembly.Init(webassembly.Config{
			MinIdle: 1, MaxIdle: pdfiumWorkers, MaxTotal: pdfiumWorkers,
			RuntimeConfig: rc,
			// Documents are handed over as bytes: no file system for the module.
			FSConfig:     wazero.NewFSConfig(),
			Stdout:       io.Discard,
			Stderr:       io.Discard,
			ReuseWorkers: true,
		})
		PdfiumInitTime = time.Since(start)
	})
	return thePool, poolErr
}

// PdfiumVersion names the PDFium build.
const PdfiumVersion = "go-pdfium 1.21 (WebAssembly)"

// ErrPasswordProtected is the message every PDF conversion gives for a locked file.
var ErrPasswordProtected = errors.New("The PDF is password-protected. Open it with its password and save a copy without one, then convert that copy.")

// pdfDoc is a document opened in one PDFium instance.
type pdfDoc struct {
	p   pdfium.Pdfium
	doc references.FPDF_DOCUMENT
}

// openPdfium opens a PDF from its bytes.
func openPdfium(ctx context.Context, data []byte) (*pdfDoc, error) {
	pool, err := pdfiumPool()
	if err != nil {
		return nil, fmt.Errorf("PDFium could not start: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	inst, err := pool.GetInstanceWithContext(ctx)
	if err != nil {
		return nil, err
	}
	res, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		inst.Close()
		if errors.Is(err, pdfiumerrors.ErrPassword) || strings.Contains(err.Error(), "password") {
			return nil, ErrPasswordProtected
		}
		return nil, fmt.Errorf("PDFium could not open the PDF: %w", err)
	}
	return &pdfDoc{p: inst, doc: res.Document}, nil
}

func openPdfiumFile(ctx context.Context, path string) (*pdfDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return openPdfium(ctx, data)
}

func (d *pdfDoc) Close() {
	if d == nil || d.p == nil {
		return
	}
	_, _ = d.p.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.doc})
	d.p.Close()
	d.p = nil
}

func (d *pdfDoc) PageCount() (int, error) {
	r, err := d.p.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.doc})
	if err != nil {
		return 0, err
	}
	return r.PageCount, nil
}

// PageSize is the size of a page as it is shown (turned by its /Rotate), in points.
func (d *pdfDoc) PageSize(index int) (float64, float64, error) {
	r, err := d.p.FPDF_GetPageSizeByIndexF(&requests.FPDF_GetPageSizeByIndexF{Document: d.doc, Index: index})
	if err != nil {
		return 0, 0, err
	}
	return float64(r.Size.Width), float64(r.Size.Height), nil
}

// PageSizes of every page.
func (d *pdfDoc) PageSizes() ([][2]float64, error) {
	n, err := d.PageCount()
	if err != nil {
		return nil, err
	}
	out := make([][2]float64, n)
	for i := range out {
		w, h, err := d.PageSize(i)
		if err != nil {
			return nil, err
		}
		out[i] = [2]float64{w, h}
	}
	return out, nil
}

func (d *pdfDoc) page(index int) requests.Page {
	return requests.Page{ByIndex: &requests.PageByIndex{Document: d.doc, Index: index}}
}

// MaxRenderPixels keeps a rendering inside PDFium's 32-bit memory (and ours).
const MaxRenderPixels = 250_000_000

// Render draws a page at dpi with its annotations and form fields on white,
// as PDF viewers show it. The result is the caller's (not PDFium's memory).
func (d *pdfDoc) Render(index, dpi int) (*image.RGBA, error) {
	if w, h, err := d.PageSize(index); err == nil {
		px := w / 72 * float64(dpi) * (h / 72 * float64(dpi))
		if px > MaxRenderPixels {
			dpi = max(1, int(float64(dpi)*math.Sqrt(MaxRenderPixels/px)))
		}
	}
	res, err := d.p.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: d.page(index), DPI: dpi, RenderFlags: enums.FPDF_RENDER_FLAG_ANNOT,
		RenderForm: true, Document: &d.doc,
	})
	if err != nil {
		if errors.Is(err, pdfiumerrors.ErrPassword) {
			return nil, ErrPasswordProtected
		}
		return nil, err
	}
	defer res.Cleanup()
	src := res.Result.RenderedImage
	out := image.NewRGBA(src.Bounds())
	// A page with transparency is drawn on transparent black: flatten on white.
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Over)
	return out, nil
}

// RenderPDF renders the given pages of a PDF file, calling page for each.
func RenderPDF(ctx context.Context, path string, pages []int, dpi int, page func(index int, img *image.RGBA) error) error {
	d, err := openPdfiumFile(ctx, path)
	if err != nil {
		return err
	}
	defer d.Close()
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		img, err := d.Render(p, dpi)
		if err != nil {
			return err
		}
		if err := page(p, img); err != nil {
			return err
		}
	}
	return nil
}
