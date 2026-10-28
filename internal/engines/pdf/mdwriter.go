// Document → Markdown (GitHub tables): headings, emphasis, links, pictures
// and pipe tables, as the C# MarkdownWriter writes them for a read PDF.

package pdf

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type mdWriter struct {
	// imageTarget maps a picture file to its link, or "" to leave it out.
	imageTarget func(path string) string
}

// Markdown writes a document; imageTarget may be nil (no pictures).
func Markdown(doc *Document, imageTarget func(string) string) string {
	if imageTarget == nil {
		imageTarget = func(string) string { return "" }
	}
	w := &mdWriter{imageTarget}
	var b strings.Builder
	if strings.TrimSpace(doc.Title) != "" {
		fmt.Fprintf(&b, "---\ntitle: \"%s\"\n---\n\n", strings.ReplaceAll(doc.Title, `"`, `\"`))
	}
	for _, s := range doc.Sections {
		w.blocks(s.Blocks, &b)
	}
	return strings.TrimRight(b.String(), " \t\r\n") + "\n"
}

func (w *mdWriter) blocks(blocks []Block, b *strings.Builder) {
	for _, blk := range blocks {
		switch v := blk.(type) {
		case *Paragraph:
			w.paragraph(v, b)
		case *Table:
			w.table(v, b)
		}
	}
}

func (w *mdWriter) paragraph(p *Paragraph, b *strings.Builder) {
	text := w.inlines(p.Inlines)
	if strings.TrimSpace(text) == "" {
		return
	}
	if p.HeadingLevel > 0 {
		b.WriteString(strings.Repeat("#", max(1, min(p.HeadingLevel, 6))))
		b.WriteByte(' ')
		b.WriteString(strings.ReplaceAll(text, "\\\n", " "))
		b.WriteString("\n\n")
		return
	}
	b.WriteString(escapeLineStart(text))
	b.WriteString("\n\n")
}

func (w *mdWriter) table(t *Table, b *strings.Builder) {
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
	covered := map[[2]int]bool{}
	var grid [][]string
	for ri, r := range t.Rows {
		cells := make([]string, columns)
		col := 0
		for _, c := range r.Cells {
			for col < columns && covered[[2]int{ri, col}] {
				col++
			}
			if col >= columns {
				break
			}
			var parts []string
			for _, blk := range c.Blocks {
				if p, ok := blk.(*Paragraph); ok {
					s := w.inlines(p.Inlines)
					s = strings.ReplaceAll(strings.ReplaceAll(s, "\\\n", " "), "\n", " ")
					parts = append(parts, s)
				}
			}
			cells[col] = strings.ReplaceAll(strings.Join(parts, " "), "|", "\\|")
			for rr := ri; rr < ri+max(1, c.RowSpan); rr++ {
				for cc := col; cc < min(columns, col+max(1, c.ColSpan)); cc++ {
					covered[[2]int{rr, cc}] = true
				}
			}
			col += max(1, c.ColSpan)
		}
		grid = append(grid, cells)
	}
	b.WriteString("| " + strings.Join(grid[0], " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", columns) + "\n")
	for _, row := range grid[1:] {
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	b.WriteByte('\n')
}

func (w *mdWriter) inlines(inlines []Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		switch v := in.(type) {
		case *Text:
			b.WriteString(mdRun(v.Text, v.Format))
		case *LineBreak:
			b.WriteString("\\\n")
		case *Tab:
			b.WriteString("    ")
		case *Link:
			content := w.inlines(v.Content)
			if strings.HasPrefix(v.Target, "#") || strings.TrimSpace(content) == "" {
				b.WriteString(content)
			} else {
				fmt.Fprintf(&b, "[%s](<%s>)", content, v.Target)
			}
		case *Image:
			if target := w.imageTarget(v.Path); target != "" {
				fmt.Fprintf(&b, "![](<%s>)", target)
			}
		}
	}
	return b.String()
}

func mdRun(text string, f CharFormat) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	// Emphasis markers must touch the text: spaces stay outside.
	core := mdEscape(strings.TrimSpace(text))
	lead := text[:len(text)-len(strings.TrimLeftFunc(text, unicode.IsSpace))]
	trail := text[len(strings.TrimRightFunc(text, unicode.IsSpace)):]
	if f.Italic {
		core = "*" + core + "*"
	}
	if f.Bold {
		core = "**" + core + "**"
	}
	return lead + core + trail
}

func mdEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>~^|=", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeLineStart escapes text that would read as a heading, list item or quote.
func escapeLineStart(text string) string {
	t := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "+ ") {
		return "\\" + t
	}
	digits := 0
	for digits < len(t) && t[digits] >= '0' && t[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(t) && (t[digits] == '.' || t[digits] == ')') {
		return t[:digits] + "\\" + t[digits:]
	}
	return text
}

// markdownPictures puts the pictures of a Markdown file into a folder next to
// it ("report.md" → "report_files/img0001.jpg") and links them relatively.
type markdownPictures struct {
	markdown string
	allocate func(ext, suffix string) string
	links    map[string]string
	folder   string
	skipped  bool
}

func (m *markdownPictures) add(path string) string {
	if l, ok := m.links[path]; ok {
		return l
	}
	folder := m.ensureFolder()
	if folder == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	name := fmt.Sprintf("img%04d%s", len(m.links)+1, strings.ToLower(filepath.Ext(path)))
	if err := os.WriteFile(filepath.Join(folder, name), data, 0o644); err != nil {
		return ""
	}
	link := url.PathEscape(filepath.Base(folder)) + "/" + name
	m.links[path] = link
	return link
}

func (m *markdownPictures) ensureFolder() string {
	if m.folder != "" || m.skipped {
		return m.folder
	}
	beside := filepath.Dir(m.markdown)
	folder := m.allocate("", "_files")
	if folder != "" && !samePath(filepath.Dir(folder), beside) {
		folder = filepath.Join(beside, strings.TrimSuffix(filepath.Base(m.markdown), filepath.Ext(m.markdown))+"_files")
	}
	if folder == "" || os.MkdirAll(folder, 0o755) != nil {
		m.skipped = true
		return ""
	}
	m.folder = folder
	return folder
}
