package ebook

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
	"github.com/dseif0x/filee-go/internal/xdom"
)

// FictionBook 2 (FB2) → Book. FB2 is XML: description/title-info holds the
// metadata (title, authors, language, cover), bodies hold nested sections
// (their titles become headings by depth), binary elements hold the
// pictures as base64. Poems, epigraphs, citations, subtitles and tables are
// kept; note links (type="note") become real footnotes with the text of the
// notes body. Every top-level section starts a new page.

const fb2Indent = 2000

type fb2Reader struct {
	binaries map[string]string
	notes    map[string]*xdom.Node
}

func readFb2Book(path, work string) (*Book, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, _, err := parseXML(b)
	if err != nil {
		return nil, errors.New("this is not a FictionBook (FB2) file: " + err.Error())
	}
	if root.Name.Local != "FictionBook" {
		return nil, errors.New("this is not a FictionBook (FB2) file")
	}
	r := &fb2Reader{binaries: map[string]string{}, notes: map[string]*xdom.Node{}}
	folder, err := newFolder(work, "fb2")
	if err != nil {
		return nil, err
	}
	r.saveBinaries(root, folder)
	bodies := localChildren(root, "body")
	for _, body := range bodies {
		if n := body.A("name"); n == "notes" || n == "comments" {
			for _, s := range body.DescendantsLocal("section") {
				if id := s.A("id"); id != "" {
					if _, ok := r.notes[id]; !ok {
						r.notes[id] = s
					}
				}
			}
		}
	}
	meta := r.metadata(root)
	section := doc.NewSection()
	for _, body := range bodies {
		if n := body.A("name"); n == "notes" || n == "comments" {
			continue
		}
		section.Blocks = r.body(body, section.Blocks)
	}
	d := &doc.Document{Title: meta.Title, Sections: []*doc.Section{section}}
	htmldoc.PruneBookmarks(d)
	return &Book{d, meta}, nil
}

func trimmedValue(n *xdom.Node) string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(n.Value())
}

func (r *fb2Reader) metadata(root *xdom.Node) *Metadata {
	desc := localChild(root, "description")
	info := localChild(desc, "title-info")
	m := &Metadata{
		Title:       trimmedValue(localChild(info, "book-title")),
		Language:    trimmedValue(localChild(info, "lang")),
		Description: trimmedValue(localChild(info, "annotation")),
		Publisher:   trimmedValue(localChild(localChild(desc, "publish-info"), "publisher")),
	}
	for _, a := range localChildren(info, "author") {
		var parts []string
		for _, n := range []string{"first-name", "middle-name", "last-name"} {
			if v := trimmedValue(localChild(a, n)); v != "" {
				parts = append(parts, v)
			}
		}
		name := strings.Join(parts, " ")
		if name == "" {
			name = trimmedValue(localChild(a, "nickname"))
		}
		if name != "" {
			m.Authors = append(m.Authors, name)
		}
	}
	if cover := localChild(localChild(info, "coverpage"), "image"); cover != nil {
		if file, ok := r.binaries[strings.TrimPrefix(attrLocal(cover, "href"), "#")]; ok {
			m.CoverImage = file
		}
	}
	return m
}

func (r *fb2Reader) saveBinaries(root *xdom.Node, folder string) {
	for _, bin := range localChildren(root, "binary") {
		id := bin.A("id")
		if id == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(bin.Value()), ""))
		if err != nil {
			continue
		}
		ext := "jpg"
		switch strings.ToLower(bin.A("content-type")) {
		case "image/png":
			ext = "png"
		case "image/gif":
			ext = "gif"
		case "image/jpeg", "image/jpg":
		default:
			if len(data) > 1 && data[0] == 0x89 && data[1] == 'P' {
				ext = "png"
			} else if len(data) > 2 && string(data[:3]) == "GIF" {
				ext = "gif"
			}
		}
		file := filepath.Join(folder, "binary"+pad4(len(r.binaries)+1)+"."+ext)
		if os.WriteFile(file, data, 0o644) == nil {
			r.binaries[id] = file
		}
	}
}

