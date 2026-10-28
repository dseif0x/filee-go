package fonts

import (
	"context"
	"encoding/binary"
	"errors"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

// Windows bitmap FON samples are made in memory, as Filee's own tests make
// them; the expected pixels are independent of the engine's reader.

type fntOptions struct {
	version, height, first, last, weight int
	fixedWidth, italic                   bool
	flags                                int // 0: none
	face                                 string
}

func defaultFnt(version int) fntOptions {
	return fntOptions{version: version, height: 13, first: 32, last: 127, weight: 400, fixedWidth: true, face: "Fixed"}
}

func artWidth(code int, fixedWidth bool) int {
	switch {
	case fixedWidth:
		return 8
	case code == 32:
		return 0
	case code%2 == 1:
		return 5
	}
	return 9
}

// artLit is the pixel art: a single pixel, a diagonal and hollow boxes.
func artLit(code, x, y, width, height int) bool {
	if x >= width || code == 32 {
		return false
	}
	rows := min(height, 7)
	switch code {
	case 33:
		return x == 2 && y == 0
	case 34:
		return x == 2+y%max(1, width-2)
	}
	return x >= 2 && y < rows && (x == 2 || x == width-1 || y == 0 || y == rows-1)
}

func fnt(o fntOptions) []byte {
	count := o.last - o.first + 1
	entrySize := 6
	if o.version == 2 {
		entrySize = 4
	} else if o.flags != 0 {
		entrySize = 8 + (bits.Len(uint(o.flags))-1-2)*2
	}
	table := 0x94
	if o.version == 2 {
		table = 0x76
	}
	face := table + (count+1)*entrySize
	bitsAt := face + 6
	data := make([]byte, bitsAt)
	p16 := func(off, v int) { binary.LittleEndian.PutUint16(data[off:], uint16(v)) }
	p32 := func(off, v int) { binary.LittleEndian.PutUint32(data[off:], uint32(v)) }
	p16(0, o.version<<8)
	p16(0x4A, min(o.height, 10))
	p16(0x4E, 2)
	p16(0x53, o.weight)
	if o.italic {
		data[0x50] = 1
	}
	if o.fixedWidth {
		p16(0x56, 8)
	}
	p16(0x58, o.height)
	p16(0x5D, 9)
	data[0x5F], data[0x60] = byte(o.first), byte(o.last)
	p32(0x69, face)
	p32(0x71, bitsAt)
	if o.version == 3 {
		flags := o.flags
		if flags == 0 {
			flags = 2
			if o.fixedWidth {
				flags = 1
			}
		}
		p32(0x76, flags)
		if o.flags != 0 {
			p16(0x7A, o.flags+1)
			p16(0x7C, o.flags+2)
			p16(0x7E, o.flags+3)
		}
	}
	for i := 0; i < 6 && i < len(o.face); i++ {
		data[face+i] = o.face[i]
	}
	for index := 0; index < count; index++ {
		code := o.first + index
		width := artWidth(code, o.fixedWidth)
		if o.flags != 0 {
			width = o.flags + index%3
		}
		entry := table + index*entrySize
		p16(entry, width)
		switch {
		case o.flags != 0:
			p16(entry+2, o.flags+index%3)
			p32(entry+4, len(data))
		case o.version == 2:
			p16(entry+2, len(data))
		default:
			p32(entry+2, len(data))
		}
		for column := 0; column < (width+7)/8; column++ {
			for y := 0; y < o.height; y++ {
				var v byte
				for bit := 0; bit < 8; bit++ {
					if artLit(code, column*8+bit, y, width, o.height) {
						v |= 0x80 >> bit
					}
				}
				data = append(data, v)
			}
		}
	}
	sentinel := table + count*entrySize
	p16(sentinel, 0)
	switch {
	case o.flags != 0:
		p32(sentinel+4, len(data))
	case o.version == 2:
		p16(sentinel+2, len(data))
	default:
		p32(sentinel+2, len(data))
	}
	p32(2, len(data))
	return data
}

// fonContainer writes NE FONTDIR and RT_FONT entries.
func fonContainer(resources ...[]byte) []byte {
	const ne, table = 0x40, 0x80
	payload := table + 2 + 8 + 12 + 8 + 12*len(resources) + 2
	data := make([]byte, payload)
	p16 := func(off, v int) { binary.LittleEndian.PutUint16(data[off:], uint16(v)) }
	data[0], data[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(data[0x3C:], ne)
	data[ne], data[ne+1] = 'N', 'E'
	p16(ne+0x24, table-ne)
	p16(ne+0x26, payload-ne)
	p16(table, 0)
	p16(table+2, 0x8007)
	p16(table+4, 1)
	p16(table+10, len(data))
	p16(table+12, 1)
	data = append(data, 0)
	fontType := table + 22
	p16(fontType, 0x8008)
	p16(fontType+2, len(resources))
	for i, r := range resources {
		entry := fontType + 8 + i*12
		p16(entry, len(data))
		p16(entry+2, len(r))
		p16(entry+6, 0x8001+i)
		data = append(data, r...)
	}
	return data
}

type fonKind struct {
	name       string
	fixedWidth bool
	first      int
	data       func() []byte
}

var fonKinds = []fonKind{
	{"Fon-v2-fixed", true, 32, func() []byte { return fonContainer(fnt(defaultFnt(2))) }},
	{"Fon-v2-proportional", false, 0, func() []byte {
		o := defaultFnt(2)
		o.fixedWidth, o.first = false, 0
		return fonContainer(fnt(o))
	}},
	{"Fon-v3-fixed", true, 32, func() []byte { return fonContainer(fnt(defaultFnt(3))) }},
	{"Fon-v3-proportional", false, 32, func() []byte {
		o := defaultFnt(3)
		o.fixedWidth = false
		return fonContainer(fnt(o))
	}},
}

func multiFon() []byte {
	o := defaultFnt(3)
	o.height = 14
	return fonContainer(fnt(defaultFnt(2)), fnt(defaultFnt(3)), fnt(o))
}

func mustReadFon(t *testing.T, data []byte) []fonStrike {
	t.Helper()
	var s []fonStrike
	var err error
	func() {
		defer recoverData(&err)
		s = readFon(context.Background(), data)
	}()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustBuildFon(t *testing.T, s *fonStrike, count, ordinal int) *sfnt {
	t.Helper()
	var f *sfnt
	var err error
	func() {
		defer recoverData(&err)
		f = buildFon(context.Background(), s, count, ordinal)
	}()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// inside tells whether a point is inside the outline (non-zero winding; all points are on-curve).
func inside(g *ttGlyph, x, y float64) bool {
	winding := 0
	for _, c := range contours(g) {
		for i := range c {
			a, b := c[i], c[(i+1)%len(c)]
			ay, by := float64(a.y), float64(b.y)
			if (ay <= y) != (by <= y) {
				cx := float64(a.x) + (y-ay)/(by-ay)*float64(b.x-a.x)
				if cx > x {
					if by > ay {
						winding++
					} else {
						winding--
					}
				}
			}
		}
	}
	return winding != 0
}

func TestGeneratedFonSamplesHaveExpectedResources(t *testing.T) {
	for _, c := range []struct {
		data           []byte
		count, version int
		pixelWidth     int
	}{
		{fonKinds[0].data(), 1, 0x200, 8},
		{fonKinds[1].data(), 1, 0x200, 0},
		{fonKinds[2].data(), 1, 0x300, 8},
		{fonKinds[3].data(), 1, 0x300, 0},
		{multiFon(), 3, 0x200, 8},
	} {
		s := mustReadFon(t, c.data)
		if len(s) != c.count || s[0].version != c.version || s[0].pixelWidth != c.pixelWidth || s[0].face != "Fixed" {
			t.Fatalf("%+v", s[0])
		}
	}
}

func TestEveryBitmapMaskMatchesTheOutline(t *testing.T) {
	scale := 1024 / 13
	for _, k := range fonKinds {
		s := mustReadFon(t, k.data())
		font := mustBuildFon(t, &s[0], 1, 1)
		glyphs := readAllGlyphs(font)
		adv, _ := metrics(font, "hhea", "hmtx")
		for i := 0; i <= 127-k.first; i++ {
			code := k.first + i
			width := artWidth(code, k.fixedWidth)
			if adv[i+1] != width*scale {
				t.Fatalf("%s code %d advance %d", k.name, code, adv[i+1])
			}
			g := glyphs[i+1]
			for y := 0; y < 13; y++ {
				for x := 0; x < width; x++ {
					want := artLit(code, x, y, width, 13)
					got := g != nil && inside(g, (float64(x)+0.5)*float64(scale), (float64(10-y)-0.5)*float64(scale))
					if want != got {
						t.Fatalf("%s code %d pixel (%d,%d): %v", k.name, code, x, y, got)
					}
				}
			}
			if g != nil {
				for _, p := range g.points {
					if !p.onCurve {
						t.Fatalf("code %d has an off-curve point", code)
					}
				}
			}
		}
		box := glyphs[35-k.first+1]
		if int(box.xMin) != 2*scale || box.numberOfContours != 2 {
			t.Fatalf("box: %d %d", box.xMin, box.numberOfContours)
		}
	}
}

func TestFonOpensInAllFiveFormats(t *testing.T) {
	for _, k := range fonKinds {
		for _, format := range Formats {
			out := mustConvert(t, k.name+".fon", k.data(), format)
			f := mustLoad(t, out)
			if f.flavor != trueTypeFlavor || f.glyphCount() != 127-k.first+2 {
				t.Fatalf("%s %s", k.name, format)
			}
			for _, tag := range []string{"head", "hhea", "maxp", "OS/2", "name", "cmap", "post", "glyf", "loca", "hmtx"} {
				if !f.has(tag) {
					t.Fatalf("%s %s: no %s", k.name, format, tag)
				}
			}
			if be16(f.tables["maxp"], 14) != 1 || be16(f.tables["head"], 48) != 2 {
				t.Fatal("maxZones / fontDirectionHint")
			}
		}
	}
}

func cmapOf(t *testing.T, f *sfnt) map[int]int {
	c := f.tables["cmap"]
	sub := c[20:]
	segX2 := int(be16(sub, 6))
	out := map[int]int{}
	for i := 0; i < segX2/2; i++ {
		end := int(be16(sub, 14+i*2))
		start := int(be16(sub, 16+segX2+i*2))
		d := int(be16(sub, 16+2*segX2+i*2))
		if end == 0xFFFF {
			continue
		}
		for ch := start; ch <= end; ch++ {
			out[ch] = (ch + d) & 0xFFFF
		}
	}
	return out
}

func TestCmapExcludesControlsAndUndefinedBytes(t *testing.T) {
	s := mustReadFon(t, fonKinds[1].data())[0]
	font := mustBuildFon(t, &s, 1, 1)
	m := cmapOf(t, font)
	if len(m) != 0x7F-0x20 {
		t.Fatal(len(m))
	}
	for c := 0x20; c < 0x7F; c++ {
		if m[c] != c+1 {
			t.Fatalf("%x → %d", c, m[c])
		}
	}
	// 0x7E..0xA0: the CP1252 high half, its five undefined bytes, and NBSP.
	px := make([][]bool, 13)
	for y := range px {
		px[y] = make([]bool, 8)
	}
	px[0][2] = true
	high := s
	high.glyphs = nil
	for b := 0x7E; b <= 0xA0; b++ {
		high.glyphs = append(high.glyphs, fonGlyph{byte(b), 8, px, 0})
	}
	high.defaultChar = 0
	font = mustBuildFon(t, &high, 1, 1)
	m = cmapOf(t, font)
	for _, undefined := range []int{0x81, 0x8D, 0x8F, 0x90, 0x9D} {
		for _, gid := range m {
			if gid == undefined-0x7E+1 {
				t.Fatalf("undefined byte %x is mapped", undefined)
			}
		}
	}
	if m[0x20AC] != 0x80-0x7E+1 || m[0xA0] != len(high.glyphs) || m[0x7E] != 1 || len(m) != 2+32-5 {
		t.Fatal(m)
	}
	if _, ok := m[0xFFFD]; ok {
		t.Fatal("U+FFFD")
	}
	// Cyrillic and Thai code pages.
	s2 := s
	s2.glyphs = nil
	for b := 0xA1; b <= 0xFE; b++ {
		s2.glyphs = append(s2.glyphs, fonGlyph{byte(b), 8, px, 0})
	}
	s2.defaultChar = 0
	for charset, ch := range map[byte]int{0: 0xC0, 204: 0x410, 222: 0xE20} {
		s2.charSet = charset
		if got := cmapOf(t, mustBuildFon(t, &s2, 1, 1))[ch]; got != 0xC0-0xA1+1 {
			t.Fatalf("charset %d: %d", charset, got)
		}
	}
}

func fontName(f *sfnt, id int) string { return readNameTable(f.tables["name"]).get(id) }

func TestNamesAndStyleBitsMatchEachResource(t *testing.T) {
	single := mustReadFon(t, fonKinds[0].data())[0]
	check := func(s fonStrike, family, style string, weight, fsSelection, macStyle, count, ordinal int) {
		t.Helper()
		f := mustBuildFon(t, &s, count, ordinal)
		if fontName(f, 1) != family || fontName(f, 2) != style || fontName(f, 4) != family+" "+style {
			t.Fatalf("%q %q %q", fontName(f, 1), fontName(f, 2), fontName(f, 4))
		}
		if int(be16(f.tables["OS/2"], 4)) != weight || int(be16(f.tables["OS/2"], 62)) != fsSelection || int(be16(f.tables["head"], 44)) != macStyle {
			t.Fatal("style bits")
		}
	}
	check(single, "Fixed", "Regular", 400, 0x40, 0, 1, 1)
	if n := fontName(mustBuildFon(t, &single, 1, 1), 6); n != "Fixed_20Regular" {
		t.Fatal(n)
	}
	bi := single
	bi.weight, bi.italic = 700, true
	check(bi, "Fixed", "Bold Italic", 700, 0x21, 3, 1, 1)
	w5 := single
	w5.weight = 500
	check(w5, "Fixed Weight500", "Regular", 500, 0x40, 0, 1, 1)
	check(single, "Fixed 13px-2", "Regular", 400, 0x40, 0, 3, 2)
	if n := fontName(mustBuildFon(t, &single, 3, 2), 6); n != "Fixed_2013px-2_20Regular" {
		t.Fatal(n)
	}
	long := single
	long.face = strings.Repeat("Ä", 40)
	ps := fontName(mustBuildFon(t, &long, 1, 1), 6)
	if len(ps) > 63 || !strings.HasPrefix(ps, strings.Repeat("_C3_84", 8)) || ps[48] != '-' {
		t.Fatal(ps)
	}
}

func TestMultiResourceOutputsHaveDistinctNames(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "Name.fon")
	data := multiFon()
	// The second strike bold italic: it keeps its own file.
	second := int(binary.LittleEndian.Uint16(data[0x80+2+8+12+8+12:])) // offset of resource 1
	binary.LittleEndian.PutUint16(data[second+0x53:], 700)
	data[second+0x50] = 1
	os.WriteFile(in, data, 0o644)
	var suffixes []string
	step := &convert.Step{InputPath: in, From: "fon", To: "ttf", WorkDir: dir, Output: convert.AllocatorFunc(func(ext, suffix string) string {
		suffixes = append(suffixes, suffix)
		return filepath.Join(dir, "Name"+suffix+"."+ext)
	})}
	outs, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(suffixes, []string{"-13px", "-13px-BoldItalic", "-14px"}) || len(outs) != 3 {
		t.Fatal(suffixes)
	}
	var families, styles []string
	for _, o := range outs {
		b, _ := os.ReadFile(o)
		f := mustLoad(t, b)
		families = append(families, fontName(f, 1))
		styles = append(styles, fontName(f, 2))
	}
	if !slices.Equal(families, []string{"Fixed 13px", "Fixed 13px", "Fixed 14px"}) || !slices.Equal(styles, []string{"Regular", "Bold Italic", "Regular"}) {
		t.Fatal(families, styles)
	}
}

func refusalKey(err error) string {
	var r *convert.RefusedError
	if errors.As(err, &r) {
		return r.Key
	}
	return ""
}

func TestFonRefusals(t *testing.T) {
	good := fonKinds[0].data()
	pe := slices.Clone(good)
	pe[0x40], pe[0x41] = 'P', 'E'
	vector := slices.Clone(good)
	start := int(binary.LittleEndian.Uint16(vector[0x80+2+8+12+8:]))
	binary.LittleEndian.PutUint16(vector[start+0x42:], 1)
	charset := slices.Clone(good)
	charset[start+0x55] = 128 // Shift-JIS
	for _, c := range []struct {
		data []byte
		key  string
	}{
		{[]byte("not a font at all, really not"), "error.fon_damaged"},
		{pe, "error.fon_unsupported"},
		{vector, "error.fon_vector"},
		{charset, "error.fon_unsupported"},
		{good[:len(good)-40], "error.fon_damaged"},
	} {
		_, err := convertFile(t, "x.fon", c.data, "ttf")
		if refusalKey(err) != c.key {
			t.Fatalf("want %s, got %v", c.key, err)
		}
	}
	// Unsupported v3 glyph table flags.
	o := defaultFnt(3)
	o.flags = 0x04
	if _, err := convertFile(t, "x.fon", fonContainer(fnt(o)), "ttf"); refusalKey(err) != "error.fon_unsupported" {
		t.Fatal(err)
	}
}

func TestFonCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	in := filepath.Join(dir, "x.fon")
	os.WriteFile(in, fonKinds[0].data(), 0o644)
	step := &convert.Step{InputPath: in, From: "fon", To: "ttf", Output: convert.AllocatorFunc(func(ext, s string) string { return filepath.Join(dir, "o"+s+"."+ext) })}
	if _, err := New().Convert(ctx, step, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "o.ttf")); err == nil {
		t.Fatal("output written")
	}
}
