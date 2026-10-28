package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/htmldoc"
)

// MHTML web archives (.mhtml / .mht, RFC 2557): a web page saved with its
// pictures and style sheets in one MIME message. Chrome and Edge write
// multipart/related with the page first, pictures named by Content-Location
// (the URL they came from) and style sheets as "cid:…@mhtml.blink" parts;
// Word and Internet Explorer write .mht files whose parts sit at
// file:///C:/… locations next to the page. The page becomes one standalone
// HTML file: linked style sheets are inlined, pictures of the archive become
// data: URIs, and the e-mail sanitizer drops scripts, frames, forms and
// anything that would load from elsewhere. Nothing is fetched: a reference
// the archive does not hold is removed, and Content-Location values are only
// compared as text, never opened or written as files.

const (
	maxMhtmlBytes    = 256 << 20 // larger archives are refused
	maxPictureBytes  = 32 << 20  // a larger picture (decoded) is left out
	maxEmbeddedChars = 192 << 20 // the data URIs of one page together
	maxStyleChars    = 8 << 20   // style sheet text inlined into one page
	maxImportDepth   = 4
)

func loadMhtml(path string) (*part, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxMhtmlBytes {
		return nil, fmt.Errorf("the web archive is larger than %d MB", maxMhtmlBytes>>20)
	}
	return loadMessage(path)
}

// readMhtmlDocument reads an archive into the document model (MHTML → DOCX, HWPX and on).
func readMhtmlDocument(path, media string) (*doc.Document, error) {
	m, err := loadMhtml(path)
	if err != nil {
		return nil, err
	}
	page, err := mhtmlPage(m)
	if err != nil {
		return nil, err
	}
	// The page refers to nothing but data: URIs; no local file is looked up outside the job's media folder.
	c := htmldoc.Parse(page, htmldoc.ReadOptions{BaseFolder: media, MediaFolder: media, ResourceRoot: media}, false)
	return htmldoc.ToDocument(c), nil
}

func isHTMLPart(p *part) bool {
	return p.mediaType == "text/html" || p.mediaType == "application/xhtml+xml"
}

// pageOf is the root of multipart/related ("start" parameter or the first part), else the first HTML part.
func pageOf(m *part) *part {
	if m.mediaType == "multipart/related" {
		if root := m.relatedRoot(); root != nil && isHTMLPart(root) && len(root.children) == 0 {
			return root
		}
	}
	for _, p := range m.bodyParts() {
		if isHTMLPart(p) {
			return p
		}
	}
	return nil
}

// partText decodes a text part with its charset parameter; without one (or
// a wrong "us-ascii" over 8-bit text) the page's BOM, its own declaration or UTF-8 decide.
func partText(p *part) string {
	cs := strings.TrimSpace(p.params["charset"])
	bom := bytes.HasPrefix(p.body, []byte{0xEF, 0xBB, 0xBF}) || bytes.HasPrefix(p.body, []byte{0xFF, 0xFE}) || bytes.HasPrefix(p.body, []byte{0xFE, 0xFF})
	ascii := strings.EqualFold(cs, "us-ascii") || strings.EqualFold(cs, "ascii")
	high := bytes.IndexFunc(p.body, func(r rune) bool { return r >= 0x80 || r == 0xFFFD }) >= 0
	if cs != "" && !bom && !(ascii && high) {
		if e := charsetEncoding(cs); e != nil {
			if s, err := e.NewDecoder().Bytes(p.body); err == nil {
				return strings.TrimPrefix(string(s), "\uFEFF")
			}
		} else if strings.EqualFold(cs, "utf-8") || strings.EqualFold(cs, "utf8") || ascii {
			return strings.TrimPrefix(string(p.body), "\uFEFF")
		}
	}
	return htmldoc.Decode(p.body)
}

var (
	uriScheme   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	languageTag = regexp.MustCompile(`^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*$`)
	cssImport   = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*)?["']?([^"')\s;]+)["']?\s*\)?[^;{}]*;?`)
	cssURL      = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s"']*))\s*\)`)
	importMark  = regexp.MustCompile("\x00([0-9]+)\x00")
	whitespaceR = regexp.MustCompile(`\s+`)
)

