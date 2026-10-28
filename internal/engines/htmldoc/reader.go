// Package htmldoc reads HTML / XHTML into the document model and writes the
// document model as XHTML (e-book chapters, single pages), Markdown and
// plain text. It is the Go form of Filee's HtmlReader, XhtmlWriter,
// MarkdownWriter and PlainTextWriter, shared by the e-book, e-mail (MHTML)
// and document engines.
package htmldoc

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/images"
)

// ReadOptions say how a document's resources, links and anchors are resolved.
type ReadOptions struct {
	// BaseFolder resolves relative image and style sheet paths (the HTML file's folder).
	BaseFolder string
	// MediaFolder receives images embedded as data: URIs (created on demand).
	MediaFolder string
	// ResourceRoot, when set, is the folder local images and style sheets must be inside (an unpacked e-book).
	ResourceRoot string
	// MapLink maps an href to the link target to write; ok false keeps only the link text. Default: unchanged.
	MapLink func(href string) (string, bool)
	// MapID maps an element id to a bookmark name; ok false: no bookmark. Default: the id itself.
	MapID func(id string) (string, bool)
}

// Content is what one HTML document held.
type Content struct {
	Blocks   []doc.Block
	Title    string
	Language string
}

const (
	quoteIndent = 2000 // per block quote level (as the Markdown and Pandoc readers)
	maxDepth    = 200  // deeper elements are read as plain text
	baseSize    = 1000 // 1em when no size is set: 10 pt
	// CodeShade is the shading of code, as the Markdown reader writes it.
	CodeShade = "#F1F0F5"
	// MarkShade is the shading of <mark> (==marked== in Markdown).
	MarkShade = "#FFF3A3"
)

var skipped = setOf("head", "title", "meta", "link", "script", "style", "noscript", "template", "nav", "button", "select",
	"option", "textarea", "input", "iframe", "object", "embed", "video", "audio", "canvas", "map", "param",
	"source", "track", "dialog", "rp", "base")

var blockElements = setOf("p", "div", "section", "article", "main", "header", "footer", "aside", "address", "center", "details",
	"summary", "fieldset", "legend", "form", "hgroup", "body", "html", "dl", "dt", "dd", "menu", "caption",
	"figure", "figcaption", "blockquote", "pre", "li", "listing", "plaintext", "xmp")

func setOf(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

// SVGRasterizer turns an SVG picture into a PNG in folder (the document
// model needs pixel pictures); nil drops SVG pictures. The vector engine may set it.
var SVGRasterizer func(svgPath, folder string) (string, error)

type reader struct {
	opts     ReadOptions
	sheet    styleSheet
	embedded int
}

// Read reads an HTML file as a document of its own; media receives embedded pictures.
func Read(path, media string) (*doc.Document, error) {
	abs, _ := filepath.Abs(path)
	c, err := ReadFile(path, ReadOptions{BaseFolder: filepath.Dir(abs), MediaFolder: media})
	if err != nil {
		return nil, err
	}
	return ToDocument(c), nil
}

// ToDocument is one section with the content, titled like the page.
func ToDocument(c *Content) *doc.Document {
	s := doc.NewSection()
	s.Blocks = c.Blocks
	d := &doc.Document{Title: c.Title, Sections: []*doc.Section{s}}
	PruneBookmarks(d)
	return d
}

// ReadFile reads an HTML file (e.g. an e-book chapter) with the given resolution rules.
func ReadFile(path string, opts ReadOptions) (*Content, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	return Parse(Decode(b), opts, ext == ".xhtml" || ext == ".xht" || ext == ".xml" || ext == ".opf"), nil
}

// Parse reads HTML text; xhtml text is XML syntax, so self-closed elements such as <div/> are closed.
func Parse(text string, opts ReadOptions, xhtml bool) *Content {
	if xhtml || strings.HasPrefix(text, "<?xml") {
		text = ExpandSelfClosing(text)
	}
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		// Nested deeper than the parser allows (512 levels): keep the text, one paragraph per block.
		if root, err = html.Parse(strings.NewReader(flatten(text))); err != nil {
			return &Content{}
		}
	}
	r := &reader{opts: opts}
	r.loadStyleSheets(root)
	sink := &sink{lineStart: true}
	if body := find(root, "body"); body != nil {
		r.children(body, sink, context{})
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
		if p, ok := sink.blocks[0].(*doc.Paragraph); ok {
			p.PageBreakBefore = false // a break before the very first paragraph would only add an empty page
		}
	}
	c := &Content{Blocks: sink.blocks}
	if t := find(root, "title"); t != nil {
		c.Title = strings.TrimSpace(textContent(t))
	}
	if h := find(root, "html"); h != nil {
		c.Language = attr(h, "lang")
		if c.Language == "" {
			c.Language = attr(h, "xml:lang")
		}
	}
	return c
}

// flatten rewrites absurdly deep HTML as flat paragraphs of its text.
func flatten(text string) string {
	z := html.NewTokenizer(strings.NewReader(text))
	var sb strings.Builder
	sb.WriteString("<html><body><p>")
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			sb.WriteString("</p></body></html>")
			return sb.String()
		case html.TextToken:
			if skip == 0 {
				sb.WriteString(html.EscapeString(string(z.Text())))
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := strings.ToLower(string(name))
			if n == "script" || n == "style" || n == "head" {
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
			}
			if blockElements[n] || n == "br" || len(n) == 2 && n[0] == 'h' && n[1] >= '1' && n[1] <= '6' {
				sb.WriteString("</p><p>")
			}
		}
	}
}

