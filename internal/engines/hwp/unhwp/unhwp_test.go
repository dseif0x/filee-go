package unhwp

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/hwpx"
	"github.com/dseif0x/filee-go/internal/testutil"
)

// ───────────────────────── Compound file writer (fixtures) ─────────────────────────

const (
	endOfChain = 0xFFFFFFFE
	freeSect   = 0xFFFFFFFF
	fatSect    = 0xFFFFFFFD
	noStream   = 0xFFFFFFFF
)

type cfbEntry struct {
	name     string
	storage  bool
	data     []byte
	children []*cfbEntry
	start    uint32
	id       int
}

// writeCFB writes a version 3 compound file; stream names may hold one storage ("BodyText/Section0").
func writeCFB(t *testing.T, path string, streams map[string][]byte) {
	t.Helper()
	root := &cfbEntry{name: "Root Entry", storage: true}
	storages := map[string]*cfbEntry{}
	names := make([]string, 0, len(streams))
	for n := range streams {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		parent := root
		leaf := n
		if i := strings.IndexByte(n, '/'); i >= 0 {
			s := storages[n[:i]]
			if s == nil {
				s = &cfbEntry{name: n[:i], storage: true}
				storages[n[:i]] = s
				root.children = append(root.children, s)
			}
			parent, leaf = s, n[i+1:]
		}
		parent.children = append(parent.children, &cfbEntry{name: leaf, data: streams[n]})
	}
	var entries []*cfbEntry
	var number func(e *cfbEntry)
	number = func(e *cfbEntry) {
		e.id = len(entries)
		entries = append(entries, e)
		for _, c := range e.children {
			number(c)
		}
	}
	number(root)

	// Small streams go into the mini stream (64-byte sectors), the others into 512-byte sectors.
	var mini []byte
	var miniFAT []uint32
	var big []*cfbEntry
	for _, e := range entries {
		if e.storage {
			continue
		}
		if len(e.data) >= 4096 {
			big = append(big, e)
			continue
		}
		e.start = uint32(len(mini) / 64)
		n := (len(e.data) + 63) / 64
		for i := 0; i < n; i++ {
			next := uint32(len(miniFAT) + 1)
			if i == n-1 {
				next = endOfChain
			}
			miniFAT = append(miniFAT, next)
		}
		mini = append(mini, pad(e.data, 64)...)
	}
	miniFATBytes := make([]byte, 0, 4*len(miniFAT))
	for _, v := range miniFAT {
		miniFATBytes = binary.LittleEndian.AppendUint32(miniFATBytes, v)
	}
	for len(miniFATBytes)%512 != 0 {
		miniFATBytes = binary.LittleEndian.AppendUint32(miniFATBytes, freeSect)
	}
	sectors := func(n int) int { return (n + 511) / 512 }
	dirSectors := sectors(128 * len(entries))
	data := dirSectors + sectors(len(miniFATBytes)) + sectors(len(mini))
	for _, e := range big {
		data += sectors(len(e.data))
	}
	fatSectors := 1
	for (fatSectors+data)*4 > fatSectors*512 {
		fatSectors++
	}
	fat := make([]uint32, fatSectors*128)
	for i := range fat {
		fat[i] = freeSect
	}
	for i := 0; i < fatSectors; i++ {
		fat[i] = fatSect
	}
	nextSector := uint32(fatSectors)
	chain := func(n int) uint32 {
		if n == 0 {
			return endOfChain
		}
		start := nextSector
		for i := 0; i < n; i++ {
			fat[nextSector] = nextSector + 1
			if i == n-1 {
				fat[nextSector] = endOfChain
			}
			nextSector++
		}
		return start
	}
	dirStart := chain(dirSectors)
	miniFATStart := uint32(endOfChain)
	if len(miniFAT) > 0 {
		miniFATStart = chain(sectors(len(miniFATBytes)))
	}
	root.start = chain(sectors(len(mini)))
	for _, e := range big {
		e.start = chain(sectors(len(e.data)))
	}

	var dir []byte
	for _, e := range entries {
		b := make([]byte, 128)
		u := utf16.Encode([]rune(e.name))
		for i, c := range u {
			binary.LittleEndian.PutUint16(b[2*i:], c)
		}
		binary.LittleEndian.PutUint16(b[64:], uint16(2*len(u)+2))
		switch {
		case e == root:
			b[66] = 5
		case e.storage:
			b[66] = 1
		default:
			b[66] = 2
		}
		b[67] = 1 // black
		binary.LittleEndian.PutUint32(b[68:], noStream)
		binary.LittleEndian.PutUint32(b[72:], noStream)
		binary.LittleEndian.PutUint32(b[76:], noStream)
		dir = append(dir, b...)
	}
	// Siblings as a chain of right children.
	for _, e := range entries {
		for i, c := range e.children {
			if i == 0 {
				binary.LittleEndian.PutUint32(dir[128*e.id+76:], uint32(c.id))
			}
			if i+1 < len(e.children) {
				binary.LittleEndian.PutUint32(dir[128*c.id+72:], uint32(e.children[i+1].id))
			}
		}
	}
	for _, e := range entries {
		size := len(e.data)
		start := e.start
		if e == root {
			size = len(mini)
		} else if e.storage {
			start = 0
		}
		binary.LittleEndian.PutUint32(dir[128*e.id+116:], start)
		binary.LittleEndian.PutUint32(dir[128*e.id+120:], uint32(size))
	}

	header := make([]byte, 512)
	copy(header, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	le := binary.LittleEndian
	le.PutUint16(header[24:], 0x3E)
	le.PutUint16(header[26:], 3)
	le.PutUint16(header[28:], 0xFFFE)
	le.PutUint16(header[30:], 9)
	le.PutUint16(header[32:], 6)
	le.PutUint32(header[44:], uint32(fatSectors))
	le.PutUint32(header[48:], dirStart)
	le.PutUint32(header[56:], 4096)
	le.PutUint32(header[60:], miniFATStart)
	le.PutUint32(header[64:], uint32(len(miniFATBytes)/512))
	le.PutUint32(header[68:], endOfChain)
	for i := 0; i < 109; i++ {
		v := uint32(freeSect)
		if i < fatSectors {
			v = uint32(i)
		}
		le.PutUint32(header[76+4*i:], v)
	}

	var out bytes.Buffer
	out.Write(header)
	for _, v := range fat {
		out.Write(le.AppendUint32(nil, v))
	}
	out.Write(pad(dir, 512))
	if len(miniFAT) > 0 {
		out.Write(miniFATBytes)
	}
	out.Write(pad(mini, 512))
	for _, e := range big {
		out.Write(pad(e.data, 512))
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pad(b []byte, n int) []byte {
	if len(b)%n == 0 {
		return b
	}
	return append(append([]byte{}, b...), make([]byte, n-len(b)%n)...)
}

// ───────────────────────── HWP records ─────────────────────────

type buf struct{ bytes.Buffer }

func (b *buf) u8(v int) *buf  { b.WriteByte(byte(v)); return b }
func (b *buf) u16(v int) *buf { b.Write(binary.LittleEndian.AppendUint16(nil, uint16(v))); return b }
func (b *buf) u32(v uint32) *buf {
	b.Write(binary.LittleEndian.AppendUint32(nil, v))
	return b
}
func (b *buf) i32(v int) *buf { return b.u32(uint32(int32(v))) }
func (b *buf) zeros(n int) *buf {
	b.Write(make([]byte, n))
	return b
}
func (b *buf) str(s string) *buf {
	u := utf16.Encode([]rune(s))
	b.u16(len(u))
	for _, c := range u {
		b.u16(int(c))
	}
	return b
}

type rec struct {
	tag, level int
	data       []byte
}

func encode(rs ...rec) []byte {
	var out []byte
	for _, r := range rs {
		size := len(r.data)
		h := uint32(r.tag) | uint32(r.level)<<10
		if size >= 0xFFF {
			out = binary.LittleEndian.AppendUint32(out, h|0xFFF<<20)
			out = binary.LittleEndian.AppendUint32(out, uint32(size))
		} else {
			out = binary.LittleEndian.AppendUint32(out, h|uint32(size)<<20)
		}
		out = append(out, r.data...)
	}
	return out
}

func deflate(b []byte) []byte {
	var out bytes.Buffer
	w, _ := flate.NewWriter(&out, flate.DefaultCompression)
	w.Write(b)
	w.Close()
	return out.Bytes()
}

// text encodes paragraph text: strings, and control characters as ints (extended and inline ones take 8 WCHARs).
func text(parts ...any) []byte {
	var b buf
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			for _, c := range utf16.Encode([]rune(v)) {
				b.u16(int(c))
			}
		case int:
			b.u16(v)
			if v != 10 && v != 13 && v != 0 && !(v >= 24 && v <= 31) {
				b.zeros(14)
			}
		}
	}
	return b.Bytes()
}

func ctrl(id string) *buf {
	var b buf
	b.u32(ctrlID(id))
	return &b
}

// para is a paragraph (level lvl): PARA_HEADER, PARA_TEXT, PARA_CHAR_SHAPE (position, shape pairs), then extra records.
func para(lvl, shape, style int, body []byte, charShapes []int, extra ...rec) []rec {
	var h buf
	h.u32(uint32(len(body) / 2)).u32(0).u16(shape).u8(style).u8(0).zeros(12)
	var cs buf
	for _, v := range charShapes {
		cs.u32(uint32(v))
	}
	out := []rec{{tagParaHeader, lvl, h.Bytes()}, {tagParaText, lvl + 1, body}, {tagParaCharShape, lvl + 1, cs.Bytes()}}
	return append(out, extra...)
}

func charShapeRecord(attr uint32, size int) []byte {
	var b buf
	b.zeros(14)            // faces
	b.zeros(7 + 7 + 7 + 7) // ratios, spacings, relative sizes, offsets
	b.i32(size).u32(attr).zeros(2)
	b.u32(0).u32(0).u32(0xFFFFFF)
	return b.Bytes()
}

func paraShapeRecord(attr1 uint32, numbering int) []byte {
	var b buf
	b.u32(attr1).i32(0).i32(0).i32(0).i32(0).i32(0).i32(160).u16(0).u16(numbering).zeros(10).u32(0).u32(0).u32(160)
	return b.Bytes()
}

func testPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 0xC0
	}
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fixture struct {
	properties uint32
	oldHeader  bool
}

