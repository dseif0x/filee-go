package htmldoc

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// The document model as clean XHTML with one small style sheet: the
// chapters of EPUB files, the page of HTMLZ, and single-page HTML exports.
// Structure is kept (headings, paragraphs with alignment and indents, lists,
// tables with spans, pictures, links and anchors, footnotes); page setup,
// headers and footers are left to the reading system. Chapters are split at
// level-1 headings and page breaks (and when one grows very long), and
// links to bookmarks in other chapters point at the right file.

// XhtmlOptions lay out the output.
type XhtmlOptions struct {
	// SplitChapters at level-1 headings and page breaks (EPUB); false writes one page.
	SplitChapters bool
	// ChapterFile names chapter n (0-based); default "ch001.xhtml".
	ChapterFile func(int) string
	// ImageSource is the src of a picture file (a path in the package or a data: URI), "" to leave it out.
	ImageSource func(string) string
	// Epub marks footnotes and references with epub:type (EPUB 3 pop-up notes).
	Epub bool
}

// Chapter is one output file.
type Chapter struct {
	FileName string
	Title    string // text of the first heading
	Body     string // markup inside <body>
	// PlainStart is the first words, a table of contents entry for chapters without headings.
	PlainStart string
	Headings   []Heading
}

// Heading is a table of contents entry with its link target ("ch002.xhtml#_h7").
type Heading struct {
	Level  int
	Text   string
	Target string
}

// Css is the style sheet of the pages (EPUB style.css, inlined in single-page HTML).
const Css = "body{font-family:serif;line-height:1.6;margin:0 3%;}\n" +
	"h1,h2,h3,h4,h5,h6{line-height:1.3;margin:1.2em 0 .6em;page-break-after:avoid;}\n" +
	"p{margin:0 0 .6em;}\n" +
	"li p{margin:.2em 0;}\n" +
	"table{border-collapse:collapse;margin:.8em 0;}\n" +
	"td,th{border:1px solid #999;padding:.2em .5em;vertical-align:top;}\n" +
	"table.plain td,table.plain th{border:none;}\n" +
	"caption{font-style:italic;margin-bottom:.3em;}\n" +
	"img{max-width:100%;}\n" +
	"pre{white-space:pre-wrap;background:#f4f3f8;padding:.5em .8em;}\n" +
	"code,pre{font-family:monospace;}\n" +
	"code{background:#f1f0f5;}\n" +
	"mark{background:#fff3a3;}\n" +
	"hr{border:none;border-top:1px solid #999;width:30%;margin:1.2em auto;}\n" +
	".page-break{page-break-before:always;break-before:page;}\n" +
	".box{border:1px solid #999;padding:.3em .6em;display:inline-block;}\n" +
	"aside.footnote,.footnotes{font-size:.9em;}\n"

// chapterLimit: chapters are cut at the next plain paragraph once they have this many characters.
const chapterLimit = 150_000

type xhtmlWriter struct {
	opts            XhtmlOptions
	bookmarkChapter map[string]int
	ids             map[string]string
	usedIDs         map[string]bool
	bodySize        int
	chapter         int
	headingCount    int
	noteCount       int
	headings        []Heading
	notes           *strings.Builder
}

// Escape is XML-escaped text without characters XML 1.0 forbids.
func Escape(s string) string { return doc.EscapeXML(s) }