func pad4(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func (r *fb2Reader) body(body *xdom.Node, out []doc.Block) []doc.Block {
	for _, e := range body.Elements() {
		switch e.Name.Local {
		case "title":
			out = r.title(e, out, 1, true)
		case "epigraph":
			out = r.epigraph(e, out)
		case "image":
			out = r.imageBlock(e, out, 0)
		case "section":
			out = r.section(e, out, 1)
		}
	}
	return out
}

func (r *fb2Reader) section(s *xdom.Node, out []doc.Block, depth int) []doc.Block {
	start := len(out)
	for _, e := range s.Elements() {
		out = r.block(e, out, depth, 0)
	}
	if len(out) == start {
		return out
	}
	// Every top-level section (a chapter) starts on a new page; bookmarks let links reach the section.
	first, ok := out[start].(*doc.Paragraph)
	if !ok {
		first = &doc.Paragraph{}
		out = append(out[:start], append([]doc.Block{first}, out[start:]...)...)
	}
	first.PageBreakBefore = depth == 1 && start > 0
	if id := s.A("id"); id != "" {
		first.Inlines = append([]doc.Inline{&doc.Bookmark{Name: id}}, first.Inlines...)
	}
	return out
}

func indentFormat(indent int) doc.ParaFormat {
	var f doc.ParaFormat
	if indent > 0 {
		f.Left = doc.Ptr(indent)
	}
	return f
}

func (r *fb2Reader) block(e *xdom.Node, out []doc.Block, depth, indent int) []doc.Block {
	switch e.Name.Local {
	case "title":
		return r.title(e, out, depth, false)
	case "section":
		return r.section(e, out, depth+1)
	case "p":
		return append(out, r.paragraph(e, indentFormat(indent), doc.CharFormat{}))
	case "subtitle":
		return append(out, r.paragraph(e, doc.ParaFormat{Align: doc.Ptr(doc.AlignCenter)}, doc.CharFormat{Bold: doc.Ptr(true)}))
	case "empty-line":
		return append(out, &doc.Paragraph{})
	case "epigraph":
		return r.epigraph(e, out)
	case "annotation", "cite":
		for _, c := range e.Elements() {
			out = r.block(c, out, depth, indent+fb2Indent)
		}
		return out
	case "text-author":
		f := indentFormat(indent)
		f.Align = doc.Ptr(doc.AlignRight)
		return append(out, r.paragraph(e, f, doc.CharFormat{Italic: doc.Ptr(true)}))
	case "poem":
		return r.poem(e, out, depth, indent+fb2Indent)
	case "image":
		return r.imageBlock(e, out, indent)
	case "table":
		return append(out, r.table(e))
	}
	return out
}

func (r *fb2Reader) title(t *xdom.Node, out []doc.Block, depth int, center bool) []doc.Block {
	// A title may have several paragraphs; they form one heading, one line each.
	h := &doc.Paragraph{HeadingLevel: max(1, min(6, depth))}
	if center {
		h.Format.Align = doc.Ptr(doc.AlignCenter)
	}
	for _, p := range localChildren(t, "p") {
		if len(h.Inlines) > 0 {
			h.Inlines = append(h.Inlines, &doc.LineBreak{})
		}
		h.Inlines = append(h.Inlines, r.line(p, doc.CharFormat{})...)
	}
	if len(h.Inlines) > 0 {
		out = append(out, h)
	}
	return out
}

func (r *fb2Reader) epigraph(e *xdom.Node, out []doc.Block) []doc.Block {
	for _, c := range e.Elements() {
		if c.Name.Local == "p" {
			out = append(out, r.paragraph(c, doc.ParaFormat{Align: doc.Ptr(doc.AlignRight)}, doc.CharFormat{Italic: doc.Ptr(true)}))
		} else {
			out = r.block(c, out, 0, fb2Indent)
		}
	}
	return out
}

func (r *fb2Reader) poem(poem *xdom.Node, out []doc.Block, depth, indent int) []doc.Block {
	for _, c := range poem.Elements() {
		switch c.Name.Local {
		case "stanza":
			// One paragraph per stanza, one line per verse.
			stanza := &doc.Paragraph{Format: doc.ParaFormat{Left: doc.Ptr(indent)}}
			for _, line := range c.Elements() {
				if line.Name.Local == "title" || line.Name.Local == "subtitle" {
					src := line
					if line.Name.Local == "title" {
						if p := localChild(line, "p"); p != nil {
							src = p
						}
					}
					out = append(out, r.paragraph(src, doc.ParaFormat{Left: doc.Ptr(indent)}, doc.CharFormat{Bold: doc.Ptr(true)}))
					continue
				}
				if len(stanza.Inlines) > 0 {
					stanza.Inlines = append(stanza.Inlines, &doc.LineBreak{})
				}
				stanza.Inlines = append(stanza.Inlines, r.line(line, doc.CharFormat{})...)
			}
			if len(stanza.Inlines) > 0 {
				out = append(out, stanza)
			}
		case "title":
			out = r.title(c, out, depth+1, false)
		default:
			out = r.block(c, out, depth, indent)
		}
	}
	return out
}

func (r *fb2Reader) imageBlock(e *xdom.Node, out []doc.Block, indent int) []doc.Block {
	file, ok := r.binaries[strings.TrimPrefix(attrLocal(e, "href"), "#")]
	if !ok {
		return out
	}
	f := indentFormat(indent)
	f.Align = doc.Ptr(doc.AlignCenter)
	return append(out, &doc.Paragraph{Format: f, Inlines: []doc.Inline{&doc.Image{Path: file}}})
}

func (r *fb2Reader) table(t *xdom.Node) *doc.Table {
	table := &doc.Table{}
	for _, row := range localChildren(t, "tr") {
		var cells []*xdom.Node
		allTh := true
		for _, c := range row.Elements() {
			if c.Name.Local == "td" || c.Name.Local == "th" {
				cells = append(cells, c)
				if c.Name.Local != "th" {
					allTh = false
				}
			}
		}
		r2 := &doc.Row{Header: len(cells) > 0 && allTh}
		for _, c := range cells {
			cell := doc.NewCell()
			if v, err := strconv.Atoi(c.A("colspan")); err == nil {
				cell.ColSpan = max(1, v)
			}
			if v, err := strconv.Atoi(c.A("rowspan")); err == nil {
				cell.RowSpan = max(1, v)
			}
			var f doc.ParaFormat
			switch strings.ToLower(c.A("align")) {
			case "center":
				f.Align = doc.Ptr(doc.AlignCenter)
			case "right":
				f.Align = doc.Ptr(doc.AlignRight)
			}
			var cf doc.CharFormat
			if c.Name.Local == "th" {
				cf.Bold = doc.Ptr(true)
			}
			cell.Blocks = []doc.Block{r.paragraph(c, f, cf)}
			r2.Cells = append(r2.Cells, cell)
		}
		if len(r2.Cells) > 0 {
			table.Rows = append(table.Rows, r2)
		}
	}
	table.ColumnCount = 1
	for _, row := range table.Rows {
		n := 0
		for _, c := range row.Cells {
			n += c.ColSpan
		}
		table.ColumnCount = max(table.ColumnCount, n)
	}
	return table
}

func (r *fb2Reader) paragraph(e *xdom.Node, f doc.ParaFormat, cf doc.CharFormat) *doc.Paragraph {
	p := &doc.Paragraph{Format: f}
	if id := e.A("id"); id != "" {
		p.Inlines = append(p.Inlines, &doc.Bookmark{Name: id})
	}
	p.Inlines = r.inlines(e, p.Inlines, cf)
	p.Inlines = trimEdges(doc.MergeTexts(p.Inlines))
	return p
}

// line is the inlines of one line (a title paragraph, a verse), edges trimmed.
func (r *fb2Reader) line(e *xdom.Node, cf doc.CharFormat) []doc.Inline {
	return trimEdges(doc.MergeTexts(r.inlines(e, nil, cf)))
}

func collapseWhitespace(s string) string {
	var sb strings.Builder
	space := false
	for _, c := range s {
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			if !space {
				sb.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		sb.WriteRune(c)
	}
	return sb.String()
}

// trimEdges: pretty-printed files indent paragraph text; spaces at the paragraph edges are dropped.
func trimEdges(inlines []doc.Inline) []doc.Inline {
	for i, in := range inlines {
		if t, ok := in.(*doc.Text); ok {
			inlines[i] = &doc.Text{Text: strings.TrimLeft(t.Text, " \t\r\n"), Format: t.Format}
			break
		}
	}
	if n := len(inlines); n > 0 {
		if t, ok := inlines[n-1].(*doc.Text); ok {
			inlines[n-1] = &doc.Text{Text: strings.TrimRight(t.Text, " \t\r\n"), Format: t.Format}
		}
	}
	out := inlines[:0]
	for _, in := range inlines {
		if t, ok := in.(*doc.Text); ok && t.Text == "" {
			continue
		}
		out = append(out, in)
	}
	return out
}

func (r *fb2Reader) inlines(e *xdom.Node, out []doc.Inline, f doc.CharFormat) []doc.Inline {
	for _, n := range e.Children {
		if n.IsText() {
			if v := collapseWhitespace(n.Text); v != "" {
				out = append(out, &doc.Text{Text: v, Format: f})
			}
			continue
		}
		g := f
		switch n.Name.Local {
		case "strong":
			g.Bold = doc.Ptr(true)
		case "emphasis":
			g.Italic = doc.Ptr(true)
		case "strikethrough":
			g.Strike = doc.Ptr(true)
		case "sub":
			g.Subscript = doc.Ptr(true)
		case "sup":
			g.Superscript = doc.Ptr(true)
		case "code":
			g.Shade = doc.Ptr(htmldoc.CodeShade)
		case "image":
			if file, ok := r.binaries[strings.TrimPrefix(attrLocal(n, "href"), "#")]; ok {
				out = append(out, &doc.Image{Path: file})
			}
			continue
		case "a":
			out = r.link(n, out, f)
			continue
		}
		out = r.inlines(n, out, g) // style and unknown inline elements: their text
	}
	return out
}

func (r *fb2Reader) link(a *xdom.Node, out []doc.Inline, f doc.CharFormat) []doc.Inline {
	href := attrLocal(a, "href")
	if strings.HasPrefix(href, "#") {
		if section, ok := r.notes[href[1:]]; ok {
			// A note reference becomes a footnote with the note's text (its title is only the number).
			note := &doc.Note{}
			for _, c := range section.Elements() {
				if c.Name.Local != "title" {
					note.Blocks = r.block(c, note.Blocks, 0, 0)
				}
			}
			if len(note.Blocks) > 0 {
				return append(out, note)
			}
		}
	}
	if href == "" {
		return r.inlines(a, out, f)
	}
	l := &doc.Link{Target: href}
	l.Content = r.inlines(a, nil, f)
	return append(out, l)
}
