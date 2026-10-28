package model3d

// A small software rasteriser for 3D → picture: an isometric view from the
// front right, above, with flat shading (one light, faces lit by their
// geometric normal turned towards the viewer) and a z-buffer, rendered at
// twice the size and scaled down for smooth edges. Colours come from the
// vertex colours, else the material (its texture times its base colour), else
// grey; both are interpolated across each triangle, which is exact for a
// parallel projection.

import (
	"bytes"
	"context"
	"image"
	"math"

	xdraw "golang.org/x/image/draw"
)

var defaultRenderColor = vec4{0.72, 0.74, 0.78, 1}

type picture struct {
	pix           []vec3
	width, height int
}

type paint struct {
	a, b, c       vec3
	texture       *picture
	uvA, uvB, uvC vec2
}

// renderModel renders m into a square picture of size pixels with a transparent background.
func renderModel(ctx context.Context, m *Model, size int) (*image.NRGBA, error) {
	const scale = 2
	full := size * scale
	pixels := image.NewRGBA(image.Rect(0, 0, full, full))
	depth := make([]float32, full*full)
	for i := range depth {
		depth[i] = float32(math.Inf(-1))
	}

	// Camera at (1, 1, 1) looking at the origin, Y up: the classic isometric
	// view. The light comes from the upper left, a little from the front, so
	// the top, left and right faces of a box differ.
	forward := vec3{-1, -1, -1}.normalize()
	right := forward.cross(vec3{0, 1, 0}).normalize()
	up := right.cross(forward)
	light := right.scale(-0.5).add(up.scale(0.8)).sub(forward.scale(0.6)).normalize()

	// Fit the projected bounds into the picture with a margin.
	minX, maxX := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	minY, maxY := minX, maxX
	for _, p := range m.Parts {
		for v := 0; v < p.VertexCount(); v++ {
			q := p.Position(v)
			x, y := q.dot(right), q.dot(up)
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	extent := max(maxX-minX, maxY-minY)
	pixelsPerUnit := float32(1)
	if extent > 0 && finite(extent) {
		pixelsPerUnit = float32(full) * 0.9 / extent
	}
	centerX, centerY := (minX+maxX)/2, (minY+maxY)/2
	project := func(q vec3) vec3 {
		return vec3{
			float32(full)/2 + (q.dot(right)-centerX)*pixelsPerUnit,
			float32(full)/2 - (q.dot(up)-centerY)*pixelsPerUnit,
			-q.dot(forward),
		}
	}

	textures := map[*Texture]*picture{}
	for _, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		material := m.MaterialOf(p)
		var texture *picture
		if material != nil && material.BaseColorTexture != nil && p.TexCoords != nil {
			texture = decodeTexture(textures, material.BaseColorTexture)
		}
		tint := rgb(defaultRenderColor)
		if material != nil {
			tint = rgb(linearToSrgb4(material.BaseColor))
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			ia, ib, ic := p.Indices[i], p.Indices[i+1], p.Indices[i+2]
			a, b, c := p.Position(ia), p.Position(ib), p.Position(ic)
			normal := b.sub(a).cross(c.sub(a))
			if normal.lengthSquared() == 0 || !finite(normal.X) {
				continue
			}
			normal = normal.normalize()
			// Seen from behind (an open surface, or a mesh wound the other way): light the side we see.
			if normal.dot(forward) > 0 {
				normal = normal.neg()
			}
			shade := 0.45 + 0.55*max(0, normal.dot(light))
			var pt paint
			if p.Colors != nil {
				pt = paint{a: rgb(p.Color(ia)).scale(shade), b: rgb(p.Color(ib)).scale(shade), c: rgb(p.Color(ic)).scale(shade)}
			} else {
				t := tint.scale(shade)
				pt = paint{a: t, b: t, c: t, texture: texture, uvA: p.TexCoord(ia), uvB: p.TexCoord(ib), uvC: p.TexCoord(ic)}
			}
			fillTriangle(pixels, depth, full, project(a), project(b), project(c), &pt)
		}
	}

	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	// The supersampled picture is premultiplied (opaque or empty pixels), so scaling it averages edges correctly.
	xdraw.BiLinear.Scale(out, out.Bounds(), pixels, pixels.Bounds(), xdraw.Src, nil)
	return out, nil
}

func rgb(c vec4) vec3 { return vec3{c.X, c.Y, c.Z} }

func edgeFn(a, b vec3, x, y float32) float32 { return (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X) }

func pack(c vec3) (uint8, uint8, uint8) {
	q := func(v float32) uint8 { return uint8(clamp01(v)*255 + 0.5) }
	return q(c.X), q(c.Y), q(c.Z)
}

// fillTriangle fills one triangle into the z-buffered picture (larger depth = nearer).
func fillTriangle(img *image.RGBA, depth []float32, size int, a, b, c vec3, pt *paint) {
	area := edgeFn(a, b, c.X, c.Y)
	if abs32(area) < 1e-6 || !finite(area) {
		return
	}
	floor := func(v float32) int { return int(math.Floor(float64(v))) }
	ceil := func(v float32) int { return int(math.Ceil(float64(v))) }
	x0 := max(0, floor(min(a.X, b.X, c.X)))
	x1 := min(size-1, ceil(max(a.X, b.X, c.X)))
	y0 := max(0, floor(min(a.Y, b.Y, c.Y)))
	y1 := min(size-1, ceil(max(a.Y, b.Y, c.Y)))
	inverse := 1 / area
	flat := pt.texture == nil && pt.a == pt.b && pt.b == pt.c
	fr, fg, fb := pack(pt.a)
	for y := y0; y <= y1; y++ {
		py := float32(y) + 0.5
		row := y * size
		for x := x0; x <= x1; x++ {
			px := float32(x) + 0.5
			w0 := edgeFn(b, c, px, py) * inverse
			w1 := edgeFn(c, a, px, py) * inverse
			w2 := edgeFn(a, b, px, py) * inverse
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			z := w0*a.Z + w1*b.Z + w2*c.Z
			if z <= depth[row+x] {
				continue
			}
			depth[row+x] = z
			o := img.PixOffset(x, y)
			if flat {
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = fr, fg, fb, 255
				continue
			}
			color := pt.a.scale(w0).add(pt.b.scale(w1)).add(pt.c.scale(w2))
			if pt.texture != nil {
				uv := vec2{w0*pt.uvA.X + w1*pt.uvB.X + w2*pt.uvC.X, w0*pt.uvA.Y + w1*pt.uvB.Y + w2*pt.uvC.Y}
				s := sampleTexture(pt.texture, uv)
				color = vec3{color.X * s.X, color.Y * s.Y, color.Z * s.Z}
			}
			r, g, bl := pack(color)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r, g, bl, 255
		}
	}
}

func decodeTexture(cache map[*Texture]*picture, t *Texture) *picture {
	if p, ok := cache[t]; ok {
		return p
	}
	var pic *picture
	if img, _, err := image.Decode(bytes.NewReader(t.Data)); err == nil {
		b := img.Bounds()
		if b.Dx() > 0 && b.Dy() > 0 {
			pic = &picture{pix: make([]vec3, b.Dx()*b.Dy()), width: b.Dx(), height: b.Dy()}
			for y := 0; y < b.Dy(); y++ {
				for x := 0; x < b.Dx(); x++ {
					r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
					// Unpremultiplied colour, as SkiaSharp's Pixels gives it.
					if a > 0 {
						r, g, bl = r*0xFFFF/a, g*0xFFFF/a, bl*0xFFFF/a
					}
					pic.pix[y*b.Dx()+x] = vec3{float32(r>>8) / 255, float32(g>>8) / 255, float32(bl>>8) / 255}
				}
			}
		}
	}
	cache[t] = pic
	return pic
}

// sampleTexture is the texture's colour at uv (origin bottom left, repeating), nearest pixel.
func sampleTexture(t *picture, uv vec2) vec3 {
	if !finite(uv.X) || !finite(uv.Y) {
		return vec3{1, 1, 1}
	}
	u := uv.X - float32(math.Floor(float64(uv.X)))
	v := uv.Y - float32(math.Floor(float64(uv.Y)))
	x := max(0, min(t.width-1, int(u*float32(t.width))))
	y := max(0, min(t.height-1, int((1-v)*float32(t.height))))
	return t.pix[y*t.width+x]
}
