package fonts

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/andybalholm/brotli"
	xsfnt "golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/dseif0x/filee-go/internal/convert"
)

// The test fonts in testdata are small subsets of Noto Sans and Noto Sans KR
// (SIL OFL 1.1, see testdata/OFL.txt) made for Filee's own tests: hinted
// TrueType with composite glyphs, CID-keyed CFF with subroutines and VORG, and
// a name-keyed CFF font with a seac accent, flex and subroutines; plus the
// TrueType one compressed by Google's reference WOFF2 encoder.

const (
	trueTypeSample = "FileeTest-TrueType.ttf"
	cidSample      = "FileeTest-CID.otf"
	nameKeyed      = "FileeTest-NameKeyed.otf"
	googleWoff2    = "FileeTest-TrueType.google.woff2"
)

func sample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustLoad(t *testing.T, data []byte) *sfnt {
	t.Helper()
	var f *sfnt
	var err error
	func() {
		defer recoverData(&err)
		f = loadFont(context.Background(), data)
	}()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func mustSave(t *testing.T, f *sfnt, format string) []byte {
	t.Helper()
	var out []byte
	var err error
	func() {
		defer recoverData(&err)
		out = saveFont(context.Background(), f, format, nil)
	}()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// convertFile runs the engine on data saved as name and returns the output bytes or the error.
func convertFile(t *testing.T, name string, data []byte, to string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, name)
	os.WriteFile(in, data, 0o644)
	from := strings.TrimPrefix(filepath.Ext(name), ".")
	step := &convert.Step{InputPath: in, From: from, To: to, WorkDir: dir,
		Output: convert.AllocatorFunc(func(ext, suffix string) string { return filepath.Join(dir, "out"+suffix+"."+ext) })}
	outs, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		return nil, err
	}
	if len(outs) != 1 {
		t.Fatalf("outputs %v", outs)
	}
	return os.ReadFile(outs[0])
}

func mustConvert(t *testing.T, name string, data []byte, to string) []byte {
	t.Helper()
	b, err := convertFile(t, name, data, to)
	if err != nil {
		t.Fatalf("%s → %s: %v", name, to, err)
	}
	return b
}

func TestEveryFontFormatConvertsToEveryOtherInOneStep(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	for _, from := range append(slices.Clone(Formats), "fon") {
		for _, to := range Formats {
			if from == to {
				continue
			}
			r := p.Plan(from, to, convert.Options{})
			if r == nil || len(r.Steps) != 1 || r.Steps[0].Converter.ID() != "font" {
				t.Fatalf("%s → %s: %+v", from, to, r)
			}
		}
	}
	if n := len(New().Edges()); n != 25 {
		t.Fatal(n)
	}
}

// sfntBytes is the plain SFNT of any output, for x/image's parser.
func sfntBytes(t *testing.T, data []byte) []byte {
	return mustLoad(t, data).write()
}

func TestFontConvertsAndTheResultLoads(t *testing.T) {
	for _, s := range []string{trueTypeSample, cidSample, nameKeyed} {
		src := mustLoad(t, sample(t, s))
		for _, to := range Formats {
			if to == strings.TrimPrefix(filepath.Ext(s), ".") {
				continue
			}
			out := mustConvert(t, s, sample(t, s), to)
			res := mustLoad(t, out)
			if res.glyphCount() != src.glyphCount() {
				t.Fatalf("%s → %s: %d glyphs", s, to, res.glyphCount())
			}
			if want := to == "ttf" || to == "eot" || !src.hasCffOutlines(); want != !res.hasCffOutlines() {
				t.Fatalf("%s → %s: CFF outlines %v", s, to, res.hasCffOutlines())
			}
			f, err := xsfnt.Parse(sfntBytes(t, out))
			if err != nil {
				t.Fatalf("%s → %s: %v", s, to, err)
			}
			if f.NumGlyphs() != src.glyphCount() {
				t.Fatal(f.NumGlyphs())
			}
			text := "Filee"
			if s == nameKeyed {
				text = "Aae"
			}
			var buf xsfnt.Buffer
			for i, r := range text {
				gi, err := f.GlyphIndex(&buf, r)
				if err != nil || gi == 0 {
					t.Fatalf("%s → %s: %q: %v %v", s, to, r, gi, err)
				}
				if i == 0 {
					segs, err := f.LoadGlyph(&buf, gi, fixed.I(48), nil)
					if err != nil || len(segs) == 0 {
						t.Fatalf("%s → %s: outline of %q: %v", s, to, r, err)
					}
				}
			}
		}
	}
}

