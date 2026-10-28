// Package metafile draws Windows metafiles (EMF and WMF) onto a gfx.Device:
// the GDI drawing records (lines, curves, shapes, paths, text, bitmaps,
// clipping, mapping modes and world transforms) are played back like GDI
// would. EMF+ records inside comments are not read; files with EMF+ have the
// plain EMF drawing too, which is what is drawn.
package metafile

import (
	"image"
	"math"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// colorRef is a COLORREF (0x00BBGGRR).
type colorRef uint32

func (c colorRef) color() gfx.Color {
	return gfx.RGB8(uint8(c), uint8(c>>8), uint8(c>>16))
}

type pen struct {
	style  uint32
	width  float64 // logical units; 0 is one device pixel
	color  colorRef
	dashes []float64 // user style (logical units)
}

const (
	psSolid       = 0
	psDash        = 1
	psDot         = 2
	psDashDot     = 3
	psDashDotDot  = 4
	psNull        = 5
	psInsideFrame = 6
	psUserStyle   = 7
	psEndcapMask  = 0x0F00
	psEndcapRound = 0x0000
	psEndcapSq    = 0x0100
	psEndcapFlat  = 0x0200
	psJoinMask    = 0xF000
	psJoinRound   = 0x0000
	psJoinBevel   = 0x1000
	psJoinMiter   = 0x2000
	psGeometric   = 0x00010000
)

type brush struct {
	style uint32
	color colorRef
	hatch uint32
	// img is the pattern of a pattern brush (drawn as its average colour).
	img image.Image
}

const (
	bsSolid      = 0
	bsNull       = 1
	bsHatched    = 2
	bsPattern    = 3
	bsDIBPattern = 5
	bsDIBPatPT   = 6
)

type font struct {
	height      float64
	width       float64
	escapement  float64 // tenths of a degree
	orientation float64
	weight      int
	italic      bool
	underline   bool
	strikeout   bool
	charset     uint8
	face        string
}

// object is a GDI object in the object table.
type object struct {
	pen   *pen
	brush *brush
	font  *font
	other bool // palettes and regions: selected but not drawn with
	rgn   []gfx.Rect
}

// dcState is what SaveDC saves.
type dcState struct {
	pen         *pen
	brush       *brush
	font        *font
	textColor   colorRef
	bkColor     colorRef
	bkMode      uint32
	polyFill    gfx.FillRule
	textAlign   uint32
	mapMode     uint32
	winOrg      gfx.Point
	winExt      gfx.Point
	viewOrg     gfx.Point
	viewExt     gfx.Point
	world       gfx.Matrix
	cur         gfx.Point
	arcCW       bool
	miterLimit  float64
	clip        []clipItem
	rop2        uint32
	charExtra   float64
	stretchMode uint32
}

// clipItem is one clip, in device coordinates, intersected with the others.
type clipItem struct {
	shapes []gfx.ClipShape
}

const (
	mmText        = 1
	mmLoMetric    = 2
	mmHiMetric    = 3
	mmLoEnglish   = 4
	mmHiEnglish   = 5
	mmTwips       = 6
	mmIsotropic   = 7
	mmAnisotropic = 8

	bkTransparent = 1
	bkOpaque      = 2

	taUpdateCP = 1
	taRight    = 2
	taCenter   = 6
	taBottom   = 8
	taBaseline = 24
	taRTL      = 256
)

// player holds the GDI state while records are played.
type player struct {
	dev      gfx.Device
	toOut    gfx.Matrix // device units → output
	devPerMm gfx.Point  // device units per millimetre (for fixed mapping modes)
	st       dcState
	saved    []dcState
	objects  []*object
	path     *gfx.Path // EMF path bracket in progress (logical → device mapped)
	inPath   bool
	applied  []clipItem // clips pushed on dev
	fonts    *gfx.Fonts
	// isWMF: WMF coordinates are 16-bit and objects take the lowest free slot.
	isWMF bool
}

func newPlayer(dev gfx.Device, toOut gfx.Matrix, devPerMm gfx.Point, fonts *gfx.Fonts) *player {
	p := &player{dev: dev, toOut: toOut, devPerMm: devPerMm, fonts: fonts}
	p.st = dcState{
		pen:        &pen{style: psSolid, color: 0},
		brush:      &brush{style: bsSolid, color: 0xFFFFFF},
		font:       &font{height: 12, face: "Arial", weight: 400},
		textColor:  0,
		bkColor:    0xFFFFFF,
		bkMode:     bkOpaque,
		polyFill:   gfx.EvenOdd,
		mapMode:    mmText,
		winExt:     gfx.Point{X: 1, Y: 1},
		viewExt:    gfx.Point{X: 1, Y: 1},
		world:      gfx.Identity,
		miterLimit: 10,
		rop2:       13,
	}
	return p
}

// pageMatrix maps logical coordinates (after the world transform) to device units.
func (p *player) pageMatrix() gfx.Matrix {
	s := &p.st
	var sx, sy float64
	switch s.mapMode {
	case mmLoMetric:
		sx, sy = p.devPerMm.X/10, -p.devPerMm.Y/10
	case mmHiMetric:
		sx, sy = p.devPerMm.X/100, -p.devPerMm.Y/100
	case mmLoEnglish:
		sx, sy = p.devPerMm.X*0.254, -p.devPerMm.Y*0.254
	case mmHiEnglish:
		sx, sy = p.devPerMm.X*0.0254, -p.devPerMm.Y*0.0254
	case mmTwips:
		sx, sy = p.devPerMm.X*25.4/1440, -p.devPerMm.Y*25.4/1440
	case mmIsotropic, mmAnisotropic:
		if s.winExt.X == 0 || s.winExt.Y == 0 {
			sx, sy = 1, 1
		} else {
			sx, sy = s.viewExt.X/s.winExt.X, s.viewExt.Y/s.winExt.Y
			if s.mapMode == mmIsotropic {
				k := math.Min(math.Abs(sx), math.Abs(sy))
				sx, sy = math.Copysign(k, sx), math.Copysign(k, sy)
			}
		}
	default:
		sx, sy = 1, 1
	}
	if s.mapMode == mmText || s.mapMode == mmIsotropic || s.mapMode == mmAnisotropic {
		return gfx.Matrix{A: sx, D: sy, E: s.viewOrg.X - s.winOrg.X*sx, F: s.viewOrg.Y - s.winOrg.Y*sy}
	}
	return gfx.Matrix{A: sx, D: sy, E: s.viewOrg.X - s.winOrg.X*sx, F: s.viewOrg.Y - s.winOrg.Y*sy}
}

// logical is the full logical → output transform.
func (p *player) logical() gfx.Matrix { return p.toOut.Mul(p.pageMatrix()).Mul(p.st.world) }

// deviceToOut maps device units (clip regions) to output.
func (p *player) deviceToOut() gfx.Matrix { return p.toOut }

// syncClip makes the device's clip stack match the state's.
func (p *player) syncClip() {
	want := p.st.clip
	same := len(want) == len(p.applied)
	if same {
		for i := range want {
			if !sameClip(want[i], p.applied[i]) {
				same = false
				break
			}
		}
	}
	if same {
		return
	}
	for range p.applied {
		p.dev.PopClip()
	}
	p.applied = append([]clipItem(nil), want...)
	for _, c := range want {
		p.dev.PushClipShapes(c.shapes)
	}
}

func sameClip(a, b clipItem) bool {
	if len(a.shapes) != len(b.shapes) {
		return false
	}
	for i := range a.shapes {
		if a.shapes[i].Path != b.shapes[i].Path {
			return false
		}
	}
	return true
}

func (p *player) finish() {
	for range p.applied {
		p.dev.PopClip()
	}
	p.applied = nil
}

// strokeStyle is the pen as a stroke in logical units; nil for no line.
func (p *player) strokeStyle() (*gfx.StrokeStyle, gfx.Paint, bool) {
	pn := p.st.pen
	style := pn.style & 0x0F
	if pn == nil || style == psNull {
		return nil, gfx.Paint{}, false
	}
	w := pn.width
	geometric := pn.style&psGeometric != 0 || w > 1
	m := p.logical()
	scale := m.MeanScale()
	if w <= 0 || !geometric {
		// Cosmetic pens are one device pixel wide whatever the mapping.
		dev := p.toOut.MeanScale()
		if scale > 0 {
			w = math.Max(w, 1) * dev / scale
			if pn.width > 1 && !geometric {
				w = pn.width
			}
		} else {
			w = 1
		}
	}
	s := &gfx.StrokeStyle{Width: w, MiterLimit: p.st.miterLimit}
	switch pn.style & psEndcapMask {
	case psEndcapSq:
		s.Cap = gfx.SquareCap
	case psEndcapFlat:
		s.Cap = gfx.ButtCap
	default:
		s.Cap = gfx.RoundCap
	}
	switch pn.style & psJoinMask {
	case psJoinBevel:
		s.Join = gfx.BevelJoin
	case psJoinMiter:
		s.Join = gfx.MiterJoin
	default:
		s.Join = gfx.RoundJoin
	}
	unit := math.Max(w, 1)
	switch style {
	case psDash:
		s.Dashes = []float64{3 * unit, unit}
	case psDot:
		s.Dashes = []float64{unit, unit}
	case psDashDot:
		s.Dashes = []float64{3 * unit, unit, unit, unit}
	case psDashDotDot:
		s.Dashes = []float64{3 * unit, unit, unit, unit, unit, unit}
	case psUserStyle:
		if len(pn.dashes) > 0 {
			s.Dashes = pn.dashes
		}
	}
	if s.Dashes != nil && s.Cap == gfx.RoundCap {
		s.Cap = gfx.ButtCap
	}
	return s, gfx.Solid(pn.color.color()), true
}

// fillPaint is the brush as a paint; ok false for the null brush.
func (p *player) fillPaint() (gfx.Paint, bool) {
	b := p.st.brush
	if b == nil {
		return gfx.Paint{}, false
	}
	switch b.style {
	case bsNull:
		return gfx.Paint{}, false
	case bsPattern, bsDIBPattern, bsDIBPatPT:
		if b.img != nil {
			return gfx.Solid(averageColor(b.img)), true
		}
		return gfx.Solid(gfx.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}), true
	}
	return gfx.Solid(b.color.color()), true
}

