package images

import (
	"math"
	"slices"
)

// Simple background removal: turns the paper or plain backdrop of signatures,
// stamps, logos and scans transparent. No model and no network: the backdrop
// colour is estimated region by region, and every pixel is "un-mixed" from it.
//
//  1. The backdrop colour is the per-channel median of the border pixels.
//  2. A light backdrop is estimated again per block, so a shadow or a lamp's
//     falloff across a phone photo of paper is removed as well; a block only
//     counts pixels that are a shade of the border colour.
//  3. Each pixel is ink of some colour over the backdrop with some opacity
//     (as GIMP's "Color to Alpha"); anti-aliased edges stay smooth. Only a
//     blend of backdrop and ink is un-mixed: artwork in colours of its own
//     stays solid.
//  4. Lone visible pixels (noise, paper grain) are cleared.
//
// Optionally only the backdrop reachable from the image edge is removed, so
// the white inside a logo stays.

const (
	bgMaxCutoff   = 0.48
	bgMinSoftBand = 0.15
	bgMinHeadroom = 96.0
	bgMinSpeck    = 32
	bgBlendDrop   = 10
	bgInkMatch    = 48
	bgNeutralInk  = 24
	bgInkBins     = 16
)

type rgbF struct{ R, G, B float32 }

// RemoveBackground makes the plain background of the frame transparent.
// tolerance 0–100: how different from the backdrop a pixel may be and still
// be removed; keepEnclosed removes only the backdrop connected to the edge.
func RemoveBackground(f *Frame, tolerance int, keepEnclosed bool) {
	img := f.Img
	width, height := img.Rect.Dx(), img.Rect.Dy()
	if width < 2 || height < 2 {
		return
	}
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		copy(pixels[y*width*4:(y+1)*width*4], img.Pix[y*img.Stride:])
	}
	border, ok := bgBorderColor(pixels, width, height)
	if !ok {
		return // the edge is mostly transparent already
	}
	tol := max(0, min(100, tolerance))
	cutoff := float32(tol) / 100 * bgMaxCutoff
	backdrop := newBackdrop(pixels, width, height, border, float32(tolerance)/100)

	raw := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			o := (y*width + x) * 4
			if pixels[o+3] < 128 {
				continue
			}
			b := backdrop.at(x, y)
			op := max(bgOpacity(pixels[o], b.R), bgOpacity(pixels[o+1], b.G), bgOpacity(pixels[o+2], b.B))
			raw[y*width+x] = byte(math.Round(float64(min(op, 1) * 255)))
		}
	}
	full := bgInkOpacity(raw, cutoff)
	distinct := min(1, cutoff+bgMinSoftBand)
	var enclosed []bool
	if keepEnclosed {
		enclosed = bgEnclosed(raw, width, height, (cutoff+full)/2)
	}
	blends := bgBlends(pixels, raw, width, backdrop, cutoff, full)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			if enclosed != nil && enclosed[i] {
				continue
			}
			o := i * 4
			alpha := clamp01((float32(raw[i])/255 - cutoff) / (full - cutoff))
			if alpha > 0 {
				b := backdrop.at(x, y)
				if alpha < 1 && !blends[i] {
					alpha = clamp01((float32(raw[i])/255 - cutoff) / (distinct - cutoff))
				}
				if alpha < 1 {
					pixels[o] = bgUnmix(pixels[o], b.R, alpha)
					pixels[o+1] = bgUnmix(pixels[o+1], b.G, alpha)
					pixels[o+2] = bgUnmix(pixels[o+2], b.B, alpha)
				}
			}
			pixels[o+3] = byte(math.Round(float64(alpha * float32(pixels[o+3]))))
		}
	}
	if width >= bgMinSpeck && height >= bgMinSpeck {
		bgRemoveSpecks(pixels, width, height)
	}
	for y := 0; y < height; y++ {
		copy(img.Pix[y*img.Stride:], pixels[y*width*4:(y+1)*width*4])
	}
	f.Alpha = true
}

func clamp01(v float32) float32 { return max(0, min(1, v)) }

// bgInkOpacity is the raw opacity that counts as fully opaque: the 75th
// percentile of the clearly visible pixels.
func bgInkOpacity(raw []byte, cutoff float32) float32 {
	floor := min(1, cutoff+bgMinSoftBand)
	var hist [256]int
	total := 0
	for _, a := range raw {
		if float32(a)/255 < floor {
			continue
		}
		hist[a]++
		total++
	}
	if total == 0 {
		return floor
	}
	seen := 0
	for bin, n := range hist {
		seen += n
		if seen*4 >= total*3 {
			return max(floor, float32(bin)/255)
		}
	}
	return 1
}

