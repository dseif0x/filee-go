// Package xmltree is a small XML tree that keeps both the namespace URI and the prefix of every name, so documents
// can be matched by namespace (OOXML) or by local name (OWPML), edited, and written back with the prefixes their
// readers expect (한글 matches "hp:", "hh:" ... literally). Whitespace text is kept: <hp:t> </hp:t> is a space.
package xmltree

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// Node is an *Element, a *Text or an *Other.
type Node interface{ node() }

// Element is one XML element.
type Element struct {
	Space  string // namespace URI ("" when none)
	Prefix string
	Local  string
	Attrs  []Attr
	Kids   []Node
	Parent *Element
}

// Attr is one attribute; a namespace declaration has Prefix "xmlns" (or Local "xmlns" for the default one).
type Attr struct {
	Space, Prefix, Local, Value string
}

// Text is character data.
type Text struct{ Data string }

// Other is a comment, processing instruction or directive, kept as written.
type Other struct{ Raw string }

func (*Element) node() {}
func (*Text) node()    {}
func (*Other) node()   {}

// Document is a parsed file: what comes before the root (declaration, comments) and the root.
type Document struct {
	Prolog []Node
	Root   *Element
}

// Parse reads a document.
func Parse(r io.Reader) (*Document, error) {
	d := xml.NewDecoder(r)
	d.Strict = true
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		// Declarations naming UTF-8 differently ("utf8") or ASCII are read as UTF-8.
		return input, nil
	}
	doc := &Document{}
	var stack []*Element
	var scopes []map[string]string
	lookup := func(prefix string) string {
		for i := len(scopes) - 1; i >= 0; i-- {
			if v, ok := scopes[i][prefix]; ok {
				return v
			}
		}
		switch prefix {
		case "xml":
			return "http://www.w3.org/XML/1998/namespace"
		case "xmlns":
			return "http://www.w3.org/2000/xmlns/"
		}
		return ""
	}
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
			ns := map[string]string{}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					ns[a.Name.Local] = a.Value
				} else if a.Name.Space == "" && a.Name.Local == "xmlns" {
					ns[""] = a.Value
				}
			}
			scopes = append(scopes, ns)
			e := &Element{Prefix: t.Name.Space, Local: t.Name.Local}
			e.Space = lookup(e.Prefix)
			for _, a := range t.Attr {
				at := Attr{Prefix: a.Name.Space, Local: a.Name.Local, Value: a.Value}
				if at.Prefix != "" && at.Prefix != "xmlns" {
					at.Space = lookup(at.Prefix)
				}
				e.Attrs = append(e.Attrs, at)
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				e.Parent = p
				p.Kids = append(p.Kids, e)
			} else if doc.Root == nil {
				doc.Root = e
			} else {
				return nil, errors.New("xml: more than one root element")
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("xml: unexpected end element")
			}
			top := stack[len(stack)-1]
			if top.Local != t.Name.Local || top.Prefix != t.Name.Space {
				return nil, errors.New("xml: element <" + top.Local + "> closed by </" + t.Name.Local + ">")
			}
			stack = stack[:len(stack)-1]
			scopes = scopes[:len(scopes)-1]
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				if n := len(p.Kids); n > 0 {
					if txt, ok := p.Kids[n-1].(*Text); ok {
						txt.Data += string(t)
						continue
					}
				}
				p.Kids = append(p.Kids, &Text{Data: string(t)})
			}
		case xml.Comment:
			o := &Other{Raw: "<!--" + string(t) + "-->"}
			if len(stack) > 0 {
				stack[len(stack)-1].Kids = append(stack[len(stack)-1].Kids, o)
			} else if doc.Root == nil {
				doc.Prolog = append(doc.Prolog, o)
			}
		case xml.ProcInst:
			if doc.Root == nil && len(stack) == 0 {
				doc.Prolog = append(doc.Prolog, &Other{Raw: "<?" + t.Target + " " + string(t.Inst) + "?>"})
			}
		case xml.Directive:
			if doc.Root == nil && len(stack) == 0 {
				doc.Prolog = append(doc.Prolog, &Other{Raw: "<!" + string(t) + ">"})
			}
		}
	}
	if len(stack) > 0 {
		return nil, errors.New("xml: unexpected end of file")
	}
	if doc.Root == nil {
		return nil, errors.New("xml: no root element")
	}
	return doc, nil
}

