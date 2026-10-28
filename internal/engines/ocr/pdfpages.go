package ocr

// PDF pages through PDFium (WebAssembly, run by wazero): which pages have
// text of their own, and the scans rendered as pictures. The recognized
// pages and the ones that kept their text are put together with pdfcpu.

import (
	"context"
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/dseif0x/filee-go/internal/convert"
)

// StampCoverage is the share of a page visible text may cover on a scan and still be only a stamp.
const StampCoverage = 0.01

var pdfiumPool = sync.OnceValues(func() (pdfium.Pool, error) {
	return webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 2, MaxTotal: 4})
})

// pdfDocument is an open PDF in one PDFium instance (not safe for concurrent use).
type pdfDocument struct {
	inst pdfium.Pdfium
	doc  references.FPDF_DOCUMENT
}

func openPdf(path string) (*pdfDocument, error) {
	pool, err := pdfiumPool()
	if err != nil {
		return nil, fmt.Errorf("PDFium: %w", err)
	}
	inst, err := pool.GetInstance(time.Minute)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		inst.Close()
		return nil, err
	}
	d, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		inst.Close()
		if strings.Contains(strings.ToLower(err.Error()), "password") {
			return nil, convert.Refuse("error.pdf_password", "")
		}
		return nil, err
	}
	return &pdfDocument{inst, d.Document}, nil
}

func (d *pdfDocument) Close() {
	d.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.doc})
	d.inst.Close()
}

func (d *pdfDocument) pageCount() (int, error) {
	r, err := d.inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.doc})
	if err != nil {
		return 0, err
	}
	return r.PageCount, nil
}

func (d *pdfDocument) page(i int) requests.Page {
	return requests.Page{ByIndex: &requests.PageByIndex{Document: d.doc, Index: i}}
}

// hasText: the page has text of its own. An OCR layer (invisible text) always
// counts. A scan with a stamp does not: a picture over most of the page whose
// only text is a little visible text (a page number the scanner added).
func (d *pdfDocument) hasText(i int) (bool, error) {
	p := d.page(i)
	tp, err := d.inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: p})
	if err != nil {
		return false, err
	}
	defer d.inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage})
	n, err := d.inst.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: tp.TextPage})
	if err != nil {
		return false, err
	}
	letters := 0
	area := 0.0
	for k := 0; k < n.Count; k++ {
		u, err := d.inst.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{TextPage: tp.TextPage, Index: k})
		if err != nil || u.Unicode == 0 || strings.TrimSpace(string(rune(u.Unicode))) == "" {
			continue
		}
		letters++
		if o, err := d.inst.FPDFText_GetTextObject(&requests.FPDFText_GetTextObject{TextPage: tp.TextPage, Index: k}); err == nil && o.TextObject != "" {
			if m, err := d.inst.FPDFTextObj_GetTextRenderMode(&requests.FPDFTextObj_GetTextRenderMode{PageObject: o.TextObject}); err == nil &&
				(m.TextRenderMode == enums.FPDF_TEXTRENDERMODE_INVISIBLE || m.TextRenderMode == enums.FPDF_TEXTRENDERMODE_CLIP) {
				return true, nil
			}
		}
		if b, err := d.inst.FPDFText_GetCharBox(&requests.FPDFText_GetCharBox{TextPage: tp.TextPage, Index: k}); err == nil {
			area += max(0, b.Right-b.Left) * max(0, b.Top-b.Bottom)
		}
	}
	if letters == 0 {
		return false, nil
	}
	w, _ := d.inst.FPDF_GetPageWidthF(&requests.FPDF_GetPageWidthF{Page: p})
	h, _ := d.inst.FPDF_GetPageHeightF(&requests.FPDF_GetPageHeightF{Page: p})
	pageArea := 0.0
	if w != nil && h != nil {
		pageArea = float64(w.PageWidth) * float64(h.PageHeight)
	}
	if pageArea <= 0 || area >= StampCoverage*pageArea {
		return true, nil
	}
	// A page drawn with little text and no picture under it (a cover, a short note) is not a scan.
	return !d.hasLargePicture(p, pageArea), nil
}

func (d *pdfDocument) hasLargePicture(p requests.Page, pageArea float64) bool {
	n, err := d.inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: p})
	if err != nil {
		return false
	}
	for k := 0; k < n.Count; k++ {
		o, err := d.inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: p, Index: k})
		if err != nil {
			continue
		}
		t, err := d.inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: o.PageObject})
		if err != nil || t.Type != enums.FPDF_PAGEOBJ_IMAGE {
			continue
		}
		b, err := d.inst.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: o.PageObject})
		if err == nil && float64(b.Right-b.Left)*float64(b.Top-b.Bottom) >= pageArea/2 {
			return true
		}
	}
	return false
}

// pagesWithText tells for each page whether it has text of its own.
func pagesWithText(path string) ([]bool, error) {
	d, err := openPdf(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	n, err := d.pageCount()
	if err != nil {
		return nil, err
	}
	out := make([]bool, n)
	for i := range out {
		if out[i], err = d.hasText(i); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// render draws page i at dpi on white, with annotations and form fields.
func (d *pdfDocument) render(i, dpi int) (image.Image, func(), error) {
	r, err := d.inst.RenderPageInDPI(&requests.RenderPageInDPI{Page: d.page(i), DPI: dpi, RenderFlags: enums.FPDF_RENDER_FLAG_ANNOT,
		RenderForm: true, Document: &d.doc})
	if err != nil {
		return nil, nil, err
	}
	return r.Result.Image, r.Cleanup, nil
}

// assemble writes output from the pages in order: page p from source when
// keep(p), else the one-page PDF recognized(p).
func assemble(ctx context.Context, source string, pages []int, keep func(int) bool, recognized func(int) string, scratch, output string) error {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	var parts []string
	for i := 0; i < len(pages); {
		if !keep(pages[i]) {
			parts = append(parts, recognized(pages[i]))
			i++
			continue
		}
		// A run of pages that keep their text, taken from the source in one go.
		var sel []string
		for ; i < len(pages) && keep(pages[i]); i++ {
			sel = append(sel, fmt.Sprint(pages[i]+1))
		}
		part := fmt.Sprintf("%s/kept-%d.pdf", scratch, len(parts))
		if err := api.TrimFile(ctx, source, part, []string{strings.Join(sel, ",")}, conf); err != nil {
			return err
		}
		parts = append(parts, part)
	}
	if len(parts) == 1 {
		return copyFile(parts[0], output)
	}
	return api.MergeCreateFile(ctx, parts, output, false, conf)
}
