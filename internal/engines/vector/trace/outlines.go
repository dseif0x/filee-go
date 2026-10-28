// Package trace turns raster pictures into vector outlines (raster → SVG),
// written for Filee from the published descriptions of the steps; no code of
// Potrace, AutoTrace or VTracer is used.
//
// Black and white: pixels darker than Otsu's threshold (or, on a transparent
// background, the opaque pixels) are traced as one black shape. Colour: the
// picture is reduced to N colours and each colour is traced as the union of
// itself and every colour stacked above it, bottom (largest) colour first, so
// a lower shape runs underneath the shapes above it instead of meeting them
// edge to edge and no background shows through between them.
package trace

import (
	"context"
	"math"
	"math/bits"
)

// Step 1: the outlines of a pixel mask, as closed polygons along the pixel
// edges. Every edge between an inside and an outside pixel is a directed edge
// of the pixel grid with the inside on its right (y down): around a filled
// area the outline runs clockwise, around a hole counter-clockwise, so the
// outlines fill correctly with the non-zero rule. Where two inside pixels
// touch only at a corner the walk turns left, which keeps diagonally touching
// pixels in one shape (8-connected shapes, 4-connected holes).

// PixelOutline is a closed outline along pixel edges: its corner points.
type PixelOutline struct {
	Xs, Ys []int
	// Area is the signed area in square pixels: positive around filled areas.
	Area float64
}

func (o *PixelOutline) count() int { return len(o.Xs) }

var (
	stepX = [4]int{1, 0, -1, 0}
	stepY = [4]int{0, 1, 0, -1}
)

const dirRight = 0

// TraceOutlines returns every outline of the pixels where mask is not 0.
// Outlines enclosing less than minArea square pixels are left out.
func TraceOutlines(ctx context.Context, mask []byte, width, height int, minArea float64) ([]PixelOutline, error) {
	var outlines []PixelOutline
	stride := width + 1
	visited := make([]byte, stride*(height+1))
	var xs, ys []int
	for y := 0; y < height; y++ {
		if y&63 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		row := y * width
		for x := 0; x < width; x++ {
			// Every outline has an edge going right: the top edge of an inside pixel below an outside one.
			if mask[row+x] == 0 || (y > 0 && mask[row-width+x] != 0) || visited[y*stride+x]&1 != 0 {
				continue
			}
			var area2 int64
			xs, ys, area2 = walk(mask, width, height, visited, x, y, xs[:0], ys[:0])
			if math.Abs(float64(area2))/2 >= math.Max(minArea, 1e-9) && len(xs) >= 4 {
				outlines = append(outlines, PixelOutline{Xs: append([]int(nil), xs...), Ys: append([]int(nil), ys...), Area: float64(area2) / 2})
			}
		}
	}
	return outlines, nil
}

func walk(mask []byte, width, height int, visited []byte, startX, startY int, xs, ys []int) ([]int, []int, int64) {
	stride := width + 1
	x, y, direction := startX, startY, dirRight
	var area2 int64
	for {
		visited[y*stride+x] |= 1 << direction
		nx, ny := x+stepX[direction], y+stepY[direction]
		area2 += int64(x)*int64(ny) - int64(nx)*int64(y)
		x, y = nx, ny
		leaving := leavingEdges(mask, width, height, x, y)
		var next int
		if leaving&(leaving-1) == 0 {
			next = bits.TrailingZeros(uint(leaving))
		} else {
			// Saddle: turn left, so pixels touching at the corner stay together.
			next = (direction + 3) & 3
			if leaving&(1<<next) == 0 {
				next = (direction + 1) & 3
			}
		}
		if x == startX && y == startY && next == dirRight {
			if direction != dirRight {
				xs = append(xs, x)
				ys = append(ys, y)
			}
			return xs, ys, area2
		}
		if next != direction {
			xs = append(xs, x)
			ys = append(ys, y)
		}
		direction = next
	}
}

func leavingEdges(mask []byte, width, height, x, y int) int {
	inside := func(x, y int) bool {
		return uint(x) < uint(width) && uint(y) < uint(height) && mask[y*width+x] != 0
	}
	tl, tr := inside(x-1, y-1), inside(x, y-1)
	bl, br := inside(x-1, y), inside(x, y)
	b := 0
	if br && !tr {
		b |= 1 // right
	}
	if bl && !br {
		b |= 2 // down
	}
	if tl && !bl {
		b |= 4 // left
	}
	if tr && !tl {
		b |= 8 // up
	}
	return b
}