// sampleHWP writes an HWP 5.0 document: a heading, mixed bold text with a tab and a line break, a bulleted list, a
// link, a footnote, a picture and a 1×2 table; the streams are compressed.
func sampleHWP(t *testing.T, dir string, f fixture) string {
	t.Helper()
	path := filepath.Join(dir, "sample.hwp")
	if f.oldHeader {
		os.WriteFile(path, append([]byte("HWP Document File V3.00 "), make([]byte, 200)...), 0o644)
		return path
	}
	header := make([]byte, 256)
	copy(header, "HWP Document File")
	binary.LittleEndian.PutUint32(header[32:], 0x05000300)
	binary.LittleEndian.PutUint32(header[36:], 1|f.properties)

	var ids buf
	for _, n := range []int{1, 1, 1, 0, 0, 0, 0, 0, 1, 2, 1, 0, 1, 3, 2, 0, 0, 0} {
		ids.i32(n)
	}
	var binData, face1, face2, border, tabs, bullet, style1, style2 buf
	binData.u16(1 | 2<<4).u16(1).str("png")
	face1.u8(0).str("함초롬바탕")
	face2.u8(0).str("Arial")
	border.u16(0)
	for i := 0; i < 4; i++ {
		border.u8(1).u8(1).u32(0)
	}
	border.zeros(6).u32(0)
	tabs.u32(0).u16(0)
	bullet.zeros(12).u16(0x25CF)
	style1.str("바탕글").str("Normal")
	style2.str("개요 1").str("Heading 1")
	docInfo := encode(
		rec{tagIDMappings, 0, ids.Bytes()},
		rec{tagBinData, 1, binData.Bytes()},
		rec{tagFaceName, 1, face1.Bytes()},
		rec{tagFaceName, 1, face2.Bytes()},
		rec{tagBorderFill, 1, border.Bytes()},
		rec{tagCharShape, 1, charShapeRecord(0, 1000)},
		rec{tagCharShape, 1, charShapeRecord(2, 1000)},
		rec{tagTabDef, 1, tabs.Bytes()},
		rec{tagBullet, 1, bullet.Bytes()},
		rec{tagParaShape, 1, paraShapeRecord(0, 0)},
		rec{tagParaShape, 1, paraShapeRecord(3<<23, 1)},
		rec{tagParaShape, 1, paraShapeRecord(3<<2, 0)},
		rec{tagStyle, 1, style1.Bytes()},
		rec{tagStyle, 1, style2.Bytes()},
	)

	var section []rec
	// Heading, with the section definition (landscape A4) in front.
	secd := ctrl("secd")
	secd.u32(0).zeros(10).u16(0).u16(3)
	var page buf
	page.u32(59528).u32(84188).u32(8504).u32(8504).u32(5668).u32(4252).u32(4252).u32(4252).u32(0).u32(1)
	section = append(section, para(0, 0, 1, text(2, "제목", 13), []int{0, 0},
		rec{tagCtrlHeader, 1, secd.Bytes()}, rec{tagPageDef, 2, page.Bytes()})...)
	// Bold, plain, tab, line break.
	section = append(section, para(0, 0, 0, text("굵게", " 보통", 9, "탭", 10, "줄", 13), []int{0, 1, 2, 0})...)
	// Bullets.
	section = append(section, para(0, 1, 0, text("하나", 13), []int{0, 0})...)
	section = append(section, para(0, 1, 0, text("둘", 13), []int{0, 0})...)
	// A link and a footnote.
	link := ctrl("%hlk")
	link.u32(0).u8(0).str(`https\://example.com/a;1;0;0;`)
	note := ctrl("fn  ")
	var noteList buf
	noteList.u16(1).zeros(6)
	section = append(section, para(0, 0, 0, text("여기 ", 3, "링크", 4, " 끝", 17, 13), []int{0, 0},
		rec{tagCtrlHeader, 1, link.Bytes()},
		rec{tagCtrlHeader, 1, note.Bytes()}, rec{tagListHeader, 2, noteList.Bytes()})...)
	section = append(section, para(2, 0, 0, text("각주 내용", 13), []int{0, 0})...)
	// A picture, treated as a character.
	gso := ctrl("gso ")
	gso.u32(1).i32(0).i32(0).u32(7200).u32(7200)
	component := ctrl("$pic")
	component.u32(ctrlID("$pic")).zeros(40)
	var pic buf
	pic.zeros(71).u16(1)
	section = append(section, para(0, 2, 0, text(11, 13), []int{0, 0},
		rec{tagCtrlHeader, 1, gso.Bytes()}, rec{tagShapeComponent, 2, component.Bytes()}, rec{tagShapePicture, 3, pic.Bytes()})...)
	// A table of one row and two cells.
	tbl := ctrl("tbl ")
	tbl.u32(0)
	var table buf
	table.u32(0).u16(1).u16(2).u16(0).u16(510).u16(510).u16(141).u16(141).u16(1).u16(1)
	cell := func(col int) []byte {
		var c buf
		c.u16(1).u16(0).u32(1 << 5).u16(col).u16(0).u16(1).u16(1).u32(20000).u32(1000).u16(510).u16(510).u16(141).u16(141).u16(1)
		return c.Bytes()
	}
	section = append(section, para(0, 0, 0, text(11, 13), []int{0, 0},
		rec{tagCtrlHeader, 1, tbl.Bytes()}, rec{tagTable, 2, table.Bytes()}, rec{tagListHeader, 2, cell(0)})...)
	section = append(section, para(2, 0, 0, text("왼쪽", 13), []int{0, 0})...)
	section = append(section, rec{tagListHeader, 2, cell(1)})
	section = append(section, para(2, 0, 0, text("오른쪽", 13), []int{0, 0})...)

	writeCFB(t, path, map[string][]byte{
		"FileHeader":           header,
		"DocInfo":              deflate(docInfo),
		"BodyText/Section0":    deflate(encode(section...)),
		"BinData/BIN0001.png":  testPNG(t),
		"PrvText":              []byte("preview"),
		"BodyText/Placeholder": bytes.Repeat([]byte{1}, 5000), // a stream outside the mini stream
	})
	return path
}

