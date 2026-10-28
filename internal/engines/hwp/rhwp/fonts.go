package rhwp

// Fonts for rhwp, chosen from the fonts installed on the computer (Prepare writes them into a copy of the HWPX).
// Nothing is downloaded or shipped.
//
//   - Language: documents Filee writes (Markdown, text, HTML → HWPX) name Korean fonts in every slot. In a Chinese or
//     Japanese document those drew Korean-style Hanja and, for characters they lack, a different fallback font per
//     character (issue #64). Without Hangul in the text, Korean fonts are swapped for an installed Chinese (Simplified
//     or Traditional) or Japanese font of the same style.
//   - Coverage: rhwp draws a character with the font of its slot when that font has it, else with a fallback of its
//     own. In text without Hangul, Han or kana, a slot's font is swapped for an installed one of the same style that
//     has every character of the text the old one had, and more of the rest.
//   - CFF2: variable fonts with CFF2 outlines (Source Han Serif VF) fail rhwp's font subsetter and the page came out
//     blank (issue #61); they are always swapped.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/dseif0x/filee-go/internal/engines/libreoffice"
)

// InstalledFont is an installed font: whether rhwp can embed it, which characters it has and how wide they are.
// Advance is the advance width of a character in em, ok false when the font lacks it; nil when not measured.
type InstalledFont struct {
	Usable  bool
	Has     func(rune) bool
	Advance func(rune) (float64, bool)
}

// Lookup finds an installed font by name, or nil.
type Lookup func(string) *InstalledFont

var styleWords = []string{"Thin", "ExtraLight", "Light", "Normal", "Regular", "Medium", "SemiBold", "Bold", "Heavy", "Black", "ExtraBold"}

type candidateFonts struct{ serif, sans []string }

// candidates are installed fonts in order of preference, by script, serif first then sans.
var candidates = map[string]candidateFonts{
	"zh-Hans": {[]string{"SimSun", "NSimSun", "Noto Serif SC", "Songti SC", "Noto Serif CJK SC"},
		[]string{"Microsoft YaHei", "DengXian", "Noto Sans SC", "PingFang SC", "Noto Sans CJK SC", "SimHei"}},
	"zh-Hant": {[]string{"PMingLiU", "MingLiU", "Noto Serif TC", "Songti TC", "Noto Serif CJK TC"},
		[]string{"Microsoft JhengHei", "Noto Sans TC", "PingFang TC", "Noto Sans CJK TC"}},
	"ja": {[]string{"Yu Mincho", "MS Mincho", "Noto Serif JP", "Hiragino Mincho ProN", "Noto Serif CJK JP"},
		[]string{"Yu Gothic", "Meiryo", "MS Gothic", "Noto Sans JP", "Hiragino Sans", "Noto Sans CJK JP"}},
	"ko": {[]string{"Batang", "HCR Batang", "Noto Serif KR", "AppleMyungjo", "Noto Serif CJK KR"},
		[]string{"Malgun Gothic", "Noto Sans KR", "Apple SD Gothic Neo", "NanumGothic", "Noto Sans CJK KR"}},
	// Wide coverage first (Tahoma and Times New Roman also have Greek, Cyrillic, Arabic, Hebrew; Tahoma Thai).
	"": {[]string{"Times New Roman", "Noto Serif", "DejaVu Serif", "Georgia", "Liberation Serif"},
		[]string{"Segoe UI", "Tahoma", "Arial", "Noto Sans", "DejaVu Sans", "Helvetica", "Liberation Sans"}},
}

// wideFonts are fonts for scripts rhwp falls back to on its own and the computer may have better ones for.
var wideFonts = []string{"Leelawadee UI", "Nirmala UI", "Microsoft Sans Serif", "Arial Unicode MS"}

// Simplified and Traditional forms of common characters: which form a Chinese text uses. Forms Japanese writes the
// same way (国, 学, 会, 点, …) are left out.
const (
	simplifiedOnly  = "这们说为时个对发经过还进动东车长门间问见现实从开关书无让应该话样"
	traditionalOnly = "這們說為時個對發經過還進動東車長門間問見現實從開關書無讓應該話樣"
)

type slotFace struct{ Slot, Face string }

