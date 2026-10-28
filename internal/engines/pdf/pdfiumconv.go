// PDF pages → pictures with PDFium: one picture per page, one multi-page TIFF,
// GIF, APNG or MNG, or (PdfOptions.CombinePages) the pages stacked into one
// tall image. The image engine applies the picture options and writes.

package pdf

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/images"
	"github.com/dseif0x/filee-go/internal/presets"
)

// MaxStackedPixels: a tall image beyond this many pixels is scaled down (a
// 300-page PDF at 300 dpi would need gigabytes).
const MaxStackedPixels = 200_000_000

// Pdfium renders PDF pages to raster images.
type Pdfium struct{}

func (Pdfium) ID() string   { return "pdfium" }
func (Pdfium) Name() string { return "PDFium" }

// MaxParallelism: one PDFium WebAssembly instance per conversion; a few run at once.
func (Pdfium) MaxParallelism() int { return pdfiumWorkers }

// Edges: PDF → every format the image engine writes (C# ImageEncoder.Writable).
func (Pdfium) Edges() []convert.Edge { return convert.Fan([]string{"pdf"}, images.Writable, 10) }

// multiPageTargets hold every page in one file.
var multiPageTargets = map[string]bool{"tiff": true, "gif": true, "apng": true, "mng": true}

func (Pdfium) Status() convert.Status { return convert.Available("", PdfiumVersion) }

// Transforms: pages are rendered, then the picture options apply to them.
func (Pdfium) Transforms(from, to string) bool { return true }

func (Pdfium) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	d, err := openPdfiumFile(ctx, step.InputPath)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	count, err := d.PageCount()
	if err != nil {
		return nil, err
	}
	opts := &step.Preset.Pdf
	img := &step.Preset.Image
	pages := convert.ParsePageRange(opts.PageRange, count)
	dpi := max(36, min(opts.RenderDpi, 1200))
	if opts.RenderDpi == 0 {
		dpi = 150
	}
	stack := opts.CombinePages && len(pages) > 1 && !multiPageTargets[step.To]
	var written []string
	var collected []*images.Frame
	// write writes one picture through the image engine, under the given suffix.
	write := func(f *images.Frame, suffix string) error {
		out := convert.AllocatorFunc(func(ext, _ string) string { return step.Output.Allocate(ext, suffix) })
		paths, err := images.Write([]*images.Frame{f}, step.To, out, img, "", false, 0)
		written = append(written, paths...)
		return err
	}
	// newFrame is a rendered picture at the render resolution (ApplyOptions sets the preset's).
	newFrame := func(pic image.Image) *images.Frame {
		f := images.NewFrame(pic)
		f.Meta.DpiX, f.Meta.DpiY = float64(dpi), float64(dpi)
		return f
	}

	for i, index := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := d.Render(index, dpi)
		if err != nil {
			return nil, err
		}
		frame := newFrame(page)
		if stack {
			// Each page turns, then they stack: a turned long image would lie on its side.
			images.Turn(frame, img)
			if opts.TrimPageMargins {
				frame.Img = toNRGBA(trimVertically(frame.Img, dpi))
			}
			collected = append(collected, frame)
			progress.Report(float64(i+1) / float64(len(pages)) * 0.8)
			continue
		}
		images.ApplyOptions(frame, img, step.To)
		if multiPageTargets[step.To] {
			collected = append(collected, frame) // one multi-page file, written after the loop
		} else {
			suffix := ""
			if len(pages) > 1 {
				suffix = fmt.Sprintf("_p%d", index+1)
			}
			if err := write(frame, suffix); err != nil {
				return nil, err
			}
		}
		progress.Report(float64(i+1) / float64(len(pages)))
	}

	switch {
	case stack:
		unturned := *img // the pages are turned already
		unturned.Rotation, unturned.Flip = presets.RotateNone, presets.FlipNone
		parts := [][]*images.Frame{collected}
		if opts.SplitTallImage {
			parts = splitRuns(collected, step.To, &unturned)
		}
		for p, part := range parts {
			pics := make([]image.Image, len(part))
			for i, f := range part {
				pics[i] = f.Img
			}
			tall := newFrame(stackPages(pics))
			images.ApplyOptions(tall, &unturned, step.To) // also scales it to fit the format
			suffix := ""
			if len(parts) > 1 {
				suffix = fmt.Sprintf("_%d", p+1)
			}
			if err := write(tall, suffix); err != nil {
				return nil, err
			}
		}
	case len(collected) > 0:
		paths, err := images.Write(collected, step.To, step.Output, img, "", false, 0)
		written = append(written, paths...)
		if err != nil {
			return nil, err
		}
	}
	progress.Report(1)
	return written, nil
}

