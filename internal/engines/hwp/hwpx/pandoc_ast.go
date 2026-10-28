package hwpx

// Pandoc JSON AST → document model. Used for the formats Pandoc parses (ODT, RTF, reStructuredText, LaTeX, ...).
//
// The mapping follows pypandoc-hwpx (MIT, https://github.com/msjang/pypandoc-hwpx), extended so nothing is dropped
// silently: BlockQuote, Div, Figure, LineBlock, DefinitionList, HorizontalRule, Span, Quoted, Cite, Strikeout,
// SmallCaps and Math are converted too, and list numbering honours the Pandoc list style (1. / A. / i) / (a) ...).
// Pandoc's model has no page setup or direct formatting, so those come from the HWPX template.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

const quoteIndent = 2000

type pandocReader struct{ resolveImage func(string) string }

type pandocContext struct{ listLevel, indent int }

// ReadPandocAST converts the output of `pandoc -t json`. resolveImage maps an image target from the AST to a local
// file, or "" when it is unavailable.
func ReadPandocAST(data []byte, resolveImage func(string) string) (*doc.Document, error) {
	var ast map[string]any
	if err := json.Unmarshal(data, &ast); err != nil {
		return nil, fmt.Errorf("pandoc produced no readable document: %w", err)
	}
	r := &pandocReader{resolveImage}
	d := &doc.Document{Title: pandocTitle(ast["meta"])}
	s := doc.NewSection()
	s.Blocks = r.blocks(arr(ast["blocks"]), nil, pandocContext{})
	d.Sections = []*doc.Section{s}
	return d, nil
}

func arr(v any) []any {
	a, _ := v.([]any)
	return a
}

func at(v any, i int) any {
	a := arr(v)
	if i < len(a) {
		return a[i]
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any, def int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return def
}

func ptype(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["t"])
	}
	return ""
}

func pcontent(v any) any {
	if m, ok := v.(map[string]any); ok {
		return m["c"]
	}
	return nil
}

// ───────────────────────── Blocks ─────────────────────────

func (r *pandocReader) blocks(blocks []any, output []doc.Block, ctx pandocContext) []doc.Block {
	for _, b := range blocks {
		if _, ok := b.(map[string]any); ok {
			output = r.block(b, output, ctx)
		}
	}
	return output
}

func newIndented(ctx pandocContext) *doc.Paragraph {
	p := &doc.Paragraph{}
	if ctx.indent > 0 {
		p.Format.Left = ptr(ctx.indent)
	}
	return p
}

func (r *pandocReader) paragraph(inlines []any, ctx pandocContext) *doc.Paragraph {
	p := newIndented(ctx)
	p.Inlines = r.inlines(inlines, nil, doc.CharFormat{})
	return p
}

func (r *pandocReader) block(block any, output []doc.Block, ctx pandocContext) []doc.Block {
	content := pcontent(block)
	switch ptype(block) {
	case "Para", "Plain":
		return append(output, r.paragraph(arr(content), ctx))
	case "Header":
		return append(output, r.heading(content, ctx))
	case "BulletList":
		return r.list(arr(content), pandocNumbering(false, 1, "", ""), output, ctx)
	case "OrderedList":
		attrs := at(content, 0)
		n := pandocNumbering(true, num(at(attrs, 0), 1), ptype(at(attrs, 1)), ptype(at(attrs, 2)))
		return r.list(arr(at(content, 1)), n, output, ctx)
	case "CodeBlock":
		for _, line := range strings.Split(str(at(content, 1)), "\n") {
			p := newIndented(ctx)
			p.Inlines = []doc.Inline{&doc.Text{Text: line}}
			output = append(output, p)
		}
	case "LineBlock":
		for _, line := range arr(content) {
			output = append(output, r.paragraph(arr(line), ctx))
		}
	case "BlockQuote":
		return r.blocks(arr(content), output, pandocContext{ctx.listLevel, ctx.indent + quoteIndent})
	case "Div":
		return r.blocks(arr(at(content, 1)), output, ctx)
	case "Figure":
		// [attr, caption [short, blocks], blocks]
		output = r.blocks(arr(at(content, 2)), output, ctx)
		return r.blocks(arr(at(at(content, 1), 1)), output, ctx)
	case "DefinitionList":
		for _, item := range arr(content) {
			term := newIndented(ctx)
			term.Inlines = r.inlines(arr(at(item, 0)), nil, doc.CharFormat{Bold: ptr(true)})
			output = append(output, term)
			for _, definition := range arr(at(item, 1)) {
				output = r.blocks(arr(definition), output, pandocContext{ctx.listLevel, ctx.indent + quoteIndent})
			}
		}
	case "HorizontalRule":
		return append(output, newIndented(ctx))
	case "Table":
		return append(output, r.table(content, ctx))
	}
	// RawBlock and unknown blocks carry no document text
	return output
}