// Plan is the replacements by (slot, face): the language, coverage and CFF2 rules above.
func Plan(slots []slotFace, text string, lookup Lookup) map[slotFace]string {
	characters := characters(text)
	language := Language(text)
	result := map[slotFace]string{}
	seen := map[slotFace]bool{}
	for _, sf := range slots {
		if seen[sf] || sf.Face == "" {
			continue
		}
		seen[sf] = true
		slot, face := sf.Slot, sf.Face
		serif := IsSerif(face)
		chosen := face
		current := lookup(face)

		// Korean fonts in a Chinese or Japanese text; a font rhwp cannot embed.
		if language != "ko" && language != "" && Script(face) == "ko" || current != nil && !current.Usable {
			script := language
			if own := Script(face); current != nil && !current.Usable && own != "" {
				script = own
			}
			for _, name := range preferred(script, serif) {
				if f := lookup(name); f != nil && f.Usable {
					chosen = name
					break
				}
			}
		}

		// Text without Hangul, Han or kana (Russian, Greek, Thai, Arabic, …): a font of the same style with every
		// character the chosen one has, and more of the others. In Korean, Chinese and Japanese text the language
		// font stays, and rhwp's own fallback draws the odd foreign word as before.
		var mine []rune
		if language == "" && slot != "HANJA" && slot != "JAPANESE" {
			for _, c := range characters {
				if !isHan(c) && !isKana(c) && (slot == "HANGUL" || !isHangul(c)) {
					mine = append(mine, c)
				}
			}
		}
		if font := lookup(chosen); len(mine) > 0 && font != nil && font.Usable {
			var covered []rune
			for _, c := range mine {
				if font.Has(c) {
					covered = append(covered, c)
				}
			}
			if len(covered) < len(mine) {
				best, bestCount := chosen, len(covered)
				var sameStyle []string
				if serif {
					sameStyle = append(append(sameStyle, styled(language).serif...), styled("").serif...)
				} else {
					sameStyle = append(append(append(sameStyle, styled(language).sans...), styled("").sans...), wideFonts...)
				}
				done := map[string]bool{}
				for _, name := range sameStyle {
					if done[strings.ToLower(name)] {
						continue
					}
					done[strings.ToLower(name)] = true
					other := lookup(name)
					if other == nil || !other.Usable || !all(covered, other.Has) {
						continue
					}
					count := 0
					for _, c := range mine {
						if other.Has(c) {
							count++
						}
					}
					if count == len(mine) {
						best, bestCount = name, count // the first one in order that has everything
						break
					}
					if count > bestCount {
						best, bestCount = name, count
					}
				}
				chosen = best
			}
		}
		if chosen != face {
			result[sf] = chosen
		}
	}
	return result
}

func all(runes []rune, has func(rune) bool) bool {
	for _, r := range runes {
		if !has(r) {
			return false
		}
	}
	return true
}

func preferred(script string, serif bool) []string {
	c := styled(script)
	if serif {
		return append(append([]string{}, c.serif...), c.sans...)
	}
	return append(append([]string{}, c.sans...), c.serif...)
}

func styled(script string) candidateFonts {
	if c, ok := candidates[script]; ok {
		return c
	}
	return candidates[""]
}

func isHangul(c rune) bool {
	return c >= 0xAC00 && c <= 0xD7A3 || c >= 0x1100 && c <= 0x11FF || c >= 0x3130 && c <= 0x318F
}

func isKana(c rune) bool { return c >= 0x3040 && c <= 0x30FF }

func isHan(c rune) bool {
	return c >= 0x4E00 && c <= 0x9FFF || c >= 0x3400 && c <= 0x4DBF || c >= 0x20000 && c <= 0x2FFFF
}

// Language is the language the Han characters of a text are in: Korean with Hangul, Japanese with kana, else
// Simplified or Traditional Chinese by common characters; "" without Han characters.
func Language(text string) string {
	hangul, kana, han, simplified, traditional := 0, 0, 0, 0, 0
	for _, c := range text {
		switch {
		case c >= '가' && c <= '힣' || c >= 'ᄀ' && c <= 'ᇿ' || c >= '㄰' && c <= '㆏':
			hangul++
		case c >= '぀' && c <= 'ヿ':
			kana++
		case c >= '一' && c <= '鿿' || c >= '㐀' && c <= '䶿':
			han++
			if strings.ContainsRune(simplifiedOnly, c) {
				simplified++
			} else if strings.ContainsRune(traditionalOnly, c) {
				traditional++
			}
		}
	}
	if hangul > 0 {
		return "ko"
	}
	// Japanese prose is mostly kana; a Chinese text quoting a Japanese title is not.
	if kana > 0 && (simplified+traditional == 0 || kana*3 >= han) {
		return "ja"
	}
	if han > 0 {
		if traditional > simplified {
			return "zh-Hant"
		}
		return "zh-Hans"
	}
	return ""
}