func averageColor(img image.Image) gfx.Color {
	b := img.Bounds()
	var r, g, bl, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r += float64(cr)
			g += float64(cg)
			bl += float64(cb)
			n++
		}
	}
	if n == 0 {
		return gfx.Black
	}
	return gfx.Color{R: r / n / 65535, G: g / n / 65535, B: bl / n / 65535, A: 1}
}

// drawShape fills (when filled) and strokes a logical-space path, or adds it
// to the path bracket.
func (p *player) drawShape(path *gfx.Path, fill bool) {
	if p.inPath {
		if p.path == nil {
			p.path = gfx.NewPath()
		}
		p.path.Append(path)
		return
	}
	p.syncClip()
	m := p.logical()
	if fill {
		if paint, ok := p.fillPaint(); ok {
			if p.st.brush.style == bsHatched {
				p.hatch(path, m, paint)
			} else {
				p.dev.Fill(path, m, p.st.polyFill, paint)
			}
		}
	}
	if s, paint, ok := p.strokeStyle(); ok {
		p.dev.Stroke(path, m, s, paint)
	}
}

// hatch draws a hatched brush: the background (opaque mode) and the hatch lines.
func (p *player) hatch(path *gfx.Path, m gfx.Matrix, paint gfx.Paint) {
	if p.st.bkMode == bkOpaque {
		p.dev.Fill(path, m, p.st.polyFill, gfx.Solid(p.st.bkColor.color()))
	}
	dp := path.Transformed(m)
	b := dp.Bounds()
	if b.Empty() {
		return
	}
	p.dev.PushClip(dp, gfx.Identity, p.st.polyFill)
	lines := gfx.NewPath()
	step := 8 * p.toOut.MeanScale()
	if step <= 0 {
		step = 8
	}
	hs := p.st.brush.hatch
	add := func(dx, dy float64) {
		// Lines through the box in direction (dx, dy), spaced step apart.
		diag := b.Width() + b.Height()
		n := int(diag/step) + 2
		if n > 5000 {
			n = 5000
		}
		for i := -n; i <= n; i++ {
			ox := (b.X0+b.X1)/2 + float64(i)*step*(-dy)
			oy := (b.Y0+b.Y1)/2 + float64(i)*step*dx
			lines.MoveTo(ox-dx*diag, oy-dy*diag)
			lines.LineTo(ox+dx*diag, oy+dy*diag)
		}
	}
	switch hs {
	case 0: // horizontal
		add(1, 0)
	case 1: // vertical
		add(0, 1)
	case 2: // forward diagonal
		add(math.Sqrt2/2, math.Sqrt2/2)
	case 3: // backward diagonal
		add(math.Sqrt2/2, -math.Sqrt2/2)
	case 4: // cross
		add(1, 0)
		add(0, 1)
	default: // diagonal cross
		add(math.Sqrt2/2, math.Sqrt2/2)
		add(math.Sqrt2/2, -math.Sqrt2/2)
	}
	p.dev.Stroke(lines, gfx.Identity, &gfx.StrokeStyle{Width: math.Max(1, p.toOut.MeanScale())}, paint)
	p.dev.PopClip()
}

