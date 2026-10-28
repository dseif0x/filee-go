package metafile

import (
	"errors"
	"image"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

const placeableKey = 0x9AC6CDD7

func parseWMFHeader(data []byte) (*Picture, error) {
	p := &Picture{data: data, wmf: true, inch: 96}
	off := 0
	if le.Uint32(data) == placeableKey {
		if len(data) < 22+18 {
			return nil, errors.New("short WMF header")
		}
		l, t := float64(int16(le.Uint16(data[6:]))), float64(int16(le.Uint16(data[8:])))
		r, b := float64(int16(le.Uint16(data[10:]))), float64(int16(le.Uint16(data[12:])))
		if in := float64(le.Uint16(data[14:])); in > 0 {
			p.inch = in
		}
		p.bounds = gfx.Rect{X0: min(l, r), Y0: min(t, b), X1: max(l, r), Y1: max(t, b)}
		off = 22
	}
	if off+18 > len(data) {
		return nil, errors.New("short WMF header")
	}
	if p.bounds.Width() <= 0 || p.bounds.Height() <= 0 {
		// No placeable header: the window the records set up.
		var org, ext gfx.Point
		hasExt := false
		walkWMF(data[off:], func(fn uint16, rec []byte) bool {
			switch fn {
			case 0x020B:
				org = gfx.Point{X: i16(rec, 2), Y: i16(rec, 0)}
			case 0x020C:
				ext = gfx.Point{X: i16(rec, 2), Y: i16(rec, 0)}
				hasExt = true
				return false
			}
			return true
		})
		if !hasExt || ext.X == 0 || ext.Y == 0 {
			// No window either: the extent of what is drawn.
			p.data = data[off:]
			p.bounds = gfx.Rect{X1: 1, Y1: 1}
			bd := &boundsDevice{r: gfx.EmptyRect()}
			p.playWMF(bd, gfx.Identity, gfx.NewFonts(false))
			if bd.r.Empty() || bd.r.Width() <= 0 || bd.r.Height() <= 0 {
				return nil, errors.New("the WMF has no size (no placeable header, no window, nothing drawn)")
			}
			p.bounds = bd.r
			p.Width, p.Height = p.bounds.Width(), p.bounds.Height()
			return p, nil
		}
		x0, y0 := org.X, org.Y
		x1, y1 := org.X+ext.X, org.Y+ext.Y
		p.bounds = gfx.Rect{X0: min(x0, x1), Y0: min(y0, y1), X1: max(x0, x1), Y1: max(y0, y1)}
		if ext.X > 2000 || ext.Y > 2000 {
			// Large windows without a placeable header are usually in twips or 0.01 mm.
			p.inch = 1440
		}
	}
	p.Width = p.bounds.Width() * 96 / p.inch
	p.Height = p.bounds.Height() * 96 / p.inch
	p.data = data[off:]
	return p, nil
}

func i16(b []byte, o int) float64 {
	if o+2 > len(b) {
		return 0
	}
	return float64(int16(le.Uint16(b[o:])))
}

func u16(b []byte, o int) uint16 {
	if o+2 > len(b) {
		return 0
	}
	return le.Uint16(b[o:])
}

func u32(b []byte, o int) uint32 {
	if o+4 > len(b) {
		return 0
	}
	return le.Uint32(b[o:])
}

// walkWMF calls fn for each record's function and parameters until it returns false.
func walkWMF(data []byte, fn func(uint16, []byte) bool) {
	if len(data) < 18 {
		return
	}
	hdr := int(le.Uint16(data[2:])) * 2
	for off := max(hdr, 18); off+6 <= len(data); {
		size := int(le.Uint32(data[off:])) * 2
		f := le.Uint16(data[off+4:])
		if size < 6 || off+size > len(data) {
			break
		}
		if f == 0 || !fn(f, data[off+6:off+size]) {
			break
		}
		off += size
	}
}

func (pic *Picture) playWMF(dev gfx.Device, m gfx.Matrix, fonts *gfx.Fonts) {
	k := 96 / pic.inch
	toOut := m.Mul(gfx.Scale(k, k)).Mul(gfx.Translate(-pic.bounds.X0, -pic.bounds.Y0))
	p := newPlayer(dev, toOut, gfx.Point{X: pic.inch / 25.4, Y: pic.inch / 25.4}, fonts)
	p.isWMF = true
	viewportSet := false
	walkWMF(pic.data, func(fn uint16, b []byte) bool {
		p.wmfRecord(fn, b, pic, &viewportSet)
		return true
	})
	p.finish()
}

// addObject puts an object in the lowest free slot of the table.
func (p *player) addObject(o *object) {
	for i, x := range p.objects {
		if x == nil {
			p.objects[i] = o
			return
		}
	}
	p.objects = append(p.objects, o)
}

func wmfPoints(b []byte, o, n int) []gfx.Point {
	pts := make([]gfx.Point, 0, n)
	for i := 0; i < n && o+4*i+4 <= len(b); i++ {
		pts = append(pts, gfx.Point{X: i16(b, o+4*i), Y: i16(b, o+4*i+2)})
	}
	return pts
}

func (p *player) wmfRecord(fn uint16, b []byte, pic *Picture, viewportSet *bool) {
	s := &p.st
	switch fn {
	case 0x0201:
		s.bkColor = colorRef(u32(b, 0))
	case 0x0209:
		s.textColor = colorRef(u32(b, 0))
	case 0x0102:
		s.bkMode = uint32(u16(b, 0))
	case 0x0103:
		s.mapMode = uint32(u16(b, 0))
	case 0x0104:
		s.rop2 = uint32(u16(b, 0))
	case 0x0106:
		if u16(b, 0) == 2 {
			s.polyFill = gfx.NonZero
		} else {
			s.polyFill = gfx.EvenOdd
		}
	case 0x0108:
		s.charExtra = i16(b, 0)
	case 0x012E:
		s.textAlign = uint32(u16(b, 0))
	case 0x020B:
		s.winOrg = gfx.Point{X: i16(b, 2), Y: i16(b, 0)}
		p.wmfWindowMapping(pic, *viewportSet)
	case 0x020C:
		s.winExt = gfx.Point{X: i16(b, 2), Y: i16(b, 0)}
		p.wmfWindowMapping(pic, *viewportSet)
	case 0x020D:
		s.viewOrg = gfx.Point{X: i16(b, 2), Y: i16(b, 0)}
		*viewportSet = true
	case 0x020E:
		s.viewExt = gfx.Point{X: i16(b, 2), Y: i16(b, 0)}
		*viewportSet = true
	case 0x020F:
		s.winOrg.X += i16(b, 2)
		s.winOrg.Y += i16(b, 0)
	case 0x0211:
		s.viewOrg.X += i16(b, 2)
		s.viewOrg.Y += i16(b, 0)
	case 0x0410, 0x0412: // SCALEWINDOWEXT, SCALEVIEWPORTEXT: yDenom, yNum, xDenom, xNum
		yd, yn, xd, xn := i16(b, 0), i16(b, 2), i16(b, 4), i16(b, 6)
		if xd != 0 && yd != 0 {
			if fn == 0x0410 {
				s.winExt.X, s.winExt.Y = s.winExt.X*xn/xd, s.winExt.Y*yn/yd
			} else {
				s.viewExt.X, s.viewExt.Y = s.viewExt.X*xn/xd, s.viewExt.Y*yn/yd
			}
		}
	case 0x0214:
		s.cur = gfx.Point{X: i16(b, 2), Y: i16(b, 0)}
	case 0x0213:
		p.lineTo([]gfx.Point{{X: i16(b, 2), Y: i16(b, 0)}})
	case 0x041B:
		p.drawShape(rectPath(i16(b, 6), i16(b, 4), i16(b, 2), i16(b, 0)), true)
	case 0x0418:
		p.drawShape(ellipsePath(i16(b, 6), i16(b, 4), i16(b, 2), i16(b, 0)), true)
	case 0x061C:
		p.drawShape(roundRectPath(i16(b, 10), i16(b, 8), i16(b, 6), i16(b, 4), i16(b, 2), i16(b, 0)), true)
	case 0x0817, 0x081A, 0x0830: // ARC, PIE, CHORD
		kind := map[uint16]int{0x0817: 0, 0x081A: 2, 0x0830: 1}[fn]
		path := arcPath(i16(b, 14), i16(b, 12), i16(b, 10), i16(b, 8), i16(b, 6), i16(b, 4), i16(b, 2), i16(b, 0), false, kind, false, s.cur)
		if kind == 0 {
			p.strokeOnly(path)
		} else {
			p.drawShape(path, true)
		}
	case 0x0324:
		p.drawShape(polyPath(wmfPoints(b, 2, int(u16(b, 0))), true), true)
	case 0x0325:
		p.strokeOnly(polyPath(wmfPoints(b, 2, int(u16(b, 0))), false))
	case 0x0538: // POLYPOLYGON
		n := int(u16(b, 0))
		o := 2 + 2*n
		path := gfx.NewPath()
		for i := 0; i < n; i++ {
			c := int(u16(b, 2+2*i))
			path.Append(polyPath(wmfPoints(b, o, c), true))
			o += 4 * c
		}
		p.drawShape(path, true)
	case 0x001E:
		saved := *s
		saved.clip = append([]clipItem(nil), s.clip...)
		p.saved = append(p.saved, saved)
	case 0x0127:
		n := int(int16(u16(b, 0)))
		idx := len(p.saved) + n
		if n > 0 {
			idx = n - 1
		}
		if idx >= 0 && idx < len(p.saved) {
			*s = p.saved[idx]
			p.saved = p.saved[:idx]
		}
	case 0x012D:
		i := int(u16(b, 0))
		if i < len(p.objects) && p.objects[i] != nil {
			o := p.objects[i]
			switch {
			case o.pen != nil:
				s.pen = o.pen
			case o.brush != nil:
				s.brush = o.brush
			case o.font != nil:
				s.font = o.font
			}
		}
	case 0x01F0:
		if i := int(u16(b, 0)); i < len(p.objects) {
			p.objects[i] = nil
		}
	case 0x02FA: // CREATEPENINDIRECT
		p.addObject(&object{pen: &pen{style: uint32(u16(b, 0)), width: i16(b, 2), color: colorRef(u32(b, 6))}})
	case 0x02FC: // CREATEBRUSHINDIRECT
		p.addObject(&object{brush: &brush{style: uint32(u16(b, 0)), color: colorRef(u32(b, 2)), hatch: uint32(u16(b, 6))}})
	case 0x02FB: // CREATEFONTINDIRECT
		f := &font{height: i16(b, 0), width: i16(b, 2), escapement: i16(b, 4), orientation: i16(b, 6), weight: int(i16(b, 8))}
		if len(b) > 13 {
			f.italic, f.underline, f.strikeout, f.charset = b[10] != 0, b[11] != 0, b[12] != 0, b[13]
		}
		if len(b) > 18 {
			f.face = decodeANSI(b[18:min(len(b), 18+32)], 0)
		}
		p.addObject(&object{font: f})
	case 0x0142, 0x01F9: // DIBCREATEPATTERNBRUSH, CREATEPATTERNBRUSH
		var img image.Image
		if fn == 0x0142 && len(b) > 4 {
			img = wmfDIB(b[4:])
		}
		p.addObject(&object{brush: &brush{style: bsDIBPatPT, img: img}})
	case 0x00F7, 0x06FF: // CREATEPALETTE, CREATEREGION
		o := &object{other: true}
		if fn == 0x06FF {
			o.rgn = wmfRegion(b)
		}
		p.addObject(o)
	case 0x0521: // TEXTOUT
		n := int(u16(b, 0))
		if 2+n > len(b) {
			return
		}
		text := decodeANSI(b[2:2+n], s.font.charset)
		o := 2 + (n+1)/2*2
		p.drawText(i16(b, o+2), i16(b, o), text, nil, nil)
	case 0x0A32: // EXTTEXTOUT
		y, x := i16(b, 0), i16(b, 2)
		n := int(u16(b, 4))
		opts := u16(b, 6)
		o := 8
		var rect *[4]float64
		var clip *[4]float64
		if opts&0x0006 != 0 && len(b) >= 16 {
			r := [4]float64{i16(b, 8), i16(b, 10), i16(b, 12), i16(b, 14)}
			if opts&0x0002 != 0 {
				rect = &r
			}
			if opts&0x0004 != 0 {
				clip = &r
			}
			o = 16
		}
		if o+n > len(b) {
			n = len(b) - o
		}
		if n <= 0 {
			return
		}
		text := decodeANSI(b[o:o+n], s.font.charset)
		var dx []float64
		do := o + (n+1)/2*2
		count := len([]rune(text))
		if do+2*count <= len(b) {
			for i := 0; i < count; i++ {
				dx = append(dx, i16(b, do+2*i))
			}
		}
		if clip != nil {
			saved := s.clip
			s.clip = append(append([]clipItem(nil), s.clip...), clipItem{shapes: []gfx.ClipShape{{Path: rectPath(clip[0], clip[1], clip[2], clip[3]), M: p.logical(), Rule: gfx.NonZero}}})
			p.drawText(x, y, text, dx, rect)
			s.clip = saved
			return
		}
		p.drawText(x, y, text, dx, rect)
	case 0x0415, 0x0416:
		p.clipRect(i16(b, 6), i16(b, 4), i16(b, 2), i16(b, 0), fn == 0x0415)
	case 0x012C: // SELECTCLIPREGION
		i := int(u16(b, 0))
		if i == 0 || i >= len(p.objects) || p.objects[i] == nil {
			s.clip = nil
			return
		}
		var shapes []gfx.ClipShape
		for _, r := range p.objects[i].rgn {
			shapes = append(shapes, gfx.ClipShape{Path: rectPath(r.X0, r.Y0, r.X1, r.Y1), M: p.logical(), Rule: gfx.NonZero})
		}
		p.selectClip(shapes, 5)
	case 0x0228, 0x012B: // FILLREGION, PAINTREGION
		i := int(u16(b, 0))
		if i >= len(p.objects) || p.objects[i] == nil {
			return
		}
		path := gfx.NewPath()
		for _, r := range p.objects[i].rgn {
			path.Rect(r.X0, r.Y0, r.Width(), r.Height())
		}
		paint, ok := p.fillPaint()
		if fn == 0x0228 {
			if j := int(u16(b, 2)); j < len(p.objects) && p.objects[j] != nil && p.objects[j].brush != nil {
				paint, ok = gfx.Solid(p.objects[j].brush.color.color()), p.objects[j].brush.style != bsNull
			}
		}
		if ok {
			p.syncClip()
			p.dev.Fill(path, p.logical(), gfx.NonZero, paint)
		}
	case 0x061D: // PATBLT: rop, height, width, y, x
		p.patBlt(i16(b, 10), i16(b, 8), i16(b, 6), i16(b, 4), u32(b, 0))
	case 0x0F43: // STRETCHDIB
		rop := u32(b, 0)
		hs, ws, ys, xs := int(i16(b, 6)), int(i16(b, 8)), int(i16(b, 10)), int(i16(b, 12))
		hd, wd, yd, xd := i16(b, 14), i16(b, 16), i16(b, 18), i16(b, 20)
		if img := wmfDIB(b[min(22, len(b)):]); img != nil {
			ih := img.Bounds().Dy()
			p.drawBitmap(crop(img, xs, ih-ys-hs, ws, hs), xd, yd, wd, hd, rop)
		}
	case 0x0B41, 0x0940: // DIBSTRETCHBLT, DIBBITBLT
		rop := u32(b, 0)
		var hs, ws, ys, xs int
		var hd, wd, yd, xd float64
		o := 0
		if fn == 0x0B41 {
			hs, ws, ys, xs = int(i16(b, 4)), int(i16(b, 6)), int(i16(b, 8)), int(i16(b, 10))
			hd, wd, yd, xd = i16(b, 12), i16(b, 14), i16(b, 16), i16(b, 18)
			o = 20
		} else {
			ys, xs = int(i16(b, 4)), int(i16(b, 6))
			hd, wd, yd, xd = i16(b, 8), i16(b, 10), i16(b, 12), i16(b, 14)
			hs, ws = int(hd), int(wd)
			o = 16
		}
		if o >= len(b) || len(b)-o < 40 {
			p.patBlt(xd, yd, wd, hd, rop)
			return
		}
		if img := wmfDIB(b[o:]); img != nil {
			p.drawBitmap(crop(img, xs, ys, ws, hs), xd, yd, wd, hd, rop)
		}
	case 0x0D33: // SETDIBTODEV
		ys, xs := int(i16(b, 6)), int(i16(b, 8))
		h, w := i16(b, 10), i16(b, 12)
		yd, xd := i16(b, 14), i16(b, 16)
		if img := wmfDIB(b[min(18, len(b)):]); img != nil {
			ih := img.Bounds().Dy()
			p.drawBitmap(crop(img, xs, ih-ys-int(h), int(w), int(h)), xd, yd, w, h, srcCopy)
		}
	}
}

// wmfWindowMapping: with a placeable header and no viewport of its own, the
// window is shown in the header's bounding box.
func (p *player) wmfWindowMapping(pic *Picture, viewportSet bool) {
	if viewportSet {
		return
	}
	s := &p.st
	if s.mapMode == mmText {
		s.mapMode = mmAnisotropic
	}
	if s.mapMode == mmAnisotropic || s.mapMode == mmIsotropic {
		s.viewOrg = gfx.Point{X: pic.bounds.X0, Y: pic.bounds.Y0}
		s.viewExt = gfx.Point{X: pic.bounds.Width(), Y: pic.bounds.Height()}
	}
}

// wmfDIB decodes a packed DIB (BITMAPINFO followed by the pixels).
func wmfDIB(b []byte) image.Image {
	if len(b) < 12 {
		return nil
	}
	hsize := int(le.Uint32(b))
	if hsize < 12 || hsize > len(b) {
		return nil
	}
	bpp := 0
	clrUsed := 0
	compression := uint32(0)
	palEntry := 4
	if hsize == 12 {
		bpp = int(le.Uint16(b[10:]))
		palEntry = 3
	} else if len(b) >= 40 {
		bpp = int(le.Uint16(b[14:]))
		compression = le.Uint32(b[16:])
		clrUsed = int(le.Uint32(b[32:]))
	}
	pal := 0
	if bpp <= 8 && bpp > 0 {
		pal = clrUsed
		if pal == 0 {
			pal = 1 << bpp
		}
	}
	infoLen := hsize + pal*palEntry
	if compression == 3 && hsize == 40 {
		infoLen += 12
	}
	if infoLen > len(b) {
		return nil
	}
	img, err := decodeDIB(b[:infoLen], b[infoLen:], false)
	if err != nil {
		return nil
	}
	return img
}

// wmfRegion reads the rectangles of a WMF region object.
func wmfRegion(b []byte) []gfx.Rect {
	if len(b) < 22 {
		return nil
	}
	scans := int(u16(b, 12))
	var out []gfx.Rect
	o := 22
	for i := 0; i < scans && o+6 <= len(b); i++ {
		count := int(u16(b, o))
		top, bottom := i16(b, o+2), i16(b, o+4)
		for j := 0; j+1 < count && o+6+2*j+4 <= len(b); j += 2 {
			out = append(out, gfx.Rect{X0: i16(b, o+6+2*j), Y0: top, X1: i16(b, o+6+2*j+2), Y1: bottom})
		}
		o += 6 + 2*count + 2
	}
	if len(out) == 0 {
		// The region's bounds.
		out = append(out, gfx.Rect{X0: i16(b, 14), Y0: i16(b, 16), X1: i16(b, 18), Y1: i16(b, 20)})
	}
	return out
}

// boundsDevice records the extent of what is drawn.
type boundsDevice struct{ r gfx.Rect }

func (b *boundsDevice) Fill(p *gfx.Path, m gfx.Matrix, rule gfx.FillRule, paint gfx.Paint) {
	b.r = b.r.Union(p.Transformed(m).Bounds())
}
func (b *boundsDevice) Stroke(p *gfx.Path, m gfx.Matrix, s *gfx.StrokeStyle, paint gfx.Paint) {
	b.r = b.r.Union(p.Transformed(m).Bounds())
}
func (b *boundsDevice) DrawImage(img image.Image, m gfx.Matrix, opacity float64) {
	ib := img.Bounds()
	b.r = b.r.Union(gfx.Rect{X1: float64(ib.Dx()), Y1: float64(ib.Dy())}.Transform(m))
}
func (b *boundsDevice) PushClip(p *gfx.Path, m gfx.Matrix, rule gfx.FillRule) {}
func (b *boundsDevice) PushClipShapes(shapes []gfx.ClipShape)                 {}
func (b *boundsDevice) PopClip()                                              {}
func (b *boundsDevice) PushGroup(opacity float64)                             {}
func (b *boundsDevice) PopGroup()                                             {}
func (b *boundsDevice) PushMask(kind gfx.MaskKind, opacity float64)           {}
func (b *boundsDevice) BeginMaskContent()                                     {}
func (b *boundsDevice) PopMask()                                              {}