// characters are the distinct characters of the text that need a glyph (no spaces or controls), at most 4000.
func characters(text string) []rune {
	seen := map[rune]bool{}
	var out []rune
	for _, r := range text {
		if len(seen) >= 4000 {
			break
		}
		if unicode.In(r, unicode.Zs, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Mn, unicode.Co) || !assigned(r) {
			continue
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

func assigned(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z, unicode.C)
}

// Script is the script a font name points to: "思源宋体" and "SC" are Simplified Chinese, "명조" Korean, ….
func Script(face string) string {
	lower := strings.ToLower(face)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(lower, strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
	words := strings.FieldsFunc(face, func(r rune) bool { return r == ' ' || r == '-' || r == '_' })
	word := func(ws ...string) bool {
		for _, w := range words {
			for _, x := range ws {
				if strings.EqualFold(w, x) {
					return true
				}
			}
		}
		return false
	}
	anyIn := func(lo, hi rune) bool {
		for _, c := range face {
			if c >= lo && c <= hi {
				return true
			}
		}
		return false
	}
	switch {
	case word("SC", "CN", "GB") || has("简", "宋体", "黑体", "仿宋", "楷体"):
		return "zh-Hans"
	case word("TC", "HK", "TW") || has("繁", "明體", "黑體", "宋體"):
		return "zh-Hant"
	case word("JP", "J") || has("明朝", "ゴシック", "游"):
		return "ja"
	}
	if word("KR", "K") || anyIn('가', '힣') {
		return "ko"
	}
	for _, n := range append(append([]string{}, candidates["ko"].serif...), candidates["ko"].sans...) {
		if strings.EqualFold(n, face) {
			return "ko"
		}
	}
	if anyIn('一', '鿿') {
		return "zh-Hans"
	}
	return ""
}

// IsSerif tells serif (Song/Ming/Mincho/Batang) from sans (Hei/Gothic/Dotum).
func IsSerif(face string) bool {
	lower := strings.ToLower(face)
	for _, p := range []string{"Serif", "Song", "Ming", "Mincho", "Myeongjo", "Myungjo", "Batang", "Times", "宋", "明", "바탕", "명조", "仿宋", "楷"} {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// ───────────────────────── Installed fonts ─────────────────────────

type faceRef struct {
	path  string
	index int
	cff2  bool
}

var (
	indexOnce  sync.Once
	fontIndex  map[string][]faceRef
	foundMu    sync.Mutex
	found      = map[string]*InstalledFont{}
	parsedMu   sync.Mutex
	parsedFont = map[faceRef]*sfnt.Font{}
)

// ReleaseCache forgets the fonts looked up so far.
func ReleaseCache() {
	foundMu.Lock()
	found = map[string]*InstalledFont{}
	foundMu.Unlock()
}

// Forget rechecks one family after the user may have installed it during this session.
func Forget(face string) {
	foundMu.Lock()
	delete(found, strings.ToLower(face))
	foundMu.Unlock()
}

// Find is an installed font by name, or nil. "思源宋体 VF Medium" names a style of "思源宋体 VF", so trailing style
// words are tried off too. Family names in every language of a font's name table count.
func Find(face string) *InstalledFont {
	key := strings.ToLower(face)
	foundMu.Lock()
	if f, ok := found[key]; ok {
		foundMu.Unlock()
		return f
	}
	foundMu.Unlock()
	indexOnce.Do(buildIndex)
	var result *InstalledFont
	for _, candidate := range names(face) {
		refs := fontIndex[strings.ToLower(candidate)]
		if len(refs) == 0 {
			continue
		}
		ref := refs[0]
		result = &InstalledFont{
			Usable:  !ref.cff2,
			Has:     func(r rune) bool { return glyphOf(ref, r) != 0 },
			Advance: func(r rune) (float64, bool) { return advanceOf(ref, r) },
		}
		break
	}
	foundMu.Lock()
	found[key] = result
	foundMu.Unlock()
	return result
}

func names(face string) []string {
	out := []string{face}
	words := strings.Fields(face)
	for len(words) > 1 {
		last := words[len(words)-1]
		isStyle := false
		for _, s := range styleWords {
			if strings.EqualFold(s, last) {
				isStyle = true
			}
		}
		if !isStyle {
			break
		}
		words = words[:len(words)-1]
		out = append(out, strings.Join(words, " "))
	}
	return out
}

func buildIndex() {
	fontIndex = map[string][]faceRef{}
	for _, dir := range libreoffice.FontDirs() {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".ttf", ".otf", ".ttc", ".otc":
				indexFile(p)
			}
			return nil
		})
	}
}

// indexFile reads the table directories and name tables of a font file (only those bytes, not the outlines).
func indexFile(p string) {
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	read := func(offset int64, n int) []byte {
		if n <= 0 || n > 16<<20 {
			return nil
		}
		b := make([]byte, n)
		if _, err := f.ReadAt(b, offset); err != nil {
			return nil
		}
		return b
	}
	be := binary.BigEndian
	head := read(0, 12)
	if head == nil {
		return
	}
	offsets := []int64{0}
	if string(head[:4]) == "ttcf" {
		count := int(be.Uint32(head[8:]))
		if count <= 0 || count > 256 {
			return
		}
		list := read(12, 4*count)
		if list == nil {
			return
		}
		offsets = offsets[:0]
		for i := 0; i < count; i++ {
			offsets = append(offsets, int64(be.Uint32(list[4*i:])))
		}
	}
	for index, offset := range offsets {
		dir := read(offset, 12)
		if dir == nil {
			continue
		}
		tables := int(be.Uint16(dir[4:]))
		records := read(offset+12, 16*tables)
		if records == nil {
			continue
		}
		cff2 := false
		var nameOffset, nameLength int64
		for t := 0; t < tables; t++ {
			rec := records[16*t:]
			switch string(rec[:4]) {
			case "CFF2":
				cff2 = true
			case "name":
				nameOffset, nameLength = int64(be.Uint32(rec[8:])), int64(be.Uint32(rec[12:]))
			}
		}
		ref := faceRef{p, index, cff2}
		for _, n := range nameTable(read(nameOffset, int(nameLength))) {
			key := strings.ToLower(n)
			fontIndex[key] = append(fontIndex[key], ref)
		}
	}
}

// nameTable are the family (1, 16) and full names (4) of a name table, in every language.
func nameTable(b []byte) []string {
	if len(b) < 6 {
		return nil
	}
	be := binary.BigEndian
	count, storage := int(be.Uint16(b[2:])), int(be.Uint16(b[4:]))
	var out []string
	for i := 0; i < count && 6+12*i+12 <= len(b); i++ {
		rec := b[6+12*i:]
		platform, id := be.Uint16(rec), be.Uint16(rec[6:])
		length, offset := int(be.Uint16(rec[8:])), int(be.Uint16(rec[10:]))
		if id != 1 && id != 4 && id != 16 {
			continue
		}
		start := storage + offset
		if start+length > len(b) {
			continue
		}
		raw := b[start : start+length]
		var s string
		if platform == 0 || platform == 3 {
			u := make([]uint16, len(raw)/2)
			for j := range u {
				u[j] = be.Uint16(raw[2*j:])
			}
			s = string(utf16.Decode(u))
		} else {
			s = string(raw)
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parsed(ref faceRef) *sfnt.Font {
	parsedMu.Lock()
	defer parsedMu.Unlock()
	if f, ok := parsedFont[ref]; ok {
		return f
	}
	var result *sfnt.Font
	if data, err := os.ReadFile(ref.path); err == nil {
		if c, err := sfnt.ParseCollection(data); err == nil {
			result, _ = c.Font(ref.index)
		}
	}
	parsedFont[ref] = result
	return result
}

func glyphOf(ref faceRef, r rune) sfnt.GlyphIndex {
	f := parsed(ref)
	if f == nil {
		return 0
	}
	var buf sfnt.Buffer
	g, err := f.GlyphIndex(&buf, r)
	if err != nil {
		return 0
	}
	return g
}

func advanceOf(ref faceRef, r rune) (float64, bool) {
	f := parsed(ref)
	g := glyphOf(ref, r)
	if f == nil || g == 0 {
		return 0, false
	}
	var buf sfnt.Buffer
	em := f.UnitsPerEm()
	a, err := f.GlyphAdvance(&buf, g, fixed.I(int(em)), font.HintingNone)
	if err != nil || em == 0 {
		return 0, false
	}
	return float64(a) / 64.0 / float64(em), true
}
