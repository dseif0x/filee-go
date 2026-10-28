package trace

import (
	"context"
	"math"
	"sort"
)

// Colour mode, before outlines are traced: the picture is reduced to a few
// colours (k-means) and cleaned up, so each colour forms regions worth an outline.
//
//  1. Quantization: every opaque pixel goes into one of 32 768 boxes (5 bits
//     per channel). k-means runs on the boxes' mean colours weighted by their
//     pixel counts; the start colours are picked greedily by weight × squared
//     distance to the colours already picked (deterministic).
//  2. Edge bands: a pixel of a one-pixel band whose colour lies between the
//     colours on either side (anti-aliasing) joins the closer of the two.
//  3. Small regions: connected regions (4-connected) smaller than the minimum
//     join the neighbour they share the longest border with, smallest first.

const (
	// Transparent is the label of transparent pixels.
	Transparent = 255
	// OpaqueAlpha: pixels less opaque than this are transparent.
	OpaqueAlpha   = 128
	maxIterations = 24
)

func box(r, g, b byte) int { return int(r>>3)<<10 | int(g>>3)<<5 | int(b>>3) }

func toByte(v float64) int { return int(math.Max(0, math.Min(255, math.Round(v)))) }

// Quantize reduces RGBA pixels to at most colors colours: the palette
// (0xRRGGBB) and a label per pixel (palette index or Transparent).
func Quantize(ctx context.Context, rgba []byte, width, height, colors int) ([]int, []byte, error) {
	n := width * height
	counts := make([]int64, 32768)
	sums := make([]int64, 32768*3)
	for p := 0; p < n; p++ {
		o := p * 4
		if rgba[o+3] < OpaqueAlpha {
			continue
		}
		b := box(rgba[o], rgba[o+1], rgba[o+2])
		counts[b]++
		sums[b*3] += int64(rgba[o])
		sums[b*3+1] += int64(rgba[o+1])
		sums[b*3+2] += int64(rgba[o+2])
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	var boxes []int
	for b, c := range counts {
		if c > 0 {
			boxes = append(boxes, b)
		}
	}
	labels := make([]byte, n)
	if len(boxes) == 0 {
		for i := range labels {
			labels[i] = Transparent
		}
		return nil, labels, nil
	}
	points := make([]float64, len(boxes)*3)
	weights := make([]float64, len(boxes))
	for i, b := range boxes {
		weights[i] = float64(counts[b])
		for c := 0; c < 3; c++ {
			points[i*3+c] = float64(sums[b*3+c]) / float64(counts[b])
		}
	}
	k := min(colors, len(boxes))
	centers := initialCenters(points, weights, k)
	assign := make([]int, len(boxes))
	for i := range assign {
		assign[i] = -1
	}
	for it := 0; it < maxIterations; it++ {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		changed := false
		for i := range boxes {
			nearest := nearestCenter(centers, k, points[i*3], points[i*3+1], points[i*3+2])
			if nearest != assign[i] {
				assign[i] = nearest
				changed = true
			}
		}
		if !changed {
			break
		}
		recenter(points, weights, assign, centers, k)
	}
	recenter(points, weights, assign, centers, k)
	used := make([]bool, k)
	for _, a := range assign {
		used[a] = true
	}
	remap := make([]int, k)
	var palette []int
	for c := 0; c < k; c++ {
		if !used[c] {
			continue
		}
		remap[c] = len(palette)
		palette = append(palette, toByte(centers[c*3])<<16|toByte(centers[c*3+1])<<8|toByte(centers[c*3+2]))
	}
	boxLabel := make([]byte, 32768)
	for i, b := range boxes {
		boxLabel[b] = byte(remap[assign[i]])
	}
	for p := 0; p < n; p++ {
		o := p * 4
		if rgba[o+3] < OpaqueAlpha {
			labels[p] = Transparent
		} else {
			labels[p] = boxLabel[box(rgba[o], rgba[o+1], rgba[o+2])]
		}
	}
	return palette, labels, nil
}

func initialCenters(points, weights []float64, k int) []float64 {
	count := len(weights)
	centers := make([]float64, k*3)
	dist := make([]float64, count)
	for i := range dist {
		dist[i] = math.MaxFloat64
	}
	pick := 0
	for i := 1; i < count; i++ {
		if weights[i] > weights[pick] {
			pick = i
		}
	}
	for c := 0; c < k; c++ {
		copy(centers[c*3:c*3+3], points[pick*3:pick*3+3])
		best := -1.0
		next := 0
		for i := 0; i < count; i++ {
			d := pointDistance(points, i, centers, c)
			if d < dist[i] {
				dist[i] = d
			}
			score := dist[i] * weights[i]
			if score > best {
				best = score
				next = i
			}
		}
		pick = next
	}
	return centers
}

func recenter(points, weights []float64, assign []int, centers []float64, k int) {
	sums := make([]float64, k*3)
	totals := make([]float64, k)
	for i, c := range assign {
		totals[c] += weights[i]
		for j := 0; j < 3; j++ {
			sums[c*3+j] += points[i*3+j] * weights[i]
		}
	}
	for c := 0; c < k; c++ {
		if totals[c] <= 0 {
			continue
		}
		for j := 0; j < 3; j++ {
			centers[c*3+j] = sums[c*3+j] / totals[c]
		}
	}
}

func nearestCenter(centers []float64, k int, r, g, b float64) int {
	best, bestD := 0, math.MaxFloat64
	for c := 0; c < k; c++ {
		dr, dg, db := centers[c*3]-r, centers[c*3+1]-g, centers[c*3+2]-b
		if d := dr*dr + dg*dg + db*db; d < bestD {
			bestD, best = d, c
		}
	}
	return best
}

func pointDistance(points []float64, i int, centers []float64, c int) float64 {
	dr := points[i*3] - centers[c*3]
	dg := points[i*3+1] - centers[c*3+1]
	db := points[i*3+2] - centers[c*3+2]
	return dr*dr + dg*dg + db*db
}

// MergeEdgeBands lets one-pixel bands of in-between colours join the closer side.
func MergeEdgeBands(labels, rgba []byte, palette []int, width, height int) {
	src := append([]byte(nil), labels...)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := y*width + x
			own := src[p]
			if own == Transparent {
				continue
			}
			a, b := -1, -1
			at := func(cond bool, q int) byte {
				if cond {
					return src[q]
				}
				return Transparent
			}
			up := at(y > 0, p-width)
			down := at(y < height-1, p+width)
			left := at(x > 0, p-1)
			right := at(x < width-1, p+1)
			if up != own && down != own && up != Transparent && down != Transparent && up != down {
				a, b = int(up), int(down)
			} else if left != own && right != own && left != Transparent && right != Transparent && left != right {
				a, b = int(left), int(right)
			}
			if a < 0 || !isBetween(palette[own], palette[a], palette[b]) {
				continue
			}
			o := p * 4
			pixel := int(rgba[o])<<16 | int(rgba[o+1])<<8 | int(rgba[o+2])
			if colorDistance(pixel, palette[a]) <= colorDistance(pixel, palette[b]) {
				labels[p] = byte(a)
			} else {
				labels[p] = byte(b)
			}
		}
	}
}