type rgb8 struct{ R, G, B byte }

// bgBlends marks the partly opaque pixels that are a blend of backdrop and an
// ink (which are un-mixed); the others are artwork in a colour of its own.
func bgBlends(pixels, raw []byte, width int, backdrop *bgBackdrop, cutoff, full float32) []bool {
	removed := byte(max(0, min(255, math.Floor(float64(cutoff*255)))))
	solid := byte(max(0, min(255, math.Ceil(float64(full*255)))))
	ink := func(i int) rgb8 {
		o := i * 4
		if raw[i] >= solid {
			return rgb8{pixels[o], pixels[o+1], pixels[o+2]}
		}
		alpha := (float32(raw[i])/255 - cutoff) / (full - cutoff)
		b := backdrop.at(i%width, i/width)
		return rgb8{bgUnmix(pixels[o], b.R, alpha), bgUnmix(pixels[o+1], b.G, alpha), bgUnmix(pixels[o+2], b.B, alpha)}
	}
	near := func(a, b rgb8) bool {
		return max(abs(int(a.R)-int(b.R)), abs(int(a.G)-int(b.G)), abs(int(a.B)-int(b.B))) <= bgInkMatch
	}
	fromBackdrop := bgFlood(len(raw), width, func(i int) bool { return raw[i] <= removed },
		func(_, from, i int) bool {
			return raw[i] > removed && raw[i] < solid && int(raw[i])+bgBlendDrop >= int(raw[from])
		})
	blends := bgFlood(len(raw), width, func(i int) bool { return raw[i] >= solid },
		func(origin, from, i int) bool {
			return fromBackdrop[i] && int(raw[i]) <= int(raw[from])+bgBlendDrop && near(ink(origin), ink(i))
		})
	inks := bgInkColours(pixels, raw, solid)
	for i := range raw {
		if !fromBackdrop[i] || blends[i] {
			continue
		}
		c := ink(i)
		blends[i] = int(max(c.R, c.G, c.B))-int(min(c.R, c.G, c.B)) <= bgNeutralInk && inks[bgBin(c.R, c.G, c.B)]
	}
	return blends
}