func normalizeHead(h []byte) []byte {
	c := slices.Clone(h)
	clear(c[8:12])
	c[16] &= 0xF7 // flags bit 11
	return c
}

func assertSameTables(t *testing.T, expected, actual *sfnt, except ...string) {
	t.Helper()
	if expected.flavor != actual.flavor {
		t.Fatalf("flavor %x vs %x", expected.flavor, actual.flavor)
	}
	keep := func(f *sfnt) []string {
		var out []string
		for _, tag := range f.sortedTags() {
			if !slices.Contains(except, tag) && tag != "DSIG" {
				out = append(out, tag)
			}
		}
		return out
	}
	if e, a := keep(expected), keep(actual); !slices.Equal(e, a) {
		t.Fatalf("tables %v vs %v", e, a)
	}
	for _, tag := range keep(expected) {
		e, a := expected.tables[tag], actual.tables[tag]
		if tag == "head" {
			e, a = normalizeHead(e), normalizeHead(a)
		}
		if !bytes.Equal(e, a) {
			t.Fatalf("table '%s' differs", tag)
		}
	}
}

func assertSameGlyphs(t *testing.T, expected, actual *sfnt, overlapBitmap bool) {
	t.Helper()
	a, b := readAllGlyphs(expected), readAllGlyphs(actual)
	if len(a) != len(b) {
		t.Fatal(len(a), len(b))
	}
	composite, overlap := false, false
	for i := range a {
		e := a[i]
		if e == nil || e.numberOfContours == 0 {
			if b[i] != nil && b[i].numberOfContours != 0 {
				t.Fatalf("glyph %d should be empty", i)
			}
			continue
		}
		composite = composite || e.isComposite()
		overlap = overlap || e.overlapSimple
		g := b[i]
		if g == nil || g.numberOfContours != e.numberOfContours || g.xMin != e.xMin || g.yMin != e.yMin || g.xMax != e.xMax || g.yMax != e.yMax ||
			!slices.Equal(e.endPoints, g.endPoints) || !slices.Equal(e.points, g.points) || !bytes.Equal(e.instructions, g.instructions) ||
			(e.overlapSimple && overlapBitmap) != g.overlapSimple || !bytes.Equal(e.components, g.components) {
			t.Fatalf("glyph %d differs", i)
		}
	}
	if !composite || !overlap {
		t.Fatal("the sample should have composite and overlapping glyphs")
	}
}

func TestRoundTripKeepsEveryTable(t *testing.T) {
	for _, c := range [][2]string{{trueTypeSample, "woff"}, {trueTypeSample, "eot"}, {trueTypeSample, "otf"}, {cidSample, "woff"}, {cidSample, "woff2"}, {nameKeyed, "woff2"}} {
		ext := strings.TrimPrefix(filepath.Ext(c[0]), ".")
		middle := mustConvert(t, c[0], sample(t, c[0]), c[1])
		back := mustConvert(t, "middle."+c[1], middle, ext)
		assertSameTables(t, mustLoad(t, sample(t, c[0])), mustLoad(t, back))
	}
}

func TestTtfToWoff2AndBackKeepsGlyphOutlines(t *testing.T) {
	src := mustLoad(t, sample(t, trueTypeSample))
	woff2 := mustConvert(t, trueTypeSample, sample(t, trueTypeSample), "woff2")
	back := mustLoad(t, mustConvert(t, "x.woff2", woff2, "ttf"))
	assertSameTables(t, src, back, "glyf", "loca")
	assertSameGlyphs(t, src, back, true)
	if be16(back.tables["head"], 16)&(1<<11) == 0 {
		t.Fatal("lossless transform bit")
	}
}

