package data

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dseif0x/filee-go/internal/engines/subtitles"
)

// XML ↔ the data tree in the widespread "@attribute / #text" convention (as
// xmltodict and Json.NET):
//   - the root element becomes an object with one key, its name;
//   - an element with only text becomes that text; an empty element (<a/>)
//     becomes null, <a></a> empty text;
//   - attributes become "@name" keys, text next to attributes or child elements
//     becomes "#text" (mixed content is joined into one text); child elements
//     become keys, and repeated ones an array (a single child is no array);
//   - XML has no types: values read are text. Comments, processing instructions
//     and the DTD are dropped; external entities are never loaded.
// Writing does the reverse. A root that is not one object with one key goes
// into <root>, array items without a name into <item>; names that are not
// valid XML names are escaped as .NET's XmlConvert does ("first name" →
// first_x0020_name) and unescaped when read back. Prefixed names ("x:a") keep
// their namespace when an "@xmlns:x" key declares it.

const (
	xmlRootName = "root"
	xmlItemName = "item"
	xmlTextKey  = "#text"
	xmlNS       = "http://www.w3.org/XML/1998/namespace"
)

type xmlNode struct {
	name     string
	attrs    []xml.Attr
	children []*xmlNode
	// texts are the element's own text nodes; all is every text inside it.
	texts []string
	parts []any // text (string) and child (*xmlNode) in document order
	empty bool
}

func (n *xmlNode) value() string {
	var b strings.Builder
	var walk func(*xmlNode)
	walk = func(n *xmlNode) {
		for _, p := range n.parts {
			switch x := p.(type) {
			case string:
				b.WriteString(x)
			case *xmlNode:
				walk(x)
			}
		}
	}
	walk(n)
	return b.String()
}

var reXMLEncoding = regexp.MustCompile(`^<\?xml[^>]*encoding\s*=\s*["']([A-Za-z0-9_.:-]+)["']`)

// xmlText decodes an XML file to UTF-8: by its byte order mark, by the encoding
// its declaration names (<?xml encoding="EUC-KR"?>), else UTF-8.
func xmlText(b []byte) (string, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}), bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return subtitles.DecodeWith(b, 1252, false), nil
	case len(b) >= 4 && b[0] == '<' && b[1] == 0 && b[2] == '?' && b[3] == 0:
		return subtitles.DecodeWith(append([]byte{0xFF, 0xFE}, b...), 1252, false), nil
	case len(b) >= 4 && b[0] == 0 && b[1] == '<' && b[2] == 0 && b[3] == '?':
		return subtitles.DecodeWith(append([]byte{0xFE, 0xFF}, b...), 1252, false), nil
	}
	if m := reXMLEncoding.FindSubmatch(b[:min(len(b), 512)]); m != nil {
		if e := subtitles.Charset(string(m[1])); e != nil && !subtitles.IsUnicode(e) {
			out, err := e.NewDecoder().Bytes(b)
			if err != nil {
				return "", err
			}
			return string(out), nil
		}
	}
	if !utf8.Valid(b) {
		return "", errors.New("Invalid character in the given encoding.")
	}
	return string(b), nil
}

func readXML(b []byte) (any, error) {
	s, err := xmlText(b)
	if err != nil {
		return nil, fmt.Errorf("Not valid XML: %v", err)
	}
	root, err := parseXML(s)
	if err != nil {
		return nil, fmt.Errorf("Not valid XML: %v", err)
	}
	if root == nil {
		return nil, errors.New("The XML file has no root element.")
	}
	m := NewMap()
	v, err := xmlElement(root, 0)
	if err != nil {
		return nil, err
	}
	m.Set(root.name, v)
	return m, nil
}

func xmlName(n xml.Name) string {
	local := decodeXMLName(n.Local)
	if n.Space == "" {
		return local
	}
	return n.Space + ":" + local
}

