package ebook

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
)

// Book → FictionBook 2: headings become nested sections with titles,
// paragraphs keep bold / italic / strikethrough / sub / sup / code and links,
// pictures become base64 binaries (JPEG or PNG), tables stay tables,
// footnotes go to a "notes" body. FB2 has no lists or line breaks: list
// markers are written out and a line break starts a new paragraph.

type fb2Binary struct{ id, file, mediaType string }

type fb2Writer struct {
	imageFolder string
	binaries    map[string]*fb2Binary
	order       []*fb2Binary
	notes       [][]doc.Block
	numbers     *htmldoc.ListNumbers
	sb          strings.Builder
	depth       int
}

type fb2Section struct {
	title    *doc.Paragraph
	level    int
	blocks   []doc.Block
	children []*fb2Section
}

func writeFb2(b *Book, output, sourcePath, work string) error {
	folder, err := newFolder(work, "fb2-images")
	if err != nil {
		return err
	}
	w := &fb2Writer{imageFolder: folder, binaries: map[string]*fb2Binary{}, numbers: htmldoc.NewListNumbers()}
	root := w.tree(b.Doc)
	w.raw("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	w.raw(`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">` + "\n")
	w.description(b, sourcePath)
	w.raw("<body>\n")
	for _, s := range root.children {
		w.section(s)
	}
	if len(root.children) == 0 {
		w.raw("<section><empty-line/></section>\n")
	}
	w.raw("</body>\n")
	if len(w.notes) > 0 {
		w.raw("<body name=\"notes\">\n")
		// Notes may add notes of their own while written.
		for i := 0; i < len(w.notes); i++ {
			fmt.Fprintf(&w.sb, "<section id=\"n%d\"><title><p>%d</p></title>\n", i+1, i+1)
			w.blocks(w.notes[i])
			w.raw("</section>\n")
		}
		w.raw("</body>\n")
	}
	for _, bin := range w.order {
		data, err := os.ReadFile(bin.file)
		if err != nil {
			continue
		}
		fmt.Fprintf(&w.sb, "<binary id=\"%s\" content-type=\"%s\">", esc(bin.id), bin.mediaType)
		enc := base64.StdEncoding.EncodeToString(data)
		for len(enc) > 76 {
			w.raw(enc[:76] + "\n")
			enc = enc[76:]
		}
		w.raw(enc + "</binary>\n")
	}
	w.raw("</FictionBook>\n")
	temp := output + ".tmp"
	if err := os.WriteFile(temp, []byte(w.sb.String()), 0o644); err != nil {
		return err
	}
	os.Remove(output)
	return os.Rename(temp, output)
}

func (w *fb2Writer) raw(s string) { w.sb.WriteString(s) }

// tree nests the blocks into sections by heading level.
func (w *fb2Writer) tree(d *doc.Document) *fb2Section {
	root := &fb2Section{}
	stack := []*fb2Section{root}
	for _, s := range d.Sections {
		for _, b := range s.Blocks {
			if p, ok := b.(*doc.Paragraph); ok && p.HeadingLevel > 0 {
				for stack[len(stack)-1].level >= p.HeadingLevel {
					stack = stack[:len(stack)-1]
				}
				sec := &fb2Section{title: p, level: p.HeadingLevel}
				top := stack[len(stack)-1]
				top.children = append(top.children, sec)
				stack = append(stack, sec)
				continue
			}
			// Content before the first heading of the root goes into an untitled section.
			if len(stack) == 1 {
				untitled := &fb2Section{level: 1}
				root.children = append(root.children, untitled)
				stack = append(stack, untitled)
			}
			top := stack[len(stack)-1]
			top.blocks = append(top.blocks, b)
		}
	}
	return root
}

func (w *fb2Writer) description(b *Book, sourcePath string) {
	w.raw("<description>\n<title-info>\n<genre>antique</genre>\n") // required; calibre uses the same neutral default
	authors := b.Meta.Authors
	if len(authors) == 0 {
		authors = []string{""}
	}
	for _, a := range authors {
		parts := strings.Fields(a)
		w.raw("<author>")
		if len(parts) >= 2 {
			fmt.Fprintf(&w.sb, "<first-name>%s</first-name><last-name>%s</last-name>", esc(strings.Join(parts[:len(parts)-1], " ")), esc(parts[len(parts)-1]))
		} else {
			name := "Unknown"
			if len(parts) == 1 {
				name = parts[0]
			}
			fmt.Fprintf(&w.sb, "<nickname>%s</nickname>", esc(name))
		}
		w.raw("</author>\n")
	}
	fmt.Fprintf(&w.sb, "<book-title>%s</book-title>\n", esc(b.DisplayTitle(sourcePath)))
	if b.Meta.Description != "" {
		fmt.Fprintf(&w.sb, "<annotation><p>%s</p></annotation>\n", esc(b.Meta.Description))
	}
	fmt.Fprintf(&w.sb, "<lang>%s</lang>\n", esc(strings.Split(b.DisplayLanguage(), "-")[0]))
	if c := b.Meta.CoverImage; c != "" {
		if id := w.binary(c); id != "" {
			w.raw("<coverpage>")
			w.image(id)
			w.raw("</coverpage>\n")
		}
	}
	now := time.Now().Format("2006-01-02")
	w.raw("</title-info>\n<document-info>\n<author><nickname>Filee</nickname></author>\n<program-used>Filee</program-used>\n")
	fmt.Fprintf(&w.sb, "<date value=\"%s\">%s</date>\n<id>%s</id>\n<version>1.0</version>\n</document-info>\n</description>\n", now, now, uuid.NewString())
}

func (w *fb2Writer) section(s *fb2Section) {
	w.raw("<section")
	if s.title != nil {
		if id := fb2Bookmark(s.title.Inlines); id != "" {
			fmt.Fprintf(&w.sb, ` id="%s"`, esc(id))
		}
	}
	w.raw(">\n")
	if s.title != nil {
		var inlines []doc.Inline
		for _, in := range s.title.Inlines {
			if _, ok := in.(*doc.Bookmark); !ok {
				inlines = append(inlines, in) // the id is on the section
			}
		}
		w.raw("<title>")
		w.paragraphs(inlines, "p")
		w.raw("</title>\n")
	}
	// FB2 sections hold either content or sub-sections: leading content moves into an untitled section.
	switch {
	case len(s.children) > 0 && len(s.blocks) > 0:
		w.raw("<section>\n")
		w.blocks(s.blocks)
		w.raw("</section>\n")
	case len(s.blocks) > 0:
		w.blocks(s.blocks)
	}
	for _, c := range s.children {
		w.section(c)
	}
	if len(s.blocks) == 0 && len(s.children) == 0 {
		w.raw("<empty-line/>")
	}
	w.raw("</section>\n")
}

func (w *fb2Writer) blocks(blocks []doc.Block) {
	for _, b := range blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			w.paragraph(b)
		case *doc.Table:
			w.table(b)
		}
	}
}

func (w *fb2Writer) paragraph(p *doc.Paragraph) {
	visible := false
	for _, in := range doc.InlinesOf(p.Inlines) {
		switch in := in.(type) {
		case *doc.Image, *doc.Note:
			visible = true
		case *doc.Text:
			if strings.TrimSpace(in.Text) != "" {
				visible = true
			}
		}
	}
	if !visible {
		lineShape := false
		for _, in := range p.Inlines {
			if s, ok := in.(*doc.Shape); ok && s.Kind == doc.ShapeLine {
				lineShape = true
			}
		}
		if lineShape {
			w.raw("<subtitle>* * *</subtitle>\n")
		} else {
			w.raw("<empty-line/>\n")
		}
		return
	}
	// A picture on its own line is a block image.
	var rest []doc.Inline
	for _, in := range p.Inlines {
		if _, ok := in.(*doc.Bookmark); !ok {
			rest = append(rest, in)
		}
	}
	if len(rest) == 1 {
		if im, ok := rest[0].(*doc.Image); ok {
			if id := w.binary(im.Path); id != "" {
				w.image(id)
				w.raw("\n")
				return
			}
		}
	}
	inlines := p.Inlines
	if p.List != nil {
		marker := ""
		if p.List.Numbered {
			marker = w.numbers.Next(p.List) + " "
		}
		inlines = append([]doc.Inline{&doc.Text{Text: strings.Repeat(" ", p.List.Level*4) + marker}}, inlines...)
	}
	element := "p"
	if doc.Val(p.Format.Align, doc.AlignLeft) == doc.AlignCenter {
		allBold := true
		for _, in := range p.Inlines {
			if t, ok := in.(*doc.Text); ok && !doc.Val(t.Format.Bold, false) {
				allBold = false
			}
		}
		if allBold {
			element = "subtitle"
		}
	}
	w.paragraphs(inlines, element)
}

// paragraphs writes inlines as one element per line (FB2 paragraphs have no line breaks).
func (w *fb2Writer) paragraphs(inlines []doc.Inline, element string) {
	lines := [][]doc.Inline{nil}
	for _, in := range inlines {
		if _, ok := in.(*doc.LineBreak); ok {
			lines = append(lines, nil)
			continue
		}
		lines[len(lines)-1] = append(lines[len(lines)-1], in)
	}
	for _, l := range lines {
		w.raw("<" + element)
		if id := fb2Bookmark(l); id != "" {
			fmt.Fprintf(&w.sb, ` id="%s"`, esc(id))
		}
		w.raw(">")
		w.inlines(l)
		w.raw("</" + element + ">\n")
	}
}

func fb2Bookmark(inlines []doc.Inline) string {
	for _, in := range inlines {
		if b, ok := in.(*doc.Bookmark); ok {
			return fb2ID(b.Name)
		}
	}
	return ""
}

func fb2ID(name string) string {
	var sb strings.Builder
	for _, c := range name {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			sb.WriteRune(c)
		} else {
			sb.WriteByte('_')
		}
	}
	id := sb.String()
	if id != "" && (id[0] >= 'a' && id[0] <= 'z' || id[0] >= 'A' && id[0] <= 'Z') {
		return id
	}
	return "id-" + id
}

func (w *fb2Writer) inlines(inlines []doc.Inline) {
	for _, in := range inlines {
		switch in := in.(type) {
		case *doc.Text:
			w.run(in.Text, in.Format)
		case *doc.Equation:
			w.run(in.Text.Text, in.Text.Format)
		case *doc.Tab:
			w.raw("    ")
		case *doc.Link:
			target := in.Target
			if strings.HasPrefix(target, "#") {
				target = "#" + fb2ID(target[1:])
			}
			fmt.Fprintf(&w.sb, `<a l:href="%s">`, esc(target))
			var content []doc.Inline
			for _, c := range in.Content {
				if _, br := c.(*doc.LineBreak); !br {
					content = append(content, c)
				}
			}
			w.inlines(content)
			w.raw("</a>")
		case *doc.Image:
			if id := w.binary(in.Path); id != "" {
				w.image(id)
			}
		case *doc.Note:
			w.notes = append(w.notes, in.Blocks)
			n := len(w.notes)
			fmt.Fprintf(&w.sb, `<a l:href="#n%d" type="note">[%d]</a>`, n, n)
		case *doc.TextBox:
			var content []doc.Inline
			for _, b := range in.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					content = append(content, &doc.Text{Text: " "})
					for _, c := range p.Inlines {
						if _, br := c.(*doc.LineBreak); !br {
							content = append(content, c)
						}
					}
				}
			}
			w.inlines(content)
		}
	}
}

func (w *fb2Writer) run(text string, f doc.CharFormat) {
	var closers []string
	open := func(name string) {
		w.raw("<" + name + ">")
		closers = append(closers, "</"+name+">")
	}
	if doc.Val(f.Shade, "") == htmldoc.CodeShade {
		open("code")
	}
	if doc.Val(f.Bold, false) {
		open("strong")
	}
	if doc.Val(f.Italic, false) {
		open("emphasis")
	}
	if doc.Val(f.Strike, false) {
		open("strikethrough")
	}
	if doc.Val(f.Superscript, false) {
		open("sup")
	} else if doc.Val(f.Subscript, false) {
		open("sub")
	}
	w.raw(esc(cleanXML(text)))
	for i := len(closers) - 1; i >= 0; i-- {
		w.raw(closers[i])
	}
}

// cleanXML removes characters XML 1.0 does not allow (e.g. vertical tabs from Word).
func cleanXML(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

func (w *fb2Writer) table(t *doc.Table) {
	w.raw("<table>\n")
	for _, row := range t.Rows {
		w.raw("<tr>")
		for _, cell := range row.Cells {
			tag := "td"
			if row.Header {
				tag = "th"
			}
			w.raw("<" + tag)
			if cell.ColSpan > 1 {
				fmt.Fprintf(&w.sb, ` colspan="%d"`, cell.ColSpan)
			}
			if cell.RowSpan > 1 {
				fmt.Fprintf(&w.sb, ` rowspan="%d"`, cell.RowSpan)
			}
			w.raw(">")
			first := true
			for _, b := range cell.Blocks {
				p, ok := b.(*doc.Paragraph)
				if !ok {
					continue
				}
				if !first {
					w.raw(" ")
				}
				first = false
				var content []doc.Inline
				for _, in := range p.Inlines {
					switch in.(type) {
					case *doc.LineBreak, *doc.Image:
					default:
						content = append(content, in)
					}
				}
				w.inlines(content)
			}
			w.raw("</" + tag + ">")
		}
		w.raw("</tr>\n")
	}
	w.raw("</table>\n")
}

func (w *fb2Writer) image(id string) { fmt.Fprintf(&w.sb, `<image l:href="#%s"/>`, esc(id)) }

// binary is the binary id of a picture (JPEG or PNG, converted when needed), "" when it can't be read.
func (w *fb2Writer) binary(path string) string {
	key := strings.ToLower(path)
	if b, ok := w.binaries[key]; ok {
		return b.id
	}
	file, mt, ok := commonImage(path, w.imageFolder, false)
	if !ok {
		return ""
	}
	ext := "jpg"
	if mt == "image/png" {
		ext = "png"
	}
	b := &fb2Binary{fmt.Sprintf("img%d.%s", len(w.order)+1, ext), file, mt}
	w.binaries[key] = b
	w.order = append(w.order, b)
	return b.id
}