// PruneBookmarks removes bookmarks no link points to (every element id becomes one while reading).
func PruneBookmarks(d *doc.Document) {
	targets := map[string]bool{}
	for _, s := range d.Sections {
		for _, in := range doc.Inlines(s.Blocks) {
			if l, ok := in.(*doc.Link); ok && strings.HasPrefix(l.Target, "#") {
				targets[l.Target[1:]] = true
			}
		}
	}
	for _, s := range d.Sections {
		EachInlineList(s.Blocks, func(list *[]doc.Inline) {
			out := (*list)[:0]
			for _, in := range *list {
				if b, ok := in.(*doc.Bookmark); ok && !targets[b.Name] {
					continue
				}
				out = append(out, in)
			}
			*list = out
		})
	}
}

// EachInlineList visits every inline list (paragraphs, link content, notes,
// text boxes, table cells and captions) so it can be changed in place.
func EachInlineList(blocks []doc.Block, visit func(list *[]doc.Inline)) {
	for _, b := range blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			eachInlineList(&b.Inlines, visit)
		case *doc.Table:
			EachInlineList(b.Caption, visit)
			for _, r := range b.Rows {
				for _, c := range r.Cells {
					EachInlineList(c.Blocks, visit)
				}
			}
		}
	}
}

func eachInlineList(list *[]doc.Inline, visit func(*[]doc.Inline)) {
	visit(list)
	for _, in := range *list {
		switch in := in.(type) {
		case *doc.Link:
			eachInlineList(&in.Content, visit)
		case *doc.Note:
			EachInlineList(in.Blocks, visit)
		case *doc.TextBox:
			EachInlineList(in.Blocks, visit)
		}
	}
}

// ---- encoding ----

