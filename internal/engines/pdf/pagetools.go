// Lossless page tools of the interactive editor: pages copied in a new order
// (also from several PDFs) and turned, and crop boxes changed, without
// redrawing their text and drawings.

package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/dseif0x/filee-go/internal/convert"
)

// PageReference is one page of an edited document; Rotation is an extra
// clockwise turn in degrees.
type PageReference struct {
	Path      string
	PageIndex int
	Rotation  int
}

func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return strings.EqualFold(a, b)
}

// SavePages copies pages in the given order and applies their turns.
func SavePages(ctx context.Context, pages []PageReference, output string) error {
	if len(pages) == 0 {
		return errors.New("A PDF must contain at least one page.")
	}
	// Every page of one document in its order only changes /Rotate: change a
	// copy, so outlines, forms, page labels and information stay.
	first := pages[0].Path
	inOrder := true
	for i, p := range pages {
		if !samePath(p.Path, first) || p.PageIndex != i {
			inOrder = false
			break
		}
	}
	if inOrder {
		pc, err := readContext(ctx, first)
		if err != nil {
			return err
		}
		if pc.PageCount == len(pages) {
			for i, p := range pages {
				if err := rotatePage(ctx, pc, i+1, p.Rotation); err != nil {
					return err
				}
			}
			return writeContext(ctx, pc, output)
		}
	}

	// Runs of pages from one file become one part each; the parts are merged.
	sources := map[string]*pdfContext{}
	type run struct {
		path  string
		pages []PageReference
	}
	var runs []run
	for _, p := range pages {
		key, _ := filepath.Abs(p.Path)
		key = strings.ToLower(key)
		src, ok := sources[key]
		if !ok {
			var err error
			if src, err = readContext(ctx, p.Path); err != nil {
				return err
			}
			sources[key] = src
		}
		if p.PageIndex < 0 || p.PageIndex >= src.PageCount {
			return fmt.Errorf("Page %d no longer exists in %s.", p.PageIndex+1, filepath.Base(p.Path))
		}
		if len(runs) == 0 || runs[len(runs)-1].path != key {
			runs = append(runs, run{path: key})
		}
		runs[len(runs)-1].pages = append(runs[len(runs)-1].pages, p)
	}
	dir, err := os.MkdirTemp(filepath.Dir(output), ".filee-pages-")
	if err != nil {
		dir, err = os.MkdirTemp("", "filee-pages-")
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(dir)
	var parts []string
	for i, r := range runs {
		idx := make([]int, len(r.pages))
		for k, p := range r.pages {
			idx[k] = p.PageIndex
		}
		out, err := extractPages(ctx, sources[r.path], idx)
		if err != nil {
			return err
		}
		for k, p := range r.pages {
			if err := rotatePage(ctx, out, k+1, p.Rotation); err != nil {
				return err
			}
		}
		part := filepath.Join(dir, fmt.Sprintf("part%d.pdf", i))
		if err := writeContext(ctx, out, part); err != nil {
			return err
		}
		parts = append(parts, part)
	}
	if len(parts) == 1 {
		return os.Rename(parts[0], output)
	}
	return MergePdfs(ctx, parts, output)
}

// CropAll applies one crop rectangle, in the shown page's normalised
// top-left coordinates (0–1), to every page. Only the crop boxes change.
func CropAll(ctx context.Context, input, output string, x, y, width, height float64) error {
	x, y = clamp01(x), clamp01(y)
	width = max(0.0001, min(width, 1-x))
	height = max(0.0001, min(height, 1-y))
	pc, err := readContext(ctx, input)
	if err != nil {
		return err
	}
	for n := 1; n <= pc.PageCount; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		d, _, inh, err := pc.PageDict(ctx, n, false)
		if err != nil {
			return err
		}
		if inh == nil || inh.MediaBox == nil {
			continue
		}
		box := inh.MediaBox
		if inh.CropBox != nil {
			box = intersect(inh.CropBox, inh.MediaBox)
		}
		l, b, r, t := mapFromShown(inh.Rotate, x, y, width, height)
		bw, bh := box.Width(), box.Height()
		d["CropBox"] = types.NewNumberArray(box.LL.X+l*bw, box.LL.Y+b*bh, box.LL.X+r*bw, box.LL.Y+t*bh)
	}
	return writeContext(ctx, pc, output)
}

// RotateAll turns every page, keeping the catalogue, forms, labels, outlines and information.
func RotateAll(ctx context.Context, input, output string, degrees int) error {
	pc, err := readContext(ctx, input)
	if err != nil {
		return err
	}
	if err := rotateAll(ctx, pc, degrees); err != nil {
		return err
	}
	return writeContext(ctx, pc, output)
}

func clamp01(v float64) float64 { return max(0, min(v, 1)) }

func intersect(a, b *types.Rectangle) *types.Rectangle {
	r := types.NewRectangle(max(a.LL.X, b.LL.X), max(a.LL.Y, b.LL.Y), min(a.UR.X, b.UR.X), min(a.UR.Y, b.UR.Y))
	if r.Width() <= 0 || r.Height() <= 0 {
		return b
	}
	return r
}

// mapFromShown turns shown top-left coordinates into the unturned box (left, bottom, right, top).
func mapFromShown(rotation int, x, y, w, h float64) (float64, float64, float64, float64) {
	u0, u1, v0, v1 := x, x+w, y, y+h
	switch normaliseRotation(rotation) {
	case 90:
		return v0, u0, v1, u1
	case 180:
		return 1 - u1, v0, 1 - u0, v1
	case 270:
		return 1 - v1, 1 - u1, 1 - v0, 1 - u0
	}
	return u0, 1 - v1, u1, 1 - v0
}

// FirstPage writes the first page of pageRange (all pages when empty) of
// input as a PDF of its own: LibreOffice Draw's SVG export draws every page
// of a PDF on top of each other.
func FirstPage(ctx context.Context, input, output, pageRange string) error {
	pc, err := readContext(ctx, input)
	if err != nil {
		return err
	}
	pages := convert.ParsePageRange(pageRange, pc.PageCount)
	if len(pages) == 0 {
		return errors.New("The PDF has no pages in the selected range.")
	}
	single, err := extractPages(ctx, pc, pages[:1])
	if err != nil {
		return err
	}
	return writeContext(ctx, single, output)
}