func (p *player) strokeOnly(path *gfx.Path) {
	if p.inPath {
		if p.path == nil {
			p.path = gfx.NewPath()
		}
		p.path.Append(path)
		return
	}
	p.syncClip()
	if s, paint, ok := p.strokeStyle(); ok {
		p.dev.Stroke(path, p.logical(), s, paint)
	}
}

// ---- shapes in logical coordinates ----

func rectPath(l, t, r, b float64) *gfx.Path {
	path := gfx.NewPath()
	path.MoveTo(l, t)
	path.LineTo(r, t)
	path.LineTo(r, b)
	path.LineTo(l, b)
	path.Close()
	return path
}

func ellipsePath(l, t, r, b float64) *gfx.Path {
	path := gfx.NewPath()
	path.Ellipse((l+r)/2, (t+b)/2, math.Abs(r-l)/2, math.Abs(b-t)/2)
	return path
}

func roundRectPath(l, t, r, b, w, h float64) *gfx.Path {
	path := gfx.NewPath()
	x, y := math.Min(l, r), math.Min(t, b)
	path.RoundRect(x, y, math.Abs(r-l), math.Abs(b-t), w/2, h/2)
	return path
}

// arcPath is the GDI arc of the ellipse in the box from the radial through
// (xs, ys) to the one through (xe, ye), counter-clockwise unless cw; kind 0
// arc, 1 chord, 2 pie.
func arcPath(l, t, r, b, xs, ys, xe, ye float64, cw bool, kind int, lineFromCur bool, cur gfx.Point) *gfx.Path {
	cx, cy := (l+r)/2, (t+b)/2
	rx, ry := math.Abs(r-l)/2, math.Abs(b-t)/2
	path := gfx.NewPath()
	if rx == 0 || ry == 0 {
		return path
	}
	// Angles on the ellipse (y down: counter-clockwise on screen is negative).
	a0 := math.Atan2((ys-cy)/ry, (xs-cx)/rx)
	a1 := math.Atan2((ye-cy)/ry, (xe-cx)/rx)
	sweep := a1 - a0
	if cw {
		for sweep <= 0 {
			sweep += 2 * math.Pi
		}
	} else {
		for sweep >= 0 {
			sweep -= 2 * math.Pi
		}
	}
	if math.Abs(xs-xe) < 1e-9 && math.Abs(ys-ye) < 1e-9 {
		if cw {
			sweep = 2 * math.Pi
		} else {
			sweep = -2 * math.Pi
		}
	}
	if kind == 2 {
		path.MoveTo(cx, cy)
		path.Arc(cx, cy, rx, ry, 0, a0, sweep)
		path.Close()
		return path
	}
	if lineFromCur {
		path.MoveTo(cur.X, cur.Y)
	}
	path.Arc(cx, cy, rx, ry, 0, a0, sweep)
	if kind == 1 {
		path.Close()
	}
	return path
}