type dirEntry struct {
	version     int
	transformed bool
}

func woff2Directory(t *testing.T, data []byte) map[string]dirEntry {
	r := newReader(data)
	r.pos = 12
	count := int(r.u16())
	r.pos = 48
	out := map[string]dirEntry{}
	for i := 0; i < count; i++ {
		flags := r.u8()
		var tag string
		if flags&63 == 63 {
			tag = tagName(r.u32())
		} else {
			tag = knownTags[flags&63]
		}
		version := int(flags >> 6)
		r.uintBase128()
		transformed := version != 0
		if tag == "glyf" || tag == "loca" {
			transformed = version != 3
		}
		if transformed {
			r.uintBase128()
		}
		out[tag] = dirEntry{version, transformed}
	}
	return out
}

func TestWoff2UsesTheTransformsAndBeatsWoff(t *testing.T) {
	woff2 := mustConvert(t, trueTypeSample, sample(t, trueTypeSample), "woff2")
	woff := mustConvert(t, trueTypeSample, sample(t, trueTypeSample), "woff")
	google := sample(t, googleWoff2)
	d := woff2Directory(t, woff2)
	if d["glyf"] != (dirEntry{0, true}) || d["loca"] != (dirEntry{0, true}) || d["hmtx"] != (dirEntry{1, true}) || d["cmap"] != (dirEntry{0, false}) {
		t.Fatal(d)
	}
	if len(woff2)%4 != 0 || int(be32(woff2, 8)) != len(woff2) {
		t.Fatal("length")
	}
	if float64(len(woff2)) >= float64(len(woff))*0.8 {
		t.Fatalf("WOFF2 %d vs WOFF %d", len(woff2), len(woff))
	}
	if float64(len(woff2)) >= float64(len(google))*1.05 {
		t.Fatalf("Filee %d vs Google %d", len(woff2), len(google))
	}
}

func TestWoff2FromGooglesReferenceEncoderDecodesToTheSource(t *testing.T) {
	src := mustLoad(t, sample(t, trueTypeSample))
	google := sample(t, googleWoff2)
	if woff2Directory(t, google)["glyf"] != (dirEntry{0, true}) {
		t.Fatal("google glyf")
	}
	// The encoder build used doesn't write the overlap bitmap, so OVERLAP_SIMPLE is lost in that file.
	dec := mustLoad(t, google)
	assertSameTables(t, src, dec, "glyf", "loca")
	assertSameGlyphs(t, src, dec, false)
	assertSameGlyphs(t, src, mustLoad(t, mustConvert(t, googleWoff2, google, "ttf")), false)
}

func TestWoff2CollectionYieldsItsFirstFont(t *testing.T) {
	first, second := mustLoad(t, sample(t, trueTypeSample)), mustLoad(t, sample(t, nameKeyed))
	type tbl struct {
		tag  string
		data []byte
	}
	var tables []tbl
	var dirs [][]int
	for _, f := range []*sfnt{first, second} {
		var idx []int
		for _, tag := range f.sortedTags() {
			idx = append(idx, len(tables))
			tables = append(tables, tbl{tag, f.tables[tag]})
		}
		dirs = append(dirs, idx)
	}
	var stream bytes.Buffer
	for _, x := range tables {
		stream.Write(x.data)
	}
	var comp bytes.Buffer
	bw := brotli.NewWriterLevel(&comp, 5)
	bw.Write(stream.Bytes())
	bw.Close()
	w := newWriter(0)
	w.u32(woff2Signature)
	w.u32(collectionTag)
	w.u32(0)
	w.u16(len(tables))
	w.u16(0)
	w.u32(0)
	w.u32(uint32(comp.Len()))
	w.zeros(24)
	for _, x := range tables {
		if x.tag == "glyf" || x.tag == "loca" {
			w.u8(63 | 3<<6)
		} else {
			w.u8(63)
		}
		w.u32(tagValue(x.tag))
		w.uintBase128(uint32(len(x.data)))
	}
	w.u32(0x00010000)
	w.write255UInt16(len(dirs))
	for i, idx := range dirs {
		w.write255UInt16(len(idx))
		w.u32([]uint32{first.flavor, second.flavor}[i])
		for _, j := range idx {
			w.write255UInt16(j)
		}
	}
	w.bytes(comp.Bytes())
	dec := mustLoad(t, w.buf)
	if dec.flavor != trueTypeFlavor {
		t.Fatal(dec.flavor)
	}
	assertSameTables(t, first, dec)
}

