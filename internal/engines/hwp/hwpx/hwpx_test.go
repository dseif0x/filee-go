package hwpx

import (
	"archive/zip"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// samplePNG writes a w×h PNG.
func samplePNG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{255, 0, 0, 255})
	}
	p := filepath.Join(dir, "pic.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func para(inlines ...doc.Inline) *doc.Paragraph { return &doc.Paragraph{Inlines: inlines} }

func txt(s string) *doc.Text { return &doc.Text{Text: s} }

// zipEntry reads one entry of a zip file.
func zipEntry(t *testing.T, path, name string) string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == name {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			return string(b)
		}
	}
	t.Fatalf("%s has no %s", path, name)
	return ""
}

func sampleDocument(t *testing.T, dir string) *doc.Document {
	numbering := &doc.Numbering{Levels: []doc.NumberingLevel{{Format: "DIGIT", Text: "^1.", Start: 1}, {Format: "LATIN_SMALL", Text: "^2)", Start: 1}}}
	bullets := &doc.Numbering{Levels: []doc.NumberingLevel{{Format: "DIGIT", Text: "●", Start: 1, Bullet: true}}}
	heading := para(txt("제목 Heading"))
	heading.HeadingLevel = 1
	bold := &doc.Text{Text: "bold", Format: doc.CharFormat{Bold: ptr(true), Size: ptr(1400), Color: ptr("#FF0000")}}
	link := &doc.Link{Target: "https://example.com/a?b=c", Content: []doc.Inline{txt("link")}}
	note := &doc.Note{Blocks: []doc.Block{para(txt("a footnote"))}}
	item1 := para(txt("first"))
	item1.List = &doc.ListRef{Numbering: numbering, Level: 0, Numbered: true}
	item2 := para(txt("second"))
	item2.List = &doc.ListRef{Numbering: numbering, Level: 1, Numbered: true}
	bullet := para(txt("dot"))
	bullet.List = &doc.ListRef{Numbering: bullets, Numbered: true}
	centered := para(txt("centre"))
	centered.Format.Align = ptr(doc.AlignCenter)
	centered.Format.Left = ptr(2000)
	centered.Format.FirstLine = ptr(-1000)
	centered.Format.Tabs = []doc.TabStop{{Position: 20000, Kind: doc.TabRight, Leader: "DOT"}}
	centered.Inlines = append(centered.Inlines, &doc.Tab{}, txt("9"))

	table := &doc.Table{ColumnCount: 2}
	h := &doc.Row{Header: true}
	c1 := doc.NewCell()
	c1.Blocks = []doc.Block{para(txt("A"))}
	c2 := doc.NewCell()
	c2.Blocks = []doc.Block{para(txt("B"))}
	c2.Fill = "#DDDDDD"
	h.Cells = []*doc.Cell{c1, c2}
	r := &doc.Row{}
	c3 := doc.NewCell()
	c3.ColSpan = 2
	c3.Blocks = []doc.Block{para(txt("merged"))}
	r.Cells = []*doc.Cell{c3}
	table.Rows = []*doc.Row{h, r}

	picture := &doc.Image{Path: samplePNG(t, dir, 96, 48)}
	box := doc.NewTextBox()
	box.Width, box.Height = 10000, 4000
	box.Anchor = &doc.Anchor{HorizontalRelativeTo: "PAGE", HorizontalAlign: "LEFT", HorizontalOffset: -500, VerticalRelativeTo: "PARA", VerticalAlign: "TOP", Wrap: "SQUARE"}
	box.Blocks = []doc.Block{para(txt("in a box"))}
	equation := NewEquation(&MathFraction{Numerator: &MathText{Text: "a"}, Denominator: &MathNumber{Digits: "2"}, Bar: true}, doc.CharFormat{})

	s := doc.NewSection()
	s.Page = &doc.Page{Width: 84190, Height: 59530, Left: 5000, Right: 5000, Top: 3000, Bottom: 3000, Header: 2000, Footer: 2000}
	s.Headers = []*doc.HeaderFooter{{Blocks: []doc.Block{para(txt("page "), &doc.Field{Kind: doc.PageNumberField})}}}
	s.Blocks = []doc.Block{heading, para(txt("plain "), bold, txt(" "), link, note), item1, item2, bullet, centered, table,
		para(picture, box), para(txt("x = "), equation)}
	s2 := doc.NewSection()
	s2.Columns = doc.Columns{Count: 2, Gap: 1000, Separator: true}
	s2.Blocks = []doc.Block{para(txt("second section"))}
	return &doc.Document{Title: "Sample <title>", Sections: []*doc.Section{s, s2}}
}