// ---- text ----

func encodingFor(charset uint8) encoding.Encoding {
	switch charset {
	case 128:
		return japanese.ShiftJIS
	case 129:
		return korean.EUCKR
	case 134:
		return simplifiedchinese.GBK
	case 136:
		return traditionalchinese.Big5
	case 161:
		return charmap.Windows1253
	case 162:
		return charmap.Windows1254
	case 177:
		return charmap.Windows1255
	case 178:
		return charmap.Windows1256
	case 186:
		return charmap.Windows1257
	case 204:
		return charmap.Windows1251
	case 222:
		return charmap.Windows874
	case 238:
		return charmap.Windows1250
	case 2:
		return charmap.Windows1252 // symbol fonts: the codes as Latin-1
	}
	return charmap.Windows1252
}

func decodeANSI(b []byte, charset uint8) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	s, err := encodingFor(charset).NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}

// fontQuery maps a LOGFONT face to font families.
func fontQuery(f *font) gfx.FontQuery {
	families := []string{}
	if f.face != "" {
		families = append(families, f.face)
	}
	switch strings.ToLower(f.face) {
	case "symbol", "wingdings", "webdings":
	case "times new roman", "times", "serif", "batang", "mingliu", "simsun":
		families = append(families, "Times New Roman", "Liberation Serif", "Noto Serif", "serif")
	case "courier new", "courier", "consolas", "lucida console":
		families = append(families, "Courier New", "Liberation Mono", "Noto Mono", "monospace")
	}
	families = append(families, "Arial", "Liberation Sans", "Noto Sans", "sans-serif")
	w := float64(f.weight)
	if w <= 0 {
		w = 400
	}
	return gfx.FontQuery{Families: families, Weight: w, Italic: f.italic}
}