func metrics(f *sfnt, header, table string) ([]int, []int) {
	count := int(be16(f.tables[header], 34))
	r := newReader(f.tables[table])
	n := f.glyphCount()
	adv, bear := make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		if i < count {
			adv[i] = int(r.u16())
		} else {
			adv[i] = adv[i-1]
		}
		bear[i] = int(r.i16())
	}
	return adv, bear
}

func contours(g *ttGlyph) [][]glyphPoint {
	var out [][]glyphPoint
	start := 0
	for _, e := range g.endPoints {
		out = append(out, g.points[start:int(e)+1])
		start = int(e) + 1
	}
	return out
}

// signedArea is the shoelace area of the control polygon: negative for clockwise contours (y up).
func signedArea(p []glyphPoint) int64 {
	var twice int64
	for i := range p {
		q := p[(i+1)%len(p)]
		twice += int64(p[i].x)*int64(q.y) - int64(q.x)*int64(p[i].y)
	}
	return twice / 2
}

// coverage renders a glyph of a font at one pixel per four font units.
func coverage(t *testing.T, f *xsfnt.Font, gid int, bounds image.Rectangle) []byte {
	var buf xsfnt.Buffer
	upem := int(f.UnitsPerEm())
	segs, err := f.LoadGlyph(&buf, xsfnt.GlyphIndex(gid), fixed.I(upem), &xsfnt.LoadGlyphOptions{})
	if err != nil {
		t.Fatalf("glyph %d: %v", gid, err)
	}
	w, h := bounds.Dx()/4+4, bounds.Dy()/4+4
	r := vector.NewRasterizer(w, h)
	tr := func(p fixed.Point26_6) (float32, float32) {
		return (float32(p.X)/64-float32(bounds.Min.X))/4 + 2, (float32(p.Y)/64-float32(bounds.Min.Y))/4 + 2
	}
	for _, s := range segs {
		switch s.Op {
		case xsfnt.SegmentOpMoveTo:
			r.MoveTo(tr(s.Args[0]))
		case xsfnt.SegmentOpLineTo:
			r.LineTo(tr(s.Args[0]))
		case xsfnt.SegmentOpQuadTo:
			x1, y1 := tr(s.Args[0])
			x2, y2 := tr(s.Args[1])
			r.QuadTo(x1, y1, x2, y2)
		case xsfnt.SegmentOpCubeTo:
			x1, y1 := tr(s.Args[0])
			x2, y2 := tr(s.Args[1])
			x3, y3 := tr(s.Args[2])
			r.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst.Pix
}

func TestCffToTtfKeepsGlyphsMetricsAndOutlines(t *testing.T) {
	for _, s := range []string{cidSample, nameKeyed} {
		src := mustLoad(t, sample(t, s))
		out := mustConvert(t, s, sample(t, s), "ttf")
		ttf := mustLoad(t, out)
		if ttf.flavor != trueTypeFlavor || ttf.has("CFF ") || ttf.has("VORG") || ttf.glyphCount() != src.glyphCount() || len(ttf.tables["maxp"]) != 32 {
			t.Fatal(s)
		}
		for _, tag := range []string{"cmap", "name", "OS/2", "GSUB", "GPOS", "BASE"} {
			if src.has(tag) && !bytes.Equal(src.tables[tag], ttf.tables[tag]) {
				t.Fatalf("%s: %s changed", s, tag)
			}
		}
		glyphs := readAllGlyphs(ttf)
		adv, lsbs := metrics(ttf, "hhea", "hmtx")
		srcAdv, _ := metrics(src, "hhea", "hmtx")
		if !slices.Equal(adv, srcAdv) {
			t.Fatal("advances")
		}
		for i, g := range glyphs {
			want := 0
			if g != nil {
				want = int(g.xMin)
			}
			if lsbs[i] != want {
				t.Fatalf("%s glyph %d lsb", s, i)
			}
			if g == nil {
				continue
			}
			if x0, y0, x1, y1 := g.computeBounds(); x0 != g.xMin || y0 != g.yMin || x1 != g.xMax || y1 != g.yMax {
				t.Fatalf("%s glyph %d bounds", s, i)
			}
			var largest int64
			for _, c := range contours(g) {
				if a := signedArea(c); abs64(a) > abs64(largest) {
					largest = a
				}
			}
			if largest >= 0 {
				t.Fatalf("%s glyph %d is counter-clockwise", s, i)
			}
		}

		// The drawn outlines against the CFF originals (x/image reads both).
		cffFace, err := xsfnt.Parse(sample(t, s))
		if err != nil {
			t.Skipf("x/image can't read %s: %v", s, err)
		}
		ttfFace, err := xsfnt.Parse(out)
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for gid, g := range glyphs {
			if g == nil {
				continue
			}
			bounds := image.Rect(int(g.xMin)-8, -int(g.yMax)-8, int(g.xMax)+8, -int(g.yMin)+8)
			var buf xsfnt.Buffer
			if _, err := cffFace.LoadGlyph(&buf, xsfnt.GlyphIndex(gid), fixed.I(1000), nil); err != nil {
				continue // x/image's CFF reader has no seac accents
			}
			checked++
			e := coverage(t, cffFace, gid, bounds)
			a := coverage(t, ttfFace, gid, bounds)
			different, covered := 0, 0
			for i := range e {
				if d := int(e[i]) - int(a[i]); d > 128 || d < -128 {
					different++
				}
				if e[i] > 128 {
					covered++
				}
			}
			if covered == 0 {
				t.Fatalf("%s glyph %d: nothing drawn", s, gid)
			}
			if different > max(8, covered/100) {
				t.Fatalf("%s glyph %d: %d of %d pixels differ", s, gid, different, covered)
			}
		}
		if checked < 3 {
			t.Fatalf("%s: only %d glyphs compared", s, checked)
		}
		t.Logf("%s: %d outlines match the CFF originals", s, checked)
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func postNames(t *testing.T, f *sfnt) []string {
	r := newReader(f.tables["post"])
	if r.u32() != 0x00020000 {
		t.Fatal("post format")
	}
	r.pos = 32
	n := int(r.u16())
	idx := make([]int, n)
	for i := range idx {
		idx[i] = int(r.u16())
	}
	var extra []string
	for r.remaining() > 0 {
		extra = append(extra, string(r.span(int(r.u8()))))
	}
	var out []string
	for _, i := range idx {
		if i < 258 {
			out = append(out, macGlyphNames[i])
		} else {
			out = append(out, extra[i-258])
		}
	}
	return out
}

func TestCffGlyphNamesBecomePostFormat2(t *testing.T) {
	ttf := mustLoad(t, mustConvert(t, nameKeyed, sample(t, nameKeyed), "ttf"))
	if got := postNames(t, ttf); !slices.Equal(got, []string{".notdef", "space", "A", "a", "e", "acute", "Aacute", "flexbar", "box"}) {
		t.Fatal(got)
	}
	cid := mustLoad(t, mustConvert(t, cidSample, sample(t, cidSample), "ttf"))
	if be32(cid.tables["post"], 0) != 0x00030000 || !bytes.Equal(mustLoad(t, sample(t, cidSample)).tables["post"][4:32], cid.tables["post"][4:32]) {
		t.Fatal("cid post")
	}
}

func TestSeacAccentFlexAndSubroutinesAreDrawn(t *testing.T) {
	g := readAllGlyphs(mustLoad(t, mustConvert(t, nameKeyed, sample(t, nameKeyed), "ttf")))
	a, acute, aacute, flexbar, box := g[2], g[5], g[6], g[7], g[8]
	if aacute.numberOfContours != a.numberOfContours+acute.numberOfContours {
		t.Fatal("contours")
	}
	want := slices.Clone(a.points)
	for _, p := range acute.points {
		want = append(want, glyphPoint{p.x + 170, p.y + 190, p.onCurve})
	}
	if !slices.Equal(want, aacute.points) {
		t.Fatal("seac points")
	}
	if [4]int16{flexbar.xMin, flexbar.yMin, flexbar.xMax, flexbar.yMax} != [4]int16{100, 0, 600, 530} || !slices.Contains(flexbar.points, glyphPoint{350, 530, true}) {
		t.Fatal("flex")
	}
	if box.numberOfContours != 2 || [4]int16{box.xMin, box.yMin, box.xMax, box.yMax} != [4]int16{50, 0, 550, 500} {
		t.Fatal("box")
	}
	c := contours(box)
	if signedArea(c[0]) != -500*500 || signedArea(c[1]) != 300*300 {
		t.Fatal(signedArea(c[0]), signedArea(c[1]))
	}
}

func TestCffToTtfMovesVorgOriginsIntoVmtx(t *testing.T) {
	src := mustLoad(t, sample(t, cidSample))
	ttf := mustLoad(t, mustConvert(t, cidSample, sample(t, cidSample), "ttf"))
	defaultOrigin := int(int16(be16(src.tables["VORG"], 4)))
	heights, tsbs := metrics(ttf, "vhea", "vmtx")
	srcHeights, _ := metrics(src, "vhea", "vmtx")
	if !slices.Equal(heights, srcHeights) {
		t.Fatal("heights")
	}
	for i, g := range readAllGlyphs(ttf) {
		if g != nil && tsbs[i] != defaultOrigin-int(g.yMax) {
			t.Fatalf("glyph %d", i)
		}
	}
}

func TestCffFontToEotCarriesTrueTypeOutlinesAndNames(t *testing.T) {
	eot := mustConvert(t, nameKeyed, sample(t, nameKeyed), "eot")
	le := binary.LittleEndian
	if int(le.Uint32(eot)) != len(eot) || le.Uint32(eot[8:]) != 0x00020001 || le.Uint32(eot[12:]) != 0 || le.Uint16(eot[34:]) != 0x504C {
		t.Fatal("header")
	}
	n := int(le.Uint16(eot[82:]))
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = le.Uint16(eot[84+2*i:])
	}
	if s := string(utf16.Decode(u)); s != "Filee Test NameKeyed" {
		t.Fatal(s)
	}
	f := mustLoad(t, eot)
	if !f.has("glyf") || f.has("CFF ") {
		t.Fatal("outlines")
	}
}

func TestErrorsAreClear(t *testing.T) {
	eot := mustConvert(t, trueTypeSample, sample(t, trueTypeSample), "eot")
	eot[12] |= 0x04 // TTEMBED_TTCOMPRESSED
	if _, err := convertFile(t, "compressed.eot", eot, "ttf"); err == nil || !strings.Contains(err.Error(), "MicroType Express") {
		t.Fatal(err)
	}
	f := mustLoad(t, sample(t, cidSample))
	f.tables["CFF2"] = f.tables["CFF "]
	delete(f.tables, "CFF ")
	if _, err := convertFile(t, "variable.otf", f.write(), "ttf"); err == nil || !strings.Contains(err.Error(), "CFF2") {
		t.Fatal(err)
	}
	if _, err := convertFile(t, "fake.woff2", []byte("this is not a font"), "ttf"); err == nil || !strings.Contains(err.Error(), "not a font") {
		t.Fatal(err)
	}
	// Damaged data never panics out of the engine.
	ttf := sample(t, trueTypeSample)
	for _, cut := range []int{13, 100, 1000, len(ttf) / 2} {
		convertFile(t, "cut.ttf", ttf[:cut], "woff2")
		w2 := mustConvert(t, trueTypeSample, ttf, "woff2")
		convertFile(t, "cut.woff2", w2[:min(cut, len(w2)-1)], "ttf")
	}
}

func TestPublicLoadAndSave(t *testing.T) {
	f, err := Load(sample(t, nameKeyed))
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Save(context.Background(), "ttf")
	if err != nil || !isSfnt(out) {
		t.Fatal(err)
	}
}
