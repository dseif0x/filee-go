package email

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// htmlEncode escapes text like .NET's WebUtility.HtmlEncode.
func htmlEncode(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}

type header struct{ name, value string }

// headers are the ones shown above the body: name and value, only those the message has.
func headers(m *part) []header {
	var out []header
	add := func(name, value string) {
		if strings.TrimSpace(value) != "" {
			out = append(out, header{name, strings.TrimSpace(value)})
		}
	}
	add("From", joinMailboxes(m.addresses("From")))
	add("To", joinMailboxes(m.addresses("To")))
	add("Cc", joinMailboxes(m.addresses("Cc")))
	if t, ok := m.date(); ok {
		add("Date", t.Format("2006-01-02 15:04:05 -07:00"))
	}
	add("Subject", m.get("Subject"))
	var names []string
	for _, a := range attachments(m) {
		names = append(names, a.name)
	}
	if len(names) > 0 {
		add("Attachments", strings.Join(names, ", "))
	}
	return out
}

func joinMailboxes(list []mailbox) string {
	s := make([]string, len(list))
	for i, m := range list {
		s[i] = m.String()
	}
	return strings.Join(s, ", ")
}

// contentSecurityPolicy allows only inline styles and data: pictures, should a browser meet anything the sanitizer let through.
const contentSecurityPolicy = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">`

// messageHTML is a standalone web page: the headers in a table, then the HTML body (or the text body, escaped).
func messageHTML(m *part) string {
	var styles, body string
	if h, ok := m.htmlBody(); ok {
		body, styles = safeHTML(inlinePictures(h, m))
	} else {
		t, _ := m.textBody()
		body = `<pre class="filee-mail-text">` + htmlEncode(t) + "</pre>"
	}
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n")
	sb.WriteString(contentSecurityPolicy + "\n")
	sb.WriteString("<title>" + htmlEncode(m.get("Subject")) + "</title>\n")
	sb.WriteString("<style>\n.filee-mail-headers { border-collapse: collapse; margin: 0 0 12px 0; font-family: sans-serif; font-size: 14px; }\n" +
		".filee-mail-headers th { text-align: left; vertical-align: top; padding: 2px 16px 2px 0; color: #555; font-weight: 600; }\n" +
		".filee-mail-headers td { padding: 2px 0; }\n" +
		".filee-mail-text { white-space: pre-wrap; font-family: inherit; }\n</style>\n")
	sb.WriteString(styles)
	sb.WriteString("</head>\n<body>\n<table class=\"filee-mail-headers\">\n")
	for _, h := range headers(m) {
		sb.WriteString("<tr><th>" + h.name + "</th><td>" + htmlEncode(h.value) + "</td></tr>\n")
	}
	sb.WriteString("</table>\n<hr>\n" + body + "\n</body>\n</html>\n")
	return sb.String()
}

var cidRef = regexp.MustCompile(`(?i)cid:([^"'\s)>]+)`)

// inlinePictures turns "cid:" references into data URIs of the parts with that Content-ID.
func inlinePictures(h string, m *part) string {
	parts := map[string]*part{}
	for _, p := range m.bodyParts() {
		if id := strings.ToLower(p.contentID()); id != "" && p.message == nil {
			if _, dup := parts[id]; !dup {
				parts[id] = p
			}
		}
	}
	if len(parts) == 0 {
		return h
	}
	return cidRef.ReplaceAllStringFunc(h, func(match string) string {
		id := cidRef.FindStringSubmatch(match)[1]
		if u, err := url.PathUnescape(id); err == nil {
			id = u
		}
		p, ok := parts[strings.ToLower(id)]
		if !ok {
			return match
		}
		return "data:" + p.mediaType + ";base64," + base64.StdEncoding.EncodeToString(p.body)
	})
}