func (r *pandocReader) heading(content any, ctx pandocContext) *doc.Paragraph {
	inlines := arr(at(content, 2))
	p := newIndented(ctx)
	p.HeadingLevel = clamp(num(at(content, 0), 1), 1, 9)
	// A line break at the very start of a heading means "column break" (pypandoc-hwpx convention).
	p.ColumnBreakBefore = len(inlines) > 0 && ptype(inlines[0]) == "LineBreak"
	if p.ColumnBreakBefore {
		inlines = inlines[1:]
	}
	p.Inlines = r.inlines(inlines, nil, doc.CharFormat{})
	// Heading ids are link targets ("#id" links, e.g. a generated table of contents).
	if id := str(at(at(content, 1), 0)); id != "" {
		p.Inlines = append([]doc.Inline{&doc.Bookmark{Name: id}}, p.Inlines...)
	}
	return p
}

func pandocNumbering(ordered bool, start int, style, delimiter string) *doc.Numbering {
	bullets := []string{"●", "○", "■"}
	defaults := []string{"DIGIT", "LATIN_CAPITAL", "ROMAN_SMALL"}
	n := &doc.Numbering{}
	for level := 1; level <= 7; level++ {
		if !ordered {
			n.Levels = append(n.Levels, doc.NumberingLevel{Format: "DIGIT", Text: bullets[(level-1)%3], Start: 1, Bullet: true})
			continue
		}
		format := defaults[(level-1)%3]
		switch style {
		case "Decimal":
			format = "DIGIT"
		case "LowerAlpha":
			format = "LATIN_SMALL"
		case "UpperAlpha":
			format = "LATIN_CAPITAL"
		case "LowerRoman":
			format = "ROMAN_SMALL"
		case "UpperRoman":
			format = "ROMAN_CAPITAL"
		}
		text := fmt.Sprintf("^%d.", level)
		switch delimiter {
		case "OneParen":
			text = fmt.Sprintf("^%d)", level)
		case "TwoParens":
			text = fmt.Sprintf("(^%d)", level)
		}
		s := 1
		if level == 1 {
			s = max(1, start)
		}
		n.Levels = append(n.Levels, doc.NumberingLevel{Format: format, Text: text, Start: s})
	}
	return n
}

func (r *pandocReader) list(items []any, n *doc.Numbering, output []doc.Block, ctx pandocContext) []doc.Block {
	for _, item := range items {
		// Only the first paragraph of an item carries the bullet or number.
		first := true
		for _, b := range arr(item) {
			if b == nil {
				continue
			}
			switch ptype(b) {
			case "Para", "Plain":
				p := r.paragraph(arr(pcontent(b)), ctx)
				p.List = &doc.ListRef{Numbering: n, Level: ctx.listLevel, Numbered: first}
				output = append(output, p)
				first = false
			case "BulletList", "OrderedList":
				output = r.block(b, output, pandocContext{ctx.listLevel + 1, ctx.indent})
			default:
				output = r.block(b, output, ctx)
				first = false
			}
		}
	}
	return output
}

func (r *pandocReader) table(content any, ctx pandocContext) *doc.Table {
	// Pandoc ≥ 2.10: [attr, caption, colSpecs, head, bodies, foot]; row = [attr, cells];
	// cell = [attr, align, rowspan, colspan, blocks]
	specs := arr(at(content, 2))
	t := &doc.Table{ColumnCount: len(specs), Indent: ctx.indent}
	for _, s := range specs {
		w := 0.0
		if ptype(at(s, 1)) == "ColWidth" {
			if f, ok := pcontent(at(s, 1)).(float64); ok {
				w = f
			}
		}
		t.RelativeWidths = append(t.RelativeWidths, w)
	}
	addRows := func(rows []any, header bool) {
		for _, row := range rows {
			r2 := &doc.Row{Header: header}
			for _, cell := range arr(at(row, 1)) {
				if cell == nil {
					continue
				}
				c := doc.NewCell()
				c.RowSpan = max(1, num(at(cell, 2), 1))
				c.ColSpan = max(1, num(at(cell, 3), 1))
				c.Blocks = r.blocks(arr(at(cell, 4)), nil, pandocContext{})
				r2.Cells = append(r2.Cells, c)
			}
			t.Rows = append(t.Rows, r2)
		}
	}
	addRows(arr(at(at(content, 3), 1)), true)
	for _, body := range arr(at(content, 4)) {
		addRows(arr(at(body, 2)), false)
		addRows(arr(at(body, 3)), false)
	}
	addRows(arr(at(at(content, 5), 1)), false)
	t.Caption = r.blocks(arr(at(at(content, 1), 1)), nil, ctx)
	return t
}