// absolute is ref as an absolute URI: as written when it has a scheme, else against base.
// Only ever compared with Content-Location values, never opened.
func absolute(ref string, base *url.URL) *url.URL {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if uriScheme.MatchString(ref) {
		u, err := url.Parse(ref)
		if err != nil || !u.IsAbs() {
			return nil
		}
		return u
	}
	if base == nil {
		return nil
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil
	}
	return base.ResolveReference(r)
}

// key is the URI without its fragment, for lookups.
func key(u *url.URL) string {
	c := *u
	c.Fragment, c.RawFragment = "", ""
	return strings.ToLower(c.String())
}

type archive struct {
	byLocation map[string]*part
	byID       map[string]*part
	pictures   map[*part]string
	embedded   int
	styles     int
	location   *url.URL
}

func location(p *part) string {
	return whitespaceR.ReplaceAllString(p.header.Get("Content-Location"), "")
}

func newArchive(m, page *part) *archive {
	a := &archive{byLocation: map[string]*part{}, byID: map[string]*part{}, pictures: map[*part]string{}}
	a.location = absolute(location(page), nil)
	for _, p := range m.bodyParts() {
		if id := strings.ToLower(p.contentID()); id != "" {
			if _, dup := a.byID[id]; !dup {
				a.byID[id] = p
			}
		}
		loc := location(p)
		if loc == "" {
			continue
		}
		if _, dup := a.byLocation[strings.ToLower(loc)]; !dup {
			a.byLocation[strings.ToLower(loc)] = p
		}
		if abs := absolute(loc, a.location); abs != nil {
			if _, dup := a.byLocation[key(abs)]; !dup {
				a.byLocation[key(abs)] = p
			}
		}
	}
	return a
}

// find is the part a reference names: its Content-Location as written or resolved, or "cid:" its Content-ID.
func (a *archive) find(ref string, base *url.URL) *part {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if p, ok := a.byLocation[strings.ToLower(ref)]; ok {
		return p
	}
	if strings.HasPrefix(strings.ToLower(ref), "cid:") {
		id, err := url.PathUnescape(ref[4:])
		if err != nil {
			id = ref[4:]
		}
		return a.byID[strings.ToLower(strings.Trim(id, "<>"))]
	}
	if abs := absolute(ref, base); abs != nil {
		return a.byLocation[key(abs)]
	}
	return nil
}

// picture is a data URI of the raster picture a reference names, "" when
// the archive does not hold one (remote, SVG, too large, or the page's
// picture budget is spent). Data URIs already in the page are kept when raster.
func (a *archive) picture(ref string, base *url.URL) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "data:") {
		if safeInlineImage(ref) {
			return a.spend(ref)
		}
		return ""
	}
	p := a.find(ref, base)
	if p == nil {
		return ""
	}
	uri, ok := a.pictures[p]
	if !ok {
		uri = dataURI(p)
		a.pictures[p] = uri
	}
	return a.spend(uri)
}

func (a *archive) spend(uri string) string {
	if uri == "" || a.embedded+len(uri) > maxEmbeddedChars {
		return ""
	}
	a.embedded += len(uri)
	return uri
}

func dataURI(p *part) string {
	declared := p.mediaType
	if !strings.HasPrefix(declared, "image/") && declared != "application/octet-stream" {
		return ""
	}
	if len(p.body) == 0 || len(p.body) > maxPictureBytes {
		return ""
	}
	typ := sniffPicture(p.body)
	if typ == "" && strings.HasPrefix(declared, "image/") && !strings.Contains(declared, "svg") {
		typ = declared
	}
	if typ == "" {
		return ""
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(p.body)
}

// sniffPicture is the picture type from the first bytes, for parts saved as application/octet-stream or mislabelled.
func sniffPicture(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF8")):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case bytes.HasPrefix(b, []byte("BM")):
		return "image/bmp"
	case len(b) >= 12 && string(b[4:12]) == "ftypavif":
		return "image/avif"
	}
	return ""
}