// ParseBytes parses a document held in memory.
func ParseBytes(b []byte) (*Document, error) { return Parse(bytes.NewReader(b)) }

// ParseString parses a document held in a string.
func ParseString(s string) (*Document, error) { return Parse(strings.NewReader(s)) }

// ---- navigation ----

// Elements returns the child elements.
func (e *Element) Elements() []*Element {
	if e == nil {
		return nil
	}
	out := make([]*Element, 0, len(e.Kids))
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok {
			out = append(out, c)
		}
	}
	return out
}

// Child is the first child element with this local name (any namespace), or nil.
func (e *Element) Child(local string) *Element {
	if e == nil {
		return nil
	}
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok && c.Local == local {
			return c
		}
	}
	return nil
}

// ChildNS is the first child element with this namespace and local name, or nil.
func (e *Element) ChildNS(space, local string) *Element {
	if e == nil {
		return nil
	}
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok && c.Local == local && c.Space == space {
			return c
		}
	}
	return nil
}

// ChildrenNS are the child elements with this namespace and local name.
func (e *Element) ChildrenNS(space, local string) []*Element {
	if e == nil {
		return nil
	}
	var out []*Element
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok && c.Local == local && c.Space == space {
			out = append(out, c)
		}
	}
	return out
}

// Children are the child elements with this local name.
func (e *Element) Children(local string) []*Element {
	if e == nil {
		return nil
	}
	var out []*Element
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok && c.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// Is tells whether the element has this namespace and local name.
func (e *Element) Is(space, local string) bool { return e != nil && e.Local == local && e.Space == space }

// Walk calls fn for every descendant element (not e itself), depth first, in document order. fn returning false
// skips the element's own descendants.
func (e *Element) Walk(fn func(*Element) bool) {
	if e == nil {
		return
	}
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok {
			if fn(c) {
				c.Walk(fn)
			}
		}
	}
}

// Descendants are every element below e in document order.
func (e *Element) Descendants() []*Element {
	var out []*Element
	e.Walk(func(c *Element) bool { out = append(out, c); return true })
	return out
}

// DescendantsNamed are the elements below e with this local name.
func (e *Element) DescendantsNamed(local string) []*Element {
	var out []*Element
	e.Walk(func(c *Element) bool {
		if c.Local == local {
			out = append(out, c)
		}
		return true
	})
	return out
}

// DescendantsNS are the elements below e with this namespace and local name.
func (e *Element) DescendantsNS(space, local string) []*Element {
	var out []*Element
	e.Walk(func(c *Element) bool {
		if c.Local == local && c.Space == space {
			out = append(out, c)
		}
		return true
	})
	return out
}

// FirstDescendant is the first element below e with this local name, or nil.
func (e *Element) FirstDescendant(local string) *Element {
	var found *Element
	var walk func(*Element) bool
	walk = func(p *Element) bool {
		for _, k := range p.Kids {
			if c, ok := k.(*Element); ok {
				if c.Local == local {
					found = c
					return true
				}
				if walk(c) {
					return true
				}
			}
		}
		return false
	}
	if e != nil {
		walk(e)
	}
	return found
}

// FirstDescendantNS is the first element below e with this namespace and local name, or nil.
func (e *Element) FirstDescendantNS(space, local string) *Element {
	var found *Element
	var walk func(*Element) bool
	walk = func(p *Element) bool {
		for _, k := range p.Kids {
			if c, ok := k.(*Element); ok {
				if c.Local == local && c.Space == space {
					found = c
					return true
				}
				if walk(c) {
					return true
				}
			}
		}
		return false
	}
	if e != nil {
		walk(e)
	}
	return found
}

// Ancestors are the parents of e, nearest first.
func (e *Element) Ancestors() []*Element {
	var out []*Element
	for p := e.Parent; p != nil; p = p.Parent {
		out = append(out, p)
	}
	return out
}

// Value is the text of the element and all its descendants.
func (e *Element) Value() string {
	if e == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*Element)
	walk = func(p *Element) {
		for _, k := range p.Kids {
			switch c := k.(type) {
			case *Text:
				sb.WriteString(c.Data)
			case *Element:
				walk(c)
			}
		}
	}
	walk(e)
	return sb.String()
}