func parseXML(s string) (*xmlNode, error) {
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = true
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var root *xmlNode
	var stack []*xmlNode
	var raw []xml.Name
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 && root != nil {
				return nil, errors.New("there are multiple root elements")
			}
			if len(stack) > MaxDepth+1 {
				return nil, errors.New("The XML is nested too deeply.")
			}
			off := int(d.InputOffset())
			n := &xmlNode{name: xmlName(t.Name), attrs: t.Attr, empty: off >= 2 && s[off-2:off] == "/>"}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
				p.parts = append(p.parts, n)
			} else {
				root = n
			}
			stack = append(stack, n)
			raw = append(raw, t.Name)
		case xml.EndElement:
			if len(stack) == 0 || raw[len(raw)-1] != t.Name {
				return nil, fmt.Errorf("the end tag </%s> does not match", xmlName(t.Name))
			}
			stack, raw = stack[:len(stack)-1], raw[:len(raw)-1]
		case xml.CharData:
			txt := string(t)
			if len(stack) == 0 {
				if strings.TrimSpace(txt) != "" {
					return nil, errors.New("text outside the root element")
				}
				continue
			}
			// Insignificant white space between elements is dropped, as XDocument does.
			if strings.TrimSpace(txt) == "" {
				continue
			}
			p := stack[len(stack)-1]
			p.texts = append(p.texts, txt)
			p.parts = append(p.parts, txt)
		}
	}
	if len(stack) > 0 {
		return nil, errors.New("unexpected end of the file")
	}
	return root, nil
}

func xmlElement(e *xmlNode, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, errors.New("The XML is nested too deeply.")
	}
	if len(e.attrs) == 0 && len(e.children) == 0 {
		if e.empty {
			return nil, nil
		}
		return e.value(), nil
	}
	m := NewMap()
	for _, a := range e.attrs {
		m.Set("@"+xmlName(a.Name), a.Value)
	}
	if len(e.children) == 0 {
		if !e.empty {
			m.Set(xmlTextKey, e.value())
		}
		return m, nil
	}
	var texts []string
	for _, t := range e.texts {
		if t = strings.TrimSpace(t); t != "" {
			texts = append(texts, t)
		}
	}
	if len(texts) > 0 {
		m.Set(xmlTextKey, strings.Join(texts, " "))
	}
	for _, c := range e.children {
		v, err := xmlElement(c, depth+1)
		if err != nil {
			return nil, err
		}
		// Element values are never arrays themselves, so an array under a name means "repeated element".
		existing, ok := m.Get(c.name)
		switch {
		case !ok:
			m.Set(c.name, v)
		default:
			if list, isList := existing.([]any); isList {
				m.Set(c.name, append(list, v))
			} else {
				m.Set(c.name, []any{existing, v})
			}
		}
	}
	return m, nil
}

// ---- writing ----

func writeXML(root any) (string, error) {
	w := &xmlWriter{}
	w.b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	scope := &xmlScope{}
	written := &xmlScope{}
	var err error
	if m, ok := root.(*Map); ok && m.Len() == 1 {
		name, value := m.At(0)
		if _, isList := value.([]any); name != "" && name[0] != '@' && name[0] != '#' && !isList {
			err = w.element(name, value, scope, written, 0, false)
			if err != nil {
				return "", err
			}
			return w.b.String() + "\n", nil
		}
	}
	if err = w.element(xmlRootName, root, scope, written, 0, false); err != nil {
		return "", err
	}
	return w.b.String() + "\n", nil
}

type xmlWriter struct{ b strings.Builder }

// xmlScope holds namespace prefixes declared on an element and its ancestors.
type xmlScope struct {
	parent *xmlScope
	ns     map[string]string
}

func (s *xmlScope) child() *xmlScope { return &xmlScope{parent: s, ns: map[string]string{}} }

func (s *xmlScope) lookup(prefix string) (string, bool) {
	for c := s; c != nil; c = c.parent {
		if v, ok := c.ns[prefix]; ok {
			return v, true
		}
	}
	return "", false
}

