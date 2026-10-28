package gfx

import (
	"image"
	"image/draw"
	"math"
)

// blend composites a premultiplied source (0–1) over a pixel.
func blend(px []uint8, sr, sg, sb, sa float32) {
	if sa <= 0 {
		return
	}
	k := 1 - sa
	px[0] = clamp8(sr*255 + float32(px[0])*k)
	px[1] = clamp8(sg*255 + float32(px[1])*k)
	px[2] = clamp8(sb*255 + float32(px[2])*k)
	px[3] = clamp8(sa*255 + float32(px[3])*k)
}

func clamp8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// source gives the premultiplied colour of a paint at a pixel.
type source struct {
	solid      [4]float32
	grad       *Gradient
	inv        Matrix
	lut        [256][4]float32
	radialPrep radialPrep
}

func newSource(paint Paint, m Matrix) *source {
	if paint.Gradient == nil {
		c := paint.Color
		a := float32(clamp01(c.A))
		return &source{solid: [4]float32{float32(clamp01(c.R)) * a, float32(clamp01(c.G)) * a, float32(clamp01(c.B)) * a, a}}
	}
	g := paint.Gradient
	inv, ok := m.Mul(g.Matrix).Invert()
	if !ok {
		return nil
	}
	s := &source{grad: g, inv: inv}
	for i := range s.lut {
		c := g.colorAt(float64(i) / 255)
		a := float32(clamp01(c.A))
		s.lut[i] = [4]float32{float32(clamp01(c.R)) * a, float32(clamp01(c.G)) * a, float32(clamp01(c.B)) * a, a}
	}
	if g.Radial {
		s.radialPrep = prepRadial(g)
	}
	return s
}

func (s *source) at(x, y int) (float32, float32, float32, float32) {
	if s.grad == nil {
		return s.solid[0], s.solid[1], s.solid[2], s.solid[3]
	}
	p := s.inv.Apply(Point{float64(x) + 0.5, float64(y) + 0.5})
	t, ok := s.grad.param(p, &s.radialPrep)
	if !ok {
		return 0, 0, 0, 0
	}
	t = applySpread(t, s.grad.Spread)
	i := int(t*255 + 0.5)
	if i < 0 {
		i = 0
	} else if i > 255 {
		i = 255
	}
	c := s.lut[i]
	return c[0], c[1], c[2], c[3]
}

func applySpread(t float64, sp Spread) float64 {
	switch sp {
	case SpreadRepeat:
		t -= math.Floor(t)
	case SpreadReflect:
		t = math.Mod(math.Abs(t), 2)
		if t > 1 {
			t = 2 - t
		}
	}
	return clamp01(t)
}

// colorAt interpolates the stops (straight colour, sRGB).
func (g *Gradient) colorAt(t float64) Color {
	st := g.Stops
	if len(st) == 0 {
		return Transparent
	}
	if t <= st[0].Offset {
		return st[0].Color
	}
	for i := 1; i < len(st); i++ {
		if t <= st[i].Offset {
			a, b := st[i-1], st[i]
			span := b.Offset - a.Offset
			if span <= 0 {
				return b.Color
			}
			f := (t - a.Offset) / span
			return Color{a.Color.R + (b.Color.R-a.Color.R)*f, a.Color.G + (b.Color.G-a.Color.G)*f,
				a.Color.B + (b.Color.B-a.Color.B)*f, a.Color.A + (b.Color.A-a.Color.A)*f}
		}
	}
	return st[len(st)-1].Color
}

type radialPrep struct {
	cdx, cdy, dr, a float64
}

func prepRadial(g *Gradient) radialPrep {
	cdx, cdy := g.CX-g.FX, g.CY-g.FY
	dr := g.R - g.FR
	return radialPrep{cdx, cdy, dr, cdx*cdx + cdy*cdy - dr*dr}
}

// param is the gradient parameter t of point p (in gradient space).
func (g *Gradient) param(p Point, rp *radialPrep) (float64, bool) {
	if !g.Radial {
		dx, dy := g.X2-g.X1, g.Y2-g.Y1
		l2 := dx*dx + dy*dy
		if l2 == 0 {
			return 1, true
		}
		return ((p.X-g.X1)*dx + (p.Y-g.Y1)*dy) / l2, true
	}
	// Two-point conical gradient: the largest t whose circle passes through p
	// with a non-negative radius.
	pdx, pdy := p.X-g.FX, p.Y-g.FY
	b := pdx*rp.cdx + pdy*rp.cdy + g.FR*rp.dr
	c := pdx*pdx + pdy*pdy - g.FR*g.FR
	if math.Abs(rp.a) < 1e-12 {
		if b == 0 {
			return 0, false
		}
		t := c / (2 * b)
		return t, g.FR+t*rp.dr >= 0
	}
	d := b*b - rp.a*c
	if d < 0 {
		return 0, false
	}
	sq := math.Sqrt(d)
	t1, t2 := (b+sq)/rp.a, (b-sq)/rp.a
	if t1 < t2 {
		t1, t2 = t2, t1
	}
	if g.FR+t1*rp.dr >= 0 {
		return t1, true
	}
	if g.FR+t2*rp.dr >= 0 {
		return t2, true
	}
	return 0, false
}

func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

func bilinear(img *image.RGBA, fx, fy float64) (float32, float32, float32, float32) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := float32(fx-float64(x0)), float32(fy-float64(y0))
	var acc [4]float32
	for j := 0; j < 2; j++ {
		yy := min(max(y0+j, 0), h-1)
		wy := ty
		if j == 0 {
			wy = 1 - ty
		}
		for i := 0; i < 2; i++ {
			xx := min(max(x0+i, 0), w-1)
			wx := tx
			if i == 0 {
				wx = 1 - tx
			}
			k := wx * wy / 255
			o := yy*img.Stride + xx*4
			acc[0] += float32(img.Pix[o]) * k
			acc[1] += float32(img.Pix[o+1]) * k
			acc[2] += float32(img.Pix[o+2]) * k
			acc[3] += float32(img.Pix[o+3]) * k
		}
	}
	return acc[0], acc[1], acc[2], acc[3]
}
