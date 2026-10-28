package hwpx

// HTML / XHTML → document model with golang.org/x/net/html, so HTML → HWPX / PDF needs no Pandoc and e-books (EPUB,
// MOBI, HTMLZ) can reuse the same reader for their chapters.
//
// Headings, paragraphs, inline formatting (b / strong / i / em / u / s / del / sub / sup / code / mark / small / font
// and the basic inline CSS: colour, background, font-weight, font-style, font-size, text-decoration, vertical-align,
// text-align), simple class and tag rules from <style> and linked style sheets, line breaks, nested lists with their
// start numbers and marker types, tables with spans / header rows / borders, images (local files and data: URIs;
// remote images are skipped), links and anchors, block quotes, pre, hr, figures and CSS page breaks. Scripts, styles,
// forms, media and <nav> are skipped. The encoding comes from the BOM, the XML declaration or <meta charset>, else
// UTF-8 when valid, else the system's ANSI code page (what browsers fall back to).

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/dseif0x/filee-go/internal/doc"
)

func init() {
	doc.RegisterReader("html", ReadHTML)
}

// HTMLReadOptions say how an HTML document's resources, links and anchors are resolved.
type HTMLReadOptions struct {
	// BaseFolder is what relative image and style sheet paths are resolved against (the HTML file's folder).
	BaseFolder string
	// MediaFolder receives images embedded as data: URIs (created on demand).
	MediaFolder string
	// ResourceRoot, when set, is the folder local images and style sheets must be inside (e.g. an unpacked e-book).
	ResourceRoot string
	// MapLink maps an href to the link target to write, or ok=false to keep only the link text. Nil: unchanged.
	MapLink func(href string) (string, bool)
	// MapID maps an element id to a bookmark name, or ok=false for no bookmark. Nil: the id itself.
	MapID func(id string) (string, bool)
}

// HTMLContent is what was read from one HTML document.
type HTMLContent struct {
	Blocks   []doc.Block
	Title    string
	Language string
}

const (
	htmlMaxDepth = 200
	htmlBaseSize = 1000 // 1em when no size is set: 10 pt, the body text of the HWPX template
)

var htmlSkipped = setOf("head", "title", "meta", "link", "script", "style", "noscript", "template", "nav", "button", "select",
	"option", "textarea", "input", "iframe", "object", "embed", "video", "audio", "canvas", "map", "param",
	"source", "track", "dialog", "rp", "base")

var htmlBlockElements = setOf("p", "div", "section", "article", "main", "header", "footer", "aside", "address", "center", "details",
	"summary", "fieldset", "legend", "form", "hgroup", "body", "html", "dl", "dt", "dd", "menu", "caption",
	"figure", "figcaption", "blockquote", "pre", "li", "listing", "plaintext", "xmp")

type htmlReader struct {
	options  HTMLReadOptions
	sheet    CSSStyleSheet
	embedded int
}

// ReadHTML reads an HTML file as a document of its own; mediaFolder receives images embedded as data: URIs.
func ReadHTML(p, mediaFolder string) (*doc.Document, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	content, err := ReadHTMLFile(p, HTMLReadOptions{BaseFolder: filepath.Dir(abs), MediaFolder: mediaFolder})
	if err != nil {
		return nil, err
	}
	return HTMLDocument(content), nil
}

// HTMLDocument is one section with what was read, titled like the page.
func HTMLDocument(content *HTMLContent) *doc.Document {
	s := doc.NewSection()
	s.Blocks = content.Blocks
	d := &doc.Document{Title: content.Title, Sections: []*doc.Section{s}}
	PruneBookmarks(d)
	return d
}

// ReadHTMLFile reads an HTML file (e.g. an e-book chapter) with the given resolution rules.
func ReadHTMLFile(p string, options HTMLReadOptions) (*HTMLContent, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(p))
	return ParseHTML(DecodeHTML(data), options, ext == ".xhtml" || ext == ".xht" || ext == ".xml" || ext == ".opf"), nil
}

// ParseHTML reads HTML text; xhtml says the text is XHTML (self-closed elements such as <div/> are closed).
func ParseHTML(text string, options HTMLReadOptions, xhtml bool) *HTMLContent {
	if xhtml || strings.HasPrefix(text, "<?xml") {
		text = ExpandSelfClosing(text)
	}
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		// Nesting deeper than the parser allows (512 levels): the text, one paragraph per block element.
		return plainHTML(text)
	}
	r := &htmlReader{options: options}
	r.loadStyleSheets(root)

	sink := &htmlSink{lineStart: true}
	if body := findElement(root, "body"); body != nil {
		r.children(body, sink, htmlContext{})
	}
	r.flush(sink, false)
	if len(sink.pendingBookmarks) > 0 {
		for i := len(sink.blocks) - 1; i >= 0; i-- {
			if p, ok := sink.blocks[i].(*doc.Paragraph); ok {
				p.Inlines = append(p.Inlines, sink.pendingBookmarks...)
				break
			}
		}
	}
	if len(sink.blocks) > 0 {
		if first, ok := sink.blocks[0].(*doc.Paragraph); ok {
			first.PageBreakBefore = false // a break before the very first paragraph would only add an empty page
		}
	}
	content := &HTMLContent{Blocks: sink.blocks}
	if t := findElement(root, "title"); t != nil {
		content.Title = strings.TrimSpace(textContent(t))
	}
	if h := findElement(root, "html"); h != nil {
		if lang := attr(h, "lang"); lang != "" {
			content.Language = lang
		} else {
			content.Language = attrNS(h, "xml", "lang")
		}
	}
	return content
}

