package hwpx

// Document model → plain text and Markdown.
//   - Plain text: paragraphs separated by an empty line, list markers and numbers written out, table rows as
//     tab-separated cells and footnotes collected at the end.
//   - Markdown (the dialect the Markdown reader reads back: GitHub tables, ~sub~, ^sup^, footnotes): headings,
//     emphasis, code, lists, block quotes, pipe tables, links, pictures and footnotes.

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/doc"
)

// newline is what the PC ends a line with (Environment.NewLine).
func newline() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}

// ───────────────────────── Plain text ─────────────────────────

// PlainTextWrite is the text of a document.
func PlainTextWrite(d *doc.Document) string {
	var sb strings.Builder
	var notes []string
	numbers := NewListNumbers()
	for _, s := range d.Sections {
		plainBlocks(s.Blocks, &sb, &notes, numbers)
	}
	nl := newline()
	if len(notes) > 0 {
		sb.WriteString(nl + "----" + nl)
		for i, n := range notes {
			fmt.Fprintf(&sb, "[%d] %s%s", i+1, n, nl)
		}
	}
	return strings.TrimRightFunc(sb.String(), unicode.IsSpace) + nl
}

func plainBlocks(blocks []doc.Block, sb *strings.Builder, notes *[]string, numbers *ListNumbers) {
	nl := newline()
	var previous doc.Block
	for _, b := range blocks {
		switch v := b.(type) {
		case *doc.Paragraph:
			text := plainInlines(v.Inlines, notes)
			if text == "" {
				for _, in := range v.Inlines {
					if s, ok := in.(*doc.Shape); ok && s.Kind == doc.ShapeLine {
						text = "* * *"
						break
					}
				}
			}
			if strings.TrimSpace(text) == "" && v.List == nil {
				continue // spacing paragraphs: the empty line between paragraphs already separates them
			}
			// List items and code lines stay together; everything else is separated by an empty line.
			together := false
			if before, ok := previous.(*doc.Paragraph); ok {
				together = before.List != nil && v.List != nil || isCodeParagraph(before) && isCodeParagraph(v)
			}
			if sb.Len() > 0 {
				sb.WriteString(nl)
			}
			if sb.Len() > 0 && !together {
				sb.WriteString(nl)
			}
			indent := max(0, val(v.Format.Left, 0)) / 1000
			if v.List != nil {
				marker := ""
				if v.List.Numbered {
					marker = numbers.Next(v.List) + " "
				}
				pad := strings.Repeat(" ", v.List.Level*4+indent)
				sb.WriteString(pad + marker + strings.ReplaceAll(text, "\n", "\n"+pad+strings.Repeat(" ", len([]rune(marker)))))
			} else {
				pad := strings.Repeat(" ", indent)
				if indent > 0 {
					text = strings.ReplaceAll(text, "\n", "\n"+pad)
				}
				sb.WriteString(pad + text)
			}
			previous = v
		case *doc.Table:
			if len(v.Rows) == 0 {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString(nl + nl)
			}
			for i, row := range v.Rows {
				if i > 0 {
					sb.WriteString(nl)
				}
				cells := make([]string, len(row.Cells))
				for j, c := range row.Cells {
					cells[j] = plainCell(c, notes)
				}
				sb.WriteString(strings.Join(cells, "\t"))
			}
			previous = v
		}
	}
}

func isCodeParagraph(p *doc.Paragraph) bool {
	if len(p.Inlines) == 0 {
		return false
	}
	for _, in := range p.Inlines {
		t, ok := in.(*doc.Text)
		if !ok || !strIs(t.Format.Shade, CodeShade) {
			return false
		}
	}
	return true
}

func plainCell(c *doc.Cell, notes *[]string) string {
	var parts []string
	for _, b := range c.Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			t := strings.NewReplacer("\r\n", " ", "\n", " ", "\t", " ").Replace(plainInlines(p.Inlines, notes))
			if t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, " ")
}

