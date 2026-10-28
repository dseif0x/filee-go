package ebook

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// KEPUB, the EPUB flavour Kobo e-readers read with their own engine: an EPUB
// whose XHTML chapters mark every sentence as <span class="koboSpan"
// id="kobo.P.S">, P counting the paragraphs (block elements) of the chapter
// and S the sentences in it; pictures get a span of their own. The body's
// content is wrapped in <div id="book-columns"><div id="book-inner">.
// Files are named "name.kepub.epub" so Kobo devices pick the KEPUB engine.
//
// Written from the public descriptions of the format (MobileRead wiki,
// kepubify's documentation). EPUB → KEPUB changes only the spine's XHTML
// documents; every other file is copied unchanged. KEPUB → EPUB removes the
// markup again.

const (
	koboSpanClass = "koboSpan"
	koboColumns   = "book-columns"
	koboInner     = "book-inner"
)

var koboSkipped = map[string]bool{"script": true, "style": true, "textarea": true, "select": true, "option": true, "noscript": true, "head": true, "title": true}

var koboBlocks = map[string]bool{
	"body": true, "p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "li": true,
	"ol": true, "ul": true, "dl": true, "dt": true, "dd": true, "blockquote": true, "pre": true, "td": true, "th": true,
	"tr": true, "table": true, "caption": true, "figure": true, "figcaption": true, "section": true, "article": true,
	"aside": true, "header": true, "footer": true, "nav": true, "address": true, "main": true, "hr": true,
}

// kepubFromEpub copies an EPUB as a KEPUB.
func kepubFromEpub(ctx context.Context, epub, output string) error {
	return rewriteEpub(ctx, epub, output, addKoboSpans)
}

// kepubToEpub copies a KEPUB as a plain EPUB without Kobo's markup.
func kepubToEpub(ctx context.Context, kepub, output string) error {
	return rewriteEpub(ctx, kepub, output, removeKoboSpans)
}

// rewriteEpub copies the book, passing each XHTML document of the spine
// through transform (false keeps it as it was). "mimetype" is written first
// and stored, as EPUB requires.
func rewriteEpub(ctx context.Context, input, output string, transform func(*xdom.Node) bool) error {
	src, err := openEpubZip(input)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := checkDRM(&src.Reader); err != nil {
		return err
	}
	documents := contentDocuments(&src.Reader)
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			os.Remove(output)
		}
	}()
	defer f.Close()
	z := zip.NewWriter(f)
	if err := zipText(z, "mimetype", "application/epub+zip", zip.Store); err != nil {
		return err
	}
	for _, e := range src.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.Name == "mimetype" || strings.HasSuffix(e.Name, "/") {
			continue
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if documents[e.Name] {
			if root, prolog, err := parseXML(data); err == nil && transform(root) {
				data = writeXML(root, prolog)
			}
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: e.Modified})
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	ok = true
	return f.Close()
}

// contentDocuments are the ZIP entry names of the spine's XHTML documents;
// every .xhtml / .html file when the package can't be read.
func contentDocuments(z *zip.Reader) map[string]bool {
	names := map[string]bool{}
	read := func(name string) *xdom.Node {
		for _, f := range z.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return nil
				}
				b, _ := io.ReadAll(io.LimitReader(rc, 64<<20))
				rc.Close()
				root, _, err := parseXML(b)
				if err != nil {
					return nil
				}
				return root
			}
		}
		return nil
	}
	opfPath := ""
	if c := read("META-INF/container.xml"); c != nil {
		for _, rf := range c.DescendantsLocal("rootfile") {
			if fp := rf.A("full-path"); fp != "" {
				opfPath = strings.TrimLeft(unescapePath(fp), "/")
				break
			}
		}
	}
	if opfPath == "" {
		for _, f := range z.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".opf") {
				opfPath = f.Name
				break
			}
		}
	}
	if pkg := read(opfPath); pkg != nil {
		folder := ""
		if i := strings.LastIndexByte(opfPath, '/'); i >= 0 {
			folder = opfPath[:i+1]
		}
		type item struct{ path, mt string }
		manifest := map[string]item{}
		for _, it := range pkg.DescendantsLocal("item") {
			id, href := it.A("id"), it.A("href")
			if id == "" || href == "" {
				continue
			}
			if p := resolveEntry(folder, unescapePath(strings.Split(href, "#")[0])); p != "" {
				if _, dup := manifest[id]; !dup {
					manifest[id] = item{p, strings.ToLower(strings.TrimSpace(it.A("media-type")))}
				}
			}
		}
		for _, ref := range pkg.DescendantsLocal("itemref") {
			if it, ok := manifest[ref.A("idref")]; ok && (it.mt == "application/xhtml+xml" || it.mt == "text/html" || isHTMLName(it.path)) {
				names[it.path] = true
			}
		}
	}
	if len(names) == 0 {
		for _, f := range z.File {
			if isHTMLName(f.Name) {
				names[f.Name] = true
			}
		}
	}
	return names
}

func isHTMLName(n string) bool {
	l := strings.ToLower(n)
	return strings.HasSuffix(l, ".xhtml") || strings.HasSuffix(l, ".html") || strings.HasSuffix(l, ".htm")
}

