// Package vector is the built-in vector graphics engine: SVG/SVGZ → PDF (vector)
// and → raster images, SVG ↔ SVGZ, SVG → DXF, raster images → SVG (tracing),
// Illustrator files → PDF, DST embroidery and HP-GL plots → SVG.
package vector

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/ghostscript"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
	"github.com/dseif0x/filee-go/internal/engines/vector/imgout"
	"github.com/dseif0x/filee-go/internal/engines/vector/svg"
	"github.com/dseif0x/filee-go/internal/engines/vector/trace"
	"github.com/dseif0x/filee-go/internal/formats"
	"github.com/dseif0x/filee-go/internal/presets"
)

// TraceCost is the cost of tracing a raster image into SVG. Tracing redraws
// the picture, so it must never be a step on the way to another format (PNG →
// SVG → PDF instead of PNG → PDF): its cost is higher than any route from an
// image to a format SVG leads to, and the planner only takes it when SVG (or
// DXF, DWG through SVG) is the target. Its edges are for raster sources only.
const TraceCost = 40

const (
	// CssDpi: SVG user units are CSS pixels.
	CssDpi = 96.0
	// MinimumLongEdge: small drawings (icons) are rendered at least this large.
	MinimumLongEdge = 1024
	maximumLongEdge = 16384
	maximumPixels   = 100_000_000
)

// Converter is the vector engine.
type Converter struct{}

