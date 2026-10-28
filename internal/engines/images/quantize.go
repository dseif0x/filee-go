package images

import (
	"image"
	"image/color"
	"slices"

	"golang.org/x/image/draw"
)

// quantize maps img to a palette of at most n colours. A picture with that
// few colours keeps them exactly; others get a median-cut palette and
// Floyd–Steinberg dithering. Pixels less than half opaque become the
// transparent entry 0 (the GIF way: one fully transparent colour).
func quantize(img *image.NRGBA, n int) *image.Paletted {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	transparent := false
	unique := map[uint32]struct{}{}
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for i := 0; i < len(row); i += 4 {
			if row[i+3] < 128 {
				transparent = true
				continue
			}
			if len(unique) <= n {
				unique[uint32(row[i])<<16|uint32(row[i+1])<<8|uint32(row[i+2])] = struct{}{}
			}
		}
	}
	slots := n
	if transparent {
		slots--
	}
	var palette color.Palette
	if transparent {
		palette = append(palette, color.NRGBA{})
	}
	exact := len(unique) <= slots
	if exact {
		keys := make([]uint32, 0, len(unique))
		for k := range unique {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			palette = append(palette, color.NRGBA{uint8(k >> 16), uint8(k >> 8), uint8(k), 255})
		}
	} else {
		palette = append(palette, medianCut(img, slots)...)
	}
	if len(palette) == 0 {
		palette = append(palette, color.NRGBA{0, 0, 0, 255})
	}
	out := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	if exact {
		index := map[uint32]uint8{}
		for i, c := range palette {
			nc := c.(color.NRGBA)
			if nc.A == 255 {
				index[uint32(nc.R)<<16|uint32(nc.G)<<8|uint32(nc.B)] = uint8(i)
			}
		}
		for y := 0; y < h; y++ {
			row := img.Pix[y*img.Stride:]
			for x := 0; x < w; x++ {
				p := row[x*4 : x*4+4]
				if p[3] < 128 {
					out.Pix[y*out.Stride+x] = 0
				} else {
					out.Pix[y*out.Stride+x] = index[uint32(p[0])<<16|uint32(p[1])<<8|uint32(p[2])]
				}
			}
		}
		return out
	}
	// Dither the opaque colours against the opaque entries only.
	opaquePal := palette
	if transparent {
		opaquePal = palette[1:]
	}
	tmp := image.NewPaletted(out.Rect, opaquePal)
	src := flatten(img, color.NRGBA{0, 0, 0, 255})
	draw.FloydSteinberg.Draw(tmp, tmp.Rect, src, image.Point{})
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			i := y*out.Stride + x
			switch {
			case row[x*4+3] < 128:
				out.Pix[i] = 0
			case transparent:
				out.Pix[i] = tmp.Pix[i] + 1
			default:
				out.Pix[i] = tmp.Pix[i]
			}
		}
	}
	return out
}

type colorBox struct {
	pixels [][3]uint8
}

func (b *colorBox) ranges() (int, int) {
	lo, hi := [3]int{255, 255, 255}, [3]int{}
	for _, p := range b.pixels {
		for c := 0; c < 3; c++ {
			lo[c], hi[c] = min(lo[c], int(p[c])), max(hi[c], int(p[c]))
		}
	}
	best, span := 0, -1
	for c := 0; c < 3; c++ {
		if hi[c]-lo[c] > span {
			best, span = c, hi[c]-lo[c]
		}
	}
	return best, span
}

// medianCut builds a palette of up to n colours from the opaque pixels.
func medianCut(img *image.NRGBA, n int) color.Palette {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	// Sample at most about a million pixels: plenty for a palette.
	step := max(1, w*h/1_000_000)
	var pixels [][3]uint8
	k := 0
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			k++
			if k%step != 0 || row[x*4+3] < 128 {
				continue
			}
			pixels = append(pixels, [3]uint8{row[x*4], row[x*4+1], row[x*4+2]})
		}
	}
	if len(pixels) == 0 || n <= 0 {
		return nil
	}
	boxes := []*colorBox{{pixels}}
	for len(boxes) < n {
		// split the box with the widest channel range times its population
		bi, bc, score := -1, 0, -1
		for i, b := range boxes {
			if len(b.pixels) < 2 {
				continue
			}
			c, span := b.ranges()
			if s := span * len(b.pixels); span > 0 && s > score {
				bi, bc, score = i, c, s
			}
		}
		if bi < 0 {
			break
		}
		b := boxes[bi]
		slices.SortFunc(b.pixels, func(p, q [3]uint8) int { return int(p[bc]) - int(q[bc]) })
		mid := len(b.pixels) / 2
		boxes[bi] = &colorBox{b.pixels[:mid]}
		boxes = append(boxes, &colorBox{b.pixels[mid:]})
	}
	pal := make(color.Palette, 0, len(boxes))
	for _, b := range boxes {
		var s [3]int
		for _, p := range b.pixels {
			for c := 0; c < 3; c++ {
				s[c] += int(p[c])
			}
		}
		m := len(b.pixels)
		pal = append(pal, color.NRGBA{uint8((s[0] + m/2) / m), uint8((s[1] + m/2) / m), uint8((s[2] + m/2) / m), 255})
	}
	return pal
}