func plainInlines(inlines []doc.Inline, notes *[]string) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch v := in.(type) {
		case *doc.Text:
			sb.WriteString(v.Text)
		case *doc.Equation:
			sb.WriteString(v.Text.Text)
		case *doc.LineBreak:
			sb.WriteByte('\n')
		case *doc.Tab:
			sb.WriteByte('\t')
		case *doc.Link:
			sb.WriteString(plainInlines(v.Content, notes))
		case *doc.Note:
			var content strings.Builder
			plainBlocks(v.Blocks, &content, notes, NewListNumbers())
			nl := newline()
			*notes = append(*notes, strings.ReplaceAll(strings.TrimSpace(content.String()), nl+nl, " "))
			fmt.Fprintf(&sb, "[%d]", len(*notes))
		case *doc.TextBox:
			var parts []string
			for _, b := range v.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					parts = append(parts, plainInlines(p.Inlines, notes))
				}
			}
			sb.WriteString(strings.Join(parts, " "))
		}
	}
	return strings.TrimRightFunc(strings.ReplaceAll(sb.String(), "\n", newline()), unicode.IsSpace)
}

// ListNumbers counts list items and formats their numbers like 한글 does ("1.", "가.", "iv)", "●").
type ListNumbers struct{ counters map[*doc.Numbering][]int }

func NewListNumbers() *ListNumbers { return &ListNumbers{counters: map[*doc.Numbering][]int{}} }

// Next is the marker of the next numbered item of a list.
func (n *ListNumbers) Next(item *doc.ListRef) string {
	levels := item.Numbering.Levels
	if len(levels) == 0 {
		return "•"
	}
	level := min(item.Level, len(levels)-1)
	definition := levels[level]
	if definition.Bullet {
		if definition.Text != "" {
			return definition.Text
		}
		return "•"
	}
	counters, ok := n.counters[item.Numbering]
	if !ok {
		counters = make([]int, max(10, len(levels)))
		n.counters[item.Numbering] = counters
	}
	if counters[level] == 0 {
		counters[level] = definition.Start
	} else {
		counters[level]++
	}
	for deeper := level + 1; deeper < len(counters); deeper++ {
		counters[deeper] = 0
	}
	text := definition.Text
	for l := min(9, len(levels)); l >= 1; l-- {
		value := counters[l-1]
		if value == 0 {
			value = levels[l-1].Start
		}
		text = strings.ReplaceAll(text, fmt.Sprintf("^%d", l), FormatNumber(value, levels[l-1].Format))
	}
	return text
}

// FormatNumber writes a number in an OWPML number format.
func FormatNumber(value int, format string) string {
	switch format {
	case "LATIN_SMALL":
		return lettersFrom(value, 'a')
	case "LATIN_CAPITAL":
		return lettersFrom(value, 'A')
	case "ROMAN_SMALL":
		if value >= 1 && value <= 3999 {
			return strings.ToLower(roman(value))
		}
	case "ROMAN_CAPITAL":
		if value >= 1 && value <= 3999 {
			return roman(value)
		}
	case "HANGUL_SYLLABLE":
		if value >= 1 && value <= 14 {
			return string([]rune("가나다라마바사아자차카타파하")[value-1])
		}
	case "CIRCLED_DIGIT":
		if value >= 1 && value <= 20 {
			return string(rune('①' + value - 1))
		}
	}
	return strconv.Itoa(value)
}

func lettersFrom(value int, first rune) string {
	if value < 1 {
		return strconv.Itoa(value)
	}
	text := ""
	for ; value > 0; value = (value - 1) / 26 {
		text = string(first+rune((value-1)%26)) + text
	}
	return text
}

// ───────────────────────── Markdown ─────────────────────────

type markdownWriter struct {
	imageTarget func(string) string
	notes       []string
	numbers     *ListNumbers
}