// New returns the engine.
func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "vector" }
func (*Converter) Name() string        { return "Vector graphics (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Transforms: only when rendering to an image (not to PDF or between SVG and SVGZ).
func (*Converter) Transforms(from, to string) bool { return slices.Contains(imgout.Writable, to) }

func (*Converter) Edges() []convert.Edge {
	e := []convert.Edge{
		convert.E("svg", "pdf"),
		convert.E("svgz", "pdf"),
		convert.E("svg", "svgz"),
		convert.E("svgz", "svg"),
	}
	for _, t := range imgout.Writable {
		e = append(e, imgout.Edge("svg", t), imgout.Edge("svgz", t))
	}
	e = append(e,
		convert.E("svg", "dxf"),
		convert.E("svgz", "dxf"),
		// Illustrator files are PDF inside (or PostScript, see illustrator).
		convert.E("ai", "pdf"),
		// Embroidery designs: their stitches as lines.
		convert.E("dst", "svg"),
		// HP-GL plot files: their pen moves as lines.
		convert.E("plt", "svg"),
	)
	// Raster images are traced (never vector files or documents).
	for _, s := range imgout.Readable {
		e = append(e, convert.Edge{From: s, To: "svg", Cost: TraceCost, ImageSourcesOnly: true})
	}
	return e
}

func (*Converter) Status() convert.Status {
	return convert.Available("SVG/SVGZ, tracing, DST, HP-GL, AI", "built-in")
}

func (c *Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	var out []string
	var err error
	switch {
	case step.From == "ai" && step.To == "pdf":
		out, err = illustrator(ctx, step, progress)
	case step.From == "svg" && step.To == "svgz":
		out, err = compress(step)
	case step.From == "svgz" && step.To == "svg":
		out, err = decompress(step)
	case step.From == "dst" && step.To == "svg":
		out, err = textToSVG(step, DstToSVG)
	case step.From == "plt" && step.To == "svg":
		out, err = textToSVG(step, HpglToSVG)
	case step.To == "svg":
		out, err = traceRaster(ctx, step, progress)
	case step.To == "dxf":
		out, err = toDxf(ctx, step)
	case step.To == "pdf":
		out, err = toPdf(ctx, step)
	default:
		out, err = toRaster(ctx, step, progress)
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return out, nil
}

func textToSVG(step *convert.Step, fn func([]byte) (string, error)) ([]string, error) {
	data, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	text, err := fn(data)
	if err != nil {
		return nil, err
	}
	path := step.Output.Allocate("svg", "")
	if path == "" {
		return nil, nil
	}
	return []string{path}, os.WriteFile(path, []byte(text), 0o644)
}

// load reads an SVG or SVGZ drawing.
func load(path string) (*svg.Document, error) {
	data, err := svg.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := svg.Parse(data, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("the SVG file could not be read: %w", err)
	}
	if !(doc.Width > 0) || !(doc.Height > 0) || math.IsInf(doc.Width, 0) || math.IsInf(doc.Height, 0) {
		return nil, errors.New("the SVG file has no drawable content or no size")
	}
	return doc, nil
}

func toPdf(ctx context.Context, step *convert.Step) ([]string, error) {
	doc, err := load(step.InputPath)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	path := step.Output.Allocate("pdf", "")
	if path == "" {
		return nil, nil
	}
	if err := WritePdf(doc, path, formats.NameWithoutExtension(step.InputPath), step.Preset.Document.PdfA); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

// WritePdf writes a one-page PDF the size of the drawing. Paths, gradients and
// text stay vector (text as glyph outlines); only effects PDF can't express
// (filters such as blur) are rasterized at 300 dpi.
func WritePdf(doc *svg.Document, output, title string, pdfA bool) error {
	const pointsPerPixel = 72 / CssDpi
	pdf := gfx.NewPDF()
	page := pdf.Page(doc.Width*pointsPerPixel, doc.Height*pointsPerPixel)
	doc.Draw(page, gfx.Scale(pointsPerPixel, pointsPerPixel), svg.Options{FilterResolution: 300.0 / 72})
	page.Finish()
	var b bytes.Buffer
	if err := pdf.Write(&b, gfx.PDFInfo{Title: title, Creator: "Filee", Producer: "Filee", Created: time.Now(), PdfA: pdfA}); err != nil {
		return err
	}
	return os.WriteFile(output, b.Bytes(), 0o644)
}

// RasterScale is the scale from CSS pixels to output pixels: drawings are
// rendered at the size they end up at — the preset's render DPI, at least
// MinimumLongEdge on the long edge, or exactly at the preset's resize target.
func RasterScale(width, height float64, image *presets.ImageOptions, pdf *presets.PdfOptions) float64 {
	longEdge := math.Max(width, height)
	dpi := float64(max(36, min(1200, pdf.RenderDpi)))
	scale := math.Max(dpi/CssDpi, MinimumLongEdge/longEdge)
	if c := image.CustomSize; c != nil && c.Width > 0 && c.Height > 0 {
		// Render at the size the custom resize asks for: with the lock at the
		// fit-inside scale, without at the larger side's scale, so the stretch
		// that follows only ever shrinks the other side.
		cw, ch := float64(c.ClampedWidth()), float64(c.ClampedHeight())
		if c.LockAspect {
			scale = math.Min(cw/width, ch/height)
		} else {
			scale = math.Max(cw/width, ch/height)
		}
	} else if m := image.ClampedMinShortEdge(); m > 0 {
		// Drawn at the size that meets the minimum instead of enlarging a
		// default-size bitmap afterwards (issue #652).
		scale = math.Max(scale, float64(m)/math.Min(width, height))
	} else {
		switch image.Resize {
		case presets.ResizeLongEdge:
			if image.LongEdge > 0 {
				scale = float64(image.LongEdge) / longEdge
			}
		case presets.ResizePercent:
			if image.ResizePercent > 0 && image.ResizePercent != 100 {
				scale *= float64(image.ResizePercent) / 100
			}
		}
	}
	limit := math.Min(maximumLongEdge/longEdge, math.Sqrt(maximumPixels/(width*height)))
	return math.Max(math.Min(scale, limit), 1e-3)
}

// Rasterize renders the drawing on a transparent bitmap.
func Rasterize(doc *svg.Document, scale float64) *gfx.Raster {
	w := max(1, int(math.Ceil(doc.Width*scale)))
	h := max(1, int(math.Ceil(doc.Height*scale)))
	r := gfx.NewRaster(w, h)
	doc.Draw(r, gfx.Scale(scale, scale), svg.Options{FilterResolution: 1})
	return r
}

func toRaster(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	doc, err := load(step.InputPath)
	if err != nil {
		return nil, err
	}
	progress.Report(0.2)
	scale := RasterScale(doc.Width, doc.Height, &step.Preset.Image, &step.Preset.Pdf)
	r := Rasterize(doc, scale)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	progress.Report(0.6)
	// The drawing was rendered at its final size, so the preset's resize must
	// not scale the bitmap again (CustomSize stays: the exact stretch of the render).
	return imgout.Write(r.Straight(), CssDpi*scale, step.To, step.Output, &step.Preset.Image, false)
}

func compress(step *convert.Step) ([]string, error) {
	data, err := os.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	gz := svg.IsGzip(data)
	if !gz {
		if err := requireSVGText(data); err != nil {
			return nil, err
		}
	}
	path := step.Output.Allocate("svgz", "")
	if path == "" {
		return nil, nil
	}
	if gz {
		// A gzipped file named .svg: already SVGZ.
		return []string{path}, os.WriteFile(path, data, 0o644)
	}
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	zw.Write(data)
	zw.Close()
	return []string{path}, os.WriteFile(path, b.Bytes(), 0o644)
}

func decompress(step *convert.Step) ([]string, error) {
	data, err := svg.ReadFile(step.InputPath)
	if err != nil {
		return nil, err
	}
	if err := requireSVGText(data); err != nil {
		return nil, err
	}
	path := step.Output.Allocate("svg", "")
	if path == "" {
		return nil, nil
	}
	return []string{path}, os.WriteFile(path, data, 0o644)
}

// requireSVGText guards SVG ↔ SVGZ, which never parse the drawing, against renamed non-SVG files.
func requireSVGText(data []byte) error {
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !strings.Contains(strings.ToLower(string(head)), "<svg") {
		return errors.New("the file is not an SVG drawing (no <svg> element)")
	}
	return nil
}

// ---- Illustrator ----

type aiKind int

const (
	aiUnknown aiKind = iota
	aiPdf
	aiPostScript
)

// illustratorKind looks at the header: PDF-compatible files have "%PDF-" in
// the first 1 KB; PostScript-based ones start with "%!PS" or the binary
// header of a DOS EPS file (C5 D0 D3 C6).
func illustratorKind(path string) (aiKind, error) {
	f, err := os.Open(path)
	if err != nil {
		return aiUnknown, err
	}
	defer f.Close()
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	head := buf[:n]
	switch {
	case bytes.Contains(head, []byte("%PDF-")):
		return aiPdf, nil
	case bytes.HasPrefix(head, []byte("%!PS")), bytes.HasPrefix(head, []byte{0xC5, 0xD0, 0xD3, 0xC6}):
		return aiPostScript, nil
	}
	return aiUnknown, nil
}

// illustrator: Illustrator 9 and later save a complete PDF inside the .ai file
// ("Create PDF compatible file", on by default), so the file is copied as a
// PDF. Files saved without it (and Illustrator 8 and older) are PostScript,
// which needs Ghostscript.
func illustrator(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	kind, err := illustratorKind(step.InputPath)
	if err != nil {
		return nil, err
	}
	if kind == aiUnknown {
		return nil, errors.New("the file is not an Adobe Illustrator document (neither PDF nor PostScript inside)")
	}
	path := step.Output.Allocate("pdf", "")
	if path == "" {
		return nil, nil
	}
	if kind == aiPdf {
		data, err := os.ReadFile(step.InputPath)
		if err != nil {
			return nil, err
		}
		return []string{path}, os.WriteFile(path, data, 0o644)
	}
	gs := ghostscript.Locate()
	if gs == "" {
		return nil, errors.New("this Illustrator file was saved without PDF compatibility, so it is PostScript. " +
			"Install Ghostscript in Settings → Engines to convert it, or save it in Illustrator with \"Create PDF compatible file\"")
	}
	if err := ghostscript.ConvertToPdf(ctx, gs, step.InputPath, path, true, step.WorkDir, progress); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

// ---- tracing ----

// traceRaster reads the picture (first frame, upright), reduces very large
// pictures, traces them and writes the SVG at the picture's own pixel size.
func traceRaster(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	img, err := imgout.Read(step.InputPath, step.From)
	if err != nil {
		return nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w == 0 || h == 0 {
		return nil, errors.New("the image has no pictures to trace")
	}
	res, err := trace.Picture(ctx, img, step.Preset.Trace, func(p float64) { progress.Report(p) })
	if err != nil {
		return nil, err
	}
	path := step.Output.Allocate("svg", "")
	if path == "" {
		return nil, nil
	}
	comment := ""
	if res.Width != w || res.Height != h {
		comment = fmt.Sprintf("Traced by Filee from a %d x %d px image reduced to %d x %d px.", w, h, res.Width, res.Height)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	err = trace.WriteSVG(res, f, w, h, comment)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return []string{path}, nil
}
