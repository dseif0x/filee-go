package hwpx

// Document model → clean XHTML with one small style sheet: the chapters of EPUB files, the page of HTMLZ, and
// single-page HTML exports. Structure is kept (headings, paragraphs with alignment and indents, lists, tables with
// spans, pictures, links and anchors, footnotes); page setup, headers and footers are left to the reading system.
//
// Chapters are split at level-1 headings and page breaks (and when a chapter grows very long), and links to
// bookmarks in other chapters point at the right file.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/doc"
)

// XhtmlOptions say how XhtmlWrite lays out its output.
type XhtmlOptions struct {
	// SplitChapters splits into chapters at level-1 headings and page breaks (EPUB); false writes one page.
	SplitChapters bool
	// ChapterFile is the file name of chapter n (0-based), used for links between chapters; nil = ch001.xhtml ...
	ChapterFile func(int) string
	// ImageSource is the src of a picture file (a path in the package or a data: URI), or "" to leave it out.
	ImageSource func(string) string
	// Epub marks footnotes and references with epub:type (EPUB 3 pop-up notes).
	Epub bool
}

// XhtmlChapter is one output file. Title is the text of its first heading; Body the markup inside <body>; PlainStart
// the first words (a table of contents entry for chapters without headings); Headings for a table of contents.
type XhtmlChapter struct {
	FileName   string
	Title      string
	Body       string
	PlainStart string
	Headings   []XhtmlHeading
}

// XhtmlHeading is a heading with its link target ("ch002.xhtml#_h7").
type XhtmlHeading struct {
	Level  int
	Text   string
	Target string
}

// XhtmlCSS is the style sheet of the pages (EPUB style.css, inlined in single-page HTML).
const XhtmlCSS = "body{font-family:serif;line-height:1.6;margin:0 3%;}\n" +
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
	options         XhtmlOptions
	bookmarkChapter map[string]int
	ids             map[string]string
	usedIDs         map[string]bool
	bodySize        int
	chapter         int
	headingCount    int
	noteCount       int
	headings        []XhtmlHeading
	notes           strings.Builder
}

// XhtmlWrite converts a document into one page or a list of chapters.
func XhtmlWrite(d *doc.Document, options XhtmlOptions) []XhtmlChapter {
	if options.ChapterFile == nil {
		options.ChapterFile = func(i int) string { return fmt.Sprintf("ch%03d.xhtml", i+1) }
	}
	if options.ImageSource == nil {
		options.ImageSource = func(string) string { return "" }
	}
	chapters := splitChapters(d, options.SplitChapters)
	w := &xhtmlWriter{options: options, bookmarkChapter: map[string]int{}, ids: map[string]string{}, usedIDs: map[string]bool{}, bodySize: bodySize(d)}
	for i, c := range chapters {
		for _, in := range doc.Inlines(c) {
			if b, ok := in.(*doc.Bookmark); ok {
				if _, seen := w.bookmarkChapter[b.Name]; !seen {
					w.bookmarkChapter[b.Name] = i
				}
			}
		}
	}
	var result []XhtmlChapter
	for i, c := range chapters {
		w.chapter = i
		w.headings = nil
		w.notes.Reset()
		var body strings.Builder
		w.blocks(c, &body)
		if w.notes.Len() > 0 {
			if options.Epub {
				body.WriteString(`<section class="footnotes" epub:type="footnotes">`)
			} else {
				body.WriteString(`<section class="footnotes"><hr/>`)
			}
			body.WriteString(w.notes.String() + "</section>\n")
		}
		inlines := doc.Inlines(c)
		if len(inlines) > 200 {
			inlines = inlines[:200]
		}
		plain := doc.PlainText(inlines)
		if utf8.RuneCountInString(plain) > 60 {
			plain = string([]rune(plain)[:60])
		}
		title := ""
		if len(w.headings) > 0 {
			title = w.headings[0].Text
		}
		result = append(result, XhtmlChapter{options.ChapterFile(i), title, body.String(), plain, w.headings})
	}
	return result
}

