package gfx

import (
	"math"
	"slices"
)

// subSamples is the number of sub-scanlines per pixel row; coverage across a
// sub-scanline is exact, so edges get subSamples × exact anti-aliasing.
const subSamples = 16

type edge struct {
	x0, y0, y1, dxdy float64
	dir              int
}

type crossing struct {
	x   float64
	dir int
}

// coverageRows rasterizes closed polygons (every polyline is treated as closed)
// into coverage rows within the pixel rectangle (cx0, cy0)–(cx1, cy1). For each
// row with ink, fn gets the row index, the first and last+1 column and the
// coverage of those columns (cov[x-x0]), which it may modify.
func coverageRows(polys []Polyline, rule FillRule, cx0, cy0, cx1, cy1 int, fn func(y, x0, x1 int, cov []float32)) {
	if cx1 <= cx0 || cy1 <= cy0 {
		return
	}
	var edges []edge
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, pl := range polys {
		n := len(pl.Pts)
		if n < 2 {
			continue
		}
		for i := 0; i < n; i++ {
			a, b := pl.Pts[i], pl.Pts[(i+1)%n]
			if a.Y == b.Y || math.IsNaN(a.X+a.Y+b.X+b.Y) || math.IsInf(a.X+a.Y+b.X+b.Y, 0) {
				continue
			}
			dir := 1
			if a.Y > b.Y {
				a, b = b, a
				dir = -1
			}
			if b.Y <= float64(cy0) || a.Y >= float64(cy1) {
				continue
			}
			edges = append(edges, edge{x0: a.X - a.Y*(b.X-a.X)/(b.Y-a.Y), y0: a.Y, y1: b.Y, dxdy: (b.X - a.X) / (b.Y - a.Y), dir: dir})
			minY = math.Min(minY, a.Y)
			maxY = math.Max(maxY, b.Y)
		}
	}
	if len(edges) == 0 {
		return
	}
	slices.SortFunc(edges, func(a, b edge) int {
		switch {
		case a.y0 < b.y0:
			return -1
		case a.y0 > b.y0:
			return 1
		}
		return 0
	})
	rowStart := max(cy0, int(math.Floor(minY)))
	rowEnd := min(cy1, int(math.Ceil(maxY)))
	width := cx1 - cx0
	cov := make([]float32, width+2)
	diff := make([]float32, width+2)
	var active []int
	next := 0
	var xs []crossing
	const w = 1.0 / subSamples
	fx0, fx1 := float64(cx0), float64(cx1)
	for y := rowStart; y < rowEnd; y++ {
		minX, maxX := width+1, -1
		touched := false
		for s := 0; s < subSamples; s++ {
			sy := float64(y) + (float64(s)+0.5)/subSamples
			for next < len(edges) && edges[next].y0 <= sy {
				active = append(active, next)
				next++
			}
			xs = xs[:0]
			k := 0
			for _, i := range active {
				e := &edges[i]
				if e.y1 <= sy {
					continue
				}
				active[k] = i
				k++
				if e.y0 <= sy {
					xs = append(xs, crossing{e.x0 + sy*e.dxdy, e.dir})
				}
			}
			active = active[:k]
			if len(xs) < 2 {
				continue
			}
			// Insertion sort: crossings stay mostly in order from one sub-scanline to the next.
			for i := 1; i < len(xs); i++ {
				for j := i; j > 0 && xs[j].x < xs[j-1].x; j-- {
					xs[j], xs[j-1] = xs[j-1], xs[j]
				}
			}
			wind := 0
			for i := 0; i < len(xs)-1; i++ {
				wind += xs[i].dir
				inside := wind != 0
				if rule == EvenOdd {
					inside = wind&1 != 0
				}
				if !inside {
					continue
				}
				xa, xb := xs[i].x, xs[i+1].x
				if xb <= fx0 || xa >= fx1 || xb <= xa {
					continue
				}
				xa = math.Max(xa, fx0) - fx0
				xb = math.Min(xb, fx1) - fx0
				ia, ib := int(xa), int(xb)
				if ib >= width {
					ib = width - 1
					if xb > float64(width) {
						xb = float64(width)
					}
				}
				touched = true
				if ia == ib {
					cov[ia] += float32((xb - xa) * w)
				} else {
					cov[ia] += float32((float64(ia+1) - xa) * w)
					diff[ia+1] += w
					diff[ib] -= w
					cov[ib] += float32((xb - float64(ib)) * w)
				}
				minX = min(minX, ia)
				maxX = max(maxX, ib)
			}
		}
		if !touched || maxX < minX {
			continue
		}
		run := float32(0)
		for x := minX; x <= maxX; x++ {
			run += diff[x]
			diff[x] = 0
			v := cov[x] + run
			if v > 1 {
				v = 1
			} else if v < 0 {
				v = 0
			}
			cov[x] = v
		}
		diff[maxX+1] = 0
		fn(y, cx0+minX, cx0+maxX+1, cov[minX:maxX+1])
		clear(cov[minX : maxX+1])
	}
}
