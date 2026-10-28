package vector

import (
	"bytes"
	"context"
	"math"
	"os"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/imgout"
	"github.com/dseif0x/filee-go/internal/engines/vector/metafile"
	"github.com/dseif0x/filee-go/internal/formats"
)

// Windows metafiles (EMF / WMF) are drawn by Filee itself. In the original,
// ImageMagick rendered them through GDI+ on Windows only, and LibreOffice
// (when installed) converted them to PDF and SVG; here they convert on every
// system, and PDF and SVG output stays vector.

// minimumMetafileDpi: metafiles are rendered at least this sharp, as ImageMagick did.
const minimumMetafileDpi = 150

var metafileFormats = []string{"emf", "wmf"}

func metafileEdges() []convert.Edge {
	var e []convert.Edge
	for _, s := range metafileFormats {
		e = append(e, convert.E(s, "pdf"), convert.E(s, "svg"))
		for _, t := range imgout.Writable {
			e = append(e, imgout.Edge(s, t))
		}
	}
	return e
}

func isMetafile(f string) bool { return f == "emf" || f == "wmf" }

func convertMetafile(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	data, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	pic, err := metafile.Parse(data)
	if err != nil {
		return nil, err
	}
	progress.Report(0.2)
	switch step.To {
	case "pdf":
		path := step.Output.Allocate("pdf", "")
		if path == "" {
			return nil, nil
		}
		const k = 72 / CssDpi
		doc := gfx.NewPDF()
		page := doc.Page(pic.Width*k, pic.Height*k)
		pic.Draw(page, gfx.Scale(k, k), nil)
		page.Finish()
		var b bytes.Buffer
		if err := doc.Write(&b, gfx.PDFInfo{Title: formats.NameWithoutExtension(step.InputPath), Creator: "Filee", Producer: "Filee", Created: time.Now(), PdfA: step.Preset.Document.PdfA}); err != nil {
			return nil, err
		}
		return []string{path}, os.WriteFile(path, b.Bytes(), 0o644)
	case "svg":
		path := step.Output.Allocate("svg", "")
		if path == "" {
			return nil, nil
		}
		s := gfx.NewSVGWriter(pic.Width, pic.Height)
		pic.Draw(s, gfx.Identity, nil)
		var b bytes.Buffer
		if err := s.Write(&b); err != nil {
			return nil, err
		}
		return []string{path}, os.WriteFile(path, b.Bytes(), 0o644)
	}
	dpi := math.Max(minimumMetafileDpi, float64(max(36, min(1200, step.Preset.Pdf.RenderDpi))))
	scale := dpi / CssDpi
	// The same memory limits as SVG rendering.
	if limit := math.Min(maximumLongEdge/math.Max(pic.Width, pic.Height), math.Sqrt(maximumPixels/(pic.Width*pic.Height))); scale > limit {
		scale = limit
		dpi = scale * CssDpi
	}
	w := max(1, int(math.Ceil(pic.Width*scale)))
	h := max(1, int(math.Ceil(pic.Height*scale)))
	r := gfx.NewRaster(w, h)
	pic.Draw(r, gfx.Scale(scale, scale), nil)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	progress.Report(0.7)
	return imgout.Write(r.Straight(), dpi, step.To, step.Output, &step.Preset.Image, true)
}