// WriteXhtml converts a document into one page or a list of chapters.
func WriteXhtml(d *doc.Document, opts XhtmlOptions) []Chapter {
	if opts.ChapterFile == nil {
		opts.ChapterFile = func(i int) string { return fmt.Sprintf("ch%03d.xhtml", i+1) }
	}
	if opts.ImageSource == nil {
		opts.ImageSource = func(string) string { return "" }
	}
	chapters := splitChapters(d, opts.SplitChapters)
	w := &xhtmlWriter{opts: opts, bookmarkChapter: map[string]int{}, ids: map[string]string{}, usedIDs: map[string]bool{}, bodySize: bodySize(d)}
	for i, ch := range chapters {
		for _, in := range doc.Inlines(ch) {
			if b, ok := in.(*doc.Bookmark); ok {
				if _, seen := w.bookmarkChapter[b.Name]; !seen {
					w.bookmarkChapter[b.Name] = i
				}
			}
		}
	}
	var out []Chapter
	for i, ch := range chapters {
		w.chapter = i
		w.headings = nil
		w.notes = &strings.Builder{}
		var body strings.Builder
		w.blocks(ch, &body)
		if w.notes.Len() > 0 {
			if opts.Epub {
				body.WriteString(`<section class="footnotes" epub:type="footnotes">`)
			} else {
				body.WriteString(`<section class="footnotes"><hr/>`)
			}
			body.WriteString(w.notes.String() + "</section>\n")
		}
		inl := doc.Inlines(ch)
		plain := []rune(Text(inl[:min(200, len(inl))]))
		if len(plain) > 60 {
			plain = plain[:60]
		}
		title := ""
		if len(w.headings) > 0 {
			title = w.headings[0].Text
		}
		out = append(out, Chapter{opts.ChapterFile(i), title, body.String(), string(plain), w.headings})
	}
	return out
}

// Page is a complete page around a body. styleSheet "" embeds Css; epub
// writes an EPUB content document (XML declaration, epub namespace); head is
// extra markup (a viewport for fixed-layout pages).
func Page(title, body, language, styleSheet string, epub bool, head string) string {
	var sb strings.Builder
	if epub {
		sb.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	}
	sb.WriteString("<!DOCTYPE html>\n<html xmlns=\"http://www.w3.org/1999/xhtml\"")
	if epub {
		sb.WriteString(` xmlns:epub="http://www.idpf.org/2007/ops"`)
	}
	sb.WriteString(` lang="` + Escape(language) + `" xml:lang="` + Escape(language) + "\">\n<head>\n<meta charset=\"utf-8\"/>\n")
	sb.WriteString("<title>" + Escape(title) + "</title>\n")
	sb.WriteString(head)
	if styleSheet == "" {
		sb.WriteString("<style>\n" + Css + "</style>\n")
	} else {
		sb.WriteString(`<link rel="stylesheet" type="text/css" href="` + Escape(styleSheet) + "\"/>\n")
	}
	sb.WriteString("</head>\n<body>\n" + body + "</body>\n</html>\n")
	return sb.String()
}

// splitChapters: a new chapter at a section start, a page break or a level-1 heading.
func splitChapters(d *doc.Document, split bool) [][]doc.Block {
	chapters := [][]doc.Block{nil}
	length := 0
	for _, s := range d.Sections {
		sectionStart := true
		for _, b := range s.Blocks {
			current := chapters[len(chapters)-1]
			if split && blocksHaveContent(current) {
				p, _ := b.(*doc.Paragraph)
				cut := sectionStart || p != nil && (p.PageBreakBefore || p.HeadingLevel == 1) ||
					length > chapterLimit && p != nil && p.List == nil && p.HeadingLevel == 0
				if cut {
					chapters = append(chapters, nil)
					length = 0
				}
			}
			sectionStart = false
			chapters[len(chapters)-1] = append(chapters[len(chapters)-1], b)
			for _, in := range doc.Inlines([]doc.Block{b}) {
				if t, ok := in.(*doc.Text); ok {
					length += len([]rune(t.Text))
				}
			}
		}
	}
	return chapters
}

func blocksHaveContent(blocks []doc.Block) bool {
	for _, b := range blocks {
		if _, ok := b.(*doc.Table); ok {
			return true
		}
		for _, in := range doc.Inlines([]doc.Block{b}) {
			switch in := in.(type) {
			case *doc.Image:
				return true
			case *doc.Text:
				if strings.TrimSpace(in.Text) != "" {
					return true
				}
			}
		}
	}
	return false
}