// SetValue replaces the content with text.
func (e *Element) SetValue(text string) {
	for _, k := range e.Kids {
		if c, ok := k.(*Element); ok {
			c.Parent = nil
		}
	}
	e.Kids = []Node{&Text{Data: text}}
}

// ---- attributes ----

// Attr is the value of the attribute with this local name and no prefix; ok is false when it is missing.
func (e *Element) Attr(local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Local == local && a.Prefix == "" {
			return a.Value, true
		}
	}
	return "", false
}

// AttrAny is the value of the first attribute with this local name, with or without a prefix.
func (e *Element) AttrAny(local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Local == local && a.Prefix != "xmlns" {
			return a.Value, true
		}
	}
	return "", false
}

// AttrNS is the value of the attribute with this namespace and local name.
func (e *Element) AttrNS(space, local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Local == local && a.Space == space && a.Prefix != "" {
			return a.Value, true
		}
	}
	return "", false
}

// A is Attr without the flag ("" when missing).
func (e *Element) A(local string) string {
	v, _ := e.Attr(local)
	return v
}

// SetAttr sets (or adds) an attribute without a prefix.
func (e *Element) SetAttr(local, value string) {
	for i, a := range e.Attrs {
		if a.Local == local && a.Prefix == "" {
			e.Attrs[i].Value = value
			return
		}
	}
	e.Attrs = append(e.Attrs, Attr{Local: local, Value: value})
}

// SetAttrNS sets (or adds) a prefixed attribute.
func (e *Element) SetAttrNS(prefix, space, local, value string) {
	for i, a := range e.Attrs {
		if a.Local == local && a.Space == space && a.Prefix != "" {
			e.Attrs[i].Value = value
			return
		}
	}
	e.Attrs = append(e.Attrs, Attr{Prefix: prefix, Space: space, Local: local, Value: value})
}

// RemoveAttr removes an unprefixed attribute.
func (e *Element) RemoveAttr(local string) {
	for i, a := range e.Attrs {
		if a.Local == local && a.Prefix == "" {
			e.Attrs = append(e.Attrs[:i], e.Attrs[i+1:]...)
			return
		}
	}
}

// ---- editing ----

// New makes an element with the namespace and prefix of like (or none).
func New(prefix, space, local string, attrs ...string) *Element {
	e := &Element{Prefix: prefix, Space: space, Local: local}
	for i := 0; i+1 < len(attrs); i += 2 {
		e.Attrs = append(e.Attrs, Attr{Local: attrs[i], Value: attrs[i+1]})
	}
	return e
}

// Copy is a deep copy of e without a parent.
func (e *Element) Copy() *Element {
	c := &Element{Space: e.Space, Prefix: e.Prefix, Local: e.Local}
	c.Attrs = append([]Attr(nil), e.Attrs...)
	c.Kids = make([]Node, 0, len(e.Kids))
	for _, k := range e.Kids {
		switch n := k.(type) {
		case *Element:
			nc := n.Copy()
			nc.Parent = c
			c.Kids = append(c.Kids, nc)
		case *Text:
			c.Kids = append(c.Kids, &Text{Data: n.Data})
		case *Other:
			c.Kids = append(c.Kids, &Other{Raw: n.Raw})
		}
	}
	return c
}

// Add appends children (elements or *Text).
func (e *Element) Add(nodes ...Node) {
	for _, n := range nodes {
		if c, ok := n.(*Element); ok {
			c.Parent = e
		}
		e.Kids = append(e.Kids, n)
	}
}

// AddText appends text.
func (e *Element) AddText(text string) { e.Add(&Text{Data: text}) }

// AddFirst inserts a child at the start.
func (e *Element) AddFirst(n Node) {
	if c, ok := n.(*Element); ok {
		c.Parent = e
	}
	e.Kids = append([]Node{n}, e.Kids...)
}

func (e *Element) index(n Node) int {
	for i, k := range e.Kids {
		if k == n {
			return i
		}
	}
	return -1
}

// AddAfterSelf inserts n right after e in its parent.
func (e *Element) AddAfterSelf(n Node) {
	p := e.Parent
	if p == nil {
		return
	}
	i := p.index(e)
	if c, ok := n.(*Element); ok {
		c.Parent = p
	}
	p.Kids = append(p.Kids[:i+1], append([]Node{n}, p.Kids[i+1:]...)...)
}