// drawText draws text at a logical point with the current font and alignment;
// dx are optional per-character advances in logical units.
func (p *player) drawText(x, y float64, text string, dx []float64, opaqueRect *[4]float64) {
	p.drawTextDY(x, y, text, dx, nil, opaqueRect)
}

// drawTextDY also takes vertical offsets per character (ETO_PDY).
func (p *player) drawTextDY(x, y float64, text string, dx, dy []float64, opaqueRect *[4]float64) {
	if text == "" {
		return
	}
	p.syncClip()
	f := p.st.font
	m := p.logical()
	if opaqueRect != nil && p.st.bkMode == bkOpaque {
		r := opaqueRect
		p.dev.Fill(rectPath(r[0], r[1], r[2], r[3]), m, gfx.NonZero, gfx.Solid(p.st.bkColor.color()))
	}
	if p.st.textAlign&taUpdateCP != 0 {
		x, y = p.st.cur.X, p.st.cur.Y
	}
	q := fontQuery(f)
	height := math.Abs(f.height)
	if height == 0 {
		height = 12
	}
	metrics := p.fonts.Metrics(q, firstRune(text))
	size := height
	if f.height > 0 {
		// A positive height is the cell height (ascent + descent).
		if cell := metrics.Ascent + metrics.Descent; cell > 0 {
			size = height / cell
		}
	}
	shaped := p.fonts.Shape(text, q, size, "")
	runes := []rune(text)
	// Advances: the Dx array when given, else the shaped advances plus char extra.
	adv := shaped.Advance
	positions := make([]float64, len(runes)+1)
	rises := make([]float64, len(runes)+1)
	for i := range runes {
		if i < len(dy) {
			rises[i+1] = rises[i] + dy[i]
		} else {
			rises[i+1] = rises[i]
		}
	}
	if len(dx) >= len(runes) {
		for i := range runes {
			positions[i+1] = positions[i] + dx[i]
		}
		adv = positions[len(runes)]
	} else if p.st.charExtra != 0 {
		adv += p.st.charExtra * float64(len(runes))
	}
	// The y axis of the text: logical y may point up (mapping modes).
	pm := p.pageMatrix().Mul(p.st.world)
	flipY := pm.D < 0
	ox := 0.0
	switch p.st.textAlign & taCenter {
	case taCenter:
		ox = -adv / 2
	case taRight:
		ox = -adv
	}
	oy := 0.0
	switch p.st.textAlign & taBaseline {
	case taBaseline:
	case taBottom:
		oy = -metrics.Descent * size
	default: // top
		oy = metrics.Ascent * size
	}
	// Glyphs are drawn y down; escapement turns the line (counter-clockwise on screen).
	t := gfx.Translate(x, y)
	angle := f.escapement / 10 * math.Pi / 180
	if flipY {
		t = t.Mul(gfx.Scale(1, -1))
	}
	t = m.Mul(t.Mul(gfx.Rotate(-angle)))
	paint := gfx.Solid(p.st.textColor.color())
	for _, g := range shaped.Glyphs {
		gx := g.X
		if len(dx) >= len(runes) && g.Cluster < len(positions) {
			gx = positions[g.Cluster] + (g.X - clusterStart(shaped, g.Cluster))
		} else if p.st.charExtra != 0 {
			gx += p.st.charExtra * float64(g.Cluster)
		}
		gy := g.Y
		if g.Cluster < len(rises) {
			rise := rises[g.Cluster]
			if flipY {
				rise = -rise
			}
			gy += rise
		}
		if unit := p.fonts.UnitGlyph(g); unit != nil && !unit.Empty() {
			p.dev.Fill(unit, t.Mul(gfx.Translate(gx+ox, gy+oy)).Mul(gfx.Scale(g.Size, g.Size)), gfx.NonZero, paint)
		}
	}
	if f.underline || f.strikeout {
		th := size / 15
		lines := gfx.NewPath()
		if f.underline {
			lines.Rect(ox, oy+size*0.1, adv, th)
		}
		if f.strikeout {
			lines.Rect(ox, oy-size*0.3, adv, th)
		}
		p.dev.Fill(lines, t, gfx.NonZero, paint)
	}
	if p.st.textAlign&taUpdateCP != 0 {
		p.st.cur.X += adv
	}
}