// bodySize is the most common text size, written as 1em (other sizes become relative to it).
func bodySize(d *doc.Document) int {
	chars := map[int]int{}
	for _, s := range d.Sections {
		for _, in := range doc.Inlines(s.Blocks) {
			if t, ok := in.(*doc.Text); ok && t.Format.Size != nil {
				chars[*t.Format.Size] += len([]rune(t.Text))
			}
		}
	}
	best, most := 0, -1
	sizes := make([]int, 0, len(chars))
	for s := range chars {
		sizes = append(sizes, s)
	}
	sort.Ints(sizes)
	for _, s := range sizes {
		if chars[s] > most {
			best, most = s, chars[s]
		}
	}
	if best > 0 {
		return best
	}
	return 1000
}

func (w *xhtmlWriter) blocks(blocks []doc.Block, sb *strings.Builder) {
	for i := 0; i < len(blocks); {
		if p, ok := blocks[i].(*doc.Paragraph); ok && p.List != nil {
			i = w.list(blocks, i, sb)
			continue
		}
		if p, ok := blocks[i].(*doc.Paragraph); ok && isCodeParagraph(p, true) {
			var lines []string
			for ; i < len(blocks); i++ {
				q, ok := blocks[i].(*doc.Paragraph)
				if !ok || !isCodeParagraph(q, true) {
					break
				}
				lines = append(lines, codeText(q))
			}
			sb.WriteString("<pre><code>" + Escape(strings.Join(lines, "\n")) + "</code></pre>\n")
			continue
		}
		switch b := blocks[i].(type) {
		case *doc.Paragraph:
			w.paragraph(b, sb)
		case *doc.Table:
			w.table(b, sb)
		}
		i++
	}
}

func (w *xhtmlWriter) paragraph(p *doc.Paragraph, sb *strings.Builder) {
	breakClass := ""
	if p.PageBreakBefore && !w.opts.SplitChapters {
		breakClass = ` class="page-break"`
	}
	if hasLine(p.Inlines) && !hasVisible(p.Inlines) {
		sb.WriteString(w.anchors(p.Inlines) + "<hr/>\n")
		return
	}
	content := w.inlines(p.Inlines, p.HeadingLevel > 0)
	if !hasVisible(p.Inlines) {
		// Empty paragraphs are spacing in word processors; reading systems space paragraphs themselves.
		if content != "" {
			sb.WriteString("<div" + breakClass + ">" + content + "</div>\n")
		}
		return
	}
	style := paragraphStyle(p.Format)
	if p.HeadingLevel > 0 {
		level := max(1, min(6, p.HeadingLevel))
		w.headingCount++
		id := "_h" + strconv.Itoa(w.headingCount)
		w.usedIDs[id] = true
		w.headings = append(w.headings, Heading{p.HeadingLevel, Text(p.Inlines), w.opts.ChapterFile(w.chapter) + "#" + id})
		fmt.Fprintf(sb, "<h%d id=\"%s\"%s%s>%s</h%d>\n", level, id, breakClass, style, content, level)
		return
	}
	sb.WriteString("<p" + breakClass + style + ">" + content + "</p>\n")
}

func paragraphStyle(f doc.ParaFormat) string {
	var styles []string
	if f.Align != nil {
		switch *f.Align {
		case doc.AlignCenter:
			styles = append(styles, "text-align:center")
		case doc.AlignRight:
			styles = append(styles, "text-align:right")
		case doc.AlignJustify, doc.AlignDistribute:
			styles = append(styles, "text-align:justify")
		}
	}
	// HWPUNIT → em of a 10 pt body text (1000 HWPUNIT).
	if f.Left != nil && *f.Left > 0 {
		styles = append(styles, "margin-left:"+em(*f.Left))
	}
	if f.FirstLine != nil && *f.FirstLine != 0 {
		styles = append(styles, "text-indent:"+em(*f.FirstLine))
	}
	if len(styles) == 0 {
		return ""
	}
	return ` style="` + strings.Join(styles, ";") + `"`
}

func em(hwp int) string { return number2(float64(hwp)/1000) + "em" }

