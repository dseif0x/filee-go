// Builds PDFs from pictures, splits / extracts / turns PDF pages, merges PDFs,
// makes PDFs smaller (PdfCompressor) and removes their metadata. It also
// finishes every PDF a job writes with a watermark, page numbers or a
// password (finishing.go). The id stays "pdfsharp", the C# engine's library.

package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/ghostscript"
	"github.com/dseif0x/filee-go/internal/presets"
)

const (
	pointsPerInch      = 72.0
	millimetresPerInch = 25.4
)

// PdfSharp is pictures → PDF and PDF → PDF.
type PdfSharp struct{}

var (
	_ convert.PdfMerger        = PdfSharp{}
	_ convert.PdfFinisher      = PdfSharp{}
	_ convert.ImageTransformer = PdfSharp{}
)

func (PdfSharp) ID() string          { return "pdfsharp" }
func (PdfSharp) Name() string        { return "PDF tools (pdfcpu)" }
func (PdfSharp) MaxParallelism() int { return 0 }

func (PdfSharp) Edges() []convert.Edge {
	edges := convert.Fan(imageSources, []string{"pdf"}, 10)
	return append(edges, convert.E("pdf", "pdf"))
}

func (PdfSharp) Status() convert.Status { return convert.Available("", PdfcpuVersion) }

// Transforms: pictures into a PDF are turned first; a PDF to a PDF is copied page by page, as is.
func (PdfSharp) Transforms(from, to string) bool { return from != "pdf" }

func (PdfSharp) MergePdfs(ctx context.Context, inputs []string, output string) error {
	return MergePdfs(ctx, inputs, output)
}

func (PdfSharp) FinishPdf(ctx context.Context, path string, options *presets.PdfOptions) error {
	return FinishPdf(ctx, path, options)
}

func (PdfSharp) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	o := &step.Preset.Pdf
	switch {
	case step.From != "pdf":
		return imageToPdf(ctx, step, progress)
	case o.Compression == presets.PdfCompressNone && o.RemoveMetadata && !o.SplitPages && o.PageRange == "":
		return withoutMetadata(ctx, step, progress)
	case o.Compression == presets.PdfCompressNone || o.Compression == "":
		return splitOrExtract(ctx, step, progress)
	}
	return compressPdf(ctx, step, progress)
}

// imageToPdf places every page of a picture (TIFF pages, …) on its own PDF page.
func imageToPdf(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	frames, err := readPictures(step.InputPath, step.From)
	if err != nil {
		return nil, err
	}
	path := step.Output.Allocate("pdf", "")
	if path == "" {
		return nil, nil
	}
	m, err := newPdfMaker(path)
	if err != nil {
		return nil, err
	}
	opts := &step.Preset.Image
	pdfOpts := &step.Preset.Pdf
	edits := opts.TurnsOrMirrors() || opts.EditsPicture()
	for i, frame := range frames {
		if err := ctx.Err(); err != nil {
			m.Abort()
			return nil, err
		}
		dpi := frame.Dpi
		if dpi <= 0 {
			dpi = 96
		}
		img := frame.Image
		jpegData := frame.Jpeg
		if edits {
			// "pdf" as target: turn / resize / grayscale, no format-specific encoding.
			img = applyOptions(img, opts, "pdf")
			jpegData = nil
			if step.From == "jpg" && !hasAlpha(img) {
				jpegData = encodeJpegBytes(img, max(opts.Quality, 85))
			}
		}
		b := img.Bounds()
		imageW := float64(b.Dx()) / dpi * pointsPerInch
		imageH := float64(b.Dy()) / dpi * pointsPerInch
		pageW, pageH := pageSize(pdfOpts.PageSize, imageW > imageH)
		margin := pdfOpts.MarginMm / millimetresPerInch * pointsPerInch
		if pdfOpts.PageSize == presets.PageFitImage || pdfOpts.PageSize == "" {
			pageW, pageH, margin = imageW, imageH, 0
		}
		// Fit inside the margins, keeping the aspect ratio, centred.
		boxW, boxH := pageW-2*margin, pageH-2*margin
		scale := min(boxW/imageW, boxH/imageH)
		drawW, drawH := imageW*scale, imageH*scale
		x := margin + (boxW-drawW)/2
		y := margin + (boxH-drawH)/2
		if err := m.addPicturePage(pageW, pageH, img, jpegData, x, y, drawW, drawH); err != nil {
			m.Abort()
			return nil, err
		}
		progress.Report(float64(i+1) / float64(len(frames)))
	}
	if err := m.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return []string{path}, nil
}

