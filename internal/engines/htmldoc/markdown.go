package htmldoc

import (
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Markdown writes a document as Markdown (the dialect the Markdown reader
// reads back: GitHub tables, ~sub~, ^sup^, footnotes): headings, emphasis,
// code, lists, block quotes, pipe tables, links, pictures and footnotes.
// imageTarget maps a picture file to the path written, "" to leave it out.
func Markdown(d *doc.Document, imageTarget func(string) string) string {
	w := &mdWriter{image: imageTarget, numbers: NewListNumbers()}
	var sb strings.Builder
	if strings.TrimSpace(d.Title) != "" {
		sb.WriteString("---\ntitle: \"" + strings.ReplaceAll(d.Title, `"`, `\"`) + "\"\n---\n\n")
	}
	for _, s := range d.Sections {
		w.blocks(s.Blocks, &sb, "")
	}
	for i, n := range w.notes {
		sb.WriteString("\n[^" + strconv.Itoa(i+1) + "]: " + n + "\n")
	}
	return strings.TrimRight(sb.String(), " \t\r\n") + "\n"
}

type mdWriter struct {
	image   func(string) string
	notes   []string
	numbers *ListNumbers
}

func (w *mdWriter) blocks(blocks []doc.Block, sb *strings.Builder, prefix string) {
	for i := 0; i < len(blocks); i++ {
		switch b := blocks[i].(type) {
		case *doc.Paragraph:
			if isCodeParagraph(b, true) {
				var lines []string
				for ; i < len(blocks); i++ {
					p, ok := blocks[i].(*doc.Paragraph)
					if !ok || !isCodeParagraph(p, true) {
						break
					}
					lines = append(lines, codeText(p))
				}
				i--
				fence := "```"
				for _, l := range lines {
					if strings.Contains(l, "```") {
						fence = "~~~~"
					}
				}
				sb.WriteString(prefix + fence + "\n")
				for _, l := range lines {
					sb.WriteString(prefix + l + "\n")
				}
				sb.WriteString(prefix + fence + "\n\n")
				continue
			}
			var next doc.Block
			if i+1 < len(blocks) {
				next = blocks[i+1]
			}
			w.paragraph(b, sb, prefix, next)
		case *doc.Table:
			w.table(b, sb, prefix)
		}
	}
}

func (w *mdWriter) paragraph(p *doc.Paragraph, sb *strings.Builder, prefix string, next doc.Block) {
	text := w.inlines(p.Inlines)
	if hasLine(p.Inlines) && strings.TrimSpace(text) == "" {
		sb.WriteString(prefix + "---\n\n")
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	// Block quotes: the readers indent quotes by 2000 HWPUNIT per level.
	quote := ""
	if p.List == nil {
		quote = strings.Repeat("> ", max(0, doc.Val(p.Format.Left, 0))/2000)
	}
	linePrefix := prefix + quote
	if p.HeadingLevel > 0 {
		sb.WriteString(linePrefix + strings.Repeat("#", max(1, min(6, p.HeadingLevel))) + " " + strings.ReplaceAll(text, "\\\n", " ") + "\n\n")
		return
	}
	if p.List != nil {
		indent := strings.Repeat(" ", p.List.Level*4)
		marker := ""
		if p.List.Numbered {
			if isBulletList(p.List) {
				marker = "- "
			} else {
				marker = w.orderedMarker(p.List)
			}
		}
		pad := len(marker)
		if !p.List.Numbered {
			pad = max(pad, 4)
		}
		continuation := linePrefix + indent + strings.Repeat(" ", pad)
		if p.List.Numbered {
			sb.WriteString(linePrefix + indent + marker)
		} else {
			sb.WriteString(continuation)
		}
		sb.WriteString(strings.ReplaceAll(text, "\n", "\n"+continuation) + "\n")
		// Items of one list stay together; a blank line ends the list.
		if np, ok := next.(*doc.Paragraph); !ok || np.List == nil {
			sb.WriteString("\n")
		}
		return
	}
	sb.WriteString(linePrefix + strings.ReplaceAll(escapeLineStart(text), "\n", "\n"+linePrefix) + "\n\n")
}

func isBulletList(l *doc.ListRef) bool {
	if l.Numbering == nil || len(l.Numbering.Levels) == 0 {
		return true
	}
	return l.Numbering.Levels[min(l.Level, len(l.Numbering.Levels)-1)].Bullet
}

// orderedMarker is "3. ": Markdown only knows numbers, so letters and roman numerals become their position.
func (w *mdWriter) orderedMarker(l *doc.ListRef) string {
	level := l.Numbering.Levels[min(l.Level, len(l.Numbering.Levels)-1)]
	number := w.numbers.Next(l)
	var digits strings.Builder
	for _, c := range number {
		if c >= '0' && c <= '9' {
			digits.WriteRune(c)
		}
	}
	if level.Format == "DIGIT" && digits.Len() > 0 {
		return digits.String() + ". "
	}
	return "1. "
}

func (w *mdWriter) table(t *doc.Table, sb *strings.Builder, prefix string) {
	if len(t.Rows) == 0 {
		return
	}
	columns := t.ColumnCount
	for _, r := range t.Rows {
		n := 0
		for _, c := range r.Cells {
			n += max(1, c.ColSpan)
		}
		columns = max(columns, n)
	}
	type rc struct{ r, c int }
	covered := map[rc]bool{}
	grid := make([][]string, len(t.Rows))
	for r, row := range t.Rows {
		cells := make([]string, columns)
		col := 0
		for _, cell := range row.Cells {
			for col < columns && covered[rc{r, col}] {
				col++
			}
			if col >= columns {
				break
			}
			var parts []string
			for _, b := range cell.Blocks {
				p, ok := b.(*doc.Paragraph)
				if !ok {
					continue
				}
				inlines := p.Inlines
				if row.Header {
					// Header cells are bold by nature: no ** inside them.
					inlines = make([]doc.Inline, len(p.Inlines))
					for i, in := range p.Inlines {
						if tx, ok := in.(*doc.Text); ok {
							f := tx.Format
							f.Bold = nil
							inlines[i] = &doc.Text{Text: tx.Text, Format: f}
						} else {
							inlines[i] = in
						}
					}
				}
				parts = append(parts, strings.ReplaceAll(strings.ReplaceAll(w.inlines(inlines), "\\\n", " "), "\n", " "))
			}
			cells[col] = strings.ReplaceAll(strings.Join(parts, " "), "|", `\|`)
			for rr := r; rr < r+max(1, cell.RowSpan); rr++ {
				for cc := col; cc < min(columns, col+max(1, cell.ColSpan)); cc++ {
					covered[rc{rr, cc}] = true
				}
			}
			col += max(1, cell.ColSpan)
		}
		grid[r] = cells
	}
	sb.WriteString(prefix + "| " + strings.Join(grid[0], " | ") + " |\n")
	sb.WriteString(prefix + "|" + strings.Repeat("---|", columns) + "\n")
	for _, row := range grid[1:] {
		sb.WriteString(prefix + "| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString("\n")
}

func (w *mdWriter) inlines(inlines []doc.Inline) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch in := in.(type) {
		case *doc.Text:
			sb.WriteString(mdRun(in.Text, in.Format))
		case *doc.Equation:
			sb.WriteString(mdRun(in.Text.Text, in.Text.Format))
		case *doc.LineBreak:
			sb.WriteString("\\\n")
		case *doc.Tab:
			sb.WriteString("    ")
		case *doc.Link:
			content := w.inlines(in.Content)
			// Bookmarks have no Markdown form: links inside the document keep their text only.
			if strings.HasPrefix(in.Target, "#") || strings.TrimSpace(content) == "" {
				sb.WriteString(content)
			} else {
				sb.WriteString("[" + content + "](<" + in.Target + ">)")
			}
		case *doc.Image:
			if t := w.image(in.Path); t != "" {
				sb.WriteString("![](<" + t + ">)")
			}
		case *doc.Note:
			var content strings.Builder
			w.blocks(in.Blocks, &content, "")
			w.notes = append(w.notes, strings.ReplaceAll(strings.TrimSpace(content.String()), "\n\n", "\n    "))
			sb.WriteString("[^" + strconv.Itoa(len(w.notes)) + "]")
		case *doc.TextBox:
			var parts []string
			for _, b := range in.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					parts = append(parts, w.inlines(p.Inlines))
				}
			}
			sb.WriteString(strings.Join(parts, " "))
		}
	}
	return sb.String()
}

func mdRun(text string, f doc.CharFormat) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	shade := doc.Val(f.Shade, "")
	if shade == CodeShade {
		if strings.Contains(text, "`") {
			return "`` " + text + " ``"
		}
		return "`" + text + "`"
	}
	// Emphasis markers must touch the text: spaces stay outside.
	core := mdEscape(strings.TrimSpace(text))
	lead := text[:len(text)-len(strings.TrimLeft(text, " \t\n"))]
	trail := text[len(strings.TrimRight(text, " \t\n")):]
	switch {
	case doc.Val(f.Superscript, false):
		core = "^" + strings.ReplaceAll(core, " ", `\ `) + "^"
	case doc.Val(f.Subscript, false):
		core = "~" + strings.ReplaceAll(core, " ", `\ `) + "~"
	}
	if doc.Val(f.Strike, false) {
		core = "~~" + core + "~~"
	}
	if doc.Val(f.Italic, false) {
		core = "*" + core + "*"
	}
	if doc.Val(f.Bold, false) {
		core = "**" + core + "**"
	}
	if shade == MarkShade {
		core = "==" + core + "=="
	}
	return lead + core + trail
}

func mdEscape(text string) string {
	var sb strings.Builder
	for _, c := range text {
		if strings.ContainsRune("\\`*_[]<>~^|=", c) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// escapeLineStart escapes text that would read as a heading, list item or quote at the start of a line.
func escapeLineStart(text string) string {
	trimmed := strings.TrimLeft(text, " \t")
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
		return `\` + trimmed
	}
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(trimmed) && (trimmed[digits] == '.' || trimmed[digits] == ')') {
		return trimmed[:digits] + `\` + trimmed[digits:]
	}
	return text
}