// ───────────────────────── Tests ─────────────────────────

func TestReadHWP(t *testing.T) {
	dir := t.TempDir()
	d, err := ReadHWP(sampleHWP(t, dir, fixture{}), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sections) != 1 {
		t.Fatalf("sections: %d", len(d.Sections))
	}
	s := d.Sections[0]
	if s.Page == nil || s.Page.Width != 84188 || s.Page.Height != 59528 || s.Page.Left != 8504 {
		t.Errorf("landscape page: %+v", s.Page)
	}
	if s.StartPageNumber == nil || *s.StartPageNumber != 3 {
		t.Errorf("start page: %v", s.StartPageNumber)
	}
	var paragraphs []*doc.Paragraph
	var table *doc.Table
	for _, b := range s.Blocks {
		switch v := b.(type) {
		case *doc.Paragraph:
			paragraphs = append(paragraphs, v)
		case *doc.Table:
			table = v
		}
	}
	if len(paragraphs) < 6 {
		t.Fatalf("paragraphs: %d", len(paragraphs))
	}
	if paragraphs[0].HeadingLevel != 1 || doc.PlainText(paragraphs[0].Inlines) != "제목" {
		t.Errorf("heading: %d %q", paragraphs[0].HeadingLevel, doc.PlainText(paragraphs[0].Inlines))
	}
	var bold, plain bool
	var tab, lineBreak bool
	for _, in := range paragraphs[1].Inlines {
		switch v := in.(type) {
		case *doc.Text:
			if v.Text == "굵게" && v.Format.Bold != nil && *v.Format.Bold {
				bold = true
			}
			if strings.Contains(v.Text, "보통") && v.Format.Bold != nil && !*v.Format.Bold {
				plain = true
			}
		case *doc.Tab:
			tab = true
		case *doc.LineBreak:
			lineBreak = true
		}
	}
	if !bold || !plain || !tab || !lineBreak {
		t.Errorf("runs: bold %v plain %v tab %v break %v: %#v", bold, plain, tab, lineBreak, paragraphs[1].Inlines)
	}
	for _, p := range paragraphs[2:4] {
		if p.List == nil || p.List.Numbering == nil || !p.List.Numbering.Levels[0].Bullet || p.List.Numbering.Levels[0].Text != "●" {
			t.Errorf("bullet: %#v", p.List)
		}
	}
	if paragraphs[2].List.Numbering != paragraphs[3].List.Numbering {
		t.Error("the bullets of one list share their numbering")
	}
	var link *doc.Link
	var note *doc.Note
	for _, in := range paragraphs[4].Inlines {
		switch v := in.(type) {
		case *doc.Link:
			link = v
		case *doc.Note:
			note = v
		}
	}
	if link == nil || link.Target != "https://example.com/a" || doc.PlainText(link.Content) != "링크" {
		t.Errorf("link: %#v", link)
	}
	if note == nil || len(note.Blocks) != 1 || !strings.Contains(doc.PlainText(doc.Inlines(note.Blocks)), "각주 내용") {
		t.Errorf("footnote: %#v", note)
	}
	if !strings.HasSuffix(doc.PlainText(paragraphs[4].Inlines), " 끝") {
		t.Errorf("text after the link: %q", doc.PlainText(paragraphs[4].Inlines))
	}
	var img *doc.Image
	for _, in := range paragraphs[5].Inlines {
		if v, ok := in.(*doc.Image); ok {
			img = v
		}
	}
	if img == nil || img.Width == nil || *img.Width != 7200 || img.Anchor != nil {
		t.Fatalf("picture: %#v", img)
	}
	if b, err := os.ReadFile(img.Path); err != nil || !bytes.Equal(b, testPNG(t)) {
		t.Errorf("picture data: %v", err)
	}
	if paragraphs[5].Format.Align == nil || *paragraphs[5].Format.Align != doc.AlignCenter {
		t.Errorf("centred: %v", paragraphs[5].Format.Align)
	}
	if table == nil || table.ColumnCount != 2 || len(table.Rows) != 1 || len(table.Rows[0].Cells) != 2 {
		t.Fatalf("table: %#v", table)
	}
	right := table.Rows[0].Cells[1]
	if right.Column != 1 || right.VerticalAlign != doc.VAlignCenter || doc.PlainText(doc.Inlines(right.Blocks)) != "오른쪽" {
		t.Errorf("cell: %#v", right)
	}
	if table.Borders == nil || table.Borders.Top.Style != doc.BorderSolid {
		t.Errorf("table borders: %#v", table.Borders)
	}
}

