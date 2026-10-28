package cad

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// Fonts for CAD text. Drawings name SHX fonts (txt.shx, romans.shx, Korean
// big fonts) that Filee does not ship, so text is drawn with a sans-serif
// typeface; a TrueType style font (arial.ttf, malgun.ttf …) is used when
// installed. Characters the typeface lacks (Hangul, Hanzi …) fall back per
// character to a CJK font.

// Typeface is a resolved font choice.
type Typeface struct {
	Family       string // the installed family named by the style, "" for the default
	Bold, Italic bool
}

// GlyphRun is a piece of a text line with its advance in em.
type GlyphRun struct {
	Text    string
	Face    Typeface
	Advance float64
}

var sansFamilies = []string{"Noto Sans", "Arial", "Helvetica", "Liberation Sans", "DejaVu Sans", "Segoe UI"}

// cjkFamilies are tried for characters the main typeface lacks: Korean first
// (Filee's main audience), then Chinese and Japanese.
var cjkFamilies = []string{
	"Noto Sans KR", "Noto Sans CJK KR", "Malgun Gothic", "Apple SD Gothic Neo", "NanumGothic",
	"Noto Sans SC", "Noto Sans CJK SC", "Microsoft YaHei", "PingFang SC", "Yu Gothic", "MS Gothic",
}

// fileFamilies maps common Windows font files named by text styles to families.
var fileFamilies = map[string]string{
	"arial": "Arial", "arialn": "Arial Narrow", "times": "Times New Roman", "cour": "Courier New",
	"calibri": "Calibri", "tahoma": "Tahoma", "verdana": "Verdana", "isocpeur": "ISOCPEUR",
	"malgun": "Malgun Gothic", "gulim": "Gulim", "batang": "Batang", "dotum": "Dotum", "gungsuh": "Gungsuh",
	"simsun": "SimSun", "simhei": "SimHei", "msyh": "Microsoft YaHei", "msgothic": "MS Gothic", "msmincho": "MS Mincho",
}

// fonts is the font set CAD text uses (tests may replace it).
var fonts = gfx.SharedFonts

var (
	familyCache sync.Map // string → bool (installed)
	measureMu   sync.Mutex
	measured    = map[measureKey]float64{}
	metricsMu   sync.Mutex
	metricCache = map[Typeface][2]float64{}
)

type measureKey struct {
	text string
	face Typeface
}

func familyOf(familyOrFile string) string {
	name := strings.TrimSpace(familyOrFile)
	if name == "" {
		return ""
	}
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".shx") {
		return ""
	}
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	if f, ok := fileFamilies[strings.ToLower(name)]; ok {
		return f
	}
	return name
}

// ResolveFace is the typeface for a text style: its TrueType family if
// installed (family name or font file), else the default sans-serif typeface.
func ResolveFace(familyOrFile string, bold, italic bool) Typeface {
	family := familyOf(familyOrFile)
	if family != "" {
		installed, ok := familyCache.Load(family)
		if !ok {
			installed = fonts().HasFamily(family)
			familyCache.Store(family, installed)
		}
		if !installed.(bool) {
			family = ""
		}
	}
	return Typeface{Family: family, Bold: bold, Italic: italic}
}

func (t Typeface) query() gfx.FontQuery {
	fam := make([]string, 0, 2+len(sansFamilies)+len(cjkFamilies))
	if t.Family != "" {
		fam = append(fam, t.Family)
	}
	fam = append(fam, sansFamilies...)
	fam = append(fam, cjkFamilies...)
	fam = append(fam, "sans-serif")
	q := gfx.FontQuery{Families: fam, Italic: t.Italic, Weight: 400}
	if t.Bold {
		q.Weight = 700
	}
	return q
}

// Layout measures a line in em; fallback per character happens while shaping.
func Layout(text string, face Typeface) []GlyphRun {
	if text == "" {
		return nil
	}
	return []GlyphRun{{Text: text, Face: face, Advance: measure(text, face)}}
}

func measure(text string, face Typeface) float64 {
	key := measureKey{text, face}
	measureMu.Lock()
	if v, ok := measured[key]; ok {
		measureMu.Unlock()
		return v
	}
	measureMu.Unlock()
	adv := fonts().Shape(text, face.query(), 1, "").Advance
	if adv <= 0 && strings.TrimSpace(text) != "" {
		adv = 0.6 * float64(len([]rune(text)))
	}
	measureMu.Lock()
	if len(measured) > 200_000 {
		measured = map[measureKey]float64{}
	}
	measured[key] = adv
	measureMu.Unlock()
	return adv
}

// Metrics are the cap height and descent of a typeface in em.
func Metrics(face Typeface) (capHeight, descent float64) {
	metricsMu.Lock()
	if v, ok := metricCache[face]; ok {
		metricsMu.Unlock()
		return v[0], v[1]
	}
	metricsMu.Unlock()
	m := fonts().Metrics(face.query(), 'H')
	capHeight, descent = m.CapHeight, m.Descent
	// Some fonts report no cap height; 0.7 em is typical for sans-serif faces.
	if !(capHeight > 0.3 && capHeight < 1.2) {
		capHeight = 0.7
	}
	if !(descent > 0 && descent < 1) {
		descent = 0.21
	}
	metricsMu.Lock()
	metricCache[face] = [2]float64{capHeight, descent}
	metricsMu.Unlock()
	return
}
