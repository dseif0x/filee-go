package spreadsheet

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/testutil"
)

func sampleBook() *Workbook {
	s := NewWorksheet("Data: 2026")
	bold := DefaultStyle
	bold.Bold, bold.Fill, bold.Align = true, "#FFFF00", AlignCenter
	bold.Bottom = CellBorder{"medium", "#FF0000"}
	s.Set(0, 0, TextCell("Name", bold))
	s.Set(0, 1, TextCell("Amount", bold))
	s.Set(0, 2, TextCell("Date", bold))
	s.Set(1, 0, TextCell("  two  spaces\tand tab", DefaultStyle))
	s.Set(1, 1, &Cell{Text: "1,234.50", Style: DefaultStyle, IsNumber: true, Value: fptr(1234.5), NumberFormat: "#,##0.00"})
	s.Set(1, 2, &Cell{Text: "2026-10-01", Style: DefaultStyle, IsNumber: true, Value: fptr(46296), NumberFormat: "yyyy-mm-dd"})
	s.Set(2, 0, &Cell{Text: "TRUE", Style: DefaultStyle, Value: fptr(1), IsBoolean: true})
	s.Set(2, 1, &Cell{Text: "25%", Style: DefaultStyle, IsNumber: true, Value: fptr(0.25), NumberFormat: "0%"})
	s.Set(3, 0, TextCell("merged\nlines", DefaultStyle))
	s.Merges = append(s.Merges, CellRange{3, 0, 3, 2})
	s.ColumnWidths[0] = 20
	s.RowHeights[3] = 30
	s.HiddenColumns[5] = true
	s.FrozenRows = 1
	return &Workbook{Sheets: []*Worksheet{s, NewWorksheet("Empty")}}
}

func checkBook(t *testing.T, b *Workbook, kind string) {
	t.Helper()
	if len(b.Sheets) != 2 {
		t.Fatalf("%s: %d sheets", kind, len(b.Sheets))
	}
	s := b.Sheets[0]
	if s.Name != "Data 2026" {
		t.Errorf("%s: name %q", kind, s.Name)
	}
	h := s.Get(0, 0)
	if h == nil || h.Text != "Name" || !h.Style.Bold || h.Style.Fill != "#FFFF00" || h.Style.Align != AlignCenter || h.Style.Bottom.Style != "medium" || h.Style.Bottom.Color != "#FF0000" {
		t.Errorf("%s: header %+v", kind, h)
	}
	if c := s.Get(1, 0); c == nil || c.Text != "  two  spaces\tand tab" {
		t.Errorf("%s: spaces %+v", kind, c)
	}
	if c := s.Get(1, 1); c == nil || c.Value == nil || *c.Value != 1234.5 || c.NumberFormat != "#,##0.00" || c.Text != "1,234.50" {
		t.Errorf("%s: number %+v", kind, c)
	}
	if c := s.Get(1, 2); c == nil || c.Value == nil || *c.Value != 46296 || c.Text != "2026-10-01" {
		t.Errorf("%s: date %+v", kind, c)
	}
	if c := s.Get(2, 0); c == nil || !c.IsBoolean || *c.Value != 1 {
		t.Errorf("%s: bool %+v", kind, c)
	}
	if c := s.Get(2, 1); c == nil || c.Text != "25%" || *c.Value != 0.25 {
		t.Errorf("%s: percent %+v", kind, c)
	}
	if c := s.Get(3, 0); c == nil || c.Text != "merged\nlines" {
		t.Errorf("%s: lines %+v", kind, c)
	}
	if len(s.Merges) != 1 || s.Merges[0] != (CellRange{3, 0, 3, 2}) {
		t.Errorf("%s: merges %v", kind, s.Merges)
	}
	if w := s.ColumnWidth(0); w < 19.9 || w > 20.1 {
		t.Errorf("%s: width %v", kind, w)
	}
	if s.RowHeights[3] != 30 || s.FrozenRows != 1 {
		t.Errorf("%s: heights %v frozen %d", kind, s.RowHeights, s.FrozenRows)
	}
}

func TestXlsxRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.xlsx")
	if err := WriteXLSX(sampleBook(), p); err != nil {
		t.Fatal(err)
	}
	b, err := ReadXLSX(p)
	if err != nil {
		t.Fatal(err)
	}
	checkBook(t, b, "xlsx")
}

func TestOdsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.ods")
	if err := WriteODS(sampleBook(), p); err != nil {
		t.Fatal(err)
	}
	b, err := ReadODS(p)
	if err != nil {
		t.Fatal(err)
	}
	checkBook(t, b, "ods")
	if z := testutil.ReadZip(t, p); !strings.Contains(z["content.xml"], `number:grouping="true"`) {
		t.Error("no data style")
	}
}

func TestNumberStylesBothWays(t *testing.T) {
	for _, code := range []string{"#,##0.00", "0%", "0.00E+00", "yyyy-mm-dd", "[h]:mm:ss", `#,##0.00;[Red]-#,##0.00`, "hh:mm AM/PM", "dddd d mmmm yyyy"} {
		x, _, ok := ExcelToOdf(code, "N1")
		if !ok {
			t.Errorf("%s: no data style", code)
			continue
		}
		root, err := parseFragment(x)
		if err != nil {
			t.Fatal(err)
		}
		styles := map[string]*nodeT{}
		for _, el := range root.Elements() {
			styles[el.AN(styleNs, "name")] = el
		}
		back := OdfToExcel(styles["N1"], func(n string) *nodeT { return styles[n] })
		if back != code {
			t.Errorf("%s → %s", code, back)
		}
	}
}

func TestCsv(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.csv")
	os.WriteFile(in, []byte("a;b;c\r\n1;\"x;y\";007\n\"multi\nline\";-2.5;\n"), 0o644)
	b, err := ReadCSV(in)
	if err != nil {
		t.Fatal(err)
	}
	s := b.Sheets[0]
	if s.Name != "in" || s.Get(1, 1).Text != "x;y" || s.Get(1, 2).IsNumber || !s.Get(1, 0).IsNumber || s.Get(2, 0).Text != "multi\nline" || *s.Get(2, 1).Value != -2.5 {
		t.Fatalf("parsed %+v", s.Rows)
	}
	step := testutil.Step(t, in, "csv", "tsv")
	out, err := New().Convert(context.Background(), step, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out[0])
	want := "\xEF\xBB\xBFa\tb\tc\r\n1\tx;y\t007\r\n\"multi\nline\"\t-2.5\t\r\n"
	if string(got) != want {
		t.Fatalf("tsv %q", got)
	}
}

func TestCsvPerSheet(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "book.xlsx")
	book := sampleBook()
	other := NewWorksheet("Two")
	other.Set(0, 0, TextCell("x", DefaultStyle))
	book.Sheets[1] = other
	if err := WriteXLSX(book, in); err != nil {
		t.Fatal(err)
	}
	out, err := New().Convert(context.Background(), testutil.Step(t, in, "xlsx", "csv"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !strings.HasSuffix(out[0], "book_Data 2026.csv") || !strings.HasSuffix(out[1], "book_Two.csv") {
		t.Fatalf("outputs %v", out)
	}
}

func TestEdges(t *testing.T) {
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	if p.Plan("xls", "ods", convert.Options{}) == nil {
		t.Fatal("no xls → ods")
	}
	for _, e := range New().Edges() {
		if e.To == "xlsx" && (e.From == "xlsm" || e.From == "xltx") {
			t.Fatal("xlsm → xlsx is the ooxml engine's")
		}
	}
	if n := len(New().Edges()); n != 8*4-4-2 {
		t.Fatalf("%d edges", n)
	}
}

func TestSheetDocument(t *testing.T) {
	d := Document(sampleBook())
	if len(d.Sections) != 1 {
		t.Fatalf("%d sections", len(d.Sections))
	}
	var table *doc.Table
	for _, b := range d.Sections[0].Blocks {
		if tb, ok := b.(*doc.Table); ok {
			table = tb
		}
	}
	if table == nil || len(table.Rows) != 4 || !table.Rows[0].Header {
		t.Fatalf("table %+v", table)
	}
	if c := table.Rows[3].Cells[0]; c.ColSpan != 3 {
		t.Fatalf("merge span %d", c.ColSpan)
	}
}

// ---- XLS fixture: a BIFF8 workbook in a compound file ----

func rec(id uint16, data ...[]byte) []byte {
	var body []byte
	for _, d := range data {
		body = append(body, d...)
	}
	h := make([]byte, 4)
	binary.LittleEndian.PutUint16(h, id)
	binary.LittleEndian.PutUint16(h[2:], uint16(len(body)))
	return append(h, body...)
}

func u16(v ...int) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(x))
	}
	return b
}

