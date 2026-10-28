package pdf

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// body is a paragraph of n lines of 11 pt text at x from the top y.
func body(x, y float64, lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(text("F1", 11, x, y-float64(i)*13.5, l))
	}
	return b.String()
}

func paragraphTexts(doc *Document) []string {
	var out []string
	for _, s := range doc.Sections {
		for _, b := range s.Blocks {
			if p, ok := b.(*Paragraph); ok {
				if t := strings.TrimSpace(plainInlines(p.Inlines)); t != "" {
					out = append(out, t)
				}
			}
		}
	}
	return out
}

func TestReaderParagraphsHeadingsFooters(t *testing.T) {
	var pages []testPage
	for i := 1; i <= 3; i++ {
		c := text("F1", 9, 72, 810, "ACME Annual Report") +
			text("F2", 20, 72, 740, fmt.Sprintf("Chapter %d", i)) +
			body(72, 700, "The quick brown fox jumps over the lazy dog and keeps on running", "through the field until the evening comes and the sun sets.") +
			body(72, 650, "A second paragraph starts here after some space between them.") +
			text("F1", 9, 290, 30, fmt.Sprintf("%d", i))
		pages = append(pages, testPage{W: 595, H: 842, Content: c,
			Extra: "/Annots [" + linkAnnot(72, 645, 200, 660, "https://example.com/x") + "]"})
	}
	src := buildPDF(t, pages...)
	doc, err := ReadDocument(context.Background(), src, t.TempDir(), ReadFull)
	if err != nil {
		t.Fatal(err)
	}
	texts := paragraphTexts(doc)
	joined := strings.Join(texts, "|")
	if strings.Contains(joined, "ACME") || strings.Contains(joined, "|2|") {
		t.Fatalf("running head or page number kept: %q", joined)
	}
	if len(texts) != 9 || texts[1] != "The quick brown fox jumps over the lazy dog and keeps on running through the field until the evening comes and the sun sets." {
		t.Fatalf("%d paragraphs: %q", len(texts), texts)
	}
	var heading *Paragraph
	var link *Link
	for _, b := range doc.Sections[0].Blocks {
		p := b.(*Paragraph)
		if p.HeadingLevel > 0 && heading == nil {
			heading = p
		}
		for _, in := range p.Inlines {
			if l, ok := in.(*Link); ok && link == nil {
				link = l
			}
		}
	}
	if heading == nil || heading.HeadingLevel != 1 || !heading.Inlines[0].(*Text).Format.Bold {
		t.Fatalf("heading %+v", heading)
	}
	if link == nil || link.Target != "https://example.com/x" {
		t.Fatalf("link %+v", link)
	}
	if doc.Title != "Test title" {
		t.Fatalf("title %q", doc.Title)
	}
	f := heading.Inlines[0].(*Text).Format
	if f.Font != "Arial" || f.Size != 2000 {
		t.Fatalf("format %+v", f)
	}
}

func TestReaderTwoColumns(t *testing.T) {
	var c strings.Builder
	c.WriteString(text("F2", 18, 72, 780, "A title that runs across both of the columns"))
	for i := 0; i < 8; i++ {
		c.WriteString(text("F1", 10, 72, 740-float64(i)*12, fmt.Sprintf("left column line %d text", i+1)))
		c.WriteString(text("F1", 10, 320, 740-float64(i)*12, fmt.Sprintf("right column line %d text", i+1)))
	}
	src := buildPDF(t, testPage{W: 595, H: 842, Content: c.String()})
	texts, err := PageTexts(context.Background(), src, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	got := texts[0]
	l8 := strings.Index(got, "left column line 8")
	r1 := strings.Index(got, "right column line 1")
	if strings.Index(got, "A title") != 0 || l8 < 0 || r1 < l8 {
		t.Fatalf("reading order:\n%s", got)
	}
}

func TestReaderRuledTable(t *testing.T) {
	// A 3 × 2 grid with text in each cell, ruled with lines.
	var c strings.Builder
	xs := []float64{72, 200, 330}
	ys := []float64{700, 680, 660, 640}
	for _, x := range xs {
		fmt.Fprintf(&c, "0.5 w %g %g m %g %g l S\n", x, ys[0], x, ys[len(ys)-1])
	}
	for _, y := range ys {
		fmt.Fprintf(&c, "0.5 w %g %g m %g %g l S\n", xs[0], y, xs[len(xs)-1], y)
	}
	cells := [][]string{{"Item", "Amount"}, {"Apples", "1,234.50"}, {"Pears", "2026-10-23"}}
	for r, row := range cells {
		for col, s := range row {
			c.WriteString(text("F1", 10, xs[col]+4, ys[r]-14, s))
		}
	}
	c.WriteString(body(72, 600, "Text below the table."))
	src := buildPDF(t, testPage{W: 595, H: 842, Content: c.String()})
	doc, err := ReadDocument(context.Background(), src, t.TempDir(), ReadFull)
	if err != nil {
		t.Fatal(err)
	}
	var table *Table
	for _, b := range doc.Sections[0].Blocks {
		if tb, ok := b.(*Table); ok {
			table = tb
		}
	}
	if table == nil || len(table.Rows) != 3 || table.ColumnCount != 2 {
		t.Fatalf("table %+v", table)
	}
	cell := table.Rows[1].Cells[1]
	if got := plainInlines(cell.Blocks[0].(*Paragraph).Inlines); got != "1,234.50" {
		t.Fatalf("cell %q", got)
	}
	if cell.Borders.Top.Style != BorderSolid {
		t.Fatalf("borders %+v", cell.Borders)
	}
	if texts := paragraphTexts(doc); len(texts) != 1 || texts[0] != "Text below the table." {
		t.Fatalf("%q", texts)
	}
}

func TestReaderScanBecomesPicture(t *testing.T) {
	src := buildPDF(t, testPage{W: 300, H: 300, Content: "0 0 1 rg 50 50 200 200 re f"})
	media := t.TempDir()
	doc, err := ReadDocument(context.Background(), src, media, ReadFull)
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Sections[0].Blocks[0].(*Paragraph)
	img, ok := p.Inlines[0].(*Image)
	if !ok || !img.Behind || img.Width != 30000 {
		t.Fatalf("%+v", p.Inlines)
	}
}