func pageSize(size presets.PdfPageSize, landscape bool) (float64, float64) {
	w, h := 595.28, 841.89 // A4
	if size == presets.PageLetter {
		w, h = 612, 792
	}
	if landscape {
		return h, w
	}
	return w, h
}

// withoutMetadata: the whole PDF, changed in place so links, bookmarks and
// form fields stay, without its document information and XMP packets.
func withoutMetadata(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	pc, err := readContext(ctx, step.InputPath)
	if err != nil {
		return nil, err
	}
	if err := stripMetadata(ctx, pc); err != nil {
		return nil, err
	}
	progress.Report(0.5)
	out := step.Output.Allocate("pdf", "")
	if out == "" {
		return nil, nil
	}
	if err := writeContext(ctx, pc, out); err != nil {
		return nil, err
	}
	return []string{out}, nil
}

func rotationDegrees(r presets.PdfRotation) int {
	switch r {
	case presets.PdfLeft90:
		return -90
	case presets.PdfRight90:
		return 90
	case presets.PdfRotate180:
		return 180
	}
	return 0
}

func allPages(pages []int, count int) bool {
	if len(pages) != count {
		return false
	}
	for i, p := range pages {
		if p != i {
			return false
		}
	}
	return true
}

// writePages copies pages of pc into a new PDF at path, each turned by degrees.
func writePages(ctx context.Context, pc *pdfContext, pages []int, degrees int, path string) error {
	out, err := extractPages(ctx, pc, pages)
	if err != nil {
		return err
	}
	if err := rotateAll(ctx, out, degrees); err != nil {
		return err
	}
	return writeContext(ctx, out, path)
}