// AddBeforeSelf inserts n right before e in its parent.
func (e *Element) AddBeforeSelf(n Node) {
	p := e.Parent
	if p == nil {
		return
	}
	i := p.index(e)
	if c, ok := n.(*Element); ok {
		c.Parent = p
	}
	p.Kids = append(p.Kids[:i], append([]Node{n}, p.Kids[i:]...)...)
}

// Remove takes e out of its parent.
func (e *Element) Remove() {
	p := e.Parent
	if p == nil {
		return
	}
	if i := p.index(e); i >= 0 {
		p.Kids = append(p.Kids[:i], p.Kids[i+1:]...)
	}
	e.Parent = nil
}

// ReplaceWith puts nodes in e's place.
func (e *Element) ReplaceWith(nodes ...Node) {
	p := e.Parent
	if p == nil {
		return
	}
	i := p.index(e)
	if i < 0 {
		return
	}
	for _, n := range nodes {
		if c, ok := n.(*Element); ok {
			c.Parent = p
		}
	}
	rest := append([]Node(nil), p.Kids[i+1:]...)
	p.Kids = append(append(p.Kids[:i], nodes...), rest...)
	e.Parent = nil
}

// ---- writing ----

// Name is "prefix:local" or "local".
func (e *Element) Name() string {
	if e.Prefix == "" {
		return e.Local
	}
	return e.Prefix + ":" + e.Local
}

func (a Attr) name() string {
	if a.Prefix == "" {
		return a.Local
	}
	return a.Prefix + ":" + a.Local
}

// String serialises e without formatting.
func (e *Element) String() string {
	var sb strings.Builder
	e.write(&sb)
	return sb.String()
}

func (e *Element) write(sb *strings.Builder) {
	sb.WriteByte('<')
	sb.WriteString(e.Name())
	for _, a := range e.Attrs {
		sb.WriteByte(' ')
		sb.WriteString(a.name())
		sb.WriteString(`="`)
		EscapeAttr(sb, a.Value)
		sb.WriteByte('"')
	}
	if len(e.Kids) == 0 {
		sb.WriteString("/>")
		return
	}
	sb.WriteByte('>')
	for _, k := range e.Kids {
		switch n := k.(type) {
		case *Element:
			n.write(sb)
		case *Text:
			EscapeText(sb, n.Data)
		case *Other:
			sb.WriteString(n.Raw)
		}
	}
	sb.WriteString("</")
	sb.WriteString(e.Name())
	sb.WriteByte('>')
}

// Bytes serialises the whole document with an XML declaration.
func (d *Document) Bytes() []byte {
	var sb strings.Builder
	wroteDecl := false
	for _, n := range d.Prolog {
		if o, ok := n.(*Other); ok {
			if strings.HasPrefix(o.Raw, "<?xml ") {
				wroteDecl = true
			}
			sb.WriteString(o.Raw)
		}
	}
	if !wroteDecl {
		return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + sb.String() + d.Root.String())
	}
	d.Root.write(&sb)
	return []byte(sb.String())
}

// EscapeText writes character data escaped.
func EscapeText(sb *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '\r':
			sb.WriteString("&#xD;")
		default:
			sb.WriteRune(r)
		}
	}
}

// EscapeAttr writes an attribute value escaped.
func EscapeAttr(sb *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\t':
			sb.WriteString("&#x9;")
		case '\n':
			sb.WriteString("&#xA;")
		case '\r':
			sb.WriteString("&#xD;")
		default:
			sb.WriteRune(r)
		}
	}
}

// StripWhitespace removes whitespace-only text between elements (the indentation of pretty-printed files), as
// .NET's XDocument.Parse does without PreserveWhitespace; xml:space="preserve" content is kept.
func (e *Element) StripWhitespace() {
	if v, ok := e.AttrNS("http://www.w3.org/XML/1998/namespace", "space"); ok && v == "preserve" {
		return
	}
	hasElement := false
	for _, k := range e.Kids {
		if _, ok := k.(*Element); ok {
			hasElement = true
			break
		}
	}
	kids := e.Kids[:0]
	for _, k := range e.Kids {
		if t, ok := k.(*Text); ok && hasElement && strings.TrimLeft(t.Data, " \t\r\n") == "" {
			continue
		}
		kids = append(kids, k)
		if c, ok := k.(*Element); ok {
			c.StripWhitespace()
		}
	}
	e.Kids = kids
}