var removedElements = map[string]bool{
	"script": true, "noscript": true, "iframe": true, "object": true, "embed": true, "applet": true, "frame": true,
	"frameset": true, "portal": true, "input": true, "button": true, "textarea": true, "select": true, "option": true,
	"meta": true, "link": true, "base": true, "svg": true, "math": true, "template": true, "video": true, "audio": true,
	"source": true, "track": true, "noembed": true, "noframes": true, "xmp": true, "plaintext": true,
}

// safeHTMLAttributes keep readable mail markup.
var safeHTMLAttributes = map[string]bool{
	"class": true, "id": true, "title": true, "lang": true, "dir": true, "style": true, "align": true, "valign": true,
	"width": true, "height": true, "colspan": true, "rowspan": true, "cellpadding": true, "cellspacing": true, "border": true,
	"bgcolor": true, "color": true, "face": true, "size": true, "nowrap": true, "start": true, "type": true, "span": true,
	"scope": true, "headers": true, "abbr": true, "summary": true, "hspace": true, "vspace": true, "clear": true,
	"noshade": true, "frame": true, "rules": true, "open": true,
}

func safeLink(v string) bool {
	if strings.HasPrefix(v, "#") {
		return true
	}
	u, err := url.Parse(v)
	if err != nil || !u.IsAbs() {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

var rasterDataImage = regexp.MustCompile(`(?i)^data:image/([a-z0-9.+-]+);base64,`)

// safeInlineImage: any raster data: image (old Outlook writes image/x-png, image/pjpeg…), never SVG, which can carry script.
func safeInlineImage(v string) bool {
	m := rasterDataImage.FindStringSubmatch(v)
	return m != nil && !strings.HasPrefix(strings.ToLower(m[1]), "svg")
}

// safeHTML keeps readable mail markup while dropping browser-active
// elements, attributes and remote images; it returns the body's markup and
// the document's style elements.
func safeHTML(text string) (body, styles string) {
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return "", ""
	}
	sanitize(root)
	var sb strings.Builder
	var styleNodes []*html.Node
	var find func(*html.Node)
	var bodyNode *html.Node
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" {
			styleNodes = append(styleNodes, n)
		}
		if n.Type == html.ElementNode && n.Data == "body" && bodyNode == nil {
			bodyNode = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	for _, s := range styleNodes {
		html.Render(&sb, s)
	}
	styles = sb.String()
	if bodyNode == nil {
		return "", styles
	}
	for _, s := range styleNodes {
		for p := s.Parent; p != nil; p = p.Parent {
			if p == bodyNode {
				s.Parent.RemoveChild(s)
				break
			}
		}
	}
	var out bytes.Buffer
	for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
		html.Render(&out, c)
	}
	return out.String(), styles
}

func sanitize(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch {
		case c.Type == html.CommentNode, c.Type == html.ElementNode && removedElements[c.Data]:
			// Raw-text elements (noembed, xmp…) and comments go too: browsers read them as text, other HTML readers may not.
			n.RemoveChild(c)
		case c.Type == html.ElementNode && c.Data == "form":
			// Newsletters wrap whole bodies in forms: keep the content, the controls are gone already.
			sanitize(c)
			for gc := c.FirstChild; gc != nil; {
				gn := gc.NextSibling
				c.RemoveChild(gc)
				n.InsertBefore(gc, c)
				gc = gn
			}
			n.RemoveChild(c)
		case c.Type == html.ElementNode:
			if c.Namespace != "" {
				n.RemoveChild(c) // foreign content (SVG, MathML) that slipped through
				break
			}
			var keep []html.Attribute
			for _, a := range c.Attr {
				name := strings.ToLower(a.Key)
				if a.Namespace == "" && (safeHTMLAttributes[name] ||
					name == "alt" && c.Data == "img" ||
					name == "href" && c.Data == "a" && safeLink(a.Val) ||
					name == "src" && c.Data == "img" && safeInlineImage(a.Val)) {
					keep = append(keep, a)
				}
			}
			c.Attr = keep
			sanitize(c)
		}
		c = next
	}
}

// ---- text ----