// resolve is the prefix, local name and namespace for a key. Elements without
// a prefix take the default namespace; attributes don't. A prefix nobody
// declared becomes part of the escaped local name.
func (s *xmlScope) resolve(name string, element bool) (prefix, local, ns string) {
	if colon := strings.IndexByte(name, ':'); colon > 0 && colon < len(name)-1 {
		p := name[:colon]
		var uri string
		if p == "xml" {
			uri = xmlNS
		} else {
			uri, _ = s.lookup(p)
		}
		if uri != "" && isNCName(p) {
			return p, encodeXMLName(name[colon+1:]), uri
		}
	}
	if element {
		ns, _ = s.lookup("")
	}
	return "", encodeXMLName(name), ns
}

func (w *xmlWriter) element(name string, value any, parent, writtenParent *xmlScope, depth int, mixed bool) error {
	if depth > MaxDepth {
		return errTooDeep
	}
	// Namespace declarations of this element ("@xmlns", "@xmlns:x") apply to its own name too.
	scope := parent.child()
	m, isMap := value.(*Map)
	if isMap {
		for _, k := range m.keys {
			if k == "@xmlns" {
				scope.ns[""] = text(m.vals[k])
			} else if strings.HasPrefix(k, "@xmlns:") {
				scope.ns[k[7:]] = text(m.vals[k])
			}
		}
	}
	written := writtenParent.child()
	prefix, local, ns := scope.resolve(name, true)
	if !mixed {
		w.b.WriteByte('\n')
		w.b.WriteString(strings.Repeat("  ", depth))
	}
	w.b.WriteByte('<')
	w.b.WriteString(qname(prefix, local))
	attr := func(n, v string) {
		w.b.WriteString(" " + n + `="` + escapeXMLAttr(v) + `"`)
	}
	declare := func(prefix, uri string) {
		if prefix == "xml" {
			return
		}
		if cur, _ := written.lookup(prefix); cur == uri {
			return
		}
		written.ns[prefix] = uri
		if prefix == "" {
			attr("xmlns", uri)
		} else {
			attr("xmlns:"+prefix, uri)
		}
	}
	type pending struct{ prefix, uri string }
	var needs []pending
	if isMap {
		for _, k := range m.keys {
			if !strings.HasPrefix(k, "@") {
				continue
			}
			a, item := k[1:], m.vals[k]
			var t string
			if isScalar(item) {
				t = cleanXML(text(item))
			} else {
				t = cleanXML(strings.TrimSpace(compactJSON(item)))
			}
			switch {
			case a == "xmlns":
				declare("", t)
			case strings.HasPrefix(a, "xmlns:"):
				declare(a[6:], t)
			default:
				ap, al, an := scope.resolve(a, false)
				attr(qname(ap, al), t)
				if ap != "" {
					needs = append(needs, pending{ap, an})
				}
			}
		}
	}
	declare(prefix, ns)
	for _, n := range needs {
		declare(n.prefix, n.uri)
	}

	switch x := value.(type) {
	case *Map:
		content, hasText := x.Get(xmlTextKey)
		hasText = hasText && content != nil
		var children []string
		for _, k := range x.keys {
			if !strings.HasPrefix(k, "@") && k != xmlTextKey {
				children = append(children, k)
			}
		}
		if !hasText && len(children) == 0 {
			w.b.WriteString(" />")
			return nil
		}
		w.b.WriteByte('>')
		childMixed := mixed
		if hasText {
			w.b.WriteString(escapeXMLText(cleanXML(text(content))))
			childMixed = true
		}
		wroteChild := false
		for _, k := range children {
			if list, ok := x.vals[k].([]any); ok {
				for _, each := range list {
					if err := w.element(k, each, scope, written, depth+1, childMixed); err != nil {
						return err
					}
					wroteChild = true
				}
			} else {
				if err := w.element(k, x.vals[k], scope, written, depth+1, childMixed); err != nil {
					return err
				}
				wroteChild = true
			}
		}
		w.end(prefix, local, depth, wroteChild && !childMixed)
	case []any:
		// An array inside an array (or at the root): its items have no name of their own.
		if len(x) == 0 {
			w.b.WriteString(" />")
			return nil
		}
		w.b.WriteByte('>')
		for _, item := range x {
			if err := w.element(xmlItemName, item, scope, written, depth+1, mixed); err != nil {
				return err
			}
		}
		w.end(prefix, local, depth, !mixed)
	case nil:
		w.b.WriteString(" />")
	default:
		// "" stays <a></a>
		w.b.WriteByte('>')
		w.b.WriteString(escapeXMLText(cleanXML(text(value))))
		w.end(prefix, local, depth, false)
	}
	return nil
}