func isBetween(c, a, b int) bool {
	ar, ag, ab := float64(a>>16), float64(a>>8&255), float64(a&255)
	dr, dg, db := float64(b>>16)-ar, float64(b>>8&255)-ag, float64(b&255)-ab
	cr, cg, cb := float64(c>>16)-ar, float64(c>>8&255)-ag, float64(c&255)-ab
	l2 := dr*dr + dg*dg + db*db
	if l2 < 1 {
		return false
	}
	t := (cr*dr + cg*dg + cb*db) / l2
	if t < 0.08 || t > 0.92 {
		return false
	}
	er, eg, eb := cr-t*dr, cg-t*dg, cb-t*db
	return er*er+eg*eg+eb*eb < 0.09*l2
}

func colorDistance(x, y int) int {
	dr := x>>16 - y>>16
	dg := x>>8&255 - y>>8&255
	db := x&255 - y&255
	return dr*dr + dg*dg + db*db
}

// MergeSmallRegions joins regions smaller than minRegion pixels to the
// neighbour they share the longest border with (the closer colour on a tie).
func MergeSmallRegions(ctx context.Context, labels []byte, palette []int, width, height, minRegion int) error {
	if minRegion <= 1 {
		return nil
	}
	n := width * height
	region := make([]int32, n)
	for i := range region {
		region[i] = -1
	}
	var regionLabel []byte
	var regionArea []int
	var regionSeed []int
	var stack []int
	for start := 0; start < n; start++ {
		if region[start] >= 0 {
			continue
		}
		if start&0xFFFF == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		id := int32(len(regionLabel))
		label := labels[start]
		regionLabel = append(regionLabel, label)
		regionSeed = append(regionSeed, start)
		area := 0
		region[start] = id
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			area++
			x, y := p%width, p/width
			if x > 0 && region[p-1] < 0 && labels[p-1] == label {
				region[p-1] = id
				stack = append(stack, p-1)
			}
			if x < width-1 && region[p+1] < 0 && labels[p+1] == label {
				region[p+1] = id
				stack = append(stack, p+1)
			}
			if y > 0 && region[p-width] < 0 && labels[p-width] == label {
				region[p-width] = id
				stack = append(stack, p-width)
			}
			if y < height-1 && region[p+width] < 0 && labels[p+width] == label {
				region[p+width] = id
				stack = append(stack, p+width)
			}
		}
		regionArea = append(regionArea, area)
	}
	count := len(regionLabel)
	parent := make([]int, count)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	var small []int
	for i := 0; i < count; i++ {
		if regionArea[i] < minRegion {
			small = append(small, i)
		}
	}
	sort.SliceStable(small, func(a, b int) bool { return regionArea[small[a]] < regionArea[small[b]] })
	labelDistance := func(a, b byte) int {
		if a == Transparent || b == Transparent {
			return math.MaxInt
		}
		return colorDistance(palette[a], palette[b])
	}
	shared := map[int]int{}
	var order []int
	seen := map[int]bool{}
	for _, id := range small {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		root := find(id)
		if regionArea[root] >= minRegion {
			continue
		}
		clear(shared)
		clear(seen)
		order = order[:0]
		stack = append(stack[:0], regionSeed[root])
		seen[regionSeed[root]] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%width, p/width
			neighbours := [4]int{-1, -1, -1, -1}
			if x > 0 {
				neighbours[0] = p - 1
			}
			if x < width-1 {
				neighbours[1] = p + 1
			}
			if y > 0 {
				neighbours[2] = p - width
			}
			if y < height-1 {
				neighbours[3] = p + width
			}
			for _, q := range neighbours {
				if q < 0 {
					continue
				}
				other := find(int(region[q]))
				if other == root {
					if !seen[q] {
						seen[q] = true
						stack = append(stack, q)
					}
				} else {
					if _, ok := shared[other]; !ok {
						order = append(order, other)
					}
					shared[other]++
				}
			}
		}
		target := -1
		// Visit neighbours in the order met, so ties resolve the same way every run.
		for _, other := range order {
			length := shared[other]
			if target < 0 || length > shared[target] ||
				(length == shared[target] && labelDistance(regionLabel[root], regionLabel[other]) < labelDistance(regionLabel[root], regionLabel[target])) {
				target = other
			}
		}
		if target < 0 {
			continue
		}
		parent[root] = target
		regionArea[target] += regionArea[root]
	}
	for p := 0; p < n; p++ {
		labels[p] = regionLabel[find(int(region[p]))]
	}
	return nil
}