// stackPages puts the pages one under another, centred on a white strip as
// wide as the widest page, scaled down past MaxStackedPixels.
func stackPages(pages []image.Image) image.Image {
	var width, height float64
	for _, p := range pages {
		width = max(width, float64(p.Bounds().Dx()))
		height += float64(p.Bounds().Dy())
	}
	scale := min(1, math.Sqrt(MaxStackedPixels/(width*height)))
	stripWidth := max(1, int(math.Floor(width*scale)))
	var scaled []image.Image
	total := 0
	for _, p := range pages {
		if scale < 1 {
			b := p.Bounds()
			p = scaleTo(p, max(1, int(math.Floor(float64(b.Dx())*scale))), max(1, int(math.Floor(float64(b.Dy())*scale))))
		}
		scaled = append(scaled, p)
		total += p.Bounds().Dy()
	}
	out := image.NewRGBA(image.Rect(0, 0, stripWidth, total))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	y := 0
	for _, p := range scaled {
		b := p.Bounds()
		x := (stripWidth - b.Dx()) / 2
		draw.Draw(out, image.Rect(x, y, x+b.Dx(), y+b.Dy()), p, b.Min, draw.Over)
		y += b.Dy()
	}
	return out
}

// splitRuns cuts the pages into runs short enough for one image of the format
// after the preset's resize (a 44-page certificate at 1200 px wide is
// 74,673 px tall, more than a JPG holds). Runs break between pages.
func splitRuns(pages []*images.Frame, target string, o *presets.ImageOptions) [][]*images.Frame {
	var width, height float64
	for _, p := range pages {
		width = max(width, float64(p.Width()))
		height += float64(p.Height())
	}
	scale := images.ResizeScale(width, height, o) * min(1, math.Sqrt(MaxStackedPixels/(width*height)))
	limit := images.MaxSide(target) / scale
	var parts [][]*images.Frame
	var run []*images.Frame
	var runHeight float64
	for _, p := range pages {
		h := float64(p.Height())
		if len(run) > 0 && runHeight+h > limit {
			parts = append(parts, run)
			run, runHeight = nil, 0
		}
		run = append(run, p)
		runHeight += h
	}
	return append(parts, run)
}

// trimVertically cuts the blank rows above and below a page's content,
// keeping about 4 mm so text does not touch the next page. A blank page
// shrinks to that margin.
func trimVertically(page image.Image, dpi int) image.Image {
	margin := max(4, dpi/6)
	b := page.Bounds()
	h := b.Dy()
	// Scanned pages are not pure white: 8 % fuzz.
	const fuzz = 20
	blankRow := func(y int) bool {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(page.At(x, y)).(color.RGBA)
			if 255-int(c.R) > fuzz || 255-int(c.G) > fuzz || 255-int(c.B) > fuzz {
				return false
			}
		}
		return true
	}
	first, last := -1, -1
	for y := 0; y < h; y++ {
		if !blankRow(b.Min.Y + y) {
			first = y
			break
		}
	}
	var top, bottom int
	if first < 0 {
		top, bottom = 0, min(h, margin*2)
	} else {
		for y := h - 1; y >= first; y-- {
			if !blankRow(b.Min.Y + y) {
				last = y
				break
			}
		}
		if first == 0 && last == h-1 {
			return page
		}
		top, bottom = max(0, first-margin), min(h, last+1+margin)
	}
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), bottom-top))
	draw.Draw(out, out.Bounds(), page, image.Pt(b.Min.X, b.Min.Y+top), draw.Src)
	return out
}
