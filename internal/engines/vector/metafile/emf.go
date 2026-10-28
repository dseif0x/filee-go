package metafile

import (
	"encoding/binary"
	"errors"
	"image"
	"math"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Picture is a parsed metafile.
type Picture struct {
	// Width and Height are the picture's size in CSS pixels (96 per inch).
	Width, Height float64
	data          []byte
	wmf           bool
	// EMF: the frame in device units and device units per millimetre.
	frame    gfx.Rect
	devPerMm gfx.Point
	// WMF: the logical bounds and logical units per inch.
	bounds gfx.Rect
	inch   float64
}

var le = binary.LittleEndian

// Parse reads an EMF or WMF (placeable or not).
func Parse(data []byte) (*Picture, error) {
	if len(data) >= 88 && le.Uint32(data) == 1 && string(data[40:44]) == " EMF" {
		return parseEMFHeader(data)
	}
	if len(data) >= 18 && (le.Uint32(data) == 0x9AC6CDD7 || le.Uint16(data) == 1 || le.Uint16(data) == 2) {
		return parseWMFHeader(data)
	}
	return nil, errors.New("the file is not a Windows metafile (EMF or WMF)")
}

func parseEMFHeader(data []byte) (*Picture, error) {
	rd := func(o int) int32 { return int32(le.Uint32(data[o:])) }
	frame := [4]float64{float64(rd(24)), float64(rd(28)), float64(rd(32)), float64(rd(36))} // 0.01 mm
	devX, devY := float64(rd(72)), float64(rd(76))
	mmX, mmY := float64(rd(80)), float64(rd(84))
	if devX <= 0 || devY <= 0 || mmX <= 0 || mmY <= 0 {
		devX, devY, mmX, mmY = 1024, 768, 320, 240
	}
	p := &Picture{data: data, devPerMm: gfx.Point{X: devX / mmX, Y: devY / mmY}}
	wmm := (frame[2] - frame[0]) / 100
	hmm := (frame[3] - frame[1]) / 100
	if wmm <= 0 || hmm <= 0 {
		// No frame: the bounds in device pixels.
		b := [4]float64{float64(rd(8)), float64(rd(12)), float64(rd(16)), float64(rd(20))}
		frame = [4]float64{b[0] / p.devPerMm.X * 100, b[1] / p.devPerMm.Y * 100, (b[2] + 1) / p.devPerMm.X * 100, (b[3] + 1) / p.devPerMm.Y * 100}
		wmm = (frame[2] - frame[0]) / 100
		hmm = (frame[3] - frame[1]) / 100
	}
	if wmm <= 0 || hmm <= 0 {
		return nil, errors.New("the metafile has no size")
	}
	p.frame = gfx.Rect{X0: frame[0] / 100 * p.devPerMm.X, Y0: frame[1] / 100 * p.devPerMm.Y,
		X1: frame[2] / 100 * p.devPerMm.X, Y1: frame[3] / 100 * p.devPerMm.Y}
	p.Width = wmm * 96 / 25.4
	p.Height = hmm * 96 / 25.4
	return p, nil
}

// Draw plays the picture; m maps the picture (CSS pixels) to the device.
func (p *Picture) Draw(dev gfx.Device, m gfx.Matrix, fonts *gfx.Fonts) {
	if fonts == nil {
		fonts = gfx.SharedFonts()
	}
	clip := gfx.NewPath()
	clip.Rect(0, 0, p.Width, p.Height)
	dev.PushClip(clip, m, gfx.NonZero)
	defer dev.PopClip()
	if p.wmf {
		p.playWMF(dev, m, fonts)
		return
	}
	k := gfx.Point{X: 96 / 25.4 / p.devPerMm.X, Y: 96 / 25.4 / p.devPerMm.Y}
	toOut := m.Mul(gfx.Scale(k.X, k.Y)).Mul(gfx.Translate(-p.frame.X0, -p.frame.Y0))
	pl := newPlayer(dev, toOut, p.devPerMm, fonts)
	pl.playEMF(p.data)
	pl.finish()
}

type emfReader struct {
	b []byte
}

func (r emfReader) i32(o int) float64 {
	if o+4 > len(r.b) {
		return 0
	}
	return float64(int32(le.Uint32(r.b[o:])))
}
func (r emfReader) u32(o int) uint32 {
	if o+4 > len(r.b) {
		return 0
	}
	return le.Uint32(r.b[o:])
}
func (r emfReader) i16(o int) float64 {
	if o+2 > len(r.b) {
		return 0
	}
	return float64(int16(le.Uint16(r.b[o:])))
}
func (r emfReader) f32(o int) float64 {
	if o+4 > len(r.b) {
		return 0
	}
	return float64(math.Float32frombits(le.Uint32(r.b[o:])))
}

func utf16String(b []byte, n int) string {
	u := make([]uint16, 0, n)
	for i := 0; i < n && 2*i+1 < len(b); i++ {
		u = append(u, le.Uint16(b[2*i:]))
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

func (p *player) object(i uint32) {
	if i&0x80000000 != 0 {
		p.stockObject(i & 0x7FFFFFFF)
		return
	}
	if int(i) >= len(p.objects) || p.objects[i] == nil {
		return
	}
	o := p.objects[i]
	switch {
	case o.pen != nil:
		p.st.pen = o.pen
	case o.brush != nil:
		p.st.brush = o.brush
	case o.font != nil:
		p.st.font = o.font
	}
}

func (p *player) stockObject(i uint32) {
	switch i {
	case 0:
		p.st.brush = &brush{style: bsSolid, color: 0xFFFFFF}
	case 1:
		p.st.brush = &brush{style: bsSolid, color: 0xC0C0C0}
	case 2:
		p.st.brush = &brush{style: bsSolid, color: 0x808080}
	case 3:
		p.st.brush = &brush{style: bsSolid, color: 0x404040}
	case 4:
		p.st.brush = &brush{style: bsSolid, color: 0}
	case 5:
		p.st.brush = &brush{style: bsNull}
	case 6:
		p.st.pen = &pen{style: psSolid, color: 0xFFFFFF}
	case 7:
		p.st.pen = &pen{style: psSolid, color: 0}
	case 8:
		p.st.pen = &pen{style: psNull}
	case 10, 11, 16:
		p.st.font = &font{height: 12, face: "Courier New", weight: 400}
	case 12, 13, 14, 17:
		p.st.font = &font{height: 12, face: "Arial", weight: 400}
	case 18:
		p.st.brush = &brush{style: bsSolid, color: 0xFFFFFF}
	case 19:
		p.st.pen = &pen{style: psSolid, color: 0}
	}
}

func (p *player) setObject(i uint32, o *object) {
	if i&0x80000000 != 0 || i > 1<<16 {
		return
	}
	for int(i) >= len(p.objects) {
		p.objects = append(p.objects, nil)
	}
	p.objects[i] = o
}

func readLogFontW(r emfReader, o int) *font {
	f := &font{
		height:      r.i32(o),
		width:       r.i32(o + 4),
		escapement:  r.i32(o + 8),
		orientation: r.i32(o + 12),
		weight:      int(r.i32(o + 16)),
	}
	if o+28 <= len(r.b) {
		f.italic = r.b[o+20] != 0
		f.underline = r.b[o+21] != 0
		f.strikeout = r.b[o+22] != 0
		f.charset = r.b[o+23]
	}
	if o+28+64 <= len(r.b) {
		f.face = utf16String(r.b[o+28:o+28+64], 32)
	}
	return f
}

// points reads n points (16- or 32-bit) at offset o.
func points(r emfReader, o, n int, small bool) []gfx.Point {
	pts := make([]gfx.Point, 0, n)
	for i := 0; i < n; i++ {
		if small {
			pts = append(pts, gfx.Point{X: r.i16(o + 4*i), Y: r.i16(o + 4*i + 2)})
		} else {
			pts = append(pts, gfx.Point{X: r.i32(o + 8*i), Y: r.i32(o + 8*i + 4)})
		}
	}
	return pts
}

func polyPath(pts []gfx.Point, closed bool) *gfx.Path {
	path := gfx.NewPath()
	if len(pts) == 0 {
		return path
	}
	path.MoveTo(pts[0].X, pts[0].Y)
	for _, q := range pts[1:] {
		path.LineTo(q.X, q.Y)
	}
	if closed {
		path.Close()
	}
	return path
}

func bezierPath(start gfx.Point, pts []gfx.Point, move bool) *gfx.Path {
	path := gfx.NewPath()
	path.MoveTo(start.X, start.Y)
	for i := 0; i+2 < len(pts); i += 3 {
		path.CubeTo(pts[i].X, pts[i].Y, pts[i+1].X, pts[i+1].Y, pts[i+2].X, pts[i+2].Y)
	}
	return path
}

// pathAddTo appends to the path bracket or draws (lines only, from the current point).
func (p *player) lineTo(pts []gfx.Point) {
	if len(pts) == 0 {
		return
	}
	path := gfx.NewPath()
	path.MoveTo(p.st.cur.X, p.st.cur.Y)
	for _, q := range pts {
		path.LineTo(q.X, q.Y)
	}
	p.st.cur = pts[len(pts)-1]
	if p.inPath {
		p.appendToPath(path, true)
		return
	}
	p.strokeOnly(path)
}

// appendToPath continues the open figure of the path bracket.
func (p *player) appendToPath(seg *gfx.Path, continueFigure bool) {
	if p.path == nil {
		p.path = gfx.NewPath()
	}
	first := true
	seg.Walk(func(op gfx.Op, pts []gfx.Point) {
		switch op {
		case gfx.MoveTo:
			if first && continueFigure && len(p.path.Ops) > 0 && p.path.Current() == pts[0] {
				first = false
				return
			}
			p.path.MoveTo(pts[0].X, pts[0].Y)
		case gfx.LineTo:
			p.path.LineTo(pts[0].X, pts[0].Y)
		case gfx.QuadTo:
			p.path.QuadTo(pts[0].X, pts[0].Y, pts[1].X, pts[1].Y)
		case gfx.CubeTo:
			p.path.CubeTo(pts[0].X, pts[0].Y, pts[1].X, pts[1].Y, pts[2].X, pts[2].Y)
		case gfx.Close:
			p.path.Close()
		}
		first = false
	})
}

func (p *player) playEMF(data []byte) {
	for off := 0; off+8 <= len(data); {
		typ := le.Uint32(data[off:])
		size := int(le.Uint32(data[off+4:]))
		if size < 8 || off+size > len(data) {
			break
		}
		r := emfReader{data[off : off+size]}
		if !p.emfRecord(typ, r) {
			break
		}
		off += size
	}
}

// emfRecord plays one record; false at the end of the file.
func (p *player) emfRecord(typ uint32, r emfReader) bool {
	s := &p.st
	switch typ {
	case 14: // EOF
		return false
	case 2, 85: // POLYBEZIER(16)
		small := typ == 85
		n := int(r.u32(24))
		pts := points(r, 28, n, small)
		if len(pts) > 0 {
			p.strokeOnly(bezierPath(pts[0], pts[1:], true))
		}
	case 3, 86: // POLYGON(16)
		pts := points(r, 28, int(r.u32(24)), typ == 86)
		p.drawShape(polyPath(pts, true), true)
	case 4, 87: // POLYLINE(16)
		pts := points(r, 28, int(r.u32(24)), typ == 87)
		p.strokeOnly(polyPath(pts, false))
	case 5, 88: // POLYBEZIERTO(16)
		pts := points(r, 28, int(r.u32(24)), typ == 88)
		path := bezierPath(s.cur, pts, false)
		if len(pts) > 0 {
			s.cur = pts[len(pts)-1]
		}
		if p.inPath {
			p.appendToPath(path, true)
		} else {
			p.strokeOnly(path)
		}
	case 6, 89: // POLYLINETO(16)
		p.lineTo(points(r, 28, int(r.u32(24)), typ == 89))
	case 7, 8, 90, 91: // POLYPOLYLINE / POLYPOLYGON (16)
		small := typ >= 90
		nPolys := int(r.u32(24))
		count := int(r.u32(28))
		if nPolys > 1<<20 || count > 1<<24 {
			break
		}
		o := 32 + 4*nPolys
		path := gfx.NewPath()
		closed := typ == 8 || typ == 91
		for i := 0; i < nPolys; i++ {
			n := int(r.u32(32 + 4*i))
			pts := points(r, o, n, small)
			path.Append(polyPath(pts, closed))
			if small {
				o += 4 * n
			} else {
				o += 8 * n
			}
		}
		if closed {
			p.drawShape(path, true)
		} else {
			p.strokeOnly(path)
		}
	case 9: // SETWINDOWEXTEX
		s.winExt = gfx.Point{X: r.i32(8), Y: r.i32(12)}
	case 10: // SETWINDOWORGEX
		s.winOrg = gfx.Point{X: r.i32(8), Y: r.i32(12)}
	case 11: // SETVIEWPORTEXTEX
		s.viewExt = gfx.Point{X: r.i32(8), Y: r.i32(12)}
	case 12: // SETVIEWPORTORGEX
		s.viewOrg = gfx.Point{X: r.i32(8), Y: r.i32(12)}
	case 17: // SETMAPMODE
		s.mapMode = r.u32(8)
	case 18: // SETBKMODE
		s.bkMode = r.u32(8)
	case 19: // SETPOLYFILLMODE: 1 alternate, 2 winding
		if r.u32(8) == 2 {
			s.polyFill = gfx.NonZero
		} else {
			s.polyFill = gfx.EvenOdd
		}
	case 20: // SETROP2
		s.rop2 = r.u32(8)
	case 21:
		s.stretchMode = r.u32(8)
	case 22: // SETTEXTALIGN
		s.textAlign = r.u32(8)
	case 24: // SETTEXTCOLOR
		s.textColor = colorRef(r.u32(8))
	case 25: // SETBKCOLOR
		s.bkColor = colorRef(r.u32(8))
	case 27: // MOVETOEX
		s.cur = gfx.Point{X: r.i32(8), Y: r.i32(12)}
		if p.inPath {
			if p.path == nil {
				p.path = gfx.NewPath()
			}
			p.path.MoveTo(s.cur.X, s.cur.Y)
		}
	case 29, 30: // EXCLUDECLIPRECT, INTERSECTCLIPRECT (logical)
		p.clipRect(r.i32(8), r.i32(12), r.i32(16), r.i32(20), typ == 29)
	case 26: // OFFSETCLIPRGN: rarely used; left as is.
	case 31: // SCALEVIEWPORTEXTEX
		if r.i32(12) != 0 && r.i32(20) != 0 {
			s.viewExt.X = s.viewExt.X * r.i32(8) / r.i32(12)
			s.viewExt.Y = s.viewExt.Y * r.i32(16) / r.i32(20)
		}
	case 32: // SCALEWINDOWEXTEX
		if r.i32(12) != 0 && r.i32(20) != 0 {
			s.winExt.X = s.winExt.X * r.i32(8) / r.i32(12)
			s.winExt.Y = s.winExt.Y * r.i32(16) / r.i32(20)
		}
	case 33: // SAVEDC
		saved := *s
		saved.clip = append([]clipItem(nil), s.clip...)
		p.saved = append(p.saved, saved)
	case 34: // RESTOREDC
		n := int(int32(r.u32(8)))
		idx := len(p.saved) + n
		if n > 0 {
			idx = n - 1
		}
		if idx >= 0 && idx < len(p.saved) {
			*s = p.saved[idx]
			p.saved = p.saved[:idx]
		}
	case 35: // SETWORLDTRANSFORM
		s.world = xform(r, 8)
	case 36: // MODIFYWORLDTRANSFORM
		x := xform(r, 8)
		switch r.u32(32) {
		case 1: // identity
			s.world = gfx.Identity
		case 2: // left multiply: x applies first
			s.world = s.world.Mul(x)
		case 3: // right multiply
			s.world = x.Mul(s.world)
		case 4: // set
			s.world = x
		}
	case 37: // SELECTOBJECT
		p.object(r.u32(8))
	case 38: // CREATEPEN
		pn := &pen{style: r.u32(12), width: r.i32(16), color: colorRef(r.u32(24))}
		p.setObject(r.u32(8), &object{pen: pn})
	case 39: // CREATEBRUSHINDIRECT
		b := &brush{style: r.u32(12), color: colorRef(r.u32(16)), hatch: r.u32(20)}
		p.setObject(r.u32(8), &object{brush: b})
	case 40: // DELETEOBJECT
		if i := r.u32(8); int(i) < len(p.objects) {
			p.objects[i] = nil
		}
	case 41: // ANGLEARC
		cx, cy, rad := r.i32(8), r.i32(12), float64(r.u32(16))
		a0, sweep := r.f32(20)*math.Pi/180, r.f32(24)*math.Pi/180
		path := gfx.NewPath()
		path.MoveTo(s.cur.X, s.cur.Y)
		// Counter-clockwise in logical space with y up on paper: negate on y-down device.
		path.Arc(cx, cy, rad, rad, 0, -a0, -sweep)
		s.cur = path.Current()
		if p.inPath {
			p.appendToPath(path, true)
		} else {
			p.strokeOnly(path)
		}
	case 42: // ELLIPSE
		p.drawShape(ellipsePath(r.i32(8), r.i32(12), r.i32(16), r.i32(20)), true)
	case 43: // RECTANGLE
		p.drawShape(rectPath(r.i32(8), r.i32(12), r.i32(16), r.i32(20)), true)
	case 44: // ROUNDRECT
		p.drawShape(roundRectPath(r.i32(8), r.i32(12), r.i32(16), r.i32(20), r.i32(24), r.i32(28)), true)
	case 45, 46, 47, 55: // ARC, CHORD, PIE, ARCTO
		kind := map[uint32]int{45: 0, 46: 1, 47: 2, 55: 0}[typ]
		path := arcPath(r.i32(8), r.i32(12), r.i32(16), r.i32(20), r.i32(24), r.i32(28), r.i32(32), r.i32(36), s.arcCW, kind, typ == 55, s.cur)
		if typ == 55 {
			s.cur = path.Current()
		}
		if kind == 0 {
			if p.inPath {
				p.appendToPath(path, typ == 55)
			} else {
				p.strokeOnly(path)
			}
		} else {
			p.drawShape(path, true)
		}
	case 54: // LINETO
		p.lineTo([]gfx.Point{{X: r.i32(8), Y: r.i32(12)}})
	case 57: // SETARCDIRECTION: 2 clockwise
		s.arcCW = r.u32(8) == 2
	case 58: // SETMITERLIMIT
		s.miterLimit = math.Max(1, float64(r.u32(8)))
	case 59: // BEGINPATH
		p.inPath = true
		p.path = gfx.NewPath()
	case 60: // ENDPATH
		p.inPath = false
	case 61: // CLOSEFIGURE
		if p.path != nil {
			p.path.Close()
		}
	case 62, 63, 64: // FILLPATH, STROKEANDFILLPATH, STROKEPATH
		if p.path != nil {
			path := p.path
			p.path = nil
			p.inPath = false
			switch typ {
			case 62:
				saved := s.pen
				s.pen = &pen{style: psNull}
				p.drawShape(path, true)
				s.pen = saved
			case 63:
				p.drawShape(path, true)
			default:
				p.strokeOnly(path)
			}
		}
	case 65, 66: // FLATTENPATH, WIDENPATH: the path stays as it is.
	case 67: // SELECTCLIPPATH
		if p.path != nil {
			p.selectClip([]gfx.ClipShape{{Path: p.path, M: p.logical(), Rule: s.polyFill}}, r.u32(8))
			p.path = nil
			p.inPath = false
		}
	case 68: // ABORTPATH
		p.path = nil
		p.inPath = false
	case 75: // EXTSELECTCLIPRGN
		cb := int(r.u32(8))
		mode := r.u32(12)
		if cb == 0 && mode == 5 { // RGN_COPY with no region: no clipping
			s.clip = nil
			break
		}
		rects := regionRects(r, 16, cb)
		var shapes []gfx.ClipShape
		for _, rc := range rects {
			shapes = append(shapes, gfx.ClipShape{Path: rectPath(rc.X0, rc.Y0, rc.X1, rc.Y1), M: p.deviceToOut(), Rule: gfx.NonZero})
		}
		p.selectClip(shapes, mode)
	case 71, 74: // FILLRGN (brush index at 28), PAINTRGN: device-space region rectangles
		var rects []gfx.Rect
		paint, ok := p.fillPaint()
		if typ == 71 {
			rects = regionRects(r, 32, int(r.u32(24)))
			if i := r.u32(28); i&0x80000000 == 0 && int(i) < len(p.objects) && p.objects[i] != nil && p.objects[i].brush != nil {
				paint, ok = gfx.Solid(p.objects[i].brush.color.color()), p.objects[i].brush.style != bsNull
			}
		} else {
			rects = regionRects(r, 28, int(r.u32(24)))
		}
		path := gfx.NewPath()
		for _, rc := range rects {
			path.Rect(rc.X0, rc.Y0, rc.Width(), rc.Height())
		}
		if ok {
			p.syncClip()
			p.dev.Fill(path, p.deviceToOut(), gfx.NonZero, paint)
		}
	case 76, 77: // BITBLT, STRETCHBLT
		p.emfBlt(r, typ == 77)
	case 80: // SETDIBITSTODEVICE
		x, y := r.i32(24), r.i32(28)
		xs, ys, w, h := int(r.i32(32)), int(r.i32(36)), int(r.i32(40)), int(r.i32(44))
		offBmi, cbBmi, offBits, cbBits := int(r.u32(48)), int(r.u32(52)), int(r.u32(56)), int(r.u32(60))
		if img := dibAt(r, offBmi, cbBmi, offBits, cbBits, false); img != nil {
			ih := img.Bounds().Dy()
			p.drawBitmap(crop(img, xs, ih-ys-h, w, h), x, y, float64(w), float64(h), srcCopy)
		}
	case 81: // STRETCHDIBITS
		x, y := r.i32(24), r.i32(28)
		xs, ys, ws, hs := int(r.i32(32)), int(r.i32(36)), int(r.i32(40)), int(r.i32(44))
		offBmi, cbBmi, offBits, cbBits := int(r.u32(48)), int(r.u32(52)), int(r.u32(56)), int(r.u32(60))
		rop := r.u32(68)
		w, h := r.i32(72), r.i32(76)
		if img := dibAt(r, offBmi, cbBmi, offBits, cbBits, false); img != nil {
			ih := img.Bounds().Dy()
			p.drawBitmap(crop(img, xs, ih-ys-hs, ws, hs), x, y, w, h, rop)
		}
	case 114: // ALPHABLEND
		x, y, w, h := r.i32(24), r.i32(28), r.i32(32), r.i32(36)
		xs, ys := int(r.i32(44)), int(r.i32(48))
		offBmi, cbBmi, offBits, cbBits := int(r.u32(84)), int(r.u32(88)), int(r.u32(92)), int(r.u32(96))
		ws, hs := int(r.i32(100)), int(r.i32(104))
		if img := dibAt(r, offBmi, cbBmi, offBits, cbBits, true); img != nil {
			p.drawBitmap(crop(img, xs, ys, ws, hs), x, y, w, h, srcCopy)
		}
	case 82: // EXTCREATEFONTINDIRECTW
		p.setObject(r.u32(8), &object{font: readLogFontW(r, 12)})
	case 83, 84: // EXTTEXTOUTA / W
		p.emfText(r, typ == 84)
	case 93, 94: // CREATEMONOBRUSH, CREATEDIBPATTERNBRUSHPT
		offBmi, cbBmi, offBits, cbBits := int(r.u32(16)), int(r.u32(20)), int(r.u32(24)), int(r.u32(28))
		b := &brush{style: bsDIBPatPT, img: dibAt(r, offBmi, cbBmi, offBits, cbBits, false)}
		p.setObject(r.u32(8), &object{brush: b})
	case 95: // EXTCREATEPEN
		style := r.u32(28)
		width := float64(r.u32(32))
		brushStyle := r.u32(36)
		col := colorRef(r.u32(40))
		n := int(r.u32(48))
		pn := &pen{style: style, width: width, color: col}
		if brushStyle == bsNull {
			pn.style = psNull
		}
		for i := 0; i < n && i < 64; i++ {
			pn.dashes = append(pn.dashes, float64(r.u32(52+4*i)))
		}
		p.setObject(r.u32(8), &object{pen: pn})
	case 118: // GRADIENTFILL
		p.gradientFill(r)
	case 108: // SMALLTEXTOUT
		x, y := r.i32(8), r.i32(12)
		n := int(r.u32(16))
		opts := r.u32(20)
		o := 36
		if opts&0x0100 == 0 { // ETO_NO_RECT absent: a rectangle follows
			o += 16
		}
		var text string
		if opts&0x0200 != 0 { // ETO_SMALL_CHARS: 8-bit
			text = decodeANSI(r.b[min(o, len(r.b)):min(o+n, len(r.b))], s.font.charset)
		} else {
			text = utf16String(r.b[min(o, len(r.b)):], n)
		}
		p.drawText(x, y, text, nil, nil)
	}
	return true
}

func xform(r emfReader, o int) gfx.Matrix {
	return gfx.Matrix{A: r.f32(o), B: r.f32(o + 4), C: r.f32(o + 8), D: r.f32(o + 12), E: r.f32(o + 16), F: r.f32(o + 20)}
}

// regionRects reads the rectangles of a RGNDATA.
func regionRects(r emfReader, o, cb int) []gfx.Rect {
	if cb < 32 {
		return nil
	}
	n := int(r.u32(o + 8))
	var out []gfx.Rect
	for i := 0; i < n && i < 1<<16; i++ {
		q := o + 32 + 16*i
		out = append(out, gfx.Rect{X0: r.i32(q), Y0: r.i32(q + 4), X1: r.i32(q + 8), Y1: r.i32(q + 12)})
	}
	return out
}

// clipRect intersects or excludes a logical rectangle.
func (p *player) clipRect(l, t, rr, b float64, exclude bool) {
	m := p.logical()
	if !exclude {
		p.st.clip = append(p.st.clip, clipItem{shapes: []gfx.ClipShape{{Path: rectPath(l, t, rr, b), M: m, Rule: gfx.NonZero}}})
		return
	}
	// Everything but the rectangle: a huge frame with a hole (even-odd).
	big := 1e7
	path := rectPath(-big, -big, big, big)
	path.Append(rectPath(l, t, rr, b))
	p.st.clip = append(p.st.clip, clipItem{shapes: []gfx.ClipShape{{Path: path, M: m, Rule: gfx.EvenOdd}}})
}

// selectClip combines a clip with the current one (RGN_AND 1, OR 2, XOR 3, DIFF 4, COPY 5).
func (p *player) selectClip(shapes []gfx.ClipShape, mode uint32) {
	switch mode {
	case 1:
		p.st.clip = append(p.st.clip, clipItem{shapes: shapes})
	case 2:
		if len(p.st.clip) == 1 {
			p.st.clip = []clipItem{{shapes: append(append([]gfx.ClipShape(nil), p.st.clip[0].shapes...), shapes...)}}
		} else if len(p.st.clip) == 0 {
			// Union with everything is everything.
		} else {
			p.st.clip = []clipItem{{shapes: shapes}}
		}
	case 4:
		// Difference: the current clip minus the shapes, as even-odd holes.
		big := 1e7
		frame := rectPath(-big, -big, big, big)
		for _, s := range shapes {
			hole := gfx.ClipShape{Path: frame.Clone(), M: gfx.Identity, Rule: gfx.EvenOdd}
			hole.Path.Append(s.Path.Transformed(s.M))
			p.st.clip = append(p.st.clip, clipItem{shapes: []gfx.ClipShape{hole}})
		}
	default:
		p.st.clip = []clipItem{{shapes: shapes}}
	}
}

func dibAt(r emfReader, offBmi, cbBmi, offBits, cbBits int, alpha bool) image.Image {
	if cbBmi <= 0 || offBmi < 0 || offBmi+cbBmi > len(r.b) || offBits < 0 || offBits > len(r.b) {
		return nil
	}
	end := min(len(r.b), offBits+cbBits)
	img, err := decodeDIB(r.b[offBmi:offBmi+cbBmi], r.b[offBits:end], alpha)
	if err != nil {
		return nil
	}
	return img
}
