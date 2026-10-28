// The PDF reader of the document model (C# DocumentReaders["pdf"] =
// PdfDocumentReader.Read): the DOCX and HWPX writers read PDFs through it,
// at the extra cost doc.PdfPenalty. A PDF's OCR layer is its text when the
// preset recognises text (doc.PdfOcrLayer, PdfDocumentReader.ReadWithOcrLayer).

package pdf

import (
	"context"

	"github.com/dseif0x/filee-go/internal/doc"
)

func init() {
	doc.RegisterReader("pdf", func(path, media string) (*doc.Document, error) {
		d, err := ReadDocument(context.Background(), path, media, ReadFull)
		if err != nil {
			return nil, err
		}
		return ToDoc(d), nil
	})
	doc.PdfOcrLayer = func(path, media string) (*doc.Document, error) {
		d, err := ReadDocument(context.Background(), path, media, ReadOcrLayer)
		if err != nil {
			return nil, err
		}
		return ToDoc(d), nil
	}
}

// ToDoc turns a read PDF into the shared document model. The PDF model is
// the part of HDocument the PDF reader fills, so this is one to one; zero
// values the reader leaves unset become nil (inherit) fields.
func ToDoc(d *Document) *doc.Document {
	out := &doc.Document{Title: d.Title}
	for _, s := range d.Sections {
		section := doc.NewSection()
		if s.Page != (PageSetup{}) {
			section.Page = &doc.Page{Width: s.Page.Width, Height: s.Page.Height,
				Left: s.Page.Left, Right: s.Page.Right, Top: s.Page.Top, Bottom: s.Page.Bottom}
		}
		section.Blocks = docBlocks(s.Blocks)
		out.Sections = append(out.Sections, section)
	}
	return out
}

func docBlocks(blocks []Block) []doc.Block {
	out := make([]doc.Block, 0, len(blocks))
	for _, b := range blocks {
		switch b := b.(type) {
		case *Paragraph:
			out = append(out, docParagraph(b))
		case *Table:
			out = append(out, docTable(b))
		}
	}
	return out
}

func docParagraph(p *Paragraph) *doc.Paragraph {
	out := &doc.Paragraph{HeadingLevel: p.HeadingLevel, PageBreakBefore: p.PageBreakBefore,
		MarkFormat: docMarkFormat(p.MarkFormat), Inlines: docInlines(p.Inlines)}
	f := p.Format
	// Empty paragraphs (a page without text) carry no paragraph format.
	if f.LineSpacing.Value != 0 {
		kind := doc.SpacingPercent
		if f.LineSpacing.Kind == SpacingAtLeast {
			kind = doc.SpacingAtLeast
		}
		out.Format = doc.ParaFormat{
			Align:       doc.Ptr(doc.Align(f.Align)),
			Left:        f.Left,
			FirstLine:   f.FirstLine,
			Before:      doc.Ptr(f.Before),
			After:       doc.Ptr(f.After),
			LineSpacing: &doc.LineSpacing{Kind: kind, Value: f.LineSpacing.Value},
		}
		for _, t := range f.Tabs {
			out.Format.Tabs = append(out.Format.Tabs, doc.TabStop{Position: t.Position, Kind: doc.TabLeft, Leader: "NONE"})
		}
		if f.RightToLeft {
			out.Format.RightToLeft = doc.Ptr(true)
		}
	}
	return out
}

func docCharFormat(f CharFormat) doc.CharFormat {
	if f == (CharFormat{}) {
		return doc.CharFormat{}
	}
	out := doc.CharFormat{Bold: doc.Ptr(f.Bold), Italic: doc.Ptr(f.Italic)}
	if f.Size > 0 {
		out.Size = doc.Ptr(f.Size)
	}
	if f.Font != "" {
		out.Font = doc.Ptr(f.Font)
	}
	if f.Color != "" {
		out.Color = doc.Ptr(f.Color)
	}
	return out
}

// docMarkFormat: the reader gives paragraph marks only a size.
func docMarkFormat(f CharFormat) doc.CharFormat {
	if f.Size > 0 {
		return doc.CharFormat{Size: doc.Ptr(f.Size)}
	}
	return doc.CharFormat{}
}

func docInlines(inlines []Inline) []doc.Inline {
	out := make([]doc.Inline, 0, len(inlines))
	for _, in := range inlines {
		switch in := in.(type) {
		case *Text:
			out = append(out, &doc.Text{Text: in.Text, Format: docCharFormat(in.Format)})
		case *Tab:
			out = append(out, &doc.Tab{Format: docCharFormat(in.Format)})
		case *LineBreak:
			out = append(out, &doc.LineBreak{Format: docCharFormat(in.Format)})
		case *Link:
			out = append(out, &doc.Link{Target: in.Target, Content: docInlines(in.Content)})
		case *Image:
			img := &doc.Image{Path: in.Path}
			if in.Width > 0 && in.Height > 0 {
				img.Width, img.Height = doc.Ptr(in.Width), doc.Ptr(in.Height)
			}
			if in.Behind {
				// Floating behind the (empty) paragraph at the paper's corner: the page image never pushes text.
				img.Anchor = &doc.Anchor{HorizontalRelativeTo: "PAPER", HorizontalAlign: "LEFT",
					VerticalRelativeTo: "PAPER", VerticalAlign: "TOP", Wrap: "BEHIND_TEXT", AllowOverlap: true}
			}
			out = append(out, img)
		}
	}
	return out
}

func docBorder(b Border) doc.Border {
	return doc.Border{Style: doc.BorderStyle(b.Style), WidthMm: b.WidthMm, Color: b.Color}
}

func docTable(t *Table) *doc.Table {
	out := &doc.Table{
		ColumnCount:  t.ColumnCount,
		ColumnWidths: t.ColumnWidths,
		Align:        doc.Align(t.Align),
		Indent:       t.Indent,
		CellMargin:   &doc.Insets{Left: t.CellMargin.Left, Right: t.CellMargin.Right, Top: t.CellMargin.Top, Bottom: t.CellMargin.Bottom},
		Borders:      doc.Ptr(doc.EmptyBorders),
	}
	for _, r := range t.Rows {
		row := &doc.Row{}
		if r.Height > 0 {
			row.Height = doc.Ptr(r.Height)
		}
		for _, c := range r.Cells {
			b := c.Borders
			row.Cells = append(row.Cells, &doc.Cell{
				Column: c.Column, RowSpan: max(1, c.RowSpan), ColSpan: max(1, c.ColSpan),
				VerticalAlign: doc.VerticalAlign(c.VerticalAlign),
				Fill:          c.Fill,
				Borders:       &doc.Borders{Left: docBorder(b.Left), Right: docBorder(b.Right), Top: docBorder(b.Top), Bottom: docBorder(b.Bottom)},
				Blocks:        docBlocks(c.Blocks),
			})
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
