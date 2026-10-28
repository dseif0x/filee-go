package geo

// XML helpers shared by the KML and GPX formats: a tree built from the token
// stream with limits on depth and element count, and an indenting writer.

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// xmlMaxDepth is how many levels of elements a KML or GPX file may nest (real files nest a dozen).
const xmlMaxDepth = 256

// xmlMaxElements is how many elements a KML or GPX file may hold: each costs
// memory in the tree whatever its size, so a file of tiny elements would use
// far more memory than its size. This allows a million features.
const xmlMaxElements = 3_000_000

// xnode is an element (local name set) or a text node.
type xnode struct {
	local, space string
	attrs        []xml.Attr
	kids         []*xnode
	text         string
	isText       bool
}

func (n *xnode) elements() []*xnode {
	var out []*xnode
	for _, k := range n.kids {
		if !k.isText {
			out = append(out, k)
		}
	}
	return out
}

// first is the first child element called local, or nil.
func (n *xnode) first(local string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if !k.isText && k.local == local {
			return k
		}
	}
	return nil
}

func (n *xnode) named(local string) []*xnode {
	var out []*xnode
	for _, k := range n.kids {
		if !k.isText && k.local == local {
			out = append(out, k)
		}
	}
	return out
}

// descendants are all elements below n in document order.
func (n *xnode) descendants(visit func(*xnode)) {
	for _, k := range n.kids {
		if !k.isText {
			visit(k)
			k.descendants(visit)
		}
	}
}

func (n *xnode) attr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Local == local && a.Name.Space == "" {
			return a.Value, true
		}
	}
	return "", false
}

// value is the concatenated text of n and its descendants.
func (n *xnode) value() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xnode)
	walk = func(x *xnode) {
		for _, k := range x.kids {
			if k.isText {
				b.WriteString(k.text)
			} else {
				walk(k)
			}
		}
	}
	walk(n)
	return b.String()
}

type limitedReader struct {
	r       io.Reader
	n       int64
	message string
}

func (l *limitedReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n < 0 {
		return n, errors.New(l.message)
	}
	return n, err
}

// loadXML parses XML without DTDs or external entities, so a hostile file
// can't read files or expand into gigabytes, and refuses elements nested more
// than xmlMaxDepth levels or more than maxElements elements.
func loadXML(r io.Reader, maxElements int) (*xnode, error) {
	d := xml.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	d.Strict = true
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		e, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return e.NewDecoder().Reader(input), nil
	}
	doc := &xnode{}
	cur := doc
	var stack []*xnode
	elements := 0
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			var se *xml.SyntaxError
			if errors.As(err, &se) {
				return nil, fmt.Errorf("The file is not valid XML (%s, line %d)", se.Msg, se.Line)
			}
			return nil, fmt.Errorf("The file is not valid XML (%v)", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= xmlMaxDepth {
				return nil, fmt.Errorf("The file nests elements more than %d levels deep.", xmlMaxDepth)
			}
			elements++
			if elements > maxElements {
				return nil, fmt.Errorf("The file has more than %s elements.", groupThousands(maxElements))
			}
			n := &xnode{local: t.Name.Local, space: t.Name.Space}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					continue
				}
				n.attrs = append(n.attrs, a)
			}
			cur.kids = append(cur.kids, n)
			stack = append(stack, cur)
			cur = n
		case xml.EndElement:
			if len(stack) == 0 || t.Name.Local != cur.local || t.Name.Space != cur.space {
				return nil, fmt.Errorf("The file is not valid XML (unexpected end element </%s>)", t.Name.Local)
			}
			cur = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case xml.CharData:
			// Text outside the root element (white space between declarations) is no content.
			if cur == doc {
				continue
			}
			if k := len(cur.kids); k > 0 && cur.kids[k-1].isText {
				cur.kids[k-1].text += string(t)
			} else {
				cur.kids = append(cur.kids, &xnode{isText: true, text: string(t)})
			}
		case xml.Directive:
			if bytes.HasPrefix(bytes.TrimSpace(t), []byte("DOCTYPE")) {
				return nil, errors.New("The file is not valid XML (it has a document type declaration, which is not allowed)")
			}
		}
	}
	if len(stack) > 0 {
		return nil, errors.New("The file is not valid XML (unexpected end of file)")
	}
	for _, k := range doc.kids {
		if !k.isText {
			return k, nil
		}
	}
	return nil, errors.New("The file is not valid XML (root element is missing)")
}

func groupThousands(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// cleanXML is text without the control characters XML 1.0 can't hold (they would make the file unreadable).
func cleanXML(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' && r != '\n' && r != '\r' || r == 0xFFFE || r == 0xFFFF {
			return -1
		}
		return r
	}, s)
}

func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeAttr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\n':
			b.WriteString("&#xA;")
		case '\r':
			b.WriteString("&#xD;")
		case '\t':
			b.WriteString("&#x9;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// xmlWriter writes indented XML (two spaces) like .NET's XmlWriter with Indent.
type xmlWriter struct {
	w     *bufio.Writer
	stack []string
	// open: the start tag of the innermost element is not closed yet (">" not written).
	open bool
	// hasChildren of each open element.
	hasChildren []bool
}

func newXMLWriter(w io.Writer) *xmlWriter {
	x := &xmlWriter{w: bufio.NewWriterSize(w, 1<<16)}
	x.w.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	return x
}

func (x *xmlWriter) closeStart() {
	if x.open {
		x.w.WriteString(">")
		x.open = false
	}
}

func (x *xmlWriter) newline() {
	x.w.WriteString("\n")
	x.w.WriteString(strings.Repeat("  ", len(x.stack)))
}

// start opens an element; attrs are name, value pairs.
func (x *xmlWriter) start(name string, attrs ...string) {
	x.closeStart()
	if n := len(x.hasChildren); n > 0 {
		x.hasChildren[n-1] = true
	}
	x.newline()
	x.w.WriteString("<" + name)
	for i := 0; i+1 < len(attrs); i += 2 {
		x.w.WriteString(" " + attrs[i] + `="` + escapeAttr(attrs[i+1]) + `"`)
	}
	x.stack = append(x.stack, name)
	x.hasChildren = append(x.hasChildren, false)
	x.open = true
}

// end closes the innermost element ("/>" when it is empty).
func (x *xmlWriter) end() {
	name := x.stack[len(x.stack)-1]
	children := x.hasChildren[len(x.hasChildren)-1]
	x.stack = x.stack[:len(x.stack)-1]
	x.hasChildren = x.hasChildren[:len(x.hasChildren)-1]
	if x.open {
		x.w.WriteString(" />")
		x.open = false
		return
	}
	if children {
		x.newline()
	}
	x.w.WriteString("</" + name + ">")
}

// element writes <name>text</name> on one line.
func (x *xmlWriter) element(name, text string, attrs ...string) {
	x.start(name, attrs...)
	x.closeStart()
	x.w.WriteString(escapeText(text))
	x.stack = x.stack[:len(x.stack)-1]
	x.hasChildren = x.hasChildren[:len(x.hasChildren)-1]
	x.w.WriteString("</" + name + ">")
}

func (x *xmlWriter) flush() error { return x.w.Flush() }
