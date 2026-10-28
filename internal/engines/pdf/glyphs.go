// Text drawn as glyph outlines (filled paths), so the stamps need no embedded
// font and any script works that a font on the PC (or the bundled Go fonts,
// for Latin, Greek and Cyrillic) has glyphs for. Glyphs are placed one after
// another with kerning; complex scripts are not shaped (the C# original used
// HarfBuzz), right-to-left text is put in visual order.

package pdf

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// pathSeg is one piece of an outline, in points, y down: 'M' move, 'L'
// line, 'C' cubic curve (three points), 'Z' close.
type pathSeg struct {
	Op  byte
	Pts [3][2]float64
}

// outline is a text's glyph outlines at a size, baseline at y = 0.
type outline struct {
	Segs                   []pathSeg
	MinX, MinY, MaxX, MaxY float64
}

func (o *outline) empty() bool { return len(o.Segs) == 0 }
func (o *outline) midX() float64 { return (o.MinX + o.MaxX) / 2 }
func (o *outline) midY() float64 { return (o.MinY + o.MaxY) / 2 }

var (
	goFontsOnce          sync.Once
	goRegular, goBoldFnt *sfnt.Font
)

func goFonts() (*sfnt.Font, *sfnt.Font) {
	goFontsOnce.Do(func() {
		goRegular, _ = sfnt.Parse(goregular.TTF)
		goBoldFnt, _ = sfnt.Parse(gobold.TTF)
	})
	return goRegular, goBoldFnt
}

// covers tells whether the font has a glyph for every letter of text.
func covers(f *sfnt.Font, text string) bool {
	var buf sfnt.Buffer
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.Is(unicode.Mn, r) || r < 0x20 {
			continue
		}
		if i, err := f.GlyphIndex(&buf, r); err != nil || i == 0 {
			return false
		}
	}
	return true
}

var (
	systemFontsMu sync.Mutex
	systemFonts   []string // font files, found once
	fontCache     = map[string]*sfnt.Font{}
)

func fontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		return []string{filepath.Join(windir, "Fonts"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts")}
	case "darwin":
		return []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts")}
	}
	return []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(home, ".local", "share", "fonts"), filepath.Join(home, ".fonts")}
}

func listSystemFonts() []string {
	systemFontsMu.Lock()
	defer systemFontsMu.Unlock()
	if systemFonts != nil {
		return systemFonts
	}
	systemFonts = []string{}
	for _, dir := range fontDirs() {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || len(systemFonts) > 5000 {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
				systemFonts = append(systemFonts, path)
			}
			return nil
		})
	}
	// Regular faces before bold and the like, so a plain match is found first.
	slices.SortStableFunc(systemFonts, func(a, b string) int {
		return boolInt(isStyled(a)) - boolInt(isStyled(b))
	})
	return systemFonts
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isStyled(path string) bool {
	n := strings.ToLower(filepath.Base(path))
	return strings.Contains(n, "bold") || strings.Contains(n, "italic") || strings.Contains(n, "light") || strings.Contains(n, "thin")
}

func loadFonts(path string) []*sfnt.Font {
	systemFontsMu.Lock()
	defer systemFontsMu.Unlock()
	if f, ok := fontCache[path]; ok {
		if f == nil {
			return nil
		}
		return []*sfnt.Font{f}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fontCache[path] = nil
		return nil
	}
	if c, err := sfnt.ParseCollection(data); err == nil {
		var out []*sfnt.Font
		for i := 0; i < c.NumFonts() && i < 8; i++ {
			if f, err := c.Font(i); err == nil {
				out = append(out, f)
			}
		}
		if len(out) > 0 {
			fontCache[path] = out[0]
		}
		return out
	}
	fontCache[path] = nil
	return nil
}