// XhtmlPage is a complete page around a body. styleSheet is a linked style sheet (EPUB, HTMLZ); "" embeds XhtmlCSS.
// epub makes an EPUB content document (XML declaration and the epub namespace, else plain HTML5); head is extra
// markup for <head>.
func XhtmlPage(title, body, language, styleSheet string, epub bool, head string) string {
	var sb strings.Builder
	if epub {
		sb.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	}
	sb.WriteString("<!DOCTYPE html>\n")
	sb.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml"`)
	if epub {
		sb.WriteString(` xmlns:epub="http://www.idpf.org/2007/ops"`)
	}
	fmt.Fprintf(&sb, " lang=\"%s\" xml:lang=\"%s\">\n<head>\n<meta charset=\"utf-8\"/>\n", Escape(language), Escape(language))
	fmt.Fprintf(&sb, "<title>%s</title>\n", Escape(title))
	sb.WriteString(head)
	if styleSheet == "" {
		sb.WriteString("<style>\n" + XhtmlCSS + "</style>\n")
	} else {
		sb.WriteString(`<link rel="stylesheet" type="text/css" href="` + Escape(styleSheet) + "\"/>\n")
	}
	sb.WriteString("</head>\n<body>\n" + body + "</body>\n</html>\n")
	return sb.String()
}

// ───────────────────────── Chapters ─────────────────────────

// splitChapters puts blocks per chapter: a new one at a section start, a page break or a level-1 heading.
func splitChapters(d *doc.Document, split bool) [][]doc.Block {
	chapters := [][]doc.Block{nil}
	length := 0
	for _, s := range d.Sections {
		sectionStart := true
		for _, b := range s.Blocks {
			current := chapters[len(chapters)-1]
			if split && hasXhtmlContent(current) {
				p, isP := b.(*doc.Paragraph)
				cut := sectionStart || isP && (p.PageBreakBefore || p.HeadingLevel == 1) || length > chapterLimit && isP && p.List == nil && p.HeadingLevel == 0
				if cut {
					chapters = append(chapters, nil)
					length = 0
				}
			}
			sectionStart = false
			chapters[len(chapters)-1] = append(chapters[len(chapters)-1], b)
			for _, in := range doc.Inlines([]doc.Block{b}) {
				if t, ok := in.(*doc.Text); ok {
					length += len(t.Text)
				}
			}
		}
	}
	return chapters
}

func hasXhtmlContent(blocks []doc.Block) bool {
	for _, b := range blocks {
		if _, ok := b.(*doc.Table); ok {
			return true
		}
		for _, in := range doc.Inlines([]doc.Block{b}) {
			switch v := in.(type) {
			case *doc.Image:
				return true
			case *doc.Text:
				if strings.TrimSpace(v.Text) != "" {
					return true
				}
			}
		}
	}
	return false
}

// bodySize is the most common text size, written as 1em (other sizes become relative to it).
func bodySize(d *doc.Document) int {
	counts := map[int]int{}
	var order []int
	for _, s := range d.Sections {
		for _, in := range doc.Inlines(s.Blocks) {
			if t, ok := in.(*doc.Text); ok && t.Format.Size != nil {
				if _, seen := counts[*t.Format.Size]; !seen {
					order = append(order, *t.Format.Size)
				}
				counts[*t.Format.Size] += utf16Len(t.Text)
			}
		}
	}
	best, bestCount := 0, -1
	for _, s := range order {
		if counts[s] > bestCount {
			best, bestCount = s, counts[s]
		}
	}
	if best > 0 {
		return best
	}
	return 1000
}

// utf16Len is the length of text in UTF-16 code units, as .NET counts string lengths.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// ───────────────────────── Blocks ─────────────────────────