// number2 is "0.##": at most two decimals, no trailing zeros.
func number2(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" || s == "-0" {
		return "0"
	}
	return s
}

type openList struct {
	numbering *doc.Numbering
	level     int
	ordered   bool
}

// list writes consecutive list paragraphs as nested ul / ol; returns the index after the list.
func (w *xhtmlWriter) list(blocks []doc.Block, start int, sb *strings.Builder) int {
	var stack []openList
	closeList := func() {
		top := stack[len(stack)-1]
		sb.WriteString("</li>\n")
		if top.ordered {
			sb.WriteString("</ol>\n")
		} else {
			sb.WriteString("</ul>\n")
		}
		stack = stack[:len(stack)-1]
	}
	i := start
	for ; i < len(blocks); i++ {
		p, ok := blocks[i].(*doc.Paragraph)
		if !ok || p.List == nil {
			break
		}
		item := p.List
		for len(stack) > 0 && stack[len(stack)-1].level > item.Level {
			closeList()
		}
		if !item.Numbered {
			if len(stack) == 0 {
				w.paragraph(p, sb)
			} else {
				f := p.Format
				f.Left = nil
				sb.WriteString("<p" + paragraphStyle(f) + ">" + w.inlines(p.Inlines, false) + "</p>")
			}
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].level == item.Level && stack[len(stack)-1].numbering != item.Numbering {
			closeList()
		}
		if len(stack) > 0 && stack[len(stack)-1].level == item.Level {
			sb.WriteString("</li>\n")
		} else {
			var level *doc.NumberingLevel
			if item.Numbering != nil && len(item.Numbering.Levels) > 0 {
				level = &item.Numbering.Levels[min(item.Level, len(item.Numbering.Levels)-1)]
			}
			ordered := level != nil && !level.Bullet
			stack = append(stack, openList{item.Numbering, item.Level, ordered})
			if !ordered {
				sb.WriteString("<ul>\n")
			} else {
				typ := map[string]string{"LATIN_SMALL": ` type="a"`, "LATIN_CAPITAL": ` type="A"`, "ROMAN_SMALL": ` type="i"`, "ROMAN_CAPITAL": ` type="I"`}[level.Format]
				startAttr := ""
				if level.Start != 1 {
					startAttr = fmt.Sprintf(` start="%d"`, level.Start)
				}
				sb.WriteString("<ol" + typ + startAttr + ">\n")
			}
		}
		f := p.Format
		f.Left, f.FirstLine = nil, nil
		sb.WriteString("<li" + paragraphStyle(f) + ">" + w.inlines(p.Inlines, false))
	}
	for len(stack) > 0 {
		closeList()
	}
	return i
}

func isEmptyBorders(b *doc.Borders) bool { return b != nil && *b == doc.EmptyBorders }