func clusterStart(s gfx.ShapedText, cluster int) float64 {
	for _, g := range s.Glyphs {
		if g.Cluster == cluster {
			return g.X
		}
	}
	return 0
}

func firstRune(s string) rune {
	for _, r := range s {
		if r > ' ' {
			return r
		}
	}
	return 'x'
}

// ---- bitmaps ----

const (
	srcCopy   = 0x00CC0020
	srcAnd    = 0x008800C6
	srcPaint  = 0x00EE0086
	srcInvert = 0x00660046
	patCopy   = 0x00F00021
	blackness = 0x00000042
	whiteness = 0x00FF0062
	dstInvert = 0x00550009
)

// drawBitmap places img (already cropped to the source rectangle) in the
// logical destination rectangle, with the raster operation's usual meaning
// for masks (AND: white is see-through, OR / XOR: black is see-through).
func (p *player) drawBitmap(img image.Image, x, y, w, h float64, rop uint32) {
	if img == nil || w == 0 || h == 0 {
		return
	}
	p.syncClip()
	switch rop {
	case srcAnd:
		img = keyOut(img, true)
	case srcPaint, srcInvert:
		img = keyOut(img, false)
	}
	b := img.Bounds()
	m := p.logical().Mul(gfx.Translate(x, y)).Mul(gfx.Scale(w/float64(b.Dx()), h/float64(b.Dy())))
	p.dev.DrawImage(img, m, 1)
}

// keyOut makes white (or black) pixels transparent.
func keyOut(img image.Image, white bool) image.Image {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			key := r < 0x0800 && g < 0x0800 && bl < 0x0800
			if white {
				key = r > 0xF7FF && g > 0xF7FF && bl > 0xF7FF
			}
			if key {
				continue
			}
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = uint8(r>>8), uint8(g>>8), uint8(bl>>8), uint8(a>>8)
		}
	}
	return out
}

// fillRect paints a logical rectangle with the brush or a raster operation colour (PATBLT).
func (p *player) patBlt(x, y, w, h float64, rop uint32) {
	p.syncClip()
	var paint gfx.Paint
	switch rop {
	case blackness:
		paint = gfx.Solid(gfx.Black)
	case whiteness:
		paint = gfx.Solid(gfx.White)
	case patCopy:
		var ok bool
		if paint, ok = p.fillPaint(); !ok {
			return
		}
	default:
		return
	}
	p.dev.Fill(rectPath(x, y, x+w, y+h), p.logical(), gfx.NonZero, paint)
}