func (w *xhtmlWriter) blocks(blocks []doc.Block, sb *strings.Builder) {
	for i := 0; i < len(blocks); {
		if p, ok := blocks[i].(*doc.Paragraph); ok && p.List != nil {
			i = w.list(blocks, i, sb)
		} else if isCodeLine(blocks[i]) {
			i = xhtmlCode(blocks, i, sb)
		} else {
			w.block(blocks[i], sb)
			i++
		}
	}
}

func (w *xhtmlWriter) block(b doc.Block, sb *strings.Builder) {
	switch v := b.(type) {
	case *doc.Paragraph:
		w.paragraph(v, sb)
	case *doc.Table:
		w.table(v, sb)
	}
}

func (w *xhtmlWriter) paragraph(p *doc.Paragraph, sb *strings.Builder) {
	breakClass := ""
	if p.PageBreakBefore && !w.options.SplitChapters {
		breakClass = ` class="page-break"`
	}
	hasLine := false
	for _, in := range p.Inlines {
		if s, ok := in.(*doc.Shape); ok && s.Kind == doc.ShapeLine {
			hasLine = true
		}
	}
	if hasLine && !hasVisible(p.Inlines) {
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
		level := clamp(p.HeadingLevel, 1, 6)
		w.headingCount++
		id := fmt.Sprintf("_h%d", w.headingCount)
		w.usedIDs[id] = true
		w.headings = append(w.headings, XhtmlHeading{p.HeadingLevel, doc.PlainText(p.Inlines), w.options.ChapterFile(w.chapter) + "#" + id})
		fmt.Fprintf(sb, `<h%d id="%s"%s%s>%s</h%d>`+"\n", level, id, breakClass, style, content, level)
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

// formatNumber formats like .NET's "0.##".
func formatNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func em(hwpUnits int) string { return formatNumber(float64(hwpUnits)/1000.0) + "em" }

// isCodeLine: a paragraph that is all code (shaded like the readers mark code blocks).
func isCodeLine(b doc.Block) bool {
	p, ok := b.(*doc.Paragraph)
	if !ok || p.List != nil || p.HeadingLevel != 0 || len(p.Inlines) == 0 {
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

func codeText(p *doc.Paragraph) string {
	var sb strings.Builder
	for _, in := range p.Inlines {
		if t, ok := in.(*doc.Text); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

func xhtmlCode(blocks []doc.Block, start int, sb *strings.Builder) int {
	var lines []string
	i := start
	for i < len(blocks) && isCodeLine(blocks[i]) {
		lines = append(lines, codeText(blocks[i].(*doc.Paragraph)))
		i++
	}
	sb.WriteString("<pre><code>" + Escape(strings.Join(lines, "\n")) + "</code></pre>\n")
	return i
}

type openList struct {
	numbering *doc.Numbering
	level     int
	ordered   bool
}

// list writes consecutive list paragraphs as nested ul / ol; it returns the index after the list.
func (w *xhtmlWriter) list(blocks []doc.Block, start int, sb *strings.Builder) int {
	var stack []openList
	i := start
	for ; i < len(blocks); i++ {
		p, ok := blocks[i].(*doc.Paragraph)
		if !ok || p.List == nil {
			break
		}
		item := p.List
		for len(stack) > 0 && stack[len(stack)-1].level > item.Level {
			stack = closeList(stack, sb)
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
			stack = closeList(stack, sb)
		}
		if len(stack) > 0 && stack[len(stack)-1].level == item.Level {
			sb.WriteString("</li>\n")
		} else {
			var level *doc.NumberingLevel
			if n := len(item.Numbering.Levels); n > 0 {
				level = &item.Numbering.Levels[min(item.Level, n-1)]
			}
			ordered := level != nil && !level.Bullet
			stack = append(stack, openList{item.Numbering, item.Level, ordered})
			if !ordered {
				sb.WriteString("<ul>\n")
			} else {
				kind := ""
				switch level.Format {
				case "LATIN_SMALL":
					kind = ` type="a"`
				case "LATIN_CAPITAL":
					kind = ` type="A"`
				case "ROMAN_SMALL":
					kind = ` type="i"`
				case "ROMAN_CAPITAL":
					kind = ` type="I"`
				}
				startAttr := ""
				if level.Start != 1 {
					startAttr = fmt.Sprintf(` start="%d"`, level.Start)
				}
				sb.WriteString("<ol" + kind + startAttr + ">\n")
			}
		}
		f := p.Format
		f.Left, f.FirstLine = nil, nil
		sb.WriteString("<li" + paragraphStyle(f) + ">" + w.inlines(p.Inlines, false))
	}
	for len(stack) > 0 {
		stack = closeList(stack, sb)
	}
	return i
}

func closeList(stack []openList, sb *strings.Builder) []openList {
	top := stack[len(stack)-1]
	sb.WriteString("</li>\n")
	if top.ordered {
		sb.WriteString("</ol>\n")
	} else {
		sb.WriteString("</ul>\n")
	}
	return stack[:len(stack)-1]
}

func (w *xhtmlWriter) table(t *doc.Table, sb *strings.Builder) {
	if t.Borders != nil && *t.Borders == doc.EmptyBorders {
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
	for _, r := range t.Rows {
		if !r.Header {
			break
		}
		header++
	}
	split := header > 0 && header < len(t.Rows)
	if split {
		sb.WriteString("<thead>\n")
	}
	for ri, row := range t.Rows {
		if ri == header && split {
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
			if p, ok := onlyPlainParagraph(cell.Blocks); ok {
				sb.WriteString(w.inlines(p.Inlines, false)) // a single paragraph needs no <p>
			} else {
				w.blocks(cell.Blocks, sb)
			}
			sb.WriteString("</" + tag + ">")
		}
		sb.WriteString("</tr>\n")
	}
	if split {
		sb.WriteString("</tbody>\n")
	}
	sb.WriteString("</table>\n")
}

func onlyPlainParagraph(blocks []doc.Block) (*doc.Paragraph, bool) {
	if len(blocks) != 1 {
		return nil, false
	}
	p, ok := blocks[0].(*doc.Paragraph)
	return p, ok && p.List == nil && p.HeadingLevel == 0
}

// ───────────────────────── Inlines ─────────────────────────

func hasVisible(inlines []doc.Inline) bool {
	for _, in := range doc.InlinesOf(inlines) {
		switch v := in.(type) {
		case *doc.Text:
			if strings.TrimSpace(v.Text) != "" || strings.Contains(v.Text, "\u00A0") {
				return true
			}
		case *doc.Equation:
			if strings.TrimSpace(v.Text.Text) != "" {
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
		switch v := in.(type) {
		case *doc.Text:
			w.run(v.Text, v.Format, heading, &sb)
		case *doc.Equation:
			w.run(v.Text.Text, v.Text.Format, heading, &sb)
		case *doc.LineBreak:
			sb.WriteString("<br/>")
		case *doc.Tab:
			sb.WriteString("\u2003")
		case *doc.Bookmark:
			if id := w.id(v.Name); id != "" {
				sb.WriteString(`<a id="` + id + `"></a>`)
			}
		case *doc.Link:
			content := w.inlines(v.Content, heading)
			if href := w.href(v.Target); href != "" {
				sb.WriteString(`<a href="` + Escape(href) + `">` + content + "</a>")
			} else {
				sb.WriteString(content)
			}
		case *doc.Image:
			w.image(v, &sb)
		case *doc.Note:
			w.note(v, &sb)
		case *doc.TextBox:
			var parts []string
			for _, b := range v.Blocks {
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
	var close []string
	open := func(tag, attributes string) {
		sb.WriteString("<" + tag + attributes + ">")
		close = append(close, "</"+tag+">")
	}
	var styles []string
	if f.Color != nil && *f.Color != "#000000" {
		styles = append(styles, "color:"+*f.Color)
	}
	if f.Shade != nil && *f.Shade != CodeShade && *f.Shade != MarkShade {
		styles = append(styles, "background-color:"+*f.Shade)
	}
	if f.Size != nil && abs(*f.Size-w.bodySize) > w.bodySize/20 && !isTrue(f.Superscript) && !isTrue(f.Subscript) {
		styles = append(styles, "font-size:"+formatNumber(float64(*f.Size)/float64(w.bodySize))+"em")
	}
	if heading && isFalse(f.Bold) {
		styles = append(styles, "font-weight:normal")
	}
	if len(styles) > 0 {
		open("span", ` style="`+strings.Join(styles, ";")+`"`)
	}
	if strIs(f.Shade, CodeShade) {
		open("code", "")
	}
	if strIs(f.Shade, MarkShade) {
		open("mark", "")
	}
	if isTrue(f.Bold) && !heading {
		open("b", "")
	}
	if isTrue(f.Italic) {
		open("i", "")
	}
	if isTrue(f.Underline) {
		open("u", "")
	}
	if isTrue(f.Strike) {
		open("s", "")
	}
	if isTrue(f.Superscript) {
		open("sup", "")
	} else if isTrue(f.Subscript) {
		open("sub", "")
	}
	sb.WriteString(Escape(text))
	for i := len(close) - 1; i >= 0; i-- {
		sb.WriteString(close[i])
	}
}

// href is the href of a link: bookmarks point into the right chapter; only absolute URIs leave the book.
func (w *xhtmlWriter) href(target string) string {
	if strings.HasPrefix(target, "#") {
		name := target[1:]
		chapter, ok := w.bookmarkChapter[name]
		id := w.id(name)
		if !ok || id == "" {
			return "" // no such bookmark: keep the text only
		}
		if !w.options.SplitChapters || chapter == w.chapter {
			return "#" + id
		}
		return w.options.ChapterFile(chapter) + "#" + id
	}
	colon := strings.Index(target, ":")
	if colon > 1 {
		for _, c := range target[:colon] {
			if !isASCIILetter(c) {
				return ""
			}
		}
		return target
	}
	return ""
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
	for _, ch := range name {
		if isASCIILetter(ch) || isASCIIDigit(ch) || ch == '-' || ch == '_' || ch == '.' {
			sb.WriteRune(ch)
		} else {
			sb.WriteByte('_')
		}
	}
	base := sb.String()
	if first := rune(base[0]); !isASCIILetter(first) && first != '_' {
		base = "id-" + base
	}
	id := base
	for n := 2; w.usedIDs[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	w.usedIDs[id] = true
	w.ids[name] = id
	return id
}

func (w *xhtmlWriter) image(img *doc.Image, sb *strings.Builder) {
	source := w.options.ImageSource(img.Path)
	if source == "" {
		return
	}
	sb.WriteString(`<img src="` + Escape(source) + `" alt=""`)
	if img.Width != nil && *img.Width > 0 {
		fmt.Fprintf(sb, ` style="width:%dpx"`, max(1, iround(float64(*img.Width)/doc.PerPixel)))
	}
	sb.WriteString("/>")
}

func (w *xhtmlWriter) note(note *doc.Note, sb *strings.Builder) {
	w.noteCount++
	number := w.noteCount
	epub, kind := "", ""
	if w.options.Epub {
		epub, kind = ` epub:type="noteref"`, ` epub:type="footnote"`
	}
	fmt.Fprintf(sb, `<a class="noteref" href="#_fn%d" id="_fnref%d"%s><sup>%d</sup></a>`, number, number, epub, number)
	var content strings.Builder
	w.blocks(note.Blocks, &content)
	fmt.Fprintf(&w.notes, `<aside class="footnote" id="_fn%d"%s><p><a href="#_fnref%d">%d.</a></p>`+"\n", number, kind, number, number)
	w.notes.WriteString(content.String() + "</aside>\n")
}