func (w *xhtmlWriter) table(t *doc.Table, sb *strings.Builder) {
	if isEmptyBorders(t.Borders) {
		sb.WriteString(`<table class="plain"`)
	} else {
		sb.WriteString("<table")
	}
	if t.Indent > 0 {
		sb.WriteString(` style="margin-left:` + em(t.Indent) + `"`)
	}
	sb.WriteString(">\n")
	if len(t.Caption) > 0 {
		var parts []string
		for _, b := range t.Caption {
			if p, ok := b.(*doc.Paragraph); ok {
				parts = append(parts, w.inlines(p.Inlines, false))
			}
		}
		sb.WriteString("<caption>" + strings.Join(parts, " ") + "</caption>\n")
	}
	header := 0
	for header < len(t.Rows) && t.Rows[header].Header {
		header++
	}
	sections := header > 0 && header < len(t.Rows)
	if sections {
		sb.WriteString("<thead>\n")
	}
	for r, row := range t.Rows {
		if sections && r == header {
			sb.WriteString("</thead>\n<tbody>\n")
		}
		sb.WriteString("<tr>")
		for _, cell := range row.Cells {
			tag := "td"
			if row.Header {
				tag = "th"
			}
			sb.WriteString("<" + tag)
			if cell.RowSpan > 1 {
				fmt.Fprintf(sb, ` rowspan="%d"`, cell.RowSpan)
			}
			if cell.ColSpan > 1 {
				fmt.Fprintf(sb, ` colspan="%d"`, cell.ColSpan)
			}
			var styles []string
			if cell.Fill != "" {
				styles = append(styles, "background-color:"+cell.Fill)
			}
			switch cell.VerticalAlign {
			case doc.VAlignCenter:
				styles = append(styles, "vertical-align:middle")
			case doc.VAlignBottom:
				styles = append(styles, "vertical-align:bottom")
			}
			if len(styles) > 0 {
				sb.WriteString(` style="` + strings.Join(styles, ";") + `"`)
			}
			sb.WriteString(">")
			if len(cell.Blocks) == 1 {
				if p, ok := cell.Blocks[0].(*doc.Paragraph); ok && p.List == nil && p.HeadingLevel == 0 {
					sb.WriteString(w.inlines(p.Inlines, false)) // a single paragraph needs no <p>
					sb.WriteString("</" + tag + ">")
					continue
				}
			}
			w.blocks(cell.Blocks, sb)
			sb.WriteString("</" + tag + ">")
		}
		sb.WriteString("</tr>\n")
	}
	if sections {
		sb.WriteString("</tbody>\n")
	}
	sb.WriteString("</table>\n")
}

func hasVisible(inlines []doc.Inline) bool {
	for _, in := range doc.InlinesOf(inlines) {
		switch in := in.(type) {
		case *doc.Text:
			if strings.TrimSpace(in.Text) != "" || strings.Contains(in.Text, " ") {
				return true
			}
		case *doc.Image, *doc.Note, *doc.TextBox, *doc.Tab:
			return true
		}
	}
	return false
}

func (w *xhtmlWriter) anchors(inlines []doc.Inline) string {
	var sb strings.Builder
	for _, in := range inlines {
		if b, ok := in.(*doc.Bookmark); ok {
			if id := w.id(b.Name); id != "" {
				sb.WriteString(`<a id="` + id + `"></a>`)
			}
		}
	}
	return sb.String()
}

func (w *xhtmlWriter) inlines(inlines []doc.Inline, heading bool) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch in := in.(type) {
		case *doc.Text:
			w.run(in.Text, in.Format, heading, &sb)
		case *doc.Equation:
			w.run(in.Text.Text, in.Text.Format, heading, &sb)
		case *doc.LineBreak:
			sb.WriteString("<br/>")
		case *doc.Tab:
			sb.WriteString(" ")
		case *doc.Bookmark:
			if id := w.id(in.Name); id != "" {
				sb.WriteString(`<a id="` + id + `"></a>`)
			}
		case *doc.Link:
			content := w.inlines(in.Content, heading)
			if href := w.href(in.Target); href != "" {
				sb.WriteString(`<a href="` + Escape(href) + `">` + content + "</a>")
			} else {
				sb.WriteString(content)
			}
		case *doc.Image:
			if src := w.opts.ImageSource(in.Path); src != "" {
				sb.WriteString(`<img src="` + Escape(src) + `" alt=""`)
				if in.Width != nil && *in.Width > 0 {
					fmt.Fprintf(&sb, ` style="width:%dpx"`, max(1, int(math.Round(float64(*in.Width)/doc.PerPixel))))
				}
				sb.WriteString("/>")
			}
		case *doc.Note:
			w.note(in, &sb)
		case *doc.TextBox:
			var parts []string
			for _, b := range in.Blocks {
				if p, ok := b.(*doc.Paragraph); ok {
					if s := w.inlines(p.Inlines, false); s != "" {
						parts = append(parts, s)
					}
				}
			}
			if len(parts) > 0 {
				sb.WriteString(`<span class="box">` + strings.Join(parts, "<br/>") + "</span>")
			}
		}
	}
	return sb.String()
}