func (w *xmlWriter) end(prefix, local string, depth int, indent bool) {
	if indent {
		w.b.WriteByte('\n')
		w.b.WriteString(strings.Repeat("  ", depth))
	}
	w.b.WriteString("</" + qname(prefix, local) + ">")
}

func qname(prefix, local string) string {
	if prefix == "" {
		return local
	}
	return prefix + ":" + local
}

func escapeXMLText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func escapeXMLAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "&#xA;", "\r", "&#xD;", "\t", "&#x9;").Replace(s)
}

func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

// cleanXML removes characters XML 1.0 cannot hold (most control characters).
func cleanXML(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !isXMLChar(r) {
			return -1
		}
		return r
	}, s)
}

func isNameStart(r rune) bool {
	return r >= 'A' && r <= 'Z' || r == '_' || r >= 'a' && r <= 'z' || r >= 0xC0 && r <= 0xD6 || r >= 0xD8 && r <= 0xF6 ||
		r >= 0xF8 && r <= 0x2FF || r >= 0x370 && r <= 0x37D || r >= 0x37F && r <= 0x1FFF || r >= 0x200C && r <= 0x200D ||
		r >= 0x2070 && r <= 0x218F || r >= 0x2C00 && r <= 0x2FEF || r >= 0x3001 && r <= 0xD7FF || r >= 0xF900 && r <= 0xFDCF ||
		r >= 0xFDF0 && r <= 0xFFFD || r >= 0x10000 && r <= 0xEFFFF
}

func isNameChar(r rune) bool {
	return isNameStart(r) || r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xB7 || r >= 0x300 && r <= 0x36F || r >= 0x203F && r <= 0x2040
}

func isNCName(s string) bool {
	for i, r := range []rune(s) {
		if i == 0 && !isNameStart(r) || !isNameChar(r) {
			return false
		}
	}
	return s != ""
}

var reEscapedName = regexp.MustCompile(`^_[xX]([0-9a-fA-F]{4}|[0-9a-fA-F]{8})_`)

// encodeXMLName is XmlConvert.EncodeLocalName: "first name" → "first_x0020_name"; "" is "_".
func encodeXMLName(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		valid := isNameChar(r)
		if i == 0 {
			valid = isNameStart(r)
		}
		switch {
		case r == '_' && reEscapedName.MatchString(string(runes[i:])):
			b.WriteString("_x005F_")
		case !valid || r == ':':
			if r > 0xFFFF {
				fmt.Fprintf(&b, "_x%08X_", r)
			} else {
				fmt.Fprintf(&b, "_x%04X_", r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var reEscapes = regexp.MustCompile(`_[xX]([0-9a-fA-F]{8}|[0-9a-fA-F]{4})_`)

// decodeXMLName is XmlConvert.DecodeName.
func decodeXMLName(name string) string {
	if !strings.Contains(name, "_") {
		return name
	}
	return reEscapes.ReplaceAllStringFunc(name, func(m string) string {
		n, err := strconv.ParseUint(m[2:len(m)-1], 16, 32)
		if err != nil || !utf8.ValidRune(rune(n)) {
			return m
		}
		return string(rune(n))
	})
}
