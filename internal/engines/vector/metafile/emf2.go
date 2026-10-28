package metafile

import (
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// emfBlt plays BITBLT / STRETCHBLT: a bitmap, or a brush fill without one.
func (p *player) emfBlt(r emfReader, stretch bool) {
	x, y, w, h := r.i32(24), r.i32(28), r.i32(32), r.i32(36)
	rop := r.u32(40)
	xs, ys := int(r.i32(44)), int(r.i32(48))
	var offBmi, cbBmi, offBits, cbBits int
	if stretch {
		offBmi, cbBmi, offBits, cbBits = int(r.u32(84)), int(r.u32(88)), int(r.u32(92)), int(r.u32(96))
	} else {
		offBmi, cbBmi, offBits, cbBits = int(r.u32(84)), int(r.u32(88)), int(r.u32(92)), int(r.u32(96))
	}
	if cbBmi == 0 {
		p.patBlt(x, y, w, h, rop)
		return
	}
	img := dibAt(r, offBmi, cbBmi, offBits, cbBits, false)
	if img == nil {
		return
	}
	ws, hs := int(w), int(h)
	if stretch {
		ws, hs = int(r.i32(100)), int(r.i32(104))
	}
	p.drawBitmap(crop(img, xs, ys, ws, hs), x, y, w, h, rop)
}

// emfText plays EXTTEXTOUTA / W.
func (p *player) emfText(r emfReader, wide bool) {
	// EMRTEXT at 36: reference point, chars, string offset, options, rectangle, dx offset.
	x, y := r.i32(36), r.i32(40)
	n := int(r.u32(44))
	offString := int(r.u32(48))
	opts := r.u32(52)
	rect := [4]float64{r.i32(56), r.i32(60), r.i32(64), r.i32(68)}
	offDx := int(r.u32(72))
	if n <= 0 || n > 1<<16 || offString >= len(r.b) {
		return
	}
	var text string
	if wide {
		text = utf16String(r.b[offString:], n)
	} else {
		end := min(len(r.b), offString+n)
		text = decodeANSI(r.b[offString:end], p.st.font.charset)
	}
	var dx []float64
	if offDx > 0 && offDx+4*n <= len(r.b) {
		count := len([]rune(text))
		step := 4
		if opts&0x2000 != 0 { // ETO_PDY: dx/dy pairs
			step = 8
		}
		for i := 0; i < count && offDx+step*i+4 <= len(r.b); i++ {
			dx = append(dx, r.i32(offDx+step*i))
		}
	}
	var opaque *[4]float64
	if opts&0x0002 != 0 && rect[2] > rect[0] { // ETO_OPAQUE
		opaque = &rect
	}
	clipped := opts&0x0004 != 0 && rect[2] > rect[0] // ETO_CLIPPED
	if clipped {
		saved := p.st.clip
		p.st.clip = append(append([]clipItem(nil), p.st.clip...), clipItem{shapes: []gfx.ClipShape{{Path: rectPath(rect[0], rect[1], rect[2], rect[3]), M: p.logical(), Rule: gfx.NonZero}}})
		p.drawText(x, y, text, dx, opaque)
		p.st.clip = saved
		return
	}
	p.drawText(x, y, text, dx, opaque)
}

// gradientFill plays GRADIENTFILL: rectangles as linear gradients, triangles
// in their average colour.
func (p *player) gradientFill(r emfReader) {
	nVer := int(r.u32(24))
	nTri := int(r.u32(28))
	mode := r.u32(32)
	if nVer > 1<<16 || nTri > 1<<16 {
		return
	}
	type vertex struct {
		x, y float64
		c    gfx.Color
	}
	vs := make([]vertex, nVer)
	for i := range vs {
		o := 36 + 16*i
		c := gfx.Color{R: float64(r.u32(o+8)&0xFFFF) / 65535, G: float64(r.u32(o+8)>>16) / 65535,
			B: float64(r.u32(o+12)&0xFFFF) / 65535, A: 1}
		vs[i] = vertex{r.i32(o), r.i32(o + 4), c}
	}
	o := 36 + 16*nVer
	p.syncClip()
	m := p.logical()
	for i := 0; i < nTri; i++ {
		if mode == 2 { // triangle
			a, b, c := int(r.u32(o+12*i)), int(r.u32(o+12*i+4)), int(r.u32(o+12*i+8))
			if a >= nVer || b >= nVer || c >= nVer {
				continue
			}
			path := polyPath([]gfx.Point{{X: vs[a].x, Y: vs[a].y}, {X: vs[b].x, Y: vs[b].y}, {X: vs[c].x, Y: vs[c].y}}, true)
			avg := gfx.Color{R: (vs[a].c.R + vs[b].c.R + vs[c].c.R) / 3, G: (vs[a].c.G + vs[b].c.G + vs[c].c.G) / 3, B: (vs[a].c.B + vs[b].c.B + vs[c].c.B) / 3, A: 1}
			p.dev.Fill(path, m, gfx.NonZero, gfx.Solid(avg))
			continue
		}
		ul, lr := int(r.u32(o+8*i)), int(r.u32(o+8*i+4))
		if ul >= nVer || lr >= nVer {
			continue
		}
		a, b := vs[ul], vs[lr]
		g := &gfx.Gradient{X1: a.x, Y1: a.y, X2: b.x, Y2: a.y, Stops: []gfx.Stop{{Offset: 0, Color: a.c}, {Offset: 1, Color: b.c}}, Matrix: gfx.Identity}
		if mode == 1 { // vertical
			g.X2, g.Y2 = a.x, b.y
		}
		p.dev.Fill(rectPath(a.x, a.y, b.x, b.y), m, gfx.NonZero, gfx.Paint{Gradient: g})
	}
}