// fontFor picks a font for text: the Go fonts when they have every glyph,
// else the first font on the PC that does.
func fontFor(text string, bold bool) *sfnt.Font {
	regular, b := goFonts()
	f := regular
	if bold {
		f = b
	}
	if covers(f, text) {
		return f
	}
	for _, path := range listSystemFonts() {
		for _, sf := range loadFonts(path) {
			if covers(sf, text) {
				return sf
			}
		}
	}
	return f
}

// visualOrder puts mostly right-to-left text in the order it is drawn.
func visualOrder(text string) string {
	rtl, ltr := 0, 0
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Arabic, unicode.Hebrew, unicode.Syriac, unicode.Thaana):
			rtl++
		case unicode.IsLetter(r):
			ltr++
		}
	}
	if rtl <= ltr {
		return text
	}
	runes := []rune(text)
	slices.Reverse(runes)
	return string(runes)
}

// textOutline returns the outlines of text at size points.
func textOutline(text string, size float64, bold bool) outline {
	text = visualOrder(text)
	f := fontFor(text, bold)
	var out outline
	if f == nil {
		return out
	}
	var buf sfnt.Buffer
	ppem := fixed.Int26_6(size * 64)
	var x float64
	prev := sfnt.GlyphIndex(0)
	first := true
	for _, r := range text {
		idx, err := f.GlyphIndex(&buf, r)
		if err != nil {
			continue
		}
		if !first && prev != 0 {
			if k, err := f.Kern(&buf, prev, idx, ppem, font.HintingNone); err == nil {
				x += float64(k) / 64
			}
		}
		segs, err := f.LoadGlyph(&buf, idx, ppem, nil)
		if err == nil {
			open := false
			var cur [2]float64
			for _, s := range segs {
				p := func(i int) [2]float64 {
					return [2]float64{x + float64(s.Args[i].X)/64, float64(s.Args[i].Y) / 64}
				}
				switch s.Op {
				case sfnt.SegmentOpMoveTo:
					if open {
						out.Segs = append(out.Segs, pathSeg{Op: 'Z'})
					}
					cur = p(0)
					out.Segs = append(out.Segs, pathSeg{Op: 'M', Pts: [3][2]float64{cur}})
					open = true
				case sfnt.SegmentOpLineTo:
					cur = p(0)
					out.Segs = append(out.Segs, pathSeg{Op: 'L', Pts: [3][2]float64{cur}})
				case sfnt.SegmentOpQuadTo:
					// A quadratic curve as a cubic one.
					c, e := p(0), p(1)
					c1 := [2]float64{cur[0] + 2.0/3*(c[0]-cur[0]), cur[1] + 2.0/3*(c[1]-cur[1])}
					c2 := [2]float64{e[0] + 2.0/3*(c[0]-e[0]), e[1] + 2.0/3*(c[1]-e[1])}
					out.Segs = append(out.Segs, pathSeg{Op: 'C', Pts: [3][2]float64{c1, c2, e}})
					cur = e
				case sfnt.SegmentOpCubeTo:
					out.Segs = append(out.Segs, pathSeg{Op: 'C', Pts: [3][2]float64{p(0), p(1), p(2)}})
					cur = p(2)
				}
			}
			if open {
				out.Segs = append(out.Segs, pathSeg{Op: 'Z'})
			}
		}
		if adv, err := f.GlyphAdvance(&buf, idx, ppem, font.HintingNone); err == nil {
			x += float64(adv) / 64
		}
		prev, first = idx, false
	}
	// Bounds of the points (control points included: close enough, and safe).
	init := false
	for _, s := range out.Segs {
		n := map[byte]int{'M': 1, 'L': 1, 'C': 3}[s.Op]
		for i := 0; i < n; i++ {
			px, py := s.Pts[i][0], s.Pts[i][1]
			if !init {
				out.MinX, out.MaxX, out.MinY, out.MaxY, init = px, px, py, py, true
				continue
			}
			out.MinX, out.MaxX = min(out.MinX, px), max(out.MaxX, px)
			out.MinY, out.MaxY = min(out.MinY, py), max(out.MaxY, py)
		}
	}
	return out
}