func (w *xhtmlWriter) run(text string, f doc.CharFormat, heading bool, sb *strings.Builder) {
	if text == "" {
		return
	}
	var closers []string
	open := func(tag, attrs string) {
		sb.WriteString("<" + tag + attrs + ">")
		closers = append(closers, "</"+tag+">")
	}
	var styles []string
	if c := doc.Val(f.Color, ""); c != "" && c != "#000000" {
		styles = append(styles, "color:"+c)
	}
	shade := doc.Val(f.Shade, "")
	if shade != "" && shade != CodeShade && shade != MarkShade {
		styles = append(styles, "background-color:"+shade)
	}
	sup, sub := doc.Val(f.Superscript, false), doc.Val(f.Subscript, false)
	if f.Size != nil && abs(*f.Size-w.bodySize) > w.bodySize/20 && !sup && !sub {
		styles = append(styles, "font-size:"+number2(float64(*f.Size)/float64(w.bodySize))+"em")
	}
	if heading && f.Bold != nil && !*f.Bold {
		styles = append(styles, "font-weight:normal")
	}
	if len(styles) > 0 {
		open("span", ` style="`+strings.Join(styles, ";")+`"`)
	}
	if shade == CodeShade {
		open("code", "")
	}
	if shade == MarkShade {
		open("mark", "")
	}
	if doc.Val(f.Bold, false) && !heading {
		open("b", "")
	}
	if doc.Val(f.Italic, false) {
		open("i", "")
	}
	if doc.Val(f.Underline, false) {
		open("u", "")
	}
	if doc.Val(f.Strike, false) {
		open("s", "")
	}
	if sup {
		open("sup", "")
	} else if sub {
		open("sub", "")
	}
	sb.WriteString(Escape(text))
	for i := len(closers) - 1; i >= 0; i-- {
		sb.WriteString(closers[i])
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// href is a link's target: bookmarks point into the right chapter; only absolute URIs leave the book.
func (w *xhtmlWriter) href(target string) string {
	if strings.HasPrefix(target, "#") {
		name := target[1:]
		chapter, ok := w.bookmarkChapter[name]
		id := w.id(name)
		if !ok || id == "" {
			return "" // no such bookmark: keep the text only
		}
		if !w.opts.SplitChapters || chapter == w.chapter {
			return "#" + id
		}
		return w.opts.ChapterFile(chapter) + "#" + id
	}
	if colon := strings.IndexByte(target, ':'); colon > 1 && isLetters(target[:colon]) {
		return target
	}
	return ""
}

func isLetters(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// id is a valid, unique XML id for a bookmark name (the same name always gets the same id).
func (w *xhtmlWriter) id(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	if known, ok := w.ids[name]; ok {
		return known
	}
	var sb strings.Builder
	for _, c := range name {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			sb.WriteRune(c)
		} else {
			sb.WriteByte('_')
		}
	}
	base := sb.String()
	if !(base[0] >= 'a' && base[0] <= 'z' || base[0] >= 'A' && base[0] <= 'Z' || base[0] == '_') {
		base = "id-" + base
	}
	id := base
	for n := 2; w.usedIDs[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	w.usedIDs[id] = true
	w.ids[name] = id
	return id
}

func (w *xhtmlWriter) note(n *doc.Note, sb *strings.Builder) {
	w.noteCount++
	number := w.noteCount
	epub := ""
	if w.opts.Epub {
		epub = ` epub:type="noteref"`
	}
	fmt.Fprintf(sb, `<a class="noteref" href="#_fn%d" id="_fnref%d"%s><sup>%d</sup></a>`, number, number, epub, number)
	var content strings.Builder
	w.blocks(n.Blocks, &content)
	typ := ""
	if w.opts.Epub {
		typ = ` epub:type="footnote"`
	}
	fmt.Fprintf(w.notes, "<aside class=\"footnote\" id=\"_fn%d\"%s><p><a href=\"#_fnref%d\">%d.</a></p>\n", number, typ, number, number)
	w.notes.WriteString(content.String() + "</aside>\n")
}