func TestRefusals(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		f    fixture
		want error
	}{{fixture{properties: 2}, ErrPassword}, {fixture{properties: 4}, ErrDistribution}, {fixture{oldHeader: true}, ErrOldVersion}} {
		_, err := ReadHWP(sampleHWP(t, t.TempDir(), c.f), dir)
		if !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.f, err, c.want)
		}
	}
	junk := filepath.Join(dir, "junk.hwp")
	os.WriteFile(junk, []byte("not a compound file at all"), 0o644)
	if _, err := ReadHWP(junk, dir); err == nil {
		t.Error("junk was read")
	}
}

func convertTo(t *testing.T, input, from, to string, leaveOutPictures bool) (string, string) {
	t.Helper()
	step := testutil.Step(t, input, from, to)
	step.Preset.Document.LeaveOutPictures = leaveOutPictures
	out, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("outputs: %v", out)
	}
	b, err := os.ReadFile(out[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b), out[0]
}

func TestConvert(t *testing.T) {
	input := sampleHWP(t, t.TempDir(), fixture{})

	txt, _ := convertTo(t, input, "hwp", "txt", false)
	if !strings.HasPrefix(txt, "\xEF\xBB\xBF") {
		t.Error("the text file starts with a byte order mark")
	}
	for _, want := range []string{"제목", "굵게 보통", "하나", "둘", "링크", "왼쪽", "오른쪽"} {
		if !strings.Contains(txt, want) {
			t.Errorf("txt lacks %q:\n%s", want, txt)
		}
	}

	md, mdPath := convertTo(t, input, "hwp", "md", false)
	if strings.HasPrefix(md, "---") || strings.Contains(md, "\n\n\n") {
		t.Errorf("markdown front matter or blank lines:\n%s", md)
	}
	for _, want := range []string{"# 제목", "[링크](<https://example.com/a>)", "**굵게** 보통", "- 하나", "[^1]: 각주 내용", "sample_files/", "왼쪽"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	pictures, _ := filepath.Glob(filepath.Join(filepath.Dir(mdPath), "sample_files", "*.png"))
	if len(pictures) != 1 {
		t.Errorf("pictures next to the Markdown: %v", pictures)
	}
	md, _ = convertTo(t, input, "hwp", "md", true)
	if strings.Contains(md, "![") || strings.Contains(md, "_files") {
		t.Errorf("pictures left in:\n%s", md)
	}

	html, _ := convertTo(t, input, "hwp", "html", false)
	for _, want := range []string{"<!DOCTYPE html>", "<title>sample</title>", "<h1", "data:image/png;base64,", `href="https://example.com/a"`, "<table>"} {
		if !strings.Contains(html, want) {
			t.Errorf("html lacks %q:\n%s", want, html)
		}
	}
}

func TestConvertHwpx(t *testing.T) {
	dir := t.TempDir()
	d := &doc.Document{Sections: []*doc.Section{{Blocks: []doc.Block{
		&doc.Paragraph{HeadingLevel: 1, Inlines: []doc.Inline{&doc.Text{Text: "HWPX 제목"}}},
		&doc.Paragraph{Inlines: []doc.Inline{&doc.Text{Text: "본문"}}},
	}}}}
	input := filepath.Join(dir, "doc.hwpx")
	if err := hwpx.Write(d, input, nil); err != nil {
		t.Fatal(err)
	}
	md, _ := convertTo(t, input, "hwpx", "md", false)
	if !strings.Contains(md, "HWPX 제목") || !strings.Contains(md, "본문") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestEdges(t *testing.T) {
	edges := New().Edges()
	if len(edges) != 6 {
		t.Fatalf("edges: %v", edges)
	}
	for _, e := range edges {
		want := 10
		if e.To == "html" {
			want = 14
		}
		if e.Cost != want {
			t.Errorf("%s → %s costs %d", e.From, e.To, e.Cost)
		}
	}
	if New().Status().Available == false {
		t.Error("built in")
	}
}