func TestWriteAndReadBack(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.hwpx")
	if err := Write(sampleDocument(t, dir), out, nil); err != nil {
		t.Fatal(err)
	}
	// mimetype first and stored
	z, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	if z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store {
		t.Errorf("mimetype is not the first stored entry: %s %d", z.File[0].Name, z.File[0].Method)
	}
	z.Close()
	for _, part := range []string{"Contents/header.xml", "Contents/section0.xml", "Contents/section1.xml", "Contents/content.hpf", "META-INF/container.rdf"} {
		if _, err := xmltree.ParseString(zipEntry(t, out, part)); err != nil {
			t.Errorf("%s is not well-formed: %v", part, err)
		}
	}
	hpf := zipEntry(t, out, "Contents/content.hpf")
	if !strings.Contains(hpf, "Sample &lt;title&gt;") || !strings.Contains(hpf, "section1.xml") || !strings.Contains(hpf, `BinData/image1.png`) {
		t.Errorf("manifest: %s", hpf)
	}
	section := zipEntry(t, out, "Contents/section0.xml")
	for _, want := range []string{`landscape="NARROWLY"`, `width="59530"`, "<hp:equation", "{{a} over {2}}", `horzRelTo="PAPER"`, "fieldBegin", `type="HYPERLINK"`, "<hp:footNote"} {
		if !strings.Contains(section, want) {
			t.Errorf("section0 lacks %s", want)
		}
	}

	read, err := ReadHwpx(out, filepath.Join(dir, "media"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if read.Title != "Sample <title>" || len(read.Sections) != 2 {
		t.Fatalf("title %q, %d sections", read.Title, len(read.Sections))
	}
	s := read.Sections[0]
	if s.Page == nil || s.Page.Width != 84190 || s.Page.Left != 5000 {
		t.Errorf("page %+v", s.Page)
	}
	if len(s.Headers) != 1 {
		t.Errorf("headers %d", len(s.Headers))
	}
	if read.Sections[1].Columns.Count != 2 || !read.Sections[1].Columns.Separator {
		t.Errorf("columns %+v", read.Sections[1].Columns)
	}
	var texts []string
	var heading, lists, tables, images, boxes, links, notes int
	for _, b := range s.Blocks {
		switch v := b.(type) {
		case *doc.Paragraph:
			texts = append(texts, doc.PlainText(v.Inlines))
			if v.HeadingLevel == 1 {
				heading++
			}
			if v.List != nil {
				lists++
			}
		case *doc.Table:
			tables++
			if len(v.Rows) != 2 || v.Rows[1].Cells[0].ColSpan != 2 || !v.Rows[0].Header || v.Rows[0].Cells[1].Fill != "#DDDDDD" {
				t.Errorf("table %+v", v.Rows)
			}
		}
	}
	for _, in := range doc.Inlines(s.Blocks) {
		switch v := in.(type) {
		case *doc.Image:
			images++
			if !fileExists(v.Path) {
				t.Errorf("picture not extracted")
			}
		case *doc.TextBox:
			boxes++
		case *doc.Link:
			links++
			if v.Target != "https://example.com/a?b=c" {
				t.Errorf("link target %q", v.Target)
			}
		case *doc.Note:
			notes++
		case *doc.Text:
			if v.Text == "bold" && (!isTrue(v.Format.Bold) || val(v.Format.Size, 0) != 1400 || val(v.Format.Color, "") != "#FF0000") {
				t.Errorf("bold format %+v", v.Format)
			}
		}
	}
	all := strings.Join(texts, "|")
	for _, want := range []string{"제목 Heading", "plain bold link", "first", "second", "centre 9", "x = {{a} over {2}}"} {
		if !strings.Contains(all, want) {
			t.Errorf("text lacks %q: %s", want, all)
		}
	}
	if heading != 1 || lists != 3 || tables != 1 || images != 1 || boxes != 1 || links != 1 || notes != 1 {
		t.Errorf("heading %d lists %d tables %d images %d boxes %d links %d notes %d", heading, lists, tables, images, boxes, links, notes)
	}
}

func TestLengthsAndWidths(t *testing.T) {
	for _, c := range []struct {
		value string
		want  int
		ok    bool
	}{{"1in", 7200, true}, {"96px", 7200, true}, {"72pt", 7200, true}, {"25.4mm", 7200, true}, {"2.54cm", 7200, true}, {"", 0, false}, {"auto", 0, false}} {
		got, ok := doc.ParseLength(c.value)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseLength(%q) = %d %v", c.value, got, ok)
		}
	}
	for mm, want := range map[float64]string{0.12: "0.12 mm", 0.13: "0.12 mm", 0.2: "0.2 mm", 0.9: "1.0 mm", 10: "5.0 mm", 0.01: "0.1 mm"} {
		if got := LineWidth(mm); got != want {
			t.Errorf("LineWidth(%v) = %s, want %s", mm, got, want)
		}
	}
	if Escape("a<b>&\v\"c") != "a&lt;b&gt;&amp;&quot;c" {
		t.Errorf("escape: %s", Escape("a<b>&\v\"c"))
	}
	if TextWidth("한글", 1000) <= TextWidth("ab", 1000) {
		t.Error("Hangul should be measured wider than Latin text")
	}
}