// splitOrExtract: one PDF per page, a page range, or the whole document turned.
func splitOrExtract(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	o := &step.Preset.Pdf
	pc, err := readContext(ctx, step.InputPath)
	if err != nil {
		return nil, err
	}
	pages := convert.ParsePageRange(o.PageRange, pc.PageCount)
	degrees := rotationDegrees(o.Rotation)
	var written []string
	if o.SplitPages {
		for i, p := range pages {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			path := step.Output.Allocate("pdf", fmt.Sprintf("_p%d", p+1))
			if path == "" {
				continue
			}
			if err := writePages(ctx, pc, []int{p}, degrees, path); err != nil {
				return nil, err
			}
			written = append(written, path)
			progress.Report(float64(i+1) / float64(len(pages)))
		}
		return written, nil
	}
	out := step.Output.Allocate("pdf", "")
	if out == "" {
		return nil, nil
	}
	if allPages(pages, pc.PageCount) {
		// The whole document keeps everything (outlines, forms, labels, information).
		if degrees == 0 {
			err = copyWritable(step.InputPath, out)
		} else if err = rotateAll(ctx, pc, degrees); err == nil {
			err = writeContext(ctx, pc, out)
		}
	} else {
		err = writePages(ctx, pc, pages, degrees, out)
	}
	if err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

// compressPdf makes a smaller PDF, also of a page range or one file per page:
// the whole file keeps everything and is compressed as it is; selected pages
// are copied into new PDFs first. When the compression isn't smaller than the
// pages as they were, those are written unchanged and the file gets a notice.
// "Compress with Ghostscript" also tries pdfwrite and keeps the smaller one.
func compressPdf(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	o := &step.Preset.Pdf
	scratch, err := os.MkdirTemp(workDir(step), "compress-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	pc, err := readContext(ctx, step.InputPath)
	if err != nil {
		return nil, err
	}
	pages := convert.ParsePageRange(o.PageRange, pc.PageCount)
	degrees := rotationDegrees(o.Rotation)
	type part struct{ plain, suffix string }
	var parts []part
	switch {
	case o.SplitPages:
		for _, p := range pages {
			path := filepath.Join(scratch, fmt.Sprintf("page-%d.pdf", p+1))
			if err := writePages(ctx, pc, []int{p}, degrees, path); err != nil {
				return nil, err
			}
			parts = append(parts, part{path, fmt.Sprintf("_p%d", p+1)})
		}
	case allPages(pages, pc.PageCount) && degrees == 0:
		parts = append(parts, part{step.InputPath, ""})
	case allPages(pages, pc.PageCount):
		path := filepath.Join(scratch, "rotated.pdf")
		if err := rotateAll(ctx, pc, degrees); err != nil {
			return nil, err
		}
		if err := writeContext(ctx, pc, path); err != nil {
			return nil, err
		}
		parts = append(parts, part{path, ""})
	default:
		path := filepath.Join(scratch, "pages.pdf")
		if err := writePages(ctx, pc, pages, degrees, path); err != nil {
			return nil, err
		}
		parts = append(parts, part{path, ""})
	}

	gs := ""
	if o.CompressWithGhostscript {
		gs = ghostscript.Locate()
	}
	var written []string
	anySmaller := false
	for i, p := range parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out := step.Output.Allocate("pdf", p.suffix)
		if out == "" {
			continue
		}
		smaller, err := compressFile(ctx, p.plain, out, step.Preset, gs, scratch)
		if err != nil {
			return nil, err
		}
		anySmaller = anySmaller || smaller
		written = append(written, out)
		progress.Report(float64(i+1) / float64(len(parts)))
	}
	if len(written) > 0 && !anySmaller && step.Notice != nil {
		step.Notice("notice.pdf_not_smaller")
	}
	return written, nil
}

// compressFile writes the smallest of plain and its compressed versions to
// output: the built-in compressor's and, with "Compress with Ghostscript",
// pdfwrite's. It reports whether a compressed version was smaller.
func compressFile(ctx context.Context, plain, output string, preset *presets.Preset, gs, scratch string) (bool, error) {
	var candidates []string
	var failure error
	builtIn := filepath.Join(scratch, fmt.Sprintf("c%d-filee.pdf", len(plain)))
	if err := compressBuiltIn(ctx, plain, builtIn, preset); err == nil {
		candidates = append(candidates, builtIn)
	} else if gs == "" || ctx.Err() != nil {
		return false, err
	} else {
		failure = err // Ghostscript may still manage
	}
	if gs != "" {
		rewritten := filepath.Join(scratch, fmt.Sprintf("c%d-gs.pdf", len(plain)))
		err := ghostscript.CompressPdf(ctx, gs, plain, rewritten, preset.Pdf.Compression, scratch)
		switch {
		case err == nil:
			candidates = append(candidates, rewritten)
		case ctx.Err() != nil:
			return false, ctx.Err()
		case len(candidates) == 0:
			return false, failure // the built-in result would have stood; there is none
		}
	}
	original, err := os.Stat(plain)
	if err != nil {
		return false, err
	}
	best, bestSize := "", original.Size()
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.Size() < bestSize {
			best, bestSize = c, fi.Size()
		}
	}
	if best != "" {
		return true, copyWritable(best, output)
	}
	return false, copyWritable(plain, output)
}

func compressBuiltIn(ctx context.Context, plain, target string, preset *presets.Preset) error {
	pc, err := readContext(ctx, plain)
	if err != nil {
		return err
	}
	if err := compressContext(ctx, pc, preset.Pdf.Compression, preset.Image.KeepMetadata); err != nil {
		return err
	}
	return writeContext(ctx, pc, target)
}

// workDir is the step's scratch folder (the system's when the step has none).
func workDir(step *convert.Step) string {
	if step.WorkDir != "" {
		if err := os.MkdirAll(step.WorkDir, 0o755); err == nil {
			return step.WorkDir
		}
	}
	return os.TempDir()
}
