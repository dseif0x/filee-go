package gfx

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

// FontQuery asks for a typeface. Families are tried in order (generic
// families such as "serif" and "monospace" included); the Go fonts that ship
// with Filee are the last resort.
type FontQuery struct {
	Families []string
	// Weight is the CSS weight (400 normal, 700 bold); 0 means 400.
	Weight float64
	Italic bool
}

// ShapedGlyph is one positioned glyph, in user units with y down and the
// baseline at 0.
type ShapedGlyph struct {
	Face    *font.Face
	GID     font.GID
	X, Y    float64
	Advance float64
	// Cluster is the index of the first rune of the glyph's cluster.
	Cluster int
	Size    float64
}

// ShapedText is a shaped line.
type ShapedText struct {
	Glyphs  []ShapedGlyph
	Advance float64
	// Ascent and Descent (positive) of the primary face at the size.
	Ascent, Descent float64
}

// Fonts finds typefaces and shapes text. It is safe for concurrent use.
type Fonts struct {
	mu       sync.Mutex
	fm       *fontscan.FontMap
	shaper   shaping.HarfbuzzShaper
	seg      shaping.Segmenter
	outlines map[glyphKey]*Path
	fallback *font.Face
	system   bool
}

type glyphKey struct {
	face *font.Face
	gid  font.GID
}

var (
	sharedFonts     *Fonts
	sharedFontsOnce sync.Once
	// SystemFonts is false in tests that must not depend on the machine's fonts.
	SystemFonts = os.Getenv("FILEE_NO_SYSTEM_FONTS") == ""
)

// SharedFonts returns the process-wide font set (system fonts plus the Go fonts).
func SharedFonts() *Fonts {
	sharedFontsOnce.Do(func() { sharedFonts = NewFonts(SystemFonts) })
	return sharedFonts
}

// NewFonts builds a font set; system adds the installed fonts.
func NewFonts(system bool) *Fonts {
	f := &Fonts{fm: fontscan.NewFontMap(log.New(io.Discard, "", 0)), outlines: map[glyphKey]*Path{}}
	if system {
		dir := ""
		if base, err := os.UserCacheDir(); err == nil {
			dir = filepath.Join(base, "filee", "fonts")
		}
		f.system = f.fm.UseSystemFonts(dir) == nil
	}
	for _, g := range []struct {
		data   []byte
		family string
		aspect font.Aspect
	}{
		{goregular.TTF, "Go", font.Aspect{Style: font.StyleNormal, Weight: font.WeightNormal}},
		{gobold.TTF, "Go", font.Aspect{Style: font.StyleNormal, Weight: font.WeightBold}},
		{goitalic.TTF, "Go", font.Aspect{Style: font.StyleItalic, Weight: font.WeightNormal}},
		{gobolditalic.TTF, "Go", font.Aspect{Style: font.StyleItalic, Weight: font.WeightBold}},
		{gomono.TTF, "Go Mono", font.Aspect{Style: font.StyleNormal, Weight: font.WeightNormal}},
		{gomonobold.TTF, "Go Mono", font.Aspect{Style: font.StyleNormal, Weight: font.WeightBold}},
	} {
		face, err := font.ParseTTF(bytes.NewReader(g.data))
		if err != nil {
			continue
		}
		f.fm.AddFace(face, fontscan.Location{File: fmt.Sprintf("go:%s:%v:%v", g.family, g.aspect.Style, g.aspect.Weight)}, font.Description{Family: g.family, Aspect: g.aspect})
		if f.fallback == nil {
			f.fallback = face
		}
	}
	return f
}

// HasFamily reports whether an installed font has the family.
func (f *Fonts) HasFamily(family string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.EqualFold(family, "Go") || strings.EqualFold(family, "Go Mono") {
		return true
	}
	_, ok := f.fm.FindSystemFont(family)
	return ok
}

func (f *Fonts) setQuery(q FontQuery) {
	families := append([]string(nil), q.Families...)
	mono := false
	for _, fam := range families {
		if strings.EqualFold(fam, "monospace") {
			mono = true
		}
	}
	if mono {
		families = append(families, "Go Mono")
	}
	families = append(families, "Go")
	w := q.Weight
	if w <= 0 {
		w = 400
	}
	style := font.StyleNormal
	if q.Italic {
		style = font.StyleItalic
	}
	f.fm.SetQuery(fontscan.Query{Families: families, Aspect: font.Aspect{Style: style, Weight: font.Weight(w)}})
}