// plainHTML reads HTML too deeply nested to build a tree as text only.
func plainHTML(text string) *HTMLContent {
	z := html.NewTokenizer(strings.NewReader(text))
	content := &HTMLContent{}
	var current strings.Builder
	skip := 0
	flush := func() {
		if t := strings.TrimSpace(htmlSpaces.ReplaceAllString(current.String(), " ")); t != "" {
			content.Blocks = append(content.Blocks, &doc.Paragraph{Inlines: []doc.Inline{&doc.Text{Text: t}}})
		}
		current.Reset()
	}
	for {
		kind := z.Next()
		switch kind {
		case html.ErrorToken:
			flush()
			return content
		case html.TextToken:
			if skip == 0 {
				current.Write(z.Text())
			}
		case html.StartTagToken, html.EndTagToken:
			name, _ := z.TagName()
			tag := strings.ToLower(string(name))
			if tag == "script" || tag == "style" || tag == "head" {
				if kind == html.StartTagToken {
					skip++
				} else if skip > 0 {
					skip--
				}
			}
			if htmlBlockElements[tag] || tag == "br" {
				flush()
			}
		}
	}
}

// PruneBookmarks removes bookmarks that no link points to (every element id becomes one while reading).
func PruneBookmarks(d *doc.Document) {
	targets := map[string]bool{}
	for _, s := range d.Sections {
		for _, in := range doc.Inlines(s.Blocks) {
			if l, ok := in.(*doc.Link); ok && strings.HasPrefix(l.Target, "#") {
				targets[l.Target[1:]] = true
			}
		}
	}
	keep := func(inlines []doc.Inline) []doc.Inline {
		out := inlines[:0]
		for _, in := range inlines {
			if b, ok := in.(*doc.Bookmark); ok && !targets[b.Name] {
				continue
			}
			out = append(out, in)
		}
		return out
	}
	var blocks func([]doc.Block)
	var inlines func([]doc.Inline) []doc.Inline
	inlines = func(list []doc.Inline) []doc.Inline {
		list = keep(list)
		for _, in := range list {
			switch v := in.(type) {
			case *doc.Link:
				v.Content = inlines(v.Content)
			case *doc.Note:
				blocks(v.Blocks)
			case *doc.TextBox:
				blocks(v.Blocks)
			}
		}
		return list
	}
	blocks = func(bs []doc.Block) {
		for _, b := range bs {
			switch v := b.(type) {
			case *doc.Paragraph:
				v.Inlines = inlines(v.Inlines)
			case *doc.Table:
				blocks(v.Caption)
				for _, r := range v.Rows {
					for _, c := range r.Cells {
						blocks(c.Blocks)
					}
				}
			}
		}
	}
	for _, s := range d.Sections {
		blocks(s.Blocks)
		for _, h := range s.Headers {
			blocks(h.Blocks)
		}
		for _, f := range s.Footers {
			blocks(f.Blocks)
		}
	}
}

// ───────────────────────── Encoding ─────────────────────────