var (
	xmlEncoding = regexp.MustCompile(`(?i)<\?xml[^>]*encoding\s*=\s*["']([\w.:-]+)`)
	metaCharset = regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?\s*([\w.:-]+)`)
)

// Decode decodes an HTML file: BOM, then the XML declaration or <meta
// charset>, then UTF-8 when valid, else the PC's ANSI code page, like browsers do.
func Decode(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return doc.DecodeText(b)
	}
	head := b[:min(len(b), 4096)]
	declared := ""
	if m := xmlEncoding.FindSubmatch(head); m != nil {
		declared = string(m[1])
	} else if m := metaCharset.FindSubmatch(head); m != nil {
		declared = string(m[1])
	}
	if declared != "" && !strings.HasPrefix(strings.ToLower(declared), "utf-16") {
		if strings.EqualFold(declared, "utf-8") || strings.EqualFold(declared, "utf8") {
			if utf8.Valid(b) {
				return string(b)
			}
		} else if enc, _ := charset.Lookup(declared); enc != nil {
			if s, err := enc.NewDecoder().Bytes(b); err == nil {
				return string(s)
			}
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return doc.DecodeText(b)
}

var selfClosing = regexp.MustCompile(`<([A-Za-z][\w:.-]*)(\s[^<>]*?)?\s*/>`)

var voidElements = setOf("area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr")

// ExpandSelfClosing: XHTML allows <div/> and <a id="x"/>; an HTML parser
// would treat them as start tags and nest the rest of the chapter inside.
// Void elements (br, img, …) stay as they are.
func ExpandSelfClosing(s string) string {
	return selfClosing.ReplaceAllStringFunc(s, func(m string) string {
		sub := selfClosing.FindStringSubmatch(m)
		if voidElements[strings.ToLower(sub[1])] {
			return m
		}
		return "<" + sub[1] + sub[2] + "></" + sub[1] + ">"
	})
}

// ---- DOM helpers ----

func find(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, tag); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		k := a.Key
		if a.Namespace != "" {
			k = a.Namespace + ":" + a.Key
		}
		if strings.EqualFold(k, key) {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
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

func elements(n *html.Node, visit func(*html.Node)) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			visit(c)
			elements(c, visit)
		}
	}
}

// ---- style sheets ----

func (r *reader) loadStyleSheets(root *html.Node) {
	elements(root, func(e *html.Node) {
		switch e.Data {
		case "style":
			r.sheet.add(textContent(e))
		case "link":
			if !strings.Contains(strings.ToLower(attr(e, "rel")), "stylesheet") {
				return
			}
			if css := r.localFile(attr(e, "href")); css != "" {
				if b, err := os.ReadFile(css); err == nil { // a missing style sheet only costs formatting
					r.sheet.add(Decode(b))
				}
			}
		}
	})
}

// styleOf: matching style sheet rules, then the style attribute.
func (r *reader) styleOf(e *html.Node) declarations {
	var style declarations
	if len(r.sheet.rules) == 0 {
		style = declarations{}
	} else {
		style = r.sheet.match(strings.ToLower(e.Data), strings.Fields(attr(e, "class")))
	}
	if inline := attr(e, "style"); inline != "" {
		for k, v := range parseDeclarations(inline) {
			style[k] = v
		}
	}
	return style
}

// ---- walking ----

// context is the formatting and nesting of the content being read.
type context struct {
	format    doc.CharFormat
	align     *doc.Align
	indent    int
	listLevel int
	item      *listItem
	heading   int
	pre       bool
	firstLine *int
	depth     int
}

// listItem: its paragraphs share the list, only the first one is numbered.
type listItem struct {
	numbering *doc.Numbering
	level     int
	used      bool
}

// sink is where blocks go, and the paragraph currently receiving inline content.
type sink struct {
	blocks           []doc.Block
	paragraph        *doc.Paragraph
	paragraphItem    *listItem
	preformatted     bool
	linkTarget       *string
	link             *doc.Link
	lineStart        bool
	endsWithSpace    bool
	breakPending     bool
	pendingBookmarks []doc.Inline
}

func (r *reader) children(parent *html.Node, s *sink, c context) {
	for ch := parent.FirstChild; ch != nil; ch = ch.NextSibling {
		r.node(ch, s, c)
	}
}

func (r *reader) node(n *html.Node, s *sink, c context) {
	switch n.Type {
	case html.TextNode:
		r.text(n.Data, s, c)
	case html.ElementNode:
		r.element(n, s, c)
	}
}

func isPageBreak(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "always", "page", "left", "right", "recto", "verso":
		return true
	}
	return false
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (r *reader) element(e *html.Node, s *sink, ctx context) {
	name := strings.ToLower(e.Data)
	if skipped[name] || hasAttr(e, "hidden") {
		return
	}
	if ctx.depth > maxDepth {
		r.text(textContent(e), s, ctx)
		return
	}
	style := r.styleOf(e)
	if strings.EqualFold(style.get("display"), "none") {
		return
	}
	if isPageBreak(or(style.get("page-break-before"), style.get("break-before"))) {
		r.flush(s, false)
		s.breakPending = true
	}
	c := ctx
	c.format = charFormat(name, e, style, ctx.format)
	c.depth = ctx.depth + 1
	if a := alignOf(or(attr(e, "align"), style.get("text-align"))); a != nil {
		c.align = a
	}
	if ws := style.get("white-space"); ws != "" {
		c.pre = strings.HasPrefix(strings.ToLower(ws), "pre")
	}
	if v, ok := cssLength(style.get("text-indent"), baseSize); ok {
		c.firstLine = &v
	}

	switch name {
	case "br":
		r.addInline(&doc.LineBreak{Format: c.format}, s, c)
		s.lineStart = true
		s.endsWithSpace = false
	case "img":
		r.image(e, attr(e, "src"), style, s, c)
	case "image": // <image xlink:href> inside <svg>, e.g. EPUB cover pages
		r.image(e, or(attr(e, "xlink:href"), attr(e, "href")), style, s, c)
	case "svg":
		elements(e, func(im *html.Node) {
			if strings.EqualFold(im.Data, "image") {
				r.element(im, s, c)
			}
		})
	case "a":
		r.anchor(e, s, c)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.heading = int(name[1] - '0')
		r.block(e, s, c)
	case "ul", "ol", "menu", "dir":
		r.list(e, name == "ol", style, s, c)
	case "table":
		r.table(e, style, s, c)
	case "blockquote", "dd":
		c.indent += quoteIndent
		r.block(e, s, c)
	case "pre", "listing", "xmp", "plaintext":
		c.pre = true
		c.format.Shade = doc.Ptr(CodeShade)
		r.block(e, s, c)
	case "figure", "center":
		if c.align == nil {
			c.align = doc.Ptr(doc.AlignCenter)
		}
		r.block(e, s, c)
	case "figcaption":
		if c.align == nil {
			c.align = doc.Ptr(doc.AlignCenter)
		}
		if c.format.Italic == nil {
			c.format.Italic = doc.Ptr(true)
		}
		r.block(e, s, c)
	case "hr":
		r.flush(s, false)
		r.horizontalRule(s, c)
	case "mbp:pagebreak": // Mobipocket page break
		r.flush(s, false)
		s.breakPending = true
		r.children(e, s, c) // an HTML parser nests what follows <mbp:pagebreak/> inside it
	case "q":
		r.addText("“", s, c)
		r.inline(e, s, c)
		r.addText("”", s, c)
	case "rt": // ruby annotation after its base text: 漢字(かんじ)
		r.addText("(", s, c)
		r.inline(e, s, c)
		r.addText(")", s, c)
	default:
		if isBlock(name, style) {
			r.block(e, s, c)
		} else {
			r.inline(e, s, c)
		}
	}

	if isPageBreak(or(style.get("page-break-after"), style.get("break-after"))) {
		r.flush(s, false)
		s.breakPending = true
	}
}

func isBlock(name string, style declarations) bool {
	switch strings.ToLower(style.get("display")) {
	case "block", "flex", "grid", "list-item", "table", "flow-root":
		return true
	case "inline", "inline-block", "inline-flex", "contents":
		return false
	}
	return blockElements[name]
}

// block: the element's content forms paragraphs of their own.
func (r *reader) block(e *html.Node, s *sink, c context) {
	r.flush(s, false)
	r.bookmark(e, s)
	r.children(e, s, c)
	r.flush(s, false)
}

// inline: the content continues the current paragraph.
func (r *reader) inline(e *html.Node, s *sink, c context) {
	r.bookmark(e, s)
	r.children(e, s, c)
}

// bookmark: an element id becomes a bookmark, so links to it keep working (unused ones are pruned later).
func (r *reader) bookmark(e *html.Node, s *sink) {
	ids := []string{attr(e, "id")}
	if strings.EqualFold(e.Data, "a") {
		ids = append(ids, attr(e, "name"))
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		name := id
		if r.opts.MapID != nil {
			var ok bool
			if name, ok = r.opts.MapID(id); !ok {
				continue
			}
		}
		if s.paragraph != nil {
			s.paragraph.Inlines = append(s.paragraph.Inlines, &doc.Bookmark{Name: name})
		} else {
			s.pendingBookmarks = append(s.pendingBookmarks, &doc.Bookmark{Name: name})
		}
	}
}

func (r *reader) anchor(e *html.Node, s *sink, c context) {
	r.bookmark(e, s)
	href := strings.TrimSpace(attr(e, "href"))
	target, ok := href, href != ""
	if ok && r.opts.MapLink != nil {
		target, ok = r.opts.MapLink(href)
	}
	if !ok || s.linkTarget != nil {
		r.children(e, s, c) // no link, or a link inside a link (invalid HTML): text only
		return
	}
	s.linkTarget = &target
	s.link = nil
	r.children(e, s, c)
	s.linkTarget = nil
	s.link = nil
}

func (r *reader) horizontalRule(s *sink, c context) {
	// A short centred line: a separator in documents and a scene break in books.
	c.align = doc.Ptr(doc.AlignCenter)
	c.heading, c.item, c.firstLine = 0, nil, nil
	p := r.newParagraph(s, c)
	line := doc.ThinBorder
	line.Color = "#808080"
	p.Inlines = append(p.Inlines, &doc.Shape{Kind: doc.ShapeLine, Width: 14000, Height: 0, Line: line})
	s.lineStart = false
	r.flush(s, false)
}

// ---- paragraphs and text ----

func (r *reader) newParagraph(s *sink, c context) *doc.Paragraph {
	p := &doc.Paragraph{HeadingLevel: c.heading, PageBreakBefore: s.breakPending}
	p.Format.Align = c.align
	if c.indent > 0 {
		p.Format.Left = doc.Ptr(c.indent)
	}
	if c.firstLine != nil && *c.firstLine != 0 {
		p.Format.FirstLine = doc.Ptr(*c.firstLine)
	}
	s.breakPending = false
	if c.item != nil {
		p.List = &doc.ListRef{Numbering: c.item.numbering, Level: c.item.level, Numbered: !c.item.used}
		c.item.used = true
	}
	p.Inlines = append(p.Inlines, s.pendingBookmarks...)
	s.pendingBookmarks = nil
	s.paragraph = p
	s.paragraphItem = c.item
	s.preformatted = c.pre
	s.link = nil
	s.lineStart = true
	s.endsWithSpace = false
	s.blocks = append(s.blocks, p)
	return p
}

func (r *reader) addInline(in doc.Inline, s *sink, c context) {
	p := s.paragraph
	if p == nil {
		p = r.newParagraph(s, c)
	}
	if s.linkTarget != nil {
		if s.link == nil {
			s.link = &doc.Link{Target: *s.linkTarget}
			p.Inlines = append(p.Inlines, s.link)
		}
		s.link.Content = append(s.link.Content, in)
		return
	}
	p.Inlines = append(p.Inlines, in)
}

func (r *reader) addText(text string, s *sink, c context) {
	if text == "" {
		return
	}
	r.addInline(&doc.Text{Text: text, Format: c.format}, s, c)
	s.lineStart = false
	s.endsWithSpace = strings.HasSuffix(text, " ")
}

var whitespace = regexp.MustCompile(`[ \t\n\r\f]+`)

func (r *reader) text(data string, s *sink, c context) {
	if c.pre {
		lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n"), "\n")
		for i, line := range lines {
			if i > 0 {
				if s.paragraph == nil {
					r.newParagraph(s, c) // an empty line of preformatted text stays
				}
				r.flush(s, true)
			}
			r.addText(strings.ReplaceAll(line, "\t", "    "), s, c)
		}
		return
	}
	text := whitespace.ReplaceAllString(data, " ")
	if strings.HasPrefix(text, " ") && (s.paragraph == nil || s.lineStart || s.endsWithSpace) {
		text = text[1:]
	}
	r.addText(text, s, c)
}

// flush closes the current paragraph; an empty one is dropped (its bookmarks and break move on).
func (r *reader) flush(s *sink, keepEmpty bool) {
	p := s.paragraph
	if p == nil {
		return
	}
	s.paragraph = nil
	s.link = nil
	hadBreak := false
	if !s.preformatted {
		p.Inlines, hadBreak = trim(p.Inlines)
	}
	p.Inlines = MergeRuns(p.Inlines)
	for _, in := range p.Inlines {
		if l, ok := in.(*doc.Link); ok {
			l.Content = MergeRuns(l.Content)
		}
	}
	if keepEmpty || hadBreak || s.preformatted || hasContent(p.Inlines) {
		return
	}
	// Nothing visible: <div id="x"></div>, whitespace between blocks, an empty list item.
	for i := len(s.blocks) - 1; i >= 0; i-- {
		if s.blocks[i] == doc.Block(p) {
			s.blocks = append(s.blocks[:i], s.blocks[i+1:]...)
			break
		}
	}
	s.breakPending = s.breakPending || p.PageBreakBefore
	for _, in := range doc.InlinesOf(p.Inlines) {
		if b, ok := in.(*doc.Bookmark); ok {
			s.pendingBookmarks = append(s.pendingBookmarks, b)
		}
	}
	if p.List != nil && p.List.Numbered && s.paragraphItem != nil {
		s.paragraphItem.used = false
	}
}

// trim drops leading and trailing spaces and a trailing line break, as
// browsers do; true when the paragraph was only a line break (<p><br/></p>,
// an intended empty line).
func trim(inlines []doc.Inline) ([]doc.Inline, bool) {
	onlyBreak := false
	if lb, ok := lastVisible(inlines).(*doc.LineBreak); ok {
		inlines = removeInline(inlines, lb)
		onlyBreak = !hasContent(inlines)
	}
	inlines = trimEnd(inlines)
	for i := 0; i < len(inlines); i++ {
		switch t := inlines[i].(type) {
		case *doc.Bookmark:
			continue
		case *doc.Text:
			trimmed := strings.TrimLeft(t.Text, " ")
			if trimmed == "" {
				inlines = append(inlines[:i], inlines[i+1:]...)
				i--
				continue
			}
			inlines[i] = &doc.Text{Text: trimmed, Format: t.Format}
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

// removeInline removes target from inlines or from the content of the link holding it.
func removeInline(inlines []doc.Inline, target doc.Inline) []doc.Inline {
	for i, in := range inlines {
		if in == target {
			return append(inlines[:i], inlines[i+1:]...)
		}
		if l, ok := in.(*doc.Link); ok {
			l.Content = removeInline(l.Content, target)
		}
	}
	return inlines
}

func lastVisible(inlines []doc.Inline) doc.Inline {
	for i := len(inlines) - 1; i >= 0; i-- {
		if _, ok := inlines[i].(*doc.Bookmark); ok {
			continue
		}
		if l, ok := inlines[i].(*doc.Link); ok {
			if v := lastVisible(l.Content); v != nil {
				return v
			}
		}
		return inlines[i]
	}
	return nil
}

func trimEnd(inlines []doc.Inline) []doc.Inline {
	for i := len(inlines) - 1; i >= 0; i-- {
		switch t := inlines[i].(type) {
		case *doc.Bookmark:
			continue
		case *doc.Link:
			t.Content = trimEnd(t.Content)
			return inlines
		case *doc.Text:
			trimmed := strings.TrimRight(t.Text, " ")
			if trimmed == "" {
				inlines = append(inlines[:i], inlines[i+1:]...)
				continue
			}
			inlines[i] = &doc.Text{Text: trimmed, Format: t.Format}
			return inlines
		default:
			return inlines
		}
	}
	return inlines
}

func hasContent(inlines []doc.Inline) bool {
	for _, in := range inlines {
		switch t := in.(type) {
		case *doc.Text:
			if t.Text != "" {
				return true
			}
		case *doc.Link:
			if hasContent(t.Content) {
				return true
			}
		case *doc.Bookmark:
		default:
			return true
		}
	}
	return false
}

// MergeRuns joins neighbouring texts of the same format, so the output has one run per format change.
func MergeRuns(inlines []doc.Inline) []doc.Inline { return doc.MergeTexts(inlines) }

// ---- formatting ----

func charFormat(name string, e *html.Node, style declarations, f doc.CharFormat) doc.CharFormat {
	size := doc.Val(f.Size, baseSize)
	switch name {
	case "b", "strong":
		f.Bold = doc.Ptr(true)
	case "i", "em", "cite", "dfn", "var", "address":
		f.Italic = doc.Ptr(true)
	case "u", "ins":
		f.Underline = doc.Ptr(true)
	case "s", "strike", "del":
		f.Strike = doc.Ptr(true)
	case "sub":
		f.Subscript, f.Superscript = doc.Ptr(true), nil
	case "sup":
		f.Superscript, f.Subscript = doc.Ptr(true), nil
	case "code", "kbd", "samp", "tt":
		f.Shade = doc.Ptr(CodeShade)
	case "mark":
		f.Shade = doc.Ptr(MarkShade)
	case "small":
		f.Size = doc.Ptr(int(float64(size) * 0.83))
	case "big":
		f.Size = doc.Ptr(int(float64(size) * 1.2))
	case "th":
		if f.Bold == nil {
			f.Bold = doc.Ptr(true)
		}
	case "dt":
		f.Bold = doc.Ptr(true)
	}
	if name == "font" {
		if c := cssColor(attr(e, "color")); c != "" {
			f.Color = doc.Ptr(c)
		}
		if factor, ok := fontTagSize(attr(e, "size")); ok {
			f.Size = doc.Ptr(int(baseSize * factor))
		}
	}
	if w := strings.ToLower(style.get("font-weight")); w != "" {
		n, err := strconv.Atoi(w)
		f.Bold = doc.Ptr(w == "bold" || w == "bolder" || err == nil && n >= 600)
	}
	if fs := strings.ToLower(style.get("font-style")); fs != "" {
		f.Italic = doc.Ptr(strings.HasPrefix(fs, "italic") || strings.HasPrefix(fs, "oblique"))
	}
	if d := strings.ToLower(or(style.get("text-decoration-line"), style.get("text-decoration"))); d != "" {
		if strings.Contains(d, "none") {
			f.Underline, f.Strike = doc.Ptr(false), doc.Ptr(false)
		}
		if strings.Contains(d, "underline") {
			f.Underline = doc.Ptr(true)
		}
		if strings.Contains(d, "line-through") {
			f.Strike = doc.Ptr(true)
		}
	}
	switch strings.ToLower(style.get("vertical-align")) {
	case "super":
		f.Superscript, f.Subscript = doc.Ptr(true), nil
	case "sub":
		f.Subscript, f.Superscript = doc.Ptr(true), nil
	}
	if c := cssColor(style.get("color")); c != "" {
		f.Color = doc.Ptr(c)
	}
	if shade := firstColor(or(style.get("background-color"), style.get("background"))); shade != "" && shade != "#FFFFFF" {
		f.Shade = doc.Ptr(shade)
	}
	if fs, ok := cssFontSize(style.get("font-size"), size); ok {
		f.Size = doc.Ptr(fs)
	}
	return f
}

// fontTagSize: <font size="1..7"> (3 is normal), also relative "+1" / "-2".
func fontTagSize(v string) (float64, bool) {
	text := strings.TrimSpace(v)
	if text == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(text, "+"))
	if err != nil {
		return 0, false
	}
	size := n
	if text[0] == '+' || text[0] == '-' {
		size = 3 + n
	}
	switch max(1, min(7, size)) {
	case 1:
		return 0.63, true
	case 2:
		return 0.82, true
	case 3:
		return 1.0, true
	case 4:
		return 1.13, true
	case 5:
		return 1.5, true
	case 6:
		return 2.0, true
	}
	return 3.0, true
}

func alignOf(v string) *doc.Align {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "left", "start":
		return doc.Ptr(doc.AlignLeft)
	case "right", "end":
		return doc.Ptr(doc.AlignRight)
	case "center", "middle", "-webkit-center":
		return doc.Ptr(doc.AlignCenter)
	case "justify":
		return doc.Ptr(doc.AlignJustify)
	}
	return nil
}

// ---- lists ----

func (r *reader) list(list *html.Node, ordered bool, style declarations, s *sink, c context) {
	r.flush(s, false)
	r.bookmark(list, s)
	typ := strings.TrimSpace(or(or(style.get("list-style-type"), style.get("list-style")), attr(list, "type")))
	unmarked := strings.HasPrefix(strings.ToLower(typ), "none")
	start := 1
	if v, err := strconv.Atoi(strings.TrimSpace(attr(list, "start"))); err == nil {
		start = v
	}
	level := min(c.listLevel, 6)
	var numbering *doc.Numbering
	if !unmarked {
		numbering = Numbering(ordered, start, typ, level)
	}
	inner := c
	inner.listLevel = c.listLevel + 1
	inner.heading = 0
	for ch := list.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode || !strings.EqualFold(ch.Data, "li") {
			stray := inner
			stray.item = c.item
			r.node(ch, s, stray) // whitespace or stray elements in a list
			continue
		}
		itemStyle := r.styleOf(ch)
		if hasAttr(ch, "hidden") || strings.EqualFold(itemStyle.get("display"), "none") {
			continue
		}
		r.flush(s, false)
		r.bookmark(ch, s)
		ic := inner
		if numbering == nil {
			// Items of an unmarked list (e.g. a table of contents) are indented paragraphs.
			ic.indent = c.indent + quoteIndent
			ic.item = nil
		} else {
			ic.item = &listItem{numbering: numbering, level: level}
		}
		if a := alignOf(or(attr(ch, "align"), itemStyle.get("text-align"))); a != nil {
			ic.align = a
		}
		ic.format = charFormat("li", ch, itemStyle, c.format)
		r.children(ch, s, ic)
		r.flush(s, false)
	}
	r.flush(s, false)
}

// Numbering is a list definition in which level follows the HTML list; other levels use defaults.
func Numbering(ordered bool, start int, typ string, level int) *doc.Numbering {
	bullets := []string{"●", "○", "■"}
	n := &doc.Numbering{}
	for l := 0; l < 7; l++ {
		if !ordered {
			bullet := bullets[l%3]
			if l == level {
				switch strings.ToLower(typ) {
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
			switch typ {
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
		st := 1
		if l == level {
			st = max(0, start)
		}
		n.Levels = append(n.Levels, doc.NumberingLevel{Format: format, Text: "^" + strconv.Itoa(l+1) + ".", Start: st})
	}
	return n
}

// ---- tables ----

func childElements(n *html.Node, names ...string) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		for _, name := range names {
			if strings.EqualFold(c.Data, name) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (r *reader) table(t *html.Node, style declarations, s *sink, c context) {
	r.flush(s, false)
	table := &doc.Table{Indent: c.indent, Align: doc.Val(c.align, doc.AlignLeft), Borders: tableBorders(t, style)}
	type rowEl struct {
		n    *html.Node
		head bool
	}
	var rows []rowEl
	for ch := t.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode {
			continue
		}
		switch strings.ToLower(ch.Data) {
		case "caption":
			cs := &sink{lineStart: true}
			cc := c
			cc.indent, cc.item, cc.listLevel, cc.align = 0, nil, 0, doc.Ptr(doc.AlignCenter)
			r.children(ch, cs, cc)
			r.flush(cs, false)
			table.Caption = append(table.Caption, cs.blocks...)
		case "thead", "tbody", "tfoot":
			for _, tr := range childElements(ch, "tr") {
				rows = append(rows, rowEl{tr, strings.EqualFold(ch.Data, "thead")})
			}
		case "tr":
			rows = append(rows, rowEl{ch, false})
		}
	}
	for _, re := range rows {
		cells := childElements(re.n, "td", "th")
		allTh := len(cells) > 0
		for _, cell := range cells {
			if !strings.EqualFold(cell.Data, "th") {
				allTh = false
			}
		}
		row := &doc.Row{Header: re.head || allTh}
		for _, cell := range cells {
			row.Cells = append(row.Cells, r.cell(cell, c, row.Header))
		}
		if len(row.Cells) > 0 {
			table.Rows = append(table.Rows, row)
		}
	}
	if len(table.Rows) == 0 {
		s.blocks = append(s.blocks, table.Caption...)
		return
	}
	// Header rows repeat on every page: only the leading ones are headers.
	leading := true
	for _, row := range table.Rows {
		leading = leading && row.Header
		row.Header = leading
	}
	table.ColumnCount = columnCount(table)
	var first *html.Node
	if len(rows) > 0 {
		first = rows[0].n
	}
	table.RelativeWidths = r.relativeWidths(t, first, table.ColumnCount)

	r.bookmark(t, s)
	if s.breakPending {
		// A table cannot start a new page itself: an empty paragraph in front of it does.
		pc := c
		pc.item, pc.heading = nil, 0
		r.newParagraph(s, pc)
		s.paragraph = nil
	} else if len(s.pendingBookmarks) > 0 {
		// Links to the table land in its first cell.
		cell := table.Rows[0].Cells[0]
		var p *doc.Paragraph
		if len(cell.Blocks) > 0 {
			p, _ = cell.Blocks[0].(*doc.Paragraph)
		}
		if p == nil {
			p = &doc.Paragraph{}
			cell.Blocks = append([]doc.Block{p}, cell.Blocks...)
		}
		p.Inlines = append(append([]doc.Inline{}, s.pendingBookmarks...), p.Inlines...)
		s.pendingBookmarks = nil
	}
	s.blocks = append(s.blocks, table)
}

func (r *reader) cell(n *html.Node, c context, header bool) *doc.Cell {
	style := r.styleOf(n)
	cell := doc.NewCell()
	if v, err := strconv.Atoi(strings.TrimSpace(attr(n, "rowspan"))); err == nil {
		cell.RowSpan = max(1, min(1000, v))
	}
	if v, err := strconv.Atoi(strings.TrimSpace(attr(n, "colspan"))); err == nil {
		cell.ColSpan = max(1, min(1000, v))
	}
	cell.Fill = cssColor(attr(n, "bgcolor"))
	if cell.Fill == "" {
		cell.Fill = firstColor(or(style.get("background-color"), style.get("background")))
	}
	switch strings.ToLower(strings.TrimSpace(or(attr(n, "valign"), style.get("vertical-align")))) {
	case "middle", "center":
		cell.VerticalAlign = doc.VAlignCenter
	case "bottom":
		cell.VerticalAlign = doc.VAlignBottom
	}
	cs := &sink{lineStart: true}
	format := charFormat(strings.ToLower(n.Data), n, style, c.format)
	if header && format.Bold == nil {
		format.Bold = doc.Ptr(true)
	}
	align := alignOf(or(attr(n, "align"), style.get("text-align")))
	if align == nil && strings.EqualFold(n.Data, "th") {
		align = doc.Ptr(doc.AlignCenter)
	}
	cc := context{format: format, align: align, depth: c.depth + 1}
	r.bookmark(n, cs)
	r.children(n, cs, cc)
	r.flush(cs, false)
	cell.Blocks = cs.blocks
	if len(cs.pendingBookmarks) > 0 {
		var p *doc.Paragraph
		for _, b := range cell.Blocks {
			if q, ok := b.(*doc.Paragraph); ok {
				p = q
				break
			}
		}
		if p == nil {
			p = &doc.Paragraph{}
			cell.Blocks = append([]doc.Block{p}, cell.Blocks...)
		}
		p.Inlines = append(append([]doc.Inline{}, cs.pendingBookmarks...), p.Inlines...)
	}
	return cell
}

// columnCount is the grid width, taking cells reaching down from rows above into account.
func columnCount(t *doc.Table) int {
	var busy []int
	width := 0
	for _, row := range t.Rows {
		col := 0
		for _, cell := range row.Cells {
			for col < len(busy) && busy[col] > 0 {
				col++
			}
			for c := col; c < col+cell.ColSpan; c++ {
				for len(busy) <= c {
					busy = append(busy, 0)
				}
				busy[c] = cell.RowSpan
			}
			col += cell.ColSpan
		}
		width = max(width, col)
		for c := range busy {
			busy[c] = max(0, busy[c]-1)
		}
	}
	return max(1, width)
}

// relativeWidths: column widths from <col> or the first row, as fractions of the table width.
func (r *reader) relativeWidths(t, firstRow *html.Node, columns int) []float64 {
	var sources []*html.Node
	for _, g := range childElements(t, "colgroup") {
		sources = append(sources, childElements(g, "col")...)
	}
	sources = append(sources, childElements(t, "col")...)
	if len(sources) == 0 && firstRow != nil {
		sources = childElements(firstRow, "td", "th")
	}
	if len(sources) == 0 {
		return nil
	}
	for _, s := range sources {
		if v, err := strconv.Atoi(or(attr(s, "colspan"), attr(s, "span"))); err == nil && v > 1 {
			return nil
		}
	}
	type w struct {
		v       float64
		percent bool
	}
	var widths []w
	for i, s := range sources {
		if i >= columns {
			break
		}
		text := strings.TrimSpace(or(r.styleOf(s).get("width"), attr(s, "width")))
		if strings.HasSuffix(text, "%") {
			if p, err := strconv.ParseFloat(strings.TrimSuffix(text, "%"), 64); err == nil {
				widths = append(widths, w{p, true})
				continue
			}
		}
		l, _ := cssLength(text, baseSize)
		widths = append(widths, w{float64(l), false})
	}
	for len(widths) < columns {
		widths = append(widths, w{})
	}
	allZero, allPercent, allAbs := true, true, true
	total := 0.0
	for _, x := range widths {
		if x.v > 0 {
			allZero = false
		}
		if !(x.percent || x.v <= 0) {
			allPercent = false
		}
		if x.percent || x.v <= 0 {
			allAbs = false
		}
		total += x.v
	}
	switch {
	case allZero:
		return nil
	case allPercent:
		out := make([]float64, len(widths))
		for i, x := range widths {
			out[i] = max(0, x.v) / 100
		}
		return out
	case allAbs:
		out := make([]float64, len(widths))
		for i, x := range widths {
			out[i] = x.v / total
		}
		return out
	}
	return nil
}

// tableBorders: border="0" or CSS border: none turns the grid off.
func tableBorders(t *html.Node, style declarations) *doc.Borders {
	border := strings.ToLower(strings.TrimSpace(or(style.get("border"), style.get("border-style"))))
	if attr(t, "border") == "0" || border == "none" || border == "0" || border == "hidden" {
		b := doc.EmptyBorders
		return &b
	}
	return nil
}

// ---- images ----

func (r *reader) image(e *html.Node, src string, style declarations, s *sink, c context) {
	path := r.resolveImage(src)
	if path == "" {
		return // remote or missing image: skip rather than produce a broken reference
	}
	im := &doc.Image{Path: path, Width: imageLength(or(style.get("width"), attr(e, "width"))), Height: imageLength(or(style.get("height"), attr(e, "height")))}
	r.addInline(im, s, c)
	s.lineStart = false
	s.endsWithSpace = false
}

// imageLength: pixels (width="300"), CSS lengths or percentages; "auto" and em sizes are left to the image.
func imageLength(v string) *int {
	if v == "" || strings.Contains(strings.ToLower(v), "em") {
		return nil
	}
	if l, ok := doc.ParseLength(v); ok && l > 0 {
		return &l
	}
	return nil
}

func (r *reader) resolveImage(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(src), "data:") {
		return r.saveDataURI(src)
	}
	if p := r.localFile(src); p != "" {
		return UsableImage(p, r.opts.MediaFolder)
	}
	return ""
}

// localFile is a local file referenced from the document, or "" for remote and missing files.
func (r *reader) localFile(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "file:") {
		u, err := url.Parse(ref)
		if err != nil {
			return ""
		}
		local := u.Path
		if runtime.GOOS == "windows" {
			local = strings.TrimPrefix(local, "/")
		}
		local = filepath.FromSlash(local)
		if InsideFolder(local, r.opts.ResourceRoot) && isFile(local) {
			return local
		}
		return ""
	}
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") || strings.HasPrefix(lower, "mailto:") {
		return ""
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	rel, err := url.PathUnescape(ref)
	if err != nil {
		rel = ref
	}
	if rel == "" {
		return ""
	}
	candidate, err := filepath.Abs(filepath.Join(r.opts.BaseFolder, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	if InsideFolder(candidate, r.opts.ResourceRoot) && isFile(candidate) {
		return candidate
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// InsideFolder: packaged books may only refer to files in their own folder;
// true when root is "" or path is inside it.
func InsideFolder(path, root string) bool {
	if root == "" {
		return true
	}
	r, err1 := filepath.Abs(root)
	p, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	r = strings.TrimRight(r, `/\`) + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(r))
	}
	return strings.HasPrefix(p, r)
}

// saveDataURI writes the image of a data: URI into the media folder.
func (r *reader) saveDataURI(uri string) string {
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return ""
	}
	header := strings.ToLower(uri[5:comma])
	mime, _, _ := strings.Cut(header, ";")
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/jpg": "jpg", "image/gif": "gif", "image/webp": "webp",
		"image/bmp": "bmp", "image/svg+xml": "svg", "image/avif": "avif"}[mime]
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
	if err := os.MkdirAll(r.opts.MediaFolder, 0o755); err != nil {
		return ""
	}
	r.embedded++
	p := filepath.Join(r.opts.MediaFolder, "embedded-"+randomHex()+"-"+strconv.Itoa(r.embedded)+"."+ext)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return ""
	}
	return UsableImage(p, r.opts.MediaFolder)
}

func randomHex() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// UsableImage: raster pictures are used as they are; SVG is rasterised to
// PNG (the document model wants pixel pictures). "" when the file is no
// picture Filee can read.
func UsableImage(path, mediaFolder string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp":
		return path
	case ".svg", ".svgz":
		if SVGRasterizer == nil {
			return ""
		}
		os.MkdirAll(mediaFolder, 0o755)
		png, err := SVGRasterizer(path, mediaFolder)
		if err != nil {
			return ""
		}
		return png
	}
	// TIFF, AVIF, PSD, …: the image engine reads them, and the writers convert them.
	if _, _, ok := images.PictureSize(path); !ok {
		return ""
	}
	return path
}