func u32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

func f64(v float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, mathFloat64bits(v))
	return b
}

func xlstr(s string, lenSize int) []byte {
	u := utf16.Encode([]rune(s))
	var b []byte
	if lenSize == 1 {
		b = []byte{byte(len(u))}
	} else {
		b = u16(len(u))
	}
	b = append(b, 1)
	for _, c := range u {
		b = append(b, u16(int(c))...)
	}
	return b
}

func buildXls(t *testing.T) []byte {
	xf := func(format int, align byte) []byte {
		return rec(0x00E0, u16(0, format, 0), []byte{align, 0}, make([]byte, 12))
	}
	sheetName := xlstr("Blatt", 1)
	globals := func(offset uint32) []byte {
		var b []byte
		b = append(b, rec(0x0809, u16(0x0600, 0x0005, 0, 0), u32(0), u32(0))...)
		b = append(b, rec(0x0042, u16(1200))...)
		b = append(b, rec(0x041E, u16(164), xlstr("0.0", 2))...)
		for i := 0; i < 16; i++ {
			b = append(b, xf(0, 0)...)
		}
		b = append(b, xf(164, 3)...) // 16: 0.0, right aligned
		b = append(b, xf(14, 0)...)  // 17: date
		b = append(b, rec(0x0085, u32(offset), []byte{0, 0}, sheetName)...)
		b = append(b, rec(0x00FC, u32(2), u32(2), xlstr("Hallo", 2), xlstr("Welt ö", 2))...)
		b = append(b, rec(0x000A)...)
		return b
	}
	g := globals(0)
	g = globals(uint32(len(g)))
	var s []byte
	s = append(s, rec(0x0809, u16(0x0600, 0x0010, 0, 0), u32(0), u32(0))...)
	s = append(s, rec(0x007D, u16(0, 0, 20*256, 15, 0, 0))...)
	s = append(s, rec(0x0208, u16(1, 0, 3, 600, 0, 0, 0x40, 0x0F))...)
	s = append(s, rec(0x00FD, u16(0, 0, 15), u32(0))...)
	s = append(s, rec(0x00FD, u16(0, 1, 15), u32(1))...)
	s = append(s, rec(0x0203, u16(1, 0, 16), f64(3.14159))...)
	s = append(s, rec(0x027E, u16(1, 1, 17), u32(uint32(46296)<<2|2))...)
	s = append(s, rec(0x0205, u16(1, 2, 15), []byte{1, 0})...)
	s = append(s, rec(0x00E5, u16(1, 2, 3, 0, 1))...)
	s = append(s, rec(0x000A)...)
	stream := append(g, s...)
	for len(stream) < 4096 {
		stream = append(stream, rec(0x0000)...) // padding records, never read: they follow EOF
	}
	return cfb(stream)
}