// styleSheet is the style sheet a reference names, its url()s rewritten (see css), or "".
func (a *archive) styleSheet(ref string, base *url.URL, depth int) (string, bool) {
	if depth > maxImportDepth {
		return "", false
	}
	p := a.find(ref, base)
	if p == nil || p.mediaType != "text/css" {
		return "", false
	}
	// Chrome names style sheets "cid:…", which is no base: their url()s are absolute, else read like the page's.
	sheetBase := base
	if loc := absolute(location(p), a.location); loc != nil {
		switch strings.ToLower(loc.Scheme) {
		case "http", "https", "file", "ftp":
			sheetBase = loc
		}
	}
	return a.css(partText(p), sheetBase, depth), true
}

// css replaces @import with the imported sheet when the archive holds it,
// url() of archived raster pictures with data URIs and every other url()
// with "none", so a browser loads nothing. "<" is escaped so the text cannot
// close its <style> element.
func (a *archive) css(css string, base *url.URL, depth int) string {
	if css == "" {
		return css
	}
	a.styles += len(css)
	if a.styles > maxStyleChars {
		return ""
	}
	var imported []string
	css = cssImport.ReplaceAllStringFunc(strings.ReplaceAll(css, "\x00", "\uFFFD"), func(m string) string {
		sub := cssImport.FindStringSubmatch(m)
		s, _ := a.styleSheet(sub[1], base, depth+1)
		imported = append(imported, s)
		return "\x00" + strconv.Itoa(len(imported)-1) + "\x00"
	})
	css = cssURL.ReplaceAllStringFunc(css, func(m string) string {
		sub := cssURL.FindStringSubmatch(m)
		ref := sub[1] + sub[2] + sub[3]
		if d := a.picture(ref, base); d != "" {
			return `url("` + d + `")`
		}
		return "none"
	})
	css = strings.ReplaceAll(css, "<", `\3c `)
	if len(imported) == 0 {
		return css
	}
	return importMark.ReplaceAllStringFunc(css, func(m string) string {
		i, _ := strconv.Atoi(importMark.FindStringSubmatch(m)[1])
		return imported[i]
	})
}

func srcset(v string) []string {
	var out []string
	for _, c := range strings.Split(v, ",") {
		if f := strings.Fields(c); len(f) > 0 {
			out = append([]string{f[0]}, out...) // largest first: the last is usually the sharpest
		}
	}
	return out
}

func getAttr(n *html.Node, k string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, k) {
			return a.Val, true
		}
	}
	return "", false
}

func setAttr(n *html.Node, k, v string) {
	for i, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, k) {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: k, Val: v})
}

func removeAttr(n *html.Node, k string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if !(a.Namespace == "" && strings.EqualFold(a.Key, k)) {
			out = append(out, a)
		}
	}
	n.Attr = out
}

func elementsOf(n *html.Node, visit func(*html.Node)) {
	var list []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			list = append(list, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	for _, e := range list {
		visit(e)
	}
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return sb.String()
}

func setText(n *html.Node, s string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		c = next
	}
	n.AppendChild(&html.Node{Type: html.TextNode, Data: s})
}