var (
	xmlEncoding  = regexp.MustCompile(`(?i)<\?xml[^>]*encoding\s*=\s*["']([\w.:-]+)`)
	metaCharset  = regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?\s*([\w.:-]+)`)
	selfClosing  = regexp.MustCompile(`<([A-Za-z][\w:.-]*)(\s[^<>]*?)?\s*/>`)
	voidElements = setOf("area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr")
	htmlSpaces   = regexp.MustCompile(`[ \t\n\r\f]+`)
)

// DecodeHTML decodes an HTML file: BOM, then the XML declaration or <meta charset>, then UTF-8 when the bytes are
// valid UTF-8, else the system's ANSI code page (CP949 on Korean Windows), like browsers do.
func DecodeHTML(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		return string(b[3:])
	}
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		return utf16Text(b[2:], b[0] == 0xFE)
	}
	n := min(len(b), 4096)
	var head strings.Builder
	for _, c := range b[:n] {
		head.WriteRune(rune(c)) // Latin-1, as the declaration is ASCII
	}
	declared := ""
	if m := xmlEncoding.FindStringSubmatch(head.String()); m != nil {
		declared = m[1]
	} else if m := metaCharset.FindStringSubmatch(head.String()); m != nil {
		declared = m[1]
	}
	if declared != "" && !strings.HasPrefix(strings.ToLower(declared), "utf-16") {
		if e, err := htmlindex.Get(declared); err == nil {
			if s, err := e.NewDecoder().Bytes(b); err == nil {
				return string(s)
			}
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if s, err := ansiEncoding().NewDecoder().Bytes(b); err == nil {
		return string(s)
	}
	return string(b)
}

func utf16Text(b []byte, bigEndian bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

// ansiEncoding is the encoding of the PC's ANSI code page.
func ansiEncoding() encoding.Encoding {
	switch doc.AnsiCodePage() {
	case 949:
		return korean.EUCKR
	case 936:
		return simplifiedchinese.GBK
	case 950:
		return traditionalchinese.Big5
	case 932:
		return japanese.ShiftJIS
	case 1251:
		return charmap.Windows1251
	case 1250:
		return charmap.Windows1250
	}
	return charmap.Windows1252
}

// ExpandSelfClosing: XHTML allows <div/> and <a id="x"/>; an HTML parser would treat them as start tags and nest
// the rest of the chapter inside. Void elements (br, img, ...) stay as they are.
func ExpandSelfClosing(text string) string {
	return selfClosing.ReplaceAllStringFunc(text, func(m string) string {
		sub := selfClosing.FindStringSubmatch(m)
		if voidElements[strings.ToLower(sub[1])] {
			return m
		}
		return "<" + sub[1] + sub[2] + "></" + sub[1] + ">"
	})
}

// ───────────────────────── DOM helpers ─────────────────────────

func findElement(n *html.Node, name string) *html.Node {
	if n.Type == html.ElementNode && n.Data == name {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findElement(c, name); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, name string) string {
	v, _ := attrOK(n, name)
	return v
}

func attrOK(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

func attrNS(n *html.Node, namespace, name string) string {
	for _, a := range n.Attr {
		if (a.Namespace == namespace || a.Key == namespace+":"+name) && (a.Key == name || a.Key == namespace+":"+name) {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func localName(n *html.Node) string { return strings.ToLower(n.Data) }

func elementChildren(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// ───────────────────────── Style sheets ─────────────────────────

func (r *htmlReader) loadStyleSheets(root *html.Node) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "style":
				r.sheet.Add(textContent(n))
			case "link":
				if strings.Contains(strings.ToLower(attr(n, "rel")), "stylesheet") {
					if css := r.localFile(attr(n, "href")); css != "" {
						if data, err := os.ReadFile(css); err == nil {
							r.sheet.Add(DecodeHTML(data))
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// styleOf is the declarations for an element: matching style sheet rules, then its style attribute.
func (r *htmlReader) styleOf(e *html.Node) CSSDeclarations {
	style := CSSDeclarations{}
	if !r.sheet.empty() {
		style = r.sheet.Match(localName(e), strings.Fields(attr(e, "class")))
	}
	if inline := attr(e, "style"); inline != "" {
		for k, v := range ParseCSSDeclarations(inline) {
			style[k] = v
		}
	}
	return style
}

// ───────────────────────── Walking ─────────────────────────

// htmlContext is the formatting and nesting of the content being read. item is the list item receiving the next
// paragraphs (the first gets the bullet or number); pre keeps whitespace; firstLine is the CSS text-indent.
type htmlContext struct {
	format    doc.CharFormat
	align     *doc.Align
	indent    int
	listLevel int
	item      *htmlListItem
	heading   int
	pre       bool
	firstLine *int
	depth     int
}

type htmlListItem struct {
	numbering *doc.Numbering
	level     int
	used      bool
}

// htmlSink is where blocks go, and the paragraph currently receiving inline content.
type htmlSink struct {
	blocks           []doc.Block
	paragraph        *doc.Paragraph
	paragraphItem    *htmlListItem
	preformatted     bool
	linkTarget       string
	inLink           bool
	link             *doc.Link
	lineStart        bool
	endsWithSpace    bool
	breakPending     bool
	pendingBookmarks []doc.Inline
}

func (r *htmlReader) children(parent *html.Node, sink *htmlSink, c htmlContext) {
	for n := parent.FirstChild; n != nil; n = n.NextSibling {
		r.node(n, sink, c)
	}
}

func (r *htmlReader) node(n *html.Node, sink *htmlSink, c htmlContext) {
	switch n.Type {
	case html.TextNode:
		r.text(n.Data, sink, c)
	case html.ElementNode:
		r.element(n, sink, c)
	}
}

func (r *htmlReader) element(e *html.Node, sink *htmlSink, context htmlContext) {
	name := localName(e)
	if _, hidden := attrOK(e, "hidden"); htmlSkipped[name] || hidden {
		return
	}
	if context.depth > htmlMaxDepth {
		r.text(textContent(e), sink, context)
		return
	}
	style := r.styleOf(e)
	if style.Get("display") == "none" {
		return
	}
	if isPageBreak(or(style.Get("page-break-before"), style.Get("break-before"))) {
		r.flush(sink, false)
		sink.breakPending = true
	}
	c := context
	c.format = htmlCharFormat(name, e, style, context.format)
	c.depth = context.depth + 1
	if a := htmlAlign(or(attr(e, "align"), style.Get("text-align"))); a != nil {
		c.align = a
	}
	if ws := style.Get("white-space"); ws != "" {
		c.pre = strings.HasPrefix(strings.ToLower(ws), "pre")
	}
	if indent, ok := CSSLength(style.Get("text-indent"), htmlBaseSize); ok {
		c.firstLine = ptr(indent)
	}

	switch name {
	case "br":
		r.addInline(&doc.LineBreak{Format: c.format}, sink, c)
		sink.lineStart = true
		sink.endsWithSpace = false
	case "img":
		r.image(e, attr(e, "src"), style, sink, c)
	case "image": // <image xlink:href> inside <svg>, e.g. EPUB cover pages
		src := attrNS(e, "xlink", "href")
		if src == "" {
			src = attr(e, "href")
		}
		r.image(e, src, style, sink, c)
	case "svg":
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				if ch.Type == html.ElementNode && strings.EqualFold(ch.Data, "image") {
					r.element(ch, sink, c)
				}
				walk(ch)
			}
		}
		walk(e)
	case "a":
		r.anchor(e, sink, c)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		h := c
		h.heading = int(name[1] - '0')
		r.block(e, sink, h)
	case "ul", "ol", "menu", "dir":
		r.list(e, name == "ol", style, sink, c)
	case "table":
		r.table(e, style, sink, c)
	case "blockquote", "dd":
		q := c
		q.indent += quoteIndent
		r.block(e, sink, q)
	case "pre", "listing", "xmp", "plaintext":
		p := c
		p.pre = true
		p.format.Shade = ptr(CodeShade)
		r.block(e, sink, p)
	case "figure", "center":
		f := c
		if f.align == nil {
			f.align = ptr(doc.AlignCenter)
		}
		r.block(e, sink, f)
	case "figcaption":
		f := c
		if f.align == nil {
			f.align = ptr(doc.AlignCenter)
		}
		if f.format.Italic == nil {
			f.format.Italic = ptr(true)
		}
		r.block(e, sink, f)
	case "hr":
		r.flush(sink, false)
		r.horizontalRule(sink, c)
	case "mbp:pagebreak": // Mobipocket page break
		r.flush(sink, false)
		sink.breakPending = true
		r.children(e, sink, c) // an HTML parser nests what follows <mbp:pagebreak/> inside it
	case "q":
		r.addText("“", sink, c)
		r.inline(e, sink, c)
		r.addText("”", sink, c)
	case "rt": // ruby annotation after its base text: 漢字(かんじ)
		r.addText("(", sink, c)
		r.inline(e, sink, c)
		r.addText(")", sink, c)
	default:
		if isBlockElement(name, style) {
			r.block(e, sink, c)
		} else {
			r.inline(e, sink, c)
		}
	}
	if isPageBreak(or(style.Get("page-break-after"), style.Get("break-after"))) {
		r.flush(sink, false)
		sink.breakPending = true
	}
}

func isBlockElement(name string, style CSSDeclarations) bool {
	switch strings.ToLower(style.Get("display")) {
	case "block", "flex", "grid", "list-item", "table", "flow-root":
		return true
	case "inline", "inline-block", "inline-flex", "contents":
		return false
	}
	return htmlBlockElements[name]
}

func isPageBreak(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "always", "page", "left", "right", "recto", "verso":
		return true
	}
	return false
}

// block reads a block element: its content forms paragraphs of their own.
func (r *htmlReader) block(e *html.Node, sink *htmlSink, c htmlContext) {
	r.flush(sink, false)
	r.bookmark(e, sink)
	r.children(e, sink, c)
	r.flush(sink, false)
}

// inline reads an inline element: content continues the current paragraph.
func (r *htmlReader) inline(e *html.Node, sink *htmlSink, c htmlContext) {
	r.bookmark(e, sink)
	r.children(e, sink, c)
}

// bookmark: an element id becomes a bookmark, so links to it keep working (unused ones are pruned later).
func (r *htmlReader) bookmark(e *html.Node, sink *htmlSink) {
	ids := []string{attr(e, "id")}
	if localName(e) == "a" {
		ids = append(ids, attr(e, "name"))
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		name := id
		if r.options.MapID != nil {
			var ok bool
			if name, ok = r.options.MapID(id); !ok {
				continue
			}
		}
		if sink.paragraph != nil {
			sink.paragraph.Inlines = append(sink.paragraph.Inlines, &doc.Bookmark{Name: name})
		} else {
			sink.pendingBookmarks = append(sink.pendingBookmarks, &doc.Bookmark{Name: name})
		}
	}
}

func (r *htmlReader) anchor(e *html.Node, sink *htmlSink, c htmlContext) {
	r.bookmark(e, sink)
	href := strings.TrimSpace(attr(e, "href"))
	target, ok := href, href != ""
	if ok && r.options.MapLink != nil {
		target, ok = r.options.MapLink(href)
	}
	if !ok || sink.inLink {
		r.children(e, sink, c) // no link, or a link inside a link (invalid HTML): text only
		return
	}
	sink.linkTarget, sink.inLink, sink.link = target, true, nil
	r.children(e, sink, c)
	sink.linkTarget, sink.inLink, sink.link = "", false, nil
}

func (r *htmlReader) horizontalRule(sink *htmlSink, c htmlContext) {
	// A short centred line: a separator in documents and a scene break in books.
	rc := c
	rc.align, rc.heading, rc.item, rc.firstLine = ptr(doc.AlignCenter), 0, nil, nil
	p := r.newParagraph(sink, rc)
	line := doc.ThinBorder
	line.Color = "#808080"
	p.Inlines = append(p.Inlines, &doc.Shape{Kind: doc.ShapeLine, Width: 14000, Line: line})
	sink.lineStart = false
	r.flush(sink, false)
}

// ───────────────────────── Paragraphs and text ─────────────────────────

func (r *htmlReader) newParagraph(sink *htmlSink, c htmlContext) *doc.Paragraph {
	p := &doc.Paragraph{Format: doc.ParaFormat{Align: c.align}, HeadingLevel: c.heading, PageBreakBefore: sink.breakPending}
	if c.indent > 0 {
		p.Format.Left = ptr(c.indent)
	}
	if c.firstLine != nil && *c.firstLine != 0 {
		p.Format.FirstLine = c.firstLine
	}
	sink.breakPending = false
	if c.item != nil {
		p.List = &doc.ListRef{Numbering: c.item.numbering, Level: c.item.level, Numbered: !c.item.used}
		c.item.used = true
	}
	p.Inlines = append(p.Inlines, sink.pendingBookmarks...)
	sink.pendingBookmarks = nil
	sink.paragraph = p
	sink.paragraphItem = c.item
	sink.preformatted = c.pre
	sink.link = nil
	sink.lineStart = true
	sink.endsWithSpace = false
	sink.blocks = append(sink.blocks, p)
	return p
}

func (r *htmlReader) addInline(in doc.Inline, sink *htmlSink, c htmlContext) {
	p := sink.paragraph
	if p == nil {
		p = r.newParagraph(sink, c)
	}
	if sink.inLink {
		if sink.link == nil {
			sink.link = &doc.Link{Target: sink.linkTarget}
			p.Inlines = append(p.Inlines, sink.link)
		}
		sink.link.Content = append(sink.link.Content, in)
	} else {
		p.Inlines = append(p.Inlines, in)
	}
}

func (r *htmlReader) addText(text string, sink *htmlSink, c htmlContext) {
	if text == "" {
		return
	}
	r.addInline(&doc.Text{Text: text, Format: c.format}, sink, c)
	sink.lineStart = false
	sink.endsWithSpace = strings.HasSuffix(text, " ")
}

func (r *htmlReader) text(data string, sink *htmlSink, c htmlContext) {
	if c.pre {
		lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n"), "\n")
		for i, line := range lines {
			if i > 0 {
				if sink.paragraph == nil {
					r.newParagraph(sink, c) // an empty line of preformatted text stays
				}
				r.flush(sink, true)
			}
			r.addText(strings.ReplaceAll(line, "\t", "    "), sink, c)
		}
		return
	}
	text := htmlSpaces.ReplaceAllString(data, " ")
	if strings.HasPrefix(text, " ") && (sink.paragraph == nil || sink.lineStart || sink.endsWithSpace) {
		text = text[1:]
	}
	r.addText(text, sink, c)
}

// flush closes the current paragraph; an empty one is dropped (its bookmarks and break move on).
func (r *htmlReader) flush(sink *htmlSink, keepEmpty bool) {
	p := sink.paragraph
	if p == nil {
		return
	}
	sink.paragraph = nil
	sink.link = nil

	// Preformatted lines keep their spaces and stay even when empty.
	hadBreak := false
	if !sink.preformatted {
		p.Inlines, hadBreak = trimInlines(p.Inlines)
	}
	p.Inlines = MergeRuns(p.Inlines)
	for _, in := range p.Inlines {
		if l, ok := in.(*doc.Link); ok {
			l.Content = MergeRuns(l.Content)
		}
	}
	if keepEmpty || hadBreak || sink.preformatted || hasContentInlines(p.Inlines) {
		return
	}
	// Nothing visible: <div id="x"></div>, whitespace between blocks, an empty list item.
	for i := len(sink.blocks) - 1; i >= 0; i-- {
		if sink.blocks[i] == doc.Block(p) {
			sink.blocks = append(sink.blocks[:i], sink.blocks[i+1:]...)
			break
		}
	}
	sink.breakPending = sink.breakPending || p.PageBreakBefore
	for _, in := range doc.InlinesOf(p.Inlines) {
		if b, ok := in.(*doc.Bookmark); ok {
			sink.pendingBookmarks = append(sink.pendingBookmarks, b)
		}
	}
	if p.List != nil && p.List.Numbered && sink.paragraphItem != nil {
		sink.paragraphItem.used = false
	}
}

// trimInlines drops leading and trailing spaces and a trailing line break, as browsers do. The flag tells whether
// the paragraph was only a line break (<p><br/></p>, an intended empty line).
func trimInlines(inlines []doc.Inline) ([]doc.Inline, bool) {
	onlyBreak := false
	if last, owner := lastVisible(inlines, nil); last != nil {
		if _, ok := last.(*doc.LineBreak); ok {
			if owner != nil {
				owner.Content = removeInline(owner.Content, last)
			} else {
				inlines = removeInline(inlines, last)
			}
			onlyBreak = !hasContentInlines(inlines)
		}
	}
	inlines = trimEnd(inlines)
	for i := 0; i < len(inlines); i++ {
		switch v := inlines[i].(type) {
		case *doc.Bookmark:
			continue
		case *doc.Text:
			trimmed := strings.TrimLeft(v.Text, " ")
			if trimmed == "" {
				inlines = append(inlines[:i], inlines[i+1:]...)
				i--
				continue
			}
			inlines[i] = &doc.Text{Text: trimmed, Format: v.Format}
		}
		break
	}
	// Spaces before a line break are not shown either.
	for i := 0; i+1 < len(inlines); i++ {
		if t, ok := inlines[i].(*doc.Text); ok {
			if _, br := inlines[i+1].(*doc.LineBreak); br && strings.HasSuffix(t.Text, " ") {
				inlines[i] = &doc.Text{Text: strings.TrimRight(t.Text, " "), Format: t.Format}
			}
		}
	}
	return inlines, onlyBreak
}

func removeInline(list []doc.Inline, target doc.Inline) []doc.Inline {
	for i, in := range list {
		if in == target {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// lastVisible is the last inline that is not a bookmark, looking into a trailing link; owner is that link.
func lastVisible(inlines []doc.Inline, owner *doc.Link) (doc.Inline, *doc.Link) {
	for i := len(inlines) - 1; i >= 0; i-- {
		if _, ok := inlines[i].(*doc.Bookmark); ok {
			continue
		}
		if l, ok := inlines[i].(*doc.Link); ok {
			if inner, o := lastVisible(l.Content, l); inner != nil {
				return inner, o
			}
			return l, owner
		}
		return inlines[i], owner
	}
	return nil, nil
}

func trimEnd(inlines []doc.Inline) []doc.Inline {
	for i := len(inlines) - 1; i >= 0; i-- {
		switch v := inlines[i].(type) {
		case *doc.Bookmark:
			continue
		case *doc.Link:
			v.Content = trimEnd(v.Content)
			return inlines
		case *doc.Text:
			trimmed := strings.TrimRight(v.Text, " ")
			if trimmed == "" {
				inlines = append(inlines[:i], inlines[i+1:]...)
				continue
			}
			inlines[i] = &doc.Text{Text: trimmed, Format: v.Format}
			return inlines
		default:
			return inlines
		}
	}
	return inlines
}

func hasContentInlines(inlines []doc.Inline) bool {
	for _, in := range inlines {
		switch v := in.(type) {
		case *doc.Text:
			if v.Text != "" {
				return true
			}
		case *doc.Link:
			if hasContentInlines(v.Content) {
				return true
			}
		case *doc.Bookmark:
		default:
			return true
		}
	}
	return false
}

// MergeRuns joins neighbouring runs with the same format, so the output has one run per format change.
func MergeRuns(inlines []doc.Inline) []doc.Inline {
	for i := len(inlines) - 1; i > 0; i-- {
		b, ok1 := inlines[i].(*doc.Text)
		a, ok2 := inlines[i-1].(*doc.Text)
		if ok1 && ok2 && a.Format.Equal(b.Format) {
			inlines[i-1] = &doc.Text{Text: a.Text + b.Text, Format: a.Format}
			inlines = append(inlines[:i], inlines[i+1:]...)
		}
	}
	return inlines
}

// ───────────────────────── Formatting ─────────────────────────

func htmlCharFormat(name string, e *html.Node, style CSSDeclarations, f doc.CharFormat) doc.CharFormat {
	size := val(f.Size, htmlBaseSize)
	switch name {
	case "b", "strong":
		f.Bold = ptr(true)
	case "i", "em", "cite", "dfn", "var", "address":
		f.Italic = ptr(true)
	case "u", "ins":
		f.Underline = ptr(true)
	case "s", "strike", "del":
		f.Strike = ptr(true)
	case "sub":
		f.Subscript, f.Superscript = ptr(true), nil
	case "sup":
		f.Superscript, f.Subscript = ptr(true), nil
	case "code", "kbd", "samp", "tt":
		f.Shade = ptr(CodeShade)
	case "mark":
		f.Shade = ptr(MarkShade)
	case "small":
		f.Size = ptr(int(float64(size) * 0.83))
	case "big":
		f.Size = ptr(int(float64(size) * 1.2))
	case "th":
		if f.Bold == nil {
			f.Bold = ptr(true)
		}
	case "dt":
		f.Bold = ptr(true)
	}
	if name == "font" {
		if c := CSSColor(attr(e, "color")); c != "" {
			f.Color = ptr(c)
		}
		if factor, ok := fontTagSize(attr(e, "size")); ok {
			f.Size = ptr(int(htmlBaseSize * factor))
		}
	}
	if weight := strings.ToLower(style.Get("font-weight")); weight != "" {
		n, err := strconv.Atoi(weight)
		f.Bold = ptr(weight == "bold" || weight == "bolder" || err == nil && n >= 600)
	}
	if fontStyle := strings.ToLower(style.Get("font-style")); fontStyle != "" {
		f.Italic = ptr(strings.HasPrefix(fontStyle, "italic") || strings.HasPrefix(fontStyle, "oblique"))
	}
	if decoration := strings.ToLower(or(style.Get("text-decoration-line"), style.Get("text-decoration"))); decoration != "" {
		if strings.Contains(decoration, "none") {
			f.Underline, f.Strike = ptr(false), ptr(false)
		}
		if strings.Contains(decoration, "underline") {
			f.Underline = ptr(true)
		}
		if strings.Contains(decoration, "line-through") {
			f.Strike = ptr(true)
		}
	}
	switch strings.ToLower(style.Get("vertical-align")) {
	case "super":
		f.Superscript, f.Subscript = ptr(true), nil
	case "sub":
		f.Subscript, f.Superscript = ptr(true), nil
	}
	if c := CSSColor(style.Get("color")); c != "" {
		f.Color = ptr(c)
	}
	if shade := CSSFirstColor(or(style.Get("background-color"), style.Get("background"))); shade != "" && shade != "#FFFFFF" {
		f.Shade = ptr(shade)
	}
	if fs, ok := CSSFontSize(style.Get("font-size"), size); ok {
		f.Size = ptr(fs)
	}
	return f
}

// fontTagSize is <font size="1..7"> (3 is normal), also relative "+1" / "-2".
func fontTagSize(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(text, "+"))
	if err != nil {
		return 0, false
	}
	size := number
	if text[0] == '+' || text[0] == '-' {
		size = 3 + number
	}
	return []float64{0.63, 0.82, 1.0, 1.13, 1.5, 2.0, 3.0}[clamp(size, 1, 7)-1], true
}

func htmlAlign(value string) *doc.Align {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "start":
		return ptr(doc.AlignLeft)
	case "right", "end":
		return ptr(doc.AlignRight)
	case "center", "middle", "-webkit-center":
		return ptr(doc.AlignCenter)
	case "justify":
		return ptr(doc.AlignJustify)
	}
	return nil
}

// ───────────────────────── Lists ─────────────────────────

func (r *htmlReader) list(list *html.Node, ordered bool, style CSSDeclarations, sink *htmlSink, c htmlContext) {
	r.flush(sink, false)
	r.bookmark(list, sink)
	kind := strings.TrimSpace(or(or(style.Get("list-style-type"), style.Get("list-style")), attr(list, "type")))
	unmarked := strings.HasPrefix(strings.ToLower(kind), "none")
	start := 1
	if v, err := strconv.Atoi(attr(list, "start")); err == nil {
		start = v
	}
	level := min(c.listLevel, 6)
	var numbering *doc.Numbering
	if !unmarked {
		numbering = HTMLNumbering(ordered, start, kind, level)
	}
	inner := c
	inner.listLevel++
	inner.heading = 0
	for n := list.FirstChild; n != nil; n = n.NextSibling {
		if n.Type != html.ElementNode || localName(n) != "li" {
			stray := inner
			stray.item = c.item
			r.node(n, sink, stray) // whitespace or stray elements in a list
			continue
		}
		itemStyle := r.styleOf(n)
		if _, hidden := attrOK(n, "hidden"); hidden || itemStyle.Get("display") == "none" {
			continue
		}
		r.flush(sink, false)
		r.bookmark(n, sink)
		// Items of an unmarked list (e.g. a table of contents) are indented paragraphs.
		ic := inner
		if numbering == nil {
			ic.indent = c.indent + quoteIndent
			ic.item = nil
		} else {
			ic.item = &htmlListItem{numbering: numbering, level: level}
		}
		if a := htmlAlign(or(attr(n, "align"), itemStyle.Get("text-align"))); a != nil {
			ic.align = a
		}
		ic.format = htmlCharFormat("li", n, itemStyle, c.format)
		r.children(n, sink, ic)
		r.flush(sink, false)
	}
	r.flush(sink, false)
}

// HTMLNumbering is a list definition in which level follows the HTML list; other levels use defaults.
func HTMLNumbering(ordered bool, start int, kind string, level int) *doc.Numbering {
	bullets := []string{"●", "○", "■"}
	n := &doc.Numbering{}
	for l := 0; l < 7; l++ {
		if !ordered {
			bullet := bullets[l%3]
			if l == level {
				switch strings.ToLower(kind) {
				case "disc":
					bullet = "●"
				case "circle":
					bullet = "○"
				case "square":
					bullet = "■"
				}
			}
			n.Levels = append(n.Levels, doc.NumberingLevel{Format: "DIGIT", Text: bullet, Start: 1, Bullet: true})
			continue
		}
		format := "DIGIT"
		if l == level {
			switch kind {
			case "a", "lower-alpha", "lower-latin":
				format = "LATIN_SMALL"
			case "A", "upper-alpha", "upper-latin":
				format = "LATIN_CAPITAL"
			case "i", "lower-roman":
				format = "ROMAN_SMALL"
			case "I", "upper-roman":
				format = "ROMAN_CAPITAL"
			case "hangul", "korean-hangul-formal":
				format = "HANGUL_SYLLABLE"
			}
		}
		s := 1
		if l == level {
			s = max(0, start)
		}
		n.Levels = append(n.Levels, doc.NumberingLevel{Format: format, Text: fmt.Sprintf("^%d.", l+1), Start: s})
	}
	return n
}

// ───────────────────────── Tables ─────────────────────────

func (r *htmlReader) table(table *html.Node, style CSSDeclarations, sink *htmlSink, c htmlContext) {
	r.flush(sink, false)
	t := &doc.Table{Indent: c.indent, Align: val(c.align, doc.AlignLeft), Borders: tableBorders(table, style)}
	type rowOf struct {
		row  *html.Node
		head bool
	}
	var rows []rowOf
	for _, child := range elementChildren(table) {
		switch localName(child) {
		case "caption":
			captionSink := &htmlSink{lineStart: true}
			cc := c
			cc.indent, cc.item, cc.listLevel, cc.align = 0, nil, 0, ptr(doc.AlignCenter)
			r.children(child, captionSink, cc)
			r.flush(captionSink, false)
			t.Caption = append(t.Caption, captionSink.blocks...)
		case "thead", "tbody", "tfoot":
			for _, rr := range elementChildren(child) {
				if localName(rr) == "tr" {
					rows = append(rows, rowOf{rr, localName(child) == "thead"})
				}
			}
		case "tr":
			rows = append(rows, rowOf{child, false})
		}
	}
	for _, ro := range rows {
		var cells []*html.Node
		allTh := true
		for _, cell := range elementChildren(ro.row) {
			if n := localName(cell); n == "td" || n == "th" {
				cells = append(cells, cell)
				allTh = allTh && n == "th"
			}
		}
		row := &doc.Row{Header: ro.head || len(cells) > 0 && allTh}
		for _, cell := range cells {
			row.Cells = append(row.Cells, r.cell(cell, c, row.Header))
		}
		if len(row.Cells) > 0 {
			t.Rows = append(t.Rows, row)
		}
	}
	if len(t.Rows) == 0 {
		sink.blocks = append(sink.blocks, t.Caption...)
		return
	}
	// Header rows repeat on every page in 한글: only the leading ones are headers.
	leading := true
	for _, row := range t.Rows {
		leading = leading && row.Header
		row.Header = leading
	}
	t.ColumnCount = htmlColumnCount(t)
	var first *html.Node
	if len(rows) > 0 {
		first = rows[0].row
	}
	t.RelativeWidths = r.relativeWidths(table, first, t.ColumnCount)

	r.bookmark(table, sink)
	if sink.breakPending {
		// A table cannot start a new page itself: an empty paragraph in front of it does.
		pc := c
		pc.item, pc.heading = nil, 0
		r.newParagraph(sink, pc)
		sink.paragraph = nil
	} else if len(sink.pendingBookmarks) > 0 {
		// Links to the table land in its first cell.
		cell := t.Rows[0].Cells[0]
		p, ok := firstParagraph(cell.Blocks)
		if !ok {
			p = &doc.Paragraph{}
			cell.Blocks = append([]doc.Block{p}, cell.Blocks...)
		}
		p.Inlines = append(append([]doc.Inline{}, sink.pendingBookmarks...), p.Inlines...)
		sink.pendingBookmarks = nil
	}
	sink.blocks = append(sink.blocks, t)
}

func (r *htmlReader) cell(cell *html.Node, c htmlContext, header bool) *doc.Cell {
	style := r.styleOf(cell)
	h := doc.NewCell()
	if v, err := strconv.Atoi(attr(cell, "rowspan")); err == nil {
		h.RowSpan = clamp(v, 1, 1000)
	}
	if v, err := strconv.Atoi(attr(cell, "colspan")); err == nil {
		h.ColSpan = clamp(v, 1, 1000)
	}
	h.Fill = CSSColor(attr(cell, "bgcolor"))
	if h.Fill == "" {
		h.Fill = CSSFirstColor(or(style.Get("background-color"), style.Get("background")))
	}
	switch strings.ToLower(strings.TrimSpace(or(attr(cell, "valign"), style.Get("vertical-align")))) {
	case "middle", "center":
		h.VerticalAlign = doc.VAlignCenter
	case "bottom":
		h.VerticalAlign = doc.VAlignBottom
	}
	cellSink := &htmlSink{lineStart: true}
	format := htmlCharFormat(localName(cell), cell, style, c.format)
	if header && format.Bold == nil {
		format.Bold = ptr(true)
	}
	align := htmlAlign(or(attr(cell, "align"), style.Get("text-align")))
	if align == nil && localName(cell) == "th" {
		align = ptr(doc.AlignCenter)
	}
	cc := htmlContext{format: format, align: align, depth: c.depth + 1}
	r.bookmark(cell, cellSink)
	r.children(cell, cellSink, cc)
	r.flush(cellSink, false)
	h.Blocks = cellSink.blocks
	if len(cellSink.pendingBookmarks) > 0 {
		var p *doc.Paragraph
		for _, b := range h.Blocks {
			if pp, ok := b.(*doc.Paragraph); ok {
				p = pp
				break
			}
		}
		if p == nil {
			p = &doc.Paragraph{}
			h.Blocks = append([]doc.Block{p}, h.Blocks...)
		}
		p.Inlines = append(append([]doc.Inline{}, cellSink.pendingBookmarks...), p.Inlines...)
	}
	return h
}

// htmlColumnCount is the grid width, taking cells that reach down from rows above into account.
func htmlColumnCount(t *doc.Table) int {
	var busy []int // per column: rows still covered by a row span from above
	width := 0
	for _, row := range t.Rows {
		column := 0
		for _, cell := range row.Cells {
			for column < len(busy) && busy[column] > 0 {
				column++
			}
			for c := column; c < column+cell.ColSpan; c++ {
				for len(busy) <= c {
					busy = append(busy, 0)
				}
				busy[c] = cell.RowSpan
			}
			column += cell.ColSpan
		}
		width = max(width, column)
		for c := range busy {
			busy[c] = max(0, busy[c]-1)
		}
	}
	return max(1, width)
}

// relativeWidths are the column widths from <col> or the first row, as fractions of the table width.
func (r *htmlReader) relativeWidths(table, firstRow *html.Node, columns int) []float64 {
	var sources []*html.Node
	for _, c := range elementChildren(table) {
		switch localName(c) {
		case "colgroup":
			for _, col := range elementChildren(c) {
				if localName(col) == "col" {
					sources = append(sources, col)
				}
			}
		case "col":
			sources = append(sources, c)
		}
	}
	if len(sources) == 0 && firstRow != nil {
		for _, c := range elementChildren(firstRow) {
			if n := localName(c); n == "td" || n == "th" {
				sources = append(sources, c)
			}
		}
	}
	if len(sources) == 0 {
		return nil
	}
	for _, s := range sources {
		if span, err := strconv.Atoi(or(attr(s, "colspan"), attr(s, "span"))); err == nil && span > 1 {
			return nil
		}
	}
	type width struct {
		value   float64
		percent bool
	}
	var widths []width
	for i, s := range sources {
		if i >= columns {
			break
		}
		text := strings.TrimSpace(or(r.styleOf(s).Get("width"), attr(s, "width")))
		if strings.HasSuffix(text, "%") {
			if v, err := strconv.ParseFloat(text[:len(text)-1], 64); err == nil {
				widths = append(widths, width{v, true})
				continue
			}
		}
		v, _ := CSSLength(text, htmlBaseSize)
		widths = append(widths, width{float64(v), false})
	}
	for len(widths) < columns {
		widths = append(widths, width{})
	}
	allZero, allPercent, allAbsolute := true, true, true
	total := 0.0
	for _, w := range widths {
		if w.value > 0 {
			allZero = false
		}
		if !(w.percent || w.value <= 0) {
			allPercent = false
		}
		if w.percent || w.value <= 0 {
			allAbsolute = false
		}
		total += w.value
	}
	if allZero {
		return nil
	}
	out := make([]float64, len(widths))
	if allPercent {
		for i, w := range widths {
			out[i] = max(0, w.value) / 100
		}
		return out
	}
	if allAbsolute {
		for i, w := range widths {
			out[i] = w.value / total
		}
		return out
	}
	return nil
}

// tableBorders: border="0" or CSS border: none turns the grid off; everything else keeps the default thin grid.
func tableBorders(table *html.Node, style CSSDeclarations) *doc.Borders {
	border := strings.ToLower(strings.TrimSpace(or(style.Get("border"), style.Get("border-style"))))
	if attr(table, "border") == "0" || border == "none" || border == "0" || border == "hidden" {
		e := doc.EmptyBorders
		return &e
	}
	return nil
}

// ───────────────────────── Images ─────────────────────────

func (r *htmlReader) image(e *html.Node, source string, style CSSDeclarations, sink *htmlSink, c htmlContext) {
	p := r.resolveImage(source)
	if p == "" {
		return // remote or missing image: skip rather than produce a broken reference
	}
	img := &doc.Image{Path: p}
	if w, ok := imageLength(or(style.Get("width"), attr(e, "width"))); ok {
		img.Width = ptr(w)
	}
	if h, ok := imageLength(or(style.Get("height"), attr(e, "height"))); ok {
		img.Height = ptr(h)
	}
	r.addInline(img, sink, c)
	sink.lineStart = false
	sink.endsWithSpace = false
}

// imageLength reads pixels (width="300"), CSS lengths or percentages of the text width; "auto" and em-based sizes
// are left to the image.
func imageLength(value string) (int, bool) {
	if value == "" || strings.Contains(strings.ToLower(value), "em") {
		return 0, false
	}
	if v, ok := doc.ParseLength(value); ok && v > 0 {
		return v, true
	}
	return 0, false
}

// resolveImage is a local image file for an img src: relative paths, file: URIs and data: URIs; "" for remote ones.
func (r *htmlReader) resolveImage(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if len(source) > 5 && strings.EqualFold(source[:5], "data:") {
		return r.saveDataURI(source)
	}
	if p := r.localFile(source); p != "" {
		return UsableImage(p, r.options.MediaFolder)
	}
	return ""
}

// localFile is a local file referenced from the document, or "" for remote and missing files.
func (r *htmlReader) localFile(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	lower := strings.ToLower(reference)
	if strings.HasPrefix(lower, "file:") {
		u, err := url.Parse(reference)
		if err != nil {
			return ""
		}
		local := u.Path
		if runtime.GOOS == "windows" {
			local = strings.TrimPrefix(local, "/")
		}
		local = filepath.FromSlash(local)
		if InsideFolder(local, r.options.ResourceRoot) && fileExists(local) {
			return local
		}
		return ""
	}
	if strings.Contains(reference, "://") || strings.HasPrefix(reference, "//") || strings.HasPrefix(lower, "mailto:") {
		return ""
	}
	if cut := strings.IndexAny(reference, "?#"); cut >= 0 {
		reference = reference[:cut]
	}
	relative, err := url.PathUnescape(reference)
	if err != nil {
		relative = reference
	}
	if relative == "" {
		return ""
	}
	candidate, err := filepath.Abs(filepath.Join(r.options.BaseFolder, filepath.FromSlash(relative)))
	if err != nil {
		return ""
	}
	if InsideFolder(candidate, r.options.ResourceRoot) && fileExists(candidate) {
		return candidate
	}
	return ""
}

// InsideFolder: packaged books may only refer to files in their own unpacked folder: true when there is no root or
// path is inside it.
func InsideFolder(path, root string) bool {
	if root == "" {
		return true
	}
	absRoot, err1 := filepath.Abs(root)
	absPath, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	absRoot = strings.TrimRight(absRoot, string(filepath.Separator)) + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absRoot))
	}
	return strings.HasPrefix(absPath, absRoot)
}

// saveDataURI writes the image of a data: URI to the media folder.
func (r *htmlReader) saveDataURI(uri string) string {
	comma := strings.Index(uri, ",")
	if comma < 0 {
		return ""
	}
	header := strings.ToLower(uri[5:comma])
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/jpg": "jpg", "image/gif": "gif", "image/webp": "webp",
		"image/bmp": "bmp", "image/svg+xml": "svg", "image/avif": "avif"}[strings.Split(header, ";")[0]]
	if ext == "" {
		return ""
	}
	payload, err := url.PathUnescape(uri[comma+1:])
	if err != nil {
		payload = uri[comma+1:]
	}
	var data []byte
	if strings.HasSuffix(header, ";base64") {
		data, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
		if err != nil {
			return ""
		}
	} else {
		data = []byte(payload)
	}
	if os.MkdirAll(r.options.MediaFolder, 0o755) != nil {
		return ""
	}
	r.embedded++
	p := filepath.Join(r.options.MediaFolder, fmt.Sprintf("embedded-%d.%s", r.embedded, ext))
	if os.WriteFile(p, data, 0o644) != nil {
		return ""
	}
	return UsableImage(p, r.options.MediaFolder)
}

// UsableImage: raster images are used as they are; others must be pictures Go can decode (the writers convert them
// to PNG). SVG has no pure-Go rasteriser here and is left out. "" when the file is no usable picture.
func UsableImage(path, mediaFolder string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp":
		return path
	case ".svg", ".svgz":
		return ""
	}
	if readable(path) {
		return path // TIFF, ...: the HWPX writer converts them, other writers too
	}
	return ""
}
