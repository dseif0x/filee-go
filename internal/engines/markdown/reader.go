package markdown

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Markdown → document model, so Markdown → DOCX / HWPX needs no external
// engine: headings (their ids as bookmarks), paragraphs, bold / italic /
// strike, code, lists (with the start number, nested levels), task lists,
// block quotes, tables, definition lists, links, images, footnotes. The YAML
// front matter's "title" becomes the document title. Raw HTML is skipped,
// except <br>.

// quoteIndent per block quote level.
const quoteIndent = 2000

var codeShade = "#F1F0F5"

func init() {
	doc.RegisterReader("md", func(path, _ string) (*doc.Document, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		abs, _ := filepath.Abs(path)
		return Read(doc.DecodeText(b), filepath.Dir(abs), ""), nil
	})
}

type reader struct {
	src        []byte
	base, root string
	footnotes  map[int]*east.Footnote
}

type mdCtx struct{ listLevel, indent int }

// Read parses Markdown text; relative image paths resolve against
// baseFolder, and with resourceRoot set images must be inside it.
func Read(md, baseFolder, resourceRoot string) *doc.Document {
	root, src, title := Parse(md)
	r := &reader{src: src, base: baseFolder, root: resourceRoot, footnotes: map[int]*east.Footnote{}}
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if f, ok := n.(*east.Footnote); ok && entering {
			r.footnotes[f.Index] = f
		}
		return ast.WalkContinue, nil
	})
	s := doc.NewSection()
	s.Blocks = r.blocks(root, nil, mdCtx{})
	return &doc.Document{Title: title, Sections: []*doc.Section{s}}
}

func (r *reader) blocks(n ast.Node, out []doc.Block, ctx mdCtx) []doc.Block {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = r.block(c, out, ctx)
	}
	return out
}

func newParagraph(ctx mdCtx) *doc.Paragraph {
	p := &doc.Paragraph{}
	if ctx.indent > 0 {
		p.Format.Left = doc.Ptr(ctx.indent)
	}
	return p
}

func (r *reader) paragraph(n ast.Node, ctx mdCtx) *doc.Paragraph {
	p := newParagraph(ctx)
	p.Inlines = doc.MergeTexts(r.inlines(n, nil, doc.CharFormat{}))
	return p
}

func (r *reader) block(n ast.Node, out []doc.Block, ctx mdCtx) []doc.Block {
	switch n := n.(type) {
	case *ast.HTMLBlock, *east.FootnoteList:
		// raw HTML carries no text; footnotes are read at their links
	case *ast.Heading:
		p := r.paragraph(n, ctx)
		p.HeadingLevel = max(1, min(9, n.Level))
		// Heading ids are link targets ("#id" links, e.g. a table of contents).
		if id, ok := n.AttributeString("id"); ok {
			if b, ok := id.([]byte); ok && len(b) > 0 {
				p.Inlines = append([]doc.Inline{&doc.Bookmark{Name: string(b)}}, p.Inlines...)
			}
		}
		out = append(out, p)
	case *ast.Paragraph, *ast.TextBlock:
		out = append(out, r.paragraph(n, ctx))
	case *ast.List:
		out = r.list(n, out, ctx)
	case *ast.Blockquote:
		out = r.blocks(n, out, mdCtx{ctx.listLevel, ctx.indent + quoteIndent})
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		code := strings.TrimSuffix(strings.ReplaceAll(lines(n, r.src), "\r\n", "\n"), "\n")
		for _, line := range strings.Split(code, "\n") {
			p := newParagraph(ctx)
			if line != "" {
				p.Inlines = append(p.Inlines, &doc.Text{Text: line, Format: doc.CharFormat{Shade: doc.Ptr(codeShade)}})
			}
			out = append(out, p)
		}
	case *ast.ThematicBreak:
		out = append(out, newParagraph(ctx))
	case *east.Table:
		out = append(out, r.table(n, ctx))
	case *east.DefinitionList:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c.(type) {
			case *east.DefinitionTerm:
				p := newParagraph(ctx)
				p.Inlines = r.inlines(c, nil, doc.CharFormat{Bold: doc.Ptr(true)})
				out = append(out, p)
			default:
				out = r.blocks(c, out, mdCtx{ctx.listLevel, ctx.indent + quoteIndent})
			}
		}
	default:
		if n.HasChildren() {
			if n.FirstChild().Type() == ast.TypeInline {
				out = append(out, r.paragraph(n, ctx))
			} else {
				out = r.blocks(n, out, ctx)
			}
		}
	}
	return out
}

func numbering(l *ast.List) *doc.Numbering {
	bullets := []string{"●", "○", "■"}
	defaults := []string{"DIGIT", "LATIN_CAPITAL", "ROMAN_SMALL"}
	nb := &doc.Numbering{}
	start := max(1, l.Start)
	for level := 1; level <= 7; level++ {
		if !l.IsOrdered() {
			nb.Levels = append(nb.Levels, doc.NumberingLevel{Format: "DIGIT", Text: bullets[(level-1)%3], Start: 1, Bullet: true})
			continue
		}
		// The first level follows the Markdown list; deeper levels use 1. → A. → i.
		format := "DIGIT"
		if level > 1 {
			format = defaults[(level-1)%3]
		}
		text := "^" + strconv.Itoa(level) + "."
		if l.Marker == ')' {
			text = "^" + strconv.Itoa(level) + ")"
		}
		s := 1
		if level == 1 {
			s = start
		}
		nb.Levels = append(nb.Levels, doc.NumberingLevel{Format: format, Text: text, Start: s})
	}
	return nb
}

