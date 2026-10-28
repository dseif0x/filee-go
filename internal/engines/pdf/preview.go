// Page previews for the interactive tools (page editor, crop, print studio):
// PNG bytes of one page or of every page's thumbnail, bounded in size.

package pdf

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
)

// PagePreview is one rendered page.
type PagePreview struct {
	// PageNumber is 1-based (RenderPage, RenderThumbnails); 0 for Preview.
	PageNumber    int
	Png           []byte
	Width, Height int
}

// PreviewDocument is a PDF read once for previews: its bytes and page sizes,
// so a document with hundreds of pages isn't inspected again per thumbnail.
type PreviewDocument struct {
	Path  string
	data  []byte
	sizes [][2]float64
}

// OpenPreview reads a PDF for previews.
func OpenPreview(path string) (*PreviewDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := openPdfium(context.Background(), data)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	sizes, err := d.PageSizes()
	if err != nil {
		return nil, err
	}
	return &PreviewDocument{Path: path, data: data, sizes: sizes}, nil
}

func (p *PreviewDocument) PageCount() int { return len(p.sizes) }

// PageSize is the shown size of a page in points (0-based).
func (p *PreviewDocument) PageSize(index int) (float64, float64) {
	return p.sizes[index][0], p.sizes[index][1]
}

// Preview renders one page (0-based) for the crop tool and page editor, at
// most maxDimension pixels on its long side (36–150 dpi).
func (p *PreviewDocument) Preview(ctx context.Context, pageIndex, maxDimension int) (*PagePreview, error) {
	if pageIndex < 0 || pageIndex >= len(p.sizes) {
		return nil, fmt.Errorf("page %d out of range", pageIndex+1)
	}
	if maxDimension <= 0 {
		maxDimension = 2560
	}
	longest := max(p.sizes[pageIndex][0], p.sizes[pageIndex][1])
	dpi := 96
	if longest > 0 {
		dpi = max(36, min(int(math.Floor(float64(maxDimension)*72/longest)), 150))
	}
	return p.render(ctx, []int{pageIndex}, dpi, 0, false)
}

// RenderPage renders page pageNumber (1-based) at most maxLongEdge pixels on its long side.
func (p *PreviewDocument) RenderPage(ctx context.Context, pageNumber, maxLongEdge int) (*PagePreview, error) {
	if pageNumber < 1 || pageNumber > len(p.sizes) {
		return nil, fmt.Errorf("page %d out of range", pageNumber)
	}
	s := p.sizes[pageNumber-1]
	dpi := int(math.Ceil(max(24, min(float64(maxLongEdge)*72/max(s[0], s[1]), 180))))
	return p.render(ctx, []int{pageNumber - 1}, dpi, maxLongEdge, true)
}

// RenderThumbnails renders every page in one PDFium document load.
func (p *PreviewDocument) RenderThumbnails(ctx context.Context, maxLongEdge int) ([]*PagePreview, error) {
	if len(p.sizes) == 0 {
		return nil, nil
	}
	var largest float64
	for _, s := range p.sizes {
		largest = max(largest, s[0], s[1])
	}
	dpi := int(math.Ceil(max(24, min(float64(maxLongEdge)*72/largest, 180))))
	d, err := openPdfium(ctx, p.data)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	out := make([]*PagePreview, 0, len(p.sizes))
	for i := range p.sizes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		img, err := d.Render(i, dpi)
		if err != nil {
			return nil, err
		}
		pv, err := previewOf(downscale(img, maxLongEdge), i+1)
		if err != nil {
			return nil, err
		}
		out = append(out, pv)
	}
	return out, nil
}

func (p *PreviewDocument) render(ctx context.Context, pages []int, dpi, maxLongEdge int, numbered bool) (*PagePreview, error) {
	d, err := openPdfium(ctx, p.data)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	img, err := d.Render(pages[0], dpi)
	if err != nil {
		return nil, err
	}
	var shown image.Image = img
	if maxLongEdge > 0 {
		shown = downscale(img, maxLongEdge)
	}
	number := 0
	if numbered {
		number = pages[0] + 1
	}
	return previewOf(shown, number)
}

func previewOf(img image.Image, number int) (*PagePreview, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, err
	}
	b := img.Bounds()
	return &PagePreview{PageNumber: number, Png: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

func downscale(img image.Image, maxLongEdge int) image.Image {
	b := img.Bounds()
	longest := max(b.Dx(), b.Dy())
	if maxLongEdge <= 0 || longest <= maxLongEdge {
		return img
	}
	f := float64(maxLongEdge) / float64(longest)
	return scaleTo(img, max(1, int(math.Round(float64(b.Dx())*f))), max(1, int(math.Round(float64(b.Dy())*f))))
}