// resolveEntry: "OEBPS/" + "../Text/ch1.xhtml" → "Text/ch1.xhtml"; "" when it leaves the book.
func resolveEntry(folder, href string) string {
	var parts []string
	for _, p := range strings.Split(strings.ReplaceAll(folder+href, `\`, "/"), "/") {
		switch p {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return ""
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

func isKoboSpan(n *xdom.Node) bool {
	return !n.IsText() && n.Name.Local == "span" && contains(strings.Fields(n.A("class")), koboSpanClass)
}

func isKoboWrapper(n *xdom.Node) bool {
	id := n.A("id")
	return !n.IsText() && n.Name.Local == "div" && (id == koboColumns || id == koboInner)
}

// addKoboSpans adds Kobo's sentence spans and wrapper divs; false for a document that has them already.
func addKoboSpans(root *xdom.Node) bool {
	bodies := root.DescendantsLocal("body")
	if len(bodies) == 0 {
		return false
	}
	has := false
	root.Walk(func(n *xdom.Node) bool {
		if isKoboSpan(n) {
			has = true
		}
		return !has
	})
	if has {
		return false
	}
	body := bodies[0]
	st := &spanState{ns: body.Name.Space}
	visitKobo(body, st)
	inner := xdom.NewElement(st.ns, "div")
	inner.SetAttr("", "id", koboInner)
	for _, c := range body.Children {
		inner.Add(c)
	}
	columns := xdom.NewElement(st.ns, "div")
	columns.SetAttr("", "id", koboColumns)
	columns.Add(inner)
	body.Children = nil
	body.Add(columns)
	return true
}

type spanState struct {
	ns                  string
	block               *xdom.Node
	paragraph, sentence int
}

// nextID is a new span id, starting a new paragraph number when block changed.
func (s *spanState) nextID(block *xdom.Node) string {
	if block != s.block {
		s.block = block
		s.paragraph++
		s.sentence = 0
	}
	s.sentence++
	return "kobo." + itoa(s.paragraph) + "." + itoa(s.sentence)
}

func visitKobo(e *xdom.Node, st *spanState) {
	children := append([]*xdom.Node(nil), e.Children...)
	var out []*xdom.Node
	for _, c := range children {
		switch {
		case c.IsText() && strings.TrimSpace(c.Text) != "":
			out = append(out, wrapSentences(c, e, st)...)
		case c.IsText():
			out = append(out, c)
		case c.Name.Space != st.ns || koboSkipped[strings.ToLower(c.Name.Local)]:
			out = append(out, c) // SVG, MathML, scripts, …
		case strings.EqualFold(c.Name.Local, "img"):
			out = append(out, koboSpan(st, blockOf(e), c))
		default:
			visitKobo(c, st)
			out = append(out, c)
		}
	}
	e.Children = nil
	for _, c := range out {
		e.Add(c)
	}
}

// wrapSentences replaces a text node with one span per sentence (whitespace before the first stays outside).
func wrapSentences(t, parent *xdom.Node, st *spanState) []*xdom.Node {
	v := t.Text
	start := 0
	for start < len(v) {
		r := rune(v[start])
		if r >= 0x80 || !unicode.IsSpace(r) {
			break
		}
		start++
	}
	block := blockOf(parent)
	var out []*xdom.Node
	if start > 0 {
		out = append(out, &xdom.Node{Text: v[:start]})
	}
	for _, s := range sentences(v[start:]) {
		out = append(out, koboSpan(st, block, &xdom.Node{Text: s}))
	}
	return out
}

func koboSpan(st *spanState, block, content *xdom.Node) *xdom.Node {
	span := xdom.NewElement(st.ns, "span")
	span.SetAttr("", "class", koboSpanClass)
	span.SetAttr("", "id", st.nextID(block))
	span.Add(content)
	return span
}

func blockOf(n *xdom.Node) *xdom.Node {
	for p := n; p != nil; p = p.Parent {
		if koboBlocks[strings.ToLower(p.Name.Local)] || p.Parent == nil {
			return p
		}
	}
	return n
}

func isSentenceEnd(r rune) bool { return strings.ContainsRune(".!?…。！？", r) }

func isCloser(r rune) bool { return strings.ContainsRune("\"')]”’」』）", r) }

// sentences splits text after sentence ends: . ! ? … (with closing quotes and
// brackets) followed by whitespace, which stays with the sentence, and the
// full-width 。！？ of Chinese and Japanese, which need no space.
func sentences(text string) []string {
	r := []rune(text)
	var out []string
	start, i := 0, 0
	for i < len(r) {
		c := r[i]
		if !isSentenceEnd(c) {
			i++
			continue
		}
		end := i + 1
		for end < len(r) && (isSentenceEnd(r[end]) || isCloser(r[end])) {
			end++
		}
		wide := c == '。' || c == '！' || c == '？'
		if end < len(r) && unicode.IsSpace(r[end]) {
			for end < len(r) && unicode.IsSpace(r[end]) {
				end++
			}
		} else if !wide {
			i = end
			continue // "3.14", "e.g.x", an ellipsis inside a sentence
		}
		if end < len(r) {
			out = append(out, string(r[start:end]))
			start = end
		}
		i = end
	}
	if start < len(r) {
		out = append(out, string(r[start:]))
	}
	return out
}

// removeKoboSpans removes Kobo's spans and wrapper divs; false when there are none.
func removeKoboSpans(root *xdom.Node) bool {
	changed := false
	var unwrap func(e *xdom.Node)
	unwrap = func(e *xdom.Node) {
		var out []*xdom.Node
		for _, c := range e.Children {
			if c.IsText() {
				out = append(out, c)
				continue
			}
			unwrap(c)
			if isKoboSpan(c) || isKoboWrapper(c) {
				changed = true
				out = append(out, c.Children...)
				continue
			}
			out = append(out, c)
		}
		e.Children = nil
		for _, c := range out {
			// Neighbouring texts join, as they were before the spans.
			if c.IsText() && len(e.Children) > 0 && e.Children[len(e.Children)-1].IsText() {
				e.Children[len(e.Children)-1].Text += c.Text
				continue
			}
			e.Add(c)
		}
	}
	unwrap(root)
	return changed
}