var (
	headBlock      = regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`)
	styleBlock     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	scriptBlock    = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	lineBreakTag   = regexp.MustCompile(`(?i)<br\s*/?>`)
	blockEndTag    = regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6]|table|blockquote)>`)
	anyTag         = regexp.MustCompile(`<[^>]+>`)
	spaceRun       = regexp.MustCompile(`[ \t]+`)
	multipleBlanks = regexp.MustCompile(`\n{3,}`)
)

// messageText is the headers, an empty line and the text body (or the HTML body as text).
func messageText(m *part) string {
	var sb strings.Builder
	for _, h := range headers(m) {
		sb.WriteString(h.name + ": " + h.value + "\r\n")
	}
	sb.WriteString("\r\n")
	text, ok := m.textBody()
	if !ok {
		if h, ok := m.htmlBody(); ok {
			text = htmlToText(h)
		}
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	sb.WriteString(strings.TrimRightFunc(text, unicode.IsSpace) + "\r\n")
	return sb.String()
}

// htmlToText is the rough text of an HTML body: blocks and line breaks become new lines, tags go, entities are decoded.
func htmlToText(h string) string {
	t := scriptBlock.ReplaceAllString(h, "")
	t = styleBlock.ReplaceAllString(t, "")
	t = headBlock.ReplaceAllString(t, "")
	t = lineBreakTag.ReplaceAllString(t, "\n")
	t = blockEndTag.ReplaceAllString(t, "\n")
	t = anyTag.ReplaceAllString(t, "")
	t = strings.ReplaceAll(html.UnescapeString(t), " ", " ")
	lines := strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(multipleBlanks.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// ---- attachments ----

type attachment struct {
	name string
	p    *part
}

// attachments are the attached files and messages, each with a unique, safe file name.
func attachments(m *part) []attachment {
	var out []attachment
	used := map[string]bool{}
	for _, p := range m.bodyParts() {
		var name string
		switch {
		case p.message != nil || p.mediaType == "message/rfc822":
			name = p.fileName()
			if name == "" && p.message != nil {
				name = p.message.get("Subject")
			}
			if name == "" {
				name = "message"
			}
			name += ".eml"
		case p.isAttachment() || p.fileName() != "" && p.contentID() == "":
			name = p.fileName()
			if name == "" {
				ext := ".bin"
				if exts, _ := mime.ExtensionsByType(p.mediaType); len(exts) > 0 {
					ext = exts[0]
				}
				name = "attachment" + ext
			}
		default:
			continue
		}
		name = safeName(name)
		unique := name
		ext := filepath.Ext(name)
		for i := 2; used[strings.ToLower(unique)]; i++ {
			unique = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
		}
		used[strings.ToLower(unique)] = true
		out = append(out, attachment{unique, p})
	}
	return out
}

// safeName is a file name without folders or characters Windows does not allow.
func safeName(name string) string {
	parts := strings.Split(strings.ReplaceAll(name, `\`, "/"), "/")
	name = parts[len(parts)-1]
	var sb strings.Builder
	for _, r := range name {
		if strings.ContainsRune(`<>:"/\|?*`, r) || unicode.IsControl(r) {
			sb.WriteByte('_')
		} else {
			sb.WriteRune(r)
		}
	}
	name = strings.Trim(sb.String(), " .")
	if name == "" {
		return "attachment"
	}
	if r := []rune(name); len(r) > 150 {
		return string(r[:150])
	}
	return name
}

func writeAttachmentsZip(list []attachment, path string) error {
	temp := path + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	z := zip.NewWriter(f)
	for _, a := range list {
		w, err := z.CreateHeader(&zip.FileHeader{Name: a.name, Method: zip.Deflate})
		if err != nil {
			f.Close()
			return err
		}
		data := a.p.body
		if a.p.mediaType == "message/rfc822" || a.p.message != nil {
			data = a.p.raw
		}
		if _, err := w.Write(data); err != nil {
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	os.Remove(path)
	return os.Rename(temp, path)
}