// ───────────────────────── Inlines ─────────────────────────

func (r *pandocReader) inlines(inlines []any, output []doc.Inline, f doc.CharFormat) []doc.Inline {
	with := func(change func(*doc.CharFormat)) doc.CharFormat {
		c := f
		change(&c)
		return c
	}
	for _, in := range inlines {
		if in == nil {
			continue
		}
		c := pcontent(in)
		switch ptype(in) {
		case "Str":
			output = append(output, &doc.Text{Text: str(c), Format: f})
		case "Space", "SoftBreak":
			output = append(output, &doc.Text{Text: " ", Format: f})
		case "LineBreak":
			output = append(output, &doc.LineBreak{Format: f})
		case "Strong":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Bold = ptr(true) }))
		case "Emph":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Italic = ptr(true) }))
		case "Underline":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Underline = ptr(true) }))
		case "Strikeout":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Strike = ptr(true) }))
		case "Superscript":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Superscript, c.Subscript = ptr(true), nil }))
		case "Subscript":
			output = r.inlines(arr(c), output, with(func(c *doc.CharFormat) { c.Subscript, c.Superscript = ptr(true), nil }))
		case "SmallCaps":
			output = r.inlines(arr(c), output, f)
		case "Span":
			if id := str(at(at(c, 0), 0)); id != "" {
				output = append(output, &doc.Bookmark{Name: id})
			}
			output = r.inlines(arr(at(c, 1)), output, f)
		case "Cite":
			output = r.inlines(arr(at(c, 1)), output, f)
		case "Quoted":
			open, close := "“", "”"
			if ptype(at(c, 0)) == "SingleQuote" {
				open, close = "‘", "’"
			}
			output = append(output, &doc.Text{Text: open, Format: f})
			output = r.inlines(arr(at(c, 1)), output, f)
			output = append(output, &doc.Text{Text: close, Format: f})
		case "Code":
			output = append(output, &doc.Text{Text: str(at(c, 1)), Format: f})
		case "Math":
			output = append(output, &doc.Text{Text: str(at(c, 1)), Format: with(func(c *doc.CharFormat) { c.Italic = ptr(true) })})
		case "Link":
			link := &doc.Link{Target: str(at(at(c, 2), 0))}
			link.Content = r.inlines(arr(at(c, 1)), nil, f)
			output = append(output, link)
		case "Image":
			if img := r.image(c); img != nil {
				output = append(output, img)
			}
		case "Note":
			output = append(output, &doc.Note{Blocks: r.blocks(arr(c), nil, pandocContext{})})
		}
		// RawInline: format-specific markup, nothing to show.
	}
	return output
}

func (r *pandocReader) image(content any) *doc.Image {
	// [attr [id, classes, [[key, value]]], caption inlines, [target, title]]
	p := r.resolveImage(str(at(at(content, 2), 0)))
	if p == "" || !fileExists(p) {
		return nil // missing image: skip rather than produce a broken reference
	}
	attributes := map[string]string{}
	for _, kv := range arr(at(at(content, 0), 2)) {
		if pair := arr(kv); len(pair) == 2 {
			if _, seen := attributes[str(pair[0])]; !seen {
				attributes[str(pair[0])] = str(pair[1])
			}
		}
	}
	img := &doc.Image{Path: p}
	if w, ok := doc.ParseLength(attributes["width"]); ok {
		img.Width = ptr(w)
	}
	if h, ok := doc.ParseLength(attributes["height"]); ok {
		img.Height = ptr(h)
	}
	return img
}

// ───────────────────────── Helpers ─────────────────────────

func pandocTitle(meta any) string {
	m, _ := meta.(map[string]any)
	title := m["title"]
	switch ptype(title) {
	case "MetaInlines":
		return pandocPlain(arr(pcontent(title)))
	case "MetaString":
		return str(pcontent(title))
	}
	return ""
}

func pandocPlain(inlines []any) string {
	var sb strings.Builder
	for _, in := range inlines {
		c := pcontent(in)
		switch ptype(in) {
		case "Str":
			sb.WriteString(str(c))
		case "Space", "SoftBreak", "LineBreak":
			sb.WriteByte(' ')
		case "Code", "Math":
			sb.WriteString(str(at(c, 1)))
		case "Link", "Image", "Span", "Cite":
			sb.WriteString(pandocPlain(arr(at(c, 1))))
		case "Quoted":
			sb.WriteString(`"` + pandocPlain(arr(at(c, 1))) + `"`)
		case "Strong", "Emph", "Underline", "Strikeout", "Superscript", "Subscript", "SmallCaps":
			sb.WriteString(pandocPlain(arr(c)))
		}
	}
	return sb.String()
}
