package docxwriter

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/images"
	_ "github.com/dseif0x/filee-go/internal/engines/markdown"
	_ "github.com/dseif0x/filee-go/internal/engines/spreadsheet"
	"github.com/dseif0x/filee-go/internal/testutil"
	"github.com/dseif0x/filee-go/internal/xdom"
)

func TestMarkdownToDocx(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 96, 48))
	img.Set(1, 1, color.Black)
	f, _ := os.Create(filepath.Join(dir, "p.png"))
	png.Encode(f, img)
	f.Close()
	in := filepath.Join(dir, "a.md")
	os.WriteFile(in, []byte("# Title\n\nHello **bold** [link](https://example.com) and [inner](#title) é<>&.\n\n- one\n- two\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n![p](p.png)\n\nNote[^n].\n\n[^n]: Footnote text.\n\nمرحبا بالعالم\n"), 0o644)
	out, err := New().Convert(context.Background(), testutil.Step(t, in, "md", "docx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	z := testutil.ReadZip(t, out[0])
	for name, content := range z {
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			if _, err := xdom.ParseBytes([]byte(content)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
	d := z["word/document.xml"]
	for _, want := range []string{`<w:pStyle w:val="Heading1"/>`, ">bold</w:t>", `w:anchor="title"`, "<w:numPr>", "<w:tbl>", "<w:drawing>", `<w:footnoteReference w:id="1"/>`, "é&lt;&gt;&amp;.", "<w:rtl/>"} {
		if !strings.Contains(d, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	if !strings.Contains(z["word/_rels/document.xml.rels"], "https://example.com") || z["word/media/image1.png"] == "" {
		t.Error("link or picture missing")
	}
	if !strings.Contains(z["word/footnotes.xml"], "Footnote text.") || !strings.Contains(z["word/numbering.xml"], `w:numFmt w:val="bullet"`) {
		t.Errorf("notes %q numbering %q", z["word/footnotes.xml"], z["word/numbering.xml"])
	}
	// 96 px at 96 dpi = 1 inch = 914400 EMU.
	if !strings.Contains(d, `cx="914400" cy="457200"`) {
		t.Error("picture size")
	}
}

func TestEdges(t *testing.T) {
	edges := New().Edges()
	has := func(f string) bool {
		for _, e := range edges {
			if e.From == f && e.To == "docx" && e.Cost == 8 {
				return true
			}
		}
		return false
	}
	for _, f := range []string{"txt", "md", "xlsx", "csv", "ods", "xls"} {
		if !has(f) {
			t.Errorf("no %s → docx", f)
		}
	}
	for _, e := range edges {
		if e.From == "docx" || e.From == "docm" {
			t.Error("word source")
		}
	}
	p := convert.NewPlanner([]convert.Converter{New()}, nil)
	if p.Plan("csv", "docx", convert.Options{}) == nil {
		t.Fatal("csv → docx")
	}
}

func TestSectionsAndHeaders(t *testing.T) {
	s := doc.NewSection()
	s.Page = &doc.Page{Width: 84188, Height: 59528, Left: 2000, Right: 2000, Top: 1000, Bottom: 1000, Header: 1000, Footer: 1000}
	s.Headers = append(s.Headers, &doc.HeaderFooter{Blocks: []doc.Block{&doc.Paragraph{Inlines: []doc.Inline{&doc.Text{Text: "head"}, &doc.Field{Kind: doc.PageNumberField}}}}})
	s.HideFirstHeader = true
	s.Blocks = []doc.Block{&doc.Table{Rows: []*doc.Row{{Cells: []*doc.Cell{{Column: -1, RowSpan: 2, ColSpan: 1}, doc.NewCell()}}, {Cells: []*doc.Cell{doc.NewCell()}}}}}
	out := filepath.Join(t.TempDir(), "s.docx")
	if err := Write(&doc.Document{Sections: []*doc.Section{s, doc.NewSection()}}, out); err != nil {
		t.Fatal(err)
	}
	z := testutil.ReadZip(t, out)
	d := z["word/document.xml"]
	for _, want := range []string{`w:orient="landscape"`, `<w:titlePg/>`, `<w:headerReference w:type="first"`, `<w:vMerge w:val="restart"/>`, `<w:vMerge/>`} {
		if !strings.Contains(d, want) {
			t.Errorf("lacks %s", want)
		}
	}
	if !strings.Contains(z["word/header1.xml"], "head") || !strings.Contains(z["word/header1.xml"], " PAGE ") {
		t.Error("header")
	}
}

// Pictures Word can't show (QOI, PSD, …) are read by the image engine and stored as PNG.
func TestPicturesThroughTheImageEngine(t *testing.T) {
	dir := t.TempDir()
	var inlines []doc.Inline
	for _, f := range []string{"qoi", "psd"} {
		path := filepath.Join(dir, "p."+f)
		if err := images.Encode(path, image.NewNRGBA(image.Rect(0, 0, 40, 20)), f, nil); err != nil {
			t.Fatal(err)
		}
		inlines = append(inlines, &doc.Image{Path: path})
	}
	s := doc.NewSection()
	s.Blocks = []doc.Block{&doc.Paragraph{Inlines: inlines}}
	out := filepath.Join(dir, "p.docx")
	if err := Write(&doc.Document{Sections: []*doc.Section{s}}, out); err != nil {
		t.Fatal(err)
	}
	z := testutil.ReadZip(t, out)
	for _, name := range []string{"word/media/image1.png", "word/media/image2.png"} {
		if cfg, err := png.DecodeConfig(strings.NewReader(z[name])); err != nil || cfg.Width != 40 {
			t.Errorf("%s: %v", name, err)
		}
	}
	// 40 px at 96 dpi = 381000 EMU.
	if !strings.Contains(z["word/document.xml"], `cx="381000"`) {
		t.Error("picture size")
	}
}