// Shape shapes one line of text at size (user units per em).
func (f *Fonts) Shape(text string, q FontQuery, size float64, lang string) ShapedText {
	var out ShapedText
	if text == "" || !(size > 0) {
		return out
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setQuery(q)
	runes := []rune(text)
	primary := f.fm.ResolveFace(firstVisible(runes))
	if primary == nil {
		primary = f.fallback
	}
	if primary != nil {
		if ext, ok := primary.FontHExtents(); ok {
			upem := float64(primary.Upem())
			out.Ascent = float64(ext.Ascender) / upem * size
			out.Descent = -float64(ext.Descender) / upem * size
		}
	}
	// Shaping at a fixed size keeps fixed-point rounding small; positions are scaled after.
	const shapeSize = 1024
	input := shaping.Input{
		Text: runes, RunStart: 0, RunEnd: len(runes),
		Direction: di.DirectionLTR,
		Size:      fixed.I(shapeSize),
		Language:  language.NewLanguage(lang),
	}
	runs := f.seg.Split(input, faceMap{f})
	outputs := make([]shaping.Output, 0, len(runs))
	allRTL := len(runs) > 0
	for _, run := range runs {
		if run.Face == nil {
			run.Face = f.fallback
			if run.Face == nil {
				continue
			}
		}
		outputs = append(outputs, f.shaper.Shape(run))
		if run.Direction.Progression() != di.TowardTopLeft {
			allRTL = false
		}
	}
	if allRTL {
		for i, j := 0, len(outputs)-1; i < j; i, j = i+1, j-1 {
			outputs[i], outputs[j] = outputs[j], outputs[i]
		}
	}
	k := size / shapeSize
	x := 0.0
	for _, o := range outputs {
		for _, g := range o.Glyphs {
			adv := fx(g.Advance) * k
			out.Glyphs = append(out.Glyphs, ShapedGlyph{
				Face: o.Face, GID: g.GlyphID,
				X: x + fx(g.XOffset)*k, Y: -fx(g.YOffset) * k,
				Advance: adv, Cluster: g.ClusterIndex, Size: size,
			})
			x += adv
		}
	}
	out.Advance = x
	return out
}

func fx(v fixed.Int26_6) float64 { return float64(v) / 64 }

func firstVisible(rs []rune) rune {
	for _, r := range rs {
		if r > ' ' {
			return r
		}
	}
	if len(rs) > 0 {
		return rs[0]
	}
	return 'a'
}

// faceMap resolves faces with the font map, falling back to the Go font.
type faceMap struct{ f *Fonts }

func (m faceMap) ResolveFace(r rune) *font.Face {
	if face := m.f.fm.ResolveFace(r); face != nil {
		return face
	}
	return m.f.fallback
}

// GlyphPath returns the outline of a glyph at its size, origin at (x, y)
// on the baseline, y down.
func (f *Fonts) GlyphPath(g ShapedGlyph) *Path {
	unit := f.UnitGlyph(g)
	if unit == nil {
		return nil
	}
	return unit.Transformed(Matrix{A: g.Size, D: g.Size, E: g.X, F: g.Y})
}

// UnitGlyph is the shared outline of a glyph at size 1 (origin on the
// baseline, y down), marked Reusable so PDF output stores it once. Don't
// modify it.
func (f *Fonts) UnitGlyph(g ShapedGlyph) *Path {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := glyphKey{g.Face, g.GID}
	unit, ok := f.outlines[key]
	if !ok {
		unit = glyphOutline(g.Face, g.GID)
		if unit != nil {
			unit.Reusable = true
		}
		f.outlines[key] = unit
	}
	return unit
}

// GlyphMatrix places a unit glyph of g.
func GlyphMatrix(g ShapedGlyph) Matrix { return Matrix{A: g.Size, D: g.Size, E: g.X, F: g.Y} }

// TextPath returns the outlines of shaped text placed at (x, y).
func (f *Fonts) TextPath(t ShapedText, x, y float64) *Path {
	p := NewPath()
	for _, g := range t.Glyphs {
		g.X += x
		g.Y += y
		if gp := f.GlyphPath(g); gp != nil {
			p.Append(gp)
		}
	}
	return p
}

// glyphOutline is the glyph at size 1 (one em), y down.
func glyphOutline(face *font.Face, gid font.GID) *Path {
	if face == nil {
		return nil
	}
	data := face.GlyphData(gid)
	outline, ok := data.(font.GlyphOutline)
	if !ok {
		return nil
	}
	s := 1 / float64(face.Upem())
	p := NewPath()
	pt := func(a ot.SegmentPoint) (float64, float64) { return float64(a.X) * s, -float64(a.Y) * s }
	for _, seg := range outline.Segments {
		switch seg.Op {
		case ot.SegmentOpMoveTo:
			p.Close()
			x, y := pt(seg.Args[0])
			p.MoveTo(x, y)
		case ot.SegmentOpLineTo:
			x, y := pt(seg.Args[0])
			p.LineTo(x, y)
		case ot.SegmentOpQuadTo:
			cx, cy := pt(seg.Args[0])
			x, y := pt(seg.Args[1])
			p.QuadTo(cx, cy, x, y)
		case ot.SegmentOpCubeTo:
			c1x, c1y := pt(seg.Args[0])
			c2x, c2y := pt(seg.Args[1])
			x, y := pt(seg.Args[2])
			p.CubeTo(c1x, c1y, c2x, c2y, x, y)
		}
	}
	p.Close()
	return p
}

// FaceMetrics are a face's vertical metrics in em.
type FaceMetrics struct {
	Ascent, Descent, CapHeight, XHeight float64
}

// Metrics returns the metrics of the face that would draw r for q.
func (f *Fonts) Metrics(q FontQuery, r rune) FaceMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setQuery(q)
	face := f.fm.ResolveFace(r)
	if face == nil {
		face = f.fallback
	}
	m := FaceMetrics{Ascent: 0.8, Descent: 0.2, CapHeight: 0.7, XHeight: 0.5}
	if face == nil {
		return m
	}
	upem := float64(face.Upem())
	if ext, ok := face.FontHExtents(); ok {
		m.Ascent = float64(ext.Ascender) / upem
		m.Descent = -float64(ext.Descender) / upem
	}
	if v := float64(face.LineMetric(font.CapHeight)) / upem; v > 0.3 && v < 1.2 {
		m.CapHeight = v
	}
	if v := float64(face.LineMetric(font.XHeight)) / upem; v > 0.2 && v < 1 {
		m.XHeight = v
	}
	return m
}

// HasGlyph reports whether the face resolved for q covers r.
func (f *Fonts) Covers(q FontQuery, r rune) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setQuery(q)
	face := f.fm.ResolveFace(r)
	if face == nil {
		return false
	}
	_, ok := face.NominalGlyph(r)
	return ok
}

// AddFontFile adds a TrueType / OpenType font (or collection) file.
func (f *Fonts) AddFontFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fm.AddFont(bytes.NewReader(data), path, "")
}