// bgFlood is the set of pixels reached from the seed pixels over 4-connected
// steps that step(origin seed, from, to) allows.
func bgFlood(count, width int, seed func(int) bool, step func(origin, from, to int) bool) []bool {
	reached := make([]bool, count)
	type item struct{ pixel, seed int }
	var queue []item
	visit := func(origin, from, i int) {
		if !reached[i] && step(origin, from, i) {
			reached[i] = true
			queue = append(queue, item{i, origin})
		}
	}
	spread := func(from, origin int) {
		x := from % width
		if x > 0 {
			visit(origin, from, from-1)
		}
		if x < width-1 {
			visit(origin, from, from+1)
		}
		if from >= width {
			visit(origin, from, from-width)
		}
		if from < count-width {
			visit(origin, from, from+width)
		}
	}
	for i := 0; i < count; i++ {
		if seed(i) {
			spread(i, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		spread(queue[head].pixel, queue[head].seed)
	}
	return reached
}

// bgInkColours are the colours of the solid ink in bins of 16 levels, widened by one bin.
func bgInkColours(pixels, raw []byte, solid byte) []bool {
	const n = bgInkBins
	counts := make([]int, n*n*n)
	total := 0
	for i := range raw {
		if raw[i] < solid {
			continue
		}
		o := i * 4
		counts[bgBin(pixels[o], pixels[o+1], pixels[o+2])]++
		total++
	}
	least := max(2, total/500)
	inks := make([]bool, len(counts))
	for r := 0; r < n; r++ {
		for g := 0; g < n; g++ {
			for b := 0; b < n; b++ {
				if counts[(r*n+g)*n+b] < least {
					continue
				}
				for nr := max(0, r-1); nr <= min(n-1, r+1); nr++ {
					for ng := max(0, g-1); ng <= min(n-1, g+1); ng++ {
						for nb := max(0, b-1); nb <= min(n-1, b+1); nb++ {
							inks[(nr*n+ng)*n+nb] = true
						}
					}
				}
			}
		}
	}
	return inks
}

func bgBin(r, g, b byte) int { return (int(r>>4)*bgInkBins+int(g>>4))*bgInkBins + int(b>>4) }

// bgOpacity is how opaque ink of value over backdrop must at least be.
func bgOpacity(value byte, backdrop float32) float32 {
	d := float32(value) - backdrop
	if d >= 0 {
		return d / max(255-backdrop, bgMinHeadroom)
	}
	return -d / max(backdrop, bgMinHeadroom)
}

func bgUnmix(value byte, backdrop, alpha float32) byte {
	return byte(max(0, min(255, math.Round(float64(backdrop+(float32(value)-backdrop)/alpha)))))
}

// bgBorderColor is the per-channel median of the opaque border pixels; ok is
// false when most of the border is transparent.
func bgBorderColor(pixels []byte, width, height int) (rgbF, bool) {
	var r, g, b []byte
	all := 0
	add := func(x, y int) {
		all++
		o := (y*width + x) * 4
		if pixels[o+3] < 128 {
			return
		}
		r, g, b = append(r, pixels[o]), append(g, pixels[o+1]), append(b, pixels[o+2])
	}
	for x := 0; x < width; x++ {
		add(x, 0)
		add(x, height-1)
	}
	for y := 1; y < height-1; y++ {
		add(0, y)
		add(width-1, y)
	}
	if len(r)*2 < all {
		return rgbF{}, false
	}
	return rgbF{bgMedian(r), bgMedian(g), bgMedian(b)}, true
}

func bgMedian(v []byte) float32 {
	slices.Sort(v)
	return float32(v[len(v)/2])
}

// bgRemoveSpecks clears visible pixels with at most one visible neighbour.
func bgRemoveSpecks(px []byte, width, height int) {
	var specks []int
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if px[(y*width+x)*4+3] == 0 {
				continue
			}
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if (dx != 0 || dy != 0) && nx >= 0 && nx < width && ny >= 0 && ny < height && px[(ny*width+nx)*4+3] != 0 {
						n++
					}
				}
			}
			if n <= 1 {
				specks = append(specks, y*width+x)
			}
		}
	}
	for _, i := range specks {
		px[i*4+3] = 0
	}
}

// bgEnclosed is the backdrop inside closed shapes: backdrop the image edge
// cannot reach through pixels less than wall opaque.
func bgEnclosed(raw []byte, width, height int, wall float32) []bool {
	limit := byte(max(0, min(255, math.Round(float64(wall*255)))))
	reached := make([]bool, len(raw))
	var queue []int
	visit := func(i int) {
		if !reached[i] && raw[i] < limit {
			reached[i] = true
			queue = append(queue, i)
		}
	}
	for x := 0; x < width; x++ {
		visit(x)
		visit((height-1)*width + x)
	}
	for y := 0; y < height; y++ {
		visit(y * width)
		visit(y*width + width - 1)
	}
	for head := 0; head < len(queue); head++ {
		i := queue[head]
		x := i % width
		if x > 0 {
			visit(i - 1)
		}
		if x < width-1 {
			visit(i + 1)
		}
		if i >= width {
			visit(i - width)
		}
		if i < len(raw)-width {
			visit(i + width)
		}
	}
	for i := range raw {
		reached[i] = !reached[i] && raw[i] < limit
	}
	return reached
}

// bgBackdrop is the backdrop colour at each pixel: the border colour,
// corrected block by block for uneven lighting when the backdrop is light.
type bgBackdrop struct {
	border              rgbF
	grid                []rgbF
	block, cols, rows   int
	darkest             float32
}

const bgBlocks = 48

func newBackdrop(pixels []byte, width, height int, border rgbF, tolerance float32) *bgBackdrop {
	b := &bgBackdrop{border: border}
	if 0.299*border.R+0.587*border.G+0.114*border.B < 96 {
		return b
	}
	b.block = max(8, int(math.Ceil(float64(max(width, height))/bgBlocks)))
	b.cols = (width + b.block - 1) / b.block
	b.rows = (height + b.block - 1) / b.block
	if b.cols < 2 && b.rows < 2 {
		return b
	}
	b.darkest = 0.92 - 0.30*clamp01(tolerance)
	b.grid = b.estimate(pixels, width, height)
	return b
}