// MarkdownWrite is the Markdown text of a document. imageTarget maps a picture file to the path written into the
// Markdown, or "" to leave it out.
func MarkdownWrite(d *doc.Document, imageTarget func(string) string) string {
	if imageTarget == nil {
		imageTarget = func(string) string { return "" }
	}
	w := &markdownWriter{imageTarget: imageTarget, numbers: NewListNumbers()}
	var sb strings.Builder
	if strings.TrimSpace(d.Title) != "" {
		sb.WriteString("---\ntitle: \"" + strings.ReplaceAll(d.Title, `"`, `\"`) + "\"\n---\n\n")
	}
	for _, s := range d.Sections {
		w.blocks(s.Blocks, &sb, "")
	}
	for i, n := range w.notes {
		fmt.Fprintf(&sb, "\n[^%d]: %s\n", i+1, n)
	}
	return strings.TrimRightFunc(sb.String(), unicode.IsSpace) + "\n"
}

func (w *markdownWriter) blocks(blocks []doc.Block, sb *strings.Builder, prefix string) {
	for i := 0; i < len(blocks); i++ {
		switch v := blocks[i].(type) {
		case *doc.Paragraph:
			if isMarkdownCode(v) {
				var lines []string
				for ; i < len(blocks); i++ {
					p, ok := blocks[i].(*doc.Paragraph)
					if !ok || !isMarkdownCode(p) {
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
			w.paragraph(v, sb, prefix, next)
		case *doc.Table:
			w.table(v, sb, prefix)
		}
	}
}

func isMarkdownCode(p *doc.Paragraph) bool {
	return p.List == nil && p.HeadingLevel == 0 && isCodeParagraph(p)
}

func (w *markdownWriter) paragraph(p *doc.Paragraph, sb *strings.Builder, prefix string, next doc.Block) {
	text := w.inlines(p.Inlines)
	if strings.TrimSpace(text) == "" {
		for _, in := range p.Inlines {
			if s, ok := in.(*doc.Shape); ok && s.Kind == doc.ShapeLine {
				sb.WriteString(prefix + "---\n\n")
				return
			}
		}
		return
	}
	// Block quotes: the readers indent quotes by 2000 HWPUNIT per level.
	quote := ""
	if p.List == nil {
		quote = strings.Repeat("> ", max(0, val(p.Format.Left, 0))/2000)
	}
	linePrefix := prefix + quote
	if p.HeadingLevel > 0 {
		sb.WriteString(linePrefix + strings.Repeat("#", clamp(p.HeadingLevel, 1, 6)) + " " + strings.ReplaceAll(text, "\\\n", " ") + "\n\n")
		return
	}
	if list := p.List; list != nil {
		indent := strings.Repeat(" ", list.Level*4)
		marker := ""
		if list.Numbered {
			if isBulletList(list) {
				marker = "- "
			} else {
				marker = w.orderedMarker(list)
			}
		}
		width := len(marker)
		if !list.Numbered {
			width = max(width, 4)
		}
		continuation := linePrefix + indent + strings.Repeat(" ", width)
		if list.Numbered {
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

func isBulletList(list *doc.ListRef) bool {
	levels := list.Numbering.Levels
	return len(levels) == 0 || levels[min(list.Level, len(levels)-1)].Bullet
}

// orderedMarker is "3. " — Markdown only knows numbers, so letters and roman numerals become their position.
func (w *markdownWriter) orderedMarker(list *doc.ListRef) string {
	levels := list.Numbering.Levels
	level := levels[min(list.Level, len(levels)-1)]
	number := w.numbers.Next(list)
	var digits strings.Builder
	for _, c := range number {
		if isASCIIDigit(c) {
			digits.WriteRune(c)
		}
	}
	if level.Format == "DIGIT" && digits.Len() > 0 {
		return digits.String() + ". "
	}
	return "1. "
}

func (w *markdownWriter) table(t *doc.Table, sb *strings.Builder, prefix string) {
	if len(t.Rows) == 0 {
		return
	}
	columns := t.ColumnCount
	for _, r := range t.Rows {
		n := 0
		for _, c := range r.Cells {
			n += c.ColSpan
		}
		columns = max(columns, n)
	}
	var grid [][]string
	covered := map[[2]int]bool{}
	for ri, row := range t.Rows {
		cells := make([]string, columns)
		column := 0
		for _, cell := range row.Cells {
			for column < columns && covered[[2]int{ri, column}] {
				column++
			}
			if column >= columns {
				break
			}
			// Header cells are bold by nature: no ** inside them.
			var parts []string
			for _, b := range cell.Blocks {
				p, ok := b.(*doc.Paragraph)
				if !ok {
					continue
				}
				inlines := p.Inlines
				if row.Header {
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
			cells[column] = strings.ReplaceAll(strings.Join(parts, " "), "|", "\\|")
			for rr := ri; rr < ri+cell.RowSpan; rr++ {
				for cc := column; cc < min(columns, column+cell.ColSpan); cc++ {
					covered[[2]int{rr, cc}] = true
				}
			}
			column += cell.ColSpan
		}
		grid = append(grid, cells)
	}
	sb.WriteString(prefix + "| " + strings.Join(grid[0], " | ") + " |\n")
	sb.WriteString(prefix + "|" + strings.Repeat("---|", columns) + "\n")
	for _, row := range grid[1:] {
		sb.WriteString(prefix + "| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString("\n")
}

func (w *markdownWriter) inlines(inlines []doc.Inline) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch v := in.(type) {
		case *doc.Text:
			sb.WriteString(markdownRun(v.Text, v.Format))
		case *doc.Equation:
			sb.WriteString(markdownRun(v.Text.Text, v.Text.Format))
		case *doc.LineBreak:
			sb.WriteString("\\\n")
		case *doc.Tab:
			sb.WriteString("    ")
		case *doc.Link:
			content := w.inlines(v.Content)
			// Bookmarks have no Markdown form: links inside the document keep their text only.
			if strings.HasPrefix(v.Target, "#") || strings.TrimSpace(content) == "" {
				sb.WriteString(content)
			} else {
				sb.WriteString("[" + content + "](<" + v.Target + ">)")
			}
		case *doc.Image:
			if target := w.imageTarget(v.Path); target != "" {
				sb.WriteString("![](<" + target + ">)")
			}
		case *doc.Note:
			var content strings.Builder
			w.blocks(v.Blocks, &content, "")
			w.notes = append(w.notes, strings.ReplaceAll(strings.TrimSpace(content.String()), "\n\n", "\n    "))
			fmt.Fprintf(&sb, "[^%d]", len(w.notes))
		case *doc.TextBox:
			var parts []string
			for _, b := range v.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					parts = append(parts, w.inlines(p.Inlines))
				}
			}
			sb.WriteString(strings.Join(parts, " "))
		}
	}
	return sb.String()
}

func markdownRun(text string, f doc.CharFormat) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if strIs(f.Shade, CodeShade) {
		if strings.Contains(text, "`") {
			return "`` " + text + " ``"
		}
		return "`" + text + "`"
	}
	// Emphasis markers must touch the text: spaces stay outside.
	core := markdownEscape(strings.TrimSpace(text))
	lead := text[:len(text)-len(strings.TrimLeftFunc(text, unicode.IsSpace))]
	trail := text[len(strings.TrimRightFunc(text, unicode.IsSpace)):]
	if isTrue(f.Superscript) {
		core = "^" + strings.ReplaceAll(core, " ", "\\ ") + "^"
	} else if isTrue(f.Subscript) {
		core = "~" + strings.ReplaceAll(core, " ", "\\ ") + "~"
	}
	if isTrue(f.Strike) {
		core = "~~" + core + "~~"
	}
	if isTrue(f.Italic) {
		core = "*" + core + "*"
	}
	if isTrue(f.Bold) {
		core = "**" + core + "**"
	}
	if strIs(f.Shade, MarkShade) {
		core = "==" + core + "=="
	}
	return lead + core + trail
}

func markdownEscape(text string) string {
	var sb strings.Builder
	for _, ch := range text {
		if strings.ContainsRune("\\`*_[]<>~^|=", ch) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(ch)
	}
	return sb.String()
}

// escapeLineStart escapes text that would read as a heading, list item or quote at the start of a line.
func escapeLineStart(text string) string {
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
		return "\\" + trimmed
	}
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(trimmed) && (trimmed[digits] == '.' || trimmed[digits] == ')') {
		return trimmed[:digits] + "\\" + trimmed[digits:]
	}
	return text
}