// mhtmlPage is the saved page as one HTML file: style sheets inline, pictures as data URIs, nothing active or remote.
func mhtmlPage(m *part) (string, error) {
	page := pageOf(m)
	if page == nil {
		return "", errors.New("the web archive holds no HTML page")
	}
	a := newArchive(m, page)
	root, err := html.Parse(strings.NewReader(partText(page)))
	if err != nil {
		return "", err
	}
	base := a.location
	elementsOf(root, func(e *html.Node) {
		if e.Data == "base" && base == a.location {
			if href, ok := getAttr(e, "href"); ok {
				if d := absolute(href, a.location); d != nil {
					base = d
				}
			}
		}
	})
	elementsOf(root, func(e *html.Node) {
		switch e.Data {
		case "style":
			setText(e, a.css(nodeText(e), base, 0))
		case "link":
			href, ok := getAttr(e, "href")
			rel, _ := getAttr(e, "rel")
			isSheet := false
			for _, r := range strings.Fields(rel) {
				if strings.EqualFold(r, "stylesheet") {
					isSheet = true
				}
			}
			if !ok || !isSheet {
				return
			}
			css, ok := a.styleSheet(href, base, 0)
			if !ok {
				return
			}
			media, _ := getAttr(e, "media")
			media = strings.TrimSpace(media)
			if media != "" && !strings.EqualFold(media, "all") {
				css = "@media " + strings.ReplaceAll(media, "<", `\3c `) + " {\n" + css + "\n}"
			}
			style := &html.Node{Type: html.ElementNode, Data: "style"}
			style.AppendChild(&html.Node{Type: html.TextNode, Data: css})
			e.Parent.InsertBefore(style, e)
			e.Parent.RemoveChild(e)
		}
	})
	elementsOf(root, func(e *html.Node) {
		if v, ok := getAttr(e, "style"); ok {
			setAttr(e, "style", a.css(v, base, 0))
		}
		if v, ok := getAttr(e, "background"); ok {
			// Old pages and Word put the background picture in an attribute the sanitizer drops: move it into CSS.
			if pic := a.picture(v, base); pic != "" {
				old, _ := getAttr(e, "style")
				setAttr(e, "style", `background-image: url("`+pic+`"); `+old)
			}
		}
		switch e.Data {
		case "img":
			var candidates []string
			if v, ok := getAttr(e, "src"); ok {
				candidates = append(candidates, v)
			}
			if v, ok := getAttr(e, "srcset"); ok {
				candidates = append(candidates, srcset(v)...)
			}
			if p := e.Parent; p != nil && p.Type == html.ElementNode && p.Data == "picture" {
				for c := p.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "source" {
						if v, ok := getAttr(c, "srcset"); ok {
							candidates = append(candidates, srcset(v)...)
						}
					}
				}
			}
			found := ""
			for _, c := range candidates {
				if found = a.picture(c, base); found != "" {
					break
				}
			}
			if found != "" {
				setAttr(e, "src", found)
			} else {
				removeAttr(e, "src")
			}
		case "a":
			href, ok := getAttr(e, "href")
			href = strings.TrimSpace(href)
			if !ok || strings.HasPrefix(href, "#") {
				return
			}
			target := absolute(href, base)
			if target == nil {
				return
			}
			if a.location != nil && key(target) == key(a.location) {
				if target.Fragment != "" {
					setAttr(e, "href", "#"+target.Fragment)
				} else {
					setAttr(e, "href", "#")
				}
			} else if s := strings.ToLower(target.Scheme); s == "http" || s == "https" || s == "mailto" {
				setAttr(e, "href", target.String())
			}
		}
	})

	title := ""
	language := ""
	var bodyAttrs strings.Builder
	elementsOf(root, func(e *html.Node) {
		switch e.Data {
		case "title":
			if title == "" {
				title = strings.TrimSpace(nodeText(e))
			}
		case "html":
			if l, ok := getAttr(e, "lang"); ok && languageTag.MatchString(l) {
				language = l
			}
		case "body":
			if bodyAttrs.Len() == 0 {
				for _, at := range e.Attr {
					if at.Namespace == "" && safeHTMLAttributes[strings.ToLower(at.Key)] {
						bodyAttrs.WriteString(" " + at.Key + `="` + htmlEncode(at.Val) + `"`)
					}
				}
			}
		}
	})
	if title == "" {
		title = strings.TrimSpace(m.get("Subject"))
	}
	var full bytes.Buffer
	html.Render(&full, root)
	body, styles := safeHTML(full.String())

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html")
	if language != "" {
		sb.WriteString(` lang="` + language + `"`)
	}
	sb.WriteString(">\n<head>\n<meta charset=\"utf-8\">\n" + contentSecurityPolicy + "\n")
	sb.WriteString("<title>" + htmlEncode(title) + "</title>\n")
	sb.WriteString(styles)
	sb.WriteString("\n</head>\n<body" + bodyAttrs.String() + ">\n")
	sb.WriteString(body)
	sb.WriteString("\n</body>\n</html>\n")
	return sb.String(), nil
}