func (b *bgBackdrop) at(x, y int) rgbF {
	if b.grid == nil {
		return b.border
	}
	fx := max(0, min(float32(b.cols-1), (float32(x)+0.5)/float32(b.block)-0.5))
	fy := max(0, min(float32(b.rows-1), (float32(y)+0.5)/float32(b.block)-0.5))
	x0, y0 := int(fx), int(fy)
	x1, y1 := min(x0+1, b.cols-1), min(y0+1, b.rows-1)
	tx, ty := fx-float32(x0), fy-float32(y0)
	p, q, s, t := b.grid[y0*b.cols+x0], b.grid[y0*b.cols+x1], b.grid[y1*b.cols+x0], b.grid[y1*b.cols+x1]
	mix := func(p, q, s, t float32) float32 { return (p*(1-tx)+q*tx)*(1-ty) + (s*(1-tx)+t*tx)*ty }
	return rgbF{mix(p.R, q.R, s.R, t.R), mix(p.G, q.G, s.G, t.G), mix(p.B, q.B, s.B, t.B)}
}

func (b *bgBackdrop) estimate(pixels []byte, width, height int) []rgbF {
	grid := make([]*rgbF, b.cols*b.rows)
	bo := b.border
	dot := bo.R*bo.R + bo.G*bo.G + bo.B*bo.B
	var r, g, bl []byte
	for row := 0; row < b.rows; row++ {
		for col := 0; col < b.cols; col++ {
			r, g, bl = r[:0], g[:0], bl[:0]
			total := 0
			yEnd, xEnd := min(height, (row+1)*b.block), min(width, (col+1)*b.block)
			for y := row * b.block; y < yEnd; y++ {
				for x := col * b.block; x < xEnd; x++ {
					total++
					o := (y*width + x) * 4
					if pixels[o+3] < 128 {
						continue
					}
					pr, pg, pb := float32(pixels[o]), float32(pixels[o+1]), float32(pixels[o+2])
					k := (pr*bo.R + pg*bo.G + pb*bo.B) / dot
					if k < b.darkest || k > 1.6 {
						continue
					}
					off := max(float32(math.Abs(float64(pr-k*bo.R))), float32(math.Abs(float64(pg-k*bo.G))), float32(math.Abs(float64(pb-k*bo.B))))
					if off > 20 {
						continue
					}
					r, g, bl = append(r, pixels[o]), append(g, pixels[o+1]), append(bl, pixels[o+2])
				}
			}
			if len(r)*5 >= total*2 {
				c := rgbF{bgMedian(r), bgMedian(g), bgMedian(bl)}
				grid[row*b.cols+col] = &c
			}
		}
	}
	return b.smooth(b.fill(grid))
}

func (b *bgBackdrop) neighbours(i int) []int {
	col, row := i%b.cols, i/b.cols
	var out []int
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			c, r := col+dx, row+dy
			if (dx != 0 || dy != 0) && c >= 0 && c < b.cols && r >= 0 && r < b.rows {
				out = append(out, r*b.cols+c)
			}
		}
	}
	return out
}

// fill gives blocks without an estimate the average of their estimated neighbours, ring by ring.
func (b *bgBackdrop) fill(grid []*rgbF) []rgbF {
	known := 0
	for _, c := range grid {
		if c != nil {
			known++
		}
	}
	out := make([]rgbF, len(grid))
	if known == 0 {
		for i := range out {
			out[i] = b.border
		}
		return out
	}
	for known < len(grid) {
		next := slices.Clone(grid)
		for i, c := range grid {
			if c != nil {
				continue
			}
			var s rgbF
			n := 0
			for _, j := range b.neighbours(i) {
				if grid[j] != nil {
					s.R, s.G, s.B = s.R+grid[j].R, s.G+grid[j].G, s.B+grid[j].B
					n++
				}
			}
			if n > 0 {
				v := rgbF{s.R / float32(n), s.G / float32(n), s.B / float32(n)}
				next[i] = &v
				known++
			}
		}
		grid = next
	}
	for i, c := range grid {
		out[i] = *c
	}
	return out
}

// smooth is one 3 × 3 box blur of the grid.
func (b *bgBackdrop) smooth(grid []rgbF) []rgbF {
	out := make([]rgbF, len(grid))
	for i := range grid {
		all := append(b.neighbours(i), i)
		var s rgbF
		for _, j := range all {
			s.R, s.G, s.B = s.R+grid[j].R, s.G+grid[j].G, s.B+grid[j].B
		}
		n := float32(len(all))
		out[i] = rgbF{s.R / n, s.G / n, s.B / n}
	}
	return out
}