// cfb wraps one "Workbook" stream (≥ 4096 bytes) in a compound file.
func cfb(stream []byte) []byte {
	const ss = 512
	n := (len(stream) + ss - 1) / ss
	// Sectors: 0 = FAT, 1 = directory, 2.. = stream.
	header := make([]byte, ss)
	copy(header, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	le := binary.LittleEndian
	le.PutUint16(header[24:], 0x3E)
	le.PutUint16(header[26:], 3)
	le.PutUint16(header[28:], 0xFFFE)
	le.PutUint16(header[30:], 9)
	le.PutUint16(header[32:], 6)
	le.PutUint32(header[44:], 1)
	le.PutUint32(header[48:], 1)
	le.PutUint32(header[56:], 4096)
	le.PutUint32(header[60:], 0xFFFFFFFE)
	le.PutUint32(header[68:], 0xFFFFFFFE)
	for i := 0; i < 109; i++ {
		le.PutUint32(header[76+4*i:], 0xFFFFFFFF)
	}
	le.PutUint32(header[76:], 0)
	fat := make([]byte, ss)
	for i := 0; i < 128; i++ {
		le.PutUint32(fat[4*i:], 0xFFFFFFFF)
	}
	le.PutUint32(fat[0:], 0xFFFFFFFD)
	le.PutUint32(fat[4:], 0xFFFFFFFE)
	for i := 0; i < n; i++ {
		next := uint32(2 + i + 1)
		if i == n-1 {
			next = 0xFFFFFFFE
		}
		le.PutUint32(fat[4*(2+i):], next)
	}
	dir := make([]byte, ss)
	entry := func(i int, name string, typ byte, start uint32, size uint32, child uint32) {
		e := dir[128*i:]
		u := utf16.Encode([]rune(name))
		for k, c := range u {
			le.PutUint16(e[2*k:], c)
		}
		le.PutUint16(e[64:], uint16(2*len(u)+2))
		e[66] = typ
		le.PutUint32(e[68:], 0xFFFFFFFF)
		le.PutUint32(e[72:], 0xFFFFFFFF)
		le.PutUint32(e[76:], child)
		le.PutUint32(e[116:], start)
		le.PutUint32(e[120:], size)
	}
	entry(0, "Root Entry", 5, 0xFFFFFFFE, 0, 1)
	entry(1, "Workbook", 2, 2, uint32(len(stream)), 0xFFFFFFFF)
	out := append(append(append(header, fat...), dir...), stream...)
	for len(out)%ss != 0 {
		out = append(out, 0)
	}
	return out
}

func TestXls(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.xls")
	os.WriteFile(p, buildXls(t), 0o644)
	b, err := ReadXLS(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sheets) != 1 || b.Sheets[0].Name != "Blatt" {
		t.Fatalf("sheets %+v", b.Sheets)
	}
	s := b.Sheets[0]
	if s.Get(0, 0).Text != "Hallo" || s.Get(0, 1).Text != "Welt ö" {
		t.Errorf("strings %q %q", s.Get(0, 0).Text, s.Get(0, 1).Text)
	}
	if c := s.Get(1, 0); c.Text != "3.1" || c.NumberFormat != "0.0" || c.Style.Align != AlignRight {
		t.Errorf("number %+v", c)
	}
	if c := s.Get(1, 1); c.Text != "2026-10-01" || *c.Value != 46296 {
		t.Errorf("date %+v", c)
	}
	if c := s.Get(1, 2); !c.IsBoolean || c.Text != "TRUE" {
		t.Errorf("bool %+v", c)
	}
	if len(s.Merges) != 1 || s.Merges[0] != (CellRange{2, 0, 3, 1}) {
		t.Errorf("merges %v", s.Merges)
	}
	if s.ColumnWidths[0] != 20 || s.RowHeights[1] != 30 {
		t.Errorf("layout %v %v", s.ColumnWidths, s.RowHeights)
	}
	if !bytes.HasPrefix(buildXls(t), []byte{0xD0, 0xCF}) {
		t.Fatal("fixture")
	}
}