func (r *reader) list(l *ast.List, out []doc.Block, ctx mdCtx) []doc.Block {
	nb := numbering(l)
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		// Only the first paragraph of an item carries the bullet or number.
		first := true
		for b := item.FirstChild(); b != nil; b = b.NextSibling() {
			switch b := b.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				p := r.paragraph(b, ctx)
				p.List = &doc.ListRef{Numbering: nb, Level: ctx.listLevel, Numbered: first}
				out = append(out, p)
				first = false
			case *ast.List:
				out = r.list(b, out, mdCtx{ctx.listLevel + 1, ctx.indent})
			default:
				out = r.block(b, out, ctx)
				first = false
			}
		}
	}
	return out
}

func (r *reader) table(t *east.Table, ctx mdCtx) *doc.Table {
	table := &doc.Table{Indent: ctx.indent, ColumnCount: max(1, len(t.Alignments))}
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		dr := &doc.Row{Header: header}
		i := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			c := doc.NewCell()
			f := doc.CharFormat{}
			if header {
				// Header cells are bold, as Markdown renderers show them.
				f.Bold = doc.Ptr(true)
			}
			p := &doc.Paragraph{Inlines: r.inlines(cell, nil, f)}
			if i < len(t.Alignments) {
				switch t.Alignments[i] {
				case east.AlignCenter:
					p.Format.Align = doc.Ptr(doc.AlignCenter)
				case east.AlignRight:
					p.Format.Align = doc.Ptr(doc.AlignRight)
				}
			}
			c.Blocks = append(c.Blocks, p)
			dr.Cells = append(dr.Cells, c)
			i++
		}
		table.ColumnCount = max(table.ColumnCount, i)
		table.Rows = append(table.Rows, dr)
	}
	return table
}

func (r *reader) inlines(n ast.Node, out []doc.Inline, f doc.CharFormat) []doc.Inline {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			t := string(c.Value(r.src))
			if !c.IsRaw() {
				t = unescape(c.Value(r.src))
			}
			if t != "" {
				out = append(out, &doc.Text{Text: t, Format: f})
			}
			if c.HardLineBreak() {
				out = append(out, &doc.LineBreak{Format: f})
			} else if c.SoftLineBreak() {
				out = append(out, &doc.Text{Text: " ", Format: f})
			}
		case *ast.String:
			out = append(out, &doc.Text{Text: string(c.Value), Format: f})
		case *ast.CodeSpan:
			cf := f
			cf.Shade = doc.Ptr(codeShade)
			out = append(out, &doc.Text{Text: textOf(c, r.src), Format: cf})
		case *ast.Emphasis:
			ef := f
			if c.Level >= 2 {
				ef.Bold = doc.Ptr(true)
			} else {
				ef.Italic = doc.Ptr(true)
			}
			out = r.inlines(c, out, ef)
		case *east.Strikethrough:
			sf := f
			sf.Strike = doc.Ptr(true)
			out = r.inlines(c, out, sf)
		case *ast.Image:
			attrs := imageAttributes(c, r.src)
			if p := ResolveImage(string(c.Destination), r.base, r.root); p != "" {
				img := &doc.Image{Path: p}
				// Sizes from attributes: ![](a.png){width=50% height=3cm}
				if w, ok := doc.ParseLength(attrs["width"]); ok {
					img.Width = &w
				}
				if h, ok := doc.ParseLength(attrs["height"]); ok {
					img.Height = &h
				}
				out = append(out, img)
			}
		case *ast.Link:
			dest := string(c.Destination)
			if strings.TrimSpace(dest) == "" {
				out = r.inlines(c, out, f)
				continue
			}
			l := &doc.Link{Target: dest}
			l.Content = r.inlines(c, nil, f)
			out = append(out, l)
		case *ast.AutoLink:
			u := string(c.URL(r.src))
			if c.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(u), "mailto:") {
				u = "mailto:" + u
			}
			label := string(c.Label(r.src))
			out = append(out, &doc.Link{Target: u, Content: []doc.Inline{&doc.Text{Text: label, Format: f}}})
		case *ast.RawHTML:
			var raw strings.Builder
			for i := 0; i < c.Segments.Len(); i++ {
				s := c.Segments.At(i)
				raw.Write(s.Value(r.src))
			}
			if strings.HasPrefix(strings.ToLower(raw.String()), "<br") {
				out = append(out, &doc.LineBreak{Format: f})
			}
		case *east.TaskCheckBox:
			box := "☐"
			if c.IsChecked {
				box = "☑"
			}
			out = append(out, &doc.Text{Text: box + " ", Format: f})
		case *east.FootnoteLink:
			note := &doc.Note{}
			if fn := r.footnotes[c.Index]; fn != nil {
				note.Blocks = r.blocks(fn, nil, mdCtx{})
			}
			out = append(out, note)
		case *east.FootnoteBacklink:
		default:
			out = r.inlines(c, out, f)
		}
	}
	return out
}

// imageAttributes takes a "{width=50% height=3cm}" right after an image
// (Markdig's generic attributes) out of the following text.
func imageAttributes(img *ast.Image, src []byte) map[string]string {
	next, ok := img.NextSibling().(*ast.Text)
	if !ok {
		return nil
	}
	v := string(next.Value(src))
	if !strings.HasPrefix(v, "{") {
		return nil
	}
	end := strings.IndexByte(v, '}')
	if end < 0 {
		return nil
	}
	attrs := map[string]string{}
	for _, f := range strings.Fields(v[1:end]) {
		if k, val, ok := strings.Cut(f, "="); ok {
			attrs[strings.ToLower(k)] = strings.Trim(val, `"'`)
		}
	}
	next.Segment = next.Segment.WithStart(next.Segment.Start + end + 1)
	return attrs
}
